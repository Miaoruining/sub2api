import { onScopeDispose, readonly, ref } from 'vue'

// 仅负责展示，目标奖项必须来自服务器；不在客户端生成或修改开奖结果。
const route = [1, 5, 10, 100, 50, 20, 0]
const minimumSpin = 1400

export function useLotteryReveal() {
  const phase = ref<'idle' | 'spinning' | 'settling' | 'revealed'>('idle')
  const activePrize = ref<number | null>(null)
  const media = typeof window.matchMedia === 'function' ? window.matchMedia('(prefers-reduced-motion: reduce)') : null
  let reduced = media?.matches ?? false
  let timer: ReturnType<typeof setTimeout> | undefined
  let startedAt = 0
  let index = 0
  let target: number | null = null
  let remaining = 0
  let totalSteps = 0
  let disposed = false
  let resolveFinish: ((completed: boolean) => void) | undefined

  function clearTimer() {
    if (timer !== undefined) clearTimeout(timer)
    timer = undefined
  }
  function complete() {
    clearTimer()
    activePrize.value = target
    phase.value = 'revealed'
    resolveFinish?.(true)
    resolveFinish = undefined
  }
  function tick() {
    if (disposed || reduced) return
    if (phase.value === 'spinning' && target !== null && Date.now() - startedAt >= minimumSpin) {
      phase.value = 'settling'
      remaining = route.length + (route.indexOf(target) - index + route.length) % route.length
      totalSteps = remaining
    }
    index = (index + 1) % route.length
    activePrize.value = route[index]
    if (phase.value === 'settling') {
      remaining--
      if (remaining === 0) {
        timer = setTimeout(complete, 260)
        return
      }
    }
    const progress = totalSteps ? 1 - remaining / totalSteps : 0
    timer = setTimeout(tick, phase.value === 'settling' ? 100 + 230 * progress ** 2 : 90)
  }
  function cancel() {
    clearTimer()
    resolveFinish?.(false)
    resolveFinish = undefined
    target = null
    activePrize.value = null
    phase.value = 'idle'
  }
  function start() {
    cancel()
    if (disposed) return
    phase.value = 'spinning'
    startedAt = Date.now()
    index = 0
    remaining = totalSteps = 0
    activePrize.value = reduced ? null : route[index]
    if (!reduced) timer = setTimeout(tick, 90)
  }
  function finish(prize: number): Promise<boolean> {
    if (disposed || !route.includes(prize)) { cancel(); return Promise.resolve(false) }
    target = prize
    return new Promise(resolve => {
      resolveFinish = resolve
      if (reduced) complete()
    })
  }
  function motionChanged(event: MediaQueryListEvent) {
    reduced = event.matches
    if (phase.value !== 'spinning' && phase.value !== 'settling') return
    clearTimer()
    if (reduced) {
      if (target !== null) complete()
      else activePrize.value = null
    } else timer = setTimeout(tick, 90)
  }
  media?.addEventListener('change', motionChanged)
  onScopeDispose(() => {
    disposed = true
    cancel()
    media?.removeEventListener('change', motionChanged)
  })
  return { phase: readonly(phase), activePrize: readonly(activePrize), start, finish, cancel }
}
