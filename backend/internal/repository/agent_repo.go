package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"
)

// 代理佣金状态机取值，与 migration 292 里 agent_commission_ledger 的 CHECK 约束保持一致。
// 放在仓储层是因为账本状态的迁移由这里驱动（解冻把 pending 改成 available），
// service 层直接引用这些常量即可避免拼写漂移。
const (
	AgentCommissionStatusPending   = "pending"
	AgentCommissionStatusAvailable = "available"
	AgentCommissionStatusWithdrawn = "withdrawn"
	AgentCommissionStatusReversed  = "reversed"
)

// 提现登记状态：paid 表示已线下打款，voided 表示该笔打款作废（金额已人工退回）。
const (
	AgentWithdrawalStatusPaid   = "paid"
	AgentWithdrawalStatusVoided = "voided"
)

var (
	// ErrAgentProfileNotFound 表示该用户没有代理档案（未被管理员开通代理身份）。
	ErrAgentProfileNotFound = errors.New("agent profile not found")
	// ErrAgentCommissionInsufficient 表示可提现佣金余额不足，扣减被拒绝。
	ErrAgentCommissionInsufficient = errors.New("agent commission available balance insufficient")
	// ErrAgentCommissionAccountNotFound 表示该代理还没有佣金账户行（从未产生过佣金）。
	ErrAgentCommissionAccountNotFound = errors.New("agent commission account not found")
	// ErrAgentCommissionAmountInvalid 表示传入的金额/参数非法（<=0）。
	ErrAgentCommissionAmountInvalid = errors.New("agent commission amount invalid")
	// ErrAgentMultiplierInvalid 表示倍率非法：成本与售价倍率都必须 > 0，
	// 否则差价公式会变成负数或除零，直接拒绝比在计费链路上产生脏数据更安全。
	ErrAgentMultiplierInvalid = errors.New("agent multiplier must be positive")
	// ErrAgentWithdrawalConflict 表示同一 operation_id 已被另一笔不同代理/金额的
	// 提现登记占用，属于调用方幂等键复用错误。
	ErrAgentWithdrawalConflict = errors.New("agent withdrawal operation id conflict")
	// ErrAgentProfileInvalid 表示传入的代理档案本身不可用（nil 或缺少用户 ID），
	// 属于调用方编程错误而非业务状态。
	ErrAgentProfileInvalid = errors.New("agent profile is nil or invalid")
)

// AgentProfile 代理档案。UserID 即代理的用户 ID，也是主键，因此一个用户最多一条档案。
type AgentProfile struct {
	UserID                     int64
	Enabled                    bool
	DefaultChildRateMultiplier *float64
	CommissionFrozenHours      int
	Note                       string
	CreatedAt                  time.Time
	UpdatedAt                  time.Time
}

// AgentListFilter 代理列表筛选条件。
type AgentListFilter struct {
	Search   string
	Enabled  *bool
	Page     int
	PageSize int
}

// AgentListItem 代理列表行：档案 + 下级规模 + 佣金账户摘要。
// 累计佣金统计的是账本中所有非 reversed 状态的行，所以已提现部分不会被扣掉。
type AgentListItem struct {
	UserID          int64
	Email           string
	Username        string
	Enabled         bool
	ChildCount      int
	Available       float64
	Frozen          float64
	TotalCommission float64
	CreatedAt       time.Time
}

// AgentChildRate 代理给某个下级配置的售价倍率。GroupID 为 nil 表示该下级的全局默认值。
type AgentChildRate struct {
	AgentUserID    int64
	ChildUserID    int64
	GroupID        *int64
	RateMultiplier float64
	UpdatedAt      time.Time
}

// CommissionLedgerEntry 一笔待写入的佣金流水。
//
// BaseCost 是倍率前的基础成本（TotalCost），ChildPaid/AgentCost/Commission 由计费链路
// 用当次生效的倍率算好后传入：仓储只负责持久化与幂等，不重复推导金额，避免同一笔
// 请求在两处算出不同结果。
type CommissionLedgerEntry struct {
	AgentUserID   int64
	ChildUserID   *int64
	RequestID     string
	APIKeyID      int64
	GroupID       *int64
	Model         string
	BaseCost      float64
	ChildRate     float64
	AgentCostRate float64
	ChildPaid     float64
	AgentCost     float64
	Commission    float64
	Status        string
	FrozenUntil   *time.Time
	ReversalOf    *int64
}

// CommissionListFilter 佣金流水筛选条件。AgentUserID/ChildUserID 为 nil 表示不按该维度过滤。
type CommissionListFilter struct {
	AgentUserID *int64
	ChildUserID *int64
	Status      string
	StartAt     *time.Time
	EndAt       *time.Time
	Page        int
	PageSize    int
}

// CommissionRecord 佣金流水列表行（含代理与下级的邮箱，便于后台对账）。
type CommissionRecord struct {
	ID            int64
	AgentUserID   int64
	AgentEmail    string
	ChildUserID   *int64
	ChildEmail    string
	RequestID     string
	APIKeyID      int64
	GroupID       *int64
	Model         string
	BaseCost      float64
	ChildRate     float64
	AgentCostRate float64
	ChildPaid     float64
	AgentCost     float64
	Commission    float64
	Status        string
	FrozenUntil   *time.Time
	CreatedAt     time.Time
}

// CommissionAccount 佣金账户余额快照。
type CommissionAccount struct {
	AgentUserID int64
	Available   float64
	Frozen      float64
	Withdrawn   float64
	UpdatedAt   time.Time
}

// AgentWithdrawalEntry 一笔待登记的提现。OperationID 是调用方的幂等键：
// 同一 operation_id 重放不会重复扣款。
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

// AgentWithdrawal 提现记录行。
type AgentWithdrawal struct {
	ID          int64
	AgentUserID int64
	AgentEmail  string
	Amount      float64
	Status      string
	OperationID string
	Note        string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// AgentChild 代理的下级用户，附该代理为其配置的售价倍率。
// DefaultRate 是 group_id 为 NULL 的全局默认；GroupRates 是按分组的覆盖。
type AgentChild struct {
	UserID      int64
	Email       string
	Username    string
	CreatedAt   time.Time
	DefaultRate *float64
	GroupRates  map[int64]float64
}

// AgentRepository 代理佣金模块的数据访问契约。
// 上游按接口注入，便于 service 层测试打桩。
type AgentRepository interface {
	GetAgentProfile(ctx context.Context, userID int64) (*AgentProfile, error)
	UpsertAgentProfile(ctx context.Context, profile *AgentProfile) error
	ListAgents(ctx context.Context, filter AgentListFilter) ([]AgentListItem, int, error)

	GetGroupCosts(ctx context.Context, agentUserID int64) (map[int64]float64, error)
	ReplaceGroupCosts(ctx context.Context, agentUserID int64, costs map[int64]float64) error
	GetAgentCostMultiplier(ctx context.Context, agentUserID, groupID int64) (*float64, error)

	GetChildRate(ctx context.Context, agentUserID, childUserID int64, groupID *int64) (*float64, error)
	ReplaceChildRate(ctx context.Context, agentUserID, childUserID int64, groupID *int64, rate float64) error
	DeleteChildRate(ctx context.Context, agentUserID, childUserID int64, groupID *int64) error
	ListChildRates(ctx context.Context, agentUserID, childUserID int64) ([]AgentChildRate, error)

	GetInviterID(ctx context.Context, userID int64) (*int64, error)

	InsertCommission(ctx context.Context, entry CommissionLedgerEntry) (bool, error)
	RecordCommission(ctx context.Context, entry CommissionLedgerEntry, frozenHours int) (bool, error)
	ListCommissions(ctx context.Context, filter CommissionListFilter) ([]CommissionRecord, int, error)

	CreditCommissionAccount(ctx context.Context, agentUserID int64, amount float64, toFrozen bool, frozenHours int) error
	GetCommissionAccount(ctx context.Context, agentUserID int64) (*CommissionAccount, error)
	ThawFrozenCommissions(ctx context.Context, now time.Time) (int, error)
	DebitAvailable(ctx context.Context, agentUserID int64, amount float64) error

	ListWithdrawals(ctx context.Context, filter AgentWithdrawalFilter) ([]AgentWithdrawal, int, error)
	InsertWithdrawal(ctx context.Context, entry AgentWithdrawalEntry) (*AgentWithdrawal, bool, error)
}

type agentRepository struct {
	db *sql.DB
}

// 编译期校验：实现与接口签名漂移在构建时就会暴露，而不是等到注入时才报错。
var _ AgentRepository = (*agentRepository)(nil)

// NewAgentRepository 创建代理佣金仓储。
func NewAgentRepository(db *sql.DB) *agentRepository {
	return &agentRepository{db: db}
}

// runInTx 在单个事务内执行 fn，成功提交、失败回滚。
// 写法与 channelRepository.runInTx 一致（database/sql 层面没有共享的公共实现）。
func (r *agentRepository) runInTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin agent tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

// --- 代理档案 ---

func (r *agentRepository) GetAgentProfile(ctx context.Context, userID int64) (*AgentProfile, error) {
	if userID <= 0 {
		return nil, nil
	}
	var profile AgentProfile
	var defaultRate sql.NullFloat64
	err := scanSingleRow(ctx, r.db, `
SELECT user_id, enabled, default_child_rate_multiplier, commission_frozen_hours, note, created_at, updated_at
FROM agent_profiles
WHERE user_id = $1`, []any{userID},
		&profile.UserID,
		&profile.Enabled,
		&defaultRate,
		&profile.CommissionFrozenHours,
		&profile.Note,
		&profile.CreatedAt,
		&profile.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		// 不存在不是错误：绝大多数用户不是代理，调用方靠 nil 判断即可。
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get agent profile: %w", err)
	}
	profile.DefaultChildRateMultiplier = nullableFloat64Ptr(defaultRate)
	return &profile, nil
}

// UpsertAgentProfile 写入或覆盖代理档案，并回填 CreatedAt/UpdatedAt。
// 用 upsert 而不是先查后写：管理员保存时不需要区分新建与编辑。
func (r *agentRepository) UpsertAgentProfile(ctx context.Context, profile *AgentProfile) error {
	if profile == nil {
		return ErrAgentProfileInvalid
	}
	if profile.UserID <= 0 {
		return ErrAgentProfileNotFound
	}
	if profile.CommissionFrozenHours < 0 {
		return ErrAgentCommissionAmountInvalid
	}
	if profile.DefaultChildRateMultiplier != nil && *profile.DefaultChildRateMultiplier <= 0 {
		return ErrAgentMultiplierInvalid
	}
	if err := r.db.QueryRowContext(ctx, `
INSERT INTO agent_profiles (
    user_id, enabled, default_child_rate_multiplier, commission_frozen_hours, note, created_at, updated_at
)
VALUES ($1, $2, $3, $4, $5, NOW(), NOW())
ON CONFLICT (user_id) DO UPDATE
SET enabled = EXCLUDED.enabled,
    default_child_rate_multiplier = EXCLUDED.default_child_rate_multiplier,
    commission_frozen_hours = EXCLUDED.commission_frozen_hours,
    note = EXCLUDED.note,
    updated_at = NOW()
RETURNING created_at, updated_at`,
		profile.UserID,
		profile.Enabled,
		nullableArg(profile.DefaultChildRateMultiplier),
		profile.CommissionFrozenHours,
		profile.Note,
	).Scan(&profile.CreatedAt, &profile.UpdatedAt); err != nil {
		return fmt.Errorf("upsert agent profile: %w", err)
	}
	return nil
}

// ListAgents 分页列出代理档案，并聚合下级数量与佣金账户余额。
// 软删除（users.deleted_at 非空）的用户不出现在列表里：档案行仍在，
// 但后台不再展示已被删除的账号，避免管理员对一个不存在的用户改配置。
func (r *agentRepository) ListAgents(ctx context.Context, filter AgentListFilter) ([]AgentListItem, int, error) {
	where, args := buildAgentListWhere(filter)
	limit, offset := agentPageLimitOffset(filter.Page, filter.PageSize)

	const baseFrom = `
FROM agent_profiles p
JOIN users u ON u.id = p.user_id
LEFT JOIN agent_commission_accounts acc ON acc.agent_user_id = p.user_id`

	total, err := scanInt64(ctx, r.db, "SELECT COUNT(*) "+baseFrom+" WHERE u.deleted_at IS NULL"+where, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("count agents: %w", err)
	}

	queryArgs := append([]any{}, args...)
	queryArgs = append(queryArgs, limit, offset)
	rows, err := r.db.QueryContext(ctx, `
SELECT p.user_id,
       COALESCE(u.email, ''),
       COALESCE(u.username, ''),
       p.enabled,
       (SELECT COUNT(*)::integer FROM user_affiliates ua WHERE ua.inviter_id = p.user_id) AS child_count,
       COALESCE(acc.available, 0)::double precision,
       COALESCE(acc.frozen, 0)::double precision,
       (SELECT COALESCE(SUM(l.commission), 0)::double precision
          FROM agent_commission_ledger l
         WHERE l.agent_user_id = p.user_id
           AND l.status <> 'reversed') AS total_commission,
       p.created_at`+baseFrom+" WHERE u.deleted_at IS NULL"+where+`
ORDER BY p.created_at DESC, p.user_id DESC
LIMIT $`+fmt.Sprint(len(queryArgs)-1)+` OFFSET $`+fmt.Sprint(len(queryArgs)), queryArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("list agents: %w", err)
	}
	defer func() { _ = rows.Close() }()

	items := make([]AgentListItem, 0)
	for rows.Next() {
		var item AgentListItem
		if err := rows.Scan(
			&item.UserID,
			&item.Email,
			&item.Username,
			&item.Enabled,
			&item.ChildCount,
			&item.Available,
			&item.Frozen,
			&item.TotalCommission,
			&item.CreatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("scan agent list item: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate agent list: %w", err)
	}
	return items, int(total), nil
}

// buildAgentListWhere 生成代理列表的 AND 子句（无过滤条件时为空串）。
// 调用方已经写了 WHERE u.deleted_at IS NULL，所以这里只返回 AND 连接的条件。
func buildAgentListWhere(filter AgentListFilter) (string, []any) {
	clauses := make([]string, 0, 2)
	args := make([]any, 0, 2)

	if filter.Enabled != nil {
		args = append(args, *filter.Enabled)
		clauses = append(clauses, fmt.Sprintf("p.enabled = $%d", len(args)))
	}
	if search := strings.TrimSpace(filter.Search); search != "" {
		args = append(args, "%"+strings.ToLower(search)+"%")
		clauses = append(clauses, fmt.Sprintf("(LOWER(u.email) LIKE $%d OR LOWER(u.username) LIKE $%d)", len(args), len(args)))
	}
	if len(clauses) == 0 {
		return "", args
	}
	return " AND " + strings.Join(clauses, " AND "), args
}

// --- 代理×分组成本折扣 ---

func (r *agentRepository) GetGroupCosts(ctx context.Context, agentUserID int64) (map[int64]float64, error) {
	costs := make(map[int64]float64)
	if agentUserID <= 0 {
		return costs, nil
	}
	rows, err := r.db.QueryContext(ctx, `
SELECT group_id, cost_multiplier::double precision
FROM agent_group_costs
WHERE agent_user_id = $1`, agentUserID)
	if err != nil {
		return nil, fmt.Errorf("get agent group costs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var groupID int64
		var cost float64
		if err := rows.Scan(&groupID, &cost); err != nil {
			return nil, fmt.Errorf("scan agent group cost: %w", err)
		}
		costs[groupID] = cost
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate agent group costs: %w", err)
	}
	return costs, nil
}

// ReplaceGroupCosts 全量替换某代理的分组成本折扣。
// 采用「事务内删除 + 批量插入」而不是逐行 upsert：管理员保存的是完整表单，
// 从表单里删掉的分组必须一并失效，否则会出现无法删除的折扣。
func (r *agentRepository) ReplaceGroupCosts(ctx context.Context, agentUserID int64, costs map[int64]float64) error {
	if agentUserID <= 0 {
		return ErrAgentProfileNotFound
	}
	groupIDs := make([]int64, 0, len(costs))
	multipliers := make([]float64, 0, len(costs))
	for groupID, cost := range costs {
		if groupID <= 0 {
			return ErrAgentMultiplierInvalid
		}
		if cost <= 0 {
			return ErrAgentMultiplierInvalid
		}
		groupIDs = append(groupIDs, groupID)
		multipliers = append(multipliers, cost)
	}

	return r.runInTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM agent_group_costs WHERE agent_user_id = $1`, agentUserID); err != nil {
			return fmt.Errorf("delete agent group costs: %w", err)
		}
		if len(groupIDs) == 0 {
			return nil
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO agent_group_costs (agent_user_id, group_id, cost_multiplier, created_at, updated_at)
SELECT $1::bigint, data.group_id, data.cost_multiplier, NOW(), NOW()
FROM unnest($2::bigint[], $3::double precision[]) AS data(group_id, cost_multiplier)`,
			agentUserID, pq.Array(groupIDs), pq.Array(multipliers)); err != nil {
			return fmt.Errorf("insert agent group costs: %w", err)
		}
		return nil
	})
}

// GetAgentCostMultiplier 返回代理在指定分组的成本倍率；没有配置时返回 nil，
// 表示按 1.0（无折扣）处理。nil 与「配置成 1.0」在语义上等价，但前者能省掉一行存储。
func (r *agentRepository) GetAgentCostMultiplier(ctx context.Context, agentUserID, groupID int64) (*float64, error) {
	if agentUserID <= 0 || groupID <= 0 {
		return nil, nil
	}
	var cost float64
	err := scanSingleRow(ctx, r.db, `
SELECT cost_multiplier::double precision
FROM agent_group_costs
WHERE agent_user_id = $1 AND group_id = $2`, []any{agentUserID, groupID}, &cost)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get agent cost multiplier: %w", err)
	}
	return &cost, nil
}

// --- 代理→下级售价覆盖 ---

// GetChildRate 读取下级在某分组的售价倍率：先找精确分组，再回落到 group_id 为 NULL 的
// 全局默认值。两者都没有返回 nil，由调用方决定是否退回分组公开售价。
func (r *agentRepository) GetChildRate(ctx context.Context, agentUserID, childUserID int64, groupID *int64) (*float64, error) {
	if agentUserID <= 0 || childUserID <= 0 {
		return nil, nil
	}
	// $3 为 NULL 时 `group_id = $3` 恒为 NULL（不成立），因此只可能命中全局默认行；
	// 非 NULL 时精确行排在前面（CASE 0），全局默认作为回落（CASE 1）。
	var rate float64
	err := scanSingleRow(ctx, r.db, `
SELECT rate_multiplier::double precision
FROM agent_child_rates
WHERE agent_user_id = $1
  AND child_user_id = $2
  AND (group_id IS NULL OR group_id = $3::bigint)
ORDER BY CASE WHEN group_id IS NULL THEN 1 ELSE 0 END, id DESC
LIMIT 1`, []any{agentUserID, childUserID, nullableInt64Arg(groupID)}, &rate)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get agent child rate: %w", err)
	}
	return &rate, nil
}

// ReplaceChildRate 写入下级售价倍率（已存在则覆盖）。
//
// 用「先 UPDATE、未命中再 INSERT」而不是 ON CONFLICT 表达式推断：唯一索引建在
// COALESCE(group_id, 0) 这个表达式上，而表达式索引的 ON CONFLICT 推断对表达式
// 写法敏感（少一层括号就推不出索引），线上报错的成本比多一次 UPDATE 高。
// 并发插入时靠 uq_agent_child_rates_scope 兜底，冲突会以唯一冲突错误返回，
// 调用方重试即可（后台配置写入不属于高频路径）。
func (r *agentRepository) ReplaceChildRate(ctx context.Context, agentUserID, childUserID int64, groupID *int64, rate float64) error {
	if agentUserID <= 0 || childUserID <= 0 {
		return ErrAgentProfileNotFound
	}
	if rate <= 0 {
		return ErrAgentMultiplierInvalid
	}
	return r.runInTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `
UPDATE agent_child_rates
SET rate_multiplier = $4,
    updated_at = NOW()
WHERE agent_user_id = $1
  AND child_user_id = $2
  AND (($3::bigint IS NULL AND group_id IS NULL) OR group_id = $3::bigint)`,
			agentUserID, childUserID, nullableInt64Arg(groupID), rate)
		if err != nil {
			return fmt.Errorf("update agent child rate: %w", err)
		}
		if affected, err := res.RowsAffected(); err == nil && affected > 0 {
			return nil
		}

		if _, err := tx.ExecContext(ctx, `
INSERT INTO agent_child_rates (agent_user_id, child_user_id, group_id, rate_multiplier, created_at, updated_at)
VALUES ($1, $2, $3, $4, NOW(), NOW())`,
			agentUserID, childUserID, nullableInt64Arg(groupID), rate); err != nil {
			return fmt.Errorf("insert agent child rate: %w", err)
		}
		return nil
	})
}

// DeleteChildRate 删除一条售价覆盖；groupID 为 nil 时只删全局默认行，
// 不会连带删掉分组级配置。
func (r *agentRepository) DeleteChildRate(ctx context.Context, agentUserID, childUserID int64, groupID *int64) error {
	if agentUserID <= 0 || childUserID <= 0 {
		return nil
	}
	if _, err := r.db.ExecContext(ctx, `
DELETE FROM agent_child_rates
WHERE agent_user_id = $1
  AND child_user_id = $2
  AND (($3::bigint IS NULL AND group_id IS NULL) OR group_id = $3::bigint)`,
		agentUserID, childUserID, nullableInt64Arg(groupID)); err != nil {
		return fmt.Errorf("delete agent child rate: %w", err)
	}
	return nil
}

// ListChildRates 列出某代理给某下级配置的全部售价倍率（含全局默认行）。
func (r *agentRepository) ListChildRates(ctx context.Context, agentUserID, childUserID int64) ([]AgentChildRate, error) {
	items := make([]AgentChildRate, 0)
	if agentUserID <= 0 || childUserID <= 0 {
		return items, nil
	}
	rows, err := r.db.QueryContext(ctx, `
SELECT agent_user_id, child_user_id, group_id, rate_multiplier::double precision, updated_at
FROM agent_child_rates
WHERE agent_user_id = $1 AND child_user_id = $2
ORDER BY group_id NULLS FIRST, id`, agentUserID, childUserID)
	if err != nil {
		return nil, fmt.Errorf("list agent child rates: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var item AgentChildRate
		var groupID sql.NullInt64
		if err := rows.Scan(&item.AgentUserID, &item.ChildUserID, &groupID, &item.RateMultiplier, &item.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan agent child rate: %w", err)
		}
		item.GroupID = nullableInt64Ptr(groupID)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate agent child rates: %w", err)
	}
	return items, nil
}

// --- 上下级关系 ---

// GetInviterID 读取 user_affiliates.inviter_id。复用既有邀请关系表，
// 不再单独维护一份上下级关系：一级代理的下级就是该用户的邀请人。
func (r *agentRepository) GetInviterID(ctx context.Context, userID int64) (*int64, error) {
	if userID <= 0 {
		return nil, nil
	}
	var inviterID sql.NullInt64
	err := scanSingleRow(ctx, r.db, `
SELECT inviter_id
FROM user_affiliates
WHERE user_id = $1`, []any{userID}, &inviterID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get inviter id: %w", err)
	}
	return nullableInt64Ptr(inviterID), nil
}

// ListAgentChildren 分页列出某代理的下级，并带上该代理为其配置的售价倍率。
//
// 一次查询带出所有分组的覆盖倍率，避免每个下级再查一次（N+1）。
// 排序用 user_affiliates.created_at：这是「成为下级」的时间，比 users.created_at 更贴近业务。
func (r *agentRepository) ListAgentChildren(ctx context.Context, agentUserID int64, page, pageSize int) ([]AgentChild, int, error) {
	limit, offset := agentPageLimitOffset(page, pageSize)

	var total int
	if err := scanSingleRow(ctx, r.db, `
SELECT COUNT(*)
FROM user_affiliates
WHERE inviter_id = $1`, []any{agentUserID}, &total); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, 0, fmt.Errorf("count agent children: %w", err)
	}
	if total == 0 {
		return nil, 0, nil
	}

	rows, err := r.db.QueryContext(ctx, `
SELECT u.id, u.email, COALESCE(u.username, ''), ua.created_at
FROM user_affiliates ua
JOIN users u ON u.id = ua.user_id AND u.deleted_at IS NULL
WHERE ua.inviter_id = $1
ORDER BY ua.created_at DESC, u.id DESC
LIMIT $2 OFFSET $3`, agentUserID, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list agent children: %w", err)
	}
	defer func() { _ = rows.Close() }()

	children := make([]AgentChild, 0, limit)
	for rows.Next() {
		var child AgentChild
		if err := rows.Scan(&child.UserID, &child.Email, &child.Username, &child.CreatedAt); err != nil {
			return nil, 0, fmt.Errorf("scan agent child: %w", err)
		}
		children = append(children, child)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate agent children: %w", err)
	}
	if len(children) == 0 {
		return children, total, nil
	}

	rates, err := r.childRatesByChild(ctx, agentUserID)
	if err != nil {
		return nil, 0, err
	}
	for i := range children {
		if rate, ok := rates[children[i].UserID]; ok {
			groupRates := make(map[int64]float64, len(rate.groups))
			for groupID, value := range rate.groups {
				groupRates[groupID] = value
			}
			children[i].DefaultRate = rate.defaultRate
			if len(groupRates) > 0 {
				children[i].GroupRates = groupRates
			}
		}
	}
	return children, total, nil
}

type agentChildRateSet struct {
	defaultRate *float64
	groups      map[int64]float64
}

// childRatesByChild 一次性取出该代理为所有下级配置的倍率，按下级分组。
func (r *agentRepository) childRatesByChild(ctx context.Context, agentUserID int64) (map[int64]agentChildRateSet, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT child_user_id, group_id, rate_multiplier
FROM agent_child_rates
WHERE agent_user_id = $1`, agentUserID)
	if err != nil {
		return nil, fmt.Errorf("list agent child rates: %w", err)
	}
	defer func() { _ = rows.Close() }()

	result := make(map[int64]agentChildRateSet)
	for rows.Next() {
		var childID int64
		var groupID sql.NullInt64
		var rate float64
		if err := rows.Scan(&childID, &groupID, &rate); err != nil {
			return nil, fmt.Errorf("scan agent child rate: %w", err)
		}
		entry := result[childID]
		if groupID.Valid {
			if entry.groups == nil {
				entry.groups = make(map[int64]float64)
			}
			entry.groups[groupID.Int64] = rate
		} else {
			value := rate
			entry.defaultRate = &value
		}
		result[childID] = entry
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate agent child rates: %w", err)
	}
	return result, nil
}

// GetAffiliateCode 读取用户的邀请码。没有档案时返回空串（代理页降级为不可分享）。
func (r *agentRepository) GetAffiliateCode(ctx context.Context, userID int64) (string, error) {
	if userID <= 0 {
		return "", nil
	}
	var code string
	err := scanSingleRow(ctx, r.db, `
SELECT aff_code FROM user_affiliates WHERE user_id = $1`, []any{userID}, &code)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("get affiliate code: %w", err)
	}
	return code, nil
}

// --- 佣金流水 ---

// InsertCommission 写入一笔佣金流水，返回是否真的插入。// 幂等键与 usage_billing_dedup 同键 (request_id, api_key_id)：同一次用量结算被重放时
// 返回 inserted=false，调用方据此跳过账户加钱，避免重复发佣。
//
// 注意：本方法只写账本，不动账户余额。两者必须原子完成时请用 RecordCommission；
// 只在补录历史流水这种场景下单独调用。
//
// 补录时需要调用方自己给准 Status：
//   - status=available → 钱必须已由调用方另行入账，否则这笔佣金永远不会进余额；
//   - status=pending（含留空）→ ThawFrozenCommissions 会在 frozen_until 到期（NULL 视为
//     已到期）时把金额搬进 available，即“账本先落、余额稍后由解冻任务补齐”。
//
// 选 pending 作为默认值是有意的：宁可让钱到得晚一点，也不要因为漏写一条入账指令而
// 把佣金永久卡在账本里。
func (r *agentRepository) InsertCommission(ctx context.Context, entry CommissionLedgerEntry) (bool, error) {
	if entry.RequestID == "" {
		return false, fmt.Errorf("insert agent commission: request id is required")
	}
	return insertCommissionExec(ctx, r.db, entry)
}

// RecordCommission 在一个事务里写账本 + 增加账户余额，是计费链路的推荐入口。
//
// 冻结与否完全由账本 status 决定，账户余额桶跟着同一个布尔值走：
// 两侧若各自推导（一边按 status、一边按 frozenHours），就可能出现「账本还是 pending
// 但钱已经进可提现余额」，解冻时再搬一次就是双发佣金。
//
//	status 为空 → frozenHours>0 取 pending，否则取 available
//	status=pending → 钱进 frozen 桶，frozen_until = now + frozenHours
//	status=available → 钱进 available 桶，不留 frozen_until
//
// withdrawn/reversed 属于人工处置后的终态，本方法不接受：直接落账会把余额弄乱。
func (r *agentRepository) RecordCommission(ctx context.Context, entry CommissionLedgerEntry, frozenHours int) (bool, error) {
	if entry.RequestID == "" {
		return false, fmt.Errorf("record agent commission: request id is required")
	}
	if entry.Commission <= 0 {
		// 零佣金（下级售价不高于代理成本）不必留流水，否则账本里全是噪声。
		return false, nil
	}

	status := strings.TrimSpace(entry.Status)
	if status == "" {
		if frozenHours > 0 {
			status = AgentCommissionStatusPending
		} else {
			status = AgentCommissionStatusAvailable
		}
	}
	if status != AgentCommissionStatusPending && status != AgentCommissionStatusAvailable {
		return false, fmt.Errorf("record agent commission: unsupported status %q", status)
	}

	toFrozen := status == AgentCommissionStatusPending
	if toFrozen && frozenHours > 0 && entry.FrozenUntil == nil {
		frozenUntil := time.Now().Add(time.Duration(frozenHours) * time.Hour)
		entry.FrozenUntil = &frozenUntil
	}
	if !toFrozen {
		// 不冻结就不该留下待解冻时间，否则解冻任务会把它当到期行处理。
		entry.FrozenUntil = nil
	}
	entry.Status = status

	inserted := false
	err := r.runInTx(ctx, func(tx *sql.Tx) error {
		var err error
		inserted, err = insertCommissionExec(ctx, tx, entry)
		if err != nil || !inserted {
			return err
		}
		return creditCommissionAccountExec(ctx, tx, entry.AgentUserID, entry.Commission, toFrozen)
	})
	if err != nil {
		return false, err
	}
	return inserted, nil
}

func insertCommissionExec(ctx context.Context, exec dbExec, entry CommissionLedgerEntry) (bool, error) {
	if entry.AgentUserID <= 0 {
		return false, ErrAgentProfileNotFound
	}
	status := strings.TrimSpace(entry.Status)
	if status == "" {
		// 与 InsertCommission 的说明一致：默认 pending，由解冻任务负责搬到可提现余额。
		status = AgentCommissionStatusPending
	}
	var id int64
	err := exec.QueryRowContext(ctx, `
INSERT INTO agent_commission_ledger (
    agent_user_id, child_user_id, request_id, api_key_id, group_id, model,
    base_cost, child_rate, agent_cost_rate, child_paid, agent_cost, commission,
    status, frozen_until, reversal_of, created_at, updated_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, NOW(), NOW())
ON CONFLICT (request_id, api_key_id) DO NOTHING
RETURNING id`,
		entry.AgentUserID,
		nullableInt64Arg(entry.ChildUserID),
		entry.RequestID,
		entry.APIKeyID,
		nullableInt64Arg(entry.GroupID),
		entry.Model,
		entry.BaseCost,
		entry.ChildRate,
		entry.AgentCostRate,
		entry.ChildPaid,
		entry.AgentCost,
		entry.Commission,
		status,
		agentNullTimeArg(entry.FrozenUntil),
		nullableInt64Arg(entry.ReversalOf),
	).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("insert agent commission ledger: %w", err)
	}
	return true, nil
}

// ListCommissions 分页列出佣金流水。代理/下级邮箱通过 LEFT JOIN users 补齐，
// 用户被删除后流水仍然保留（child_user_id 会被置空），因此邮箱列可能为空串。
func (r *agentRepository) ListCommissions(ctx context.Context, filter CommissionListFilter) ([]CommissionRecord, int, error) {
	where, args := buildAgentCommissionWhere(filter)
	limit, offset := agentPageLimitOffset(filter.Page, filter.PageSize)

	total, err := scanInt64(ctx, r.db, "SELECT COUNT(*) FROM agent_commission_ledger l"+where, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("count agent commissions: %w", err)
	}

	queryArgs := append([]any{}, args...)
	queryArgs = append(queryArgs, limit, offset)
	rows, err := r.db.QueryContext(ctx, `
SELECT l.id,
       l.agent_user_id,
       COALESCE(ag.email, ''),
       l.child_user_id,
       COALESCE(ch.email, ''),
       l.request_id,
       l.api_key_id,
       l.group_id,
       l.model,
       l.base_cost::double precision,
       l.child_rate::double precision,
       l.agent_cost_rate::double precision,
       l.child_paid::double precision,
       l.agent_cost::double precision,
       l.commission::double precision,
       l.status,
       l.frozen_until,
       l.created_at
FROM agent_commission_ledger l
LEFT JOIN users ag ON ag.id = l.agent_user_id
LEFT JOIN users ch ON ch.id = l.child_user_id`+where+`
ORDER BY l.created_at DESC, l.id DESC
LIMIT $`+fmt.Sprint(len(queryArgs)-1)+` OFFSET $`+fmt.Sprint(len(queryArgs)), queryArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("list agent commissions: %w", err)
	}
	defer func() { _ = rows.Close() }()

	items := make([]CommissionRecord, 0)
	for rows.Next() {
		var item CommissionRecord
		var childUserID, groupID sql.NullInt64
		var frozenUntil sql.NullTime
		if err := rows.Scan(
			&item.ID,
			&item.AgentUserID,
			&item.AgentEmail,
			&childUserID,
			&item.ChildEmail,
			&item.RequestID,
			&item.APIKeyID,
			&groupID,
			&item.Model,
			&item.BaseCost,
			&item.ChildRate,
			&item.AgentCostRate,
			&item.ChildPaid,
			&item.AgentCost,
			&item.Commission,
			&item.Status,
			&frozenUntil,
			&item.CreatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("scan agent commission: %w", err)
		}
		item.ChildUserID = nullableInt64Ptr(childUserID)
		item.GroupID = nullableInt64Ptr(groupID)
		item.FrozenUntil = agentNullTimePtr(frozenUntil)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate agent commissions: %w", err)
	}
	return items, int(total), nil
}

// buildAgentCommissionWhere 生成佣金流水的 WHERE 子句（无过滤条件时为空串）。
// 所有值都走占位符，status 仅作为值比较，不做字符串拼接。
func buildAgentCommissionWhere(filter CommissionListFilter) (string, []any) {
	clauses := make([]string, 0, 5)
	args := make([]any, 0, 5)
	if filter.AgentUserID != nil && *filter.AgentUserID > 0 {
		args = append(args, *filter.AgentUserID)
		clauses = append(clauses, fmt.Sprintf("l.agent_user_id = $%d", len(args)))
	}
	if filter.ChildUserID != nil && *filter.ChildUserID > 0 {
		args = append(args, *filter.ChildUserID)
		clauses = append(clauses, fmt.Sprintf("l.child_user_id = $%d", len(args)))
	}
	if status := strings.TrimSpace(filter.Status); status != "" {
		args = append(args, status)
		clauses = append(clauses, fmt.Sprintf("l.status = $%d", len(args)))
	}
	if filter.StartAt != nil {
		args = append(args, *filter.StartAt)
		clauses = append(clauses, fmt.Sprintf("l.created_at >= $%d", len(args)))
	}
	if filter.EndAt != nil {
		args = append(args, *filter.EndAt)
		clauses = append(clauses, fmt.Sprintf("l.created_at <= $%d", len(args)))
	}
	if len(clauses) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(clauses, " AND "), args
}

// --- 佣金账户 ---

// CreditCommissionAccount 原子地给佣金账户加钱。
// 单条 upsert 完成「没有账户就建、有账户就累加」，避免先查后写的竞态。
//
// toFrozen 决定这笔钱进冻结余额还是可提现余额；frozenHours 仅用于与账本
// 的 frozen_until 对齐，本身不影响落哪个桶（冻结时长由 agent_profile 决定）。
func (r *agentRepository) CreditCommissionAccount(ctx context.Context, agentUserID int64, amount float64, toFrozen bool, frozenHours int) error {
	if agentUserID <= 0 {
		return ErrAgentProfileNotFound
	}
	if amount <= 0 {
		return ErrAgentCommissionAmountInvalid
	}
	return creditCommissionAccountExec(ctx, r.db, agentUserID, amount, toFrozen)
}

// creditCommissionAccountExec 在给定连接上把金额记入指定余额桶。
// 两个桶各自累加，因此同一句 upsert 同时适用于冻结与直接入账两种情况。
func creditCommissionAccountExec(ctx context.Context, exec dbExec, agentUserID int64, amount float64, toFrozen bool) error {
	if amount <= 0 {
		return nil
	}
	available := amount
	frozen := 0.0
	if toFrozen {
		available, frozen = 0, amount
	}
	if _, err := exec.ExecContext(ctx, `
INSERT INTO agent_commission_accounts (agent_user_id, available, frozen, withdrawn, updated_at)
VALUES ($1, $2, $3, 0, NOW())
ON CONFLICT (agent_user_id) DO UPDATE
SET available = agent_commission_accounts.available + EXCLUDED.available,
    frozen = agent_commission_accounts.frozen + EXCLUDED.frozen,
    updated_at = NOW()`,
		agentUserID, available, frozen); err != nil {
		return fmt.Errorf("credit agent commission account: %w", err)
	}
	return nil
}

// GetCommissionAccount 返回佣金账户快照；没有账户行（从未产生佣金）返回 (nil, nil)。
func (r *agentRepository) GetCommissionAccount(ctx context.Context, agentUserID int64) (*CommissionAccount, error) {
	if agentUserID <= 0 {
		return nil, nil
	}
	var account CommissionAccount
	err := scanSingleRow(ctx, r.db, `
SELECT agent_user_id,
       available::double precision,
       frozen::double precision,
       withdrawn::double precision,
       updated_at
FROM agent_commission_accounts
WHERE agent_user_id = $1`, []any{agentUserID},
		&account.AgentUserID, &account.Available, &account.Frozen, &account.Withdrawn, &account.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get agent commission account: %w", err)
	}
	return &account, nil
}

// ThawFrozenCommissions 解冻到期佣金：把 frozen_until 已到的 pending 流水转成 available，
// 并把同一代理的金额从冻结余额搬到可提现余额。返回解冻的流水条数。
//
// 用一条 CTE 语句完成「改流水 + 按代理汇总 + 同步账户」：PostgreSQL 会执行所有
// 数据修改 CTE（无论主查询是否引用它的输出），中间结果不落库、也不需要显式事务，
// 因此不存在「流水已解冻而余额没搬」的中间态。
//
// frozen_until 为 NULL 的 pending 行视作无需冻结，一并解冻。
//
// 只翻转 commission > 0 的 pending 行：这类行是正常发佣，每一行都对应一笔应该从
// frozen 搬到 available 的金额，翻转与搬钱一一对应。佣金 <= 0 的行（人工冲正、
// 或绕过 RecordCommission 直接补录的负数）保持 pending，留给人工处理——
// 若让它们一起翻转，流水状态已变而余额因非负 CHECK 不能相减，账就对不上了。
//
// 返回值是解冻的流水条数（不是代理数），便于调度器判断是否有进展。
func (r *agentRepository) ThawFrozenCommissions(ctx context.Context, now time.Time) (int, error) {
	var thawedRows int64
	// 第二个聚合列把数据修改 CTE credited 显式引用进来。PostgreSQL 本身保证
	// 数据修改 CTE 一定会被执行，这里保持显式引用是为了让读者一眼看出
	// 「余额确实在同一句里同步了」，同时也方便后续接监控指标。
	var creditedAgents int64
	err := r.db.QueryRowContext(ctx, `
WITH thawed AS (
    UPDATE agent_commission_ledger
    SET status = 'available',
        frozen_until = NULL,
        updated_at = NOW()
    WHERE status = 'pending'
      AND commission > 0
      AND (frozen_until IS NULL OR frozen_until <= $1)
    RETURNING agent_user_id, commission
),
per_agent AS (
    SELECT agent_user_id, SUM(commission) AS amount, COUNT(*) AS row_count
    FROM thawed
    GROUP BY agent_user_id
),
credited AS (
    INSERT INTO agent_commission_accounts (agent_user_id, available, frozen, withdrawn, updated_at)
    SELECT agent_user_id, amount, 0, 0, NOW()
    FROM per_agent
    WHERE amount > 0
    ON CONFLICT (agent_user_id) DO UPDATE
    SET available = agent_commission_accounts.available + EXCLUDED.available,
        frozen = GREATEST(agent_commission_accounts.frozen - EXCLUDED.available, 0),
        updated_at = NOW()
    RETURNING agent_user_id
)
SELECT (SELECT COALESCE(SUM(row_count), 0)::bigint FROM per_agent),
       (SELECT COUNT(*)::bigint FROM credited)`, now).Scan(&thawedRows, &creditedAgents)
	if err != nil {
		return 0, fmt.Errorf("thaw agent commissions: %w", err)
	}
	// creditedAgents 目前只用于诊断；解冻条数才是调用方关心的返回值。
	_ = creditedAgents
	return int(thawedRows), nil
}

// DebitAvailable 提现扣减：可提现余额不足时返回 ErrAgentCommissionInsufficient。
func (r *agentRepository) DebitAvailable(ctx context.Context, agentUserID int64, amount float64) error {
	return debitAvailableExec(ctx, r.db, agentUserID, amount)
}

// debitAvailableExec 在给定连接（db 或 tx）上扣减可提现余额。
// 条件写在 WHERE 里（available >= amount），由数据库保证不会扣成负数，
// 比「先查余额再扣」少一次往返且没有竞态窗口。
// 影响行数为 0 时无法区分「账户不存在」与「余额不足」，两者对调用方都是
// 不可提现，因此统一返回同一个哨兵错误。
func debitAvailableExec(ctx context.Context, exec dbExec, agentUserID int64, amount float64) error {
	if agentUserID <= 0 {
		return ErrAgentProfileNotFound
	}
	if amount <= 0 {
		return ErrAgentCommissionAmountInvalid
	}
	res, err := exec.ExecContext(ctx, `
UPDATE agent_commission_accounts
SET available = available - $2,
    withdrawn = withdrawn + $2,
    updated_at = NOW()
WHERE agent_user_id = $1
  AND available >= $2`, agentUserID, amount)
	if err != nil {
		return fmt.Errorf("debit agent commission available: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("debit agent commission available: %w", err)
	}
	if affected == 0 {
		return ErrAgentCommissionInsufficient
	}
	return nil
}

// --- 提现记录 ---

// InsertWithdrawal 登记一笔线下提现：扣减可提现余额 + 写入提现记录，同一事务完成。
//
// 幂等：先按 operation_id 抢占记录行，抢不到说明是同一请求的重放，
// 直接返回首次结果（inserted=false），不会二次扣款；若该 operation_id 已被
// 其他代理或不同金额占用，返回 ErrAgentWithdrawalConflict。
// 余额不足时整笔回滚，operation_id 不被占用，调用方可以改正金额后重试。
func (r *agentRepository) InsertWithdrawal(ctx context.Context, entry AgentWithdrawalEntry) (*AgentWithdrawal, bool, error) {
	if entry.AgentUserID <= 0 {
		return nil, false, ErrAgentProfileNotFound
	}
	if entry.Amount <= 0 {
		return nil, false, ErrAgentCommissionAmountInvalid
	}
	operationID := strings.TrimSpace(entry.OperationID)
	if operationID == "" {
		return nil, false, ErrAgentCommissionAmountInvalid
	}
	status := strings.TrimSpace(entry.Status)
	if status == "" {
		status = AgentWithdrawalStatusPaid
	}

	var result *AgentWithdrawal
	inserted := false
	err := r.runInTx(ctx, func(tx *sql.Tx) error {
		var id int64
		err := tx.QueryRowContext(ctx, `
INSERT INTO agent_commission_withdrawals (agent_user_id, amount, status, operation_id, note, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, NOW(), NOW())
ON CONFLICT (operation_id) DO NOTHING
RETURNING id`,
			entry.AgentUserID, entry.Amount, status, operationID, entry.Note,
		).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			existing, findErr := findAgentWithdrawalByOperation(ctx, tx, operationID)
			if findErr != nil {
				return findErr
			}
			if existing.AgentUserID != entry.AgentUserID || !agentAmountEqual(existing.Amount, entry.Amount) {
				return ErrAgentWithdrawalConflict
			}
			result = existing
			inserted = false
			return nil
		}
		if err != nil {
			return fmt.Errorf("insert agent withdrawal: %w", err)
		}

		if err := debitAvailableExec(ctx, tx, entry.AgentUserID, entry.Amount); err != nil {
			return err
		}

		record, err := findAgentWithdrawalByOperation(ctx, tx, operationID)
		if err != nil {
			return err
		}
		result = record
		inserted = true
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	return result, inserted, nil
}

func findAgentWithdrawalByOperation(ctx context.Context, exec dbExec, operationID string) (*AgentWithdrawal, error) {
	var record AgentWithdrawal
	err := scanSingleRow(ctx, exec, `
SELECT w.id,
       w.agent_user_id,
       COALESCE(u.email, ''),
       w.amount::double precision,
       w.status,
       w.operation_id,
       w.note,
       w.created_at,
       w.updated_at
FROM agent_commission_withdrawals w
LEFT JOIN users u ON u.id = w.agent_user_id
WHERE w.operation_id = $1`, []any{operationID},
		&record.ID, &record.AgentUserID, &record.AgentEmail, &record.Amount,
		&record.Status, &record.OperationID, &record.Note, &record.CreatedAt, &record.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("find agent withdrawal by operation id %q: %w", operationID, sql.ErrNoRows)
	}
	if err != nil {
		return nil, fmt.Errorf("find agent withdrawal: %w", err)
	}
	return &record, nil
}

// ListWithdrawals 分页列出提现记录。
func (r *agentRepository) ListWithdrawals(ctx context.Context, filter AgentWithdrawalFilter) ([]AgentWithdrawal, int, error) {
	clauses := make([]string, 0, 4)
	args := make([]any, 0, 4)
	if filter.AgentUserID != nil && *filter.AgentUserID > 0 {
		args = append(args, *filter.AgentUserID)
		clauses = append(clauses, fmt.Sprintf("w.agent_user_id = $%d", len(args)))
	}
	if status := strings.TrimSpace(filter.Status); status != "" {
		args = append(args, status)
		clauses = append(clauses, fmt.Sprintf("w.status = $%d", len(args)))
	}
	if filter.StartAt != nil {
		args = append(args, *filter.StartAt)
		clauses = append(clauses, fmt.Sprintf("w.created_at >= $%d", len(args)))
	}
	if filter.EndAt != nil {
		args = append(args, *filter.EndAt)
		clauses = append(clauses, fmt.Sprintf("w.created_at <= $%d", len(args)))
	}
	where := ""
	if len(clauses) > 0 {
		where = " WHERE " + strings.Join(clauses, " AND ")
	}
	limit, offset := agentPageLimitOffset(filter.Page, filter.PageSize)

	total, err := scanInt64(ctx, r.db, "SELECT COUNT(*) FROM agent_commission_withdrawals w"+where, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("count agent withdrawals: %w", err)
	}

	queryArgs := append([]any{}, args...)
	queryArgs = append(queryArgs, limit, offset)
	rows, err := r.db.QueryContext(ctx, `
SELECT w.id,
       w.agent_user_id,
       COALESCE(u.email, ''),
       w.amount::double precision,
       w.status,
       w.operation_id,
       w.note,
       w.created_at,
       w.updated_at
FROM agent_commission_withdrawals w
LEFT JOIN users u ON u.id = w.agent_user_id`+where+`
ORDER BY w.created_at DESC, w.id DESC
LIMIT $`+fmt.Sprint(len(queryArgs)-1)+` OFFSET $`+fmt.Sprint(len(queryArgs)), queryArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("list agent withdrawals: %w", err)
	}
	defer func() { _ = rows.Close() }()

	items := make([]AgentWithdrawal, 0)
	for rows.Next() {
		var item AgentWithdrawal
		if err := rows.Scan(
			&item.ID,
			&item.AgentUserID,
			&item.AgentEmail,
			&item.Amount,
			&item.Status,
			&item.OperationID,
			&item.Note,
			&item.CreatedAt,
			&item.UpdatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("scan agent withdrawal: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate agent withdrawals: %w", err)
	}
	return items, int(total), nil
}

// --- 内部辅助 ---

// agentPageLimitOffset 归一化分页参数：默认 20 条、最多 200 条，
// 避免后台一次拉全量流水。
func agentPageLimitOffset(page, pageSize int) (int, int) {
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 || pageSize > 200 {
		pageSize = 20
	}
	return pageSize, (page - 1) * pageSize
}

// agentNullTimeArg 把可选时间转成 SQL 参数：nil 或零值都当 NULL。
func agentNullTimeArg(v *time.Time) any {
	if v == nil || v.IsZero() {
		return nil
	}
	return *v
}

// agentNullTimePtr 把扫描出的可空时间还原成指针。
func agentNullTimePtr(v sql.NullTime) *time.Time {
	if !v.Valid {
		return nil
	}
	t := v.Time
	return &t
}

// agentAmountEqual 比较金额是否属于同一笔（库里是 DECIMAL(20,8)，
// 浮点直接比较会因 8 位缩放后的表示差异误判，这里放到 1e-8 的容差里比）。
func agentAmountEqual(a, b float64) bool {
	diff := a - b
	if diff < 0 {
		diff = -diff
	}
	return diff < 1e-8
}
