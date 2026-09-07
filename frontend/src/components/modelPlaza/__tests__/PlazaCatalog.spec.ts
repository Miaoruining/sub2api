import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import PlazaCatalog from '../PlazaCatalog.vue'
import type { ModelPlazaGroup } from '@/api/modelPlaza'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ cachedPublicSettings: null }) }))

function group(id: number, platform: string, name: string): ModelPlazaGroup {
  return { id, name: `Route ${id}`, platform, description: '', subscription_type: 'standard', rate_multiplier: id,
    peak_rate_enabled: false, peak_start: '', peak_end: '', peak_rate_multiplier: 1, is_exclusive: false,
    image_rate_independent: false, image_rate_multiplier: 1, long_context_pricing_enabled: true,
    models: [{ name, platform, pricing: null, official_pricing: null }] }
}
describe('PlazaCatalog', () => {
  it('keeps all model groups in details after selecting a single group and orders provider colors', async () => {
    const wrapper = mount(PlazaCatalog, {
      props: { groups: [group(4, 'grok', 'grok-test'), group(3, 'gemini', 'gemini-test'), group(2, 'anthropic', 'claude-test'), group(1, 'openai', 'gpt-test'), group(5, 'openai', 'gpt-test')] },
      global: { stubs: { PlazaModelDrawer: { props: ['show', 'entry'], template: '<div v-if="show" data-test="detail">{{ entry.routes.length }}</div>' } } }
    })
    const chips = wrapper.findAll('aside section')[0]!.findAll('button')
    expect(chips.slice(1).map(chip => chip.text().split('x')[0]?.trim())).toEqual(['Route 1', 'Route 5', 'Route 2', 'Route 3', 'Route 4'])
    expect(chips[1]!.classes()).toContain('text-green-700')
    expect(chips[3]!.classes()).toContain('text-red-700')
    expect(chips[4]!.classes()).toContain('text-blue-700')
    expect(chips[5]!.classes()).toContain('text-purple-700')
    await chips[1]!.trigger('click')
    await wrapper.get('article .details-button').trigger('click')
    expect(wrapper.get('[data-test=detail]').text()).toBe('2')
    wrapper.unmount()
  })
  it('searches unique model cards, resets filters and opens the model drawer', async () => {
    const wrapper = mount(PlazaCatalog, {
      props: { groups: [group(1, 'openai', 'gpt-test'), group(2, 'openai', 'gpt-test'), group(3, 'anthropic', 'claude-test')] },
      global: { stubs: { PlazaModelDrawer: { props: ['show', 'entry'], template: '<div v-if="show" data-test="detail">{{ entry.routes.length }}</div>' } } }
    })
    expect(wrapper.findAll('article')).toHaveLength(2)
    await wrapper.get('input[type=search]').setValue('gpt')
    expect(wrapper.findAll('article')).toHaveLength(1)
    const details = wrapper.get('article .details-button')
    await details.trigger('click')
    expect(wrapper.get('[data-test=detail]').text()).toBe('2')
    await wrapper.get('[data-test=reset]').trigger('click')
    expect(wrapper.findAll('article')).toHaveLength(2)
    await wrapper.get('input[type=search]').setValue('missing-model')
    expect(wrapper.findAll('article')).toHaveLength(0)
    expect(wrapper.text()).toContain('modelPlaza.noSearchResult')
  })
})
