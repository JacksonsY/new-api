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

import { DetectorLeaderboard } from '@/features/detector'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

const leaderboardSearchSchema = z.object({
  page: z.number().optional().catch(1),
  pageSize: z.number().optional().catch(undefined),
})

export const Route = createFileRoute('/_authenticated/detection/leaderboard/')({
  beforeLoad: () => {
    // v2 P2:榜单对供应商开放——后端本就是公开的按域名聚合红黑榜,此前只有
    // 前端守卫把供应商挡在外面,看不到自己渠道的公开表现。
    const { auth } = useAuthStore.getState()
    const isAdmin = (auth.user?.role ?? 0) >= ROLE.ADMIN
    const isSupplier = auth.user?.supplier_status === 2
    if (!isAdmin && !isSupplier) {
      throw redirect({ to: '/403' })
    }
  },
  validateSearch: leaderboardSearchSchema,
  component: DetectorLeaderboard,
})
