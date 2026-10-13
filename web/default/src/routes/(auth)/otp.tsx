import { z } from 'zod'
import { createFileRoute } from '@tanstack/react-router'
import { Otp } from '@/features/auth/otp'

// 2FA 步骤必须把「登录后本来要去的页面」带过来，否则校验通过后
// 用户会被送到 dashboard 而不是原目标页。
const otpSearchSchema = z.object({
  redirect: z.string().optional(),
})

export const Route = createFileRoute('/(auth)/otp')({
  validateSearch: otpSearchSchema,
  component: Otp,
})
