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
import z from 'zod'

import { AdminSuppliers } from '@/features/supplier'
import { hasRootAccess } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

const suppliersSearchSchema = z.object({
  page: z.number().optional().catch(1),
  pageSize: z.number().optional().catch(undefined),
  filter: z.string().optional().catch(''),
  status: z
    .array(z.enum(['1', '2', '3']))
    .optional()
    .catch([]),
})

export const Route = createFileRoute('/_authenticated/suppliers/')({
  beforeLoad: () => {
    // 后端供应商管理端点均为 RootAuth,前端须对齐(否则 role-10 管理员看得到菜单
    // 却每个接口都 403)。与代理管理端一致收紧到超管。
    const { auth } = useAuthStore.getState()
    if (!hasRootAccess(auth.user?.role)) {
      throw redirect({ to: '/403' })
    }
  },
  validateSearch: suppliersSearchSchema,
  component: AdminSuppliers,
})
