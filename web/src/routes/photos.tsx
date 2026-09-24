import { useQuery } from '@tanstack/react-query'
import { Link, useLoaderData } from 'react-router'

import { listPhotos } from '@/api/media'
import { fetchProfile } from '@/api/profile'
import type { Photo, Profile } from '@/api/types'
import { AvatarUploader, PhotoUploader } from '@/components/PhotoUpload'

export async function photosLoader() {
  const [profile, photos] = await Promise.all([fetchProfile(), listPhotos()])
  return { profile, photos }
}

/**
 * 照片管理。九宫格 + 第一张即封面。
 *
 * 拖拽排序在 SortablePhotoGrid 里：@dnd-kit 的三套传感器（鼠标 8px 起拖、
 * 触屏按住 200ms 才算拖、键盘空格起落），顺序即存即发，没有「保存顺序」按钮。
 */
export function PhotosPage() {
  const initial = useLoaderData() as { profile: Profile; photos: Photo[] }

  const { data: profile } = useQuery({
    queryKey: ['profile'],
    queryFn: () => fetchProfile(),
    initialData: initial.profile,
  })
  const { data: photos } = useQuery({
    queryKey: ['photos'],
    queryFn: () => listPhotos(),
    initialData: initial.photos,
  })

  return (
    <div className="mx-auto max-w-[640px] px-5 py-8">
      <Link to="/me" className="text-[13px] text-muted hover:text-ink-2">
        ← 我的
      </Link>
      <h1 className="mt-4 font-serif text-[22px] leading-tight text-ink">照片</h1>

      <div className="mt-8">
        <AvatarUploader avatarUrl={profile.avatar_url} />
      </div>

      <div className="mt-10 border-t border-line pt-8">
        <h2 className="font-serif text-[17px] text-ink">相册</h2>
        <p className="mt-2 text-[13px] leading-[1.8] text-muted">
          最多 9 张。第一张会出现在引荐卡上。
        </p>
        <div className="mt-6">
          <PhotoUploader photos={photos} />
        </div>
      </div>
    </div>
  )
}
