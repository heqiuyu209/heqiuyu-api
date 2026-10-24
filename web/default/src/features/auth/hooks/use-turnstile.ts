import { useCallback, useState } from 'react'
import i18next from 'i18next'
import { toast } from 'sonner'
import { useStatus } from '@/hooks/use-status'

/**
 * Hook for managing Turnstile verification
 */
export function useTurnstile() {
  const { status } = useStatus()
  const [turnstileToken, setTurnstileToken] = useState('')
  const [resetSignal, setResetSignal] = useState(0)

  const isTurnstileEnabled = !!(
    status?.turnstile_check && status?.turnstile_site_key
  )
  const turnstileSiteKey = status?.turnstile_site_key || ''

  /**
   * Validate if turnstile is ready when required
   */
  const validateTurnstile = (): boolean => {
    if (isTurnstileEnabled && !turnstileToken) {
      toast.info(
        i18next.t('Please wait a moment, human check is initializing...')
      )
      return false
    }
    return true
  }

  /**
   * Drop the current token and reset the widget.
   * Turnstile tokens are single-use (strict mode verifies every request), so a
   * new token must be obtained after each submission attempt.
   */
  const resetTurnstile = useCallback(() => {
    setTurnstileToken('')
    setResetSignal((value) => value + 1)
  }, [])

  return {
    isTurnstileEnabled,
    turnstileSiteKey,
    turnstileToken,
    setTurnstileToken,
    validateTurnstile,
    resetSignal,
    resetTurnstile,
  }
}
