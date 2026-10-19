/**
 * 一笔代理提现登记及标识它的幂等键。
 *
 * 与 `views/admin/affiliates/affiliateWithdrawOperation.ts` 同一套做法：
 * 幂等键由「管理员 ID + 时间戳 + 随机串」组成；提交结果未知时（网络中断、超时、5xx）
 * 保留键与金额，再次提交沿用同一键，服务端据此返回首次登记而不重复扣款。
 */
export interface AgentWithdrawOperation {
  agentUserId: number
  amount: number
  key: string
  storageKey: string | null
  outcomeUncertain: boolean
}

/** 佣金按 8 位小数记账（DECIMAL(20,8)），金额比较在同一精度上进行。 */
export const AGENT_LEDGER_SCALE = 1e8

const pendingKeys = new Map<string, string>()

function currentAdminId(): number | null {
  try {
    const user = JSON.parse(globalThis.localStorage?.getItem('auth_user') ?? 'null') as { id?: unknown } | null
    const id = user?.id
    return typeof id === 'number' && Number.isSafeInteger(id) && id > 0 ? id : null
  } catch {
    return null
  }
}

function readStoredKey(storageKey: string): string | null {
  try {
    return globalThis.sessionStorage?.getItem(storageKey) ?? null
  } catch {
    return null
  }
}

function storeKey(storageKey: string, key: string | null) {
  try {
    if (key) globalThis.sessionStorage?.setItem(storageKey, key)
    else globalThis.sessionStorage?.removeItem(storageKey)
  } catch {
    // 浏览器存储不可用时，同一页面内的重试仍由内存中的键保护。
  }
}

/**
 * 取得该代理与金额对应的登记。结果未知的登记在完成前一直保留它的键，
 * 再次提交同一代理与金额时沿用该键，服务端据此返回首次结果而不重复扣减。
 */
export function prepareAgentWithdrawOperation(
  agentUserId: number,
  amount: number,
): AgentWithdrawOperation {
  const normalizedAmount = Math.round(amount * AGENT_LEDGER_SCALE) / AGENT_LEDGER_SCALE
  const adminId = currentAdminId()
  const storageKey = adminId
    ? `sub2api:admin:agent-withdraw:${adminId}:${agentUserId}:${normalizedAmount}`
    : null
  let key = storageKey ? pendingKeys.get(storageKey) ?? readStoredKey(storageKey) : null
  const outcomeUncertain = !!key
  if (!key) {
    // 时间戳 + 随机串：既避免同一毫秒内两次登记撞键，也便于在日志里定位这一笔。
    const random = globalThis.crypto?.randomUUID?.() ?? `${Date.now()}-${Math.random().toString(36).slice(2)}`
    key = `agent-withdraw-${adminId ?? 'unknown'}-${agentUserId}-${random}`
  }
  if (storageKey) {
    pendingKeys.set(storageKey, key)
    storeKey(storageKey, key)
  }
  return { agentUserId, amount: normalizedAmount, key, storageKey, outcomeUncertain }
}

/** 登记有了确定结果后释放它的键。 */
export function completeAgentWithdrawOperation(operation: AgentWithdrawOperation) {
  if (!operation.storageKey) return
  pendingKeys.delete(operation.storageKey)
  storeKey(operation.storageKey, null)
}

/** 格式化金额为至少 2 位小数、最多 8 位小数的定点字符串（用于提示文案）。 */
export function formatAgentAmount(value: number | null | undefined): string {
  const num = Number(value ?? 0)
  if (!Number.isFinite(num)) return '0.00'
  const [intPart, fraction = ''] = num.toFixed(8).replace(/0+$/, '').split('.')
  return `${intPart}.${fraction.padEnd(2, '0')}`
}
