<template>
  <section class="card mb-5 space-y-4 p-5">
    <div class="flex flex-wrap items-center justify-between gap-3"><div><h2 class="font-semibold">{{ t('pool.resourceTitle') }}</h2><p class="mt-1 text-sm text-gray-500">{{ t('pool.resourceDescription') }}</p></div><router-link to="/admin/pool-orders" class="btn btn-secondary">{{ t('pool.adminTitle') }}</router-link></div>
    <p v-if="error" role="alert" class="text-sm text-red-600">{{ error }}</p>
    <p v-if="success" role="status" class="text-sm text-emerald-600">{{ success }}</p>
    <form class="flex flex-wrap items-end gap-3" @submit.prevent="deliver">
      <label class="min-w-48 flex-1 text-sm">{{ t('pool.deliveryOrder') }}<select v-model.number="orderId" required class="input mt-1" :disabled="busy"><option :value="0" disabled>{{ t('pool.selectOrder') }}</option><option v-for="o in pending" :key="o.id" :value="o.id">#{{ o.id }} · {{ o.title }}{{ overdue(o) ? ' · ' + t('pool.overdue') : '' }}</option></select></label>
      <label class="min-w-48 flex-1 text-sm">{{ t('pool.resource') }}<select v-model.number="resourceId" required class="input mt-1" :disabled="busy"><option :value="0" disabled>{{ t('pool.select') }}</option><option v-for="r in available" :key="r.id" :value="r.id">#{{ r.id }} · {{ r.name }}</option></select></label>
      <button class="btn btn-primary" :disabled="busy || !orderId || !resourceId">{{ t('pool.deliver') }}</button>
      <button type="button" class="btn btn-secondary" :disabled="busy" @click="load">{{ t('pool.refresh') }}</button>
    </form>
    <p v-if="selected?.delivery_deadline" class="text-sm text-amber-700 dark:text-amber-300">{{ t('pool.deliveryDeadline') }}: {{ new Date(selected.delivery_deadline).toLocaleString() }} <strong v-if="overdue(selected)"> · {{ t('pool.overdue') }}</strong></p>
    <p class="text-xs leading-6 text-gray-500">{{ t('pool.deliveryHint') }}</p>
    <div v-if="delivered.length" class="flex flex-wrap gap-3 text-xs text-gray-500"><span v-for="o in delivered" :key="o.id">#{{ o.id }} {{ o.title }} → {{ resources.find(r => r.id === o.resource_id)?.name || '#' + o.resource_id }} · {{ t('pool.state.active') }}</span></div>
  </section>
</template>
<script setup lang="ts">
import {computed, onMounted, onUnmounted, ref, watch} from 'vue'
import {useRoute} from 'vue-router'
import {useI18n} from 'vue-i18n'
import {poolAPI, poolError, type PoolOrder, type PoolResource} from '@/api/poolOrders'
const props = defineProps<{revision: number}>()
const emit = defineEmits<{delivered: []}>()
const {t} = useI18n(), route = useRoute()
const orders = ref<PoolOrder[]>([]), resources = ref<PoolResource[]>([]), orderId = ref(Number(route.query.order) || 0), resourceId = ref(0), busy = ref(false), error = ref(''), success = ref('')
const pending = computed(() => orders.value.filter(o => o.status === 'awaiting_delivery'))
const delivered = computed(() => orders.value.filter(o => o.status === 'active'))
const available = computed(() => resources.value.filter(r => r.status === 'active' && !orders.value.some(o => o.group_id === r.group_id && !['expired', 'cancelled'].includes(o.status))))
const selected = computed(() => pending.value.find(o => o.id === orderId.value))
const overdue = (o: PoolOrder) => !!o.delivery_deadline && new Date(o.delivery_deadline).getTime() < Date.now()
async function fetchData() { [orders.value, resources.value] = await Promise.all([poolAPI.list(true), poolAPI.resources()]); if (!pending.value.some(o => o.id === orderId.value)) orderId.value = 0; if (!available.value.some(r => r.id === resourceId.value)) resourceId.value = 0 }
async function load() {if (busy.value) return; busy.value = true; error.value = ''; try {await fetchData()} catch(e) {error.value = poolError(e,t('pool.failed'))} finally {busy.value=false}}
async function deliver() {if(busy.value || !orderId.value || !resourceId.value) return; busy.value=true; error.value=''; success.value=''; try {await poolAPI.deliver(orderId.value,resourceId.value); success.value=t('pool.delivered'); emit('delivered'); await fetchData()} catch(e){error.value=poolError(e,t('pool.failed'))} finally{busy.value=false}}
watch(() => props.revision, () => void load())
watch(() => route.query.order, value => {orderId.value=Number(value)||0; void load()})
let timer: ReturnType<typeof setInterval>
onMounted(() => {void load(); timer=setInterval(() => {if(!document.hidden) void load()},30000)})
onUnmounted(() => clearInterval(timer))
</script>
