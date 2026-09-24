import * as React from 'react'

/**
 * 全站唯一移动断点。表格卡片化、侧边栏抽屉、天体系统降级档位
 * 都必须引用它，避免出现「侧边栏已是抽屉、表格仍是桌面版」的夹缝区间。
 */
export const MOBILE_BREAKPOINT = 768

export function useIsMobile() {
  const [isMobile, setIsMobile] = React.useState<boolean>(() =>
    typeof window === 'undefined'
      ? false
      : window.innerWidth < MOBILE_BREAKPOINT
  )

  React.useEffect(() => {
    const mql = window.matchMedia(`(max-width: ${MOBILE_BREAKPOINT - 1}px)`)
    const onChange = () => {
      setIsMobile(window.innerWidth < MOBILE_BREAKPOINT)
    }
    mql.addEventListener('change', onChange)
    return () => mql.removeEventListener('change', onChange)
  }, [])

  return isMobile
}
