import { Link } from 'react-router'

/** 404。措辞保持和空状态同一个语气 —— 不喊「错误」、不给红色。 */
export function NotFoundPage() {
  return (
    <div className="mx-auto flex min-h-dvh max-w-[420px] flex-col items-center justify-center px-6 text-center">
      <p className="font-serif text-[17px] leading-[1.9] text-ink-2">这一页不存在。</p>
      <p className="mt-3 text-[13px] leading-[1.8] text-muted">
        可能是链接过期了，或者它本来就不该在这里。
      </p>
      <Link to="/" className="mt-8 text-[14px] text-accent hover:text-accent-ink">
        回首页
      </Link>
    </div>
  )
}
