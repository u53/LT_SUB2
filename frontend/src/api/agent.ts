/**
 * 用户端「代理中心」API（代理自助）。
 *
 * 全部接口挂在 user 分组下：`/api/v1/user/agent/*`。
 * 作用域由登录态决定 —— 接口不接收 agent_user_id / 代理 ID 入参，
 * 因此前端只需要关心分页与筛选参数，不可能读到或改到别人的佣金与下级。
 */

import { apiClient } from './client'
import type { PaginatedResponse } from '@/types'

/** 佣金流水状态：冻结中 / 可提现 / 已提现 / 已冲正。 */
export type AgentCommissionStatus = 'pending' | 'available' | 'withdrawn' | 'reversed'

/** 提现记录状态：已打款 / 已作废。 */
export type AgentWithdrawalStatus = 'paid' | 'voided'

/**
 * 代理自助档案（GET /user/agent/profile）。
 *
 * 余额三个桶（available / frozen / withdrawn）互不重叠，
 * total_commission 就是三者之和（后端已算好，前端不要再累加）。
 * `min_withdraw` 是最低提现额（设置项缺失时为 0），用于提交前提示。
 */
export interface AgentSelfProfile {
  user_id: number
  aff_code: string
  enabled: boolean
  available: number
  frozen: number
  withdrawn: number
  total_commission: number
  min_withdraw: number
  /** 老版本后端没有该字段，缺失时页面回落到下级列表的 total。 */
  invite_count?: number
  /** null / 缺失表示未设置默认下级折扣，下级按分组公开价计费。 */
  default_child_rate_multiplier?: number | null
  /** 佣金冻结期（小时），仅用于展示。 */
  commission_frozen_hours?: number
  note?: string
}

/**
 * 代理的下级用户及其已配置的售价覆盖（GET /user/agent/children）。
 *
 * `default_rate` 对应「全局默认售价」（group_id 为 NULL 的那一行）；
 * `group_rates` 是分组级覆盖。两者都为空表示该下级仍按分组公开售价计费。
 * 注意：`group_rates` 的键经过 JSON 序列化后是字符串。
 */
export interface AgentChild {
  user_id: number
  email: string
  username: string
  created_at: string
  default_rate?: number | null
  group_rates?: Record<string, number> | null
}

/** 佣金流水行（GET /user/agent/commissions）。金额单位与站点余额一致。 */
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
  /** 用量原始成本（未打折） */
  base_cost: number
  /** 下级售价倍率 */
  child_rate: number
  /** 代理自己的成本倍率 */
  agent_cost_rate: number
  /** 下级实付 */
  child_paid: number
  /** 代理成本 */
  agent_cost: number
  /** 佣金 = 下级实付 - 代理成本 */
  commission: number
  status: AgentCommissionStatus
  frozen_until?: string | null
  created_at: string
}

/** 提现记录行（GET /user/agent/withdrawals）。 */
export interface AgentWithdrawal {
  id: number
  agent_user_id: number
  agent_email: string
  amount: number
  status: AgentWithdrawalStatus
  operation_id: string
  note: string
  created_at: string
  updated_at: string
}

/**
 * 给下级设置售价倍率（PUT /user/agent/children/:id/rate）。
 *
 * `group_id` 为 null / 省略表示该下级的全局默认售价，非空表示只在指定分组覆盖；
 * 优先级是「分组级 > 全局默认 > 分组公开价」。
 * 售价不得低于代理在该分组的成本倍率，否则后端返回 400 并在 message 里给出原因。
 */
export interface SetAgentChildRateRequest {
  group_id?: number | null
  /** 1.0 = 原价，0.6 = 6 折。必须 > 0。 */
  rate_multiplier: number
}

export interface SetAgentChildRateResponse {
  child_user_id: number
  group_id: number | null
  rate: number
}

export interface AgentCommissionQuery {
  page?: number
  page_size?: number
  child_user_id?: number
  status?: AgentCommissionStatus | ''
  /** RFC3339 或 YYYY-MM-DD */
  start_at?: string
  end_at?: string
}

export interface AgentPageQuery {
  page?: number
  page_size?: number
}

export interface AgentWithdrawRequest {
  amount: number
  /** 备注。后端当前未落库该字段，仅为后续扩展保留。 */
  note?: string
}

/** 去掉空值，避免把 `status=` 之类的空参数发给后端。 */
function toParams<T extends object>(query: T): Record<string, unknown> {
  const params: Record<string, unknown> = {}
  for (const [key, value] of Object.entries(query)) {
    if (value === undefined || value === null || value === '') continue
    params[key] = value
  }
  return params
}

/** GET /user/agent/profile —— 当前代理的余额、邀请码与最低提现额。 */
export async function getAgentProfile(): Promise<AgentSelfProfile> {
  const { data } = await apiClient.get<AgentSelfProfile>('/user/agent/profile')
  return data
}

/** GET /user/agent/children —— 分页列出自己的下级。 */
export async function listAgentChildren(
  query: AgentPageQuery = {},
): Promise<PaginatedResponse<AgentChild>> {
  const { data } = await apiClient.get<PaginatedResponse<AgentChild>>('/user/agent/children', {
    params: toParams(query),
  })
  return data
}

/** PUT /user/agent/children/:id/rate —— 给下级设置售价倍率（按分组或全局默认）。 */
export async function setAgentChildRate(
  childUserId: number,
  payload: SetAgentChildRateRequest,
): Promise<SetAgentChildRateResponse> {
  // 后端 handler 绑定的字段名是 `rate`（见 backend/internal/handler/agent_handler.go），
  // 而交接文档写的是 `rate_multiplier`；这里两个都发，任一实现都能取到值，
  // 多余的字段会被 gin 的 ShouldBindJSON 忽略。
  const body = {
    group_id: payload.group_id ?? null,
    rate_multiplier: payload.rate_multiplier,
    rate: payload.rate_multiplier,
  }
  const { data } = await apiClient.put<SetAgentChildRateResponse>(
    `/user/agent/children/${childUserId}/rate`,
    body,
  )
  return data
}

/** GET /user/agent/commissions —— 分页佣金流水，支持按下级 / 状态 / 时间范围筛选。 */
export async function listAgentCommissions(
  query: AgentCommissionQuery = {},
): Promise<PaginatedResponse<AgentCommissionRecord>> {
  const { data } = await apiClient.get<PaginatedResponse<AgentCommissionRecord>>(
    '/user/agent/commissions',
    { params: toParams(query) },
  )
  return data
}

/** GET /user/agent/withdrawals —— 分页列出自己的提现记录。 */
export async function listAgentWithdrawals(
  query: AgentPageQuery = {},
): Promise<PaginatedResponse<AgentWithdrawal>> {
  const { data } = await apiClient.get<PaginatedResponse<AgentWithdrawal>>(
    '/user/agent/withdrawals',
    { params: toParams(query) },
  )
  return data
}

/** POST /user/agent/withdraw —— 申请提现（线下打款，后台登记后到账）。 */
export async function requestAgentWithdraw(
  payload: AgentWithdrawRequest,
): Promise<AgentWithdrawal> {
  const { data } = await apiClient.post<AgentWithdrawal>('/user/agent/withdraw', toParams(payload))
  return data
}

export const agentAPI = {
  getAgentProfile,
  listAgentChildren,
  setAgentChildRate,
  listAgentCommissions,
  listAgentWithdrawals,
  requestAgentWithdraw,
}

export default agentAPI
