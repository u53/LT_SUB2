<template>
  <div class="card p-6" data-testid="agent-withdrawals">
    <div class="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
      <div>
        <h3 class="text-base font-semibold text-gray-900 dark:text-white">
          {{ t('agentCenter.withdrawals.title') }}
        </h3>
        <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">
          {{ t('agentCenter.withdrawals.description') }}
        </p>
      </div>
      <button
        type="button"
        class="btn btn-secondary shrink-0"
        :disabled="loading"
        :title="t('agentCenter.refresh')"
        @click="emit('refresh')"
      >
        <Icon name="refresh" size="md" :class="loading ? 'animate-spin' : ''" />
      </button>
    </div>

    <div v-if="loading && items.length === 0" class="mt-5 space-y-3">
      <div v-for="i in 3" :key="i" class="h-12 animate-pulse rounded-xl bg-gray-100 dark:bg-dark-700"></div>
    </div>

    <div
      v-else-if="items.length === 0"
      class="mt-5 rounded-xl border border-dashed border-gray-300 p-6 text-center text-sm text-gray-500 dark:border-dark-700 dark:text-dark-400"
    >
      {{ t('agentCenter.withdrawals.empty') }}
    </div>

    <div v-else class="mt-5 overflow-x-auto">
      <table class="w-full min-w-[640px] text-left text-sm">
        <thead>
          <tr class="border-b border-gray-200 text-gray-500 dark:border-dark-700 dark:text-dark-400">
            <th class="px-3 py-2 font-medium">{{ t('agentCenter.withdrawals.columns.time') }}</th>
            <th class="px-3 py-2 text-right font-medium">{{ t('agentCenter.withdrawals.columns.amount') }}</th>
            <th class="px-3 py-2 font-medium">{{ t('agentCenter.withdrawals.columns.status') }}</th>
            <th class="px-3 py-2 font-medium">{{ t('agentCenter.withdrawals.columns.note') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="item in items"
            :key="item.id"
            class="border-b border-gray-100 last:border-b-0 dark:border-dark-800"
            data-testid="agent-withdrawal-row"
          >
            <td class="px-3 py-3 whitespace-nowrap text-gray-700 dark:text-gray-300">
              {{ formatDateTime(item.created_at) || '-' }}
            </td>
            <td class="px-3 py-3 text-right font-medium tabular-nums text-gray-900 dark:text-white">
              ${{ formatAmount(item.amount) }}
            </td>
            <td class="px-3 py-3">
              <span :class="withdrawalStatusClass(item.status)" :data-status="item.status">
                {{ statusLabel(item.status) }}
              </span>
            </td>
            <td class="px-3 py-3 text-gray-500 dark:text-dark-400">{{ item.note || t('agentCenter.withdrawals.noNote') }}</td>
          </tr>
        </tbody>
      </table>
    </div>

    <Pagination
      v-if="total > pageSize"
      class="mt-4"
      :page="page"
      :total="total"
      :page-size="pageSize"
      :show-page-size-selector="false"
      @update:page="onPageChange"
    />
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import Pagination from '@/components/common/Pagination.vue'
import type { AgentWithdrawal, AgentWithdrawalStatus } from '@/api/agent'
import { formatDateTime } from '@/utils/format'
import { formatAmount, withdrawalStatusClass } from './agentCenterUtils'

defineProps<{
  items: AgentWithdrawal[]
  total: number
  page: number
  pageSize: number
  loading: boolean
}>()

const emit = defineEmits<{
  (e: 'update:page', page: number): void
  (e: 'refresh'): void
}>()

const { t } = useI18n()

const STATUS_VALUES: AgentWithdrawalStatus[] = ['paid', 'voided']

function statusLabel(status: AgentWithdrawalStatus | string): string {
  if (STATUS_VALUES.includes(status as AgentWithdrawalStatus)) {
    return t(`agentCenter.withdrawals.status.${status}`)
  }
  return String(status)
}

function onPageChange(page: number): void {
  emit('update:page', page)
}
</script>
