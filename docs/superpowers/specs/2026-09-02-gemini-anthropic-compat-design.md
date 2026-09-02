# Gemini Anthropic Messages 兼容层设计

日期：2026-09-02

状态：待用户审阅

目标版本：基于当前 Sub2API 主线和 ModelPort `v0.1.185` 生产配置

## 1. 背景

ModelPort 已经通过 Gemini OAuth 账号池对外提供 Gemini 模型，并已有 `POST /v1/messages` 的 Anthropic Messages 兼容入口。现有 `GeminiMessagesCompatService` 能完成基础请求转换、非流式响应、SSE 流式响应、工具调用、图片输入、用量提取、账号调度、失败切换和计费记录。

当前兼容层仍不足以让 Claude Code 用户只配置 ModelPort API 地址和 API Key 后稳定使用 Gemini，主要问题是：

1. Gemini 分组的 `/v1/messages/count_tokens` 没有走 Gemini 计数路径。
2. Claude Code 默认发送 `claude-*` 模型名，而 Google One Gemini OAuth 账号只接受受支持的 `gemini-*` 模型名。
3. Gemini OAuth 转发没有完整应用公开模型到上游模型的映射。
4. `tool_choice`、思考内容、缓存语义和错误格式仍有兼容缺口。
5. 当前生产 Gemini 账号尚无真实成功请求记录，缺少 Claude Code、计费和余额扣减的端到端证据。

本设计在现有网关内补齐兼容能力，不引入 LiteLLM 或新的公开协议服务。

## 2. 目标与非目标

### 2.1 目标

- 保持统一公开入口 `POST /v1/messages` 和 `POST /v1/messages/count_tokens`。
- 客户只需获得 `https://api.modelport.top` 和自己的 API Key。
- Claude Code 默认的主模型和快速模型请求可以在 Gemini 分组内映射到真实 Gemini 模型。
- 普通文本、流式输出、工具调用和工具结果回传可形成完整会话闭环。
- 返回 Anthropic Messages 兼容的成功、流式和错误结构。
- 客户计费采用实际上游 Gemini 模型价卡；使用记录同时保留客户请求模型和实际上游模型。
- 所有映射只在目标 Gemini 分组内生效，不改变 Anthropic、Grok、OpenAI 等其他分组行为。
- 兼容策略可配置、可审计、可关闭，并能安全回滚。

### 2.2 非目标

- 不伪装成 Anthropic 官方 Claude 模型，也不宣称 Gemini 具备与 Claude 完全相同的语义。
- 不实现 Anthropic Files、Batches、Admin、Skills、Computer Use 等其他 API。
- 不承诺完整复刻 Anthropic Prompt Caching；Gemini 上游实际返回缓存用量时只用于准确计费和 usage 展示。
- 不把 Gemini OAuth 凭据、Google 项目、账号状态或调度信息暴露给客户。
- 不新增第二套用户、API Key、余额、订阅或计费系统。

## 3. 兼容契约

### 3.1 公开端点

继续使用：

- `POST /v1/messages`
- `POST /v1/messages/count_tokens`
- `GET /v1/models`

Claude Code 的 Base URL 必须是站点根地址，例如 `https://api.modelport.top`，由客户端自行追加 `/v1/messages`。

认证继续接受：

- `Authorization: Bearer <ModelPort API Key>`
- `x-api-key: <ModelPort API Key>`

### 3.2 模型别名

模型别名属于分组公开契约，不属于单个账号的偶然配置。Gemini 分组增加一份显式的 Anthropic 兼容映射，首期默认值为：

| 客户请求模型 | 实际 Gemini 模型 |
| --- | --- |
| `claude-opus-*` | `gemini-2.5-pro` |
| `claude-sonnet-*` | `gemini-2.5-pro` |
| `claude-haiku-*` | `gemini-2.5-flash` |
| `gemini-2.5-pro` | `gemini-2.5-pro` |
| `gemini-2.5-flash` | `gemini-2.5-flash` |
| `gemini-2.0-flash` | `gemini-2.0-flash` |

规则要求：

- 精确映射优先于通配符映射。
- 通配符采用最长前缀优先。
- 映射必须在账号选择和模型支持检查之前完成。
- 映射结果必须再次通过账号模型白名单检查，不能借别名绕过账号能力限制。
- 映射配置缺失或目标模型不可用时返回 `not_found_error`，不得静默换到其他平台。
- 客户响应的 `model` 保留客户请求值；内部 `ForwardResult.UpstreamModel`、usage log 和计费使用实际 Gemini 模型。
- 模型广场和接入文档明确标注“兼容别名及实际模型”，避免让客户误认为调用的是 Anthropic Claude。

配置优先复用分组现有 JSON 配置字段；只有现有字段无法表达“公开别名到上游模型”的稳定契约时才新增迁移字段。不得把这套公共映射仅存放在单个 OAuth 账号凭据中。

## 4. 请求处理设计

### 4.1 数据流

请求处理顺序为：

1. API Key 鉴权并确定分组。
2. 以 Anthropic 协议解析请求并保存原始客户模型名。
3. 根据 Gemini 分组兼容映射解析实际模型名。
4. 使用实际模型执行渠道限制、账号模型支持检查和账号调度。
5. 将 Anthropic 请求转换为 Gemini `generateContent` 或 `streamGenerateContent`。
6. 转发至选中的 Gemini OAuth/API Key/Service Account 上游。
7. 将 Gemini 响应转换为 Anthropic Message 或 Anthropic SSE 事件。
8. 使用实际上游模型和上游 usage 计费；记录公开模型、实际上游模型和映射来源。

原始模型和实际上游模型必须使用不同变量传递，禁止在原请求体上无痕覆盖后丢失客户请求值。

### 4.2 内容块

首期正式支持：

- `system` 字符串或文本块数组
- `messages[].content` 字符串或内容块数组
- `text`
- `image`，仅 base64 图片来源
- `tool_use`
- `tool_result`
- 标准工具和 `custom` 工具定义
- 显式声明的服务端网页搜索工具

未知内容块不再直接 JSON 序列化为普通文本。处理规则为：

- 可安全忽略且不影响语义的元数据字段予以忽略。
- 会改变语义的未知内容块返回 `invalid_request_error`，错误中指出不支持的 block type。
- `document`、文件引用等首期未支持内容块明确返回错误，避免模型看到意外的 JSON 文本。

### 4.3 生成参数

继续映射：

- `max_tokens` → `maxOutputTokens`
- `temperature` → `temperature`
- `top_p` → `topP`
- `stop_sequences` → `stopSequences`

新增 `tool_choice` 映射：

| Anthropic | Gemini |
| --- | --- |
| `auto` | `AUTO` |
| `any` | `ANY` |
| 指定 `tool` | `ANY` 并设置允许的函数名 |
| `none` | 禁用函数调用 |

`disable_parallel_tool_use` 在 Gemini 上游无法等价表达时不得伪造支持。当该字段为 `true` 且请求包含多个可调用工具时，首期直接返回 `invalid_request_error`；字段为 `false` 或未提供时按 Gemini 的正常工具调用能力处理。

### 4.4 思考内容

首期不向客户承诺 Anthropic Extended Thinking 兼容。

- Anthropic 请求中的 `thinking` 不直接伪装成 Gemini 的等价能力。
- Gemini 返回标记为 thought 的 part 不得作为普通正文泄漏。
- Gemini 返回的 `thought: true` part 一律不写入客户正文；原始内部思考和签名不对外暴露。
- 工具调用所需 Gemini `thoughtSignature` 继续在内部保存或补齐，不进入客户可见凭据。
- 无法满足签名要求时沿用现有保守重试：先降级思考历史，再降级签名敏感的工具历史；降级必须写入内部观测字段。

## 5. `count_tokens` 设计

Gemini 分组必须使用独立的 Anthropic 计数处理器，不能调用 Anthropic 上游 URL。

处理过程：

1. 按 `/v1/messages` 相同规则解析公开模型并完成 Gemini 模型映射。
2. 把 Anthropic 内容转换为 Gemini `contents`、`systemInstruction` 和 `tools`。
3. 对支持的账号调用 Gemini `countTokens`。
4. 将 Gemini `{ "totalTokens": n }` 转为 Anthropic `{ "input_tokens": n }`。
5. OAuth scope 不允许调用 `countTokens` 时使用现有本地估算器回退。
6. 本地估算响应增加 `x-modelport-token-count-estimated: true`；真实上游计数不设置该头。
7. 计数请求检查 API Key、分组、余额/订阅和模型权限，但不占生成并发、不扣费、不写生成 usage。

本地估算必须覆盖 system、messages 文本和工具 schema。请求包含图片且上游计数不可用时，返回 `invalid_request_error` 并说明当前无法可靠估算图片 token，不得返回低估值。

## 6. 响应与流式事件

### 6.1 非流式响应

保持 Anthropic Message 结构：

- `id` 使用 `msg_01...` 格式
- `type: message`
- `role: assistant`
- `model` 使用客户请求模型
- `content` 包含 `text` 和 `tool_use`
- `stop_reason` 映射为 `end_turn`、`max_tokens` 或 `tool_use`
- `usage` 至少返回 `input_tokens` 和 `output_tokens`

若上游返回缓存读取量，响应可追加 Anthropic 兼容的 `cache_read_input_tokens`；没有真实缓存数据时不得虚构缓存命中。

### 6.2 流式响应

事件顺序必须满足：

1. `message_start`
2. 一个或多个 `content_block_start`
3. 对应的 `content_block_delta`
4. 对应的 `content_block_stop`
5. `message_delta`
6. `message_stop`

工具参数使用 `input_json_delta.partial_json`。任何时刻只能有一个同索引的内容块处于打开状态。上游开始向客户输出后禁止账号 failover，避免把两个账号的流拼接为一条响应。

## 7. 错误处理

`/v1/messages` 和 `/v1/messages/count_tokens` 的入口、鉴权、限流、调度和上游错误统一返回：

```json
{
  "type": "error",
  "error": {
    "type": "authentication_error",
    "message": "..."
  }
}
```

错误类型至少包括：

- `authentication_error`
- `permission_error`
- `invalid_request_error`
- `not_found_error`
- `rate_limit_error`
- `api_error`
- `overloaded_error`

流式响应开始前可返回普通 HTTP JSON 错误；流式响应开始后必须发送 Anthropic `error` SSE 事件并结束连接，不再写第二个 HTTP 响应。

对客户隐藏 OAuth、Google 项目 ID、代理地址、账号名称和上游凭据。内部日志保留 request ID、账号 ID、映射结果和脱敏后的上游错误。

## 8. 计费与使用记录

- 计费模型始终使用映射后的实际 Gemini 模型。
- 客户请求模型写入 `model`，实际模型写入 `upstream_model`。
- 映射来源写入既有 model mapping chain/观测字段；如果现有字段不能表达分组兼容映射，再增加非敏感枚举值。
- 普通输入、缓存读取、缓存写入、输出和图片输出继续分开计价。
- Gemini `promptTokenCount` 包含缓存读取量时，普通输入必须扣除 `cachedContentTokenCount`，避免重复收费。
- 思考 token 若包含在上游输出 usage 中，计入输出成本，但不得作为普通文本回传。
- `count_tokens` 不扣费、不占账号生成配额统计。
- 计费异步任务失败必须记录告警，不能影响已经成功返回给客户的生成响应。

## 9. 配置与管理界面

Gemini 分组编辑页增加“Anthropic / Claude Code 兼容”区域：

- 启用/关闭兼容映射
- 主模型别名目标
- 快速模型别名目标
- 可选的精确模型映射列表
- 只读预览：客户模型 → 实际模型
- 明确提示实际调用的是 Gemini，不是 Anthropic Claude

界面保存时验证：

- 目标模型非空
- 目标模型至少被分组内一个可调度账号支持
- 不允许循环映射
- 不允许映射到其他平台模型
- 通配符冲突时拒绝保存并指出冲突规则

首期必须同时实现上述最小管理界面和管理 API；不得只写入数据库隐藏配置，避免运维人员无法判断实际映射。

## 10. 测试与验收

### 10.1 单元测试

- 精确映射、通配符映射和最长前缀优先。
- Gemini OAuth 应用映射后的模型。
- 映射发生在调度前，目标模型不受支持时不可选择账号。
- system、text、image、tool_use、tool_result 转换。
- `tool_choice` 四种模式。
- 未知内容块返回明确错误。
- thought part 不泄漏为正文。
- 非流式 Message 格式。
- SSE 事件顺序、索引、文本增量和工具 JSON 增量。
- `count_tokens` 真实结果转换与估算回退。
- Anthropic 鉴权、限流和上游错误格式。
- usage 中普通输入和缓存输入互斥。

### 10.2 集成测试

- Gemini API Key、Google One OAuth、Service Account 三类账号。
- `/v1/messages` 非流式与流式。
- `/v1/messages/count_tokens`。
- 请求开始前 429/5xx failover。
- 流开始后上游失败不拼接第二账号。
- 客户请求模型、实际上游模型和计费模型一致性。
- 余额、API Key quota 和账号内部成本只更新一次。

### 10.3 生产验收

使用专门的低额度测试客户 Key，不读取或展示现有客户 Key：

1. Anthropic SDK 发送非流式文本请求。
2. Anthropic SDK 发送流式文本请求。
3. 完成一轮工具调用和工具结果回传。
4. 调用 `count_tokens`。
5. 使用最新 Claude Code 创建真实会话。
6. 验证 Claude Code 主模型和快速模型自动映射。
7. 核对响应、usage log、上游模型、余额和 API Key quota。
8. 核对失败请求不会重复扣费。

生产验收涉及创建测试 Key 和产生真实上游费用，实施时须在执行前再次取得用户确认。

## 11. 发布与回滚

发布采用兼容开关控制：

1. 代码上线但默认关闭 Gemini Anthropic 兼容映射。
2. 在测试分组启用并完成端到端验收。
3. 再为正式 Gemini 分组启用。

出现问题时：

- 关闭分组兼容映射即可恢复仅接受 `gemini-*` 模型名的原行为。
- `/v1/messages` 原有 Gemini 直传模型不受别名开关影响。
- 数据库迁移只增加向后兼容字段，不删除或重写现有配置。
- 不通过回滚数据库来撤销功能。

## 12. 实施边界

预计实施涉及：

- 网关路由和 Gemini `count_tokens` 分流
- 分组级公开模型映射解析
- Gemini OAuth 转发模型映射
- Anthropic 请求、响应、SSE 和错误转换
- 使用记录与模型映射观测
- 分组管理 API 和最小管理 UI
- 单元、集成和生产前验收脚本

不修改用户注册、API Key 生命周期、余额账户、订阅、支付和其他平台网关。
