import { useEffect } from 'react'
import { createFileRoute, useNavigate, useSearch } from '@tanstack/react-router'
import i18next from 'i18next'
import { toast } from 'sonner'
import { useAuthStore, type AuthUser } from '@/stores/auth-store'
import { getSelf } from '@/lib/api'
import { wechatLoginByCode } from '@/features/auth/api'
import { isTwoFactorRequired } from '@/features/auth/lib/two-factor'

function OAuthComponent() {
  const navigate = useNavigate()
  const search = useSearch({ from: '/(auth)/oauth' }) as {
    redirect?: string
    provider?: 'github' | 'discord' | 'oidc' | 'linuxdo' | 'telegram' | 'wechat'
    code?: string
    state?: string
  }

  useEffect(() => {
    ;(async () => {
      try {
        if (search?.provider === 'wechat' && search.code) {
          const wechatRes = await wechatLoginByCode(search.code)
          // 2FA challenge: no session was established, continue on the OTP page
          if (isTwoFactorRequired(wechatRes?.data)) {
            toast.info(i18next.t('Please enter the authentication code.'))
            navigate({ to: '/otp', replace: true })
            return
          }
        }
        const res = await getSelf()
        if (res?.success) {
          useAuthStore.getState().auth.setUser(res.data as AuthUser)
          const target = search?.redirect || '/dashboard'
          navigate({ to: target, replace: true })
          return
        }
      } catch {
        /* empty */
      }
      toast.error(i18next.t('OAuth failed'))
      navigate({ to: '/sign-in', replace: true })
    })()
  }, [navigate, search])

  return null
}

export const Route = createFileRoute('/(auth)/oauth')({
  component: OAuthComponent,
})
