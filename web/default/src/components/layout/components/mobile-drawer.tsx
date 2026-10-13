/**
 * 已废弃（dead code）：本组件从未被任何页面引用过。
 *
 * 移动端导航实际由 `public-header.tsx` 里的内联全屏 overlay 承担
 * （见 `PublicHeader` 的 `mobile-nav-overlay` 节点）。
 *
 * 为什么这里只剩一段说明：本文件在当前环境下无法删除
 * （项目目录的删除操作被系统拒绝），所以先把 273 行死实现清掉，
 * 留下墓碑避免后续有人误用。
 *
 * barrel 导出已经清理完毕（`src/components/layout/index.ts` 里的
 * `MobileDrawer` 与 `MOBILE_DRAWER_*` 两处导出都已移除），
 * 剩下的收尾动作只有「删文件」，可执行仓库根目录的清理脚本：
 *   pwsh -NoProfile -File .\cleanup-dead-code.ps1 -Apply
 * 它会删除本文件与 `src/components/layout/constants.ts`。
 *
 * 原始实现可从 git 历史取回：
 *   git show HEAD:web/default/src/components/layout/components/mobile-drawer.tsx
 */
export {}
