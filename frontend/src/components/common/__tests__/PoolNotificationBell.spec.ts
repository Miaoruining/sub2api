import { mount, flushPromises } from '@vue/test-utils'
import { defineComponent, h } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import PoolNotificationBell from '../PoolNotificationBell.vue'

const mocks = vi.hoisted(() => ({ notifications: vi.fn(), read: vi.fn(), openAnnouncements: vi.fn() }))
vi.mock('@/api/poolOrders', () => ({ poolAPI: { notifications: mocks.notifications, readNotification: mocks.read }, poolError: () => '加载失败' }))
vi.mock('@/stores/announcements', () => ({ useAnnouncementStore: () => ({ unreadCount: 2 }) }))
vi.mock('vue-i18n', async original => ({ ...await original<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))

function render() {
  return mount(PoolNotificationBell, { global: { stubs: {
    AnnouncementBell: defineComponent({ props: ['hideTrigger'], setup(_, { expose }) { expose({ openModal: mocks.openAnnouncements }); return () => h('div') } }),
    RouterLink: { props: ['to'], template: '<a :data-target="typeof to === \'string\' ? to : to.path"><slot /></a>' },
    Icon: { props: ['name'], template: '<i :data-icon="name" />' }
  } } })
}

describe('顶部统一通知入口', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mocks.notifications.mockResolvedValue([{ id: 4, order_id: 9, title: 'Plus 2 人团', kind: 'delivery_required', deadline: '2030-01-01T00:00:00Z', read: false }])
    mocks.read.mockResolvedValue(undefined)
  })

  it('仅展示一个铃铛，合并公告和拼单未读数，仍可打开公告', async () => {
    const w = render(); await flushPromises()
    expect(w.findAll('[data-icon="bell"]')).toHaveLength(1)
    const trigger = w.get('button[aria-expanded]')
    expect(trigger.text()).toContain('3')
    expect(trigger.attributes('aria-label')).toContain('announcements.title')
    await trigger.trigger('click'); await flushPromises()
    await w.findAll('button').find(b => b.text().includes('announcements.title'))!.trigger('click')
    expect(mocks.openAnnouncements).toHaveBeenCalledOnce()
    expect(trigger.attributes('aria-expanded')).toBe('false')
    w.unmount()
  })

  it('拼单通知保留发货跳转和已读操作，并更新合计未读数', async () => {
    const w = render(); await flushPromises()
    await w.get('button[aria-expanded]').trigger('click'); await flushPromises()
    const link = w.get('a[data-target="/admin/pool-resources"]')
    expect(link.text()).toContain('#9')
    await link.trigger('click'); await flushPromises()
    expect(mocks.read).toHaveBeenCalledWith(4)
    expect(w.get('button[aria-expanded]').text()).toContain('2')
    w.unmount()
  })
  it('用户发货通知跳转到我的订阅中的拼单订单', async () => {
    mocks.notifications.mockResolvedValue([{id:5,order_id:9,title:'已发货',kind:'delivered',deadline:null,read:false}])
    const w=render();await flushPromises()
    await w.get('button[aria-expanded]').trigger('click');await flushPromises()
    expect(w.find('a[data-target="/subscriptions#pool-orders"]').exists()).toBe(true)
    w.unmount()
  })

})
