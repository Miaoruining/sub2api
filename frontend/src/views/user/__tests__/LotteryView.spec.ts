import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, afterEach, describe, expect, it, vi } from 'vitest'
import LotteryView from '../LotteryView.vue'
import locale from '@/i18n/locales/zh/lottery'
import type { LotteryStatus } from '@/api/lottery'

const api = vi.hoisted(() => ({ status: vi.fn(), draw: vi.fn(), refreshUser: vi.fn() }))
vi.mock('@/api/lottery', () => ({ lotteryAPI: api }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ refreshUser: api.refreshUser }) }))
vi.mock('vue-i18n', async (importOriginal) => ({ ...await importOriginal<typeof import('vue-i18n')>(), useI18n: () => ({
  t: (key: string, params: Record<string, unknown> = {}) => {
    const text = locale.lottery[key.replace('lottery.', '') as keyof typeof locale.lottery] || key
    return text.replace(/\{(\w+)\}/g, (_, name: string) => String(params[name] ?? ''))
  },
}) }))
function state(overrides: Partial<LotteryStatus> = {}): LotteryStatus {
  return { activity_date: '2026-09-08', state: 'ready', eligible: true, server_time: '2026-09-08T02:00:00Z', opens_at: '2026-09-08T10:00:00+08:00', next_opens_at: '2026-09-09T10:00:00+08:00', prizes: [0, 1, 5, 10, 20, 50, 100], today: null, history: [], ...overrides }
}
function render() {
  return mount(LotteryView, { global: {
    stubs: { AppLayout: { template: '<main><slot /></main>' }, Icon: true, RouterLink: { template: '<a><slot /></a>' } },
  } })
}
describe('LotteryView', () => {
  beforeEach(() => { vi.useFakeTimers(); vi.clearAllMocks(); api.status.mockResolvedValue(state()); api.refreshUser.mockResolvedValue(undefined) })
  afterEach(() => { vi.useRealTimers() })
  it('shows rewards and rules without probabilities or budget', async () => {
    const w = render(); await flushPromises()
    expect(w.findAll('.prize-tile')).toHaveLength(6)
    expect(w.text()).toContain('累计有效人民币实付充值满 10 元')
    for (const hidden of ['95.52', '概率', '奖池', '预算', '每日发放上限']) expect(w.text()).not.toContain(hidden)
    expect(w.get('.draw-button').attributes('disabled')).toBeUndefined()
    w.unmount()
  })
  it.each(['not_open', 'ineligible', 'drawn', 'ended', 'disabled'] as const)('disables drawing in %s state', async stateName => {
    api.status.mockResolvedValue(state({ state: stateName }))
    const w = render(); await flushPromises()
    expect(w.get('.draw-button').attributes('disabled')).toBeDefined()
    expect(api.draw).not.toHaveBeenCalled(); w.unmount()
  })
  it('sends only the activity date and blocks double clicks', async () => {
    let resolveDraw!: (value: unknown) => void
    api.draw.mockImplementation(() => new Promise(resolve => { resolveDraw = resolve }))
    const w = render(); await flushPromises()
    await w.get('.draw-button').trigger('click'); await w.get('.draw-button').trigger('click')
    expect(api.draw).toHaveBeenCalledTimes(1); expect(api.draw).toHaveBeenCalledWith('2026-09-08')
    const today = { id: 1, activity_date: '2026-09-08', prize: 5, created_at: '2026-09-08T02:00:00Z' }
    api.status.mockResolvedValue(state({ state: 'drawn', today, history: [today] }))
    resolveDraw(today); await flushPromises()
    expect(w.text()).toContain('5 额度已到账'); expect(api.refreshUser).toHaveBeenCalledTimes(1)
    expect(w.get('.draw-button').attributes('disabled')).toBeDefined(); w.unmount()
  })
  it('recovers a committed result when the draw response is lost', async () => {
    const w = render(); await flushPromises()
    const today = { id: 2, activity_date: '2026-09-08', prize: 0, created_at: '2026-09-08T02:00:00Z' }
    api.draw.mockRejectedValue(new Error('network'))
    api.status.mockResolvedValue(state({ state: 'drawn', today, history: [today] }))
    await w.get('.draw-button').trigger('click'); await flushPromises()
    expect(w.find('[role="alert"]').exists()).toBe(false)
    expect(w.text()).toContain('今日抽奖结果'); expect(w.get('.draw-button').attributes('disabled')).toBeDefined(); w.unmount()
  })
  it('refreshes at 10:00 using server time instead of client wall time', async () => {
    api.status.mockResolvedValueOnce(state({ state: 'not_open', server_time: '2026-09-08T01:59:59Z' }))
    const w = render(); await flushPromises()
    expect(w.text()).toContain('00:00:01')
    await vi.advanceTimersByTimeAsync(1000); await flushPromises()
    expect(api.status).toHaveBeenCalledTimes(2)
    expect(w.get('.draw-button').attributes('disabled')).toBeUndefined(); w.unmount()
  })
  it('offers retry after status load failure without an active draw button', async () => {
    api.status.mockRejectedValueOnce(new Error('unavailable'))
    const w = render(); await flushPromises()
    expect(w.get('[role="alert"]').text()).toContain('状态加载失败')
    expect(w.find('.draw-button').exists()).toBe(false); w.unmount()
  })
})
