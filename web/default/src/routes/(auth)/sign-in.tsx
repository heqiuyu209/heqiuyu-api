import { z } from 'zod'
import { createFileRoute, redirect } from '@tanstack/react-router'
import { useAuthStore } from '@/stores/auth-store'
import { resolveInternalRedirect } from '@/lib/redirect'
import { SignIn } from '@/features/auth/sign-in'

const searchSchema = z.object({
  redirect: z.string().optional(),
})

export const Route = createFileRoute('/(auth)/sign-in')({
  component: SignIn,
  validateSearch: searchSchema,
  beforeLoad: async ({ search }) => {
    const { auth } = useAuthStore.getState()

    // 如果已经有用户信息，说明已登录
    if (auth.user) {
      // 优先使用 redirect 参数（用户之前想去的地方）；
      // 站外/非法/缺失时回退到 dashboard，避免跳到 404 或站外
      throw redirect({ to: resolveInternalRedirect(search?.redirect) })
    }
  },
})
