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
import { api } from '@/lib/api'

import type {
  AgentUser,
  AgentUsersResult,
  ApiEnvelope,
  CommissionsResult,
  CreateWithdrawalRequest,
  PagedResult,
  SetAgentRequest,
  Withdrawal,
  WithdrawalReviewAction,
} from './types'

// ---- 代理自助 ----

export async function agentListUsers(
  page = 1,
  pageSize = 20,
  keyword = '',
  status = ''
): Promise<ApiEnvelope<AgentUsersResult>> {
  const res = await api.get('/api/user/agent/users', {
    params: { p: page, page_size: pageSize, keyword, status },
  })
  return res.data
}

export async function agentListCommissions(
  page = 1,
  pageSize = 20
): Promise<ApiEnvelope<CommissionsResult>> {
  const res = await api.get('/api/user/agent/commissions', {
    params: { p: page, page_size: pageSize },
  })
  return res.data
}

// ---- 超管：代理管理 ----

export async function adminListAgents(
  page = 1,
  pageSize = 20,
  keyword = '',
  status = ''
): Promise<ApiEnvelope<PagedResult<AgentUser>>> {
  const res = await api.get('/api/user/agent/list', {
    params: { p: page, page_size: pageSize, keyword, status },
  })
  return res.data
}

export async function adminSetAgent(
  req: SetAgentRequest
): Promise<ApiEnvelope<null>> {
  const res = await api.post('/api/user/agent/create', req)
  return res.data
}

// 搜索用户(设代理用),复用平台现成的 /api/user/search。
export async function searchUsers(
  keyword: string
): Promise<ApiEnvelope<PagedResult<AgentUser>>> {
  const res = await api.get('/api/user/search', { params: { keyword } })
  return res.data
}

export async function adminRevokeAgent(
  userId: number
): Promise<ApiEnvelope<null>> {
  const res = await api.post('/api/user/agent/revoke', { user_id: userId })
  return res.data
}

// ---- 代理自助：我的用户操作 ----

// ---- 分润出口：转额度 / 提现 ----

export async function convertCommission(
  amount: number
): Promise<ApiEnvelope<null>> {
  const res = await api.post('/api/user/agent/commission/convert', { amount })
  return res.data
}

export async function createWithdrawal(
  req: CreateWithdrawalRequest
): Promise<ApiEnvelope<Withdrawal>> {
  const res = await api.post('/api/user/agent/withdraw', req)
  return res.data
}

export async function agentListWithdrawals(
  page = 1,
  pageSize = 20
): Promise<ApiEnvelope<PagedResult<Withdrawal>>> {
  const res = await api.get('/api/user/agent/withdraws', {
    params: { p: page, page_size: pageSize },
  })
  return res.data
}

export async function adminListWithdrawals(
  status = 0,
  page = 1,
  pageSize = 20,
  keyword = ''
): Promise<ApiEnvelope<PagedResult<Withdrawal>>> {
  const res = await api.get('/api/user/agent/withdraw/all', {
    params: { status, p: page, page_size: pageSize, keyword },
  })
  return res.data
}

export async function reviewWithdrawal(
  id: number,
  action: WithdrawalReviewAction,
  adminRemark = ''
): Promise<ApiEnvelope<null>> {
  const res = await api.post('/api/user/agent/withdraw/review', {
    id,
    action,
    admin_remark: adminRemark,
  })
  return res.data
}

export async function cancelWithdrawal(id: number): Promise<ApiEnvelope<null>> {
  const res = await api.post('/api/user/agent/withdraw/cancel', { id })
  return res.data
}

// ---- 超管：提现策略配置（复用通用 option 端点，RootAuth）----

export interface WithdrawSettings {
  minQuota: number
  feeRate: number
  maxPending: number
}

// 从通用 option 列表里挑出提现相关三项。
export async function getWithdrawSettings(): Promise<
  ApiEnvelope<WithdrawSettings>
> {
  const res = await api.get('/api/option/')
  if (!res.data?.success) {
    return { success: false, message: res.data?.message }
  }
  const map: Record<string, string> = {}
  for (const o of (res.data.data || []) as { key: string; value: string }[]) {
    map[o.key] = o.value
  }
  return {
    success: true,
    data: {
      minQuota: Number(map.AgentWithdrawMinQuota ?? 0),
      feeRate: Number(map.AgentWithdrawFeeRate ?? 0),
      maxPending: Number(map.AgentWithdrawMaxPending ?? 0),
    },
  }
}

// option 端点单次只改一个 key，逐项 PUT。
export async function updateWithdrawSettings(
  s: WithdrawSettings
): Promise<ApiEnvelope<null>> {
  const entries: [string, string][] = [
    ['AgentWithdrawMinQuota', String(Math.round(s.minQuota))],
    ['AgentWithdrawFeeRate', String(s.feeRate)],
    ['AgentWithdrawMaxPending', String(Math.round(s.maxPending))],
  ]
  for (const [key, value] of entries) {
    const res = await api.put('/api/option/', { key, value })
    if (!res.data?.success) {
      return { success: false, message: res.data?.message }
    }
  }
  return { success: true }
}
