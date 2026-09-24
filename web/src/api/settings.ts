import { api } from './client'
import type { Settings, SettingsInput } from './types'

export function fetchSettings(signal?: AbortSignal): Promise<Settings> {
  return api.get<Settings>('/me/settings', { signal })
}

/**
 * 保存设置。
 *
 * 三个字段必须全传（后端据此区分「没传」和「传了 0」—— 静默时段的
 * 两个小时数里 0 点是合法取值）。所以调用方要提交完整的一份，
 * 不能只提交改动的那一项。
 */
export function saveSettings(input: SettingsInput): Promise<Settings> {
  return api.put<Settings>('/me/settings', input)
}
