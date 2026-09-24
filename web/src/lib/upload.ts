import { ApiError, uploadFile } from '@/api/client'
import { createUploadTicket } from '@/api/media'

import { extensionOf, isSupportedImage, prepareUpload } from './image'

/**
 * 上传一张图，返回它的 object_key。
 *
 * 三步：签票据 → 直传 → 拿到 key。注意这一步**还没有登记**，
 * 登记是调用方的事（头像走 PUT /me/avatar，照片走 POST /me/photos）——
 * 两者对图片的要求不同，头像要过正脸检测，照片不用。
 */
export async function uploadImage(
  file: File,
  onProgress?: (percent: number) => void,
): Promise<string> {
  if (!isSupportedImage(file)) {
    throw new ApiError(415, 'UPLOAD_BAD_TYPE', '只支持 JPEG 和 PNG 图片', null)
  }

  const prepared = await prepareUpload(file)
  const ticket = await createUploadTicket(extensionOf(prepared))

  if (prepared.size > ticket.max_bytes) {
    throw new ApiError(413, 'UPLOAD_TOO_LARGE', '图片超出大小限制', null)
  }

  await uploadFile(ticket.upload_url, prepared, onProgress)
  return ticket.object_key
}
