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
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Input } from '@/components/design-system/input'
import { Label } from '@/components/ui/label'

import { supplierFetchModels } from '../api'
import type { SupplierChannelForm } from '../types'
import { ModelPicker } from './model-picker'

// 受控的渠道申请字段组（入驻申请与追加渠道复用）：上游地址 + 密钥 + 模型列表 + 渠道说明。
// 类型默认 OpenAI 兼容；报价率/分组由管理员审批时定，故此处不收。模型选择器可一键获取上游模型。
export function ChannelOfferFields({
  value,
  onChange,
  idPrefix = 'offer',
}: {
  value: SupplierChannelForm
  onChange: (next: SupplierChannelForm) => void
  idPrefix?: string
}) {
  const { t } = useTranslation()
  const [available, setAvailable] = useState<string[]>([])
  const [fetching, setFetching] = useState(false)

  const set = (patch: Partial<SupplierChannelForm>) =>
    onChange({ ...value, ...patch })

  const selected = value.models.split(',').filter(Boolean)
  const setSelected = (next: string[]) => set({ models: next.join(',') })

  async function fetchModels() {
    if (!value.base_url.trim() || !value.key.trim()) {
      toast.error(t('Please enter the Base URL and API key first'))
      return
    }
    setFetching(true)
    try {
      const res = await supplierFetchModels({
        base_url: value.base_url.trim(),
        type: value.type,
        key: value.key.trim(),
      })
      if (res.success && res.data) {
        const models = res.data
        setAvailable((prev) => [...new Set([...prev, ...models])])
        toast.success(t('Fetched {{count}} models', { count: models.length }))
      } else {
        toast.error(res.message || t('Failed to fetch models'))
      }
    } catch {
      toast.error(t('Failed to fetch models'))
    } finally {
      setFetching(false)
    }
  }

  return (
    <div className='grid gap-4 sm:grid-cols-2'>
      <div className='grid gap-1.5'>
        <Label htmlFor={`${idPrefix}-name`}>{t('Channel name')}</Label>
        <Input
          id={`${idPrefix}-name`}
          value={value.name}
          onChange={(e) => set({ name: e.target.value })}
          placeholder={t('e.g. acme-01')}
        />
        <p className='text-muted-foreground text-xs'>
          {t(
            '1-10 characters, no commas, quotes, or leading/trailing spaces.'
          )}
        </p>
      </div>

      <div className='grid gap-1.5'>
        <Label htmlFor={`${idPrefix}-baseurl`}>{t('API Base URL')}</Label>
        <Input
          id={`${idPrefix}-baseurl`}
          value={value.base_url}
          onChange={(e) => set({ base_url: e.target.value })}
          placeholder='https://api.example.com'
        />
      </div>

      <div className='grid gap-1.5'>
        <Label htmlFor={`${idPrefix}-key`}>{t('API Key')}</Label>
        <Input
          id={`${idPrefix}-key`}
          value={value.key}
          onChange={(e) => set({ key: e.target.value })}
          placeholder='sk-...'
          autoComplete='off'
        />
      </div>

      <div className='grid gap-1.5'>
        <Label htmlFor={`${idPrefix}-note`}>{t('Channel notes')}</Label>
        <Input
          id={`${idPrefix}-note`}
          value={value.note ?? ''}
          onChange={(e) => set({ note: e.target.value })}
          placeholder={t('e.g. official direct connection, high concurrency')}
        />
      </div>

      <div className='sm:col-span-2'>
        <ModelPicker
          selected={selected}
          available={available}
          fetching={fetching}
          onSelectedChange={setSelected}
          onAvailableChange={setAvailable}
          onFetch={fetchModels}
        />
      </div>
    </div>
  )
}
