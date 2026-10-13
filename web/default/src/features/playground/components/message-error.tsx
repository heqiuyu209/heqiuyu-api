import { Link } from '@tanstack/react-router'
import { AlertCircle, AlertTriangle, Settings } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useAuthStore } from '@/stores/auth-store'
import { ROLE } from '@/lib/roles'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { MESSAGE_STATUS } from '../constants'
import type { Message } from '../types'

interface MessageErrorProps {
  message: Message
  className?: string
}

/**
 * Display error messages using Alert component
 * Following ai-elements pattern for error handling
 */
export function MessageError({ message, className = '' }: MessageErrorProps) {
  const { t } = useTranslation()
  const user = useAuthStore((s) => s.auth.user)
  // 模型价格设置页 /system-settings/models/ratio 仅超管可访问，
  // 按钮门槛必须与目标页守卫一致，否则点过去只会看到 403。
  const canConfigureModelPrice = user?.role === ROLE.SUPER_ADMIN

  if (message.status !== MESSAGE_STATUS.ERROR) {
    return null
  }

  const errorContent =
    message.versions[0]?.content || 'An unknown error occurred'

  if (message.errorCode === 'model_price_error') {
    return (
      <Alert variant='default' className={className}>
        <AlertTriangle className='text-orange-500' />
        <AlertTitle>{t('Model Price Not Configured')}</AlertTitle>
        <AlertDescription className='space-y-2'>
          <p>{errorContent}</p>
          {canConfigureModelPrice && (
            <Button variant='outline' size='sm' asChild>
              <Link
                to='/system-settings/models/$section'
                params={{ section: 'ratio' }}
                target='_blank'
                rel='noopener noreferrer'
              >
                <Settings className='mr-1 h-3.5 w-3.5' />
                {t('Go to Settings')}
              </Link>
            </Button>
          )}
        </AlertDescription>
      </Alert>
    )
  }

  return (
    <Alert variant='destructive' className={className}>
      <AlertCircle />
      <AlertTitle>{t('Error')}</AlertTitle>
      <AlertDescription>{errorContent}</AlertDescription>
    </Alert>
  )
}
