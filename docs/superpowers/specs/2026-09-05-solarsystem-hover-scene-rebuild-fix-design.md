# 行星悬停场景重建修复 · 设计文档

- 日期：2026-09-05
- 状态：已确认（用户选定方案 A）
- 涉及模块：`web/default` 首页天体系统（Three.js）

## 问题描述

鼠标悬停运行中的行星时：目标行星不停下来，且所有行星表现为快速移动/旋转。

## 根因

`solar-system.tsx` 中 `SolarCanvas` 的 `useEffect(..., [onHover])` 依赖了不稳定回调：

- 父组件 `SolarSystem` 每次渲染都新建 `handleHover = (name) => setHovered(name)`，
  其引用每次都变；
- hover 命中行星 → `onHover(name)` → `setHovered(name)` → 父组件重渲染 →
  `handleHover` 引用变化 → `useEffect` 依赖变化 → **整个 Three.js 场景
  （renderer / scene / 行星 / 星尘）被销毁重建**；
- 重建时每颗行星 `startAngle = Math.random()` 重新随机、星尘重新采样、
  hover 记录清零；重建后行星相位跳变可能导致鼠标不再命中 → hover 清除 →
  再触发重建，形成高频重建震荡。

视觉表现：所有行星相位不停跳变、星尘整体重采样 = "所有行星快速移动/旋转"；
目标行星因重建无法稳定暂停。

## 方案（选定 A：稳定 onHover 引用）

| 改动 | 内容 |
|---|---|
| import | 从 react 引入 `useCallback` |
| `handleHover` | `const handleHover = useCallback((name) => setHovered(name), [])` |

效果：
- `[onHover]` 依赖引用恒定 → `useEffect` 仅在挂载时执行一次，场景只创建一次；
- hover 仅更新名称浮层 + 暂停对应行星，不再触发重建；
- 消除行星相位重置、星尘重采样与 hover 震荡，提升性能与内存稳定性；
- 不改任何视觉参数。

## 验证

- `npm run build`（web/default）→ 重启 `go run .`；
- browser-agent 实机验证：悬停一颗行星并保持，确认该行星停止公转、
  其余行星匀速正常转动、无相位跳变、无场景重建闪烁；
- 移动鼠标跨行星，确认无反复震动/重采样。
