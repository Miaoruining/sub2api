<template>
 <section class="card space-y-3 p-4">
  <div class="flex items-center justify-between"><h2 class="font-medium">{{ t('pool.holdsTitle') }}</h2><button class="btn btn-secondary" :disabled="busy" @click="load">{{ t('pool.refresh') }}</button></div>
  <p class="text-xs leading-6 text-gray-500">{{ t('pool.holdsHint') }}</p>
  <p v-if="error" role="alert" class="text-sm text-red-600">{{ error }}</p>
  <p v-else-if="!visible.length" class="text-sm text-gray-500">{{ t('pool.noHolds') }}</p>
  <ul v-else class="divide-y divide-gray-100 text-sm dark:divide-dark-700">
   <li v-for="r in visible" :key="r.id" class="flex flex-wrap items-center justify-between gap-2 py-3"><div><span class="text-violet-600">{{ t('pool.poolUsage') }} #{{r.order_id}}</span><span class="ml-3 text-gray-500">{{ new Date(r.created_at).toLocaleString() }}</span><p class="mt-1 text-xs text-gray-400">{{r.id}}</p></div><div><strong class="tabular-nums">${{r.credit.toFixed(8)}}</strong><span class="ml-3 text-amber-600">{{t(r.status==='pending'?'pool.holdPending':'pool.holdUncertain')}}</span></div></li>
  </ul>
 </section>
</template>
<script setup lang="ts">
import {computed,onMounted,ref} from 'vue'
import {useI18n} from 'vue-i18n'
import {poolAPI,poolError,type PoolCreditHold} from '@/api/poolOrders'
const props=defineProps<{keyId?:number|null}>(),{t}=useI18n()
const rows=ref<PoolCreditHold[]>([]),busy=ref(false),error=ref('')
const visible=computed(()=>rows.value.filter(r=>!props.keyId || r.key_id===props.keyId))
async function load(){busy.value=true;error.value='';try{rows.value=await poolAPI.creditHolds()}catch(e){error.value=poolError(e,t('pool.failed'))}finally{busy.value=false}}
onMounted(load)
</script>
