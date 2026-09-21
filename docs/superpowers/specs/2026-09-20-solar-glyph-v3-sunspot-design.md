# 首页天体系统中央字标 v3（太阳黑子 H）设计说明

- 日期：2026-09-20
- 范围：`web/default` 首页 Hero 区「天体系统」中央恒星字标
- 目标：把 v2 的「几何多边形负形刻蚀 H + 暖金横梁」重做为 **太阳黑子（umbra / penumbra）** 字形：保留"刻进恒星"的负形概念，字形改用品牌字体轮廓，配色全部转冷，整体强度内敛
- 前置问题修复：`--font-display` 字体名不匹配（详见 §5）

## 1. 背景与问题

v2 实现（`docs/superpowers/specs/2026-09-20-solar-hero-artword-design.md`）的实测观感问题：

1. 字形是 12 顶点圆角多边形自绘，不是字体轮廓 —— 笔画等宽、缺少字体骨架，读起来像"管子拼的 H"；
2. 字身接近纯黑（`rgb(2,3,8)`）+ 外圈近黑接触阴影，在明亮等离子上像一块**黑色贴纸**；
3. 唯一亮部是**暖金**横梁（`rgb(255,196,116)`）与暖金勾线，与冷蓝恒星色调冲突；
4. 细勾线（0.017h）与横梁（0.064h）在缩放后锐度不足，观感偏"糊"。

## 2. 设计方向：太阳黑子

真实太阳黑子由**本影**（umbra，最暗）与**半影**（penumbra，较亮、纤维状）构成，并常有横跨本影的**光桥**（light bridge）。v3 直接借用这套结构表达字母 H：

| 部位 | 天体隐喻 | 实现 |
| --- | --- | --- |
| 字身 | 本影 | 深蓝黑（非纯黑）纵向三段渐变，整体 alpha 0.92，让等离子微微透出 |
| 字身外缘 | 半影 | 两级冷色柔光描边（宽 0.055h / 0.03h，alpha 0.09 / 0.14） |
| 口沿 | 受光边缘 | `destination-out` 两级减淡（0.022h / 0.011h，alpha 0.26 / 0.22），让恒星光斜掠进刻蚀口 |
| 内壁 | 刻蚀口余韵 | 两级冷白光带（0.03h / 0.013h，alpha 0.10 / 0.16）——刻意不做宽光带，否则整字会读成霓虹描边框 |
| 下/右内壁 | 刻蚀纵深 | 方向性内阴影（偏移 0.014h，alpha 0.40） |
| 横梁 | 光桥 | 只铺在两侧竖画之间的中央 50% 区域，极淡冷白（高 0.045h，峰值 alpha 0.14 + 0.18 核心线） |
| 外缘勾线 | 半影边界 | 0.01h 冷色细线（alpha 0.24 / 靠近中线 0.34），几乎不铺光晕 |

**字形来源**：品牌字体 `Space Grotesk Variable`（700）。不再自绘多边形，字身因此获得真实笔画对比。画布仍为正方形（`GLYPH_ASPECT = 1`），用 `ctx.measureText('H')` 的 `actualBoundingBox*` 把 ink box 缩放到画布高度的 90% 并垂直居中（宽度超限时按宽度再缩一次，兼容回退字体）。

**移除项**：暖金 `wall / beam / beamEdge / beamCore / junction`、`lighter` 加色热斑、几何路径（`addRoundPolygon` / `addRoundRect`）与 `BAR_W / SIDE_MARGIN / CAP_MARGIN / BEAM_* / FILLET` 等几何常量。

## 3. 配色（全冷色）

| token | 深色主题 | 浅色主题 |
| --- | --- | --- |
| `umbra` | `rgb(10 14 30)` | `rgb(30 36 60)` |
| `umbraDeep` | `rgb(4 7 18)` | `rgb(14 18 36)` |
| `umbraLift` | `rgb(20 28 52)` | `rgb(46 54 84)` |
| `penumbra` | `rgb(130 175 255)` | `rgb(80 110 180)` |
| `lipLight` | `rgb(214 232 255)` | `rgb(236 242 255)` |
| `wallLight` | `rgb(150 190 255)` | `rgb(96 132 205)` |
| `rim` | `rgb(168 205 255)` | `rgb(86 118 190)` |
| `rimHot` | `rgb(226 240 255)` | `rgb(140 172 235)` |
| `bridge` | `rgb(226 240 255)` | `rgb(232 240 255)` |

字标后方 sprite（`SolarPalette.glyphHalo`）：深色 `rgba(130,175,255,0.55)`、浅色 `rgba(80,110,180,0.40)`。

## 4. 尺度与动效（内敛）

- 世界高 `GLYPH_WORLD_HEIGHT`：`1.12 → 0.95`（最内圈轨道半径 2.05）
- 本影纹理 alpha 0.90，sprite 材质 opacity 恒为 1（合计 ≈0.90，等离子微透）
- 半影 halo sprite：`opacity 0.26 → 0.14`，尺寸由 `2.3W × 1.9H → 2.0W × 1.7H`
- 呼吸：周期 4.2s 不变；字身 scale 幅度 `0.012 → 0.008`，halo opacity `0.14 ± 0.05`，字身不闪烁
- 凌星遮挡：仍为 `THREE.Sprite` + `alphaTest 0.02` + `depthWrite false`，绘制顺序不变

## 5. 字体缺陷修复（全局）

`styles/theme.css` 的 `--font-display` 原写 `'Space Grotesk'`，而 `@fontsource-variable/space-grotesk` 注册的家族名是 `'Space Grotesk Variable'`（构建产物 CSS 中只有后者有 `@font-face`），因此全站标题与 `.brand-wordmark` 一直静默回退到 `Public Sans`。

修复：`--font-display: 'Space Grotesk Variable', 'Space Grotesk', 'Public Sans', …`。影响面为全站 `h1–h6` 与品牌字标，字宽/换行可能有细微变化。

字体加载竞态：Canvas 绘制早于品牌字体就绪时会计量到回退字体。`solar-glyph.ts` 导出 `ensureGlyphFont()`（`document.fonts.load('700 100px "Space Grotesk Variable"')`，失败静默），`SolarCanvas` 在首帧用回退字形绘制，字体就绪后调用 `redrawSolarGlyphTexture()` 在同一张画布上重绘（`needsUpdate`），避免字标空白或字形跳变；卸载时通过 `glyphRedrawCancelled` 守卫并 `dispose()` 纹理。

## 5.1 附带修复：等离子粒子塌缩成中轴竖线

验证截图时发现恒星正中下方有一条固定的亮点竖线（约 1.18 世界单位长，正压在字标背后，容易被误认成 H 的一部分）。

根因在 `solar-system.tsx` 的 `loop()`：重算粒子纬度时写成 `u = clamp(Math.sin(seeds[i*2]) * 2 - 1)`，而 `sin(seed) < 0` 的概率约 50%，这部分粒子被夹到 `u = -1`、`s = 0`，位置坍缩为 `(0, -r, 0)`——一半粒子堆在中轴南极点，形成竖直点列。

修复：`aSeed` 从 2 分量扩为 4 分量 `[纬度 u, 环向相位, 径向扰动相位, 基准半径]`，初始化时把真实纬度 `u`、初始相位 `theta` 与半径 `r` 写入，`loop()` 直接复用；球面因此保持完整，径向扰动只表现为表面起伏，首帧与动画帧也不再跳变。

## 5.2 附带修复：静态档行星全叠在圆心、3D 档前景行星胀成白斑

**静态档（`StaticSolar`）行星不渲染**：行星定位写成
`transform: translate(calc(-50% + ${x}%), calc(-50% + ${y}%))`，而 CSS 里 `translate` 的百分比是按**元素自身**尺寸解析的——行星容器只有 `clamp(20px,3.2vw,30px)`，于是 10 颗行星全部被塞在圆心附近、彼此重叠、并被中央光晕盖住，看起来"没有行星"。
修复：改用相对**轨道容器**解析的 `left`/`top` 百分比居中：
`left: ${50 + x}%`、`top: ${50 + y}%`、`transform: translate(-50%,-50%)`。

**3D 档前景行星胀成实心白斑**：相机原为 `z=9 / fov 42°`，轨道最前方行星距相机仅 3.55、最后方 14.45（半径 5.45），前后视觉尺寸差约 4 倍；行星走到前景时图标被放大数倍，再叠上 `opacity 0.35`、`2.0×1.0` 的白色加色拖尾，就糊成一块白色实心块。
修复：
1. 相机改为"远机位 + 窄视角"——`z=15 / fov 26°`（`tan(fov/2)*z` 不变，因此构图与粒子尺寸完全不变），高度同步 `1.4 → 2.33` 以保持俯角；前后缩放差降到约 2.1 倍；
2. 拖尾降为 `opacity 0.16`、`1.55×0.7`、下移 `0.8×size`（hover 时 0.06），只作"受光"暗示，不再吞掉图标。

## 6. 双渲染路径

- 3D 档（`full` / `lite`）：`drawSolarGlyphH()` 绘制到 512（dpr≥1.5 为 1024）画布 → `THREE.CanvasTexture`。
  字体字形无法取得 `Path2D`，因此内部效果用 `globalCompositeOperation` 表达：`source-atop` 代替 `clip()`（画芯刻痕、内壁冷光、方向性内阴影、光桥都只在字身内部生效），`destination-out` 做口沿透光。
- 静态档（`static`）：`index.css` 的 `.solar-glyph-h` 改为**单个字体字形**（`background-clip: text` 的本影渐变 + `-webkit-text-stroke` 勾线 + `drop-shadow` 半影），删除原先拼装笔画的 `__stem` / `__bar` / 两个 `::after` 规则，与 3D 档天然同字体、同比例。
- 兼容：`label` 长度 > 1 时仍回退 `makeBrandTexture()` 原文字渲染（字体栈同步改为品牌字体）。

## 7. 文件级改动

| 文件 | 改动 |
| --- | --- |
| `web/default/src/features/home/lib/solar-glyph.ts` | 重写：字体轮廓测定、太阳黑子绘制层次、新 `GlyphColors`、`ensureGlyphFont()`、`redrawSolarGlyphTexture()` |
| `web/default/src/features/home/components/solar-system.tsx` | 两套 palette 的 `glyph` / `glyphHalo` 换新；字标纹理异步重绘与释放；呼吸与 halo 参数；`makeBrandTexture` 字体栈；static 档单 span；`aSeed` 扩为 4 分量修复粒子塌缩 |
| `web/default/src/styles/index.css` | 重写 `.solar-glyph-h`，删除 stem/bar 规则 |
| `web/default/src/styles/theme.css` | 修正 `--font-display` 家族名 |

## 8. 验收标准

1. `tsc` / `eslint` / `prettier` 在改动文件上零问题，`rsbuild build` 成功
2. 深浅两主题下 H 清晰可辨，读作恒星表面暗纹而非黑色贴纸
3. 字身及余光效不含暖色
4. 行星经过时仍真实遮挡字标
5. 静态档与 3D 档字形/比例一致
6. 修复后标题实际使用 Space Grotesk Variable（构建产物 CSS 中的 `--font-display` 包含该家族名）

## 9. 非目标

- 不改行星、轨道、星尘、相机、视差、hover 与凌星遮挡的实现机制
- 不改档位检测与移动端 / reduced-motion 降级策略
- 不改 hero 文案、i18n、布局，也不改 `.brand-wordmark` 自身的字号/字重（仅字体族因全局修复而变）
- 不引入新依赖、不新增 shader

*（内容由AI生成，仅供参考）*
