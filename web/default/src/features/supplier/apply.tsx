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
import {
  CheckCircle2,
  Clock,
  PackageOpen,
  ShieldAlert,
  ShieldX,
} from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/design-system/button'
import { SectionPageLayout } from '@/components/layout'
import { Card, CardContent } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/design-system/table'
import { formatTimestamp } from '@/lib/format'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

import { getSupplierProfile, listSupplierApplications, supplierApply } from './api'
import { ChannelAuditBadge } from './channel-audit-badge'
import { ChannelOfferFields } from './components/channel-offer-fields'
import { MerchantProfileFields } from './components/merchant-profile-fields'
import {
  EMPTY_CHANNEL_FORM,
  EMPTY_SUPPLIER_PROFILE,
  SUPPLIER_STATUS,
  type SupplierChannel,
  type SupplierChannelForm,
  type SupplierProfileForm,
  validateChannelForm,
  validateSupplierProfileForm,
} from './types'

// 「入驻申请」——商户资料 + 渠道申请（含一键获取模型）+ 申请记录。首次入驻收商户资料 + 一条渠道；
// 已在途/已通过可继续追加渠道要约（商户资料预填、自动带上）。管理员/暂停有独立状态态。
export function SupplierApply() {
  const { t } = useTranslation()
  const [submitting, setSubmitting] = useState(false)
  const [profile, setProfile] = useState<SupplierProfileForm>(
    EMPTY_SUPPLIER_PROFILE
  )
  const [channel, setChannel] = useState<SupplierChannelForm>(EMPTY_CHANNEL_FORM)

  // 管理员及以上不能作为供应商（裁判/运动员），与后端 EnsureSupplierApplied 一致。
  const user = useAuthStore((s) => s.auth.user)
  const isAdmin = (user?.role ?? 0) >= ROLE.ADMIN

  const { data, isLoading, refetch } = useQuery({
    queryKey: ['supplier-profile'],
    enabled: !isAdmin,
    queryFn: async () => {
      const res = await getSupplierProfile()
      if (!res.success) {
        toast.error(res.message || t('Failed to load'))
        return null
      }
      return res.data ?? null
    },
  })

  const status = data?.supplier_status ?? SUPPLIER_STATUS.NONE
  const isSuspended = status === SUPPLIER_STATUS.SUSPENDED
  // 商户资料区：资料不全就显示（首次入驻，或本功能前入驻、name/contact 尚空的老供应商，
  // 否则会在隐藏状态下卡在必填校验）。基于已加载的 data 判定，避免随实时输入中途消失。
  const showMerchantProfile =
    !data?.supplier_name?.trim() || !data?.supplier_contact?.trim()

  // 申请记录（owner scope、任意审核态）：pending 申请者也能看，故不依赖供应商权限。
  const { data: records, refetch: refetchRecords } = useQuery({
    queryKey: ['supplier-applications'],
    enabled: !isAdmin && !isSuspended,
    queryFn: async () => {
      const res = await listSupplierApplications(1, 50)
      return res.success ? (res.data?.items ?? []) : []
    },
  })

  // 已保存过商户资料时预填（追加渠道要约时后端仍需商户字段，自动带上）。
  useEffect(() => {
    if (!data) return
    if (data.supplier_name || data.supplier_contact || data.supplier_intro) {
      setProfile({
        name: data.supplier_name || '',
        contact: data.supplier_contact || '',
        intro: data.supplier_intro || '',
      })
    }
  }, [data])

  async function onSubmit() {
    const pErr = validateSupplierProfileForm(profile)
    if (pErr) {
      toast.error(t(pErr))
      return
    }
    const cErr = validateChannelForm(channel)
    if (cErr) {
      toast.error(t(cErr))
      return
    }
    setSubmitting(true)
    try {
      const res = await supplierApply({ profile, channel })
      if (res.success) {
        toast.success(t('Application submitted'))
        setChannel(EMPTY_CHANNEL_FORM) // 清空以便继续追加下一条要约
        refetch()
        refetchRecords()
      } else {
        toast.error(res.message || t('Failed'))
      }
    } catch {
      toast.error(t('Failed'))
    } finally {
      setSubmitting(false)
    }
  }

  let content: React.ReactNode
  if (isAdmin) {
    content = (
      <StateCard
        icon={<ShieldAlert className='text-muted-foreground size-8' />}
        title={t('Supplier program is not available for administrators')}
        description={t(
          'Administrator accounts cannot act as suppliers. Please use a regular account to join the supplier program.'
        )}
      />
    )
  } else if (isLoading) {
    content = (
      <Card data-card-hover='false'>
        <CardContent className='space-y-3 p-6'>
          <Skeleton className='h-6 w-40' />
          <Skeleton className='h-4 w-full' />
          <Skeleton className='h-9 w-32' />
        </CardContent>
      </Card>
    )
  } else if (isSuspended) {
    content = (
      <StateCard
        icon={<ShieldX className='text-destructive size-8' />}
        title={t('Your supplier access is suspended')}
        description={t('Please contact the administrator for more information.')}
      />
    )
  } else {
    content = (
      <div className='space-y-4'>
        {status === SUPPLIER_STATUS.PENDING && (
          <StateCard
            icon={<Clock className='text-warning size-8' />}
            title={t('Your application is under review')}
            description={t(
              'We are reviewing your supplier application. You can submit more channel offers below in the meantime.'
            )}
          />
        )}
        {status === SUPPLIER_STATUS.APPROVED && (
          <StateCard
            icon={<CheckCircle2 className='text-success size-8' />}
            title={t('You are an approved supplier')}
            description={t(
              'You can submit more channel offers below, or manage existing ones in My Channels.'
            )}
          />
        )}
        <ApplicationForm
          isFirst={status === SUPPLIER_STATUS.NONE}
          showMerchantProfile={showMerchantProfile}
          profile={profile}
          onProfileChange={setProfile}
          channel={channel}
          onChannelChange={setChannel}
          submitting={submitting}
          onSubmit={onSubmit}
        />
        <RecordsCard
          records={records ?? []}
          merchantName={profile.name}
          contact={profile.contact}
        />
      </div>
    )
  }

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>
        {t('Onboarding Application')}
      </SectionPageLayout.Title>
      <SectionPageLayout.Content>
        <div className='mx-auto w-full max-w-6xl'>{content}</div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}

// 申请表单：首次(isFirst)收商户资料 + 渠道申请；追加时只收渠道（商户资料已存、预填自动带上）。
function ApplicationForm({
  isFirst,
  showMerchantProfile,
  profile,
  onProfileChange,
  channel,
  onChannelChange,
  submitting,
  onSubmit,
}: {
  isFirst: boolean
  showMerchantProfile: boolean
  profile: SupplierProfileForm
  onProfileChange: (next: SupplierProfileForm) => void
  channel: SupplierChannelForm
  onChannelChange: (next: SupplierChannelForm) => void
  submitting: boolean
  onSubmit: () => void
}) {
  const { t } = useTranslation()
  return (
    <Card data-card-hover='false'>
      <CardContent className='space-y-6 p-6'>
        {showMerchantProfile && (
          <section className='space-y-3'>
            <div className='flex items-start gap-3'>
              <div className='bg-muted flex size-10 shrink-0 items-center justify-center rounded-full'>
                <PackageOpen className='text-primary size-5' />
              </div>
              <div className='space-y-0.5'>
                <h2 className='text-base font-semibold'>
                  {t('Merchant profile')}
                </h2>
                <p className='text-muted-foreground text-sm leading-relaxed'>
                  {t(
                    'First-time onboarding requires a merchant name and contact so reviewers can reach you.'
                  )}
                </p>
              </div>
            </div>
            <MerchantProfileFields
              value={profile}
              onChange={onProfileChange}
              idPrefix='apply-merchant'
            />
          </section>
        )}

        <section className='space-y-3'>
          <div className='space-y-0.5'>
            <h2 className='text-base font-semibold'>
              {isFirst
                ? t('Channel application')
                : t('Submit another channel offer')}
            </h2>
            <p className='text-muted-foreground text-sm leading-relaxed'>
              {t(
                'Fill in the upstream URL, key and model list, then submit for review.'
              )}
            </p>
          </div>
          <ChannelOfferFields
            value={channel}
            onChange={onChannelChange}
            idPrefix='apply-offer'
          />
        </section>

        <div>
          <Button onClick={onSubmit} disabled={submitting}>
            {submitting ? t('Submitting...') : t('Submit Application')}
          </Button>
        </div>
      </CardContent>
    </Card>
  )
}

// 申请记录：owner 名下已提交的渠道入驻请求（含审核态与审核原因）。
function RecordsCard({
  records,
  merchantName,
  contact,
}: {
  records: SupplierChannel[]
  merchantName: string
  contact: string
}) {
  const { t } = useTranslation()
  return (
    <Card data-card-hover='false'>
      <CardContent className='space-y-3 p-6'>
        <div className='space-y-0.5'>
          <h2 className='text-base font-semibold'>{t('Application records')}</h2>
          <p className='text-muted-foreground text-sm leading-relaxed'>
            {t('Your most recent channel onboarding requests.')}
          </p>
        </div>
        <div className='overflow-x-auto'>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t('ID')}</TableHead>
                <TableHead>{t('Merchant name')}</TableHead>
                <TableHead>{t('Channel name')}</TableHead>
                <TableHead>{t('Models')}</TableHead>
                <TableHead>{t('Submitted at')}</TableHead>
                <TableHead>{t('Contact')}</TableHead>
                <TableHead>{t('Status')}</TableHead>
                <TableHead>{t('Review reason')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {records.length === 0 ? (
                <TableRow>
                  <TableCell
                    colSpan={8}
                    className='text-muted-foreground py-8 text-center text-sm'
                  >
                    {t('No application records yet')}
                  </TableCell>
                </TableRow>
              ) : (
                records.map((r) => (
                  <TableRow key={r.id}>
                    <TableCell className='text-muted-foreground'>
                      {r.id}
                    </TableCell>
                    <TableCell>{merchantName || '-'}</TableCell>
                    <TableCell className='font-medium'>{r.name}</TableCell>
                    <TableCell>
                      <span
                        className='block max-w-[220px] truncate'
                        title={r.models}
                      >
                        {r.models || '-'}
                      </span>
                    </TableCell>
                    <TableCell className='text-muted-foreground whitespace-nowrap'>
                      {r.created_time ? formatTimestamp(r.created_time) : '-'}
                    </TableCell>
                    <TableCell>{contact || '-'}</TableCell>
                    <TableCell>
                      <ChannelAuditBadge status={r.audit_status} />
                    </TableCell>
                    <TableCell className='text-muted-foreground'>
                      {r.remark || '-'}
                    </TableCell>
                  </TableRow>
                ))
              )}
            </TableBody>
          </Table>
        </div>
      </CardContent>
    </Card>
  )
}

function StateCard({
  icon,
  title,
  description,
}: {
  icon: React.ReactNode
  title: string
  description: string
}) {
  return (
    <Card data-card-hover='false'>
      <CardContent className='flex flex-col items-center gap-3 p-8 text-center'>
        <div className='bg-muted flex size-16 items-center justify-center rounded-full'>
          {icon}
        </div>
        <h2 className='text-base font-semibold'>{title}</h2>
        <p className='text-muted-foreground max-w-md text-sm leading-relaxed'>
          {description}
        </p>
      </CardContent>
    </Card>
  )
}
