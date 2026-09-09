import {mount,flushPromises} from '@vue/test-utils'
import {describe,it,expect,vi,beforeEach} from 'vitest'
import PoolResourcesView from '../PoolResourcesView.vue'
import PoolDeliveryPanel from '@/components/account/PoolDeliveryPanel.vue'
const api=vi.hoisted(() => ({list:vi.fn(),resources:vi.fn(),deliver:vi.fn()}))
vi.mock('@/api/poolOrders',async original => ({...await original<typeof import('@/api/poolOrders')>(),poolAPI:api}))
vi.mock('vue-router',async original => ({...await original<typeof import('vue-router')>(),useRoute:() => ({query:{order:'12'}})}))
vi.mock('vue-i18n',async original => ({...await original<typeof import('vue-i18n')>(),useI18n:() => ({t:(key:string) => key})}))
vi.mock('../AccountsView.vue',() => ({default:{props:{poolScope:Boolean},template:'<main :data-pool-scope="poolScope"/>'}}))
describe('拼单号池沿用原账号管理并关联订单',() => {
 beforeEach(() => {vi.clearAllMocks();api.list.mockResolvedValue([{id:12,title:'待发货订单',status:'awaiting_delivery',group_id:0,delivery_deadline:'2020-01-01T00:00:00Z'},{id:13,status:'active',group_id:30}]);api.resources.mockResolvedValue([{id:7,group_id:20,name:'可用OAuth账号',status:'active'},{id:8,group_id:30,name:'占用账号',status:'active'}]);api.deliver.mockResolvedValue(undefined)})
 it('复用完整账号管理页并启用独立范围',() => {const w=mount(PoolResourcesView);expect(w.get('main').attributes('data-pool-scope')).toBe('true');w.unmount()})
 it('按订单号进入发货，仅可选空闲账号，成功后同步状态',async () => {const w=mount(PoolDeliveryPanel,{props:{revision:0},global:{stubs:{RouterLink:{template:'<a><slot/></a>'}}}});await flushPromises();expect(w.text()).toContain('pool.overdue');expect(w.findAll('option').some(o=>o.text().includes('占用账号'))).toBe(false);await w.findAll('select')[1].setValue('7');await w.get('form').trigger('submit');await flushPromises();expect(api.deliver).toHaveBeenCalledWith(12,7);expect(w.emitted('delivered')).toHaveLength(1);expect(w.get('[role="status"]').text()).toBe('pool.delivered');w.unmount()})
 it('发货失败保留选择，不误报成功',async () => {api.deliver.mockRejectedValue(new Error('账号已占用'));const w=mount(PoolDeliveryPanel,{props:{revision:0},global:{stubs:{RouterLink:true}}});await flushPromises();await w.findAll('select')[1].setValue('7');await w.get('form').trigger('submit');await flushPromises();expect(w.get('[role="alert"]').text()).toBe('账号已占用');expect(w.emitted('delivered')).toBeUndefined();w.unmount()})
})
