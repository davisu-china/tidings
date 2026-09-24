import { api } from './client'
import type { Preference, PreferenceInput } from './types'

export function fetchPreference(signal?: AbortSignal): Promise<Preference> {
  return api.get<Preference>('/me/preferences', { signal })
}

/**
 * 保存偏好。
 *
 * PUT 而不是 PATCH：偏好是「我当前的要求」，没有历史价值，也不需要
 * 区分「没传这个字段」和「显式清空」—— 两者都是不限。所以每次都要
 * 把整份偏好提交上去，而不是只提交改过的那几项。
 */
export function savePreference(input: PreferenceInput): Promise<Preference> {
  return api.put<Preference>('/me/preferences', input)
}
