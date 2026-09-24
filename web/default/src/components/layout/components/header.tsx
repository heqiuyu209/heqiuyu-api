import { cn } from '@/lib/utils'
import { Separator } from '@/components/ui/separator'
import { SidebarTrigger } from '@/components/ui/sidebar'

type HeaderProps = React.HTMLAttributes<HTMLElement>

export function Header({ className, children, ...props }: HeaderProps) {
  return (
    <header
      className={cn(
        // 高度随顶部安全区自适应：无刘海时 env() 为 0，等价于原来的 h-16
        'bg-background/85 z-50 h-[calc(4rem+env(safe-area-inset-top))] shrink-0 border-b pt-[env(safe-area-inset-top)] pr-[env(safe-area-inset-right)] pl-[env(safe-area-inset-left)] backdrop-blur-md',
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
