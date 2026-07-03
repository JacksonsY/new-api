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

// jzlh-agent 代理分销前端类型（对齐后端 /api/user/agent/* 契约）。

export type AgentType = 'normal' | 'oem' | 'api'

export interface AgentUser {
  id: number
  username: string
  display_name?: string
  email?: string
  group?: string
  quota?: number
  used_quota?: number
  request_count?: number
  status?: number
  last_login_at?: number
  agent_type?: string
  usage_profit_rate?: number
  commission_quota?: number
  commission_history_quota?: number
  downstream_count?: number
  created_at?: number
}

export interface CommissionRecord {
  id: number
  agent_id: number
  from_user_id: number
  from_username?: string
  log_id: number
  quota: number
  status?: number // 1=成熟期内待结转 2=已成熟(见 model/commission.go)
  created_at: number
}

export const COMMISSION_STATUS = {
  PENDING: 1,
  MATURED: 2,
} as const

export interface ApiEnvelope<T> {
  success: boolean
  message?: string
  data?: T
}

export interface PagedResult<T> {
  items: T[]
  total: number
  page: number
  page_size: number
}

export interface AgentUsersResult extends PagedResult<AgentUser> {
  total_quota: number
  total_used_quota: number
}

export interface CommissionsResult extends PagedResult<CommissionRecord> {
  commission_quota: number
  commission_history_quota: number
  commission_pending_quota: number
  agent_type: string
  usage_profit_rate: number
  // 提现策略(随汇总下发)：最低提现额(quota)与手续费比例(0-1)，用于展示与前置校验。
  withdraw_min_quota?: number
  withdraw_fee_rate?: number
}

export interface SetAgentRequest {
  user_id: number
  agent_type: AgentType
  usage_profit_rate: number
}

export type WithdrawalMethod = 'alipay' | 'wxpay' | 'bank'

// 提现方式 → i18n 文案 key（展示时经 t() 翻译；未知值原样显示）。
export const WITHDRAWAL_METHOD_LABELS: Record<string, string> = {
  alipay: 'Alipay',
  wxpay: 'WeChat Pay',
  bank: 'Bank Card',
}

export const WITHDRAWAL_STATUS = {
  PENDING: 1,
  APPROVED: 2,
  REJECTED: 3,
  PROCESSING: 4, // 已被管理员认领，线下打款进行中
  CANCELLED: 5, // 代理自行撤销
} as const

export interface Withdrawal {
  id: number
  user_id: number
  username?: string
  amount: number
  fee: number
  method: string
  payee_name: string
  payee_account: string
  remark: string
  status: number
  admin_remark: string
  reviewer_id?: number
  reviewer_name?: string
  exchange_rate?: number // 申请时的「本地货币/美元」价格快照，取支付共享配置 Price(0=未配置)
  created_at: number
}

export type WithdrawalReviewAction = 'claim' | 'release' | 'approve' | 'reject'

export interface CreateWithdrawalRequest {
  amount: number
  method: WithdrawalMethod
  payee_name: string
  payee_account: string
  remark: string
}
