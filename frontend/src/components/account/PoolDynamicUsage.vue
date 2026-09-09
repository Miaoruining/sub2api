<template>
  <section class="card space-y-3 p-4" data-testid="pool-dynamic-usage">
    <div class="flex flex-wrap items-center justify-between gap-2">
      <h2 class="font-medium">{{ t('pool.dynamicUsageTitle') }}</h2>
      <button class="btn btn-secondary" :disabled="busy" @click="load">{{ t('pool.refresh') }}</button>
    </div>
    <p class="text-xs leading-6 text-gray-500">{{ t('pool.dynamicUsageHint') }}</p>
    <p v-if="error" role="alert" class="text-sm text-red-600">{{ error }}</p>
    <p v-else-if="!visible.length && !busy" class="text-sm text-gray-500">{{ t('pool.noDynamicUsage') }}</p>
    <p v-else-if="busy" role="status" class="text-sm text-gray-500">{{ t('pool.loading') }}</p>
    <ul v-else class="divide-y divide-gray-100 text-sm dark:divide-dark-700">
      <li v-for="record in visible" :key="record.id" class="space-y-3 py-3">
        <div class="flex flex-wrap items-start justify-between gap-2">
          <div>
            <span class="text-violet-600">{{ t('pool.poolUsage') }} #{{ record.order_id }}</span>
            <span class="ml-3 text-gray-500">{{ date(record.created_at) }}</span>
            <p class="mt-1 text-xs text-gray-400">{{ record.id }} · {{ t('pool.dynamicKey') }} #{{ record.key_id }}</p>
          </div>
          <div class="text-right">
            <strong class="tabular-nums">{{ money(record.credit) }}</strong>
            <span class="ml-2 text-xs text-gray-500">{{ t('pool.standardReference') }}</span>
            <p class="mt-1 text-xs" :class="statusClass(record.status)">{{ statusLabel(record.status) }}</p>
          </div>
        </div>
        <div v-if="record.windows?.length" class="grid gap-2 sm:grid-cols-2">
          <div v-for="window in record.windows" :key="window.key" class="rounded-lg bg-gray-50 p-3 text-xs dark:bg-dark-800">
            <div class="flex items-center justify-between gap-2">
              <span class="font-medium">{{ windowLabel(window.key) }}</span>
              <span class="text-gray-500">{{ statusLabel(window.status) }}</span>
            </div>
            <p class="mt-2 tabular-nums">
              {{ t('pool.dynamicAccountRemaining') }} {{ pp(window.account_remaining_percent) }} · {{ t('pool.dynamicUsed') }} {{ pp(window.used_percent) }} · {{ t('pool.dynamicReserved') }} {{ pp(window.reserved_percent) }}
            </p>
            <p class="mt-1 tabular-nums">
              {{ t('pool.dynamicRemaining') }} {{ pp(window.remaining_percent) }} / {{ t('pool.dynamicEntitlement') }} {{ pp(window.entitlement_percent) }}
            </p>
          </div>
        </div>
        <p v-else class="text-xs text-amber-700 dark:text-amber-300">{{ t('pool.dynamicWaiting') }}</p>
      </li>
    </ul>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { poolAPI, poolError, type PoolDynamicStatus, type PoolDynamicUsage } from '@/api/poolOrders'

const props = defineProps<{ keyId?: number | null }>()
const { t } = useI18n()
const rows = ref<PoolDynamicUsage[]>([])
const busy = ref(false)
const error = ref('')
const visible = computed(() => rows.value.filter((record) => !props.keyId || record.key_id === props.keyId))

const pp = (value: number | null | undefined) => {
  if (value === null || value === undefined || !Number.isFinite(value)) return t('pool.dynamicWaiting')
  if (value > 0 && value < 0.0001) return '<0.0001 pp'
  return `${value.toLocaleString(undefined, { maximumFractionDigits: 8 })} pp`
}
const money = (value: number | null | undefined) => value === null || value === undefined ? '—' : `$${value.toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 8 })}`
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
const statusClass = (status: PoolDynamicStatus | undefined) => ['calibrated', 'ready'].includes(status || '') ? 'text-emerald-600' : ['blocked', 'exhausted'].includes(status || '') ? 'text-red-600' : 'text-amber-600'

async function load() {
  busy.value = true
  error.value = ''
  try {
    rows.value = await poolAPI.dynamicUsage()
  } catch (e) {
    error.value = poolError(e, t('pool.failed'))
  } finally {
    busy.value = false
  }
}

onMounted(load)
</script>
