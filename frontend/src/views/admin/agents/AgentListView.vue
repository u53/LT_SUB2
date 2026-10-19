<template>
  <AppLayout>
    <TablePageLayout>
      <template #filters>
        <div class="flex flex-wrap items-center gap-3">
          <div class="relative w-full md:w-80">
            <Icon name="search" size="md" class="absolute left-3 top-1/2 -translate-y-1/2 text-gray-400" />
            <input
              v-model="search"
              type="text"
              class="input pl-10"
              :placeholder="t('agents.list.searchPlaceholder')"
              @input="debounceLoad"
            />
          </div>

          <div class="w-full sm:w-40">
            <Select
              v-model="enabledFilter"
              :options="enabledOptions"
              @change="reloadFromFirstPage"
            />
          </div>

          <button
            type="button"
            class="btn btn-secondary px-2 md:px-3"
            :disabled="loading"
            :title="t('common.refresh')"
            @click="loadAgents"
          >
            <Icon name="refresh" size="md" :class="loading ? 'animate-spin' : ''" />
          </button>
        </div>
      </template>

      <template #table>
        <DataTable :columns="columns" :data="agents" :loading="loading" row-key="user_id">
          <template #cell-agent="{ row }">
            <div class="flex items-center gap-3">
              <span
                class="flex h-9 w-9 flex-none items-center justify-center rounded-full bg-primary-50 text-sm font-semibold uppercase text-primary-600 dark:bg-primary-900/30 dark:text-primary-300"
              >
                {{ agentInitial(row) }}
              </span>
              <div class="min-w-0">
                <div class="truncate text-sm font-medium text-gray-900 dark:text-white">
                  {{ row.email || '-' }}
                </div>
                <div class="mt-0.5 flex items-center gap-2 text-xs text-gray-500 dark:text-dark-400">
                  <span class="font-mono">#{{ row.user_id }}</span>
                  <span v-if="row.username" class="truncate">{{ row.username }}</span>
                </div>
              </div>
            </div>
          </template>

          <template #cell-status="{ row }">
            <span class="inline-flex items-center gap-1.5 text-sm text-gray-700 dark:text-gray-300">
              <span
                class="h-1.5 w-1.5 flex-none rounded-full"
                :class="row.enabled ? 'bg-emerald-500' : 'bg-gray-400 dark:bg-dark-500'"
              ></span>
              {{ row.enabled ? t('common.enabled') : t('common.disabled') }}
            </span>
          </template>

          <template #cell-child_count="{ row }">
            <span class="text-sm text-gray-900 dark:text-white">{{ row.child_count ?? 0 }}</span>
          </template>

          <template #cell-available="{ row }">
            <span class="text-sm font-semibold text-emerald-600 dark:text-emerald-400">
              {{ money(row.available) }}
            </span>
          </template>

          <template #cell-frozen="{ row }">
            <span class="text-sm font-semibold text-amber-600 dark:text-amber-400">
              {{ money(row.frozen) }}
            </span>
          </template>

          <template #cell-total_commission="{ row }">
            <span class="text-sm text-gray-900 dark:text-white">{{ money(row.total_commission) }}</span>
          </template>

          <template #cell-actions="{ row }">
            <div class="flex items-center justify-end gap-1">
              <button
                type="button"
                class="inline-flex h-8 w-8 items-center justify-center rounded-lg text-gray-500 transition-colors hover:bg-gray-100 hover:text-primary-600 dark:text-dark-400 dark:hover:bg-dark-700 dark:hover:text-primary-400"
                :title="t('agents.list.configure')"
                :aria-label="t('agents.list.configure')"
                :data-test="`agent-configure-${row.user_id}`"
                @click="openDetail(row)"
              >
                <Icon name="cog" size="sm" />
              </button>
              <button
                type="button"
                class="inline-flex h-8 w-8 items-center justify-center rounded-lg text-gray-500 transition-colors hover:bg-gray-100 hover:text-primary-600 dark:text-dark-400 dark:hover:bg-dark-700 dark:hover:text-primary-400"
                :title="t('agents.list.children')"
                :aria-label="t('agents.list.children')"
                :data-test="`agent-children-${row.user_id}`"
                @click="openChildren(row)"
              >
                <Icon name="users" size="sm" />
              </button>
            </div>
          </template>

          <template #empty>
            <div class="flex flex-col items-center">
              <Icon name="inbox" size="xl" class="mb-4 h-12 w-12 text-gray-400 dark:text-dark-500" />
              <p class="text-lg font-medium text-gray-900 dark:text-gray-100">{{ t('agents.list.empty') }}</p>
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

    <AgentDetailDrawer
      :show="detailVisible"
      :agent-user-id="selectedAgentId"
      @close="detailVisible = false"
      @saved="loadAgents"
    />

    <AgentChildrenDrawer
      :show="childrenVisible"
      :agent-user-id="selectedAgentId"
      :agent-email="selectedAgentEmail"
      @close="childrenVisible = false"
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
import AgentDetailDrawer from './AgentDetailDrawer.vue'
import AgentChildrenDrawer from './AgentChildrenDrawer.vue'
import type { Column } from '@/components/common/types'
import { useAppStore } from '@/stores/app'
import { agentsAPI, type AgentListItem } from '@/api/admin/agents'
import { extractI18nErrorMessage } from '@/utils/apiError'
import { money } from './agentFormat'

const { t } = useI18n()
const appStore = useAppStore()

const loading = ref(false)
const agents = ref<AgentListItem[]>([])
const search = ref('')
/** '' = 全部；'true' / 'false' 对应后端 enabled 查询参数。 */
const enabledFilter = ref<string>('')
const pagination = reactive({ page: 1, page_size: 20, total: 0 })

const detailVisible = ref(false)
const childrenVisible = ref(false)
const selectedAgentId = ref<number | null>(null)
const selectedAgentEmail = ref('')

let debounceTimer: ReturnType<typeof setTimeout> | null = null

const enabledOptions = computed(() => [
  { value: '', label: t('agents.filters.statusAll') },
  { value: 'true', label: t('agents.filters.enabledOnly') },
  { value: 'false', label: t('agents.filters.disabledOnly') },
])

const columns = computed<Column[]>(() => [
  { key: 'agent', label: t('agents.list.columns.agent'), class: 'min-w-64' },
  { key: 'status', label: t('agents.list.columns.status'), class: 'w-28' },
  { key: 'child_count', label: t('agents.list.columns.childCount'), class: 'w-24 text-center' },
  { key: 'available', label: t('agents.list.columns.available'), class: 'w-32 text-right' },
  { key: 'frozen', label: t('agents.list.columns.frozen'), class: 'w-32 text-right' },
  { key: 'total_commission', label: t('agents.list.columns.totalCommission'), class: 'w-36 text-right' },
  { key: 'actions', label: t('agents.list.columns.actions'), class: 'w-28 text-right' },
])

function showError(error: unknown) {
  appStore.showError(
    extractI18nErrorMessage(error, t, 'agents.errors', t('agents.list.loadFailed')),
  )
}

async function loadAgents() {
  loading.value = true
  try {
    const res = await agentsAPI.listAgents({
      page: pagination.page,
      page_size: pagination.page_size,
      search: search.value,
      // 空串表示「全部」，不传给后端，否则后端会把它当成不筛选 —— 行为相同但仍保持参数干净
      enabled: enabledFilter.value === '' ? null : enabledFilter.value === 'true',
    })
    agents.value = res.items || []
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
  void loadAgents()
}

function handlePageChange(page: number) {
  pagination.page = page
  void loadAgents()
}

function handlePageSizeChange(size: number) {
  pagination.page_size = size
  pagination.page = 1
  void loadAgents()
}

/** 取头像首字母：优先邮箱，其次用户名，最后退化为 #。 */
function agentInitial(row: AgentListItem): string {
  const source = (row.email || row.username || '').trim()
  return source.charAt(0) || '#'
}

function openDetail(row: AgentListItem) {  selectedAgentId.value = row.user_id
  selectedAgentEmail.value = row.email || ''
  detailVisible.value = true
}

function openChildren(row: AgentListItem) {
  selectedAgentId.value = row.user_id
  selectedAgentEmail.value = row.email || ''
  childrenVisible.value = true
}

onMounted(() => {
  void loadAgents()
})

onBeforeUnmount(() => {
  if (debounceTimer) clearTimeout(debounceTimer)
})
</script>
