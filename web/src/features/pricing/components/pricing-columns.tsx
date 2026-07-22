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
import type { ColumnDef } from '@tanstack/react-table'
import type { TFunction } from 'i18next'
import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { BadgeListCell } from '@/components/data-table'
import { Button } from '@/components/design-system/button'
import { StatusBadge } from '@/components/status-badge'
import { getLobeIcon } from '@/lib/lobe-icon'

import { DEFAULT_TOKEN_UNIT } from '../constants'
import {
  getDynamicDisplayGroupRatio,
  getDynamicPricingSummary,
} from '../lib/dynamic-price'
import { parseTags } from '../lib/filters'
import {
  formatContextLength,
  getBillingTypeLabel,
  isPerSecondVideoModel,
  isTokenBasedModel,
} from '../lib/model-helpers'
import {
  formatPrice,
  formatRequestPrice,
  stripTrailingZeros,
} from '../lib/price'
import type { PriceType, PricingModel, TokenUnit } from '../types'
import { AvailabilityBars, type ModelPerfBadgeData } from './model-perf-badge'

export interface PricingColumnsOptions {
  tokenUnit?: TokenUnit
  priceRate?: number
  usdExchangeRate?: number
  showRechargePrice?: boolean
  selectedGroup?: string
  perfMap?: Map<string, ModelPerfBadgeData>
  /** Opens the model detail panel. */
  onModelClick?: (modelName: string) => void
  /** Opens the playground with this model preselected. */
  onTryModel?: (modelName: string) => void
}

type PriceColumnType = Extract<PriceType, 'input' | 'cache' | 'output'>

const DYNAMIC_FIELD_BY_PRICE_TYPE: Record<PriceColumnType, string> = {
  input: 'inputPrice',
  cache: 'cacheReadPrice',
  output: 'outputPrice',
}

function renderEmptyCell(align: 'left' | 'right' = 'left'): ReactNode {
  const dash = (
    <span className='text-muted-foreground/50 text-sm tabular-nums'>—</span>
  )
  if (align === 'right') {
    return <div className='text-right'>{dash}</div>
  }
  return dash
}

function renderEmptyPrice(): ReactNode {
  return renderEmptyCell('right')
}

type PriceRenderOptions = Required<
  Omit<
    PricingColumnsOptions,
    'selectedGroup' | 'perfMap' | 'onModelClick' | 'onTryModel'
  >
> & {
  selectedGroup?: string
}

/**
 * 价格合并列：按量计费显示「输入 / 输出」，按次/按秒计费只有单一价格，
 * 显示价格与其计价单位。
 */
function renderPriceSummaryCell(
  props: { model: PricingModel; options: PriceRenderOptions },
  t: TFunction
): ReactNode {
  const { model, options } = props
  const dynamicSummary = getDynamicPricingSummary(model, {
    tokenUnit: options.tokenUnit,
    showRechargePrice: options.showRechargePrice,
    priceRate: options.priceRate,
    usdExchangeRate: options.usdExchangeRate,
    groupRatioMultiplier: getDynamicDisplayGroupRatio(model, options.selectedGroup),
  })

  if (dynamicSummary?.isSpecialExpression) {
    return (
      <div className='text-right'>
        <p className='text-warning text-xs font-medium'>
          {t('Special billing expression')}
        </p>
        <p className='text-muted-foreground mt-0.5 text-xs'>
          {t('View details')}
        </p>
      </div>
    )
  }

  if (dynamicSummary) {
    const pick = (field: string) =>
      dynamicSummary.entries.find((item) => item.field === field)
    const input = pick(DYNAMIC_FIELD_BY_PRICE_TYPE.input)
    const output = pick(DYNAMIC_FIELD_BY_PRICE_TYPE.output)
    if (!input && !output) return renderEmptyPrice()
    return (
      <div className='text-right'>
        <p className='text-sm font-medium tabular-nums'>
          {input ? stripTrailingZeros(input.formatted) : '—'}
          <span className='text-muted-foreground mx-1'>/</span>
          {output ? stripTrailingZeros(output.formatted) : '—'}
        </p>
        {dynamicSummary.tierCount > 1 && (
          <p className='text-muted-foreground mt-0.5 text-xs'>
            {t('{{count}} tiers', { count: dynamicSummary.tierCount })}
          </p>
        )}
      </div>
    )
  }

  if (!isTokenBasedModel(model)) {
    const unit = isPerSecondVideoModel(model) ? t('second') : t('request')
    return (
      <div className='text-right'>
        <p className='text-sm font-medium tabular-nums'>
          {stripTrailingZeros(
            formatRequestPrice(
              model,
              options.showRechargePrice,
              options.priceRate,
              options.usdExchangeRate,
              options.selectedGroup
            )
          )}
          <span className='text-muted-foreground'>/{unit}</span>
        </p>
      </div>
    )
  }

  const priceOf = (priceType: PriceColumnType) =>
    stripTrailingZeros(
      formatPrice(
        model,
        priceType,
        options.tokenUnit,
        options.showRechargePrice,
        options.priceRate,
        options.usdExchangeRate,
        options.selectedGroup
      )
    )

  return (
    <div className='text-right'>
      <p className='text-sm font-medium tabular-nums'>
        {priceOf('input')}
        <span className='text-muted-foreground mx-1'>/</span>
        {priceOf('output')}
      </p>
    </div>
  )
}


export function usePricingColumns(
  options: PricingColumnsOptions = {}
): ColumnDef<PricingModel>[] {
  const { t } = useTranslation()
  const priceOptions = {
    tokenUnit: options.tokenUnit ?? DEFAULT_TOKEN_UNIT,
    priceRate: options.priceRate ?? 1,
    usdExchangeRate: options.usdExchangeRate ?? 1,
    showRechargePrice: options.showRechargePrice ?? false,
    selectedGroup: options.selectedGroup,
  }

  return [
    {
      accessorKey: 'model_name',
      header: t('Model name'),
      cell: ({ row }) => {
        const model = row.original
        const modelIconKey = model.icon || model.vendor_icon
        const modelIcon = modelIconKey ? getLobeIcon(modelIconKey, 20) : null

        return (
          <div className='flex min-w-0 items-start gap-3 py-1'>
            <div className='bg-muted flex size-8 shrink-0 items-center justify-center rounded-md'>
              {modelIcon || (
                <span className='text-muted-foreground text-xs font-medium'>
                  {model.model_name?.charAt(0).toUpperCase() || '?'}
                </span>
              )}
            </div>
            <div className='min-w-0'>
              <p className='truncate font-mono text-sm font-medium'>
                {model.model_name}
              </p>
              <p className='text-muted-foreground mt-0.5 line-clamp-1 text-xs'>
                {model.vendor_name || model.description || ''}
              </p>
            </div>
          </div>
        )
      },
      minSize: 240,
      enableSorting: false,
    },
    {
      accessorKey: 'tags',
      header: t('Tags'),
      cell: ({ row }) => {
        const tags = parseTags(row.original.tags)
        if (tags.length === 0) {
          return renderEmptyCell()
        }
        return (
          <BadgeListCell
            items={tags.map((tag) => (
              <StatusBadge key={tag} variant='neutral' size='md'>
                {tag}
              </StatusBadge>
            ))}
          />
        )
      },
      size: 150,
      enableSorting: false,
    },
    {
      id: 'context',
      header: t('Context'),
      cell: ({ row }) => {
        const label = formatContextLength(row.original.context_length)
        if (!label) return renderEmptyCell()
        return <span className='text-sm tabular-nums'>{label}</span>
      },
      size: 90,
      enableSorting: false,
    },
    {
      id: 'billing',
      header: t('Billing'),
      cell: ({ row }) => (
        <StatusBadge variant='neutral' size='md'>
          {getBillingTypeLabel(t, row.original)}
        </StatusBadge>
      ),
      size: 110,
      enableSorting: false,
    },
    {
      id: 'price',
      header: () => (
        <div className='text-right'>
          {t('Input/Output')} $/{priceOptions.tokenUnit}
        </div>
      ),
      cell: ({ row }) =>
        renderPriceSummaryCell({ model: row.original, options: priceOptions }, t),
      size: 170,
      enableSorting: false,
    },
    {
      id: 'health',
      header: t('Availability'),
      cell: ({ row }) => {
        const perf = options.perfMap?.get(row.original.model_name || '')
        if (!perf) {
          return renderEmptyCell()
        }
        return <AvailabilityBars perf={perf} />
      },
      size: 190,
      enableSorting: false,
    },
    {
      id: 'actions',
      header: '',
      cell: ({ row }) => {
        const model = row.original
        const canTry = isTokenBasedModel(model) && Boolean(options.onTryModel)
        return (
          <div className='flex justify-end'>
            <Button
              variant='ghost'
              size='sm'
              onClick={(event) => {
                // 行本身也可点开详情，避免两个处理器同时触发
                event.stopPropagation()
                if (canTry) {
                  options.onTryModel?.(model.model_name)
                  return
                }
                options.onModelClick?.(model.model_name)
              }}
            >
              {canTry ? t('Try') : t('Details')}
            </Button>
          </div>
        )
      },
      size: 90,
      enableSorting: false,
    },
  ]
}