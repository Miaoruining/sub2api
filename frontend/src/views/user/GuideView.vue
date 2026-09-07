<template>
  <AppLayout>
    <div class="mx-auto max-w-[1440px]">
      <section class="overflow-hidden rounded-2xl border border-gray-200/80 bg-white shadow-sm dark:border-dark-700 dark:bg-dark-900">
        <div class="border-b border-gray-200/80 bg-gradient-to-br from-primary-50 via-white to-cyan-50/70 px-5 py-6 dark:border-dark-700 dark:from-primary-950/35 dark:via-dark-900 dark:to-cyan-950/20 sm:px-7">
          <div class="flex flex-col gap-5 xl:flex-row xl:items-end xl:justify-between">
            <div>
              <div class="mb-3 inline-flex items-center gap-2 rounded-full border border-primary-200 bg-white/80 px-3 py-1 text-xs font-medium text-primary-700 dark:border-primary-800 dark:bg-dark-900/80 dark:text-primary-300">
                <Icon name="book" size="sm" />
                {{ t('guide.updated') }}
              </div>
              <h2 class="text-2xl font-bold tracking-tight text-gray-950 dark:text-white sm:text-3xl">
                {{ t('guide.title') }}
              </h2>
              <p class="mt-2 max-w-2xl text-base leading-7 text-gray-600 dark:text-dark-300">
                {{ t('guide.description') }}
              </p>
            </div>

            <label class="relative block w-full xl:max-w-md">
              <span class="sr-only">{{ t('guide.searchPlaceholder') }}</span>
              <Icon name="search" size="sm" class="pointer-events-none absolute left-3.5 top-1/2 -translate-y-1/2 text-gray-400" />
              <input
                v-model.trim="searchQuery"
                type="search"
                :placeholder="t('guide.searchPlaceholder')"
                class="h-12 w-full rounded-xl border border-gray-200 bg-white/90 pl-10 pr-4 text-base text-gray-900 shadow-sm outline-none transition focus:border-primary-400 focus:ring-4 focus:ring-primary-100 dark:border-dark-700 dark:bg-dark-800/90 dark:text-white dark:focus:border-primary-600 dark:focus:ring-primary-900/30"
              >
            </label>
          </div>
        </div>

        <div class="lg:grid lg:grid-cols-[270px_minmax(0,1fr)]">
          <button
            type="button"
            class="flex w-full items-center justify-between border-b border-gray-200 px-5 py-3 text-sm font-medium text-gray-700 dark:border-dark-700 dark:text-dark-200 lg:hidden"
            :aria-expanded="mobileNavigationOpen"
            @click="mobileNavigationOpen = !mobileNavigationOpen"
          >
            <span class="flex items-center gap-2">
              <Icon name="menu" size="sm" />
              {{ t('guide.categories') }}
            </span>
            <Icon :name="mobileNavigationOpen ? 'chevronUp' : 'chevronDown'" size="sm" />
          </button>

          <aside
            :class="mobileNavigationOpen ? 'block' : 'hidden'"
            class="border-b border-gray-200 bg-gray-50/70 p-4 dark:border-dark-700 dark:bg-dark-950/35 lg:block lg:min-h-[680px] lg:border-b-0 lg:border-r"
          >
            <button
              type="button"
              class="guide-nav-item mb-3"
              :class="{ 'guide-nav-item-active': !selectedArticle && !searchQuery }"
              @click="goHome"
            >
              <Icon name="home" size="sm" />
              <span>{{ t('guide.home') }}</span>
            </button>

            <template v-if="searchQuery">
              <p class="mb-3 px-3 text-xs font-medium uppercase tracking-wide text-gray-400">
                {{ t('guide.searchResults', { count: filteredArticles.length }) }}
              </p>
              <button
                v-for="article in filteredArticles"
                :key="article.slug"
                type="button"
                class="guide-nav-item"
                :class="{ 'guide-nav-item-active': selectedArticle?.slug === article.slug }"
                @click="goToArticle(article.slug)"
              >
                <Icon :name="articleIcon(article)" size="sm" />
                <span class="min-w-0 text-left">
                  <span class="block truncate">{{ article.title }}</span>
                  <span class="mt-0.5 block text-xs font-normal text-gray-400">{{ article.duration }}</span>
                </span>
              </button>
            </template>

            <template v-else>
              <div v-for="category in categories" :key="category.id" class="mb-5 last:mb-0">
                <p class="mb-2 px-3 text-xs font-semibold uppercase tracking-[0.12em] text-gray-400">
                  {{ category.title }}
                </p>
                <button
                  v-for="article in articlesByCategory(category.id)"
                  :key="article.slug"
                  type="button"
                  class="guide-nav-item"
                  :class="{ 'guide-nav-item-active': selectedArticle?.slug === article.slug }"
                  @click="goToArticle(article.slug)"
                >
                  <Icon :name="articleIcon(article)" size="sm" />
                  <span class="min-w-0 truncate text-left">{{ article.title }}</span>
                </button>
              </div>
            </template>
          </aside>

          <main class="min-w-0 p-5 sm:p-7 lg:p-9">
            <div v-if="searchQuery && filteredArticles.length === 0" class="flex min-h-[480px] flex-col items-center justify-center text-center">
              <div class="mb-4 flex h-14 w-14 items-center justify-center rounded-2xl bg-gray-100 text-gray-400 dark:bg-dark-800">
                <Icon name="search" size="lg" />
              </div>
              <h3 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('guide.noResults') }}</h3>
              <p class="mt-2 text-sm text-gray-500 dark:text-dark-400">{{ t('guide.noResultsHint') }}</p>
            </div>

            <article v-else-if="selectedArticle" class="mx-auto max-w-4xl">
              <button type="button" class="mb-5 inline-flex items-center gap-1.5 text-sm font-medium text-gray-500 hover:text-primary-600 dark:text-dark-400 dark:hover:text-primary-400" @click="goHome">
                <Icon name="arrowLeft" size="sm" />
                {{ t('guide.backHome') }}
              </button>

              <div class="flex flex-wrap items-center gap-2 text-xs">
                <span v-if="selectedArticle.os" class="rounded-full bg-primary-50 px-2.5 py-1 font-semibold text-primary-700 dark:bg-primary-900/25 dark:text-primary-300">
                  {{ selectedArticle.os }}
                </span>
                <span class="rounded-full bg-gray-100 px-2.5 py-1 font-medium text-gray-600 dark:bg-dark-800 dark:text-dark-300">
                  {{ selectedArticle.duration }}
                </span>
              </div>
              <h3 class="mt-4 text-2xl font-bold tracking-tight text-gray-950 dark:text-white sm:text-3xl">
                {{ selectedArticle.title }}
              </h3>
              <p class="mt-3 text-base leading-7 text-gray-600 dark:text-dark-300">
                {{ selectedArticle.summary }}
              </p>

              <section class="mt-8 rounded-2xl border border-amber-200 bg-amber-50/70 p-5 dark:border-amber-900/70 dark:bg-amber-950/20">
                <h4 class="flex items-center gap-2 text-base font-semibold text-amber-900 dark:text-amber-200">
                  <Icon name="clipboard" size="sm" />
                  {{ t('guide.prerequisites') }}
                </h4>
                <ul class="mt-3 space-y-2">
                  <li v-for="item in selectedArticle.requirements" :key="item" class="flex items-start gap-2 text-sm leading-6 text-amber-900/80 dark:text-amber-100/80">
                    <Icon name="check" size="sm" class="mt-1 shrink-0" />
                    <span>{{ item }}</span>
                  </li>
                </ul>
              </section>

              <section class="mt-9">
                <h4 class="text-lg font-semibold text-gray-950 dark:text-white">{{ t('guide.steps') }}</h4>
                <ol class="mt-5 space-y-5">
                  <li v-for="(step, index) in selectedArticle.steps" :key="step.title" class="relative grid grid-cols-[36px_minmax(0,1fr)] gap-4">
                    <div class="flex h-9 w-9 items-center justify-center rounded-xl bg-primary-600 text-sm font-bold text-white shadow-sm shadow-primary-500/25">
                      {{ index + 1 }}
                    </div>
                    <div class="min-w-0 border-b border-gray-100 pb-5 dark:border-dark-800">
                      <h5 class="text-base font-semibold text-gray-900 dark:text-white">{{ step.title }}</h5>
                      <p class="mt-1.5 text-base leading-7 text-gray-600 dark:text-dark-300">{{ step.description }}</p>
                      <div v-if="step.code" class="mt-3 overflow-hidden rounded-xl border border-dark-700 bg-dark-950">
                        <div class="flex items-center justify-between border-b border-dark-700 px-3 py-2">
                          <span class="text-xs font-medium uppercase tracking-wide text-dark-400">Command</span>
                          <button type="button" class="inline-flex items-center gap-1.5 text-xs font-medium text-dark-300 hover:text-white" @click="copyCode(step.code)">
                            <Icon :name="copiedCode === step.code ? 'check' : 'copy'" size="xs" />
                            {{ copiedCode === step.code ? t('guide.copied') : t('guide.copy') }}
                          </button>
                        </div>
                        <pre class="overflow-x-auto p-4 text-sm leading-6 text-emerald-300"><code>{{ step.code }}</code></pre>
                      </div>
                      <p v-if="step.note" class="mt-3 rounded-lg border-l-4 border-primary-400 bg-primary-50 px-4 py-3 text-sm leading-6 text-primary-900 dark:bg-primary-950/25 dark:text-primary-200">
                        {{ step.note }}
                      </p>
                    </div>
                  </li>
                </ol>
              </section>

              <section class="mt-9 rounded-2xl border border-emerald-200 bg-emerald-50/65 p-5 dark:border-emerald-900/70 dark:bg-emerald-950/20">
                <h4 class="flex items-center gap-2 text-base font-semibold text-emerald-900 dark:text-emerald-200">
                  <Icon name="checkCircle" size="md" />
                  {{ t('guide.success') }}
                </h4>
                <ul class="mt-3 space-y-2">
                  <li v-for="item in selectedArticle.success" :key="item" class="flex items-start gap-2 text-sm leading-6 text-emerald-900/80 dark:text-emerald-100/80">
                    <span class="mt-2 h-1.5 w-1.5 shrink-0 rounded-full bg-emerald-500"></span>
                    <span>{{ item }}</span>
                  </li>
                </ul>
              </section>

              <section v-if="selectedArticle.troubleshooting?.length" class="mt-9">
                <h4 class="text-lg font-semibold text-gray-950 dark:text-white">{{ t('guide.troubleshooting') }}</h4>
                <div class="mt-4 divide-y divide-gray-100 overflow-hidden rounded-2xl border border-gray-200 dark:divide-dark-700 dark:border-dark-700">
                  <div v-for="item in selectedArticle.troubleshooting" :key="item.title" class="p-4 sm:p-5">
                    <h5 class="font-semibold text-gray-900 dark:text-white">{{ item.title }}</h5>
                    <p class="mt-1.5 text-sm leading-6 text-gray-600 dark:text-dark-300">{{ item.solution }}</p>
                  </div>
                </div>
              </section>

              <div class="mt-9 flex flex-wrap gap-3">
                <template v-for="action in selectedArticle.actions" :key="action.href">
                  <a
                    v-if="action.external"
                    :href="action.href"
                    target="_blank"
                    rel="noopener noreferrer"
                    :class="action.primary ? 'btn btn-primary' : 'btn btn-secondary'"
                  >
                    {{ action.label }}
                    <Icon name="externalLink" size="sm" class="ml-1.5" />
                  </a>
                  <router-link v-else :to="action.href" :class="action.primary ? 'btn btn-primary' : 'btn btn-secondary'">
                    {{ action.label }}
                    <Icon name="arrowRight" size="sm" class="ml-1.5" />
                  </router-link>
                </template>
              </div>
            </article>

            <div v-else-if="searchQuery" class="mx-auto max-w-5xl">
              <div class="mb-5 flex items-center justify-between gap-4">
                <h3 class="text-lg font-semibold text-gray-950 dark:text-white">
                  {{ t('guide.searchResults', { count: filteredArticles.length }) }}
                </h3>
                <button type="button" class="text-sm font-medium text-primary-600 hover:text-primary-700 dark:text-primary-400" @click="searchQuery = ''">
                  {{ t('guide.clearSearch') }}
                </button>
              </div>
              <div class="grid gap-4 sm:grid-cols-2">
                <button
                  v-for="article in filteredArticles"
                  :key="article.slug"
                  type="button"
                  class="rounded-2xl border border-gray-200 p-5 text-left transition hover:-translate-y-0.5 hover:border-primary-300 hover:shadow-md focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500/40 dark:border-dark-700 dark:hover:border-primary-800"
                  @click="goToArticle(article.slug)"
                >
                  <span class="flex items-center justify-between gap-3">
                    <span class="flex h-9 w-9 items-center justify-center rounded-xl bg-gray-100 text-gray-600 dark:bg-dark-800 dark:text-dark-300">
                      <Icon :name="articleIcon(article)" size="sm" />
                    </span>
                    <span class="text-xs font-medium text-gray-400">{{ article.duration }}</span>
                  </span>
                  <span class="mt-4 block font-semibold text-gray-900 dark:text-white">{{ article.title }}</span>
                  <span class="mt-2 block text-sm leading-6 text-gray-500 dark:text-dark-400">{{ article.summary }}</span>
                </button>
              </div>
            </div>

            <div v-else class="mx-auto max-w-5xl">
              <div class="mb-7">
                <h3 class="text-lg font-semibold text-gray-950 dark:text-white">{{ t('guide.quickPaths') }}</h3>
                <div class="mt-4 grid gap-4 md:grid-cols-3">
                  <button type="button" class="guide-path-card" @click="goToArticle('quick-start')">
                    <span class="guide-path-icon bg-emerald-100 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300"><Icon name="play" size="md" /></span>
                    <span class="font-semibold text-gray-900 dark:text-white">{{ t('guide.firstUse') }}</span>
                    <span class="text-sm leading-6 text-gray-500 dark:text-dark-400">{{ t('guide.firstUseDesc') }}</span>
                  </button>
                  <button type="button" class="guide-path-card" @click="goToArticle(preferredCodexSlug)">
                    <span class="guide-path-icon bg-primary-100 text-primary-700 dark:bg-primary-900/30 dark:text-primary-300"><Icon name="download" size="md" /></span>
                    <span class="font-semibold text-gray-900 dark:text-white">{{ t('guide.installCodex') }}</span>
                    <span class="text-sm leading-6 text-gray-500 dark:text-dark-400">{{ t('guide.installCodexDesc') }}</span>
                  </button>
                  <button type="button" class="guide-path-card" @click="goToArticle('common-errors')">
                    <span class="guide-path-icon bg-amber-100 text-amber-700 dark:bg-amber-900/30 dark:text-amber-300"><Icon name="exclamationTriangle" size="md" /></span>
                    <span class="font-semibold text-gray-900 dark:text-white">{{ t('guide.havingTrouble') }}</span>
                    <span class="text-sm leading-6 text-gray-500 dark:text-dark-400">{{ t('guide.havingTroubleDesc') }}</span>
                  </button>
                </div>
              </div>

              <button
                v-if="lastArticle"
                type="button"
                class="mb-7 flex w-full items-center justify-between rounded-2xl border border-primary-200 bg-primary-50/60 p-4 text-left transition hover:border-primary-300 hover:bg-primary-50 dark:border-primary-900 dark:bg-primary-950/20 dark:hover:border-primary-800"
                @click="goToArticle(lastArticle.slug)"
              >
                <span>
                  <span class="block text-xs font-semibold uppercase tracking-wide text-primary-600 dark:text-primary-400">{{ t('guide.continueReading') }}</span>
                  <span class="mt-1 block font-semibold text-gray-900 dark:text-white">{{ lastArticle.title }}</span>
                </span>
                <Icon name="arrowRight" size="md" class="text-primary-600 dark:text-primary-400" />
              </button>

              <div class="grid gap-5 sm:grid-cols-2">
                <section v-for="category in categories" :key="category.id" class="rounded-2xl border border-gray-200 p-5 dark:border-dark-700">
                  <div class="mb-4 flex items-start justify-between gap-4">
                    <div>
                      <h4 class="font-semibold text-gray-900 dark:text-white">{{ category.title }}</h4>
                      <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">{{ category.description }}</p>
                    </div>
                    <span class="rounded-full bg-gray-100 px-2 py-1 text-xs font-medium text-gray-500 dark:bg-dark-800 dark:text-dark-300">
                      {{ articlesByCategory(category.id).length }}
                    </span>
                  </div>
                  <div class="space-y-1">
                    <button
                      v-for="article in articlesByCategory(category.id)"
                      :key="article.slug"
                      type="button"
                      class="flex w-full items-center justify-between gap-3 rounded-xl px-3 py-2.5 text-left text-sm text-gray-700 transition hover:bg-gray-50 hover:text-primary-700 dark:text-dark-200 dark:hover:bg-dark-800 dark:hover:text-primary-300"
                      @click="goToArticle(article.slug)"
                    >
                      <span class="flex min-w-0 items-center gap-2.5">
                        <Icon :name="articleIcon(article)" size="sm" class="shrink-0 text-gray-400" />
                        <span class="truncate">{{ article.title }}</span>
                      </span>
                      <Icon name="chevronRight" size="xs" class="shrink-0 text-gray-400" />
                    </button>
                  </div>
                </section>
              </div>

              <a
                v-if="docUrl"
                :href="docUrl"
                target="_blank"
                rel="noopener noreferrer"
                class="mt-6 flex items-center justify-between rounded-2xl border border-gray-200 p-5 transition hover:border-primary-300 hover:bg-primary-50/30 dark:border-dark-700 dark:hover:border-primary-800 dark:hover:bg-primary-950/10"
              >
                <span class="flex items-center gap-3">
                  <span class="flex h-10 w-10 items-center justify-center rounded-xl bg-gray-100 text-gray-600 dark:bg-dark-800 dark:text-dark-300"><Icon name="document" size="md" /></span>
                  <span>
                    <span class="block font-semibold text-gray-900 dark:text-white">{{ t('guide.moreDocs') }}</span>
                    <span class="mt-0.5 block text-sm text-gray-500 dark:text-dark-400">{{ t('guide.moreDocsDesc') }}</span>
                  </span>
                </span>
                <Icon name="externalLink" size="sm" class="text-gray-400" />
              </a>
            </div>
          </main>
        </div>
      </section>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import { useAppStore } from '@/stores'
import { sanitizeUrl } from '@/utils/url'
import { getGuideCatalog, type GuideArticle } from '@/features/guide/catalog'

const LAST_ARTICLE_KEY = 'modelport-guide-last-article'

const { t, locale } = useI18n()
const route = useRoute()
const router = useRouter()
const appStore = useAppStore()
const searchQuery = ref('')
const copiedCode = ref('')
const mobileNavigationOpen = ref(false)
const lastArticleSlug = ref(typeof window === 'undefined' ? '' : localStorage.getItem(LAST_ARTICLE_KEY) || '')

const catalog = computed(() => getGuideCatalog(locale.value))
const categories = computed(() => catalog.value.categories)
const articles = computed(() => catalog.value.articles)
const docUrl = computed(() => sanitizeUrl(appStore.docUrl))
const selectedArticle = computed(() => {
  const slug = route.params.articleSlug
  if (typeof slug !== 'string') return null
  return articles.value.find((article) => article.slug === slug) || null
})
const lastArticle = computed(() => articles.value.find((article) => article.slug === lastArticleSlug.value) || null)
const preferredCodexSlug = computed(() => {
  if (typeof navigator !== 'undefined' && navigator.userAgent.toLowerCase().includes('win')) {
    return 'codex-desktop-windows'
  }
  return 'codex-desktop-macos'
})
const filteredArticles = computed(() => {
  const query = searchQuery.value.trim().toLowerCase()
  if (!query) return articles.value
  return articles.value.filter((article) => {
    const haystack = [
      article.title,
      article.summary,
      article.os || '',
      ...article.keywords,
      ...article.steps.flatMap((step) => [step.title, step.description])
    ].join(' ').toLowerCase()
    return haystack.includes(query)
  })
})

function articlesByCategory(category: GuideArticle['category']) {
  return articles.value.filter((article) => article.category === category)
}

function articleIcon(article: GuideArticle): 'play' | 'download' | 'cog' | 'exclamationTriangle' {
  if (article.category === 'start') return 'play'
  if (article.category === 'downloads') return 'download'
  if (article.category === 'configuration') return 'cog'
  return 'exclamationTriangle'
}

async function goToArticle(slug: string) {
  mobileNavigationOpen.value = false
  await router.push('/guide/' + slug)
}

async function goHome() {
  mobileNavigationOpen.value = false
  searchQuery.value = ''
  await router.push('/guide')
}

async function copyCode(code: string) {
  try {
    await navigator.clipboard.writeText(code)
    copiedCode.value = code
    window.setTimeout(() => {
      if (copiedCode.value === code) copiedCode.value = ''
    }, 1600)
  } catch {
    copiedCode.value = ''
  }
}

watch(selectedArticle, (article) => {
  if (!article || typeof window === 'undefined') return
  lastArticleSlug.value = article.slug
  localStorage.setItem(LAST_ARTICLE_KEY, article.slug)
  window.scrollTo({ top: 0, behavior: 'smooth' })
}, { immediate: true })
</script>

<style scoped>
.guide-nav-item {
  @apply flex min-h-10 w-full items-center gap-2.5 rounded-xl px-3 py-2 text-sm font-medium text-gray-600 transition-colors hover:bg-white hover:text-gray-950 focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500/40 dark:text-dark-300 dark:hover:bg-dark-800 dark:hover:text-white;
}

.guide-nav-item-active {
  @apply bg-white text-primary-700 shadow-sm ring-1 ring-gray-200 dark:bg-dark-800 dark:text-primary-300 dark:ring-dark-700;
}

.guide-path-card {
  @apply flex min-h-[164px] flex-col items-start rounded-2xl border border-gray-200 bg-white p-5 text-left shadow-sm transition hover:-translate-y-0.5 hover:border-primary-300 hover:shadow-md focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500/40 dark:border-dark-700 dark:bg-dark-900 dark:hover:border-primary-800;
}

.guide-path-icon {
  @apply mb-4 flex h-10 w-10 items-center justify-center rounded-xl;
}
</style>
