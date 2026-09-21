# 首页天体系统中央字标艺术字化（heqiuyu → H）设计说明

- 日期：2026-09-20
- 范围：`web/default` 首页 Hero 区「天体系统」中央恒星字标
- 目标：将原文字 `heqiuyu` 替换为单字母 `H`，并以「轨道同构」艺术字形式呈现，与恒星 / 轨道 / 深空视觉定位一致

## 1. 背景

`SolarSystem` 的中央字标走两条渲染链路：

| 档位 | 渲染方式 |
| --- | --- |
| `full` / `lite`（桌面） | `makeBrandTexture()` 将文字绘制为 Canvas 纹理，作为 `THREE.Sprite` 挂在恒星核心（z≈0，`renderOrder=10`），行星经过时靠 `depthTest` 产生真实凌星遮挡 |
| `static`（移动端 / `prefers-reduced-motion`） | HTML 层 `.brand-wordmark`（Space Grotesk） |

原文字宽高比约 4.0，替换为单字母 `H` 后宽高比约 0.72，字标可显著放大而让出轨道空间。

## 2. 字形结构（轨道同构）

以字高 `h` 为基准单位：

| 部位 | 天体隐喻 | 几何 |
| --- | --- | --- |
| 左竖 / 右竖 | 纵向极轨光柱 | 宽 `0.17h`，圆头，上下端椭圆封口 |
| 横梁 | 赤道光环侧视投影 | 高度位于 `0.52h`，椭圆 `rx = 字宽/2`、`ry = 0.075h` |
| 笔间弧光 | 三条轨道带的切向弧 | 两竖外侧各一道细弧，呼应 `ORBIT_TILT` |

约束：衔接处圆头圆角；笔画粗细差不超过 2:1，确保首眼可辨识为字母 `H`。

## 3. 材质（Canvas → Sprite）

绘制层次（由下至上）：

1. 宽域辉光：复用 `makeGlowTexture(palette.brandGlow)`，呼吸 `opacity` 0.45 ↔ 0.70
2. 字身垂直三段渐变
   - 深色主题：`#eaf2ff → #b9d2ff → #7fa8f0`
   - 浅色主题：`#34426f → #4a5f9c → #6379bd`
3. 内侧 `shadowBlur` 内发光 + 1.5px 渐变描边
4. 横梁白化「光刃」高光（alpha 0.70）
5. 字身内 3% 稀疏粒子亮点（与恒星粒子同族，消除贴纸感）

## 4. 尺度与动效

- 世界高 `0.58 → 1.05`；宽度按字形比例自适应（`worldW = (w/h) * targetHeight`）
- 呼吸脉光：`scale 1.00 ↔ 1.02`、`opacity 0.90 ↔ 1.00`，周期 4.2s，由既有 `loop()` 驱动
- 保持 `renderOrder=10`、`alphaTest=0.05` 与 `depthWrite=false`，凌星遮挡行为不变

## 5. 双渲染路径

- 3D 档：使用新 glyph 纹理
- 静态档：`index.css` 新增 `.solar-glyph-h`（渐变字 + 多层 `text-shadow` + 伪元素椭圆光环），为同族简化版
- 兼容：`label` 长度 > 1 时回退原文字渲染，不影响其他调用方

## 6. 文件级改动

| 文件 | 改动 |
| --- | --- |
| `web/default/src/features/home/lib/solar-glyph.ts` | 新增：字形绘制 `drawSolarGlyphH()` 与纹理生成 |
| `web/default/src/features/home/components/solar-system.tsx` | palette 增 glyph 色字段、接入新纹理、尺度与呼吸动效 |
| `web/default/src/styles/index.css` | 新增 `.solar-glyph-h` |
| `web/default/src/features/home/components/sections/hero.tsx` | `label='heqiuyu'` → `'H'`；`sr-only` h1 品牌名保留 |

## 7. 验收标准

1. `tsc` 零类型错误
2. `npm run build` 成功
3. Go 重新 embed 后 `localhost:3000` 可视验证：
   - 深浅两主题下 `H` 清晰可辨、与背景融洽
   - 行星经过时仍真实遮挡字标
   - `static` 档（模拟 reduced-motion / 移动端视口）正常降级显示

## 8. 非目标

- 不涉及品牌名 `heqiuyu` 在其他位置（logo、auth 页、h1 无障碍标题）的改动
- 不涉及供应商行星、轨道、星尘等既有渲染逻辑
