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
import { Loader2, RefreshCw } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/design-system/button'
import { Input } from '@/components/design-system/input'
import { cn } from '@/lib/utils'

// 模型多选器：一键获取上游模型 + 全选/反选/清空 + 回车手动追加。展示池 = 已获取 ∪ 已选，
// 保证已选模型始终可见。selected/available 由父级持有（父级把 selected 折成逗号串存表单）。
export function ModelPicker({
  selected,
  available,
  fetching,
  onSelectedChange,
  onAvailableChange,
  onFetch,
}: {
  selected: string[]
  available: string[]
  fetching: boolean
  onSelectedChange: (next: string[]) => void
  onAvailableChange: (next: string[]) => void
  onFetch: () => void
}) {
  const { t } = useTranslation()
  const [input, setInput] = useState('')

  const pool = useMemo(
    () => [...new Set([...available, ...selected])],
    [available, selected]
  )
  const selectedSet = useMemo(() => new Set(selected), [selected])

  const toggle = (m: string) => {
    if (selectedSet.has(m)) {
      onSelectedChange(selected.filter((x) => x !== m))
    } else {
      onSelectedChange([...selected, m])
    }
  }
  const selectAll = () => onSelectedChange([...pool])
  const invert = () => onSelectedChange(pool.filter((m) => !selectedSet.has(m)))
  const clear = () => onSelectedChange([])

  // 手动追加：支持逗号分隔一次多个；加入展示池并选中；回车触发。
  const manualAdd = () => {
    const names = input
      .split(',')
      .map((s) => s.trim())
      .filter(Boolean)
    if (names.length === 0) return
    onAvailableChange([...new Set([...available, ...names])])
    onSelectedChange([...new Set([...selected, ...names])])
    setInput('')
  }

  return (
    <div className='grid gap-2'>
      <div className='flex flex-wrap items-center justify-between gap-2'>
        <span className='text-sm font-medium'>{t('Supported models')}</span>
        <div className='flex items-center gap-1.5'>
          <Button
            type='button'
            variant='outline'
            size='sm'
            onClick={onFetch}
            disabled={fetching}
          >
            {fetching ? (
              <Loader2 className='animate-spin' aria-hidden='true' />
            ) : (
              <RefreshCw aria-hidden='true' />
            )}
            {t('Fetch models')}
          </Button>
          <Button
            type='button'
            variant='ghost'
            size='sm'
            onClick={selectAll}
            disabled={pool.length === 0}
          >
            {t('Select all')}
          </Button>
          <Button
            type='button'
            variant='ghost'
            size='sm'
            onClick={invert}
            disabled={pool.length === 0}
          >
            {t('Invert')}
          </Button>
          <Button
            type='button'
            variant='ghost'
            size='sm'
            onClick={clear}
            disabled={selected.length === 0}
          >
            {t('Clear')}
          </Button>
        </div>
      </div>

      <div className='border-input max-h-52 overflow-y-auto rounded-md border p-2'>
        {pool.length === 0 ? (
          <p className='text-muted-foreground px-1 py-6 text-center text-xs leading-relaxed'>
            {t(
              'No models yet — click "Fetch models" in the top right, or add them manually in the input below.'
            )}
          </p>
        ) : (
          <div className='flex flex-wrap gap-1.5'>
            {pool.map((m) => {
              const on = selectedSet.has(m)
              return (
                <button
                  key={m}
                  type='button'
                  aria-pressed={on}
                  onClick={() => toggle(m)}
                  className={cn(
                    'rounded-full border px-2.5 py-1 text-xs transition-colors',
                    on
                      ? 'border-primary bg-primary text-primary-foreground'
                      : 'border-input bg-background text-foreground hover:bg-muted'
                  )}
                >
                  {m}
                </button>
              )
            })}
          </div>
        )}
      </div>

      <Input
        value={input}
        onChange={(e) => setInput(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === 'Enter') {
            e.preventDefault()
            manualAdd()
          }
        }}
        placeholder={t('Add a model name manually, press Enter to append')}
      />

      <p className='text-muted-foreground text-xs leading-relaxed'>
        {t('Selected {{selected}} / available {{total}}.', {
          selected: selected.length,
          total: pool.length === 0 ? '—' : pool.length,
        })}{' '}
        {t(
          'Multi-select / select all / invert supported; press Enter to add a new model.'
        )}
      </p>
    </div>
  )
}
