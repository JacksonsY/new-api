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
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/design-system/select'
import { Label } from '@/components/ui/label'

import {
  payoutMethodLabelKey,
  SUPPLIER_PAYOUT_METHODS,
  type SupplierPayoutInfo,
} from '../types'

// 受控的收款/联系方式字段组（入驻申请与收益页编辑复用）。
// 注：EMPTY_PAYOUT_INFO / validatePayoutInfo 收敛在 ../types，本文件只导出组件
// （满足 react-refresh 的 only-export-components）。
export function PayoutInfoFields({
  value,
  onChange,
  idPrefix = 'payout',
}: {
  value: SupplierPayoutInfo
  onChange: (next: SupplierPayoutInfo) => void
  idPrefix?: string
}) {
  const { t } = useTranslation()
  const set = (patch: Partial<SupplierPayoutInfo>) =>
    onChange({ ...value, ...patch })

  return (
    <div className='grid gap-4'>
      <div className='grid content-start gap-1.5'>
        <Label>{t('Payout Method')}</Label>
        <Select
          value={value.method}
          onValueChange={(v) => set({ method: v ?? 'alipay' })}
        >
          <SelectTrigger className='w-full'>
            <SelectValue>{t(payoutMethodLabelKey(value.method))}</SelectValue>
          </SelectTrigger>
          <SelectContent alignItemWithTrigger={false}>
            {SUPPLIER_PAYOUT_METHODS.map((m) => (
              <SelectItem key={m} value={m}>
                {t(payoutMethodLabelKey(m))}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      <div className='grid gap-1.5'>
        <Label htmlFor={`${idPrefix}-account`}>{t('Payout Account')}</Label>
        <Input
          id={`${idPrefix}-account`}
          value={value.account}
          onChange={(e) => set({ account: e.target.value })}
          placeholder={t('Account number / address for the selected method')}
          autoComplete='off'
        />
      </div>

      <div className='grid gap-1.5'>
        <Label htmlFor={`${idPrefix}-name`}>{t('Account Holder Name')}</Label>
        <Input
          id={`${idPrefix}-name`}
          value={value.name}
          onChange={(e) => set({ name: e.target.value })}
          autoComplete='off'
        />
      </div>

      <div className='grid gap-1.5'>
        <Label htmlFor={`${idPrefix}-contact`}>{t('Contact')}</Label>
        <Input
          id={`${idPrefix}-contact`}
          value={value.contact}
          onChange={(e) => set({ contact: e.target.value })}
          placeholder={t('WeChat / QQ / Telegram / Email')}
          autoComplete='off'
        />
      </div>
    </div>
  )
}
