<template>
  <BaseDialog :show="show" :title="t('agentCenter.rateDialog.title')" width="normal" @close="close">
    <form v-if="child" class="space-y-5" @submit.prevent="submit">
      <p class="text-sm text-gray-500 dark:text-dark-400">
        {{ t('agentCenter.rateDialog.subtitle', { email: child.email || child.username || `#${child.user_id}` }) }}
      </p>

      <div v-if="groupOptions.length > 1">
        <label class="input-label" for="agent-rate-group">{{ t('agentCenter.rateDialog.group') }}</label>
        <Select
          id="agent-rate-group"
          :model-value="groupSelection"
          :options="groupOptions"
          :searchable="true"
          @update:model-value="onGroupChange"
        />
        <p class="input-hint">{{ t('agentCenter.rateDialog.groupHint') }}</p>
      </div>

      <div>
        <label class="input-label" for="agent-rate-multiplier">{{ t('agentCenter.rateDialog.multiplier') }}</label>
        <input
          id="agent-rate-multiplier"
          v-model="rateInput"
          type="number"
          step="0.001"
          min="0.001"
          inputmode="decimal"
          autocomplete="off"
          class="input hide-spinner"
          :placeholder="t('agentCenter.rateDialog.multiplierPlaceholder')"
          data-testid="agent-rate-input"
        />
        <p class="input-hint">{{ t('agentCenter.rateDialog.multiplierHint') }}</p>
        <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">{{ currentHint }}</p>
      </div>

      <p class="rounded-xl border border-amber-200 bg-amber-50 px-3 py-2 text-xs text-amber-800 dark:border-amber-900/40 dark:bg-amber-900/20 dark:text-amber-200">
        {{ t('agentCenter.rateDialog.floorHint') }}
      </p>

      <p v-if="localError" class="text-sm text-red-600 dark:text-red-400" data-testid="agent-rate-error">
        {{ localError }}
      </p>

      <div class="flex justify-end gap-3">
        <button type="button" class="btn btn-secondary" :disabled="submitting" @click="close">
          {{ t('agentCenter.actions.cancel') }}
        </button>
        <button type="submit" class="btn btn-primary" :disabled="submitting" data-testid="agent-rate-submit">
          <Icon v-if="submitting" name="refresh" size="sm" class="mr-1 animate-spin" />
          {{ submitting ? t('agentCenter.rateDialog.submitting') : t('agentCenter.rateDialog.submit') }}
        </button>
      </div>
    </form>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Select from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import { setAgentChildRate, type AgentChild } from '@/api/agent'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'
import { hasRate, rateTextParams, toPositiveRate, type AgentRateGroupOption } from './agentCenterUtils'

const props = defineProps<{
  show: boolean
  child: AgentChild | null
  groups: AgentRateGroupOption[]
}>()

const emit = defineEmits<{ (e: 'close'): void; (e: 'saved'): void }>()

const { t } = useI18n()
const appStore = useAppStore()

// 0 表示「全局默认售价」（提交时转换为 group_id: null）。
const ALL_GROUPS = 0

const submitting = ref(false)
const rateInput = ref('')
const groupSelection = ref<number>(ALL_GROUPS)
const localError = ref('')

const groupOptions = computed(() => [
  { value: ALL_GROUPS, label: t('agentCenter.rateDialog.groupAll') },
  ...props.groups.map((group) => ({ value: group.id, label: group.name })),
])

/** 当前选中范围内已配置的倍率（全局默认或分组覆盖）。 */
const selectedRate = computed<number | null>(() => {
  const child = props.child
  if (!child) return null
  if (groupSelection.value === ALL_GROUPS) {
    return typeof child.default_rate === 'number' ? child.default_rate : null
  }
  const raw = child.group_rates?.[String(groupSelection.value)]
  const parsed = typeof raw === 'number' ? raw : Number(raw)
  return Number.isFinite(parsed) && parsed > 0 ? parsed : null
})

const currentHint = computed(() => {
  const rate = selectedRate.value
  if (!hasRate(rate)) return t('agentCenter.rateDialog.currentUnset')
  return t('agentCenter.rateDialog.current', {
    value: t('agentCenter.children.rateDisplay', rateTextParams(rate)),
  })
})

function resetForm(): void {
  localError.value = ''
  rateInput.value = ''
  groupSelection.value = ALL_GROUPS
  const rate = props.child?.default_rate
  if (typeof rate === 'number' && rate > 0) rateInput.value = String(rate)
}

watch(
  () => props.show,
  (visible) => {
    if (visible) resetForm()
  },
  { immediate: true },
)

// 编辑中切换分组时把该分组已配置的倍率带进输入框，避免误改成别的分组的价。
function onGroupChange(value: unknown): void {
  const parsed = Number(value)
  groupSelection.value = Number.isFinite(parsed) ? parsed : ALL_GROUPS
  const rate = selectedRate.value
  rateInput.value = rate === null ? '' : String(rate)
}

function close(): void {
  if (submitting.value) return
  emit('close')
}

async function submit(): Promise<void> {
  if (!props.child || submitting.value) return
  const parsed = Number(rateInput.value)
  if (toPositiveRate(parsed) === null) {
    localError.value = t('agentCenter.rateDialog.invalidRate')
    return
  }

  submitting.value = true
  localError.value = ''
  try {
    await setAgentChildRate(props.child.user_id, {
      group_id: groupSelection.value === ALL_GROUPS ? null : groupSelection.value,
      rate_multiplier: parsed,
    })
    appStore.showSuccess(t('agentCenter.rateDialog.success'))
    emit('saved')
    emit('close')
  } catch (error) {
    // 成本下限等业务校验由后端给出具体原因（含中文说明），原样展示。
    // 同时弹一次 toast，避免长文案在弹窗里被忽略。
    const message = extractApiErrorMessage(error, t('agentCenter.errors.unknown'))
    localError.value = message
    appStore.showError(message)
  } finally {
    submitting.value = false
  }
}
</script>

<style scoped>
/* 隐藏数字输入框的箭头按钮，避免误触 0.001 的步进 */
.hide-spinner::-webkit-outer-spin-button,
.hide-spinner::-webkit-inner-spin-button {
  -webkit-appearance: none;
  margin: 0;
}
.hide-spinner {
  -moz-appearance: textfield;
}
</style>
