# Gemini 账号的 Anthropic Messages 兼容接入

ModelPort 可将 Gemini 账号的模型能力以 Anthropic Messages API 形式提供给 Claude Code 等客户端。客户只需要 ModelPort 的 API 请求地址和自己的 API Key，不会获得 Google OAuth 凭据、Gemini 账号状态或内部上游地址。

> 重要：这是协议兼容层。客户请求中的 `claude-*` 是公开别名，实际生成内容的上游是 Google Gemini，不是 Anthropic Claude。

## 客户接入

### Claude Code

macOS/Linux 可直接配置：

```bash
export ANTHROPIC_BASE_URL="https://api.modelport.top"
export ANTHROPIC_AUTH_TOKEN="<客户自己的 ModelPort API Key>"
export ANTHROPIC_MODEL="claude-sonnet-4-6"
export ANTHROPIC_SMALL_FAST_MODEL="claude-haiku-4-5"
```

然后在同一终端启动 Claude Code。不要在命令、截图、工单或聊天中公开真实 API Key。

### REST API

非流式消息请求：

```bash
curl -sS "https://api.modelport.top/v1/messages" \
  -H "x-api-key: <客户自己的 ModelPort API Key>" \
  -H "anthropic-version: 2023-06-01" \
  -H "content-type: application/json" \
  -d '{
    "model": "claude-sonnet-4-6",
    "max_tokens": 256,
    "messages": [{"role": "user", "content": "Reply with pong"}]
  }'
```

流式请求只需要在 JSON 中增加 `"stream": true`，响应按 Anthropic SSE 事件顺序返回。

Token 计数：

```bash
curl -i -sS "https://api.modelport.top/v1/messages/count_tokens" \
  -H "x-api-key: <客户自己的 ModelPort API Key>" \
  -H "anthropic-version: 2023-06-01" \
  -H "content-type: application/json" \
  -d '{
    "model": "claude-sonnet-4-6",
    "messages": [{"role": "user", "content": "hello"}]
  }'
```

返回体为 `{"input_tokens": <数值>}`。如果响应头包含 `x-modelport-token-count-estimated: true`，该数值是上游无法计数时的本地估算，不应作为精确计费凭证。

## 管理员配置

1. 在账号管理中添加 Gemini 账号，支持 API Key、OAuth 和 Vertex Service Account。
2. 确认账号状态为可调度，且能访问要映射的实际 `gemini-*` 模型。
3. 创建平台为 Gemini 的分组，将可调度账号绑定到该分组。
4. 编辑该分组，在「Anthropic Messages 兼容」中开启 `allow_messages_dispatch`。新建分组时该开关不可用；先保存并绑定账号，再编辑开启。
5. 核对默认映射，并按需增加精确别名。建议映射如下：

| 客户公开模型 | 实际 Gemini 模型 |
| --- | --- |
| `claude-opus-4-6` | `gemini-2.5-pro` |
| `claude-sonnet-4-6` | `gemini-2.5-pro` |
| `claude-haiku-4-5` | `gemini-2.5-flash` |

6. 为实际 `gemini-*` 模型配置价格。计费使用实际上游 Gemini 模型，使用日志同时保留客户公开别名和实际上游模型，便于对账。
7. 使用专用测试 API Key 做非生产验收，确认 `/v1/models`、`/v1/messages` 和 `/v1/messages/count_tokens` 后再对客户开放。

`/v1/models` 只会展示已启用且能解析到可用实际 Gemini 模型的 Claude 别名。如果模型广场未显示，依次检查分组平台、账号绑定和可调度状态、兼容开关、别名目标是否可用。

## 能力边界

首期支持文本内容、常用图片输入、system 指令、工具定义、`tool_use` / `tool_result`、非流式与 SSE 输出，以及 token 计数。

首期不支持：

- Anthropic Files API 和 Batches API。
- Computer Use。
- Extended Thinking 的完整语义对齐。
- 无上游计数时，对本地 base64 图片做可靠 token 估算。该场景会直接返回参数错误，不伪造精确数值。

Gemini 不支持或无法等价转换的 Anthropic beta 能力不会被虚构为已支持。客户应以本文档列出的能力为准。

## 错误与故障切换

- 客户看到的错误始终为 Anthropic `error` JSON 形状，不直接暴露 Google 响应体或内部凭据。
- 在还未向客户发送 `message_start` 时，符合策略的上游错误可以切换账号。
- 一旦流式响应已开始，系统不会拼接另一个账号的输出。
- 每个已计费的成功请求只写入一次 usage；缓存读取 token 与普通输入 token 分开计算，避免重复计费。

## 回滚

在 Gemini 分组编辑页关闭 `allow_messages_dispatch`（界面中的「Anthropic Messages 兼容」开关）即可停止公开 Claude 别名和 `/v1/messages` 分发。该操作不删除账号、分组或原生 `gemini-*` 模型能力，因此可以快速恢复。

不要为了回滚而删除 OAuth 凭据或数据库记录。如需再次开启，确认实际 Gemini 模型可调度后重新打开开关即可。
