---
AIGC:
    Label: "1"
    ContentProducer: 001191440300708461136T1XGW3
    ProduceID: 2f7a9debecd5603c3de893343c2c0af8_592a8807aa6b11f190de525400461939
    ReservedCode1: lQz8kc71S8zBCW+TGynnOR6nY5txiv/JvVLyyI0F2RLfspRnP+qNp5z2L4Mw8OwhNgnrmMA6/0XXE2hXotE6k9aiO9lXSvYYI07TttSYiQRdbTlLNpbZO25rj2KiBs/9CNJFCTUWbVB+M8mhYIl2asGf5SdpW4TRnZMFq6sLSEVfXkDCtMZ8mahz0bQ=
    ContentPropagator: 001191440300708461136T1XGW3
    PropagateID: 2f7a9debecd5603c3de893343c2c0af8_592a8807aa6b11f190de525400461939
    ReservedCode2: lQz8kc71S8zBCW+TGynnOR6nY5txiv/JvVLyyI0F2RLfspRnP+qNp5z2L4Mw8OwhNgnrmMA6/0XXE2hXotE6k9aiO9lXSvYYI07TttSYiQRdbTlLNpbZO25rj2KiBs/9CNJFCTUWbVB+M8mhYIl2asGf5SdpW4TRnZMFq6sLSEVfXkDCtMZ8mahz0bQ=
---

# 侧边栏新增「代充」菜单与页面 — 设计文档

- 日期：2026-09-07
- 项目：E:\heqiuyu-api-main\heqiuyu-api-main（new-api 二开 LLM 网关）
- 状态：已获用户批准，进入实现

## 目标

在项目仪表盘界面的侧边栏新增「代充」菜单项，点击进入独立页面，展示代充 GPT 的服务说明与联系方式。两套前端（classic / default）均需实现，保持一致。

## 需求要点

1. 菜单文案：「代充」
2. 页面内容：可代充 GPT；如有需要请联系管理员；联系方式 `QQ：3756686882`
3. 形态：标准菜单项 + 独立页面（方案 A：纯前端双栈落地，零后端改动）
4. 路由：两套统一 `/recharge`，沿用现有页面守卫（登录可见）
5. 视觉：跟随各自现有主题（classic=semi-ui 星辰换肤变量；default=Tailwind 现有卡片风格），不引入新依赖
6. 验证：两套分别 `npm run build` 零错误

## 改动清单

### classic（web/classic，semi-ui + vite，.jsx）

- `src/components/layout/SiderBar.jsx`
  - `workspaceItems`（控制台区域）追加菜单项「代充」（`itemKey: 'recharge'`, `to: '/recharge'`），位于「使用日志」之后
  - `routerMap` 增加 `recharge: '/recharge'`
  - 注意 `isModuleVisible` 过滤逻辑：确保新菜单项默认可见（若为白名单式过滤，需同步默认可见配置或选择可见性明确的实现方式）
- `src/App.jsx`：注册 `/recharge` 路由到新页面组件
- 新增 `src/pages/Recharge/index.jsx`：semi-ui 卡片，标题「代充 GPT」+ 文案「如有需要请联系管理员」+ `QQ：3756686882`，接入现有布局与主题

### default（web/default，Tailwind v4 + rsbuild，.tsx）

- `src/hooks/use-sidebar-data.ts`：`navGroups` 的 General 分组 `items` 追加「代充」（`title: t('代充')`, `url: '/recharge'`, 复用现有 icon），位于 Dashboard 之下
- 新增 `src/routes/_authenticated/recharge/` 页面（Tailwind 卡片）：标题「代充 GPT」+ 文案「如有需要请联系管理员」+ `QQ：3756686882`，沿用现有路由与布局守卫

## 非目标

- 不改后端、不动鉴权逻辑、不改数据库
- 不做后台可开关配置（YAGNI）
- 不新增第三方依赖

## 验收

- classic / default 各自 `npm run build` 零错误
- 本地启动后侧边栏可见「代充」菜单，点击进入页面，文案与 QQ 正确展示
*（内容由AI生成，仅供参考）*
