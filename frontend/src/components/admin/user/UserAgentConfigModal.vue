<template>
  <BaseDialog
    :show="show"
    :title="t('admin.users.agentConfig.title')"
    width="wide"
    @close="$emit('close')"
  >
    <div v-if="user" class="space-y-6">
      <!-- 用户信息头部 -->
      <div class="flex items-center gap-4 rounded-2xl bg-gradient-to-r from-emerald-50 to-emerald-100 p-5 dark:from-emerald-900/30 dark:to-emerald-800/20">
        <div class="flex h-14 w-14 items-center justify-center rounded-full bg-white shadow-sm dark:bg-dark-700">
          <span class="text-2xl font-semibold text-emerald-600 dark:text-emerald-400">
            {{ user.email.charAt(0).toUpperCase() }}
          </span>
        </div>
        <div class="flex-1">
          <p class="text-lg font-semibold text-gray-900 dark:text-white">{{ user.email }}</p>
          <div class="mt-1 flex flex-wrap items-center gap-2">
            <span class="badge" :class="isAgentUser ? 'badge-success' : 'badge-gray'">
              {{ isAgentUser ? t('admin.users.roles.agent') : t('admin.users.roles.' + currentRole) }}
            </span>
            <span class="text-sm text-gray-600 dark:text-gray-400">
              {{ t('admin.users.agentConfig.hint', { email: user.email }) }}
            </span>
          </div>
        </div>
      </div>

      <!-- 非代理用户：提供「设为代理」入口 -->
      <div
        v-if="!isAgentUser"
        class="rounded-xl border border-amber-200 bg-amber-50 p-4 dark:border-amber-900/60 dark:bg-amber-950/30"
        data-testid="agent-promote-card"
      >
        <p class="text-sm font-medium text-amber-900 dark:text-amber-200">
          {{ t('admin.users.agentConfig.notAgentTitle') }}
        </p>
        <p class="mt-1 text-sm text-amber-800 dark:text-amber-300">
          {{ t('admin.users.agentConfig.notAgentHint') }}
        </p>
        <button
          type="button"
          class="btn btn-primary mt-3"
          :disabled="promoting"
          data-testid="agent-promote"
          @click="handlePromote"
        >
          <Icon v-if="promoting" name="refresh" size="sm" class="mr-1 animate-spin" />
          {{ promoting ? t('common.saving') : t('admin.users.agentConfig.promote') }}
        </button>
      </div>

      <!-- 加载状态 -->
      <div v-if="loading" class="flex justify-center py-12">
        <svg class="h-10 w-10 animate-spin text-primary-500" fill="none" viewBox="0 0 24 24">
          <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
          <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
        </svg>
      </div>

      <template v-else>
        <!-- 佣金账户概览 -->
        <div v-if="loaded" class="grid gap-3 sm:grid-cols-3" data-testid="agent-account-summary">
          <div class="rounded-xl border border-gray-200 p-4 dark:border-dark-600">
            <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.users.agentConfig.account.available') }}</p>
            <p class="mt-1 text-lg font-semibold text-emerald-600 dark:text-emerald-400">{{ formatAmount(detail?.available) }}</p>
          </div>
          <div class="rounded-xl border border-gray-200 p-4 dark:border-dark-600">
            <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.users.agentConfig.account.frozen') }}</p>
            <p class="mt-1 text-lg font-semibold text-amber-600 dark:text-amber-400">{{ formatAmount(detail?.frozen) }}</p>
          </div>
          <div class="rounded-xl border border-gray-200 p-4 dark:border-dark-600">
            <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.users.agentConfig.account.total') }}</p>
            <p class="mt-1 text-lg font-semibold text-gray-900 dark:text-white">{{ formatAmount(detail?.total_commission) }}</p>
          </div>
        </div>

        <!-- 代理基础配置 -->
        <div class="space-y-5">
          <div class="flex items-start justify-between gap-4">
            <div>
              <label class="text-sm font-medium text-gray-700 dark:text-gray-300">
                {{ t('admin.users.agentConfig.enabled') }}
              </label>
              <p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
                {{ t('admin.users.agentConfig.enabledHint') }}
              </p>
            </div>
            <Toggle v-model="form.enabled" data-testid="agent-config-enabled" />
          </div>

          <div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <div>
              <label class="input-label">{{ t('admin.users.agentConfig.defaultRate') }}</label>
              <input
                v-model.number="form.default_child_rate_multiplier"
                type="number"
                step="0.001"
                min="0.001"
                max="1"
                class="input"
                :placeholder="t('admin.users.agentConfig.useGroupPrice')"
                data-testid="agent-config-default-rate"
              />
              <p class="input-hint">{{ t('admin.users.agentConfig.defaultRateHint') }}</p>
            </div>
            <div>
              <label class="input-label">{{ t('admin.users.agentConfig.frozenHours') }}</label>
              <input
                v-model.number="form.commission_frozen_hours"
                type="number"
                step="1"
                min="0"
                class="input"
                data-testid="agent-config-frozen-hours"
              />
              <p class="input-hint">{{ t('admin.users.agentConfig.frozenHoursHint') }}</p>
            </div>
          </div>

          <div>
            <label class="input-label">{{ t('admin.users.agentConfig.notes') }}</label>
            <textarea
              v-model="form.note"
              rows="3"
              class="input"
              :placeholder="t('admin.users.agentConfig.notesPlaceholder')"
              data-testid="agent-config-notes"
            ></textarea>
          </div>
        </div>

        <!-- 按分组成本折扣 -->
        <div class="space-y-3 border-t border-gray-100 pt-6 dark:border-dark-700">
          <div>
            <h4 class="text-sm font-semibold text-gray-900 dark:text-white">
              {{ t('admin.users.agentConfig.groupCosts.title') }}
            </h4>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.users.agentConfig.groupCosts.description') }}
            </p>
          </div>

          <div v-if="groupRows.length === 0" class="rounded-xl border border-dashed border-gray-300 p-6 text-center text-sm text-gray-500 dark:border-dark-700 dark:text-dark-400">
            {{ t('admin.users.agentConfig.groupCosts.empty') }}
          </div>

          <div v-else class="overflow-hidden rounded-xl border border-gray-200 dark:border-dark-600">
            <div class="max-h-[320px] overflow-auto">
              <table class="w-full text-sm">
                <thead class="sticky top-0 z-[1]">
                  <tr class="border-b border-gray-200 bg-gray-50 dark:border-dark-600 dark:bg-dark-700">
                    <th class="px-3 py-2 text-left text-xs font-medium text-gray-500 dark:text-gray-400">
                      {{ t('admin.users.agentConfig.groupCosts.group') }}
                    </th>
                    <th class="px-3 py-2 text-left text-xs font-medium text-gray-500 dark:text-gray-400">
                      {{ t('admin.users.agentConfig.groupCosts.groupRate') }}
                    </th>
                    <th class="px-3 py-2 text-left text-xs font-medium text-gray-500 dark:text-gray-400">
                      {{ t('admin.users.agentConfig.groupCosts.costRate') }}
                    </th>
                  </tr>
                </thead>
                <tbody class="divide-y divide-gray-100 dark:divide-dark-600">
                  <tr
                    v-for="row in groupRows"
                    :key="row.groupId"
                    class="hover:bg-gray-50 dark:hover:bg-dark-700/50"
                  >
                    <td class="px-3 py-2">
                      <span class="inline-flex items-center gap-1.5 text-gray-900 dark:text-white">
                        <PlatformIcon :platform="row.platform" size="xs" />
                        {{ row.groupName }}
                      </span>
                    </td>
                    <td class="whitespace-nowrap px-3 py-2 text-gray-500 dark:text-gray-400">
                      {{ row.groupRate }}x
                    </td>
                    <td class="whitespace-nowrap px-3 py-2">
                      <input
                        type="number"
                        step="0.001"
                        min="0.001"
                        autocomplete="off"
                        :value="row.costMultiplier ?? ''"
                        :placeholder="t('admin.users.agentConfig.groupCosts.useGroupRate')"
                        class="hide-spinner w-28 rounded-lg border border-gray-300 bg-white px-3 py-1.5 text-sm transition-colors focus:border-primary-500 focus:outline-none focus:ring-2 focus:ring-primary-500/20 dark:border-dark-500 dark:bg-dark-700 dark:focus:border-primary-500"
                        :data-testid="`agent-group-cost-${row.groupId}`"
                        @change="updateCostMultiplier(row.groupId, ($event.target as HTMLInputElement).value)"
                      />
                    </td>
                  </tr>
                </tbody>
              </table>
            </div>
          </div>
        </div>

        <p v-if="loadFailed" class="text-xs text-amber-600 dark:text-amber-400">
          {{ t('admin.users.agentConfig.loadFailed') }}
        </p>
      </template>
    </div>

    <template #footer>
      <div class="flex justify-end gap-3">
        <button @click="$emit('close')" type="button" class="btn btn-secondary px-5">
          {{ t('common.cancel') }}
        </button>
        <button
          @click="handleSave"
          type="button"
          :disabled="submitting || loading || !isAgentUser || !loaded"
          class="btn btn-primary px-6"
        >
          <Icon v-if="submitting" name="refresh" size="sm" class="mr-1 animate-spin" />
          {{ submitting ? t('common.saving') : t('common.save') }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { apiClient } from '@/api/client'
import { adminAPI } from '@/api/admin'
import { useAppStore } from '@/stores/app'
import type {
  AdminUser,
  AgentDetail,
  AgentGroupCostItem,
  GroupPlatform,
  ReplaceAgentGroupCostsRequest,
  UpdateAgentRequest
} from '@/types'
import { extractApiErrorMessage } from '@/utils/apiError'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Toggle from '@/components/common/Toggle.vue'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import Icon from '@/components/icons/Icon.vue'

interface GroupCostRow {
  groupId: number
  groupName: string
  platform: GroupPlatform
  groupRate: number
  costMultiplier: number | null
}

const props = defineProps<{ show: boolean; user: AdminUser | null }>()
const emit = defineEmits(['close', 'success'])
const { t } = useI18n()
const appStore = useAppStore()

const loading = ref(false)
const loaded = ref(false)
const loadFailed = ref(false)
const submitting = ref(false)
const promoting = ref(false)
// 角色在弹窗内可被「设为代理」改变，避免依赖父组件刷新后才解锁表单
const currentRole = ref<string>('user')
const isAgentUser = computed(() => currentRole.value === 'agent')

const form = reactive({
  enabled: true,
  // null = 不设置默认下级折扣（下级按分组公开价计费）
  default_child_rate_multiplier: null as number | null,
  commission_frozen_hours: 0,
  note: ''
})

const detail = ref<AgentDetail | null>(null)
const groupRows = ref<GroupCostRow[]>([])

const formatAmount = (value?: number): string => `$${(value ?? 0).toFixed(4)}`

const resetLocalState = () => {
  Object.assign(form, {
    enabled: true,
    default_child_rate_multiplier: null,
    commission_frozen_hours: 0,
    note: ''
  })
  detail.value = null
  groupRows.value = []
  loaded.value = false
  loadFailed.value = false
}

watch(
  () => props.show,
  (visible) => {
    if (!visible) return
    resetLocalState()
    currentRole.value = props.user?.role || 'user'
    if (props.user) void load()
  }
)

/** 后端 group_costs 是 map[string]float64，键在 JSON 里是字符串，这里统一转成数字键。 */
const normalizeGroupCosts = (raw?: Record<string, number> | null): Record<number, number> => {
  const out: Record<number, number> = {}
  for (const [key, value] of Object.entries(raw || {})) {
    const groupId = Number(key)
    if (Number.isFinite(groupId) && typeof value === 'number') out[groupId] = value
  }
  return out
}

const load = async () => {
  if (!props.user) return
  const userId = props.user.id
  loading.value = true
  try {
    // 非代理用户还没有代理档案，读取失败属预期状态，只标记不弹错。
    try {
      const { data } = await apiClient.get<AgentDetail>(`/admin/agents/${userId}`)
      if (data) {
        detail.value = data
        Object.assign(form, {
          enabled: data.enabled ?? true,
          default_child_rate_multiplier: data.default_child_rate_multiplier ?? null,
          commission_frozen_hours: data.commission_frozen_hours ?? 0,
          note: data.note || ''
        })
        if (data.role) currentRole.value = data.role
      }
    } catch (error) {
      console.error('Failed to load agent detail:', error)
      if (isAgentUser.value) loadFailed.value = true
    }

    // group_costs 随详情一起返回，无需额外请求。
    const resolvedCosts = normalizeGroupCosts(detail.value?.group_costs)

    // 分组清单以「启用中的分组」为准，成本倍率取自代理档案的 group_costs。
    const groupsRes = await adminAPI.groups.list(1, 1000)
    groupRows.value = (groupsRes.items || [])
      .filter((group) => group.status === 'active')
      .map((group) => ({
        groupId: group.id,
        groupName: group.name,
        platform: group.platform,
        groupRate: group.rate_multiplier,
        costMultiplier: resolvedCosts[group.id] ?? null
      }))
    loaded.value = true
  } catch (error) {
    console.error('Failed to load groups for agent config:', error)
    loadFailed.value = true
  } finally {
    loading.value = false
  }
}

const updateCostMultiplier = (groupId: number, value: string) => {
  const row = groupRows.value.find((item) => item.groupId === groupId)
  if (!row) return
  if (value === '' || value === null || value === undefined) {
    row.costMultiplier = null
    return
  }
  const parsed = parseFloat(value)
  row.costMultiplier = Number.isFinite(parsed) && parsed > 0 ? parsed : null
}

const handlePromote = async () => {
  if (!props.user || promoting.value) return
  promoting.value = true
  try {
    await adminAPI.users.update(props.user.id, { role: 'agent' })
    currentRole.value = 'agent'
    appStore.showSuccess(t('admin.users.agentConfig.promoted'))
    emit('success')
    await load()
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('admin.users.agentConfig.promoteFailed')))
  } finally {
    promoting.value = false
  }
}

const handleSave = async () => {
  if (!props.user || !loaded.value || !isAgentUser.value || submitting.value) return
  submitting.value = true
  try {
    const rawRate = form.default_child_rate_multiplier
    const parsedRate = rawRate === null || rawRate === undefined ? null : Number(rawRate)
    const payload: UpdateAgentRequest = {
      enabled: form.enabled,
      // 0/非法值等同于「不设置默认折扣」，避免后端 gt=0 校验报错
      default_child_rate_multiplier:
        parsedRate !== null && Number.isFinite(parsedRate) && parsedRate > 0 ? parsedRate : null,
      commission_frozen_hours: Math.max(
        0,
        Math.min(720, Math.trunc(Number(form.commission_frozen_hours) || 0))
      ),
      note: form.note
    }
    await apiClient.put<{ user_id: number }>(`/admin/agents/${props.user.id}`, payload)

    // 全量替换语义：只提交当前有效（>0）的分组，未提交的分组视为删除。
    const costsPayload: ReplaceAgentGroupCostsRequest = {
      group_costs: groupRows.value
        .filter((row) => typeof row.costMultiplier === 'number' && row.costMultiplier > 0)
        .map<AgentGroupCostItem>((row) => ({
          group_id: row.groupId,
          cost_multiplier: row.costMultiplier as number
        }))
    }
    await apiClient.put(`/admin/agents/${props.user.id}/group-costs`, costsPayload)

    appStore.showSuccess(t('admin.users.agentConfig.saved'))
    emit('success')
    emit('close')
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('admin.users.agentConfig.saveFailed')))
  } finally {
    submitting.value = false
  }
}
</script>

<style scoped>
/* 隐藏数字输入框的箭头按钮 */
.hide-spinner::-webkit-outer-spin-button,
.hide-spinner::-webkit-inner-spin-button {
  -webkit-appearance: none;
  margin: 0;
}
.hide-spinner {
  -moz-appearance: textfield;
}
</style>
