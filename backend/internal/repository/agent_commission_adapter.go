package repository

import (
	"context"
	"database/sql"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// agentCommissionAdapter 把仓储适配成计费热路径使用的 service.AgentCommissionRepository。
//
// 与 agentServiceAdapter 分开：两者都暴露 GetAgentProfile，但返回各自包里的档案类型
// （service.AgentProfile vs service.AgentProfileRecord）。Go 不允许同一类型用同名方法
// 返回不同类型，因此必须拆成两个适配器，各自承担一种消费方的视图。
type agentCommissionAdapter struct {
	repo *agentRepository
}

// NewAgentCommissionRepository 构造供计费链路使用的佣金解析仓储。
func NewAgentCommissionRepository(db *sql.DB) service.AgentCommissionRepository {
	return &agentCommissionAdapter{repo: &agentRepository{db: db}}
}

func (a *agentCommissionAdapter) GetAgentProfile(ctx context.Context, userID int64) (*service.AgentProfile, error) {
	profile, err := a.repo.GetAgentProfile(ctx, userID)
	if err != nil || profile == nil {
		return nil, err
	}
	return &service.AgentProfile{
		UserID:                     profile.UserID,
		Enabled:                    profile.Enabled,
		DefaultChildRateMultiplier: profile.DefaultChildRateMultiplier,
		CommissionFrozenHours:      profile.CommissionFrozenHours,
	}, nil
}

func (a *agentCommissionAdapter) GetAgentCostMultiplier(ctx context.Context, agentUserID, groupID int64) (*float64, error) {
	return a.repo.GetAgentCostMultiplier(ctx, agentUserID, groupID)
}

func (a *agentCommissionAdapter) GetChildRate(ctx context.Context, agentUserID, childUserID int64, groupID *int64) (*float64, error) {
	return a.repo.GetChildRate(ctx, agentUserID, childUserID, groupID)
}

func (a *agentCommissionAdapter) GetInviterID(ctx context.Context, userID int64) (*int64, error) {
	return a.repo.GetInviterID(ctx, userID)
}
