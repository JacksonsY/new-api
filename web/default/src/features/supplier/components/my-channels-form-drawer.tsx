/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/design-system/button'
import { Input } from '@/components/design-system/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/design-system/select'
import {
  SideDrawerSection,
  sideDrawerContentClassName,
  sideDrawerFooterClassName,
  sideDrawerFormClassName,
  sideDrawerHeaderClassName,
} from '@/components/drawer-layout'
import { Label } from '@/components/ui/label'
import {
  Sheet,
  SheetClose,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { Textarea } from '@/components/ui/textarea'
import {
  CHANNEL_TYPE_OPTIONS,
  CHANNEL_TYPES,
} from '@/features/channels/constants'

import { createSupplierChannel, updateSupplierChannel } from '../api'
import { CHANNEL_AUDIT_STATUS, type SupplierChannel } from '../types'

// 受限渠道表单抽屉：仅 name/type/key/base_url/models/报价率/test_model。
export function ChannelFormDrawer({
  open,
  target,
  onOpenChange,
  onDone,
}: {
  open: boolean
  target: SupplierChannel | null
  onOpenChange: (open: boolean) => void
  onDone: () => void
}) {
  const { t } = useTranslation()
  const isEdit = !!target
  const [name, setName] = useState('')
  const [type, setType] = useState<number>(1)
  const [apiKey, setApiKey] = useState('')
  const [baseUrl, setBaseUrl] = useState('')
  const [models, setModels] = useState('')
  const [ratio, setRatio] = useState('1')
  const [testModel, setTestModel] = useState('')
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    if (target) {
      setName(target.name)
      setType(target.type)
      setApiKey('')
      setBaseUrl(target.base_url ?? '')
      setModels(target.models ?? '')
      setRatio(String(target.channel_ratio ?? 1))
      setTestModel(target.test_model ?? '')
    } else {
      setName('')
      setType(1)
      setApiKey('')
      setBaseUrl('')
      setModels('')
      setRatio('1')
      setTestModel('')
    }
  }, [target])

  const showReReviewHint =
    isEdit && target?.audit_status === CHANNEL_AUDIT_STATUS.APPROVED

  async function onSave() {
    const trimmedName = name.trim()
    if (!trimmedName) {
      toast.error(t('Please enter a channel name'))
      return
    }
    const trimmedModels = models.trim()
    if (!trimmedModels) {
      toast.error(t('Please enter at least one model'))
      return
    }
    const r = Number.parseFloat(ratio)
    if (Number.isNaN(r) || r <= 0 || r > 1) {
      toast.error(t('Quote Rate must be between 0 and 1'))
      return
    }
    if (!isEdit && !apiKey.trim()) {
      toast.error(t('Please enter an API key'))
      return
    }
    setBusy(true)
    try {
      const form = {
        name: trimmedName,
        type,
        key: apiKey.trim(),
        base_url: baseUrl.trim(),
        models: trimmedModels,
        channel_ratio: r,
        test_model: testModel.trim(),
      }
      const res =
        isEdit && target
          ? await updateSupplierChannel(target.id, form)
          : await createSupplierChannel(form)
      if (res.success) {
        toast.success(t('Saved'))
        onDone()
      } else {
        toast.error(res.message || t('Failed'))
      }
    } catch {
      toast.error(t('Failed'))
    } finally {
      setBusy(false)
    }
  }

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent className={sideDrawerContentClassName('sm:max-w-[520px]')}>
        <SheetHeader className={sideDrawerHeaderClassName()}>
          <SheetTitle>
            {isEdit ? t('Edit Channel') : t('Add Channel')}
          </SheetTitle>
          <SheetDescription>
            {t(
              'Provide your upstream credentials. Group, priority and weight are set by the administrator on approval.'
            )}
          </SheetDescription>
        </SheetHeader>

        <div className={sideDrawerFormClassName()}>
          <SideDrawerSection>
            <div className='grid gap-4'>
              <div className='grid gap-1.5'>
                <Label htmlFor='sc-name'>{t('Name')}</Label>
                <Input
                  id='sc-name'
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                />
              </div>

              <div className='grid content-start gap-1.5'>
                <Label>{t('Type')}</Label>
                <Select
                  value={String(type)}
                  onValueChange={(v) => setType(Number(v))}
                >
                  <SelectTrigger className='w-full'>
                    <SelectValue>
                      {CHANNEL_TYPES[type as keyof typeof CHANNEL_TYPES] ||
                        `#${type}`}
                    </SelectValue>
                  </SelectTrigger>
                  <SelectContent alignItemWithTrigger={false}>
                    {CHANNEL_TYPE_OPTIONS.map((opt) => (
                      <SelectItem key={opt.value} value={String(opt.value)}>
                        {opt.label}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>

              <div className='grid gap-1.5'>
                <Label htmlFor='sc-key'>{t('Key')}</Label>
                <Input
                  id='sc-key'
                  value={apiKey}
                  onChange={(e) => setApiKey(e.target.value)}
                  placeholder={
                    isEdit ? t('Leave blank to keep the current key') : ''
                  }
                  autoComplete='off'
                />
              </div>

              <div className='grid gap-1.5'>
                <Label htmlFor='sc-baseurl'>{t('Base URL')}</Label>
                <Input
                  id='sc-baseurl'
                  value={baseUrl}
                  onChange={(e) => setBaseUrl(e.target.value)}
                  placeholder='https://'
                />
              </div>

              <div className='grid gap-1.5'>
                <Label htmlFor='sc-models'>{t('Models')}</Label>
                <Textarea
                  id='sc-models'
                  value={models}
                  onChange={(e) => setModels(e.target.value)}
                  placeholder={t('e.g. gpt-4o, gemini-2.5-flash')}
                  rows={3}
                />
              </div>

              <div className='grid grid-cols-2 gap-4'>
                <div className='grid gap-1.5'>
                  <Label htmlFor='sc-ratio'>{t('Quote Rate')} (0-1)</Label>
                  <Input
                    id='sc-ratio'
                    value={ratio}
                    onChange={(e) => setRatio(e.target.value)}
                    inputMode='decimal'
                  />
                </div>
                <div className='grid gap-1.5'>
                  <Label htmlFor='sc-testmodel'>{t('Test Model')}</Label>
                  <Input
                    id='sc-testmodel'
                    value={testModel}
                    onChange={(e) => setTestModel(e.target.value)}
                  />
                </div>
              </div>

              {showReReviewHint && (
                <p className='text-muted-foreground bg-muted/40 rounded-md px-3 py-2 text-xs leading-relaxed'>
                  {t(
                    'Changing the key, base URL or models will send this channel back to review.'
                  )}
                </p>
              )}
            </div>
          </SideDrawerSection>
        </div>

        <SheetFooter className={sideDrawerFooterClassName()}>
          <SheetClose render={<Button variant='outline' />}>
            {t('Close')}
          </SheetClose>
          <Button onClick={onSave} disabled={busy}>
            {busy ? t('Saving...') : t('Save changes')}
          </Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  )
}
