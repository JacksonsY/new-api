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
  Activity,
  Box,
  Boxes,
  ClipboardCheck,
  Coins,
  CreditCard,
  FileText,
  FlaskConical,
  HandCoins,
  Handshake,
  HeartPulse,
  Key,
  LayoutDashboard,
  ListTodo,
  MessageSquare,
  Radio,
  ServerCog,
  Settings,
  Share2,
  ShieldAlert,
  Store,
  Ticket,
  Trophy,
  User,
  Users,
  Wallet,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'

import type { SidebarData } from '@/components/layout/types'
import { useStatus } from '@/hooks/use-status'
import { ROLE, hasRootAccess } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

/**
 * Root navigation groups for the application sidebar.
 *
 * These are shown when the URL does not match any nested sidebar view
 * registered in `layout/lib/sidebar-view-registry.ts`.
 */
export function useSidebarData(): SidebarData {
  const { t } = useTranslation()
  const user = useAuthStore((s) => s.auth.user)
  const { status } = useStatus()
  const isAgent = Boolean(user?.agent_type)
  const hasAgentCenterAccess = isAgent || Boolean(user?.agent_grace_access)
  const isAdmin = (user?.role ?? 0) >= ROLE.ADMIN
  const isRoot = hasRootAccess(user?.role)
  const isSupplier = user?.supplier_status === 2
  const isSubAccount = Boolean(user?.parent_id) // jzlh-sub 子号不显示管理台
  const subAccountEnabled = status?.sub_account_enabled === true
  // jzlh-sub 子号菜单按功能权限白名单门控（非子号恒放行）；普通子号无 wallet 权限则不显示钱包菜单。
  const subPerms = user?.sub_account?.permissions ?? {}
  const subCan = (p: string) => !isSubAccount || Boolean(subPerms[p])
  // v2 P2 招商模块开关:关闭时隐藏对应招商入口(存量身份的条目不受影响)。
  const agentEnabled = status?.agent_enabled !== false
  const supplierEnabled = status?.supplier_enabled !== false


  return {
    navGroups: [
      {
        id: 'chat',
        title: t('Chat'),
        // jzlh-sub 子号无 playground 权限则整组不显示对话入口
        items: subCan('playground')
          ? [
              {
                title: t('Playground'),
                url: '/playground',
                icon: FlaskConical,
              },
              {
                title: t('Chat'),
                icon: MessageSquare,
                type: 'chat-presets' as const,
              },
            ]
          : [],
      },
      {
        id: 'general',
        title: t('General'),
        items: [
          {
            title: t('Overview'),
            url: '/dashboard/overview',
            icon: Activity,
          },
          {
            title: t('Dashboard'),
            url: '/dashboard/models',
            icon: LayoutDashboard,
          },
          ...(subCan('api_keys')
            ? [
                {
                  title: t('API Keys'),
                  url: '/keys',
                  icon: Key,
                },
              ]
            : []),
          ...(subCan('usage_logs')
            ? [
                {
                  title: t('Usage Logs'),
                  url: '/usage-logs/common',
                  icon: FileText,
                },
                {
                  title: t('Task Logs'),
                  url: '/usage-logs/task',
                  activeUrls: ['/usage-logs/drawing'],
                  configUrls: ['/usage-logs/drawing', '/usage-logs/task'],
                  icon: ListTodo,
                },
              ]
            : []),
        ],
      },
      {
        id: 'personal',
        title: t('Personal'),
        items: [
          // jzlh-sub 普通子号(无 wallet 权限)不显示钱包菜单
          ...(subCan('wallet')
            ? [
                {
                  title: t('Wallet'),
                  url: '/wallet',
                  icon: Wallet,
                },
              ]
            : []),
          {
            title: t('Profile'),
            url: '/profile',
            icon: User,
          },
        ],
      },
      // jzlh-agent:自助项按 isAgent 门控、招商入口按开关,管理项按 isRoot。
      ...(hasAgentCenterAccess || isRoot || (!isAdmin && agentEnabled)
        ? [
            {
              id: 'agent',
              title: t('Agent Center'),
              items: [
                ...(!isAdmin && !hasAgentCenterAccess && agentEnabled
                  ? [
                      {
                        title: t('Become an Agent'),
                        url: '/agent/apply',
                        icon: Handshake,
                      },
                    ]
                  : []),
                ...(hasAgentCenterAccess
                  ? [
                      {
                        title: t('Agent Wallet'),
                        url: '/agent/wallet',
                        icon: Wallet,
                      },
                    ]
                  : []),
                ...(isAgent
                  ? [
                      {
                        title: t('My Users'),
                        url: '/agent/users',
                        icon: Share2,
                      },
                    ]
                  : []),
                ...(isRoot
                  ? [
                      {
                        title: t('Agent Management'),
                        url: '/agents',
                        icon: Users,
                      },
                      {
                        title: t('Agent Applications'),
                        url: '/agents/applications',
                        icon: ClipboardCheck,
                      },
                      {
                        title: t('Withdrawal Review'),
                        url: '/withdrawals',
                        icon: Wallet,
                      },
                      {
                        title: t('Risk Control'),
                        url: '/risk',
                        icon: ShieldAlert,
                      },
                    ]
                  : []),
              ],
            },
          ]
        : []),
      // jzlh-supplier:自助项给已过审供应商(含公开榜单),招商入口按开关,管理项 root。
      ...((!isAdmin && (isSupplier || supplierEnabled)) || isRoot
        ? [
            {
              id: 'supplier',
              title: t('Supplier Center'),
              items: [
                ...(!isAdmin && isSupplier
                  ? [
                      {
                        title: t('My Channels'),
                        url: '/supplier/channels',
                        icon: Boxes,
                      },
                      {
                        title: t('My Earnings'),
                        url: '/supplier/earnings',
                        icon: Coins,
                      },
                      {
                        title: t('Payout Settings'),
                        url: '/supplier/payout',
                        icon: Wallet,
                      },
                      {
                        title: t('Relay Leaderboard'),
                        url: '/detection/leaderboard',
                        icon: Trophy,
                      },
                    ]
                  : []),
                ...(!isAdmin && !isSupplier && supplierEnabled
                  ? [
                      {
                        title: t('Onboarding Application'),
                        url: '/supplier/apply',
                        icon: Store,
                      },
                    ]
                  : []),
                ...(isRoot
                  ? [
                      {
                        title: t('Supplier Management'),
                        url: '/suppliers',
                        icon: Store,
                      },
                      {
                        title: t('Channel Review'),
                        url: '/suppliers/review',
                        icon: ClipboardCheck,
                      },
                      {
                        title: t('Settlement'),
                        url: '/suppliers/settlement',
                        icon: HandCoins,
                      },
                    ]
                  : []),
              ],
            },
          ]
        : []),
      // 检测管理：仅管理端的中转站验真榜单(检测 QA,不属供应商——路由归 /detection)。
      ...(isAdmin
        ? [
            {
              id: 'detection',
              title: t('Detection Management'),
              items: [
                {
                  title: t('Relay Leaderboard'),
                  url: '/detection/leaderboard',
                  icon: Trophy,
                },
              ],
            },
          ]
        : []),
      // jzlh-sub 团队管理（子账号）：站点开关开启且非子号（主号/普通用户）可见。
      ...(subAccountEnabled && !isSubAccount
        ? [
            {
              id: 'sub-account',
              title: t('Team Management'),
              items: [
                {
                  title: t('Team Management'),
                  url: '/sub-account',
                  icon: Users,
                },
              ],
            },
          ]
        : []),
      {
        id: 'admin',
        title: t('Admin'),
        items: [
          {
            title: t('Channels'),
            url: '/channels',
            icon: Radio,
          },
          {
            title: t('Models'),
            url: '/models/metadata',
            icon: Box,
          },
          {
            title: t('Users'),
            url: '/users',
            icon: Users,
          },
          {
            title: t('Redemption Codes'),
            url: '/redemption-codes',
            icon: Ticket,
          },
          {
            title: t('Subscriptions'),
            url: '/subscriptions',
            icon: CreditCard,
          },
        ],
      },
      ...(isAdmin
        ? [
            {
              id: 'system',
              title: t('System'),
              items: [
                {
                  title: t('Ops Monitor'),
                  url: '/ops-monitor',
                  icon: HeartPulse,
                },
                {
                  title: t('System Info'),
                  url: '/system-info',
                  icon: ServerCog,
                  requiredRole: ROLE.SUPER_ADMIN,
                },
                {
                  title: t('System Settings'),
                  url: '/system-settings/site',
                  activeUrls: ['/system-settings'],
                  icon: Settings,
                },
              ],
            },
          ]
        : []),
    ],
  }
}
