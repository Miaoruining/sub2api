<template>
  <section id="pool-orders" class="scroll-mt-24 space-y-5">
    <header class="flex flex-wrap items-start justify-between gap-4">
      <div>
        <h2 class="text-xl font-semibold">{{ t('pool.myOrders') }}</h2>
        <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">{{ t('pool.subscriptionsHint') }}</p>
      </div>
      <div class="flex flex-wrap items-center gap-2">
        <router-link to="/pool-orders" class="btn btn-secondary btn-sm">{{ t('pool.backToLobby') }}</router-link>
        <button type="button" class="btn btn-secondary btn-sm" :disabled="busy" @click="load">{{ t('pool.refresh') }}</button>
      </div>
    </header>

    <p v-if="error && !selected" role="alert" class="rounded-xl bg-red-50 p-4 text-sm text-red-700 dark:bg-red-950/30 dark:text-red-300">{{ error }}</p>
    <p v-if="success" role="status" class="rounded-xl bg-emerald-50 p-4 text-sm text-emerald-700 dark:bg-emerald-950/30 dark:text-emerald-300">{{ success }}</p>
    <p v-if="loading" role="status" class="card p-10 text-center text-gray-500">{{ t('pool.loading') }}</p>

    <div v-else-if="orders.length" class="grid gap-5 lg:grid-cols-2">
      <div v-for="order in orders" :id="`pool-order-${order.id}`" :key="order.id">
        <PoolOrderCard :o="order" :busy="busy" @act="handleAction" />
      </div>
    </div>

    <div v-else-if="!error" class="card flex flex-col items-center justify-center gap-4 p-10 text-center">
      <p class="text-sm text-gray-500">{{ t('pool.noMyOrders') }}</p>
      <router-link to="/pool-orders" class="btn btn-secondary">{{ t('pool.backToLobby') }}</router-link>
    </div>

    <BaseDialog
      :show="!!selected"
      :title="selected?.title || t('pool.confirm')"
      width="narrow"
      :show-close-button="!busy"
      :close-on-escape="!busy"
      @close="!busy && (selected = null)"
    >
      <p v-if="error" role="alert" class="mb-3 text-sm text-red-600 dark:text-red-300">{{ error }}</p>
      <p v-if="selected" class="text-sm leading-6">{{ t('pool.confirm_leave') }}</p>
      <template #footer>
        <div class="flex gap-3">
          <button type="button" class="btn btn-primary" :disabled="busy" @click="leave">{{ t('pool.confirm') }}</button>
          <button type="button" class="btn btn-secondary" :disabled="busy" @click="selected = null">{{ t('pool.back') }}</button>
        </div>
      </template>
    </BaseDialog>
  </section>
</template>

<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import PoolOrderCard from './PoolOrderCard.vue'
import { useAuthStore } from '@/stores/auth'
import { poolAPI, poolError, type PoolOrder } from '@/api/poolOrders'

const { t } = useI18n()
const auth = useAuthStore()
const orders = ref<PoolOrder[]>([])
const loading = ref(true)
const busy = ref(false)
const error = ref('')
const success = ref('')
const selected = ref<PoolOrder | null>(null)

async function fetchOrders(): Promise<void> {
  const result = await poolAPI.list()
  orders.value = result.filter((order) => Boolean(order.mine))
}

async function load(): Promise<void> {
  if (busy.value) return
  busy.value = true
  error.value = ''
  try {
    await fetchOrders()
  } catch (e) {
    error.value = poolError(e, t('pool.failed'))
  } finally {
    loading.value = false
    busy.value = false
  }
}

function handleAction(order: PoolOrder, action: 'join' | 'leave' | 'cancel'): void {
  if (action !== 'leave' || busy.value) return
  error.value = ''
  success.value = ''
  selected.value = order
}

async function leave(): Promise<void> {
  const order = selected.value
  if (!order || busy.value) return

  busy.value = true
  error.value = ''
  success.value = ''
  try {
    await poolAPI.act(order.id, 'leave')
    selected.value = null
    success.value = t('pool.saved')
    await Promise.all([fetchOrders(), auth.refreshUser()])
  } catch (e) {
    error.value = poolError(e, t('pool.failed'))
  } finally {
    busy.value = false
  }
}

let timer: ReturnType<typeof setInterval> | undefined

onMounted(() => {
  void load()
  timer = setInterval(() => {
    if (!document.hidden && !busy.value && !selected.value) void load()
  }, 30000)
})

onUnmounted(() => {
  if (timer) clearInterval(timer)
})
</script>
