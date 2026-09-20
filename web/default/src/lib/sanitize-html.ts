// 统一的 HTML 消毒层（DOMPurify 风格白名单实现，零依赖）。
// 用于所有 dangerouslySetInnerHTML 入口，防止管理端配置的富文本/公告/文档
// 在管理员账号失陷时形成存储型 XSS 直达普通用户（审查项 R2）。
// 设计取舍：移除可执行/交互标签（script/iframe/form/object/embed 等）、
// 事件属性与危险协议（javascript:/data:/vbscript:），保留常见排版标签与
// 内联样式（style 属性中的 url()/expression/@import 会被清洗）。

const ALLOWED_TAGS = new Set([
  'a', 'b', 'strong', 'i', 'em', 'u', 's', 'small', 'sub', 'sup', 'br',
  'p', 'div', 'span', 'h1', 'h2', 'h3', 'h4', 'h5', 'h6',
  'ul', 'ol', 'li', 'dl', 'dt', 'dd',
  'img', 'figure', 'figcaption', 'table', 'thead', 'tbody', 'tfoot', 'tr', 'th', 'td',
  'blockquote', 'pre', 'code', 'hr', 'details', 'summary', 'mark', 'abbr', 'cite', 'kbd',
])

const ALLOWED_ATTRS = new Set([
  'class', 'id', 'title', 'alt', 'width', 'height', 'align', 'dir', 'lang',
  'target', 'rel', 'start', 'type', 'colspan', 'rowspan', 'scope', 'headers',
])

const URL_ATTRS = new Set([
  'href', 'src', 'srcset', 'poster', 'cite', 'action', 'formaction',
  'background', 'longdesc', 'usemap', 'xlink:href',
])

const SAFE_PROTOCOLS = ['http:', 'https:', 'mailto:', 'tel:', 'ftp:']

// 白名单外的危险标签直接连内容移除（不留文本）
const DANGEROUS_TAGS = [
  'script', 'iframe', 'object', 'embed', 'link', 'meta', 'base',
  'form', 'input', 'button', 'textarea', 'select', 'option', 'optgroup',
  'frame', 'frameset', 'applet', 'audio', 'video', 'source', 'track',
  'svg', 'math', 'template', 'style', 'title', 'head', 'body',
]

function isSafeUrl(value: string): boolean {
  if (!value) return false
  const trimmed = String(value).trim()
  if (!trimmed) return false
  // 协议相对 URL（//host/path）会跟随当前页协议加载外部资源，禁止放行
  if (/^\/\//.test(trimmed)) return false
  // 锚点 / 相对路径 / 查询串
  if (/^(#|\/|\.\/|\.\.\/|\?)/.test(trimmed)) return true
  // 带协议时必须命中白名单
  if (/^[a-zA-Z][a-zA-Z0-9+.-]*:/.test(trimmed)) {
    const protocol = trimmed.split(':')[0].toLowerCase() + ':'
    return SAFE_PROTOCOLS.includes(protocol)
  }
  // 无协议按相对路径处理
  return true
}

// 清洗内联 style 中的危险 CSS：url()、expression()、@import、javascript:
function cleanInlineStyle(styleValue: string | null): string {
  return String(styleValue || '')
    .replace(/url\s*\(/gi, ' /*sanitized*/ ')
    .replace(/expression\s*\(/gi, ' /*sanitized*/ ')
    .replace(/@import/gi, ' /*sanitized*/ ')
    .replace(/javascript\s*:/gi, ' ')
    .replace(/vbscript\s*:/gi, ' ')
    .replace(/data\s*:/gi, ' ')
}

/**
 * 清洗不可信 HTML 字符串，返回可安全用于 dangerouslySetInnerHTML 的内容。
 * 在浏览器环境（DOM 可用）下执行；非浏览器环境原样返回空串。
 */
export function sanitizeHtml(html: string): string {
  if (!html || typeof html !== 'string') return ''
  if (typeof document === 'undefined') return ''

  const template = document.createElement('template')
  template.innerHTML = html // template 内 script 不会执行
  const root = template.content

  // 1) 直接移除危险标签（连同内容）
  for (const tag of DANGEROUS_TAGS) {
    root.querySelectorAll(tag).forEach((el) => el.remove())
  }

  // 2) 白名单外标签解包保留文本；白名单内标签清洗属性
  root.querySelectorAll('*').forEach((el) => {
    const tagName = el.tagName.toLowerCase()
    if (!ALLOWED_TAGS.has(tagName)) {
      el.replaceWith(...el.childNodes)
      return
    }
    Array.from(el.attributes).forEach((attr) => {
      const name = attr.name.toLowerCase()
      if (name.startsWith('on')) {
        el.removeAttribute(attr.name)
        return
      }
      if (URL_ATTRS.has(name)) {
        if (!isSafeUrl(attr.value)) {
          el.removeAttribute(attr.name)
        }
        return
      }
      if (!ALLOWED_ATTRS.has(name)) {
        el.removeAttribute(attr.name)
      }
    })
  })

  // 3) 清洗内联 style
  root.querySelectorAll('[style]').forEach((el) => {
    el.setAttribute('style', cleanInlineStyle(el.getAttribute('style')))
  })

  return root.innerHTML
}

export default sanitizeHtml
