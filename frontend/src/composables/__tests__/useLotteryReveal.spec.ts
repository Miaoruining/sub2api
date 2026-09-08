import { effectScope } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { useLotteryReveal } from '../useLotteryReveal'

describe('useLotteryReveal', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.stubGlobal('matchMedia', vi.fn(() => ({ matches: false, addEventListener: vi.fn(), removeEventListener: vi.fn() })))
  })
  afterEach(() => { vi.useRealTimers(); vi.unstubAllGlobals() })
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
