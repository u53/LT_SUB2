package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/google/uuid"
)

// 本文件是「代理佣金模块」的 service 层：管理端后台（/admin/agents*）与代理自助端
// （/agent/*）共用同一套校验与错误语义，避免两端对同一份数据给出不同结论。
//
// 与仓储层的边界：service 不能 import repository（该包反向依赖 service，导入即成环），
// 所以下面重新声明 AgentRepository —— 方法名与参数结构和 repository.AgentRepository
// 一一对应，由 repository 包一个薄适配器实现（与 service.UsageBillingRepository /
// service.UserPlatformQuotaRepository 的既有做法一致）。适配器的唯一职责是搬运字段，
// 不做任何业务判断：判断只在本文件里发生，测试桩也只需要实现这里的接口。

// 佣金账本与提现状态字面量。
//
// 必须与 repository.AgentCommissionStatus* / repository.AgentWithdrawalStatus* 以及
// migration 292 里的 CHECK 约束保持一致；这里不引用 repository 包，原因同上（成环）。
//
// pending / available 直接别名到 agent_commission.go 里的 CommissionStatus*，
// 而不是重新写一遍字符串字面量：两处取值必须永远相同（一处在计费链路上写，
// 一处在后台查询里判），别名让编译器替我们保证这一点。
const (
	// AgentCommissionStatusPending 佣金已记账但仍在冻结期内。
	AgentCommissionStatusPending = CommissionStatusPending
	// AgentCommissionStatusAvailable 佣金已解冻，可提现。
	AgentCommissionStatusAvailable = CommissionStatusAvailable
	// AgentCommissionStatusWithdrawn 佣金已被提现占用（账本终态）。
	// withdrawn / reversed 属于账本迁移状态，由仓储层驱动，计算层不产生这两个值，
	// 因此 agent_commission.go 里没有对应的常量可复用。
	AgentCommissionStatusWithdrawn = "withdrawn"
	// AgentCommissionStatusReversed 佣金已被冲正（账本终态）。
	AgentCommissionStatusReversed = "reversed"

	// AgentWithdrawalStatusPaid 提现已线下打款。
	AgentWithdrawalStatusPaid = "paid"
	// AgentWithdrawalStatusVoided 该笔打款已作废（金额已人工退回）。
	AgentWithdrawalStatusVoided = "voided"
)

const (
	// agentRecordMaxPageSize 分页上限：与 repository 的 agentPageLimitOffset 对齐，
	// 避免后台一次拉全量佣金流水。
	agentRecordMaxPageSize = 200
	// agentDefaultPageSize 未指定 page_size 时的默认页大小。
	agentDefaultPageSize = 20
	// agentAmountEpsilon 金额比较容差。库里是 DECIMAL(20,8)，float64 表示不了 1e-8，
	// 直接用 == 比较会把「刚好提完」判成不足。
	agentAmountEpsilon = 1e-9
	// agentWithdrawOperationScopeAdmin / agentWithdrawOperationScopeSelf 把管理端登记与
	// 代理自助申请的幂等键空间分开：同一个人在两处填同一个键不应互相干扰。
	agentWithdrawOperationScopeAdmin = "admin.agents.withdraw"
	agentWithdrawOperationScopeSelf  = "agent.self.withdraw"
)

var (
	// ErrAgentProfileNotFound 表示该用户没有代理档案（未被管理员开通代理身份）。
	ErrAgentProfileNotFound = infraerrors.NotFound("AGENT_PROFILE_NOT_FOUND", "该账号不是代理或代理档案不存在")
	// ErrAgentCommissionInsufficient 表示可提现佣金余额不足。
	ErrAgentCommissionInsufficient = infraerrors.BadRequest("AGENT_COMMISSION_INSUFFICIENT", "可提现佣金余额不足")
	// ErrAgentCommissionAmountInvalid 表示传入金额非法（<=0 或非有限数）。
	ErrAgentCommissionAmountInvalid = infraerrors.BadRequest("AGENT_COMMISSION_AMOUNT_INVALID", "佣金金额必须是大于 0 的有限数值")
	// ErrAgentMultiplierInvalid 表示倍率非法（<=0 或非有限数）。
	ErrAgentMultiplierInvalid = infraerrors.BadRequest("AGENT_MULTIPLIER_INVALID", "倍率必须是大于 0 的有限数值")
	// ErrAgentChildForbidden 表示目标用户不是当前代理的下级。
	ErrAgentChildForbidden = infraerrors.Forbidden("AGENT_CHILD_FORBIDDEN", "该用户不是当前代理的下级，无权调整其折扣")
	// ErrAgentDisabled 表示代理已被停用，不再允许改动折扣或申请提现。
	ErrAgentDisabled = infraerrors.Forbidden("AGENT_DISABLED", "代理已停用，请先由管理员启用")
	// ErrAgentRoleForbidden 表示该角色不能被开通为代理（管理员/审计账号与代理身份互斥）。
	ErrAgentRoleForbidden = infraerrors.Forbidden("AGENT_ROLE_FORBIDDEN", "管理员或审计账号不能开通为代理")
	// ErrAgentWithdrawalConflict 表示同一幂等键被另一笔不同代理/金额的提现占用。
	ErrAgentWithdrawalConflict = infraerrors.Conflict("AGENT_WITHDRAWAL_CONFLICT", "幂等键已被另一笔提现占用，请更换 Idempotency-Key")
)

// AgentRepository 是代理佣金管理所需的数据访问契约（service 侧声明）。
//
// 方法名与参数结构都与 repository.AgentRepository 一一对应，区别只有两类：
//   - 返回的 DTO 是本包的类型（见文件头说明），由 repository 侧的薄适配器搬运字段；
//   - 档案读写叫 LoadAgentProfile / SaveAgentProfile 而不是 GetAgentProfile / UpsertAgentProfile。
//     这是有意避开的：同包的 AgentCommissionRepository（计费热路径那个）已经声明了
//     `GetAgentProfile(...) (*AgentProfile, error)`，若这里再用同名但返回不同类型，
//     一个适配器就无法同时满足两个接口（Go 不允许同签同名的不同返回类型共存）。
//     改名后同一个适配器可以同时服务「解析器」与「管理端」两套接口。
//
// 适配器（由主会话在 repository 包内提供）除了搬运字段，还应翻译两个哨兵错误，
// 否则余额竞态会变成 500 而不是 400：
//
//	repository.ErrAgentCommissionInsufficient → service.ErrAgentCommissionInsufficient
//	repository.ErrAgentWithdrawalConflict     → service.ErrAgentWithdrawalConflict
//
// 其余错误可以原样返回（会被 response.ErrorFrom 当成 500 处理，这是期望行为）。
type AgentRepository interface {
	LoadAgentProfile(ctx context.Context, userID int64) (*AgentProfileRecord, error)
	SaveAgentProfile(ctx context.Context, profile *AgentProfileRecord) error
	ListAgents(ctx context.Context, filter AgentListFilter) ([]AgentListItem, int, error)

	GetGroupCosts(ctx context.Context, agentUserID int64) (map[int64]float64, error)
	ReplaceGroupCosts(ctx context.Context, agentUserID int64, costs map[int64]float64) error
	GetAgentCostMultiplier(ctx context.Context, agentUserID, groupID int64) (*float64, error)

	GetInviterID(ctx context.Context, userID int64) (*int64, error)
	ReplaceChildRate(ctx context.Context, agentUserID, childUserID int64, groupID *int64, rate float64) error

	GetCommissionAccount(ctx context.Context, agentUserID int64) (*AgentCommissionAccount, error)
	ListCommissions(ctx context.Context, filter AgentCommissionFilter) ([]AgentCommissionRecord, int, error)

	ListWithdrawals(ctx context.Context, filter AgentWithdrawalFilter) ([]AgentWithdrawal, int, error)
	InsertWithdrawal(ctx context.Context, entry AgentWithdrawalEntry) (*AgentWithdrawal, bool, error)
}

// AgentChildReader 是「按代理分页列出下级」所需的可选能力。
//
// 单独立出来而不是塞进 AgentRepository：repository.AgentRepository 当前没有这条查询
// （只有 GetInviterID 这种单点查询），而仓储文件不在本次改动范围内。做成可选能力后：
//   - 仓储补上 ListAgentChildren 后会被自动识别，代理自助页立刻可用；
//   - 补上之前，下级列表接口返回一个说清楚的 503，而不是让整个 backend 编译不过。
//
// 需要的 SQL 大致是：user_affiliates ua JOIN users u ON u.id = ua.user_id
// WHERE ua.inviter_id = $1 ORDER BY ua.created_at DESC，再按代理+下级的
// agent_child_rates 补齐已配置的售价覆盖。
type AgentChildReader interface {
	ListAgentChildren(ctx context.Context, agentUserID int64, page, pageSize int) ([]AgentChild, int, error)
}

// AgentAffiliateCodeReader 是代理自助页展示邀请码所需的可选能力。
//
// 做成可选接口而不是 AgentRepository 的必需方法：邀请码只影响代理页面的"分享链接"
// 展示，缺这条能力时页面降级为空码即可，不应该让整个模块编译不过或不可用。
// repository 侧实现一条 `SELECT aff_code FROM user_affiliates WHERE user_id = $1`
// 就能被自动识别。
type AgentAffiliateCodeReader interface {
	GetAffiliateCode(ctx context.Context, userID int64) (string, error)
}

// AgentProfileRecord 代理档案（service 侧镜像）。
type AgentProfileRecord struct {
	UserID                     int64
	Enabled                    bool
	DefaultChildRateMultiplier *float64
	CommissionFrozenHours      int
	Note                       string
	CreatedAt                  time.Time
	UpdatedAt                  time.Time
}

// AgentListFilter 管理端代理列表筛选条件。
type AgentListFilter struct {
	Search   string
	Enabled  *bool
	Page     int
	PageSize int
}

// AgentListItem 代理列表行：档案摘要 + 下级规模 + 佣金账户余额。
type AgentListItem struct {
	UserID          int64     `json:"user_id"`
	Email           string    `json:"email"`
	Username        string    `json:"username"`
	Enabled         bool      `json:"enabled"`
	ChildCount      int       `json:"child_count"`
	Available       float64   `json:"available"`
	Frozen          float64   `json:"frozen"`
	TotalCommission float64   `json:"total_commission"`
	CreatedAt       time.Time `json:"created_at"`
}

// AgentDetail 代理详情：档案 + 账号信息 + 佣金账户 + 分组成本折扣。
//
// TotalCommission 是累计已产生佣金：佣金账户的三个余额桶互不重叠地覆盖了
// 「还没解冻 + 可以提现 + 已经提走」，因此三者之和就是历史累计值，
// 不需要为了统计再扫一遍账本。
type AgentDetail struct {
	UserID                     int64             `json:"user_id"`
	Email                      string            `json:"email"`
	Username                   string            `json:"username"`
	Role                       string            `json:"role"`
	Enabled                    bool              `json:"enabled"`
	DefaultChildRateMultiplier *float64          `json:"default_child_rate_multiplier,omitempty"`
	CommissionFrozenHours      int               `json:"commission_frozen_hours"`
	Note                       string            `json:"note"`
	Available                  float64           `json:"available"`
	Frozen                     float64           `json:"frozen"`
	Withdrawn                  float64           `json:"withdrawn"`
	TotalCommission            float64           `json:"total_commission"`
	GroupCosts                 map[int64]float64 `json:"group_costs"`
	CreatedAt                  time.Time         `json:"created_at"`
	UpdatedAt                  time.Time         `json:"updated_at"`
}

// AgentUpsertInput 管理员保存代理档案的入参。
type AgentUpsertInput struct {
	Enabled                    bool
	DefaultChildRateMultiplier *float64
	CommissionFrozenHours      int
	Note                       string
}

// AgentChild 代理的下级用户及其已配置的售价覆盖。
//
// DefaultRate 对应 group_id 为 NULL 的全局默认行；GroupRates 是分组级覆盖。
// 两者都为空表示该下级仍按分组公开售价计费。
type AgentChild struct {
	UserID      int64             `json:"user_id"`
	Email       string            `json:"email"`
	Username    string            `json:"username"`
	CreatedAt   time.Time         `json:"created_at"`
	DefaultRate *float64          `json:"default_rate,omitempty"`
	GroupRates  map[int64]float64 `json:"group_rates,omitempty"`
}

// AgentCommissionAccount 佣金账户余额快照。
type AgentCommissionAccount struct {
	AgentUserID int64
	Available   float64
	Frozen      float64
	Withdrawn   float64
	UpdatedAt   time.Time
}

// AgentCommissionFilter 佣金流水筛选条件。AgentUserID/ChildUserID 为 nil 表示不按该维度过滤。
type AgentCommissionFilter struct {
	AgentUserID *int64
	ChildUserID *int64
	Status      string
	StartAt     *time.Time
	EndAt       *time.Time
	Page        int
	PageSize    int
}

// AgentCommissionRecord 佣金流水列表行（含代理与下级邮箱，便于对账）。
type AgentCommissionRecord struct {
	ID            int64      `json:"id"`
	AgentUserID   int64      `json:"agent_user_id"`
	AgentEmail    string     `json:"agent_email"`
	ChildUserID   *int64     `json:"child_user_id,omitempty"`
	ChildEmail    string     `json:"child_email"`
	RequestID     string     `json:"request_id"`
	APIKeyID      int64      `json:"api_key_id"`
	GroupID       *int64     `json:"group_id,omitempty"`
	Model         string     `json:"model"`
	BaseCost      float64    `json:"base_cost"`
	ChildRate     float64    `json:"child_rate"`
	AgentCostRate float64    `json:"agent_cost_rate"`
	ChildPaid     float64    `json:"child_paid"`
	AgentCost     float64    `json:"agent_cost"`
	Commission    float64    `json:"commission"`
	Status        string     `json:"status"`
	FrozenUntil   *time.Time `json:"frozen_until,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
}

// AgentWithdrawalEntry 一笔待登记的提现。OperationID 是幂等键。
type AgentWithdrawalEntry struct {
	AgentUserID int64
	Amount      float64
	OperationID string
	Status      string
	Note        string
}

// AgentWithdrawalFilter 提现记录筛选条件。
type AgentWithdrawalFilter struct {
	AgentUserID *int64
	Status      string
	StartAt     *time.Time
	EndAt       *time.Time
	Page        int
	PageSize    int
}

// AgentWithdrawal 提现记录。
//
// Replayed 表示本次请求命中了同一幂等键的已有登记（没有二次扣款）：
// 只用于让 handler 回 X-Idempotency-Replayed 头，不落库也不返回给前端。
type AgentWithdrawal struct {
	ID          int64     `json:"id"`
	AgentUserID int64     `json:"agent_user_id"`
	AgentEmail  string    `json:"agent_email"`
	Amount      float64   `json:"amount"`
	Status      string    `json:"status"`
	OperationID string    `json:"operation_id"`
	Note        string    `json:"note"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	Replayed    bool      `json:"-"`
}

// AgentSelfProfile 代理自助页的档案视图：账户余额 + 邀请码 + 累计佣金。
//
// MinWithdraw 是当前最低提现额（设置项 agent_min_withdraw，缺失按 0），
// 让前端在提交前就能提示，而不是等后端报错。
type AgentSelfProfile struct {
	UserID                     int64    `json:"user_id"`
	AffCode                    string   `json:"aff_code"`
	Enabled                    bool     `json:"enabled"`
	DefaultChildRateMultiplier *float64 `json:"default_child_rate_multiplier,omitempty"`
	CommissionFrozenHours      int      `json:"commission_frozen_hours"`
	Note                       string   `json:"note"`
	Available                  float64  `json:"available"`
	Frozen                     float64  `json:"frozen"`
	Withdrawn                  float64  `json:"withdrawn"`
	TotalCommission            float64  `json:"total_commission"`
	MinWithdraw                float64  `json:"min_withdraw"`
}

// AgentService 代理佣金管理服务。
type AgentService struct {
	repo           AgentRepository
	settingService *SettingService
	resolver       *AgentCommissionResolver
	userRepo       UserRepository
	// userGroupRates 是计费链路实际读取售价的来源（user_group_rate_multipliers）。
	// 代理设折扣时必须同步写入这里，否则 agent_child_rates 只是一份「展示用副本」，
	// 用户仍按老价格被扣费、佣金也按错的售价计算。
	userGroupRates UserGroupRateRepository
}

// NewAgentService 创建代理佣金管理服务。
//
// resolver 用于在写完折扣后清掉计费侧的缓存（已就绪）。
func NewAgentService(
	repo AgentRepository,
	settingService *SettingService,
	resolver *AgentCommissionResolver,
	userRepo UserRepository,
	userGroupRates UserGroupRateRepository,
) *AgentService {
	return &AgentService{
		repo:           repo,
		settingService: settingService,
		resolver:       resolver,
		userRepo:       userRepo,
		userGroupRates: userGroupRates,
	}
}

func (s *AgentService) ensureReady() error {
	if s == nil || s.repo == nil {
		return infraerrors.ServiceUnavailable("SERVICE_UNAVAILABLE", "agent service unavailable")
	}
	return nil
}

// =========================
// 管理端
// =========================

// ListAgents 分页列出代理档案。
func (s *AgentService) ListAgents(ctx context.Context, filter AgentListFilter) ([]AgentListItem, int, error) {
	if err := s.ensureReady(); err != nil {
		return nil, 0, err
	}
	filter.Page, filter.PageSize = normalizeAgentPaging(filter.Page, filter.PageSize)
	filter.Search = strings.TrimSpace(filter.Search)
	return s.repo.ListAgents(ctx, filter)
}

// GetAgent 读取单个代理的档案、账户余额与分组成本折扣。
func (s *AgentService) GetAgent(ctx context.Context, id int64) (*AgentDetail, error) {
	if err := s.ensureReady(); err != nil {
		return nil, err
	}
	if id <= 0 {
		return nil, infraerrors.BadRequest("INVALID_AGENT_ID", "代理用户 ID 非法")
	}

	profile, err := s.repo.LoadAgentProfile(ctx, id)
	if err != nil {
		return nil, err
	}
	if profile == nil {
		return nil, ErrAgentProfileNotFound
	}

	detail := &AgentDetail{
		UserID:                     profile.UserID,
		Enabled:                    profile.Enabled,
		DefaultChildRateMultiplier: profile.DefaultChildRateMultiplier,
		CommissionFrozenHours:      profile.CommissionFrozenHours,
		Note:                       profile.Note,
		CreatedAt:                  profile.CreatedAt,
		UpdatedAt:                  profile.UpdatedAt,
	}

	// 账号信息只用于展示，取不到不影响档案本身可管理，因此这里不让它中断请求。
	if s.userRepo != nil {
		if user, userErr := s.userRepo.GetByID(ctx, id); userErr == nil && user != nil {
			detail.Email = user.Email
			detail.Username = user.Username
			detail.Role = user.Role
		}
	}

	account, err := s.repo.GetCommissionAccount(ctx, id)
	if err != nil {
		return nil, err
	}
	applyAgentAccountToDetail(detail, account)

	costs, err := s.repo.GetGroupCosts(ctx, id)
	if err != nil {
		return nil, err
	}
	if costs == nil {
		costs = map[int64]float64{}
	}
	detail.GroupCosts = costs
	return detail, nil
}

// UpsertAgent 新建或更新代理档案。
//
// 角色处理是有意放在这里的：/agent 自助接口按 RoleAgent 鉴权，如果只写档案不同步
// 角色，代理登录后仍然拿不到自助权限，表现为「后台开通了但代理什么也看不到」。
// 停用/降级时不回退角色——历史佣金与流水仍归属该用户，把角色改回 user 会让
// 他连自己的佣金记录都查不到。
func (s *AgentService) UpsertAgent(ctx context.Context, id int64, input AgentUpsertInput) error {
	if err := s.ensureReady(); err != nil {
		return err
	}
	if id <= 0 {
		return infraerrors.BadRequest("INVALID_AGENT_ID", "代理用户 ID 非法")
	}
	if input.CommissionFrozenHours < 0 {
		return infraerrors.BadRequest("AGENT_FROZEN_HOURS_INVALID", "佣金冻结期不能为负数")
	}
	if input.DefaultChildRateMultiplier != nil {
		if _, ok := NormalizeMultiplier(*input.DefaultChildRateMultiplier); !ok {
			return ErrAgentMultiplierInvalid
		}
	}
	if s.userRepo == nil {
		return infraerrors.ServiceUnavailable("SERVICE_UNAVAILABLE", "user repository unavailable")
	}

	user, err := s.userRepo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if user == nil {
		return ErrUserNotFound
	}
	if user.Role == RoleAdmin || user.Role == RoleObserver {
		return ErrAgentRoleForbidden
	}

	profile := &AgentProfileRecord{
		UserID:                     id,
		Enabled:                    input.Enabled,
		DefaultChildRateMultiplier: input.DefaultChildRateMultiplier,
		CommissionFrozenHours:      input.CommissionFrozenHours,
		Note:                       strings.TrimSpace(input.Note),
	}
	if err := s.repo.SaveAgentProfile(ctx, profile); err != nil {
		return err
	}

	if input.Enabled && user.Role == RoleUser {
		// 只写 role 一列：UserRepository.Update 按 UserUpdateFields 声明写哪些列，
		// 传一个只填了 ID/Role 的 User 不会覆盖并发改动的其他列。
		if err := s.userRepo.Update(ctx, &User{ID: id, Role: RoleAgent}, UserUpdateFields{Role: true}); err != nil {
			return err
		}
	}
	return nil
}

// GetGroupCosts 读取代理的分组成本折扣表。
func (s *AgentService) GetGroupCosts(ctx context.Context, agentID int64) (map[int64]float64, error) {
	if err := s.ensureReady(); err != nil {
		return nil, err
	}
	if agentID <= 0 {
		return nil, infraerrors.BadRequest("INVALID_AGENT_ID", "代理用户 ID 非法")
	}
	costs, err := s.repo.GetGroupCosts(ctx, agentID)
	if err != nil {
		return nil, err
	}
	if costs == nil {
		costs = map[int64]float64{}
	}
	return costs, nil
}

// ReplaceGroupCosts 全量替换代理的分组成本折扣。
//
// 逐项校验、任何一项非法就整体拒绝：这是"全量替换"语义，如果放行合法项而丢掉非法项，
// 管理员会以为保存成功，但实际有一批分组的成本折扣被静默清空——直接影响佣金金额。
func (s *AgentService) ReplaceGroupCosts(ctx context.Context, agentID int64, costs map[int64]float64) error {
	if err := s.ensureReady(); err != nil {
		return err
	}
	if agentID <= 0 {
		return infraerrors.BadRequest("INVALID_AGENT_ID", "代理用户 ID 非法")
	}

	cleaned := make(map[int64]float64, len(costs))
	for groupID, cost := range costs {
		if groupID <= 0 {
			return infraerrors.BadRequest("INVALID_GROUP_ID", "分组 ID 非法")
		}
		if err := ValidateAgentCostMultiplier(cost); err != nil {
			return infraerrors.BadRequest("AGENT_COST_MULTIPLIER_INVALID",
				fmt.Sprintf("分组 %d 的成本倍率非法：%s", groupID, err.Error()))
		}
		cleaned[groupID] = cost
	}

	profile, err := s.repo.LoadAgentProfile(ctx, agentID)
	if err != nil {
		return err
	}
	if profile == nil {
		return ErrAgentProfileNotFound
	}
	return s.repo.ReplaceGroupCosts(ctx, agentID, cleaned)
}

// ListChildren 分页列出某代理的下级（管理端）。
func (s *AgentService) ListChildren(ctx context.Context, agentID int64, page int) ([]AgentChild, int, error) {
	if err := s.ensureReady(); err != nil {
		return nil, 0, err
	}
	if agentID <= 0 {
		return nil, 0, infraerrors.BadRequest("INVALID_AGENT_ID", "代理用户 ID 非法")
	}
	page, pageSize := normalizeAgentPaging(page, 0)
	return s.listChildren(ctx, agentID, page, pageSize)
}

// ListCommissions 分页列出佣金流水（管理端，可按代理/下级/状态/时间过滤）。
func (s *AgentService) ListCommissions(ctx context.Context, filter AgentCommissionFilter) ([]AgentCommissionRecord, int, error) {
	if err := s.ensureReady(); err != nil {
		return nil, 0, err
	}
	filter = normalizeAgentCommissionFilter(filter)
	if filter.Status != "" && !isAgentCommissionStatus(filter.Status) {
		return nil, 0, infraerrors.BadRequest("AGENT_COMMISSION_STATUS_INVALID",
			"佣金状态非法，可选值为 pending/available/withdrawn/reversed")
	}
	return s.repo.ListCommissions(ctx, filter)
}

// ListWithdrawals 分页列出提现登记记录（管理端）。
func (s *AgentService) ListWithdrawals(ctx context.Context, filter AgentWithdrawalFilter) ([]AgentWithdrawal, int, error) {
	if err := s.ensureReady(); err != nil {
		return nil, 0, err
	}
	filter = normalizeAgentWithdrawalFilter(filter)
	if filter.Status != "" && filter.Status != AgentWithdrawalStatusPaid && filter.Status != AgentWithdrawalStatusVoided {
		return nil, 0, infraerrors.BadRequest("AGENT_WITHDRAWAL_STATUS_INVALID",
			"提现状态非法，可选值为 paid/voided")
	}
	return s.repo.ListWithdrawals(ctx, filter)
}

// WithdrawForAgent 登记一笔线下已打款的代理提现（管理端）。
//
// operationID 是幂等键（handler 要求 Idempotency-Key 头）：同一键重放时不会二次扣款，
// 金额或代理不一致则返回冲突错误，避免"换了金额换个键"这种误操作被当成重放。
func (s *AgentService) WithdrawForAgent(ctx context.Context, agentID int64, amount float64, note string, operationID string) (*AgentWithdrawal, error) {
	if err := s.ensureReady(); err != nil {
		return nil, err
	}
	if agentID <= 0 {
		return nil, infraerrors.BadRequest("INVALID_AGENT_ID", "代理用户 ID 非法")
	}

	key, err := NormalizeIdempotencyKey(operationID)
	if err != nil {
		return nil, err
	}
	if key == "" {
		return nil, ErrIdempotencyKeyRequired
	}

	profile, err := s.repo.LoadAgentProfile(ctx, agentID)
	if err != nil {
		return nil, err
	}
	if profile == nil {
		return nil, ErrAgentProfileNotFound
	}

	normalized, err := s.normalizeWithdrawAmount(ctx, agentID, amount)
	if err != nil {
		return nil, err
	}

	record, inserted, err := s.repo.InsertWithdrawal(ctx, AgentWithdrawalEntry{
		AgentUserID: agentID,
		Amount:      normalized,
		OperationID: agentWithdrawOperationID(agentWithdrawOperationScopeAdmin, key),
		Status:      AgentWithdrawalStatusPaid,
		Note:        strings.TrimSpace(note),
	})
	if err != nil {
		return nil, err
	}
	if record != nil {
		record.Replayed = !inserted
	}
	return record, nil
}

// =========================
// 代理自助
// =========================

// GetSelfProfile 返回代理自己的档案、余额与邀请码。
func (s *AgentService) GetSelfProfile(ctx context.Context, agentUserID int64) (*AgentSelfProfile, error) {
	if err := s.ensureReady(); err != nil {
		return nil, err
	}
	if agentUserID <= 0 {
		return nil, ErrAgentProfileNotFound
	}

	profile, err := s.repo.LoadAgentProfile(ctx, agentUserID)
	if err != nil {
		return nil, err
	}
	if profile == nil {
		return nil, ErrAgentProfileNotFound
	}

	result := &AgentSelfProfile{
		UserID:                     profile.UserID,
		Enabled:                    profile.Enabled,
		DefaultChildRateMultiplier: profile.DefaultChildRateMultiplier,
		CommissionFrozenHours:      profile.CommissionFrozenHours,
		Note:                       profile.Note,
		MinWithdraw:                s.minWithdrawAmount(ctx),
	}

	account, err := s.repo.GetCommissionAccount(ctx, agentUserID)
	if err != nil {
		return nil, err
	}
	if account != nil {
		result.Available = account.Available
		result.Frozen = account.Frozen
		result.Withdrawn = account.Withdrawn
		result.TotalCommission = roundTo(account.Available+account.Frozen+account.Withdrawn, 8)
	}

	// 邀请码读不到不影响余额与佣金视图，降级为空串即可。
	// 依次在仓储与用户仓储上探测可选能力：两者任意一侧实现 GetAffiliateCode
	// （`SELECT aff_code FROM user_affiliates WHERE user_id = $1`）就能自动生效。
	result.AffCode = s.resolveAffiliateCode(ctx, agentUserID)
	return result, nil
}

// resolveAffiliateCode 在可选能力上读邀请码，读不到返回空串。
func (s *AgentService) resolveAffiliateCode(ctx context.Context, userID int64) string {
	for _, candidate := range []any{s.repo, s.userRepo} {
		reader, ok := candidate.(AgentAffiliateCodeReader)
		if !ok {
			continue
		}
		if code, err := reader.GetAffiliateCode(ctx, userID); err == nil {
			return strings.TrimSpace(code)
		}
	}
	return ""
}

// ListSelfChildren 分页列出代理自己的下级。
func (s *AgentService) ListSelfChildren(ctx context.Context, agentUserID int64, page int) ([]AgentChild, int, error) {
	if err := s.ensureReady(); err != nil {
		return nil, 0, err
	}
	if agentUserID <= 0 {
		return nil, 0, ErrAgentProfileNotFound
	}
	profile, err := s.repo.LoadAgentProfile(ctx, agentUserID)
	if err != nil {
		return nil, 0, err
	}
	if profile == nil {
		return nil, 0, ErrAgentProfileNotFound
	}
	page, pageSize := normalizeAgentPaging(page, 0)
	return s.listChildren(ctx, agentUserID, page, pageSize)
}

// SetChildRate 代理给自己的下级设置售价倍率。
//
// 这是整个模块最关键的一处写入，三道校验缺一不可：
//  1. 归属：childID 的直接上级必须是当前代理。少了这道校验，任何代理都能给
//     别人的下级改价，等于越权改动他人的计费口径。
//  2. 正数：倍率必须 > 0（0 等于免费，负数会把佣金算反）。
//  3. 成本下限：售价不得低于代理在该分组的成本倍率，否则每笔用量都会产生负数佣金
//     （资损），而不是"折扣"。
//
// 写完后必须清 resolver 缓存：计费链路按 60s TTL 缓存"下级 → 代理 + 倍率"，
// 不清缓存会出现「后台已改价、用户仍按旧价被扣费」的窗口。
func (s *AgentService) SetChildRate(ctx context.Context, agentUserID, childID int64, groupID *int64, rate float64) error {
	if err := s.ensureReady(); err != nil {
		return err
	}
	if agentUserID <= 0 || childID <= 0 {
		return infraerrors.BadRequest("INVALID_AGENT_CHILD_ID", "代理或下级用户 ID 非法")
	}
	if agentUserID == childID {
		return infraerrors.BadRequest("AGENT_CHILD_SELF", "不能给自己设置下级折扣")
	}
	if groupID != nil && *groupID <= 0 {
		return infraerrors.BadRequest("INVALID_GROUP_ID", "分组 ID 非法")
	}

	inviterID, err := s.repo.GetInviterID(ctx, childID)
	if err != nil {
		return err
	}
	if inviterID == nil || *inviterID != agentUserID {
		return ErrAgentChildForbidden
	}

	profile, err := s.repo.LoadAgentProfile(ctx, agentUserID)
	if err != nil {
		return err
	}
	if profile == nil {
		return ErrAgentProfileNotFound
	}
	if !profile.Enabled {
		return ErrAgentDisabled
	}

	floor, err := s.resolveCostFloor(ctx, agentUserID, groupID)
	if err != nil {
		return err
	}
	// ValidateChildRate 的中文错误信息已经写清了"售价低于成本"，直接透出给调用方。
	if err := ValidateChildRate(rate, floor); err != nil {
		return infraerrors.BadRequest("AGENT_CHILD_RATE_INVALID", err.Error())
	}

	if err := s.repo.ReplaceChildRate(ctx, agentUserID, childID, groupID, rate); err != nil {
		return err
	}
	// 同步到计费真正读取的那张表。两步不是同一个事务：先写代理侧记录（含校验与展示），
	// 再写计费侧倍率。若后一步失败，代理侧保留记录但售价未生效，重试即可修复；
	// 反过来先写计费侧则可能出现「已按新价扣费但代理侧无记录」。
	if err := s.syncChildBillingRate(ctx, childID, groupID, rate); err != nil {
		return err
	}
	if s.resolver != nil {
		s.resolver.InvalidateChild(childID)
	}
	return nil
}

// syncChildBillingRate 把代理设置的售价写入 user_group_rate_multipliers。
//
// 只改目标分组这一项，其余分组保持原值：SyncUserGroupRates 是全量替换语义，
// 直接传只含一个键的 map 会把该用户其他分组的专属倍率清空。
func (s *AgentService) syncChildBillingRate(ctx context.Context, childID int64, groupID *int64, rate float64) error {
	if s == nil || s.userGroupRates == nil {
		// 未注入时明确失败而不是静默跳过：否则调用方会以为折扣已生效。
		return infraerrors.ServiceUnavailable("AGENT_RATE_SYNC_UNAVAILABLE", "代理折扣同步能力未就绪，请稍后重试")
	}
	existing, err := s.userGroupRates.GetByUserID(ctx, childID)
	if err != nil {
		return err
	}
	rates := make(map[int64]*float64, len(existing)+1)
	for id, value := range existing {
		v := value
		rates[id] = &v
	}
	if groupID == nil {
		// 全局默认折扣无法落到「用户 × 分组」表：该表按分组存储。
		// 此时只保留代理侧记录，实际生效需要按分组逐项设置，返回明确错误比静默无效更好。
		return infraerrors.BadRequest("AGENT_GLOBAL_RATE_UNSUPPORTED",
			"暂不支持全局默认折扣，请按分组分别设置")
	}
	value := rate
	rates[*groupID] = &value
	return s.userGroupRates.SyncUserGroupRates(ctx, childID, rates)
}

// ListSelfCommissions 分页列出代理自己的佣金流水。
//
// 代理维度由参数强制指定，不接受调用方传入他人的 agent_user_id。
func (s *AgentService) ListSelfCommissions(ctx context.Context, agentUserID int64, filter AgentCommissionFilter) ([]AgentCommissionRecord, int, error) {
	if err := s.ensureReady(); err != nil {
		return nil, 0, err
	}
	if agentUserID <= 0 {
		return nil, 0, ErrAgentProfileNotFound
	}
	profile, err := s.repo.LoadAgentProfile(ctx, agentUserID)
	if err != nil {
		return nil, 0, err
	}
	if profile == nil {
		return nil, 0, ErrAgentProfileNotFound
	}

	filter = normalizeAgentCommissionFilter(filter)
	filter.AgentUserID = &agentUserID
	if filter.Status != "" && !isAgentCommissionStatus(filter.Status) {
		return nil, 0, infraerrors.BadRequest("AGENT_COMMISSION_STATUS_INVALID",
			"佣金状态非法，可选值为 pending/available/withdrawn/reversed")
	}
	return s.repo.ListCommissions(ctx, filter)
}

// RequestSelfWithdraw 代理自助申请提现（线下打款，后台登记）。
func (s *AgentService) RequestSelfWithdraw(ctx context.Context, agentUserID int64, amount float64, note string) (*AgentWithdrawal, error) {
	if err := s.ensureReady(); err != nil {
		return nil, err
	}
	if agentUserID <= 0 {
		return nil, ErrAgentProfileNotFound
	}

	profile, err := s.repo.LoadAgentProfile(ctx, agentUserID)
	if err != nil {
		return nil, err
	}
	if profile == nil {
		return nil, ErrAgentProfileNotFound
	}
	if !profile.Enabled {
		return nil, ErrAgentDisabled
	}

	normalized, err := s.normalizeWithdrawAmount(ctx, agentUserID, amount)
	if err != nil {
		return nil, err
	}

	// 自助提现没有客户端幂等键（接口签名不含 operationID），这里生成一次性键满足
	// 仓储的幂等约束。代价是同一笔申请被重复提交会各记一笔，因此防线放在余额校验上
	// （第二条会因余额不足被拒），前端仍应禁用重复提交。
	record, _, err := s.repo.InsertWithdrawal(ctx, AgentWithdrawalEntry{
		AgentUserID: agentUserID,
		Amount:      normalized,
		OperationID: agentWithdrawOperationID(agentWithdrawOperationScopeSelf, newAgentOperationID()),
		Status:      AgentWithdrawalStatusPaid,
		Note:        strings.TrimSpace(note),
	})
	if err != nil {
		return nil, err
	}
	return record, nil
}

// =========================
// 内部辅助
// =========================

// listChildren 是下级列表的唯一出口，同时处理可选能力的降级。
// 仓储尚未实现 AgentChildReader 时返回 503 而不是空列表：空列表会让代理以为
// "我的下级都不见了"，明确的 503 才能告诉前端该功能的后端能力尚未就绪。
func (s *AgentService) listChildren(ctx context.Context, agentUserID int64, page, pageSize int) ([]AgentChild, int, error) {
	reader, ok := s.repo.(AgentChildReader)
	if !ok {
		return nil, 0, infraerrors.ServiceUnavailable("AGENT_CHILDREN_UNAVAILABLE",
			"下级列表暂不可用：仓储尚未实现 ListAgentChildren")
	}
	return reader.ListAgentChildren(ctx, agentUserID, page, pageSize)
}

// resolveCostFloor 计算下级售价倍率的下限。
//
// 下限 = 代理在该分组的成本倍率；没有配置成本倍率时按"代理自用成本价"处理，
// 即代理自己在该分组的售价（user_group_rates 的专属倍率），未知时取 1.0（原价）。
// 这样既不会让代理以低于成本的价格放货，也不会因为一处配置缺失就放行任意低价。
//
// 代理自身售价来自 UserRepository.GetByID 填充的 User.GroupRates：该字段由
// AdminService 侧显式补载，普通 GetByID 不查 user_group_rates，因此这里查不到就
// 保持 1.0（下限偏高只会拒绝一次配置并提示，不会放行可能资损的低价）。
//
// groupID 为 nil 表示配置的是下级的全局默认售价：它作用于所有未被分组级覆盖的分组，
// 因此下限取「所有已配置分组成本里的最大值」——只有这样才能保证在任何一个分组上
// 都不产生负数佣金。一个成本也没配时仍然按 1.0。
func (s *AgentService) resolveCostFloor(ctx context.Context, agentUserID int64, groupID *int64) (float64, error) {
	if groupID == nil || *groupID <= 0 {
		costs, err := s.repo.GetGroupCosts(ctx, agentUserID)
		if err != nil {
			return 0, err
		}
		floor := 0.0
		for _, cost := range costs {
			if cost, ok := NormalizeMultiplier(cost); ok && cost > floor {
				floor = cost
			}
		}
		if floor <= 0 {
			floor = 1.0
		}
		return floor, nil
	}

	configured, err := s.repo.GetAgentCostMultiplier(ctx, agentUserID, *groupID)
	if err != nil {
		return 0, err
	}

	agentOwnSellingRate := 1.0
	if s.userRepo != nil {
		if user, userErr := s.userRepo.GetByID(ctx, agentUserID); userErr == nil && user != nil {
			if rate, ok := user.GroupRates[*groupID]; ok && rate > 0 {
				agentOwnSellingRate = rate
			}
		}
	}
	return ResolveAgentCostMultiplier(configured, agentOwnSellingRate), nil
}

// normalizeWithdrawAmount 校验并量化提现金额：> 0、不低于最低提现额、余额充足。
func (s *AgentService) normalizeWithdrawAmount(ctx context.Context, agentUserID int64, amount float64) (float64, error) {
	if math.IsNaN(amount) || math.IsInf(amount, 0) {
		return 0, ErrAgentCommissionAmountInvalid
	}
	// 与佣金账户 DECIMAL(20,8) 对齐，避免 8 位之后的浮点噪声导致余额比较误判。
	amount = roundTo(amount, 8)
	if amount <= 0 {
		return 0, ErrAgentCommissionAmountInvalid
	}
	if min := s.minWithdrawAmount(ctx); min > 0 && amount < min {
		return 0, infraerrors.BadRequest("AGENT_WITHDRAW_BELOW_MINIMUM",
			fmt.Sprintf("提现金额不能低于最低提现额 %s", formatAgentAmount(min)))
	}

	account, err := s.repo.GetCommissionAccount(ctx, agentUserID)
	if err != nil {
		return 0, err
	}
	available := 0.0
	if account != nil {
		available = account.Available
	}
	// 这里只是提前给出可读的错误信息；真正的并发防线是仓储的
	// `UPDATE ... WHERE available >= amount`，它保证余额不会被扣成负数。
	// 金额放在 metadata 而不是拼进 message：前端可以直接用结构化字段渲染提示，
	// 不必去解析一句已经格式化的中文。
	if amount-available > agentAmountEpsilon {
		return 0, ErrAgentCommissionInsufficient.WithMetadata(map[string]string{
			"available": formatAgentAmount(available),
			"requested": formatAgentAmount(amount),
		})
	}
	return amount, nil
}

// minWithdrawAmount 读取最低提现额（设置项 agent_min_withdraw）。
//
// 缺失、解析失败或负数都按 0（不限制）：设置读取失败不应该把代理的提现入口整体关掉，
// 金额上限的真实验证在余额与仓储层。
func (s *AgentService) minWithdrawAmount(ctx context.Context) float64 {
	if s == nil || s.settingService == nil || s.settingService.settingRepo == nil {
		return 0
	}
	raw, err := s.settingService.settingRepo.GetValue(ctx, SettingKeyAgentMinWithdraw)
	if err != nil {
		return 0
	}
	value, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
		return 0
	}
	return value
}

// applyAgentAccountToDetail 把账户余额填进详情（账户行不存在时保持 0）。
func applyAgentAccountToDetail(detail *AgentDetail, account *AgentCommissionAccount) {
	if detail == nil || account == nil {
		return
	}
	detail.Available = account.Available
	detail.Frozen = account.Frozen
	detail.Withdrawn = account.Withdrawn
	detail.TotalCommission = roundTo(account.Available+account.Frozen+account.Withdrawn, 8)
}

// normalizeAgentPaging 归一化分页参数：默认 20 条、最多 200 条。
func normalizeAgentPaging(page, pageSize int) (int, int) {
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = agentDefaultPageSize
	}
	if pageSize > agentRecordMaxPageSize {
		pageSize = agentRecordMaxPageSize
	}
	return page, pageSize
}

func normalizeAgentCommissionFilter(filter AgentCommissionFilter) AgentCommissionFilter {
	filter.Page, filter.PageSize = normalizeAgentPaging(filter.Page, filter.PageSize)
	filter.Status = strings.TrimSpace(filter.Status)
	if filter.AgentUserID != nil && *filter.AgentUserID <= 0 {
		filter.AgentUserID = nil
	}
	if filter.ChildUserID != nil && *filter.ChildUserID <= 0 {
		filter.ChildUserID = nil
	}
	return filter
}

func normalizeAgentWithdrawalFilter(filter AgentWithdrawalFilter) AgentWithdrawalFilter {
	filter.Page, filter.PageSize = normalizeAgentPaging(filter.Page, filter.PageSize)
	filter.Status = strings.TrimSpace(filter.Status)
	if filter.AgentUserID != nil && *filter.AgentUserID <= 0 {
		filter.AgentUserID = nil
	}
	return filter
}

func isAgentCommissionStatus(status string) bool {
	switch status {
	case AgentCommissionStatusPending, AgentCommissionStatusAvailable,
		AgentCommissionStatusWithdrawn, AgentCommissionStatusReversed:
		return true
	default:
		return false
	}
}

// agentWithdrawOperationID 把调用方的幂等键映射为库里的 operation_id。
//
// 加作用域前缀再做哈希：同一个字符串键在管理端登记与代理自助申请之间、
// 以及与邀请返利模块的提现键之间都不会互相顶掉。
func agentWithdrawOperationID(scope, key string) string {
	sum := sha256.Sum256([]byte(scope + "\x00" + key))
	return hex.EncodeToString(sum[:])
}

// newAgentOperationID 生成一次性 operation id（随机源不可用时退化为纳秒时间戳）。
func newAgentOperationID() string {
	if id, err := uuid.NewRandom(); err == nil {
		return id.String()
	}
	return strconv.FormatInt(time.Now().UnixNano(), 10)
}

// formatAgentAmount 以定点形式输出金额（最多 8 位小数、去掉无意义的尾零），
// 只用于错误提示，避免把 0.30000000000000004 这种浮点噪声写给用户看。
func formatAgentAmount(v float64) string {
	return strconv.FormatFloat(roundTo(v, 8), 'f', -1, 64)
}
