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

          <div class="w-full sm:w-40">
            <Select v-model="filters.status" :options="statusOptions" @change="reloadFromFirstPage" />
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

          <button
            type="button"
            class="btn btn-primary ml-auto"
            data-test="agent-withdraw-open"
            @click="withdrawDialog = true"
          >
            {{ t('agents.withdrawals.open') }}
          </button>
        </div>
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

          <template #cell-amount="{ row }">
            <span class="text-sm font-semibold text-emerald-600 dark:text-emerald-400">{{ money(row.amount) }}</span>
          </template>

          <template #cell-status="{ row }">
            <span :class="['badge whitespace-nowrap', row.status === 'paid' ? 'badge-success' : 'badge-gray']">
              {{ statusLabel(row.status) }}
            </span>
          </template>

          <template #cell-operation_id="{ row }">
            <span class="max-w-40 truncate font-mono text-xs text-gray-500 dark:text-dark-400">{{ row.operation_id || '-' }}</span>
          </template>

          <template #cell-note="{ row }">
            <span v-if="row.note" class="block max-w-56 truncate text-sm text-gray-700 dark:text-gray-300" :title="row.note">
              {{ row.note }}
            </span>
            <span v-else class="text-sm text-gray-400 dark:text-dark-500">-</span>
          </template>

          <template #empty>
            <div class="flex flex-col items-center">
              <Icon name="inbox" size="xl" class="mb-4 h-12 w-12 text-gray-400 dark:text-dark-500" />
              <p class="text-lg font-medium text-gray-900 dark:text-gray-100">{{ t('agents.withdrawals.empty') }}</p>
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

    <AgentWithdrawDialog
      :show="withdrawDialog"
      @close="withdrawDialog = false"
      @success="handleWithdrawSuccess"
    />
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
import AgentWithdrawDialog from './AgentWithdrawDialog.vue'
import type { Column } from '@/components/common/types'
import { useAppStore } from '@/stores/app'
import {
  agentsAPI,
  type AgentListItem,
  type AgentWithdrawal,
  type AgentWithdrawalStatus,
} from '@/api/admin/agents'
import { extractI18nErrorMessage } from '@/utils/apiError'
import { dateTimeLabel, money } from './agentFormat'

const { t } = useI18n()
const appStore = useAppStore()

const loading = ref(false)
const agentsLoading = ref(false)
const records = ref<AgentWithdrawal[]>([])
const withdrawDialog = ref(false)
/** 远程搜索得到的代理选项；「全部」项由 computed 拼在最前面，跟随语言切换。 */
const agentItems = ref<Array<{ value: number; label: string }>>([])

const filters = reactive<{
  agentUserId: number | ''
  status: AgentWithdrawalStatus | ''
  startAt: string
  endAt: string
}>({
  agentUserId: '',
  status: '',
  startAt: '',
  endAt: '',
})

const pagination = reactive({ page: 1, page_size: 20, total: 0 })

let agentSearchSeq = 0

const agentOptions = computed<Array<{ value: string | number; label: string }>>(() => [
  { value: '', label: t('agents.filters.agentPlaceholder') },
  ...agentItems.value,
])

const statusOptions = computed(() => [
  { value: '', label: t('agents.filters.statusAll') },
  { value: 'paid', label: t('agents.withdrawals.status.paid') },
  { value: 'voided', label: t('agents.withdrawals.status.voided') },
])

const columns = computed<Column[]>(() => [
  { key: 'created_at', label: t('agents.withdrawals.columns.createdAt') },
  { key: 'agent', label: t('agents.withdrawals.columns.agent') },
  { key: 'amount', label: t('agents.withdrawals.columns.amount') },
  { key: 'status', label: t('agents.withdrawals.columns.status') },
  { key: 'operation_id', label: t('agents.withdrawals.columns.operationId') },
  { key: 'note', label: t('agents.withdrawals.columns.note') },
])

function statusLabel(status: string): string {
  if (status === 'paid') return t('agents.withdrawals.status.paid')
  if (status === 'voided') return t('agents.withdrawals.status.voided')
  return status || '-'
}

function showError(error: unknown) {
  appStore.showError(
    extractI18nErrorMessage(error, t, 'agents.errors', t('agents.withdrawals.loadFailed')),
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
  loading.value = true
  try {
    const res = await agentsAPI.listWithdrawals({
      page: pagination.page,
      page_size: pagination.page_size,
      agent_user_id: typeof filters.agentUserId === 'number' ? filters.agentUserId : null,
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

function handleWithdrawSuccess() {
  withdrawDialog.value = false
  reloadFromFirstPage()
}

onMounted(() => {
  void searchAgents('')
  void loadRecords()
})

onBeforeUnmount(() => {
  agentSearchSeq++
})
</script>
