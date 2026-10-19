<template>
  <BaseDialog
    :show="show"
    :title="t('agents.detail.title')"
    width="wide"
    placement="right"
    @close="handleClose"
  >
    <div v-if="loading" class="flex justify-center py-12">
      <div class="h-8 w-8 animate-spin rounded-full border-2 border-primary-500 border-t-transparent"></div>
    </div>

    <div v-else-if="loadFailed" class="rounded-lg border border-amber-200 bg-amber-50 p-3 text-sm text-amber-800 dark:border-amber-700/50 dark:bg-amber-900/20 dark:text-amber-300">
      {{ t('agents.detail.loadFailed') }}
    </div>

    <div v-else-if="detail" class="space-y-6">
      <!-- 账号头部 -->
      <div class="rounded-2xl bg-gradient-to-r from-emerald-50 to-emerald-100 p-5 dark:from-emerald-900/30 dark:to-emerald-800/20">
        <div class="flex items-center gap-4">
          <div class="flex h-14 w-14 shrink-0 items-center justify-center rounded-full bg-white shadow-sm dark:bg-dark-700">
            <span class="text-2xl font-semibold text-emerald-600 dark:text-emerald-400">
              {{ (detail.email || String(detail.user_id)).charAt(0).toUpperCase() }}
            </span>
          </div>
          <div class="min-w-0 flex-1">
            <p class="truncate text-lg font-semibold text-gray-900 dark:text-white">{{ detail.email || '-' }}</p>
            <p class="mt-0.5 truncate text-sm text-gray-600 dark:text-dark-300">
              <span class="font-mono">#{{ detail.user_id }}</span>
              <span v-if="detail.username" class="ml-2">{{ detail.username }}</span>
            </p>
          </div>
          <span :class="['badge shrink-0', detail.enabled ? 'badge-success' : 'badge-gray']">
            {{ detail.enabled ? t('common.enabled') : t('common.disabled') }}
          </span>
        </div>
        <p class="mt-3 text-sm text-gray-600 dark:text-dark-300">
          {{ t('agents.detail.hint', { email: detail.email || `#${detail.user_id}` }) }}
        </p>
      </div>

      <!-- 佣金账户概览 -->
      <div class="grid gap-3 sm:grid-cols-4">
        <div class="rounded-xl border border-gray-200 p-4 dark:border-dark-600">
          <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('agents.detail.account.available') }}</p>
          <p class="mt-1 text-lg font-semibold text-emerald-600 dark:text-emerald-400">{{ money(detail.available) }}</p>
        </div>
        <div class="rounded-xl border border-gray-200 p-4 dark:border-dark-600">
          <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('agents.detail.account.frozen') }}</p>
          <p class="mt-1 text-lg font-semibold text-amber-600 dark:text-amber-400">{{ money(detail.frozen) }}</p>
        </div>
        <div class="rounded-xl border border-gray-200 p-4 dark:border-dark-600">
          <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('agents.detail.account.withdrawn') }}</p>
          <p class="mt-1 text-lg font-semibold text-gray-900 dark:text-white">{{ money(detail.withdrawn) }}</p>
        </div>
        <div class="rounded-xl border border-gray-200 p-4 dark:border-dark-600">
          <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('agents.detail.account.total') }}</p>
          <p class="mt-1 text-lg font-semibold text-gray-900 dark:text-white">{{ money(detail.total_commission) }}</p>
        </div>
      </div>

      <!-- 只读字段 -->
      <div class="grid gap-3 sm:grid-cols-3">
        <div class="rounded-lg border border-gray-100 bg-gray-50 p-3 dark:border-dark-700 dark:bg-dark-800">
          <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('agents.detail.fields.role') }}</p>
          <p class="mt-1 text-sm font-medium text-gray-900 dark:text-white">{{ detail.role || '-' }}</p>
        </div>
        <div class="rounded-lg border border-gray-100 bg-gray-50 p-3 dark:border-dark-700 dark:bg-dark-800">
          <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('agents.detail.fields.createdAt') }}</p>
          <p class="mt-1 text-sm font-medium text-gray-900 dark:text-white">{{ dateTimeLabel(detail.created_at) }}</p>
        </div>
        <div class="rounded-lg border border-gray-100 bg-gray-50 p-3 dark:border-dark-700 dark:bg-dark-800">
          <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('agents.detail.fields.updatedAt') }}</p>
          <p class="mt-1 text-sm font-medium text-gray-900 dark:text-white">{{ dateTimeLabel(detail.updated_at) }}</p>
        </div>
      </div>

      <!-- 可编辑字段 -->
      <div class="space-y-5">
        <div class="flex items-start justify-between gap-4">
          <div>
            <label class="text-sm font-medium text-gray-700 dark:text-dark-300">
              {{ t('agents.detail.fields.enabled') }}
            </label>
            <p class="mt-0.5 text-xs text-gray-500 dark:text-dark-400">
              {{ t('agents.detail.fields.enabledHint') }}
            </p>
          </div>
          <Toggle v-model="form.enabled" data-test="agent-detail-enabled" />
        </div>

        <div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <div>
            <label class="input-label">{{ t('agents.detail.fields.defaultRate') }}</label>
            <input
              v-model="form.defaultRate"
              type="number"
              step="0.001"
              min="0.001"
              class="input"
              :placeholder="t('agents.detail.fields.useGroupPrice')"
              data-test="agent-detail-default-rate"
            />
            <p v-if="rateError" class="input-error-text">{{ rateError }}</p>
            <p v-else class="input-hint">{{ t('agents.detail.fields.defaultRateHint') }}</p>
          </div>
          <div>
            <label class="input-label">{{ t('agents.detail.fields.frozenHours') }}</label>
            <input
              v-model="form.frozenHours"
              type="number"
              step="1"
              min="0"
              max="720"
              class="input"
              data-test="agent-detail-frozen-hours"
            />
            <p v-if="frozenHoursError" class="input-error-text">{{ frozenHoursError }}</p>
            <p v-else class="input-hint">{{ t('agents.detail.fields.frozenHoursHint') }}</p>
          </div>
        </div>

        <div>
          <label class="input-label">{{ t('agents.detail.fields.note') }}</label>
          <textarea
            v-model="form.note"
            rows="3"
            class="input"
            :placeholder="t('agents.detail.fields.notePlaceholder')"
            data-test="agent-detail-note"
          ></textarea>
        </div>
      </div>

      <!-- 分组成本折扣 -->
      <div class="space-y-3 border-t border-gray-100 pt-6 dark:border-dark-700">
        <div>
          <h4 class="text-sm font-semibold text-gray-900 dark:text-white">
            {{ t('agents.detail.groupCosts.title') }}
          </h4>
          <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">
            {{ t('agents.detail.groupCosts.description') }}
          </p>
        </div>

        <p v-if="groupsLoading" class="input-hint">{{ t('common.loading') }}</p>

        <div
          v-else-if="groupRows.length === 0"
          class="rounded-xl border border-dashed border-gray-300 p-6 text-center text-sm text-gray-500 dark:border-dark-700 dark:text-dark-400"
        >
          {{ t('agents.detail.groupCosts.empty') }}
        </div>

        <div v-else class="overflow-hidden rounded-xl border border-gray-200 dark:border-dark-600">
          <div class="max-h-[320px] overflow-auto">
            <table class="w-full text-sm">
              <thead class="sticky top-0 z-[1]">
                <tr class="border-b border-gray-200 bg-gray-50 dark:border-dark-600 dark:bg-dark-700">
                  <th class="px-3 py-2 text-left text-xs font-medium text-gray-500 dark:text-dark-400">
                    {{ t('agents.detail.groupCosts.group') }}
                  </th>
                  <th class="px-3 py-2 text-left text-xs font-medium text-gray-500 dark:text-dark-400">
                    {{ t('agents.detail.groupCosts.groupRate') }}
                  </th>
                  <th class="px-3 py-2 text-left text-xs font-medium text-gray-500 dark:text-dark-400">
                    {{ t('agents.detail.groupCosts.costRate') }}
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
                      <PlatformIcon v-if="row.platform" :platform="row.platform" size="xs" />
                      <span class="max-w-48 truncate">{{ row.groupName }}</span>
                      <span v-if="row.inactive" class="badge badge-gray">
                        {{ t('agents.detail.groupCosts.inactive') }}
                      </span>
                    </span>
                  </td>
                  <td class="whitespace-nowrap px-3 py-2 text-gray-500 dark:text-dark-400">
                    {{ row.groupRate }}x
                  </td>
                  <td class="whitespace-nowrap px-3 py-2">
                    <input
                      :value="row.costMultiplier"
                      type="number"
                      step="0.001"
                      min="0.001"
                      autocomplete="off"
                      class="hide-spinner w-28 rounded-lg border border-gray-300 bg-white px-3 py-1.5 text-sm transition-colors focus:border-primary-500 focus:outline-none focus:ring-2 focus:ring-primary-500/20 dark:border-dark-500 dark:bg-dark-700 dark:focus:border-primary-500"
                      :class="{ 'border-red-500 dark:border-red-500': !!row.error }"
                      :placeholder="t('agents.detail.groupCosts.placeholder')"
                      :data-test="`agent-cost-${row.groupId}`"
                      @input="updateCostMultiplier(row, ($event.target as HTMLInputElement).value)"
                    />
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>

        <p v-if="costError" class="input-error-text">{{ costError }}</p>
      </div>
    </div>

    <template #footer>
      <div class="flex justify-end gap-3">
        <button type="button" class="btn btn-secondary px-5" :disabled="submitting" @click="handleClose">
          {{ t('common.cancel') }}
        </button>
        <button
          type="button"
          class="btn btn-primary px-6"
          :disabled="!canSubmit"
          data-test="agent-detail-save"
          @click="save"
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
import BaseDialog from '@/components/common/BaseDialog.vue'
import Toggle from '@/components/common/Toggle.vue'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import Icon from '@/components/icons/Icon.vue'
import { useAppStore } from '@/stores/app'
import { agentsAPI, type AgentDetail, type AgentGroupCostItem } from '@/api/admin/agents'
import { groupsAPI } from '@/api/admin/groups'
import type { GroupPlatform } from '@/types'
import { extractI18nErrorMessage } from '@/utils/apiError'
import { dateTimeLabel, money } from './agentFormat'

interface GroupCostRow {
  groupId: number
  groupName: string
  /** 已删除的分组没有平台信息，此时不渲染平台图标。 */
  platform?: GroupPlatform
  groupRate: number
  /** 输入框的原始字符串：'' 表示未配置。用字符串保存才能区分「空」与「0」。 */
  costMultiplier: string
  inactive: boolean
  error: string
}

const props = defineProps<{
  show: boolean
  agentUserId: number | null
}>()

const emit = defineEmits<{
  close: []
  saved: []
}>()

const { t } = useI18n()
const appStore = useAppStore()

const loading = ref(false)
const loadFailed = ref(false)
const groupsLoading = ref(false)
const submitting = ref(false)
const detail = ref<AgentDetail | null>(null)
const groupRows = ref<GroupCostRow[]>([])

// 输入框都是 type=number：直接 v-model 在未输入时会拿到 ''，输入后是 string，
// 因此字段类型写成 string | number，取值时统一 String() 后解析。
const form = reactive<{
  enabled: boolean
  /** '' 表示未设置默认下级折扣。 */
  defaultRate: string | number
  frozenHours: string | number
  note: string
}>({
  enabled: true,
  defaultRate: '',
  frozenHours: '',
  note: '',
})

function resetState() {
  detail.value = null
  groupRows.value = []
  loading.value = false
  loadFailed.value = false
  groupsLoading.value = false
  submitting.value = false
  Object.assign(form, { enabled: true, defaultRate: '', frozenHours: '', note: '' })
}

/** 后端倍率 → 输入框字符串；null / 缺失 / 非有限数一律视为「未设置」。 */
function toRateInput(value: number | null | undefined): string {
  return typeof value === 'number' && Number.isFinite(value) ? String(value) : ''
}

/** 后端 group_costs 是 map[string]float64，键在 JSON 里是字符串，这里统一转成数字键。 */
function normalizeGroupCosts(raw: Record<string, number> | null | undefined): Map<number, number> {
  const out = new Map<number, number>()
  for (const [key, value] of Object.entries(raw || {})) {
    const groupId = Number(key)
    if (Number.isFinite(groupId) && groupId > 0 && typeof value === 'number') {
      out.set(groupId, value)
    }
  }
  return out
}

const parsedRate = computed(() => {
  const raw = String(form.defaultRate ?? '').trim()
  if (raw === '') return null
  const value = Number(raw)
  return Number.isFinite(value) ? value : Number.NaN
})

const rateError = computed(() => {
  const value = parsedRate.value
  if (value === null) return ''
  if (!Number.isFinite(value) || value <= 0) return t('agents.detail.invalidRate')
  return ''
})

const frozenHoursError = computed(() => {
  const raw = String(form.frozenHours ?? '').trim()
  const value = raw === '' ? 0 : Number(raw)

  if (!Number.isFinite(value) || !Number.isInteger(value) || value < 0 || value > 720) {
    return t('agents.detail.invalidFrozenHours')
  }
  return ''
})

const costError = computed(() => groupRows.value.find((row) => row.error)?.error || '')

const canSubmit = computed(
  () =>
    !!detail.value
    && !loading.value
    && !submitting.value
    && !rateError.value
    && !frozenHoursError.value
    && !costError.value,
)

function showError(error: unknown) {
  appStore.showError(
    extractI18nErrorMessage(error, t, 'agents.errors', t('agents.detail.saveFailed')),
  )
}

/** 某个分组的成本倍率输入是否有效：'' = 未配置（合法）。 */
function validateCostInput(row: GroupCostRow): string {
  const raw = row.costMultiplier.trim()
  if (raw === '') return ''
  const value = Number(raw)
  if (!Number.isFinite(value) || value <= 0) {
    return t('agents.detail.invalidCostRate', { group: row.groupName })
  }
  return ''
}

function updateCostMultiplier(row: GroupCostRow, value: string) {
  row.costMultiplier = value
  row.error = validateCostInput(row)
}

/** 已配置但已从分组表里消失的分组（通常是被删除了），补一行只读提示。 */
function appendOrphanRows(configured: Map<number, number>) {
  const known = new Set(groupRows.value.map((row) => row.groupId))
  for (const [groupId, value] of configured) {
    if (known.has(groupId)) continue
    groupRows.value.push({
      groupId,
      groupName: t('agents.detail.groupCosts.unknownGroup', { id: groupId }),
      groupRate: 0,
      costMultiplier: String(value),
      inactive: true,
      error: '',
    })
  }
}

async function loadDetail(agentUserId: number) {
  loading.value = true
  loadFailed.value = false
  try {
    const data = await agentsAPI.getAgent(agentUserId)
    detail.value = data
    Object.assign(form, {
      enabled: data.enabled ?? true,
      // @/types 里该字段是 number | null（不是可选），null 表示未设置默认折扣。
      defaultRate: toRateInput(data.default_child_rate_multiplier),
      frozenHours: String(data.commission_frozen_hours ?? 0),
      note: data.note || '',
    })
  } catch (error) {
    loadFailed.value = true
    showError(error)
  } finally {
    loading.value = false
  }
}

async function loadGroups() {
  groupsLoading.value = true
  try {
    // 用含停用的分组列表：已停用分组上仍可能有历史成本折扣，管理员需要看到并清理它。
    const groups = await groupsAPI.getAllIncludingInactive()
    const configured = normalizeGroupCosts(detail.value?.group_costs)
    groupRows.value = (groups || []).map((group) => {
      const configuredValue = configured.get(group.id)
      return {
        groupId: group.id,
        groupName: group.name,
        platform: group.platform,
        groupRate: group.rate_multiplier,
        costMultiplier: configuredValue === undefined ? '' : String(configuredValue),
        inactive: group.status !== 'active',
        error: '',
      }
    })
    // 严格按成本倍率排序会让管理员找不到分组，保持接口顺序并追加孤儿行。
    appendOrphanRows(configured)
  } catch (error) {
    groupRows.value = []
    showError(error)
  } finally {
    groupsLoading.value = false
  }
}

async function reload() {
  resetState()
  const agentUserId = props.agentUserId
  if (!agentUserId) return
  await loadDetail(agentUserId)
  if (detail.value) await loadGroups()
}

async function save() {
  const agentUserId = props.agentUserId
  if (!agentUserId || !detail.value || !canSubmit.value) return

  // 提交前再校验一遍分组输入，避免刚输入完立刻点保存时跳过了 input 事件。
  for (const row of groupRows.value) {
    row.error = validateCostInput(row)
  }
  if (costError.value) return

  submitting.value = true
  try {
    await agentsAPI.updateAgent(agentUserId, {
      enabled: form.enabled,
      // 空值等同于「不设置默认折扣」，避免后端 gt=0 校验报错。
      default_child_rate_multiplier:
        parsedRate.value === null ? null : (parsedRate.value as number),
      commission_frozen_hours: Math.trunc(Number(form.frozenHours) || 0),
      note: form.note,
    })

    // 全量替换语义：只提交有效（>0）的分组，未提交的分组视为清空。
    const items: AgentGroupCostItem[] = []
    for (const row of groupRows.value) {
      const raw = row.costMultiplier.trim()
      if (raw === '') continue
      const value = Number(raw)
      if (Number.isFinite(value) && value > 0) {
        items.push({ group_id: row.groupId, cost_multiplier: value })
      }
    }
    await agentsAPI.replaceGroupCosts(agentUserId, items)

    appStore.showSuccess(t('agents.detail.saved'))
    emit('saved')
    emit('close')
  } catch (error) {
    showError(error)
  } finally {
    submitting.value = false
  }
}

function handleClose() {
  if (submitting.value) return
  emit('close')
}

watch(
  // 同时盯住打开状态与目标代理：列表页先改 id 再置 visible，分开监听会在同一次 flush 里
  // 触发两次 reload（重复请求）。合成一个 key 后每次打开只加载一次。
  () => (props.show ? `${props.agentUserId ?? ''}` : ''),
  (key) => {
    if (!key) {
      resetState()
      return
    }
    void reload()
  },
  { immediate: true },
)
</script>

<style scoped>
/* 隐藏数字输入框的箭头按钮，避免与分组表格的窄输入框挤在一起。 */
.hide-spinner::-webkit-outer-spin-button,
.hide-spinner::-webkit-inner-spin-button {
  -webkit-appearance: none;
  margin: 0;
}
.hide-spinner {
  -moz-appearance: textfield;
}
</style>
