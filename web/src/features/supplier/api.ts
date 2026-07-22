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
  ApiEnvelope,
  PagedResult,
  SupplierApplyPayload,
  SupplierChannel,
  SupplierChannelForm,
  SupplierChannelReviewParams,
  SupplierDailyEarning,
  SupplierEarningsResult,
  SupplierPayoutInfo,
  SupplierPayParams,
  SupplierProfile,
  SupplierSettlementResult,
  SupplierUser,
} from './types'

// ---- 供应商自助 ----

// 入驻申请：随申请提交商户资料（名称/联系方式/简介）+ 一条渠道要约。
export async function supplierApply(
  payload: SupplierApplyPayload
): Promise<ApiEnvelope<{ channel_id: number }>> {
  const res = await api.post('/api/user/supplier/apply', payload)
  return res.data
}

// 一键获取上游模型（申请页测活）：给 base_url/type/key，返回可用模型名列表。
export async function supplierFetchModels(body: {
  base_url: string
  type: number
  key: string
}): Promise<ApiEnvelope<string[]>> {
  const res = await api.post('/api/user/supplier/fetch-models', body)
  return res.data
}

// 更新收款/联系方式（审核通过后换卡等，不改审核状态）。
export async function updateSupplierPayoutInfo(
  payload: SupplierPayoutInfo
): Promise<ApiEnvelope<Record<string, never>>> {
  const res = await api.put('/api/user/supplier/payout-info', payload)
  return res.data
}

export async function getSupplierProfile(): Promise<
  ApiEnvelope<SupplierProfile>
> {
  const res = await api.get('/api/user/supplier/profile')
  return res.data
}

export async function listSupplierChannels(
  page = 1,
  pageSize = 20
): Promise<ApiEnvelope<PagedResult<SupplierChannel>>> {
  const res = await api.get('/api/user/supplier/channels', {
    params: { p: page, page_size: pageSize },
  })
  return res.data
}

// 申请记录（owner scope、任意审核态）：apply 页展示已提交的渠道入驻请求（pending 也可见）。
export async function listSupplierApplications(
  page = 1,
  pageSize = 20
): Promise<ApiEnvelope<PagedResult<SupplierChannel>>> {
  const res = await api.get('/api/user/supplier/applications', {
    params: { p: page, page_size: pageSize },
  })
  return res.data
}

export async function createSupplierChannel(
  form: SupplierChannelForm
): Promise<ApiEnvelope<{ id: number }>> {
  const res = await api.post('/api/user/supplier/channel', form)
  return res.data
}

export async function updateSupplierChannel(
  id: number,
  form: SupplierChannelForm
): Promise<ApiEnvelope<{ id: number; audit_status: number }>> {
  const res = await api.put(`/api/user/supplier/channel/${id}`, form)
  return res.data
}

export async function getSupplierEarnings(
  page = 1
): Promise<ApiEnvelope<SupplierEarningsResult>> {
  const res = await api.get('/api/user/supplier/earnings', {
    params: { p: page },
  })
  return res.data
}

// v2 §4.3 经营透明:按渠道×按天毛收入明细(口径与结算一致)。
export async function getSupplierDailyEarnings(
  days = 30
): Promise<ApiEnvelope<{ items: SupplierDailyEarning[] }>> {
  const res = await api.get('/api/user/supplier/earnings/daily', {
    params: { days },
  })
  return res.data
}

// ---- 管理端：供应商管理 ----

export async function adminListSuppliers(
  status = '',
  keyword = '',
  page = 1,
  pageSize = 20
): Promise<ApiEnvelope<PagedResult<SupplierUser>>> {
  const res = await api.get('/api/user/supplier/admin/list', {
    params: { status, keyword, p: page, page_size: pageSize },
  })
  return res.data
}

export async function adminReviewSupplier(
  userId: number,
  status: number
): Promise<ApiEnvelope<Record<string, never>>> {
  const res = await api.post('/api/user/supplier/admin/review', {
    user_id: userId,
    status,
  })
  return res.data
}

// ---- 管理端：渠道审核 ----

export async function adminListPendingChannels(
  page = 1,
  pageSize = 20
): Promise<ApiEnvelope<PagedResult<SupplierChannel>>> {
  const res = await api.get('/api/user/supplier/admin/channel/pending', {
    params: { p: page, page_size: pageSize },
  })
  return res.data
}

export async function adminApproveChannel(
  params: SupplierChannelReviewParams
): Promise<ApiEnvelope<Record<string, never>>> {
  const res = await api.post('/api/user/supplier/admin/channel/approve', params)
  return res.data
}

export async function adminRejectChannel(
  channelId: number,
  remark: string
): Promise<ApiEnvelope<Record<string, never>>> {
  const res = await api.post('/api/user/supplier/admin/channel/reject', {
    channel_id: channelId,
    remark,
  })
  return res.data
}

// ---- 管理端：结算打款 ----

export async function adminGetSettlement(
  userId: number,
  page = 1,
  pageSize = 20
): Promise<ApiEnvelope<SupplierSettlementResult>> {
  const res = await api.get('/api/user/supplier/admin/settlement', {
    params: { user_id: userId, p: page, page_size: pageSize },
  })
  return res.data
}

export async function adminPaySupplier(
  params: SupplierPayParams
): Promise<ApiEnvelope<Record<string, never>>> {
  const res = await api.post('/api/user/supplier/admin/pay', params)
  return res.data
}

export async function adminConfiscateSupplier(
  userId: number,
  amount: number,
  remark: string
): Promise<ApiEnvelope<Record<string, never>>> {
  const res = await api.post('/api/user/supplier/admin/confiscate', {
    user_id: userId,
    amount,
    remark,
  })
  return res.data
}
