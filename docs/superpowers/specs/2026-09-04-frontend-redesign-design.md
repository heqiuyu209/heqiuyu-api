# 前端视觉重构设计 —「星辰浅域」

- 日期：2026-09-04
- 项目：heqiuyu-api（new-api 二开 LLM 网关）
- 范围：`web/default`（React 18 + TanStack Router + Tailwind v4）与 `web/classic`（React + semi-ui + antd + Tailwind）
- 目标：全站清新科幻视觉、强记忆点、适当调整页面框架；**功能 / API / 路由零改动**

## 1 设计概念：Light Stellar Observatory（可居住的星际工作站）

星雾浅底承载高密度后台数据；太空靛蓝为轴、极光青做强调、修道紫收尾装饰；装饰克制（星尘点阵、细轨道弧、微辉光），保证数据密度优先。

### 1.1 拒绝清单（AI Slop Test）
- 禁 cyan-on-dark 深色霓虹主视觉
- 禁纯黑 #000 / 纯白 #fff
- 禁灰字配彩底
- 禁卡片套卡片
- 禁随处玻璃拟态
- 禁用过度圆角矩形 + 平庸投影
- 禁 Inter/Roboto 作标题

## 2 设计系统（Design Tokens）

> 全量化 oklch；亮/暗两套完整覆盖。

| Token | 亮色 | 暗色 |
|---|---|---|
| --background | oklch(0.975 0.004 255) | oklch(0.18 0.02 265) |
| --foreground | oklch(0.22 0.04 265) | oklch(0.92 0.008 255) |
| --card | oklch(0.985 0.003 255) | oklch(0.205 0.015 265) |
| --primary（太空靛蓝） | oklch(0.52 0.15 268) | oklch(0.72 0.13 255) |
| --primary-hover | oklch(0.46 0.16 268) | oklch(0.78 0.12 255) |
| --accent（极光青） | oklch(0.72 0.11 192) | oklch(0.80 0.11 192) |
| --accent-soft（修道紫） | oklch(0.62 0.13 305) | oklch(0.70 0.14 305) |
| --chart-1..5（深空系） | 靛蓝 0.60 0.14 268 / 极光青 0.72 0.11 192 / 紫罗兰 0.66 0.14 305 / 暖金 0.78 0.14 82 / 星粉 0.70 0.09 352 | 略提亮（同色相 +8~12 L） |
| --border | oklch(0.90 0.012 260) | oklch(0.31 0.015 265) |
| --ring | oklch(0.60 0.13 268) | oklch(0.62 0.13 255) |
| --radius | 0.75rem（基础） / 1.25rem（大容器） / 999px（胶囊） | 同左 |

**字体**
- 标题/品牌：Space Grotesk Variable（`@fontsource-variable/space-grotesk`，default 新增依赖；classic 走 Google Fonts 链接或同包）
- 正文：Public Sans Variable（default 现有 `@fontsource-variable/public-sans`；classic 现有字体栈并入）
- 等宽（代码/密钥/ID）：JetBrains Mono 或系统 mono，仅数据展示用，不得滥用

**间距** 4pt 基准：4/8/12/16/24/32/48/64；内容 max-width 1440px。
**阴影** 双层：`0 1px 2px rgb(0 0 0 / 0.04), 0 8px 24px rgb(0 0 0 / 0.06)`；关键态附加辉光 `0 0 0 1px var(--ring), 0 0 24px oklch(0.6 0.13 268 / 0.35)`。
**运动** 入场 `cubic-bezier(0.16,1,0.3,1)`（ease-out-expo），120-240ms；hover 上浮 2px（transform only）；`prefers-reduced-motion` 全部禁用。

## 3 模块化改造点

### 模块 A：default 设计基座
- `src/styles/theme.css`：全量重写亮/暗 tokens、`@theme inline` 字体/圆角/阴影映射
- `src/styles/index.css`：全局底层（点阵背景、滚动条、选中、焦点环、按钮/输入字体继承）

### 模块 B：default 布局外壳（`src/components/layout/`）
- `app-sidebar.tsx`：选中态「辉光底 + 靛蓝标记条」，44px 触达，分组标题字距大写字标
- `app-header.tsx` / `header-logo.tsx` / `glow.tsx` / `main.tsx`：星舰仪表带视觉，品牌字标 Space Grotesk
- `public-layout.tsx` / `public-header.tsx` / `footer.tsx`：着陆站壳

### 模块 C：default 重点页面
- `routes/index.tsx`（首页 hero + 轨道装饰 + 辉光 CTA）
- `routes/(auth)/sign-in.tsx`（天体仪表双栏）
- `routes/_authenticated/dashboard/*`（指标卡辉光上边缘 + 深空图表色）
- `features/errors/*`（失联星站插画式错误页）
- 通用组件微调：`status-badge.tsx`、`empty-state.tsx`、`loading-state.tsx`、`data-table/*`

### 模块 D：classic 套件
- 技术栈不变（semi-ui/antd + Tailwind v3），通过 CSS 变量覆盖 + 主题定制达成同一视觉
- `src/index.css`：全局 tokens（CSS 变量 + OKLCH 回退 hex）、背景点阵、字体栈
- 覆盖 semi-ui `--semi-color-primary` 等变量；antd ConfigProvider + less/theme 覆盖主色
- 布局：`components/layout` 与侧边栏视觉同步（选中标记条、品牌字标）
- 重点页：Home、Dashboard、Login、(NotFound/Forbidden)

## 4 工程执行
1. 新增依赖：default `@fontsource-variable/space-grotesk`；classic 字体包（或本地 link）
2. 按模块 A→B→C→D 顺序落地
3. 每个套件 `npm run build` 验证通过
4. 启动后端（DEBUG=true / SESSION_SECRET）预览，浏览器逐页截图验收
5. git 提交：模块维度分批 commit（design-tokens / layout-shell / key-pages / classic），不 push、不 amend

## 5 验收标准
- 两套件构建零错误；dist 无旧品牌文本（heqiuyu 一致）
- 亮/暗双主题关键页截图通过「Squint Test」（模糊后层级清晰）
- 交互元素五态齐全（default/hover/active/focus/disabled）；触达 ≥44px
- 空/载/错状态均有设计；对比度 WCAG AA；reduced-motion 生效
- 功能零回归：鉴权、路由、数据表格、图表可用
