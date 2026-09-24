import { ApiError, NetworkError } from '@/api/client'

/**
 * 把任意异常翻成一句能给用户看的话。
 *
 * 后端的 message 本来就是写给用户的中文（「头像需要是一张能看清正脸的照片」），
 * 所以优先原样透出；只有非业务异常才需要兜底文案。
 */
export function messageOf(err: unknown): string {
  if (err instanceof ApiError) return err.message
  if (err instanceof NetworkError) return '网络连接失败，请检查网络后重试'
  if (err instanceof Error && err.name === 'AbortError') return '已取消'
  return '服务开小差了，请稍后再试'
}

/** 某些错误码需要额外引导（比如被锁了要告诉用户去哪解锁）。 */
export function isErrorCode(err: unknown, code: string): boolean {
  return err instanceof ApiError && err.code === code
}
