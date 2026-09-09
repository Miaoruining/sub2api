<template>
  <AppLayout>
    <div class="mx-auto max-w-6xl space-y-6">
      <header class="flex flex-wrap items-start justify-between gap-4">
        <div><p class="mb-2 text-sm font-medium text-primary-600">MODELPORT / {{ t('pool.equalShare') }}</p><h1 class="text-2xl font-semibold">{{ t(admin ? 'pool.adminTitle' : 'pool.title') }}</h1><p class="mt-2 max-w-3xl text-sm leading-6 text-gray-500">{{ t('pool.description') }}</p></div>
        <button class="btn btn-secondary" :disabled="busy" @click="load">{{ t('pool.refresh') }}</button>
      </header>
      <p v-if="error" role="alert" class="rounded-xl bg-red-50 p-4 text-sm text-red-700 dark:bg-red-950/30 dark:text-red-300">{{ error }}</p>
      <p v-if="success" role="status" class="rounded-xl bg-emerald-50 p-4 text-sm text-emerald-700 dark:bg-emerald-950/30 dark:text-emerald-300">{{ success }}</p>
      <form v-if="admin" class="card space-y-4 p-6" @submit.prevent="create">
        <div class="flex flex-wrap items-center justify-between gap-3"><h2 class="font-semibold">{{ t('pool.create') }}</h2><router-link to="/admin/pool-resources" class="text-sm text-primary-600">{{ t('pool.resourceTitle') }} →</router-link></div>
        <div class="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          <label class="text-sm">{{ t('pool.name') }}<input v-model="form.title" required maxlength="100" class="input mt-1" :disabled="busy" /></label>
          <label v-for="f in numberFields" :key="f.key" class="text-sm">{{ t(`pool.${f.key}`) }}<input v-model.number="form[f.key]" type="number" required :min="f.min" :max="f.max" :step="f.step || 1" class="input mt-1" :disabled="busy" /></label>
          <label class="text-sm">{{ t('pool.deadline') }}<input v-model="deadline" type="datetime-local" required class="input mt-1" :disabled="busy" /></label>
        </div>
        <p class="text-xs leading-6 text-gray-500">{{ t('pool.createHint') }}</p>
        <button class="btn btn-primary" :disabled="busy">{{ t('pool.create') }}</button>
      </form>
      <section class="rounded-xl border border-gray-200 p-4 text-sm leading-6 dark:border-dark-700">{{ t('pool.rules') }}</section>
      <div v-if="loading" role="status" class="card p-10 text-center text-gray-500">{{ t('pool.loading') }}</div>
      <div v-else-if="!orders.length" class="card p-10 text-center text-gray-500">{{ t('pool.empty') }}</div>
      <div class="grid gap-5 lg:grid-cols-2">
        <article v-for="o in orders" :key="o.id" class="card min-w-0 space-y-5 p-6">
          <div class="flex items-start justify-between gap-3"><div><p class="text-xs text-gray-500">#{{ o.id }} · {{ o.joined }}/{{ o.seats }} {{ t('pool.people') }}</p><h2 class="mt-2 break-words text-lg font-semibold">{{ o.title }}</h2></div><span class="shrink-0 rounded-full bg-gray-100 px-3 py-1 text-xs dark:bg-dark-700">{{ t(`pool.state.${o.status}`) }}</span></div>
          <div class="flex items-baseline gap-2"><strong class="text-3xl tabular-nums">{{ n(o.price) }}</strong><span class="text-sm text-gray-500">{{ t('pool.balanceUnit') }} / {{ t('pool.seat') }} · {{ o.duration_hours }} {{ t('pool.hours') }}</span></div>
          <dl class="grid grid-cols-2 gap-3 rounded-xl bg-gray-50 p-4 text-sm dark:bg-dark-800">
            <div><dt class="text-gray-500">{{ t('pool.sharedTokens') }}</dt><dd class="mt-1 font-medium tabular-nums">{{ n(o.tokens_used) }} / {{ n(o.total_tokens) }}</dd></div>
            <div><dt class="text-gray-500">{{ t('pool.sharedRequests') }}</dt><dd class="mt-1 font-medium tabular-nums">{{ n(o.requests_used) }} / {{ n(o.total_requests) }}</dd></div>
            <div><dt class="text-gray-500">{{ t(o.mine ? 'pool.personalTokens' : 'pool.shareTokens') }}</dt><dd class="mt-1 font-medium tabular-nums"><span v-if="o.mine">{{ n(o.mine.tokens_used) }} / </span> {{ n(Math.floor(o.total_tokens / o.seats)) }}</dd></div>
            <div><dt class="text-gray-500">{{ t(o.mine ? 'pool.personalRequests' : 'pool.shareRequests') }}</dt><dd class="mt-1 font-medium tabular-nums"><span v-if="o.mine">{{ n(o.mine.requests_used) }} / </span> {{ n(Math.floor(o.total_requests / o.seats)) }}</dd></div>
          </dl>
          <div v-if="o.mine?.status === 'joined'" class="space-y-2 text-sm">
            <p>{{ t('pool.remaining') }}: <strong>{{ n(Math.max(0, Math.floor(o.total_tokens / o.seats) - o.mine.tokens_used - o.mine.reserved_tokens)) }}</strong> Tokens</p>
            <progress :value="Math.min(o.mine.tokens_used + o.mine.reserved_tokens, Math.floor(o.total_tokens / o.seats))" :max="Math.floor(o.total_tokens / o.seats)" class="h-2 w-full accent-emerald-600" :aria-label="t('pool.personalTokens')" />
            <p class="text-xs text-gray-500">{{ t('pool.reserved') }} {{ n(o.mine.reserved_tokens) }} Tokens · {{ t('pool.concurrency') }} {{ o.mine.inflight }}/{{ o.concurrency }}</p>
          </div>
          <div v-if="o.shared_account" class="rounded-lg border border-gray-200 p-3 text-xs leading-6 dark:border-dark-700"><p>{{ t('pool.upstream') }}</p><p>5h: {{ percent(o.shared_account.used_5h) }} · 7d: {{ percent(o.shared_account.used_7d) }} · {{ o.shared_account.status }}</p><p class="text-gray-500">{{ t('pool.upstreamHint') }}</p></div>
          <div v-if="o.status === 'awaiting_delivery'" class="rounded-lg bg-amber-50 p-3 text-sm leading-6 text-amber-800 dark:bg-amber-950/30 dark:text-amber-200">
            <p>{{ t('pool.awaitingHint') }}</p><p v-if="o.delivery_deadline">{{ t('pool.deliveryDeadline') }}: {{ date(o.delivery_deadline) }} <strong v-if="new Date(o.delivery_deadline).getTime() < Date.now()"> · {{ t('pool.overdue') }}</strong></p>
            <router-link v-if="admin" :to="{path: '/admin/pool-resources', query: {order: o.id}}" class="btn btn-primary mt-2">{{ t('pool.deliver') }} #{{ o.id }}</router-link>
          </div>
          <p class="text-xs text-gray-500">{{ t(o.expires_at ? 'pool.expires' : 'pool.deadline') }}: {{ date(o.expires_at || o.join_deadline) }}</p>
          <p v-if="o.mine?.status === 'refunded'" class="text-sm text-emerald-700 dark:text-emerald-300">{{ t('pool.refunded') }} {{ n(o.mine.refunded) }} {{ t('pool.balanceUnit') }}</p>
          <div class="flex flex-wrap gap-2">
            <button v-if="!o.mine && o.status === 'forming' && !admin" class="btn btn-primary" :disabled="busy" @click="selected = { order: o, action: 'join' }">{{ t('pool.join') }}</button>
            <button v-if="o.mine?.status === 'joined' && ['forming', 'closed'].includes(o.status)" class="btn btn-secondary" :disabled="busy" @click="selected = { order: o, action: 'leave' }">{{ t('pool.leave') }}</button>
            <router-link v-if="o.mine?.key_id && o.status === 'active' && o.mine.status === 'joined'" to="/keys" class="btn btn-primary">{{ t('pool.getKey') }} #{{ o.mine.key_id }}</router-link>
            <button v-if="admin && ['forming', 'closed', 'active', 'awaiting_delivery'].includes(o.status)" class="btn btn-secondary" :disabled="busy" @click="selected = { order: o, action: 'cancel' }">{{ t('pool.cancelOrder') }}</button>
          </div>
        </article>
      </div>
      <BaseDialog :show="!!selected" :title="selected?.order.title || t('pool.confirm')" width="narrow" :show-close-button="!busy" :close-on-escape="!busy" @close="!busy && (selected = null)">
        <p v-if="selected" class="text-sm leading-6">{{ t(`pool.confirm_${selected.action}`, { price: n(selected.order.price) }) }}</p>
        <template #footer><div class="flex gap-3"><button class="btn btn-primary" :disabled="busy" @click="act">{{ t('pool.confirm') }}</button><button class="btn btn-secondary" :disabled="busy" @click="selected = null">{{ t('pool.back') }}</button></div></template>
      </BaseDialog>
    </div>
  </AppLayout>
</template>
<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import { useAuthStore } from '@/stores/auth'
import AppLayout from '@/components/layout/AppLayout.vue'
import { poolAPI, poolError, type PoolConfig, type PoolOrder } from '@/api/poolOrders'
const props = defineProps<{ admin?: boolean }>()
const { t } = useI18n()
const auth = useAuthStore()
const orders = ref<PoolOrder[]>([]), busy = ref(false), loading = ref(true), error = ref(''), success = ref('')
const selected = ref<{ order: PoolOrder; action: 'join' | 'leave' | 'cancel' } | null>(null)
const deadline = ref('')
const form = ref<PoolConfig>({ title: '', seats: 4, price: 10, duration_hours: 720, total_tokens: 4000000, total_requests: 4000, concurrency: 1, join_deadline: '' })
type NumberKey = 'seats' | 'price' | 'duration_hours' | 'total_tokens' | 'total_requests' | 'concurrency'
const numberFields: { key: NumberKey; min: number; max: number; step?: number }[] = [
 { key: 'seats', min: 2, max: 50 }, { key: 'price', min: 0.01, max: 1000000, step: 0.01 }, { key: 'duration_hours', min: 1, max: 8760 }, { key: 'total_tokens', min: 16384, max: 1000000000000 }, { key: 'total_requests', min: 2, max: 1000000000 }, { key: 'concurrency', min: 1, max: 10 },
]
const n = (value: number) => value.toLocaleString(undefined, { maximumFractionDigits: 8 })
const date = (value: string) => new Date(value).toLocaleString()
const percent = (value: number | null) => value == null ? t('pool.unknown') : `${n(Math.max(0, 100 - value))}% ${t('pool.remaining')}`
async function fetchData() { orders.value = await poolAPI.list(props.admin) }
async function load() { if (busy.value) return; busy.value = true; error.value = ''; try { await fetchData() } catch (e) { error.value = poolError(e, t('pool.failed')) } finally { busy.value = false; loading.value = false } }
async function create() { if (busy.value) return; busy.value = true; error.value = ''; success.value = ''; try { await poolAPI.create({ ...form.value, join_deadline: new Date(deadline.value).toISOString() }); success.value = t('pool.created'); await fetchData() } catch (e) { error.value = poolError(e, t('pool.failed')) } finally { busy.value = false } }
async function act() { if (busy.value || !selected.value) return; busy.value = true; error.value = ''; success.value = ''; try { await poolAPI.act(selected.value.order.id, selected.value.action); selected.value = null; success.value = t('pool.saved'); await Promise.all([fetchData(), auth.refreshUser()]) } catch (e) { error.value = poolError(e, t('pool.failed')); selected.value = null } finally { busy.value = false } }
let timer: ReturnType<typeof setInterval> | undefined
onMounted(() => { void load(); timer = setInterval(() => { if (!busy.value && !selected.value && !document.hidden) void load() }, 30000) })
onUnmounted(() => { if (timer) clearInterval(timer) })
</script>
