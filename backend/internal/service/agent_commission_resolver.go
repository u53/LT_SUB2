package service

import (
	"context"
	"time"

	gocache "github.com/patrickmn/go-cache"
	"golang.org/x/sync/singleflight"
)

// AgentCommissionResolver 在计费热路径上解析"这笔请求属于哪个代理、成本与售价各是多少"。
//
// 设计约束：
//   - 计费路径每个请求都会调用，必须带缓存，且缓存失败要能安全降级；
//   - 任何解析失败都退回"无佣金"，绝不能因为佣金模块故障阻断正常计费；
//   - 只处理一级代理：取直接上级，不递归。
type AgentCommissionResolver struct {
	repo AgentCommissionRepository

	cache    *gocache.Cache
	cacheTTL time.Duration
	sf       singleflight.Group
}

// AgentCommissionRepository 是佣金解析所需的最小仓储集合。
// 由 repository.agentRepository 实现。
type AgentCommissionRepository interface {
	// GetInviterID 返回用户的直接上级；无上级返回 (nil, nil)。
	GetInviterID(ctx context.Context, userID int64) (*int64, error)
	// GetAgentCostMultiplier 返回该代理在该分组的成本倍率；未配置返回 (nil, nil)。
	GetAgentCostMultiplier(ctx context.Context, agentUserID, groupID int64) (*float64, error)
	// GetChildRate 返回代理给下级设置的售价倍率；groupID 为 nil 表示查全局默认。
	GetChildRate(ctx context.Context, agentUserID, childUserID int64, groupID *int64) (*float64, error)
	// GetAgentProfile 读取代理档案；不存在返回 (nil, nil)。
	GetAgentProfile(ctx context.Context, userID int64) (*AgentProfile, error)
}

// AgentProfile 是佣金解析关心的代理档案字段。
type AgentProfile struct {
	UserID                     int64
	Enabled                    bool
	DefaultChildRateMultiplier *float64
	CommissionFrozenHours      int
}

// AgentCommissionContext 是一次请求的佣金解析结果。
type AgentCommissionContext struct {
	// AgentUserID 为 0 表示该用户没有上级代理，本次请求不产生佣金。
	AgentUserID int64
	// AgentCostRate 是代理的成本倍率；未配置时为 0，由计费侧回落到代理自身售价。
	AgentCostRate *float64
	// ChildRate 是代理为该下级设置的售价倍率；未设置时为 nil。
	ChildRate *float64
}

// HasAgent 报告本次请求是否归属某个代理。
func (c AgentCommissionContext) HasAgent() bool { return c.AgentUserID > 0 }

// NewAgentCommissionResolver 创建解析器。cacheTTL <= 0 时使用默认值。
func NewAgentCommissionResolver(repo AgentCommissionRepository) *AgentCommissionResolver {
	const defaultTTL = 60 * time.Second
	return &AgentCommissionResolver{
		repo:     repo,
		cache:    gocache.New(defaultTTL, 2*time.Minute),
		cacheTTL: defaultTTL,
	}
}

// Resolve 解析指定用户在某分组的佣金上下文。
//
// 返回零值表示"无代理、不计佣"。任何错误都被吞掉并降级为零值——
// 佣金模块不可用不应影响用户正常调用与扣费。
func (r *AgentCommissionResolver) Resolve(ctx context.Context, userID int64, groupID *int64) AgentCommissionContext {
	if r == nil || r.repo == nil || userID <= 0 {
		return AgentCommissionContext{}
	}

	key := "inviter:" + formatInt64(userID)
	inviterID, ok := r.cachedInviter(key)
	if !ok {
		value, err, _ := r.sf.Do(key, func() (any, error) {
			if cached, hit := r.cachedInviter(key); hit {
				return cached, nil
			}
			inviter, err := r.repo.GetInviterID(ctx, userID)
			if err != nil {
				return int64(0), err
			}
			var result int64
			if inviter != nil {
				result = *inviter
			}
			r.cache.Set(key, result, r.cacheTTL)
			return result, nil
		})
		if err != nil {
			return AgentCommissionContext{}
		}
		inviterID, _ = value.(int64)
	}
	if inviterID <= 0 || inviterID == userID {
		// 自邀不产生佣金：数据层已拦截，这里再兜一层，避免脏数据造成自买自卖。
		return AgentCommissionContext{}
	}

	result := AgentCommissionContext{AgentUserID: inviterID}

	// 代理被停用时不再计佣，但也不阻断下级请求。
	if profile, err := r.repo.GetAgentProfile(ctx, inviterID); err == nil {
		if profile != nil && !profile.Enabled {
			return AgentCommissionContext{}
		}
		if profile != nil && profile.DefaultChildRateMultiplier != nil {
			result.ChildRate = profile.DefaultChildRateMultiplier
		}
	}

	if groupID != nil && *groupID > 0 {
		if cost, err := r.repo.GetAgentCostMultiplier(ctx, inviterID, *groupID); err == nil && cost != nil {
			result.AgentCostRate = cost
		}
		// 下级在该分组的专属售价优先于代理的全局默认。
		if rate, err := r.repo.GetChildRate(ctx, inviterID, userID, groupID); err == nil && rate != nil {
			result.ChildRate = rate
		}
	}

	return result
}

func (r *AgentCommissionResolver) cachedInviter(key string) (int64, bool) {
	if r.cache == nil {
		return 0, false
	}
	if cached, ok := r.cache.Get(key); ok {
		if value, castOK := cached.(int64); castOK {
			return value, true
		}
	}
	return 0, false
}

// InvalidateChild 在代理修改下级折扣或成本折扣后清缓存。
// 不清理会沿用旧倍率直到 TTL 到期，导致用户被按错误价格扣费。
func (r *AgentCommissionResolver) InvalidateChild(userID int64) {
	if r == nil || r.cache == nil || userID <= 0 {
		return
	}
	r.cache.Delete("inviter:" + formatInt64(userID))
}

func formatInt64(v int64) string {
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	neg := v < 0
	if neg {
		v = -v
	}
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
