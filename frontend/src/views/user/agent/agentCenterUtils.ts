/**
 * 「代理中心」页面的展示辅助函数。
 *
 * 金额统一 4 位小数（与站点余额展示一致）。售价倍率既不直接展示原值，
 * 也不在调用处判断语言：
 *   - 中文说「6.00 折」= 倍率 × 10
 *   - 英文说「60.00% of list price」= 倍率 × 100
 * 因此 {@link rateTextParams} 一次给出两个占位符，两份 locale 文案各取所需。
 */

import type { AgentCommissionStatus, AgentWithdrawalStatus } from '@/api/agent'

/** 按分组设置折扣时使用的下拉项（分组来自 /groups/available）。 */
export interface AgentRateGroupOption {
  id: number
  name: string
}

/** 金额：固定 4 位小数，非法值回落为 0。 */
export function formatAmount(value: number | null | undefined): string {
  const n = typeof value === 'number' && Number.isFinite(value) ? value : 0
  return n.toFixed(4)
}

/** 倍率规整：只保留有限正数，其余返回 null。 */
export function toPositiveRate(multiplier: number | null | undefined): number | null {
  if (multiplier === null || multiplier === undefined) return null
  const n = typeof multiplier === 'number' ? multiplier : Number(multiplier)
  if (!Number.isFinite(n) || n <= 0) return null
  return n
}

/** 是否有可展示的折扣（模板里用来切换「X 折」与「未设置」）。 */
export function hasRate(multiplier: number | null | undefined): boolean {
  return toPositiveRate(multiplier) !== null
}

/**
 * 折扣文案的插值参数：`value`（×10，中文「折」）与 `percent`（×100，英文百分比）。
 * 无有效倍率时返回空对象（配合 hasRate 使用，不会渲染出来）。
 */
export function rateTextParams(
  multiplier: number | null | undefined,
): Record<string, string> {
  const n = toPositiveRate(multiplier)
  if (n === null) return {}
  return { value: (n * 10).toFixed(2), percent: (n * 100).toFixed(2) }
}

/** 佣金流水状态徽章样式。 */
export function commissionStatusClass(status: AgentCommissionStatus | string): string {
  switch (status) {
    case 'pending':
      return 'badge badge-warning'
    case 'available':
      return 'badge badge-success'
    case 'withdrawn':
      return 'badge badge-gray'
    case 'reversed':
      return 'badge badge-danger'
    default:
      return 'badge badge-gray'
  }
}

/** 提现记录状态徽章样式。 */
export function withdrawalStatusClass(status: AgentWithdrawalStatus | string): string {
  switch (status) {
    case 'paid':
      return 'badge badge-success'
    case 'voided':
      return 'badge badge-danger'
    default:
      return 'badge badge-gray'
  }
}

/**
 * 把 `group_rates`（JSON 里键是字符串）规整成数组，便于展示与统计。
 * 非数字键、非正数倍率一律丢弃。
 */
export function normalizeGroupRates(
  raw?: Record<string, number> | null,
): Array<{ groupId: number; rate: number }> {
  const rows: Array<{ groupId: number; rate: number }> = []
  for (const [key, value] of Object.entries(raw ?? {})) {
    const groupId = Number(key)
    const rate = Number(value)
    if (!Number.isFinite(groupId) || groupId <= 0) continue
    if (!Number.isFinite(rate) || rate <= 0) continue
    rows.push({ groupId, rate })
  }
  return rows.sort((a, b) => a.groupId - b.groupId)
}
