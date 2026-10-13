import { useEffect, useRef, useState } from 'react'
import { z } from 'zod'
import type { AxiosRequestConfig } from 'axios'
import {
  createFileRoute,
  useNavigate,
  useParams,
  useSearch,
} from '@tanstack/react-router'
import i18next from 'i18next'
import { toast } from 'sonner'
import { useAuthStore, type AuthUser } from '@/stores/auth-store'
import { api, getSelf } from '@/lib/api'
import { resolveInternalRedirect } from '@/lib/redirect'
import { OAuthCallbackScreen } from '@/features/auth/components/oauth-callback-screen'
import { OAUTH_BIND_STORAGE_KEY } from '@/features/auth/constants'
import { isTwoFactorRequired } from '@/features/auth/lib/two-factor'

type OAuthRequestConfig = AxiosRequestConfig & {
  skipBusinessError?: boolean
}

function OAuthCallback() {
  const navigate = useNavigate()
  const { provider } = useParams({ from: '/oauth/$provider' }) as {
    provider: string
  }
  const search = useSearch({ from: '/oauth/$provider' }) as {
    code?: string
    state?: string
    redirect?: string
  }
  const [mode, setMode] = useState<'login' | 'bind'>(() => {
    if (typeof window === 'undefined') return 'login'
    return window.opener ? 'bind' : 'login'
  })
  // 回调里的 code 只能兑换一次：用 ref 防止 effect 重跑（StrictMode、
  // mode/search 引用变化）时重复调用后端接口。
  const startedRef = useRef(false)

  useEffect(() => {
    if (typeof window === 'undefined') return
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setMode(window.opener ? 'bind' : 'login')
  }, [])

  useEffect(() => {
    if (startedRef.current) return
    startedRef.current = true
    ;(async () => {
      /**
       * 唯一的跳转入口：只允许站内路径（见 @/lib/redirect），
       * 并且完全交给路由器，不再用 window.location 兜底。
       */
      const safeNavigate = (target: string) => {
        navigate({
          to: resolveInternalRedirect(target) as never,
          replace: true,
        })
      }

      if (!search?.code) {
        toast.error(i18next.t('Missing code'))
        safeNavigate('/sign-in')
        return
      }
      const isBindingFlow =
        typeof window !== 'undefined' ? Boolean(window.opener) : mode === 'bind'
      if (isBindingFlow && mode !== 'bind') {
        setMode('bind')
      } else if (!isBindingFlow && mode !== 'login') {
        setMode('login')
      }
      const notifyBindingResult = (status: 'success' | 'error') => {
        if (typeof window === 'undefined') return
        try {
          window.localStorage.setItem(
            OAUTH_BIND_STORAGE_KEY,
            JSON.stringify({
              provider,
              status,
              timestamp: Date.now(),
            })
          )
        } catch (_error) {
          // ignore storage write failures
          void _error
        }
      }

      const closeBindingWindow = () => {
        if (typeof window === 'undefined') return
        window.close()
        setTimeout(() => {
          if (!window.closed) {
            // 绑定窗口关不掉时留在站内个人中心（`_authenticated` 是
            // pathless 布局路由，不能出现在 URL 里）
            safeNavigate('/profile')
          }
        }, 200)
      }

      const finalizeLogin = async (): Promise<boolean> => {
        try {
          const selfResponse = (await getSelf()) as {
            success?: boolean
            data?: AuthUser | null
          }
          if (selfResponse?.success && selfResponse.data) {
            useAuthStore.getState().auth.setUser(selfResponse.data)
            try {
              if (
                typeof window !== 'undefined' &&
                selfResponse.data?.id != null
              ) {
                window.localStorage.setItem('uid', String(selfResponse.data.id))
              }
            } catch (_error) {
              void _error
            }
            return true
          }
        } catch (_error) {
          void _error
        }
        return false
      }

      const redirectAfterLogin = (target?: string) => {
        const to = target || search?.redirect || '/dashboard'
        safeNavigate(to)
        toast.success(i18next.t('Signed in successfully!'))
      }

      const handleBindingFailure = (message: string) => {
        notifyBindingResult('error')
        toast.error(message)
      }

      const handleLoginFailure = async (message: string) => {
        if (await finalizeLogin()) {
          redirectAfterLogin()
          return
        }
        toast.error(message)
        safeNavigate('/sign-in')
      }

      try {
        const config: OAuthRequestConfig = {
          params: { code: search.code, state: search.state },
          skipBusinessError: true,
        }
        const res = await api.get(`/api/oauth/${provider}`, config)
        if (res?.data?.success) {
          const { message } = res.data
          const loginUser = (res.data?.data ?? null) as AuthUser | null
          // Check if this is a bind operation
          if (message === 'bind') {
            toast.success(i18next.t('Binding successful!'))
            notifyBindingResult('success')
            if (isBindingFlow) {
              // Close the callback window if we opened a new tab for binding
              closeBindingWindow()
            } else {
              safeNavigate('/profile')
            }
            return
          }
          // The account has 2FA enabled: the backend did not establish a
          // session, it only stored the pending user in the session. Hand the
          // user over to the same OTP step the password login uses.
          if (isTwoFactorRequired(loginUser)) {
            toast.info(i18next.t('Please enter the authentication code.'))
            // 把原目标页一起带到 /otp，校验通过后才能回到这里
            navigate({
              to: '/otp',
              search: { redirect: resolveInternalRedirect(search?.redirect) },
              replace: true,
            })
            return
          }
          // Otherwise it's a login, use payload user if available
          if (loginUser) {
            useAuthStore.getState().auth.setUser(loginUser)
            try {
              if (typeof window !== 'undefined' && loginUser.id != null) {
                window.localStorage.setItem('uid', String(loginUser.id))
              }
            } catch (_error) {
              void _error
            }
            redirectAfterLogin()
            return
          }
          if (await finalizeLogin()) {
            redirectAfterLogin()
            return
          }
          toast.error(res?.data?.message || i18next.t('OAuth failed'))
          safeNavigate('/sign-in')
          return
        }
        const message = res?.data?.message || 'OAuth failed'
        if (!res?.data?.success && !isBindingFlow) {
          // When logging in with an already bound GitHub account, backend may return this message
          if (message === '该 GitHub 账户已被绑定') {
            if (await finalizeLogin()) {
              redirectAfterLogin()
              return
            }
          }
        }
        if (isBindingFlow) {
          handleBindingFailure(message)
        } else {
          await handleLoginFailure(message)
        }
        return
      } catch (error) {
        const message = ((error &&
          typeof error === 'object' &&
          'response' in error &&
          (error as { response?: { data?: { message?: string } } }).response
            ?.data?.message) ??
          (error instanceof Error ? error.message : undefined) ??
          'OAuth failed') as string

        if (isBindingFlow) {
          handleBindingFailure(message)
          return
        }
        await handleLoginFailure(message)
        return
      }
    })()
  }, [mode, navigate, provider, search])

  return <OAuthCallbackScreen provider={provider} mode={mode} />
}

export const Route = createFileRoute('/oauth/$provider')({
  // redirect 来自 URL，属于不可信输入；这里先做形状校验，
  // 真正的站内路径校验在 resolveInternalRedirect 里完成。
  validateSearch: z.object({
    code: z.string().optional(),
    state: z.string().optional(),
    redirect: z.string().optional(),
  }),
  component: OAuthCallback,
})
