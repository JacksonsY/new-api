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
import { z } from 'zod'

import { SignUp } from '@/features/auth/sign-up'
import { useAuthStore } from '@/stores/auth-store'

// redirect 一路透传:未登录访问受保护页 → 登录页(带 redirect)→ 去注册,
// 注册成功后回到原目的地,否则链路在注册处断裂。
const signUpSearchSchema = z.object({
  redirect: z.string().optional(),
})

export const Route = createFileRoute('/(auth)/sign-up')({
  component: SignUp,
  validateSearch: signUpSearchSchema,
  beforeLoad: async ({ search }) => {
    const { auth } = useAuthStore.getState()

    // 已登录时注册页无意义:优先回用户原本想去的地方
    if (auth.user) {
      throw redirect({ to: search?.redirect || '/dashboard' })
    }
  },
})
