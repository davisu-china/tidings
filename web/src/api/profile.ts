import { api } from './client'
import type { Profile, ProfileInput } from './types'

export function fetchProfile(signal?: AbortSignal): Promise<Profile> {
  return api.get<Profile>('/me/profile', { signal })
}

/**
 * 保存资料。只提交这一步改动的字段 —— 后端按 PATCH 语义合并，
 * 没传的字段保持不变，所以分步向导每步带自己那几个字段就行。
 */
export function saveProfile(input: ProfileInput): Promise<Profile> {
  return api.patch<Profile>('/me/profile', input)
}
