import { mount, flushPromises } from '@vue/test-utils'
import { beforeEach, describe, it, expect, vi } from 'vitest'
import PoolResourcesView from '../PoolResourcesView.vue'
const api = vi.hoisted(() => ({ resources: vi.fn(), createResource: vi.fn(), setResourceStatus: vi.fn() }))
vi.mock('@/api/poolOrders', async original => ({ ...await original<typeof import('@/api/poolOrders')>(), poolAPI: api }))
vi.mock('vue-i18n', async original => ({ ...await original<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))
function render() { return mount(PoolResourcesView, { global: { stubs: { AppLayout: { template: '<main><slot/></main>' }, RouterLink: { template: '<a><slot/></a>' } } } }) }
describe('独立拼单号池', () => {
 beforeEach(() => { vi.clearAllMocks(); api.resources.mockResolvedValue([]); api.createResource.mockResolvedValue(undefined) })
 it('非法凭据不提交并显示错误', async () => { const w = render(); await flushPromises(); await w.get('textarea').setValue('invalid'); await w.get('form').trigger('submit'); await flushPromises(); expect(api.createResource).not.toHaveBeenCalled(); expect(w.get('[role="alert"]').text()).toBe('pool.invalidCredentials'); w.unmount() })
 it('创建成功后立即清空凭据，不在账号列表显示凭据', async () => { const w = render(); await flushPromises(); await w.get('input').setValue('独立账号'); await w.get('textarea').setValue('{"access_token":"test-only"}'); await w.get('form').trigger('submit'); await flushPromises(); expect(api.createResource).toHaveBeenCalledWith({ name: '独立账号', type: 'oauth', concurrency: 3, credentials: { access_token: 'test-only' } }); expect((w.get('textarea').element as HTMLTextAreaElement).value).toBe(''); expect(w.text()).not.toContain('test-only'); w.unmount() })
})
