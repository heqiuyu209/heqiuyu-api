import { useState } from 'react'
import i18next from 'i18next'
import { toast } from 'sonner'
import { useCountdown } from '@/hooks/use-countdown'
import { sendEmailVerification } from '../api'
import { EMAIL_VERIFICATION_COUNTDOWN } from '../constants'

interface UseEmailVerificationOptions {
  turnstileToken?: string
  validateTurnstile?: () => boolean
  /** Invite code forwarded to the backend when registration requires one. */
  inviteCode?: string
  /** Verification purpose; the sign-up form must use "register". */
  purpose?: string
  /**
   * Called after a verification email request has been attempted (success or
   * failure). Strict Turnstile mode verifies every request, so the single-use
   * token must be refreshed by resetting the widget.
   */
  onAfterSend?: () => void
}

/**
 * Hook for managing email verification code sending
 */
export function useEmailVerification(options?: UseEmailVerificationOptions) {
  const [isSending, setIsSending] = useState(false)
  const {
    secondsLeft,
    isActive,
    start: startCountdown,
  } = useCountdown({ initialSeconds: EMAIL_VERIFICATION_COUNTDOWN })

  /**
   * Send verification code to email
   */
  const sendCode = async (email: string) => {
    if (!email) {
      toast.error(i18next.t('Please enter your email first'))
      return false
    }

    // Validate turnstile if validation function is provided
    if (options?.validateTurnstile && !options.validateTurnstile()) {
      return false
    }

    setIsSending(true)
    try {
      const res = await sendEmailVerification(
        email,
        options?.turnstileToken,
        options?.inviteCode,
        options?.purpose
      )
      if (res?.success) {
        startCountdown()
        toast.success(i18next.t('Verification email sent'))
        return true
      }
      // Business failures (200 + success=false) are already surfaced with the
      // backend message by the global axios interceptor; don't duplicate it.
      return false
    } catch (_error) {
      // Errors are handled by global interceptor
      return false
    } finally {
      setIsSending(false)
      options?.onAfterSend?.()
    }
  }

  return {
    isSending,
    secondsLeft,
    isActive,
    sendCode,
  }
}
