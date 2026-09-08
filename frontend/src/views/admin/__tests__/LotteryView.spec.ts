import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import LotteryView from '../LotteryView.vue'
import locale from '@/i18n/locales/zh/lottery'

const api = vi.hoisted(() => ({ status: vi.fn(), configure: vi.fn() }))
vi.mock('@/api/lottery', () => ({ lotteryAdminAPI: api }))
vi.mock('vue-i18n', async importOriginal => ({ ...await importOriginal<typeof import('vue-i18n')>(), useI18n: () => ({
  t: (key: string, params: Record<string, unknown> = {}) => {
    const text = locale.lottery[key.replace('lottery.', '') as keyof typeof locale.lottery] || key
    return text.replace(/\{(\w+)\}/g, (_, name: string) => String(params[name] ?? ''))
  },
}) }))
const current = () => ({ enabled: false, admin_repeat_enabled: true, activity_date: '2026-09-08', daily_budget: 100, spent: 6, draw_count: 40, weights: [9552, 400, 30, 10, 5, 2, 1], next_weights: [9552, 400, 30, 10, 5, 2, 1], next_effective_date: '2026-09-09', distribution: [38, 1, 1, 0, 0, 0, 0], records: [] })
function render() { return mount(LotteryView, { global: { stubs: { AppLayout: { template: '<main><slot /></main>' } } } }) }
describe('Admin lottery configuration', () => {
  beforeEach(() => { vi.clearAllMocks(); api.status.mockResolvedValue(current()); api.configure.mockResolvedValue(undefined) })
  it('keeps internal budget and audit details on the admin page', async () => {
    const w = render(); await flushPromises()
    expect(w.text()).toContain('每日发放上限'); expect(w.text()).toContain('仅管理员可见')
    expect(w.findAll('input[type="number"]')).toHaveLength(7); w.unmount()
  })
  it('rejects an invalid total without submitting configuration', async () => {
    const w = render(); await flushPromises()
    await w.findAll('input[type="number"]')[0].setValue('90')
    await w.get('form').trigger('submit'); await flushPromises()
    expect(api.configure).not.toHaveBeenCalled(); expect(w.get('[role="alert"]').text()).toContain('合计必须为 100%'); w.unmount()
  })
  it('saves integer weights for tomorrow without changing the enable switch', async () => {
    const w = render(); await flushPromises()
    await w.get('form').trigger('submit'); await flushPromises()
    expect(api.configure).toHaveBeenCalledWith({ weights: [9552, 400, 30, 10, 5, 2, 1] })
    expect(w.text()).toContain('2026-09-09'); expect(w.text()).toContain('配置已保存'); w.unmount()
  })
  it('changes only the enable flag and retains the old state after failure', async () => {
    const w = render(); await flushPromises(); api.configure.mockRejectedValue(new Error('network'))
    await w.get('input[role="switch"]').setValue(true); await flushPromises()
    expect(api.configure).toHaveBeenCalledWith({ enabled: true })
    expect((w.get('input[role="switch"]').element as HTMLInputElement).checked).toBe(false)
    expect(w.get('[role="alert"]').exists()).toBe(true); w.unmount()
  })
  it('keeps the default administrator switch independent and restores it after a failed change', async () => {
    const w = render(); await flushPromises()
    const input = w.get('input[aria-label="允许管理员随时重复抽奖"]')
    expect((input.element as HTMLInputElement).checked).toBe(true)
    api.configure.mockRejectedValueOnce(new Error('network'))
    await input.setValue(false); await flushPromises()
    expect(api.configure).toHaveBeenCalledWith({ admin_repeat_enabled: false })
    expect((input.element as HTMLInputElement).checked).toBe(true)
    api.status.mockResolvedValue({ ...current(), admin_repeat_enabled: false })
    await input.setValue(false); await flushPromises()
    expect((input.element as HTMLInputElement).checked).toBe(false)
    expect((w.get('input[aria-label="开放抽奖活动"]').element as HTMLInputElement).checked).toBe(false)
    w.unmount()
  })
})
