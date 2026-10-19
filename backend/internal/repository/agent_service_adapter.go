package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// agentServiceAdapter 把 repository 的仓储实现适配成 service 侧声明的接口。
//
// 为什么需要它：service 不能 import repository（会成环），所以两边各自声明了
// 方法名相同、DTO 不同的接口。适配器只做字段搬运与哨兵错误翻译，不做业务判断——
// 所有校验都在 service 层，避免同一份规则出现两个版本。
//
// 档案读写的方法名是 LoadAgentProfile / SaveAgentProfile，因为
// service.AgentCommissionRepository（计费热路径）已经占用了 GetAgentProfile
// 这个名字且返回不同类型，Go 不允许同签同名的不同返回类型。
type agentServiceAdapter struct {
	repo *agentRepository
}

// NewAgentServiceRepository 构造供 service.AgentService 使用的适配器。
func NewAgentServiceRepository(db *sql.DB) service.AgentRepository {
	return &agentServiceAdapter{repo: &agentRepository{db: db}}
}

func (a *agentServiceAdapter) LoadAgentProfile(ctx context.Context, userID int64) (*service.AgentProfileRecord, error) {
	profile, err := a.repo.GetAgentProfile(ctx, userID)
	if err != nil {
		return nil, err
	}
	if profile == nil {
		return nil, nil
	}
	return &service.AgentProfileRecord{
		UserID:                     profile.UserID,
		Enabled:                    profile.Enabled,
		DefaultChildRateMultiplier: profile.DefaultChildRateMultiplier,
		CommissionFrozenHours:      profile.CommissionFrozenHours,
		Note:                       profile.Note,
		CreatedAt:                  profile.CreatedAt,
		UpdatedAt:                  profile.UpdatedAt,
	}, nil
}

func (a *agentServiceAdapter) SaveAgentProfile(ctx context.Context, profile *service.AgentProfileRecord) error {
	if profile == nil {
		return ErrAgentProfileInvalid
	}
	return a.repo.UpsertAgentProfile(ctx, &AgentProfile{
		UserID:                     profile.UserID,
		Enabled:                    profile.Enabled,
		DefaultChildRateMultiplier: profile.DefaultChildRateMultiplier,
		CommissionFrozenHours:      profile.CommissionFrozenHours,
		Note:                       profile.Note,
	})
}

func (a *agentServiceAdapter) ListAgents(ctx context.Context, filter service.AgentListFilter) ([]service.AgentListItem, int, error) {
	items, total, err := a.repo.ListAgents(ctx, AgentListFilter{
		Search:   filter.Search,
		Enabled:  filter.Enabled,
		Page:     filter.Page,
		PageSize: filter.PageSize,
	})
	if err != nil {
		return nil, 0, err
	}
	out := make([]service.AgentListItem, 0, len(items))
	for _, item := range items {
		out = append(out, service.AgentListItem{
			UserID:          item.UserID,
			Email:           item.Email,
			Username:        item.Username,
			Enabled:         item.Enabled,
			ChildCount:      item.ChildCount,
			Available:       item.Available,
			Frozen:          item.Frozen,
			TotalCommission: item.TotalCommission,
			CreatedAt:       item.CreatedAt,
		})
	}
	return out, total, nil
}

func (a *agentServiceAdapter) GetGroupCosts(ctx context.Context, agentUserID int64) (map[int64]float64, error) {
	return a.repo.GetGroupCosts(ctx, agentUserID)
}

func (a *agentServiceAdapter) ReplaceGroupCosts(ctx context.Context, agentUserID int64, costs map[int64]float64) error {
	return a.repo.ReplaceGroupCosts(ctx, agentUserID, costs)
}

func (a *agentServiceAdapter) GetAgentCostMultiplier(ctx context.Context, agentUserID, groupID int64) (*float64, error) {
	return a.repo.GetAgentCostMultiplier(ctx, agentUserID, groupID)
}

func (a *agentServiceAdapter) GetInviterID(ctx context.Context, userID int64) (*int64, error) {
	return a.repo.GetInviterID(ctx, userID)
}

func (a *agentServiceAdapter) ReplaceChildRate(ctx context.Context, agentUserID, childUserID int64, groupID *int64, rate float64) error {
	return a.repo.ReplaceChildRate(ctx, agentUserID, childUserID, groupID, rate)
}

func (a *agentServiceAdapter) GetCommissionAccount(ctx context.Context, agentUserID int64) (*service.AgentCommissionAccount, error) {
	account, err := a.repo.GetCommissionAccount(ctx, agentUserID)
	if err != nil || account == nil {
		return nil, err
	}
	return &service.AgentCommissionAccount{
		AgentUserID: account.AgentUserID,
		Available:   account.Available,
		Frozen:      account.Frozen,
		Withdrawn:   account.Withdrawn,
		UpdatedAt:   account.UpdatedAt,
	}, nil
}

func (a *agentServiceAdapter) ListCommissions(ctx context.Context, filter service.AgentCommissionFilter) ([]service.AgentCommissionRecord, int, error) {
	records, total, err := a.repo.ListCommissions(ctx, CommissionListFilter{
		AgentUserID: filter.AgentUserID,
		ChildUserID: filter.ChildUserID,
		Status:      filter.Status,
		StartAt:     filter.StartAt,
		EndAt:       filter.EndAt,
		Page:        filter.Page,
		PageSize:    filter.PageSize,
	})
	if err != nil {
		return nil, 0, err
	}
	out := make([]service.AgentCommissionRecord, 0, len(records))
	for _, record := range records {
		out = append(out, service.AgentCommissionRecord{
			ID:            record.ID,
			AgentUserID:   record.AgentUserID,
			AgentEmail:    record.AgentEmail,
			ChildUserID:   record.ChildUserID,
			ChildEmail:    record.ChildEmail,
			RequestID:     record.RequestID,
			APIKeyID:      record.APIKeyID,
			GroupID:       record.GroupID,
			Model:         record.Model,
			BaseCost:      record.BaseCost,
			ChildRate:     record.ChildRate,
			AgentCostRate: record.AgentCostRate,
			ChildPaid:     record.ChildPaid,
			AgentCost:     record.AgentCost,
			Commission:    record.Commission,
			Status:        record.Status,
			FrozenUntil:   record.FrozenUntil,
			CreatedAt:     record.CreatedAt,
		})
	}
	return out, total, nil
}

func (a *agentServiceAdapter) ListWithdrawals(ctx context.Context, filter service.AgentWithdrawalFilter) ([]service.AgentWithdrawal, int, error) {
	items, total, err := a.repo.ListWithdrawals(ctx, AgentWithdrawalFilter{
		AgentUserID: filter.AgentUserID,
		Status:      filter.Status,
		StartAt:     filter.StartAt,
		EndAt:       filter.EndAt,
		Page:        filter.Page,
		PageSize:    filter.PageSize,
	})
	if err != nil {
		return nil, 0, err
	}
	out := make([]service.AgentWithdrawal, 0, len(items))
	for _, item := range items {
		out = append(out, toServiceWithdrawal(item))
	}
	return out, total, nil
}

func (a *agentServiceAdapter) InsertWithdrawal(ctx context.Context, entry service.AgentWithdrawalEntry) (*service.AgentWithdrawal, bool, error) {
	item, replayed, err := a.repo.InsertWithdrawal(ctx, AgentWithdrawalEntry{
		AgentUserID: entry.AgentUserID,
		Amount:      entry.Amount,
		OperationID: entry.OperationID,
		Note:        entry.Note,
	})
	if err != nil {
		// 余额竞态必须以业务错误返回（400），而不是 500：
		// 这是"提现额超过可提现余额"的正常拒绝路径。
		if errors.Is(err, ErrAgentCommissionInsufficient) {
			return nil, false, service.ErrAgentCommissionInsufficient
		}
		if errors.Is(err, ErrAgentWithdrawalConflict) {
			return nil, false, service.ErrAgentWithdrawalConflict
		}
		return nil, false, err
	}
	if item == nil {
		return nil, replayed, nil
	}
	converted := toServiceWithdrawal(*item)
	converted.Replayed = replayed
	return &converted, replayed, nil
}

func toServiceWithdrawal(item AgentWithdrawal) service.AgentWithdrawal {
	return service.AgentWithdrawal{
		ID:          item.ID,
		AgentUserID: item.AgentUserID,
		AgentEmail:  item.AgentEmail,
		Amount:      item.Amount,
		Status:      item.Status,
		OperationID: item.OperationID,
		Note:        item.Note,
		CreatedAt:   item.CreatedAt,
		UpdatedAt:   item.UpdatedAt,
	}
}

// 可选能力：代理自助页的下级列表与邀请码。
// service 侧把它们声明成可选接口，仓储补齐后自动生效。

func (a *agentServiceAdapter) ListAgentChildren(ctx context.Context, agentUserID int64, page, pageSize int) ([]service.AgentChild, int, error) {
	children, total, err := a.repo.ListAgentChildren(ctx, agentUserID, page, pageSize)
	if err != nil {
		return nil, 0, err
	}
	out := make([]service.AgentChild, 0, len(children))
	for _, child := range children {
		out = append(out, service.AgentChild{
			UserID:      child.UserID,
			Email:       child.Email,
			Username:    child.Username,
			CreatedAt:   child.CreatedAt,
			DefaultRate: child.DefaultRate,
			GroupRates:  child.GroupRates,
		})
	}
	return out, total, nil
}

func (a *agentServiceAdapter) GetAffiliateCode(ctx context.Context, userID int64) (string, error) {
	return a.repo.GetAffiliateCode(ctx, userID)
}
