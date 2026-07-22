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
import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/design-system/button'
import { Dialog } from '@/components/dialog'
import { api } from '@/lib/api'

interface ChannelHealthRow {
  channel_id: number
  has_data: boolean
  error_rate: number
  has_ttft: boolean
  ttft_ms: number
  inflight: number
  circuit_open: boolean
  circuit_half_open: boolean
}

interface Props {
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function ChannelHealthDialog(props: Props) {
  const { t } = useTranslation()
  const [loading, setLoading] = useState(false)
  const [enabled, setEnabled] = useState(true)
  const [rows, setRows] = useState<ChannelHealthRow[]>([])
  const seqRef = useRef(0)

  const load = useCallback(() => {
    const seq = ++seqRef.current
    setLoading(true)
    api
      .get('/api/channel_health', {
        disableDuplicate: true,
      } as Record<string, unknown>)
      .then((res) => {
        if (seq !== seqRef.current) return
        const body = res.data
        if (body?.success) {
          setEnabled(Boolean(body.data?.enabled))
          setRows((body.data?.channels as ChannelHealthRow[]) || [])
        } else {
          toast.error(body?.message || t('Request failed'))
        }
      })
      .catch(() => {
        if (seq !== seqRef.current) return
        toast.error(t('Request failed'))
      })
      .finally(() => {
        if (seq !== seqRef.current) return
        setLoading(false)
      })
  }, [t])

  useEffect(() => {
    if (!props.open) return
    load()
  }, [props.open, load])

  const reset = useCallback(
    async (channelId?: number) => {
      try {
        const res = await api.delete('/api/channel_health', {
          params: channelId ? { channel_id: channelId } : undefined,
        })
        if (res.data?.success) {
          toast.success(t('Done'))
          load()
        } else {
          toast.error(res.data?.message || t('Request failed'))
        }
      } catch {
        toast.error(t('Request failed'))
      }
    },
    [t, load]
  )

  const stateLabel = (r: ChannelHealthRow) => {
    if (r.circuit_open) return t('Circuit open')
    if (r.circuit_half_open) return t('Half-open')
    return t('Healthy')
  }

  return (
    <Dialog
      open={props.open}
      onOpenChange={props.onOpenChange}
      title={t('Channel routing health')}
      contentClassName='sm:max-w-2xl'
      contentHeight='auto'
      bodyClassName='space-y-4'
    >
      <div className='flex items-center justify-between gap-4'>
        <p className='text-muted-foreground text-xs'>
          {enabled
            ? t(
                'Live per-channel health observed by this instance (in-flight counts are per-instance).'
              )
            : t(
                'Adaptive routing is disabled; values are stale until enabled.'
              )}
        </p>
        <div className='flex shrink-0 gap-2'>
          <Button
            type='button'
            variant='outline'
            size='sm'
            onClick={() => load()}
            disabled={loading}
          >
            {t('Refresh')}
          </Button>
          <Button
            type='button'
            variant='outline'
            size='sm'
            onClick={() => reset()}
            disabled={loading || rows.length === 0}
          >
            {t('Clear all')}
          </Button>
        </div>
      </div>
      {loading ? (
        <div className='text-muted-foreground py-8 text-center text-sm'>
          {t('Loading...')}
        </div>
      ) : rows.length > 0 ? (
        <div className='overflow-x-auto'>
          <table className='w-full text-sm'>
            <thead>
              <tr className='text-muted-foreground border-b text-left'>
                <th className='py-1 pr-4'>{t('Channel')}</th>
                <th className='py-1 pr-4'>{t('TTFT (ms)')}</th>
                <th className='py-1 pr-4'>{t('Error rate')}</th>
                <th className='py-1 pr-4'>{t('In-flight')}</th>
                <th className='py-1 pr-4'>{t('State')}</th>
                <th className='py-1'></th>
              </tr>
            </thead>
            <tbody>
              {rows.map((r) => (
                <tr key={r.channel_id} className='border-b'>
                  <td className='py-1 pr-4 font-medium'>{r.channel_id}</td>
                  <td className='py-1 pr-4'>
                    {r.has_ttft ? Math.round(r.ttft_ms) : '-'}
                  </td>
                  <td className='py-1 pr-4'>
                    {r.has_data ? `${(r.error_rate * 100).toFixed(1)}%` : '-'}
                  </td>
                  <td className='py-1 pr-4'>{r.inflight}</td>
                  <td className='py-1 pr-4'>{stateLabel(r)}</td>
                  <td className='py-1 text-right'>
                    {(r.circuit_open || r.circuit_half_open) && (
                      <Button
                        type='button'
                        variant='ghost'
                        size='sm'
                        onClick={() => reset(r.channel_id)}
                      >
                        {t('Reset')}
                      </Button>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : (
        <div className='text-muted-foreground py-8 text-center text-sm'>
          {t('No data available')}
        </div>
      )}
    </Dialog>
  )
}
