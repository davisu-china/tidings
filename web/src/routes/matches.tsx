import { useQuery } from '@tanstack/react-query'
import { Link, useLoaderData } from 'react-router'

import { fetchMatches, type Match, type MatchList } from '@/api/conversation'
import { cn } from '@/lib/cn'

export async function matchesLoader() {
  return { matches: await fetchMatches() }
}

/**
 * 会话列表（§19.7）。
 *
 * 「匹配列表」和「会话列表」在这里是同一个界面：成匹配的唯一结果就是
 * 开了一条会话，为它单独做一页只会在两页之间来回跳。未读数在最上面 ——
 * 这一页的存在意义就是让人看见谁在等他回话。
 */
export function MatchesPage() {
  const { matches: initial } = useLoaderData() as { matches: MatchList }

  const { data } = useQuery({
    queryKey: ['matches'],
    queryFn: () => fetchMatches(),
    initialData: initial,
  })

  if (data.matches.length === 0) {
    return (
      <div className="mx-auto flex w-full max-w-[420px] flex-1 flex-col items-center justify-center px-6 py-20 text-center">
        <p className="font-serif text-[17px] leading-[1.9] text-ink-2">
          还没有人互相点过「想认识」。
          <br />
          成了匹配，会话就会出现在这里。
        </p>
      </div>
    )
  }

  return (
    <div className="mx-auto w-full max-w-[640px] px-5 py-6">
      <h1 className="font-serif text-[22px] leading-tight text-ink">会话</h1>

      <div className="mt-5 border-t border-line-soft">
        {data.matches.map((m) => (
          <MatchRow key={m.match_id} match={m} />
        ))}
      </div>
    </div>
  )
}

/** 会话列表里的时间。今天给时刻，今年给月日，跨年才给年 —— 越近越具体。 */
function listTime(iso: string | null): string {
  if (!iso) return ''
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return ''

  const now = new Date()
  const sameDay =
    d.getFullYear() === now.getFullYear() &&
    d.getMonth() === now.getMonth() &&
    d.getDate() === now.getDate()
  if (sameDay) {
    return `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
  }
  if (d.getFullYear() === now.getFullYear()) return `${d.getMonth() + 1} 月 ${d.getDate()} 日`
  return `${d.getFullYear()} 年 ${d.getMonth() + 1} 月 ${d.getDate()} 日`
}

function MatchRow({ match }: { match: Match }) {
  const hasUnread = match.unread > 0

  return (
    <Link
      to={`/chat/${match.match_id}`}
      className={cn(
        'flex min-h-[68px] items-center gap-4 border-b border-line-soft px-1 py-3',
        'transition-colors duration-150 hover:bg-surface-2',
      )}
    >
      <div className="relative h-12 w-12 shrink-0">
        <div className="h-full w-full overflow-hidden rounded-card border border-line bg-surface-2">
          {match.cover_url ? (
            <img src={match.cover_url} alt="" className="h-full w-full object-cover" />
          ) : null}
        </div>
        {hasUnread && (
          <span
            className="absolute -right-1.5 -top-1.5 h-2.5 w-2.5 rounded-full bg-seal"
            aria-label={`${match.unread} 条未读`}
          />
        )}
      </div>

      <div className="min-w-0 flex-1">
        <div className="flex items-baseline justify-between gap-3">
          <span className="truncate font-serif text-[16px] text-ink">
            {match.nickname ?? '这位朋友'}
          </span>
          <span className="tnum shrink-0 font-mono text-[11px] text-muted">
            {listTime(match.last_msg_at)}
          </span>
        </div>

        <p
          className={cn(
            'mt-1 truncate text-[13px]',
            hasUnread ? 'text-ink-2' : 'text-muted',
          )}
        >
          {match.last_content === null ? (
            // 成了匹配但还没人开口。这一句写在这里，比一个空行有用 ——
            // 它同时也是「该你了」的提示
            '你们都点了「想认识」，说点什么吧'
          ) : (
            <>
              {match.last_mine && '我：'}
              {match.last_content}
            </>
          )}
        </p>
      </div>
    </Link>
  )
}
