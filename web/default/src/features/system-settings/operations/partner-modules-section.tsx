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
// jzlh v2 P2 招商模块运营设置:代理/供应商入驻开关(只拦新入驻,存量身份
// 照常结算)、代理审批默认分润、供应商结算参数(此前是 Go 硬编码常量)。
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
import { Switch } from '@/components/ui/switch'

import {
  SettingsForm,
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useResetForm } from '../hooks/use-reset-form'
import { useUpdateOption } from '../hooks/use-update-option'

const partnerModulesSchema = z.object({
  AgentEnabled: z.boolean(),
  SupplierEnabled: z.boolean(),
  AgentDefaultProfitRate: z.string(),
  SupplierMatureDays: z.string(),
  SupplierMaxRate: z.string(),
})

type PartnerModulesFormValues = z.infer<typeof partnerModulesSchema>

type RawPartnerModulesSettings = {
  AgentEnabled: boolean
  SupplierEnabled: boolean
  AgentDefaultProfitRate: string
  SupplierMatureDays: string
  SupplierMaxRate: string
}

// 原始 option(0-1 费率)→ 表单展示(百分比)。
function toDisplay(
  raw: RawPartnerModulesSettings
): PartnerModulesFormValues {
  return {
    AgentEnabled: raw.AgentEnabled,
    SupplierEnabled: raw.SupplierEnabled,
    AgentDefaultProfitRate: String(
      Number(((Number(raw.AgentDefaultProfitRate) || 0) * 100).toFixed(1))
    ),
    SupplierMatureDays: raw.SupplierMatureDays || '3',
    SupplierMaxRate: String(
      Number(((Number(raw.SupplierMaxRate) || 1) * 100).toFixed(1))
    ),
  }
}

export function PartnerModulesSection({
  defaultValues,
}: {
  defaultValues: RawPartnerModulesSettings
}) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()

  const displayDefaults = toDisplay(defaultValues)
  const form = useForm<PartnerModulesFormValues>({
    resolver: zodResolver(partnerModulesSchema),
    defaultValues: displayDefaults,
  })
  useResetForm(form, displayDefaults)

  const onSubmit = async (data: PartnerModulesFormValues) => {
    const profitPercent = Number(data.AgentDefaultProfitRate) || 0
    const maxRatePercent = Number(data.SupplierMaxRate) || 100
    const raw: Record<string, string | boolean> = {
      AgentEnabled: data.AgentEnabled,
      SupplierEnabled: data.SupplierEnabled,
      AgentDefaultProfitRate: String(
        Math.min(1, Math.max(0, profitPercent / 100))
      ),
      SupplierMatureDays: String(
        Math.max(0, Math.round(Number(data.SupplierMatureDays) || 0))
      ),
      SupplierMaxRate: String(
        Math.min(1, Math.max(0.01, maxRatePercent / 100))
      ),
    }
    for (const [key, value] of Object.entries(raw)) {
      await updateOption.mutateAsync({ key, value })
    }
  }

  return (
    <SettingsSection title={t('Partner Modules')}>
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)}>
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            isSaving={updateOption.isPending}
          />
          {(
            [
              {
                name: 'AgentEnabled',
                label: t('Agent module'),
                desc: t(
                  'Off: no new agent applications. Existing agents keep earning and withdrawing.'
                ),
              },
              {
                name: 'SupplierEnabled',
                label: t('Supplier module'),
                desc: t(
                  'Off: no new supplier onboarding. Existing supplier channels and settlement continue.'
                ),
              },
            ] as const
          ).map((item) => (
            <FormField
              key={item.name}
              control={form.control}
              name={item.name}
              render={({ field }) => (
                <SettingsSwitchItem>
                  <SettingsSwitchContent>
                    <FormLabel>{item.label}</FormLabel>
                    <FormDescription>{item.desc}</FormDescription>
                  </SettingsSwitchContent>
                  <FormControl>
                    <Switch
                      checked={field.value}
                      onCheckedChange={field.onChange}
                    />
                  </FormControl>
                </SettingsSwitchItem>
              )}
            />
          ))}
          <FormField
            control={form.control}
            name='AgentDefaultProfitRate'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Default agent profit rate')} (%)</FormLabel>
                <FormControl>
                  <Input inputMode='decimal' {...field} />
                </FormControl>
                <FormDescription>
                  {t(
                    'Pre-filled in the agent application review dialog to avoid typing it per approval.'
                  )}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name='SupplierMatureDays'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Supplier maturation window (days)')}</FormLabel>
                <FormControl>
                  <Input inputMode='numeric' {...field} />
                </FormControl>
                <FormDescription>
                  {t(
                    'Supplier earnings older than this count toward Payable (covers refunds and risk checks).'
                  )}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name='SupplierMaxRate'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Supplier quote rate cap')} (%)</FormLabel>
                <FormControl>
                  <Input inputMode='decimal' {...field} />
                </FormControl>
                <FormDescription>
                  {t(
                    'Hard cap for channel quote rates (approval and price-change requests). 100% means supplier cost equals list price — platform margin zero.'
                  )}
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
