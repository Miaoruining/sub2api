<template>
  <div class="space-y-5" data-test="original-plaza">
    <PlazaFilterBar :platforms="platforms" :groups="options" :rates="rates" v-model:platform="platform" v-model:group-id="groupId" v-model:rate="rate" v-model:search="search" />
    <PlazaGroupSection v-for="group in filtered" :key="group.id" :group="group" />
    <p v-if="!filtered.length" class="rounded-2xl border border-dashed border-gray-300 py-12 text-center text-sm text-gray-500 dark:border-dark-600 dark:text-dark-400">{{ t(search.trim() ? 'modelPlaza.noSearchResult' : 'modelPlaza.empty') }}</p>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ModelPlazaGroup } from '@/api/modelPlaza'
import PlazaFilterBar from './PlazaFilterBar.vue'
import PlazaGroupSection from './PlazaGroupSection.vue'
import { plazaProviderOrder } from './catalog'

// 复用原版 Sub2API 的筛选器、分组分节与计价表，不复制任何计费逻辑。
const props = defineProps<{ groups: ModelPlazaGroup[] }>()
const { t } = useI18n()
const platform = ref('all')
const groupId = ref<number | 'all'>('all')
const rate = ref<number | 'all'>('all')
const search = ref('')
const effectiveRate = (group: ModelPlazaGroup) => group.user_rate_multiplier ?? group.rate_multiplier
const platforms = computed(() => [...new Set(props.groups.map(group => group.platform))].sort((a,b) => plazaProviderOrder(a)-plazaProviderOrder(b)))
const options = computed(() => [...props.groups].sort((a,b) => plazaProviderOrder(a.platform)-plazaProviderOrder(b.platform)).map(group => ({ id: group.id, name: group.name, platform: group.platform, rate: effectiveRate(group) })))
const rates = computed(() => [...new Set(props.groups.map(effectiveRate))].sort((a,b) => a-b))
watch(rates, values => { if (rate.value !== 'all' && !values.includes(rate.value)) rate.value = 'all' })
const filtered = computed(() => props.groups
  .filter(group => (platform.value === 'all' || group.platform === platform.value) && (groupId.value === 'all' || group.id === groupId.value) && (rate.value === 'all' || effectiveRate(group) === rate.value))
  .map(group => ({ ...group, models: group.models.filter(model => model.name.toLowerCase().includes(search.value.trim().toLowerCase())) }))
  .filter(group => group.models.length > 0)
  .sort((a,b) => effectiveRate(a)-effectiveRate(b) || a.name.localeCompare(b.name)))
</script>
