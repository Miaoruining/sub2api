<template>
  <section class="space-y-3" data-testid="pool-dynamic-quota-summary">
    <div class="flex flex-wrap items-center justify-between gap-2 text-sm">
      <span class="font-medium">{{ t('pool.dynamicQuota') }}</span>
      <span class="rounded-full bg-primary-50 px-2.5 py-1 text-xs font-medium text-primary-700 dark:bg-primary-950/30 dark:text-primary-300">
        {{ t(`pool.quotaMode_${mode}`) }}
      </span>
    </div>

    <span v-if="seats > 0" class="inline-flex rounded-full bg-primary-50 px-2.5 py-1 text-xs font-medium text-primary-700 dark:bg-primary-950/30 dark:text-primary-300">
      {{ t('pool.subscriptionShare', { seats, plan: planLabel }) }}
    </span>

    <dl v-if="mode === 'dynamic_shadow'" class="grid grid-cols-2 gap-3 rounded-lg bg-gray-50 p-3 text-xs dark:bg-dark-800">
      <div><dt class="text-gray-500">{{ t('pool.dynamicFallbackCycle') }}</dt><dd class="mt-1 font-medium">{{ moneyPerSeat(totalCredit) }}</dd></div>
      <div v-if="plan !== 'pro'"><dt class="text-gray-500">{{ t('pool.dynamicFallback5h') }}</dt><dd class="mt-1 font-medium">{{ moneyPerSeat(credit5h) }}</dd></div>
      <div v-if="plan !== 'pro'"><dt class="text-gray-500">{{ t('pool.dynamicFallback7d') }}</dt><dd class="mt-1 font-medium">{{ moneyPerSeat(credit7d) }}</dd></div>
    </dl>

    <p v-if="undelivered" class="rounded-lg bg-gray-50 p-4 text-sm leading-6 text-gray-600 dark:bg-dark-800 dark:text-gray-300">
      {{ t('pool.dynamicUndelivered') }}
    </p>
    <template v-else-if="!quota || !quota.windows?.length">
      <p class="rounded-lg bg-amber-50 p-4 text-sm leading-6 text-amber-800 dark:bg-amber-950/30 dark:text-amber-200">
        {{ t('pool.dynamicWaiting') }}
      </p>
    </template>
    <template v-else>
      <div class="flex flex-wrap items-center gap-2 text-xs text-gray-500">
        <span>{{ t('pool.dynamicState') }}: {{ statusLabel(quota.status) }}</span>
        <span v-if="quota.shadow" class="rounded-full bg-amber-100 px-2 py-0.5 text-amber-800 dark:bg-amber-900/40 dark:text-amber-200">{{ t('pool.quotaMode_dynamic_shadow') }}</span>
      </div>
      <p class="text-xs leading-5 text-gray-500">{{ t('pool.dynamicScope') }}</p>
      <p class="text-xs leading-5 text-gray-500">{{ t('pool.dynamicDenominator') }}</p>
      <div class="grid gap-3 sm:grid-cols-2">
        <article v-for="window in quota.windows" :key="window.key" class="rounded-lg border border-gray-200 p-3 dark:border-dark-700">
          <div class="flex items-start justify-between gap-2">
            <h3 class="font-medium">{{ windowLabel(window.key) }}</h3>
            <span class="text-xs text-gray-500">{{ statusLabel(window.status) }}</span>
          </div>
          <dl class="mt-3 grid grid-cols-2 gap-x-3 gap-y-2 text-xs">
            <div><dt class="text-gray-500">{{ t('pool.dynamicAccountRemaining') }}</dt><dd class="mt-1 font-medium tabular-nums">{{ pp(window.account_remaining_percent) }}</dd></div>
            <div><dt class="text-gray-500">{{ t('pool.dynamicEntitlement') }}</dt><dd class="mt-1 font-medium tabular-nums">{{ pp(window.entitlement_percent) }}</dd></div>
            <div><dt class="text-gray-500">{{ t('pool.dynamicRemaining') }}</dt><dd class="mt-1 font-medium tabular-nums">{{ pp(window.remaining_percent) }}</dd></div>
            <div><dt class="text-gray-500">{{ t('pool.dynamicUsed') }}</dt><dd class="mt-1 font-medium tabular-nums">{{ pp(window.used_percent) }}</dd></div>
            <div><dt class="text-gray-500">{{ t('pool.dynamicReserved') }}</dt><dd class="mt-1 font-medium tabular-nums">{{ pp(window.reserved_percent) }}</dd></div>
          </dl>
          <p v-if="window.reset_at" class="mt-2 text-xs text-gray-500">{{ t('pool.resetsAt') }} {{ date(window.reset_at) }}</p>
          <p v-if="window.observed_at" class="mt-1 text-xs text-gray-500">{{ t('pool.dynamicObservedAt') }} {{ date(window.observed_at) }}</p>
        </article>
      </div>
    </template>
  </section>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { PoolDynamicQuota, PoolDynamicStatus, PoolQuotaMode } from '@/api/poolOrders'

const props = withDefaults(defineProps<{
  mode: Extract<PoolQuotaMode, 'dynamic' | 'dynamic_shadow'>
  seats: number
  plan?: 'plus' | 'pro'
  quota?: PoolDynamicQuota | null
  undelivered?: boolean
  totalCredit?: number
  credit5h?: number
  credit7d?: number
}>(), { plan: 'plus', quota: null, undelivered: false, totalCredit: 0, credit5h: 0, credit7d: 0 })

const { t } = useI18n()
const mode = computed(() => props.mode)
const planLabel = computed(() => props.plan === 'pro' ? 'Pro' : 'Plus')
const money = (value: number | null | undefined) => value === null || value === undefined || value <= 0 ? t('pool.dynamicWaiting') : `$${value.toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 8 })}`
const moneyPerSeat = (value: number | null | undefined) => value === null || value === undefined || value <= 0 || props.seats <= 0 ? t('pool.dynamicWaiting') : money(value / props.seats)

const pp = (value: number | null | undefined) => {
  if (value === null || value === undefined || !Number.isFinite(value)) return t('pool.dynamicWaiting')
  if (value > 0 && value < 0.0001) return '<0.0001 pp'
  return `${value.toLocaleString(undefined, { maximumFractionDigits: 8 })} pp`
}

const date = (value: string) => new Date(value).toLocaleString()
const windowLabel = (key: string) => {
  if (key === '5h' || key === '5_hour' || key === '5-hour' || key === 'codex/300') return t('pool.dynamicWindow5h')
  if (key === '7d' || key === 'weekly' || key === 'week' || key === 'codex/10080') return t('pool.dynamicWindow7d')
  const match = key.match(/(?:\/|:|_)(\d+)$/)
  if (match) return t('pool.dynamicWindowDuration', { minutes: Number(match[1]) })
  return key
}
const statusLabel = (status: PoolDynamicStatus | undefined) => {
  const aliases: Record<string, string> = { pending: 'estimated', settled: 'estimated', completed: 'estimated' }
  const key = status ? (aliases[status] || status) : ''
  const known = ['ready', 'stale', 'unknown', 'estimated', 'calibrated', 'uncertain', 'blocked', 'exhausted']
  return known.includes(key) ? t(`pool.dynamicStatus.${key}`) : t('pool.dynamicUnknown')
}
</script>
