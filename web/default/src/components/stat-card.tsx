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
import type { ReactNode } from 'react'

import { Card, CardContent } from '@/components/ui/card'

// 各业务中心（代理/团队等）共用的统计卡（图标 + 数值 + 标签）。
export function StatCard({
  icon,
  label,
  value,
  emphasize = false,
}: {
  icon: ReactNode
  label: string
  value: string
  emphasize?: boolean
}) {
  return (
    <Card data-card-hover='false' className='py-0'>
      <CardContent className='flex items-center gap-3.5 p-4'>
        <div
          className={
            emphasize
              ? 'bg-primary/10 text-primary flex size-10 shrink-0 items-center justify-center rounded-full'
              : 'bg-muted text-muted-foreground flex size-10 shrink-0 items-center justify-center rounded-full'
          }
        >
          {icon}
        </div>
        <div className='min-w-0'>
          <div className='truncate text-xl font-semibold tabular-nums'>
            {value}
          </div>
          <div className='text-muted-foreground truncate text-xs'>{label}</div>
        </div>
      </CardContent>
    </Card>
  )
}
