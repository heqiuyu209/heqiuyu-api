import { useMemo } from 'react'
import {
  LayoutDashboard,
  Key,
  FileText,
  Wallet,
  Box,
  Users,
  Ticket,
  User,
  Command,
  Radio,
  FlaskConical,
  MessageSquare,
  Coins,
  Settings,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useAuthStore } from '@/stores/auth-store'
import { ROLE } from '@/lib/roles'
import { WORKSPACE_IDS } from '@/components/layout/lib/workspace-registry'
import {
  type NavGroup,
  type SidebarData,
  type Workspace,
} from '@/components/layout/types'
import { getDashboardSectionNavItems } from '@/features/dashboard/section-registry'
import { getModelsSectionNavItems } from '@/features/models/section-registry'
import { getUsageLogsSectionNavItems } from '@/features/usage-logs/section-registry'

export function useSidebarData(): SidebarData {
  const { t } = useTranslation()
  const user = useAuthStore((s) => s.auth.user)
  const isAdmin = Boolean(user?.role && user.role >= 10)
  const isSuperAdmin = user?.role === ROLE.SUPER_ADMIN

  // 工作区切换器需要「系统设置」工作区存在才能切换；之前列表里只有
  // default，导致 canSwitchWorkspace 恒为 false、切换器永远不可用。
  // 用 useMemo 固定引用：否则每次渲染都是新数组，会让下游 memo 全部失效。
  const workspaces = useMemo<Workspace[]>(() => {
    const list: Workspace[] = [
      {
        id: WORKSPACE_IDS.DEFAULT,
        name: '', // Dynamically fetches system name
        logo: Command,
        plan: '', // Dynamically fetches system version
      },
    ]

    if (isSuperAdmin) {
      list.push({
        id: WORKSPACE_IDS.SYSTEM_SETTINGS,
        name: t('System Settings'),
        logo: Settings,
        plan: t('System Administration'),
      })
    }

    return list
  }, [isSuperAdmin, t])

  const navGroups = useMemo<NavGroup[]>(
    () => [
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
            title: t('Dashboard'),
            icon: LayoutDashboard,
            items: getDashboardSectionNavItems(t, { isAdmin }),
          },
          {
            title: t('代充'),
            url: '/recharge',
            icon: Coins,
          },
          {
            title: t('API Keys'),
            url: '/keys',
            icon: Key,
          },
          {
            title: t('Usage Logs'),
            icon: FileText,
            items: getUsageLogsSectionNavItems(t),
          },
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
            icon: Box,
            items: getModelsSectionNavItems(t),
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
        ],
      },
    ],
    [t, isAdmin]
  )

  return { workspaces, navGroups }
}
