<template>
  <AppLayout>
    <div class="mx-auto max-w-7xl space-y-6 pb-8">
      <header class="flex flex-wrap items-start justify-between gap-x-4 gap-y-2">
        <div>
          <p v-if="admin" class="mb-2 text-sm font-medium text-primary-600">MODELPORT / {{ t('pool.equalShare') }}</p>
          <h1 class="text-2xl font-semibold tracking-tight sm:text-3xl">{{ t(admin ? 'pool.adminTitle' : 'pool.title') }}</h1>
          <template v-if="admin">
            <p class="mt-2 max-w-3xl text-sm leading-6 text-gray-500">{{ t('pool.description') }}</p>
          </template>
        </div>
        <div class="flex flex-wrap items-center justify-end gap-2">
          <router-link to="/pool-orders/pricing" class="inline-flex items-center gap-1 text-sm font-medium text-primary-600 underline-offset-4 transition hover:text-primary-500 hover:underline dark:text-primary-300 dark:hover:text-primary-200">
            {{ t('pool.lobby.pricingEntry') }} <span aria-hidden="true">↗</span>
          </router-link>
          <router-link v-if="admin" to="/admin/pool-resources" class="btn btn-secondary btn-sm">{{ t('pool.resourceTitle') }}</router-link>
          <button class="btn btn-secondary btn-sm" :disabled="busy" @click="load">{{ t('pool.refresh') }}</button>
        </div>
        <p v-if="!admin" data-test="my-orders-notice" class="flex basis-full flex-wrap items-center justify-between gap-x-4 gap-y-1 text-sm leading-5 text-gray-500 dark:text-dark-400">
          <span>{{ t('pool.lobby.meta') }}</span>
          <router-link to="/subscriptions#pool-orders" :title="t('pool.myOrdersNotice')" class="shrink-0 font-medium text-primary-600 hover:underline dark:text-primary-300">{{ t('pool.lobby.myOrdersLink') }} →</router-link>
        </p>
      </header>
      <p v-if="error && !selected" role="alert" class="rounded-xl bg-red-50 p-4 text-sm text-red-700 dark:bg-red-950/30 dark:text-red-300">{{ error }}</p>
      <p v-if="success" role="status" class="rounded-xl bg-emerald-50 p-4 text-sm text-emerald-700 dark:bg-emerald-950/30 dark:text-emerald-300">{{ success }}</p>
      <template v-if="admin">
      <form class="card space-y-4 p-6" @submit.prevent="saveProduct">
        <h2 class="font-semibold">{{ t(form.id ? 'pool.editProduct' : 'pool.publishProduct') }}</h2>
        <div class="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          <label class="text-sm">{{ t('pool.productName') }}<input v-model="form.title" required maxlength="100" class="input mt-1" :placeholder="t('pool.productExample')" :disabled="busy" /></label>
          <label v-for="f in numberFields" :key="f.key" class="text-sm">{{ t(`pool.${f.key}`) }}<input v-model.number="form[f.key]" type="number" required :min="f.min" :max="f.max" :step="f.step || 1" class="input mt-1" :disabled="busy" /></label>
          <label class="text-sm">{{ t('pool.quotaMode') }}<select v-model="form.quota_mode" class="input mt-1" :disabled="busy" @change="changeQuotaMode"><option value="credits">{{ t('pool.creditQuota') }}</option><option value="dynamic">{{ t('pool.quotaMode_dynamic') }}</option><option value="dynamic_shadow">{{ t('pool.quotaMode_dynamic_shadow') }}</option><option value="tokens">{{ t('pool.legacyTokens') }}</option></select></label>
 <label v-if="form.quota_mode !== 'tokens'" class="text-sm">{{ t('pool.planType') }}<select v-model="form.plan_type" class="input mt-1" :disabled="busy" @change="changePlan"><option value="plus">Plus</option><option value="pro">Pro</option></select></label>
 <label class="text-sm">{{ t('pool.productStatus') }}<select v-model="form.status" class="input mt-1" :disabled="busy"><option value="active">{{ t('pool.onSale') }}</option><option value="disabled">{{ t('pool.offSale') }}</option></select></label>
        </div>
        <label class="block text-sm">{{ t('pool.productDescription') }}<textarea v-model="form.description" maxlength="2000" rows="2" class="input mt-1" :disabled="busy" /></label>
        <p class="text-xs leading-6 text-gray-500">{{ form.quota_mode === 'dynamic' ? t('pool.dynamicProductHint') : form.quota_mode === 'dynamic_shadow' ? t('pool.dynamicShadowProductHint') : t('pool.productHint') }}</p>
        <div class="flex gap-2"><button class="btn btn-primary" :disabled="busy">{{ t(form.id ? 'pool.saveProduct' : 'pool.publishProduct') }}</button><button v-if="form.id" type="button" class="btn btn-secondary" :disabled="busy" @click="form = newProduct()">{{ t('pool.back') }}</button></div>
      </form>
      <p v-if="loading" role="status" class="card p-10 text-center text-gray-500">{{ t('pool.loading') }}</p>
      <div class="grid items-start gap-6 lg:grid-cols-[minmax(0,2.5fr)_minmax(260px,1fr)]">
        <section class="space-y-4">
          <div class="flex items-baseline justify-between gap-3"><h2 class="text-xl font-semibold">{{ t('pool.ongoing') }}</h2><span class="text-sm text-gray-500">{{ forming.length }} {{ t('pool.groupsUnit') }}</span></div>
          <div v-if="!forming.length && !loading" class="card flex min-h-64 flex-col items-center justify-center gap-4 p-8 text-center"><Icon name="search" size="lg" class="text-gray-400"/><h3 class="text-lg font-medium">{{ t('pool.noForming') }}</h3><p class="text-sm text-gray-500">{{ t('pool.startHint') }}</p><a href="#pool-products" class="btn btn-secondary">{{ t('pool.viewProducts') }}</a></div>
          <div class="grid gap-4 xl:grid-cols-2"><PoolOrderCard v-for="o in forming" :key="o.id" :o="o" :admin="admin" :busy="busy" @act="selectOrder" /></div>
        </section>
        <aside class="card space-y-4 p-6">
          <h2 class="text-xl font-semibold">{{ t('pool.recentSuccess') }}</h2><p class="text-sm text-gray-500">{{ t('pool.recentHint') }}</p>
          <div v-if="!recent.length" class="flex min-h-48 flex-col items-center justify-center gap-3 text-center text-sm text-gray-500"><Icon name="users" size="lg"/><p>{{ t('pool.noRecent') }}</p></div>
          <ul v-else class="divide-y divide-gray-100 dark:divide-dark-700"><li v-for="o in recent" :key="o.id" class="py-4"><p class="font-medium">{{ o.title }}</p><p class="mt-2 text-xs text-gray-500">#{{ o.id }} · {{ o.seats }} {{ t('pool.people') }} · {{ date(o.formed_at!) }}</p><p class="mt-1 text-xs text-emerald-600">{{ t(`pool.state.${o.status}`) }}</p></li></ul>
        </aside>
      </div>
      <section id="pool-products" class="scroll-mt-24 space-y-5 border-t border-gray-200 pt-7 dark:border-dark-700">
        <div class="flex flex-wrap items-baseline justify-between gap-3"><div><h2 class="inline text-xl font-semibold">{{ t('pool.products') }}</h2><span class="ml-3 text-sm text-gray-500">{{ t('pool.productsHint') }}</span></div><span class="text-sm text-gray-500">{{ products.length }} {{ t('pool.productsUnit') }}</span></div>
        <p v-if="!products.length && !loading" class="card p-12 text-center text-gray-500">{{ t('pool.noProducts') }}</p>
        <div class="grid gap-5 md:grid-cols-2 xl:grid-cols-3">
          <article v-for="p in products" :key="p.id" class="card flex flex-col gap-4 p-6">
            <div class="flex items-start justify-between gap-3"><h3 class="text-lg font-semibold">{{ p.title }}</h3><span class="shrink-0 rounded-full bg-primary-50 px-3 py-1 text-xs font-medium text-primary-700 dark:bg-primary-950/30 dark:text-primary-300">{{ p.seats }} {{ t('pool.personGroup') }}</span></div>
            <p class="whitespace-pre-line text-sm leading-6 text-gray-500">{{ p.description || t('pool.defaultProductDescription') }}</p>
            <p><strong class="text-3xl tabular-nums">{{ n(p.price) }}</strong><span class="ml-2 text-sm text-gray-500">{{ t('pool.balanceUnit') }} / {{ t('pool.seat') }}</span></p>
            <PoolCreditSummary v-if="p.quota_mode === 'credits'" :config="p" :seats="p.seats" />
            <PoolDynamicQuotaSummary v-else-if="isDynamicMode(p.quota_mode)" :mode="p.quota_mode" :plan="p.plan_type" :seats="p.seats" :total-credit="p.total_credit" :credit5h="p.credit_5h" :credit7d="p.credit_7d" undelivered />
 <p v-if="p.quota_mode === 'credits' || isDynamicMode(p.quota_mode)" class="text-sm text-gray-500">{{ p.duration_days }} {{ t('pool.days') }} · {{ t('pool.formation_days') }} {{ p.formation_days }} {{ t('pool.days') }}</p>
 <dl v-else class="grid grid-cols-2 gap-3 rounded-lg bg-gray-50 p-4 text-sm dark:bg-dark-800"><div><dt class="text-gray-500">{{ t('pool.duration_days') }}</dt><dd class="mt-1 font-medium">{{ p.duration_days }} {{ t('pool.days') }}</dd></div><div><dt class="text-gray-500">{{ t('pool.formation_days') }}</dt><dd class="mt-1 font-medium">{{ p.formation_days }} {{ t('pool.days') }}</dd></div><div><dt class="text-gray-500">{{ t('pool.shareTokens') }}</dt><dd class="mt-1 font-medium">{{ n(Math.floor(p.total_tokens / p.seats)) }}</dd></div><div><dt class="text-gray-500">{{ t('pool.shareRequests') }}</dt><dd class="mt-1 font-medium">{{ n(Math.floor(p.total_requests / p.seats)) }}</dd></div></dl>
            <p class="text-xs text-gray-500">{{ t('pool.concurrency') }}: {{ p.concurrency }} · {{ t('pool.deliveryPromise') }}</p>
            <div v-if="admin" class="mt-auto flex items-center justify-between gap-2"><span class="text-sm" :class="p.status === 'active' ? 'text-emerald-600' : 'text-gray-500'">{{ t(p.status === 'active' ? 'pool.onSale' : 'pool.offSale') }}</span><button class="btn btn-secondary" :disabled="busy" @click="editProduct(p)">{{ t('pool.editProduct') }}</button></div>
            <button v-else class="btn btn-primary mt-auto w-full" :disabled="busy" @click="selected = {item: p, action: 'purchase', requestId: newRequestId()}">{{ t('pool.startGroup') }}</button>
          </article>
        </div>
      </section>
      <section v-if="managed.length" class="space-y-4"><h2 class="text-xl font-semibold">{{ t(admin ? 'pool.orderManagement' : 'pool.myOrders') }}</h2><div class="grid gap-5 lg:grid-cols-2"><PoolOrderCard v-for="o in managed" :key="o.id" :o="o" :admin="admin" :busy="busy" @act="selectOrder" /></div></section>
      <details class="rounded-xl border border-gray-200 p-4 text-sm leading-6 dark:border-dark-700"><summary class="cursor-pointer font-medium">{{ t('pool.rulesTitle') }}</summary><p class="mt-3 text-gray-500">{{ t('pool.rules') }}</p></details>
      </template>
      <template v-else>
        <p v-if="loading" role="status" class="card p-8 text-center text-gray-500">{{ t('pool.loading') }}</p>
        <section id="pool-products" data-test="lobby-products" class="scroll-mt-24 space-y-3">
          <div class="flex flex-wrap items-baseline justify-between gap-2">
            <h2 class="text-lg font-semibold">{{ t('pool.products') }} <span class="ml-2 text-sm font-normal text-gray-500">{{ t('pool.productsHint') }}</span></h2>
            <button v-if="products.length > 2" data-test="toggle-products" class="text-sm text-primary-600 hover:underline dark:text-primary-300" @click="showAllProducts = !showAllProducts">{{ t(showAllProducts ? 'pool.lobby.showFewerProducts' : 'pool.lobby.showAllProducts', {count: products.length}) }}</button>
          </div>
          <p v-if="!products.length && !loading" class="card p-8 text-center text-sm text-gray-500">{{ t('pool.noProducts') }}</p>
          <div class="grid auto-cols-[90%] grid-flow-col gap-4 overflow-x-auto pb-1 snap-x snap-mandatory sm:grid-flow-row sm:grid-cols-2 sm:overflow-visible">
            <PoolLobbyProductCard v-for="p in visibleProducts" :key="p.id" :product="p" :busy="busy" class="snap-start" @purchase="selectProduct" />
          </div>
        </section>
        <section data-test="lobby-forming" class="space-y-3">
          <div class="flex flex-wrap items-baseline justify-between gap-2">
            <h2 class="text-lg font-semibold"><span class="text-red-600 dark:text-red-400">{{ t('pool.ongoing') }}</span> <span class="ml-2 text-sm font-normal text-gray-500">{{ t('pool.lobby.formingHint') }}</span></h2>
            <span class="text-xs text-gray-500">{{ forming.length }} {{ t('pool.groupsUnit') }}</span>
          </div>
          <div v-if="!forming.length && !loading" class="card flex flex-wrap items-center justify-center gap-3 p-8 text-sm text-gray-500"><Icon name="search"/><span>{{ t('pool.noForming') }} · {{ t('pool.startHint') }}</span></div>
          <div v-else-if="forming.length" class="rounded-xl border border-gray-200 bg-white dark:border-dark-700 dark:bg-dark-800/40">
            <div class="hidden grid-cols-[minmax(0,1.35fr)_minmax(0,1.2fr)_minmax(100px,.6fr)_minmax(150px,.8fr)_auto] gap-4 border-b border-gray-200 px-5 py-3 text-xs text-gray-500 lg:grid dark:border-dark-700">
              <span>{{ t('pool.productName') }}</span><span>{{ t('pool.lobby.progressLabel') }}</span><span>{{ t('pool.lobby.priceLabel') }}</span><span>{{ t('pool.lobby.deadlineSuffix') }}</span><span class="w-[178px] text-right">{{ t('pool.lobby.actions') }}</span>
            </div>
            <PoolLobbyOrderRow v-for="o in forming" :key="o.id" :order="o" :busy="busy" @act="selectOrder" />
          </div>
        </section>
        <aside>
          <details class="rounded-xl border border-gray-200 p-4 dark:border-dark-700">
            <summary class="cursor-pointer text-sm font-medium">{{ t('pool.recentSuccess') }} <span class="ml-2 font-normal text-gray-500">{{ t('pool.recentHint') }} · {{ recent.length }}</span></summary>
            <p v-if="!recent.length" class="mt-3 text-sm text-gray-500">{{ t('pool.noRecent') }}</p>
            <ul v-else class="mt-3 grid gap-3 text-sm sm:grid-cols-2"><li v-for="o in recent" :key="o.id"><p class="font-medium">{{ o.title }}</p><p class="mt-1 text-xs text-gray-500">#{{ o.id }} · {{ o.seats }} {{ t('pool.people') }} · {{ date(o.formed_at!) }}</p><p class="mt-1 text-xs text-emerald-600">{{ t(`pool.state.${o.status}`) }}</p></li></ul>
          </details>
        </aside>
        <details class="rounded-xl border border-gray-200 p-4 text-sm leading-6 dark:border-dark-700"><summary class="cursor-pointer font-medium">{{ t('pool.rulesTitle') }}</summary><p class="mt-3 text-gray-500">{{ t('pool.rules') }}</p></details>
      </template>
      <BaseDialog :show="!!selected" :title="selected?.item.title || t('pool.confirm')" width="narrow" :show-close-button="!busy" :close-on-escape="!busy" @close="!busy && (selected = null)">
        <p v-if="error" role="alert" class="mb-3 text-sm text-red-600">{{ error }}</p>
        <p v-if="selected" class="text-sm leading-6">{{ t(`pool.confirm_${selected.action}`, { price: n(selected.item.price) }) }}</p>
        <template #footer><div class="flex gap-3"><button class="btn btn-primary" :disabled="busy" @click="act">{{ t('pool.confirm') }}</button><button class="btn btn-secondary" :disabled="busy" @click="selected = null">{{ t('pool.back') }}</button></div></template>
      </BaseDialog>
    </div>
  </AppLayout>
</template>
<script setup lang="ts">
import {computed, onMounted, onUnmounted, ref} from 'vue'
import {useI18n} from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import PoolCreditSummary from '@/components/account/PoolCreditSummary.vue'
import PoolDynamicQuotaSummary from '@/components/account/PoolDynamicQuotaSummary.vue'
import PoolOrderCard from '@/components/account/PoolOrderCard.vue'
import PoolLobbyProductCard from '@/components/account/PoolLobbyProductCard.vue'
import PoolLobbyOrderRow from '@/components/account/PoolLobbyOrderRow.vue'
import Icon from '@/components/icons/Icon.vue'
import {useAuthStore} from '@/stores/auth'
import AppLayout from '@/components/layout/AppLayout.vue'
import {poolAPI,poolError,type PoolOrder,type PoolProduct,type PoolQuotaMode} from '@/api/poolOrders'
const props=defineProps<{admin?:boolean}>(), {t}=useI18n(), auth=useAuthStore()
const orders=ref<PoolOrder[]>([]), products=ref<PoolProduct[]>([]),busy=ref(false),loading=ref(true),error=ref(''),success=ref('')
type Selection={item:PoolOrder;action:'join'|'leave'|'cancel'}|{item:PoolProduct;action:'purchase';requestId:string}
const selected=ref<Selection|null>(null)
const showAllProducts=ref(false)
const visibleProducts=computed(()=>showAllProducts.value ? products.value : products.value.slice(0,2))
function selectProduct(product:PoolProduct){selected.value={item:product,action:'purchase',requestId:newRequestId()}}
const newProduct=():PoolProduct=>({id:0,title:'',description:'',quota_mode:'dynamic_shadow',plan_type:'plus',total_credit:100,credit_5h:10,credit_7d:50,seats:2,price:10,duration_days:30,formation_days:2,total_tokens:0,total_requests:0,concurrency:1,status:'active',version:0})
const form=ref(newProduct())
type NumberKey='seats'|'price'|'duration_days'|'formation_days'|'total_tokens'|'total_requests'|'concurrency'|'total_credit'|'credit_5h'|'credit_7d'
const allNumberFields:{key:NumberKey;min:number;max:number;step?:number}[]=[{key:'seats',min:2,max:50},{key:'price',min:0.01,max:1000000,step:0.01},{key:'duration_days',min:1,max:365},{key:'formation_days',min:1,max:90},{key:'total_tokens',min:16384,max:1000000000000},{key:'total_requests',min:2,max:1000000000},{key:'concurrency',min:1,max:10}]
const isDynamicMode=(mode?:PoolQuotaMode): mode is 'dynamic'|'dynamic_shadow' => mode === 'dynamic' || mode === 'dynamic_shadow'
const hasFixedCredits=computed(()=>form.value.quota_mode === 'credits' || form.value.quota_mode === 'dynamic_shadow')
const numberFields=computed(()=>[...allNumberFields.filter(f=>!['total_tokens','total_requests'].includes(f.key) || !['credits','dynamic_shadow','dynamic'].includes(form.value.quota_mode || '')),...(hasFixedCredits.value ? [{key:'total_credit' as NumberKey,min:0.01,max:100000000,step:0.01},...(form.value.plan_type === 'plus' ? [{key:'credit_5h' as NumberKey,min:0.01,max:100000000,step:0.01},{key:'credit_7d' as NumberKey,min:0.01,max:100000000,step:0.01}] : [])] : [])])
function changePlan(){if(form.value.plan_type==='pro'){form.value.credit_5h=0;form.value.credit_7d=0}else if(hasFixedCredits.value){form.value.credit_5h=10;form.value.credit_7d=50}}
function changeQuotaMode(){
 if (form.value.quota_mode === 'dynamic') {
  form.value.total_credit = 0; form.value.credit_5h = 0; form.value.credit_7d = 0
 } else if (form.value.quota_mode === 'dynamic_shadow' && !(form.value.total_credit || form.value.credit_5h || form.value.credit_7d)) {
  form.value.total_credit = 100; form.value.credit_5h = form.value.plan_type === 'pro' ? 0 : 10; form.value.credit_7d = form.value.plan_type === 'pro' ? 0 : 50
 } else if (form.value.quota_mode === 'credits' && !(form.value.total_credit || form.value.credit_5h || form.value.credit_7d)) {
  form.value.total_credit = 100; form.value.credit_5h = form.value.plan_type === 'pro' ? 0 : 10; form.value.credit_7d = form.value.plan_type === 'pro' ? 0 : 50
 } else if (form.value.quota_mode === 'tokens') {
  if (form.value.total_tokens < form.value.seats * 8192) form.value.total_tokens = form.value.seats * 1000000
  if (form.value.total_requests < form.value.seats) form.value.total_requests = form.value.seats * 1000
 }
}
const forming=computed(()=>orders.value.filter(o=>o.status==='forming'&&o.joined>0))
const recent=computed(()=>orders.value.filter(o=>['active','awaiting_delivery'].includes(o.status)&&o.formed_at&&Date.now()-new Date(o.formed_at).getTime()<86400000).sort((a,b)=>new Date(b.formed_at!).getTime()-new Date(a.formed_at!).getTime()).slice(0,6))
const managed=computed(()=>orders.value.filter(o=>!forming.value.some(f=>f.id===o.id)&&(props.admin||o.mine)))
const n=(v:number)=>v.toLocaleString(undefined,{maximumFractionDigits:8})
const date=(v:string)=>new Date(v).toLocaleString()
// crypto.getRandomValues 也支持以局域网 HTTP 打开的管理面板。
function newRequestId(){const b=crypto.getRandomValues(new Uint8Array(16));b[6]=(b[6]&15)|64;b[8]=(b[8]&63)|128;const h=Array.from(b,x=>x.toString(16).padStart(2,'0')).join('');return `${h.slice(0,8)}-${h.slice(8,12)}-${h.slice(12,16)}-${h.slice(16,20)}-${h.slice(20)}`}
function selectOrder(order:PoolOrder,action:'join'|'leave'|'cancel'){selected.value={item:order,action}}
function editProduct(p:PoolProduct){form.value={...p};window.scrollTo({top:0,behavior:'smooth'})}
async function fetchData(){[orders.value,products.value]=await Promise.all([poolAPI.list(props.admin),poolAPI.products(props.admin)])}
async function load(){if(busy.value)return;busy.value=true;error.value='';try{await fetchData()}catch(e){error.value=poolError(e,t('pool.failed'))}finally{busy.value=false;loading.value=false}}
async function saveProduct(){if(busy.value)return;busy.value=true;error.value='';success.value='';try{await poolAPI.saveProduct(form.value);form.value=newProduct();success.value=t('pool.productSaved');await fetchData()}catch(e){error.value=poolError(e,t('pool.failed'))}finally{busy.value=false}}
async function act(){if(busy.value||!selected.value)return;busy.value=true;error.value='';success.value='';try{const s=selected.value;if(s.action==='purchase'){const result=await poolAPI.purchase(s.item,s.requestId);success.value=t('pool.groupStarted',{id:result.order_id})}else{await poolAPI.act(s.item.id,s.action);success.value=t('pool.saved')}selected.value=null;await Promise.all([fetchData(),auth.refreshUser()])}catch(e){error.value=poolError(e,t('pool.failed'))}finally{busy.value=false}}
let timer:ReturnType<typeof setInterval>|undefined
onMounted(()=>{void load();timer=setInterval(()=>{if(!busy.value&&!selected.value&&!document.hidden)void load()},30000)})
onUnmounted(()=>{if(timer)clearInterval(timer)})
</script>
