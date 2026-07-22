// jzlh-sub 子账号前端类型（对齐后端 /api/sub-account/* 契约）。

export interface ApiEnvelope<T> {
  success: boolean
  message?: string
  data?: T
}

// 后端 subAccountView：额度/消耗均为美元（-1=无限）。
export interface SubAccountView {
  id: number
  email: string
  username: string // display_name（对齐 302「用户名」）
  note: string
  role_preset: string // "user" | "admin"
  status: number // 1=启用 2=停用
  created_at: number
  initial_password?: string
  total_used_usd: number
  month_used_usd: number
  day_used_usd: number
  total_limit_usd: number // -1=无限
  month_limit_usd: number
  day_limit_usd: number
  permissions: Record<string, boolean>
}

export interface LimitInput {
  unlimited: boolean
  value: number // 美元
}

export interface CreateSubAccountsRequest {
  prefix: string
  count: number
  role_preset: string
  permissions: Record<string, boolean>
  note: string
  total_limit: LimitInput
  month_limit: LimitInput
  day_limit: LimitInput
}

export interface UpdateSubAccountRequest {
  display_name?: string
  note?: string
  role_preset?: string
  permissions?: Record<string, boolean>
  new_password?: string
  total_limit?: LimitInput
  month_limit?: LimitInput
  day_limit?: LimitInput
}

export interface SubAccountSummary {
  sub_used_usd: number
  balance_usd: number
  main_email: string
}

export interface ListResult {
  items: SubAccountView[]
  total: number
  page: number
  page_size: number
  summary?: SubAccountSummary
}

export interface CreateResult {
  items: SubAccountView[]
}

// 功能权限开关（对齐 fork 侧栏，非照搬 302）。
export const SUB_CORE_PERMISSIONS = [
  'playground',
  'api_keys',
  'usage_logs',
] as const

export const SUB_ADMIN_PERMISSIONS = ['wallet', 'team_management'] as const

export const ROLE_PRESET_USER = 'user'
export const ROLE_PRESET_ADMIN = 'admin'
