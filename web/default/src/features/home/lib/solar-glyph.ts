import * as THREE from 'three'

/**
 * ─────────────────────────────────────────────────────────────
 * solar-glyph v3 — 恒星中央「太阳黑子 H」字标
 *
 * 视觉方向：H 不发光，而是恒星表面的一处太阳黑子 —— 本影（umbra）
 * 是一块深蓝黑的暗纹，被一圈柔和冷色的半影（penumbra）包住。
 *   · 字形取自品牌字体轮廓（Space Grotesk Variable 700），不再自绘
 *     几何多边形，字身因此有真实的字体骨架与笔画对比；
 *   · 本影是深蓝黑而**不是纯黑**，整体 alpha 0.9 让等离子微微透出，
 *     读作「温度低一点的表面」而不是贴上去的黑色贴纸；
 *   · 半影是字身外缘一圈冷色柔光（替代原 v2 的近黑接触阴影），
 *     口沿还有一线恒星光斜掠进来的唇口透光（destination-out）；
 *   · 内壁一圈冷白光带（wallLight）四档递减 + 方向性内阴影，读出
 *     刻蚀纵深；
 *   · 横梁不再是一道暖金亮条，只留字形水平中线上一条极淡的冷色
 *     「光桥」（light bridge，真实太阳黑子中横跨本影的亮桥），
 *     且只铺在两侧竖画之间的中央区域，不扫过笔画；
 *   · 字身外缘一圈极细冷色勾线（rim），靠近光桥处最热，不铺光晕。
 *
 * v3 相比 v2 的取舍：彻底移除暖金（wall/beam/junction）与 `lighter`
 * 加色热斑，把「唯一亮部」的能量降一个量级，使 H 与恒星辉光融为一
 * 体（内敛），只在近处才被读成字母。
 *
 * 绘制实现：Canvas 2D 的 fillText/strokeText + globalCompositeOperation
 * （字体字形无法取到 Path2D，故用 source-atop 代替 clip、用
 * destination-out 做口沿透光）。输出 CanvasTexture 供 THREE.Sprite 使
 * 用，保留与行星的真实凌星遮挡；同族视觉的 HTML/CSS 版本见
 * styles/index.css 的 .solar-glyph-h（同一字体 + 同一比例）。
 * ─────────────────────────────────────────────────────────────
 */

export interface GlyphColors {
  /** 本影主体：字身的深蓝黑（非纯黑） */
  umbra: string
  /** 本影最深档：纵向渐变中段、画芯刻痕与方向性内阴影 */
  umbraDeep: string
  /** 本影两端抬起（贴近外层等离子处略微提亮） */
  umbraLift: string
  /** 半影外沿：绕字身一圈的柔和冷光 */
  penumbra: string
  /** 唇口受光：恒星光斜掠进刻蚀口的冷白一线 */
  lipLight: string
  /** 内壁冷光带 */
  wallLight: string
  /** 极细外勾线 */
  rim: string
  /** 勾线最热点（贴近水平中线处） */
  rimHot: string
  /** 光桥（横梁）微光 */
  bridge: string
}

/** 画布宽 / 画布高（含四周留白） */
export const GLYPH_ASPECT = 1
/** 纹理画布基准边长 */
const GLYPH_CANVAS_SIZE = 512
/** 字标字体族（与 --font-display 保持一致） */
export const GLYPH_FONT_STACK =
  "'Space Grotesk Variable', 'Public Sans', -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, sans-serif"
export const GLYPH_FONT_WEIGHT = 700
/** 用于量测与 document.fonts.load 的探针字号 */
const PROBE_FONT_PX = 100
/** 字形 ink box 高度 / 画布高 */
const INK_H_RATIO = 0.9
/** 字形 ink box 宽度上限 / 画布宽（防回退字体过宽溢出） */
const INK_W_LIMIT = 0.96
/** 本影主体不透明度（留一线透光，让等离子微微透出） */
const UMBRA_ALPHA = 0.92
/**
 * 半影：黑子外围一圈比本影亮、比光球暗的柔性亮环，位于字身**外侧**。
 * 两级（宽而淡 → 窄而实），alpha 刻意压低——半影是"暗纹外沿的过渡"，
 * 不是描边，写高了整字会变成霓虹管。
 */
const PENUMBRA_WIDE = 0.055
const PENUMBRA_WIDE_ALPHA = 0.09
const PENUMBRA_TIGHT = 0.03
const PENUMBRA_TIGHT_ALPHA = 0.14
/** 唇口透光带宽度 / 字高（让恒星的光斜掠进刻蚀口） */
const LIP_WIDE = 0.022
const LIP_WIDE_ALPHA = 0.26
const LIP_TIGHT = 0.011
const LIP_TIGHT_ALPHA = 0.22
/**
 * 内壁冷光：只保留贴着口沿的一线余韵（两级），不再是四级的宽光带——
 * 后者会让字身读成"双层描边框"，与"内敛"的方向相反。
 */
const WALL_W = [0.03, 0.013]
const WALL_ALPHA = [0.1, 0.16]
/** 画芯刻痕：沿字形中心纵向暗带（宽 / ink 宽，峰值 alpha） */
const CORE_W_RATIO = 0.4
const CORE_ALPHA = 0.45
/** 方向性内阴影偏移量 / 字高（刻蚀纵深，压到最低限度） */
const BEVEL_OFFSET = 0.014
const BEVEL_ALPHA = 0.4
/** 光桥：高度 / 字高、峰值 alpha、两端渐隐比例、横向占 ink 宽的比例 */
const BRIDGE_H = 0.045
const BRIDGE_ALPHA = 0.14
const BRIDGE_CORE_ALPHA = 0.18
const BRIDGE_FADE = 0.35
const BRIDGE_SPAN = 0.5
/** 冷色勾线线宽 / 字高与透明度（上/中/下三段） */
const RIM_LW = 0.01
const RIM_ALPHA = 0.24
const RIM_HOT_ALPHA = 0.34

/** 把任意 css 颜色（#rgb / #rrggbb / rgb() / rgba()）改写为指定 alpha */
function withAlpha(color: string, alpha: number): string {
  const c = color.trim()
  const rgba = c.match(
    /^rgba?\(\s*([\d.]+)[,\s]+([\d.]+)[,\s]+([\d.]+)(?:[,\s/]+[\d.]+)?\s*\)$/i
  )
  if (rgba) return `rgba(${rgba[1]},${rgba[2]},${rgba[3]},${alpha})`
  const hex = c.match(/^#([0-9a-f]{3}|[0-9a-f]{6})$/i)
  if (hex) {
    let hx = hex[1]
    if (hx.length === 3) {
      hx = hx
        .split('')
        .map((ch) => ch + ch)
        .join('')
    }
    const n = parseInt(hx, 16)
    return `rgba(${(n >> 16) & 255},${(n >> 8) & 255},${n & 255},${alpha})`
  }
  return c
}

/** 字体简写（供 ctx.font / document.fonts.load 使用） */
function fontShorthand(px: number): string {
  return `${GLYPH_FONT_WEIGHT} ${px}px ${GLYPH_FONT_STACK}`
}

interface GlyphMetrics {
  /** 实际绘制字号（CSS px） */
  fontPx: number
  /** 字形 ink box 相对基线的上/下沿 */
  ascent: number
  descent: number
  /** 基线 y（使 ink box 在画布内垂直居中） */
  baselineY: number
  /** ink box 尺寸 */
  inkW: number
  inkH: number
}

/**
 * 以探针字号量测 H 的 ink box，换算成填满画布高度 90% 的实际字号。
 * 使用 actualBoundingBox* 而非 em 框，因此对不同字体 / 回退字体都成立。
 */
function measureGlyph(
  ctx: CanvasRenderingContext2D,
  w: number,
  h: number
): GlyphMetrics {
  ctx.font = fontShorthand(PROBE_FONT_PX)
  const m = ctx.measureText('H')
  const ascent100 = m.actualBoundingBoxAscent || PROBE_FONT_PX * 0.72
  const descent100 = m.actualBoundingBoxDescent || 0
  const inkW100 = Math.max(
    1,
    (m.actualBoundingBoxLeft || 0) + (m.actualBoundingBoxRight || 0)
  )
  const capH100 = Math.max(1, ascent100 + descent100)

  let fontPx = (INK_H_RATIO * h * PROBE_FONT_PX) / capH100
  const inkW0 = (inkW100 * fontPx) / PROBE_FONT_PX
  if (inkW0 > INK_W_LIMIT * w) {
    fontPx *= (INK_W_LIMIT * w) / inkW0
  }

  const scale = fontPx / PROBE_FONT_PX
  const ascent = ascent100 * scale
  const descent = descent100 * scale
  return {
    fontPx,
    ascent,
    descent,
    baselineY: h / 2 + (ascent - descent) / 2,
    inkW: inkW100 * scale,
    inkH: Math.max(1, (capH100 * scale) as number),
  }
}

/**
 * 在 ctx 上绘制「太阳黑子 H」。坐标基准：字高 = h，画布宽 = w。
 * 调用方负责 dpr 缩放，本函数内部全部使用 CSS 尺寸。
 */
export function drawSolarGlyphH(
  ctx: CanvasRenderingContext2D,
  w: number,
  h: number,
  colors: GlyphColors
): void {
  const { fontPx, ascent, descent, baselineY, inkW } = measureGlyph(ctx, w, h)
  const cx = w / 2
  const inkTop = baselineY - ascent
  const inkBottom = baselineY + descent
  const inkH = Math.max(1, inkBottom - inkTop)
  const midY = (inkTop + inkBottom) / 2

  ctx.save()
  ctx.font = fontShorthand(fontPx)
  ctx.textAlign = 'center'
  ctx.textBaseline = 'alphabetic'
  ctx.lineJoin = 'round'

  const stroke = () => ctx.strokeText('H', cx, baselineY)
  const fill = () => ctx.fillText('H', cx, baselineY)

  // 0) 半影：黑子外围比本影亮、比光球暗的柔性亮环（两级，宽而淡 → 窄而实）
  ctx.lineWidth = PENUMBRA_WIDE * h
  ctx.strokeStyle = withAlpha(colors.penumbra, PENUMBRA_WIDE_ALPHA)
  stroke()
  ctx.lineWidth = PENUMBRA_TIGHT * h
  ctx.strokeStyle = withAlpha(colors.penumbra, PENUMBRA_TIGHT_ALPHA)
  stroke()

  // 1) 本影实底：纵向三段渐变的深蓝黑
  const body = ctx.createLinearGradient(0, inkTop, 0, inkBottom)
  body.addColorStop(0, withAlpha(colors.umbraLift, UMBRA_ALPHA))
  body.addColorStop(0.5, withAlpha(colors.umbraDeep, UMBRA_ALPHA))
  body.addColorStop(1, withAlpha(colors.umbraLift, UMBRA_ALPHA))
  ctx.fillStyle = body
  fill()

  // 2) 唇口透光：贴着轮廓的一线不死黑，让恒星的光斜掠进裂隙
  ctx.save()
  ctx.globalCompositeOperation = 'destination-out'
  ctx.lineWidth = LIP_WIDE * h
  ctx.strokeStyle = withAlpha(colors.lipLight, LIP_WIDE_ALPHA)
  stroke()
  ctx.lineWidth = LIP_TIGHT * h
  ctx.strokeStyle = withAlpha(colors.lipLight, LIP_TIGHT_ALPHA)
  stroke()
  ctx.restore()

  // 以下效果一律约束在字身内部（字体字形无法 clip，改用 source-atop）
  ctx.save()
  ctx.globalCompositeOperation = 'source-atop'

  // 3) 画芯：沿字形中心再深一档的纵向刻痕
  const coreW = inkW * CORE_W_RATIO
  const core = ctx.createLinearGradient(cx - coreW / 2, 0, cx + coreW / 2, 0)
  core.addColorStop(0, withAlpha(colors.umbraDeep, 0))
  core.addColorStop(0.5, withAlpha(colors.umbraDeep, CORE_ALPHA))
  core.addColorStop(1, withAlpha(colors.umbraDeep, 0))
  ctx.fillStyle = core
  ctx.fillRect(cx - coreW / 2, inkTop, coreW, inkH)

  // 4) 内壁冷光：只留贴着口沿的一线余韵（越靠内越淡）
  for (let i = 0; i < WALL_W.length; i++) {
    ctx.lineWidth = WALL_W[i] * h
    ctx.strokeStyle = withAlpha(colors.wallLight, WALL_ALPHA[i])
    stroke()
  }

  // 5) 方向性内阴影：上/左内壁见光，下/右内壁沉入更深的黑
  ctx.save()
  ctx.translate(BEVEL_OFFSET * h, BEVEL_OFFSET * h)
  ctx.lineWidth = WALL_W[1] * h * 0.94
  ctx.strokeStyle = withAlpha(colors.umbraDeep, BEVEL_ALPHA)
  stroke()
  ctx.restore()

  // 6) 光桥：横梁上一条极淡的冷色亮桥，只铺在两侧竖画之间的中央区域
  const spanHalf = (inkW * BRIDGE_SPAN) / 2
  const bridgeGrad = ctx.createLinearGradient(
    cx - spanHalf,
    0,
    cx + spanHalf,
    0
  )
  bridgeGrad.addColorStop(0, withAlpha(colors.bridge, 0))
  bridgeGrad.addColorStop(BRIDGE_FADE, withAlpha(colors.bridge, BRIDGE_ALPHA))
  bridgeGrad.addColorStop(
    1 - BRIDGE_FADE,
    withAlpha(colors.bridge, BRIDGE_ALPHA)
  )
  bridgeGrad.addColorStop(1, withAlpha(colors.bridge, 0))
  ctx.fillStyle = bridgeGrad
  ctx.fillRect(
    cx - spanHalf,
    midY - (BRIDGE_H * h) / 2,
    spanHalf * 2,
    BRIDGE_H * h
  )

  // 光桥核心高光：一条极细的冷白线，守住「唯一亮部」的锐度但量级很低
  ctx.strokeStyle = withAlpha(colors.bridge, BRIDGE_CORE_ALPHA)
  ctx.lineWidth = Math.max(1, h * 0.003)
  ctx.lineCap = 'round'
  ctx.beginPath()
  ctx.moveTo(cx - spanHalf * (1 - BRIDGE_FADE), midY)
  ctx.lineTo(cx + spanHalf * (1 - BRIDGE_FADE), midY)
  ctx.stroke()

  ctx.restore()

  // 7) 极细冷色勾线：只沿真实外缘走一圈，靠近光桥段最热，不铺光晕
  const rimGrad = ctx.createLinearGradient(0, inkTop, 0, inkBottom)
  rimGrad.addColorStop(0, withAlpha(colors.rim, RIM_ALPHA))
  rimGrad.addColorStop(0.5, withAlpha(colors.rimHot, RIM_HOT_ALPHA))
  rimGrad.addColorStop(1, withAlpha(colors.rim, RIM_ALPHA))
  ctx.lineWidth = Math.max(1, RIM_LW * h)
  ctx.strokeStyle = rimGrad
  ctx.shadowColor = withAlpha(colors.rimHot, 0.16)
  ctx.shadowBlur = h * 0.006
  stroke()

  ctx.restore()
}

/** 纹理画布 / dpr 缩放：dpr ≥ 1.5 时用 1024（2 的幂），细线降采样不抖动 */
function makeGlyphCanvas(dpr: number): {
  canvas: HTMLCanvasElement
  ctx: CanvasRenderingContext2D
} {
  const h = GLYPH_CANVAS_SIZE
  const w = Math.round(h * GLYPH_ASPECT)
  const scale = dpr >= 1.5 ? 2 : 1
  const canvas = document.createElement('canvas')
  canvas.width = Math.max(8, w * scale)
  canvas.height = Math.max(8, h * scale)
  const ctx = canvas.getContext('2d')!
  ctx.scale(scale, scale)
  return { canvas, ctx }
}

function configureGlyphTexture(tex: THREE.CanvasTexture): void {
  tex.generateMipmaps = true
  tex.minFilter = THREE.LinearMipmapLinearFilter
  tex.magFilter = THREE.LinearFilter
  tex.anisotropy = 4
  tex.needsUpdate = true
}

/**
 * 生成字标纹理，并返回按目标世界高度推算的世界尺寸。
 * 首帧可能仍在使用回退字体（品牌字体尚未 load 完成），
 * 加载完成后调用 redrawSolarGlyphTexture() 重绘即可。
 */
export function makeSolarGlyphTexture(
  colors: GlyphColors,
  targetHeight: number,
  dpr: number
): { tex: THREE.CanvasTexture; worldW: number; worldH: number } {
  const { canvas, ctx } = makeGlyphCanvas(dpr)
  const w = Math.round(GLYPH_CANVAS_SIZE * GLYPH_ASPECT)
  const h = GLYPH_CANVAS_SIZE
  drawSolarGlyphH(ctx, w, h, colors)
  const tex = new THREE.CanvasTexture(canvas)
  configureGlyphTexture(tex)
  return {
    tex,
    worldW: (w / h) * targetHeight,
    worldH: targetHeight,
  }
}

/**
 * 品牌字体加载完成后的重绘：在同一张画布上重画字形，避免换贴图引起闪烁。
 */
export function redrawSolarGlyphTexture(
  tex: THREE.CanvasTexture,
  colors: GlyphColors
): void {
  const canvas = tex.image as HTMLCanvasElement | null
  if (!canvas || typeof canvas.getContext !== 'function') return
  const ctx = canvas.getContext('2d')
  if (!ctx) return
  const scale = canvas.width / (GLYPH_CANVAS_SIZE * GLYPH_ASPECT)
  ctx.setTransform(1, 0, 0, 1, 0, 0)
  ctx.clearRect(0, 0, canvas.width, canvas.height)
  ctx.setTransform(scale, 0, 0, scale, 0, 0)
  drawSolarGlyphH(ctx, canvas.width / scale, canvas.height / scale, colors)
  tex.needsUpdate = true
}

/**
 * 确保品牌字体已加载（可变字体，需显式指定字重）。
 * 失败时静默返回，调用方继续使用回退字体栈。
 */
export async function ensureGlyphFont(): Promise<void> {
  try {
    if (typeof document === 'undefined' || !('fonts' in document)) return
    await document.fonts.load(
      `${GLYPH_FONT_WEIGHT} ${PROBE_FONT_PX}px "Space Grotesk Variable"`
    )
  } catch {
    /* 品牌字体不可用：保持回退字体绘制结果 */
  }
}

/** 字标在世界坐标中的目标高度（单字符；最内圈轨道半径 2.05） */
export const GLYPH_WORLD_HEIGHT = 0.95

/** 呼吸脉光周期（秒） */
export const GLYPH_BREATH_PERIOD = 4.2
