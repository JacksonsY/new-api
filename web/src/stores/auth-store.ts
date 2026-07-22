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
import { create } from 'zustand'

import type { AdminCapabilities } from '@/lib/admin-permissions'

export type UserPermissions = {
  sidebar_settings?: boolean
  sidebar_modules?: Record<string, unknown>
  admin_permissions?: AdminCapabilities
}

export interface AuthUser {
  id: number
  username: string
  display_name?: string
  email?: string
  role: number
  status?: number
  group?: string
  quota?: number
  used_quota?: number
  request_count?: number
  aff_code?: string
  aff_count?: number
  aff_quota?: number
  aff_history_quota?: number
  inviter_id?: number
  // jzlh-agent 代理分销
  agent_type?: string
  usage_profit_rate?: number
  commission_quota?: number
  commission_history_quota?: number
  agent_grace_access?: boolean
  // 供应商中心：0=未申请 1=待审核 2=已通过 3=已暂停
  supplier_status?: number
  supplier_payable_quota?: number
  // jzlh-sub 子账号：>0=子号(隶属主号),0/undefined=主号/普通用户
  parent_id?: number
  // 子号功能权限白名单(nil=非子号)：前端据此门控菜单(钱包/Playground/API Keys 等)
  sub_account?: {
    permissions?: Record<string, boolean>
    role_preset?: string
  }
  github_id?: string
  discord_id?: string
  oidc_id?: string
  wechat_id?: string
  telegram_id?: string
  linux_do_id?: string
  language?: string
  setting?: Record<string, unknown> | string
  stripe_customer?: string
  sidebar_modules?: string
  permissions?: UserPermissions
}

export interface LoginSession {
  sid: string
  current: boolean
  login_method: string
  ip: string
  user_agent: string
  created_at: number
  last_active_at: number
  expires_at: number
}

export interface AuthBundle {
  access_token: string
  token_type: 'Bearer' | string
  access_expires_at: number
  user: AuthUser
  session: LoginSession
}

export type AuthBootstrapState = 'idle' | 'checking' | 'complete'

interface AuthState {
  auth: {
    user: AuthUser | null
    accessToken: string | null
    accessExpiresAt: number | null
    session: LoginSession | null
    pending2FAFlowToken: string | null
    bootstrapState: AuthBootstrapState
    setBundle: (bundle: AuthBundle) => void
    setUser: (user: AuthUser | null) => void
    setPending2FAFlowToken: (flowToken: string | null) => void
    setBootstrapState: (bootstrapState: AuthBootstrapState) => void
    reset: (bootstrapState?: AuthBootstrapState) => void
  }
}

export const useAuthStore = create<AuthState>()((set) => ({
  auth: {
    user: null,
    accessToken: null,
    accessExpiresAt: null,
    session: null,
    pending2FAFlowToken: null,
    bootstrapState: 'idle',
    setBundle: (bundle) =>
      set((state) => ({
        ...state,
        auth: {
          ...state.auth,
          user: bundle.user,
          accessToken: bundle.access_token,
          accessExpiresAt: bundle.access_expires_at,
          session: bundle.session,
          pending2FAFlowToken: null,
          bootstrapState: 'complete',
        },
      })),
    setUser: (user) =>
      set((state) => ({
        ...state,
        auth: { ...state.auth, user },
      })),
    setPending2FAFlowToken: (pending2FAFlowToken) =>
      set((state) => ({
        ...state,
        auth: { ...state.auth, pending2FAFlowToken },
      })),
    setBootstrapState: (bootstrapState) =>
      set((state) => ({
        ...state,
        auth: { ...state.auth, bootstrapState },
      })),
    reset: (bootstrapState = 'complete') =>
      set((state) => ({
        ...state,
        auth: {
          ...state.auth,
          user: null,
          accessToken: null,
          accessExpiresAt: null,
          session: null,
          pending2FAFlowToken: null,
          bootstrapState,
        },
      })),
  },
}))
