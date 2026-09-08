<template>
  <AppLayout>
    <div class="mx-auto max-w-6xl space-y-6">
      <header class="flex flex-wrap items-center justify-between gap-4">
        <div><h2 class="text-2xl font-semibold">{{ t('lottery.adminTitle') }}</h2><p class="mt-2 text-sm text-gray-500">{{ t('lottery.adminOnly') }}</p></div>
        <label class="text-sm">{{ t('lottery.date') }}<input v-model="date" type="date" class="input mt-1" :disabled="busy" @change="load" /></label>
      </header>
      <p v-if="error" role="alert" class="rounded-lg bg-red-50 p-4 text-red-700 dark:bg-red-950/30 dark:text-red-300">{{ error }}</p>
      <p v-if="success" role="status" class="rounded-lg bg-emerald-50 p-4 text-emerald-700 dark:bg-emerald-950/30 dark:text-emerald-300">{{ success }}</p>
      <button class="btn btn-secondary" :disabled="busy" @click="load">{{ t('lottery.refresh') }}</button>
      <template v-if="status">
        <section class="card flex items-center justify-between gap-4 p-6">
          <div><h3 class="font-semibold">{{ t('lottery.enabled') }}</h3><p class="mt-1 text-sm text-gray-500">{{ t('lottery.enableHint') }}</p></div>
          <input type="checkbox" role="switch" :aria-label="t('lottery.enabled')" :checked="status.enabled" :disabled="busy" class="h-6 w-6 accent-rose-600" @change="toggle" />
        </section>
        <dl class="grid grid-cols-1 gap-4 sm:grid-cols-3">
          <div v-for="item in summaries" :key="item.label" class="card p-6"><dt class="text-sm text-gray-500">{{ item.label }}</dt><dd class="mt-3 text-3xl font-semibold tabular-nums">{{ item.value }}</dd></div>
        </dl>
        <form class="card p-5 sm:p-6" @submit.prevent="saveWeights">
          <h3 class="text-lg font-semibold">{{ t('lottery.configuration') }}</h3>
          <p class="mt-2 text-sm text-gray-500">{{ t('lottery.effective', { date: status.next_effective_date }) }}</p>
          <div class="mt-5 overflow-x-auto"><table class="w-full text-left text-sm">
            <thead><tr class="text-gray-500"><th class="py-3">{{ t('lottery.recordPrize') }}</th><th>{{ t('lottery.currentProbability') }}</th><th>{{ t('lottery.count') }}</th><th>{{ t('lottery.probability') }}</th></tr></thead>
            <tbody><tr v-for="(prize, i) in prizes" :key="prize" class="border-t border-gray-100 dark:border-dark-700">
              <td class="py-3">{{ prize ? `${prize} ${t('lottery.quota')}` : t('lottery.noPrize') }}</td><td>{{ (status.weights[i] / 100).toFixed(2) }}%</td><td>{{ status.distribution[i] }}</td>
              <td class="py-2"><input v-model="probabilities[i]" type="number" min="0" max="100" step="0.01" required :aria-label="`${prize} ${t('lottery.probability')}`" :disabled="busy" class="input max-w-28" /></td>
            </tr></tbody>
          </table></div>
          <div class="mt-5 flex items-center justify-between"><span class="text-sm tabular-nums">{{ t('lottery.total') }}: {{ total.toFixed(2) }}%</span><button class="btn btn-primary" :disabled="busy">{{ t('lottery.save') }}</button></div>
        </form>
        <section class="card overflow-hidden">
          <div class="p-5"><h3 class="font-semibold">{{ t('lottery.audit') }}</h3><p class="mt-1 text-xs text-gray-500">{{ t('lottery.auditNote') }}</p></div>
          <div class="overflow-x-auto"><table class="w-full text-left text-sm"><thead class="bg-gray-50 dark:bg-dark-800"><tr>
            <th class="p-4">ID</th><th class="p-4">{{ t('lottery.userId') }}</th><th class="p-4">{{ t('lottery.recordPrize') }}</th><th class="p-4">{{ t('lottery.balanceAfter') }}</th><th class="p-4">{{ t('lottery.time') }}</th>
          </tr></thead><tbody><tr v-for="record in status.records" :key="record.id" class="border-t border-gray-100 dark:border-dark-700">
            <td class="p-4">{{ record.id }}</td><td class="p-4">{{ record.user_id }}</td><td class="p-4">{{ record.prize }}</td><td class="p-4 tabular-nums">{{ record.balance_after.toFixed(2) }}</td><td class="p-4">{{ new Date(record.created_at).toLocaleString('zh-CN', { timeZone: 'Asia/Shanghai' }) }}</td>
          </tr></tbody></table></div>
          <p v-if="!status.records.length" class="p-8 text-center text-sm text-gray-500">{{ t('lottery.empty') }}</p>
        </section>
      </template>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import { lotteryAdminAPI, type LotteryAdminStatus } from '@/api/lottery'
const { t } = useI18n()
const status = ref<LotteryAdminStatus | null>(null)
const date = ref('')
const busy = ref(false)
const error = ref('')
const success = ref('')
const probabilities = ref<(string | number)[]>([])
const prizes = [0, 1, 5, 10, 20, 50, 100]
const total = computed(() => probabilities.value.reduce<number>((sum, p) => sum + (Number(p) || 0), 0))
const summaries = computed(() => [
  { label: t('lottery.budget'), value: status.value?.daily_budget },
  { label: t('lottery.spent'), value: status.value?.spent },
  { label: t('lottery.draws'), value: status.value?.draw_count },
])
async function refreshData() {
  status.value = await lotteryAdminAPI.status(date.value || undefined)
  date.value = status.value.activity_date
  probabilities.value = status.value.next_weights.map(w => (w / 100).toFixed(2))
}
async function load() {
  if (busy.value) return
  busy.value = true; error.value = ''; success.value = ''
  try { await refreshData() } catch { error.value = t('lottery.adminError') } finally { busy.value = false }
}
async function configure(config: { enabled?: boolean; weights?: number[] }) {
  if (busy.value) return
  busy.value = true; error.value = ''; success.value = ''
  try { await lotteryAdminAPI.configure(config); await refreshData(); success.value = t('lottery.saved') }
  catch { error.value = t('lottery.adminError') }
  finally { busy.value = false }
}
function toggle(event: Event) {
  const input = event.target as HTMLInputElement
  const enabled = input.checked
  input.checked = status.value?.enabled ?? false
  void configure({ enabled })
}
function saveWeights() {
  success.value = ''
  const valid = probabilities.value.length === 7 && probabilities.value.every(p => /^\d+(\.\d{1,2})?$/.test(String(p)) && Number(p) >= 0 && Number(p) <= 100)
  const weights = probabilities.value.map(p => Math.round(Number(p) * 100))
  if (!valid || weights.reduce((a, b) => a + b, 0) !== 10000) { error.value = t('lottery.invalid'); return }
  void configure({ weights })
}
onMounted(load)
</script>
