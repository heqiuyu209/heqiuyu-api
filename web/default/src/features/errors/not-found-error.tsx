import { useNavigate, useRouter } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'

export function NotFoundError() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const { history } = useRouter()
  return (
    <div className='h-svh'>
      <div className='m-auto flex h-full w-full flex-col items-center justify-center gap-2'>
        <div aria-hidden className='starburst-glow h-80 w-80' />
        <h1 className='bg-linear-to-br from-oklch(0.52 0.15 268) via-oklch(0.7 0.11 192) to-oklch(0.64 0.14 305) bg-clip-text text-[7rem] leading-tight font-bold text-transparent'>
          404
        </h1>
        <span className='font-medium'>{t('Oops! Page Not Found!')}</span>
        <p className='text-muted-foreground text-center'>
          {t("It seems like the page you're looking for")} <br />
          {t('does not exist or might have been removed.')}
        </p>
        <div className='mt-6 flex gap-4'>
          <Button variant='outline' onClick={() => history.go(-1)}>
            {t('Go Back')}
          </Button>
          <Button onClick={() => navigate({ to: '/' })}>
            {t('Back to Home')}
          </Button>
        </div>
      </div>
    </div>
  )
}
