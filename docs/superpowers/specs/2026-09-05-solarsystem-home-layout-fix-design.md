# heqiuyu 首页「天体系统」布局微调：名称浮层间距 + 底部光云回正（default 主题）

- 日期：2026-09-05
- 状态：已确认（用户拍板方案 A）
- 范围：web/default 首页 hero（Three.js 天体系统）两处纯 CSS/类名微调，不涉及三维坐标系、业务逻辑与路由

## 1. 背景与问题

default 主题 Three.js 新版首页 hero 区域，用户反馈两处观感问题：

1. **供应商名称主体与底部光圈距离相差过大**：悬停任一供应商行星徽标时，底部浮现的供应商名称浮层固定在容器 `bottom-[8%]`，与中央恒星辉光（蓝色柔光光晕）垂直间距约 237–250px（1920x1080 实测），视觉上"名称孤悬在底部、与光圈脱节"。
2. **底部光云过度偏左**：hero 背景径向光第三层柔光圆心定在水平 `40%`（`at 40% 80%`），随视口变宽越显偏左于天体系统中心，需回正居中。

实测补充：1920x1080 宽视口下中央恒星辉光圆心本身仅偏左约 5px（渲染底噪，可忽略），鼠标视差几乎不引起光云水平移动——因此"偏左"的真实来源是 hero 背景径向光第三层的 `at 40% 80%`。

## 2. 方案（用户已确认 · 方案 A：最小改动）

- ① 名称浮层上移贴近光晕：`bottom-[8%]` → `bottom-[16%]`，垂直间距由约 240px 缩至约 130px；
- ② 底部紫色光云回正居中：径向光圆心 `at 40% 80%` → `at 50% 80%`。

## 3. 修改点

### 3.1 `web/default/src/features/home/components/sections/hero.tsx`（背景径向光第三层）

现状：
```tsx
'radial-gradient(ellipse 40% 35% at 40% 80%, oklch(0.70 0.12 280 / 40%) 0%, transparent 70%)',
```
改为：
```tsx
'radial-gradient(ellipse 40% 35% at 50% 80%, oklch(0.70 0.12 280 / 40%) 0%, transparent 70%)',
```

### 3.2 `web/default/src/features/home/components/solar-system.tsx`（hover 名称浮层）

现状：
```tsx
<div className='pointer-events-none absolute bottom-[8%] left-1/2 z-30 -translate-x-1/2'>
```
改为：
```tsx
<div className='pointer-events-none absolute bottom-[16%] left-1/2 z-30 -translate-x-1/2'>
```

## 4. 验收标准

- 两处文件本机预览构建通过（`web/default`），页面 200、无 JS 报错。
- 1920x1080 与 1280x720 视口下：底部紫色柔光水平居中于天体系统中心；悬停行星时名称浮层贴近恒星光晕下缘（垂直间距约 130px），整体左右对称。
- 动效（行星公转、hover 放大、视差）不受影响；移动端 static 档不受影响。
