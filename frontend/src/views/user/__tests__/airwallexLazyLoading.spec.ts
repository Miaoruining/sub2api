// @vitest-environment node

import { describe, expect, it } from 'vitest'
import viteConfig from '../../../../vite.config'

function getManualChunks() {
  const config = typeof viteConfig === 'function'
    ? viteConfig({ command: 'build', mode: 'production' })
    : viteConfig
  const manualChunks = config.build?.rollupOptions?.output?.manualChunks
  if (typeof manualChunks !== 'function') {
    throw new Error('Expected Vite manualChunks to be a function')
  }
  return manualChunks as (id: string) => string | undefined
}

describe('Airwallex lazy-loading contract', () => {
  it('keeps the SDK and its scoped dependency in the deferred vendor chunk', () => {
    const manualChunks = getManualChunks()

    expect(manualChunks('/workspace/node_modules/@airwallex/components-sdk/lib/index.mjs'))
      .toBe('vendor-airwallex')
    expect(manualChunks('/workspace/node_modules/@airwallex/airtracker/lib/index.mjs'))
      .toBe('vendor-airwallex')
    expect(manualChunks('/workspace/node_modules/axios/index.js')).toBe('vendor-misc')
    expect(manualChunks('/workspace/src/main.ts')).toBeUndefined()
  })
})
