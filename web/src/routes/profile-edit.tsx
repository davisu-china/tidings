import { useQuery } from '@tanstack/react-query'
import * as React from 'react'
import { Link, useLoaderData } from 'react-router'

import { listPhotos } from '@/api/media'
import { fetchProfile } from '@/api/profile'
import type { Photo, Profile } from '@/api/types'
import { AvatarUploader, PhotoUploader } from '@/components/PhotoUpload'
import { BasicFields, FigureFields, MoreFields } from '@/components/profile/fields'
import { useProfileForm } from '@/components/profile/form'
import { BASIC_FIELDS, FIGURE_FIELDS, MORE_FIELDS } from '@/components/profile/steps'
import { Button } from '@/components/ui/button'
import { Spinner } from '@/components/ui/spinner'
import { missingLabels } from '@/lib/dict'

export async function profileEditLoader() {
  const [profile, photos] = await Promise.all([fetchProfile(), listPhotos()])
  return { profile, photos }
}

/**
 * 一个区块：标题 + 字段 + 自己的保存按钮。
 *
 * 分段保存而不是一个页面一个「全部保存」：建档向导本来就是分步提交的，
 * 编辑页沿用同一套，用户在「补充」里改一半被叫走，前面几步也已经存上了。
 */
function Block({
  title,
  description,
  dirty,
  saving,
  onSave,
  children,
}: {
  title: string
  description?: string
  dirty: boolean
  saving: boolean
  onSave: () => void
  children: React.ReactNode
}) {
  return (
    <section className="mt-10 border-t border-line pt-8">
      <h2 className="font-serif text-[17px] text-ink">{title}</h2>
      {description && <p className="mt-2 text-[13px] leading-[1.8] text-muted">{description}</p>}
      <div className="mt-6">{children}</div>

      <div className="mt-7 flex items-center gap-4">
        <Button size="sm" variant="outline" onClick={onSave} disabled={!dirty || saving}>
          {saving ? <Spinner /> : '保存'}
        </Button>
        {dirty && !saving && <span className="text-[13px] text-muted">有未保存的改动</span>}
      </div>
    </section>
  )
}

export function ProfileEditPage() {
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

  const { form, save, saving, isDirty } = useProfileForm(profile)
  const {
    control,
    formState: { errors },
  } = form

  const missing = missingLabels(profile.missing_required)

  return (
    <div className="mx-auto max-w-[640px] px-5 py-8">
      <Link to="/me" className="text-[13px] text-muted hover:text-ink-2">
        ← 我的
      </Link>
      <h1 className="mt-4 font-serif text-[22px] leading-tight text-ink">编辑资料</h1>

      <p className="mt-3 text-[13px] leading-[1.8] text-muted">
        档案完成度 <span className="tnum font-mono text-ink-2">{profile.completeness}%</span>
        {missing.length > 0 && <>，还差：{missing.join('、')}</>}
      </p>

      <Block
        title="基本"
        description="昵称、性别、出生年月、城市。这四项决定你会被引荐给谁。"
        dirty={isDirty(BASIC_FIELDS)}
        saving={saving}
        onSave={() => void save(BASIC_FIELDS)}
      >
        <BasicFields control={control} errors={errors} />
      </Block>

      <Block
        title="外形"
        description="身高和学历是硬条件过滤里最常用的两项。"
        dirty={isDirty(FIGURE_FIELDS)}
        saving={saving}
        onSave={() => void save(FIGURE_FIELDS)}
      >
        <FigureFields control={control} errors={errors} />
      </Block>

      <section className="mt-10 border-t border-line pt-8">
        <h2 className="font-serif text-[17px] text-ink">照片</h2>
        <p className="mt-2 text-[13px] leading-[1.8] text-muted">
          照片改完即时生效，不用再点保存。
        </p>

        <div className="mt-6">
          <AvatarUploader avatarUrl={profile.avatar_url} />
        </div>

        <div className="mt-7">
          <PhotoUploader photos={photos} />
        </div>
      </section>

      <Block
        title="补充"
        description="全是选填，但占完成度的 40 分。填得越具体，越容易被人记住。"
        dirty={isDirty(MORE_FIELDS)}
        saving={saving}
        onSave={() => void save(MORE_FIELDS)}
      >
        <MoreFields control={control} errors={errors} />
      </Block>

      <div className="h-8" />
    </div>
  )
}
