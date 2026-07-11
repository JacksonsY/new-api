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
import {
  QueryCache,
  QueryClient,
  QueryClientProvider,
} from '@tanstack/react-query'
import { RouterProvider, createRouter } from '@tanstack/react-router'
import { AxiosError } from 'axios'
import i18next from 'i18next'
import { StrictMode } from 'react'
import ReactDOM from 'react-dom/client'
import { toast } from 'sonner'

import { RoutePendingLoader } from '@/components/brand-loader'
import { getStatus } from '@/lib/api'
import { installBuildMetadata } from '@/lib/build-metadata'
import { applyFaviconToDom } from '@/lib/dom-utils'
import '@/lib/dayjs'
import { initializeFrontendCache } from '@/lib/frontend-cache'
import { handleServerError } from '@/lib/handle-server-error'
import { useAuthStore } from '@/stores/auth-store'

import { DirectionProvider } from './context/direction-provider'
import { ThemeProvider } from './context/theme-provider'
import { i18nReady } from './i18n/config'
import { startApplicationAfterI18n } from './i18n/start-application'
// Generated Routes
import { routeTree } from './routeTree.gen'

// Styles
import './styles/index.css'

// Ensure VChart theme is initialized before any chart mounts (prevents white default theme flash)
// VChart theme is driven by our ThemeProvider (html.light/html.dark) via per-chart `theme` prop.
initializeFrontendCache()
installBuildMetadata()

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      retry: (failureCount, error) => {
        // eslint-disable-next-line no-console
        if (import.meta.env.DEV) console.log({ failureCount, error })

        if (failureCount >= 0 && import.meta.env.DEV) return false
        if (failureCount > 3 && import.meta.env.PROD) return false

        return !(
          error instanceof AxiosError &&
          [401, 403].includes(error.response?.status ?? 0)
        )
      },
      // Keep focused tabs from silently re-running heavy pages like logs.
      refetchOnWindowFocus: false,
      staleTime: 10 * 1000, // 10s
    },
    mutations: {
      onError: (error) => {
        handleServerError(error)

        if (error instanceof AxiosError) {
          if (error.response?.status === 304) {
            toast.error(i18next.t('Content not modified!'))
          }
        }
      },
    },
  },
  queryCache: new QueryCache({
    onError: (error) => {
      if (error instanceof AxiosError) {
        if (error.response?.status === 401) {
          toast.error(i18next.t('Session expired!'))
          useAuthStore.getState().auth.reset()
          const redirect = `${router.history.location.href}`
          router.navigate({ to: '/sign-in', search: { redirect } })
        }
        if (error.response?.status === 500) {
          toast.error(i18next.t('Internal Server Error!'))
          router.navigate({ to: '/500' })
        }
      }
    },
  }),
})

// Create a new router instance
const router = createRouter({
  routeTree,
  context: { queryClient },
  defaultPreload: 'intent',
  defaultPreloadStaleTime: 0,
  // 路由跳转/懒加载 chunk 的等待界面复用品牌开屏视觉。阈值 300ms：热切换
  // （chunk 已缓存、数据很快）保持 TanStack 默认的“保留旧页面 + 顶部进度条”
  // 顺滑直达，绝不闪 loader；只有真实等待（首次拉取 chunk、慢数据）超过
  // 300ms 才切换到品牌 loader，且一旦出现至少停留 400ms 避免一闪而过。
  defaultPendingComponent: RoutePendingLoader,
  defaultPendingMs: 300,
  defaultPendingMinMs: 400,
})

// Register the router instance for type safety
declare module '@tanstack/react-router' {
  interface Register {
    router: typeof router
  }
}

// Render the app
const rootElement = document.querySelector<HTMLElement>('#root')
if (!rootElement) {
  throw new Error('Root element not found')
}
// Set document.title and favicon from cached status, then refresh from network
;(function initSystemBranding() {
  try {
    if (typeof window === 'undefined' || typeof document === 'undefined') return
    const apply = (name: string) => {
      document.title = name
      const metaTitle = document.querySelector(
        'meta[name="title"]'
      ) as HTMLMetaElement | null
      if (metaTitle) metaTitle.setAttribute('content', name)
    }
    // Cache-first
    try {
      const saved = localStorage.getItem('status')
      if (saved) {
        const s = JSON.parse(saved)
        if (s?.system_name) apply(s.system_name)
        if (s?.logo) applyFaviconToDom(s.logo)
      }
    } catch {
      /* empty */
    }
    // Background refresh
    getStatus()
      .then((s) => {
        if (s?.system_name) {
          apply(s.system_name as string)
          try {
            localStorage.setItem('status', JSON.stringify(s))
          } catch {
            /* empty */
          }
        }
        if (s?.logo) applyFaviconToDom(s.logo as string)
      })
      .catch(() => {
        /* empty */
      })
  } catch {
    /* empty */
  }
})()
// 开屏页最短可见时长（自页面导航起算）：加载很快时避免 logo 闪一下就消失。
const SPLASH_MIN_VISIBLE_MS = 500

// 淡出 index.html 内联的开屏加载页（连同其样式）。幂等：重复调用无副作用。
function dismissSplash() {
  const splash = document.querySelector<HTMLElement>('#splash')
  if (!splash || splash.classList.contains('splash-leave')) return
  const delay = Math.max(0, SPLASH_MIN_VISIBLE_MS - performance.now())
  window.setTimeout(() => {
    splash.classList.add('splash-leave')
    window.setTimeout(() => {
      splash.remove()
      document.querySelector('#splash-style')?.remove()
    }, 400)
  }, delay)
}

if (!rootElement.innerHTML) {
  const root = ReactDOM.createRoot(rootElement)
  // Wait for the active language bundle (loaded on demand, see i18n/config) so
  // the first render is fully translated. If its chunk fails, start anyway;
  // the loader evicts failed requests so later language changes can retry.
  void startApplicationAfterI18n(i18nReady, () => {
    root.render(
      <StrictMode>
        <QueryClientProvider client={queryClient}>
          <ThemeProvider>
            <DirectionProvider>
              <RouterProvider router={router} />
            </DirectionProvider>
          </ThemeProvider>
        </QueryClientProvider>
      </StrictMode>
    )
    // 等首个路由完全解析（懒加载 chunk、beforeLoad/loader 都完成、重定向链
    // 走到终点）后再撤开屏——root.render 只是挂载了外壳，此时页面内容往往
    // 还没渲染，过早撤屏会露白。双 rAF 确保浏览器已把解析后的内容绘制出来。
    const unsubscribe = router.subscribe('onResolved', () => {
      unsubscribe()
      requestAnimationFrame(() => requestAnimationFrame(dismissSplash))
    })
    // 兜底：初始导航异常（路由报错等）时 onResolved 不会触发，10s 强制撤屏，
    // 让 errorComponent 可见（index.html 里另有独立于主包的 15s 最终兜底）。
    window.setTimeout(dismissSplash, 10_000)
  })
}
