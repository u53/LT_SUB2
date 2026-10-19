/**
 * Admin Agents API endpoints（代理佣金模块 · 管理端）
 *
 * 覆盖代理档案、分组成本折扣、下级列表、佣金流水与提现登记：
 *   GET  /admin/agents
 *   GET  /admin/agents/:id
 *   PUT  /admin/agents/:id
 *   GET  /admin/agents/:id/group-costs
 *   PUT  /admin/agents/:id/group-costs
 *   GET  /admin/agents/:id/children
 *   GET  /admin/agents/commissions
 *   GET  /admin/agents/withdrawals
 *   POST /admin/agents/:id/withdraw
 *
 * 与服务端约定（见 backend/internal/handler/admin/agent_handler.go）：
 *   - 分页响应统一是 { items, total, page, page_size, pages }；
 *   - 金额一律是 USD 数值，佣金可能小到 1e-8，展示层自行决定保留几位；
 *   - 提现登记必须带 Idempotency-Key，重放命中时服务端回 X-Idempotency-Replayed: true。
 */

import { apiClient } from '../client'
import type {
  AgentDetail,
  AgentGroupCostItem,
  AgentListItem,
  PaginatedResponse,
  UpdateAgentRequest,
} from '@/types'

// 复用 @/types 里已有的代理类型（同一份定义同时服务用户管理弹窗），
// 只在本地补充管理端新增的接口类型，避免两份定义各自漂移。
export type { AgentDetail, AgentGroupCostItem, AgentListItem }

/** 佣金流水状态：冻结中 / 可提现 / 已提现 / 已冲正。 */
export type AgentCommissionStatus = 'pending' | 'available' | 'withdrawn' | 'reversed'

/** 提现记录状态：已打款 / 已作废。 */
export type AgentWithdrawalStatus = 'paid' | 'voided'

/**
 * 保存代理档案（PUT /admin/agents/:id）。
 *
 * 四个字段都是「显式覆盖」语义：提交的值就是表单最终状态，
 * `default_child_rate_multiplier` 传 null 表示清除默认折扣。
 */
export type UpdateAgentPayload = UpdateAgentRequest

/** 分组成本折扣的读取 / 写入结果。 */
export interface AgentGroupCosts {
  agent_user_id: number
  group_costs: Record<string, number>
}

/**
 * 代理的下级用户（GET /admin/agents/:id/children）。
 *
 * `default_rate` 对应 group_id 为 NULL 的全局默认行，`group_rates` 是分组级覆盖；
 * 两者都为空表示该下级仍按分组公开售价计费。
 */
export interface AgentChild {
  user_id: number
  email: string
  username: string
  created_at: string
  default_rate?: number | null
  group_rates?: Record<string, number> | null
}

/** 佣金流水行（GET /admin/agents/commissions）。 */
export interface AgentCommissionRecord {
  id: number
  agent_user_id: number
  agent_email: string
  child_user_id?: number | null
  child_email: string
  request_id: string
  api_key_id: number
  group_id?: number | null
  model: string
  /** 用量原始成本（未打折）。 */
  base_cost: number
  /** 下级售价倍率。 */
  child_rate: number
  /** 代理自己的成本倍率。 */
  agent_cost_rate: number
  /** 下级实付。 */
  child_paid: number
  /** 代理成本。 */
  agent_cost: number
  /** 佣金 = 下级实付 - 代理成本。 */
  commission: number
  status: AgentCommissionStatus
  frozen_until?: string | null
  created_at: string
}

/** 提现记录（GET /admin/agents/withdrawals）。 */
export interface AgentWithdrawal {
  id: number
  agent_user_id: number
  agent_email: string
  amount: number
  status: AgentWithdrawalStatus
  /** 幂等键在库内的哈希，展示给管理员对账时使用。 */
  operation_id: string
  note: string
  created_at: string
  updated_at: string
}

export interface ListAgentsParams {
  page?: number
  page_size?: number
  search?: string
  /** null / undefined 表示不按启用状态过滤（后端列出全部，含已停用）。 */
  enabled?: boolean | null
}

export interface ListCommissionsParams {
  page?: number
  page_size?: number
  agent_user_id?: number | null
  child_user_id?: number | null
  status?: AgentCommissionStatus | ''
  /** RFC3339 或 YYYY-MM-DD。 */
  start_at?: string
  end_at?: string
}

export interface ListWithdrawalsParams {
  page?: number
  page_size?: number
  agent_user_id?: number | null
  status?: AgentWithdrawalStatus | ''
  /** RFC3339 或 YYYY-MM-DD。 */
  start_at?: string
  end_at?: string
}

/** 登记线下提现（POST /admin/agents/:id/withdraw）。 */
export interface AgentWithdrawPayload {
  /** 已在站外打款给该代理的金额（USD）。 */
  amount: number
  note?: string
}

export interface AgentWithdrawResponse {
  result: AgentWithdrawal | null
  /** true 表示这个幂等键之前已经登记过，本次没有二次扣款。 */
  replayed: boolean
}

/** 去掉空值，避免把 `status=` / `agent_user_id=` 之类的空参数发给后端。 */
function compactParams(params: Record<string, unknown>): Record<string, unknown> {
  const out: Record<string, unknown> = {}
  for (const [key, value] of Object.entries(params)) {
    if (value === undefined || value === null || value === '') continue
    out[key] = value
  }
  return out
}

/** GET /admin/agents —— 分页列出代理（含下级数量与佣金账户余额）。 */
export async function listAgents(
  params: ListAgentsParams = {},
): Promise<PaginatedResponse<AgentListItem>> {
  const { data } = await apiClient.get<PaginatedResponse<AgentListItem>>('/admin/agents', {
    params: compactParams({
      page: params.page ?? 1,
      page_size: params.page_size ?? 20,
      search: params.search?.trim() || undefined,
      enabled: typeof params.enabled === 'boolean' ? params.enabled : undefined,
    }),
  })
  return data
}

/** GET /admin/agents/:id —— 读取代理详情（档案 + 账户余额 + 分组成本折扣）。 */
export async function getAgent(agentUserId: number): Promise<AgentDetail> {
  const { data } = await apiClient.get<AgentDetail>(`/admin/agents/${agentUserId}`)
  return data
}

/** PUT /admin/agents/:id —— 保存代理档案（启用、默认下级折扣、冻结期、备注）。 */
export async function updateAgent(
  agentUserId: number,
  payload: UpdateAgentPayload,
): Promise<{ user_id: number }> {
  const { data } = await apiClient.put<{ user_id: number }>(
    `/admin/agents/${agentUserId}`,
    payload,
  )
  return data
}

/** GET /admin/agents/:id/group-costs —— 读取代理的分组成本折扣。 */
export async function getGroupCosts(agentUserId: number): Promise<AgentGroupCosts> {
  const { data } = await apiClient.get<AgentGroupCosts>(
    `/admin/agents/${agentUserId}/group-costs`,
  )
  return data
}

/**
 * PUT /admin/agents/:id/group-costs —— 全量替换分组成本折扣。
 *
 * 注意这是全量替换：没提交的分组等于「未配置」，所以调用方必须提交表单的完整状态，
 * 不能只提交变更项。
 */
export async function replaceGroupCosts(
  agentUserId: number,
  items: AgentGroupCostItem[],
): Promise<AgentGroupCosts> {
  const { data } = await apiClient.put<AgentGroupCosts>(
    `/admin/agents/${agentUserId}/group-costs`,
    { group_costs: items },
  )
  return data
}

/** GET /admin/agents/:id/children —— 分页列出代理的下级。 */
export async function listChildren(
  agentUserId: number,
  params: { page?: number; page_size?: number } = {},
): Promise<PaginatedResponse<AgentChild>> {
  const { data } = await apiClient.get<PaginatedResponse<AgentChild>>(
    `/admin/agents/${agentUserId}/children`,
    {
      params: compactParams({
        page: params.page ?? 1,
        page_size: params.page_size ?? 20,
      }),
    },
  )
  return data
}

/** GET /admin/agents/commissions —— 分页佣金流水，支持按代理 / 下级 / 状态 / 时间筛选。 */
export async function listCommissions(
  params: ListCommissionsParams = {},
): Promise<PaginatedResponse<AgentCommissionRecord>> {
  const { data } = await apiClient.get<PaginatedResponse<AgentCommissionRecord>>(
    '/admin/agents/commissions',
    {
      params: compactParams({
        page: params.page ?? 1,
        page_size: params.page_size ?? 20,
        agent_user_id: params.agent_user_id,
        child_user_id: params.child_user_id,
        status: params.status,
        start_at: params.start_at,
        end_at: params.end_at,
      }),
    },
  )
  return data
}

/** GET /admin/agents/withdrawals —— 分页提现登记记录。 */
export async function listWithdrawals(
  params: ListWithdrawalsParams = {},
): Promise<PaginatedResponse<AgentWithdrawal>> {
  const { data } = await apiClient.get<PaginatedResponse<AgentWithdrawal>>(
    '/admin/agents/withdrawals',
    {
      params: compactParams({
        page: params.page ?? 1,
        page_size: params.page_size ?? 20,
        agent_user_id: params.agent_user_id,
        status: params.status,
        start_at: params.start_at,
        end_at: params.end_at,
      }),
    },
  )
  return data
}

/**
 * POST /admin/agents/:id/withdraw —— 登记一笔线下已打款的代理提现。
 *
 * `idempotencyKey` 标识一笔登记：同一键重放不会二次扣款，服务端回
 * X-Idempotency-Replayed: true 并带上首次的登记结果。
 */
export async function withdrawAgent(
  agentUserId: number,
  payload: AgentWithdrawPayload,
  idempotencyKey: string,
): Promise<AgentWithdrawResponse> {
  const response = await apiClient.post<AgentWithdrawal>(
    `/admin/agents/${agentUserId}/withdraw`,
    compactParams({
      amount: payload.amount,
      note: payload.note,
    }),
    { headers: { 'Idempotency-Key': idempotencyKey } },
  )
  return {
    result: response.data ?? null,
    replayed: response.headers?.['x-idempotency-replayed'] === 'true',
  }
}

export const agentsAPI = {
  listAgents,
  getAgent,
  updateAgent,
  getGroupCosts,
  replaceGroupCosts,
  listChildren,
  listCommissions,
  listWithdrawals,
  withdrawAgent,
}

export default agentsAPI
