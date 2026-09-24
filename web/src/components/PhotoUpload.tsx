import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Plus } from 'lucide-react'
import * as React from 'react'

import { addPhoto, deletePhoto, reorderPhotos, setAvatar } from '@/api/media'
import type { Photo } from '@/api/types'
import { SortablePhotoGrid } from '@/components/SortablePhotoGrid'
import { Spinner } from '@/components/ui/spinner'
import { useToast } from '@/components/ui/toast'
import { cn } from '@/lib/cn'
import { messageOf } from '@/lib/errors'
import { uploadImage } from '@/lib/upload'

const MAX_PHOTOS = 9

/** 隐藏的文件选择框。accept 只放 JPEG/PNG —— 与后端 allowedExt 一致。 */
function useFilePicker(label: string, onPick: (file: File) => void) {
  const ref = React.useRef<HTMLInputElement>(null)

  const open = React.useCallback(() => ref.current?.click(), [])

  const input = (
    <input
      ref={ref}
      type="file"
      accept="image/jpeg,image/png"
      // 它是隐藏的，视觉上由外面的按钮代表；没有 aria-label 的话
      // 读屏软件和自动化都只能看到一个没有名字的文件框
      aria-label={label}
      className="hidden"
      onChange={(e) => {
        const file = e.target.files?.[0]
        // 先清空 value：不然连续选同一个文件不会触发 change
        e.target.value = ''
        if (file) onPick(file)
      }}
    />
  )

  return { open, input }
}

/** 上传中的进度条。一条 2px 的 accent 线，不是转圈 —— 大图上传要能看见走了多远。 */
function ProgressBar({ percent }: { percent: number | null }) {
  if (percent === null) return null
  return (
    <div className="absolute inset-x-0 bottom-0 h-[2px] bg-line-soft">
      <div className="h-full bg-accent transition-[width] duration-200" style={{ width: `${percent}%` }} />
    </div>
  )
}

/**
 * 头像。必须能检出正脸，否则后端拒绝（真实人像检测是 M1 唯一
 * 真正在跑的自动门槛，见 §4.1）。
 */
export function AvatarUploader({ avatarUrl }: { avatarUrl: string }) {
  const qc = useQueryClient()
  const toast = useToast()
  const [percent, setPercent] = React.useState<number | null>(null)

  const mutation = useMutation({
    mutationFn: async (file: File) => {
      const key = await uploadImage(file, setPercent)
      return setAvatar(key)
    },
    onMutate: () => setPercent(0),
    onSettled: () => setPercent(null),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['profile'] })
    },
    onError: (err) => toast.show(messageOf(err)),
  })

  const { open, input } = useFilePicker('选择头像', (file) => mutation.mutate(file))

  return (
    <div className="flex items-center gap-5">
      <button
        type="button"
        onClick={open}
        disabled={mutation.isPending}
        aria-label={avatarUrl ? '更换头像' : '上传头像'}
        className={cn(
          'relative h-[88px] w-[88px] shrink-0 overflow-hidden rounded-card border border-line',
          'bg-surface-2 transition-colors duration-150 hover:border-ink-2',
          'focus-visible:outline-none focus-visible:border-accent',
        )}
      >
        {avatarUrl ? (
          <img src={avatarUrl} alt="" className="h-full w-full object-cover" />
        ) : (
          <span className="flex h-full w-full items-center justify-center text-muted">
            <Plus aria-hidden className="h-5 w-5" strokeWidth={1.5} />
          </span>
        )}
        {mutation.isPending && (
          <span className="absolute inset-0 flex items-center justify-center bg-paper/70 text-accent">
            <Spinner />
          </span>
        )}
        <ProgressBar percent={percent} />
      </button>

      <div className="min-w-0">
        <p className="text-[14px] text-ink-2">
          {avatarUrl ? '头像已就位' : '上传一张能看清正脸的照片'}
        </p>
        <p className="mt-1 text-[13px] leading-[1.7] text-muted">
          它是引荐卡上唯一的脸。
          <br />
          背影、远景、多人合照都会被判为不合格。
        </p>
        <button
          type="button"
          onClick={open}
          disabled={mutation.isPending}
          className="mt-2 text-[13px] text-accent hover:text-accent-ink disabled:opacity-45"
        >
          {avatarUrl ? '换一张' : '选择照片'}
        </button>
      </div>

      {input}
    </div>
  )
}

/**
 * 照片九宫格。至少 3 张才入池（照片单独判，不参与完成度）。
 *
 * 第一张即封面 —— 后端按 position = 0 取，所以排序就是选封面，
 * 不另做「设为主图」按钮。
 */
export function PhotoUploader({
  photos,
  onChanged,
}: {
  photos: Photo[]
  /** 照片或头像变动后回调。列表本身靠 query 失效刷新，这个钩子留给调用方做局部判断 */
  onChanged?: () => void
}) {
  const qc = useQueryClient()
  const toast = useToast()
  const [percent, setPercent] = React.useState<number | null>(null)
  const [deleting, setDeleting] = React.useState<number | null>(null)

  const upload = useMutation({
    mutationFn: async (file: File) => {
      const key = await uploadImage(file, setPercent)
      return addPhoto(key)
    },
    onMutate: () => setPercent(0),
    onSettled: () => setPercent(null),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['photos'] })
      qc.invalidateQueries({ queryKey: ['profile'] })
      onChanged?.()
    },
    onError: (err) => toast.show(messageOf(err)),
  })

  const remove = useMutation({
    mutationFn: (id: number) => deletePhoto(id),
    onMutate: (id) => setDeleting(id),
    onSettled: () => setDeleting(null),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['photos'] })
      qc.invalidateQueries({ queryKey: ['profile'] })
      onChanged?.()
    },
    onError: (err) => toast.show(messageOf(err)),
  })

  const reorder = useMutation({
    mutationFn: (ids: number[]) => reorderPhotos(ids),
    onSuccess: (next) => qc.setQueryData(['photos'], next),
    onError: (err) => {
      // 本地顺序已经改了，得让服务端的版本盖回来，否则界面在撒谎
      qc.invalidateQueries({ queryKey: ['photos'] })
      toast.show(messageOf(err))
    },
  })

  const { open, input } = useFilePicker('添加照片', (file) => upload.mutate(file))

  return (
    <div>
      <SortablePhotoGrid
        photos={photos}
        deleting={deleting}
        maxPhotos={MAX_PHOTOS}
        uploading={upload.isPending}
        percent={percent}
        onDelete={(id) => remove.mutate(id)}
        onReorder={(ids) => reorder.mutate(ids)}
        onAdd={open}
      />

      <p className="mt-3 text-[13px] leading-[1.7] text-muted">
        {photos.length < 3 ? (
          <>
            已选 <span className="tnum font-mono text-ink-2">{photos.length}</span> 张，
            还差 <span className="tnum font-mono text-ink-2">{3 - photos.length}</span> 张才够入池。
          </>
        ) : (
          <>
            已选 <span className="tnum font-mono text-ink-2">{photos.length}</span> 张。
            第一张是封面。
          </>
        )}
        {photos.length > 1 && <><br />按住拖动可以换顺序。</>}
      </p>

      {input}
    </div>
  )
}
