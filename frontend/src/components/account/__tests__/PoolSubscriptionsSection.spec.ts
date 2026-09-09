import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import PoolSubscriptionsSection from '../PoolSubscriptionsSection.vue'
import locale from '@/i18n/locales/zh/pool'
import type { PoolMember, PoolOrder } from '@/api/poolOrders'

const api = vi.hoisted(() => ({
  list: vi.fn(),
  act: vi.fn(),
  refreshUser: vi.fn(),
}))

vi.mock('@/api/poolOrders', async (original) => ({
  ...await original<typeof import('@/api/poolOrders')>(),
  poolAPI: {
    ...(await original<typeof import('@/api/poolOrders')>()).poolAPI,
    list: api.list,
    act: api.act,
  },
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({ refreshUser: api.refreshUser }),
}))

vi.mock('vue-i18n', async (original) => ({
  ...await original<typeof import('vue-i18n')>(),
  useI18n: () => ({
    t: (key: string, params: Record<string, unknown> = {}) => {
      let value: unknown = locale
      for (const part of key.split('.')) value = (value as Record<string, unknown>)?.[part]
      return String(value ?? key).replace(/\{(\w+)\}/g, (_, name: string) => String(params[name] ?? ''))
    },
  }),
}))

const BaseDialogStub = defineComponent({
  props: { show: Boolean, title: { type: String, default: '' } },
  template: '<section v-if="show" role="dialog"><h2>{{ title }}</h2><slot /><slot name="footer" /></section>',
})

function member(overrides: Partial<PoolMember> = {}): PoolMember {
  return {
    id: 100,
    status: 'joined',
    key_id: null,
    paid: 10,
    refunded: 0,
    tokens_used: 0,
    requests_used: 0,
    reserved_tokens: 0,
    inflight: 0,
    ...overrides,
  }
}

function order(overrides: Partial<PoolOrder> = {}): PoolOrder {
  return {
    id: 1,
    title: '测试拼单',
    group_id: 10,
    seats: 2,
    price: 10,
    duration_hours: 720,
    duration_days: 30,
    total_tokens: 2_000_000,
    total_requests: 2_000,
    concurrency: 1,
    join_deadline: '2030-01-01T00:00:00Z',
    starts_at: null,
    expires_at: '2030-02-01T00:00:00Z',
    status: 'active',
    joined: 2,
    tokens_used: 0,
    requests_used: 0,
    reserved_tokens: 0,
    mine: member(),
    shared_account: null,
    ...overrides,
  }
}

function render() {
  return mount(PoolSubscriptionsSection, {
    global: {
      stubs: {
        BaseDialog: BaseDialogStub,
        RouterLink: { template: '<a><slot /></a>' },
      },
    },
  })
}

describe('PoolSubscriptionsSection', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.clearAllMocks()
    api.list.mockResolvedValue([])
    api.act.mockResolvedValue(undefined)
    api.refreshUser.mockResolvedValue(undefined)
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('只展示自己的订单，并保留待发货、active、expired 和 refunded 状态', async () => {
    api.list.mockResolvedValue([
      order({ id: 1, status: 'forming', mine: null }),
      order({ id: 2, status: 'awaiting_delivery', mine: member() }),
      order({
        id: 3,
        status: 'active',
        quota_mode: 'credits',
        plan_type: 'plus',
        total_credit: 100,
        credit_5h: 10,
        credit_7d: 50,
        mine: member({ key_id: 42, credit_used: 3, reserved_credit: 1 }),
      }),
      order({ id: 4, status: 'expired', mine: member({ key_id: 43 }) }),
      order({ id: 5, status: 'refunded', mine: member({ status: 'refunded', refunded: 10 }) }),
    ])

    const wrapper = render()
    await flushPromises()

    expect(api.list).toHaveBeenCalledTimes(1)
    expect(api.list).toHaveBeenCalledWith()
    expect(wrapper.find('#pool-order-1').exists()).toBe(false)
    for (const id of [2, 3, 4, 5]) expect(wrapper.find(`#pool-order-${id}`).exists()).toBe(true)
    expect(wrapper.text()).toContain('等待管理员绑定账号发货')
    expect(wrapper.text()).toContain('查看专属 Key #42')
    expect(wrapper.text()).toContain('每席周期额度')
    expect(wrapper.text()).toContain('已退回余额')
  })

  it('错误态可刷新，空列表显示返回大厅入口', async () => {
    api.list.mockRejectedValueOnce(new Error('暂不可用'))
    const wrapper = render()
    await flushPromises()

    expect(wrapper.get('[role="alert"]').text()).toContain('暂不可用')
    expect(wrapper.text()).not.toContain('暂无已参与的拼单订单')

    api.list.mockResolvedValueOnce([])
    await wrapper.findAll('button').find((button) => button.text() === '刷新')!.trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('暂无已参与的拼单订单')
    expect(wrapper.text()).toContain('前往拼单大厅')
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
  })

  it('退出确认请求防重复，失败后保留弹窗并可重试，成功后刷新订单和用户', async () => {
    let rejectFirst!: (reason?: unknown) => void
    api.act
      .mockImplementationOnce(() => new Promise<void>((_, reject) => { rejectFirst = reject }))
      .mockResolvedValueOnce(undefined)
    api.list.mockResolvedValue([order({ id: 9, status: 'forming', mine: member() })])

    const wrapper = render()
    await flushPromises()
    await wrapper.find('#pool-order-9').findAll('button').find((button) => button.text() === '退出并退款')!.trigger('click')
    const confirm = () => wrapper.get('[role="dialog"]').findAll('button').find((button) => button.text() === '确认')!

    await confirm().trigger('click')
    await confirm().trigger('click')
    expect(api.act).toHaveBeenCalledTimes(1)
    rejectFirst(new Error('退款失败'))
    await flushPromises()
    expect(wrapper.get('[role="dialog"]').text()).toContain('退款失败')

    await confirm().trigger('click')
    await flushPromises()
    expect(api.act).toHaveBeenNthCalledWith(2, 9, 'leave')
    expect(api.refreshUser).toHaveBeenCalledTimes(1)
    expect(wrapper.text()).toContain('操作完成，状态已更新')
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
  })

  it('后台、确认弹窗和请求进行中时跳过三十秒轮询，并在卸载时清理定时器', async () => {
    const owned = order({ id: 7, status: 'forming', mine: member() })
    api.list.mockResolvedValue([owned])
    const wrapper = render()
    await flushPromises()
    api.list.mockClear()

    Object.defineProperty(document, 'hidden', { configurable: true, value: true })
    await vi.advanceTimersByTimeAsync(30_000)
    expect(api.list).not.toHaveBeenCalled()

    Object.defineProperty(document, 'hidden', { configurable: true, value: false })
    await wrapper.find('#pool-order-7').findAll('button').find((button) => button.text() === '退出并退款')!.trigger('click')
    await vi.advanceTimersByTimeAsync(30_000)
    expect(api.list).not.toHaveBeenCalled()
    wrapper.find('[role="dialog"]').findAll('button').find((button) => button.text() === '返回')!.trigger('click')
    await flushPromises()

    let resolveRequest!: (orders: PoolOrder[]) => void
    api.list.mockImplementationOnce(() => new Promise<PoolOrder[]>((resolve) => { resolveRequest = resolve }))
    await wrapper.find('button').trigger('click')
    await vi.advanceTimersByTimeAsync(30_000)
    expect(api.list).toHaveBeenCalledTimes(1)
    resolveRequest([owned])
    await flushPromises()
    wrapper.unmount()
    await vi.advanceTimersByTimeAsync(30_000)
    expect(api.list).toHaveBeenCalledTimes(1)
  })
})
