<template>
  <AppLayout>
    <div class="mx-auto max-w-5xl space-y-6">
      <header class="flex flex-wrap items-start justify-between gap-4"><div><h1 class="text-2xl font-semibold">{{ t('pool.resourceTitle') }}</h1><p class="mt-2 text-sm leading-6 text-gray-500">{{ t('pool.resourceDescription') }}</p></div><router-link to="/admin/pool-orders" class="btn btn-secondary">{{ t('pool.adminTitle') }}</router-link></header>
      <p v-if="error" role="alert" class="rounded-lg bg-red-50 p-4 text-red-700 dark:bg-red-950/30 dark:text-red-300">{{ error }}</p>
      <p v-if="success" role="status" class="rounded-lg bg-emerald-50 p-4 text-emerald-700 dark:bg-emerald-950/30 dark:text-emerald-300">{{ success }}</p>
      <form class="card space-y-4 p-6" @submit.prevent="create">
        <h2 class="font-semibold">{{ t('pool.addResource') }}</h2>
        <div class="grid gap-4 sm:grid-cols-3">
          <label class="text-sm">{{ t('pool.name') }}<input v-model="name" required maxlength="80" class="input mt-1" :disabled="busy" /></label>
          <label class="text-sm">{{ t('pool.accountType') }}<select v-model="type" class="input mt-1" :disabled="busy"><option value="oauth">OpenAI OAuth</option><option value="apikey">OpenAI API Key</option></select></label>
          <label class="text-sm">{{ t('pool.resourceConcurrency') }}<input v-model.number="concurrency" type="number" required min="1" max="50" class="input mt-1" :disabled="busy" /></label>
        </div>
        <label class="block text-sm">{{ t('pool.credentials') }}<textarea v-model="credentials" required rows="5" autocomplete="off" spellcheck="false" class="input mt-1 font-mono" :disabled="busy" :placeholder="type === 'oauth' ? '{ &quot;access_token&quot;: &quot;…&quot;, &quot;refresh_token&quot;: &quot;…&quot;, &quot;chatgpt_account_id&quot;: &quot;…&quot; }' : '{ &quot;api_key&quot;: &quot;…&quot;, &quot;base_url&quot;: &quot;https://api.openai.com&quot; }'" /></label>
        <p class="text-xs leading-6 text-gray-500">{{ t('pool.credentialsHint') }}</p><button class="btn btn-primary" :disabled="busy">{{ t('pool.addResource') }}</button>
      </form>
      <button class="btn btn-secondary" :disabled="busy" @click="load">{{ t('pool.refresh') }}</button>
      <p v-if="loading" role="status">{{ t('pool.loading') }}</p>
      <p v-else-if="!resources.length" class="card p-8 text-center text-gray-500">{{ t('pool.emptyResources') }}</p>
      <div class="grid gap-4 sm:grid-cols-2">
        <article v-for="r in resources" :key="r.id" class="card space-y-4 p-5">
          <div><h2 class="break-words font-semibold">{{ r.name }}</h2><p class="mt-1 text-xs text-gray-500">#{{ r.id }} · OpenAI {{ r.type }} · {{ r.status }}</p></div>
          <p class="text-sm">{{ t('pool.upstream') }}<br />5h: {{ percent(r.used_5h) }} · 7d: {{ percent(r.used_7d) }}</p>
          <button class="btn btn-secondary" :disabled="busy" @click="toggle(r)">{{ t(r.status === 'active' ? 'pool.disable' : 'pool.enable') }}</button>
        </article>
      </div>
    </div>
  </AppLayout>
</template>
<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import { poolAPI, poolError, type PoolResource } from '@/api/poolOrders'
const { t } = useI18n()
const resources = ref<PoolResource[]>([]), name = ref(''), type = ref('oauth'), credentials = ref(''), concurrency = ref(3), busy = ref(false), loading = ref(true), error = ref(''), success = ref('')
const percent = (v: number | null) => v == null ? t('pool.unknown') : `${Math.max(0, 100 - v).toFixed(1)}% ${t('pool.remaining')}`
async function load() { if (busy.value) return; busy.value = true; error.value = ''; try { resources.value = await poolAPI.resources() } catch (e) { error.value = poolError(e, t('pool.failed')) } finally { busy.value = false; loading.value = false } }
async function create() {
 if (busy.value) return
 busy.value = true; error.value = ''; success.value = ''
 try {
  let parsed: Record<string, unknown>
  try { parsed = JSON.parse(credentials.value); if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) throw new Error() } catch { error.value = t('pool.invalidCredentials'); return }
  await poolAPI.createResource({ name: name.value, type: type.value, concurrency: concurrency.value, credentials: parsed })
  credentials.value = ''; name.value = ''; success.value = t('pool.resourceCreated'); resources.value = await poolAPI.resources()
 } catch (e) { error.value = poolError(e, t('pool.failed')) } finally { busy.value = false }
}
async function toggle(r: PoolResource) { if (busy.value) return; busy.value = true; error.value = ''; try { await poolAPI.setResourceStatus(r.id, r.status === 'active' ? 'disabled' : 'active'); resources.value = await poolAPI.resources() } catch (e) { error.value = poolError(e, t('pool.failed')) } finally { busy.value = false } }
onMounted(load)
</script>
