import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type ComponentType,
  type ReactElement,
} from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import * as THREE from 'three'
import {
  OpenAI,
  Claude,
  Gemini,
  Meta,
  Mistral,
  Grok,
  Aws,
  AzureAI,
  Nvidia,
  Cohere,
} from '@lobehub/icons'

/**
 * ─────────────────────────────────────────────────────────────
 * SolarSystem — heqiuyu 首页「天体系统」
 * 中央恒星 = heqiuyu；10 家国外供应商徽标 = 行星，沿 3 条带倾角的
 * 椭圆轨道公转。桌面端 Three.js（WebGL 粒子）全量渲染；
 * 低配降级 lite（粒子减半）；移动端 / prefers-reduced-motion
 * 降级 static（纯 CSS 静态轨道 + 少量粒子）。
 * 纯表现层，不依赖业务数据与路由。
 * ─────────────────────────────────────────────────────────────
 */

// ---------- 供应商数据（仅国外 10 家，作为行星） ----------
type Tier = 'full' | 'lite' | 'static'

interface ProviderDef {
  id: string
  name: string
  Icon: ComponentType<Record<string, unknown>>
  orbit: 0 | 1 | 2
  radius: number
  speed: number
  size: number
}

const PROVIDERS: ProviderDef[] = [
  { id: 'openai', name: 'OpenAI', Icon: OpenAI, orbit: 0, radius: 2.05, speed: 1.12, size: 0.52 },
  { id: 'claude', name: 'Anthropic Claude', Icon: Claude, orbit: 0, radius: 2.45, speed: 0.96, size: 0.58 },
  { id: 'gemini', name: 'Google Gemini', Icon: Gemini, orbit: 0, radius: 2.85, speed: 0.82, size: 0.52 },
  { id: 'llama', name: 'Meta Llama', Icon: Meta, orbit: 0, radius: 3.25, speed: 0.71, size: 0.5 },
  { id: 'mistral', name: 'Mistral AI', Icon: Mistral, orbit: 1, radius: 3.7, speed: 0.63, size: 0.48 },
  { id: 'grok', name: 'xAI Grok', Icon: Grok, orbit: 1, radius: 4.05, speed: 0.57, size: 0.55 },
  { id: 'aws', name: 'Amazon Bedrock', Icon: Aws, orbit: 1, radius: 4.42, speed: 0.51, size: 0.58 },
  { id: 'azure', name: 'Azure OpenAI', Icon: AzureAI, orbit: 2, radius: 4.85, speed: 0.46, size: 0.5 },
  { id: 'nvidia', name: 'NVIDIA', Icon: Nvidia, orbit: 2, radius: 5.15, speed: 0.42, size: 0.56 },
  { id: 'cohere', name: 'Cohere', Icon: Cohere, orbit: 2, radius: 5.45, speed: 0.38, size: 0.5 },
]

const ORBIT_TILT: Record<0 | 1 | 2, number> = {
  0: -0.1,
  1: 0.02,
  2: 0.13,
}

// ---------- 档位检测 ----------
function detectTier(): Tier {
  if (typeof window === 'undefined') return 'static'
  const reduced = window.matchMedia('(prefers-reduced-motion: reduce)').matches
  const coarse = window.matchMedia('(pointer: coarse)').matches
  const small = window.innerWidth < 768
  if (reduced) return 'static'
  if (coarse && small) return 'static'
  // 低配桌面 / 触屏设备 → 减粒子
  const cores = navigator.hardwareConcurrency || 8
  if (coarse || cores < 4) return 'lite'
  return 'full'
}

// ---------- 纹理工具 ----------
function makeDotTexture(): THREE.Texture {
  const c = document.createElement('canvas')
  c.width = c.height = 64
  const ctx = c.getContext('2d')!
  const g = ctx.createRadialGradient(32, 32, 0, 32, 32, 32)
  g.addColorStop(0, 'rgba(255,255,255,1)')
  g.addColorStop(0.35, 'rgba(205,228,255,0.8)')
  g.addColorStop(1, 'rgba(160,200,255,0)')
  ctx.fillStyle = g
  ctx.beginPath()
  ctx.arc(32, 32, 32, 0, Math.PI * 2)
  ctx.fill()
  const tex = new THREE.CanvasTexture(c)
  tex.needsUpdate = true
  return tex
}

function makeGlowTexture(color: string): THREE.Texture {
  const c = document.createElement('canvas')
  c.width = c.height = 256
  const ctx = c.getContext('2d')!
  const g = ctx.createRadialGradient(128, 128, 0, 128, 128, 128)
  g.addColorStop(0, color)
  g.addColorStop(0.4, color.replace('1)', '0.35)'))
  g.addColorStop(1, color.replace('1)', '0)'))
  ctx.fillStyle = g
  ctx.fillRect(0, 0, 256, 256)
  const tex = new THREE.CanvasTexture(c)
  tex.needsUpdate = true
  return tex
}

function iconToTexture(Icon: ComponentType<Record<string, unknown>>): THREE.Texture | null {
  try {
    const svg = renderToStaticMarkup(
      <Icon width={128} height={128} color='#ffffff' /> as unknown as ReactElement
    )
    const blob = new Blob([svg], { type: 'image/svg+xml;charset=utf-8' })
    const url = URL.createObjectURL(blob)
    const img = new Image()
    const tex = new THREE.Texture()
    img.onload = () => {
      tex.image = img
      tex.needsUpdate = true
    }
    img.src = url
    return tex
  } catch {
    return null
  }
}

// ---------- Canvas 三维视图 ----------
function SolarCanvas({
  className,
  onHover,
}: {
  className?: string
  onHover: (name: string | null) => void
}) {
  const mountRef = useRef<HTMLDivElement | null>(null)
  const tierRef = useRef<Tier>('full')
  const hoveredRef = useRef<string | null>(null)

  useEffect(() => {
    const mount = mountRef.current
    if (!mount) return

    const tier = detectTier()
    tierRef.current = tier
    if (tier === 'static') return

    const particleScale = tier === 'lite' ? 0.5 : 1
    const reduced = window.matchMedia('(prefers-reduced-motion: reduce)').matches

    const scene = new THREE.Scene()
    const camera = new THREE.PerspectiveCamera(
      42,
      mount.clientWidth / Math.max(mount.clientHeight, 1),
      0.1,
      100
    )
    camera.position.set(0, 1.4, 9)
    camera.lookAt(0, 0, 0)

    const renderer = new THREE.WebGLRenderer({ alpha: true, antialias: true })
    renderer.setPixelRatio(Math.min(window.devicePixelRatio, 2))
    renderer.setSize(mount.clientWidth, mount.clientHeight)
    renderer.setClearColor(0x000000, 0)
    mount.appendChild(renderer.domElement)

    // ── 恒星粒子球（等离子表面：圆周谐波扰动） ──
    const CORE_COUNT = Math.floor(2600 * particleScale)
    const coreGeo = new THREE.BufferGeometry()
    const corePos = new Float32Array(CORE_COUNT * 3)
    const coreSeeds = new Float32Array(CORE_COUNT * 2)
    for (let i = 0; i < CORE_COUNT; i++) {
      const u = Math.random() * 2 - 1
      const theta = Math.random() * Math.PI * 2
      const r = Math.cbrt(Math.random()) * 1.18
      const s = Math.sqrt(1 - u * u)
      corePos[i * 3] = r * s * Math.cos(theta)
      corePos[i * 3 + 1] = r * u
      corePos[i * 3 + 2] = r * s * Math.sin(theta)
      coreSeeds[i * 2] = Math.random() * Math.PI * 2
      coreSeeds[i * 2 + 1] = Math.random() * Math.PI * 2
    }
    coreGeo.setAttribute('position', new THREE.BufferAttribute(corePos, 3))
    coreGeo.setAttribute('aSeed', new THREE.BufferAttribute(coreSeeds, 2))
    const coreMat = new THREE.PointsMaterial({
      size: 0.055,
      map: makeDotTexture(),
      transparent: true,
      depthWrite: false,
      blending: THREE.AdditiveBlending,
      color: new THREE.Color('#bcd6ff'),
    })
    const corePoints = new THREE.Points(coreGeo, coreMat)
    scene.add(corePoints)

    // 恒星核心辉光
    const coreGlow = new THREE.Sprite(
      new THREE.SpriteMaterial({
        map: makeGlowTexture('rgba(120,170,255,1)'),
        transparent: true,
        depthWrite: false,
        blending: THREE.AdditiveBlending,
        opacity: 0.85,
      })
    )
    coreGlow.scale.set(4.6, 4.6, 1)
    scene.add(coreGlow)

    // ── 轨道粒子环 + 行星 ──
    const orbitGroups: Record<0 | 1 | 2, THREE.Group> = { 0: new THREE.Group(), 1: new THREE.Group(), 2: new THREE.Group() }
    ;(Object.keys(orbitGroups) as unknown as (0 | 1 | 2)[]).forEach((k) => {
      orbitGroups[k].rotation.set(ORBIT_TILT[k], 0, 0)
      scene.add(orbitGroups[k])
    })

    // 轨道环粒子
    const dotTex = makeDotTexture()
    Object.keys(orbitGroups).forEach((k) => {
      const orbit = k as unknown as 0 | 1 | 2
      const radius = PROVIDERS.filter((p) => p.orbit === orbit).reduce(
        (m, p) => Math.max(m, p.radius),
        0
      )
      const r = radius + 0.55
      const seg = 220
      const geo = new THREE.BufferGeometry()
      const pts: number[] = []
      for (let i = 0; i < seg; i++) {
        const a = (i / seg) * Math.PI * 2
        pts.push(Math.cos(a) * r, 0, Math.sin(a) * r)
      }
      geo.setAttribute('position', new THREE.Float32BufferAttribute(pts, 3))
      const mat = new THREE.LineBasicMaterial({ color: 0x8fb3ff, transparent: true, opacity: 0.22 })
      const ring = new THREE.LineLoop(geo, mat)
      orbitGroups[orbit].add(ring)
    })

    // 行星 sprite + 尾迹
    const dot = makeGlowTexture('rgba(255,255,255,1)')
    const planetSprites: THREE.Sprite[] = []
    const planetData: { def: ProviderDef; angle: number; sprite: THREE.Sprite; group: THREE.Group }[] = []

    PROVIDERS.forEach((def) => {
      const tex = iconToTexture(def.Icon)
      const sprite = new THREE.Sprite(
        new THREE.SpriteMaterial({
          map: tex ?? dot,
          transparent: true,
          depthWrite: false,
          opacity: 1,
        })
      )
      sprite.scale.set(def.size, def.size, 1)

      // 底部光圈（贴合行星下缘的横向柔光）
      const trail = new THREE.Sprite(
        new THREE.SpriteMaterial({
          map: dot,
          transparent: true,
          depthWrite: false,
          blending: THREE.AdditiveBlending,
          opacity: 0.35,
        })
      )
      trail.scale.set(def.size * 2.0, def.size * 1.0, 1)
      trail.position.set(0, -def.size * 0.95, 0)

      const group = new THREE.Group()
      group.add(sprite)
      group.add(trail)

      const startAngle = Math.random() * Math.PI * 2
      planetSprites.push(sprite)
      planetData.push({ def, angle: startAngle, sprite, group })
      orbitGroups[def.orbit].add(group)

      group.position.set(
        Math.cos(startAngle) * def.radius,
        0,
        Math.sin(startAngle) * def.radius
      )
      sprite.userData.name = def.name
    })

    // ── 背景星尘 ──
    const DUST_COUNT = Math.floor(1500 * particleScale)
    const dustGeo = new THREE.BufferGeometry()
    const dustPos = new Float32Array(DUST_COUNT * 3)
    for (let i = 0; i < DUST_COUNT; i++) {
      const r = 9 + Math.random() * 6
      const theta = Math.random() * Math.PI * 2
      const phi = Math.acos(2 * Math.random() - 1)
      dustPos[i * 3] = r * Math.sin(phi) * Math.cos(theta)
      dustPos[i * 3 + 1] = r * Math.cos(phi) * 0.8
      dustPos[i * 3 + 2] = r * Math.sin(phi) * Math.sin(theta)
    }
    dustGeo.setAttribute('position', new THREE.BufferAttribute(dustPos, 3))
    const dustMat = new THREE.PointsMaterial({
      size: 0.045,
      map: dotTex,
      transparent: true,
      opacity: 0.5,
      depthWrite: false,
      blending: THREE.AdditiveBlending,
      color: new THREE.Color('#aac4ff'),
    })
    const dust = new THREE.Points(dustGeo, dustMat)
    scene.add(dust)

    // ── 交互 ──
    const raycaster = new THREE.Raycaster()
    const pointer = new THREE.Vector2(0, 0)
    const mouse = new THREE.Vector3(0, 0, 0)
    let hoveredName: string | null = null

    const onPointerMove = (e: PointerEvent) => {
      const rect = renderer.domElement.getBoundingClientRect()
      pointer.x = ((e.clientX - rect.left) / rect.width) * 2 - 1
      pointer.y = -((e.clientY - rect.top) / rect.height) * 2 + 1
      mouse.x = (e.clientX - rect.left) / rect.width - 0.5
      mouse.y = -(e.clientY - rect.top) / rect.height + 0.5
    }

    const onPointerLeave = () => {
      pointer.set(0, 0)
      mouse.set(0, 0, 0)
      if (hoveredRef.current) {
        hoveredRef.current = null
        onHover(null)
      }
    }

    renderer.domElement.addEventListener('pointermove', onPointerMove)
    renderer.domElement.addEventListener('pointerleave', onPointerLeave)

    // hover 检测（仅桌面鼠标）
    const hasHover = window.matchMedia('(hover: hover)').matches

    // ── 播放控制 ──
    let raf = 0
    let running = true
    const clock = new THREE.Clock()

    const loop = () => {
      if (!running) return
      raf = requestAnimationFrame(loop)
      const dt = Math.min(clock.getDelta(), 0.05)
      const t = clock.elapsedTime

      // 恒星粒子表面扰动
      const posAttr = coreGeo.attributes.position as THREE.BufferAttribute
      const pos = posAttr.array as Float32Array
      const seeds = (coreGeo.attributes.aSeed as THREE.BufferAttribute).array as Float32Array
      for (let i = 0; i < CORE_COUNT; i++) {
        const baseR = 1.18 * Math.cbrt(((i * 9301 + 49297) % 1000) / 1000 + 1e-6)
        const u = Math.sin(seeds[i * 2]) * 2 - 1
        const s = Math.sqrt(1 - u * u)
        const a = seeds[i * 2 + 1] + t * 0.6
        const wob = 0.06 * Math.sin(seeds[i] + t * 1.4)
        const r = baseR + wob
        pos[i * 3] = r * s * Math.cos(a)
        pos[i * 3 + 1] = r * u
        pos[i * 3 + 2] = r * s * Math.sin(a)
      }
      posAttr.needsUpdate = true

      const coreRot = t * 0.05
      corePoints.rotation.y = coreRot
      coreGlow.material.rotation = coreRot

      // 行星公转
      hoveredName = hoveredRef.current
      planetData.forEach((pd) => {
        const speed = pd.def.speed
        const paused = hasHover && hoveredName === pd.def.name
        if (!paused) pd.angle += dt * speed * 0.55

        // hover 放大
        const targetScale = paused ? 1.45 : 1
        const cur = pd.sprite.scale.x / (pd.def.size * 1)
        const next = THREE.MathUtils.lerp(cur, targetScale, dt * 6)
        const base = pd.def.size
        pd.sprite.scale.set(base * next, base * next, 1)
        // 尾迹随速度淡（受光）
        ;(pd.group.children[1] as THREE.Sprite).material.opacity = paused ? 0.12 : 0.35

        pd.group.position.set(
          Math.cos(pd.angle) * pd.def.radius,
          0,
          Math.sin(pd.angle) * pd.def.radius
        )
        // 行星面向切向
        pd.group.rotation.y = -pd.angle
      })

      // 星尘缓转
      dust.rotation.y = t * 0.008

      // 视差（full 档）
      if (tier === 'full') {
        camera.position.x = THREE.MathUtils.lerp(camera.position.x, mouse.x * 0.9, dt * 2)
        camera.position.y = THREE.MathUtils.lerp(camera.position.y, 1.4 + mouse.y * 0.6, dt * 2)
        camera.lookAt(0, 0, 0)
      }

      // hover 射线
      if (hasHover && pointer.lengthSq() > 0.0001) {
        raycaster.setFromCamera(pointer, camera)
        const hits = raycaster.intersectObjects(planetSprites)
        const name = hits.length > 0 ? (hits[0].object.userData.name as string) : null
        if (name !== hoveredRef.current) {
          hoveredRef.current = name
          onHover(name)
        }
      }

      renderer.render(scene, camera)
    }

    if (!reduced) loop()
    else {
      // reduced-motion：渲染一帧静态
      planetData.forEach((pd) => {
        pd.group.position.set(pd.def.radius, 0, 0)
      })
      renderer.render(scene, camera)
    }

    // 可见性暂停
    const io = new IntersectionObserver((entries) => {
      const visible = entries[0].isIntersecting
      if (visible && !reduced && !running) {
        running = true
        loop()
      } else if (!visible && running) {
        running = false
        cancelAnimationFrame(raf)
      }
    })
    io.observe(mount)

    const onResize = () => {
      const w = mount.clientWidth
      const h = mount.clientHeight
      camera.aspect = w / Math.max(h, 1)
      camera.updateProjectionMatrix()
      renderer.setSize(w, h)
    }
    window.addEventListener('resize', onResize)

    return () => {
      cancelAnimationFrame(raf)
      io.disconnect()
      window.removeEventListener('resize', onResize)
      renderer.domElement.removeEventListener('pointermove', onPointerMove)
      renderer.domElement.removeEventListener('pointerleave', onPointerLeave)
      renderer.dispose()
      mount.removeChild(renderer.domElement)
    }
  }, [onHover])

  return <div ref={mountRef} className={className} />
}

// ---------- 静态视图（移动端 / reduced-motion） ----------
function StaticSolar({ icons }: { icons: Record<string, ComponentType<Record<string, unknown>>> }) {
  const orbitCounts: Record<0 | 1 | 2, number> = { 0: 0, 1: 0, 2: 0 }
  const positions = PROVIDERS.map((p) => {
    const idx = orbitCounts[p.orbit]++
    const total = PROVIDERS.filter((x) => x.orbit === p.orbit).length
    const angle = (idx / total) * Math.PI * 2 + p.orbit * 0.6
    return { ...p, angle }
  })

  const orbits = [0, 1, 2].map((k) => {
    const maxR = Math.max(...PROVIDERS.filter((p) => p.orbit === k).map((p) => p.radius))
    return { tilt: ORBIT_TILT[k as 0 | 1 | 2] * (180 / Math.PI), radius: maxR }
  })

  return (
    <div className='relative flex h-full w-full items-center justify-center overflow-hidden'>
      {/* 恒星核心光晕 */}
      <div className='bg-[radial-gradient(circle_at_50%_50%,color-mix(in_oklch,var(--primary)_60%,transparent)_0%,color-mix(in_oklch,var(--accent)_34%,transparent)_48%,transparent_72%)] pointer-events-none absolute top-1/2 left-1/2 size-[min(62vw,380px)] -translate-x-1/2 -translate-y-1/2 blur-[2px]' />

      {/* 轨道 + 行星（椭圆透视） */}
      <div className='pointer-events-none absolute inset-0 flex items-center justify-center'>
        <div className='relative size-[min(92vw,620px)]' style={{ transform: 'scaleY(0.68)' }}>
          {orbits.map((o, i) => (
            <div
              key={i}
              className='absolute top-1/2 left-1/2 -translate-x-1/2 -translate-y-1/2 rounded-full border border-[color-mix(in_oklch,var(--accent)_30%,transparent)]'
              style={{
                width: `${(o.radius / 5.8) * 100}%`,
                height: `${(o.radius / 5.8) * 100}%`,
                transform: `translate(-50%,-50%) rotateX(${o.tilt}deg)`,
              }}
            />
          ))}
          {positions.map((p) => {
            const Ico = icons[p.id]
            const r = (p.radius / 5.8) * 50 // 半径%（容器一半对应半径）
            const x = Math.cos(p.angle) * r
            const y = Math.sin(p.angle) * r
            return (
              <div
                key={p.id}
                className='absolute top-1/2 left-1/2'
                style={{ transform: `translate(calc(-50% + ${x}%), calc(-50% + ${y}%))` }}
              >
                <div className='grid size-[clamp(20px,3.2vw,30px)] place-items-center rounded-full bg-[color-mix(in_oklch,var(--background)_88%,transparent)] shadow-[0_0_16px_color-mix(in_oklch,var(--accent)_60%,transparent)] ring-1 ring-[color-mix(in_oklch,var(--primary)_25%,transparent)]'>
                  <span className='scale-[0.55] text-foreground/90'>
                    <Ico width={40} height={40} />
                  </span>
                </div>
              </div>
            )
          })}
        </div>
      </div>
    </div>
  )
}

// ---------- 主出口 ----------
export function SolarSystem({
  className = '',
  label,
}: {
  className?: string
  label?: string
}) {
  const [hovered, setHovered] = useState<string | null>(null)
  const [tier, setTier] = useState<Tier>('full')
  const iconMap: Record<string, ComponentType<Record<string, unknown>>> = {
    openai: OpenAI,
    claude: Claude,
    gemini: Gemini,
    llama: Meta,
    mistral: Mistral,
    grok: Grok,
    aws: Aws,
    azure: AzureAI,
    nvidia: Nvidia,
    cohere: Cohere,
  }

  useEffect(() => {
    setTier(detectTier())
  }, [])

  const handleHover = useCallback((name: string | null) => setHovered(name), [])

  return (
    <div className={`relative h-full w-full overflow-hidden ${className}`}>
      {(tier === 'full' || tier === 'lite') && <SolarCanvas className='absolute inset-0' onHover={handleHover} />}
      {tier === 'static' && <StaticSolar icons={iconMap} />}

      {/* 恒星中心字标 */}
      {label && (
        <div className='pointer-events-none absolute inset-0 z-10 flex items-center justify-center'>
          <div className='bg-[radial-gradient(circle_at_50%_50%,color-mix(in_oklch,var(--primary)_55%,transparent)_0%,color-mix(in_oklch,var(--accent)_30%,transparent)_45%,transparent_72%)] absolute size-[min(58vw,340px)] blur-[2px]' />
          <span className='brand-wordmark text-[clamp(1.6rem,6vw,2.8rem)] text-foreground drop-shadow-[0_0_22px_color-mix(in_oklch,var(--primary)_70%,transparent)]'>
            {label}
          </span>
        </div>
      )}

      {/* hover 名称浮层 */}
      {hovered && (
        <div className='pointer-events-none absolute bottom-[16%] left-1/2 z-30 -translate-x-1/2'>
          <span className='border border-border/50 bg-background/70 px-3 py-1 text-xs font-medium text-foreground shadow-lg backdrop-blur-sm'>
            {hovered}
          </span>
        </div>
      )}
    </div>
  )
}
