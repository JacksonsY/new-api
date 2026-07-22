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
import type { Row } from '@tanstack/react-table'
import { useTranslation } from 'react-i18next'

import { StatusBadge } from '@/components/status-badge'
import { formatQuota } from '@/lib/format'

import { API_KEY_STATUSES } from '../constants'
import type { ApiKey } from '../types'
import { ApiKeyCell, UnlimitedQuotaBadge } from './api-keys-cells'
import { DataTableRowActions } from './data-table-row-actions'

// 移动端密钥行(上游 #6329 版式):名称+状态 / 密钥+行操作 / 额度。
// 外层容器(divide 列表、内边距、禁用置灰)由 DataTable 的 mobile-card-list 提供。
export function ApiKeyCard(props: { row: Row<ApiKey> }) {
  const { t } = useTranslation()
  const apiKey = props.row.original
  const statusConfig = API_KEY_STATUSES[apiKey.status]
  const total = apiKey.used_quota + apiKey.remain_quota

  return (
    <div className='space-y-2.5'>
      <div className='flex items-start justify-between gap-3'>
        <div className='min-w-0'>
          <div className='truncate text-sm font-semibold'>{apiKey.name}</div>
          <div className='text-muted-foreground text-[11px]'>
            {t('API Key')}
          </div>
        </div>
        {statusConfig && (
          <StatusBadge variant={statusConfig.variant}>
            {t(statusConfig.label)}
          </StatusBadge>
        )}
      </div>

      <div className='flex min-w-0 items-center justify-between gap-2'>
        <div className='min-w-0 flex-1 [&_button:first-child]:max-w-full [&_button:first-child]:truncate [&_button:first-child]:px-0'>
          <ApiKeyCell apiKey={apiKey} />
        </div>
        <DataTableRowActions row={props.row} />
      </div>

      <div className='flex items-center justify-between gap-2 text-xs'>
        <span className='text-muted-foreground'>{t('Quota')}</span>
        {apiKey.unlimited_quota ? (
          <UnlimitedQuotaBadge used={apiKey.used_quota} />
        ) : (
          <span className='font-medium tabular-nums'>
            {formatQuota(apiKey.remain_quota)}
            <span className='text-muted-foreground font-normal'>
              {' / '}
              {formatQuota(total)}
            </span>
          </span>
        )}
      </div>
    </div>
  )
}
