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
import { QRCodeSVG } from 'qrcode.react'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Loader2 } from 'lucide-react'

import { getEpayOrderStatus, requestEpayQRPayment } from '../../api'
import type { PaymentMethod } from '../../types'

interface EpayQRDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  amount: number
  paymentMethod: PaymentMethod | undefined
  onSuccess: () => void
}

const POLL_INTERVAL_MS = 3000
const POLL_TIMEOUT_MS = 5 * 60 * 1000

export function EpayQRDialog({
  open,
  onOpenChange,
  amount,
  paymentMethod,
  onSuccess,
}: EpayQRDialogProps) {
  const { t } = useTranslation()
  const [loading, setLoading] = useState(false)
  const [qrValue, setQrValue] = useState('')
  const [tradeNo, setTradeNo] = useState('')
  const [errorMsg, setErrorMsg] = useState('')
  const pollTimer = useRef<number | null>(null)
  const pollDeadline = useRef<number>(0)

  const stopPolling = () => {
    if (pollTimer.current !== null) {
      window.clearInterval(pollTimer.current)
      pollTimer.current = null
    }
  }

  // 发起下单：每次打开对话框都拉一次新二维码
  useEffect(() => {
    if (!open || !paymentMethod) return
    let cancelled = false
    setLoading(true)
    setQrValue('')
    setTradeNo('')
    setErrorMsg('')

    const createOrder = async () => {
      try {
        const res = await requestEpayQRPayment({
          amount,
          payment_method: paymentMethod.type,
        })
        if (cancelled) return
        if (res.data && (res.data.qrcode || res.data.payurl)) {
          setQrValue(res.data.qrcode || res.data.payurl)
          setTradeNo(res.data.trade_no)
        } else {
          setErrorMsg(res.message || t('Failed to create payment'))
        }
      } catch (e: unknown) {
        if (!cancelled) {
          setErrorMsg(
            e instanceof Error ? e.message : t('Failed to create payment')
          )
        }
      } finally {
        if (!cancelled) setLoading(false)
      }
    }
    void createOrder()

    return () => {
      cancelled = true
    }
  }, [open, amount, paymentMethod, t])

  // 轮询本地订单状态，支付成功即回调 + 关闭
  useEffect(() => {
    if (!open || !tradeNo) return
    pollDeadline.current = Date.now() + POLL_TIMEOUT_MS
    pollTimer.current = window.setInterval(async () => {
      if (Date.now() > pollDeadline.current) {
        stopPolling()
        return
      }
      try {
        const res = await getEpayOrderStatus(tradeNo)
        if (res.success && res.data?.status === 'success') {
          stopPolling()
          toast.success(t('Payment successful'))
          onSuccess()
          onOpenChange(false)
        }
      } catch {
        /* 单次轮询失败忽略，继续下一轮 */
      }
    }, POLL_INTERVAL_MS)

    return stopPolling
  }, [open, tradeNo, onSuccess, onOpenChange, t])

  const renderQRBody = () => {
    if (loading) {
      return (
        <Loader2 className='text-muted-foreground h-8 w-8 animate-spin' />
      )
    }
    if (errorMsg) {
      return <p className='text-destructive text-center text-sm'>{errorMsg}</p>
    }
    if (!qrValue) return null
    return (
      <>
        <div className='rounded-lg bg-white p-4'>
          <QRCodeSVG value={qrValue} size={200} />
        </div>
        <p className='text-muted-foreground flex items-center gap-1.5 text-xs'>
          <Loader2 className='h-3 w-3 animate-spin' />
          {t('Waiting for payment...')}
        </p>
      </>
    )
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className='sm:max-w-sm'>
        <DialogHeader>
          <DialogTitle>{t('Scan to Pay')}</DialogTitle>
          <DialogDescription>
            {t('Scan the QR code with {{method}} to complete payment', {
              method: paymentMethod?.name ?? '',
            })}
          </DialogDescription>
        </DialogHeader>

        <div className='flex min-h-56 flex-col items-center justify-center gap-3 py-2'>
          {renderQRBody()}
        </div>
      </DialogContent>
    </Dialog>
  )
}
