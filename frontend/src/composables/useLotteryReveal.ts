import { computed, onScopeDispose, readonly, ref } from 'vue'

// 仅负责展示，目标奖项必须来自服务器；不在客户端生成或修改开奖结果。
// 顺时针绕过中心。两个无奖格仅为展示位置，不改变服务端的无奖概率。
export const lotteryRing = [
  { prize: 1, x: 0, y: 0 }, { prize: 5, x: 1, y: 0 },
  { prize: 0, x: 2, y: 0 }, { prize: 10, x: 2, y: 1 },
  { prize: 20, x: 2, y: 2 }, { prize: 50, x: 1, y: 2 },
  { prize: 0, x: 0, y: 2 }, { prize: 100, x: 0, y: 1 },
] as const
const route = lotteryRing.map(slot => slot.prize as number)
const minimumSpin = 1400

export function useLotteryReveal() {
  const phase = ref<'idle' | 'spinning' | 'settling' | 'revealed'>('idle')
  const activeSlot = ref<number | null>(null)
  const activePrize = computed(() => activeSlot.value === null ? null : route[activeSlot.value])
  const stepDuration = ref(0)
  const media = typeof window.matchMedia === 'function' ? window.matchMedia('(prefers-reduced-motion: reduce)') : null
  let reduced = media?.matches ?? false
  let timer: ReturnType<typeof setTimeout> | undefined
  let startedAt = 0
  let index = 0
  let target: number | null = null
  let targetSlot = 0
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
    activeSlot.value = targetSlot
    phase.value = 'revealed'
    resolveFinish?.(true)
    resolveFinish = undefined
  }
  function tick() {
    if (disposed || reduced) return
    if (phase.value === 'spinning' && target !== null && Date.now() - startedAt >= minimumSpin) {
      phase.value = 'settling'
      // 完整一圈后停在顺时针最近的匹配格，无奖不跳格、不瞬移。
      let distance = 1
      while (route[(index + distance) % route.length] !== target) distance++
      targetSlot = (index + distance) % route.length
      remaining = route.length + distance
      totalSteps = remaining
    }
    const progress = totalSteps ? 1 - (remaining - 1) / totalSteps : 0
    stepDuration.value = phase.value === 'settling' ? Math.round(90 + 310 * progress ** 2) : 90
    index = (index + 1) % route.length
    activeSlot.value = index
    if (phase.value === 'settling') {
      remaining--
      if (remaining === 0) {
        // 等滑块抵达最后一格，再停留片刻揭晓结果。
        timer = setTimeout(complete, stepDuration.value + 180)
        return
      }
    }
    timer = setTimeout(tick, stepDuration.value)
  }
  function cancel() {
    clearTimer()
    resolveFinish?.(false)
    resolveFinish = undefined
    target = null
    activeSlot.value = null
    stepDuration.value = 0
    phase.value = 'idle'
  }
  function start() {
    cancel()
    if (disposed) return
    phase.value = 'spinning'
    startedAt = Date.now()
    index = 0
    remaining = totalSteps = 0
    activeSlot.value = reduced ? null : index
    if (!reduced) timer = setTimeout(tick, 90)
  }
  function finish(prize: number): Promise<boolean> {
    if (disposed || !route.includes(prize)) { cancel(); return Promise.resolve(false) }
    target = prize
    targetSlot = route.indexOf(prize)
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
      else activeSlot.value = null
    } else timer = setTimeout(tick, 90)
  }
  media?.addEventListener('change', motionChanged)
  onScopeDispose(() => {
    disposed = true
    cancel()
    media?.removeEventListener('change', motionChanged)
  })
  return { phase: readonly(phase), activePrize, activeSlot: readonly(activeSlot), stepDuration: readonly(stepDuration), start, finish, cancel }
}
