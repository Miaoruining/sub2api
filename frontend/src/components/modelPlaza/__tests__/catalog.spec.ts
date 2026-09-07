import { describe, expect, it } from 'vitest'
import type { ModelPlazaGroup, PlazaModel } from '@/api/modelPlaza'
import { buildModelCatalog, catalogAutoRoutes, catalogPrices } from '../catalog'

function model(overrides: Partial<PlazaModel> = {}): PlazaModel {
  return { name: 'gpt-test', platform: 'openai', official_pricing: null,
    pricing: { billing_mode: 'token', input_price: 0.000002, output_price: 0.000006,
      cache_read_price: 0, cache_write_price: null, image_input_price: null,
      image_output_price: null, per_request_price: null, intervals: [] }, ...overrides }
}
function group(id: number, overrides: Partial<ModelPlazaGroup> = {}): ModelPlazaGroup {
  return { id, name: `Group ${id}`, platform: 'openai', description: '',
    subscription_type: 'standard', is_exclusive: false, rate_multiplier: 1,
    peak_rate_enabled: false, peak_start: '', peak_end: '', peak_rate_multiplier: 1,
    image_rate_independent: false, image_rate_multiplier: 1, long_context_pricing_enabled: true,
    models: [model()], ...overrides }
}

describe('model catalog', () => {
  it('merges models without losing the selected group prices', () => {
    const groups = [group(1), group(2, { rate_multiplier: 4, user_rate_multiplier: 3 })]
    const catalog = buildModelCatalog(groups)
    expect(catalog).toHaveLength(1)
    expect(catalog[0].routes).toHaveLength(2)
    expect(catalogPrices(catalog[0], 'input_price')).toEqual([2, 6])
    expect(catalogPrices(catalog[0], 'cache_read_price')).toEqual([0, 0])
    expect(catalogPrices(buildModelCatalog([groups[1]])[0], 'input_price')).toEqual([6])
  })
  it('does not mix model providers or incompatible billing units', () => {
    const image = model()
    image.pricing = { ...image.pricing!, billing_mode: 'image', per_request_price: 0.02 }
    const catalog = buildModelCatalog([group(1), group(2, { models: [model({ platform: 'anthropic' })] }), group(3, { image_rate_independent: true, image_rate_multiplier: 2, rate_multiplier: 5, models: [image] })])
    expect(catalog).toHaveLength(3)
    expect(catalogPrices(catalog.find(m => m.mode === 'image')!, 'per_request_price')).toEqual([0.04])
  })
  it('never substitutes official prices or zero for missing customer prices', () => {
    const catalog = buildModelCatalog([group(1, { models: [model({ pricing: null, official_pricing: { input_price: 0.01, output_price: 0.02, cache_read_price: null, cache_write_price: null } })] })])
    expect(catalogPrices(catalog[0], 'input_price')).toEqual([])
  })
  it('keeps display-only prices but orders actual routes by the backend route order', () => {
    const catalog = buildModelCatalog([
      group(1, { sort_order: 10, models: [model({ auto_route_order: 1 })] }),
      group(2, { sort_order: 0, models: [model()] }),
      group(3, { sort_order: 20, models: [model({ auto_route_order: 0 })] })
    ])
    expect(catalog[0].routes.map(route => route.group.id)).toEqual([3, 1, 2])
    expect(catalogAutoRoutes(catalog[0]).map(route => route.group.id)).toEqual([3, 1])
  })
})
