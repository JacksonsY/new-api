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
// 蓝图C 充值自动升级分组配置（运营设置）：规则表格 + 总开关 + 受控链基准组 +
// OnlyNewTopups。规则以 JSON 字符串写入 payment_setting.auto_switch_group_rules。
import { Plus, Trash2 } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'

import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'

type Rule = { threshold_usd: number; group: string }

type Props = {
  defaultValues: {
    enabled: boolean
    onlyNewTopups: boolean
    baseGroup: string
    rules: string // JSON 数组字符串
  }
}

function parseRules(raw: string): Rule[] {
  if (!raw?.trim()) return []
  try {
    const parsed = JSON.parse(raw)
    if (!Array.isArray(parsed)) return []
    return parsed
      .filter((r) => r && typeof r === 'object')
      .map((r) => ({
        threshold_usd: Number(r.threshold_usd) || 0,
        group: String(r.group ?? ''),
      }))
  } catch {
    return []
  }
}

export function TopupAutoGroupSection({ defaultValues }: Props) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()

  const [enabled, setEnabled] = useState(defaultValues.enabled)
  const [onlyNewTopups, setOnlyNewTopups] = useState(
    defaultValues.onlyNewTopups
  )
  const [baseGroup, setBaseGroup] = useState(defaultValues.baseGroup || 'default')
  const [rules, setRules] = useState<Rule[]>(() =>
    parseRules(defaultValues.rules)
  )
  const [saving, setSaving] = useState(false)

  const initial = useMemo(
    () => ({
      enabled: defaultValues.enabled,
      onlyNewTopups: defaultValues.onlyNewTopups,
      baseGroup: defaultValues.baseGroup || 'default',
      rules: parseRules(defaultValues.rules),
    }),
    [defaultValues]
  )

  const addRule = () =>
    setRules((prev) => [...prev, { threshold_usd: 0, group: '' }])
  const removeRule = (idx: number) =>
    setRules((prev) => prev.filter((_, i) => i !== idx))
  const updateRule = (idx: number, patch: Partial<Rule>) =>
    setRules((prev) =>
      prev.map((r, i) => (i === idx ? { ...r, ...patch } : r))
    )

  async function onSave() {
    // 校验：启用时规则非空、阈值≥0、分组名非空、阈值不重复
    const cleaned = rules
      .map((r) => ({
        threshold_usd: Number(r.threshold_usd) || 0,
        group: r.group.trim(),
      }))
      .filter((r) => r.group !== '')
    if (enabled && cleaned.length === 0) {
      toast.error(t('Add at least one rule, or turn off auto-upgrade'))
      return
    }
    if (cleaned.some((r) => r.threshold_usd < 0)) {
      toast.error(t('Threshold must be >= 0'))
      return
    }
    const thresholds = cleaned.map((r) => r.threshold_usd)
    if (new Set(thresholds).size !== thresholds.length) {
      toast.error(t('Thresholds must be unique'))
      return
    }

    setSaving(true)
    try {
      const updates: { key: string; value: string | number | boolean }[] = []
      if (enabled !== initial.enabled) {
        updates.push({
          key: 'payment_setting.auto_switch_group_enabled',
          value: enabled,
        })
      }
      // OnlyNewTopups 打开的瞬间把生效起点打为当前时间（后端据此截断历史充值）；
      // 关闭则清零表示统计全部历史。
      if (onlyNewTopups !== initial.onlyNewTopups) {
        updates.push({
          key: 'payment_setting.auto_switch_group_only_new_topups',
          value: onlyNewTopups,
        })
        updates.push({
          key: 'payment_setting.auto_switch_group_enabled_from',
          value: onlyNewTopups ? Math.floor(Date.now() / 1000) : 0,
        })
      }
      if (baseGroup.trim() !== initial.baseGroup) {
        updates.push({
          key: 'payment_setting.auto_switch_group_base_group',
          value: baseGroup.trim() || 'default',
        })
      }
      const rulesJson = JSON.stringify(cleaned)
      if (rulesJson !== JSON.stringify(initial.rules)) {
        updates.push({
          key: 'payment_setting.auto_switch_group_rules',
          value: rulesJson,
        })
      }
      if (updates.length === 0) {
        toast.info(t('No changes'))
        return
      }
      for (const u of updates) {
        await updateOption.mutateAsync(u)
      }
      toast.success(t('Saved'))
    } catch {
      toast.error(t('Failed'))
    } finally {
      setSaving(false)
    }
  }

  return (
    <SettingsSection title={t('Top-up Auto Group Upgrade')}>
      <Alert>
        <AlertDescription>
          {t(
            'When a user’s cumulative successful top-up (USD) reaches a threshold, their group is upgraded automatically on the next payment. Users on manually-assigned groups outside the rule chain are never touched.'
          )}
        </AlertDescription>
      </Alert>

      <div className='flex items-center justify-between rounded-lg border p-3'>
        <div className='space-y-0.5'>
          <Label>{t('Enable auto group upgrade')}</Label>
        </div>
        <Switch checked={enabled} onCheckedChange={setEnabled} />
      </div>

      <div className='grid gap-4 sm:grid-cols-2'>
        <div className='grid gap-1.5'>
          <Label htmlFor='ag-base'>{t('Base group')}</Label>
          <Input
            id='ag-base'
            value={baseGroup}
            onChange={(e) => setBaseGroup(e.target.value)}
            placeholder='default'
          />
          <p className='text-muted-foreground text-xs'>
            {t(
              'The controlled chain = base group + all target groups. Only users currently in the chain are auto-switched.'
            )}
          </p>
        </div>
        <div className='flex items-start gap-2 pt-6'>
          <Switch
            id='ag-only-new'
            checked={onlyNewTopups}
            onCheckedChange={setOnlyNewTopups}
          />
          <div className='space-y-0.5'>
            <Label htmlFor='ag-only-new'>{t('Only count new top-ups')}</Label>
            <p className='text-muted-foreground text-xs'>
              {t(
                'Only top-ups made after enabling this count toward the cumulative total. Existing history is ignored.'
              )}
            </p>
          </div>
        </div>
      </div>

      <div className='grid gap-2'>
        <div className='flex items-center justify-between'>
          <Label>{t('Upgrade rules')}</Label>
          <Button type='button' variant='outline' size='sm' onClick={addRule}>
            <Plus className='size-4' />
            {t('Add rule')}
          </Button>
        </div>
        {rules.length === 0 ? (
          <div className='text-muted-foreground rounded-lg border border-dashed p-4 text-center text-sm'>
            {t('No rules yet')}
          </div>
        ) : (
          <div className='grid gap-2'>
            {rules.map((rule, idx) => (
              // eslint-disable-next-line react/no-array-index-key -- 行内可编辑，阈值/分组随输入变动，index 是唯一稳定标识
              <div key={idx} className='flex items-center gap-2'>
                <div className='grid flex-1 gap-1'>
                  <span className='text-muted-foreground text-xs'>
                    {t('Cumulative top-up ≥ (USD)')}
                  </span>
                  <Input
                    type='number'
                    step='0.01'
                    min='0'
                    inputMode='decimal'
                    value={rule.threshold_usd}
                    onChange={(e) =>
                      updateRule(idx, {
                        threshold_usd: Number(e.target.value),
                      })
                    }
                  />
                </div>
                <div className='grid flex-1 gap-1'>
                  <span className='text-muted-foreground text-xs'>
                    {t('Target group')}
                  </span>
                  <Input
                    value={rule.group}
                    onChange={(e) => updateRule(idx, { group: e.target.value })}
                    placeholder='vip'
                  />
                </div>
                <Button
                  type='button'
                  variant='ghost'
                  size='icon'
                  className='mt-5 shrink-0'
                  onClick={() => removeRule(idx)}
                  aria-label={t('Remove')}
                >
                  <Trash2 className='size-4' />
                </Button>
              </div>
            ))}
          </div>
        )}
      </div>

      <div className='flex justify-end'>
        <Button onClick={onSave} disabled={saving}>
          {saving ? t('Saving...') : t('Save')}
        </Button>
      </div>
    </SettingsSection>
  )
}
