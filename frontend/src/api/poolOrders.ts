import { apiClient } from './client'
export interface PoolResource { id: number; group_id: number; name: string; type: string; status: string; used_5h: number | null; used_7d: number | null }
export interface PoolConfig { title: string; group_id: number; seats: number; price: number; duration_hours: number; total_tokens: number; total_requests: number; concurrency: number; join_deadline: string }
export interface PoolMember { id: number; status: string; key_id: number | null; paid: number; refunded: number; tokens_used: number; requests_used: number; reserved_tokens: number; inflight: number }
export interface PoolOrder extends PoolConfig { id: number; status: string; joined: number; starts_at: string | null; expires_at: string | null; tokens_used: number; requests_used: number; reserved_tokens: number; mine: PoolMember | null; shared_account: PoolResource | null }
export const poolAPI = {
 async list(admin = false): Promise<PoolOrder[]> { return (await apiClient.get<PoolOrder[]>(`${admin ? '/admin' : ''}/pool-orders`)).data },
 async create(config: PoolConfig): Promise<void> { await apiClient.post('/admin/pool-orders', config) },
 async act(id: number, action: 'join' | 'leave' | 'cancel'): Promise<void> { await apiClient.post(`${action === 'cancel' ? '/admin' : ''}/pool-orders/${id}/${action}`) },
 async resources(): Promise<PoolResource[]> { return (await apiClient.get<PoolResource[]>('/admin/pool-resources')).data },
 async createResource(input: { name: string; type: string; concurrency: number; credentials: Record<string, unknown> }): Promise<void> { await apiClient.post('/admin/pool-resources', input) },
 async setResourceStatus(id: number, status: string): Promise<void> { await apiClient.put(`/admin/pool-resources/${id}/status`, { status }) },
}
export function poolError(error: unknown, fallback: string): string {
 const e = error as { response?: { data?: { message?: string } }; message?: string }
 return e?.response?.data?.message || e?.message || fallback
}
