<template>
  <div class="card p-6" data-testid="agent-children">
    <div class="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
      <div>
        <h3 class="text-base font-semibold text-gray-900 dark:text-white">
          {{ t('agentCenter.children.title') }}
        </h3>
        <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">
          {{ t('agentCenter.children.description') }}
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

    <div v-if="loading && items.length === 0" class="mt-6 space-y-3">
      <div v-for="i in 3" :key="i" class="h-12 animate-pulse rounded-xl bg-gray-100 dark:bg-dark-700"></div>
    </div>

    <div
      v-else-if="items.length === 0"
      class="mt-6 rounded-xl border border-dashed border-gray-300 p-6 text-center dark:border-dark-700"
    >
      <p class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('agentCenter.children.empty') }}</p>
      <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">{{ t('agentCenter.children.emptyHint') }}</p>
    </div>

    <div v-else class="mt-5 overflow-x-auto">
      <table class="w-full min-w-[720px] text-left text-sm">
        <thead>
          <tr class="border-b border-gray-200 text-gray-500 dark:border-dark-700 dark:text-dark-400">
            <th class="px-3 py-2 font-medium">{{ t('agentCenter.children.columns.email') }}</th>
            <th class="px-3 py-2 font-medium">{{ t('agentCenter.children.columns.username') }}</th>
            <th class="px-3 py-2 font-medium">{{ t('agentCenter.children.columns.joinedAt') }}</th>
            <th class="px-3 py-2 font-medium">{{ t('agentCenter.children.columns.rate') }}</th>
            <th class="px-3 py-2 text-right font-medium">{{ t('agentCenter.children.columns.action') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="item in items"
            :key="item.user_id"
            class="border-b border-gray-100 last:border-b-0 dark:border-dark-800"
            data-testid="agent-child-row"
          >
            <td class="px-3 py-3 text-gray-900 dark:text-white">{{ item.email || '-' }}</td>
            <td class="px-3 py-3 text-gray-700 dark:text-gray-300">{{ item.username || '-' }}</td>
            <td class="px-3 py-3 text-gray-700 dark:text-gray-300">{{ formatDateTime(item.created_at) || '-' }}</td>
            <td class="px-3 py-3">
              <span v-if="hasRate(item.default_rate)" class="font-medium text-emerald-600 dark:text-emerald-400">
                {{ t('agentCenter.children.rateDisplay', rateTextParams(item.default_rate)) }}
              </span>
              <span v-else class="text-gray-400 dark:text-dark-500">{{ t('agentCenter.children.rateUnset') }}</span>
              <p
                v-if="groupOverrideCount(item) > 0"
                class="mt-0.5 text-xs text-gray-500 dark:text-dark-400"
              >
                {{ t('agentCenter.children.groupOverrides', { count: groupOverrideCount(item) }) }}
              </p>
            </td>
            <td class="px-3 py-3 text-right">
              <button
                type="button"
                class="btn btn-secondary btn-sm"
                :disabled="disabled"
                data-testid="agent-child-set-rate"
                @click="emit('set-rate', item)"
              >
                <Icon name="edit" size="sm" />
                <span>{{ t('agentCenter.children.setRate') }}</span>
              </button>
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
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import Pagination from '@/components/common/Pagination.vue'
import type { AgentChild } from '@/api/agent'
import { formatDateTime } from '@/utils/format'
import { hasRate, normalizeGroupRates, rateTextParams } from './agentCenterUtils'

defineProps<{
  items: AgentChild[]
  total: number
  page: number
  pageSize: number
  loading: boolean
  /** 代理被停用时禁止改价（后端也会拒）。 */
  disabled: boolean
}>()

const emit = defineEmits<{
  (e: 'update:page', page: number): void
  (e: 'set-rate', child: AgentChild): void
  (e: 'refresh'): void
}>()

const { t } = useI18n()

/** 该下级单独设置过倍率的分组数量（用于「另有 N 个分组单独设置」）。 */
function groupOverrideCount(child: AgentChild): number {
  return normalizeGroupRates(child.group_rates).length
}

function onPageChange(page: number): void {
  emit('update:page', page)
}
</script>
