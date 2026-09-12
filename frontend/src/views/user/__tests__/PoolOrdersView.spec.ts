import { mount, flushPromises } from '@vue/test-utils'
import { beforeEach, afterEach, describe, it, expect, vi } from 'vitest'
import PoolOrdersView from '../PoolOrdersView.vue'
import PoolSubscriptionsSection from '@/components/account/PoolSubscriptionsSection.vue'
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
function render(admin = false, mine = false) { return mount(mine ? PoolSubscriptionsSection : PoolOrdersView, { props: mine ? {} : {admin}, global: { stubs: { AppLayout: { template: '<main><slot/></main>' }, BaseDialog: { props: ['show', 'title'], template: '<section v-if="show" role="dialog"><h2>{{title}}</h2><slot/><slot name="footer"/></section>' }, RouterLink: { props: ['to'], template: '<a :href="typeof to === \'string\' ? to : to.path"><slot/></a>' } } } }) }
describe('拼单席位和个人额度', () => {
 beforeEach(() => { vi.clearAllMocks(); vi.spyOn(window, 'scrollTo').mockImplementation(() => {}); api.list.mockResolvedValue([order()]); api.products.mockResolvedValue([]); api.act.mockResolvedValue(undefined); api.refreshUser.mockResolvedValue(undefined) })
 afterEach(() => vi.restoreAllMocks())
 it('发布不选择号池，待发货成员不能提前领取 Key', async () => {
  const admin = render(true); await flushPromises(); expect(admin.text()).toContain('发货后有效天数'); expect(admin.text()).not.toContain('720'); expect(api.resources).not.toHaveBeenCalled(); admin.unmount()
  api.list.mockResolvedValue([order({status:'awaiting_delivery',delivery_deadline:'2030-01-01T00:00:00Z',mine:{id:2,status:'joined',key_id:null,paid:10,refunded:0,tokens_used:0,requests_used:0,reserved_tokens:0,inflight:0}})])
  const member=render(false, true); await flushPromises(); expect(member.text()).toContain('等待管理员绑定账号发货'); expect(member.text()).not.toContain('查看专属 Key'); member.unmount()
 })
 it('商品固定展示，确认付款后才创建新团，失败重试复用请求号', async () => {
  const product={id:5,title:'Plus 2 人团',description:'共享套餐',seats:2,price:10,duration_days:30,formation_days:2,total_tokens:2000000,total_requests:2000,concurrency:1,status:'active',version:1}
  api.products.mockResolvedValue([product]);api.purchase.mockRejectedValueOnce(new Error('网络错误')).mockResolvedValueOnce({order_id:10})
  const w=render();await flushPromises();expect(api.purchase).not.toHaveBeenCalled();expect(w.text()).toContain('30 天')
  await w.findAll('button').find(b=>b.text()==='发起拼团')!.trigger('click')
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
  await w.findAll('button').find(b => b.text() === '立即参团')!.trigger('click')
  expect(w.get('[role="dialog"]').text()).toContain('将扣除 10 站内额度')
  const confirm = w.get('[role="dialog"]').findAll('button').find(b => b.text() === '确认')!
  await confirm.trigger('click'); await confirm.trigger('click'); expect(api.act).toHaveBeenCalledTimes(1); expect(api.act).toHaveBeenCalledWith(1, 'join')
  resolve(); await flushPromises(); expect(api.refreshUser).toHaveBeenCalled(); expect(w.find('[role="dialog"]').exists()).toBe(false); w.unmount()
 })
 it('显示整车和个人配额及预占，并隐藏其他成员 Key', async () => {
  api.list.mockResolvedValue([order({ status: 'active', mine: { id: 2, status: 'joined', key_id: 8, paid: 10, refunded: 0, tokens_used: 100, requests_used: 2, reserved_tokens: 900, inflight: 1 }, shared_account: { id: 0, group_id: 0, name: '', type: 'oauth', status: 'active', used_5h: null, used_7d: 25 } })])
  const w = render(false, true); await flushPromises(); expect(w.text()).toContain('我的已用 / Token 配额'); expect(w.text()).toContain('进行中预占 900'); expect(w.text()).toContain('未知'); expect(w.text()).toContain('75%'); expect(w.text()).toContain('查看专属 Key #8'); expect(w.findAll('button').some(b => b.text() === '立即参团')).toBe(false); w.unmount()
 })
 it('失败时展示错误并恢复按钮，空列表可刷新', async () => {
  api.list.mockRejectedValueOnce(new Error('暂不可用')); const w = render(); await flushPromises(); expect(w.get('[role="alert"]').text()).toContain('暂不可用')
  api.list.mockResolvedValue([]); await w.findAll('button').find(b => b.text() === '刷新')!.trigger('click'); await flushPromises(); expect(w.text()).toContain('暂时没有正在拼团的团'); expect(w.find('[role="alert"]').exists()).toBe(false); w.unmount()
 })
 it('Plus 显示美元窗口额度，Pro 隐藏窗口且可直达拼单使用记录',async()=>{
  const mine={id:2,status:'joined',key_id:8,paid:10,refunded:0,tokens_used:100,requests_used:2,reserved_tokens:0,inflight:0,credit_used:3,reserved_credit:1,credit_used_5h:1,credit_used_7d:4}
  api.list.mockResolvedValue([order({status:'active',quota_mode:'credits',plan_type:'plus',total_credit:100,credit_5h:10,credit_7d:50,mine})])
  const w=render(false, true);await flushPromises();expect(w.text()).toContain('每席 5 小时额度');expect(w.text()).toContain('$25.00');expect(w.text()).toContain('$21.00');expect(w.text()).not.toContain('我的已用 / Token 配额');expect(w.text()).toContain('查看使用记录 #1');w.unmount()
  api.list.mockResolvedValue([order({status:'active',quota_mode:'credits',plan_type:'pro',total_credit:100,credit_5h:0,credit_7d:0,mine})])
  const pro=render(false, true);await flushPromises();expect(pro.text()).toContain('Pro 不设置 5 小时和周额度');expect(pro.text()).not.toContain('每席 5 小时额度');expect(pro.text()).not.toContain('每席每周额度');pro.unmount()
 })
 it('发布商品切换 Pro 会移除两个窗口，保存仍保留周期美元额度',async()=>{
  api.saveProduct.mockResolvedValue(undefined)
  const w=render(true);await flushPromises()
  const plan=w.findAll('select').find(s=>s.findAll('option').some(o=>o.text()==='Pro'))!
  await plan.setValue('pro');expect(w.text()).not.toContain('整车 5 小时额度（$）');expect(w.text()).not.toContain('整车每周额度（$）')
  await w.get('input[maxlength="100"]').setValue('Pro 3 人团');await w.get('form').trigger('submit');await flushPromises()
  expect(api.saveProduct).toHaveBeenCalledWith(expect.objectContaining({quota_mode:'dynamic_shadow',plan_type:'pro',total_credit:100,credit_5h:0,credit_7d:0}));w.unmount()
 })
 it('动态额度新建不显示固定美元字段并提交零固定额度', async()=>{
  api.saveProduct.mockResolvedValue(undefined)
  const w=render(true);await flushPromises()
  const mode=w.findAll('select').find(s=>s.find('option[value="dynamic"]').exists())!
  await mode.setValue('dynamic')
  expect(w.text()).toContain('动态额度按发货账号实际上游窗口和席位均分')
  expect(w.text()).not.toContain('整车周期额度（$）')
  await w.get('input[maxlength="100"]').setValue('动态 Plus 2 人团');await w.get('form').trigger('submit');await flushPromises()
  expect(api.saveProduct).toHaveBeenCalledWith(expect.objectContaining({quota_mode:'dynamic',total_credit:0,credit_5h:0,credit_7d:0}))
  w.unmount()
 })
 it('新建商品切回 Token 模式时恢复可提交的默认 Token 和次数', async()=>{
  api.saveProduct.mockResolvedValue(undefined)
  const w=render(true);await flushPromises()
  const mode=w.findAll('select').find(s=>s.find('option[value="dynamic"]').exists())!
  await mode.setValue('tokens')
  await w.get('input[maxlength="100"]').setValue('Token 2 人团');await w.get('form').trigger('submit');await flushPromises()
  expect(api.saveProduct).toHaveBeenCalledWith(expect.objectContaining({quota_mode:'tokens',total_tokens:2000000,total_requests:2000}))
  w.unmount()
 })
 it('编辑旧 credits 商品时保留原配额模式', async()=>{
  const product={id:8,title:'旧 credits 商品',description:'旧规则',quota_mode:'credits',plan_type:'plus',total_credit:100,credit_5h:10,credit_7d:50,seats:2,price:10,duration_days:30,formation_days:2,total_tokens:0,total_requests:0,concurrency:1,status:'active' as const,version:3}
  api.products.mockResolvedValue([product]);const w=render(true);await flushPromises()
  await w.findAll('button').find(b=>b.text()==='编辑商品')!.trigger('click')
  const mode=w.findAll('select').find(s=>s.find('option[value="dynamic"]').exists())!
  expect((mode.element as HTMLSelectElement).value).toBe('credits');expect(w.text()).toContain('整车周期额度（$）');w.unmount()
 })
 it('管理卡片可直接下架和重新上架商品', async()=>{
  const active={id:8,title:'在售商品',description:'',quota_mode:'credits',plan_type:'plus',total_credit:100,credit_5h:10,credit_7d:50,seats:2,price:10,duration_days:30,formation_days:2,total_tokens:0,total_requests:0,concurrency:1,status:'active' as const,version:3}
  const disabled={...active,id:9,title:'已下架商品',status:'disabled' as const,version:4}
  api.products.mockResolvedValue([active,disabled]);api.saveProduct.mockResolvedValue(undefined)
  const w=render(true);await flushPromises()
  await w.findAll('button').find(b=>b.text()==='下架')!.trigger('click');await flushPromises()
  expect(api.saveProduct).toHaveBeenNthCalledWith(1,expect.objectContaining({id:8,status:'disabled',version:3}))
  expect(w.text()).toContain('商品已下架，拼单大厅将不再展示该商品。')
  await w.findAll('button').find(b=>b.text()==='重新上架')!.trigger('click');await flushPromises()
  expect(api.saveProduct).toHaveBeenNthCalledWith(2,expect.objectContaining({id:9,status:'active',version:4}));w.unmount()
 })
 it('内容区标题只在顶部栏不显示标题的移动端保留',async()=>{
  const w=render();await flushPromises();expect(w.get('[data-test="content-page-title"]').classes()).toContain('lg:hidden');w.unmount()
 })
 it('动态窗口明确使用整号百分点，缺失或过期不会显示零额度', async()=>{
  api.list.mockResolvedValue([order({status:'active',quota_mode:'dynamic',plan_type:'plus',mine:{id:2,status:'joined',key_id:8,paid:10,refunded:0,tokens_used:0,requests_used:0,reserved_tokens:0,inflight:0,dynamic_quota:{status:'stale',shadow:false,windows:[{key:'5h',account_remaining_percent:null,used_percent:null,reserved_percent:null,remaining_percent:null,entitlement_percent:null,reset_at:null,observed_at:null,status:'stale'}]}}})])
  const w=render(false, true);await flushPromises();expect(w.text()).toContain('占整号窗口的百分点');expect(w.text()).toContain('数据过期');expect(w.text()).not.toContain('0 pp');w.unmount()
 })
 it('Pro 动态账号返回窗口时展示窗口而非无限承诺', async()=>{
  api.list.mockResolvedValue([order({status:'active',quota_mode:'dynamic',plan_type:'pro',mine:{id:2,status:'joined',key_id:8,paid:10,refunded:0,tokens_used:0,requests_used:0,reserved_tokens:0,inflight:0,dynamic_quota:{status:'ready',shadow:false,windows:[{key:'5h',account_remaining_percent:80,used_percent:20,reserved_percent:0,remaining_percent:40,entitlement_percent:40,reset_at:'2030-01-01T00:00:00Z',observed_at:'2029-12-31T00:00:00Z',status:'ready'}]}}})])
  const w=render(false, true);await flushPromises();expect(w.text()).toContain('Pro');expect(w.text()).toContain('5 小时窗口');expect(w.text()).toContain('40 pp');expect(w.text()).not.toContain('Pro 不设置 5 小时和周额度');w.unmount()
 })

 it('首屏先展示商品，更多商品可展开，拼团列表保持独立', async()=>{
  api.products.mockResolvedValue([1,2,3].map(id=>({id,title:`商品 ${id}`,description:'',seats:2,price:10,duration_days:30,formation_days:2,total_tokens:2000000,total_requests:2000,concurrency:1,status:'active',version:1})))
  const w=render();await flushPromises()
  const products=w.get('[data-test="lobby-products"]')
  expect(products.findAll('article').length).toBe(2)
  expect(w.html().indexOf('lobby-products')).toBeLessThan(w.html().indexOf('lobby-forming'))
  await w.get('[data-test="toggle-products"]').trigger('click')
  expect(products.text()).toContain('商品 3')
  await w.get('[data-test="toggle-products"]').trigger('click')
  expect(products.text()).not.toContain('商品 3');w.unmount()
 })
 it('已参加的团保留退出入口，截止或满员的团不能再次参团', async()=>{
  api.list.mockResolvedValue([
   order({mine:{id:2,status:'joined',key_id:null,paid:10,refunded:0,tokens_used:0,requests_used:0,reserved_tokens:0,inflight:0}}),
   order({id:2,join_deadline:'2020-01-01T00:00:00Z'}),order({id:3,joined:4})
  ])
  const w=render();await flushPromises();const rows=w.findAll('[data-test="lobby-order-row"]')
  expect(rows[0].find('[data-test="lobby-join"]').exists()).toBe(false)
  await rows[0].get('[data-test="lobby-leave"]').trigger('click')
  expect(w.get('[role="dialog"]').text()).toContain('全额退回余额')
  expect(rows[1].get('[data-test="lobby-join"]').attributes('disabled')).toBeDefined()
  expect(rows[2].get('[data-test="lobby-join"]').attributes('disabled')).toBeDefined();w.unmount()
 })

 it('已退款成员不能重新加入同一团，订单详情可单独查看', async()=>{
  api.list.mockResolvedValue([order({mine:{id:2,status:'refunded',key_id:null,paid:10,refunded:10,tokens_used:7,requests_used:1,reserved_tokens:0,inflight:0}})])
  const w=render();await flushPromises();const row=w.get('[data-test="lobby-order-row"]')
  expect(row.find('[data-test="lobby-join"]').exists()).toBe(false)
  expect(row.get('[data-test="lobby-left"]').text()).toContain('退')
  await row.get('[data-test="lobby-details"]').trigger('click')
  expect(w.get('[role="dialog"]').text()).toContain('7 / 1,000,000');w.unmount()
 })

 it('大厅提示用户去我的订阅，个人订单详情不再占用大厅', async()=>{
  api.list.mockResolvedValue([order({status:'active',mine:{id:2,status:'joined',key_id:8,paid:10,refunded:0,tokens_used:0,requests_used:0,reserved_tokens:0,inflight:0}})])
  const w=render();await flushPromises()
  expect(w.get('[data-test="my-orders-notice"]').text()).toContain('我的订阅')
  expect(w.get('[data-test="my-orders-notice"] a').attributes('href')).toBe('/subscriptions#pool-orders')
  expect(w.findComponent(PoolSubscriptionsSection).exists()).toBe(false)
  expect(w.text()).not.toContain('查看专属 Key #8');w.unmount()
 })

})
