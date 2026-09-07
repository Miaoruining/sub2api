import type { ModelPlazaGroup, PlazaModel } from '@/api/modelPlaza'

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
  return [...models.values()].sort((a, b) => a.name.localeCompare(b.name))
}

export function catalogPrices(entry: CatalogModel, field: 'input_price' | 'output_price' | 'cache_read_price' | 'per_request_price'): number[] {
  return entry.routes.flatMap(({ group, model }) => {
    const price = model.pricing?.[field]
    if (price == null || !Number.isFinite(price) || price < 0) return []
    const rate = entry.mode === 'image' && group.image_rate_independent
      ? group.image_rate_multiplier : group.user_rate_multiplier ?? group.rate_multiplier
    if (!Number.isFinite(rate) || rate < 0) return []
    return [price * rate * (field === 'per_request_price' ? 1 : 1_000_000)]
  })
}
