<!--
Adapted from New API web/src/features/pricing/components/model-card.tsx.
Copyright (C) 2023-2026 QuantumNous
SPDX-License-Identifier: AGPL-3.0-or-later
See NEWAPI-LICENSE for the full notice.
-->
<template>
  <article
    data-test="model-card"
    class="group relative flex min-h-[214px] min-w-0 flex-col rounded-xl border border-gray-200 bg-white p-3 text-gray-900 transition-colors hover:bg-gray-50 dark:border-white/10 dark:bg-[#171717] dark:text-white dark:hover:bg-[#1d1d1d] sm:p-5"
  >
    <div class="flex items-start justify-between gap-2.5 sm:gap-3">
      <div class="flex min-w-0 flex-1 items-start gap-2.5 sm:gap-3">
        <div
          class="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg bg-gray-100 dark:bg-white/[0.06] sm:h-10 sm:w-10 sm:rounded-xl"
          :class="`provider-${entry.platform}`"
          aria-hidden="true"
        >
          <img v-if="entry.platform === 'gemini'" :src="geminiLogo" alt="" width="28" height="28" />
          <PlatformIcon v-else :platform="entry.platform as GroupPlatform" size="lg" />
        </div>

        <div class="min-w-0 flex-1">
          <h3
            class="truncate font-mono text-[15px] font-bold leading-tight text-gray-900 dark:text-white"
            :title="entry.name"
          >
            {{ entry.name }}
          </h3>
          <dl class="mt-0.5 flex flex-wrap items-baseline gap-x-2 gap-y-0.5 text-sm sm:mt-1 sm:gap-x-3">
            <template v-if="entry.mode === 'token'">
              <div v-for="field in fields" :key="field.value" class="whitespace-nowrap">
                <dt class="inline text-gray-500 dark:text-gray-400">{{ t(`modelPlaza.catalog.${field.label}`) }}</dt>
                <dd class="ml-1 inline font-mono font-semibold text-gray-900 dark:text-white">{{ priceRange(field.value) }}</dd>
              </div>
            </template>
            <div v-else class="whitespace-nowrap">
              <dd class="inline font-mono font-semibold text-gray-900 dark:text-white">{{ priceRange('per_request_price') }}</dd>
              <dt class="ml-1 inline text-gray-500 dark:text-gray-400">/ {{ t('modelPlaza.catalog.requestUnit') }}</dt>
            </div>
          </dl>
        </div>
      </div>

      <div class="flex shrink-0 items-center gap-1.5">
        <button
          class="details-button inline-flex items-center gap-1 rounded-md border border-gray-200 px-2 py-1 text-xs text-gray-500 transition-colors hover:bg-gray-100 hover:text-gray-900 focus-visible:outline focus-visible:outline-2 focus-visible:outline-primary-500 dark:border-white/10 dark:text-gray-400 dark:hover:bg-white/[0.06] dark:hover:text-white sm:px-2.5 sm:py-1.5"
          type="button"
          :aria-label="`${t('modelPlaza.catalog.details')}: ${entry.name}`"
          @click="$emit('details')"
        >
          {{ t('modelPlaza.catalog.detailsShort') }}
          <Icon name="chevronRight" size="xs" :stroke-width="2" aria-hidden="true" />
        </button>
        <button
          class="copy-button rounded-md border border-gray-200 p-1.5 text-gray-500 transition-colors hover:bg-gray-100 hover:text-gray-900 focus-visible:outline focus-visible:outline-2 focus-visible:outline-primary-500 dark:border-white/10 dark:text-gray-400 dark:hover:bg-white/[0.06] dark:hover:text-white"
          type="button"
          :title="t('modelPlaza.catalog.copy')"
          :aria-label="`${t('modelPlaza.catalog.copy')}: ${entry.name}`"
          @click="$emit('copy', entry.name)"
        >
          <Icon name="copy" size="xs" :stroke-width="2" aria-hidden="true" />
        </button>
      </div>
    </div>

    <p data-test="model-description" class="model-description mt-2 line-clamp-1 flex-1 text-[13px] leading-relaxed text-gray-500 dark:text-gray-400 sm:mt-4 sm:min-h-[2.5rem] sm:line-clamp-2">
      {{ metadata?.description || t('modelPlaza.catalog.noDescription') }}
    </p>

    <div class="mt-2 grid grid-cols-[minmax(0,1fr)_auto] items-start gap-x-2 gap-y-1 sm:mt-4">
      <div class="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
        <span v-if="primaryRoute" class="text-sm font-medium text-gray-500 dark:text-gray-400">
          {{ primaryRoute.group.name }}
        </span>
        <span
          class="inline-flex h-5 items-center whitespace-nowrap rounded-full px-1.5 text-sm font-medium"
          :class="entry.mode === 'token' ? 'text-sky-600 dark:text-sky-400' : 'text-fuchsia-600 dark:text-fuchsia-400'"
        >
          {{ t(`modelPlaza.catalog.${entry.mode === 'token' ? 'token' : 'request'}`) }}
        </span>
      </div>

      <div class="capability-tags flex min-w-0 flex-wrap items-center gap-x-2.5 gap-y-0.5 text-xs text-gray-400 dark:text-gray-500 sm:gap-x-3 sm:gap-y-1">
        <span v-for="tag in bottomTags" :key="tag">{{ tag }}</span>
        <span class="token-unit opacity-70">1M</span>
        <span v-if="hiddenCount > 0" class="opacity-50">+{{ hiddenCount }}</span>
      </div>
    </div>
  </article>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { GroupPlatform } from '@/types'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import Icon from '@/components/icons/Icon.vue'
import geminiLogo from '@/assets/model-plaza/gemini.svg'
import { catalogAutoRoutes, catalogPrices, type CatalogModel } from './catalog'

const props = defineProps<{ entry: CatalogModel }>()
defineEmits<{ details: []; copy: [name: string] }>()
const { t } = useI18n()
const autoRoutes = computed(() => catalogAutoRoutes(props.entry))
const primaryRoute = computed(() => autoRoutes.value[0] || props.entry.routes[0])
const metadata = computed(() => props.entry.routes.find(route => route.model.description || route.model.tags?.length || route.model.supported_endpoint_types?.length)?.model)
const bottomTags = computed(() => [...(metadata.value?.supported_endpoint_types?.slice(0, 2) || []), ...(metadata.value?.tags?.slice(0, 2) || [])])
const hiddenCount = computed(() => Math.max(props.entry.routes.length - 1, 0) + Math.max((metadata.value?.tags?.length || 0) - 2, 0) + Math.max((metadata.value?.supported_endpoint_types?.length || 0) - 2, 0))
const fields = computed(() => [
  { value: 'input_price' as const, label: 'input' },
  { value: 'output_price' as const, label: 'output' },
  { value: 'cache_read_price' as const, label: 'cacheShort' }
].filter(field => field.value !== 'cache_read_price' || catalogPrices(props.entry, field.value).length))

function priceRange(field: Parameters<typeof catalogPrices>[1]): string {
  const values = catalogPrices(props.entry, field)
  if (!values.length) return t('modelPlaza.catalog.unknown')
  const format = (value: number) => `$${value.toLocaleString('en-US', { maximumFractionDigits: 6 })}`
  const low = Math.min(...values)
  const high = Math.max(...values)
  return `${format(low)}${high !== low ? ` – ${format(high)}` : ''}`
}
</script>

<style scoped>
.provider-anthropic { color: #d97757; }
</style>
