import { describe, expect, it } from 'vitest'
import entry from './LotteryEntry.vue?raw'
import header from '../layout/AppHeader.vue?raw'

describe('Lottery header entry', () => {
  it('sits immediately to the left of the balance display', () => {
    const link = header.indexOf('<LotteryEntry')
    expect(link).toBeGreaterThan(0)
    expect(header.slice(link, header.indexOf('<!-- Balance Display -->'))).not.toContain('<div')
    expect(entry).toContain('to="/lottery"')
    expect(entry).toContain('background: #c72d40')
  })
  it('uses a slow breathing effect with reduced-motion support', () => {
    expect(entry).toContain('lottery-breathe 2.8s ease-in-out infinite')
    expect(entry).toContain('prefers-reduced-motion: reduce')
    expect(entry).toContain('animation: none')
    expect(entry).toContain(':focus-visible')
  })
})
