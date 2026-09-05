# 行星底部光圈贴近修复 · 设计文档

- 日期：2026-09-05
- 状态：已确认（用户选定方案 A）
- 涉及模块：`web/default` 首页天体系统（Three.js）

## 问题描述

行星绕恒星星公转时，行星「底部光圈」（辉光 sprite）距行星过远，且明显偏左，
不贴合行星。

## 根因

`web/default/src/features/home/components/solar-system.tsx` 中行星跟随辉光：

```ts
const trail = new THREE.Sprite(/* additive 白色径向渐变 */)
trail.scale.set(def.size * 3.4, def.size * 1.5, 1)
trail.position.set(-def.size * 1.2, 0, 0)
trail.rotation.z = Math.PI / 2
```

1. `trail.rotation.z = Math.PI / 2` 为无效代码：three 0.185.1 中 Sprite 的面内旋转
   只读取 `material.rotation`，Object3D 的 `rotation` 对 billboard 不生效，故实际渲染
   为**横向椭圆**（宽 3.4s，高 1.5s）。
2. 行星 group 带 `rotation.y = -angle`，局部 -X 指向恒星方向，辉光中心落在
   `1.2 * size`（约 2.4 倍行星半径）的恒星侧，并向左再伸出约 `1.7 * size`，
   视觉即「底部偏左、离行星过远」。

## 方案（选定 A：底部居中光圈）

把辉光改为贴合行星正下方的对称横向柔光：

| 字段 | 原值 | 新值 |
|---|---|---|
| `trail.position` | `(-size*1.2, 0, 0)` | `(0, -size*0.95, 0)` |
| `trail.scale` | `(size*3.4, size*1.5, 1)` | `(size*2.0, size*1.0, 1)` |
| `trail.rotation.z` | `Math.PI / 2` | 删除（无效代码） |

- 辉光中心位于行星正下方 0.95s；行星半径 0.5s，可视亮部顶端约 -0.6s，与行星下缘
  （-0.5s）保持约 0.1s 的贴合间隙。
- 横向宽度 2.0s（±1.0s），径向渐变可见亮部约 ±0.6~0.8s，略宽于行星，居中对称，
  消除「偏左」。
- 行星公转到轨道任意位置均保持贴合正下方（sprite 始终面向相机，局部 Y 近乎竖直向下）。

## 验证

- 重新 `npm run build`（web/default），重启 `go run .`（DEBUG/SESSION_SECRET/CRYPTO_SECRET），
- browser-agent 以 1920x1080 截图，确认各行星底部光圈贴合、居中、不再偏左。
