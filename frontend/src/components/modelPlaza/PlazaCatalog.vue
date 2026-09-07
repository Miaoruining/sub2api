<!--
Catalog layout adapted from New API web/src/features/pricing/components/pricing-sidebar.tsx.
Copyright (C) 2023-2026 QuantumNous
SPDX-License-Identifier: AGPL-3.0-or-later
See NEWAPI-LICENSE for the full notice.
-->
<template>
  <div class="grid min-w-0 items-start gap-6 xl:grid-cols-[320px_minmax(0,1fr)]">
    <aside class="relative z-10 min-w-0 overflow-hidden rounded-2xl border border-gray-200 bg-white p-4 dark:border-white/10 dark:bg-[#171717] xl:sticky xl:top-5" :aria-label="t('modelPlaza.catalog.filtersTitle')">
      <div class="flex items-start justify-between gap-3">
        <div>
          <h2 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('modelPlaza.catalog.filtersTitle') }}</h2>
          <p class="mt-1 text-xs leading-5 text-gray-500 dark:text-gray-400">{{ t('modelPlaza.catalog.filtersDescription') }}</p>
        </div>
        <button data-test="reset" class="inline-flex shrink-0 items-center gap-1.5 rounded-lg px-2 py-1.5 text-xs font-medium text-gray-500 hover:bg-gray-100 hover:text-gray-900 dark:text-gray-400 dark:hover:bg-white/[0.06] dark:hover:text-white" type="button" @click="reset">
          <Icon name="refresh" size="xs" />{{ t('modelPlaza.catalog.reset') }}
        </button>
      </div>
      <span v-if="hasActiveFilter" class="mt-3 inline-flex rounded-full bg-gray-100 px-2 py-1 text-[11px] font-medium text-gray-600 dark:bg-white/[0.06] dark:text-gray-300">{{ t('modelPlaza.catalog.activeFilters') }}</span>

      <section v-for="filter in filters" :key="filter.key" class="mt-5 border-b border-gray-100 pb-5 last:border-b-0 last:pb-0 dark:border-white/[0.07]">
        <h3 class="mb-2.5 text-sm font-semibold text-gray-900 dark:text-white">{{ filter.label }}</h3>
        <div class="flex min-w-0 flex-wrap gap-2">
          <button v-for="option in filter.options" :key="option.value" type="button" class="inline-flex max-w-full items-center gap-1.5 rounded-full border px-2.5 py-1.5 text-left text-xs transition-colors focus-visible:outline focus-visible:outline-2 focus-visible:outline-primary-500"
            :class="selection[filter.key] === option.value ? 'border-gray-500 bg-gray-100 font-semibold text-gray-900 dark:border-gray-500 dark:bg-white/10 dark:text-white' : 'border-gray-200 text-gray-600 hover:border-gray-400 hover:text-gray-900 dark:border-white/10 dark:text-gray-400 dark:hover:border-white/25 dark:hover:text-white'"
            :aria-pressed="selection[filter.key] === option.value" @click="selection[filter.key] = option.value">
            <span class="truncate">{{ option.label }}</span>
            <span v-if="option.rate" class="rounded-full bg-gray-100 px-1.5 py-0.5 font-mono text-[10px] dark:bg-white/[0.06]">x{{ option.rate }}</span>
            <span class="tabular-nums opacity-60">{{ option.count }}</span>
          </button>
        </div>
      </section>
    </aside>

    <main class="relative z-0 min-w-0 space-y-4">
      <div class="flex min-w-0 flex-wrap items-center gap-3 rounded-2xl border border-gray-200 bg-white p-3 dark:border-white/10 dark:bg-[#171717]">
        <span class="shrink-0 text-sm font-semibold text-gray-900 dark:text-white">{{ t('modelPlaza.catalog.models', { count: visible.length }) }} <span class="font-normal text-gray-400">/ {{ catalog.length }}</span></span>
        <label class="relative ml-auto min-w-[200px] max-w-sm flex-1">
          <Icon name="search" size="sm" class="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-gray-400" />
          <input v-model="search" type="search" class="input w-full pl-9" :aria-label="t('modelPlaza.catalog.search')" :placeholder="t('modelPlaza.catalog.search')" />
        </label>
      </div>
      <p class="text-xs leading-5 text-gray-500 dark:text-dark-400">{{ t('modelPlaza.catalog.priceNote') }}</p>
      <p v-if="copyStatus" role="status" class="text-xs text-primary-600">{{ copyStatus }}</p>
      <div v-if="visible.length" class="model-card-grid">
        <PlazaModelCard v-for="entry in visible" :key="entry.id" :entry="entry" @copy="copy" @details="selected = entry" />
      </div>
      <p v-else class="rounded-xl border border-dashed border-gray-300 py-14 text-center text-sm text-gray-500 dark:border-white/10 dark:text-gray-400">{{ t('modelPlaza.noSearchResult') }}</p>
    </main>

    <PlazaModelDrawer :show="selected !== null" :entry="selected" @close="selected = null" @copy="copy" />
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ModelPlazaGroup } from '@/api/modelPlaza'
import Icon from '@/components/icons/Icon.vue'
import PlazaModelCard from './PlazaModelCard.vue'
import PlazaModelDrawer from './PlazaModelDrawer.vue'
import { buildModelCatalog, type CatalogModel } from './catalog'

const props = defineProps<{ groups: ModelPlazaGroup[] }>()
const { t } = useI18n()
const search = ref('')
const selection = ref({ platform: 'all', mode: 'all', group: 'all' })
const selected = ref<CatalogModel | null>(null)
const copyStatus = ref('')
type FilterKey = 'platform' | 'mode' | 'group'
interface FilterOption { value: string; label: string; count: number; rate?: number }
interface CatalogFilter { key: FilterKey; label: string; options: FilterOption[] }
const catalog = computed(() => buildModelCatalog(props.groups))
const hasActiveFilter = computed(() => search.value.trim() !== '' || Object.values(selection.value).some(value => value !== 'all'))
const visible = computed(() => buildModelCatalog(props.groups.filter(group => selection.value.group === 'all' || String(group.id) === selection.value.group)).filter(entry =>
  (selection.value.platform === 'all' || entry.platform === selection.value.platform) &&
  (selection.value.mode === 'all' || entry.mode === selection.value.mode) &&
  entry.name.toLowerCase().includes(search.value.trim().toLowerCase())
))
function providerLabel(platform: string): string {
  return ({ openai: 'OpenAI', anthropic: 'Anthropic', gemini: 'Google', grok: 'xAI', deepseek: 'DeepSeek', kimi: 'Moonshot', zhipu: 'Zhipu', antigravity: 'Antigravity' } as Record<string, string>)[platform] || platform
}
function modeLabel(mode: string) { return t(`modelPlaza.catalog.${['token', 'image'].includes(mode) ? mode : 'request'}`) }
function effectiveRate(group: ModelPlazaGroup): number { return group.user_rate_multiplier ?? group.rate_multiplier }
const filters = computed<CatalogFilter[]>(() => [
  { key: 'group' as const, label: t('modelPlaza.catalog.groups'), options: [{ value: 'all', label: t('modelPlaza.catalog.allGroups'), count: props.groups.length }, ...props.groups.map(group => ({ value: String(group.id), label: group.name, count: group.models.length, rate: effectiveRate(group) }))] },
  { key: 'platform' as const, label: t('modelPlaza.catalog.suppliers'), options: [{ value: 'all', label: t('modelPlaza.catalog.allSuppliers'), count: catalog.value.length }, ...[...new Set(catalog.value.map(model => model.platform))].map(value => ({ value, label: providerLabel(value), count: catalog.value.filter(model => model.platform === value).length }))] },
  { key: 'mode' as const, label: t('modelPlaza.catalog.billing'), options: [{ value: 'all', label: t('modelPlaza.catalog.allBilling'), count: catalog.value.length }, ...[...new Set(catalog.value.map(model => model.mode))].map(value => ({ value, label: modeLabel(value), count: catalog.value.filter(model => model.mode === value).length }))] }
])
function reset() { selection.value = { platform: 'all', mode: 'all', group: 'all' }; search.value = '' }
async function copy(name: string) {
  try { await navigator.clipboard.writeText(name); copyStatus.value = t('modelPlaza.catalog.copied') }
  catch { copyStatus.value = t('modelPlaza.catalog.copyFailed') }
}
</script>

<style scoped>
.model-card-grid { display: grid; grid-template-columns: minmax(0, 1fr); gap: 16px; }
@media (min-width: 768px) { .model-card-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); } }
@media (min-width: 1536px) { .model-card-grid { grid-template-columns: repeat(3, minmax(0, 1fr)); } }
</style>
