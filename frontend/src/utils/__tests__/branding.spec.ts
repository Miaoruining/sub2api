import { beforeEach, describe, expect, it } from 'vitest'
import { updateFavicon } from '@/utils/branding'

describe('updateFavicon', () => {
  beforeEach(() => {
    document.head.innerHTML = '<link rel="icon" href="/logo.svg">'
  })

  it('replaces the default favicon with the configured logo', () => {
    updateFavicon('https://example.com/custom-logo.png')

    const link = document.querySelector<HTMLLinkElement>('link[rel="icon"]')
    expect(link?.href).toBe('https://example.com/custom-logo.png')
  })

  it('ignores unsafe logo URLs', () => {
    updateFavicon('javascript:alert(1)')

    const link = document.querySelector<HTMLLinkElement>('link[rel="icon"]')
    expect(link?.getAttribute('href')).toBe('/logo.svg')
  })

  it('preserves the server favicon for the injected logo and updates it when branding changes', () => {
    const link = document.querySelector<HTMLLinkElement>('link[rel="icon"]')!
    link.href = '/branding/logo/current-v1-48.png'
    link.type = 'image/png'
    link.dataset.brandingLogo = '/branding/logo/current-v1-256.png'

    updateFavicon('/branding/logo/current-v1-256.png')
    expect(link.getAttribute('href')).toBe('/branding/logo/current-v1-48.png')
    expect(link.type).toBe('image/png')

    updateFavicon('/next-logo.svg')
    expect(link.getAttribute('href')).toBe('/next-logo.svg')
    expect(link.type).toBe('image/svg+xml')
    expect(link.dataset.brandingLogo).toBeUndefined()
  })
})
