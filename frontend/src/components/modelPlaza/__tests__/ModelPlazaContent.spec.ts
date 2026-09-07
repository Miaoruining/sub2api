import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import ModelPlazaContent from '../ModelPlazaContent.vue'
import PlazaOriginalCatalog from '../PlazaOriginalCatalog.vue'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ isAuthenticated: true }) }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ cachedPublicSettings: null }) }))

describe('ModelPlazaContent site-wide style', () => {
  it('defaults to cards and switches to the original renderer without changing the data', async () => {
    const groups: never[] = []
    const wrapper = mount(ModelPlazaContent, {
      props: { response: { description: '', groups }, loading: false },
      global: { stubs: { PlazaCatalog: { props: ['groups'], template: '<div data-test="cards" />' }, PlazaOriginalCatalog: { props: ['groups'], template: '<div data-test="original" />' } } }
    })
    expect(wrapper.find('[data-test=cards]').exists()).toBe(true)
    await wrapper.setProps({ response: { description: '', groups, style: 'sub2api' } })
    expect(wrapper.find('[data-test=cards]').exists()).toBe(false)
    expect(wrapper.find('[data-test=original]').exists()).toBe(true)
    expect(wrapper.findComponent(PlazaOriginalCatalog).props('groups')).toEqual(groups)
    await wrapper.setProps({ response: { description: '', groups, style: 'cards' } })
    expect(wrapper.find('[data-test=cards]').exists()).toBe(true)
    wrapper.unmount()
  })
})
