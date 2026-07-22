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

import { SubAccountPage } from '@/features/sub-account'
import { useAuthStore } from '@/stores/auth-store'

export const Route = createFileRoute('/_authenticated/sub-account/')({
  beforeLoad: () => {
    // jzlh-sub 子号(parent_id>0)不进管理台（MVP 只放主号 Owner；
    // 有 team_management 的管理员子号后续放开，后端已支持二次鉴权）。
    const { auth } = useAuthStore.getState()
    if (auth.user?.parent_id) {
      throw redirect({ to: '/403' })
    }
  },
  component: SubAccountPage,
})
