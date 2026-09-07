<!--
Detail layout adapted from New API web/src/features/pricing/components/model-details.tsx.
Copyright (C) 2023-2026 QuantumNous
SPDX-License-Identifier: AGPL-3.0-or-later
See NEWAPI-LICENSE for the full notice.
-->
<template>
  <Teleport to="body">
    <Transition name="plaza-drawer">
      <div v-if="show && entry" class="fixed inset-0 z-[70]" role="dialog" aria-modal="true" :aria-label="entry.name">
        <button class="absolute inset-0 h-full w-full cursor-default bg-black/55 backdrop-blur-[2px]" type="button" :aria-label="t('modelPlaza.catalog.close')" @click="emit('close')" />
        <aside class="absolute inset-y-0 right-0 flex w-full max-w-[980px] flex-col overflow-hidden border-l border-gray-200 bg-white text-gray-900 shadow-2xl dark:border-white/10 dark:bg-[#171717] dark:text-white">
          <header class="flex items-start justify-between gap-4 border-b border-gray-200 px-5 py-5 dark:border-white/10 sm:px-7">
            <div class="flex min-w-0 items-center gap-3">
              <div class="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl bg-gray-100 dark:bg-white/[0.06]" :class="`provider-${entry.platform}`" aria-hidden="true">
                <img v-if="entry.platform === 'gemini'" :src="geminiLogo" alt="" width="28" height="28" />
                <PlatformIcon v-else :platform="entry.platform as GroupPlatform" size="lg" />
              </div>
              <div class="min-w-0">
                <div class="flex items-center gap-2">
                  <h2 class="truncate font-mono text-xl font-bold sm:text-2xl">{{ entry.name }}</h2>
                  <button class="rounded-md border border-gray-200 p-1.5 text-gray-500 hover:bg-gray-100 hover:text-gray-900 dark:border-white/10 dark:text-gray-400 dark:hover:bg-white/[0.06] dark:hover:text-white" type="button" :aria-label="t('modelPlaza.catalog.copy')" @click="emit('copy', entry.name)">
                    <Icon name="copy" size="sm" />
                  </button>
                </div>
                <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ providerLabel(entry.platform) }} · <span class="font-medium text-sky-600 dark:text-sky-400">{{ t(`modelPlaza.catalog.${entry.mode === 'token' ? 'token' : 'request'}`) }}</span></p>
              </div>
            </div>
            <button class="rounded-lg p-2 text-gray-500 hover:bg-gray-100 hover:text-gray-900 dark:text-gray-400 dark:hover:bg-white/[0.06] dark:hover:text-white" type="button" :aria-label="t('modelPlaza.catalog.close')" @click="emit('close')">
              <Icon name="x" size="md" />
            </button>
          </header>

          <div class="min-h-0 flex-1 overflow-y-auto px-5 py-5 sm:px-7">
            <div class="grid grid-cols-3 rounded-xl bg-gray-100 p-1 dark:bg-white/[0.06]">
              <button v-for="tab in tabs" :key="tab" type="button" class="rounded-lg px-3 py-2 text-sm font-medium transition-colors" :class="activeTab === tab ? 'bg-white text-gray-900 shadow-sm dark:bg-white/10 dark:text-white' : 'text-gray-500 dark:text-gray-400'" @click="activeTab = tab">
                {{ t(`modelPlaza.catalog.${tab}`) }}
              </button>
            </div>

            <template v-if="activeTab === 'overview'">
              <div class="mt-5 grid grid-cols-3 overflow-hidden rounded-xl border border-gray-200 dark:border-white/10">
                <div v-for="metric in metrics" :key="metric.label" class="border-r border-gray-200 px-4 py-3 last:border-r-0 dark:border-white/10">
                  <p class="text-xs text-gray-500 dark:text-gray-400">{{ metric.label }}</p>
                  <p class="mt-1 font-mono text-lg font-semibold">—</p>
                </div>
              </div>

              <section class="mt-6 rounded-2xl border border-gray-200 p-4 dark:border-white/10 sm:p-5">
                <h3 class="text-sm font-semibold">{{ t('modelPlaza.catalog.pricing') }}</h3>
                <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('modelPlaza.catalog.basePrice') }}</p>
                <div v-if="entry.mode === 'token'" class="mt-4 grid gap-3 sm:grid-cols-2">
                  <PriceBox :label="t('modelPlaza.catalog.input')" :value="basePrice('input_price')" />
                  <PriceBox :label="t('modelPlaza.catalog.output')" :value="basePrice('output_price')" />
                </div>
                <div v-else class="mt-4">
                  <PriceBox :label="t('modelPlaza.catalog.request')" :value="basePrice('per_request_price', false)" />
                </div>
                <dl v-if="entry.mode === 'token'" class="mt-3 rounded-xl border border-gray-200 px-4 py-3 text-sm dark:border-white/10">
                  <div class="flex items-center justify-between gap-4"><dt class="text-gray-500 dark:text-gray-400">{{ t('modelPlaza.catalog.cache') }}</dt><dd class="font-mono font-semibold">{{ basePrice('cache_read_price') }}</dd></div>
                  <div class="mt-2 flex items-center justify-between gap-4"><dt class="text-gray-500 dark:text-gray-400">{{ t('modelPlaza.catalog.cacheWrite') }}</dt><dd class="font-mono font-semibold">{{ basePrice('cache_write_price') }}</dd></div>
                </dl>

                <div class="mt-6">
                  <h4 class="text-sm font-semibold">{{ t('modelPlaza.catalog.groupPricing') }}</h4>
                  <div v-if="autoRoutes.length" data-test="route-chain" class="mt-3 flex flex-wrap items-center gap-2 text-sm">
                    <span class="text-gray-500 dark:text-gray-400">{{ t('modelPlaza.catalog.autoRouteChain') }}</span>
                    <template v-for="(route, index) in autoRoutes" :key="route.group.id">
                      <Icon v-if="index" name="arrowRight" size="xs" class="text-gray-400" />
                      <span class="rounded-full border px-2 py-1 font-semibold" :class="plazaProviderBadge(route.group.platform)">{{ route.group.name }}</span>
                    </template>
                  </div>
                  <p v-else class="mt-3 rounded-xl bg-amber-50 px-3 py-2 text-xs text-amber-700 dark:bg-amber-500/10 dark:text-amber-300">{{ t('modelPlaza.catalog.autoRouteEmpty') }}</p>

                  <div v-if="entry.routes.length" class="mt-4 overflow-x-auto">
                    <table class="w-full min-w-[700px] text-left text-sm">
                      <thead class="border-b border-gray-200 text-xs text-gray-500 dark:border-white/10 dark:text-gray-400">
                        <tr>
                          <th class="px-3 py-3 font-medium">{{ t('modelPlaza.catalog.groups') }}</th>
                          <th class="px-3 py-3 font-medium">{{ t('modelPlaza.catalog.rate') }}</th>
                          <th class="px-3 py-3 font-medium">{{ t('modelPlaza.catalog.input') }}</th>
                          <th class="px-3 py-3 font-medium">{{ t('modelPlaza.catalog.output') }}</th>
                          <th class="px-3 py-3 font-medium">{{ t('modelPlaza.catalog.cache') }}</th>
                          <th class="px-3 py-3 font-medium">{{ t('modelPlaza.catalog.cacheWrite') }}</th>
                        </tr>
                      </thead>
                      <tbody>
                        <tr v-for="route in entry.routes" :key="route.group.id" data-test="route-row" class="border-b border-gray-100 last:border-b-0 dark:border-white/[0.07]">
                          <td class="px-3 py-4 font-semibold"><span class="rounded-full border px-2 py-1" :class="plazaProviderBadge(route.group.platform)">{{ route.group.name }}</span><span v-if="route.model.auto_route_order == null" class="mt-2 block text-xs font-normal text-gray-500">{{ t('modelPlaza.catalog.displayOnly') }}</span></td>
                          <td class="px-3 py-4 font-mono">{{ effectiveRate(route) }}x</td>
                          <td class="px-3 py-4 font-mono">{{ routePrice(route, 'input_price') }}</td>
                          <td class="px-3 py-4 font-mono">{{ routePrice(route, 'output_price') }}</td>
                          <td class="px-3 py-4 font-mono">{{ routePrice(route, 'cache_read_price') }}</td>
                          <td class="px-3 py-4 font-mono">{{ routePrice(route, 'cache_write_price') }}</td>
                        </tr>
                      </tbody>
                    </table>
                  </div>
                  <p class="mt-3 text-xs text-gray-400 dark:text-gray-500">{{ t('modelPlaza.catalog.routeOrderNote') }}</p>
                  <a href="/keys" class="mt-3 inline-block text-sm font-medium text-primary-600 underline dark:text-primary-400">{{ t('keys.routingStrategy') }} →</a>
                </div>
              </section>
            </template>

            <section v-else-if="activeTab === 'performance'" class="mt-6 rounded-2xl border border-dashed border-gray-300 px-5 py-16 text-center text-sm text-gray-500 dark:border-white/10 dark:text-gray-400">
              {{ t('modelPlaza.catalog.noPerformance') }}
            </section>
            <section v-else class="mt-6 rounded-2xl border border-gray-200 p-5 dark:border-white/10">
              <h3 class="text-sm font-semibold">{{ t('modelPlaza.catalog.endpoints') }}</h3>
              <div v-if="endpoints.length" class="mt-3 flex flex-wrap gap-2"><code v-for="endpoint in endpoints" :key="endpoint" class="rounded-lg bg-gray-100 px-3 py-2 text-xs dark:bg-white/[0.06]">{{ endpoint }}</code></div>
              <p v-else class="mt-3 text-sm text-gray-500 dark:text-gray-400">{{ t('modelPlaza.catalog.noEndpoints') }}</p>
            </section>
          </div>
        </aside>
      </div>
    </Transition>
  </Teleport>
</template>

<script setup lang="ts">
import { computed, defineComponent, h, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { GroupPlatform } from '@/types'
import Icon from '@/components/icons/Icon.vue'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import geminiLogo from '@/assets/model-plaza/gemini.svg'
import { catalogAutoRoutes, plazaProviderBadge, type CatalogModel, type ModelRoute } from './catalog'

const props = defineProps<{ show: boolean; entry: CatalogModel | null }>()
const emit = defineEmits<{ close: []; copy: [name: string] }>()
const { t } = useI18n()
const activeTab = ref<'overview' | 'performance' | 'api'>('overview')
const tabs = ['overview', 'performance', 'api'] as const
const autoRoutes = computed(() => props.entry ? catalogAutoRoutes(props.entry) : [])
const endpoints = computed(() => [...new Set(props.entry?.routes.flatMap(route => route.model.supported_endpoint_types || []) || [])])
const metrics = computed(() => [
  { label: 'TPS' },
  { label: t('modelPlaza.catalog.avgLatency') },
  { label: t('modelPlaza.catalog.successRate') }
])

const PriceBox = defineComponent({
  props: { label: { type: String, required: true }, value: { type: String, required: true } },
  setup(boxProps) {
    return () => h('div', { class: 'rounded-xl border border-gray-200 px-4 py-3 dark:border-white/10' }, [
      h('p', { class: 'text-xs text-gray-500 dark:text-gray-400' }, boxProps.label),
      h('p', { class: 'mt-1 font-mono text-lg font-semibold' }, boxProps.value)
    ])
  }
})

function providerLabel(platform: string): string {
  return ({ openai: 'OpenAI', anthropic: 'Anthropic', gemini: 'Google', grok: 'xAI', deepseek: 'DeepSeek', kimi: 'Moonshot', zhipu: 'Zhipu', antigravity: 'Antigravity' } as Record<string, string>)[platform] || platform
}

function effectiveRate(route: ModelRoute): number {
  return props.entry?.mode === 'image' && route.group.image_rate_independent
    ? route.group.image_rate_multiplier
    : route.group.user_rate_multiplier ?? route.group.rate_multiplier
}

function formatMoney(value: number | null | undefined): string {
  if (value == null || !Number.isFinite(value) || value < 0) return '—'
  return `$${value.toLocaleString('en-US', { maximumFractionDigits: 6 })}`
}

function basePrice(field: 'input_price' | 'output_price' | 'cache_read_price' | 'cache_write_price' | 'per_request_price', perMillion = true): string {
  const value = props.entry?.routes.find(route => route.model.pricing?.[field] != null)?.model.pricing?.[field]
  return formatMoney(value == null ? value : value * (perMillion ? 1_000_000 : 1))
}

function routePrice(route: ModelRoute, field: 'input_price' | 'output_price' | 'cache_read_price' | 'cache_write_price'): string {
  const value = route.model.pricing?.[field]
  return formatMoney(value == null ? value : value * effectiveRate(route) * 1_000_000)
}


function onKeydown(event: KeyboardEvent) { if (event.key === 'Escape' && props.show) emit('close') }
let previousOverflow = ''
watch(() => props.show, (show) => {
  activeTab.value = 'overview'
  if (show) { previousOverflow = document.body.style.overflow; document.body.style.overflow = 'hidden' }
  else document.body.style.overflow = previousOverflow
}, { immediate: true })
onMounted(() => window.addEventListener('keydown', onKeydown))
onBeforeUnmount(() => { window.removeEventListener('keydown', onKeydown); document.body.style.overflow = previousOverflow })
</script>

<style scoped>
.provider-anthropic { color: #d97757; }
.plaza-drawer-enter-active, .plaza-drawer-leave-active { transition: opacity .2s ease; }
.plaza-drawer-enter-active aside, .plaza-drawer-leave-active aside { transition: transform .2s ease; }
.plaza-drawer-enter-from, .plaza-drawer-leave-to { opacity: 0; }
.plaza-drawer-enter-from aside, .plaza-drawer-leave-to aside { transform: translateX(28px); }
</style>
