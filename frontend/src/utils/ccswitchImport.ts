import type { GroupPlatform } from '@/types'

export const OPENAI_CC_SWITCH_CODEX_MODEL = 'gpt-5.6-sol'
export const GROK_CC_SWITCH_MODEL = 'grok-4.5'

export type CcSwitchClientType = 'claude' | 'gemini'

export interface CcSwitchImportConfig {
  app: string
  endpoint: string
  model?: string
}

export interface CcSwitchImportDeeplinkInput {
  baseUrl: string
  platform?: GroupPlatform | null
  clientType: CcSwitchClientType
  providerName: string
  apiKey: string
  model?: string
  usageEnabled?: boolean
  usageScript?: string
}

function withV1Endpoint(baseUrl: string): string {
  const normalizedBaseUrl = baseUrl.trim().replace(/\/+$/, '')
  return normalizedBaseUrl.endsWith('/v1') ? normalizedBaseUrl : `${normalizedBaseUrl}/v1`
}

export function resolveCcSwitchImportConfig(
  platform: GroupPlatform | undefined | null,
  clientType: CcSwitchClientType,
  baseUrl: string
): CcSwitchImportConfig {
  baseUrl = baseUrl.trim().replace(/\/+$/, '')
  switch (platform || 'anthropic') {
    case 'antigravity':
      return {
        app: clientType === 'gemini' ? 'gemini' : 'claude',
        endpoint: `${baseUrl.replace(/\/v1$/, '')}/antigravity`
      }
    case 'openai':
      return {
        app: 'codex',
        endpoint: withV1Endpoint(baseUrl),
        model: OPENAI_CC_SWITCH_CODEX_MODEL
      }
    case 'gemini':
      return {
        app: 'gemini',
        endpoint: baseUrl
      }
    case 'grok':
      return {
        app: 'grokbuild',
        endpoint: withV1Endpoint(baseUrl),
        model: GROK_CC_SWITCH_MODEL
      }
    default:
      return {
        app: 'claude',
        endpoint: baseUrl
      }
  }
}

export function buildCcSwitchImportDeeplink(input: CcSwitchImportDeeplinkInput): string {
  const config = resolveCcSwitchImportConfig(input.platform, input.clientType, input.baseUrl)
  const entries: [string, string][] = [
    ['resource', 'provider'],
    ['app', config.app],
    ['name', input.providerName],
    ['homepage', input.baseUrl],
    ['endpoint', config.endpoint],
    ['apiKey', input.apiKey]
  ]

  const model = input.model?.trim() || config.model
  if (model) {
    entries.push(['model', model])
  }
  if (input.usageEnabled && input.usageScript) {
    const bytes = new TextEncoder().encode(input.usageScript)
    entries.push(
      ['usageEnabled', 'true'],
      ['usageScript', btoa(Array.from(bytes, byte => String.fromCharCode(byte)).join(''))],
      ['usageBaseUrl', input.baseUrl.trim().replace(/\/+$/, '').replace(/\/v1$/, '')],
      ['usageAutoInterval', '30']
    )
  }

  return `ccswitch://v1/import?${new URLSearchParams(entries).toString()}`
}

export const CC_SWITCH_USAGE_SCRIPT = `({
  request: {
    url: "{{baseUrl}}/v1/usage",
    method: "GET",
    headers: { "Authorization": "Bearer {{apiKey}}" }
  },
  extractor: function(response) {
    return {
      isValid: response?.is_active ?? response?.isValid ?? false,
      remaining: response?.remaining ?? response?.quota?.remaining ?? response?.balance,
      unit: response?.unit ?? response?.quota?.unit ?? "USD"
    };
  }
})`
