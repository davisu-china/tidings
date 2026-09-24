import { api } from './client'
import type { Profile, ProfileInput, SchoolItem } from './types'

export function fetchProfile(signal?: AbortSignal): Promise<Profile> {
  return api.get<Profile>('/me/profile', { signal })
}

/**
 * 院校库联想。q 为空返回空列表而不是热门排行 —— 没输入时不该弹一层
 * 用户没要的东西出来。
 */
export function searchSchools(
  q: string,
  limit = 10,
  signal?: AbortSignal,
): Promise<{ items: SchoolItem[]; total: number }> {
  return api.get<{ items: SchoolItem[]; total: number }>(
    `/schools?q=${encodeURIComponent(q)}&limit=${limit}`,
    { signal },
  )
}

/**
 * 保存资料。只提交这一步改动的字段 —— 后端按 PATCH 语义合并，
 * 没传的字段保持不变，所以分步向导每步带自己那几个字段就行。
 */
export function saveProfile(input: ProfileInput): Promise<Profile> {
  return api.patch<Profile>('/me/profile', input)
}
