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
import type { LucideIcon } from 'lucide-react'

import { Skeleton } from '@/components/ui/skeleton'
import { cn } from '@/lib/utils'

export type MetricTone =
  | 'default'
  | 'success'
  | 'warning'
  | 'destructive'
  | 'muted'

const TONE_CLASSES: Record<MetricTone, string> = {
  default: 'text-foreground',
  success: 'text-success',
  warning: 'text-warning',
  destructive: 'text-destructive',
  muted: 'text-muted-foreground',
}

interface MetricTileProps {
  icon: LucideIcon
  label: string
  value: string | number
  hint?: string
  tone?: MetricTone
  loading?: boolean
}

export function MetricTile(props: MetricTileProps) {
  const Icon = props.icon
  return (
    <div className='bg-card flex min-h-24 flex-col justify-between gap-2 rounded-xl border p-3 sm:p-4'>
      <div className='text-muted-foreground flex items-center gap-1.5 text-xs font-medium'>
        <Icon
          className='text-muted-foreground/60 size-3.5 shrink-0'
          aria-hidden
        />
        <span className='truncate'>{props.label}</span>
      </div>
      {props.loading ? (
        <Skeleton className='h-7 w-16' />
      ) : (
        <div
          className={cn(
            'font-mono text-2xl font-semibold tracking-tight tabular-nums',
            TONE_CLASSES[props.tone ?? 'default']
          )}
        >
          {props.value}
        </div>
      )}
      {props.hint ? (
        <p className='text-muted-foreground/60 truncate text-[11px]'>
          {props.hint}
        </p>
      ) : null}
    </div>
  )
}
