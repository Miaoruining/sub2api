<template>
  <article
    data-test="lobby-product-card"
    class="group flex min-h-[216px] min-w-0 flex-col overflow-hidden rounded-2xl border border-gray-200 bg-white shadow-card transition-all hover:-translate-y-0.5 hover:shadow-card-hover dark:border-cyan-900/60 dark:bg-[#0b2030]"
  >
    <div class="flex flex-1 flex-col p-4 sm:p-5">
      <div class="flex min-w-0 items-start gap-3">
        <div
          class="flex h-11 w-11 shrink-0 items-center justify-center rounded-xl text-white shadow-sm sm:h-12 sm:w-12"
          :class="product.plan_type === 'pro' ? 'bg-gray-950 dark:bg-black' : 'bg-emerald-500 dark:bg-teal-500'"
          aria-hidden="true"
        >
          <PlatformIcon platform="openai" size="lg" class="h-7 w-7 sm:h-8 sm:w-8" />
        </div>
        <div class="min-w-0 flex-1">
          <h3 class="break-words text-base font-bold leading-6 text-gray-900 dark:text-white sm:text-lg">
            {{ product.title }}
          </h3>
          <p class="mt-0.5 text-xs text-gray-500 dark:text-dark-400">
            {{ planLabel }} · {{ product.seats }} {{ t('pool.people') }}
          </p>
        </div>
      </div>

      <dl class="mt-3 grid grid-cols-3 divide-x divide-gray-200/80 rounded-xl bg-gray-50/80 py-2 dark:divide-cyan-900/50 dark:bg-[#10283a] sm:mt-4">
        <div class="min-w-0 px-2 text-center first:pl-1 last:pr-1 sm:px-3">
          <dt class="truncate text-[11px] text-gray-500 dark:text-dark-400">{{ t('pool.lobby.priceLabel') }}</dt>
          <dd class="mt-0.5 truncate text-xl font-bold tabular-nums text-gray-900 dark:text-white">{{ n(product.price) }}</dd>
          <dd class="truncate text-[11px] text-gray-500 dark:text-dark-400">{{ t('pool.balanceUnit') }} / {{ t('pool.seat') }}</dd>
        </div>
        <div class="min-w-0 px-2 text-center first:pl-1 last:pr-1 sm:px-3">
          <dt class="truncate text-[11px] text-gray-500 dark:text-dark-400">{{ t('pool.lobby.durationLabel') }}</dt>
          <dd class="mt-0.5 truncate text-xl font-bold tabular-nums text-gray-900 dark:text-white">{{ product.duration_days }}</dd>
          <dd class="truncate text-[11px] text-gray-500 dark:text-dark-400">{{ t('pool.days') }}</dd>
        </div>
        <div class="min-w-0 px-2 text-center first:pl-1 last:pr-1 sm:px-3">
          <dt class="truncate text-[11px] text-gray-500 dark:text-dark-400">{{ t('pool.lobby.shareLabel') }}</dt>
          <dd class="mt-0.5 truncate text-xl font-bold tabular-nums text-gray-900 dark:text-white">1/{{ product.seats }}</dd>
          <dd class="truncate text-[11px] text-gray-500 dark:text-dark-400">{{ planLabel }}</dd>
        </div>
      </dl>

      <div class="mt-3 flex flex-wrap items-center gap-x-3 gap-y-1.5 text-xs text-gray-500 dark:text-dark-300 sm:mt-4">
        <span class="inline-flex items-center gap-1.5 whitespace-nowrap">
          <Icon name="key" size="sm" class="text-primary-600 dark:text-primary-300" />
          {{ t('pool.lobby.dedicatedKey') }}
        </span>
        <span class="inline-flex items-center gap-1.5 whitespace-nowrap">
          <Icon name="users" size="sm" class="text-primary-600 dark:text-primary-300" />
          {{ t('pool.lobby.equalShare') }}
        </span>
        <span class="inline-flex items-center gap-1.5 whitespace-nowrap">
          <Icon name="shield" size="sm" class="text-primary-600 dark:text-primary-300" />
          {{ t('pool.lobby.deliveryPromise') }}
        </span>
      </div>

      <div class="mt-auto flex items-center gap-2 border-t border-gray-100 pt-2.5 dark:border-cyan-900/50">
        <details class="group/details min-w-0 flex-1">
          <summary class="inline-flex cursor-pointer list-none items-center gap-1 text-xs font-medium text-gray-500 hover:text-primary-600 dark:text-dark-400 dark:hover:text-primary-300">
            <Icon name="infoCircle" size="xs" />{{ t('pool.lobby.viewDetails') }}
          </summary>
          <div class="space-y-3 pt-3 text-xs leading-5 text-gray-500 dark:text-dark-300">
            <p v-if="product.description" class="whitespace-pre-line">{{ product.description }}</p>
            <PoolCreditSummary v-if="product.quota_mode === 'credits' && hasKnownPlan" :config="product" :seats="product.seats" />
            <PoolDynamicQuotaSummary
              v-else-if="(product.quota_mode === 'dynamic' || product.quota_mode === 'dynamic_shadow') && hasKnownPlan"
              :mode="product.quota_mode"
              :plan="product.plan_type"
              :seats="product.seats"
              :total-credit="product.total_credit"
              :credit5h="product.credit_5h"
              :credit7d="product.credit_7d"
              undelivered
            />
            <dl v-else-if="product.quota_mode === 'credits' || product.quota_mode === 'dynamic' || product.quota_mode === 'dynamic_shadow'" class="grid grid-cols-2 gap-2 rounded-lg bg-gray-50 p-3 dark:bg-dark-800">
              <div><dt>{{ t('pool.quotaMode') }}</dt><dd class="mt-1 font-medium text-gray-700 dark:text-gray-200">{{ t('pool.unknownPlan') }}</dd></div>
              <div><dt>{{ t('pool.planType') }}</dt><dd class="mt-1 font-medium text-gray-700 dark:text-gray-200">{{ t('pool.unknownPlan') }}</dd></div>
              <div class="col-span-2"><dt>{{ t('pool.lobby.orderDetailsHint') }}</dt><dd class="mt-1 font-medium text-gray-700 dark:text-gray-200">{{ t('pool.lobby.planDetailsPending') }}</dd></div>
            </dl>
            <dl v-else class="grid grid-cols-2 gap-2 rounded-lg bg-gray-50 p-3 dark:bg-dark-800">
              <div><dt>{{ t('pool.duration_days') }}</dt><dd class="mt-1 font-medium text-gray-700 dark:text-gray-200">{{ product.duration_days }} {{ t('pool.days') }}</dd></div>
              <div><dt>{{ t('pool.formation_days') }}</dt><dd class="mt-1 font-medium text-gray-700 dark:text-gray-200">{{ product.formation_days }} {{ t('pool.days') }}</dd></div>
              <div><dt>{{ t('pool.shareTokens') }}</dt><dd class="mt-1 font-medium text-gray-700 dark:text-gray-200">{{ n(Math.floor(product.total_tokens / product.seats)) }}</dd></div>
              <div><dt>{{ t('pool.shareRequests') }}</dt><dd class="mt-1 font-medium text-gray-700 dark:text-gray-200">{{ n(Math.floor(product.total_requests / product.seats)) }}</dd></div>
            </dl>
            <p>{{ t('pool.concurrency') }}: {{ product.concurrency }} · {{ t('pool.lobby.formationWindow', { days: product.formation_days }) }}</p>
          </div>
        </details>
        <button
          data-test="lobby-start-group"
          type="button"
          class="btn btn-secondary shrink-0 border-primary-500 px-3 py-2 text-primary-700 hover:border-primary-600 hover:bg-primary-50 dark:!border-primary-400 dark:!bg-transparent dark:text-primary-200 dark:hover:!bg-primary-950/40"
          :disabled="busy || product.status !== 'active'"
          @click="$emit('purchase', product)"
        >
          {{ product.status === 'active' ? t('pool.lobby.startGroup') : t('pool.offSale') }}
        </button>
      </div>
    </div>
  </article>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { PoolProduct } from '@/api/poolOrders'
import Icon from '@/components/icons/Icon.vue'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import PoolCreditSummary from './PoolCreditSummary.vue'
import PoolDynamicQuotaSummary from './PoolDynamicQuotaSummary.vue'

const props = withDefaults(defineProps<{ product: PoolProduct; busy?: boolean }>(), { busy: false })
defineEmits<{ purchase: [product: PoolProduct] }>()
const { t } = useI18n()

const product = computed(() => props.product)
const hasKnownPlan = computed(() => product.value.plan_type === 'plus' || product.value.plan_type === 'pro')

const planLabel = computed(() => {
  if (product.value.quota_mode === 'tokens' || !product.value.quota_mode) return t('pool.legacyTokens')
  if (product.value.plan_type === 'pro') return 'Pro'
  if (product.value.plan_type === 'plus') return 'Plus'
  return t('pool.creditQuota')
})
const n = (value: number) => value.toLocaleString(undefined, { maximumFractionDigits: 8 })
</script>
