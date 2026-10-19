<template>
  <div class="card p-6" data-testid="agent-stats">
    <div class="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
      <div>
        <h3 class="text-base font-semibold text-gray-900 dark:text-white">
          {{ t('agentCenter.stats.title') }}
        </h3>
        <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">
          {{ t('agentCenter.stats.subtitle') }}
        </p>
      </div>
      <button
        type="button"
        class="btn btn-primary shrink-0"
        :disabled="!canWithdraw"
        data-testid="agent-withdraw-button"
        @click="emit('withdraw')"
      >
        <Icon name="dollar" size="sm" class="mr-1" />
        {{ t('agentCenter.withdraw.button') }}
      </button>
    </div>

    <div class="mt-5 grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
      <div class="rounded-xl border border-emerald-200 bg-emerald-50 p-4 dark:border-emerald-900/40 dark:bg-emerald-900/20">
        <p class="text-xs font-medium text-emerald-800 dark:text-emerald-300">
          {{ t('agentCenter.stats.available') }}
        </p>
        <p class="mt-1 text-2xl font-semibold text-emerald-600 dark:text-emerald-400" data-testid="agent-available">
          ${{ formatAmount(available) }}
        </p>
        <p class="mt-1 text-xs text-emerald-700/80 dark:text-emerald-300/80">
          {{ t('agentCenter.stats.availableHint') }}
        </p>
      </div>

      <div class="rounded-xl border border-amber-200 bg-amber-50 p-4 dark:border-amber-900/40 dark:bg-amber-900/20">
        <p class="text-xs font-medium text-amber-800 dark:text-amber-300" :title="t('agentCenter.stats.frozenHint')">
          {{ t('agentCenter.stats.frozen') }}
        </p>
        <p class="mt-1 text-2xl font-semibold text-amber-600 dark:text-amber-400" data-testid="agent-frozen">
          ${{ formatAmount(profile?.frozen) }}
        </p>
        <p class="mt-1 text-xs text-amber-700/80 dark:text-amber-300/80">
          {{ freezeHint }}
        </p>
      </div>

      <div class="rounded-xl border border-gray-200 p-4 dark:border-dark-600">
        <p class="text-xs font-medium text-gray-500 dark:text-dark-400">
          {{ t('agentCenter.stats.total') }}
        </p>
        <p class="mt-1 text-2xl font-semibold text-gray-900 dark:text-white" data-testid="agent-total">
          ${{ formatAmount(profile?.total_commission) }}
        </p>
        <p class="mt-1 text-xs text-gray-400 dark:text-dark-500">
          {{ t('agentCenter.stats.totalHint') }}
        </p>
      </div>

      <div class="rounded-xl border border-gray-200 p-4 dark:border-dark-600">
        <p class="text-xs font-medium text-gray-500 dark:text-dark-400">
          {{ t('agentCenter.stats.withdrawn') }}
        </p>
        <p class="mt-1 text-2xl font-semibold text-gray-900 dark:text-white" data-testid="agent-withdrawn">
          ${{ formatAmount(profile?.withdrawn) }}
        </p>
        <p class="mt-1 text-xs text-gray-400 dark:text-dark-500">
          {{ t('agentCenter.stats.withdrawnHint') }}
        </p>
      </div>

      <div class="rounded-xl border border-gray-200 p-4 dark:border-dark-600">
        <p class="text-xs font-medium text-gray-500 dark:text-dark-400">
          {{ t('agentCenter.stats.inviteCount') }}
        </p>
        <p class="mt-1 text-2xl font-semibold text-gray-900 dark:text-white" data-testid="agent-invite-count">
          {{ inviteCount }}
        </p>
        <p class="mt-1 text-xs text-gray-400 dark:text-dark-500">
          {{ t('agentCenter.stats.inviteCountHint') }}
        </p>
      </div>

      <div class="rounded-xl border border-gray-200 p-4 dark:border-dark-600">
        <p class="text-xs font-medium text-gray-500 dark:text-dark-400">
          {{ t('agentCenter.stats.minWithdraw') }}
        </p>
        <p class="mt-1 text-2xl font-semibold text-gray-900 dark:text-white" data-testid="agent-min-withdraw">
          ${{ formatAmount(minWithdraw) }}
        </p>
        <p class="mt-1 text-xs text-gray-400 dark:text-dark-500">
          {{ t('agentCenter.withdraw.minWithdraw', { amount: `$${formatAmount(minWithdraw)}` }) }}
        </p>
      </div>
    </div>

    <p
      v-if="profile && !profile.enabled"
      class="mt-4 rounded-xl border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-800 dark:border-amber-900/40 dark:bg-amber-900/20 dark:text-amber-200"
      data-testid="agent-disabled-notice"
    >
      {{ t('agentCenter.stats.disabledNotice') }}
    </p>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import type { AgentSelfProfile } from '@/api/agent'
import { formatAmount } from './agentCenterUtils'

const props = defineProps<{
  profile: AgentSelfProfile | null
  inviteCount: number
}>()

const emit = defineEmits<{ (e: 'withdraw'): void }>()

const { t } = useI18n()

const available = computed(() => props.profile?.available ?? 0)
const minWithdraw = computed(() => props.profile?.min_withdraw ?? 0)

// 余额不足最低提现额时按钮不可点，避免用户提交必被拒的请求。
const canWithdraw = computed(
  () => available.value > 0 && available.value >= minWithdraw.value,
)

const freezeHint = computed(() => {
  const hours = props.profile?.commission_frozen_hours ?? 0
  return hours > 0
    ? t('agentCenter.stats.freezeHours', { hours })
    : t('agentCenter.stats.freezeHoursNone')
})
</script>
