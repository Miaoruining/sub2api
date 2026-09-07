<template>
  <BaseDialog :show="show" :title="t('keys.ccSwitchGuide.title')" @close="emit('close')">
    <div class="space-y-5">
      <p class="text-sm text-gray-600 dark:text-dark-300">{{ t('keys.ccSwitchGuide.intro') }}</p>
      <p v-if="!apiKey?.group && !apiKey?.auto_group" role="alert" class="text-sm text-amber-700 dark:text-amber-300">
        {{ t('keys.useKeyModal.noGroupDescription') }}
      </p>
      <p v-else-if="apiKey?.status !== 'active'" role="alert" class="text-sm text-amber-700 dark:text-amber-300">
        {{ t('keys.ccSwitchGuide.inactive') }}
      </p>
      <dl class="space-y-3 rounded-xl bg-gray-50 p-4 text-sm dark:bg-dark-800">
        <div><dt class="text-gray-500 dark:text-dark-400">{{ t('keys.ccSwitchGuide.provider') }}</dt><dd class="mt-1 break-words font-medium text-gray-900 dark:text-white">{{ providerName }}</dd></div>
        <div><dt class="text-gray-500 dark:text-dark-400">{{ t('keys.groupLabel') }}</dt><dd class="mt-1 text-gray-900 dark:text-white">{{ apiKey?.auto_group ? t('keys.autoGroup') : apiKey?.group?.name || '—' }}</dd></div>
        <div><dt class="text-gray-500 dark:text-dark-400">{{ t('keys.ccSwitchGuide.endpoint') }}</dt><dd class="mt-1 break-all font-mono text-gray-900 dark:text-white">{{ config.endpoint }}</dd></div>
      </dl>
      <label v-if="apiKey?.auto_group" class="block text-sm font-medium text-gray-900 dark:text-white">
        {{ t('keys.ccsClientSelect.title') }}
        <select v-model="autoPlatform" class="input mt-2">
          <option value="openai">Codex</option><option value="anthropic">Claude Code</option><option value="gemini">Gemini CLI</option>
        </select>
      </label>
      <label v-if="platform === 'antigravity'" class="block text-sm font-medium text-gray-900 dark:text-white">
        {{ t('keys.ccsClientSelect.title') }}
        <select v-model="clientType" class="input mt-2">
          <option value="claude">Claude Code</option><option value="gemini">Gemini CLI</option>
        </select>
      </label>
      <div v-if="platform === 'openai'" class="space-y-2">
        <label for="ccswitch-model" class="block text-sm font-medium text-gray-900 dark:text-white">{{ t('keys.ccSwitchGuide.model') }}</label>
        <select v-if="modelState === 'ready'" id="ccswitch-model" v-model="model" class="input">
          <option v-for="slug in models" :key="slug" :value="slug">{{ slug }}</option>
        </select>
        <input v-else id="ccswitch-model" v-model="model" class="input font-mono" :disabled="modelState === 'loading'" :placeholder="OPENAI_CC_SWITCH_CODEX_MODEL" />
        <p v-if="modelState === 'loading'" role="status" class="text-xs text-gray-500">{{ t('keys.ccSwitchGuide.loading') }}</p>
        <div v-if="modelState === 'error'" role="status" class="text-xs text-amber-700 dark:text-amber-300">
          {{ t('keys.ccSwitchGuide.modelError') }}
          <button type="button" class="ml-1 underline" @click="loadModels">{{ t('keys.useKeyModal.codexModelCatalog.retry') }}</button>
        </div>
      </div>
      <label class="flex items-start gap-2 text-sm text-gray-700 dark:text-dark-300">
        <input v-model="usageEnabled" type="checkbox" class="mt-1 rounded" />
        <span>{{ t('keys.ccSwitchGuide.usage') }}<span class="mt-1 block text-xs text-gray-500 dark:text-dark-400">{{ t('keys.ccSwitchGuide.usageHint') }}</span></span>
      </label>
      <p class="text-xs leading-5 text-gray-500 dark:text-dark-400">{{ t('keys.ccSwitchGuide.secretHint') }}</p>
      <div class="rounded-xl border border-gray-200 p-4 dark:border-dark-700">
        <p class="mb-2 text-sm font-medium text-gray-900 dark:text-white">{{ t('keys.ccSwitchGuide.stepsTitle') }}</p>
        <ol class="list-decimal space-y-2 pl-5 text-sm text-gray-600 dark:text-dark-300">
          <li>{{ t('keys.ccSwitchGuide.step1') }}</li>
          <li>{{ t('keys.ccSwitchGuide.step2') }}</li>
          <li>{{ t('keys.ccSwitchGuide.step3') }}</li>
        </ol>
      </div>
      <p v-if="launchState !== 'idle'" role="status" class="rounded-lg bg-blue-50 p-3 text-sm text-blue-800 dark:bg-blue-950/30 dark:text-blue-200">
        {{ t(launchState === 'error' ? 'keys.ccSwitchGuide.launchError' : 'keys.ccSwitchGuide.launched') }}
      </p>
      <a href="https://github.com/farion1231/cc-switch/releases/latest" target="_blank" rel="noopener noreferrer" class="inline-block text-sm text-primary-600 underline dark:text-primary-400">{{ t('keys.ccSwitchGuide.download') }}</a>
    </div>
    <template #footer>
      <div class="flex flex-wrap justify-end gap-2">
        <button type="button" class="btn btn-secondary" @click="emit('manual')">{{ t('keys.ccSwitchGuide.manual') }}</button>
        <button type="button" class="btn btn-primary" :disabled="!canImport" @click="launch">{{ t('keys.ccSwitchGuide.import') }}</button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import type { ApiKey } from '@/types'
import { fetchCodexModelsManifest } from '@/api/codex'
import { parseCodexCatalogModels } from '@/utils/codexCatalogConfig'
import {
  buildCcSwitchImportDeeplink, resolveCcSwitchImportConfig,
  CC_SWITCH_USAGE_SCRIPT, OPENAI_CC_SWITCH_CODEX_MODEL, type CcSwitchClientType
} from '@/utils/ccswitchImport'

const props = defineProps<{ show: boolean; apiKey: ApiKey | null; baseUrl: string; siteName: string }>()
const emit = defineEmits<{ (e: 'close'): void; (e: 'manual'): void }>()
const { t } = useI18n()
const clientType = ref<CcSwitchClientType>('claude')
const usageEnabled = ref(false)
const model = ref('')
const models = ref<string[]>([])
const modelState = ref<'idle' | 'loading' | 'ready' | 'error'>('idle')
const launchState = ref<'idle' | 'launched' | 'error'>('idle')
const autoPlatform = ref<'openai' | 'anthropic' | 'gemini'>('openai')
const platform = computed(() => props.apiKey?.auto_group ? autoPlatform.value : props.apiKey?.group?.platform)
const baseUrl = computed(() => props.baseUrl.trim() || window.location.origin)
const providerName = computed(() => [props.siteName.trim() || 'ModelPort', props.apiKey?.group?.name, props.apiKey?.name].filter(Boolean).join(' · '))
const config = computed(() => resolveCcSwitchImportConfig(platform.value, clientType.value, baseUrl.value))
const canImport = computed(() => Boolean(props.apiKey?.key && (props.apiKey.group || props.apiKey.auto_group) && props.apiKey.status === 'active') &&
  (platform.value !== 'openai' || ((props.apiKey?.auto_group ? modelState.value === 'ready' : modelState.value !== 'loading') && Boolean(model.value.trim()))))
let controller: AbortController | null = null

async function loadModels() {
  controller?.abort()
  if (!props.show || platform.value !== 'openai' || !props.apiKey?.key) return
  const request = new AbortController()
  controller = request
  const timeout = setTimeout(() => request.abort(), 10000)
  modelState.value = 'loading'
  try {
    const manifest = await fetchCodexModelsManifest(baseUrl.value, props.apiKey.key, request.signal)
    if (controller !== request || request.signal.aborted) return
    const slugs = [...new Set(parseCodexCatalogModels(manifest.content).map(entry => entry.slug))]
    if (!slugs.length) throw new Error('Empty catalog')
    models.value = slugs
    model.value = slugs.includes(OPENAI_CC_SWITCH_CODEX_MODEL) ? OPENAI_CC_SWITCH_CODEX_MODEL : slugs[0]
    modelState.value = 'ready'
  } catch {
    if (controller === request) {
      model.value = ''
      modelState.value = 'error'
    }
  } finally {
    clearTimeout(timeout)
  }
}

watch(() => [props.show, props.apiKey?.id, props.apiKey?.key, props.apiKey?.group_id, props.apiKey?.auto_group, autoPlatform.value, props.baseUrl], () => {
  controller?.abort()
  controller = null
  model.value = ''
  models.value = []
  modelState.value = 'idle'
  launchState.value = 'idle'
  usageEnabled.value = false
  clientType.value = platform.value === 'gemini' ? 'gemini' : 'claude'
  if (props.show) void loadModels()
}, { immediate: true })
onBeforeUnmount(() => { controller?.abort(); controller = null })

function launch() {
  if (!canImport.value || !props.apiKey) return
  try {
    // Build only on the user's click; never persist, log, or expose the secret URL in the DOM.
    const deeplink = buildCcSwitchImportDeeplink({
      baseUrl: baseUrl.value, platform: platform.value, clientType: clientType.value,
      providerName: providerName.value, apiKey: props.apiKey.key,
      model: platform.value === 'openai' ? model.value : undefined,
      usageEnabled: usageEnabled.value, usageScript: CC_SWITCH_USAGE_SCRIPT
    })
    window.open(deeplink, '_self')
    launchState.value = 'launched'
  } catch {
    launchState.value = 'error'
  }
}
</script>
