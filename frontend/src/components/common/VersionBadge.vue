<template>
  <!--
    只读版本徽章。

    本仓库是独立二次开发分支：在线更新会把运行中的二进制换回上游版本，
    导致本地功能（代理佣金、人民币计费、品牌定制等）消失。因此这里不再提供
    检查更新、更新、回滚入口，只展示当前版本号；升级由运维手动构建与部署。
  -->
  <span
    v-if="displayVersion"
    class="inline-flex items-center rounded-lg bg-gray-100 px-2 py-1 text-xs font-medium text-gray-600 dark:bg-dark-800 dark:text-dark-400"
    :title="t('version.currentVersion')"
  >
    v{{ displayVersion }}
  </span>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores'

const { t } = useI18n()

const props = defineProps<{
  version?: string
}>()

const appStore = useAppStore()

// 优先使用父组件传入的版本（侧边栏取公开设置里的 version），
// 退回 store 中的站点版本，两者都没有时不渲染，避免显示空徽章。
const displayVersion = computed(() => props.version || appStore.siteVersion || '')
</script>
