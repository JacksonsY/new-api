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

// 后端 AgentTypeValid 目前只接受 'normal'(oem/api 未实现,曾是占位)。
// 收窄到实际有效值,避免类型谎报可选项。
export type AgentType = 'normal'

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
  // 成熟窗口(分钟);为 0 时分润即时可提,前端据此隐藏"成熟中"相关 UI。
  commission_mature_minutes?: number
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

// ---- 反欺诈（jzlh-agent 蓝图F，对齐后端 /api/user/agent/fraud|risk/* 契约） ----

export const FRAUD_ALERT_STATUS = {
  DETECTED: 'detected',
  RESOLVED: 'resolved',
  DISMISSED: 'dismissed',
} as const

export type FraudReviewAction = 'unbind' | 'clawback' | 'dismiss' | 'delete'

export interface FraudAlert {
  id: number
  agent_id: number
  invitee_id: number
  shared_ips: string // JSON 数组字符串
  shared_ip_count: number
  status: string
  resolved_action?: string
  clawback_quota?: number
  admin_id?: number
  admin_remark?: string
  detected_at: number
  resolved_at?: number
  agent_username?: string
  invitee_username?: string
}

export interface RiskUser {
  id: number
  user_id: number
  status: string // active / removed
  freeze_assets: boolean
  block_invite_code: boolean
  reason?: string
  admin_id?: number
  removed_by?: number
  remove_remark?: string
  removed_at?: number
  created_at: number
  username?: string
}

export interface ApplyRiskControlsRequest {
  user_id: number
  freeze_assets: boolean
  block_invite_code: boolean
  reason: string
}

// ---- jzlh-agent 代理自助入驻申请 ----

export const AGENT_APPLICATION_STATUS = {
  PENDING: 1,
  APPROVED: 2,
  REJECTED: 3,
} as const

export interface AgentApplication {
  id: number
  user_id: number
  contact: string
  note: string
  status: number
  reason: string
  created_time: number
  updated_time: number
  reviewed_time: number
}

export interface AgentApplicationRow {
  application: AgentApplication
  username: string
}

export type AgentApplicationsResult = PagedResult<AgentApplicationRow>
