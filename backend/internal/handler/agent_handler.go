package handler

import (
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// AgentHandler 是「代理佣金模块」的代理自助 handler（对应 /agent/* 路由）。
//
// 与管理端 handler 的关键区别：所有操作的作用域都由 gin context 里的当前登录用户
// 决定，接口不接收 agent_user_id 之类的入参。这样即使前端传错参数，也不可能
// 读到或改到别人的佣金与下级；归属校验在 service 层还会再确认一次。
type AgentHandler struct {
	agentService *service.AgentService
}

// NewAgentHandler 创建代理自助 handler。
func NewAgentHandler(agentService *service.AgentService) *AgentHandler {
	return &AgentHandler{agentService: agentService}
}

// Profile 返回当前登录代理的档案、佣金余额与邀请码。
// GET /agent/profile
func (h *AgentHandler) Profile(c *gin.Context) {
	userID, ok := currentAgentUserID(c)
	if !ok {
		return
	}
	profile, err := h.agentService.GetSelfProfile(c.Request.Context(), userID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, profile)
}

// Children 分页列出当前代理自己的下级。
// GET /agent/children
func (h *AgentHandler) Children(c *gin.Context) {
	userID, ok := currentAgentUserID(c)
	if !ok {
		return
	}
	page, pageSize := response.ParsePagination(c)

	items, total, err := h.agentService.ListSelfChildren(c.Request.Context(), userID, page)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Paginated(c, items, int64(total), page, pageSize)
}

// SetChildRateRequest 是给下级设置售价倍率的请求体。
//
// GroupID 为 nil 表示该下级的全局默认售价（对所有分组生效），
// 非 nil 表示只在指定分组覆盖；优先级是「分组级 > 全局默认 > 分组公开价」。
//
// Rate 是规范字段名；RateMultiplier 是前端 API 文档使用的别名。
// 两者同时存在时以 Rate 为准，避免因字段名差异导致设置静默不生效。
type SetChildRateRequest struct {
	GroupID        *int64  `json:"group_id"`
	Rate           float64 `json:"rate"`
	RateMultiplier float64 `json:"rate_multiplier"`
}

// resolvedRate 返回实际生效的倍率。
func (r SetChildRateRequest) resolvedRate() float64 {
	if r.Rate > 0 {
		return r.Rate
	}
	return r.RateMultiplier
}

// SetChildRate 给当前代理的下级设置售价倍率。
//
// 归属校验（该用户确实是当前代理的下级）在 service 层完成，这里只负责
// 取值与错误映射：越权时返回 403，售价低于代理成本时返回 400 并带中文原因。
// PUT /agent/children/:id/rate
func (h *AgentHandler) SetChildRate(c *gin.Context) {
	userID, ok := currentAgentUserID(c)
	if !ok {
		return
	}

	childID, ok := parseAgentPathID(c, "id")
	if !ok {
		return
	}

	var req SetChildRateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	// 用实际生效的倍率（Rate 为主，RateMultiplier 为兼容别名）。
	rate := req.resolvedRate()
	if !isFiniteAgentRate(rate) || !(rate > 0) {
		response.BadRequest(c, "rate must be a finite number greater than 0")
		return
	}
	if req.GroupID != nil && *req.GroupID <= 0 {
		response.BadRequest(c, "Invalid group_id")
		return
	}

	if err := h.agentService.SetChildRate(c.Request.Context(), userID, childID, req.GroupID, rate); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{
		"child_user_id": childID,
		"group_id":      req.GroupID,
		"rate":          rate,
	})
}

// Commissions 分页列出当前代理的佣金流水。
// GET /agent/commissions
func (h *AgentHandler) Commissions(c *gin.Context) {
	userID, ok := currentAgentUserID(c)
	if !ok {
		return
	}
	page, pageSize := response.ParsePagination(c)

	items, total, err := h.agentService.ListSelfCommissions(c.Request.Context(), userID, service.AgentCommissionFilter{
		ChildUserID: parseAgentSelfInt64Ptr(c.Query("child_user_id")),
		Status:      c.Query("status"),
		Page:        page,
		PageSize:    pageSize,
		StartAt:     parseAgentSelfTimeParam(c.Query("start_at"), false),
		EndAt:       parseAgentSelfTimeParam(c.Query("end_at"), true),
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Paginated(c, items, int64(total), page, pageSize)
}

// SelfWithdrawRequest 是代理申请提现的请求体。
type SelfWithdrawRequest struct {
	Amount float64 `json:"amount" binding:"required,gt=0"`
	Note   string  `json:"note" binding:"omitempty,max=200"`
}

// Withdraw 代理自助申请提现（线下打款，管理端登记后到账）。
// POST /agent/withdraw
func (h *AgentHandler) Withdraw(c *gin.Context) {
	userID, ok := currentAgentUserID(c)
	if !ok {
		return
	}

	var req SelfWithdrawRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	result, err := h.agentService.RequestSelfWithdraw(c.Request.Context(), userID, req.Amount, req.Note)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

// Withdrawals 分页列出当前代理自己的提现记录。
// GET /agent/withdrawals
func (h *AgentHandler) Withdrawals(c *gin.Context) {
	userID, ok := currentAgentUserID(c)
	if !ok {
		return
	}
	page, pageSize := response.ParsePagination(c)

	// 代理维度显式锁定为当前登录用户：接口不接受外部传入的 agent_user_id，
	// 因此这里不存在"看到别人提现记录"的可能。
	items, total, err := h.agentService.ListWithdrawals(c.Request.Context(), service.AgentWithdrawalFilter{
		AgentUserID: &userID,
		Page:        page,
		PageSize:    pageSize,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Paginated(c, items, int64(total), page, pageSize)
}

// currentAgentUserID 取当前登录用户 ID。
//
// 取值方式与 UserHandler 一致：JWT / 管理员 Key 认证中间件把 AuthSubject 写进
// gin context（middleware.ContextKeyUser），这里用 GetAuthSubjectFromContext 读取。
// 取不到说明路由没挂认证中间件，直接 401，绝不能回落到"默认用户"。
func currentAgentUserID(c *gin.Context) (int64, bool) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.Unauthorized(c, "User not authenticated")
		return 0, false
	}
	return subject.UserID, true
}

// parseAgentPathID 解析路径里的 :id（下级用户 ID）。
func parseAgentPathID(c *gin.Context, name string) (int64, bool) {
	id, err := strconv.ParseInt(strings.TrimSpace(c.Param(name)), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid "+name)
		return 0, false
	}
	return id, true
}

// isFiniteAgentRate 判断倍率是否为可用于比较的有限数。
// NaN 与任何数比较都是 false，若漏过去会绕过 gt=0 校验。
func isFiniteAgentRate(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}

// parseAgentSelfTimeParam 解析时间过滤参数（RFC3339 或 YYYY-MM-DD）。
//
// 与管理端 handler 里的同名逻辑语义一致，但分属不同包（handler / admin），
// 各自保留一份实现，避免为了共享一个十行的时间解析而在两个包之间建依赖。
// endOfDay 为 true 时把纯日期扩展到当天 23:59:59.999999999，否则「到 X 日」
// 会把 X 日整天的流水排除在外。非法值返回 nil（不缩小范围），而不是让整次查询报错。
func parseAgentSelfTimeParam(raw string, endOfDay bool) *time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
		return &parsed
	}
	if parsed, err := time.Parse("2006-01-02", raw); err == nil {
		if endOfDay {
			end := parsed.AddDate(0, 0, 1).Add(-time.Nanosecond)
			return &end
		}
		return &parsed
	}
	return nil
}

// parseAgentSelfInt64Ptr 解析可选的 int64 查询参数；空值或非法值返回 nil。
// 非法值按「不筛选」处理，避免一次手误把整页流水清空。
func parseAgentSelfInt64Ptr(raw string) *int64 {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value <= 0 {
		return nil
	}
	return &value
}
