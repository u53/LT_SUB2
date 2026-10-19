<template>
  <BaseDialog
    :show="show"
    :title="t('agents.children.title')"
    width="extra-wide"
    placement="right"
    @close="handleClose"
  >
    <div class="space-y-4">
      <div v-if="agentUserId" class="rounded-lg border border-gray-100 bg-gray-50 px-4 py-3 dark:border-dark-700 dark:bg-dark-800">
        <p class="font-mono text-sm text-gray-500 dark:text-dark-400">
          #{{ agentUserId }}
        </p>
        <p class="mt-0.5 truncate text-sm font-medium text-gray-900 dark:text-white">{{ agentEmail || '-' }}</p>
      </div>

      <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('agents.children.hint') }}</p>

      <div class="overflow-hidden rounded-xl border border-gray-200 dark:border-dark-600">
        <table class="w-full text-sm">
          <thead>
            <tr class="border-b border-gray-200 bg-gray-50 dark:border-dark-600 dark:bg-dark-700">
              <th class="px-4 py-2 text-left text-xs font-medium text-gray-500 dark:text-dark-400">
                {{ t('agents.children.columns.user') }}
              </th>
              <th class="px-4 py-2 text-left text-xs font-medium text-gray-500 dark:text-dark-400">
                {{ t('agents.children.columns.joinedAt') }}
              </th>
              <th class="px-4 py-2 text-left text-xs font-medium text-gray-500 dark:text-dark-400">
                {{ t('agents.children.columns.defaultRate') }}
              </th>
            </tr>
          </thead>
          <tbody class="divide-y divide-gray-100 dark:divide-dark-600">
            <tr v-if="loading">
              <td colspan="3" class="px-4 py-8 text-center text-gray-500 dark:text-dark-400">
                {{ t('common.loading') }}
              </td>
            </tr>
            <tr v-else-if="children.length === 0">
              <td colspan="3" class="px-4 py-8 text-center text-gray-500 dark:text-dark-400">
                {{ t('agents.children.empty') }}
              </td>
            </tr>
            <template v-else>
              <tr
                v-for="child in children"
                :key="child.user_id"
                class="hover:bg-gray-50 dark:hover:bg-dark-700/50"
              >
                <td class="px-4 py-2">
                  <div class="space-y-0.5">
                    <div class="font-mono text-xs text-gray-500 dark:text-dark-400">#{{ child.user_id }}</div>
                    <div class="max-w-64 truncate text-gray-900 dark:text-white">{{ child.email || '-' }}</div>
                    <div class="max-w-64 truncate text-xs text-gray-500 dark:text-dark-400">
                      {{ child.username || '-' }}
                    </div>
                  </div>
                </td>
                <td class="whitespace-nowrap px-4 py-2 text-gray-700 dark:text-dark-300">
                  {{ dateTimeLabel(child.created_at) }}
                </td>
                <td class="whitespace-nowrap px-4 py-2">
                  <span
                    v-if="child.default_rate === null || child.default_rate === undefined"
                    class="text-gray-400 dark:text-dark-500"
                  >-</span>
                  <span v-else class="text-gray-900 dark:text-white">{{ rateLabel(t, child.default_rate) }}</span>
                </td>
              </tr>
            </template>
          </tbody>
        </table>
      </div>

      <Pagination
        v-if="pagination.total > 0"
        :page="pagination.page"
        :total="pagination.total"
        :page-size="pagination.page_size"
        @update:page="handlePageChange"
        @update:pageSize="handlePageSizeChange"
      />
    </div>
  </BaseDialog>
</template>

<script setup lang="ts">
import { onBeforeUnmount, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Pagination from '@/components/common/Pagination.vue'
import { useAppStore } from '@/stores/app'
import { agentsAPI, type AgentChild } from '@/api/admin/agents'
import { extractI18nErrorMessage } from '@/utils/apiError'
import { dateTimeLabel, rateLabel } from './agentFormat'

const props = defineProps<{
  show: boolean
  agentUserId: number | null
  agentEmail?: string
}>()

const emit = defineEmits<{
  close: []
}>()

const { t } = useI18n()
const appStore = useAppStore()

const loading = ref(false)
const children = ref<AgentChild[]>([])
const pagination = reactive({ page: 1, page_size: 20, total: 0 })

function showError(error: unknown) {
  appStore.showError(
    extractI18nErrorMessage(error, t, 'agents.errors', t('agents.children.loadFailed')),
  )
}

async function loadChildren() {
  const agentUserId = props.agentUserId
  if (!agentUserId) return
  loading.value = true
  try {
    const res = await agentsAPI.listChildren(agentUserId, {
      page: pagination.page,
      page_size: pagination.page_size,
    })
    children.value = res.items || []
    pagination.total = res.total || 0
  } catch (error) {
    children.value = []
    pagination.total = 0
    showError(error)
  } finally {
    loading.value = false
  }
}

function handlePageChange(page: number) {
  pagination.page = page
  void loadChildren()
}

function handlePageSizeChange(size: number) {
  pagination.page_size = size
  pagination.page = 1
  void loadChildren()
}

function handleClose() {
  emit('close')
}

watch(
  // 同时盯住打开状态与目标代理：列表页先改 id 再置 visible，分开监听会在同一次 flush 里
  // 触发两次加载（重复请求）。合成一个 key 后每次打开只请求一次。
  () => (props.show ? `${props.agentUserId ?? ''}` : ''),
  (key) => {
    if (!key) {
      children.value = []
      pagination.page = 1
      pagination.total = 0
      return
    }
    pagination.page = 1
    void loadChildren()
  },
  { immediate: true },
)

onBeforeUnmount(() => {
  children.value = []
})
</script>
