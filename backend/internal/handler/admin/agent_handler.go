package admin

import (
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// AgentHandler 是「代理佣金模块」的管理端 handler。
//
// 分页、错误映射与 Idempotency-Key 的处理方式与 AffiliateHandler 保持一致：
// 错误统一交给 response.ErrorFrom（它按 infraerrors 里的 HTTP 状态码映射），
// 分页统一走 response.ParsePagination + response.Paginated。
type AgentHandler struct {
	agentService *service.AgentService
}

// NewAgentHandler 创建管理端代理 handler。
func NewAgentHandler(agentService *service.AgentService) *AgentHandler {
	return &AgentHandler{agentService: agentService}
}

// List 分页列出代理。
// GET /admin/agents
func (h *AgentHandler) List(c *gin.Context) {
	page, pageSize := response.ParsePagination(c)

	// enabled 用 *bool 表达"不筛选"：只有显式传 true/false 才过滤，
	// 缺省时列出全部（含已停用），否则管理员看不到被停用的代理。
	items, total, err := h.agentService.ListAgents(c.Request.Context(), service.AgentListFilter{
		Search:   c.Query("search"),
		Enabled:  parseListEnabled(c.Query("enabled")),
		Page:     page,
		PageSize: pageSize,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Paginated(c, items, int64(total), page, pageSize)
}

// ListCommissions 分页列出佣金流水。
// GET /admin/agents/commissions
func (h *AgentHandler) ListCommissions(c *gin.Context) {
	page, pageSize := response.ParsePagination(c)

	filter := service.AgentCommissionFilter{
		Status:   c.Query("status"),
		Page:     page,
		PageSize: pageSize,
		StartAt:  parseAgentTimeParam(c.Query("start_at"), false),
		EndAt:    parseAgentTimeParam(c.Query("end_at"), true),
	}
	// 代理/下级 ID 是可选筛选维度：解析失败时返回 400 而不是静默忽略，
	// 否则管理员会拿到一份看起来"没有数据"的全量列表，误判成业务问题。
	if raw := strings.TrimSpace(c.Query("agent_user_id")); raw != "" {
		id, ok := parseAgentOptionalID(c, "agent_user_id", raw)
		if !ok {
			return
		}
		filter.AgentUserID = &id
	}
	if raw := strings.TrimSpace(c.Query("child_user_id")); raw != "" {
		id, ok := parseAgentOptionalID(c, "child_user_id", raw)
		if !ok {
			return
		}
		filter.ChildUserID = &id
	}

	items, total, err := h.agentService.ListCommissions(c.Request.Context(), filter)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Paginated(c, items, int64(total), page, pageSize)
}

// ListWithdrawals 分页列出提现登记。
// GET /admin/agents/withdrawals
func (h *AgentHandler) ListWithdrawals(c *gin.Context) {
	page, pageSize := response.ParsePagination(c)

	filter := service.AgentWithdrawalFilter{
		Status:   c.Query("status"),
		Page:     page,
		PageSize: pageSize,
		StartAt:  parseAgentTimeParam(c.Query("start_at"), false),
		EndAt:    parseAgentTimeParam(c.Query("end_at"), true),
	}
	if raw := strings.TrimSpace(c.Query("agent_user_id")); raw != "" {
		id, ok := parseAgentOptionalID(c, "agent_user_id", raw)
		if !ok {
			return
		}
		filter.AgentUserID = &id
	}

	items, total, err := h.agentService.ListWithdrawals(c.Request.Context(), filter)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Paginated(c, items, int64(total), page, pageSize)
}

// Get 读取单个代理详情。
// GET /admin/agents/:id
func (h *AgentHandler) Get(c *gin.Context) {
	id, ok := parsePositiveIDParam(c, "id")
	if !ok {
		return
	}
	detail, err := h.agentService.GetAgent(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, detail)
}

// UpdateAgentRequest 是保存代理档案的请求体。
//
// 三个字段都是"显式覆盖"语义（全量保存表单），因此都非指针：
// 前端提交的就是表单最终状态，不需要区分"未提供"与"清空"。
type UpdateAgentRequest struct {
	Enabled bool `json:"enabled"`
	// DefaultChildRateMultiplier 为 nil 表示不设置默认下级折扣（下级按分组公开价计费）。
	DefaultChildRateMultiplier *float64 `json:"default_child_rate_multiplier"`
	// CommissionFrozenHours 是佣金冻结期（小时），0 表示不冻结。
	CommissionFrozenHours int    `json:"commission_frozen_hours" binding:"min=0,max=720"`
	Note                  string `json:"note"`
}

// Update 保存代理档案（启用/停用、默认下级折扣、冻结期、备注）。
// PUT /admin/agents/:id
func (h *AgentHandler) Update(c *gin.Context) {
	id, ok := parsePositiveIDParam(c, "id")
	if !ok {
		return
	}

	var req UpdateAgentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	// 倍率必须 > 0：0 等于免费、负数会把佣金算反，service 层也会拦，
	// 但在这里拦能给出更贴近请求字段的报错。用 `!(x > 0)` 而不是 `x <= 0`，
	// 这样 NaN 也会被拒绝（NaN 与任何数比较都为 false）。
	if req.DefaultChildRateMultiplier != nil {
		value := *req.DefaultChildRateMultiplier
		if !(value > 0) || math.IsInf(value, 0) {
			response.BadRequest(c, "default_child_rate_multiplier must be a finite number > 0")
			return
		}
	}

	if err := h.agentService.UpsertAgent(c.Request.Context(), id, service.AgentUpsertInput{
		Enabled:                    req.Enabled,
		DefaultChildRateMultiplier: req.DefaultChildRateMultiplier,
		CommissionFrozenHours:      req.CommissionFrozenHours,
		Note:                       req.Note,
	}); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"user_id": id})
}

// GetGroupCosts 读取代理的分组成本折扣。
// GET /admin/agents/:id/group-costs
func (h *AgentHandler) GetGroupCosts(c *gin.Context) {
	id, ok := parsePositiveIDParam(c, "id")
	if !ok {
		return
	}
	costs, err := h.agentService.GetGroupCosts(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{
		"agent_user_id": id,
		"group_costs":   costs,
	})
}

// AgentGroupCostItem 是一项分组成本折扣。做成 {group_id, cost_multiplier} 数组
// 而不是 map[string]float64：JSON 的对象键只能是字符串，用数组可以避免
// "12" 与 12 的转换歧义，也便于前端保持分组顺序。
//
// 故意不写 binding tag：validator 不会自动下钻切片元素（需要 `dive`），
// 写了 tag 反而会让人以为已经在绑定阶段校验过，所以下面逐项显式校验。
type AgentGroupCostItem struct {
	GroupID        int64   `json:"group_id"`
	CostMultiplier float64 `json:"cost_multiplier"`
}

// ReplaceAgentGroupCostsRequest 是全量替换的请求体。
type ReplaceAgentGroupCostsRequest struct {
	GroupCosts []AgentGroupCostItem `json:"group_costs"`
}

// ReplaceGroupCosts 全量替换代理的分组成本折扣。
// PUT /admin/agents/:id/group-costs
func (h *AgentHandler) ReplaceGroupCosts(c *gin.Context) {
	id, ok := parsePositiveIDParam(c, "id")
	if !ok {
		return
	}

	var req ReplaceAgentGroupCostsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	costs := make(map[int64]float64, len(req.GroupCosts))
	for _, item := range req.GroupCosts {
		// gt=0 的手工版：NaN/±Inf 与 0 一律拒绝（NaN 与任何数比较都为 false，
		// 只用 `<= 0` 是拦不住 NaN 的）。
		if item.GroupID <= 0 || !(item.CostMultiplier > 0) || math.IsInf(item.CostMultiplier, 0) {
			response.BadRequest(c, "group_id must be > 0 and cost_multiplier must be a finite number > 0")
			return
		}
		// 同一分组重复出现时后者覆盖前者，而不是报错：前端多选场景下重复项
		// 属于表单瑕疵，最终取值仍应是确定的。
		costs[item.GroupID] = item.CostMultiplier
	}

	if err := h.agentService.ReplaceGroupCosts(c.Request.Context(), id, costs); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{
		"agent_user_id": id,
		"group_costs":   costs,
	})
}

// ListChildren 分页列出代理的下级。
// GET /admin/agents/:id/children
func (h *AgentHandler) ListChildren(c *gin.Context) {
	id, ok := parsePositiveIDParam(c, "id")
	if !ok {
		return
	}
	page, pageSize := response.ParsePagination(c)

	items, total, err := h.agentService.ListChildren(c.Request.Context(), id, page)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Paginated(c, items, int64(total), page, pageSize)
}

// WithdrawRequest 是登记线下提现的请求体。Amount 是已在站外打款给代理的金额。
type WithdrawRequest struct {
	Amount float64 `json:"amount" binding:"required,gt=0"`
	Note   string  `json:"note" binding:"omitempty,max=200"`
}

// Withdraw 登记一笔线下已打款的代理提现。
//
// Idempotency-Key 头必填：同一键重放不会二次扣款，命中已有登记时回
// X-Idempotency-Replayed: true（与邀请返利提现的约定一致）。
// POST /admin/agents/:id/withdraw
func (h *AgentHandler) Withdraw(c *gin.Context) {
	id, ok := parsePositiveIDParam(c, "id")
	if !ok {
		return
	}

	var req WithdrawRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	result, err := h.agentService.WithdrawForAgent(
		c.Request.Context(), id, req.Amount, req.Note, c.GetHeader("Idempotency-Key"))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if result != nil && result.Replayed {
		c.Header("X-Idempotency-Replayed", "true")
	}
	response.Success(c, result)
}

// parseAgentOptionalID 解析可选的 ID 类查询参数。
func parseAgentOptionalID(c *gin.Context, name, raw string) (int64, bool) {
	id, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid "+name)
		return 0, false
	}
	return id, true
}

// parseAgentTimeParam 解析时间过滤参数。
//
// 接受 RFC3339（前端传 ISO 时间）与 YYYY-MM-DD（后台常用的日期选择器）。
// 非法值返回 nil 而不是 400：时间过滤是"缩小范围"的辅助条件，
// 让它把整次查询变成错误只会让管理员在页面上一头雾水。
// endOfDay 为 true 时把纯日期扩展到当天 23:59:59.999999999，
// 否则「到 X 日」会把 X 日整天的数据排除在外。
func parseAgentTimeParam(raw string, endOfDay bool) *time.Time {
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
