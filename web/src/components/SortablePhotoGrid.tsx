import {
  DndContext,
  DragOverlay,
  KeyboardSensor,
  PointerSensor,
  TouchSensor,
  closestCenter,
  useSensor,
  useSensors,
  type Announcements,
  type DragEndEvent,
  type DragStartEvent,
} from '@dnd-kit/core'
import { restrictToParentElement } from '@dnd-kit/modifiers'
import {
  SortableContext,
  arrayMove,
  rectSortingStrategy,
  sortableKeyboardCoordinates,
  useSortable,
} from '@dnd-kit/sortable'
import { CSS } from '@dnd-kit/utilities'
import { Plus, X } from 'lucide-react'
import * as React from 'react'

import type { Photo } from '@/api/types'
import { Spinner } from '@/components/ui/spinner'
import { cn } from '@/lib/cn'

/** 拖动时的浮层。触屏上手指会盖住格子本身，没有浮层就看不见自己拖的是哪张。 */
function PhotoTile({ photo, cover }: { photo: Photo; cover: boolean }) {
  return (
    <>
      <img src={photo.card_url} alt="" className="h-full w-full object-cover" />
      {cover && (
        <span className="absolute left-0 top-0 bg-paper/85 px-1.5 py-0.5 text-[11px] text-muted">
          封面
        </span>
      )}
    </>
  )
}

function SortablePhoto({
  photo,
  cover,
  deleting,
  onDelete,
}: {
  photo: Photo
  cover: boolean
  deleting: boolean
  onDelete: () => void
}) {
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({
    id: photo.id,
  })

  return (
    <div
      ref={setNodeRef}
      style={{ transform: CSS.Transform.toString(transform), transition }}
      className={cn(
        'group relative aspect-square touch-manipulation overflow-hidden rounded-card bg-surface-2',
        // 被拖的那张留在原位但变淡 —— 位置由 DragOverlay 代表
        isDragging && 'opacity-30',
        'focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-accent',
      )}
      {...attributes}
      {...listeners}
    >
      <PhotoTile photo={photo} cover={cover} />

      <button
        type="button"
        aria-label="删除这张照片"
        disabled={deleting}
        // 不拦下来的话，按住删除键 200ms 会被 TouchSensor 当成拖拽起手，
        // 删除就点不动了
        onPointerDown={(e) => e.stopPropagation()}
        onTouchStart={(e) => e.stopPropagation()}
        onKeyDown={(e) => e.stopPropagation()}
        onClick={onDelete}
        className={cn(
          'absolute right-1 top-1 flex h-7 w-7 items-center justify-center rounded-card',
          'bg-paper/85 text-ink-2 transition-colors duration-150 hover:text-ink',
          'focus-visible:outline-none focus-visible:border focus-visible:border-accent',
          deleting && 'opacity-45',
        )}
      >
        <X aria-hidden className="h-3.5 w-3.5" strokeWidth={1.5} />
      </button>
    </div>
  )
}

/**
 * dnd-kit 的朗读默认是英文，这里换掉。空格起落、方向键移动是它的默认键位。
 * 不说「第 N 张」—— 回调只给 active/over 的 id，要报位置还得再查一次，
 * 而且读屏用户真正需要的是「怎么操作」和「成了没有」。
 */
const ANNOUNCEMENTS: Announcements = {
  onDragStart: () => '已拿起照片，用方向键移动，空格放下',
  onDragOver: () => '移动中',
  onDragEnd: () => '已放下，顺序已保存',
  onDragCancel: () => '已取消，顺序没有变',
}

/**
 * 照片九宫格，可拖拽排序。
 *
 * 排序即保存 —— 没有「保存顺序」按钮。松手后 POST 完整 id 列表，
 * 失败就把本地顺序退回服务端的版本（后端只接受完整且不重复的列表，
 * 所以不能只发变动的那两张）。
 *
 * 键位：Tab 聚焦到某张，空格拿起，方向键移动，空格放下。dnd-kit 的
 * KeyboardSensor 负责这套，不用自己写。
 */
export function SortablePhotoGrid({
  photos,
  deleting,
  maxPhotos,
  uploading,
  percent,
  onDelete,
  onReorder,
  onAdd,
}: {
  photos: Photo[]
  deleting: number | null
  maxPhotos: number
  uploading: boolean
  percent: number | null
  onDelete: (id: number) => void
  onReorder: (ids: number[]) => void
  onAdd: () => void
}) {
  // 本地顺序。拖动时要立刻反馈，不能等请求回来。
  const [order, setOrder] = React.useState<number[]>(() => photos.map((p) => p.id))
  const [dragging, setDragging] = React.useState<Photo | null>(null)

  // 服务端数据变了就以后者为准：上传、删除、排序成功后都会走到这里。
  // 这也是排序请求失败时的回退路径。
  React.useEffect(() => {
    setOrder(photos.map((p) => p.id))
  }, [photos])

  const byId = React.useMemo(() => new Map(photos.map((p) => [p.id, p])), [photos])
  const items = order.map((id) => byId.get(id)).filter((p): p is Photo => p !== undefined)

  const sensors = useSensors(
    // 鼠标：移动 8px 才算拖，否则一次点击会被吃掉，删除按钮点不动
    useSensor(PointerSensor, { activationConstraint: { distance: 8 } }),
    // 触屏：按住 200ms 才算拖。不这么做的话，手指一划就开始排序，页面滚不动
    useSensor(TouchSensor, { activationConstraint: { delay: 200, tolerance: 8 } }),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }),
  )

  function handleDragEnd({ active, over }: DragEndEvent) {
    setDragging(null)
    if (!over || active.id === over.id) return

    const from = order.indexOf(Number(active.id))
    const to = order.indexOf(Number(over.id))
    if (from < 0 || to < 0) return

    const next = arrayMove(order, from, to)
    setOrder(next)
    onReorder(next)
  }

  const remaining = maxPhotos - photos.length

  return (
    <DndContext
      sensors={sensors}
      collisionDetection={closestCenter}
      modifiers={[restrictToParentElement]}
      accessibility={{ announcements: ANNOUNCEMENTS }}
      onDragStart={({ active }: DragStartEvent) => setDragging(byId.get(Number(active.id)) ?? null)}
      onDragEnd={handleDragEnd}
      onDragCancel={() => setDragging(null)}
    >
      <div className="grid grid-cols-3 gap-2">
        <SortableContext items={order} strategy={rectSortingStrategy}>
          {items.map((p, i) => (
            <SortablePhoto
              key={p.id}
              photo={p}
              cover={i === 0}
              deleting={deleting === p.id}
              onDelete={() => onDelete(p.id)}
            />
          ))}
        </SortableContext>

        {remaining > 0 && (
          <button
            type="button"
            onClick={onAdd}
            disabled={uploading}
            aria-label="添加照片"
            className={cn(
              'relative flex aspect-square items-center justify-center rounded-card',
              'border border-dashed border-line bg-surface text-muted',
              'transition-colors duration-150 hover:border-ink-2 hover:text-ink-2',
              'focus-visible:outline-none focus-visible:border-accent',
            )}
          >
            {uploading ? <Spinner /> : <Plus aria-hidden className="h-5 w-5" strokeWidth={1.5} />}
            {percent !== null && (
              <span className="absolute inset-x-0 bottom-0 h-[2px] bg-line-soft">
                <span
                  className="block h-full bg-accent transition-[width] duration-200"
                  style={{ width: `${percent}%` }}
                />
              </span>
            )}
          </button>
        )}
      </div>

      <DragOverlay dropAnimation={null}>
        {dragging && (
          <div className="aspect-square overflow-hidden rounded-card border border-accent bg-surface">
            <PhotoTile photo={dragging} cover={false} />
          </div>
        )}
      </DragOverlay>
    </DndContext>
  )
}
