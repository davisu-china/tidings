const MAX_EDGE = 2000
const TARGET_BYTES = 2 << 20 // 2MB
const QUALITY = 0.85

/**
 * 上传前压一道。
 *
 * 手机相册里的照片动辄 4–8MB，而后端最终只留三档 ≤1080w 的派生图 ——
 * 把原图整张传上去，耗的是用户的流量和等待时间。压到 2MB 以内之后，
 * 输出质量与派生图没有可见差别。
 *
 * 解码失败时原样返回文件：iOS 相册里的 HEIC 在某些版本上解不开，
 * 这种时候不该假装成功，也不该在客户端把它丢掉 —— 交给服务端判，
 * 用户至少能看到一条明确的错误。
 */
export async function prepareUpload(file: File): Promise<File> {
  if (!file.type.startsWith('image/')) return file
  // 已经足够小的不动它，省掉一次无谓的解码
  if (file.size <= TARGET_BYTES) return file

  let bitmap: ImageBitmap
  try {
    bitmap = await createImageBitmap(file)
  } catch {
    return file
  }

  try {
    const scale = Math.min(1, MAX_EDGE / Math.max(bitmap.width, bitmap.height))
    const width = Math.round(bitmap.width * scale)
    const height = Math.round(bitmap.height * scale)

    const canvas = document.createElement('canvas')
    canvas.width = width
    canvas.height = height
    const ctx = canvas.getContext('2d')
    if (!ctx) return file

    ctx.drawImage(bitmap, 0, 0, width, height)

    const blob = await new Promise<Blob | null>((resolve) =>
      canvas.toBlob(resolve, 'image/jpeg', QUALITY),
    )
    // 压完反而更大就别换了（小图重编码偶尔会这样）
    if (!blob || blob.size >= file.size) return file

    return new File([blob], renameToJpg(file.name), {
      type: 'image/jpeg',
      lastModified: file.lastModified,
    })
  } finally {
    bitmap.close()
  }
}

/** 重编码之后扩展名必须跟着变，否则预签名票据签的是 .png、内容是 JPEG。 */
function renameToJpg(name: string): string {
  const base = name.replace(/\.[^.]+$/, '')
  return `${base || 'photo'}.jpg`
}

/** 从文件名或 MIME 推扩展名。服务端只收 .jpg/.jpeg/.png。 */
export function extensionOf(file: File): string {
  const fromName = file.name.toLowerCase().match(/\.(jpe?g|png)$/)?.[0]
  if (fromName) return fromName
  if (file.type === 'image/png') return '.png'
  return '.jpg'
}

export function isSupportedImage(file: File): boolean {
  const name = file.name.toLowerCase()
  return (
    file.type === 'image/jpeg' ||
    file.type === 'image/png' ||
    /\.(jpe?g|png)$/.test(name)
  )
}
