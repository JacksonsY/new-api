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
import { createFileRoute, redirect } from '@tanstack/react-router'

import { SupplierApply } from '@/features/supplier'
import { SUPPLIER_STATUS } from '@/features/supplier/types'
import { useAuthStore } from '@/stores/auth-store'

export const Route = createFileRoute('/_authenticated/supplier/apply/')({
  beforeLoad: () => {
    // 已入驻供应商的家是「我的渠道」;入驻页只服务未入驻/审核中/被停用者。
    // 加渠道统一走「我的渠道」,不再从入驻页重复提交(消除死界面 + 重复流程)。
    const { auth } = useAuthStore.getState()
    if (auth.user?.supplier_status === SUPPLIER_STATUS.APPROVED) {
      throw redirect({ to: '/supplier/channels' })
    }
  },
  component: SupplierApply,
})
