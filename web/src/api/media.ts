import { api } from './client'
import type { Photo, PhotoList, UploadTicket } from './types'

/** 签发预签名直传凭据。只发票据不写库 —— 用户可能拿了 URL 却不上传。 */
export function createUploadTicket(ext: string): Promise<UploadTicket> {
  return api.post<UploadTicket>('/media/upload-url', { ext })
}

export interface AvatarResult {
  avatar_key: string
  avatar_url: string
}

/** 登记头像。后端会先做正脸检测，没检出人脸直接报错。 */
export function setAvatar(objectKey: string): Promise<AvatarResult> {
  return api.put<AvatarResult>('/me/avatar', { object_key: objectKey })
}

/**
 * 照片接口的响应外层是 { photos: [...] }，不是裸数组。
 * 在这里拆掉，调用方拿到的就是数组 —— 让每个使用点各写一次 `.photos`
 * 迟早会有人忘（这个类型的原型就是这么错的）。
 */
async function unwrap(req: Promise<PhotoList>): Promise<Photo[]> {
  return (await req).photos
}

export function listPhotos(signal?: AbortSignal): Promise<Photo[]> {
  return unwrap(api.get<PhotoList>('/me/photos', { signal }))
}

export function addPhoto(objectKey: string): Promise<Photo> {
  return api.post<Photo>('/me/photos', { object_key: objectKey })
}

/** 必须提交完整且不重复的 id 列表，只发一部分会被拒。 */
export function reorderPhotos(ids: number[]): Promise<Photo[]> {
  return unwrap(api.put<PhotoList>('/me/photos/order', { ids }))
}

export function deletePhoto(id: number): Promise<{ ok: boolean }> {
  return api.del<{ ok: boolean }>(`/me/photos/${id}`)
}
