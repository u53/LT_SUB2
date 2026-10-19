<template>
  <div class="card p-6" data-testid="agent-commissions">
    <div class="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
      <div>
        <h3 class="text-base font-semibold text-gray-900 dark:text-white">
          {{ t('agentCenter.commissions.title') }}
        </h3>
        <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">
          {{ t('agentCenter.commissions.description') }}
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

    <div class="mt-5 flex flex-wrap items-end gap-3">
      <div class="w-full sm:w-52">
        <label class="input-label" for="agent-filter-child">{{ t('agentCenter.commissions.filters.child') }}</label>
        <Select
          id="agent-filter-child"
          :model-value="childFilter"
          :options="childFilterOptions"
          @update:model-value="onChildFilterChange"
        />
      </div>
      <div class="w-full sm:w-40">
        <label class="input-label" for="agent-filter-status">{{ t('agentCenter.commissions.filters.status') }}</label>
        <Select
          id="agent-filter-status"
          :model-value="statusFilter"
          :options="statusFilterOptions"
          @update:model-value="onStatusFilterChange"
        />
      </div>
      <div>
        <label class="input-label" for="agent-filter-start">{{ t('agentCenter.commissions.filters.startAt') }}</label>
        <input
          id="agent-filter-start"
          :value="startAt"
          type="date"
          class="input w-full sm:w-40"
          :max="endAt || undefined"
          @change="onStartChange"
        />
      </div>
      <div>
        <label class="input-label" for="agent-filter-end">{{ t('agentCenter.commissions.filters.endAt') }}</label>
        <input
          id="agent-filter-end"
          :value="endAt"
          type="date"
          class="input w-full sm:w-40"
          :min="startAt || undefined"
          @change="onEndChange"
        />
      </div>
      <button
        v-if="hasFilters"
        type="button"
        class="btn btn-ghost btn-sm"
        data-testid="agent-filter-clear"
        @click="emit('clear-filters')"
      >
        <Icon name="x" size="sm" />
        <span>{{ t('agentCenter.commissions.filters.clear') }}</span>
      </button>
    </div>

    <div v-if="loading && items.length === 0" class="mt-5 space-y-3">
      <div v-for="i in 4" :key="i" class="h-12 animate-pulse rounded-xl bg-gray-100 dark:bg-dark-700"></div>
    </div>

    <div
      v-else-if="items.length === 0"
      class="mt-5 rounded-xl border border-dashed border-gray-300 p-6 text-center dark:border-dark-700"
    >
      <p class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('agentCenter.commissions.empty') }}</p>
      <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">{{ t('agentCenter.commissions.emptyHint') }}</p>
    </div>

    <div v-else class="mt-5 overflow-x-auto">
      <table class="w-full min-w-[860px] text-left text-sm">
        <thead>
          <tr class="border-b border-gray-200 text-gray-500 dark:border-dark-700 dark:text-dark-400">
            <th class="px-3 py-2 font-medium">{{ t('agentCenter.commissions.columns.time') }}</th>
            <th class="px-3 py-2 font-medium">{{ t('agentCenter.commissions.columns.child') }}</th>
            <th class="px-3 py-2 font-medium">{{ t('agentCenter.commissions.columns.model') }}</th>
            <th class="px-3 py-2 text-right font-medium">{{ t('agentCenter.commissions.columns.base') }}</th>
            <th class="px-3 py-2 text-right font-medium">{{ t('agentCenter.commissions.columns.childPaid') }}</th>
            <th class="px-3 py-2 text-right font-medium">{{ t('agentCenter.commissions.columns.cost') }}</th>
            <th class="px-3 py-2 text-right font-medium">{{ t('agentCenter.commissions.columns.commission') }}</th>
            <th class="px-3 py-2 font-medium">{{ t('agentCenter.commissions.columns.status') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="item in items"
            :key="item.id"
            class="border-b border-gray-100 last:border-b-0 dark:border-dark-800"
            data-testid="agent-commission-row"
          >
            <td class="px-3 py-3 whitespace-nowrap text-gray-700 dark:text-gray-300">
              {{ formatDateTime(item.created_at) || '-' }}
            </td>
            <td class="px-3 py-3 text-gray-900 dark:text-white">{{ item.child_email || '-' }}</td>
            <td class="px-3 py-3 font-mono text-xs text-gray-700 dark:text-gray-300">{{ item.model || '-' }}</td>
            <td class="px-3 py-3 text-right tabular-nums text-gray-700 dark:text-gray-300">${{ formatAmount(item.base_cost) }}</td>
            <td class="px-3 py-3 text-right tabular-nums text-gray-700 dark:text-gray-300">${{ formatAmount(item.child_paid) }}</td>
            <td class="px-3 py-3 text-right tabular-nums text-gray-500 dark:text-dark-400">${{ formatAmount(item.agent_cost) }}</td>
            <td class="px-3 py-3 text-right font-medium tabular-nums" :class="commissionClass(item)">
              ${{ formatAmount(item.commission) }}
            </td>
            <td class="px-3 py-3">
              <span :class="commissionStatusClass(item.status)" :data-status="item.status">
                {{ statusLabel(item.status) }}
              </span>
            </td>
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
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import Select from '@/components/common/Select.vue'
import Pagination from '@/components/common/Pagination.vue'
import type { AgentChild, AgentCommissionRecord, AgentCommissionStatus } from '@/api/agent'
import { formatDateTime } from '@/utils/format'
import { commissionStatusClass, formatAmount } from './agentCenterUtils'

const props = defineProps<{
  items: AgentCommissionRecord[]
  total: number
  page: number
  pageSize: number
  loading: boolean
  /** 下级筛选值：'' 表示全部。 */
  childFilter: number | ''
  statusFilter: AgentCommissionStatus | ''
  /** YYYY-MM-DD（或空串）。 */
  startAt: string
  endAt: string
  childOptions: AgentChild[]
}>()

const emit = defineEmits<{
  (e: 'update:page', page: number): void
  (e: 'update:childFilter', value: number | ''): void
  (e: 'update:statusFilter', value: AgentCommissionStatus | ''): void
  (e: 'update:startAt', value: string): void
  (e: 'update:endAt', value: string): void
  (e: 'clear-filters'): void
  (e: 'refresh'): void
}>()

const { t } = useI18n()

const STATUS_VALUES: AgentCommissionStatus[] = ['pending', 'available', 'withdrawn', 'reversed']

const statusFilterOptions = computed(() => [
  { value: '', label: t('agentCenter.commissions.filters.allStatuses') },
  ...STATUS_VALUES.map((value) => ({
    value,
    label: t(`agentCenter.commissions.status.${value}`),
  })),
])

const childFilterOptions = computed(() => [
  { value: '', label: t('agentCenter.commissions.filters.allChildren') },
  ...props.childOptions.map((child) => ({
    value: child.user_id,
    label: child.email || child.username || `#${child.user_id}`,
  })),
])

const hasFilters = computed(
  () => props.childFilter !== '' || props.statusFilter !== '' || !!props.startAt || !!props.endAt,
)

function toNumberOrEmpty(value: unknown): number | '' {
  if (value === '' || value === null || value === undefined) return ''
  const parsed = Number(value)
  return Number.isFinite(parsed) ? parsed : ''
}

function onChildFilterChange(value: unknown): void {
  emit('update:childFilter', toNumberOrEmpty(value))
}

function onStatusFilterChange(value: unknown): void {
  const next = String(value ?? '')
  emit('update:statusFilter', STATUS_VALUES.includes(next as AgentCommissionStatus)
    ? (next as AgentCommissionStatus)
    : '')
}

function onPageChange(page: number): void {
  emit('update:page', page)
}

function statusLabel(status: AgentCommissionStatus | string): string {
  if (STATUS_VALUES.includes(status as AgentCommissionStatus)) {
    return t(`agentCenter.commissions.status.${status}`)
  }
  return String(status)
}

function commissionClass(item: AgentCommissionRecord): string {
  if (item.status === 'reversed') return 'text-red-600 dark:text-red-400'
  if (item.commission < 0) return 'text-red-600 dark:text-red-400'
  return 'text-emerald-600 dark:text-emerald-400'
}

function onStartChange(event: Event): void {
  emit('update:startAt', (event.target as HTMLInputElement).value)
}

function onEndChange(event: Event): void {
  emit('update:endAt', (event.target as HTMLInputElement).value)
}
</script>
