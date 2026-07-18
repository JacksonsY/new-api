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

import { AgentApply } from '@/features/agent'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

export const Route = createFileRoute('/_authenticated/agent/apply/')({
  beforeLoad: () => {
    const { auth } = useAuthStore.getState()
    // 已是代理:家在代理钱包;管理员:裁判/运动员隔离,不可申请(后端同规)
    if (auth.user?.agent_type) {
      throw redirect({ to: '/agent/wallet' })
    }
    if ((auth.user?.role ?? 0) >= ROLE.ADMIN) {
      throw redirect({ to: '/403' })
    }
  },
  component: AgentApply,
})
