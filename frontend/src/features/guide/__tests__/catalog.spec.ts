import { describe, expect, it } from 'vitest'
import { getGuideCatalog } from '../catalog'

describe('guide catalog', () => {
  it('provides separate Codex desktop guides for macOS and Windows', () => {
    const { articles } = getGuideCatalog('zh-CN')

    const macGuide = articles.find((article) => article.slug === 'codex-desktop-macos')
    const windowsGuide = articles.find((article) => article.slug === 'codex-desktop-windows')

    expect(macGuide?.os).toBe('macOS')
    expect(windowsGuide?.os).toBe('Windows')
    expect(macGuide?.actions[0].href).toMatch(/^https:\/\/learn\.chatgpt\.com\//)
    expect(windowsGuide?.actions[0].href).toMatch(/^https:\/\/learn\.chatgpt\.com\//)
  })

  it('does not place API keys or fixed release versions in guide content', () => {
    const serialized = JSON.stringify(getGuideCatalog('zh-CN'))

    expect(serialized).not.toMatch(/sk-[a-zA-Z0-9]{16,}/)
    expect(serialized).not.toMatch(/CC Switch v\d+\.\d+\.\d+/)
  })

  it('keeps every action on an internal route or an HTTPS URL', () => {
    const { articles } = getGuideCatalog('zh-CN')
    const actions = articles.flatMap((article) => article.actions)

    expect(actions.length).toBeGreaterThan(0)
    for (const action of actions) {
      expect(action.href.startsWith('/') || action.href.startsWith('https://')).toBe(true)
    }
  })
})
