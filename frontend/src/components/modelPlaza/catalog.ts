import type { ModelPlazaGroup, PlazaModel } from '@/api/modelPlaza'

export function plazaProviderOrder(platform: string): number {
  return ({ openai: 0, anthropic: 1, gemini: 2, grok: 3 } as Record<string, number>)[platform] ?? 4
}

export function plazaProviderBadge(platform: string): string {
  return ({
    openai: 'border-green-500/40 bg-green-500/10 text-green-700 dark:text-green-400',
    anthropic: 'border-red-500/40 bg-red-500/10 text-red-700 dark:text-red-400',
    gemini: 'border-blue-500/40 bg-blue-500/10 text-blue-700 dark:text-blue-400',
    grok: 'border-purple-500/40 bg-purple-500/10 text-purple-700 dark:text-purple-400'
  } as Record<string, string>)[platform] || 'border-gray-500/40 text-gray-600 dark:text-gray-400'
}

export interface ModelRoute { group: ModelPlazaGroup; model: PlazaModel }
export interface CatalogModel {
  id: string
  name: string
  platform: string
  mode: string
  routes: ModelRoute[]
}

// 同名、同供应商、同计费单位合并；不能把美元/张与美元/百万 token 混成区间。
export function buildModelCatalog(groups: ModelPlazaGroup[]): CatalogModel[] {
  const models = new Map<string, CatalogModel>()
  for (const group of groups) {
    for (const model of group.models) {
      const mode = model.pricing?.billing_mode || 'token'
      const id = `${model.platform}:${model.name}:${mode}`
      let entry = models.get(id)
      if (!entry) {
        entry = { id, name: model.name, platform: model.platform, mode, routes: [] }
        models.set(id, entry)
      }
      if (!entry.routes.some(route => route.group.id === group.id)) entry.routes.push({ group, model })
    }
  }

  for (const entry of models.values()) {
    entry.routes.sort((a, b) => {
      const aAuto = a.model.auto_route_order ?? Number.MAX_SAFE_INTEGER
      const bAuto = b.model.auto_route_order ?? Number.MAX_SAFE_INTEGER
      return aAuto - bAuto || (a.group.sort_order ?? 0) - (b.group.sort_order ?? 0) || a.group.id - b.group.id
    })
  }
  return [...models.values()].sort((a, b) => plazaProviderOrder(a.platform) - plazaProviderOrder(b.platform) || a.name.localeCompare(b.name))
}

/** 当前用户的默认智能候选快照；实际请求会按密钥策略和最新状态重排。 */
export function catalogAutoRoutes(entry: CatalogModel): ModelRoute[] {
  return entry.routes.filter(route => route.model.auto_route_order != null)
}

/** 模型级来源倍率；入口组只作为旧模型及字段缺失时的兼容回退。 */
export function routeRate(route: ModelRoute, mode?: string): number {
  const { group, model } = route
  const hasSource = model.source_group_id != null
  const baseRate = hasSource ? (model.rate_multiplier ?? group.rate_multiplier) : group.rate_multiplier
  const userRate = hasSource ? model.user_rate_multiplier : group.user_rate_multiplier
  if ((mode ?? model.pricing?.billing_mode) === 'image') {
    const independent = hasSource ? model.image_rate_independent : group.image_rate_independent
    if (independent) return hasSource ? (model.image_rate_multiplier ?? 1) : group.image_rate_multiplier
  }
  return userRate ?? baseRate
}

export function routeRatesForGroup(group: ModelPlazaGroup): number[] {
  const rates = group.models.map(model => routeRate({ group, model }))
  return [...new Set(rates.filter(rate => Number.isFinite(rate) && rate >= 0))].sort((a, b) => a - b)
}

export function catalogPrices(entry: CatalogModel, field: 'input_price' | 'output_price' | 'cache_read_price' | 'per_request_price'): number[] {
  return entry.routes.flatMap(route => {
    const { model } = route
    const price = model.pricing?.[field]
    if (price == null || !Number.isFinite(price) || price < 0) return []
    const rate = routeRate(route, entry.mode)
    if (!Number.isFinite(rate) || rate < 0) return []
    return [price * rate * (field === 'per_request_price' ? 1 : 1_000_000)]
  })
}
