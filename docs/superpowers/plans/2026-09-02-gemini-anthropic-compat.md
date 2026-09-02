# Gemini Anthropic Messages 兼容层 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让 Gemini/SuperGrok 订阅账号所在的 Gemini 分组通过 ModelPort 的 `/v1/messages`、`/v1/messages/count_tokens` 和 `/v1/models` 向 Claude Code/Anthropic SDK 提供可配置、可计费、可回滚的 Anthropic 兼容服务。

**Architecture:** 复用分组现有 `allow_messages_dispatch` 与 `messages_dispatch_model_config`，在账号调度前把公开 `claude-*` 模型解析成实际 `gemini-*` 模型，同时始终分别保存公开模型和上游模型。现有 `GeminiMessagesCompatService` 继续负责 Anthropic↔Gemini 转换；Gemini 原生 `countTokens` 抽成共享内核，由 Google 原生入口和 Anthropic 包装入口共同调用。管理界面只对 Gemini 分组开放这组兼容配置，保存时以分组内可调度账号进行服务端校验。

**Tech Stack:** Go 1.27.0、Gin、Ent/PostgreSQL、Redis、Testify；Vue 3、TypeScript 5.6、Vite、Vitest、Tailwind CSS；现有 Sub2API 调度、计费和 usage log 基础设施。

**Spec:** `docs/superpowers/specs/2026-09-02-gemini-anthropic-compat-design.md`

## Global Constraints

- 公开入口保持为 `POST /v1/messages`、`POST /v1/messages/count_tokens`、`GET /v1/models`，客户只获得 ModelPort Base URL 和自己的 API Key。
- 功能仅影响目标 Gemini 分组；Anthropic、OpenAI、Grok、Antigravity、国产供应商和 Composite 的既有行为必须保持不变。
- 兼容映射默认关闭；关闭后 `gemini-*` 直传仍可用，`claude-*` 别名返回 `not_found_error`。
- 精确映射优先于通配符，通配符按最长前缀优先；映射必须在账号选择和模型能力检查前完成。
- 响应 `model` 保留客户请求模型；`ForwardResult.UpstreamModel`、usage log 和计费使用实际 Gemini 模型。
- 未知且会改变语义的 Anthropic 内容块必须返回 `invalid_request_error`，不得序列化成普通文本。
- 不宣称支持 Anthropic Extended Thinking；Gemini `thought: true` 内容不得进入正文或 SSE 文本增量。
- `count_tokens` 校验鉴权、分组、余额/订阅和模型能力，但不占生成并发、不扣费、不写生成 usage。
- 图片请求无法执行真实 `countTokens` 时必须报错，不得使用会低估图片 token 的本地估算。
- 不新增第二套用户、API Key、余额、订阅或计费系统，不增加 LiteLLM，不新增数据库迁移。
- 生产验收会创建测试 Key 并产生真实上游费用，执行前必须再次取得用户明确确认。

---

## File map and interface contract

### Backend domain and routing

- `backend/internal/domain/openai_messages_dispatch.go`：保留数据库 JSON 结构，更新为 OpenAI/Gemini 共用的 Messages 调度配置说明。
- `backend/internal/service/openai_messages_dispatch.go`：按分组平台归一化配置，解析精确/最长通配符/Claude 家族默认映射，校验 Gemini 目标模型。
- `backend/internal/service/admin_group.go`：创建/编辑 Gemini 分组时校验开关、映射和可调度账号能力；继续使用现有字段，不迁移数据库。
- `backend/internal/handler/gateway_handler.go`：解析公开模型与实际调度模型，实际模型进入选号，公开模型进入响应与 usage 的 requested model。
- `backend/internal/service/gemini_messages_compat_service.go`：接收公开模型、调度模型两个显式参数，完成请求/响应/SSE/usage 转换。
- `backend/internal/server/routes/gateway.go`：Gemini 分组的 `/messages/count_tokens` 路由到专用处理器。
- `backend/internal/server/middleware/middleware.go`：Messages 入口的鉴权早退使用 Anthropic 错误结构。

### Backend token counting and observability

- Create `backend/internal/service/gemini_anthropic_count_tokens.go`：共享 Gemini `countTokens` 内核、Anthropic 包装、估算标记和图片回退门禁。
- Create `backend/internal/service/gemini_anthropic_count_tokens_test.go`：API Key/OAuth/Service Account、真实计数和估算回退测试。
- `backend/internal/service/gateway_usage_billing.go`：锁定实际 Gemini 模型计费和公开/上游模型分栏记录。
- `backend/internal/handler/gateway_models_test.go`：锁定启用兼容后的公开 Claude 别名列表。

### Frontend administration

- `frontend/src/types/index.ts`：把现有配置类型从 OpenAI 专属语义改成 Messages 兼容映射语义，保持 JSON 字段名不变。
- `frontend/src/views/admin/groupsMessagesDispatch.ts`：提供按平台默认值、预览、序列化和表单校验。
- `frontend/src/views/admin/GroupsView.vue`：Gemini 编辑页显示“Anthropic / Claude Code 兼容”卡片；创建页只能保持关闭并提示先绑定账号。
- `frontend/src/i18n/locales/zh/admin/overview.ts`、`frontend/src/i18n/locales/en/admin/overview.ts`：增加 Gemini 兼容说明、真实模型提示和校验文案。
- `frontend/src/views/admin/__tests__/groupsMessagesDispatch.spec.ts`：平台默认值、映射预览、序列化和切换重置测试。

### Stable interfaces produced by this plan

```go
type MessagesDispatchResolution struct {
	PublicModel  string
	TargetModel  string
	MappingRule  string
	Mapped       bool
}

func ResolveGeminiAnthropicModel(group *Group, requestedModel string) (MessagesDispatchResolution, error)

func (s *GeminiMessagesCompatService) ForwardAnthropic(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	body []byte,
	publicModel string,
	dispatchModel string,
) (*ForwardResult, error)

func (s *GeminiMessagesCompatService) CountAnthropicTokens(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	publicModel string,
	dispatchModel string,
	claudeBody []byte,
) (*ForwardResult, error)
```

```ts
export function createDefaultMessagesDispatchFormState(
  platform?: string,
): MessagesDispatchFormState

export function messagesDispatchPreview(
  state: MessagesDispatchFormState,
): Array<{ public_model: string; target_model: string }>
```

---

### Task 1: 扩展分组级 Messages 映射到 Gemini

**Files:**
- Modify: `backend/internal/domain/openai_messages_dispatch.go`
- Modify: `backend/internal/service/openai_messages_dispatch.go`
- Modify: `backend/internal/service/openai_messages_dispatch_test.go`
- Modify: `backend/internal/service/admin_group.go`
- Modify: `backend/internal/service/admin_service_group_test.go`

**Interfaces:**
- Consumes: 现有 `Group.AllowMessagesDispatch`、`Group.MessagesDispatchModelConfig`、`Account.IsModelSupported(string)`、`AccountRepository.ListSchedulableByGroupID`。
- Produces: `MessagesDispatchResolution`、`ResolveGeminiAnthropicModel`、平台感知的 `normalizeMessagesDispatchModelConfig`、`validateGeminiMessagesDispatchConfig`。

- [ ] **Step 1: 写映射顺序和开关的失败测试**

在 `backend/internal/service/openai_messages_dispatch_test.go` 增加：

```go
func TestResolveGeminiAnthropicModel(t *testing.T) {
	group := &Group{
		Platform:              PlatformGemini,
		AllowMessagesDispatch: true,
		MessagesDispatchModelConfig: OpenAIMessagesDispatchModelConfig{
			OpusMappedModel:   "gemini-2.5-pro",
			SonnetMappedModel: "gemini-2.5-pro",
			HaikuMappedModel:  "gemini-2.5-flash",
			ExactModelMappings: map[string]string{
				"claude-sonnet-*":        "gemini-2.5-flash",
				"claude-sonnet-4-*":      "gemini-2.5-pro",
				"claude-sonnet-4-6":      "gemini-2.0-flash",
			},
		},
	}

	exact, err := ResolveGeminiAnthropicModel(group, "claude-sonnet-4-6")
	require.NoError(t, err)
	require.Equal(t, "gemini-2.0-flash", exact.TargetModel)
	require.Equal(t, "claude-sonnet-4-6", exact.MappingRule)

	longest, err := ResolveGeminiAnthropicModel(group, "claude-sonnet-4-5")
	require.NoError(t, err)
	require.Equal(t, "gemini-2.5-pro", longest.TargetModel)
	require.Equal(t, "claude-sonnet-4-*", longest.MappingRule)

	direct, err := ResolveGeminiAnthropicModel(group, "gemini-2.5-flash")
	require.NoError(t, err)
	require.False(t, direct.Mapped)
	require.Equal(t, "gemini-2.5-flash", direct.TargetModel)
}

func TestResolveGeminiAnthropicModel_DisabledAliasDoesNotAffectDirectGemini(t *testing.T) {
	group := &Group{Platform: PlatformGemini, AllowMessagesDispatch: false}

	_, err := ResolveGeminiAnthropicModel(group, "claude-sonnet-4-6")
	require.ErrorIs(t, err, ErrMessagesDispatchModelNotFound)

	direct, err := ResolveGeminiAnthropicModel(group, "gemini-2.5-pro")
	require.NoError(t, err)
	require.Equal(t, "gemini-2.5-pro", direct.TargetModel)
}
```

- [ ] **Step 2: 运行测试并确认失败**

Run:

```bash
cd backend && go test ./internal/service -run 'TestResolveGeminiAnthropicModel' -count=1
```

Expected: FAIL，提示 `ResolveGeminiAnthropicModel` 或 `MessagesDispatchResolution` 未定义。

- [ ] **Step 3: 实现平台感知归一化和解析器**

在 `backend/internal/service/openai_messages_dispatch.go` 增加并接入以下核心实现；OpenAI 默认值保持原样，Gemini 默认值使用设计文档中的三组目标：

```go
var ErrMessagesDispatchModelNotFound = errors.New("messages dispatch model not found")

type MessagesDispatchResolution struct {
	PublicModel string
	TargetModel string
	MappingRule string
	Mapped      bool
}

func normalizeMessagesDispatchMappedModel(platform, model string) string {
	model = strings.TrimSpace(model)
	if platform == PlatformOpenAI || platform == PlatformComposite {
		return strings.TrimSpace(NormalizeOpenAICompatRequestedModel(model))
	}
	return model
}

func normalizeMessagesDispatchModelConfig(platform string, cfg OpenAIMessagesDispatchModelConfig) OpenAIMessagesDispatchModelConfig {
	out := OpenAIMessagesDispatchModelConfig{
		OpusMappedModel: normalizeMessagesDispatchMappedModel(platform, cfg.OpusMappedModel),
		SonnetMappedModel: normalizeMessagesDispatchMappedModel(platform, cfg.SonnetMappedModel),
		HaikuMappedModel: normalizeMessagesDispatchMappedModel(platform, cfg.HaikuMappedModel),
	}
	if platform == PlatformGemini {
		defaults := defaultGeminiMessagesDispatchModelConfig()
		if out.OpusMappedModel == "" { out.OpusMappedModel = defaults.OpusMappedModel }
		if out.SonnetMappedModel == "" { out.SonnetMappedModel = defaults.SonnetMappedModel }
		if out.HaikuMappedModel == "" { out.HaikuMappedModel = defaults.HaikuMappedModel }
	}
	if len(cfg.ExactModelMappings) > 0 {
		out.ExactModelMappings = make(map[string]string, len(cfg.ExactModelMappings))
		for rule, target := range cfg.ExactModelMappings {
			rule = strings.TrimSpace(rule)
			target = normalizeMessagesDispatchMappedModel(platform, target)
			if rule != "" && target != "" { out.ExactModelMappings[rule] = target }
		}
		if len(out.ExactModelMappings) == 0 { out.ExactModelMappings = nil }
	}
	return out
}

func longestMessagesDispatchMapping(mappings map[string]string, requested string) (string, string) {
	if target := strings.TrimSpace(mappings[requested]); target != "" {
		return target, requested
	}
	bestPrefix, bestTarget, bestRule := "", "", ""
	for rule, target := range mappings {
		rule = strings.TrimSpace(rule)
		target = strings.TrimSpace(target)
		if !strings.HasSuffix(rule, "*") || target == "" {
			continue
		}
		prefix := strings.TrimSuffix(rule, "*")
		if strings.HasPrefix(requested, prefix) && len(prefix) > len(bestPrefix) {
			bestPrefix, bestTarget, bestRule = prefix, target, rule
		}
	}
	return bestTarget, bestRule
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
		resolution.TargetModel, resolution.MappingRule, resolution.Mapped = target, rule, target != requestedModel
		return resolution, nil
	}

	switch claudeMessagesDispatchFamily(requestedModel) {
	case "opus":
		resolution.TargetModel, resolution.MappingRule = cfg.OpusMappedModel, "claude-opus-*"
	case "sonnet":
		resolution.TargetModel, resolution.MappingRule = cfg.SonnetMappedModel, "claude-sonnet-*"
	case "haiku":
		resolution.TargetModel, resolution.MappingRule = cfg.HaikuMappedModel, "claude-haiku-*"
	default:
		return resolution, ErrMessagesDispatchModelNotFound
	}
	resolution.Mapped = resolution.TargetModel != requestedModel
	return resolution, nil
}
```

将 Gemini 默认配置固定为：

```go
func defaultGeminiMessagesDispatchModelConfig() OpenAIMessagesDispatchModelConfig {
	return OpenAIMessagesDispatchModelConfig{
		OpusMappedModel:   "gemini-2.5-pro",
		SonnetMappedModel: "gemini-2.5-pro",
		HaikuMappedModel:  "gemini-2.5-flash",
	}
}
```

`normalizeMessagesDispatchModelConfig(PlatformGemini, cfg)` 必须以该默认值为基底：用户未填写的 family 字段继承默认值，非空字段覆盖默认值，`ExactModelMappings` 保留用户配置。OpenAI/Composite 仍沿用当前 GPT 归一化和默认回退，不改变已有测试。

修改 `sanitizeGroupMessagesDispatchFields`：允许 `PlatformGemini` 保留 `AllowMessagesDispatch` 和配置；Gemini 仍清空 OpenAI 专属 `DefaultMappedModel`、`AllowLive`、OAuth/Privacy 限制不在此函数改变。

- [ ] **Step 4: 增加服务端配置校验的失败测试**

在 `backend/internal/service/admin_service_group_test.go` 增加一个覆盖成功、无账号、目标非 Gemini 和账号不支持目标的表驱动测试。核心断言如下：

```go
func TestValidateGeminiMessagesDispatchConfig(t *testing.T) {
	group := &Group{
		Platform: PlatformGemini,
		AllowMessagesDispatch: true,
		MessagesDispatchModelConfig: defaultGeminiMessagesDispatchModelConfig(),
	}
	supported := []Account{{
		Platform: PlatformGemini,
		Status: StatusActive,
		Schedulable: true,
		Credentials: map[string]any{"model_mapping": map[string]any{
			"gemini-2.5-pro": "gemini-2.5-pro",
			"gemini-2.5-flash": "gemini-2.5-flash",
		}},
	}}

	require.NoError(t, validateGeminiMessagesDispatchConfig(group, supported))
	require.ErrorContains(t, validateGeminiMessagesDispatchConfig(group, nil), "可调度账号")

	group.MessagesDispatchModelConfig.HaikuMappedModel = "claude-haiku-4-5"
	require.ErrorContains(t, validateGeminiMessagesDispatchConfig(group, supported), "Gemini 模型")
}

func TestValidateGeminiMessagesDispatchConfig_RejectsNormalizedWildcardConflict(t *testing.T) {
	err := validateMessagesDispatchMappingRules(map[string]string{
		"claude-sonnet-*":   "gemini-2.5-pro",
		" claude-sonnet-* ": "gemini-2.5-flash",
	})
	require.ErrorContains(t, err, "冲突")
}
```

- [ ] **Step 5: 实现创建/编辑校验并运行专项测试**

实现：

```go
func validateGeminiMessagesDispatchConfig(group *Group, accounts []Account) error {
	if group == nil || group.Platform != PlatformGemini || !group.AllowMessagesDispatch {
		return nil
	}
	cfg := normalizeMessagesDispatchModelConfig(PlatformGemini, group.MessagesDispatchModelConfig)
	targets := []string{cfg.OpusMappedModel, cfg.SonnetMappedModel, cfg.HaikuMappedModel}
	for _, target := range cfg.ExactModelMappings {
		targets = append(targets, strings.TrimSpace(target))
	}
	if len(accounts) == 0 {
		return infraerrors.New(http.StatusBadRequest, "GEMINI_MESSAGES_DISPATCH_NO_ACCOUNT", "请先为分组绑定至少一个可调度 Gemini 账号")
	}
	for _, target := range targets {
		if target == "" || !strings.HasPrefix(strings.ToLower(target), "gemini-") {
			return infraerrors.Newf(http.StatusBadRequest, "INVALID_GEMINI_MESSAGES_DISPATCH_TARGET", "目标 %q 必须是 Gemini 模型", target)
		}
		supported := false
		for i := range accounts {
			if accounts[i].Platform == PlatformGemini && accounts[i].IsModelSupported(target) {
				supported = true
				break
			}
		}
		if !supported {
			return infraerrors.Newf(http.StatusBadRequest, "UNSUPPORTED_GEMINI_MESSAGES_DISPATCH_TARGET", "分组内没有可调度账号支持模型 %q", target)
		}
	}
	return nil
}

func validateMessagesDispatchMappingRules(mappings map[string]string) error {
	seen := make(map[string]struct{}, len(mappings))
	for rawRule, rawTarget := range mappings {
		rule, target := strings.TrimSpace(rawRule), strings.TrimSpace(rawTarget)
		if rule == "" || target == "" {
			return infraerrors.New(http.StatusBadRequest, "INVALID_MESSAGES_DISPATCH_MAPPING", "模型映射的来源和目标不能为空")
		}
		if _, exists := seen[rule]; exists {
			return infraerrors.Newf(http.StatusBadRequest, "MESSAGES_DISPATCH_MAPPING_CONFLICT", "模型映射规则 %q 归一化后冲突", rule)
		}
		seen[rule] = struct{}{}
	}
	return nil
}
```

在 Create/Update 归一化之前先调用 `validateMessagesDispatchMappingRules`，因此空映射和 trim 后冲突不会被静默丢弃。目标必须以 `gemini-` 开头，所以映射回 `claude-*` 或映射到自身公开别名会作为循环/跨平台目标被拒绝。

在 Create 中，仅当 `allow_messages_dispatch=true` 时用 `copy_accounts_from_group_ids` 得到的账号执行校验；未复制账号时返回明确错误。Update 中在保存前调用 `ListSchedulableByGroupID(ctx, id)` 并执行校验。

Run:

```bash
cd backend && go test -tags=unit ./internal/service -run 'Test(ResolveGeminiAnthropicModel|ValidateGeminiMessagesDispatchConfig|AdminService_.*MessagesDispatch)' -count=1
```

Expected: PASS。

- [ ] **Step 6: 提交分组映射实现**

```bash
git add backend/internal/domain/openai_messages_dispatch.go backend/internal/service/openai_messages_dispatch.go backend/internal/service/openai_messages_dispatch_test.go backend/internal/service/admin_group.go backend/internal/service/admin_service_group_test.go
git commit -m "feat: add Gemini messages dispatch mapping"
```

---

### Task 2: 在 Gemini 选号前应用别名并分离公开/实际模型

**Files:**
- Modify: `backend/internal/handler/gateway_handler.go`
- Modify: `backend/internal/service/gemini_messages_compat_service.go`
- Modify: `backend/internal/service/gemini_messages_compat_service_test.go`

**Interfaces:**
- Consumes: Task 1 的 `ResolveGeminiAnthropicModel`。
- Produces: `GeminiMessagesCompatService.ForwardAnthropic(ctx, c, account, body, publicModel, dispatchModel)`；`ForwardResult.Model` 是公开模型，`ForwardResult.UpstreamModel` 是账号映射后的实际模型。

- [ ] **Step 1: 写 OAuth 和公开模型保真的失败测试**

在 `backend/internal/service/gemini_messages_compat_service_test.go` 用现有 fake upstream 模式增加：

```go
func TestGeminiMessagesCompatService_ForwardAnthropic_MapsOAuthAndKeepsPublicModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	httpStub := &geminiCompatHTTPUpstreamStub{response: &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{"Content-Type": []string{"text/event-stream"}},
		Body: io.NopCloser(strings.NewReader(
			"data: {\"response\":{\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"hello\"}]},\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":2,\"candidatesTokenCount\":1}}}\n\n"+
				"data: [DONE]\n\n",
		)),
	}}
	svc := &GeminiMessagesCompatService{
		tokenProvider: &GeminiTokenProvider{}, httpUpstream: httpStub, cfg: &config.Config{},
	}
	account := &Account{ID: 1, Platform: PlatformGemini, Type: AccountTypeOAuth, Concurrency: 1,
		Credentials: map[string]any{"access_token": "ya29.test", "project_id": "project-1"}}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	result, err := svc.ForwardAnthropic(
		context.Background(), c, account,
		[]byte(`{"model":"claude-sonnet-4-6","max_tokens":64,"messages":[{"role":"user","content":"hi"}]}`),
		"claude-sonnet-4-6", "gemini-2.5-pro",
	)

	require.NoError(t, err)
	sent, readErr := io.ReadAll(httpStub.lastReq.Body)
	require.NoError(t, readErr)
	require.Equal(t, "gemini-2.5-pro", gjson.GetBytes(sent, "model").String())
	require.Equal(t, "claude-sonnet-4-6", result.Model)
	require.Equal(t, "gemini-2.5-pro", result.UpstreamModel)
	require.Equal(t, "claude-sonnet-4-6", gjson.Get(recorder.Body.String(), "model").String())
}
```

- [ ] **Step 2: 运行服务测试并确认失败**

Run:

```bash
cd backend && go test ./internal/service -run 'TestGeminiMessagesCompatService_ForwardAnthropic' -count=1
```

Expected: FAIL，提示 `ForwardAnthropic` 未定义。

- [ ] **Step 3: 实现显式双模型转发接口**

在 `gemini_messages_compat_service.go` 把现有 `Forward` 的主体迁到以下签名；所有上游 URL/包装 body 使用 `account.GetMappedModel(dispatchModel)`，所有 Anthropic Message/SSE `model` 和 `ForwardResult.Model` 使用 `publicModel`：

```go
func (s *GeminiMessagesCompatService) ForwardAnthropic(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	body []byte,
	publicModel string,
	dispatchModel string,
) (*ForwardResult, error) {
	publicModel = strings.TrimSpace(publicModel)
	dispatchModel = strings.TrimSpace(dispatchModel)
	if publicModel == "" || dispatchModel == "" {
		return nil, s.writeClaudeError(c, http.StatusBadRequest, "invalid_request_error", "model is required")
	}

	mappedModel := account.GetMappedModel(dispatchModel)
	// 后续保留现有请求构造、重试、错误映射和响应转换；
	// convertGeminiToClaudeMessage/handleStreamingResponse 接收 publicModel。
	// 返回值固定如下两个模型字段。
	return &ForwardResult{
		RequestID: requestID,
		Usage: usage,
		Model: publicModel,
		UpstreamModel: mappedModel,
		Stream: req.Stream,
		Duration: time.Since(startTime),
		FirstTokenMs: firstTokenMs,
	}, nil
}
```

保留兼容包装，避免其他内部调用在同一提交中断裂：

```go
func (s *GeminiMessagesCompatService) Forward(ctx context.Context, c *gin.Context, account *Account, body []byte) (*ForwardResult, error) {
	model := strings.TrimSpace(gjson.GetBytes(body, "model").String())
	return s.ForwardAnthropic(ctx, c, account, body, model, model)
}
```

- [ ] **Step 4: 写“实际模型决定账号能力”的调度测试**

在 `backend/internal/service/gemini_messages_compat_service_test.go` 增加：

```go
func TestGeminiAliasResolutionFeedsActualModelIntoAccountSelection(t *testing.T) {
	group := &Group{Platform: PlatformGemini, AllowMessagesDispatch: true,
		MessagesDispatchModelConfig: defaultGeminiMessagesDispatchModelConfig()}
	resolution, err := ResolveGeminiAnthropicModel(group, "claude-sonnet-4-6")
	require.NoError(t, err)
	accounts := []Account{
		{ID: 1, Platform: PlatformGemini, Status: StatusActive, Schedulable: true,
			Credentials: map[string]any{"model_mapping": map[string]any{"claude-sonnet-4-6": "gemini-2.0-flash"}}},
		{ID: 2, Platform: PlatformGemini, Status: StatusActive, Schedulable: true,
			Credentials: map[string]any{"model_mapping": map[string]any{"gemini-2.5-pro": "gemini-2.5-pro"}}},
	}
	svc := &GeminiMessagesCompatService{}
	selected := svc.selectBestGeminiAccount(context.Background(), accounts, resolution.TargetModel, nil, PlatformGemini, false)
	require.NotNil(t, selected)
	require.Equal(t, int64(2), selected.ID)
}
```

- [ ] **Step 5: 在 Gemini 分支以实际模型调度**

在 `GatewayHandler.Messages` 完成 `ParseGatewayRequest` 后、调用 `ResolveChannelMappingAndRestrict` 之前解析双模型：

```go
publicModel := parsedReq.Model
dispatchModel := publicModel
if platform == service.PlatformGemini {
	resolution, resolveErr := service.ResolveGeminiAnthropicModel(apiKey.Group, publicModel)
	if resolveErr != nil {
		h.errorResponse(c, http.StatusNotFound, "not_found_error", "Requested model is not available in this Gemini group")
		return
	}
	dispatchModel = resolution.TargetModel
	service.SetOpsUpstreamModel(c, dispatchModel)
}
```

渠道限制调用对 Gemini 使用 `dispatchModel`，其他平台保持原模型：

```go
channelLookupModel := publicModel
if platform == service.PlatformGemini { channelLookupModel = dispatchModel }
channelMapping, _ := h.gatewayService.ResolveChannelMappingAndRestrict(c.Request.Context(), apiKey.GroupID, channelLookupModel)
```

Gemini 分支中的 `SelectAccountWithLoadAwareness`、`classifyNoAccountErrorFromGin`、模型限流 key 和账号能力判断统一改用 `dispatchModel`；安全审核、客户日志的 requested model、响应和 `RecordUsageInput.ChannelUsageFields.OriginalModel` 仍使用 `publicModel`。转发调用改成：

```go
result, err = h.geminiCompatService.ForwardAnthropic(
	requestCtx, c, account, body, publicModel, dispatchModel,
)
```

- [ ] **Step 6: 运行 handler/service 专项测试**

```bash
cd backend && go test ./internal/handler ./internal/service -run 'Test.*Gemini.*(Dispatch|ForwardAnthropic|PublicModel)' -count=1
```

Expected: PASS。

- [ ] **Step 7: 提交调度和转发修改**

```bash
git add backend/internal/handler/gateway_handler.go backend/internal/service/gemini_messages_compat_service.go backend/internal/service/gemini_messages_compat_service_test.go
git commit -m "feat: route Claude aliases through Gemini accounts"
```

---

### Task 3: 补齐 Anthropic 请求语义和 thought 隔离

**Files:**
- Modify: `backend/internal/service/gemini_messages_compat_service.go`
- Modify: `backend/internal/service/gemini_messages_compat_service_test.go`

**Interfaces:**
- Consumes: Task 2 的 `ForwardAnthropic`。
- Produces: `convertClaudeToolConfig(req, callableToolCount)`；未知内容块返回可直接映射为 `invalid_request_error` 的错误；非流式和 SSE 都过滤 `thought: true` part。

- [ ] **Step 1: 写请求转换失败测试**

增加表驱动测试：

```go
func TestConvertClaudeMessagesToGeminiGenerateContent_ToolChoice(t *testing.T) {
	tests := []struct {
		name string
		choice string
		wantMode string
		wantAllowed string
	}{
		{name: "auto", choice: `{"type":"auto"}`, wantMode: "AUTO"},
		{name: "any", choice: `{"type":"any"}`, wantMode: "ANY"},
		{name: "tool", choice: `{"type":"tool","name":"lookup"}`, wantMode: "ANY", wantAllowed: "lookup"},
		{name: "none", choice: `{"type":"none"}`, wantMode: "NONE"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := []byte(fmt.Sprintf(`{"model":"claude-sonnet-4-6","max_tokens":32,"messages":[{"role":"user","content":"hi"}],"tools":[{"name":"lookup","input_schema":{"type":"object"}}],"tool_choice":%s}`, tt.choice))
			got, err := convertClaudeMessagesToGeminiGenerateContent(body)
			require.NoError(t, err)
			require.Equal(t, tt.wantMode, gjson.GetBytes(got, "toolConfig.functionCallingConfig.mode").String())
			if tt.wantAllowed != "" {
				require.Equal(t, tt.wantAllowed, gjson.GetBytes(got, "toolConfig.functionCallingConfig.allowedFunctionNames.0").String())
			}
		})
	}
}

func TestConvertClaudeMessagesToGeminiGenerateContent_RejectsSemanticUnknownBlock(t *testing.T) {
	_, err := convertClaudeMessagesToGeminiGenerateContent([]byte(`{"messages":[{"role":"user","content":[{"type":"document","source":{"type":"base64","data":"AA=="}}]}]}`))
	require.ErrorContains(t, err, `unsupported content block type "document"`)
}

func TestConvertClaudeMessagesToGeminiGenerateContent_RejectsDisabledParallelTools(t *testing.T) {
	_, err := convertClaudeMessagesToGeminiGenerateContent([]byte(`{"messages":[{"role":"user","content":"hi"}],"tools":[{"name":"a"},{"name":"b"}],"tool_choice":{"type":"auto","disable_parallel_tool_use":true}}`))
	require.ErrorContains(t, err, "disable_parallel_tool_use")
}
```

- [ ] **Step 2: 运行转换测试并确认失败**

```bash
cd backend && go test ./internal/service -run 'TestConvertClaudeMessagesToGeminiGenerateContent_(ToolChoice|Rejects)' -count=1
```

Expected: FAIL；当前没有 `toolConfig`，未知 block 被转成文本。

- [ ] **Step 3: 实现 tool_choice 和严格内容块处理**

把 `convertClaudeToolsToGeminiTools` 改为同时返回可调用函数数量，再把以下结果写入 `out["toolConfig"]`：

```go
func convertClaudeToolConfig(req map[string]any, callableToolCount int) (map[string]any, error) {
	choice, ok := req["tool_choice"].(map[string]any)
	if !ok || choice == nil {
		return nil, nil
	}
	choiceType, _ := choice["type"].(string)
	if disabled, _ := choice["disable_parallel_tool_use"].(bool); disabled && callableToolCount > 1 {
		return nil, errors.New("disable_parallel_tool_use=true is not supported with multiple Gemini function tools")
	}
	config := map[string]any{}
	switch choiceType {
	case "", "auto":
		config["mode"] = "AUTO"
	case "any":
		config["mode"] = "ANY"
	case "tool":
		name, _ := choice["name"].(string)
		if strings.TrimSpace(name) == "" {
			return nil, errors.New("tool_choice.name is required when type is tool")
		}
		config["mode"] = "ANY"
		config["allowedFunctionNames"] = []string{name}
	case "none":
		config["mode"] = "NONE"
	default:
		return nil, fmt.Errorf("unsupported tool_choice type %q", choiceType)
	}
	return map[string]any{"functionCallingConfig": config}, nil
}
```

将内容块 default 分支替换为：

```go
case "thinking", "redacted_thinking":
	// 客户历史中的思考块不转成 Gemini 正文。
	continue
default:
	return nil, fmt.Errorf("unsupported content block type %q", bt)
```

`extractClaudeContentText` 对 `tool_result.content` 只接受字符串或 `text` 块；遇到其他类型返回错误，并把函数签名改成 `(string, error)`，调用处传播该错误。

- [ ] **Step 4: 写 thought 不泄漏的非流式与 SSE 失败测试**

```go
func TestConvertGeminiToClaudeMessage_FiltersThoughtParts(t *testing.T) {
	resp := map[string]any{"candidates": []any{map[string]any{"content": map[string]any{"parts": []any{
		map[string]any{"text": "hidden chain", "thought": true},
		map[string]any{"text": "visible answer"},
	}}}}}
	got, _ := convertGeminiToClaudeMessage(resp, "claude-sonnet-4-6", nil, false)
	encoded, err := json.Marshal(got)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "hidden chain")
	require.Contains(t, string(encoded), "visible answer")
}

func TestGeminiMessagesCompatService_StreamFiltersThoughtParts(t *testing.T) {
	stream := "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"secret\",\"thought\":true},{\"text\":\"answer\"}]}}]}\n\n"
	httpStub := &geminiCompatHTTPUpstreamStub{response: &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{"Content-Type": []string{"text/event-stream"}},
		Body: io.NopCloser(strings.NewReader(stream)),
	}}
	svc := &GeminiMessagesCompatService{httpUpstream: httpStub, cfg: &config.Config{}}
	account := &Account{ID: 1, Platform: PlatformGemini, Type: AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "test-key"}}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	_, err := svc.ForwardAnthropic(context.Background(), c, account,
		[]byte(`{"model":"claude-sonnet-4-6","stream":true,"max_tokens":32,"messages":[{"role":"user","content":"hi"}]}`),
		"claude-sonnet-4-6", "gemini-2.5-pro")
	require.NoError(t, err)
	require.NotContains(t, rec.Body.String(), "secret")
	require.Contains(t, rec.Body.String(), "answer")
}
```

- [ ] **Step 5: 在非流式和 SSE part 循环首行过滤 thought**

```go
if thought, _ := pm["thought"].(bool); thought {
	continue
}
```

SSE 的 `part` 循环使用相同判断；过滤发生在读取 `text` 和 `functionCall` 之前。保留 `thoughtSignature` 内部处理，不把签名写入 Anthropic content。

- [ ] **Step 6: 运行请求/响应专项测试**

```bash
cd backend && go test ./internal/service -run 'Test(ConvertClaudeMessagesToGeminiGenerateContent|ConvertGeminiToClaudeMessage|GeminiMessagesCompatService_Stream)' -count=1
```

Expected: PASS。

- [ ] **Step 7: 提交兼容转换修改**

```bash
git add backend/internal/service/gemini_messages_compat_service.go backend/internal/service/gemini_messages_compat_service_test.go
git commit -m "feat: harden Gemini Anthropic request conversion"
```

---

### Task 4: 实现 Gemini 专用 Anthropic count_tokens

**Files:**
- Create: `backend/internal/service/gemini_anthropic_count_tokens.go`
- Create: `backend/internal/service/gemini_anthropic_count_tokens_test.go`
- Modify: `backend/internal/service/gemini_messages_compat_service.go`
- Modify: `backend/internal/handler/gateway_handler.go`
- Modify: `backend/internal/server/routes/gateway.go`
- Modify: `backend/internal/server/routes/gateway_test.go`

**Interfaces:**
- Consumes: Task 1 的模型解析、Task 3 的 Anthropic→Gemini 请求转换、现有 `estimateGeminiCountTokens` 和 Gemini OAuth token provider。
- Produces: `CountAnthropicTokens`；内部 `countGeminiTokens(ctx, c, account, model, body) (total int, estimated bool, result *ForwardResult, err error)`；`GatewayHandler.GeminiCountTokens`。

- [ ] **Step 1: 写路由分流失败测试**

在 `backend/internal/server/routes/gateway.go` 将 count 路由选择抽成纯函数，并在 `gateway_test.go` 先写失败测试：

```go
func TestCountTokensGatewayKind(t *testing.T) {
	require.Equal(t, countTokensGatewayGemini, countTokensGatewayKind(service.PlatformGemini))
	require.Equal(t, countTokensGatewayOpenAI, countTokensGatewayKind(service.PlatformOpenAI))
	require.Equal(t, countTokensGatewayGrok, countTokensGatewayKind(service.PlatformGrok))
	require.Equal(t, countTokensGatewayAnthropic, countTokensGatewayKind(service.PlatformAnthropic))
}
```

- [ ] **Step 2: 写真实计数、估算头和图片门禁失败测试**

在新测试文件中增加三组测试：

```go
func newGeminiCountTokensTestContext(status int, responseBody string) (*GeminiMessagesCompatService, *Account, *gin.Context, *httptest.ResponseRecorder) {
	httpStub := &geminiCompatHTTPUpstreamStub{response: &http.Response{
		StatusCode: status,
		Header: http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(strings.NewReader(responseBody)),
	}}
	svc := &GeminiMessagesCompatService{
		tokenProvider: &GeminiTokenProvider{}, httpUpstream: httpStub, cfg: &config.Config{},
	}
	account := &Account{ID: 1, Platform: PlatformGemini, Type: AccountTypeOAuth,
		Credentials: map[string]any{"access_token": "ya29.test"}}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages/count_tokens", nil)
	return svc, account, c, rec
}

func TestCountAnthropicTokens_ConvertsGeminiTotalTokens(t *testing.T) {
	svc, account, c, rec := newGeminiCountTokensTestContext(http.StatusOK, `{"totalTokens":123}`)
	result, err := svc.CountAnthropicTokens(context.Background(), c, account,
		"claude-sonnet-4-6", "gemini-2.5-pro",
		[]byte(`{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"hello"}]}`))
	require.NoError(t, err)
	require.Equal(t, int64(123), gjson.Get(rec.Body.String(), "input_tokens").Int())
	require.Empty(t, rec.Header().Get("x-modelport-token-count-estimated"))
	require.Equal(t, "gemini-2.5-pro", result.UpstreamModel)
}

func TestCountAnthropicTokens_OAuthScopeFallbackIsMarked(t *testing.T) {
	svc, account, c, rec := newGeminiCountTokensTestContext(http.StatusForbidden, `{"error":{"status":"PERMISSION_DENIED","message":"insufficient authentication scopes"}}`)
	_, err := svc.CountAnthropicTokens(context.Background(), c, account,
		"claude-haiku-4-5", "gemini-2.5-flash",
		[]byte(`{"model":"claude-haiku-4-5","system":"rules","messages":[{"role":"user","content":"hello"}],"tools":[{"name":"lookup","input_schema":{"type":"object","properties":{"q":{"type":"string"}}}}]}`))
	require.NoError(t, err)
	require.Equal(t, "true", rec.Header().Get("x-modelport-token-count-estimated"))
	require.Positive(t, gjson.Get(rec.Body.String(), "input_tokens").Int())
}

func TestCountAnthropicTokens_ImageCannotUseLocalFallback(t *testing.T) {
	svc, account, c, rec := newGeminiCountTokensTestContext(http.StatusForbidden, `{"error":{"status":"PERMISSION_DENIED","message":"insufficient authentication scopes"}}`)
	_, err := svc.CountAnthropicTokens(context.Background(), c, account,
		"claude-sonnet-4-6", "gemini-2.5-pro",
		[]byte(`{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"AA=="}}]}]}`))
	require.Error(t, err)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Equal(t, "invalid_request_error", gjson.Get(rec.Body.String(), "error.type").String())
}
```

- [ ] **Step 3: 运行测试并确认失败**

```bash
cd backend && go test ./internal/server/routes ./internal/service -run 'Test(CountTokensGatewayKind|CountAnthropicTokens)' -count=1
```

Expected: FAIL，`countTokensGatewayKind` 和 Gemini 专用 service 尚不存在。

- [ ] **Step 4: 抽取 Gemini countTokens 共享内核**

在新文件实现：

```go
type geminiTokenCountResult struct {
	Total     int
	Estimated bool
	RequestID string
	Model     string
}

func anthropicRequestContainsBase64Image(body []byte) bool {
	var req struct {
		Messages []struct { Content any `json:"content"` } `json:"messages"`
	}
	if json.Unmarshal(body, &req) != nil { return false }
	for _, message := range req.Messages {
		blocks, ok := message.Content.([]any)
		if !ok { continue }
		for _, raw := range blocks {
			block, ok := raw.(map[string]any)
			if !ok || block["type"] != "image" { continue }
			source, _ := block["source"].(map[string]any)
			if source["type"] == "base64" { return true }
		}
	}
	return false
}

func (s *GeminiMessagesCompatService) CountAnthropicTokens(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	publicModel string,
	dispatchModel string,
	claudeBody []byte,
) (*ForwardResult, error) {
	geminiBody, err := convertClaudeMessagesToGeminiGenerateContent(claudeBody)
	if err != nil {
		return nil, s.writeClaudeError(c, http.StatusBadRequest, "invalid_request_error", err.Error())
	}
	count, err := s.countGeminiTokens(ctx, c, account, dispatchModel, geminiBody)
	if err != nil {
		return nil, err
	}
	if count.Estimated && anthropicRequestContainsBase64Image(claudeBody) {
		return nil, s.writeClaudeError(c, http.StatusBadRequest, "invalid_request_error", "Image token count cannot be estimated reliably for this Gemini account")
	}
	if count.Estimated {
		c.Header("x-modelport-token-count-estimated", "true")
	}
	c.JSON(http.StatusOK, gin.H{"input_tokens": count.Total})
	return &ForwardResult{
		RequestID: count.RequestID,
		Model: publicModel,
		UpstreamModel: count.Model,
		Stream: false,
	}, nil
}
```

把 `ForwardNative` 的 `action == "countTokens"` 分支改为调用同一个 `countGeminiTokens`，然后写 Google `{totalTokens}`；其他 action 保持现有转发逻辑。`countGeminiTokens` 只在 OAuth insufficient-scope、网络最终失败或既有可回退错误上估算；真实 4xx 参数错误继续返回错误，不得全部吞成估算成功。

- [ ] **Step 5: 实现 GeminiCountTokens handler 和路由**

在 `GatewayHandler` 增加 `GeminiCountTokens`：复用现有 CountTokens 的鉴权、body 解析、billing eligibility 和 session hash；解析 Task 1 的 actual model 后调用 `SelectAccountForModel(..., resolution.TargetModel)`，不申请并发槽、不提交 usage task，最后调用 `CountAnthropicTokens`。

路由选择实现为：

```go
const (
	countTokensGatewayAnthropic = "anthropic"
	countTokensGatewayOpenAI = "openai"
	countTokensGatewayGrok = "grok"
	countTokensGatewayGemini = "gemini"
)

func countTokensGatewayKind(platform string) string {
	switch platform {
	case service.PlatformOpenAI, service.PlatformKimi, service.PlatformZhipu, service.PlatformDeepseek:
		return countTokensGatewayOpenAI
	case service.PlatformGrok:
		return countTokensGatewayGrok
	case service.PlatformGemini:
		return countTokensGatewayGemini
	default:
		return countTokensGatewayAnthropic
	}
}
```

`countTokensHandler` switch 该返回值，Gemini case 调用 `h.Gateway.GeminiCountTokens(c)`。

- [ ] **Step 6: 运行 count_tokens 专项测试**

```bash
cd backend && go test ./internal/server/routes ./internal/handler ./internal/service -run 'Test.*(GeminiCountTokens|CountAnthropicTokens)' -count=1
```

Expected: PASS。

- [ ] **Step 7: 提交计数实现**

```bash
git add backend/internal/service/gemini_anthropic_count_tokens.go backend/internal/service/gemini_anthropic_count_tokens_test.go backend/internal/service/gemini_messages_compat_service.go backend/internal/handler/gateway_handler.go backend/internal/server/routes/gateway.go backend/internal/server/routes/gateway_test.go
git commit -m "feat: add Gemini Anthropic token counting"
```

---

### Task 5: 统一 Messages 鉴权、调度和流式错误格式

**Files:**
- Modify: `backend/internal/server/middleware/middleware.go`
- Modify: `backend/internal/server/middleware/api_key_auth_test.go`
- Modify: `backend/internal/handler/gateway_handler.go`
- Modify: `backend/internal/handler/gateway_handler_stream_failover_test.go`
- Modify: `backend/internal/service/gemini_messages_compat_service.go`
- Modify: `backend/internal/service/gemini_error_policy_test.go`

**Interfaces:**
- Consumes: 现有 `AbortWithError`、`GatewayHandler.errorResponse`、`handleStreamingAwareError`、`writeClaudeError`。
- Produces: `isAnthropicMessagesPath(path string) bool`、`anthropicErrorTypeForCode(code string) string`；Messages 两个入口的所有早退都采用 Anthropic JSON/SSE。

- [ ] **Step 1: 写鉴权格式失败测试**

在 `api_key_auth_test.go` 增加：

```go
func TestAPIKeyAuth_AnthropicMessagesPathsUseAnthropicError(t *testing.T) {
	cfg := &config.Config{RunMode: config.RunModeSimple}
	repo := &stubApiKeyRepo{getByKey: func(context.Context, string) (*service.APIKey, error) {
		return nil, service.ErrAPIKeyNotFound
	}}
	apiKeyService := service.NewAPIKeyService(repo, nil, nil, nil, nil, nil, cfg)
	for _, path := range []string{"/v1/messages", "/v1/messages/count_tokens", "/messages", "/messages/count_tokens"} {
		t.Run(path, func(t *testing.T) {
			router := gin.New()
			router.Use(gin.HandlerFunc(NewAPIKeyAuthMiddleware(apiKeyService, nil, cfg)))
			router.POST(path, func(c *gin.Context) { c.Status(http.StatusNoContent) })
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, path, nil)
			router.ServeHTTP(rec, req)
			require.Equal(t, http.StatusUnauthorized, rec.Code)
			require.Equal(t, "error", gjson.Get(rec.Body.String(), "type").String())
			require.Equal(t, "authentication_error", gjson.Get(rec.Body.String(), "error.type").String())
			require.Empty(t, gjson.Get(rec.Body.String(), "code").String())
		})
	}
}
```

- [ ] **Step 2: 运行鉴权测试并确认失败**

```bash
cd backend && go test -tags=unit ./internal/server/middleware -run 'TestAPIKeyAuth_AnthropicMessagesPathsUseAnthropicError' -count=1
```

Expected: FAIL，当前 body 是 `{"code":"API_KEY_REQUIRED",...}`。

- [ ] **Step 3: 让 AbortWithError 按 Messages 路径输出协议格式**

```go
func isAnthropicMessagesPath(path string) bool {
	switch strings.TrimRight(path, "/") {
	case "/v1/messages", "/v1/messages/count_tokens", "/messages", "/messages/count_tokens":
		return true
	default:
		return false
	}
}

func anthropicErrorTypeForCode(code string) string {
	switch code {
	case "API_KEY_REQUIRED", "INVALID_API_KEY", "API_KEY_DISABLED", "USER_NOT_FOUND", "USER_INACTIVE":
		return "authentication_error"
	case "ACCESS_DENIED", "API_KEY_EXPIRED", "SUBSCRIPTION_NOT_FOUND":
		return "permission_error"
	case "USAGE_LIMIT_EXCEEDED", "INVALID_AUTH_RATE_LIMITED":
		return "rate_limit_error"
	default:
		return "api_error"
	}
}

func AbortWithError(c *gin.Context, statusCode int, code, message string) {
	if c != nil && c.Request != nil && isAnthropicMessagesPath(c.Request.URL.Path) {
		c.JSON(statusCode, gin.H{"type": "error", "error": gin.H{
			"type": anthropicErrorTypeForCode(code), "message": message,
		}})
		c.Abort()
		return
	}
	c.JSON(statusCode, NewErrorResponse(code, message))
	c.Abort()
}
```

这样只改变四个 Messages 路径，不改变后台 API、OpenAI、Gemini 原生入口。

- [ ] **Step 4: 写流开始后错误事件失败测试**

在 `gateway_handler_stream_failover_test.go` 增加断言：Gemini SSE 已发送 `message_start` 后 upstream read error，只追加一个 Anthropic `event: error`，不写 JSON HTTP body、不进入第二账号。

```go
require.Contains(t, rec.Body.String(), "event: message_start")

require.Contains(t, rec.Body.String(), "event: error")

require.Contains(t, rec.Body.String(), `"type":"api_error"`)

require.Equal(t, 1, upstreamCalls.Load())
```

- [ ] **Step 5: 统一 Gemini 错误类型并保护敏感信息**

将 Gemini→Claude 错误映射限制为：`authentication_error`、`permission_error`、`invalid_request_error`、`not_found_error`、`rate_limit_error`、`api_error`、`overloaded_error`。流式开始后由 `handleStreamingAwareError` 写：

```go
writeSSE(c.Writer, "error", gin.H{
	"type": "error",
	"error": gin.H{"type": errType, "message": sanitizeUpstreamErrorMessage(message)},
})
```

错误消息进入响应前继续调用 `sanitizeUpstreamErrorMessage`；测试 body 中放入 `project_id`、代理 URL 和 account name，断言这些值不出现在客户响应。

- [ ] **Step 6: 运行错误专项测试**

```bash
cd backend && go test -tags=unit ./internal/server/middleware ./internal/handler ./internal/service -run 'Test.*(AnthropicMessagesPaths|Stream.*Error|Gemini.*Error)' -count=1
```

Expected: PASS。

- [ ] **Step 7: 提交错误格式修改**

```bash
git add backend/internal/server/middleware/middleware.go backend/internal/server/middleware/api_key_auth_test.go backend/internal/handler/gateway_handler.go backend/internal/handler/gateway_handler_stream_failover_test.go backend/internal/service/gemini_messages_compat_service.go backend/internal/service/gemini_error_policy_test.go
git commit -m "fix: normalize Anthropic gateway errors"
```

---

### Task 6: 暴露兼容别名并改造 Gemini 分组管理界面

**Files:**
- Modify: `backend/internal/handler/gateway_handler.go`
- Modify: `backend/internal/handler/gateway_models_test.go`
- Modify: `frontend/src/types/index.ts`
- Modify: `frontend/src/views/admin/groupsMessagesDispatch.ts`
- Modify: `frontend/src/views/admin/__tests__/groupsMessagesDispatch.spec.ts`
- Modify: `frontend/src/views/admin/GroupsView.vue`
- Modify: `frontend/src/i18n/locales/zh/admin/overview.ts`
- Modify: `frontend/src/i18n/locales/en/admin/overview.ts`

**Interfaces:**
- Consumes: Task 1 的配置结构和默认映射。
- Produces: Gemini `/v1/models` 中的公开兼容别名；按平台生成的表单默认值和只读映射预览。

- [ ] **Step 1: 写模型列表失败测试**

在 `gateway_models_test.go` 增加：

```go
func TestGatewayModels_GeminiDispatchIncludesPublicClaudeAliases(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const groupID int64 = 77
	group := &service.Group{
		ID: groupID, Platform: service.PlatformGemini, AllowMessagesDispatch: true,
		MessagesDispatchModelConfig: service.OpenAIMessagesDispatchModelConfig{
			OpusMappedModel: "gemini-2.5-pro", SonnetMappedModel: "gemini-2.5-pro", HaikuMappedModel: "gemini-2.5-flash",
			ExactModelMappings: map[string]string{"claude-sonnet-4-6": "gemini-2.5-pro"},
		},
	}
	h := newGatewayModelsHandlerForTest(&gatewayModelsAccountRepoStub{byGroup: map[int64][]service.Account{
		groupID: {{ID: 1, Platform: service.PlatformGemini, Status: service.StatusActive, Schedulable: true,
			Credentials: map[string]any{"model_mapping": map[string]any{
				"gemini-2.5-pro": "gemini-2.5-pro", "gemini-2.5-flash": "gemini-2.5-flash",
			}}}},
	}})
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{Group: group})
	h.Models(c)
	require.Equal(t, http.StatusOK, rec.Code)
	var response gatewayModelsResponseForTest
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	ids := make([]string, 0, len(response.Data))
	for _, item := range response.Data { ids = append(ids, item.ID) }
	require.Contains(t, ids, "claude-opus-4-6")
	require.Contains(t, ids, "claude-sonnet-4-6")
	require.Contains(t, ids, "claude-haiku-4-5")
	require.Contains(t, ids, "gemini-2.5-pro")
}
```

兼容关闭的对称测试断言 Claude 别名不出现。

- [ ] **Step 2: 实现公开模型列表合并**

在 handler 增加：

```go
func geminiMessagesDispatchPublicModels(group *service.Group, availableModels []string) []string {
	if group == nil || group.Platform != service.PlatformGemini || !group.AllowMessagesDispatch {
		return nil
	}
	available := make(map[string]struct{}, len(availableModels))
	for _, model := range availableModels { available[strings.TrimSpace(model)] = struct{}{} }
	models := make([]string, 0, 3+len(group.MessagesDispatchModelConfig.ExactModelMappings))
	for _, publicModel := range []string{"claude-opus-4-6", "claude-sonnet-4-6", "claude-haiku-4-5"} {
		resolution, err := service.ResolveGeminiAnthropicModel(group, publicModel)
		if err == nil {
			if _, ok := available[resolution.TargetModel]; ok { models = append(models, publicModel) }
		}
	}
	for requested := range group.MessagesDispatchModelConfig.ExactModelMappings {
		requested = strings.TrimSpace(requested)
		if !strings.HasSuffix(requested, "*") {
			resolution, err := service.ResolveGeminiAnthropicModel(group, requested)
			if err == nil {
				if _, ok := available[resolution.TargetModel]; ok { models = append(models, requested) }
			}
		}
	}
	return mergeModelIDs(models, nil)
}
```

Gemini `Models` 与 `codexModelIDsForGroup` 在拿到实际模型后调用 `geminiMessagesDispatchPublicModels(group, availableModels)` 并合并；目标模型当前不在实际可用列表时不展示其公开别名。自定义 models list 仍在合并后执行过滤，保证管理员可继续限制展示范围。

- [ ] **Step 3: 写前端平台默认值和预览失败测试**

修改 `groupsMessagesDispatch.spec.ts`：

```ts
it('supports Gemini with Gemini defaults', () => {
  expect(supportsMessagesDispatchPlatform('gemini')).toBe(true)
  expect(createDefaultMessagesDispatchFormState('gemini')).toMatchObject({
    allow_messages_dispatch: false,
    opus_mapped_model: 'gemini-2.5-pro',
    sonnet_mapped_model: 'gemini-2.5-pro',
    haiku_mapped_model: 'gemini-2.5-flash',
  })
})

it('builds a public-to-upstream preview', () => {
  expect(messagesDispatchPreview({
    ...createDefaultMessagesDispatchFormState('gemini'),
    allow_messages_dispatch: true,
    exact_model_mappings: [{ claude_model: 'claude-sonnet-4-6', target_model: 'gemini-2.0-flash' }],
  })).toEqual([
    { public_model: 'claude-sonnet-4-6', target_model: 'gemini-2.0-flash' },
    { public_model: 'claude-opus-*', target_model: 'gemini-2.5-pro' },
    { public_model: 'claude-sonnet-*', target_model: 'gemini-2.5-pro' },
    { public_model: 'claude-haiku-*', target_model: 'gemini-2.5-flash' },
  ])
})
```

- [ ] **Step 4: 运行前端测试并确认失败**

```bash
pnpm --dir frontend exec vitest run src/views/admin/__tests__/groupsMessagesDispatch.spec.ts
```

Expected: FAIL，Gemini 尚不受支持且默认值仍是 GPT。

- [ ] **Step 5: 实现平台感知表单 helper**

```ts
export function supportsMessagesDispatchPlatform(platform: string): boolean {
  return platform === 'openai' || platform === 'gemini' || platform === 'composite'
}

export function createDefaultMessagesDispatchFormState(platform = 'openai'): MessagesDispatchFormState {
  const gemini = platform === 'gemini'
  return {
    allow_messages_dispatch: false,
    opus_mapped_model: gemini ? 'gemini-2.5-pro' : 'gpt-5.4',
    sonnet_mapped_model: gemini ? 'gemini-2.5-pro' : 'gpt-5.3-codex',
    haiku_mapped_model: gemini ? 'gemini-2.5-flash' : 'gpt-5.4-mini',
    exact_model_mappings: [],
  }
}

export function messagesDispatchPreview(state: MessagesDispatchFormState) {
  const exact = state.exact_model_mappings
    .filter(row => row.claude_model.trim() && row.target_model.trim())
    .map(row => ({ public_model: row.claude_model.trim(), target_model: row.target_model.trim() }))
  return [
    ...exact,
    { public_model: 'claude-opus-*', target_model: state.opus_mapped_model.trim() },
    { public_model: 'claude-sonnet-*', target_model: state.sonnet_mapped_model.trim() },
    { public_model: 'claude-haiku-*', target_model: state.haiku_mapped_model.trim() },
  ]
}
```

`messagesDispatchConfigToFormState` 和 `resetMessagesDispatchFormState` 增加 `platform` 参数，调用方始终传当前分组平台，防止切换后残留 GPT/Gemini 默认值。

- [ ] **Step 6: 改造 GroupsView 的 Gemini 编辑卡片**

创建页：Gemini 只显示关闭状态和“请先创建分组、绑定可调度账号，再编辑开启”的说明，不允许创建时直接开启。开关使用：

```vue
<button
  type="button"
  :disabled="createForm.platform === 'gemini'"
  @click="createForm.platform !== 'gemini' && (createForm.allow_messages_dispatch = !createForm.allow_messages_dispatch)"
  :class="[
    createForm.allow_messages_dispatch ? 'bg-primary-500' : 'bg-gray-300 dark:bg-dark-600',
    createForm.platform === 'gemini' ? 'cursor-not-allowed opacity-50' : '',
  ]"
>
```

平台选择变化时执行：

```ts
resetMessagesDispatchFormState(createForm, createForm.platform)
if (createForm.platform === 'gemini') createForm.allow_messages_dispatch = false
```

编辑页：允许开启，三个目标输入使用 Gemini placeholder；映射编辑区条件改为 `editForm.platform === 'openai' || editForm.platform === 'gemini'`。卡片底部用紧凑表格遍历 `messagesDispatchPreview(editForm)`，列为“客户模型”“实际 Gemini 模型”。

提交条件改为：

```ts
messages_dispatch_model_config:
  editForm.platform === 'openai' || editForm.platform === 'gemini'
    ? messagesDispatchFormStateToConfig(editForm)
    : undefined,
```

Gemini 卡片必须显示固定警示：`实际调用 Google Gemini，不是 Anthropic Claude；费用按右侧实际模型计算。`

- [ ] **Step 7: 更新中英文文案并运行前端验收**

中文键值至少包含：

```ts
geminiMessages: {
  title: 'Anthropic / Claude Code 兼容',
  allowDispatch: '启用 Claude 模型别名',
  allowDispatchHint: '开启后，Claude Code 的 claude-* 请求会在选号前映射到下方 Gemini 模型。',
  actualProviderWarning: '实际调用 Google Gemini，不是 Anthropic Claude；费用按实际 Gemini 模型计算。',
  createDisabledHint: '请先创建分组并绑定可调度 Gemini 账号，再进入编辑页开启。',
  publicModel: '客户模型',
  upstreamModel: '实际 Gemini 模型',
}
```

Run:

```bash
pnpm --dir frontend exec vitest run src/views/admin/__tests__/groupsMessagesDispatch.spec.ts
pnpm --dir frontend run typecheck
pnpm --dir frontend run lint:check
```

Expected: 全部 PASS。

- [ ] **Step 8: 提交模型列表和管理界面**

```bash
git add backend/internal/handler/gateway_handler.go backend/internal/handler/gateway_models_test.go frontend/src/types/index.ts frontend/src/views/admin/groupsMessagesDispatch.ts frontend/src/views/admin/__tests__/groupsMessagesDispatch.spec.ts frontend/src/views/admin/GroupsView.vue frontend/src/i18n/locales/zh/admin/overview.ts frontend/src/i18n/locales/en/admin/overview.ts
git commit -m "feat: manage Gemini Claude compatibility aliases"
```

---

### Task 7: 锁定 usage、缓存 token 和实际模型计费

**Files:**
- Modify: `backend/internal/service/gateway_usage_billing.go`
- Modify: `backend/internal/service/gateway_record_usage_test.go`
- Modify: `backend/internal/service/gemini_messages_compat_service_test.go`
- Modify: `backend/internal/handler/gateway_handler_usage_test.go`

**Interfaces:**
- Consumes: Task 2 的 `ForwardResult{Model: publicModel, UpstreamModel: actualModel}` 和现有 `RecordUsageInput`。
- Produces: usage log `model=publicModel`、`upstream_model=actualModel`；计费查价模型固定为 actual model；缓存读取与普通输入互斥。

- [ ] **Step 1: 写 usage/billing 失败测试**

在 `gateway_record_usage_test.go` 使用文件中已有的 `openAIRecordUsageLogRepoStub` 增加：

```go
func TestRecordUsage_GeminiAliasBillsActualModelAndRecordsBoth(t *testing.T) {
	usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
	svc := newGatewayRecordUsageServiceForTest(usageRepo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{})
	err := svc.RecordUsage(context.Background(), &RecordUsageInput{
		Result: &ForwardResult{
			RequestID: "gemini_alias_usage", Model: "claude-sonnet-4-6", UpstreamModel: "gemini-2.5-pro",
			Usage: ClaudeUsage{InputTokens: 800, CacheReadInputTokens: 200, OutputTokens: 100},
		},
		APIKey: &APIKey{ID: 501, Quota: 100}, User: &User{ID: 601},
		Account: &Account{ID: 701, Platform: PlatformGemini},
	})
	require.NoError(t, err)
	require.NotNil(t, usageRepo.lastLog)
	require.Equal(t, "claude-sonnet-4-6", usageRepo.lastLog.Model)
	require.Equal(t, "claude-sonnet-4-6", usageRepo.lastLog.RequestedModel)
	require.NotNil(t, usageRepo.lastLog.UpstreamModel)
	require.Equal(t, "gemini-2.5-pro", *usageRepo.lastLog.UpstreamModel)
	require.Equal(t, "gemini-2.5-pro", forwardResultBillingModel("claude-sonnet-4-6", "gemini-2.5-pro"))
}
```

在 `gemini_messages_compat_service_test.go` 锁定 usage 提取：

```go
func TestExtractGeminiUsage_CacheReadIsExcludedFromOrdinaryInput(t *testing.T) {
	usage := extractGeminiUsage([]byte(`{"usageMetadata":{"promptTokenCount":1000,"cachedContentTokenCount":250,"candidatesTokenCount":100,"thoughtsTokenCount":20}}`))
	require.Equal(t, 750, usage.InputTokens)
	require.Equal(t, 250, usage.CacheReadInputTokens)
	require.Equal(t, 120, usage.OutputTokens)
}

func TestConvertGeminiToClaudeMessage_ExposesRealCacheReadUsage(t *testing.T) {
	raw := []byte(`{"usageMetadata":{"promptTokenCount":1000,"cachedContentTokenCount":250,"candidatesTokenCount":100}}`)
	resp, _ := convertGeminiToClaudeMessage(map[string]any{}, "claude-sonnet-4-6", raw, false)
	encoded, err := json.Marshal(resp)
	require.NoError(t, err)
	require.Equal(t, int64(750), gjson.GetBytes(encoded, "usage.input_tokens").Int())
	require.Equal(t, int64(250), gjson.GetBytes(encoded, "usage.cache_read_input_tokens").Int())
}
```

- [ ] **Step 2: 运行测试并确认实际模型计费断言**

```bash
cd backend && go test -tags=unit ./internal/service ./internal/handler -run 'Test(RecordUsage_GeminiAlias|ExtractGeminiUsage_CacheRead|GatewayHandler.*Gemini.*Usage)' -count=1
```

Expected: 如果现有 `forwardResultBillingModel` 已正确选择 upstream，第二个测试先 PASS，而新的端到端 usage 测试暴露 public/upstream 丢失位置；不得为了制造红灯改坏已有正确逻辑。

- [ ] **Step 3: 最小修正计费模型和 usage 字段传递**

保持或明确以下不变式：

```go
func forwardResultBillingModel(publicModel, upstreamModel string) string {
	if model := strings.TrimSpace(upstreamModel); model != "" {
		return model
	}
	return strings.TrimSpace(publicModel)
}
```

在 `GatewayHandler.Messages` 的 Gemini usage task 中把 `ChannelUsageFields.OriginalModel` 设为公开模型，把 `result.UpstreamModel` 传给 mapping chain；不得把 `dispatchModel` 覆盖到 `result.Model`。`count_tokens` 不调用 `RecordUsage`，对应 handler 测试断言 usage worker 调用次数为 0。

非流式响应仅在 `usage.CacheReadInputTokens > 0` 时增加：

```go
usageObject["cache_read_input_tokens"] = usage.CacheReadInputTokens
```

SSE 最终 `message_delta.usage` 使用相同条件增加该字段；没有真实 `cachedContentTokenCount` 时不输出，也不虚构 `cache_creation_input_tokens`。

- [ ] **Step 4: 增加重复扣费回归测试**

模拟首次账号 429、第二账号成功，断言只提交一次 usage task、余额扣减一次；模拟流开始后 read error，若上游已返回 usage，只按现有 partial-usage 规则提交一次，绝不同时提交失败账号和成功账号两笔。

核心断言：

```go
require.Equal(t, int64(1), usageRecorder.calls.Load())
require.Equal(t, int64(1), balanceRecorder.debits.Load())
require.Equal(t, "gemini-2.5-pro", usageRecorder.last.UpstreamModel)
```

- [ ] **Step 5: 运行专项和全量 service/handler 测试**

```bash
cd backend && go test -tags=unit ./internal/service ./internal/handler -count=1
```

Expected: PASS。

- [ ] **Step 6: 提交 usage 和计费门禁**

```bash
git add backend/internal/service/gateway_usage_billing.go backend/internal/service/gateway_record_usage_test.go backend/internal/service/gemini_messages_compat_service_test.go backend/internal/handler/gateway_handler_usage_test.go
git commit -m "test: lock Gemini alias billing integrity"
```

---

### Task 8: 集成回归、接入文档和本地可回滚验收

**Files:**
- Create: `docs/gemini-anthropic-compat.md`
- Create: `backend/internal/service/gemini_anthropic_compat_integration_test.go`
- Modify: `README.md`

**Interfaces:**
- Consumes: Tasks 1–7 全部接口。
- Produces: 本地可重复的协议验收、客户侧配置示例、开关/回滚说明；不创建生产 Key、不发生产请求。

- [ ] **Step 1: 写集成测试**

集成测试使用可重复响应的 HTTP stub 和静态 token cache，不读取生产凭据，覆盖 API Key/OAuth/Service Account 三种 Account Type。先在测试文件加入以下完整测试 doubles：

```go
package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type queuedGeminiResponse struct {
	status int
	header http.Header
	body   string
}

type queuedGeminiHTTPStub struct {
	responses []queuedGeminiResponse
	requests  []*http.Request
}

func (s *queuedGeminiHTTPStub) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	s.requests = append(s.requests, req)
	if len(s.responses) == 0 {
		return nil, errors.New("unexpected upstream request")
	}
	next := s.responses[0]
	s.responses = s.responses[1:]
	return &http.Response{
		StatusCode: next.status,
		Header: next.header.Clone(),
		Body: io.NopCloser(strings.NewReader(next.body)),
	}, nil
}

func (s *queuedGeminiHTTPStub) DoWithTLS(req *http.Request, proxyURL string, accountID int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return s.Do(req, proxyURL, accountID, concurrency)
}

type staticGeminiTokenCache struct{ token string }

func (s staticGeminiTokenCache) GetAccessToken(context.Context, string) (string, error) { return s.token, nil }
func (s staticGeminiTokenCache) SetAccessToken(context.Context, string, string, time.Duration) error { return nil }
func (s staticGeminiTokenCache) DeleteAccessToken(context.Context, string) error { return nil }
func (s staticGeminiTokenCache) AcquireRefreshLock(context.Context, string, time.Duration) (bool, error) { return true, nil }
func (s staticGeminiTokenCache) ReleaseRefreshLock(context.Context, string) error { return nil }

func geminiAnthropicIntegrationAccount(accountType string) *Account {
	credentials := map[string]any{"api_key": "test-api-key", "access_token": "ya29.test"}
	if accountType == AccountTypeServiceAccount {
		credentials = map[string]any{"service_account_json": map[string]any{
			"project_id": "vertex-project", "private_key_id": "kid-1",
			"private_key": "-----BEGIN PRIVATE KEY-----\ntest-only\n-----END PRIVATE KEY-----\n",
			"client_email": "fixture@vertex-project.iam.gserviceaccount.com",
		}}
	}
	return &Account{ID: 10, Platform: PlatformGemini, Type: accountType, Concurrency: 1, Credentials: credentials}
}

func TestGeminiAnthropicCompatibility(t *testing.T) {
	for _, accountType := range []string{AccountTypeAPIKey, AccountTypeOAuth, AccountTypeServiceAccount} {
		t.Run(accountType, func(t *testing.T) {
			stub := &queuedGeminiHTTPStub{responses: []queuedGeminiResponse{
				{status: http.StatusOK, header: http.Header{"Content-Type": []string{"application/json"}}, body: `{"candidates":[{"content":{"parts":[{"text":"pong"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":3,"candidatesTokenCount":1}}`},
				{status: http.StatusOK, header: http.Header{"Content-Type": []string{"text/event-stream"}}, body: "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"pong\"}]},\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":3,\"candidatesTokenCount\":1}}\n\n"},
				{status: http.StatusOK, header: http.Header{"Content-Type": []string{"application/json"}}, body: `{"totalTokens":17}`},
			}}
			tokenProvider := NewGeminiTokenProvider(nil, staticGeminiTokenCache{token: "static-token"}, nil)
			svc := &GeminiMessagesCompatService{httpUpstream: stub, tokenProvider: tokenProvider, cfg: &config.Config{}}
			account := geminiAnthropicIntegrationAccount(accountType)

			messageRec := httptest.NewRecorder()
			messageContext, _ := gin.CreateTestContext(messageRec)
			messageContext.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
			messageResult, err := svc.ForwardAnthropic(context.Background(), messageContext, account,
				[]byte(`{"model":"claude-sonnet-4-6","max_tokens":32,"messages":[{"role":"user","content":"ping"}]}`),
				"claude-sonnet-4-6", "gemini-2.5-pro")
			require.NoError(t, err)
			require.Equal(t, "claude-sonnet-4-6", gjson.Get(messageRec.Body.String(), "model").String())
			require.Equal(t, "gemini-2.5-pro", messageResult.UpstreamModel)

			streamRec := httptest.NewRecorder()
			streamContext, _ := gin.CreateTestContext(streamRec)
			streamContext.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
			_, err = svc.ForwardAnthropic(context.Background(), streamContext, account,
				[]byte(`{"model":"claude-haiku-4-5","stream":true,"max_tokens":32,"messages":[{"role":"user","content":"ping"}]}`),
				"claude-haiku-4-5", "gemini-2.5-flash")
			require.NoError(t, err)
			events := streamRec.Body.String()
			previous := -1
			for _, event := range []string{"event: message_start", "event: content_block_start", "event: content_block_delta", "event: content_block_stop", "event: message_delta", "event: message_stop"} {
				index := strings.Index(events, event)
				require.Greater(t, index, previous, event)
				previous = index
			}

			countRec := httptest.NewRecorder()
			countContext, _ := gin.CreateTestContext(countRec)
			countContext.Request = httptest.NewRequest(http.MethodPost, "/v1/messages/count_tokens", nil)
			_, err = svc.CountAnthropicTokens(context.Background(), countContext, account,
				"claude-sonnet-4-6", "gemini-2.5-pro",
				[]byte(`{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"ping"}]}`))
			require.NoError(t, err)
			require.Equal(t, int64(17), gjson.Get(countRec.Body.String(), "input_tokens").Int())
		})
	}
}

func TestGeminiAnthropicCompatibility_ToolRoundTrip(t *testing.T) {
	stub := &queuedGeminiHTTPStub{responses: []queuedGeminiResponse{
		{status: http.StatusOK, header: http.Header{"Content-Type": []string{"application/json"}}, body: `{"candidates":[{"content":{"parts":[{"functionCall":{"name":"lookup","args":{"q":"weather"}}}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":5,"candidatesTokenCount":3}}`},
		{status: http.StatusOK, header: http.Header{"Content-Type": []string{"application/json"}}, body: `{"candidates":[{"content":{"parts":[{"text":"sunny"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":8,"candidatesTokenCount":2}}`},
	}}
	svc := &GeminiMessagesCompatService{httpUpstream: stub, cfg: &config.Config{}}
	account := geminiAnthropicIntegrationAccount(AccountTypeAPIKey)

	firstRec := httptest.NewRecorder()
	firstContext, _ := gin.CreateTestContext(firstRec)
	firstContext.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	_, err := svc.ForwardAnthropic(context.Background(), firstContext, account,
		[]byte(`{"model":"claude-sonnet-4-6","max_tokens":32,"messages":[{"role":"user","content":"weather"}],"tools":[{"name":"lookup","input_schema":{"type":"object","properties":{"q":{"type":"string"}}}}]}`),
		"claude-sonnet-4-6", "gemini-2.5-pro")
	require.NoError(t, err)
	toolID := gjson.Get(firstRec.Body.String(), "content.0.id").String()
	require.Equal(t, "tool_use", gjson.Get(firstRec.Body.String(), "content.0.type").String())
	require.NotEmpty(t, toolID)

	secondBody := fmt.Sprintf(`{"model":"claude-sonnet-4-6","max_tokens":32,"messages":[{"role":"assistant","content":[{"type":"tool_use","id":%q,"name":"lookup","input":{"q":"weather"}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":%q,"content":"sunny"}]}]}`, toolID, toolID)
	secondRec := httptest.NewRecorder()
	secondContext, _ := gin.CreateTestContext(secondRec)
	secondContext.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	_, err = svc.ForwardAnthropic(context.Background(), secondContext, account, []byte(secondBody),
		"claude-sonnet-4-6", "gemini-2.5-pro")
	require.NoError(t, err)
	require.Contains(t, secondRec.Body.String(), "sunny")
	sent, readErr := io.ReadAll(stub.requests[1].Body)
	require.NoError(t, readErr)
	require.Equal(t, "lookup", gjson.GetBytes(sent, "contents.1.parts.0.functionResponse.name").String())
}
```

failover 前可切换与 `message_start` 后不可切换由 Task 5 和 Task 7 的 handler 测试覆盖，集成测试不复制 handler 的 failover fixture。

- [ ] **Step 2: 运行集成测试并修复测试夹具编译问题**

```bash
cd backend && go test ./internal/service -run 'TestGeminiAnthropicCompatibility' -count=1 -v
```

Expected: PASS；所有 upstream 都是本地 `httptest.Server`。

- [ ] **Step 3: 写客户和管理员接入文档**

`docs/gemini-anthropic-compat.md` 必须包含可复制配置：

```bash
export ANTHROPIC_BASE_URL="https://api.modelport.top"
export ANTHROPIC_AUTH_TOKEN="<客户自己的 ModelPort API Key>"
export ANTHROPIC_MODEL="claude-sonnet-4-6"
export ANTHROPIC_SMALL_FAST_MODEL="claude-haiku-4-5"
```

同时写明：

- 实际上游是 Gemini，不是 Anthropic Claude。
- 客户不得获得 Google OAuth 凭据、账号状态或内部上游地址。
- 管理员先绑定可调度 Gemini 账号，再在分组编辑页开启兼容映射。
- 关闭 `allow_messages_dispatch` 即回滚 Claude 别名；原生 `gemini-*` 不受影响。
- `x-modelport-token-count-estimated: true` 表示计数为本地估算。
- 首期不支持 Files、Batches、Computer Use、Extended Thinking 和可靠的图片本地 token 估算。

README 只增加一条文档索引，不复制整份说明。

- [ ] **Step 4: 运行全量静态和测试门禁**

```bash
make test-backend
make -C backend test-unit
pnpm --dir frontend exec vitest run src/views/admin/__tests__/groupsMessagesDispatch.spec.ts
pnpm --dir frontend run typecheck
pnpm --dir frontend run lint:check
make build
git status --short
```

Expected:

- Go tests 与 `golangci-lint` PASS。
- Vitest、TypeScript、ESLint PASS。
- 后端和前端 build PASS。
- `git status --short` 只包含本计划预期文件；提交后为空。

- [ ] **Step 5: 做本地 HTTP 协议验收**

使用本地测试分组和专用本地 Key，不使用生产客户 Key：

```bash
curl -sS "$LOCAL_MODELPORT_BASE/v1/messages" \
  -H "x-api-key: $LOCAL_MODELPORT_TEST_KEY" \
  -H "anthropic-version: 2023-06-01" \
  -H "content-type: application/json" \
  -d '{"model":"claude-sonnet-4-6","max_tokens":64,"messages":[{"role":"user","content":"Reply with pong"}]}'

curl -i -sS "$LOCAL_MODELPORT_BASE/v1/messages/count_tokens" \
  -H "x-api-key: $LOCAL_MODELPORT_TEST_KEY" \
  -H "anthropic-version: 2023-06-01" \
  -H "content-type: application/json" \
  -d '{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"hello"}]}'
```

验收响应的 `model`、Anthropic error shape、`input_tokens`、估算 header、usage log 的 public/upstream model 和本地测试余额只变更一次。

- [ ] **Step 6: 提交文档和集成验收**

```bash
git add README.md backend/internal/service/gemini_anthropic_compat_integration_test.go
git add -f docs/gemini-anthropic-compat.md
git commit -m "docs: add Gemini Anthropic compatibility guide"
```

- [ ] **Step 7: 停在生产变更门禁前**

输出本地测试证据、提交 SHA、未推送状态和待执行的生产检查清单。不要创建生产 API Key，不要开启正式 Gemini 分组兼容开关，不要向 `https://api.modelport.top` 发计费请求；先向用户请求一次明确的生产验收授权。

---

## Self-review results

- Spec coverage: Tasks 1–2 覆盖映射、调度顺序和双模型；Task 3 覆盖内容块、工具选择、thinking 隔离、非流式/SSE；Task 4 覆盖真实计数、估算和图片门禁；Task 5 覆盖鉴权/限流/上游/流式错误；Task 6 覆盖 `/v1/models` 与最小管理 UI；Task 7 覆盖 usage、缓存 token 和计费；Task 8 覆盖三类账号集成、文档、回滚和生产门禁。
- Schema review: 复用现有 `allow_messages_dispatch` 和 `messages_dispatch_model_config`，没有数据库迁移、Ent generate 或缓存 schema 版本变更。
- Type consistency: `publicModel` 始终进入响应和 `ForwardResult.Model`；`dispatchModel` 进入选号；账号映射后的模型进入 `ForwardResult.UpstreamModel` 和计费。
- Rollback: 关闭 Gemini 分组 `allow_messages_dispatch` 即禁用 Claude 别名，原生 Gemini 模型继续可用。
- Production safety: 实施计划只允许本地/模拟 upstream 验收；真实 Key、正式开关和上游费用全部位于二次确认门禁之后。
