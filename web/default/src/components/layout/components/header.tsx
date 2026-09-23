import { cn } from '@/lib/utils'
import { Separator } from '@/components/ui/separator'
import { SidebarTrigger } from '@/components/ui/sidebar'

type HeaderProps = React.HTMLAttributes<HTMLElement>

export function Header({ className, children, ...props }: HeaderProps) {
  return (
    <header
      className={cn(
        'bg-background/85 z-50 h-16 shrink-0 border-b backdrop-blur-md',
        className
      )}
      {...props}
    >
      <div className='flex h-full items-center gap-3 p-4 sm:gap-4'>
        <SidebarTrigger variant='outline' />
        <Separator orientation='vertical' className='h-6' />
        {children}
      </div>
      {/* 星舰仪表带：顶部靛蓝辉光线 */}
      <span
        aria-hidden
        className='via-oklch(0.52 0.15 268 / 0.55) absolute inset-x-0 -bottom-px h-px bg-linear-to-r from-transparent to-transparent'
      />
    </header>
  )
}
