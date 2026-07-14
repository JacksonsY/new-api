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
  // jzlh-agent：代理分销菜单按独立维度 agent_type / role 自条件显示
  const user = useAuthStore((s) => s.auth.user)
  const isAgent = Boolean(user?.agent_type)
  const hasAgentCenterAccess = isAgent || Boolean(user?.agent_grace_access)
  const isAdmin = (user?.role ?? 0) >= ROLE.ADMIN
  const isRoot = hasRootAccess(user?.role)
  // 供应商中心：supplier_status 2 = 已通过（是与 role 正交的独立维度）。
  const isSupplier = user?.supplier_status === 2

  return {
    navGroups: [
      {
        id: 'chat',
        title: t('Chat'),
        items: [
          {
            title: t('Playground'),
            url: '/playground',
            icon: FlaskConical,
          },
          {
            title: t('Chat'),
            icon: MessageSquare,
            type: 'chat-presets',
          },
        ],
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
          {
            title: t('API Keys'),
            url: '/keys',
            icon: Key,
          },
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
        ],
      },
      {
        id: 'personal',
        title: t('Personal'),
        items: [
          {
            title: t('Wallet'),
            url: '/wallet',
            icon: Wallet,
          },
          {
            title: t('Profile'),
            url: '/profile',
            icon: User,
          },
        ],
      },
      // 代理中心：与供应商中心对称——自助项按 isAgent 门控，管理项按 isRoot 门控，
      // 组整体在「是代理 或 是 root」时显示（root 看得到独立的代理中心菜单）。
      ...(hasAgentCenterAccess || isRoot
        ? [
            {
              id: 'agent',
              title: t('Agent Center'),
              items: [
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
      {
        id: 'supplier',
        title: t('Supplier Center'),
        items: [
          // 供应商自助项：管理员不是供应商（裁判/运动员），入驻与自助一律不给管理员看。
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
              ]
            : []),
          ...(!isAdmin && !isSupplier
            ? [
                {
                  title: t('Onboarding Application'),
                  url: '/supplier/apply',
                  icon: Store,
                },
              ]
            : []),
          ...(isAdmin
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
      // 检测管理：仅管理端的中转站验真榜单。模型检测走公共 /detector 页，不进侧栏。
      ...(isAdmin
        ? [
            {
              id: 'detection',
              title: t('Detection Management'),
              items: [
                {
                  title: t('Relay Leaderboard'),
                  url: '/suppliers/leaderboard',
                  icon: Trophy,
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
          // 代理管理/提现审核/风控 已移入「代理中心」组（与供应商中心对称）。
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
