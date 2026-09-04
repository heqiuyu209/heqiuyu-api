# classic 经典版首页天体系统公转 Bug 修复设计

日期：2026-09-04
状态：已批准（方案 A）
关联：2026-09-04-solarsystem-home-design.md

## 背景与问题

classic 经典版首页的纯 CSS 天体系统（恒星 heqiuyu + 国外供应商行星沿椭圆轨道公转）存在 Bug：
所有行星徽标全部缩在场地正中心原地旋转，没有沿星轨（椭圆轨道）公转。

## 根因

`.orbit-planet` 元素定义了 `width: 0; height: 0;`，动画 `solar-orbit` 使用
`transform: rotate(...) translateX(var(--r))`，其中 `--r` 是百分比值（如 16%）。

CSS 中 `translateX(百分比)` 的百分比是**相对元素自身宽度**计算的，而非父容器。
自身宽度为 0 → `translateX(16%)` = `16% × 0px = 0px`。

因此无论 `rotate` 怎么转，行星位移恒为 0，全部堆在中心，只有图标自身反向旋转。

对照：`.orbit-ring`（轨道环）用 `width: 百分比` 是相对父容器 `.orbit-belt` 的，
所以轨道环渲染正常；行星却是用 `translateX` 走自身百分比，踩了同一处坑。

## 方案

采用方案 A：CSS 变量记录视窗尺寸，行星半径用 `calc()` 换算成真实像素值。

### 具体改动（仅 web/classic/src/pages/Home/index.jsx）

1. `.solar-viewport` 增加 CSS 变量 `--belt: min(86vw, 620px);`
   （记录轨道场地实际宽度，与 viewport 尺寸一致）。

2. `PLANETS` 数据增加小数系数：`rf: p.r / 100`
   （如 `r: 16` → `rf: 0.16`），半径百分比换算为小数。

3. 行星渲染 style 改为：
   ```
   '--r': `calc(var(--belt) * ${p.rf})`,
   ```
   `translateX` 收到的 `--r` 即真实像素半径 → 行星沿星轨公转。

4. 保留 `.orbit-belt { transform: scaleY(0.62); }` 椭圆压扁效果，
   保留 `.p-badge` 反向 `solar-spin` 自转（使图标公转时保持正立）。

### 不改动的部分

- 轨道环、恒星、光晕、副标语、CTA 等其余结构不动。
- `prefers-reduced-motion` 降级逻辑不动。
- default 端（Three.js 实现）不受影响。

## 验证

- `web/classic` 执行 `npm run build` 构建通过。
- 服务启动后浏览器截图：行星徽标应分布在椭圆星轨上绕恒星 heqiuyu 公转，
  图标自身保持正立。
