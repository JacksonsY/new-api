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

import { MyUsers } from '@/features/agent'
import { useAuthStore } from '@/stores/auth-store'

// 分页/搜索状态同步到 URL(对齐 API 密钥页的模式)
const myUsersSearchSchema = z.object({
  page: z.number().optional().catch(1),
  pageSize: z.number().optional().catch(undefined),
  filter: z.string().optional().catch(''),
  status: z
    .array(z.enum(['1', '2']))
    .optional()
    .catch([]),
})

export const Route = createFileRoute('/_authenticated/agent/users/')({
  validateSearch: myUsersSearchSchema,
  beforeLoad: () => {
    const { auth } = useAuthStore.getState()
    // 代理身份是与 role 正交的独立维度：只有 agent_type 非空的用户可进。
    if (!auth.user || !auth.user.agent_type) {
      throw redirect({ to: '/403' })
    }
  },
  component: MyUsers,
})
