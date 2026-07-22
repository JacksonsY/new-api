// jzlh-sub 编辑子账号弹窗（对齐 302 截图2）。
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/design-system/button'
import { Dialog } from '@/components/dialog'
import { Input } from '@/components/design-system/input'
import { Label } from '@/components/ui/label'

import { updateSubAccount } from './api'
import { LimitField, PermissionToggles } from './form-fields'
import {
  ROLE_PRESET_ADMIN,
  ROLE_PRESET_USER,
  type LimitInput,
  type SubAccountView,
} from './types'

function limitFromUsd(usd: number): LimitInput {
  return usd < 0 ? { unlimited: true, value: 0 } : { unlimited: false, value: usd }
}

export function EditSubAccountDialog({
  target,
  open,
  onOpenChange,
  onSuccess,
  canGrantAdmin,
}: {
  target: SubAccountView | null
  open: boolean
  onOpenChange: (o: boolean) => void
  onSuccess: () => void
  canGrantAdmin: boolean
}) {
  const { t } = useTranslation()
  const [displayName, setDisplayName] = useState('')
  const [note, setNote] = useState('')
  const [newPassword, setNewPassword] = useState('')
  const [preset, setPreset] = useState(ROLE_PRESET_USER)
  const [permissions, setPermissions] = useState<Record<string, boolean>>({})
  const [total, setTotal] = useState<LimitInput>({ unlimited: true, value: 0 })
  const [month, setMonth] = useState<LimitInput>({ unlimited: true, value: 0 })
  const [day, setDay] = useState<LimitInput>({ unlimited: true, value: 0 })
  const [submitting, setSubmitting] = useState(false)

  useEffect(() => {
    if (open && target) {
      setDisplayName(target.username)
      setNote(target.note)
      setNewPassword('')
      setPreset(target.role_preset === ROLE_PRESET_ADMIN ? ROLE_PRESET_ADMIN : ROLE_PRESET_USER)
      setPermissions({ ...target.permissions })
      setTotal(limitFromUsd(target.total_limit_usd))
      setMonth(limitFromUsd(target.month_limit_usd))
      setDay(limitFromUsd(target.day_limit_usd))
    }
  }, [open, target])

  async function submit() {
    if (!target) return
    setSubmitting(true)
    try {
      const res = await updateSubAccount(target.id, {
        display_name: displayName,
        note,
        role_preset: preset,
        permissions,
        new_password: newPassword || undefined,
        total_limit: total,
        month_limit: month,
        day_limit: day,
      })
      if (res.success) {
        toast.success(t('Saved'))
        onOpenChange(false)
        onSuccess()
      } else {
        toast.error(res.message || t('Failed'))
      }
    } catch {
      toast.error(t('Failed'))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title={t('Edit Sub-account')}
      contentHeight='auto'
      contentClassName='sm:max-w-2xl'
      footer={
        <div className='flex justify-end gap-2'>
          <Button variant='outline' onClick={() => onOpenChange(false)}>
            {t('Cancel')}
          </Button>
          <Button onClick={submit} disabled={submitting}>
            {t('Save')}
          </Button>
        </div>
      }
    >
      <div className='space-y-4 py-1'>
        <div className='flex items-center gap-3'>
          <Label className='w-24 shrink-0'>{t('Username')}</Label>
          <Input value={displayName} onChange={(e) => setDisplayName(e.target.value)} />
        </div>
        <div className='flex items-center gap-3'>
          <Label className='w-24 shrink-0'>{t('Note')}</Label>
          <Input value={note} onChange={(e) => setNote(e.target.value)} />
        </div>
        <div className='flex items-center gap-3'>
          <Label className='w-24 shrink-0'>{t('Sub-account')}</Label>
          <Input value={target?.email ?? ''} disabled />
        </div>
        <div className='flex items-center gap-3'>
          <Label className='w-24 shrink-0'>{t('Change Password')}</Label>
          <Input
            type='password'
            value={newPassword}
            onChange={(e) => setNewPassword(e.target.value)}
            placeholder={t('Leave blank to keep unchanged')}
          />
        </div>

        <PermissionToggles
          preset={preset}
          onPresetChange={setPreset}
          permissions={permissions}
          onToggle={(k, on) => setPermissions((p) => ({ ...p, [k]: on }))}
          canGrantAdmin={canGrantAdmin}
        />

        <div className='space-y-2 border-t pt-3'>
          <LimitField label={t('Total')} value={total} onChange={setTotal} />
          <LimitField label={t('Monthly')} value={month} onChange={setMonth} />
          <LimitField label={t('Daily')} value={day} onChange={setDay} />
        </div>
      </div>
    </Dialog>
  )
}
