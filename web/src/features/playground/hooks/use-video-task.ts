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
import { t } from 'i18next'
import { useCallback, useEffect, useRef, useState } from 'react'
import { toast } from 'sonner'

import {
  submitVideoGeneration,
  fetchVideoTaskStatus,
  fetchTokenKey,
} from '../api'
import {
  VIDEO_MAX_POLL_ATTEMPTS,
  VIDEO_POLLING_INTERVAL,
  VIDEO_TASK_HISTORY_LIMIT,
  STORAGE_KEYS_VIDEO,
} from '../constants'
import type {
  VideoGenerationRequest,
  VideoTaskItem,
  VideoTaskStatus,
  VideoModelType,
} from '../types'

function loadTasksFromStorage(): VideoTaskItem[] {
  try {
    const raw = localStorage.getItem(STORAGE_KEYS_VIDEO.TASK_QUEUE)
    if (!raw) return []
    const parsed = JSON.parse(raw) as VideoTaskItem[]
    return Array.isArray(parsed)
      ? parsed.slice(0, VIDEO_TASK_HISTORY_LIMIT)
      : []
  } catch {
    return []
  }
}

function saveTasksToStorage(tasks: VideoTaskItem[]) {
  try {
    localStorage.setItem(STORAGE_KEYS_VIDEO.TASK_QUEUE, JSON.stringify(tasks))
  } catch {
    // ignore storage errors (quota exceeded, private mode)
  }
}

// 后端在任务刚入库、轮询 worker 尚未接手时可能返回四态之外的值。原样存下来会让
// 任务既不算进行中也不算已完成，在队列里整条消失，直到下一次状态刷新才冒出来。
function normalizeVideoStatus(status: string | undefined): VideoTaskStatus {
  switch (status) {
    case 'queued':
    case 'in_progress':
    case 'completed':
    case 'failed':
      return status
    default:
      return 'in_progress'
  }
}

export function useVideoTask() {
  const [tasks, setTasks] = useState<VideoTaskItem[]>(() =>
    loadTasksFromStorage()
  )
  const [isSubmitting, setIsSubmitting] = useState(false)
  const [submitError, setSubmitError] = useState<string | null>(null)
  const pollingTimers = useRef<Record<string, ReturnType<typeof setInterval>>>(
    {}
  )

  // Persist tasks whenever they change
  useEffect(() => {
    saveTasksToStorage(tasks)
  }, [tasks])

  const updateTask = useCallback(
    (id: string, patch: Partial<VideoTaskItem>) => {
      setTasks((prev) =>
        prev.map((task) => (task.id === id ? { ...task, ...patch } : task))
      )
    },
    []
  )

  const stopPolling = useCallback((id: string) => {
    const timer = pollingTimers.current[id]
    if (timer) {
      clearInterval(timer)
      delete pollingTimers.current[id]
    }
  }, [])

  const startPolling = useCallback(
    (id: string, apiKey: string) => {
      // Avoid duplicate intervals
      if (pollingTimers.current[id]) return
      let inFlight = false
      let attempts = 0

      const poll = async () => {
        if (inFlight) return
        inFlight = true
        try {
          attempts += 1
          if (attempts > VIDEO_MAX_POLL_ATTEMPTS) {
            stopPolling(id)
            updateTask(id, {
              status: 'failed',
              error: t('Timed out waiting for the video task'),
            })
            return
          }
          const res = await fetchVideoTaskStatus(id, apiKey)
          const status = normalizeVideoStatus(res.status)
          const videoUrl =
            status === 'completed'
              ? (res.metadata?.url as string | undefined)
              : undefined
          const errorMsg =
            status === 'failed'
              ? (res.error?.message ?? t('Generation failed'))
              : undefined

          updateTask(id, {
            status,
            progress: res.progress ?? 0,
            ...(videoUrl ? { videoUrl } : {}),
            ...(res.completed_at ? { completedAt: res.completed_at } : {}),
            ...(errorMsg ? { error: errorMsg } : {}),
          })

          if (status === 'completed' || status === 'failed') {
            stopPolling(id)
            if (status === 'completed') {
              toast.success(t('Video generation completed'))
            } else {
              toast.error(errorMsg ?? t('Video generation failed'))
            }
          }
        } catch {
          // polling errors are transient; keep retrying
        } finally {
          inFlight = false
        }
      }

      // First poll immediately, then repeat
      void poll()
      pollingTimers.current[id] = setInterval(poll, VIDEO_POLLING_INTERVAL)
    },
    [updateTask, stopPolling]
  )

  // On mount, resume polling for any unfinished task that still has a tokenId.
  // All three dependencies are stable useCallbacks, so this runs once.
  useEffect(() => {
    const resumePolling = async () => {
      for (const task of loadTasksFromStorage()) {
        const tokenId = task.tokenId
        if (tokenId == null) continue
        if (task.status !== 'queued' && task.status !== 'in_progress') continue

        try {
          const realKey = await fetchTokenKey(tokenId)
          if (realKey) {
            startPolling(task.id, realKey)
          } else {
            updateTask(task.id, {
              status: 'failed',
              error: t('API Key no longer valid'),
            })
            toast.error(
              t('Task failed: API Key invalid', {
                prompt: task.prompt.slice(0, 30),
              })
            )
          }
        } catch {
          updateTask(task.id, {
            status: 'failed',
            error: t('Failed to restore API Key'),
          })
          toast.error(
            t('Task failed: cannot restore key', {
              prompt: task.prompt.slice(0, 30),
            })
          )
        }
      }
    }
    void resumePolling()

    // 每个标签页各持一份内存副本，而持久化是整体覆写。不监听 storage 的话，
    // 本标签页的下一次写入会抹掉另一个标签页刚提交的任务（已付费任务失联）。
    const syncFromOtherTab = (event: StorageEvent) => {
      if (event.key !== STORAGE_KEYS_VIDEO.TASK_QUEUE) return
      setTasks(loadTasksFromStorage())
    }
    window.addEventListener('storage', syncFromOtherTab)

    const timers = pollingTimers
    return () => {
      window.removeEventListener('storage', syncFromOtherTab)
      // Clear all timers on unmount
      Object.values(timers.current).forEach(clearInterval)
      timers.current = {}
    }
  }, [startPolling, updateTask])

  const submitTask = useCallback(
    async (
      req: VideoGenerationRequest,
      apiKey: string,
      tokenId: number,
      meta?: { size?: string; duration?: number; type?: VideoModelType }
    ) => {
      setIsSubmitting(true)
      setSubmitError(null)
      try {
        const res = await submitVideoGeneration(req, apiKey)
        const taskId = res.id ?? res.task_id
        if (!taskId) {
          throw new Error(
            res.error?.message ?? t('No task ID returned from server')
          )
        }
        const newTask: VideoTaskItem = {
          id: taskId,
          model: req.model,
          prompt: req.prompt,
          status: res.status ?? 'queued',
          progress: res.progress ?? 0,
          createdAt: res.created_at ?? Math.floor(Date.now() / 1000),
          tokenId,
          ...(meta?.size ? { size: meta.size } : {}),
          ...(meta?.duration != null ? { duration: meta.duration } : {}),
          ...(meta?.type ? { type: meta.type } : {}),
        }
        setTasks((prev) =>
          [newTask, ...prev].slice(0, VIDEO_TASK_HISTORY_LIMIT)
        )
        startPolling(taskId, apiKey)
        return newTask
      } catch (err) {
        const msg = err instanceof Error ? err.message : t('Submission failed')
        setSubmitError(msg)
        throw err
      } finally {
        setIsSubmitting(false)
      }
    },
    [startPolling]
  )

  const clearFinishedTasks = useCallback(() => {
    setTasks((prev) =>
      prev.filter(
        (task) => task.status === 'queued' || task.status === 'in_progress'
      )
    )
  }, [])

  const removeTask = useCallback(
    (id: string) => {
      stopPolling(id)
      setTasks((prev) => prev.filter((task) => task.id !== id))
    },
    [stopPolling]
  )

  return {
    tasks,
    isSubmitting,
    submitError,
    submitTask,
    clearFinishedTasks,
    removeTask,
  }
}
