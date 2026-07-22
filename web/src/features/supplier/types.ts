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

// 供应商中心前端类型（对齐后端 /api/user/supplier/* 契约）。

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

// 供应商入驻状态：0=未申请 1=待审核 2=已通过 3=已暂停。
export const SUPPLIER_STATUS = {
  NONE: 0,
  PENDING: 1,
  APPROVED: 2,
  SUSPENDED: 3,
} as const

export type SupplierStatus =
  (typeof SUPPLIER_STATUS)[keyof typeof SUPPLIER_STATUS]

// 收款方式（与后端 model.SupplierPayoutMethods 同步；非法值后端归一为 other）。
export const SUPPLIER_PAYOUT_METHODS = [
  'alipay',
  'wechat',
  'bank',
  'usdt',
  'other',
] as const

export type SupplierPayoutMethod = (typeof SUPPLIER_PAYOUT_METHODS)[number]

const PAYOUT_METHOD_LABEL_KEYS: Record<string, string> = {
  alipay: 'Alipay',
  wechat: 'WeChat Pay',
  bank: 'Bank Card',
  usdt: 'USDT',
  other: 'Other',
}

// 收款方式 → i18n 文案 key（展示时经 t() 翻译）。
export function payoutMethodLabelKey(method?: string): string {
  return PAYOUT_METHOD_LABEL_KEYS[method ?? ''] ?? 'Other'
}

// 供应商收款与联系方式（入驻/编辑表单契约）。
export interface SupplierPayoutInfo {
  method: string
  account: string
  name: string
  contact: string
}

// 收款/联系方式初始值（默认支付宝）。
export const EMPTY_PAYOUT_INFO: SupplierPayoutInfo = {
  method: 'alipay',
  account: '',
  name: '',
  contact: '',
}

// 校验收款/联系方式，返回可 t() 的错误文案 key，无误返回 null。
// 长度上限与后端列宽一致（account/contact 128、name 64）。
export function validatePayoutInfo(v: SupplierPayoutInfo): string | null {
  if (!v.account.trim() || !v.name.trim() || !v.contact.trim()) {
    return 'Please fill in payout account, name and contact'
  }
  if (
    v.account.trim().length > 128 ||
    v.name.trim().length > 64 ||
    v.contact.trim().length > 128
  ) {
    return 'Payout information is too long'
  }
  return null
}

// 结算流水类型：1=打款 2=罚没 3=调整。
export const SUPPLIER_LEDGER_TYPE = {
  PAYOUT: 1,
  CONFISCATION: 2,
  ADJUSTMENT: 3,
} as const

export type SupplierLedgerType =
  (typeof SUPPLIER_LEDGER_TYPE)[keyof typeof SUPPLIER_LEDGER_TYPE]

const LEDGER_LABEL_KEYS: Record<number, string> = {
  [SUPPLIER_LEDGER_TYPE.PAYOUT]: 'Payout',
  [SUPPLIER_LEDGER_TYPE.CONFISCATION]: 'Confiscation',
  [SUPPLIER_LEDGER_TYPE.ADJUSTMENT]: 'Adjustment',
}

// 结算流水类型 → i18n 文案 key（展示时经 t() 翻译）。
export function ledgerTypeLabelKey(type: number): string {
  return LEDGER_LABEL_KEYS[type] ?? 'Adjustment'
}

// 渠道审核状态：0=已通过 1=待审核 2=已驳回。
export const CHANNEL_AUDIT_STATUS = {
  APPROVED: 0,
  PENDING: 1,
  REJECTED: 2,
} as const

export type ChannelAuditStatus =
  (typeof CHANNEL_AUDIT_STATUS)[keyof typeof CHANNEL_AUDIT_STATUS]

// 供应商结算汇总（全部为整数 quota）。
export interface SupplierSettlement {
  gross_quota: number
  matured_quota: number
  paid_quota: number
  confiscated_quota: number
  payable_quota: number
}

export interface SupplierProfile {
  supplier_status: number
  settlement?: SupplierSettlement
  // 商户资料（入驻申请填写）。
  supplier_name?: string
  supplier_intro?: string
  supplier_contact?: string
  // 收款账户（审核通过后的收款设置页填写）。
  supplier_payout_method?: string
  supplier_payout_account?: string
  supplier_payout_name?: string
}

// 商户资料表单（入驻申请：名称/联系方式/简介，与收款账户解耦）。
export interface SupplierProfileForm {
  name: string
  contact: string
  intro: string
}

export const EMPTY_SUPPLIER_PROFILE: SupplierProfileForm = {
  name: '',
  contact: '',
  intro: '',
}

// 校验商户资料，返回可 t() 的错误文案 key，无误返回 null（长度与后端列宽一致）。
export function validateSupplierProfileForm(
  v: SupplierProfileForm
): string | null {
  if (!v.name.trim() || !v.contact.trim()) {
    return 'Please enter merchant name and contact'
  }
  if (
    v.name.trim().length > 64 ||
    v.contact.trim().length > 128 ||
    v.intro.length > 255
  ) {
    return 'Merchant profile is too long'
  }
  return null
}

// 供应商渠道（平台 Channel 的受限视图 + 审核字段）。
export interface SupplierChannel {
  id: number
  name: string
  type: number
  key?: string
  base_url?: string
  models: string
  channel_ratio?: number
  test_model?: string
  audit_status: number
  status?: number
  group?: string
  priority?: number
  weight?: number
  remark?: string
  created_time?: number
}

// 结算流水一条记录。
export interface SupplierLedger {
  id: number
  user_id: number
  type: number
  quota: number
  voucher?: string
  remark?: string
  admin_id?: number
  admin_name?: string
  created_at: number
}

// 管理端供应商列表条目（复用平台 User，含供应商相关字段）。
export interface SupplierUser {
  id: number
  username: string
  display_name?: string
  email?: string
  status?: number
  supplier_status: number
  supplier_payable_quota?: number
  created_at?: number
  // 收款/联系方式：管理端审核与结算打款时展示（RootAuth 列表返回，omitempty）。
  supplier_payout_method?: string
  supplier_payout_account?: string
  supplier_payout_name?: string
  supplier_contact?: string
}

export interface SupplierEarningsResult {
  settlement: SupplierSettlement
  ledger: SupplierLedger[]
  total: number
  page: number
  page_size: number
}

// v2 §4.3:按渠道×按天毛收入明细行(gross = Σ min(channel_quota, quota),
// 口径与结算一致)。day 为天桶起点 unix 秒。
export interface SupplierDailyEarning {
  channel_id: number
  channel_name: string
  day: number
  gross: number
  count: number
}

export interface SupplierSettlementResult {
  settlement: SupplierSettlement
  ledger: SupplierLedger[]
  total: number
}

// 提交给后端的受限渠道表单（供应商不可设置 group/priority/weight）。
// note = 渠道说明（可选，存后端 Remark）；channel_ratio/test_model 申请页不收，管理员审批时定。
export interface SupplierChannelForm {
  name: string
  type: number
  key: string
  base_url: string
  models: string
  note?: string
  channel_ratio: number
  test_model: string
}

// 渠道要约初值（默认类型 1 = OpenAI，报价率 1.0）。
export const EMPTY_CHANNEL_FORM: SupplierChannelForm = {
  name: '',
  type: 1,
  key: '',
  base_url: '',
  models: '',
  note: '',
  channel_ratio: 1,
  test_model: '',
}

// 校验渠道要约，返回可 t() 的错误文案 key，无误返回 null。名称规则与后端一致：
// 1-10 字、无首尾空格、不含逗号/引号（逗号会破坏 models 的逗号分隔）。
export function validateChannelForm(v: SupplierChannelForm): string | null {
  const name = v.name
  if (name !== name.trim()) {
    return 'Channel name cannot have leading or trailing spaces'
  }
  const len = [...name].length
  if (len < 1 || len > 10) return 'Channel name must be 1-10 characters'
  if (/[,"'，“”‘’]/.test(name)) {
    return 'Channel name cannot contain commas or quotes'
  }
  if (!v.models.trim()) return 'Please enter at least one model'
  if (!v.key.trim()) return 'Please enter an API key'
  return null
}

// 入驻申请载荷：商户资料 + 一条渠道要约。
export interface SupplierApplyPayload {
  profile: SupplierProfileForm
  channel: SupplierChannelForm
}

export interface SupplierChannelReviewParams {
  channel_id: number
  group: string
  priority: number
  weight: number
  channel_ratio: number
}

export interface SupplierPayParams {
  user_id: number
  amount: number
  voucher: string
  remark: string
}
