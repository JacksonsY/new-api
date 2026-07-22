// jzlh-sub 子账号 API 客户端。
import { api } from '@/lib/api'

import type {
  ApiEnvelope,
  CreateResult,
  CreateSubAccountsRequest,
  ListResult,
  UpdateSubAccountRequest,
} from './types'

export async function listSubAccounts(
  page = 1,
  email = ''
): Promise<ApiEnvelope<ListResult>> {
  const res = await api.get('/api/sub-account', {
    params: email ? { p: page, email } : { p: page },
  })
  return res.data
}

export async function createSubAccounts(
  body: CreateSubAccountsRequest
): Promise<ApiEnvelope<CreateResult>> {
  const res = await api.post('/api/sub-account', body)
  return res.data
}

export async function updateSubAccount(
  id: number,
  body: UpdateSubAccountRequest
): Promise<ApiEnvelope<null>> {
  const res = await api.put(`/api/sub-account/${id}`, body)
  return res.data
}

export async function disableSubAccount(
  id: number
): Promise<ApiEnvelope<null>> {
  const res = await api.post(`/api/sub-account/${id}/disable`)
  return res.data
}

export async function deleteSubAccount(id: number): Promise<ApiEnvelope<null>> {
  const res = await api.delete(`/api/sub-account/${id}`)
  return res.data
}
