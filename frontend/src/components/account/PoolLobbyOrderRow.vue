<template>
  <article data-test="lobby-order-row" class="border-b border-gray-100 last:border-b-0 dark:border-cyan-900/50">
    <div class="grid grid-cols-2 gap-3 px-4 py-4 lg:grid-cols-[minmax(0,1.35fr)_minmax(0,1.2fr)_minmax(100px,.6fr)_minmax(150px,.8fr)_auto] lg:items-center lg:gap-4 lg:px-5">
      <div class="col-span-2 flex min-w-0 items-center gap-3 lg:col-span-1">
        <div
          class="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl text-white"
          :class="order.plan_type === 'pro' ? 'bg-gray-950 dark:bg-black' : 'bg-emerald-500 dark:bg-teal-500'"
          aria-hidden="true"
        >
          <PlatformIcon platform="openai" size="lg" class="h-6 w-6" />
        </div>
        <div class="min-w-0">
          <h3 class="truncate text-sm font-semibold text-gray-900 dark:text-white" :title="order.title">{{ order.title }}</h3>
          <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">#{{ order.id }} · {{ planLabel }} · {{ order.seats }} {{ t('pool.people') }}</p>
        </div>
      </div>

      <div class="col-span-2 min-w-0 lg:col-span-1">
        <p class="mb-1 flex items-center justify-between gap-2 text-xs text-gray-500 dark:text-dark-300">
          <span>{{ t('pool.lobby.progressLabel') }} {{ order.joined }}/{{ order.seats }}</span>
          <span class="shrink-0 rounded-md border border-red-500/30 bg-red-500/10 px-1.5 py-0.5 font-medium text-red-600 dark:text-red-300">{{ t('pool.state.forming') }}</span>
        </p>
        <div class="h-2 overflow-hidden rounded-full bg-gray-200 dark:bg-dark-700" role="progressbar" :aria-valuenow="order.joined" :aria-valuemin="0" :aria-valuemax="order.seats" :aria-label="t('pool.lobby.progressLabel')">
          <div class="h-full rounded-full bg-primary-500 transition-[width] duration-300" :style="{ width: `${progress}%` }" />
        </div>
        <p class="mt-1 text-xs font-medium text-red-600 dark:text-red-300">{{ t('pool.lobby.remainingMembers', { count: remainingMembers }) }}</p>
      </div>

      <div class="min-w-0">
        <p class="text-[11px] text-gray-500 dark:text-dark-400 lg:hidden">{{ t('pool.lobby.priceLabel') }}</p>
        <p class="truncate text-lg font-bold tabular-nums text-gray-900 dark:text-white">{{ n(order.price) }}</p>
        <p class="truncate text-xs text-gray-500 dark:text-dark-400">{{ t('pool.balanceUnit') }} / {{ t('pool.seat') }} · {{ durationDays }} {{ t('pool.days') }}</p>
      </div>

      <div class="min-w-0">
        <p class="text-[11px] text-gray-500 dark:text-dark-400 lg:hidden">{{ t('pool.deadline') }}</p>
        <p class="truncate text-sm font-bold tabular-nums" :class="remainingMs > 0 ? 'text-red-600 dark:text-red-300' : 'text-gray-500 dark:text-dark-400'">{{ countdown }}</p>
        <p class="truncate text-xs text-red-500/80 dark:text-red-200/80"><time :datetime="order.join_deadline">{{ date(order.join_deadline) }}</time> · {{ t('pool.lobby.deadlineSuffix') }}</p>
      </div>

      <div class="col-span-2 flex items-center justify-end gap-2 lg:col-span-1 lg:min-w-[178px]">
        <button
          data-test="lobby-details"
          type="button"
          class="inline-flex shrink-0 items-center gap-1 rounded-lg border border-gray-200 px-2 py-1.5 text-xs font-medium text-gray-500 transition-colors hover:border-primary-400 hover:text-primary-600 dark:border-dark-600 dark:text-dark-300 dark:hover:border-primary-400 dark:hover:text-primary-300"
          @click="showDetails = true"
        >
          <Icon name="infoCircle" size="xs" />{{ t('pool.lobby.viewDetails') }}
        </button>
        <BaseDialog :show="showDetails" :title="order.title" width="wide" @close="showDetails = false">
          <div class="text-left">
            <PoolCreditSummary v-if="order.quota_mode === 'credits' && hasKnownPlan" :config="order" :seats="order.seats" :member="order.mine" />
            <PoolDynamicQuotaSummary
              v-else-if="(order.quota_mode === 'dynamic' || order.quota_mode === 'dynamic_shadow') && hasKnownPlan"
              :mode="order.quota_mode"
              :plan="order.plan_type"
              :seats="order.seats"
              :total-credit="order.total_credit"
              :credit5h="order.credit_5h"
              :credit7d="order.credit_7d"
              :quota="order.mine?.dynamic_quota"
              :undelivered="order.status === 'awaiting_delivery' || order.status === 'forming'"
            />
            <dl v-else-if="order.quota_mode === 'credits' || order.quota_mode === 'dynamic' || order.quota_mode === 'dynamic_shadow'" class="grid grid-cols-2 gap-2 rounded-lg bg-gray-50 p-3 text-xs dark:bg-dark-800">
              <div><dt>{{ t('pool.quotaMode') }}</dt><dd class="mt-1 font-medium text-gray-700 dark:text-gray-200">{{ t('pool.unknownPlan') }}</dd></div>
              <div><dt>{{ t('pool.planType') }}</dt><dd class="mt-1 font-medium text-gray-700 dark:text-gray-200">{{ t('pool.unknownPlan') }}</dd></div>
              <div class="col-span-2"><dt>{{ t('pool.lobby.orderDetailsHint') }}</dt><dd class="mt-1 font-medium text-gray-700 dark:text-gray-200">{{ t('pool.lobby.planDetailsPending') }}</dd></div>
            </dl>
            <dl v-else class="grid grid-cols-2 gap-2 rounded-lg bg-gray-50 p-3 text-xs dark:bg-dark-800">
              <div><dt>{{ t('pool.sharedTokens') }}</dt><dd class="mt-1 font-medium text-gray-700 dark:text-gray-200">{{ n(order.tokens_used) }} / {{ n(order.total_tokens) }}</dd></div>
              <div><dt>{{ t('pool.sharedRequests') }}</dt><dd class="mt-1 font-medium text-gray-700 dark:text-gray-200">{{ n(order.requests_used) }} / {{ n(order.total_requests) }}</dd></div>
              <div><dt>{{ t(order.mine ? 'pool.personalTokens' : 'pool.shareTokens') }}</dt><dd class="mt-1 font-medium text-gray-700 dark:text-gray-200"><span v-if="order.mine">{{ n(order.mine.tokens_used) }} / </span>{{ n(Math.floor(order.total_tokens / order.seats)) }}</dd></div>
              <div><dt>{{ t(order.mine ? 'pool.personalRequests' : 'pool.shareRequests') }}</dt><dd class="mt-1 font-medium text-gray-700 dark:text-gray-200"><span v-if="order.mine">{{ n(order.mine.requests_used) }} / </span>{{ n(Math.floor(order.total_requests / order.seats)) }}</dd></div>
            </dl>
            <p class="mt-3 text-xs leading-5 text-gray-500 dark:text-dark-300">{{ t('pool.lobby.orderDetailsHint') }}</p>
          </div>
        </BaseDialog>
        <template v-if="order.mine?.status === 'joined'">
          <button
            data-test="lobby-leave"
            type="button"
            class="btn btn-secondary w-full whitespace-nowrap sm:w-auto"
            :disabled="busy"
            @click="$emit('act', order, 'leave')"
          >
            {{ t('pool.lobby.leaveNow') }}
          </button>
        </template>
        <template v-else-if="!order.mine">
          <button
            data-test="lobby-join"
            type="button"
            class="btn btn-primary w-full whitespace-nowrap sm:w-auto"
            :disabled="busy || remainingMs <= 0 || remainingMembers <= 0"
            @click="$emit('act', order, 'join')"
          >
            {{ t('pool.lobby.joinNow') }}
          </button>
        </template>
        <span v-else data-test="lobby-left" class="inline-flex items-center rounded-lg bg-gray-100 px-2.5 py-2 text-xs font-medium text-gray-500 dark:bg-dark-700 dark:text-dark-300">
          {{ order.mine.status === 'refunded' ? t('pool.refunded') : t('pool.leave') }}
        </span>
      </div>
    </div>
  </article>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useNow } from '@vueuse/core'
import { useI18n } from 'vue-i18n'
import type { PoolOrder } from '@/api/poolOrders'
import Icon from '@/components/icons/Icon.vue'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import PoolCreditSummary from './PoolCreditSummary.vue'
import PoolDynamicQuotaSummary from './PoolDynamicQuotaSummary.vue'

const props = withDefaults(defineProps<{ order: PoolOrder; busy?: boolean }>(), { busy: false })
defineEmits<{ act: [order: PoolOrder, action: 'join' | 'leave'] }>()
const { t } = useI18n()
const now = useNow({ interval: 1000 })
const showDetails = ref(false)

const hasKnownPlan = computed(() => props.order.plan_type === 'plus' || props.order.plan_type === 'pro')

const planLabel = computed(() => {
  if (props.order.quota_mode === 'tokens' || !props.order.quota_mode) return t('pool.legacyTokens')
  if (props.order.plan_type === 'pro') return 'Pro'
  if (props.order.plan_type === 'plus') return 'Plus'
  return t('pool.creditQuota')
})

const remainingMs = computed(() => {
  const deadline = new Date(props.order.join_deadline).getTime()
  return Number.isFinite(deadline) ? Math.max(0, deadline - now.value.getTime()) : 0
})
const remainingMembers = computed(() => Math.max(0, props.order.seats - props.order.joined))
const progress = computed(() => props.order.seats > 0 ? Math.min(100, Math.max(0, props.order.joined / props.order.seats * 100)) : 0)
const durationDays = computed(() => props.order.duration_days ?? props.order.duration_hours / 24)
const countdown = computed(() => {
  if (remainingMs.value <= 0) return t('pool.formationEnded')
  const seconds = Math.ceil(remainingMs.value / 1000)
  const pad = (value: number) => String(value).padStart(2, '0')
  return t('pool.formationCountdown', {
    days: Math.floor(seconds / 86400),
    hours: pad(Math.floor(seconds / 3600) % 24),
    minutes: pad(Math.floor(seconds / 60) % 60),
    seconds: pad(seconds % 60)
  })
})
const n = (value: number) => value.toLocaleString(undefined, { maximumFractionDigits: 8 })
const date = (value: string) => new Date(value).toLocaleString()
</script>
