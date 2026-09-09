<template>
  <AppLayout>
    <div class="mx-auto max-w-6xl space-y-6 pb-10">
      <router-link to="/pool-orders" class="inline-flex items-center gap-2 text-sm text-gray-500 hover:text-primary-600"><span aria-hidden="true">←</span>{{ t('pool.pricingBack') }}</router-link>
      <header class="space-y-3">
        <p class="text-sm font-medium text-primary-600">MODELPORT / OPENAI STANDARD</p>
        <h1 class="text-3xl font-semibold tracking-tight">{{ t('pool.pricingTitle') }}</h1>
        <p class="max-w-3xl text-sm leading-7 text-gray-600 dark:text-gray-400">{{ t('pool.pricingIntro') }}</p>
      </header>
      <div class="grid gap-4 md:grid-cols-2">
        <section v-for="kind in ['Wallet', 'Tier']" :key="kind" class="card space-y-2 p-5">
          <h2 class="font-semibold">{{ t(`pool.pricing${kind}Title`) }}</h2>
          <p class="text-sm leading-6 text-gray-500">{{ t(`pool.pricing${kind}`) }}</p>
        </section>
      </div>
      <p v-if="loading" role="status" class="card p-10 text-center text-gray-500">{{ t('pool.loading') }}</p>
      <div v-else-if="error" role="alert" class="card space-y-4 p-6"><p class="text-red-600">{{ error }}</p><button class="btn btn-secondary" @click="load">{{ t('pool.refresh') }}</button></div>
      <section v-else-if="catalog" class="card overflow-hidden">
        <div class="flex flex-wrap items-center justify-between gap-4 border-b border-gray-100 p-5 dark:border-dark-700">
          <div><h2 class="font-semibold">{{ t('pool.pricingUnit') }}</h2><p class="mt-1 text-xs text-gray-500">{{ t('pool.pricingVersion', { version: catalog.version }) }}</p></div>
          <a :href="catalog.source_url" target="_blank" rel="noopener noreferrer" class="text-sm text-primary-600 hover:underline">{{ t('pool.pricingSource') }}</a>
        </div>
        <div class="flex flex-wrap items-center justify-between gap-3 p-5">
          <input v-model="search" type="search" :aria-label="t('pool.pricingSearch')" :placeholder="t('pool.pricingSearch')" class="input sm:max-w-xs" />
          <div class="flex rounded-lg bg-gray-100 p-1 dark:bg-dark-800" :aria-label="t('pool.pricingLong')" role="group">
            <button v-for="mode in ['short', 'long'] as const" :key="mode" class="rounded-md px-3 py-2 text-sm" :class="context === mode ? 'bg-white font-medium shadow-sm dark:bg-dark-700' : 'text-gray-500'" :aria-pressed="context === mode" @click="context = mode">{{ t(mode === 'long' ? 'pool.pricingLong' : 'pool.pricingShort') }}</button>
          </div>
        </div>
        <div class="overflow-x-auto">
          <table class="w-full whitespace-nowrap text-left text-sm">
            <caption class="sr-only">{{ t('pool.pricingTitle') }} · {{ t('pool.pricingUnit') }}</caption>
            <thead class="bg-gray-50 text-xs text-gray-500 dark:bg-dark-800"><tr><th v-for="col in ['Model', 'Input', 'Read', 'Write', 'Output']" :key="col" scope="col" class="px-5 py-3">{{ t(`pool.pricing${col}`) }}</th></tr></thead>
            <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
              <tr v-for="p in prices" :key="p.model" class="hover:bg-gray-50/70 dark:hover:bg-dark-800/50">
                <th scope="row" class="px-5 py-4 font-medium">{{ p.model }}</th>
                <td class="px-5 py-4 tabular-nums">{{ money(p.input) }}</td><td class="px-5 py-4 tabular-nums">{{ money(p.cache_read) }}</td><td class="px-5 py-4 tabular-nums">{{ money(p.cache_write) }}</td><td class="px-5 py-4 tabular-nums">{{ money(p.output) }}</td>
              </tr>
            </tbody>
          </table>
          <p v-if="!prices.length" class="p-10 text-center text-gray-500">{{ t('pool.pricingEmpty') }}</p>
        </div>
        <div class="space-y-2 border-t border-gray-100 p-5 text-xs leading-6 text-gray-500 dark:border-dark-700"><p>{{ t('pool.pricingContextHint') }}</p><p>{{ t('pool.pricingCacheHint') }}</p><p>{{ t('pool.pricingUpdate') }}</p></div>
      </section>
      <section class="card space-y-4 p-6">
        <h2 class="text-lg font-semibold">{{ t('pool.pricingFormulaTitle') }}</h2>
        <p class="rounded-lg bg-primary-50 p-4 text-sm leading-7 text-primary-800 dark:bg-primary-950/30 dark:text-primary-200">{{ t('pool.pricingFormula') }}</p>
        <p v-if="exampleCost !== null" class="text-sm leading-6">{{ t('pool.pricingExample', { cost: money(exampleCost) }) }}</p>
        <p class="text-sm leading-6 text-gray-500">{{ t('pool.pricingSettle') }}</p>
      </section>
      <section class="card space-y-3 p-6">
        <h2 class="text-lg font-semibold">{{ t('pool.pricingLimitsTitle') }}</h2>
        <p class="text-sm leading-6 text-gray-500">{{ t('pool.pricingPlus') }}</p><p class="text-sm leading-6 text-gray-500">{{ t('pool.pricingPro') }}</p>
        <router-link to="/usage?usage_source=pool" class="inline-flex text-sm font-medium text-primary-600 hover:underline">{{ t('pool.pricingHistory') }} →</router-link>
      </section>
      <p class="text-xs text-gray-500">{{ t('pool.pricingScope') }}</p>
    </div>
  </AppLayout>
</template>
<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import { poolAPI, poolError, type PoolPriceCatalog } from '@/api/poolOrders'
const { t } = useI18n()
const catalog = ref<PoolPriceCatalog | null>(null)
const loading = ref(true), error = ref(''), search = ref(''), context = ref<'short' | 'long'>('short')
const prices = computed(() => (catalog.value?.models || [])
  .filter(p => p.model.toLowerCase().includes(search.value.trim().toLowerCase()) && (context.value === 'short' || p.long_context_threshold > 0))
  .map(p => context.value === 'short' ? p : { ...p, input: p.input * 2, cache_read: p.cache_read === null ? null : p.cache_read * 2, cache_write: p.cache_write === null ? null : p.cache_write * 2, output: p.output * 1.5 }))
const exampleCost = computed(() => {
  const model = catalog.value?.models.find(p => p.model === 'gpt-5.4')
  return model ? (10000 * model.input + 2000 * model.output) / 1000000 : null
})
const money = (value: number | null) => value === null ? '—' : `$${value.toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 5 })}`
async function load() {
  loading.value = true; error.value = ''
  try { catalog.value = await poolAPI.pricing() } catch (e) { error.value = poolError(e, t('pool.failed')) } finally { loading.value = false }
}
onMounted(load)
</script>
