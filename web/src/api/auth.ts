import { api } from './client'
import type { LoginResult, Me, TokenPair } from './types'

export function register(email: string, password: string): Promise<LoginResult> {
  return api.post<LoginResult>('/auth/register', { email, password })
}

export function login(email: string, password: string): Promise<LoginResult> {
  return api.post<LoginResult>('/auth/login', { email, password })
}

export function logout(refreshToken: string): Promise<unknown> {
  return api.post('/auth/logout', { refresh_token: refreshToken })
}

/** 当前登录态。路由 loader 靠它决定去建档向导还是首页。 */
export function fetchMe(signal?: AbortSignal): Promise<Me> {
  return api.get<Me>('/me', { signal })
}

export type { TokenPair }
