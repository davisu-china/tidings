import type { TokenPair } from '@/api/types'

// 凭据存 localStorage。这样刷新页面不必重新登录，多标签页也能共用。
//
// 已知代价：localStorage 对 XSS 无免疫，脚本能读到这两个 token。
// 换成 httpOnly cookie 需要后端配合（CSRF、跨域、SameSite 一整套），
// MVP 阶段接受这一条，前提是全站没有第三方脚本、且不渲染用户提供的 HTML。
const ACCESS_KEY = 'tidings.access'
const REFRESH_KEY = 'tidings.refresh'

export function getAccessToken(): string | null {
  return localStorage.getItem(ACCESS_KEY)
}

export function getRefreshToken(): string | null {
  return localStorage.getItem(REFRESH_KEY)
}

export function saveTokens(pair: TokenPair): void {
  localStorage.setItem(ACCESS_KEY, pair.access_token)
  localStorage.setItem(REFRESH_KEY, pair.refresh_token)
}

export function clearTokens(): void {
  localStorage.removeItem(ACCESS_KEY)
  localStorage.removeItem(REFRESH_KEY)
}

export function hasSession(): boolean {
  return getRefreshToken() !== null
}
