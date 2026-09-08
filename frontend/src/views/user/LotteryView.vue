<template>
  <AppLayout>
    <div class="mx-auto max-w-6xl space-y-6">
      <header class="flex flex-wrap items-end justify-between gap-4">
        <div>
          <p class="mb-2 text-xs font-semibold tracking-wide text-rose-700 dark:text-rose-300">{{ t(status?.admin_repeat ? 'lottery.adminSchedule' : 'lottery.schedule') }}</p>
          <h2 class="text-3xl font-semibold tracking-tight text-gray-900 dark:text-white">{{ t('lottery.title') }}</h2>
        </div>
        <span class="rounded-lg bg-rose-50 px-3 py-2 text-sm font-medium text-rose-700 dark:bg-rose-950/40 dark:text-rose-300">{{ t(status?.admin_repeat ? 'lottery.adminFree' : 'lottery.free') }}</span>
      </header>

      <p v-if="error" role="alert" class="rounded-xl bg-red-50 p-4 text-sm text-red-700 dark:bg-red-950/30 dark:text-red-300">{{ error }}</p>
      <div v-if="!status && loading" class="grid gap-6 lg:grid-cols-3" :aria-label="t('common.loading')" aria-busy="true">
        <div class="h-80 animate-pulse rounded-2xl bg-gray-100 dark:bg-dark-800 lg:col-span-2" />
        <div class="h-80 animate-pulse rounded-2xl bg-gray-100 dark:bg-dark-800" />
      </div>
      <section v-if="status" class="grid gap-6 lg:grid-cols-3">
        <div class="lottery-stage rounded-2xl p-5 sm:p-8 lg:col-span-2" :class="{ 'is-drawing': drawing }" :aria-busy="drawing">
          <h3 class="text-xl font-semibold text-gray-900 dark:text-white">{{ t('lottery.prizeTitle') }}</h3>
          <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">{{ t('lottery.prizeSubtitle') }}</p>
          <div class="mt-7 grid grid-cols-3 gap-3">
            <div v-for="prize in status.prizes.filter(p => p > 0)" :key="prize" class="prize-tile" :class="{ selected: status.today?.prize === prize }">
              <span class="text-3xl font-semibold tabular-nums sm:text-4xl">{{ prize }}</span>
              <span class="mt-2 text-xs text-gray-500 dark:text-dark-400">{{ t('lottery.quota') }}</span>
            </div>
          </div>
          <div class="mt-3 flex items-center justify-between gap-3 rounded-xl bg-white/60 px-4 py-3 text-sm dark:bg-dark-900/40">
            <span class="font-medium text-gray-700 dark:text-dark-200">{{ t('lottery.noPrize') }}</span>
            <span class="text-xs text-gray-500 dark:text-dark-400">{{ noPrizeHint }}</span>
          </div>
        </div>

        <div class="flex flex-col rounded-2xl border border-gray-200 bg-white p-6 dark:border-dark-700 dark:bg-dark-900">
          <Icon name="gift" size="xl" class="mb-5 text-rose-600 dark:text-rose-400" />
          <h3 class="text-lg font-semibold text-gray-900 dark:text-white" role="status">{{ stateText }}</h3>
          <p v-if="status.state === 'not_open'" class="mt-4 font-mono text-3xl font-semibold tabular-nums text-rose-700 dark:text-rose-300">{{ countdown }}</p>
          <p class="mt-3 text-sm leading-6 text-gray-500 dark:text-dark-400">{{ t(status.admin_repeat ? 'lottery.adminQualification' : 'lottery.qualification') }}</p>
          <p class="mt-3 text-xs font-medium" :class="status.eligible ? 'text-emerald-700 dark:text-emerald-300' : 'text-gray-500 dark:text-dark-400'">
            {{ t(status.eligible ? 'lottery.eligible' : 'lottery.ineligible') }}
          </p>
          <div class="mt-auto space-y-3 pt-7">
            <button class="draw-button" :disabled="drawing || (!pendingDraw && effectiveState !== 'ready') || loading" @click="draw">
              {{ t(drawing ? 'lottery.drawing' : pendingDraw ? 'lottery.retryDraw' : status.admin_repeat && status.today ? 'lottery.again' : 'lottery.draw') }}
            </button>
            <router-link v-if="!status.eligible" to="/purchase" class="btn btn-secondary w-full">{{ t('lottery.recharge') }}</router-link>
          </div>
        </div>
      </section>

      <section v-if="result" class="rounded-2xl border border-rose-200 bg-rose-50 p-6 dark:border-rose-900 dark:bg-rose-950/30" role="status" aria-live="polite">
        <p class="text-sm text-rose-700 dark:text-rose-300">{{ t(status?.admin_repeat ? 'lottery.latestResult' : 'lottery.result') }}</p>
        <h3 class="mt-2 text-2xl font-semibold text-gray-900 dark:text-white">{{ result.prize > 0 ? t('lottery.won', { amount: result.prize }) : noPrizeHint }}</h3>
      </section>
      <div class="flex justify-end">
        <button class="btn btn-secondary" :disabled="loading || drawing" @click="load()">{{ t('lottery.refresh') }}</button>
      </div>

      <section class="card overflow-hidden">
        <div class="border-b border-gray-100 p-5 dark:border-dark-700">
          <h3 class="font-semibold text-gray-900 dark:text-white">{{ t('lottery.history') }}</h3>
          <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">{{ t('lottery.historyNote') }}</p>
        </div>
        <div v-if="status?.history.length" class="overflow-x-auto">
          <table class="w-full text-left text-sm">
            <thead class="bg-gray-50 text-xs text-gray-500 dark:bg-dark-800 dark:text-dark-400"><tr>
              <th class="px-5 py-3">{{ t('lottery.date') }}</th><th class="px-5 py-3">{{ t('lottery.recordPrize') }}</th><th class="px-5 py-3">{{ t('lottery.time') }}</th>
            </tr></thead>
            <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
              <tr v-for="item in status.history" :key="item.id">
                <td class="px-5 py-4 tabular-nums">{{ item.activity_date }}</td>
                <td class="px-5 py-4 font-medium" :class="item.prize > 0 ? 'text-rose-700 dark:text-rose-300' : 'text-gray-500'">{{ item.prize > 0 ? `+${item.prize} ${t('lottery.quota')}` : t('lottery.noPrize') }}</td>
                <td class="px-5 py-4 tabular-nums">{{ formatTime(item.created_at) }}</td>
              </tr>
            </tbody>
          </table>
        </div>
        <div v-else-if="status" class="px-6 py-12 text-center">
          <p class="font-medium text-gray-700 dark:text-dark-200">{{ t('lottery.empty') }}</p>
          <p class="mt-2 text-sm text-gray-500 dark:text-dark-400">{{ t('lottery.emptyHint') }}</p>
        </div>
      </section>
      <section class="rounded-xl bg-gray-50 p-5 text-sm leading-7 text-gray-600 dark:bg-dark-800/50 dark:text-dark-400">
        <h3 class="mb-2 font-medium text-gray-900 dark:text-white">{{ t('lottery.rules') }}</h3>
        <p v-if="status?.admin_repeat">{{ t('lottery.adminOnce') }}</p>
        <template v-else><p>{{ t('lottery.once') }}</p><p>{{ t('lottery.timeRule') }}</p><p>{{ t('lottery.qualificationNote') }}</p></template>
        <p>{{ t('lottery.creditRule') }}</p>
      </section>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import { lotteryAPI, type LotteryDraw, type LotteryStatus } from '@/api/lottery'
import { useAuthStore } from '@/stores/auth'

const { t } = useI18n()
const auth = useAuthStore()
const status = ref<LotteryStatus | null>(null)
const result = ref<LotteryDraw | null>(null)
const loading = ref(false)
const drawing = ref(false)
const error = ref('')
// 未确认的管理员请求保留同一个编号，不能因网络重试而重新开奖。
const pendingDraw = ref<{ date: string; id: string } | null>(null)
const now = ref(Date.now())
let offset = 0
let ticks = 0
let timer: ReturnType<typeof setInterval> | undefined
let disposed = false
const windowEnded = computed(() => !!status.value && !status.value.admin_repeat && now.value >= Date.parse(status.value.closes_at))
const effectiveState = computed(() => status.value?.state === 'ready' && windowEnded.value ? 'ended' : status.value?.state)
const stateText = computed(() => {
  if (!status.value) return ''
  const state = effectiveState.value
  return t(`lottery.${status.value.admin_repeat && state === 'ready' ? 'adminReady' : status.value.admin_repeat && state === 'ended' ? 'adminEnded' : state}`)
})
const noPrizeHint = computed(() => t(status.value?.admin_repeat ? 'lottery.adminNoPrizeHint' : 'lottery.noPrizeHint'))
const countdown = computed(() => {
  const seconds = Math.max(0, Math.ceil((Date.parse(status.value?.opens_at || '') - now.value) / 1000)) || 0
  return [Math.floor(seconds / 3600), Math.floor(seconds / 60) % 60, seconds % 60].map(n => String(n).padStart(2, '0')).join(':')
})
function formatTime(value: string) { return new Date(value).toLocaleTimeString('zh-CN', { timeZone: 'Asia/Shanghai', hour12: false }) }
async function load(background = false) {
  if (loading.value || disposed) return
  loading.value = true
  if (!background) error.value = ''
  try {
    const data = await lotteryAPI.status()
    if (disposed) return
    status.value = data
    offset = Date.parse(data.server_time) - Date.now()
    now.value = Date.now() + offset
    result.value = data.today
  } catch {
    error.value = t('lottery.loadError')
  } finally { loading.value = false }
}
async function draw() {
  if (drawing.value || loading.value || !status.value || (!pendingDraw.value && effectiveState.value !== 'ready')) return
  const activityDate = pendingDraw.value?.date || status.value.activity_date
  const adminAttempt = !!pendingDraw.value || !!status.value.admin_repeat
  drawing.value = true
  error.value = ''
  try {
    if (adminAttempt && !pendingDraw.value) pendingDraw.value = { date: activityDate, id: crypto.randomUUID() }
    result.value = pendingDraw.value ? await lotteryAPI.draw(activityDate, pendingDraw.value.id) : await lotteryAPI.draw(activityDate)
    pendingDraw.value = null
    // 先锁定本地状态，后续刷新失败也不能让按钮重新可点。
    if (status.value) { status.value.today = result.value; status.value.state = 'drawn' }
    await auth.refreshUser().catch(() => undefined)
    await load(true)
  } catch (cause) {
    // 明确拒绝的请求未发奖；未知网络或服务端结果则保留编号供安全重试。
    const code = (cause as { status?: number })?.status
    if (code && code >= 400 && code < 500) pendingDraw.value = null
    await load(true)
    if (adminAttempt) error.value = t(pendingDraw.value ? 'lottery.adminDrawError' : 'lottery.drawError')
    else if (!status.value?.today) error.value = t('lottery.drawError')
    else if (status.value.today.prize > 0) await auth.refreshUser().catch(() => undefined)
  } finally { drawing.value = false }
}
onMounted(() => {
  void load()
  timer = setInterval(() => {
    const wasEnded = windowEnded.value
    now.value = Date.now() + offset
    ticks++
    if (!drawing.value && document.visibilityState === 'visible' && (ticks % 30 === 0 || (!wasEnded && windowEnded.value) || (status.value?.state === 'not_open' && countdown.value === '00:00:00'))) void load(true)
  }, 1000)
})
onUnmounted(() => { disposed = true; if (timer) clearInterval(timer) })
</script>

<style scoped>
.lottery-stage { background: radial-gradient(ellipse at top right, #ffe4e6, #fff7f5 65%); border: 1px solid #f5d8db; }
:global(.dark .lottery-stage) { background: radial-gradient(ellipse at top right, #43212b, #201a20 70%); border-color: #52313b; }
.prize-tile { display: flex; flex-direction: column; align-items: center; justify-content: center; min-height: 7.5rem; border-radius: .9rem; background: #fff; color: #b42a3c; border: 1px solid #f3e2e5; }
:global(.dark .lottery-stage .prize-tile) { background: #2b2229; color: #fda4af; border-color: #4e303a; }
.prize-tile.selected { outline: 2px solid #c72d40; outline-offset: 2px; }
.draw-button { width: 100%; min-height: 3rem; padding: .75rem 1rem; border-radius: .75rem; background: #c72d40; color: white; font-size: .875rem; font-weight: 600; transition: background .2s, transform .2s; }
.draw-button:hover:not(:disabled) { background: #ae2435; }
.draw-button:active:not(:disabled) { transform: scale(.98); }
.draw-button:focus-visible { outline: 3px solid #fb7185; outline-offset: 3px; }
.draw-button:disabled { opacity: .45; cursor: not-allowed; }
.is-drawing .prize-tile { animation: reveal-pulse 1.4s ease-in-out infinite; }
@keyframes reveal-pulse { 50% { opacity: .55; transform: translateY(-2px); } }
@media (prefers-reduced-motion: reduce) { .is-drawing .prize-tile { animation: none; } .draw-button { transition: none; } }
</style>
