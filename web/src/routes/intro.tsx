import { useQuery } from '@tanstack/react-query'
import { ChevronLeft } from 'lucide-react'
import { Link, useLoaderData, useParams, type LoaderFunctionArgs } from 'react-router'

import { fetchIntroduction, type Introduction } from '@/api/introductions'
import { IntroCard } from '@/components/IntroCard'

/**
 * 引荐详情（§19.7）。
 *
 * 这一页的 loader 会把这个 GET 发出去，而这个 GET 有副作用 ——
 * 它把「打开」落下来，72 小时的时钟就此停住（§13.1 的 pending → viewed）。
 * 所以它只能在这一页调用：放进列表的查询里，任何一次后台刷新都会
 * 把整屏的信一起标成已读，而其中有些根本没滚到。
 */
export async function introLoader({ params }: LoaderFunctionArgs) {
  const id = Number(params.id)
  if (!Number.isInteger(id) || id <= 0) return { intro: null as Introduction | null }

  try {
    return { intro: await fetchIntroduction(id) }
  } catch {
    // 不是本人、是单向引荐的隐藏方、或者 id 根本不存在 —— 后端对这三种
    // 情况给的都是同一个 404（§18.3 的反枚举规则），这里也就不区分。
    // 交给页面说一句平静的话，而不是摔到错误边界上。
    return { intro: null as Introduction | null }
  }
}

export function IntroPage() {
  const { intro: initial } = useLoaderData() as { intro: Introduction | null }
  const { id } = useParams()
  const introID = Number(id)

  const { data } = useQuery({
    queryKey: ['intro', introID],
    queryFn: () => fetchIntroduction(introID),
    initialData: initial ?? undefined,
    // loader 已经拿过一次了，且这次请求有副作用（标记已查看），
    // 挂载时不该再发一次
    staleTime: Infinity,
    enabled: initial !== null,
  })

  if (!data) {
    return (
      <div className="mx-auto w-full max-w-[640px] px-5 py-8">
        <BackLink />
        <p className="mt-16 text-center font-serif text-[15px] leading-[1.9] text-muted">
          这封信不在了。
        </p>
      </div>
    )
  }

  return (
    <div className="mx-auto w-full max-w-[640px] px-5 py-6">
      <BackLink />
      <div className="mt-4">
        <IntroCard intro={data} detail />
      </div>
    </div>
  )
}

function BackLink() {
  return (
    <Link
      to="/"
      className="inline-flex h-11 items-center gap-1 text-[14px] text-muted transition-colors duration-150 hover:text-ink-2"
    >
      <ChevronLeft aria-hidden className="h-4 w-4" strokeWidth={1.5} />
      引荐
    </Link>
  )
}
