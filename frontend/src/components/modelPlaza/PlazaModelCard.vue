<!--
Adapted from New API web/src/features/pricing/components/model-card.tsx,
model-billing-mode-badge.tsx and styles/theme.css.
Copyright (C) 2023-2026 QuantumNous
SPDX-License-Identifier: AGPL-3.0-or-later
This program is free software under the GNU Affero General Public License,
version 3 or later, WITHOUT ANY WARRANTY. See NEWAPI-LICENSE.
For commercial licensing: support@quantumnous.com
Vue adapter: keeps Sub2API's authorized routes and actual group pricing.
-->
<template>
  <article class="model-card" :class="{ 'has-price-range': entry.routes.length > 1 }">
    <div class="np-card-header">
      <div class="model-identity">
        <div class="provider-logo" :class="`provider-${entry.platform}`" aria-hidden="true">
          <img v-if="entry.platform === 'gemini'" :src="geminiLogo" alt="" width="28" height="28" />
          <PlatformIcon v-else :platform="entry.platform as GroupPlatform" size="lg" />
        </div>
        <div class="model-summary">
          <h3 :title="entry.name">{{ entry.name }}</h3>
          <dl class="model-prices">
            <template v-if="entry.mode === 'token'">
              <div v-for="field in fields" :key="field.value"><dt>{{ t(`modelPlaza.catalog.${field.label}`) }}</dt><dd>{{ priceRange(field.value) }}</dd></div>
            </template>
            <div v-else><dd>{{ priceRange('per_request_price') }}</dd><dt>/ {{ t('modelPlaza.catalog.requestUnit') }}</dt></div>
          </dl>
        </div>
      </div>
      <div class="card-actions">
        <button class="details-button" type="button" :aria-label="`${t('modelPlaza.catalog.details')}: ${entry.name}`" @click="$emit('details')">{{ t('modelPlaza.catalog.detailsShort') }}<span class="action-icon" :style="{ maskImage: `url(${chevronIcon})` }" aria-hidden="true" /></button>
        <button class="copy-button" type="button" :title="t('modelPlaza.catalog.copy')" :aria-label="`${t('modelPlaza.catalog.copy')}: ${entry.name}`" @click="$emit('copy', entry.name)"><span class="action-icon" :style="{ maskImage: `url(${copyIcon})` }" aria-hidden="true" /></button>
      </div>
    </div>
    <p class="model-description">{{ metadata?.description || t('modelPlaza.catalog.noDescription') }}</p>
    <div class="np-card-footer">
      <div class="group-metadata">
        <span v-if="entry.routes[0]" class="primary-group">{{ entry.routes[0].group.name }}</span>
        <span class="billing-tag" :class="entry.mode === 'token' ? 'token-billing' : 'request-billing'">{{ t(`modelPlaza.catalog.${entry.mode === 'token' ? 'token' : 'request'}`) }}</span>
      </div>
      <div class="capability-tags">
        <span v-for="tag in bottomTags" :key="tag">{{ tag }}</span>
        <span class="token-unit">1M</span>
        <span v-if="hiddenCount > 0" class="hidden-count">+{{ hiddenCount }}</span>
      </div>
    </div>
  </article>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { GroupPlatform } from '@/types'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import geminiLogo from '@/assets/model-plaza/gemini.svg'
import copyIcon from '@/assets/model-plaza/copy.svg'
import chevronIcon from '@/assets/model-plaza/chevron-right.svg'
import { catalogPrices, type CatalogModel } from './catalog'

const props = defineProps<{ entry: CatalogModel }>()
defineEmits<{ details: []; copy: [name: string] }>()
const { t } = useI18n()
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
  const low = Math.min(...values), high = Math.max(...values)
  return `${format(low)}${high !== low ? ` – ${format(high)}` : ''}`
}
</script>

<style scoped>
/* New API 默认主题；Tailwind 4 的 radius-xl = 1rem × 1.4，并非 Tailwind 3 的 12px。 */
.model-card { --np-foreground: oklch(.145 0 0); --np-muted: oklch(.97 0 0); --np-muted-foreground: oklch(.49 0 0); --np-border: oklch(.93 0 0); --np-info: oklch(.588 .158 241.966); --np-purple: oklch(.68 .19 325); --np-mono: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, 'Liberation Mono', 'Courier New', monospace; position: relative; display: flex; flex-direction: column; min-width: 0; padding: 12px; border: 1px solid var(--np-border); border-radius: 22.4px; background: #fff; color: var(--np-foreground); font-family: 'Public Sans', sans-serif; transition: background-color .15s; }
.model-card:hover { background: color-mix(in oklch, var(--np-muted) 20%, white); }
.np-card-header { display: flex; align-items: flex-start; justify-content: space-between; gap: 10px; }
.model-identity { display: flex; min-width: 0; align-items: flex-start; gap: 10px; }
.provider-logo { width: 36px; height: 36px; display: flex; flex-shrink: 0; align-items: center; justify-content: center; border-radius: 16px; background: color-mix(in oklch, var(--np-muted) 40%, transparent); }
.provider-logo :deep(svg), .provider-logo img { width: 28px; height: 28px; }
.provider-anthropic { color: #d97757; }
.model-summary { min-width: 0; }
h3 { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-family: var(--np-mono); font-size: 15px; line-height: 1.25; font-weight: 700; margin: 0; letter-spacing: normal; }
.model-prices { display: flex; flex-wrap: wrap; align-items: baseline; column-gap: 8px; row-gap: 2px; margin: 2px 0 0; font-size: 14px; line-height: 20px; }
.model-prices > div { white-space: nowrap; }
dt { display: inline; color: var(--np-muted-foreground); margin-right: 4px; }
dd { display: inline; color: var(--np-foreground); font-family: var(--np-mono); font-weight: 600; margin: 0; overflow-wrap: anywhere; }
.has-price-range .model-prices > div { white-space: normal; overflow-wrap: anywhere; }
.card-actions { display: flex; flex-shrink: 0; align-items: center; gap: 6px; }
.card-actions button { display: inline-flex; align-items: center; justify-content: center; border: 1px solid var(--np-border); border-radius: 12.8px; color: var(--np-muted-foreground); transition: background-color .15s; }
.card-actions button:hover { color: var(--np-foreground); background: var(--np-muted); }
.card-actions button:focus-visible { outline: 2px solid var(--np-info); outline-offset: 2px; }
.action-icon { width: 14px; height: 14px; background: currentColor; mask-size: contain; mask-repeat: no-repeat; }
.details-button { gap: 4px; padding: 4px 8px; font-size: 12px; line-height: 16px; }
.copy-button { padding: 6px; }
.model-description { display: -webkit-box; -webkit-box-orient: vertical; -webkit-line-clamp: 1; overflow: hidden; flex: 1; margin: 8px 0 0; font-size: 13px; line-height: 1.625; color: var(--np-muted-foreground); }
.np-card-footer { display: grid; grid-template-columns: minmax(0,1fr) auto; align-items: start; column-gap: 8px; row-gap: 4px; margin-top: 8px; }
.group-metadata { display: flex; flex-wrap: wrap; min-width: 0; align-items: center; gap: 4px 8px; }
.primary-group { color: var(--np-muted-foreground); font-size: 14px; line-height: 20px; font-weight: 500; overflow-wrap: anywhere; }
.billing-tag { display: inline-flex; align-items: center; height: 20px; padding: 0 6px; border-radius: 32px; font-size: 14px; line-height: 20px; font-weight: 500; white-space: nowrap; }
.token-billing { color: var(--np-info); }
.request-billing { color: var(--np-purple); }
.capability-tags { display: flex; flex-wrap: wrap; align-items: center; gap: 2px 10px; min-width: 0; font-size: 12px; line-height: 16px; color: color-mix(in oklch, var(--np-muted-foreground) 70%, transparent); }
.token-unit { color: color-mix(in oklch, var(--np-muted-foreground) 50%, transparent); }
.hidden-count { color: color-mix(in oklch, var(--np-muted-foreground) 40%, transparent); }
@media (min-width: 640px) {
  .model-card { padding: 20px; }
  .np-card-header, .model-identity { gap: 12px; }
  .provider-logo { width: 40px; height: 40px; border-radius: 22.4px; }
  .model-prices { margin-top: 4px; column-gap: 12px; }
  .details-button { padding: 6px 10px; }
  .model-description { margin-top: 16px; -webkit-line-clamp: 2; min-height: 40px; }
  .np-card-footer { margin-top: 16px; }
  .capability-tags { column-gap: 12px; row-gap: 4px; }
}
:global(.dark) .model-card { --np-foreground: oklch(.965 0 0); --np-muted: oklch(.305 0 0); --np-muted-foreground: oklch(.78 0 0); --np-border: oklch(1 0 0 / 10%); background: oklch(.235 0 0); }
:global(.dark) .model-card:hover { background: oklch(.25 0 0); }
</style>
