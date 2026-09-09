<template>
        <article  class="card min-w-0 space-y-5 p-6">
          <div class="flex items-start justify-between gap-3"><div><p class="text-xs text-gray-500">#{{ o.id }} · {{ o.joined }}/{{ o.seats }} {{ t('pool.people') }}</p><h2 class="mt-2 break-words text-lg font-semibold">{{ o.title }}</h2></div><span class="shrink-0 rounded-full px-3 py-1 text-xs font-semibold" :class="o.status === 'forming' ? 'bg-red-50 text-red-600 dark:bg-red-950/40 dark:text-red-300' : 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-300'">{{ t(`pool.state.${o.status}`) }}</span></div>
          <div class="flex items-baseline gap-2"><strong class="text-3xl tabular-nums">{{ n(o.price) }}</strong><span class="text-sm text-gray-500">{{ t('pool.balanceUnit') }} / {{ t('pool.seat') }} · {{ n(o.duration_days ?? o.duration_hours / 24) }} {{ t('pool.days') }}</span></div>
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
            <button v-if="!o.mine && o.status === 'forming' && !admin" class="btn btn-primary" :disabled="busy" @click="$emit('act', o, 'join')">{{ t('pool.join') }}</button>
            <button v-if="o.mine?.status === 'joined' && ['forming', 'closed'].includes(o.status)" class="btn btn-secondary" :disabled="busy" @click="$emit('act', o, 'leave')">{{ t('pool.leave') }}</button>
            <router-link v-if="o.mine?.key_id && o.status === 'active' && o.mine.status === 'joined'" to="/keys" class="btn btn-primary">{{ t('pool.getKey') }} #{{ o.mine.key_id }}</router-link>
            <button v-if="admin && ['forming', 'closed', 'active', 'awaiting_delivery'].includes(o.status)" class="btn btn-secondary" :disabled="busy" @click="$emit('act', o, 'cancel')">{{ t('pool.cancelOrder') }}</button>
          </div>
        </article>
</template>
<script setup lang="ts">
import {useI18n} from 'vue-i18n'
import type {PoolOrder} from '@/api/poolOrders'
defineProps<{o:PoolOrder;admin?:boolean;busy:boolean}>()
defineEmits<{act:[order:PoolOrder,action:'join'|'leave'|'cancel']}>()
const {t}=useI18n()
const n=(v:number)=>v.toLocaleString(undefined,{maximumFractionDigits:8})
const date=(v:string)=>new Date(v).toLocaleString()
const percent=(v:number|null)=>v==null?t('pool.unknown'):`${n(Math.max(0,100-v))}% ${t('pool.remaining')}`
</script>
