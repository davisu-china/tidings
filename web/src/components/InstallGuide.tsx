import * as React from 'react'

/**
 * iPhone 安装引导（§19.5）。
 *
 * iOS 上不装到主屏幕，这条通道对用户就不存在 —— Safari 只在
 * 「已添加到主屏幕」的 Web App 里给出 PushManager。所以这不是增长手段，
 * 是触达的必需环节，而 MVP 没有邮件兜底。
 *
 * 三条自我约束，都写在这里免得以后被「优化」掉：
 *   · 不弹遮罩、不挡住任何操作。它就是页面里的一段，跳过就跳过。
 *   · 不反复出现。用户收起过就不再显示（记住的是「收起了」，
 *     不是「看过了」—— 收起的动作才是他要表达的意思）。
 *   · 分享图标用内联 SVG 画，不用 emoji：emoji 在各系统上长得完全不同，
 *     而这段话的全部意义就是「指出那个按钮在哪」。
 *
 * 判断已经装没装靠 display-mode，不靠任何存储标记（§19.5）——
 * 标记会在换了浏览器、清了缓存之后说谎。
 */

const DISMISS_KEY = 'tidings.install-guide.dismissed'

/** Safari 底部工具条中间的分享按钮：一个从方框里向上的箭头。 */
function ShareIcon({ className }: { className?: string }) {
  return (
    <svg
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={1.5}
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden
      className={className}
    >
      <path d="M12 15V3" />
      <path d="M8 7l4-4 4 4" />
      <path d="M5 12v7a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2v-7" />
    </svg>
  )
}

/** 「添加到主屏幕」：一个带加号的方框。 */
function AddIcon({ className }: { className?: string }) {
  return (
    <svg
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={1.5}
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden
      className={className}
    >
      <rect x="3" y="3" width="18" height="18" rx="4" />
      <path d="M12 8v8M8 12h8" />
    </svg>
  )
}

function Step({
  icon,
  children,
}: {
  icon: React.ReactNode
  children: React.ReactNode
}) {
  return (
    <li className="flex items-start gap-3">
      <span className="mt-0.5 flex h-7 w-7 shrink-0 items-center justify-center rounded-card border border-line text-ink-2">
        {icon}
      </span>
      <span className="pt-0.5 text-[13px] leading-[1.8] text-ink-2">{children}</span>
    </li>
  )
}

export function InstallGuide({ onDismiss }: { onDismiss?: () => void }) {
  function dismiss() {
    try {
      localStorage.setItem(DISMISS_KEY, '1')
    } catch {
      // 存不下也无所谓：下次还会显示一遍，只是多一段说明
    }
    onDismiss?.()
  }

  return (
    <section className="rounded-card border border-line bg-surface px-5 py-4">
      <h3 className="font-serif text-[16px] leading-snug text-ink">
        装到主屏幕才收得到信
      </h3>
      <p className="mt-2 text-[13px] leading-[1.8] text-muted">
        iPhone 上的 Safari 只把通知权限给已经添加到主屏幕的网页。
        装一次，之后从主屏幕打开就是完整的有信。
      </p>

      <ol className="mt-4 flex flex-col gap-3">
        <Step icon={<ShareIcon className="h-4 w-4" />}>
          点 Safari 底部中间那个分享按钮
        </Step>
        <Step icon={<AddIcon className="h-4 w-4" />}>
          在菜单里往下找，选「添加到主屏幕」
        </Step>
        <Step icon={<img src="/icons/icon-180.png" alt="" className="h-4 w-4" />}>
          从主屏幕上的「有信」图标打开，再回到这里开通知
        </Step>
      </ol>

      {onDismiss && (
        <button
          type="button"
          onClick={dismiss}
          className="mt-4 min-h-[44px] text-[13px] text-muted transition-colors duration-150 hover:text-ink-2"
        >
          知道了，先不装
        </button>
      )}
    </section>
  )
}

/** 用户收起过就不再自动显示。 */
export function installGuideDismissed(): boolean {
  try {
    return localStorage.getItem(DISMISS_KEY) === '1'
  } catch {
    // localStorage 不可用（无痕模式）时按没收起处理：宁可多显示一次，
    // 也不要让一个收不到推送的人什么都看不到
    return false
  }
}
