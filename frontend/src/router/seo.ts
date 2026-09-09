import type { RouteLocationNormalizedLoaded } from 'vue-router'

/** ModelPort 的公开站点地址，canonical 不跟随当前请求 host。 */
export const MODELPORT_ORIGIN = 'https://modelport.top'
export const MODELPORT_HOME_TITLE = 'ModelPort AI API 中转站｜模型接入、计费与使用指南'
export const MODELPORT_HOME_DESCRIPTION = 'ModelPort 提供多平台 AI API 接入、模型分组路由和用量计费。查看 Codex、Claude Code 接入指南，以及模型倍率、拼团订阅与每日抽奖说明。'
export const MODELPORT_PUBLIC_INTRO_ID = 'modelport-public-intro'

const SEO_MARKER = 'data-modelport-seo'

export interface SeoAppState {
  siteName?: string | null
  backendModeEnabled?: boolean
}

type SeoRoute = Pick<RouteLocationNormalizedLoaded, 'path'>

function normalizedPath(path: string): string {
  const pathWithoutQuery = path.split(/[?#]/, 1)[0]
  if (!pathWithoutQuery) return '/'
  return pathWithoutQuery.startsWith('/') ? pathWithoutQuery : `/${pathWithoutQuery}`
}

function normalizedSiteName(siteName: unknown): string {
  return typeof siteName === 'string' ? siteName.trim() : ''
}

/** 只有 ModelPort 的 SPA 首页才允许搜索引擎索引。 */
export function isHomeRoute(route: SeoRoute): boolean {
  const path = normalizedPath(route.path)
  return path === '/' || path === '/home'
}

export function isModelPortHome(route: SeoRoute, appState: SeoAppState): boolean {
  return (
    isHomeRoute(route) &&
    normalizedSiteName(appState.siteName) === 'ModelPort' &&
    appState.backendModeEnabled !== true
  )
}

function removeSeoElements(): void {
  if (typeof document === 'undefined') return

  const elements = Array.from(document.head.querySelectorAll('meta, link'))
  for (const element of elements) {
    const name = element.getAttribute('name')?.trim().toLowerCase()
    const property = element.getAttribute('property')?.trim().toLowerCase()
    const rel = element.getAttribute('rel')?.trim().toLowerCase().split(/\s+/) ?? []
    const isDescription = name === 'description'
    const isRobots = name === 'robots'
    const isOpenGraph = property?.startsWith('og:') === true
    const isCanonical = rel.includes('canonical')
    const isManaged = element.hasAttribute(SEO_MARKER)

    // Clear both our tags and any stale tags that could have been emitted by
    // the document shell, so a private route never inherits homepage metadata.
    if (isManaged || isDescription || isRobots || isOpenGraph || isCanonical) {
      element.remove()
    }
  }
}

function appendMeta(attribute: 'name' | 'property', value: string, content: string): void {
  const meta = document.createElement('meta')
  meta.setAttribute(attribute, value)
  meta.setAttribute('content', content)
  meta.setAttribute(SEO_MARKER, 'true')
  document.head.appendChild(meta)
}

function appendCanonical(): void {
  const link = document.createElement('link')
  link.setAttribute('rel', 'canonical')
  link.setAttribute('href', `${MODELPORT_ORIGIN}/`)
  link.setAttribute(SEO_MARKER, 'true')
  document.head.appendChild(link)
}

function syncPublicIntro(isPublicHome: boolean): void {
  if (typeof document === 'undefined') return

  // The server-rendered shell owns this rich, visible copy. The SPA only
  // toggles its visibility so it never replaces content or adds duplicates.
  const intro = document.getElementById(MODELPORT_PUBLIC_INTRO_ID)
  if (!intro) return
  intro.hidden = !isPublicHome
  intro.setAttribute('aria-hidden', String(!isPublicHome))
}

/**
 * Synchronize route-level SEO metadata and the public intro element.
 * Calling this repeatedly is safe: each managed tag is rebuilt exactly once.
 */
export function syncRouteSeo(route: SeoRoute, appState: SeoAppState): void {
  if (typeof document === 'undefined' || !document.head) return

  const isPublicHome = isModelPortHome(route, appState)
  removeSeoElements()

  appendMeta('name', 'robots', isPublicHome ? 'index,follow' : 'noindex,nofollow')
  syncPublicIntro(isPublicHome)

  if (!isPublicHome) {
    return
  }

  document.title = MODELPORT_HOME_TITLE
  appendMeta('name', 'description', MODELPORT_HOME_DESCRIPTION)
  appendCanonical()
  appendMeta('property', 'og:title', MODELPORT_HOME_TITLE)
  appendMeta('property', 'og:description', MODELPORT_HOME_DESCRIPTION)
  appendMeta('property', 'og:url', `${MODELPORT_ORIGIN}/`)
  appendMeta('property', 'og:type', 'website')
  appendMeta('property', 'og:site_name', 'ModelPort')
}
