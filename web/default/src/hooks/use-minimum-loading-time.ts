import { useEffect, useRef, useState } from 'react'

/**
 * Ensures a loading skeleton is shown for at least `minimumTime` ms
 * to prevent flickering when data loads too quickly.
 */
export function useMinimumLoadingTime(
  loading: boolean,
  minimumTime = 1000
): boolean {
  const [elapsed, setElapsed] = useState(!loading)
  const [prevLoading, setPrevLoading] = useState(loading)
  // 计时起点只在 effect / 回调里写（渲染期读时钟会触发 react-hooks/purity）
  const startedAtRef = useRef(0)

  // loading 变为 true 时重置「已满最短时长」。写在渲染期（React 官方的「依据 prop
  // 调整 state」写法），避免在 effect 内同步 setState 触发级联渲染。
  if (loading !== prevLoading) {
    setPrevLoading(loading)
    if (loading) setElapsed(false)
  }

  useEffect(() => {
    if (loading) {
      startedAtRef.current = Date.now()
      return
    }
    if (elapsed) return
    const remaining = Math.max(
      0,
      minimumTime - (Date.now() - startedAtRef.current)
    )
    const timer = setTimeout(() => setElapsed(true), remaining)
    return () => clearTimeout(timer)
  }, [loading, elapsed, minimumTime])

  return loading || !elapsed
}
