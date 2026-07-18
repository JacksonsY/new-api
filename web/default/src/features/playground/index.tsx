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
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import {
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
} from '@/components/design-system/tabs'
import { Skeleton } from '@/components/ui/skeleton'

import { PlaygroundChat } from './components/chat/playground-chat'
import { PlaygroundInput } from './components/input/playground-input'
import { VideoInputForm } from './components/video/video-input-form'
import { VideoPlayer } from './components/video/video-player'
import { VideoTaskQueue } from './components/video/video-task-queue'
import { HAPPYHORSE_MODEL_PREFIX } from './constants'
import {
  useChatHandler,
  usePlaygroundConversation,
  usePlaygroundOptions,
  usePlaygroundState,
  useVideoTask,
} from './hooks'
import type { VideoTaskItem } from './types'

export function Playground() {
  const { t } = useTranslation()
  const {
    config,
    parameterEnabled,
    messages,
    isLoadingMessages,
    models,
    groups,
    updateMessages,
    setModels,
    setGroups,
    updateConfig,
    updateParameterEnabled,
    clearMessages,
  } = usePlaygroundState()

  const { sendChat, stopGeneration, isGenerating } = useChatHandler({
    config,
    parameterEnabled,
    onMessageUpdate: updateMessages,
  })

  const {
    editingMessageKey,
    handleSendMessage,
    handleRegenerateMessage,
    handleEditMessage,
    handleEditOpenChange,
    applyEdit,
    handleDeleteMessage,
  } = usePlaygroundConversation({
    messages,
    updateMessages,
    sendChat,
  })

  const handleClearMessages = () => {
    handleEditOpenChange(false)
    clearMessages()
  }

  const { isLoadingModels } = usePlaygroundOptions({
    currentGroup: config.group,
    currentModel: config.model,
    setGroups,
    setModels,
    updateConfig,
  })

  const {
    tasks,
    isSubmitting,
    submitError,
    submitTask,
    clearFinishedTasks,
    removeTask,
  } = useVideoTask()

  const [previewTask, setPreviewTask] = useState<VideoTaskItem | null>(null)

  // The video tab only appears when the user can reach a video model
  const hasVideoModels = models.some((model) =>
    model.value.startsWith(HAPPYHORSE_MODEL_PREFIX)
  )

  const handleVideoSubmit: React.ComponentProps<
    typeof VideoInputForm
  >['onSubmit'] = async (req, apiKey, tokenId, meta) => {
    try {
      await submitTask(req, apiKey, tokenId, meta)
    } catch (err) {
      toast.error(
        err instanceof Error ? err.message : t('Failed to submit video task')
      )
    }
  }

  const chatPane = (
    <>
      {/* Full-width scroll container: scrolling works even over side whitespace */}
      <div className='flex min-h-0 flex-1 flex-col overflow-hidden'>
        <PlaygroundChat
          messages={messages}
          isLoadingMessages={isLoadingMessages}
          onRegenerateMessage={handleRegenerateMessage}
          onEditMessage={handleEditMessage}
          onDeleteMessage={handleDeleteMessage}
          onSelectPrompt={handleSendMessage}
          isGenerating={isGenerating}
          editingKey={editingMessageKey}
          onCancelEdit={handleEditOpenChange}
          onSaveEdit={(newContent) => applyEdit(newContent, false)}
          onSaveEditAndSubmit={(newContent) => applyEdit(newContent, true)}
        />
      </div>

      {/* Input area: center content and constrain to the same container width */}
      <div className='mx-auto w-full max-w-4xl'>
        <PlaygroundInput
          config={config}
          disabled={isGenerating}
          groups={groups}
          groupValue={config.group}
          isGenerating={isGenerating}
          isModelLoading={isLoadingModels}
          modelValue={config.model}
          models={models}
          onGroupChange={(value) => updateConfig('group', value)}
          onConfigChange={updateConfig}
          onClearMessages={handleClearMessages}
          onModelChange={(value) => updateConfig('model', value)}
          onParameterEnabledChange={updateParameterEnabled}
          onStop={stopGeneration}
          onSubmit={handleSendMessage}
          parameterEnabled={parameterEnabled}
          hasMessages={messages.length > 0}
        />
      </div>
    </>
  )

  if (!hasVideoModels) {
    return (
      <div className='relative flex size-full min-h-0 flex-col overflow-hidden'>
        {chatPane}
      </div>
    )
  }

  const hasPendingTask = tasks.some(
    (task) => task.status === 'queued' || task.status === 'in_progress'
  )

  return (
    <div className='relative flex size-full min-h-0 flex-col overflow-hidden'>
      <Tabs
        className='flex size-full min-h-0 flex-col overflow-hidden'
        defaultValue='chat'
      >
        <div className='flex shrink-0 justify-center border-b px-4 pt-2 pb-2'>
          <TabsList>
            <TabsTrigger value='chat'>{t('Chat')}</TabsTrigger>
            <TabsTrigger value='video'>{t('Video')}</TabsTrigger>
          </TabsList>
        </div>

        <TabsContent
          className='flex min-h-0 flex-1 flex-col overflow-hidden'
          value='chat'
        >
          {chatPane}
        </TabsContent>

        <TabsContent
          className='flex min-h-0 flex-1 gap-4 overflow-hidden p-4'
          value='video'
        >
          {/* Left: input form */}
          <div className='flex w-80 shrink-0 flex-col overflow-y-auto rounded-xl border'>
            <VideoInputForm
              models={models}
              isSubmitting={isSubmitting}
              onSubmit={handleVideoSubmit}
            />
          </div>

          {/* Right: task queue + preview */}
          <div className='flex flex-1 flex-col gap-4 overflow-y-auto'>
            {previewTask && (
              <VideoPlayer
                task={previewTask}
                onClose={() => setPreviewTask(null)}
              />
            )}
            {!previewTask && hasPendingTask && (
              <div className='border-border bg-background rounded-xl border shadow-sm'>
                <div className='flex items-center justify-between border-b px-4 py-2'>
                  <div className='flex flex-1 flex-col gap-1.5'>
                    <Skeleton className='h-3 w-24' />
                    <Skeleton className='h-4 w-48' />
                  </div>
                </div>
                <div className='p-3'>
                  <Skeleton className='aspect-video w-full rounded-lg' />
                </div>
                <div className='border-t px-4 py-2'>
                  <Skeleton className='h-3 w-3/4' />
                </div>
              </div>
            )}
            {submitError && (
              <div className='border-destructive/50 bg-destructive/10 text-destructive rounded-lg border px-4 py-3 text-sm'>
                {submitError}
              </div>
            )}
            <VideoTaskQueue
              tasks={tasks}
              onPreview={setPreviewTask}
              onRemove={(id) => {
                if (previewTask?.id === id) setPreviewTask(null)
                removeTask(id)
              }}
              onClearFinished={() => {
                if (
                  previewTask?.status === 'completed' ||
                  previewTask?.status === 'failed'
                ) {
                  setPreviewTask(null)
                }
                clearFinishedTasks()
              }}
            />
          </div>
        </TabsContent>
      </Tabs>
    </div>
  )
}
