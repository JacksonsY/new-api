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
import { ImageIcon, VideoIcon, XIcon } from 'lucide-react'
import { useCallback, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Input } from '@/components/design-system/input'
import { cn } from '@/lib/utils'

interface MediaDropZoneProps {
  accept: 'image' | 'video'
  value?: string
  onChange: (url: string) => void
}

export function MediaDropZone({ accept, value, onChange }: MediaDropZoneProps) {
  const { t } = useTranslation()
  const [isDragging, setIsDragging] = useState(false)
  const [previewFailed, setPreviewFailed] = useState(false)

  const isImage = accept === 'image'

  const applyUrl = useCallback(
    (url: string) => {
      setPreviewFailed(false)
      onChange(url)
    },
    [onChange]
  )

  // The upstream provider fetches reference media by URL, so only a publicly
  // reachable http(s) URL works here. Dropping a local file is rejected on
  // purpose: a blob: URL would be accepted by the form but unreachable from
  // the provider.
  const handleDrop = useCallback(
    (e: React.DragEvent) => {
      e.preventDefault()
      setIsDragging(false)
      const uri = e.dataTransfer.getData('text/uri-list').trim()
      const url = /^https?:\/\//i.test(uri)
        ? uri
        : e.dataTransfer.getData('text/plain').trim()
      if (/^https?:\/\//i.test(url)) applyUrl(url)
    },
    [applyUrl]
  )

  const showPreview = !!value && !previewFailed

  return (
    <div className='flex flex-col gap-1.5'>
      <div
        onDragOver={(e) => {
          e.preventDefault()
          setIsDragging(true)
        }}
        onDragLeave={() => setIsDragging(false)}
        onDrop={handleDrop}
        className={cn(
          'border-muted flex flex-col items-center justify-center gap-1.5 rounded-md border border-dashed p-4 transition-colors',
          isDragging && 'border-primary bg-primary/5'
        )}
      >
        {showPreview ? (
          <div className='relative w-full'>
            {isImage ? (
              <img
                src={value}
                alt={t('Preview')}
                className='mx-auto max-h-40 rounded object-contain'
                onError={() => setPreviewFailed(true)}
              />
            ) : (
              <video
                src={value}
                className='mx-auto max-h-40 rounded object-contain'
                controls
                onError={() => setPreviewFailed(true)}
              />
            )}
            <button
              type='button'
              aria-label={t('Remove')}
              onClick={() => applyUrl('')}
              className='bg-destructive text-destructive-foreground absolute top-1 right-1 flex size-5 items-center justify-center rounded-full text-xs'
            >
              <XIcon className='size-3' />
            </button>
          </div>
        ) : (
          <>
            {isImage ? (
              <ImageIcon className='text-muted-foreground size-6' />
            ) : (
              <VideoIcon className='text-muted-foreground size-6' />
            )}
            <span className='text-muted-foreground text-center text-xs'>
              {t('Paste or drop a public media URL')}
            </span>
          </>
        )}
      </div>

      <Input
        placeholder={isImage ? t('Image URL') : t('Video URL')}
        value={value ?? ''}
        onChange={(e) => applyUrl(e.target.value.trim())}
        className='text-sm'
      />
    </div>
  )
}
