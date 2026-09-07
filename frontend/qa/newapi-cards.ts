// 隔离视觉测试，使用与本地 New API 只读参考预览相同的数据；不进入生产构建。
import { createApp, h } from 'vue'
import { createI18n } from 'vue-i18n'
import PlazaModelCard from '../src/components/modelPlaza/PlazaModelCard.vue'
import { buildModelCatalog } from '../src/components/modelPlaza/catalog'
import type { ModelPlazaGroup } from '../src/api/modelPlaza'
import zh from '../src/i18n/locales/zh/dashboard'
import '../src/style.css'

const group: ModelPlazaGroup = {
  id: 1, name: 'airpaper', platform: 'grok', description: '', subscription_type: 'standard', rate_multiplier: 1,
  peak_rate_enabled: false, peak_start: '', peak_end: '', peak_rate_multiplier: 1, is_exclusive: false,
  image_rate_independent: false, image_rate_multiplier: 1, long_context_pricing_enabled: true,
  models: ['grok-4.5', 'grok-4.6'].map(name => ({ name, platform: 'grok', official_pricing: null, pricing: {
    billing_mode: 'token', input_price: .5 / 1e6, output_price: 1.5 / 1e6,
    cache_write_price: null, cache_read_price: .125 / 1e6,
    image_input_price: null, image_output_price: null, per_request_price: null, intervals: []
  } }))
}
createApp({ render: () => buildModelCatalog([group]).map(entry => h(PlazaModelCard, { entry, key: entry.id })) })
  .use(createI18n({ legacy: false, locale: 'zh', messages: { zh } })).mount('#fixture')
