import { useNavigate } from '@tanstack/react-router'
import i18n from 'i18next'
import { useAuthStore } from '@/stores/auth-store'
import { getSelf } from '@/lib/api'
import {
  normalizeInternalRedirect,
  resolveInternalRedirect,
} from '@/lib/redirect'
import type { User } from '@/features/users/types'
import { saveUserId } from '../lib/storage'

/**
 * Hook for handling authentication redirects and user data management
 */
export function useAuthRedirect() {
  const navigate = useNavigate()
  const { auth } = useAuthStore()

  /**
   * Handle successful login
   * @param userData - Optional user data from login response
   * @param redirectTo - Redirect path after login
   */
  const handleLoginSuccess = async (
    userData?: { id?: number } | null,
    redirectTo?: string
  ) => {
    // Save user ID if available
    if (userData?.id) {
      saveUserId(userData.id)
    }

    // Fetch and set user data
    try {
      const self = await getSelf()
      if (self?.success && self.data) {
        const user = self.data as User
        auth.setUser(user)

        // Update user ID if not already set
        if (user.id) {
          saveUserId(user.id)
        }

        // Restore saved language preference
        const savedLang = (user as Record<string, unknown>).language as
          string | undefined
        if (savedLang && savedLang !== i18n.language) {
          i18n.changeLanguage(savedLang)
        }
      }
    } catch (error) {
      // eslint-disable-next-line no-console
      console.error('Failed to fetch user data:', error)
    }

    // Navigate to target page
    const targetPath = resolveInternalRedirect(redirectTo)
    navigate({ to: targetPath, replace: true })
  }

  /**
   * Redirect to 2FA page, keeping the original destination so the user
   * still lands where they wanted after the code is verified.
   */
  const redirectTo2FA = (redirectTo?: string) => {
    const target = normalizeInternalRedirect(redirectTo)
    navigate({
      to: '/otp',
      search: target ? { redirect: target } : {},
      replace: true,
    })
  }

  /**
   * Redirect to login page
   */
  const redirectToLogin = () => {
    navigate({ to: '/sign-in', replace: true })
  }

  /**
   * Redirect to the page the user originally asked for (or dashboard)
   */
  const redirectAfterLogin = (redirectTo?: string) => {
    navigate({ to: resolveInternalRedirect(redirectTo), replace: true })
  }

  /**
   * Redirect to register page
   */
  const redirectToRegister = () => {
    navigate({ to: '/sign-up', replace: true })
  }

  return {
    handleLoginSuccess,
    redirectTo2FA,
    redirectToLogin,
    redirectAfterLogin,
    redirectToRegister,
  }
}
