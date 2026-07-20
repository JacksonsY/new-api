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
import type {
  PlaygroundConfig,
  ParameterEnabled,
  VideoModelConfig,
  VideoModelType,
} from './types'

// Message constants
export const MESSAGE_ROLES = {
  USER: 'user',
  ASSISTANT: 'assistant',
  SYSTEM: 'system',
} as const

export const MESSAGE_STATUS = {
  LOADING: 'loading',
  STREAMING: 'streaming',
  COMPLETE: 'complete',
  ERROR: 'error',
} as const

// API endpoints
export const API_ENDPOINTS = {
  CHAT_COMPLETIONS: '/pg/chat/completions',
  USER_MODELS: '/api/user/models',
  USER_GROUPS: '/api/user/self/groups',
} as const

// Default group — uses 'default' as the safe fallback; auto-group is
// only selected when the backend confirms it is available for the user.
export const DEFAULT_GROUP = 'default' as const

// Default configuration
export const DEFAULT_CONFIG: PlaygroundConfig = {
  model: 'gpt-4o',
  group: DEFAULT_GROUP,
  temperature: 0.7,
  top_p: 1,
  max_tokens: 4096,
  frequency_penalty: 0,
  presence_penalty: 0,
  seed: null,
  stream: true,
}

export const DEFAULT_PARAMETER_ENABLED: ParameterEnabled = {
  temperature: true,
  top_p: true,
  max_tokens: false,
  frequency_penalty: true,
  presence_penalty: true,
  seed: false,
}

// Storage keys
export const STORAGE_KEYS = {
  CONFIG: 'playground_config',
  MESSAGES: 'playground_messages',
  PARAMETER_ENABLED: 'playground_parameter_enabled',
} as const

// Error messages
export const ERROR_MESSAGES = {
  API_REQUEST_ERROR: 'Request error occurred',
  NETWORK_ERROR: 'Network connection failed or server not responding',
  PARSE_ERROR: 'Error parsing response data',
  STREAM_START_ERROR: 'Error establishing connection',
  CONNECTION_CLOSED: 'Connection closed',
  INTERRUPTED: 'Generation was interrupted',
} as const

// Message action button styles
export const MESSAGE_ACTION_BUTTON_STYLES = {
  BASE: 'size-7 text-muted-foreground hover:text-foreground',
  DELETE: 'size-7 text-muted-foreground hover:text-destructive',
  ICON: 'size-4',
} as const

// Video generation constants
export const VIDEO_API_ENDPOINTS = {
  SUBMIT: '/v1/video/generations',
  STATUS: (taskId: string) => `/v1/video/generations/${taskId}`,
} as const

// Video models are served by the `ali` channel and identified by model name
// prefix, so both the 1.0 and 1.1 families are matched.
export const VIDEO_MODELS: VideoModelConfig[] = [
  {
    model: 'happyhorse-1.0-t2v',
    label: 'Text-to-Video',
    type: 'text-to-video',
    requiresImage: false,
    requiresVideo: false,
    supportedSizes: ['720P', '1080P'],
    durationRange: [2, 15],
  },
  {
    model: 'happyhorse-1.1-t2v',
    label: 'Text-to-Video',
    type: 'text-to-video',
    requiresImage: false,
    requiresVideo: false,
    supportedSizes: ['720P', '1080P'],
    durationRange: [2, 15],
  },
  {
    model: 'happyhorse-1.0-i2v',
    label: 'Image-to-Video',
    type: 'image-to-video',
    requiresImage: true,
    requiresVideo: false,
    supportedSizes: ['720P', '1080P'],
    durationRange: [2, 15],
  },
  {
    model: 'happyhorse-1.1-i2v',
    label: 'Image-to-Video',
    type: 'image-to-video',
    requiresImage: true,
    requiresVideo: false,
    supportedSizes: ['720P', '1080P'],
    durationRange: [2, 15],
  },
  {
    model: 'happyhorse-1.0-r2v',
    label: 'Reference-to-Video',
    type: 'reference-to-video',
    requiresImage: true,
    requiresVideo: false,
    supportedSizes: ['720P', '1080P'],
    durationRange: [2, 15],
  },
  {
    model: 'happyhorse-1.1-r2v',
    label: 'Reference-to-Video',
    type: 'reference-to-video',
    requiresImage: true,
    requiresVideo: false,
    supportedSizes: ['720P', '1080P'],
    durationRange: [2, 15],
  },
  {
    model: 'happyhorse-1.0-video-edit',
    label: 'Video Edit',
    type: 'video-edit',
    requiresImage: false,
    requiresVideo: true,
    supportedSizes: ['720P', '1080P'],
    durationRange: [2, 15],
  },
  // 豆包 Seedance 2.0(aiai 渠道,按秒计费):原生字段 resolution/duration,
  // 支持 extra_body.real_person_mode 真人模式。分辨率档位与 aiai 价目表一致。
  {
    model: 'doubao-seedance-2.0',
    label: 'Text-to-Video',
    type: 'text-to-video',
    requiresImage: false,
    requiresVideo: false,
    supportedSizes: ['480P', '720P', '1080P', '4K'],
    durationRange: [4, 15],
    family: 'seedance',
  },
  {
    model: 'doubao-seedance-2.0-mini',
    label: 'Text-to-Video',
    type: 'text-to-video',
    requiresImage: false,
    requiresVideo: false,
    supportedSizes: ['480P', '720P'],
    durationRange: [4, 15],
    family: 'seedance',
  },
  {
    model: 'doubao-seedance-2.0-fast',
    label: 'Text-to-Video',
    type: 'text-to-video',
    requiresImage: false,
    requiresVideo: false,
    supportedSizes: ['480P', '720P'],
    durationRange: [4, 15],
    family: 'seedance',
  },
]

export const VIDEO_MODEL_TYPE_LABELS: Record<VideoModelType, string> = {
  'text-to-video': 'T2V',
  'image-to-video': 'I2V',
  'reference-to-video': 'R2V',
  'video-edit': 'Edit',
}

export const VIDEO_POLLING_INTERVAL = 5000

// Cap the persisted queue so localStorage cannot grow without bound
export const VIDEO_TASK_HISTORY_LIMIT = 50

// 轮询上限。上游任务被清理、密钥被删等情况下状态查询会一直失败，没有上限的话
// 只要标签页开着就会以固定频率永远轮询下去，任务也永远停在进行中。
// 240 次 × 5 秒 = 20 分钟，远超正常出片时间。
export const VIDEO_MAX_POLL_ATTEMPTS = 240

export const STORAGE_KEYS_VIDEO = {
  TASK_QUEUE: 'playground_video_tasks',
} as const

// Message action labels
export const MESSAGE_ACTION_LABELS = {
  COPY: 'Copy',
  COPIED: 'Copied!',
  REGENERATE: 'Regenerate',
  SHOW_PREVIEW: 'Show preview',
  SHOW_SOURCE: 'Show source',
  EDIT: 'Edit',
  DELETE: 'Delete',
  NO_CONTENT: 'No content to copy',
  WAIT_GENERATION: 'Please wait for the current generation to complete',
} as const
