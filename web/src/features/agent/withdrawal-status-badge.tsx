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
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'

import { WITHDRAWAL_STATUS } from './types'

export function WithdrawalStatusBadge({ status }: { status: number }) {
  const { t } = useTranslation()
  if (status === WITHDRAWAL_STATUS.APPROVED) {
    return <Badge>{t('Paid')}</Badge>
  }
  if (status === WITHDRAWAL_STATUS.REJECTED) {
    return <Badge variant='destructive'>{t('Rejected')}</Badge>
  }
  if (status === WITHDRAWAL_STATUS.PROCESSING) {
    return (
      <Badge className='bg-blue-100 text-blue-700 dark:bg-blue-500/20 dark:text-blue-300'>
        {t('Paying out')}
      </Badge>
    )
  }
  if (status === WITHDRAWAL_STATUS.CANCELLED) {
    return <Badge variant='outline'>{t('Cancelled')}</Badge>
  }
  return <Badge variant='secondary'>{t('Pending')}</Badge>
}
