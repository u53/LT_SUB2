<template>
  <AppLayout>
    <div class="space-y-6">
      <div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h1 class="text-xl font-semibold text-gray-900 dark:text-white" data-testid="agent-center-title">
            {{ t('agentCenter.title') }}
          </h1>
          <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">{{ t('agentCenter.description') }}</p>
        </div>
        <button
          type="button"
          class="btn btn-secondary shrink-0"
          :disabled="profileLoading"
          :title="t('agentCenter.refresh')"
          data-testid="agent-refresh"
          @click="refreshAll"
        >
          <Icon name="refresh" size="md" :class="profileLoading ? 'animate-spin' : ''" />
        </button>
      </div>

      <div v-if="profileLoading && !profile" class="flex justify-center py-16">
        <div class="h-8 w-8 animate-spin rounded-full border-2 border-primary-500 border-t-transparent"></div>
      </div>

      <!-- 不是代理：接口会返回 404（AGENT_PROFILE_NOT_FOUND），给出明确引导而不是空白页 -->
      <div v-else-if="notAgent" class="card p-8 text-center" data-testid="agent-not-agent">
        <Icon name="userCircle" size="xl" class="mx-auto text-gray-400 dark:text-dark-500" />
        <p class="mt-4 text-base font-semibold text-gray-900 dark:text-white">
          {{ t('agentCenter.notAgent.title') }}
        </p>
        <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">{{ t('agentCenter.notAgent.description') }}</p>
      </div>

      <template v-else-if="profile">
        <AgentStatsPanel
          :profile="profile"
          :invite-count="inviteCount"
          @withdraw="withdrawOpen = true"
        />

        <AgentInviteCard :aff-code="profile.aff_code" />

        <AgentChildrenPanel
          :items="children.items"
          :total="children.total"
          :page="children.page"
          :page-size="children.pageSize"
          :loading="children.loading"
          :disabled="!profile.enabled"
          @update:page="onChildrenPageChange"
          @set-rate="openRateDialog"
          @refresh="loadChildren"
        />

        <AgentCommissionsPanel
          :items="commissions.items"
          :total="commissions.total"
          :page="commissions.page"
          :page-size="commissions.pageSize"
          :loading="commissions.loading"
          :child-filter="commissionFilters.childId"
          :status-filter="commissionFilters.status"
          :start-at="commissionFilters.startAt"
          :end-at="commissionFilters.endAt"
          :child-options="commissionChildOptions"
          @update:page="onCommissionsPageChange"
          @update:child-filter="onCommissionChildChange"
          @update:status-filter="onCommissionStatusChange"
          @update:start-at="onCommissionStartChange"
          @update:end-at="onCommissionEndChange"
          @clear-filters="clearCommissionFilters"
          @refresh="loadCommissions"
        />

        <AgentWithdrawalsPanel
          :items="withdrawals.items"
          :total="withdrawals.total"
          :page="withdrawals.page"
          :page-size="withdrawals.pageSize"
          :loading="withdrawals.loading"
          @update:page="onWithdrawalsPageChange"
          @refresh="loadWithdrawals"
        />

        <AgentWithdrawDialog
          :show="withdrawOpen"
          :available="profile.available"
          :min-withdraw="profile.min_withdraw"
          @close="withdrawOpen = false"
          @submitted="onWithdrawSubmitted"
        />

        <AgentChildRateDialog
          :show="rateDialogOpen"
          :child="rateDialogChild"
          :groups="rateGroups"
          @close="rateDialogOpen = false"
          @saved="onRateSaved"
        />
      </template>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import userGroupsAPI from '@/api/groups'
import {
  getAgentProfile,
  listAgentChildren,
  listAgentCommissions,
  listAgentWithdrawals,
  type AgentChild,
  type AgentCommissionRecord,
  type AgentCommissionStatus,
  type AgentSelfProfile,
  type AgentWithdrawal,
} from '@/api/agent'
import { useAppStore } from '@/stores/app'
import { extractApiErrorCode, extractApiErrorMessage } from '@/utils/apiError'
import AgentStatsPanel from './agent/AgentStatsPanel.vue'
import AgentInviteCard from './agent/AgentInviteCard.vue'
import AgentChildrenPanel from './agent/AgentChildrenPanel.vue'
import AgentCommissionsPanel from './agent/AgentCommissionsPanel.vue'
import AgentWithdrawalsPanel from './agent/AgentWithdrawalsPanel.vue'
import AgentWithdrawDialog from './agent/AgentWithdrawDialog.vue'
import AgentChildRateDialog from './agent/AgentChildRateDialog.vue'
import type { AgentRateGroupOption } from './agent/agentCenterUtils'

const { t } = useI18n()
const appStore = useAppStore()

const profileLoading = ref(true)
const profile = ref<AgentSelfProfile | null>(null)
// 非代理账号不是错误状态，渲染引导卡片而不是报错 toast。
const notAgent = ref(false)

const withdrawOpen = ref(false)
const rateDialogOpen = ref(false)
const rateDialogChild = ref<AgentChild | null>(null)
const rateGroups = ref<AgentRateGroupOption[]>([])

interface PagedState<T> {
  items: T[]
  total: number
  page: number
  pageSize: number
  loading: boolean
}

const children = reactive<PagedState<AgentChild>>({
  items: [],
  total: 0,
  page: 1,
  pageSize: 20,
  loading: false,
})
const commissions = reactive<PagedState<AgentCommissionRecord>>({
  items: [],
  total: 0,
  page: 1,
  pageSize: 20,
  loading: false,
})
const withdrawals = reactive<PagedState<AgentWithdrawal>>({
  items: [],
  total: 0,
  page: 1,
  pageSize: 20,
  loading: false,
})

/** 下级筛选下拉的候选：取当前已加载的一页；分页之外的靠弹窗改价，不在这里穷举。 */
const commissionChildOptions = computed(() => children.items)

const commissionFilters = reactive<{
  childId: number | ''
  status: AgentCommissionStatus | ''
  startAt: string
  endAt: string
}>({
  childId: '',
  status: '',
  startAt: '',
  endAt: '',
})

// 老后端没有 invite_count 时回落到下级列表的 total，避免卡片显示 0。
const inviteCount = computed(() => profile.value?.invite_count ?? children.total)

/**
 * 判断「这个账号不是代理」而不是加载失败。
 *
 * 后端有两层拦截：路由层 requireAgentRole 对非 agent 角色直接 403
 * （backend/internal/server/routes/user.go），service 层在没有代理档案时返回
 * 404 AGENT_PROFILE_NOT_FOUND。两者都应当渲染引导卡片，而不是弹错误 toast。
 */
function isNotAgentError(error: unknown): boolean {
  if (extractApiErrorCode(error) === 'AGENT_PROFILE_NOT_FOUND') return true
  const status = (error as { status?: number } | null)?.status
  return status === 403
}

async function loadProfile(silent = false): Promise<void> {
  if (!silent) profileLoading.value = true
  try {
    profile.value = await getAgentProfile()
    notAgent.value = false
  } catch (error) {
    // 非代理有两种正常响应：路由层 requireAgentRole 直接 403（无代理角色），
    // 或 service 层 404 AGENT_PROFILE_NOT_FOUND（有角色但没档案）。都不是错误状态。
    if (isNotAgentError(error)) {
      notAgent.value = true
      profile.value = null
    } else {
      appStore.showError(extractApiErrorMessage(error, t('agentCenter.loadFailed')))
    }
  } finally {
    profileLoading.value = false
  }
}

async function loadChildren(): Promise<void> {
  children.loading = true
  try {
    const res = await listAgentChildren({ page: children.page, page_size: children.pageSize })
    children.items = res.items ?? []
    children.total = res.total ?? 0
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('agentCenter.errors.unknown')))
  } finally {
    children.loading = false
  }
}

async function loadCommissions(): Promise<void> {
  commissions.loading = true
  try {
    const res = await listAgentCommissions({
      page: commissions.page,
      page_size: commissions.pageSize,
      child_user_id: commissionFilters.childId === '' ? undefined : commissionFilters.childId,
      status: commissionFilters.status || undefined,
      start_at: commissionFilters.startAt || undefined,
      end_at: commissionFilters.endAt || undefined,
    })
    commissions.items = res.items ?? []
    commissions.total = res.total ?? 0
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('agentCenter.errors.unknown')))
  } finally {
    commissions.loading = false
  }
}

async function loadWithdrawals(): Promise<void> {
  withdrawals.loading = true
  try {
    const res = await listAgentWithdrawals({
      page: withdrawals.page,
      page_size: withdrawals.pageSize,
    })
    withdrawals.items = res.items ?? []
    withdrawals.total = res.total ?? 0
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('agentCenter.errors.unknown')))
  } finally {
    withdrawals.loading = false
  }
}

/** 分组下拉：用用户侧可访问的 /groups/available；失败不阻塞页面，只是不提供按分组设置。 */
async function loadRateGroups(): Promise<void> {
  try {
    const groups = await userGroupsAPI.getAvailable()
    rateGroups.value = (groups || []).map((group) => ({ id: group.id, name: group.name }))
  } catch {
    rateGroups.value = []
  }
}

async function refreshAll(): Promise<void> {
  await loadProfile()
  if (!profile.value) return
  await Promise.all([loadChildren(), loadCommissions(), loadWithdrawals(), loadRateGroups()])
}

function onChildrenPageChange(page: number): void {
  children.page = page
  void loadChildren()
}

function onCommissionsPageChange(page: number): void {
  commissions.page = page
  void loadCommissions()
}

function onWithdrawalsPageChange(page: number): void {
  withdrawals.page = page
  void loadWithdrawals()
}

function onCommissionChildChange(value: number | ''): void {
  commissionFilters.childId = value
  commissions.page = 1
  void loadCommissions()
}

function onCommissionStatusChange(value: AgentCommissionStatus | ''): void {
  commissionFilters.status = value
  commissions.page = 1
  void loadCommissions()
}

function onCommissionStartChange(value: string): void {
  commissionFilters.startAt = value
  commissions.page = 1
  void loadCommissions()
}

function onCommissionEndChange(value: string): void {
  commissionFilters.endAt = value
  commissions.page = 1
  void loadCommissions()
}

function clearCommissionFilters(): void {
  commissionFilters.childId = ''
  commissionFilters.status = ''
  commissionFilters.startAt = ''
  commissionFilters.endAt = ''
  commissions.page = 1
  void loadCommissions()
}

function openRateDialog(child: AgentChild): void {
  rateDialogChild.value = child
  rateDialogOpen.value = true
}

async function onRateSaved(): Promise<void> {
  await Promise.all([loadChildren(), loadProfile(true)])
}

async function onWithdrawSubmitted(): Promise<void> {
  // 提现会同时改动可提现余额与提现记录，两个都刷新。
  await Promise.all([loadProfile(true), loadWithdrawals(), loadChildren()])
}

onMounted(() => {
  void refreshAll()
})
</script>
