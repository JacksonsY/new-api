// jzlh-sub 创建子账号弹窗（批量，对齐 302 截图3）。
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/design-system/button'
import { Dialog } from '@/components/dialog'
import { Input } from '@/components/design-system/input'
import { Label } from '@/components/ui/label'

import { createSubAccounts } from './api'
import { LimitField, PermissionToggles } from './form-fields'
import {
  ROLE_PRESET_USER,
  type LimitInput,
  SUB_CORE_PERMISSIONS,
} from './types'

const unlimited = (): LimitInput => ({ unlimited: true, value: 0 })

function defaultPerms(): Record<string, boolean> {
  const p: Record<string, boolean> = {}
  for (const k of SUB_CORE_PERMISSIONS) p[k] = true
  return p
}

export function CreateSubAccountDialog({
  open,
  onOpenChange,
  onSuccess,
  canGrantAdmin,
}: {
  open: boolean
  onOpenChange: (o: boolean) => void
  onSuccess: () => void
  canGrantAdmin: boolean
}) {
  const { t } = useTranslation()
  const [prefix, setPrefix] = useState('')
  const [count, setCount] = useState(1)
  const [note, setNote] = useState('')
  const [preset, setPreset] = useState(ROLE_PRESET_USER)
  const [permissions, setPermissions] = useState<Record<string, boolean>>(
    defaultPerms()
  )
  const [total, setTotal] = useState<LimitInput>(unlimited())
  const [month, setMonth] = useState<LimitInput>(unlimited())
  const [day, setDay] = useState<LimitInput>(unlimited())
  const [submitting, setSubmitting] = useState(false)

  function reset() {
    setPrefix('')
    setCount(1)
    setNote('')
    setPreset(ROLE_PRESET_USER)
    setPermissions(defaultPerms())
    setTotal(unlimited())
    setMonth(unlimited())
    setDay(unlimited())
  }

  async function submit() {
    if (!prefix.trim()) {
      toast.error(t('Please enter an account prefix'))
      return
    }
    setSubmitting(true)
    try {
      const res = await createSubAccounts({
        prefix: prefix.trim(),
        count,
        role_preset: preset,
        permissions,
        note,
        total_limit: total,
        month_limit: month,
        day_limit: day,
      })
      if (res.success) {
        toast.success(t('Created'))
        reset()
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
      title={t('Create Sub-account')}
      contentHeight='auto'
      contentClassName='sm:max-w-2xl'
      footer={
        <div className='flex justify-end gap-2'>
          <Button variant='outline' onClick={() => onOpenChange(false)}>
            {t('Cancel')}
          </Button>
          <Button onClick={submit} disabled={submitting}>
            {t('Create')}
          </Button>
        </div>
      }
    >
      <div className='space-y-4 py-1'>
        <div className='flex items-center gap-3'>
          <Label className='w-20 shrink-0'>{t('Sub-account')}</Label>
          <Input
            value={prefix}
            onChange={(e) => setPrefix(e.target.value)}
            placeholder={t('Please enter an account prefix')}
          />
        </div>
        <div className='flex items-center gap-3'>
          <Label className='w-20 shrink-0'>{t('Note')}</Label>
          <Input
            value={note}
            onChange={(e) => setNote(e.target.value)}
            placeholder={t('Optional note')}
          />
        </div>
        <div className='flex items-center gap-3'>
          <Label className='w-20 shrink-0'>{t('Count')}</Label>
          <div className='flex items-center gap-2'>
            <Button
              variant='outline'
              size='sm'
              onClick={() => setCount((c) => Math.max(1, c - 1))}
            >
              −
            </Button>
            <Input
              type='number'
              min={1}
              max={100}
              value={count}
              onChange={(e) =>
                setCount(Math.min(100, Math.max(1, Number(e.target.value) || 1)))
              }
              className='w-20 text-center'
            />
            <Button
              variant='outline'
              size='sm'
              onClick={() => setCount((c) => Math.min(100, c + 1))}
            >
              +
            </Button>
          </div>
        </div>

        <PermissionToggles
          preset={preset}
          onPresetChange={setPreset}
          permissions={permissions}
          onToggle={(k, on) =>
            setPermissions((p) => ({ ...p, [k]: on }))
          }
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
