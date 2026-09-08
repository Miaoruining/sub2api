import { effectScope } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { lotteryRing, useLotteryReveal } from '../useLotteryReveal'

describe('useLotteryReveal', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.stubGlobal('matchMedia', vi.fn(() => ({ matches: false, addEventListener: vi.fn(), removeEventListener: vi.fn() })))
  })
  afterEach(() => { vi.useRealTimers(); vi.unstubAllGlobals() })
  it('has eight distinct adjacent ring positions and exactly two zero slots', () => {
    expect(lotteryRing).toHaveLength(8)
    expect(new Set(lotteryRing.map(slot => `${slot.x},${slot.y}`)).size).toBe(8)
    expect(lotteryRing.filter(slot => slot.prize === 0)).toHaveLength(2)
    lotteryRing.forEach((slot, index) => {
      const next = lotteryRing[(index + 1) % lotteryRing.length]
      expect(Math.abs(slot.x - next.x) + Math.abs(slot.y - next.y)).toBe(1)
      expect(slot.x === 1 && slot.y === 1).toBe(false)
    })
  })
  it.each([0, 1800, 9000])('glides through adjacent slots and decelerates onto zero after %s ms latency', async latency => {
    const scope = effectScope(); const reveal = scope.run(useLotteryReveal)!
    reveal.start(); await vi.advanceTimersByTimeAsync(latency)
    const done = reveal.finish(0)
    let last = reveal.activeSlot.value!
    let duration = 0
    for (let steps = 0; steps < 100 && reveal.phase.value !== 'revealed'; steps++) {
      await vi.advanceTimersToNextTimerAsync()
      const current = reveal.activeSlot.value!
      if (reveal.phase.value === 'revealed') {
        expect(current).toBe(last) // 揭晓不能瞬移到另一无奖格。
        break
      }
      expect(current).toBe((last + 1) % 8)
      if (reveal.phase.value === 'settling') {
        expect(reveal.stepDuration.value).toBeGreaterThanOrEqual(duration)
        duration = reveal.stepDuration.value
      }
      last = current
    }
    expect(await done).toBe(true)
    expect([2, 6]).toContain(reveal.activeSlot.value)
    expect(duration).toBe(400)
    scope.stop()
  })
  it('can stop at either zero slot depending on the current ring position', async () => {
    const positions = new Set<number | null>()
    for (const latency of [1800, 2160]) {
      const scope = effectScope(); const reveal = scope.run(useLotteryReveal)!
      reveal.start(); await vi.advanceTimersByTimeAsync(latency)
      const done = reveal.finish(0); await vi.advanceTimersByTimeAsync(6000)
      expect(await done).toBe(true); positions.add(reveal.activeSlot.value); scope.stop()
    }
    expect([...positions].sort()).toEqual([2, 6])
  })
  it.each([0, 1, 5, 10, 20, 50, 100])('settles only on confirmed prize %s', async prize => {
    const scope = effectScope()
    const reveal = scope.run(useLotteryReveal)!
    reveal.start()
    const done = reveal.finish(prize)
    await vi.advanceTimersByTimeAsync(1300)
    expect(reveal.phase.value).toBe('spinning')
    await vi.advanceTimersByTimeAsync(6000)
    expect(await done).toBe(true)
    expect(reveal.phase.value).toBe('revealed')
    expect(reveal.activePrize.value).toBe(prize)
    scope.stop(); expect(vi.getTimerCount()).toBe(0)
  })
  it('does not fabricate a result during a slow network request', async () => {
    const scope = effectScope(); const reveal = scope.run(useLotteryReveal)!
    reveal.start(); await vi.advanceTimersByTimeAsync(12000)
    expect(reveal.phase.value).toBe('spinning')
    reveal.cancel(); expect(reveal.activePrize.value).toBeNull()
    expect(vi.getTimerCount()).toBe(0); scope.stop()
  })
  it('resolves pending reveal cancellation on unmount', async () => {
    const scope = effectScope(); const reveal = scope.run(useLotteryReveal)!
    reveal.start(); const done = reveal.finish(1)
    scope.stop(); expect(await done).toBe(false)
    expect(vi.getTimerCount()).toBe(0)
  })
  it('reveals immediately without moving highlights when reduced motion is enabled', async () => {
    vi.stubGlobal('matchMedia', vi.fn(() => ({ matches: true, addEventListener: vi.fn(), removeEventListener: vi.fn() })))
    const scope = effectScope(); const reveal = scope.run(useLotteryReveal)!
    reveal.start(); expect(reveal.activePrize.value).toBeNull()
    expect(await reveal.finish(5)).toBe(true)
    expect(reveal.activePrize.value).toBe(5)
    expect(vi.getTimerCount()).toBe(0); scope.stop()
  })
})
