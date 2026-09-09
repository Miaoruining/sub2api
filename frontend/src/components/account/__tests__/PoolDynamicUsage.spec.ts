import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import PoolDynamicUsage from '../PoolDynamicUsage.vue'
import locale from '@/i18n/locales/zh/pool'

const dynamicUsage = vi.hoisted(() => vi.fn())
vi.mock('@/api/poolOrders', async (original) => ({
  ...await original<typeof import('@/api/poolOrders')>(),
  poolAPI: {
    ...(await original<typeof import('@/api/poolOrders')>()).poolAPI,
    dynamicUsage,
  },
}))
vi.mock('vue-i18n', async (original) => ({
  ...await original<typeof import('vue-i18n')>(),
  useI18n: () => ({
    t: (key: string, params: Record<string, unknown> = {}) => {
      let value: unknown = locale
      for (const part of key.split('.')) value = (value as Record<string, unknown>)?.[part]
      return String(value || key).replace(/\{(\w+)\}/g, (_, name: string) => String(params[name] ?? ''))
    },
  }),
}))

const window = (key: string, values: Partial<Record<string, unknown>> = {}) => ({
  key,
  account_remaining_percent: 70,
  used_percent: 30,
  reserved_percent: 2,
  remaining_percent: 18,
  entitlement_percent: 40,
  reset_at: '2030-01-01T00:00:00Z',
  observed_at: '2029-12-31T00:00:00Z',
  status: 'calibrated',
  ...values,
})

describe('PoolDynamicUsage', () => {
  beforeEach(() => {
    dynamicUsage.mockReset()
    dynamicUsage.mockResolvedValue([
      { id: 'a', order_id: 1, key_id: 10, credit: 0.1234, status: 'settled', created_at: '2030-01-01T00:00:00Z', windows: [window('codex/300', { used_percent: 12.5 })] },
      { id: 'b', order_id: 2, key_id: 11, credit: 0.2, status: 'pending', created_at: '2030-01-02T00:00:00Z', windows: [window('codex/10080')] },
    ])
  })

  it('loads only the pool endpoint and filters by the existing key filter', async () => {
    const wrapper = mount(PoolDynamicUsage, { props: { keyId: 10 } })
    await flushPromises()

    expect(dynamicUsage).toHaveBeenCalledTimes(1)
    expect(wrapper.text()).toContain('拼单 #1')
    expect(wrapper.text()).not.toContain('拼单 #2')
    expect(wrapper.text()).toContain('12.5 pp')
    expect(wrapper.text()).toContain('估算中')
    expect(wrapper.text()).toContain('整号百分点（pp）')
  })

  it('keeps absent window values unknown instead of showing a zero allowance', async () => {
    dynamicUsage.mockResolvedValueOnce([{
      id: 'missing', order_id: 3, key_id: 12, credit: 0, status: 'uncertain', created_at: '2030-01-03T00:00:00Z',
      windows: [window('codex/300', { account_remaining_percent: null, used_percent: null, reserved_percent: null, remaining_percent: null, entitlement_percent: null })],
    }])
    const wrapper = mount(PoolDynamicUsage)
    await flushPromises()

    expect(wrapper.text()).toContain('动态窗口尚在同步')
    expect(wrapper.text()).not.toContain('0 pp')
    expect(wrapper.text()).toContain('待核对')
  })

  it('keeps small percentage point values visible instead of rounding them to zero', async () => {
    dynamicUsage.mockResolvedValueOnce([{
      id: 'small', order_id: 4, key_id: 13, credit: 0.001, status: 'calibrated', created_at: '2030-01-04T00:00:00Z',
      windows: [window('codex/300', { account_remaining_percent: 99.9975, used_percent: 0.0025, reserved_percent: 0.0002, remaining_percent: 0.0025, entitlement_percent: 0.005 })],
    }])
    const wrapper = mount(PoolDynamicUsage)
    await flushPromises()

    expect(wrapper.text()).toContain('0.0025 pp')
    expect(wrapper.text()).not.toContain('0 pp')
  })
})
