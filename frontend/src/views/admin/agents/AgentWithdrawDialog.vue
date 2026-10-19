<template>
  <BaseDialog
    :show="show"
    :title="t('agents.withdrawals.withdraw.title')"
    width="normal"
    @close="handleClose"
  >
    <form class="space-y-4" @submit.prevent="submit">
      <!-- 代理选择 -->
      <div>
        <label class="input-label">{{ t('agents.withdrawals.withdraw.agent') }}</label>
        <div
          v-if="selectedAgent"
          class="flex items-center justify-between gap-3 rounded-md border border-primary-200 bg-primary-50 px-3 py-2 dark:border-primary-700/50 dark:bg-primary-900/20"
          data-test="agent-withdraw-selected"
        >
          <div class="min-w-0 truncate text-sm">
            <span class="font-mono text-gray-500 dark:text-dark-400">#{{ selectedAgent.user_id }}</span>
            <span class="ml-2 font-medium text-gray-900 dark:text-white">{{ selectedAgent.email || '-' }}</span>
            <span v-if="selectedAgent.username" class="ml-1 text-xs text-gray-500 dark:text-dark-400">
              ({{ selectedAgent.username }})
            </span>
          </div>
          <button
            type="button"
            class="text-lg leading-none text-gray-400 hover:text-red-600 disabled:opacity-50"
            :title="t('agents.withdrawals.withdraw.changeAgent')"
            :disabled="submitting || outcomeUncertain"
            data-test="agent-withdraw-clear"
            @click="clearAgent"
          >
            ×
          </button>
        </div>
        <template v-else>
          <input
            v-model="agentQuery"
            type="text"
            class="input"
            :placeholder="t('agents.withdrawals.withdraw.agentPlaceholder')"
            data-test="agent-withdraw-search"
            @input="onAgentQueryInput"
          />
          <div
            v-if="agentResults.length > 0"
            class="mt-1 max-h-40 overflow-y-auto rounded border border-gray-200 dark:border-dark-700"
          >
            <button
              v-for="agent in agentResults"
              :key="agent.user_id"
              type="button"
              class="w-full px-3 py-1.5 text-left text-sm text-gray-900 hover:bg-gray-100 dark:text-white dark:hover:bg-dark-800"
              data-test="agent-withdraw-option"
              @click="selectAgent(agent)"
            >
              {{ agent.email || `#${agent.user_id}` }}
              <span class="text-xs text-gray-500 dark:text-dark-400">
                ({{ agent.username || `#${agent.user_id}` }} · {{ money(agent.available) }})
              </span>
            </button>
          </div>
          <p v-else-if="searched" class="input-hint">{{ t('agents.withdrawals.withdraw.noAgentFound') }}</p>
        </template>
      </div>

      <!-- 可提现余额 -->
      <div
        v-if="selectedAgent"
        class="rounded-lg border border-gray-100 bg-gray-50 p-3 dark:border-dark-700 dark:bg-dark-800"
      >
        <div class="flex items-center justify-between text-sm">
          <span class="text-gray-500 dark:text-dark-400">{{ t('agents.withdrawals.withdraw.available') }}</span>
          <span class="font-semibold text-gray-900 dark:text-white" data-test="agent-withdraw-available">
            {{ money(availableQuota) }}
          </span>
        </div>
      </div>

      <!-- 金额 -->
      <div>
        <label class="input-label">{{ t('agents.withdrawals.withdraw.amount') }}</label>
        <div class="flex gap-2">
          <input
            v-model="amount"
            type="number"
            min="0"
            step="any"
            inputmode="decimal"
            class="input"
            :disabled="!selectedAgent || submitting || outcomeUncertain"
            data-test="agent-withdraw-amount"
          />
          <button
            type="button"
            class="btn btn-secondary shrink-0"
            :disabled="!selectedAgent || submitting || outcomeUncertain || availableQuota <= 0"
            data-test="agent-withdraw-fill-all"
            @click="fillAll"
          >
            {{ t('agents.withdrawals.withdraw.fillAll') }}
          </button>
        </div>
        <p v-if="amountError" class="input-error-text" data-test="agent-withdraw-amount-error">{{ amountError }}</p>
        <p v-else class="input-hint">{{ t('agents.withdrawals.withdraw.amountHint') }}</p>
      </div>

      <!-- 备注 -->
      <div>
        <label class="input-label">{{ t('agents.withdrawals.withdraw.note') }}</label>
        <textarea
          v-model="note"
          rows="2"
          class="input"
          :disabled="outcomeUncertain"
          :placeholder="t('agents.withdrawals.withdraw.notePlaceholder')"
          data-test="agent-withdraw-note"
        ></textarea>
        <p class="input-hint">{{ t('agents.withdrawals.withdraw.noteHint') }}</p>
      </div>

      <div
        v-if="outcomeUncertain"
        class="rounded-lg border border-blue-200 bg-blue-50 p-3 text-sm text-blue-800 dark:border-blue-700/50 dark:bg-blue-900/20 dark:text-blue-300"
        data-test="agent-withdraw-uncertain-hint"
      >
        {{ t('agents.withdrawals.withdraw.uncertainHint') }}
      </div>

      <div class="rounded-lg border border-amber-200 bg-amber-50 p-3 text-sm text-amber-800 dark:border-amber-700/50 dark:bg-amber-900/20 dark:text-amber-300">
        {{ t('agents.withdrawals.withdraw.warning') }}
      </div>
    </form>

    <template #footer>
      <div class="flex justify-end gap-3">
        <button type="button" class="btn btn-secondary" :disabled="submitting" @click="handleClose">
          {{ t('common.cancel') }}
        </button>
        <button
          type="button"
          class="btn btn-primary"
          :disabled="!canSubmit"
          data-test="agent-withdraw-submit"
          @click="submit"
        >
          {{ submitting ? t('agents.withdrawals.withdraw.submitting') : t('agents.withdrawals.withdraw.submit') }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import { useAppStore } from '@/stores/app'
import { agentsAPI, type AgentListItem, type AgentWithdrawal } from '@/api/admin/agents'
import { extractI18nErrorMessage } from '@/utils/apiError'
import { money } from './agentFormat'
import {
  AGENT_LEDGER_SCALE,
  completeAgentWithdrawOperation,
  formatAgentAmount,
  prepareAgentWithdrawOperation,
  type AgentWithdrawOperation,
} from './agentWithdrawOperation'

const props = defineProps<{
  show: boolean
}>()

const emit = defineEmits<{
  close: []
  success: [result: AgentWithdrawal | null]
}>()

const { t } = useI18n()
const appStore = useAppStore()

const agentQuery = ref('')
const agentResults = ref<AgentListItem[]>([])
const searched = ref(false)
const selectedAgent = ref<AgentListItem | null>(null)
const amount = ref<number | string>('')
const note = ref('')
const submitting = ref(false)
// 已提交、尚未拿到确定结果的登记。结果未知时锁定代理与金额，重试沿用它的幂等键。
const pendingOperation = ref<AgentWithdrawOperation | null>(null)
const outcomeUncertain = computed(() => pendingOperation.value?.outcomeUncertain === true)
const availableQuota = computed(() => Number(selectedAgent.value?.available || 0))

let searchTimer: ReturnType<typeof setTimeout> | null = null
let searchSeq = 0

const amountText = computed(() => String(amount.value ?? '').trim())
const parsedAmount = computed(() => (amountText.value === '' ? Number.NaN : Number(amountText.value)))

const amountError = computed(() => {
  if (outcomeUncertain.value || !selectedAgent.value || amountText.value === '') return ''
  const value = parsedAmount.value
  if (!Number.isFinite(value) || Math.round(value * AGENT_LEDGER_SCALE) <= 0) {
    return t('agents.withdrawals.withdraw.amountRequired')
  }
  if (Math.round(value * AGENT_LEDGER_SCALE) > Math.round(availableQuota.value * AGENT_LEDGER_SCALE)) {
    return t('agents.withdrawals.withdraw.amountExceeds')
  }
  return ''
})

// 结果未知的登记可能已经扣减，重试不受刷新后的可提现余额约束。
const canSubmit = computed(() => {
  if (selectedAgent.value === null || submitting.value) return false
  if (outcomeUncertain.value) return true
  return amountText.value !== '' && amountError.value === ''
})

function showError(error: unknown) {
  appStore.showError(
    extractI18nErrorMessage(error, t, 'agents.errors', t('agents.withdrawals.withdraw.failed')),
  )
}

function clearSearchTimer() {
  if (searchTimer) {
    clearTimeout(searchTimer)
    searchTimer = null
  }
}

function onAgentQueryInput() {
  clearSearchTimer()
  searched.value = false
  const query = agentQuery.value.trim()
  if (!query) {
    searchSeq++
    agentResults.value = []
    return
  }
  searchTimer = setTimeout(() => {
    void searchAgents(query)
  }, 300)
}

async function searchAgents(query: string) {
  const seq = ++searchSeq
  try {
    const res = await agentsAPI.listAgents({ page: 1, page_size: 20, search: query })
    if (seq !== searchSeq) return
    agentResults.value = res.items || []
    searched.value = true
  } catch (error) {
    if (seq !== searchSeq) return
    showError(error)
  }
}

function selectAgent(agent: AgentListItem) {
  clearSearchTimer()
  searchSeq++
  selectedAgent.value = agent
  agentQuery.value = ''
  agentResults.value = []
  searched.value = false
  amount.value = ''
}

function clearAgent() {
  searchSeq++
  selectedAgent.value = null
  amount.value = ''
}

function fillAll() {
  amount.value = Math.round(availableQuota.value * AGENT_LEDGER_SCALE) / AGENT_LEDGER_SCALE
}

function resetForm() {
  clearSearchTimer()
  searchSeq++
  agentQuery.value = ''
  agentResults.value = []
  searched.value = false
  selectedAgent.value = null
  amount.value = ''
  note.value = ''
  submitting.value = false
  pendingOperation.value = null
}

function handleClose() {
  if (submitting.value) return
  emit('close')
}

async function submit() {
  const agent = selectedAgent.value
  if (!agent || !canSubmit.value) return
  if (!pendingOperation.value) {
    pendingOperation.value = prepareAgentWithdrawOperation(agent.user_id, parsedAmount.value)
  }
  const operation = pendingOperation.value
  submitting.value = true
  try {
    const { result, replayed } = await agentsAPI.withdrawAgent(
      operation.agentUserId,
      { amount: operation.amount, note: note.value.trim() || undefined },
      operation.key,
    )
    completeAgentWithdrawOperation(operation)
    pendingOperation.value = null
    // 服务端不回新余额时，用「原余额 - 本次金额」在本地估算剩余量，避免提示里出现 $0。
    const remaining = selectedAgent.value
      ? Math.max(0, availableQuota.value - operation.amount)
      : 0
    appStore.showSuccess(
      t(replayed ? 'agents.withdrawals.withdraw.replayed' : 'agents.withdrawals.withdraw.success', {
        amount: `$${formatAgentAmount(operation.amount)}`,
        remaining: `$${formatAgentAmount(remaining)}`,
      }),
    )
    emit('success', result)
  } catch (error) {
    showError(error)
    // 网络错误、超时、5xx 与 408/409 时服务端可能已经登记：保留这笔登记，
    // 重试沿用同一请求与幂等键，直到拿到确定结果。
    const status = (error as { status?: number } | null)?.status
    if (
      !operation.outcomeUncertain
      && status
      && status >= 400
      && status < 500
      && status !== 408
      && status !== 409
    ) {
      completeAgentWithdrawOperation(operation)
      pendingOperation.value = null
    } else {
      operation.outcomeUncertain = true
    }
  } finally {
    submitting.value = false
  }
}

watch(
  () => props.show,
  (visible) => {
    if (visible) resetForm()
  },
  { immediate: true },
)

onBeforeUnmount(clearSearchTimer)
</script>
