import { mount, flushPromises, RouterLinkStub } from '@vue/test-utils'
import { beforeEach, describe, it, expect, vi } from 'vitest'
import PoolPricingView from '../PoolPricingView.vue'
import locale from '@/i18n/locales/zh/pool'
const pricing = vi.hoisted(() => vi.fn())
vi.mock('@/api/poolOrders', async original => ({ ...await original<typeof import('@/api/poolOrders')>(), poolAPI: { pricing } }))
vi.mock('vue-i18n', async original => ({ ...await original<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string, params: Record<string, unknown> = {}) => {
 let value: unknown = locale
 for (const part of key.split('.')) value = (value as Record<string, unknown>)?.[part]
 return String(value || key).replace(/\{(\w+)\}/g, (_, name: string) => String(params[name] ?? ''))
} }) }))
const catalog = { version:'2026-09-09', source_url:'https://developers.openai.com/api/docs/pricing', tier:'standard', models:[
  {model:'gpt-5.4',input:2.5,cache_read:0.25,cache_write:null,output:15,long_context_threshold:272000},
  {model:'gpt-5.4-mini',input:0.75,cache_read:0.075,cache_write:null,output:4.5,long_context_threshold:0}
] }
function render() { return mount(PoolPricingView, {global: {stubs:{AppLayout:{template:'<main><slot/></main>'},RouterLink:RouterLinkStub}}}) }
describe('拼团价表', () => {
  beforeEach(() => { pricing.mockReset(); pricing.mockResolvedValue(catalog) })
  it('展示生效价格及来源，切换长上下文并支持搜索', async () => {
    const w=render();await flushPromises()
    expect(w.text()).toContain('2026-09-09');expect(w.get('a[target="_blank"]').attributes('href')).toBe(catalog.source_url)
    expect(w.get('tbody').text()).toContain('$2.50');expect(w.get('tbody').text()).toContain('—')
    const links=w.findAllComponents(RouterLinkStub).map(l=>l.props('to'))
    expect(links).toContain('/pool-orders');expect(links).toContain('/usage?usage_source=pool')
    await w.get('button[aria-pressed="false"]').trigger('click')
    expect(w.get('tbody').text()).toContain('$5.00');expect(w.get('tbody').text()).toContain('$22.50')
    expect(w.get('tbody').text()).not.toContain('gpt-5.4-mini')
    await w.get('input[type="search"]').setValue('missing')
    expect(w.text()).toContain('没有匹配的模型');w.unmount()
  })
  it('加载失败可重试，不展示虚构默认价', async () => {
    pricing.mockRejectedValueOnce(new Error('暂时无法加载价格'))
    const w=render();await flushPromises()
    expect(w.get('[role="alert"]').text()).toContain('暂时无法加载价格');expect(w.find('table').exists()).toBe(false)
    await w.get('button').trigger('click');await flushPromises()
    expect(w.find('table').exists()).toBe(true);expect(pricing).toHaveBeenCalledTimes(2);w.unmount()
  })
})
