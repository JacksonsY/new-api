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
import { Copy } from 'lucide-react'
import { memo, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/design-system/button'
import { StatusBadge } from '@/components/status-badge'
import { useCopyToClipboard } from '@/hooks/use-copy-to-clipboard'
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
import { formatPrice, formatRequestPrice } from '../lib/price'
import type { PricingModel, TokenUnit } from '../types'

export interface ModelCardProps {
  model: PricingModel
  onClick: () => void
  priceRate?: number
  usdExchangeRate?: number
  tokenUnit?: TokenUnit
  showRechargePrice?: boolean
  selectedGroup?: string
}

export const ModelCard = memo(function ModelCard(props: ModelCardProps) {
  const { t } = useTranslation()
  const { copyToClipboard } = useCopyToClipboard()
  const tokenUnit = props.tokenUnit ?? DEFAULT_TOKEN_UNIT
  const priceRate = props.priceRate ?? 1
  const usdExchangeRate = props.usdExchangeRate ?? 1
  const showRechargePrice = props.showRechargePrice ?? false
  const unitShort = tokenUnit === 'K' ? 'K' : 'M'
  const isTokenBased = isTokenBasedModel(props.model)
  const modelIconKey = props.model.icon || props.model.vendor_icon
  const modelIcon = modelIconKey ? getLobeIcon(modelIconKey, 24) : null
  const tags = parseTags(props.model.tags)
  const visibleTags = tags.slice(0, 2)
  const hiddenTagCount = Math.max(tags.length - 2, 0)
  const contextLabel = formatContextLength(props.model.context_length)
  const dynamicSummary = getDynamicPricingSummary(props.model, {
    tokenUnit,
    showRechargePrice,
    priceRate,
    usdExchangeRate,
    groupRatioMultiplier: getDynamicDisplayGroupRatio(
      props.model,
      props.selectedGroup
    ),
  })

  // 名称下方的紧凑价格行，与表格合并列同一口径：
  // 按量 = 「输入/M · 输出/M」，按次/按秒 = 单一价格 + 计价单位。
  let priceLine: ReactNode
  if (dynamicSummary?.isSpecialExpression) {
    priceLine = (
      <span className='text-warning'>{t('Special billing expression')}</span>
    )
  } else if (dynamicSummary) {
    const pick = (field: string) =>
      dynamicSummary.entries.find((item) => item.field === field)
    const input = pick('inputPrice')
    const output = pick('outputPrice')
    priceLine = (
      <>
        {input ? `${input.formatted}/${unitShort}` : '—'}
        <span className='mx-1'>·</span>
        {output ? `${output.formatted}/${unitShort}` : '—'}
      </>
    )
  } else if (isTokenBased) {
    const priceOf = (priceType: 'input' | 'output') =>
      formatPrice(
        props.model,
        priceType,
        tokenUnit,
        showRechargePrice,
        priceRate,
        usdExchangeRate,
        props.selectedGroup
      )
    priceLine = (
      <>
        {priceOf('input')}/{unitShort}
        <span className='mx-1'>·</span>
        {priceOf('output')}/{unitShort}
      </>
    )
  } else {
    priceLine = (
      <>
        {formatRequestPrice(
          props.model,
          showRechargePrice,
          priceRate,
          usdExchangeRate,
          props.selectedGroup
        )}
        /{isPerSecondVideoModel(props.model) ? t('second') : t('request')}
      </>
    )
  }

  return (
    <article
      role='button'
      tabIndex={0}
      onClick={props.onClick}
      onKeyDown={(event) => {
        if (event.key !== 'Enter' && event.key !== ' ') return
        event.preventDefault()
        props.onClick()
      }}
      aria-label={props.model.model_name}
      className='hover:bg-muted/20 focus-visible:ring-ring flex min-h-full cursor-pointer flex-col rounded-lg border p-4 transition-colors focus-visible:ring-2 focus-visible:outline-none'
    >
      <div className='flex items-start gap-3'>
        <div className='bg-muted flex size-9 shrink-0 items-center justify-center rounded-lg'>
          {modelIcon || (
            <span className='text-muted-foreground text-sm font-medium'>
              {props.model.model_name?.charAt(0).toUpperCase() || '?'}
            </span>
          )}
        </div>

        <div className='min-w-0 flex-1'>
          <div className='flex min-w-0 items-center gap-2'>
            <h2 className='truncate font-mono text-sm font-medium'>
              {props.model.model_name}
            </h2>
            <Button
              type='button'
              variant='ghost'
              size='icon-xs'
              onClick={(event) => {
                // 整卡可点开详情，复制不应触发跳转
                event.stopPropagation()
                copyToClipboard(props.model.model_name || '')
              }}
              aria-label={t('Copy model name')}
            >
              <Copy aria-hidden='true' className='size-3' />
            </Button>
          </div>
          <p className='text-muted-foreground mt-0.5 truncate text-xs tabular-nums'>
            {priceLine}
          </p>
        </div>
      </div>

      <p className='text-muted-foreground mt-3 line-clamp-2 min-h-10 flex-1 text-sm leading-relaxed'>
        {props.model.description || t('No description available.')}
      </p>

      <div className='mt-4 flex items-center justify-between gap-2'>
        <StatusBadge variant='neutral' size='md'>
          {getBillingTypeLabel(t, props.model)}
        </StatusBadge>

        <div className='flex min-w-0 flex-wrap items-center justify-end gap-1.5'>
          {visibleTags.map((tag) => (
            <StatusBadge key={tag} variant='neutral' size='md'>
              {tag}
            </StatusBadge>
          ))}
          {hiddenTagCount > 0 && (
            <span className='text-muted-foreground text-xs'>
              +{hiddenTagCount}
            </span>
          )}
          {contextLabel && (
            <StatusBadge variant='neutral' size='md'>
              {contextLabel}
            </StatusBadge>
          )}
        </div>
      </div>
    </article>
  )
})
