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
import { useTranslation } from 'react-i18next'

import { Input } from '@/components/design-system/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'

import type { SupplierProfileForm } from '../types'

// 受控的商户资料字段组（入驻申请首次填写）：商户名称 + 联系方式 + 商户简介。
// 与收款账户解耦——审核员通过这些信息沟通，收款账户走审核通过后的收款设置页。
export function MerchantProfileFields({
  value,
  onChange,
  idPrefix = 'merchant',
}: {
  value: SupplierProfileForm
  onChange: (next: SupplierProfileForm) => void
  idPrefix?: string
}) {
  const { t } = useTranslation()
  const set = (patch: Partial<SupplierProfileForm>) =>
    onChange({ ...value, ...patch })

  return (
    <div className='grid gap-4 sm:grid-cols-2'>
      <div className='grid gap-1.5'>
        <Label htmlFor={`${idPrefix}-name`}>{t('Merchant name')}</Label>
        <Input
          id={`${idPrefix}-name`}
          value={value.name}
          onChange={(e) => set({ name: e.target.value })}
          placeholder={t('e.g. A6 Premium Model Supplier')}
        />
      </div>

      <div className='grid gap-1.5'>
        <Label htmlFor={`${idPrefix}-contact`}>{t('Contact')}</Label>
        <Input
          id={`${idPrefix}-contact`}
          value={value.contact}
          onChange={(e) => set({ contact: e.target.value })}
          placeholder={t('WeChat / Telegram / Email')}
        />
      </div>

      <div className='grid gap-1.5 sm:col-span-2'>
        <Label htmlFor={`${idPrefix}-intro`}>{t('Merchant introduction')}</Label>
        <Textarea
          id={`${idPrefix}-intro`}
          value={value.intro}
          onChange={(e) => set({ intro: e.target.value })}
          placeholder={t(
            'Service capabilities, available regions, stability notes, etc.'
          )}
          rows={3}
        />
      </div>
    </div>
  )
}
