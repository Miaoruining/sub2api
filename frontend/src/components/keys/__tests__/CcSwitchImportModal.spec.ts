import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import type { ApiKey } from '@/types'
import CcSwitchImportModal from '../CcSwitchImportModal.vue'

const { fetchManifest } = vi.hoisted(() => ({ fetchManifest: vi.fn() }))
vi.mock('@/api/codex', () => ({ fetchCodexModelsManifest: fetchManifest }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
const key = { id: 1, name: '工作密钥', key: 'test-key-do-not-use', status: 'active', group_id: 1,
  group: { id: 1, name: 'GPT Pro 满血', platform: 'openai' } } as ApiKey
const wrappers: ReturnType<typeof mount>[] = []
function render() {
  const wrapper = mount(CcSwitchImportModal, {
    props: { show: true, apiKey: key, baseUrl: 'https://example.com', siteName: 'ModelPort' },
    global: { stubs: { BaseDialog: { template: '<div><slot/><slot name="footer"/></div>' } } }
  })
  wrappers.push(wrapper)
  return wrapper
}
function catalog(slugs: string[]) {
  return { content: JSON.stringify({ models: slugs.map(slug => ({ slug })) }), modelCount: slugs.length }
}
afterEach(() => { wrappers.splice(0).forEach(wrapper => wrapper.unmount()); vi.restoreAllMocks(); fetchManifest.mockReset() })

describe('CC Switch import guide', () => {
  it('requires a verified model catalog for auto keys and allows selecting the client', async () => {
    fetchManifest.mockRejectedValue(new Error('Offline'))
    const wrapper = render()
    await wrapper.setProps({ apiKey: { ...key, auto_group: true, group_id: null, group: undefined } })
    await flushPromises()
    expect(wrapper.text()).not.toContain('keys.useKeyModal.noGroupDescription')
    await wrapper.get('#ccswitch-model').setValue('unverified-model')
    expect(wrapper.get('.btn-primary').attributes('disabled')).toBeDefined()
    await wrapper.get('select').setValue('anthropic')
    await flushPromises()
    expect(wrapper.get('.btn-primary').attributes('disabled')).toBeUndefined()
    expect(wrapper.find('#ccswitch-model').exists()).toBe(false)
  })

  it('imports the selected group model only on click, without leaking its key into the page', async () => {
    fetchManifest.mockResolvedValue(catalog(['gpt-5.6-luna', 'gpt-5.6-sol']))
    const open = vi.spyOn(window, 'open').mockReturnValue(null)
    const wrapper = render()
    await flushPromises()
    expect(wrapper.get('select').element.value).toBe('gpt-5.6-sol')
    expect(wrapper.html()).not.toContain(key.key)
    expect(open).not.toHaveBeenCalled()
    await wrapper.get('select').setValue('gpt-5.6-luna')
    await wrapper.get('.btn-primary').trigger('click')
    const params = new URL(String(open.mock.calls[0][0])).searchParams
    expect(params.get('model')).toBe('gpt-5.6-luna')
    expect(params.get('name')).toBe('ModelPort · GPT Pro 满血 · 工作密钥')
    expect(params.get('apiKey')).toBe(key.key)
    expect(params.has('usageScript')).toBe(false)
    expect(wrapper.text()).toContain('keys.ccSwitchGuide.launched')
    await wrapper.get('.btn-secondary').trigger('click')
    expect(wrapper.emitted('manual')).toHaveLength(1)
  })

  it('uses the first catalog model if the preferred model is unavailable', async () => {
    fetchManifest.mockResolvedValue(catalog(['group-only-model']))
    const wrapper = render()
    await flushPromises()
    expect(wrapper.get('select').element.value).toBe('group-only-model')
  })

  it('requires an explicit model after a failed lookup and permits retry', async () => {
    fetchManifest.mockRejectedValueOnce(new Error('Offline'))
    const wrapper = render()
    await flushPromises()
    expect(wrapper.get('.btn-primary').attributes('disabled')).toBeDefined()
    expect(wrapper.text()).toContain('keys.ccSwitchGuide.modelError')
    await wrapper.get('#ccswitch-model').setValue('verified-model')
    expect(wrapper.get('.btn-primary').attributes('disabled')).toBeUndefined()
    fetchManifest.mockResolvedValue(catalog(['recovered-model']))
    await wrapper.get('[role="status"] button').trigger('click')
    await flushPromises()
    expect(wrapper.get('select').element.value).toBe('recovered-model')
  })

  it('does not let a previous key request overwrite the next group selection', async () => {
    let finish!: (value: ReturnType<typeof catalog>) => void
    fetchManifest.mockReturnValueOnce(new Promise(resolve => { finish = resolve }))
    const wrapper = render()
    fetchManifest.mockResolvedValueOnce(catalog(['plus-model']))
    await wrapper.setProps({ apiKey: { ...key, id: 2, key: 'plus-test-key', group_id: 2, group: { ...key.group!, id: 2, name: 'GPT Plus' } } })
    await flushPromises()
    finish(catalog(['old-pro-model']))
    await flushPromises()
    expect(wrapper.get('select').element.value).toBe('plus-model')
    await wrapper.setProps({ apiKey: { ...key, status: 'inactive' } })
    expect(wrapper.get('.btn-primary').attributes('disabled')).toBeDefined()
  })
})
