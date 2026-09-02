package service

import (
	"errors"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
)

const (
	defaultOpenAIMessagesDispatchOpusMappedModel   = "gpt-5.4"
	defaultOpenAIMessagesDispatchSonnetMappedModel = "gpt-5.3-codex"
	defaultOpenAIMessagesDispatchHaikuMappedModel  = "gpt-5.4-mini"
	defaultGeminiMessagesDispatchProMappedModel    = "gemini-2.5-pro"
	defaultGeminiMessagesDispatchFlashMappedModel  = "gemini-2.5-flash"
)

var ErrMessagesDispatchModelNotFound = errors.New("messages dispatch model not found")

type MessagesDispatchResolution struct {
	PublicModel string
	TargetModel string
	MappingRule string
	Mapped      bool
}

func normalizeOpenAIMessagesDispatchMappedModel(model string) string {
	model = NormalizeOpenAICompatRequestedModel(strings.TrimSpace(model))
	return strings.TrimSpace(model)
}

func normalizeOpenAIMessagesDispatchModelConfig(cfg OpenAIMessagesDispatchModelConfig) OpenAIMessagesDispatchModelConfig {
	return normalizeMessagesDispatchModelConfig(PlatformOpenAI, cfg)
}

func defaultGeminiMessagesDispatchModelConfig() OpenAIMessagesDispatchModelConfig {
	return OpenAIMessagesDispatchModelConfig{
		OpusMappedModel:   defaultGeminiMessagesDispatchProMappedModel,
		SonnetMappedModel: defaultGeminiMessagesDispatchProMappedModel,
		HaikuMappedModel:  defaultGeminiMessagesDispatchFlashMappedModel,
	}
}

func normalizeMessagesDispatchMappedModel(platform, model string) string {
	model = strings.TrimSpace(model)
	if platform == PlatformOpenAI || platform == PlatformComposite {
		return normalizeOpenAIMessagesDispatchMappedModel(model)
	}
	return model
}

func normalizeMessagesDispatchModelConfig(platform string, cfg OpenAIMessagesDispatchModelConfig) OpenAIMessagesDispatchModelConfig {
	out := OpenAIMessagesDispatchModelConfig{
		OpusMappedModel:   normalizeMessagesDispatchMappedModel(platform, cfg.OpusMappedModel),
		SonnetMappedModel: normalizeMessagesDispatchMappedModel(platform, cfg.SonnetMappedModel),
		HaikuMappedModel:  normalizeMessagesDispatchMappedModel(platform, cfg.HaikuMappedModel),
	}
	if platform == PlatformGemini {
		defaults := defaultGeminiMessagesDispatchModelConfig()
		if out.OpusMappedModel == "" {
			out.OpusMappedModel = defaults.OpusMappedModel
		}
		if out.SonnetMappedModel == "" {
			out.SonnetMappedModel = defaults.SonnetMappedModel
		}
		if out.HaikuMappedModel == "" {
			out.HaikuMappedModel = defaults.HaikuMappedModel
		}
	}

	if len(cfg.ExactModelMappings) > 0 {
		out.ExactModelMappings = make(map[string]string, len(cfg.ExactModelMappings))
		for requestedModel, mappedModel := range cfg.ExactModelMappings {
			requestedModel = strings.TrimSpace(requestedModel)
			mappedModel = normalizeMessagesDispatchMappedModel(platform, mappedModel)
			if requestedModel == "" || mappedModel == "" {
				continue
			}
			out.ExactModelMappings[requestedModel] = mappedModel
		}
		if len(out.ExactModelMappings) == 0 {
			out.ExactModelMappings = nil
		}
	}

	return out
}

func longestMessagesDispatchMapping(mappings map[string]string, requestedModel string) (targetModel, mappingRule string) {
	if target := strings.TrimSpace(mappings[requestedModel]); target != "" {
		return target, requestedModel
	}

	bestPrefix := ""
	for rawRule, rawTarget := range mappings {
		rule := strings.TrimSpace(rawRule)
		target := strings.TrimSpace(rawTarget)
		if target == "" || !strings.HasSuffix(rule, "*") {
			continue
		}
		prefix := strings.TrimSuffix(rule, "*")
		if strings.HasPrefix(requestedModel, prefix) && len(prefix) > len(bestPrefix) {
			bestPrefix = prefix
			targetModel = target
			mappingRule = rule
		}
	}
	return targetModel, mappingRule
}

func ResolveGeminiAnthropicModel(group *Group, requestedModel string) (MessagesDispatchResolution, error) {
	requestedModel = strings.TrimSpace(requestedModel)
	resolution := MessagesDispatchResolution{PublicModel: requestedModel, TargetModel: requestedModel}
	if group == nil || group.Platform != PlatformGemini || requestedModel == "" {
		return resolution, ErrMessagesDispatchModelNotFound
	}
	if strings.HasPrefix(strings.ToLower(requestedModel), "gemini-") {
		return resolution, nil
	}
	if !group.AllowMessagesDispatch {
		return resolution, ErrMessagesDispatchModelNotFound
	}

	cfg := normalizeMessagesDispatchModelConfig(PlatformGemini, group.MessagesDispatchModelConfig)
	if target, rule := longestMessagesDispatchMapping(cfg.ExactModelMappings, requestedModel); target != "" {
		resolution.TargetModel = target
		resolution.MappingRule = rule
		resolution.Mapped = target != requestedModel
		return resolution, nil
	}

	switch claudeMessagesDispatchFamily(requestedModel) {
	case "opus":
		resolution.TargetModel = cfg.OpusMappedModel
		resolution.MappingRule = "claude-opus-*"
	case "sonnet":
		resolution.TargetModel = cfg.SonnetMappedModel
		resolution.MappingRule = "claude-sonnet-*"
	case "haiku":
		resolution.TargetModel = cfg.HaikuMappedModel
		resolution.MappingRule = "claude-haiku-*"
	default:
		return resolution, ErrMessagesDispatchModelNotFound
	}
	if resolution.TargetModel == "" {
		return resolution, ErrMessagesDispatchModelNotFound
	}
	resolution.Mapped = resolution.TargetModel != requestedModel
	return resolution, nil
}

func claudeMessagesDispatchFamily(model string) string {
	normalized := strings.ToLower(strings.TrimSpace(model))
	if !strings.HasPrefix(normalized, "claude") {
		return ""
	}
	switch {
	case strings.Contains(normalized, "opus"):
		return "opus"
	case strings.Contains(normalized, "sonnet"):
		return "sonnet"
	case strings.Contains(normalized, "haiku"):
		return "haiku"
	default:
		return ""
	}
}

func (g *Group) ResolveMessagesDispatchModel(requestedModel string) string {
	if g == nil {
		return ""
	}
	requestedModel = strings.TrimSpace(requestedModel)
	if requestedModel == "" {
		return ""
	}

	if g.Platform == PlatformGrok {
		if claudeMessagesDispatchFamily(requestedModel) == "" {
			return ""
		}
		opts := xai.RuntimeModelMappingOptions()
		if !opts.EnableCrossClientMap {
			return ""
		}
		return xai.ModelMappingWithOptions(opts)["claude-*"]
	}

	// 国产供应商分组:调度级模型映射不适用(其配置被 sanitize 置空,且下方的
	// gpt-5.x 默认值是 openai 专属,发给 CN 上游必错)。模型改写完全交给账号级
	// model_mapping;anthropic 协议上游本身接受 claude-* 模型名。
	if IsCNProvider(g.Platform) {
		return ""
	}

	cfg := normalizeOpenAIMessagesDispatchModelConfig(g.MessagesDispatchModelConfig)
	if mappedModel := strings.TrimSpace(cfg.ExactModelMappings[requestedModel]); mappedModel != "" {
		return mappedModel
	}

	switch claudeMessagesDispatchFamily(requestedModel) {
	case "opus":
		if mappedModel := strings.TrimSpace(cfg.OpusMappedModel); mappedModel != "" {
			return mappedModel
		}
		return defaultOpenAIMessagesDispatchOpusMappedModel
	case "sonnet":
		if mappedModel := strings.TrimSpace(cfg.SonnetMappedModel); mappedModel != "" {
			return mappedModel
		}
		return defaultOpenAIMessagesDispatchSonnetMappedModel
	case "haiku":
		if mappedModel := strings.TrimSpace(cfg.HaikuMappedModel); mappedModel != "" {
			return mappedModel
		}
		return defaultOpenAIMessagesDispatchHaikuMappedModel
	default:
		return ""
	}
}

func sanitizeGroupMessagesDispatchFields(g *Group) {
	if g == nil || g.Platform == PlatformOpenAI {
		return
	}
	if g.Platform == PlatformGemini {
		g.DefaultMappedModel = ""
		g.MessagesDispatchModelConfig = normalizeMessagesDispatchModelConfig(PlatformGemini, g.MessagesDispatchModelConfig)
		return
	}
	if g.Platform != PlatformComposite {
		g.AllowMessagesDispatch = false
	}
	g.DefaultMappedModel = ""
	g.MessagesDispatchModelConfig = OpenAIMessagesDispatchModelConfig{}
}
