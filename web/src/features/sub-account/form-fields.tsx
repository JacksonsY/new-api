// jzlh-sub 子账号表单公共字段：功能权限开关 + 三档额度输入（创建/编辑共用）。
import { useTranslation } from 'react-i18next'

import { Input } from '@/components/design-system/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { cn } from '@/lib/utils'

import {
  ROLE_PRESET_ADMIN,
  ROLE_PRESET_USER,
  SUB_ADMIN_PERMISSIONS,
  SUB_CORE_PERMISSIONS,
  type LimitInput,
} from './types'

// 权限 key → 展示名 i18n key
const PERM_LABELS: Record<string, string> = {
  playground: 'Playground',
  api_keys: 'API Keys',
  usage_logs: 'Usage Logs',
  wallet: 'Wallet (recharge)',
  team_management: 'Team Management',
}

export function PermissionToggles({
  preset,
  onPresetChange,
  permissions,
  onToggle,
  canGrantAdmin,
}: {
  preset: string
  onPresetChange: (p: string) => void
  permissions: Record<string, boolean>
  onToggle: (key: string, on: boolean) => void
  canGrantAdmin: boolean // 仅 Owner 能授予管理员预设 / 高权限
}) {
  const { t } = useTranslation()
  const isAdmin = preset === ROLE_PRESET_ADMIN
  const keys = isAdmin
    ? [...SUB_CORE_PERMISSIONS, ...SUB_ADMIN_PERMISSIONS]
    : [...SUB_CORE_PERMISSIONS]
  return (
    <div className='space-y-3'>
      <div className='flex items-center gap-4'>
        <Label className='shrink-0'>{t('Feature Permissions')}</Label>
        <label className='flex items-center gap-1.5 text-sm'>
          <input
            type='radio'
            checked={!isAdmin}
            onChange={() => onPresetChange(ROLE_PRESET_USER)}
          />
          {t('Normal User')}
        </label>
        <label
          className={cn(
            'flex items-center gap-1.5 text-sm',
            !canGrantAdmin && 'opacity-50'
          )}
        >
          <input
            type='radio'
            checked={isAdmin}
            disabled={!canGrantAdmin}
            onChange={() => onPresetChange(ROLE_PRESET_ADMIN)}
          />
          {t('Administrator')}
        </label>
      </div>
      <div className='grid grid-cols-2 gap-x-6 gap-y-2 sm:grid-cols-3'>
        {keys.map((key) => (
          <label
            key={key}
            className='flex items-center justify-between gap-2 text-sm'
          >
            <span className='truncate'>{t(PERM_LABELS[key] ?? key)}</span>
            <Switch
              checked={!!permissions[key]}
              onCheckedChange={(v) => onToggle(key, v)}
            />
          </label>
        ))}
      </div>
    </div>
  )
}

export function LimitField({
  label,
  value,
  onChange,
}: {
  label: string
  value: LimitInput
  onChange: (v: LimitInput) => void
}) {
  const { t } = useTranslation()
  return (
    <div className='flex items-center gap-3'>
      <Label className='w-16 shrink-0'>{label}</Label>
      <label className='flex shrink-0 items-center gap-1.5 text-sm'>
        <Switch
          checked={value.unlimited}
          onCheckedChange={(v) => onChange({ ...value, unlimited: v })}
        />
        {t('Unlimited')}
      </label>
      {!value.unlimited && (
        <div className='flex items-center gap-1'>
          <Input
            type='number'
            min={0}
            step='0.01'
            value={value.value || ''}
            onChange={(e) =>
              onChange({ ...value, value: Number(e.target.value) || 0 })
            }
            placeholder={t('Enter limit')}
            className='w-32'
          />
          <span className='text-muted-foreground text-sm'>USD</span>
        </div>
      )}
    </div>
  )
}
