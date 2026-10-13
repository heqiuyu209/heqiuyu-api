/**
 * 登录/回调后跳转目标的校验工具。
 *
 * `redirect` 这类值来自 URL 查询参数（如 `/sign-in?redirect=...`、
 * `/oauth/<provider>?redirect=...`），属于不可信输入。直接把它交给
 * `navigate` / `redirect` / `window.location` 会造成两类问题：
 *   1. 跳到站外（开放重定向），例如 `https://evil.com`；
 *   2. 或拼出 `/sign-in/https:/evil.com` 这种不存在的路径，最终渲染 404 页。
 * 因此统一在这里限制成「站内相对路径」，非法值一律丢弃由调用方回退。
 */

/** 允许的站内跳转目标前缀（必须以单个 `/` 开头） */
function isInternalPath(target: string): boolean {
  // 必须是站内绝对路径
  if (!target.startsWith('/')) return false
  // 协议相对地址（//evil.com）会跳出站外
  if (target.startsWith('//')) return false
  // 反斜杠在部分浏览器中等价于 `/`，可被用来绕过上面的判断
  if (target.includes('\\')) return false
  // 形如 /javascript:alert(1) 的伪协议
  if (/^\/[a-zA-Z][a-zA-Z0-9+.-]*:/.test(target)) return false
  return true
}

/**
 * 校验跳转目标，返回可安全使用的站内路径；非法或缺失时返回 undefined。
 */
export function normalizeInternalRedirect(
  value: string | null | undefined
): string | undefined {
  if (typeof value !== 'string') return undefined
  const target = value.trim()
  if (target === '') return undefined
  if (!isInternalPath(target)) return undefined
  return target
}

/**
 * 取站内跳转目标，非法或缺失时回退到 fallback（默认 `/dashboard`）。
 */
export function resolveInternalRedirect(
  value: string | null | undefined,
  fallback = '/dashboard'
): string {
  return normalizeInternalRedirect(value) ?? fallback
}
