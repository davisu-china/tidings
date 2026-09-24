import { useQuery, useQueryClient } from '@tanstack/react-query'
import * as React from 'react'
import { Link, useLoaderData } from 'react-router'

import { fetchSettings, saveSettings } from '@/api/settings'
import type { Settings, SettingsInput } from '@/api/types'
import { PushSetting } from '@/components/PushSetting'
import { Button } from '@/components/ui/button'
import { Segmented } from '@/components/ui/segmented'
import { Select, type Option } from '@/components/ui/select'
import { Spinner } from '@/components/ui/spinner'
import { useToast } from '@/components/ui/toast'
import { hourLabel } from '@/lib/dict'
import { messageOf } from '@/lib/errors'

export async function settingsLoader() {
  return { settings: await fetchSettings() }
}

/** 0–23 点。静默时段的两个端点是整点，不做半点。 */
const HOUR_OPTIONS: Option<number>[] = Array.from({ length: 24 }, (_, h) => ({
  value: h,
  label: hourLabel(h),
}))

/** 暂停开关。用分段控件而不是 Switch：这个项目里没有 Switch，也不该为一项新造一个。 */
const PAUSE_OPTIONS: Option<number>[] = [
  { value: 0, label: '接收引荐' },
  { value: 1, label: '暂停接收' },
]

function Block({
  title,
  description,
  children,
  footer,
}: {
  title: string
  description: React.ReactNode
  children?: React.ReactNode
  footer?: React.ReactNode
}) {
  return (
    <section className="mt-10 border-t border-line pt-8">
      <h2 className="font-serif text-[17px] text-ink">{title}</h2>
      <p className="mt-2 text-[13px] leading-[1.8] text-muted">{description}</p>
      {children && <div className="mt-6">{children}</div>}
      {footer && <div className="mt-6 flex items-center gap-4">{footer}</div>}
    </section>
  )
}

export function SettingsPage() {
  const initial = useLoaderData() as { settings: Settings }
  const qc = useQueryClient()
  const toast = useToast()

  const { data: settings } = useQuery({
    queryKey: ['settings'],
    queryFn: () => fetchSettings(),
    initialData: initial.settings,
  })

  // 静默时段的两个端点先落在本地，点「保存」才提交；
  // 暂停开关不走这条路（见下）。
  const [quietStart, setQuietStart] = React.useState(settings.quiet_start)
  const [quietEnd, setQuietEnd] = React.useState(settings.quiet_end)
  const [savingQuiet, setSavingQuiet] = React.useState(false)
  const [savingPause, setSavingPause] = React.useState(false)

  // 服务端值变了就重新对齐本地草稿。/me 那边解冻之后回到这一页，
  // 不重新对齐的话本地还留着旧值，一按保存就把它写回去了。
  React.useEffect(() => {
    setQuietStart(settings.quiet_start)
    setQuietEnd(settings.quiet_end)
  }, [settings.quiet_start, settings.quiet_end])

  const quietDirty = quietStart !== settings.quiet_start || quietEnd !== settings.quiet_end

  /**
   * PUT 是整体替换，所以每次都要把三项一起发上去 ——
   * 只发改动的那一项，另外两项会被清成零值，静默时段就悄悄变成
   * 「00:00 到 00:00」，也就是不设静默。
   */
  async function put(next: SettingsInput, done: string) {
    const saved = await saveSettings(next)
    qc.setQueryData(['settings'], saved)
    toast.show(done)
    return saved
  }

  async function onTogglePause(paused: boolean) {
    setSavingPause(true)
    try {
      await put(
        { intros_paused: paused, quiet_start: quietStart, quiet_end: quietEnd },
        paused ? '已暂停接收引荐' : '已恢复接收引荐',
      )
    } catch (err) {
      toast.show(messageOf(err))
    } finally {
      setSavingPause(false)
    }
  }

  async function onSaveQuiet() {
    setSavingQuiet(true)
    try {
      await put(
        { intros_paused: settings.intros_paused, quiet_start: quietStart, quiet_end: quietEnd },
        '已保存',
      )
    } catch (err) {
      toast.show(messageOf(err))
    } finally {
      setSavingQuiet(false)
    }
  }

  return (
    <div className="mx-auto max-w-[640px] px-5 py-8">
      <Link to="/me" className="text-[13px] text-muted hover:text-ink-2">
        ← 我的
      </Link>
      <h1 className="mt-4 font-serif text-[22px] leading-tight text-ink">通知与暂停</h1>

      <Block
        title="暂停接收引荐"
        description={
          <>
            暂停期间不会有新的引荐进来，也不会被人看到。已经在等你回音的那几封不会因为你不在而超时
            —— 暂停的这段时间从时限里扣掉，你回来的时候长度补回去。
          </>
        }
      >
        <Segmented
          value={settings.intros_paused ? 1 : 0}
          onChange={(v) => void onTogglePause(v === 1)}
          options={PAUSE_OPTIONS}
          disabled={savingPause}
          aria-labelledby="pause-label"
        />
        <span id="pause-label" className="sr-only">
          暂停接收引荐
        </span>
        <p className="mt-3 text-[13px] leading-[1.8] text-muted">
          {settings.intros_paused ? '现在是暂停状态，不会收到新的引荐。' : '现在会正常收到新引荐。'}
          {savingPause && ' 正在改…'}
        </p>
      </Block>

      {settings.push_frozen && (
        <section className="mt-8 rounded-card border border-line bg-surface px-4 py-3">
          <h2 className="text-[14px] text-ink-2">推送被暂停了</h2>
          <p className="mt-2 text-[13px] leading-[1.8] text-muted">
            连续 {settings.unopened_streak} 条推送没有被打开，我们先把推送停一停 ——
            一直发没人看的通知，只会让你干脆关掉通知权限，那之后就再也收不到了。
            <br />
            打开一次有信就会恢复。收尾通知（「你错过了谁」这类）不受影响，照样会发给你。
          </p>
        </section>
      )}

      <Block
        title="静默时段"
        description="落在这段时间里的推送会推迟到结束之后再发，不会丢。默认 22:00 到次日 09:00 —— 半夜震一下手机的代价，比晚一天知道谁来过高得多。"
        footer={
          <>
            <Button
              size="sm"
              variant="outline"
              onClick={onSaveQuiet}
              disabled={!quietDirty || savingQuiet}
            >
              {savingQuiet ? <Spinner /> : '保存'}
            </Button>
            {quietDirty && !savingQuiet && (
              <span className="text-[13px] text-muted">有未保存的改动</span>
            )}
          </>
        }
      >
        <div className="flex items-center gap-3">
          <Select
            value={quietStart}
            onChange={setQuietStart}
            options={HOUR_OPTIONS}
            id="quiet_start"
          />
          <span className="shrink-0 text-[13px] text-muted">到</span>
          <Select value={quietEnd} onChange={setQuietEnd} options={HOUR_OPTIONS} id="quiet_end" />
        </div>
        <p className="mt-3 text-[13px] leading-[1.8] text-muted">
          现在是 {hourLabel(quietStart)} 到 {hourLabel(quietEnd)}
          {quietStart === quietEnd
            ? '（两个端点相同，等于不设静默，推送随时可能发出）'
            : quietStart < quietEnd
              ? '。'
              : '，跨过零点。'}
        </p>
      </Block>

      {/* 这一块与 /me 上的那块是同一个组件、同一份浏览器状态，
          两处都放是有意的：§19.7 把「通知」和安装引导的重看入口
          归在 /me/settings，而 iOS 上没装到主屏幕就收不到推送，
          那一段引导必须在 /me 这种必经之路上也露一次面。 */}
      <PushSetting />
    </div>
  )
}
