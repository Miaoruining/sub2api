<template>
  <div ref="root" class="relative" @keydown.esc="open = false">
    <button class="btn-ghost relative min-h-10 min-w-10 px-2 text-sm" :aria-label="notificationLabel" :title="notificationLabel" :aria-expanded="open" @click="open = !open; load()"><Icon name="bell" size="md"/><span v-if="unread" class="absolute -right-1 -top-1 rounded-full bg-amber-600 px-1.5 text-xs text-white">{{ unread }}</span></button>
    <div v-if="open" class="fixed left-4 right-4 top-16 z-50 mt-2 max-h-[min(32rem,75vh)] overflow-y-auto sm:absolute sm:left-auto sm:right-0 sm:top-auto sm:w-80 rounded-xl border border-gray-200 bg-white p-3 shadow-lg dark:border-dark-700 dark:bg-dark-800">
      <button type="button" class="mb-3 flex min-h-11 w-full items-center justify-between gap-3 rounded-lg border border-gray-200 px-3 py-2 text-sm font-medium hover:bg-gray-50 focus-visible:ring-2 focus-visible:ring-primary-500 dark:border-dark-600 dark:hover:bg-dark-700" @click="openAnnouncements">
        <span>{{ t('announcements.title') }}</span>
        <span class="flex items-center gap-2"><span v-if="announcementStore.unreadCount" class="rounded-full bg-red-50 px-2 text-xs text-red-600 dark:bg-red-950/30 dark:text-red-300">{{ announcementStore.unreadCount }} {{ t('announcements.unread') }}</span><Icon name="chevronRight" size="sm" /></span>
      </button>
      <h2 class="mb-2 text-sm font-semibold">{{ t('pool.notifications') }}</h2><p v-if="error" role="alert" class="text-xs text-red-600">{{ error }}</p><p v-else-if="!items.length" class="p-3 text-sm text-gray-500">{{ t('pool.noNotifications') }}</p>
      <router-link v-for="n in items" :key="n.id" :to="n.kind === 'delivery_required' ? {path:'/admin/pool-resources',query:{order:n.order_id}} : '/pool-orders'" class="mb-2 block rounded-lg p-3 text-sm hover:bg-gray-100 dark:hover:bg-dark-700" :class="{'bg-amber-50 dark:bg-amber-950/20': !n.read}" @click="read(n)"><strong>#{{ n.order_id }} · {{ n.title }}</strong><p class="mt-1">{{ t(n.kind === 'delivery_required' ? 'pool.notifyAdmin' : 'pool.notifyMember') }}</p><p v-if="n.kind === 'delivery_required' && n.deadline" class="mt-1 text-xs text-amber-700 dark:text-amber-300">{{ t('pool.deliveryDeadline') }}: {{ new Date(n.deadline).toLocaleString() }}<strong v-if="new Date(n.deadline).getTime() < Date.now()"> · {{ t('pool.overdue') }}</strong></p></router-link>
    </div>
    <AnnouncementBell ref="announcements" hide-trigger />
  </div>
</template>
<script setup lang="ts">
import {computed,onMounted,onUnmounted,ref} from 'vue'
import {useI18n} from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import AnnouncementBell from './AnnouncementBell.vue'
import {useAnnouncementStore} from '@/stores/announcements'
import {poolAPI,poolError,type PoolNotification} from '@/api/poolOrders'
const {t}=useI18n(), items=ref<PoolNotification[]>([]), open=ref(false), error=ref(''), root=ref<HTMLElement>()
const announcementStore=useAnnouncementStore()
const announcements=ref<InstanceType<typeof AnnouncementBell>>()
const notificationLabel=computed(() => `${t('announcements.title')} / ${t('pool.notifications')}`)
const unread=computed(() => announcementStore.unreadCount + items.value.filter(n => !n.read).length)
function openAnnouncements(){open.value=false;announcements.value?.openModal()}
let loading=false, timer:ReturnType<typeof setInterval>
async function load(){if(loading) return; loading=true; try{items.value=await poolAPI.notifications(); error.value=''}catch(e){error.value=poolError(e,t('pool.failed'))}finally{loading=false}}
async function read(n:PoolNotification){open.value=false;try{await poolAPI.readNotification(n.id);n.read=true}catch(e){error.value=poolError(e,t('pool.failed'))}}
function outside(e:MouseEvent){if(!root.value?.contains(e.target as Node))open.value=false}
onMounted(() => {void load();document.addEventListener('click',outside);timer=setInterval(() => {if(!document.hidden)void load()},30000)})
onUnmounted(() => {clearInterval(timer);document.removeEventListener('click',outside)})
</script>
