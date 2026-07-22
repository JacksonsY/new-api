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
import type { TFunction } from 'i18next'

import type { PricingModel, TokenUnit } from '../types'
import {
  getDynamicDisplayGroupRatio,
  getDynamicPricingSummary,
} from './dynamic-price'
import {
  getBillingTypeLabel,
  isPerSecondVideoModel,
  isTokenBasedModel,
} from './model-helpers'
import { formatPrice, formatRequestPrice, stripTrailingZeros } from './price'

export interface PricingCsvOptions {
  t: TFunction
  tokenUnit: TokenUnit
  showRechargePrice: boolean
  priceRate: number
  usdExchangeRate: number
  selectedGroup?: string
}

/**
 * 单元格转义:含分隔符/引号/换行时加引号包裹;以公式字符开头时
 * 前置单引号,防止在 Excel/Sheets 中被当作公式执行(CSV 注入)。
 */
function csvCell(value: string | number | undefined | null): string {
  let s = value == null ? '' : String(value)
  if (/^[=+@\t\r]/.test(s)) {
    s = `'${s}`
  }
  if (/[",\n\r]/.test(s)) {
    s = `"${s.replaceAll('"', '""')}"`
  }
  return s
}

/**
 * 价格三列(输入/输出/固定价)+ 备注,与表格价格列同一套计算:
 * 动态计价取首档并标注档数;特殊表达式仅标注;按次/按秒模型只有固定价。
 */
function modelPriceCells(model: PricingModel, options: PricingCsvOptions) {
  const { t } = options
  const dynamicSummary = getDynamicPricingSummary(model, {
    tokenUnit: options.tokenUnit,
    showRechargePrice: options.showRechargePrice,
    priceRate: options.priceRate,
    usdExchangeRate: options.usdExchangeRate,
    groupRatioMultiplier: getDynamicDisplayGroupRatio(
      model,
      options.selectedGroup
    ),
  })

  if (dynamicSummary?.isSpecialExpression) {
    return {
      input: '-',
      output: '-',
      fixed: '-',
      note: t('Special billing expression'),
    }
  }

  if (dynamicSummary) {
    const pick = (field: string) =>
      dynamicSummary.entries.find((entry) => entry.field === field)
    const input = pick('inputPrice')
    const output = pick('outputPrice')
    return {
      input: input ? stripTrailingZeros(input.formatted) : '-',
      output: output ? stripTrailingZeros(output.formatted) : '-',
      fixed: '-',
      note:
        dynamicSummary.tierCount > 1
          ? t('{{count}} tiers', { count: dynamicSummary.tierCount })
          : '',
    }
  }

  if (!isTokenBasedModel(model)) {
    const unit = isPerSecondVideoModel(model) ? t('second') : t('request')
    const fixed = stripTrailingZeros(
      formatRequestPrice(
        model,
        options.showRechargePrice,
        options.priceRate,
        options.usdExchangeRate,
        options.selectedGroup
      )
    )
    return { input: '-', output: '-', fixed: `${fixed}/${unit}`, note: '' }
  }

  const priceOf = (type: 'input' | 'output') =>
    stripTrailingZeros(
      formatPrice(
        model,
        type,
        options.tokenUnit,
        options.showRechargePrice,
        options.priceRate,
        options.usdExchangeRate,
        options.selectedGroup
      )
    )
  return {
    input: priceOf('input'),
    output: priceOf('output'),
    fixed: '-',
    note: '',
  }
}

/**
 * 按当前展示状态(筛选结果、单位、充值价、分组)生成模型广场 CSV。
 * 带 UTF-8 BOM,Excel 直接打开不乱码。
 */
export function buildPricingCsv(
  models: PricingModel[],
  options: PricingCsvOptions
): string {
  const { t } = options
  const headers = [
    t('Model name'),
    t('Vendor'),
    t('Billing'),
    `${t('Input price')}/1${options.tokenUnit}`,
    `${t('Output price')}/1${options.tokenUnit}`,
    t('Per Request'),
    t('Context'),
    t('Max output'),
    t('Groups'),
    t('Tags'),
    t('Endpoints'),
    t('Remark'),
  ]

  const rows = models.map((model) => {
    const price = modelPriceCells(model, options)
    return [
      model.model_name,
      model.vendor_name || '',
      getBillingTypeLabel(t, model),
      price.input,
      price.output,
      price.fixed,
      model.context_length || '',
      model.max_output_tokens || '',
      (model.enable_groups || []).join(' | '),
      model.tags || '',
      (model.supported_endpoint_types || []).join(' | '),
      price.note,
    ]
  })

  const body = [headers, ...rows]
    .map((row) => row.map(csvCell).join(','))
    .join('\r\n')
  return `\ufeff${body}`
}

/** 生成 CSV 并触发浏览器下载。 */
export function downloadPricingCsv(
  models: PricingModel[],
  options: PricingCsvOptions
): void {
  const csv = buildPricingCsv(models, options)
  const stamp = new Date()
    .toISOString()
    .slice(0, 19)
    .replaceAll(/[-:]/g, '')
    .replace('T', '-')
  const blob = new Blob([csv], { type: 'text/csv;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = `models-pricing-${stamp}.csv`
  document.body.append(link)
  link.click()
  link.remove()
  URL.revokeObjectURL(url)
}
