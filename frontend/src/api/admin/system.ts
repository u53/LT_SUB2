/**
 * System API endpoints for admin operations
 *
 * 本仓库是独立二次开发分支，不提供在线更新/回滚能力：
 * 在线更新会把运行中的二进制换回上游版本，导致本地功能丢失。
 * 版本号通过公开设置（site_version）展示，升级由运维手动构建与部署。
 */

import { apiClient } from '../client'

export interface VersionInfo {
  version: string
}

/**
 * Get current version
 */
export async function getVersion(): Promise<VersionInfo> {
  const { data } = await apiClient.get<VersionInfo>('/admin/system/version')
  return data
}

/**
 * Restart the service
 */
export async function restartService(): Promise<{ message: string }> {
  const { data } = await apiClient.post<{ message: string }>('/admin/system/restart')
  return data
}

export const systemAPI = {
  getVersion,
  restartService
}

export default systemAPI
