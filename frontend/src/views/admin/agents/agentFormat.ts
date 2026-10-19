/**
 * 代理管理页共用的展示格式化。
 *
 * 佣金可能小到 1e-8（库里是 DECIMAL(20,8)），所以金额统一保留 4 位小数；
 * 倍率统一换算成「折」展示（rate × 10），比裸小数更容易让管理员判断折扣力度。
 */

import { formatDateTime as formatDisplayDateTime } from '@/utils/format'

type TranslateFn = (key: string, params?: Record<string, unknown>) => string

/** 金额 → 定点字符串（4 位小数，不带货币符号）。 */
export function decimal4(value: number | null | undefined): string {
  const num = Number(value ?? 0)
  return Number.isFinite(num) ? num.toFixed(4) : '0.0000'
}

/** 金额 → `$1.2345`。 */
export function money(value: number | null | undefined): string {
  return `$${decimal4(value)}`
}

/** 倍率 → 「5.00 折」中的数值部分（rate × 10）。 */
export function tenfold(value: number | null | undefined): string {
  const num = Number(value ?? 0)
  return Number.isFinite(num) ? (num * 10).toFixed(2) : '0.00'
}

/**
 * 倍率 → 可读折扣文案（「5.00 折」/「5.00 off」）。
 * null / undefined / 非有限数返回 '-'，用于「未配置」的场景。
 */
export function rateLabel(t: TranslateFn, value: number | null | undefined): string {
  if (value === null || value === undefined) return '-'
  const num = Number(value)
  if (!Number.isFinite(num)) return '-'
  return t('agents.multiplier.value', { value: tenfold(num) })
}

/** 时间文案；空值返回 '-'。 */
export function dateTimeLabel(value: string | null | undefined): string {
  return value ? formatDisplayDateTime(value) : '-'
}
