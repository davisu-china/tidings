import { useQuery } from '@tanstack/react-query'
import * as React from 'react'
import { redirect, useLoaderData, useNavigate } from 'react-router'

import { listPhotos } from '@/api/media'
import { fetchProfile } from '@/api/profile'
import type { Photo, Profile } from '@/api/types'
import { AvatarUploader, PhotoUploader } from '@/components/PhotoUpload'
import { BasicFields, FigureFields, MoreFields } from '@/components/profile/fields'
import { useProfileForm } from '@/components/profile/form'
import { firstIncompleteStep, STEPS } from '@/components/profile/steps'
import { Button } from '@/components/ui/button'
import { Progress } from '@/components/ui/progress'
import { Spinner } from '@/components/ui/spinner'
import { missingLabels } from '@/lib/dict'

/**
 * 建档向导。全屏不套壳 —— 这时候底部 tab 通向的页面一个都还不能用。
 *
 * 已经建完档的人不该再看到它：状态由服务端说了算，不看本地标记。
 */
export async function onboardingLoader() {
  const [profile, photos] = await Promise.all([fetchProfile(), listPhotos()])
  if (profile.next_step !== 'onboarding') throw redirect('/')
  return { profile, photos }
}

const STEP_HINT: Record<string, string> = {
  basic: '先让引荐卡上有个能称呼的名字。',
  figure: '让别人能判断要不要认识你。',
  // 「我们会自动检查」是 e2e-onboarding 判断「已进入照片步」的锚点，别删。
  // 措辞是建议不是要求 —— 同一屏的顶栏会立刻显示「入池条件已满足」，
  // 这句要是读起来像门槛，用户会以为「下一步」还点不动。
  photos: '一张就够，多传几张别人更容易记住你。头像要能看清正脸，我们会自动检查。',
  more: '填得越具体，越容易被记住。也可以留到以后再说。',
}

export function OnboardingPage() {
  const initial = useLoaderData() as { profile: Profile; photos: Photo[] }
  const navigate = useNavigate()

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

  const { form, save, saving } = useProfileForm(profile)
  const {
    control,
    formState: { errors },
  } = form

  // 19.6 的第一个洞：不做回填的话，中途刷新就得从第 1 步重来。
  // 这里按 missing_required 直接落到第一个没做完的步骤。
  const [step, setStep] = React.useState(() => firstIncompleteStep(initial.profile.missing_required))
  const [photoBlocked, setPhotoBlocked] = React.useState<string | null>(null)

  const current = STEPS[step]
  const isLast = step === STEPS.length - 1
  const missing = missingLabels(profile.missing_required)
  const done = profile.missing_required.length === 0

  async function onNext() {
    setPhotoBlocked(null)

    if (current.key === 'photos') {
      // 照片和头像走各自的接口，上传即生效，这里只是把门槛说清楚
      const left = missingLabels(
        profile.missing_required.filter((m) => m === 'avatar_key' || m === 'photos'),
      )
      if (left.length > 0) {
        setPhotoBlocked(`还差${left.join('、')}，补齐才能继续。`)
        return
      }
    } else if (!(await save(current.fields))) {
      return
    }

    if (isLast) {
      // 走完向导回首页。status 由服务端在照片足够时就翻成 active 了，
      // 这里不需要（也不该）在前端自己判一遍。
      navigate('/', { replace: true })
      return
    }
    setStep(step + 1)
    window.scrollTo({ top: 0 })
  }

  return (
    <div className="mx-auto flex min-h-dvh max-w-[640px] flex-col px-5">
      <header className="pt-[calc(env(safe-area-inset-top,0px)+28px)]">
        <div className="flex items-baseline justify-between">
          <span className="font-serif text-[19px] text-ink">建档</span>
          <span className="tnum font-mono text-[12px] tracking-[.08em] text-muted">
            第 {step + 1} 步 / 共 {STEPS.length} 步
          </span>
        </div>

        {/* 步骤进度用一根 2px 的线，不用圆点步骤条 —— 圆点会把「补充」这一步
            显得和必填项一样重要，而它是可以跳过的 */}
        <div className="mt-4 flex gap-1" aria-hidden>
          {STEPS.map((s, i) => (
            <span
              key={s.key}
              className={i <= step ? 'h-[2px] flex-1 bg-accent' : 'h-[2px] flex-1 bg-line'}
            />
          ))}
        </div>

        <div className="mt-6">
          <div className="flex items-baseline justify-between text-[13px] text-muted">
            <span>档案完成度</span>
            <span className="tnum font-mono text-ink-2">{profile.completeness}%</span>
          </div>
          <Progress value={profile.completeness} className="mt-2" label="档案完成度" />
          <p className="mt-2 text-[13px] leading-[1.8] text-muted">
            {done ? (
              '入池条件已满足，可以收到引荐了。'
            ) : (
              <>
                还差：{missing.join('、')}
                <br />
                补齐之后才会被引荐给别人。
              </>
            )}
          </p>
        </div>
      </header>

      <main className="flex-1 pb-40 pt-9">
        <h1 className="font-serif text-[18px] text-ink">{current.title}</h1>
        <p className="mt-2 text-[13px] leading-[1.8] text-muted">{STEP_HINT[current.key]}</p>

        <div className="mt-7">
          {current.key === 'basic' && <BasicFields control={control} errors={errors} />}
          {current.key === 'figure' && <FigureFields control={control} errors={errors} />}
          {current.key === 'more' && <MoreFields control={control} errors={errors} />}
          {current.key === 'photos' && (
            <div>
              <AvatarUploader avatarUrl={profile.avatar_url} />
              <div className="mt-8 border-t border-line-soft pt-7">
                <PhotoUploader photos={photos} />
              </div>
              {photoBlocked && (
                <p className="mt-4 text-[13px] leading-[1.8] text-muted">{photoBlocked}</p>
              )}
            </div>
          )}
        </div>
      </main>

      {/* 每一步提交即保存，所以这里只有「下一步」，没有全局的保存按钮 */}
      <div className="fixed inset-x-0 bottom-0 border-t border-line bg-paper pb-safe">
        <div className="mx-auto flex max-w-[640px] items-center gap-3 px-5 py-3">
          {step > 0 && (
            <Button
              variant="quiet"
              onClick={() => {
                setPhotoBlocked(null)
                setStep(step - 1)
                window.scrollTo({ top: 0 })
              }}
            >
              上一步
            </Button>
          )}
          <Button size="lg" className="flex-1" onClick={onNext} disabled={saving}>
            {saving ? <Spinner /> : isLast ? '完成，去首页' : '下一步'}
          </Button>
        </div>
      </div>
    </div>
  )
}
