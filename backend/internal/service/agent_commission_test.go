//go:build unit

package service

import (
	"math"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// 计佣恒等关系的测试辅助
// ---------------------------------------------------------------------------

// commissionRatePtr 返回倍率指针，用于构造 *float64 形式的配置入参。
// 命名带 commission 前缀，避免与同包其它测试文件的 floatPtr/ptrFloat64 冲突。
func commissionRatePtr(v float64) *float64 { return &v }

// commissionDecimalPlaces 返回金额最短十进制表示的小数位数，
// 即 PostgreSQL 把 float8 参数转成 numeric 时看到的刻度。
func commissionDecimalPlaces(v float64) int32 {
	return -decimal.NewFromFloat(v).Exponent()
}

// requireCommissionIdentity 断言 child_paid - agent_cost == commission。
//
// 必须走 decimal 比较：0.104 - 0.086 在 float64 上是 0.018000000000000002，
// 直接 require.Equal(got.ChildPaid-got.AgentCost, got.Commission) 会误报失败。
// 库里的三列都是 NUMERIC(20,8)，对账口径是十进制，所以恒等关系也应在十进制域验证。
func requireCommissionIdentity(t *testing.T, got CommissionBreakdown) {
	t.Helper()

	childPaid := decimal.NewFromFloat(got.ChildPaid)
	agentCost := decimal.NewFromFloat(got.AgentCost)
	commission := decimal.NewFromFloat(got.Commission)

	require.True(t, childPaid.Sub(agentCost).Equal(commission),
		"child_paid - agent_cost 必须精确等于 commission：%s - %s != %s",
		childPaid, agentCost, commission)

	// 三个金额都必须已落在 NUMERIC(20,8) 刻度上：否则入库时 PostgreSQL 会再舍入一次，
	// 逐笔累加后与这里校验过的恒等关系脱钩。
	for _, amount := range []float64{got.ChildPaid, got.AgentCost, got.Commission} {
		require.LessOrEqual(t, commissionDecimalPlaces(amount), int32(UsageBillingMonetaryScale),
			"金额 %v 超过 NUMERIC(20,8) 刻度", amount)
	}

	// float64 路径上的差值再量化也应收敛到同一刻度，避免调用方自行做
	// child_paid - agent_cost 得出一个差一个 tick 的佣金。
	require.Equal(t, got.Commission, QuantizeUsageBillingAmount(got.ChildPaid-got.AgentCost),
		"child_paid - agent_cost 量化后必须仍是 commission")
}

// ---------------------------------------------------------------------------
// 1. 基准样例：5 折成本、6 折售价，目录价 1.0 → 佣金 0.1
// ---------------------------------------------------------------------------

func TestCalculateCommission_FiveTenthsCostSixTenthsSelling(t *testing.T) {
	got := CalculateCommission(CommissionInput{
		TotalCost:     1.0,
		ChildRate:     0.6,
		AgentCostRate: 0.5,
	})

	require.True(t, got.Eligible)
	require.Equal(t, 0.6, got.ChildPaid)
	require.Equal(t, 0.5, got.AgentCost)
	require.Equal(t, 0.1, got.Commission,
		"0.6-0.5 在 float64 上是 0.09999999999999998，必须走 decimal 得到精确 0.1")
	requireCommissionIdentity(t, got)
}

// ---------------------------------------------------------------------------
// 2. 长尾目录价：三金额恒等（不能靠 epsilon 比较）
// ---------------------------------------------------------------------------

func TestCalculateCommission_IdentityHoldsOnLongTailTotalCost(t *testing.T) {
	// 0.123456789*0.6 = 0.0740740734 → 量化 0.07407407
	// 0.123456789*0.5 = 0.0617283945 → 量化 0.06172839（第 9 位 4，向下）
	got := CalculateCommission(CommissionInput{
		TotalCost:     0.123456789,
		ChildRate:     0.6,
		AgentCostRate: 0.5,
	})

	require.True(t, got.Eligible)
	require.Equal(t, 0.07407407, got.ChildPaid)
	require.Equal(t, 0.06172839, got.AgentCost)
	require.Equal(t, 0.01234568, got.Commission)
	requireCommissionIdentity(t, got)

	// 这一组数值下，「先量化两笔金额再求差」与「先求 raw 差再量化」恰好同值，
	// 所以本用例只能验算结果；能真正区分两种量化顺序的用例见
	// TestCalculateCommission_QuantizesAmountsBeforeDifferencing。
}

// ---------------------------------------------------------------------------
// 3/4/5. 不产生佣金的三种情形：售价=成本、售价<成本、目录价<=0
// ---------------------------------------------------------------------------

func TestCalculateCommission_SellingEqualsCost(t *testing.T) {
	got := CalculateCommission(CommissionInput{
		TotalCost:     12.5,
		ChildRate:     0.5,
		AgentCostRate: 0.5,
	})

	require.False(t, got.Eligible)
	require.Zero(t, got.Commission)
	// 不记账的行三个金额一律归零：否则「售价 < 成本」时会出现
	// child_paid - agent_cost 为负而 commission 被钳到 0 的破损恒等关系。
	require.Zero(t, got.ChildPaid)
	require.Zero(t, got.AgentCost)
	requireCommissionIdentity(t, got)
}

func TestCalculateCommission_SellingBelowCost(t *testing.T) {
	// 售价低于成本是配置错误（资损），由 ValidateChildRate 在写入时拦截；
	// 计算层只保证不产生负数佣金、不 panic、不报错。
	got := CalculateCommission(CommissionInput{
		TotalCost:     100,
		ChildRate:     0.4,
		AgentCostRate: 0.5,
	})

	require.False(t, got.Eligible)
	require.Zero(t, got.Commission)
	requireCommissionIdentity(t, got)

	// 同一组数值在配置写入阶段必须被拦下。
	require.Error(t, ValidateChildRate(0.4, 0.5))
}

func TestCalculateCommission_NonPositiveTotalCost(t *testing.T) {
	for _, totalCost := range []float64{0, -1, -0.00000001, math.NaN(), math.Inf(1), math.Inf(-1)} {
		got := CalculateCommission(CommissionInput{
			TotalCost:     totalCost,
			ChildRate:     0.6,
			AgentCostRate: 0.5,
		})
		require.False(t, got.Eligible, "TotalCost=%v 不应产生佣金", totalCost)
		require.Zero(t, got.Commission, "TotalCost=%v 不应产生佣金", totalCost)
		requireCommissionIdentity(t, got)
	}
}

// ---------------------------------------------------------------------------
// 6. 最低计佣阈值
// ---------------------------------------------------------------------------

func TestCalculateCommission_MinCommissionThreshold(t *testing.T) {
	// 1e-6 目录价、5.1 折售价、5 折成本 → 佣金恰为一个 tick（1e-8）。
	input := CommissionInput{
		TotalCost:     0.000001,
		ChildRate:     0.51,
		AgentCostRate: 0.5,
	}

	base := CalculateCommission(input)
	require.True(t, base.Eligible)
	require.Equal(t, 0.00000051, base.ChildPaid)
	require.Equal(t, 0.0000005, base.AgentCost)
	require.Equal(t, 1e-8, base.Commission)
	requireCommissionIdentity(t, base)

	// 阈值高于佣金 → 丢弃，且金额一并归零。
	dropped := CalculateCommission(CommissionInput{
		TotalCost:     input.TotalCost,
		ChildRate:     input.ChildRate,
		AgentCostRate: input.AgentCostRate,
		MinCommission: 0.000001,
	})
	require.False(t, dropped.Eligible)
	require.Zero(t, dropped.Commission)
	requireCommissionIdentity(t, dropped)

	// 阈值恰好等于佣金 → 不丢弃（语义是「低于该值」才丢）。
	require.True(t, CalculateCommission(CommissionInput{
		TotalCost:     input.TotalCost,
		ChildRate:     input.ChildRate,
		AgentCostRate: input.AgentCostRate,
		MinCommission: 1e-8,
	}).Eligible)

	// 亚 tick 阈值不会生效：8 位量化后最小正佣金就是 1e-8，
	// 因此 1e-9 这类阈值在 8 位刻度上是空操作（设置它不会丢掉任何合法佣金）。
	subTick := CalculateCommission(CommissionInput{
		TotalCost:     input.TotalCost,
		ChildRate:     input.ChildRate,
		AgentCostRate: input.AgentCostRate,
		MinCommission: 1e-9,
	})
	require.True(t, subTick.Eligible)
	require.Equal(t, 1e-8, subTick.Commission)

	// 脏阈值（NaN / +Inf / 负数）按「不设阈值」处理，避免 NaN 比较恒为 false 而静默丢单。
	for _, threshold := range []float64{math.NaN(), math.Inf(1), 0, -1} {
		got := CalculateCommission(CommissionInput{
			TotalCost:     input.TotalCost,
			ChildRate:     input.ChildRate,
			AgentCostRate: input.AgentCostRate,
			MinCommission: threshold,
		})
		require.True(t, got.Eligible, "MinCommission=%v 应视为不设阈值", threshold)
		require.Equal(t, 1e-8, got.Commission, "MinCommission=%v 应视为不设阈值", threshold)
	}
}

// ---------------------------------------------------------------------------
// 7. NormalizeMultiplier：非法倍率一律返回 false
// ---------------------------------------------------------------------------

func TestNormalizeMultiplier(t *testing.T) {
	tests := []struct {
		name   string
		in     float64
		want   float64
		wantOK bool
	}{
		{"half price", 0.5, 0.5, true},
		{"parity", 1.0, 1.0, true},
		{"double", 2.0, 2.0, true},
		{"tiny positive", 1e-9, 1e-9, true},
		{"large", 100, 100, true},
		{"zero", 0, 0, false},
		{"negative", -1, 0, false},
		{"negative tiny", -1e-9, 0, false},
		{"nan", math.NaN(), 0, false},
		{"positive inf", math.Inf(1), 0, false},
		{"negative inf", math.Inf(-1), 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := NormalizeMultiplier(tt.in)
			require.Equal(t, tt.wantOK, ok)
			require.Equal(t, tt.want, got, "非法输入必须返回 0 而不是原值")
		})
	}
}

// ---------------------------------------------------------------------------
// 8. ResolveSellingMultiplier：三层优先级 + 非法值逐层降级
// ---------------------------------------------------------------------------

func TestResolveSellingMultiplier(t *testing.T) {
	tests := []struct {
		name         string
		child        *float64
		agentDefault *float64
		groupDefault float64
		want         float64
	}{
		{"child override wins", commissionRatePtr(0.6), commissionRatePtr(0.7), 0.8, 0.6},
		{"agent default over group", nil, commissionRatePtr(0.7), 0.8, 0.7},
		{"group default only", nil, nil, 0.8, 0.8},
		{"invalid child falls to agent default", commissionRatePtr(0), commissionRatePtr(0.7), 0.8, 0.7},
		{"negative child falls through to group", commissionRatePtr(-1), nil, 0.8, 0.8},
		{"nan child and inf agent fall to group", commissionRatePtr(math.NaN()), commissionRatePtr(math.Inf(1)), 0.8, 0.8},
		{"all invalid falls back to parity", commissionRatePtr(0), commissionRatePtr(math.NaN()), 0, 1.0},
		{"nil everything stays parity", nil, nil, 0, 1.0},
		{"child override kept even when group invalid", commissionRatePtr(0.55), nil, math.NaN(), 0.55},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, ResolveSellingMultiplier(tt.child, tt.agentDefault, tt.groupDefault))
		})
	}
}

// ---------------------------------------------------------------------------
// 9. ResolveAgentCostMultiplier：配置缺失时回落到代理自身售价
// ---------------------------------------------------------------------------

func TestResolveAgentCostMultiplier(t *testing.T) {
	tests := []struct {
		name            string
		configured      *float64
		agentOwnSelling float64
		want            float64
		wantDescription string
	}{
		{"admin configured", commissionRatePtr(0.5), 0.7, 0.5, "管理员为该代理在该分组配了成本倍率"},
		{"fallback to agent own selling", nil, 0.7, 0.7, "未配置时按代理自用成本价"},
		{"invalid configured falls back", commissionRatePtr(0), 0.7, 0.7, "0 是脏配置，不能当成免费成本"},
		{"nan configured falls back", commissionRatePtr(math.NaN()), 0.7, 0.7, "NaN 配置跳过"},
		{"inf configured falls back", commissionRatePtr(math.Inf(-1)), 0.7, 0.7, "Inf 配置跳过"},
		{"both unusable falls back to parity", nil, 0, 1.0, "代理自身售价也非法时回落原价"},
		{"invalid configured and invalid own", commissionRatePtr(-2), math.NaN(), 1.0, "两层都非法时回落原价"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, ResolveAgentCostMultiplier(tt.configured, tt.agentOwnSelling), tt.wantDescription)
		})
	}

	// 语义闭环：回落到代理自身售价后，代理自用的用量不产生佣金。
	agentOwnSelling := 0.7
	agentCost := ResolveAgentCostMultiplier(nil, agentOwnSelling)
	childRate := ResolveSellingMultiplier(nil, nil, agentOwnSelling)
	ownUsage := CalculateCommission(CommissionInput{
		TotalCost:     10,
		ChildRate:     childRate,
		AgentCostRate: agentCost,
	})
	require.False(t, ownUsage.Eligible, "代理自用按成本价，不应从自己的用量里赚差价")
	require.Zero(t, ownUsage.Commission)
}

// ---------------------------------------------------------------------------
// 10. ValidateChildRate / ValidateAgentCostMultiplier
// ---------------------------------------------------------------------------

func TestValidateChildRate(t *testing.T) {
	require.NoError(t, ValidateChildRate(0.6, 0.5), "售价高于成本合法")
	require.NoError(t, ValidateChildRate(0.5, 0.5), "售价等于成本允许（零佣金）")
	require.NoError(t, ValidateChildRate(1, 0.01), "原价售价合法")

	err := ValidateChildRate(0.4, 0.5)
	require.Error(t, err)
	// 错误信息必须能定位问题：两个数值都要出现，供管理端直接提示。
	require.Contains(t, err.Error(), "0.4")
	require.Contains(t, err.Error(), "0.5")

	require.Error(t, ValidateChildRate(0, 0.5), "0 折售价非法")
	require.Error(t, ValidateChildRate(-1, 0.5), "负售价非法")
	require.Error(t, ValidateChildRate(math.NaN(), 0.5), "NaN 售价非法")
	require.Error(t, ValidateChildRate(0.6, 0), "0 成本非法")
	require.Error(t, ValidateChildRate(0.6, math.Inf(1)), "Inf 成本非法")
}

func TestValidateAgentCostMultiplier(t *testing.T) {
	require.NoError(t, ValidateAgentCostMultiplier(0.5))
	require.NoError(t, ValidateAgentCostMultiplier(1))
	require.NoError(t, ValidateAgentCostMultiplier(3.5))

	for _, invalid := range []float64{0, -0.5, math.NaN(), math.Inf(1), math.Inf(-1)} {
		err := ValidateAgentCostMultiplier(invalid)
		require.Error(t, err, "成本倍率 %v 应被拒绝", invalid)
		require.NotEmpty(t, err.Error())
	}
}

// ---------------------------------------------------------------------------
// 11. ThawStatus / CommissionFrozenUntil 边界
// ---------------------------------------------------------------------------

func TestThawStatus(t *testing.T) {
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	zero := time.Time{}

	tests := []struct {
		name        string
		frozenUntil *time.Time
		want        string
	}{
		{"never frozen", nil, CommissionStatusAvailable},
		{"zero value", &zero, CommissionStatusAvailable},
		{"future freeze", commissionTimePtr(now.Add(time.Hour)), CommissionStatusPending},
		{"one nanosecond left", commissionTimePtr(now.Add(time.Nanosecond)), CommissionStatusPending},
		{"exactly now thaws", commissionTimePtr(now), CommissionStatusAvailable},
		{"past freeze", commissionTimePtr(now.Add(-time.Hour)), CommissionStatusAvailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, ThawStatus(tt.frozenUntil, now))
		})
	}

	require.Equal(t, "pending", CommissionStatusPending)
	require.Equal(t, "available", CommissionStatusAvailable)
}

// commissionTimePtr 返回时间指针，避免测试里到处写局部变量取址。
// 命名带 commission 前缀：同包（unit tag）已有 ops_health_score_test.go 的 timePtr。
func commissionTimePtr(v time.Time) *time.Time { return &v }

func TestCommissionFrozenUntil(t *testing.T) {
	now := time.Date(2026, 10, 19, 12, 30, 45, 0, time.UTC)

	// 不冻结：返回 nil（落库为 NULL），而不是「冻结到当下」这种立即到期的中间态。
	require.Nil(t, CommissionFrozenUntil(now, 0))
	require.Nil(t, CommissionFrozenUntil(now, -1))
	require.Nil(t, CommissionFrozenUntil(now, -24))

	for _, hours := range []int{1, 24, 720} {
		got := CommissionFrozenUntil(now, hours)
		require.NotNil(t, got)
		require.Equal(t, now.Add(time.Duration(hours)*time.Hour), *got)
	}

	// 与 ThawStatus 联动：刚记账时 pending，到期那一刻起 available。
	frozenUntil := CommissionFrozenUntil(now, 24)
	require.NotNil(t, frozenUntil)
	require.Equal(t, CommissionStatusPending, ThawStatus(frozenUntil, now))
	require.Equal(t, CommissionStatusPending, ThawStatus(frozenUntil, now.Add(24*time.Hour-time.Nanosecond)))
	require.Equal(t, CommissionStatusAvailable, ThawStatus(frozenUntil, now.Add(24*time.Hour)))
	require.Equal(t, CommissionStatusAvailable, ThawStatus(frozenUntil, now.Add(48*time.Hour)))

	// 与仓储层解冻 SQL 的口径一致：migration 292 的 ThawFrozenCommissions 用
	// frozen_until <= now 判定到期，因此到期时刻本身必须算 available（不是 pending）。
	expired := now.Add(-time.Second)
	require.Equal(t, CommissionStatusAvailable, ThawStatus(&expired, now),
		"frozen_until 已经过去必须解冻，否则账本与纯函数判定会分叉")
}

// ---------------------------------------------------------------------------
// 12. 表驱动：多个目录价下的恒等关系
// ---------------------------------------------------------------------------

func TestCalculateCommission_IdentityAcrossTotalCosts(t *testing.T) {
	const childRate = 0.6
	const agentCostRate = 0.5

	// wantCommission 仅对刻度干净的用例断言具体数值，其余靠恒等关系兜底。
	tests := []struct {
		name           string
		totalCost      float64
		wantEligible   bool
		wantCommission float64
		assertExact    bool
	}{
		{name: "unit price", totalCost: 1, wantEligible: true, wantCommission: 0.1, assertExact: true},
		{name: "long tail", totalCost: 0.123456789, wantEligible: true, wantCommission: 0.01234568, assertExact: true},
		{name: "large", totalCost: 12345.678901234, wantEligible: true},
		{name: "thousand", totalCost: 1000, wantEligible: true, wantCommission: 100, assertExact: true},
		{name: "repeating", totalCost: 3.333333333, wantEligible: true},
		// 1.5e-8 目录价：两笔 raw 金额 9e-9 与 7.5e-9 在 8 位刻度上都进位到 1e-8，
		// 差价归零 → 不产生佣金（不能凭 raw 差值的 1.5e-9 独立造出一笔佣金）。
		{name: "sub tick total cost", totalCost: 1.5e-8, wantEligible: false},
		{name: "tick total cost", totalCost: 1e-7, wantEligible: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CalculateCommission(CommissionInput{
				TotalCost:     tt.totalCost,
				ChildRate:     childRate,
				AgentCostRate: agentCostRate,
			})

			require.Equal(t, tt.wantEligible, got.Eligible)
			requireCommissionIdentity(t, got)
			require.GreaterOrEqual(t, got.Commission, 0.0, "佣金不得为负")
			require.GreaterOrEqual(t, got.ChildPaid, got.AgentCost, "记账行的实付不得低于成本")

			if !tt.wantEligible {
				require.Zero(t, got.Commission)
				require.Zero(t, got.ChildPaid)
				require.Zero(t, got.AgentCost)
			}
			if tt.assertExact {
				require.Equal(t, tt.wantCommission, got.Commission)
			}
			// 量化误差不超过半个 tick（5e-9）乘以参与运算的金额量级。
			tolerance := math.Max(1e-8, math.Abs(tt.totalCost*(childRate-agentCostRate))*1e-9)
			require.InDelta(t, tt.totalCost*(childRate-agentCostRate), got.Commission, tolerance)
		})
	}
}

// 量化顺序的分岔用例：“先量化两笔金额再求差”与“先求 raw 差再量化”在这里结果不同。
//
// 目录价取 2^-24、售价 1.0、成本 0.75：两个乘积都是精确可表示的二进制数，
// 断言因此不依赖 float64 的表示细节：
//
//	child_paid raw = 2^-24     = 5.9604644775390625e-8      → 量化 6e-8
//	agent_cost raw = 3 × 2^-26 = 4.470348358154296875e-8    → 量化 4e-8
//	正确算法：6e-8 - 4e-8 = 2e-8
//	错误算法：raw 差值 2^-26 = 1.490116119384765625e-8      → 量化 1e-8
//
// 错误算法会把佣金算成 1e-8，而库里 child_paid - agent_cost 是 2e-8，
// 对账直接差一个 tick。
func TestCalculateCommission_QuantizesAmountsBeforeDifferencing(t *testing.T) {
	got := CalculateCommission(CommissionInput{
		TotalCost:     math.Ldexp(1, -24), // 2^-24
		ChildRate:     1,
		AgentCostRate: 0.75,
	})

	require.True(t, got.Eligible)
	require.Equal(t, 6e-8, got.ChildPaid)
	require.Equal(t, 4e-8, got.AgentCost)
	require.Equal(t, 2e-8, got.Commission,
		"必须先量化两笔金额再求差，而不是对 raw 差值独立量化")
	requireCommissionIdentity(t, got)

	// 显式固定住“错误算法”的结果，证明上面这 1e-8 的差距真实存在。
	require.Equal(t, 1e-8, QuantizeUsageBillingAmount(math.Ldexp(1, -26)))
}

// 非法倍率联动：任一倍率非法时整笔不产生佣金，且不 panic。
func TestCalculateCommission_InvalidMultipliersProduceNothing(t *testing.T) {
	invalid := []float64{0, -1, math.NaN(), math.Inf(1), math.Inf(-1)}
	for _, rate := range invalid {
		got := CalculateCommission(CommissionInput{TotalCost: 10, ChildRate: rate, AgentCostRate: 0.5})
		require.False(t, got.Eligible, "ChildRate=%v 不应产生佣金", rate)
		requireCommissionIdentity(t, got)

		got = CalculateCommission(CommissionInput{TotalCost: 10, ChildRate: 0.6, AgentCostRate: rate})
		require.False(t, got.Eligible, "AgentCostRate=%v 不应产生佣金", rate)
		requireCommissionIdentity(t, got)
	}
}

// 溢出防护：倍率各自合法，但乘积累积溢出成 +Inf。
// shopspring/decimal 的 NewFromFloat 遇到 Inf/NaN 会 panic，而不是返回错误，
// 所以 CalculateCommission 必须在进 decimal 之前拦下，否则一条脏配置就能打崩计费链路。
func TestCalculateCommission_OverflowDoesNotPanic(t *testing.T) {
	got := CalculateCommission(CommissionInput{
		TotalCost:     math.MaxFloat64,
		ChildRate:     2, // MaxFloat64*2 = +Inf
		AgentCostRate: 1,
	})

	require.False(t, got.Eligible, "溢出成 +Inf 的金额不可记账")
	require.Zero(t, got.Commission)
	requireCommissionIdentity(t, got)
}

// 累加对账：多笔佣金在十进制口径下求和必须等于逐笔恒等关系推出的总额，不依赖 epsilon。
func TestCalculateCommission_AccumulationReconciles(t *testing.T) {
	inputs := []float64{1, 0.123456789, 1000, 3.333333333, 0.000123456}

	// 用声明零值而不是共享 decimal.Zero：Decimal 内部持有 *big.Int，
	// 从零值起步（Add 会先 ensureInitialized）能确保三个累加器各自独立。
	var (
		commissionSum decimal.Decimal
		childSum      decimal.Decimal
		costSum       decimal.Decimal
	)
	for _, totalCost := range inputs {
		got := CalculateCommission(CommissionInput{
			TotalCost:     totalCost,
			ChildRate:     0.6,
			AgentCostRate: 0.5,
		})
		require.True(t, got.Eligible, "TotalCost=%v 应产生佣金", totalCost)
		requireCommissionIdentity(t, got)

		commissionSum = commissionSum.Add(decimal.NewFromFloat(got.Commission))
		childSum = childSum.Add(decimal.NewFromFloat(got.ChildPaid))
		costSum = costSum.Add(decimal.NewFromFloat(got.AgentCost))
	}

	require.True(t, childSum.Sub(costSum).Equal(commissionSum),
		"累计 child_paid - agent_cost 必须精确等于累计佣金：%s - %s != %s",
		childSum, costSum, commissionSum)
}
