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
import { Plus } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/design-system/button'
import { SectionPageLayout } from '@/components/layout'

import { ChannelFormDrawer } from './components/my-channels-form-drawer'
import {
  MyChannelsTable,
  SUPPLIER_CHANNELS_QUERY_KEY,
} from './components/my-channels-table'

// 「我的渠道」— 供应商受限渠道表单（不暴露 group/priority/weight）。
export function SupplierChannels() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [createOpen, setCreateOpen] = useState(false)

  return (
    <>
      <SectionPageLayout fixedContent>
        <SectionPageLayout.Title>{t('My Channels')}</SectionPageLayout.Title>
        <SectionPageLayout.Actions>
          <Button onClick={() => setCreateOpen(true)}>
            <Plus className='size-4' />
            {t('Add Channel')}
          </Button>
        </SectionPageLayout.Actions>
        <SectionPageLayout.Content>
          <MyChannelsTable />
        </SectionPageLayout.Content>
      </SectionPageLayout>

      <ChannelFormDrawer
        key='create'
        open={createOpen}
        target={null}
        onOpenChange={setCreateOpen}
        onDone={() => {
          setCreateOpen(false)
          queryClient.invalidateQueries({
            queryKey: [SUPPLIER_CHANNELS_QUERY_KEY],
          })
        }}
      />
    </>
  )
}
