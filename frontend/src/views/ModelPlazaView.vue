<template>
  <!-- 后台内嵌形态:?embedded=1 且已登录,套完整后台布局 -->
  <AppLayout v-if="isEmbedded">
    <ModelPlazaContent v-model:available-only="availableOnly" :response="data" :loading="loading" :error="loadFailed" embedded />
  </AppLayout>

  <!-- 独立形态:自带导航条(logo/站名 + 登录/回后台) -->
  <div v-else class="min-h-screen bg-gray-50 dark:bg-dark-950">
    <PlazaNavBar />
    <main class="mx-auto max-w-[1800px] px-4 py-6 sm:px-6 lg:px-8 lg:py-8">
      <ModelPlazaContent v-model:available-only="availableOnly" :response="data" :loading="loading" :error="loadFailed" />
    </main>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import AppLayout from '@/components/layout/AppLayout.vue'
import PlazaNavBar from '@/components/modelPlaza/PlazaNavBar.vue'
import ModelPlazaContent from '@/components/modelPlaza/ModelPlazaContent.vue'
import { getModelPlaza, type ModelPlazaResponse } from '@/api/modelPlaza'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'

const route = useRoute()
const appStore = useAppStore()
const authStore = useAuthStore()

// embedded=1 但未登录(如转发的链接)自动降级为独立形态。
const isEmbedded = computed(() => route.query.embedded === '1' && authStore.isAuthenticated)

const data = ref<ModelPlazaResponse | null>(null)
const loading = ref(true)
const loadFailed = ref(false)
const availableOnly = ref(true)
let requestID = 0

onMounted(() => {
  // 独立形态导航条需要站点名/Logo;有 __APP_CONFIG__ 注入时同步命中缓存。
  void appStore.fetchPublicSettings()
})

watch([availableOnly, () => authStore.isAuthenticated], async () => {
  const id = ++requestID
  loading.value = true
  loadFailed.value = false
  try {
    const result = await getModelPlaza({ available: authStore.isAuthenticated && availableOnly.value })
    if (id === requestID) data.value = result
  } catch {
    if (id === requestID) loadFailed.value = true
  } finally {
    if (id === requestID) loading.value = false
  }
}, { immediate: true })
</script>
