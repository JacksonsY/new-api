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
import { Loader2 } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/design-system/alert-dialog'
import { Button } from '@/components/design-system/button'
import { Input } from '@/components/design-system/input'
import { PasswordInput } from '@/components/password-input'
import { Card, CardContent, CardHeader } from '@/components/ui/card'
import { Label } from '@/components/ui/label'
import { Skeleton } from '@/components/ui/skeleton'
import { TitledCard } from '@/components/ui/titled-card'

import { deleteStorageSetting, saveStorageSetting } from '../api'
import { parseUserSettings } from '../lib'
import type { UserProfile } from '../types'

interface StorageSettingsCardProps {
  profile: UserProfile | null
  loading: boolean
  onUpdate: () => void
}

interface StorageForm {
  endpoint: string
  bucket: string
  region: string
  access_key_id: string
  secret_key: string
  public_domain: string
}

const EMPTY_FORM: StorageForm = {
  endpoint: '',
  bucket: '',
  region: '',
  access_key_id: '',
  secret_key: '',
  public_domain: '',
}

export function StorageSettingsCard({
  profile,
  loading: pageLoading,
  onUpdate,
}: StorageSettingsCardProps) {
  const { t } = useTranslation()
  const [saving, setSaving] = useState(false)
  const [clearing, setClearing] = useState(false)
  const [confirmClear, setConfirmClear] = useState(false)
  const [configured, setConfigured] = useState(false)
  const [form, setForm] = useState<StorageForm>(EMPTY_FORM)

  useEffect(() => {
    if (!profile?.setting) return
    const storage = parseUserSettings(profile.setting).storage
    if (!storage) {
      setConfigured(false)
      return
    }
    setConfigured(!!(storage.endpoint && storage.bucket))
    setForm({
      endpoint: storage.endpoint ?? '',
      bucket: storage.bucket ?? '',
      region: storage.region ?? '',
      access_key_id: storage.access_key_id ?? '',
      secret_key: storage.secret_key ?? '',
      public_domain: storage.public_domain ?? '',
    })
  }, [profile])

  const updateField = (field: keyof StorageForm, value: string) => {
    setForm((prev) => ({ ...prev, [field]: value }))
  }

  const handleSave = async () => {
    const payload = {
      endpoint: form.endpoint.trim(),
      bucket: form.bucket.trim(),
      region: form.region.trim(),
      access_key_id: form.access_key_id.trim(),
      secret_key: form.secret_key.trim(),
      public_domain: form.public_domain.trim(),
    }
    if (!payload.endpoint || !payload.bucket || !payload.access_key_id) {
      toast.error(t('Please fill in Endpoint, Bucket and AccessKey ID'))
      return
    }
    try {
      setSaving(true)
      const response = await saveStorageSetting(payload)
      if (response.success) {
        toast.success(t('Storage verified and saved'))
        onUpdate()
      } else {
        toast.error(response.message || t('Failed to save storage settings'))
      }
    } catch {
      toast.error(t('Failed to save storage settings'))
    } finally {
      setSaving(false)
    }
  }

  const handleClear = async () => {
    try {
      setClearing(true)
      const response = await deleteStorageSetting()
      if (response.success) {
        toast.success(t('Storage settings cleared'))
        setForm(EMPTY_FORM)
        setConfigured(false)
        onUpdate()
      } else {
        toast.error(response.message || t('Failed to clear storage settings'))
      }
    } catch {
      toast.error(t('Failed to clear storage settings'))
    } finally {
      setClearing(false)
      setConfirmClear(false)
    }
  }

  if (pageLoading) {
    return (
      <Card data-card-hover='false' className='gap-0 overflow-hidden py-0'>
        <CardHeader className='border-b p-4 !pb-4 sm:p-5 sm:!pb-5'>
          <Skeleton className='h-6 w-40' />
          <Skeleton className='mt-2 h-4 w-64' />
        </CardHeader>
        <CardContent className='space-y-4 p-4 sm:p-5'>
          <Skeleton className='h-9 w-full' />
          <Skeleton className='h-9 w-full' />
          <Skeleton className='h-9 w-full' />
        </CardContent>
      </Card>
    )
  }

  const origin = typeof window !== 'undefined' ? window.location.origin : ''

  return (
    <TitledCard
      title={t('Storage Settings')}
      description={t(
        'Configure an S3-compatible bucket (Cloudflare R2, Aliyun OSS, Tencent COS, AWS S3, MinIO, etc.); images and videos generated by API and on-site requests will be archived to your bucket for long-term retention'
      )}
      disableHoverEffect
      contentClassName='space-y-5 sm:space-y-6'
    >
      {/* Endpoint */}
      <div className='space-y-1.5'>
        <Label htmlFor='storageEndpoint'>
          {t('Endpoint (S3 API address)')}
        </Label>
        <Input
          id='storageEndpoint'
          type='url'
          value={form.endpoint}
          onChange={(e) => updateField('endpoint', e.target.value)}
          placeholder='https://<accountid>.r2.cloudflarestorage.com'
        />
      </div>

      {/* Bucket + Region */}
      <div className='grid gap-4 sm:grid-cols-2'>
        <div className='space-y-1.5'>
          <Label htmlFor='storageBucket'>{t('Bucket Name')}</Label>
          <Input
            id='storageBucket'
            value={form.bucket}
            onChange={(e) => updateField('bucket', e.target.value)}
            placeholder='my-bucket'
          />
        </div>
        <div className='space-y-1.5'>
          <Label htmlFor='storageRegion'>{t('Region (optional)')}</Label>
          <Input
            id='storageRegion'
            value={form.region}
            onChange={(e) => updateField('region', e.target.value)}
            placeholder='auto'
          />
        </div>
      </div>

      {/* AccessKey + SecretKey */}
      <div className='grid gap-4 sm:grid-cols-2'>
        <div className='space-y-1.5'>
          <Label htmlFor='storageAccessKey'>{t('AccessKey ID')}</Label>
          <Input
            id='storageAccessKey'
            value={form.access_key_id}
            onChange={(e) => updateField('access_key_id', e.target.value)}
            autoComplete='off'
          />
        </div>
        <div className='space-y-1.5'>
          <Label htmlFor='storageSecretKey'>{t('SecretKey')}</Label>
          <PasswordInput
            id='storageSecretKey'
            value={form.secret_key}
            onChange={(e) => updateField('secret_key', e.target.value)}
            placeholder={t('Enter secret key')}
          />
        </div>
      </div>

      {/* Public access domain */}
      <div className='space-y-1.5'>
        <Label htmlFor='storagePublicDomain'>
          {t('Public Access Domain (optional)')}
        </Label>
        <Input
          id='storagePublicDomain'
          type='url'
          value={form.public_domain}
          onChange={(e) => updateField('public_domain', e.target.value)}
          placeholder={t(
            'https://cdn.example.com (public domain bound to the bucket)'
          )}
        />
      </div>

      {/* Bucket preparation notice */}
      <div className='bg-muted/30 rounded-lg border p-3 sm:p-4'>
        <h5 className='mb-1.5 text-sm font-medium'>
          {t('The bucket needs two preparations:')}
        </h5>
        <ol className='text-muted-foreground space-y-1 text-xs'>
          <li>
            {t(
              '1. CORS: allow GET, HEAD and PUT requests from this site ({{origin}}), with AllowedHeaders set to *',
              { origin }
            )}
          </li>
          <li>
            {t(
              '2. Public read: enable public access or bind a custom domain, otherwise images cannot be displayed on the page.'
            )}
          </li>
        </ol>
        <p className='text-muted-foreground mt-3 text-xs'>
          {t(
            'Saving will write a small test file to the bucket to verify credentials and permissions.'
          )}
        </p>
      </div>

      {/* Actions */}
      <div className='flex justify-end gap-2'>
        {configured && (
          <Button
            variant='outline'
            onClick={() => setConfirmClear(true)}
            disabled={saving || clearing}
          >
            {t('Clear Configuration')}
          </Button>
        )}
        <Button onClick={handleSave} disabled={saving || clearing}>
          {saving && <Loader2 className='mr-2 size-4 animate-spin' />}
          {saving ? t('Verifying...') : t('Verify and Save')}
        </Button>
      </div>

      <AlertDialog
        open={confirmClear}
        onOpenChange={(open) => {
          if (!open) setConfirmClear(false)
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t('Clear storage settings?')}</AlertDialogTitle>
            <AlertDialogDescription>
              {t(
                'Files already archived in the bucket are not affected; new images and videos will no longer be saved to your bucket.'
              )}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={clearing}>
              {t('Cancel')}
            </AlertDialogCancel>
            <AlertDialogAction
              variant='destructive'
              onClick={handleClear}
              disabled={clearing}
            >
              {clearing && <Loader2 className='mr-2 size-4 animate-spin' />}
              {t('Clear Configuration')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </TitledCard>
  )
}
