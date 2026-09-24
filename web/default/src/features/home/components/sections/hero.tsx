import { Link } from '@tanstack/react-router'
import { ArrowRight } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useSystemConfig } from '@/hooks/use-system-config'
import { Button } from '@/components/ui/button'
import { SolarSystem } from '../solar-system'

interface HeroProps {
  className?: string
  isAuthenticated?: boolean
}

export function Hero(props: HeroProps) {
  const { t } = useTranslation()
  const { systemName } = useSystemConfig()

  return (
    <section className='relative z-10 flex flex-col items-center overflow-hidden px-4 pt-14 pb-16 md:px-6 md:pt-16 md:pb-24'>
      {/* 背景径向光（陪衬，微弱）。样式在 styles/index.css 的 .solar-hero-aurora，
          以便为不支持 oklch() 的旧浏览器提供 rgba 降级。 */}
      <div
        aria-hidden
        className='solar-hero-aurora pointer-events-none absolute inset-0 -z-10 opacity-25 dark:opacity-[0.12]'
      />

      {/* 天体系统视窗（深空星窗） */}
      <div className='relative z-10 mx-auto w-full max-w-5xl'>
        <div
          className='border-border/40 relative h-[480px] w-full overflow-hidden rounded-[2rem] border sm:h-[560px] md:h-[620px]'
          style={{
            background: 'var(--solar-bg)',
          }}
        >
          <SolarSystem label='H' />
        </div>
      </div>

      {/* CTA 区：恒星下方保留入口 */}
      <div className='landing-animate-fade-up z-10 mt-10 flex max-w-2xl flex-col items-center text-center'>
        <p className='text-muted-foreground/80 text-base leading-relaxed md:text-lg'>
          {systemName}{' '}
          {t(
            'aggregates 50+ AI providers behind one unified API. Manage access, track costs, and scale effortlessly.'
          )}
        </p>
        <div className='mt-6 flex flex-wrap items-center justify-center gap-3'>
          {props.isAuthenticated ? (
            <Button className='group rounded-lg' asChild>
              <Link to='/dashboard'>
                {t('Go to Dashboard')}
                <ArrowRight className='ml-1 size-3.5 transition-transform duration-200 group-hover:translate-x-0.5' />
              </Link>
            </Button>
          ) : (
            <>
              <Button className='group rounded-lg' asChild>
                <Link to='/sign-up'>
                  {t('Get Started')}
                  <ArrowRight className='ml-1 size-3.5 transition-transform duration-200 group-hover:translate-x-0.5' />
                </Link>
              </Button>
              <Button
                variant='outline'
                className='border-border/50 hover:border-border hover:bg-muted/50 rounded-lg'
                asChild
              >
                <Link to='/pricing'>{t('View Pricing')}</Link>
              </Button>
            </>
          )}
        </div>
      </div>

      {/* 无障碍标题 */}
      <h1 className='sr-only'>
        heqiuyu — {t('Unified API Gateway for All Your AI Models')}
      </h1>
    </section>
  )
}
