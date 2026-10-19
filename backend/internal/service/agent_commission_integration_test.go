//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// stubAgentCommissionRepo 是佣金解析所需仓储的最小桩。
type stubAgentCommissionRepo struct {
	inviterID  *int64
	costRate   *float64
	childRate  *float64
	profile    *AgentProfile
	inviterErr error
	costErr    error
	childErr   error
	profileErr error
	calls      int
}

func (s *stubAgentCommissionRepo) GetInviterID(context.Context, int64) (*int64, error) {
	s.calls++
	return s.inviterID, s.inviterErr
}

func (s *stubAgentCommissionRepo) GetAgentCostMultiplier(context.Context, int64, int64) (*float64, error) {
	return s.costRate, s.costErr
}

func (s *stubAgentCommissionRepo) GetChildRate(context.Context, int64, int64, *int64) (*float64, error) {
	return s.childRate, s.childErr
}

func (s *stubAgentCommissionRepo) GetAgentProfile(context.Context, int64) (*AgentProfile, error) {
	return s.profile, s.profileErr
}

func agentFloatPtr(v float64) *float64 { return &v }
func agentIntPtr(v int64) *int64       { return &v }

func TestAgentCommissionResolver_NoInviterProducesNoCommission(t *testing.T) {
	repo := &stubAgentCommissionRepo{}
	resolver := NewAgentCommissionResolver(repo)

	got := resolver.Resolve(context.Background(), 10, agentIntPtr(5))
	require.False(t, got.HasAgent())
	require.Zero(t, got.AgentUserID)
}

func TestAgentCommissionResolver_SelfInviteIsRejected(t *testing.T) {
	repo := &stubAgentCommissionRepo{inviterID: agentIntPtr(10)}
	resolver := NewAgentCommissionResolver(repo)

	got := resolver.Resolve(context.Background(), 10, agentIntPtr(5))
	require.False(t, got.HasAgent(), "自邀不得产生佣金")
}

func TestAgentCommissionResolver_DisabledAgentProducesNoCommission(t *testing.T) {
	repo := &stubAgentCommissionRepo{
		inviterID: agentIntPtr(99),
		profile:   &AgentProfile{UserID: 99, Enabled: false},
	}
	resolver := NewAgentCommissionResolver(repo)

	got := resolver.Resolve(context.Background(), 10, agentIntPtr(5))
	require.False(t, got.HasAgent(), "代理停用后不再计佣")
}

func TestAgentCommissionResolver_ResolvesCostAndChildRate(t *testing.T) {
	repo := &stubAgentCommissionRepo{
		inviterID: agentIntPtr(99),
		costRate:  agentFloatPtr(0.5),
		childRate: agentFloatPtr(0.6),
		profile:   &AgentProfile{UserID: 99, Enabled: true},
	}
	resolver := NewAgentCommissionResolver(repo)

	got := resolver.Resolve(context.Background(), 10, agentIntPtr(5))
	require.True(t, got.HasAgent())
	require.Equal(t, int64(99), got.AgentUserID)
	require.NotNil(t, got.AgentCostRate)
	require.InDelta(t, 0.5, *got.AgentCostRate, 1e-12)
	require.NotNil(t, got.ChildRate)
	require.InDelta(t, 0.6, *got.ChildRate, 1e-12)
}

func TestAgentCommissionResolver_RepositoryErrorDegradesToNoCommission(t *testing.T) {
	repo := &stubAgentCommissionRepo{inviterErr: context.DeadlineExceeded}
	resolver := NewAgentCommissionResolver(repo)

	got := resolver.Resolve(context.Background(), 10, agentIntPtr(5))
	require.False(t, got.HasAgent(), "解析失败必须降级为不计佣，不能阻断计费")
}

func TestAgentCommissionResolver_NilResolverIsSafe(t *testing.T) {
	var resolver *AgentCommissionResolver
	require.False(t, resolver.Resolve(context.Background(), 10, nil).HasAgent())
}

// 端到端口径验证：5 折成本 + 6 折售价，佣金应为 TotalCost × 0.1。
func TestAgentCommissionSnapshot_DiscrepancyMath(t *testing.T) {
	totalCost := 0.123456789
	childRate := 0.6
	agentCostRate := 0.5

	breakdown := CalculateCommission(CommissionInput{
		TotalCost:     totalCost,
		ChildRate:     childRate,
		AgentCostRate: agentCostRate,
	})
	require.True(t, breakdown.Eligible)

	expectedCommission := QuantizeUsageBillingAmount(breakdown.ChildPaid - breakdown.AgentCost)
	require.InDelta(t, expectedCommission, breakdown.Commission, 1e-15)

	// 佣金加上代理成本应精确等于下级实付，这是对账的关键恒等式。
	require.InDelta(t, breakdown.ChildPaid, breakdown.AgentCost+breakdown.Commission, 1e-15)
}

// BudgetUsageBillingCommand 的佣金字段必须参与幂等指纹：
// 同一 request_id 下佣金口径变化应被判为冲突，而不是静默沿用旧结果。
func TestUsageBillingCommand_CommissionFieldsAffectFingerprint(t *testing.T) {
	base := UsageBillingCommand{
		RequestID:   "req-1",
		APIKeyID:    1,
		UserID:      10,
		Model:       "gpt-5",
		BalanceCost: 1.0,
	}
	withCommission := base
	withCommission.AgentUserID = 99
	withCommission.CommissionAmount = 0.1
	withCommission.CommissionBaseCost = 1.0

	require.NotEqual(t, buildUsageBillingFingerprint(&base), buildUsageBillingFingerprint(&withCommission),
		"佣金相关字段必须参与指纹，否则重放时无法发现口径漂移")
}

// 无代理时佣金字段保持零值，仓储层据此跳过，不产生额外写入。
func TestUsageBillingCommand_NoAgentLeavesCommissionZero(t *testing.T) {
	cmd := UsageBillingCommand{RequestID: "req-2", APIKeyID: 1, UserID: 10, BalanceCost: 1.0}
	cmd.Normalize()
	require.Zero(t, cmd.AgentUserID)
	require.Zero(t, cmd.CommissionAmount)
}
