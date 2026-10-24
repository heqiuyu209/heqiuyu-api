import { useEffect, useRef } from 'react'

declare global {
  interface Window {
    turnstile?: {
      render: (element: HTMLElement, options: Record<string, unknown>) => string
      reset: (widgetId?: string) => void
    }
  }
}

interface TurnstileProps {
  siteKey: string
  onVerify: (token: string) => void
  onExpire?: () => void
  className?: string
  /**
   * Increment this value to reset the widget.
   * Turnstile tokens are single-use, so callers must reset after a failed or
   * already-consumed submission to obtain a fresh token.
   */
  resetSignal?: number
}

export function Turnstile({
  siteKey,
  onVerify,
  onExpire,
  className,
  resetSignal = 0,
}: TurnstileProps) {
  const containerRef = useRef<HTMLDivElement | null>(null)
  const widgetIdRef = useRef<string | null>(null)
  const lastResetSignalRef = useRef(resetSignal)

  useEffect(() => {
    const render = () => {
      if (!containerRef.current || !window.turnstile) return
      try {
        widgetIdRef.current = window.turnstile.render(containerRef.current, {
          sitekey: siteKey,
          callback: (token: string) => onVerify(token),
          'error-callback': () => onExpire?.(),
          'expired-callback': () => onExpire?.(),
        })
      } catch {
        /* empty */
      }
    }

    if (window.turnstile) {
      render()
      return
    }
    const scriptId = 'cf-turnstile'
    if (document.getElementById(scriptId)) return
    const s = document.createElement('script')
    s.id = scriptId
    s.src =
      'https://challenges.cloudflare.com/turnstile/v0/api.js?render=explicit'
    s.async = true
    s.defer = true
    s.onload = () => render()
    document.head.appendChild(s)
  }, [siteKey, onVerify, onExpire])

  useEffect(() => {
    if (lastResetSignalRef.current === resetSignal) return
    lastResetSignalRef.current = resetSignal
    if (!widgetIdRef.current || !window.turnstile) return
    try {
      window.turnstile.reset(widgetIdRef.current)
    } catch {
      /* empty */
    }
  }, [resetSignal])

  return <div ref={containerRef} className={className} />
}
