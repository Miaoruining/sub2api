import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import PlazaModelCard from '../PlazaModelCard.vue'
import { buildModelCatalog } from '../catalog'
import type { ModelPlazaGroup } from '@/api/modelPlaza'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

function entry() {
  const group: ModelPlazaGroup = {
    id: 1, name: 'test', platform: 'openai', description: '', subscription_type: 'standard',
    rate_multiplier: 2, peak_rate_enabled: false, peak_start: '', peak_end: '', peak_rate_multiplier: 1,
    is_exclusive: false, image_rate_independent: false, image_rate_multiplier: 1, long_context_pricing_enabled: true,
    models: [{ name: 'test-model', platform: 'openai', official_pricing: null, pricing: {
      billing_mode: 'token', input_price: 0.000001, output_price: 0.000002, cache_write_price: null,
      cache_read_price: 0, image_input_price: null, image_output_price: null, per_request_price: null, intervals: []
    } }]
  }
  return buildModelCatalog([group])[0]!
}

describe('PlazaModelCard', () => {
  it('uses real group rates, preserves zero and omits absent cache prices', () => {
    const wrapper = mount(PlazaModelCard, { props: { entry: entry() } })
    expect(wrapper.get('dl').text()).toContain('$2')
    expect(wrapper.get('dl').text()).toContain('$4')
    expect(wrapper.get('dl').text()).toContain('$0')
    expect(wrapper.get('dl').text()).toContain('cacheShort')
    expect(wrapper.findAll('.capability-tags > span')).toHaveLength(1)
    expect(wrapper.get('.token-unit').text()).toBe('1M')
  })

  it('keeps details and copy actions independent', async () => {
    const wrapper = mount(PlazaModelCard, { props: { entry: entry() } })
    await wrapper.get('.details-button').trigger('click')
    await wrapper.get('.copy-button').trigger('click')
    expect(wrapper.emitted('details')).toHaveLength(1)
    expect(wrapper.emitted('copy')).toEqual([['test-model']])
  })

  it('renders configured metadata as text, without guessing capabilities', () => {
    const model = entry()
    Object.assign(model.routes[0]!.model, { description: '<script>not HTML</script>', tags: ['对话', '工具'] })
    const wrapper = mount(PlazaModelCard, { props: { entry: model } })
    expect(wrapper.get('.model-description').text()).toBe('<script>not HTML</script>')
    expect(wrapper.find('script').exists()).toBe(false)
    expect(wrapper.findAll('.capability-tags > span')).toHaveLength(3)
  })

  it('does not substitute a made-up or official price when actual prices are missing', () => {
    const model = entry()
    model.routes[0]!.model.pricing = null
    const wrapper = mount(PlazaModelCard, { props: { entry: model } })
    expect(wrapper.get('dl').text()).not.toContain('$')
    expect(wrapper.get('dl').text()).toContain('modelPlaza.catalog.unknown')
  })
})
