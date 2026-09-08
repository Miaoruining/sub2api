import { apiClient } from './client'

export interface LotteryDraw {
  id: number
  activity_date: string
  prize: number
  created_at: string
}
export interface LotteryStatus {
  activity_date: string
  state: 'ready' | 'drawn' | 'disabled' | 'not_open' | 'ended' | 'ineligible'
  eligible: boolean
  admin_repeat?: boolean
  server_time: string
  opens_at: string
  closes_at: string
  next_opens_at: string
  prizes: number[]
  today: LotteryDraw | null
  history: LotteryDraw[]
}
export interface LotteryAdminStatus {
  enabled: boolean
  admin_repeat_enabled: boolean
  activity_date: string
  daily_budget: number
  spent: number
  draw_count: number
  weights: number[]
  next_weights: number[]
  intro_weights: number[]
  intro_draw_limit: number
  next_effective_date: string
  distribution: number[]
  records: (LotteryDraw & { user_id: number; balance_after: number; intro_draw_number: number })[]
}
export const lotteryAPI = {
  async status(): Promise<LotteryStatus> {
    return (await apiClient.get<LotteryStatus>('/lottery')).data
  },
  async draw(activityDate: string, requestId?: string): Promise<LotteryDraw> {
    return (await apiClient.post<LotteryDraw>('/lottery/draw', { activity_date: activityDate, ...(requestId ? { request_id: requestId } : {}) })).data
  },
}
// 管理端接口与用户状态接口隔离，用户端不获取内部奖池或权重数据。
export const lotteryAdminAPI = {
  async status(date?: string): Promise<LotteryAdminStatus> {
    return (await apiClient.get<LotteryAdminStatus>('/admin/lottery', { params: { date } })).data
  },
  async configure(config: { enabled?: boolean; admin_repeat_enabled?: boolean; weights?: number[] }): Promise<void> {
    await apiClient.put('/admin/lottery/config', config)
  },
}
