import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import PlazaOriginalCatalog from '../PlazaOriginalCatalog.vue'
import type { ModelPlazaGroup, PlazaModel } from '@/api/modelPlaza'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ cachedPublicSettings: null }) }))

function sourceModel(name: string, rate: number): PlazaModel {
  return {
    name,
    platform: 'openai',
    source_group_id: rate === 0.16 ? 16 : 23,
    rate_multiplier: rate,
    pricing: null,
    official_pricing: null
  }
}

function group(): ModelPlazaGroup {
  return {
    id: 99,
    name: 'composite',
    platform: 'openai',
    description: '',
    subscription_type: 'standard',
    rate_multiplier: 1,
    peak_rate_enabled: false,
    peak_start: '',
    peak_end: '',
    peak_rate_multiplier: 1,
    is_exclusive: false,
    image_rate_independent: false,
    image_rate_multiplier: 1,
    long_context_pricing_enabled: true,
    models: [sourceModel('gpt-016', 0.16), sourceModel('gpt-023', 0.23)]
  }
}

describe('PlazaOriginalCatalog source model rate filter', () => {
  it('can select each source rate and leaves the entry group identity intact', async () => {
    const wrapper = mount(PlazaOriginalCatalog, {
      props: { groups: [group()] },
      global: {
        stubs: {
          PlazaGroupSection: {
            props: ['group'],
            template: '<div data-test="group">{{ group.id }}:{{ group.models.map(m => m.name).join(",") }}</div>'
          }
        }
      }
    })

    const rateButtons = wrapper.findAll('button').filter(button => ['0.16x', '0.23x'].includes(button.text()))
    expect(rateButtons).toHaveLength(2)
    await rateButtons[0]!.trigger('click')
    expect(wrapper.get('[data-test="group"]').text()).toBe('99:gpt-016')
    await rateButtons[1]!.trigger('click')
    expect(wrapper.get('[data-test="group"]').text()).toBe('99:gpt-023')
  })
})
