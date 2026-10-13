/**
 * Layout 组件统一导出
 */

// 核心组件
export { AppHeader } from './components/app-header'
export { AppSidebar } from './components/app-sidebar'
export { AuthenticatedLayout } from './components/authenticated-layout'
export { PublicLayout } from './components/public-layout'
export { PublicHeader } from './components/public-header'
export { PublicNavigation } from './components/public-navigation'
export { HeaderLogo } from './components/header-logo'
export { NavLinkItem, NavLinkList } from './components/nav-link-item'
export { Header } from './components/header'
export { Main } from './components/main'
export { PageFooterPortal } from './components/page-footer'
export { NavGroup } from './components/nav-group'
export { SectionPageLayout } from './components/section-page-layout'
export { WorkspaceSwitcher } from './components/workspace-switcher'
export { TopNav } from './components/top-nav'
// MobileDrawer 已废弃：从未被任何页面引用，移动端导航由 PublicHeader 的内联 overlay 承担

// 上下文
export { WorkspaceProvider, useWorkspace } from './context/workspace-context'

// 配置
export {
  getSystemSettingsNavGroups,
  WORKSPACE_SYSTEM_SETTINGS_ID,
} from './config/system-settings.config'
export { defaultTopNavLinks } from './config/top-nav.config'

// 常量
// MOBILE_DRAWER_* 已随 MobileDrawer 一起废弃（清理脚本 cleanup-dead-code.mjs 会删除该组件与 constants.ts）

// 工具函数 - 工作区注册表
export {
  getWorkspaceByPath,
  getNavGroupsForPath,
  isInWorkspace,
  getAllWorkspaces,
  WORKSPACE_IDS,
} from './lib/workspace-registry'

// 类型导出（使用 type-only 导出避免与组件冲突）
export type {
  Workspace,
  NavLink,
  NavCollapsible,
  NavItem,
  NavGroup as NavGroupType,
  SidebarData,
  TopNavLink,
} from './types'
export type { WorkspaceConfig, WorkspaceId } from './lib/workspace-registry'
export type { SectionPageLayoutProps } from './components/section-page-layout'
