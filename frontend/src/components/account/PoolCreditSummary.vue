<template>
 <div class="space-y-3" data-testid="pool-credit-summary">
  <div class="flex items-center justify-between text-sm"><span class="font-medium">{{ plan === 'pro' ? 'Pro' : 'Plus' }} · {{ t('pool.creditQuota') }}</span><span class="text-gray-500">{{ t('pool.perSeat') }}</span></div>
  <span v-if="seats > 0" class="inline-flex rounded-full bg-primary-50 px-2.5 py-1 text-xs font-medium text-primary-700 dark:bg-primary-950/30 dark:text-primary-300">{{ t('pool.subscriptionShare', { seats, plan: plan === 'pro' ? 'Pro' : 'Plus' }) }}</span>
  <dl class="grid grid-cols-2 gap-3 rounded-xl bg-gray-50 p-4 text-sm dark:bg-dark-800">
   <div><dt class="text-gray-500">{{ t('pool.total_credit') }}</dt><dd class="mt-1 font-semibold">{{ money(config.total_credit) }}</dd></div>
   <div><dt class="text-gray-500">{{ t('pool.personalCredit') }}</dt><dd class="mt-1 font-semibold">{{ money(limit) }}</dd></div>
   <template v-if="plan === 'plus'">
    <div><dt class="text-gray-500">{{ t('pool.personal5h') }}</dt><dd class="mt-1"><span v-if="member">{{ money(member.credit_used_5h) }} / </span>{{ money((config.credit_5h || 0) / seats) }}</dd><dd v-if="member?.credit_reset_5h" class="mt-1 text-xs text-gray-500">{{ t('pool.resetsAt') }} {{ date(member.credit_reset_5h) }}</dd></div>
    <div><dt class="text-gray-500">{{ t('pool.personal7d') }}</dt><dd class="mt-1"><span v-if="member">{{ money(member.credit_used_7d) }} / </span>{{ money((config.credit_7d || 0) / seats) }}</dd><dd v-if="member?.credit_reset_7d" class="mt-1 text-xs text-gray-500">{{ t('pool.resetsAt') }} {{ date(member.credit_reset_7d) }}</dd></div>
   </template>
  </dl>
  <p v-if="plan === 'pro'" class="text-xs text-gray-500">{{ t('pool.proNoWindows') }}</p>
  <div v-if="member" class="space-y-2 text-sm">
   <p>{{ t('pool.creditUsed') }} {{ money(member.credit_used) }} · {{ t('pool.remaining') }} <strong>{{ money(Math.max(0, limit - (member.credit_used || 0) - (member.reserved_credit || 0))) }}</strong></p>
   <progress :value="(member.credit_used || 0) + (member.reserved_credit || 0)" :max="limit" class="h-2 w-full accent-emerald-600" :aria-label="t('pool.creditQuota')" />
   <p class="text-xs text-gray-500">{{ t('pool.reserved') }} {{ money(member.reserved_credit) }} · {{ t('pool.windowIncludesReserved') }}</p>
  </div>
 </div>
</template>
<script setup lang="ts">
import {computed} from 'vue'
import {useI18n} from 'vue-i18n'
import type {PoolCreditConfig,PoolMember} from '@/api/poolOrders'
const props=defineProps<{config:PoolCreditConfig;seats:number;member?:PoolMember|null}>()
const {t}=useI18n()
const plan=computed(()=>props.config.plan_type || 'plus')
const limit=computed(()=>Math.floor((props.config.total_credit || 0) / props.seats * 1e8) / 1e8)
const money=(v?:number)=>`$${(v || 0).toLocaleString(undefined,{minimumFractionDigits:2,maximumFractionDigits:8})}`
const date=(v:string)=>new Date(v).toLocaleString()
</script>
