<template>
  <BaseDialog :show="show" :title="t('agentCenter.withdraw.title')" width="normal" @close="close">
    <form class="space-y-5" @submit.prevent="submit">
      <p class="text-sm text-gray-500 dark:text-dark-400">
        {{ t('agentCenter.withdraw.description') }}
      </p>

      <div class="rounded-xl bg-gray-50 p-4 dark:bg-dark-800">
        <div class="flex justify-between text-sm">
          <span class="text-gray-500 dark:text-gray-400">{{ t('agentCenter.stats.available') }}</span>
          <span class="font-medium text-emerald-600 dark:text-emerald-400">${{ formatAmount(available) }}</span>
        </div>
        <div class="mt-2 flex justify-between text-sm">
          <span class="text-gray-500 dark:text-gray-400">{{ t('agentCenter.stats.minWithdraw') }}</span>
          <span class="font-medium text-gray-900 dark:text-white">${{ formatAmount(minWithdraw) }}</span>
        </div>
      </div>

      <div>
        <label class="input-label" for="agent-withdraw-amount">{{ t('agentCenter.withdraw.amount') }}</label>
        <input
          id="agent-withdraw-amount"
          v-model="amountInput"
          type="number"
          step="0.0001"
          min="0"
          inputmode="decimal"
          autocomplete="off"
          class="input hide-spinner"
          :placeholder="t('agentCenter.withdraw.amountPlaceholder')"
          data-testid="agent-withdraw-amount"
        />
        <div class="mt-1 flex items-center justify-between gap-2">
          <p class="text-xs text-gray-500 dark:text-dark-400">
            {{ t('agentCenter.withdraw.minWithdraw', { amount: `$${formatAmount(minWithdraw)}` }) }}
          </p>
          <button
            type="button"
            class="text-xs font-medium text-primary-600 hover:underline dark:text-primary-400"
            @click="fillMax"
          >
            {{ t('agentCenter.withdraw.fillMax') }}
          </button>
        </div>
      </div>

      <div>
        <label class="input-label" for="agent-withdraw-note">{{ t('agentCenter.withdraw.note') }}</label>
        <textarea
          id="agent-withdraw-note"
          v-model="noteInput"
          rows="2"
          maxlength="200"
          class="input"
          :placeholder="t('agentCenter.withdraw.notePlaceholder')"
        />
      </div>

      <p v-if="localError" class="text-sm text-red-600 dark:text-red-400" data-testid="agent-withdraw-error">
        {{ localError }}
      </p>

      <div class="flex justify-end gap-3">
        <button type="button" class="btn btn-secondary" :disabled="submitting" @click="close">
          {{ t('agentCenter.actions.cancel') }}
        </button>
        <button type="submit" class="btn btn-primary" :disabled="submitting" data-testid="agent-withdraw-submit">
          <Icon v-if="submitting" name="refresh" size="sm" class="mr-1 animate-spin" />
          {{ submitting ? t('agentCenter.withdraw.submitting') : t('agentCenter.withdraw.submit') }}
        </button>
      </div>
    </form>
  </BaseDialog>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import { requestAgentWithdraw } from '@/api/agent'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'
import { formatAmount } from './agentCenterUtils'

const props = defineProps<{
  show: boolean
  available: number
  minWithdraw: number
}>()

const emit = defineEmits<{ (e: 'close'): void; (e: 'submitted'): void }>()

const { t } = useI18n()
const appStore = useAppStore()

const amountInput = ref('')
const noteInput = ref('')
const localError = ref('')
const submitting = ref(false)

watch(
  () => props.show,
  (visible) => {
    if (!visible) return
    amountInput.value = ''
    noteInput.value = ''
    localError.value = ''
  },
  { immediate: true },
)

function fillMax(): void {
  if (props.available <= 0) return
  // 按 8 位小数取整，避免浮点噪声导致「刚好等于余额」被判超额。
  amountInput.value = String(Math.floor(props.available * 1e8) / 1e8)
}

function close(): void {
  if (submitting.value) return
  emit('close')
}

async function submit(): Promise<void> {
  if (submitting.value) return

  const raw = String(amountInput.value).trim()
  if (!raw) {
    localError.value = t('agentCenter.withdraw.error.required')
    return
  }
  const amount = Number(raw)
  if (!Number.isFinite(amount) || amount <= 0) {
    localError.value = t('agentCenter.withdraw.error.invalid')
    return
  }
  if (props.minWithdraw > 0 && amount < props.minWithdraw) {
    localError.value = t('agentCenter.withdraw.error.belowMinimum', {
      amount: `$${formatAmount(props.minWithdraw)}`,
    })
    return
  }
  if (amount > props.available) {
    localError.value = t('agentCenter.withdraw.error.insufficient', {
      amount: `$${formatAmount(props.available)}`,
    })
    return
  }

  submitting.value = true
  localError.value = ''
  try {
    const note = noteInput.value.trim()
    await requestAgentWithdraw(note ? { amount, note } : { amount })
    appStore.showSuccess(t('agentCenter.withdraw.success', { amount: `$${formatAmount(amount)}` }))
    emit('submitted')
    emit('close')
  } catch (error) {
    // 余额 / 最低额等最终校验在服务端，错误文案原样透出。
    const message = extractApiErrorMessage(error, t('agentCenter.errors.unknown'))
    localError.value = message
    appStore.showError(message)
  } finally {
    submitting.value = false
  }
}
</script>

<style scoped>
.hide-spinner::-webkit-outer-spin-button,
.hide-spinner::-webkit-inner-spin-button {
  -webkit-appearance: none;
  margin: 0;
}
.hide-spinner {
  -moz-appearance: textfield;
}
</style>
