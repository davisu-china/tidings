import { zodResolver } from '@hookform/resolvers/zod'
import { useQueryClient } from '@tanstack/react-query'
import * as React from 'react'
import { useForm, type UseFormReturn } from 'react-hook-form'

import { saveProfile } from '@/api/profile'
import type { Profile } from '@/api/types'
import { useToast } from '@/components/ui/toast'
import { messageOf } from '@/lib/errors'

import { buildPatch, profileSchema, toFormValues, type FieldKey, type ProfileForm } from './schema'

export interface ProfileFormApi {
  form: UseFormReturn<ProfileForm>
  save: (keys: readonly FieldKey[]) => Promise<boolean>
  saving: boolean
  /** 这些字段里有没有跟服务端不一致的。用来决定「保存」按钮亮不亮 */
  isDirty: (keys: readonly FieldKey[]) => boolean
}

/**
 * 建档表单的共享状态。建档向导和 /me/edit 用的是同一套 ——
 * 19.7 要求编辑页复用向导的表单控件，状态层也一起复用才不会出现两套校验。
 *
 * 脏值靠「当前值 vs 基线快照」比较，不用 RHF 的 dirtyFields：
 * 分步向导里未渲染的步骤字段压根没注册，dirtyFields 会把它们算成干净的，
 * 而保存成功后我们也不 reset 表单（reset 会丢掉另一个区块里正在敲的字）。
 */
export function useProfileForm(profile: Profile): ProfileFormApi {
  const qc = useQueryClient()
  const toast = useToast()

  const form = useForm<ProfileForm>({
    resolver: zodResolver(profileSchema),
    defaultValues: toFormValues(profile),
    mode: 'onTouched',
  })

  const values = form.watch()
  const [baseline, setBaseline] = React.useState<ProfileForm>(() => toFormValues(profile))
  const [saving, setSaving] = React.useState(false)

  const isDirty = React.useCallback(
    (keys: readonly FieldKey[]) => keys.some((k) => values[k] !== baseline[k]),
    [values, baseline],
  )

  const save = React.useCallback(
    async (keys: readonly FieldKey[]): Promise<boolean> => {
      if (keys.length > 0 && !(await form.trigger(keys as FieldKey[]))) return false

      setSaving(true)
      try {
        const next = await saveProfile(buildPatch(form.getValues(), keys))
        // 保存返回的就是最新的完整档案，直接写进缓存，省一次 GET
        qc.setQueryData(['profile'], next)
        setBaseline(toFormValues(next))
        return true
      } catch (err) {
        // 后端会挡下前端的漏网之鱼（性别不可改、昵称含联系方式），
        // 它的 message 是写给用户的中文，原样透出即可
        toast.show(messageOf(err))
        return false
      } finally {
        setSaving(false)
      }
    },
    [form, qc, toast],
  )

  return { form, save, saving, isDirty }
}
