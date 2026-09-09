import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import {
  MODELPORT_HOME_TITLE,
  MODELPORT_PUBLIC_INTRO_ID,
  syncRouteSeo,
} from '@/router/seo'

function route(path: string) {
  return { path } as Parameters<typeof syncRouteSeo>[0]
}

function metaByName(name: string): HTMLMetaElement[] {
  return Array.from(document.head.querySelectorAll('meta')).filter(
    (element) => element.getAttribute('name')?.toLowerCase() === name.toLowerCase(),
  ) as HTMLMetaElement[]
}

function metaByProperty(property: string): HTMLMetaElement[] {
  return Array.from(document.head.querySelectorAll('meta')).filter(
    (element) => element.getAttribute('property')?.toLowerCase() === property.toLowerCase(),
  ) as HTMLMetaElement[]
}

afterEach(() => {
  document.head.innerHTML = ''
  document.body.innerHTML = '<div id="app"></div><section id="modelport-public-intro">服务端简介</section>'
  document.title = ''
})

beforeEach(() => {
  document.head.innerHTML = ''
  document.body.innerHTML = '<div id="app"></div><section id="modelport-public-intro">服务端简介</section>'
})

describe('客户端 SEO 同步', () => {
  it('ModelPort 首页写入标题、索引元数据，并显示 #app 外的服务端简介', () => {
    syncRouteSeo(route('/home'), { siteName: 'ModelPort', backendModeEnabled: false })

    expect(document.title).toBe(MODELPORT_HOME_TITLE)
    expect(metaByName('robots')).toHaveLength(1)
    expect(metaByName('robots')[0].content).toBe('index,follow')
    expect(metaByName('description')).toHaveLength(1)
    expect(metaByName('description')[0].content)
      .toBe('ModelPort 提供多平台 AI API 接入、模型分组路由和用量计费。查看 Codex、Claude Code 接入指南，以及模型倍率、拼团订阅与每日抽奖说明。')
    expect(metaByProperty('og:title')).toHaveLength(1)
    expect(document.querySelector('link[rel="canonical"]')).not.toBeNull()

    const intro = document.getElementById(MODELPORT_PUBLIC_INTRO_ID)
    expect(intro).not.toBeNull()
    expect(intro?.textContent).toBe('服务端简介')
    expect(intro?.hidden).toBe(false)
    expect(intro?.parentElement).toBe(document.body)
    expect(document.getElementById('app')?.contains(intro)).toBe(false)
  })

  it('私有页切换会移除首页 canonical/OG/简介并设置 noindex', () => {
    syncRouteSeo(route('/home'), { siteName: 'ModelPort', backendModeEnabled: false })
    syncRouteSeo(route('/dashboard'), { siteName: 'ModelPort', backendModeEnabled: false })

    expect(metaByName('robots')[0].content).toBe('noindex,nofollow')
    expect(metaByName('description')).toHaveLength(0)
    expect(document.querySelector('link[rel="canonical"]')).toBeNull()
    expect(metaByProperty('og:title')).toHaveLength(0)
    expect(document.getElementById(MODELPORT_PUBLIC_INTRO_ID)?.hidden).toBe(true)
    expect(document.getElementById(MODELPORT_PUBLIC_INTRO_ID)?.textContent).toBe('服务端简介')
  })

  it('重复同步不会累积标签，canonical 和 og:url 会剥离查询参数', () => {
    syncRouteSeo(route('/home?utm_source=test'), { siteName: 'ModelPort', backendModeEnabled: false })
    syncRouteSeo(route('/home?utm_source=again'), { siteName: 'ModelPort', backendModeEnabled: false })

    expect(metaByName('robots')).toHaveLength(1)
    expect(metaByName('description')).toHaveLength(1)
    expect(metaByProperty('og:title')).toHaveLength(1)
    expect(metaByProperty('og:description')).toHaveLength(1)
    expect(metaByProperty('og:url')).toHaveLength(1)
    expect(document.querySelector('link[rel="canonical"]')?.getAttribute('href'))
      .toBe('https://modelport.top/')
    expect(metaByProperty('og:url')[0].content).toBe('https://modelport.top/')
  })

  it('没有服务端简介时不自行创建文案节点', () => {
    document.body.innerHTML = '<div id="app"></div>'

    syncRouteSeo(route('/home'), { siteName: 'ModelPort', backendModeEnabled: false })

    expect(document.getElementById(MODELPORT_PUBLIC_INTRO_ID)).toBeNull()
  })

  it.each([
    ['other site', { siteName: 'Sub2API', backendModeEnabled: false }],
    ['backend mode', { siteName: 'ModelPort', backendModeEnabled: true }],
  ])('%s 首页不会被索引，也不会暴露 ModelPort 简介', (_label, appState) => {
    document.title = 'Existing brand title'
    syncRouteSeo(route('/home'), appState)

    expect(metaByName('robots')[0].content).toBe('noindex,nofollow')
    expect(document.title).toBe('Existing brand title')
    expect(document.getElementById(MODELPORT_PUBLIC_INTRO_ID)?.hidden).toBe(true)
    expect(document.querySelector('link[rel="canonical"]')).toBeNull()
    expect(metaByProperty('og:title')).toHaveLength(0)
  })
})
