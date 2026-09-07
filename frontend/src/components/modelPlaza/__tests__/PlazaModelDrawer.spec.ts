import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import type { ModelPlazaGroup, PlazaModel } from '@/api/modelPlaza'
import PlazaModelDrawer from '../PlazaModelDrawer.vue'
import { buildModelCatalog } from '../catalog'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

function model(order?: number): PlazaModel {
  return {
    name: 'claude-test', platform: 'anthropic', official_pricing: null, auto_route_order: order,
    pricing: { billing_mode: 'token', input_price: 0.000002, output_price: 0.000006,
      cache_read_price: 0.0000002, cache_write_price: 0.0000025, image_input_price: null,
      image_output_price: null, per_request_price: null, intervals: [] }
  }
}

function group(id: number, name: string, order?: number): ModelPlazaGroup {
  return {
    id, name, platform: 'anthropic', description: '', sort_order: id,
    subscription_type: 'standard', is_exclusive: false, rate_multiplier: id,
    peak_rate_enabled: false, peak_start: '', peak_end: '', peak_rate_multiplier: 1,
    image_rate_independent: false, image_rate_multiplier: 1, long_context_pricing_enabled: true,
    models: [model(order)]
  }
}

describe('PlazaModelDrawer', () => {
  it('shows every visible group price while keeping ineligible groups out of the route chain', async () => {
    const entry = buildModelCatalog([
      group(1, 'second', 1),
      group(2, 'display only'),
      group(3, 'first', 0)
    ])[0]!
    const wrapper = mount(PlazaModelDrawer, {
      props: { show: true, entry },
      global: { stubs: { Teleport: true } }
    })
    const rows = wrapper.findAll('[data-test=route-row]')
    expect(rows).toHaveLength(3)
    expect(rows[0]!.text()).toContain('first')
    expect(rows[0]!.text()).toContain('$6')
    expect(rows[1]!.text()).toContain('second')
    expect(rows[2]!.text()).toContain('display only')
    expect(rows[2]!.text()).toContain('modelPlaza.catalog.displayOnly')
    expect(wrapper.get('[data-test=route-chain]').text()).not.toContain('display only')
    await wrapper.get('button[aria-label="modelPlaza.catalog.close"]').trigger('click')
    expect(wrapper.emitted('close')).toHaveLength(1)
  })
})
