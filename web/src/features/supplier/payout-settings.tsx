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
import { useQuery } from '@tanstack/react-query'
import { Wallet } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/design-system/button'
import { SectionPageLayout } from '@/components/layout'
import { Card, CardContent } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'

import { getSupplierProfile, updateSupplierPayoutInfo } from './api'
import { PayoutInfoFields } from './components/payout-info-fields'
import {
  EMPTY_PAYOUT_INFO,
  type SupplierPayoutInfo,
  validatePayoutInfo,
} from './types'

// 「收款设置」——审核通过后的供应商在此填写/更新结构化收款账户（支付宝/银行卡/USDT），
// 人工打款依据。商户资料（名称/联系方式/简介）在入驻申请页维护，与收款账户解耦。
export function SupplierPayoutSettings() {
  const { t } = useTranslation()
  const [saving, setSaving] = useState(false)
  const [payout, setPayout] = useState<SupplierPayoutInfo>(EMPTY_PAYOUT_INFO)

  const { data, isLoading } = useQuery({
    queryKey: ['supplier-profile'],
    queryFn: async () => {
      const res = await getSupplierProfile()
      if (!res.success) {
        toast.error(res.message || t('Failed to load'))
        return null
      }
      return res.data ?? null
    },
  })

  useEffect(() => {
    if (!data) return
    setPayout({
      method: data.supplier_payout_method || 'alipay',
      account: data.supplier_payout_account || '',
      name: data.supplier_payout_name || '',
      contact: data.supplier_contact || '',
    })
  }, [data])

  async function onSave() {
    const err = validatePayoutInfo(payout)
    if (err) {
      toast.error(t(err))
      return
    }
    setSaving(true)
    try {
      const res = await updateSupplierPayoutInfo(payout)
      if (res.success) {
        toast.success(t('Saved'))
      } else {
        toast.error(res.message || t('Failed'))
      }
    } catch {
      toast.error(t('Failed'))
    } finally {
      setSaving(false)
    }
  }

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>{t('Payout Settings')}</SectionPageLayout.Title>
      <SectionPageLayout.Content>
        <div className='mx-auto w-full max-w-2xl'>
          <Card data-card-hover='false'>
            <CardContent className='space-y-5 p-6'>
              {isLoading ? (
                <div className='space-y-3'>
                  <Skeleton className='h-6 w-40' />
                  <Skeleton className='h-9 w-full' />
                  <Skeleton className='h-9 w-32' />
                </div>
              ) : (
                <>
                  <div className='flex items-start gap-3'>
                    <div className='bg-muted flex size-11 shrink-0 items-center justify-center rounded-full'>
                      <Wallet className='text-primary size-6' />
                    </div>
                    <div className='space-y-1'>
                      <h2 className='text-base font-semibold'>
                        {t('Payout Account')}
                      </h2>
                      <p className='text-muted-foreground text-sm leading-relaxed'>
                        {t(
                          'Payouts are made manually by the administrator. Please keep your payout details accurate to receive your earnings.'
                        )}
                      </p>
                    </div>
                  </div>
                  <PayoutInfoFields
                    value={payout}
                    onChange={setPayout}
                    idPrefix='payout-settings'
                  />
                  <div>
                    <Button onClick={onSave} disabled={saving}>
                      {saving ? t('Saving...') : t('Save')}
                    </Button>
                  </div>
                </>
              )}
            </CardContent>
          </Card>
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
