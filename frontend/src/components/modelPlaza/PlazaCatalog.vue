<template>
  <div class="grid items-start gap-5 lg:grid-cols-[190px_minmax(0,1fr)]">
    <aside class="space-y-6 lg:sticky lg:top-5" :aria-label="t('modelPlaza.catalog.reset')">
      <button class="btn btn-secondary w-full" type="button" @click="reset">{{ t('modelPlaza.catalog.reset') }}</button>
      <section v-for="filter in filters" :key="filter.key">
        <h2 class="mb-2 text-xs font-semibold text-gray-500 dark:text-dark-400">{{ filter.label }}</h2>
        <div class="flex flex-wrap gap-1 lg:flex-col">
          <button v-for="option in filter.options" :key="option.value" type="button" class="flex min-w-0 items-center justify-between gap-3 rounded-lg px-2.5 py-2 text-left text-sm transition-colors focus-visible:outline focus-visible:outline-2 focus-visible:outline-primary-500"
            :class="selection[filter.key] === option.value ? 'bg-primary-50 font-semibold text-primary-600 dark:bg-primary-950/40 dark:text-primary-300' : 'text-gray-600 hover:bg-gray-100 dark:text-dark-300 dark:hover:bg-dark-800'"
            :aria-pressed="selection[filter.key] === option.value" @click="selection[filter.key] = option.value">
            <span class="truncate">{{ option.label }}</span><span class="text-xs tabular-nums opacity-70">{{ option.count }}</span>
          </button>
        </div>
      </section>
    </aside>
    <div class="min-w-0 space-y-4">
      <div class="flex flex-wrap items-center gap-3">
        <input v-model="search" type="search" class="input min-w-48 flex-1" :aria-label="t('modelPlaza.catalog.search')" :placeholder="t('modelPlaza.catalog.search')" />
        <span class="text-xs tabular-nums text-gray-500">{{ t('modelPlaza.catalog.models', { count: visible.length }) }}</span>
      </div>
      <p class="text-xs leading-5 text-gray-500 dark:text-dark-400">{{ t('modelPlaza.catalog.priceNote') }}</p>
      <p v-if="copyStatus" role="status" class="text-xs text-primary-600">{{ copyStatus }}</p>
      <div v-if="visible.length" class="grid gap-4 md:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-4">
        <article v-for="entry in visible" :key="entry.id" class="flex min-w-0 flex-col rounded-2xl border border-gray-200/80 bg-white p-4 transition-colors hover:border-primary-200 dark:border-dark-700 dark:bg-dark-800/60">
          <div class="mb-4 flex items-start justify-between gap-2">
            <div class="flex h-12 w-12 items-center justify-center rounded-2xl border border-gray-100 bg-gray-50 text-gray-800 dark:border-dark-700 dark:bg-dark-900 dark:text-dark-100"><PlatformIcon :platform="entry.platform as GroupPlatform" size="lg" /></div>
            <span class="rounded-md bg-gray-100 px-2 py-1 text-[10px] text-gray-500 dark:bg-dark-700 dark:text-dark-300">{{ modeLabel(entry.mode) }}</span>
          </div>
          <h3 class="break-words text-base font-semibold tracking-tight text-gray-900 dark:text-white">{{ entry.name }}</h3>
          <p class="mt-1 text-xs text-gray-500">{{ providerLabel(entry.platform) }}</p>
          <p class="mb-4 mt-3 text-xs leading-5 text-gray-500 dark:text-dark-400">{{ t('modelPlaza.catalog.routes', { count: entry.routes.length }) }}</p>
          <dl class="mb-4 mt-auto space-y-1.5 text-xs tabular-nums">
            <template v-if="entry.mode === 'token'">
              <div v-for="field in tokenFields" :key="field.value" class="flex flex-wrap justify-between gap-1"><dt class="text-gray-500">{{ field.label }}</dt><dd class="font-semibold text-gray-800 dark:text-dark-100">{{ priceRange(entry, field.value) }}</dd></div>
            </template>
            <div v-else class="flex flex-wrap justify-between gap-1"><dt class="text-gray-500">{{ modeLabel(entry.mode) }}</dt><dd class="font-semibold text-gray-800 dark:text-dark-100">{{ priceRange(entry, 'per_request_price') }}</dd></div>
          </dl>
          <div class="flex items-center gap-2 border-t border-gray-100 pt-3 dark:border-dark-700">
            <button type="button" class="rounded-lg border border-gray-200 p-2 hover:bg-gray-50 focus-visible:outline focus-visible:outline-primary-500 dark:border-dark-600 dark:hover:bg-dark-700" :aria-label="`${t('modelPlaza.catalog.copy')}: ${entry.name}`" @click="copy(entry.name)"><Icon name="clipboard" size="sm" /></button>
            <button type="button" class="flex-1 rounded-lg border border-primary-200 bg-primary-50/50 px-2 py-2 text-xs font-medium text-primary-600 hover:bg-primary-100 focus-visible:outline focus-visible:outline-primary-500 dark:border-primary-800 dark:bg-primary-950/20 dark:text-primary-300" @click="selected = entry">{{ t('modelPlaza.catalog.details') }}</button>
          </div>
        </article>
      </div>
      <p v-else class="rounded-xl border border-dashed border-gray-300 py-14 text-center text-sm text-gray-500">{{ t('modelPlaza.noSearchResult') }}</p>
    </div>
    <BaseDialog :show="selected !== null" :title="selected?.name || ''" width="wide" @close="selected = null">
      <p class="mb-4 text-xs leading-5 text-gray-500">{{ t('modelPlaza.catalog.priceNote') }}</p>
      <div class="space-y-4"><PlazaGroupSection v-for="route in selected?.routes || []" :key="route.group.id" :group="{ ...route.group, models: [route.model] }" /></div>
    </BaseDialog>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ModelPlazaGroup } from '@/api/modelPlaza'
import type { GroupPlatform } from '@/types'
import BaseDialog from '@/components/common/BaseDialog.vue'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import Icon from '@/components/icons/Icon.vue'
import PlazaGroupSection from './PlazaGroupSection.vue'
import { buildModelCatalog, catalogPrices, type CatalogModel } from './catalog'

const props = defineProps<{ groups: ModelPlazaGroup[] }>()
const { t } = useI18n()
const search = ref('')
const selection = ref({ platform: 'all', mode: 'all', group: 'all' })
const selected = ref<CatalogModel | null>(null)
const copyStatus = ref('')
const catalog = computed(() => buildModelCatalog(props.groups))
const visible = computed(() => buildModelCatalog(props.groups.filter(g => selection.value.group === 'all' || String(g.id) === selection.value.group)).filter(entry =>
  (selection.value.platform === 'all' || entry.platform === selection.value.platform) &&
  (selection.value.mode === 'all' || entry.mode === selection.value.mode) &&
  entry.name.toLowerCase().includes(search.value.trim().toLowerCase())
))
function providerLabel(platform: string): string {
  return ({ openai: 'OpenAI', anthropic: 'Anthropic', gemini: 'Google', grok: 'xAI', deepseek: 'DeepSeek', kimi: 'Moonshot', zhipu: 'Zhipu', antigravity: 'Antigravity' } as Record<string, string>)[platform] || platform
}
function modeLabel(mode: string) { return t(`modelPlaza.catalog.${['token', 'image'].includes(mode) ? mode : 'request'}`) }
const filters = computed(() => [
  { key: 'platform' as const, label: t('modelPlaza.catalog.suppliers'), options: [{ value: 'all', label: t('modelPlaza.catalog.all'), count: catalog.value.length }, ...[...new Set(catalog.value.map(m => m.platform))].map(value => ({ value, label: providerLabel(value), count: catalog.value.filter(m => m.platform === value).length }))] },
  { key: 'mode' as const, label: t('modelPlaza.catalog.billing'), options: [{ value: 'all', label: t('modelPlaza.catalog.all'), count: catalog.value.length }, ...[...new Set(catalog.value.map(m => m.mode))].map(value => ({ value, label: modeLabel(value), count: catalog.value.filter(m => m.mode === value).length }))] },
  { key: 'group' as const, label: t('modelPlaza.catalog.groups'), options: [{ value: 'all', label: t('modelPlaza.catalog.all'), count: props.groups.length }, ...props.groups.map(g => ({ value: String(g.id), label: g.name, count: g.models.length }))] }
])
const tokenFields = computed(() => [
  { value: 'input_price' as const, label: t('modelPlaza.catalog.input') },
  { value: 'output_price' as const, label: t('modelPlaza.catalog.output') },
  { value: 'cache_read_price' as const, label: t('modelPlaza.catalog.cache') }
])
function priceRange(entry: CatalogModel, field: Parameters<typeof catalogPrices>[1]): string {
  const values = catalogPrices(entry, field)
  if (!values.length) return t('modelPlaza.catalog.unknown')
  const format = (value: number) => `$${value.toLocaleString('en-US', { maximumFractionDigits: 6 })}`
  const low = Math.min(...values), high = Math.max(...values)
  return `${format(low)}${high !== low ? ` – ${format(high)}` : ''}${field === 'per_request_price' ? '' : '/M'}`
}
function reset() { selection.value = { platform: 'all', mode: 'all', group: 'all' }; search.value = '' }
async function copy(name: string) {
  try { await navigator.clipboard.writeText(name); copyStatus.value = t('modelPlaza.catalog.copied') }
  catch { copyStatus.value = t('modelPlaza.catalog.copyFailed') }
}
</script>
