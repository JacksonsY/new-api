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
// jzlh-agent 代理分润/提现设置归位:成熟窗口与被邀请人账龄门原本无任何 UI(只能
// 戳原始 option API),提现三参原本散在提现审核页的弹窗里。统一收进系统设置一处。
// 金额/费率的美元与百分比换算沿用原弹窗同一套 helper,避免把提现下限配错。
import { zodResolver } from '@hookform/resolvers/zod'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import * as z from 'zod'

import { Input } from '@/components/design-system/input'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { parseQuotaFromDollars, quotaUnitsToDollars } from '@/lib/format'

import { SettingsForm } from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useResetForm } from '../hooks/use-reset-form'
import { useUpdateOption } from '../hooks/use-update-option'

const agentCommissionSchema = z.object({
  AgentCommissionMatureMinutes: z.string(),
  AgentInviteeMinAgeDays: z.string(),
  AgentWithdrawMinQuota: z.string(),
  AgentWithdrawFeeRate: z.string(),
  AgentWithdrawMaxPending: z.string(),
})

type AgentCommissionFormValues = z.infer<typeof agentCommissionSchema>

type RawAgentSettings = {
  AgentCommissionMatureMinutes: string
  AgentInviteeMinAgeDays: string
  AgentWithdrawMinQuota: string
  AgentWithdrawFeeRate: string
  AgentWithdrawMaxPending: string
}

// 原始 option(分钟/天/额度/0-1 费率/条数)→ 表单展示(分钟/天/美元/百分比/条数)。
function toDisplay(raw: RawAgentSettings): AgentCommissionFormValues {
  return {
    AgentCommissionMatureMinutes: raw.AgentCommissionMatureMinutes || '0',
    AgentInviteeMinAgeDays: raw.AgentInviteeMinAgeDays || '0',
    AgentWithdrawMinQuota: String(
      quotaUnitsToDollars(Number(raw.AgentWithdrawMinQuota) || 0)
    ),
    AgentWithdrawFeeRate: String(
      Number(((Number(raw.AgentWithdrawFeeRate) || 0) * 100).toFixed(1))
    ),
    AgentWithdrawMaxPending: raw.AgentWithdrawMaxPending || '0',
  }
}

const nonNegInt = (v: string) => String(Math.max(0, Math.round(Number(v) || 0)))

export function AgentCommissionSection({
  defaultValues,
}: {
  defaultValues: RawAgentSettings
}) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()

  const displayDefaults = toDisplay(defaultValues)
  const form = useForm<AgentCommissionFormValues>({
    resolver: zodResolver(agentCommissionSchema),
    defaultValues: displayDefaults,
  })
  useResetForm(form, displayDefaults)

  const onSubmit = async (data: AgentCommissionFormValues) => {
    const feePercent = Number(data.AgentWithdrawFeeRate) || 0
    const minDollars = Number(data.AgentWithdrawMinQuota) || 0
    // 全量写回:换算后逐项存原始 option 值(与原弹窗一致的口径),显式 Save 触发。
    const raw: RawAgentSettings = {
      AgentCommissionMatureMinutes: nonNegInt(data.AgentCommissionMatureMinutes),
      AgentInviteeMinAgeDays: nonNegInt(data.AgentInviteeMinAgeDays),
      AgentWithdrawMinQuota: String(
        Math.round(parseQuotaFromDollars(Math.max(0, minDollars)))
      ),
      AgentWithdrawFeeRate: String(Math.min(1, Math.max(0, feePercent / 100))),
      AgentWithdrawMaxPending: nonNegInt(data.AgentWithdrawMaxPending),
    }
    for (const [key, value] of Object.entries(raw)) {
      await updateOption.mutateAsync({ key, value })
    }
  }

  return (
    <SettingsSection title={t('Agent Commission & Withdrawal')}>
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)}>
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            isSaving={updateOption.isPending}
          />
          <FormField
            control={form.control}
            name='AgentCommissionMatureMinutes'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Commission maturation window (minutes)')}</FormLabel>
                <FormControl>
                  <Input inputMode='numeric' {...field} />
                </FormControl>
                <FormDescription>
                  {t(
                    'Commission is held this long before it becomes withdrawable. 0 = withdrawable immediately.'
                  )}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name='AgentInviteeMinAgeDays'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Minimum invitee account age (days)')}</FormLabel>
                <FormControl>
                  <Input inputMode='numeric' {...field} />
                </FormControl>
                <FormDescription>
                  {t(
                    'Anti-fraud: an invited user must be registered at least this long before their spend earns commission. 0 = no limit.'
                  )}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name='AgentWithdrawMinQuota'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Minimum withdrawal amount')}</FormLabel>
                <FormControl>
                  <Input inputMode='decimal' {...field} />
                </FormControl>
                <FormDescription>
                  {t('Agents cannot request less than this per withdrawal.')}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name='AgentWithdrawFeeRate'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Withdrawal fee rate')} (%)</FormLabel>
                <FormControl>
                  <Input inputMode='decimal' {...field} />
                </FormControl>
                <FormDescription>
                  {t('Deducted from the payout as a handling fee. 0 = no fee.')}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name='AgentWithdrawMaxPending'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Max pending requests per agent')}</FormLabel>
                <FormControl>
                  <Input inputMode='numeric' {...field} />
                </FormControl>
                <FormDescription>
                  {t('Caps unreviewed + in-progress requests. 0 = unlimited.')}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
        </SettingsForm>
      </Form>
    </SettingsSection>
  )
}
