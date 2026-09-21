import { useState } from 'react'
import { useNavigate } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { api } from '@/lib/api'
import { useCountdown } from '@/hooks/use-countdown'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { AuthLayout } from '../auth-layout'

export type ResetPasswordSearchParams = {
  email?: string
  token?: string
}

type ResetPasswordConfirmProps = ResetPasswordSearchParams

export function ResetPasswordConfirm({
  email,
  token,
}: ResetPasswordConfirmProps) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const [newPassword, setNewPassword] = useState('')
  const [loading, setLoading] = useState(false)
  const {
    secondsLeft,
    isActive,
    start: startCountdown,
  } = useCountdown({ initialSeconds: 30 })

  const isValidResetLink = Boolean(email && token)

  async function handleSubmit() {
    if (!isValidResetLink || !email || !token) {
      toast.error(t('Invalid reset link, please request a new password reset'))
      return
    }

    if (!newPassword) {
      toast.error(t('Please enter a new password'))
      return
    }
    if (newPassword.length < 8 || newPassword.length > 20) {
      toast.error(t('Password must be 8-20 characters'))
      return
    }

    startCountdown()
    setLoading(true)
    try {
      const res = await api.post(
        '/api/user/reset',
        { email, token, new_password: newPassword },
        {
          skipBusinessError: true,
        } as Record<string, unknown>
      )

      if (res?.data?.success) {
        toast.success('Password reset successfully')
        navigate({ to: '/sign-in', replace: true })
      }
    } catch {
      // Errors handled by global interceptor
    } finally {
      setLoading(false)
    }
  }

  return (
    <AuthLayout>
      <div className='w-full space-y-8'>
        <div className='space-y-2'>
          <h2 className='text-center text-2xl font-semibold tracking-tight sm:text-left'>
            {t('Reset password')}
          </h2>
          <p className='text-muted-foreground text-left text-sm sm:text-base'>
            {t('Please enter a new password')}
          </p>
        </div>

        <div className='space-y-4'>
          {!isValidResetLink && (
            <Alert variant='destructive'>
              <AlertDescription>
                {t('Invalid reset link, please request a new password reset.')}
              </AlertDescription>
            </Alert>
          )}

          <div className='space-y-2'>
            <Label htmlFor='email'>{t('Email')}</Label>
            <Input
              id='email'
              type='email'
              value={email || ''}
              disabled
              placeholder={t('Waiting for email...')}
            />
          </div>

          <div className='space-y-2'>
            <Label htmlFor='password'>{t('New password')}</Label>
            <Input
              id='password'
              type='password'
              value={newPassword}
              onChange={(e) => setNewPassword(e.target.value)}
              placeholder={t('Please enter a new password')}
            />
            <p className='text-muted-foreground text-xs'>
              {t('Password must be 8-20 characters')}
            </p>
          </div>

          <Button
            className='w-full'
            onClick={handleSubmit}
            disabled={loading || isActive || !isValidResetLink}
          >
            {isActive ? `Retry (${secondsLeft}s)` : 'Confirm reset password'}
          </Button>

          {!isValidResetLink && (
            <Button
              variant='link'
              className='w-full'
              onClick={() => navigate({ to: '/sign-in', replace: true })}
            >
              {t('Back to login')}
            </Button>
          )}
        </div>
      </div>
    </AuthLayout>
  )
}
