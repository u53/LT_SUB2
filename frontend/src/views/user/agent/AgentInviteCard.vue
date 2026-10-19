<template>
  <div class="card p-6" data-testid="agent-invite">
    <h3 class="text-base font-semibold text-gray-900 dark:text-white">
      {{ t('agentCenter.invite.title') }}
    </h3>
    <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">
      {{ t('agentCenter.invite.description') }}
    </p>

    <p
      v-if="!affCode"
      class="mt-4 rounded-xl border border-dashed border-amber-300 bg-amber-50 px-4 py-3 text-sm text-amber-800 dark:border-amber-900/40 dark:bg-amber-900/20 dark:text-amber-200"
      data-testid="agent-invite-missing"
    >
      {{ t('agentCenter.invite.missingCode') }}
    </p>

    <div v-else class="mt-5 grid gap-4 md:grid-cols-2">
      <div class="space-y-2">
        <p class="text-sm font-medium text-gray-700 dark:text-gray-300">
          {{ t('agentCenter.invite.code') }}
        </p>
        <div class="flex flex-col items-stretch gap-2 rounded-xl border border-gray-200 bg-gray-50 px-3 py-2 dark:border-dark-700 dark:bg-dark-900 sm:flex-row sm:items-center">
          <code class="min-w-0 break-all text-sm font-semibold text-gray-900 dark:text-white sm:flex-1 sm:truncate" data-testid="agent-aff-code">{{ affCode }}</code>
          <button
            type="button"
            class="btn btn-secondary btn-sm w-full sm:w-auto sm:shrink-0"
            data-testid="agent-copy-code"
            @click="copyCode"
          >
            <Icon name="copy" size="sm" />
            <span>{{ t('agentCenter.invite.copyCode') }}</span>
          </button>
        </div>
      </div>

      <div class="space-y-2">
        <p class="text-sm font-medium text-gray-700 dark:text-gray-300">
          {{ t('agentCenter.invite.link') }}
        </p>
        <div class="flex flex-col items-stretch gap-2 rounded-xl border border-gray-200 bg-gray-50 px-3 py-2 dark:border-dark-700 dark:bg-dark-900 sm:flex-row sm:items-center">
          <code class="min-w-0 break-all text-sm text-gray-700 dark:text-gray-300 sm:flex-1 sm:truncate" data-testid="agent-invite-link">{{ inviteLink }}</code>
          <button
            type="button"
            class="btn btn-secondary btn-sm w-full sm:w-auto sm:shrink-0"
            data-testid="agent-copy-link"
            @click="copyLink"
          >
            <Icon name="copy" size="sm" />
            <span>{{ t('agentCenter.invite.copyLink') }}</span>
          </button>
        </div>
      </div>
    </div>

    <div
      v-if="affCode"
      class="mt-5 rounded-xl border border-primary-200 bg-primary-50 p-4 dark:border-primary-900/40 dark:bg-primary-900/20"
    >
      <p class="flex items-start gap-2 text-sm text-primary-700 dark:text-primary-300">
        <Icon name="infoCircle" size="sm" class="mt-0.5 shrink-0" />
        <span>{{ t('agentCenter.invite.hint') }}</span>
      </p>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import { useClipboard } from '@/composables/useClipboard'

const props = defineProps<{ affCode: string }>()

const { t } = useI18n()
const { copyToClipboard } = useClipboard()

// 邀请链接与 AffiliateView 保持一致：/register?aff=<code>。
const inviteLink = computed(() => {
  const code = props.affCode
  if (!code) return ''
  if (typeof window === 'undefined') return `/register?aff=${encodeURIComponent(code)}`
  return `${window.location.origin}/register?aff=${encodeURIComponent(code)}`
})

async function copyCode(): Promise<void> {
  if (!props.affCode) return
  await copyToClipboard(props.affCode, t('agentCenter.invite.codeCopied'))
}

async function copyLink(): Promise<void> {
  if (!inviteLink.value) return
  await copyToClipboard(inviteLink.value, t('agentCenter.invite.linkCopied'))
}
</script>
