package service

import (
	"fmt"
	"math"
	"time"

	"github.com/shopspring/decimal"
)

// 本文件是「代理佣金」（分销代理分佣）的纯计算层。
//
// 设计约束（P3 计费链路与 P4 管理端 API 共用）：
//   - 所有导出函数都是纯函数：不读 DB、不读配置、不读全局状态，输入即全部信息；
//   - 金额一律量化到 UsageBillingMonetaryScale（NUMERIC(20,8)）刻度，
//     且 child_paid - agent_cost == commission 恒定成立，否则逐笔累加后
//     代理余额与流水会出现 1e-8 级偏差、无法精确对账；
//   - 非法输入不做静默补贴：倍率非法直接不产生佣金，售价低于成本由
//     ValidateChildRate 在配置写入时拦截，计算层只保证「不产生负数佣金」。
//
// 一级代理口径（首期）：调用方只传入「直接上级」的倍率快照，多级分佣不在本层展开；
// 若后续要支持多级，应在调用方按层级多次调用本层函数再汇总，而不是把层级塞进输入。

// 佣金账目状态。冻结期未满 → pending（不可结转/提现）；无冻结或已到期 → available。
//
// 两个取值必须与 repository.AgentCommissionStatusPending / AgentCommissionStatusAvailable
// 以及 migration 292 里 agent_commission_ledger.status 的 CHECK 约束保持一致。
// service 层不 import repository（该包反向依赖 service，导入即成环），所以这里是
// 独立字面量，而非别名；withdrawn / reversed 属于账本迁移状态，由仓储层驱动。
const (
	// CommissionStatusPending 表示佣金已记账但仍在冻结期内。
	CommissionStatusPending = "pending"
	// CommissionStatusAvailable 表示佣金已解冻，可结转/提现。
	CommissionStatusAvailable = "available"
)

// CommissionInput 是一次用量计佣所需的输入快照。
//
// 倍率语义统一为「售价倍率」：0.5 = 5 折，1.0 = 原价，2.0 = 双倍。
type CommissionInput struct {
	TotalCost     float64 // 倍率前的目录价（CostBreakdown.TotalCost，不含任何售价倍率）
	ChildRate     float64 // 下级售价倍率：下级实付 = TotalCost * ChildRate
	AgentCostRate float64 // 代理成本倍率：代理成本 = TotalCost * AgentCostRate
	MinCommission float64 // 最低计佣阈值，低于该值的佣金丢弃（<=0 / NaN / +Inf 表示不设阈值）
}

// CommissionBreakdown 是逐笔佣金的计算结果。
//
// 三个金额均已量化到 UsageBillingMonetaryScale 位小数，并满足
// Commission == ChildPaid - AgentCost（十进制口径严格相等）。
// Eligible=false 时三个金额一律为 0，因此该恒等关系在任何返回路径上都成立：
// 不记账的行不需要参与对账，把金额归零可以避免调用方误用未入账的差额。
type CommissionBreakdown struct {
	ChildPaid  float64 // 下级实付 = TotalCost * ChildRate
	AgentCost  float64 // 代理成本 = TotalCost * AgentCostRate
	Commission float64 // 佣金 = ChildPaid - AgentCost
	// Eligible 表示这笔是否应当记账（差价 > 0 且 >= 最低计佣阈值）
	Eligible bool // 是否记账：Eligible=false 时上面三个金额恒为 0
}

// CalculateCommission 计算一次用量的佣金。
//
// 佣金 = 下级实付 - 代理成本 = TotalCost * (ChildRate - AgentCostRate)。
//
// 不返回 error 是有意的：本函数被放在每条用量的计费热路径上，而「不产生佣金」
// （倍率非法、目录价为 0、售价不高于成本、低于阈值）都是正常业务结果而不是故障。
// 需要拒绝的配置错误由 ValidateChildRate / ValidateAgentCostMultiplier 在写入时拦截。
func CalculateCommission(input CommissionInput) CommissionBreakdown {
	// 倍率非法时「实付 - 成本」没有任何可靠含义，宁可返回零账目也不制造
	// 一笔可能为负、NaN 或 +Inf 的佣金把代理余额写坏。
	childRate, childOK := NormalizeMultiplier(input.ChildRate)
	agentCostRate, agentCostOK := NormalizeMultiplier(input.AgentCostRate)
	if !childOK || !agentCostOK {
		return CommissionBreakdown{}
	}
	if math.IsNaN(input.TotalCost) || math.IsInf(input.TotalCost, 0) || input.TotalCost <= 0 {
		return CommissionBreakdown{}
	}

	// 两笔金额各自先量化到 8 位小数，再用「已量化的两笔金额」求差作为佣金：
	//   * 量化后的差值与 DB 里存的两个金额同刻度，Commission == ChildPaid - AgentCost
	//     在十进制下严格成立，逐笔累加不会漂出 1e-8；
	//   * 若改成「先按 raw 差值算佣金、再单独量化」，raw 差值的第 9 位与两个已量化
	//     金额的第 9 位可能落在舍入边界的同一侧或两侧，于是 commission 与
	//     child_paid - agent_cost 相差恰好 1e-8，对账时表现为账不平。
	childPaidRaw := input.TotalCost * childRate
	agentCostRaw := input.TotalCost * agentCostRate
	// 倍率合法不代表乘积有限：1e308 的目录价乘 10 就会溢出成 +Inf，而 shopspring/decimal
	// 的 NewFromFloat 遇到 Inf/NaN 是 panic 而不是返回错误。这里先做有限性检查，
	// 避免一条脏配置把整条计费链路打崩。
	if !isFiniteAmount(childPaidRaw) || !isFiniteAmount(agentCostRaw) {
		return CommissionBreakdown{}
	}

	childPaid := QuantizeUsageBillingAmount(childPaidRaw)
	agentCost := QuantizeUsageBillingAmount(agentCostRaw)
	commission := quantizeCommissionDifference(childPaid, agentCost)

	// 兜底：量化/求差后再验一次有限性。NaN 与任何数比较都是 false，
	// 若只写 commission <= 0，NaN 佣金会漏到记账层，把代理余额写成 NaN。
	if !isFiniteAmount(commission) || commission <= 0 {
		// 下级售价不高于代理成本（含"相等后量化成零差价"）：不产生佣金、不报错。
		return CommissionBreakdown{}
	}
	// 阈值只是防刷小额的护栏，不是资损防线：脏阈值（NaN/Inf）按"不设阈值"处理，
	// 避免 NaN 比较恒为 false 而在合规的行上静默丢单。
	if minCommission := input.MinCommission; minCommission > 0 && !math.IsInf(minCommission, 0) && commission < minCommission {
		return CommissionBreakdown{}
	}

	return CommissionBreakdown{
		ChildPaid:  childPaid,
		AgentCost:  agentCost,
		Commission: commission,
		Eligible:   true,
	}
}

// isFiniteAmount 报告金额是否为可用于 decimal 运算的有限数。
func isFiniteAmount(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}

// quantizeCommissionDifference 在 decimal 域内计算 childPaid - agentCost 并量化到
// UsageBillingMonetaryScale 位小数。
//
// 为什么不在 float64 上直接相减：两个金额虽然都落在 1e-8 刻度上，但 float64 表示不了
// 1e-8，0.6-0.5 会算出 0.09999999999999998；把这串二进制噪声再量化虽然多数情况能
// 回到 0.1，但在舍入边界上仍可能被推到错误的一侧。decimal 对 8 位小数是精确十进制
// 运算，差值本身就在刻度上，再做一次 half-away-from-zero 舍入通常是恒等操作。
func quantizeCommissionDifference(childPaid, agentCost float64) float64 {
	diff := decimal.NewFromFloat(childPaid).Sub(decimal.NewFromFloat(agentCost))
	quantized, _ := diff.Round(UsageBillingMonetaryScale).Float64()
	return quantized
}

// NormalizeMultiplier 归一化一个售价/成本倍率：非法（NaN、±Inf、<=0）返回 (0,false)，
// 合法则原样返回。
//
// 不把 <=0 归一化成 1.0 是有意的：0 倍率在业务上等于"免费"，静默改成 1.0 会把
// 误配置放大成资损；调用方拿到 false 后按「逐层跳过」显式降级，降级链路可测。
func NormalizeMultiplier(v float64) (float64, bool) {
	if math.IsNaN(v) || math.IsInf(v, 0) || v <= 0 {
		return 0, false
	}
	return v, true
}

// ValidateAgentCostMultiplier 校验代理成本倍率配置：必须有限且 > 0。
//
// 成本倍率是佣金的上界来源（低于成本的售价不产生佣金），配成 0 或负数会让所有
// 下级用量的佣金恒等于下级实付，等价于把整笔收入打给代理，因此在写入时拦截。
func ValidateAgentCostMultiplier(v float64) error {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return fmt.Errorf("代理成本倍率必须是有限数值，got %v", v)
	}
	if v <= 0 {
		return fmt.Errorf("代理成本倍率必须大于 0（0.5 = 5 折，1.0 = 原价），got %v", v)
	}
	return nil
}

// ValidateChildRate 校验下级售价倍率不得低于代理成本倍率。
//
// 相等是允许的：零佣金的正常配置（代理按成本价放给下级，用于引流）。
// 低于成本才是配置错误——每笔用量都会产生负数佣金，属于资损而不是折扣。
func ValidateChildRate(childRate, agentCostRate float64) error {
	child, childOK := NormalizeMultiplier(childRate)
	if !childOK {
		return fmt.Errorf("下级售价倍率必须是有限正数，got %v", childRate)
	}
	cost, costOK := NormalizeMultiplier(agentCostRate)
	if !costOK {
		return fmt.Errorf("代理成本倍率必须是有限正数，got %v", agentCostRate)
	}
	if child < cost {
		return fmt.Errorf("下级售价倍率不能低于代理成本倍率：售价 %v 低于成本 %v", child, cost)
	}
	return nil
}

// ResolveSellingMultiplier 解析下级实际生效的售价倍率。
//
// 优先级：下级专属覆盖 > 代理为该下级配置的默认值 > 分组默认倍率。
// 非法值（nil 以外的脏数据：0、负数、NaN、Inf）逐层跳过而不是报错，
// 保证计费热路径永不被脏配置打断；若三层全部非法，最终回落到原价 1.0
// ——与 PeakMultiplierAt 的安全降级口径一致，绝不降级成 0（免费）。
func ResolveSellingMultiplier(childOverride, agentDefault *float64, groupDefault float64) float64 {
	if childOverride != nil {
		if v, ok := NormalizeMultiplier(*childOverride); ok {
			return v
		}
	}
	if agentDefault != nil {
		if v, ok := NormalizeMultiplier(*agentDefault); ok {
			return v
		}
	}
	if v, ok := NormalizeMultiplier(groupDefault); ok {
		return v
	}
	return 1.0
}

// ResolveAgentCostMultiplier 解析代理在某分组上的成本倍率。
//
// 管理员为该代理在该分组配置了成本倍率时用配置值；否则回落到代理自己的售价倍率，
// 即"代理自用按成本价"：代理自己调用的用量不产生佣金（ChildRate == AgentCostRate），
// 避免管理员只配售价就意外让代理从自己的用量里赚差价。
// ResolveAgentCostMultiplier 解析代理成本倍率。
//
// configured 为管理员按「代理 × 分组」配置的成本折扣；未配置时回落到 fallbackRate。
// 计费链路传入的是本次请求的售价倍率，因此未配置成本时差价恒为 0、不产生佣金。
// 这是有意选择的保守方向：少发佣金可以事后补发，按猜测的成本多发则难以追回。
func ResolveAgentCostMultiplier(configured *float64, fallbackRate float64) float64 {
	if configured != nil {
		if v, ok := NormalizeMultiplier(*configured); ok {
			return v
		}
	}
	if v, ok := NormalizeMultiplier(fallbackRate); ok {
		return v
	}
	return 1.0
}

// ThawStatus 返回佣金账目的解冻状态：pending（冻结中）或 available（可用）。
//
// 纯函数，便于用固定时刻穷举边界；解冻判定点取"到期时刻已到即解冻"
// （frozenUntil == now 返回 available），与 affiliate 侧 frozen_until <= NOW() 的口径一致。
// frozenUntil 为 nil 或零值表示从未冻结，直接 available。
func ThawStatus(frozenUntil *time.Time, now time.Time) string {
	if frozenUntil == nil || frozenUntil.IsZero() {
		return CommissionStatusAvailable
	}
	if frozenUntil.After(now) {
		return CommissionStatusPending
	}
	return CommissionStatusAvailable
}

// CommissionFrozenUntil 计算佣金冻结到期时刻。
//
// hours <= 0 表示不冻结，返回 nil（与 AffiliateRebateFreezeHoursDefault = 0 的
// 向后兼容语义一致，落库为 NULL 而不是 now，避免"冻结到当下"这种立即到期却
// 仍被判为有冻结期的中间态）。
func CommissionFrozenUntil(now time.Time, hours int) *time.Time {
	if hours <= 0 {
		return nil
	}
	frozenUntil := now.Add(time.Duration(hours) * time.Hour)
	return &frozenUntil
}
