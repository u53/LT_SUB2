<template>
  <AppLayout>
    <TablePageLayout>
      <template #filters>
        <div class="flex flex-wrap items-center gap-3">
          <div class="w-full sm:w-56">
            <Select
              v-model="filters.agentUserId"
              :options="agentOptions"
              searchable
              remote
              :loading="agentsLoading"
              :placeholder="t('agents.filters.agentPlaceholder')"
              :search-placeholder="t('agents.list.searchPlaceholder')"
              :aria-label="t('agents.filters.agent')"
              @search="searchAgents"
              @change="reloadFromFirstPage"
            />
          </div>

          <div class="w-full sm:w-44">
            <input
              v-model="filters.childUserId"
              type="number"
              min="1"
              step="1"
              class="input"
              :class="{ 'border-red-500': !!childIdError }"
              :title="t('agents.filters.child')"
              :placeholder="t('agents.filters.childPlaceholder')"
              @input="debounceLoad"
            />
          </div>

          <div class="w-full sm:w-40">
            <Select
              v-model="filters.status"
              :options="statusOptions"
              @change="reloadFromFirstPage"
            />
          </div>

          <input
            v-model="filters.startAt"
            type="date"
            class="input w-full sm:w-44"
            :title="t('agents.filters.startAt')"
            @change="reloadFromFirstPage"
          />
          <input
            v-model="filters.endAt"
            type="date"
            class="input w-full sm:w-44"
            :title="t('agents.filters.endAt')"
            @change="reloadFromFirstPage"
          />

          <button
            type="button"
            class="btn btn-secondary px-2 md:px-3"
            :disabled="loading"
            :title="t('common.refresh')"
            @click="loadRecords"
          >
            <Icon name="refresh" size="md" :class="loading ? 'animate-spin' : ''" />
          </button>
        </div>
        <p v-if="childIdError" class="input-error-text">{{ childIdError }}</p>
      </template>

      <template #table>
        <DataTable :columns="columns" :data="records" :loading="loading" row-key="id">
          <template #cell-created_at="{ row }">
            <span class="text-sm text-gray-700 dark:text-gray-300">{{ dateTimeLabel(row.created_at) }}</span>
          </template>

          <template #cell-agent="{ row }">
            <div class="space-y-0.5">
              <div class="font-mono text-xs text-gray-500 dark:text-dark-400">#{{ row.agent_user_id }}</div>
              <div class="max-w-48 truncate text-sm text-gray-900 dark:text-white">{{ row.agent_email || '-' }}</div>
            </div>
          </template>

          <template #cell-child="{ row }">
            <span v-if="row.child_user_id === null || row.child_user_id === undefined" class="text-sm text-gray-400 dark:text-dark-500">-</span>
            <div v-else class="space-y-0.5">
              <div class="font-mono text-xs text-gray-500 dark:text-dark-400">#{{ row.child_user_id }}</div>
              <div class="max-w-48 truncate text-sm text-gray-900 dark:text-white">{{ row.child_email || '-' }}</div>
            </div>
          </template>

          <template #cell-model="{ row }">
            <span class="max-w-56 truncate font-mono text-sm text-gray-900 dark:text-white">{{ row.model || '-' }}</span>
          </template>

          <template #cell-base_cost="{ row }">
            <span class="text-sm text-gray-700 dark:text-gray-300">{{ money(row.base_cost) }}</span>
          </template>

          <template #cell-child_rate="{ row }">
            <span class="text-sm text-gray-900 dark:text-white">{{ rateLabel(t, row.child_rate) }}</span>
          </template>

          <template #cell-agent_cost_rate="{ row }">
            <span class="text-sm text-gray-900 dark:text-white">{{ rateLabel(t, row.agent_cost_rate) }}</span>
          </template>

          <template #cell-child_paid="{ row }">
            <span class="text-sm text-gray-900 dark:text-white">{{ money(row.child_paid) }}</span>
          </template>

          <template #cell-agent_cost="{ row }">
            <span class="text-sm text-gray-700 dark:text-gray-300">{{ money(row.agent_cost) }}</span>
          </template>

          <template #cell-commission="{ row }">
            <span class="text-sm font-semibold text-emerald-600 dark:text-emerald-400">{{ money(row.commission) }}</span>
          </template>

          <template #cell-status="{ row }">
            <div class="flex flex-col items-start gap-1">
              <span :class="['badge whitespace-nowrap', statusClass(row.status)]">
                {{ statusLabel(row.status) }}
              </span>
              <span
                v-if="row.status === 'pending' && row.frozen_until"
                class="whitespace-nowrap text-xs text-gray-500 dark:text-dark-400"
              >
                {{ t('agents.commissions.frozenUntil', { time: dateTimeLabel(row.frozen_until) }) }}
              </span>
            </div>
          </template>

          <template #empty>
            <div class="flex flex-col items-center">
              <Icon name="inbox" size="xl" class="mb-4 h-12 w-12 text-gray-400 dark:text-dark-500" />
              <p class="text-lg font-medium text-gray-900 dark:text-gray-100">{{ t('agents.commissions.empty') }}</p>
            </div>
          </template>
        </DataTable>
      </template>

      <template #pagination>
        <Pagination
          v-if="pagination.total > 0"
          :page="pagination.page"
          :total="pagination.total"
          :page-size="pagination.page_size"
          @update:page="handlePageChange"
          @update:pageSize="handlePageSizeChange"
        />
      </template>
    </TablePageLayout>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import DataTable from '@/components/common/DataTable.vue'
import Pagination from '@/components/common/Pagination.vue'
import Select from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import type { Column } from '@/components/common/types'
import { useAppStore } from '@/stores/app'
import {
  agentsAPI,
  type AgentCommissionRecord,
  type AgentCommissionStatus,
  type AgentListItem,
} from '@/api/admin/agents'
import { extractI18nErrorMessage } from '@/utils/apiError'
import { dateTimeLabel, money, rateLabel } from './agentFormat'

const { t } = useI18n()
const appStore = useAppStore()

const loading = ref(false)
const agentsLoading = ref(false)
const records = ref<AgentCommissionRecord[]>([])
/** 远程搜索得到的代理选项；「全部」项由 computed 拼在最前面，跟随语言切换。 */
const agentItems = ref<Array<{ value: number; label: string }>>([])

const filters = reactive<{
  agentUserId: number | ''
  /** 直接用 v-model 绑定数字输入框，值是 string；统一按字符串解析并校验。 */
  childUserId: string | number
  status: AgentCommissionStatus | ''
  startAt: string
  endAt: string
}>({
  agentUserId: '',
  childUserId: '',
  status: '',
  startAt: '',
  endAt: '',
})

const pagination = reactive({ page: 1, page_size: 20, total: 0 })

let debounceTimer: ReturnType<typeof setTimeout> | null = null
let agentSearchSeq = 0

const childIdError = computed(() => {
  const raw = String(filters.childUserId ?? '').trim()
  if (raw === '') return ''
  const value = Number(raw)
  return Number.isInteger(value) && value > 0 ? '' : t('agents.filters.childInvalid')
})

const agentOptions = computed<Array<{ value: string | number; label: string }>>(() => [
  { value: '', label: t('agents.filters.agentPlaceholder') },
  ...agentItems.value,
])

const statusOptions = computed(() => [
  { value: '', label: t('agents.filters.statusAll') },
  { value: 'pending', label: t('agents.commissions.status.pending') },
  { value: 'available', label: t('agents.commissions.status.available') },
  { value: 'withdrawn', label: t('agents.commissions.status.withdrawn') },
  { value: 'reversed', label: t('agents.commissions.status.reversed') },
])

const columns = computed<Column[]>(() => [
  { key: 'created_at', label: t('agents.commissions.columns.createdAt') },
  { key: 'agent', label: t('agents.commissions.columns.agent') },
  { key: 'child', label: t('agents.commissions.columns.child') },
  { key: 'model', label: t('agents.commissions.columns.model') },
  { key: 'base_cost', label: t('agents.commissions.columns.baseCost') },
  { key: 'child_rate', label: t('agents.commissions.columns.childRate') },
  { key: 'agent_cost_rate', label: t('agents.commissions.columns.agentCostRate') },
  { key: 'child_paid', label: t('agents.commissions.columns.childPaid') },
  { key: 'agent_cost', label: t('agents.commissions.columns.agentCost') },
  { key: 'commission', label: t('agents.commissions.columns.commission') },
  { key: 'status', label: t('agents.commissions.columns.status') },
])

function statusLabel(status: string): string {
  switch (status) {
    case 'pending':
      return t('agents.commissions.status.pending')
    case 'available':
      return t('agents.commissions.status.available')
    case 'withdrawn':
      return t('agents.commissions.status.withdrawn')
    case 'reversed':
      return t('agents.commissions.status.reversed')
    default:
      return status || '-'
  }
}

function statusClass(status: string): string {
  switch (status) {
    case 'available':
      return 'badge-success'
    case 'pending':
      return 'badge-warning'
    case 'withdrawn':
      return 'badge-primary'
    case 'reversed':
      return 'badge-danger'
    default:
      return 'badge-gray'
  }
}

function showError(error: unknown) {
  appStore.showError(
    extractI18nErrorMessage(error, t, 'agents.errors', t('agents.commissions.loadFailed')),
  )
}

/** 代理筛选下拉：后端没有「代理 lookup」接口，复用列表接口按 search 远程取前 50 条。 */
async function searchAgents(query: string) {
  const seq = ++agentSearchSeq
  agentsLoading.value = true
  try {
    const res = await agentsAPI.listAgents({ page: 1, page_size: 50, search: query })
    if (seq !== agentSearchSeq) return
    const items: AgentListItem[] = res.items || []
    const selected = filters.agentUserId
    const options = items.map((item) => ({
      value: item.user_id,
      label: item.email ? `${item.email}${item.username ? ` (${item.username})` : ''}` : `#${item.user_id}`,
    }))
    // 已选中的代理可能不在当前搜索结果里，补一条以免触发器显示成「全部代理」。
    if (typeof selected === 'number' && !options.some((option) => option.value === selected)) {
      options.unshift({ value: selected, label: `#${selected}` })
    }
    agentItems.value = options
  } catch (error) {
    if (seq !== agentSearchSeq) return
    showError(error)
  } finally {
    if (seq === agentSearchSeq) agentsLoading.value = false
  }
}

async function loadRecords() {
  if (childIdError.value) return
  loading.value = true
  try {
    const rawChild = String(filters.childUserId ?? '').trim()
    const res = await agentsAPI.listCommissions({
      page: pagination.page,
      page_size: pagination.page_size,
      agent_user_id: typeof filters.agentUserId === 'number' ? filters.agentUserId : null,
      child_user_id: rawChild === '' ? null : Number(rawChild),
      status: filters.status,
      start_at: filters.startAt || undefined,
      end_at: filters.endAt || undefined,
    })
    records.value = res.items || []
    pagination.total = res.total || 0
  } catch (error) {
    showError(error)
  } finally {
    loading.value = false
  }
}

function debounceLoad() {
  if (debounceTimer) clearTimeout(debounceTimer)
  debounceTimer = setTimeout(() => reloadFromFirstPage(), 300)
}

function reloadFromFirstPage() {
  pagination.page = 1
  void loadRecords()
}

function handlePageChange(page: number) {
  pagination.page = page
  void loadRecords()
}

function handlePageSizeChange(size: number) {
  pagination.page_size = size
  pagination.page = 1
  void loadRecords()
}

onMounted(() => {
  void searchAgents('')
  void loadRecords()
})

onBeforeUnmount(() => {
  if (debounceTimer) clearTimeout(debounceTimer)
  agentSearchSeq++
})
</script>
