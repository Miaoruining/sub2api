import { mount, flushPromises } from '@vue/test-utils'
import { beforeEach, afterEach, describe, it, expect, vi } from 'vitest'
import PoolOrdersView from '../PoolOrdersView.vue'
import locale from '@/i18n/locales/zh/pool'
import type { PoolOrder } from '@/api/poolOrders'
const api = vi.hoisted(() => ({ list: vi.fn(), act: vi.fn(), resources: vi.fn(), create: vi.fn(), products: vi.fn(), saveProduct: vi.fn(), purchase: vi.fn(), refreshUser: vi.fn() }))
vi.mock('@/api/poolOrders', async original => ({ ...await original<typeof import('@/api/poolOrders')>(), poolAPI: api }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ refreshUser: api.refreshUser }) }))
vi.mock('vue-i18n', async original => ({ ...await original<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string, params: Record<string, unknown> = {}) => {
 let value: unknown = locale
 for (const part of key.split('.')) value = (value as Record<string, unknown>)?.[part]
 return String(value || key).replace(/\{(\w+)\}/g, (_, name: string) => String(params[name] ?? ''))
} }) }))
function order(overrides: Partial<PoolOrder> = {}): PoolOrder { return { id: 1, title: '测试拼单', group_id: 10, seats: 4, price: 10, duration_hours: 720, total_tokens: 4000000, total_requests: 4000, concurrency: 1, join_deadline: '2030-01-01T00:00:00Z', starts_at: null, expires_at: null, status: 'forming', joined: 1, tokens_used: 0, requests_used: 0, reserved_tokens: 0, mine: null, shared_account: null, ...overrides } }
function render(admin = false) { return mount(PoolOrdersView, { props: {admin}, global: { stubs: { AppLayout: { template: '<main><slot/></main>' }, BaseDialog: { props: ['show', 'title'], template: '<section v-if="show" role="dialog"><h2>{{title}}</h2><slot/><slot name="footer"/></section>' }, RouterLink: { template: '<a><slot/></a>' } } } }) }
describe('拼单席位和个人额度', () => {
 beforeEach(() => { vi.clearAllMocks(); api.list.mockResolvedValue([order()]); api.products.mockResolvedValue([]); api.act.mockResolvedValue(undefined); api.refreshUser.mockResolvedValue(undefined) })
 afterEach(() => vi.restoreAllMocks())
 it('发布不选择号池，待发货成员不能提前领取 Key', async () => {
  const admin = render(true); await flushPromises(); expect(admin.text()).toContain('发货后有效天数'); expect(admin.text()).not.toContain('720'); expect(api.resources).not.toHaveBeenCalled(); admin.unmount()
  api.list.mockResolvedValue([order({status:'awaiting_delivery',delivery_deadline:'2030-01-01T00:00:00Z',mine:{id:2,status:'joined',key_id:null,paid:10,refunded:0,tokens_used:0,requests_used:0,reserved_tokens:0,inflight:0}})])
  const member=render(); await flushPromises(); expect(member.text()).toContain('等待管理员绑定账号发货'); expect(member.text()).not.toContain('查看专属 Key'); member.unmount()
 })
 it('商品固定展示，确认付款后才创建新团，失败重试复用请求号', async () => {
  const product={id:5,title:'Plus 2 人团',description:'共享套餐',seats:2,price:10,duration_days:30,formation_days:2,total_tokens:2000000,total_requests:2000,concurrency:1,status:'active',version:1}
  api.products.mockResolvedValue([product]);api.purchase.mockRejectedValueOnce(new Error('网络错误')).mockResolvedValueOnce({order_id:10})
  const w=render();await flushPromises();expect(api.purchase).not.toHaveBeenCalled();expect(w.text()).toContain('30 天')
  await w.findAll('button').find(b=>b.text()==='购买并发起拼团')!.trigger('click')
  const confirm=()=>w.get('[role="dialog"]').findAll('button').find(b=>b.text()==='确认')!
  await confirm().trigger('click');await flushPromises();expect(w.get('[role="alert"]').text()).toBe('网络错误')
  const requestId=api.purchase.mock.calls[0][1];await confirm().trigger('click');await flushPromises();expect(api.purchase.mock.calls[1][1]).toBe(requestId);expect(w.text()).toContain('拼单 #10 已创建');expect(w.text()).toContain('Plus 2 人团');w.unmount()
 })
 it('拼团中的状态标红，近期成团独立展示', async()=>{
  api.list.mockResolvedValue([order(),order({id:9,title:'旧空团',joined:0}),order({id:2,title:'已成团商品',status:'awaiting_delivery',formed_at:new Date().toISOString()})])
  const w=render();await flushPromises();const badge=w.findAll('span').find(s=>s.text()==='正在拼团中')!;expect(badge.classes()).toContain('text-red-600');expect(w.get('aside').text()).toContain('已成团商品');expect(w.text()).not.toContain('旧空团');w.unmount()
 })
 it('确认席位金额后购买，重复点击不会重复发送请求', async () => {
  let resolve!: () => void; api.act.mockImplementation(() => new Promise<void>(r => { resolve = r }))
  const w = render(); await flushPromises()
  await w.findAll('button').find(b => b.text() === '购买席位')!.trigger('click')
  expect(w.get('[role="dialog"]').text()).toContain('将扣除 10 站内额度')
  const confirm = w.get('[role="dialog"]').findAll('button').find(b => b.text() === '确认')!
  await confirm.trigger('click'); await confirm.trigger('click'); expect(api.act).toHaveBeenCalledTimes(1); expect(api.act).toHaveBeenCalledWith(1, 'join')
  resolve(); await flushPromises(); expect(api.refreshUser).toHaveBeenCalled(); expect(w.find('[role="dialog"]').exists()).toBe(false); w.unmount()
 })
 it('显示整车和个人配额及预占，并隐藏其他成员 Key', async () => {
  api.list.mockResolvedValue([order({ status: 'active', mine: { id: 2, status: 'joined', key_id: 8, paid: 10, refunded: 0, tokens_used: 100, requests_used: 2, reserved_tokens: 900, inflight: 1 }, shared_account: { id: 0, group_id: 0, name: '', type: 'oauth', status: 'active', used_5h: null, used_7d: 25 } })])
  const w = render(); await flushPromises(); expect(w.text()).toContain('我的已用 / Token 配额'); expect(w.text()).toContain('进行中预占 900'); expect(w.text()).toContain('未知'); expect(w.text()).toContain('75%'); expect(w.text()).toContain('查看专属 Key #8'); expect(w.findAll('button').some(b => b.text() === '购买席位')).toBe(false); w.unmount()
 })
 it('失败时展示错误并恢复按钮，空列表可刷新', async () => {
  api.list.mockRejectedValueOnce(new Error('暂不可用')); const w = render(); await flushPromises(); expect(w.get('[role="alert"]').text()).toContain('暂不可用')
  api.list.mockResolvedValue([]); await w.findAll('button').find(b => b.text() === '刷新')!.trigger('click'); await flushPromises(); expect(w.text()).toContain('暂时没有正在拼团的团'); expect(w.find('[role="alert"]').exists()).toBe(false); w.unmount()
 })
 it('Plus 显示美元窗口额度，Pro 隐藏窗口且可直达拼单使用记录',async()=>{
  const mine={id:2,status:'joined',key_id:8,paid:10,refunded:0,tokens_used:100,requests_used:2,reserved_tokens:0,inflight:0,credit_used:3,reserved_credit:1,credit_used_5h:1,credit_used_7d:4}
  api.list.mockResolvedValue([order({status:'active',quota_mode:'credits',plan_type:'plus',total_credit:100,credit_5h:10,credit_7d:50,mine})])
  const w=render();await flushPromises();expect(w.text()).toContain('每席 5 小时额度');expect(w.text()).toContain('$25.00');expect(w.text()).toContain('$21.00');expect(w.text()).not.toContain('我的已用 / Token 配额');expect(w.text()).toContain('查看使用记录 #1');w.unmount()
  api.list.mockResolvedValue([order({status:'active',quota_mode:'credits',plan_type:'pro',total_credit:100,credit_5h:0,credit_7d:0,mine})])
  const pro=render();await flushPromises();expect(pro.text()).toContain('Pro 不设置 5 小时和周额度');expect(pro.text()).not.toContain('每席 5 小时额度');expect(pro.text()).not.toContain('每席每周额度');pro.unmount()
 })
 it('发布商品切换 Pro 会移除两个窗口，保存仍保留周期美元额度',async()=>{
  api.saveProduct.mockResolvedValue(undefined)
  const w=render(true);await flushPromises()
  const plan=w.findAll('select').find(s=>s.findAll('option').some(o=>o.text()==='Pro'))!
  await plan.setValue('pro');expect(w.text()).not.toContain('整车 5 小时额度（$）');expect(w.text()).not.toContain('整车每周额度（$）')
  await w.get('input[maxlength="100"]').setValue('Pro 3 人团');await w.get('form').trigger('submit');await flushPromises()
  expect(api.saveProduct).toHaveBeenCalledWith(expect.objectContaining({quota_mode:'credits',plan_type:'pro',total_credit:100,credit_5h:0,credit_7d:0}));w.unmount()
 })

})
