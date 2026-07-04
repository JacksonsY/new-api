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
// 蓝图C 充值自动升级档位展示（营销）：档位表 + 当前累计充值进度 +
// "再充 X 升级 Y"提示。纯展示，判定在后端结算事务内完成。
import { Crown } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'

import type { AutoGroupInfo } from '../types'

export function AutoGroupTiersCard({ info }: { info: AutoGroupInfo }) {
  const { t } = useTranslation()
  if (!info?.enabled || !info.rules?.length) return null

  const total = info.total_topup_usd || 0
  const fmt = (n: number) =>
    `$${Number.isInteger(n) ? n : n.toFixed(2)}`

  // 当前已达最高档 + 下一档（用于进度提示）
  const reached = info.rules.filter((r) => total >= r.threshold_usd)
  const currentTier = reached.at(-1) ?? null
  const nextTier = info.rules.find((r) => total < r.threshold_usd) || null

  return (
    <div className='space-y-3 rounded-lg border bg-muted/30 p-4'>
      <div className='flex items-center gap-1.5'>
        <Crown className='size-4 text-amber-500' />
        <span className='text-sm font-medium'>
          {t('Top-up rewards: spend more, save more')}
        </span>
      </div>

      {/* 进度提示 */}
      <div className='text-muted-foreground text-xs'>
        {t('Cumulative top-up')}:{' '}
        <span className='text-foreground font-medium tabular-nums'>
          {fmt(total)}
        </span>
        {currentTier && (
          <>
            {' · '}
            {t('current tier')}:{' '}
            <Badge variant='secondary' className='align-middle'>
              {currentTier.group}
            </Badge>
          </>
        )}
      </div>
      {nextTier && (
        <div className='text-xs text-amber-600 dark:text-amber-400'>
          {t('Top up {{amount}} more to reach {{group}}', {
            amount: fmt(Math.max(0, nextTier.threshold_usd - total)),
            group: nextTier.group,
          })}
        </div>
      )}

      {/* 档位表 */}
      <div className='grid gap-1.5'>
        {info.rules.map((rule) => {
          const done = total >= rule.threshold_usd
          return (
            <div
              key={`${rule.threshold_usd}-${rule.group}`}
              className='flex items-center justify-between rounded-md border px-3 py-1.5 text-sm'
            >
              <span className='text-muted-foreground'>
                {t('Cumulative ≥ {{amount}}', {
                  amount: fmt(rule.threshold_usd),
                })}
              </span>
              <div className='flex items-center gap-2'>
                <Badge variant={done ? 'default' : 'outline'}>
                  {rule.group}
                </Badge>
                {done && (
                  <span className='text-xs text-green-600'>
                    {t('reached')}
                  </span>
                )}
              </div>
            </div>
          )
        })}
      </div>
    </div>
  )
}
