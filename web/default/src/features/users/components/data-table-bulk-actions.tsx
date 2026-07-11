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
import { useQueryClient } from '@tanstack/react-query'
import type { Table } from '@tanstack/react-table'
import { Coins, UsersRound } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { DataTableBulkActions as BulkActionsToolbar } from '@/components/data-table'
import { Button } from '@/components/design-system/button'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'

import type { User } from '../types'
import {
  UsersBatchGroupDialog,
  UsersBatchQuotaDialog,
} from './users-batch-dialogs'

interface DataTableBulkActionsProps {
  table: Table<User>
}

export function DataTableBulkActions({ table }: DataTableBulkActionsProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [showGroupDialog, setShowGroupDialog] = useState(false)
  const [showQuotaDialog, setShowQuotaDialog] = useState(false)

  const selectedRows = table.getFilteredSelectedRowModel().rows
  const selectedIds = selectedRows.reduce<number[]>((ids, row) => {
    const id = row.original.id
    if (typeof id === 'number') {
      ids.push(id)
    }
    return ids
  }, [])

  const handleSuccess = () => {
    table.resetRowSelection()
    queryClient.invalidateQueries({ queryKey: ['users'] })
  }

  return (
    <>
      <BulkActionsToolbar table={table} entityName='user'>
        <Tooltip>
          <TooltipTrigger
            render={
              <Button
                variant='outline'
                size='icon'
                onClick={() => setShowGroupDialog(true)}
                className='size-8'
                aria-label={t('Batch Change Group')}
              />
            }
          >
            <UsersRound className='h-4 w-4' />
          </TooltipTrigger>
          <TooltipContent>{t('Batch Change Group')}</TooltipContent>
        </Tooltip>
        <Tooltip>
          <TooltipTrigger
            render={
              <Button
                variant='outline'
                size='icon'
                onClick={() => setShowQuotaDialog(true)}
                className='size-8'
                aria-label={t('Batch Adjust Quota')}
              />
            }
          >
            <Coins className='h-4 w-4' />
          </TooltipTrigger>
          <TooltipContent>{t('Batch Adjust Quota')}</TooltipContent>
        </Tooltip>
      </BulkActionsToolbar>

      <UsersBatchGroupDialog
        open={showGroupDialog}
        onOpenChange={setShowGroupDialog}
        userIds={selectedIds}
        onSuccess={handleSuccess}
      />
      <UsersBatchQuotaDialog
        open={showQuotaDialog}
        onOpenChange={setShowQuotaDialog}
        userIds={selectedIds}
        onSuccess={handleSuccess}
      />
    </>
  )
}
