<template>
  <AppLayout>
    <div class="mx-auto max-w-6xl space-y-6">
      <header class="flex flex-wrap items-end justify-between gap-4">
        <div>
          <p class="mb-2 text-xs font-semibold tracking-wide text-rose-700 dark:text-rose-300">{{ t(status?.admin_repeat ? 'lottery.adminSchedule' : makeupDay ? 'lottery.makeupSchedule' : 'lottery.schedule') }}</p>
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
          <p class="mt-1 text-sm text-gray-500 dark:text-dark-400" role="status">{{ t(drawing ? revealPhase === 'settling' ? 'lottery.settling' : 'lottery.spinning' : 'lottery.prizeSubtitle') }}</p>
          <div class="lottery-ring mt-7">
            <div v-for="(slot, slotIndex) in lotteryRing" :key="slotIndex" class="prize-tile" :data-prize="slot.prize" :data-slot="slotIndex" :style="{ gridColumn: slot.x + 1, gridRow: slot.y + 1 }" :class="{ 'no-prize-tile': slot.prize === 0, active: drawing && activeSlot === slotIndex, selected: !drawing && selectedSlot === slotIndex }">
              <template v-if="slot.prize > 0">
                <span class="prize-amount font-semibold tabular-nums">{{ slot.prize }}</span>
                <span class="mt-2 text-xs text-gray-500 dark:text-dark-400">{{ t('lottery.quota') }}</span>
              </template>
              <template v-else>
                <Icon name="refresh" size="md" class="mb-2 opacity-60" aria-hidden="true" />
                <span class="no-prize-label font-semibold">{{ t('lottery.tryNextTime') }}</span>
              </template>
            </div>
            <div class="ring-center">
              <Icon name="gift" size="xl" class="center-gift text-rose-600 dark:text-rose-400" aria-hidden="true" />
              <button class="draw-button" :disabled="drawing || (!pendingDraw && effectiveState !== 'ready') || loading" @click="draw">
                {{ t(drawing ? 'lottery.drawing' : pendingDraw ? 'lottery.retryDraw' : status.admin_repeat && status.today ? 'lottery.again' : 'lottery.draw') }}
              </button>
            </div>
            <div v-if="drawing && activeSlot !== null" class="ring-slider" aria-hidden="true" :style="sliderStyle" />
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
            <router-link v-if="!status.eligible" to="/purchase" class="btn btn-secondary w-full">{{ t('lottery.recharge') }}</router-link>
          </div>
        </div>
      </section>

      <Transition name="result-reveal">
      <section v-if="result && !drawing" class="lottery-result rounded-2xl border border-rose-200 bg-rose-50 p-6 dark:border-rose-900 dark:bg-rose-950/30" role="status" aria-live="polite">
        <p class="text-sm text-rose-700 dark:text-rose-300">{{ t(status?.admin_repeat ? 'lottery.latestResult' : 'lottery.result') }}</p>
        <h3 class="mt-2 text-2xl font-semibold text-gray-900 dark:text-white">{{ result.prize > 0 ? t('lottery.won', { amount: result.prize }) : noPrizeHint }}</h3>
      </section>
      </Transition>
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
        <template v-else><p>{{ t('lottery.once') }}</p><p>{{ t(makeupDay ? 'lottery.makeupTimeRule' : 'lottery.timeRule') }}</p><p>{{ t('lottery.qualificationNote') }}</p></template>
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
import { lotteryRing, useLotteryReveal } from '@/composables/useLotteryReveal'

const { t } = useI18n()
const auth = useAuthStore()
const status = ref<LotteryStatus | null>(null)
const makeupDay = computed(() => status.value?.activity_date === '2026-09-09')
const result = ref<LotteryDraw | null>(null)
const loading = ref(false)
const drawing = ref(false)
const { phase: revealPhase, activeSlot, stepDuration, start: startReveal, finish: finishReveal, cancel: cancelReveal } = useLotteryReveal()
const selectedSlot = computed(() => {
  if (!result.value) return null
  if (activeSlot.value !== null && lotteryRing[activeSlot.value].prize === result.value.prize) return activeSlot.value
  return lotteryRing.findIndex(slot => slot.prize === result.value?.prize)
})
const sliderStyle = computed(() => {
  const slot = lotteryRing[activeSlot.value ?? 0]
  return {
    transform: `translate(calc(${slot.x * 100}% + ${slot.x} * var(--ring-gap)), calc(${slot.y * 100}% + ${slot.y} * var(--ring-gap)))`,
    transitionDuration: `${stepDuration.value}ms`,
  }
})
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
  if (drawing.value) return t('lottery.drawing')
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
  result.value = null
  startReveal()
  error.value = ''
  try {
    if (adminAttempt && !pendingDraw.value) pendingDraw.value = { date: activityDate, id: crypto.randomUUID() }
    const confirmed = pendingDraw.value ? await lotteryAPI.draw(activityDate, pendingDraw.value.id) : await lotteryAPI.draw(activityDate)
    if (disposed) return
    pendingDraw.value = null
    // 先锁定本地状态，后续刷新失败也不能让按钮重新可点。
    if (status.value) { status.value.today = confirmed; status.value.state = 'drawn' }
    if (!await finishReveal(confirmed.prize) || disposed) return
    result.value = confirmed
    await auth.refreshUser().catch(() => undefined)
    await load(true)
  } catch (cause) {
    cancelReveal()
    if (disposed) return
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
.lottery-ring { --ring-gap: clamp(.5rem, 1.5vw, .875rem); position: relative; display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); grid-template-rows: repeat(3, minmax(0, 1fr)); gap: var(--ring-gap); width: 100%; max-width: 32rem; aspect-ratio: 1; margin-inline: auto; }
.prize-tile { display: flex; flex-direction: column; align-items: center; justify-content: center; min-width: 0; border-radius: .9rem; background: #fff; color: #b42a3c; border: 1px solid #f3e2e5; }
.prize-amount { font-size: clamp(1.65rem, 4vw, 2.5rem); line-height: 1; }
.no-prize-label { font-size: clamp(.8rem, 2vw, 1.05rem); }
.ring-center { grid-column: 2; grid-row: 2; display: flex; flex-direction: column; align-items: center; justify-content: center; gap: .75rem; min-width: 0; padding: .3rem; }
.ring-center .draw-button { padding: .6rem .35rem; font-size: clamp(.75rem, 1.5vw, .875rem); line-height: 1.5; text-wrap: balance; }
.ring-slider { position: absolute; top: 0; left: 0; width: calc((100% - 2 * var(--ring-gap)) / 3); height: calc((100% - 2 * var(--ring-gap)) / 3); border: 2px solid #e55368; border-radius: .9rem; background: #e553680d; box-shadow: 0 0 0 3px #e553681a, 0 4px 20px #c72d4033; pointer-events: none; transition-property: transform; transition-timing-function: linear; will-change: transform; }
:global(.dark .lottery-stage .prize-tile) { background: #2b2229; color: #fda4af; border-color: #4e303a; }
.prize-tile { position: relative; transition: box-shadow .15s ease; }
.prize-tile::after { content: ''; position: absolute; inset: -1px; border: 2px solid #e55368; border-radius: inherit; opacity: 0; pointer-events: none; }
.prize-tile.selected::after { opacity: 1; }
.prize-tile.selected, .no-prize-tile.selected { box-shadow: 0 0 0 4px #e553681a; }
.result-reveal-enter-active { transition: opacity .45s ease, transform .45s cubic-bezier(.2,.8,.2,1); }
.result-reveal-enter-from { opacity: 0; transform: translateY(12px) scale(.98); }
.draw-button { width: 100%; min-height: 3rem; padding: .75rem 1rem; border-radius: .75rem; background: #c72d40; color: white; font-size: .875rem; font-weight: 600; transition: background .2s, transform .2s; }
.draw-button:hover:not(:disabled) { background: #ae2435; }
.draw-button:active:not(:disabled) { transform: scale(.98); }
.draw-button:focus-visible { outline: 3px solid #fb7185; outline-offset: 3px; }
.draw-button:disabled { opacity: .45; cursor: not-allowed; }
@media (prefers-reduced-motion: reduce) {
  .ring-slider { transition: none; }
  .prize-tile, .no-prize-tile, .prize-tile::after, .no-prize-tile::after, .draw-button, .result-reveal-enter-active { transition: none; }
  .prize-tile.active, .no-prize-tile.active, .result-reveal-enter-from { transform: none; }
}
@media (max-width: 400px) { .ring-center { gap: .35rem; padding: 0; } .center-gift { display: none; } }
</style>
