# Composite 来源分组配置与接入说明

本文记录本轮 Composite 路由可选 `source_group_id` 的行为、模型组合建议和待发布操作。**当前功能尚未部署到生产环境。** 文中的接入片段只供用户按需复制，不会自动修改用户配置。

## 来源分组行为

标准余额 Composite 组设置 `source_group_id` 后，会继承来源组的完整计费规则和账号池，包括基础倍率、逐模型价格、阶梯价、分时价以及可用账号。请求仍由外层 Composite 组接收和解析模型别名；真实 usage 的分组归因使用来源组，用户/API key 仍记录为原始请求所使用的 key，不会改写为来源组 key。

选择来源组只建立配置引用，不复制账号。来源组的账号、价格和模型配置发生变化时，Composite 路由按最新来源组配置生效。

来源校验规则如下：

- 外层 Composite 余额组和来源组都必须是 `standard` 类型。
- 来源组必须处于 `active` 状态，并且是公开组。
- 禁止来源组指向自身。
- 禁止多层 source route；后端会拒绝 Composite → source → source 的链式配置。
- 允许来源组本身是 Composite 组，包括国产模型场景，但仍受上述 `standard`、active、公开和禁止链式规则约束。
- 未设置 `source_group_id`（`null`）时，继续沿用当前 Composite 组自己的账号、倍率和价卡，兼容已有配置。

模型别名和上游模型继续使用已有路由字段。需要让同名 GPT 根据不同来源组选择时，应给客户端使用不同公开别名，例如 `-016` 与 `-023` 后缀，并分别将别名映射到对应的上游模型；不要依赖同名 `gpt` 在多个来源组之间隐式猜测。

## 目标用户模型组合

当前建议的组合边界：

- GPT 0.16 来源与 GPT 0.23 来源使用不同公开别名；推荐保留 `gpt-5.6-sol-016` 和 `gpt-5.6-sol-023` 这类可审查的命名方式，实际名称以后台已配置别名为准。
- 国产模型的 11 款条目沿来源组现有模型和别名提供，不额外复制或扩展条目。
- 不新增已删除的 DeepSeek 模型；来源组若已删除对应条目，Composite 也不应重新加入。
- Gemini 型号必须在部署前根据来源组实际能力、上游返回和生产验收名单再次核实，不沿用未经核实的历史型号。
- image 请求使用 Images 工具验证和验收，不交给聊天子代理模拟图片调用。

生产验收前应锁定最终模型名单，至少逐项确认公开别名、上游模型、endpoint、来源组和实际计费归因；本文件中的模型名称只表达组合示例，不替代生产验收名单。

## 待发布操作

本轮尚未部署。发布前由主控确认以下事项：

1. 当前代码、数据库迁移和镜像均为同一冻结提交，并取得不可变的 `linux/amd64` 镜像 digest。
2. 目标来源组满足 `standard`、active、公开、非自身，并确认不存在多层 source route。
3. GPT `-016`、`-023`、国产 11 款和 Gemini 最终模型名单已与后台来源组配置逐项对照；已删除 DeepSeek 不得出现。
4. 候选实例先验证健康、版本、别名解析、上游模型、来源组计费和 usage 归因，再进行候选切流。
5. 生产切流沿用 `deploy/modelport-release.py` 的候选、Caddy 切换、连接排空、正式晋升和 finalize 流程；数据库与 Redis 不因本次应用发布重启。

## Codex 接入模板

以下内容对应用户级 `~/.codex/config.toml` 示例。请用户自行审阅后配置，本文不会写入该文件：

```toml
model = "gpt-5.6-sol-023"
model_provider = "modelport"

[model_providers.modelport]
name = "ModelPort"
base_url = "https://modelport.top/v1"
env_key = "MODELPORT_API_KEY"
wire_api = "responses"
supports_websockets = false
```

该复合组使用 HTTP/SSE 的 Responses 接入，明确关闭 WebSocket，保证每次请求都按公开模型别名选择来源。

自定义 Codex agent 放在项目或用户的 `.codex/agents/*.toml`。官方要求至少提供 `name`、`description` 和 `developer_instructions`；`model` 可选：

```toml
name = "code-reviewer"
description = "检查代码改动中的错误和遗漏"
developer_instructions = "阅读相关代码并报告可复现的问题；未经主代理要求，不修改文件。"
model = "deepseek-v4.1-flash"
```

主模型 `gpt-5.6-sol-023`、子 agent `deepseek-v4.1-flash` 或 `glm-5.3` 仅为接入示例，最终必须以生产验收模型名单为准。需要为子 agent 指定模型时，可在相应 `.codex/agents/*.toml` 中按名单填写 `model`。

参考官方文档：[Codex 高级配置](https://learn.chatgpt.com/docs/config-file/config-advanced)、[Codex 子代理配置](https://learn.chatgpt.com/docs/agent-configuration/subagents)。

## Claude 接入模板

Claude 的 LLM gateway 仅提供环境变量示例；令牌由用户自行提供，不在本文记录或回显：

```bash
export ANTHROPIC_BASE_URL="https://modelport.top"
export ANTHROPIC_AUTH_TOKEN="<用户自己的令牌>"
claude --model gpt-5.6-sol-023
```

Claude 子 agent 放在 `.claude/agents/*.md`，使用 YAML front matter。`model` 应填写生产验收名单中的完整 model ID：

```markdown
---
name: code-reviewer
description: 检查代码改动中的错误和遗漏
model: glm-5.3
---

阅读相关代码并报告可复现的问题；未经主代理要求，不修改文件。
```

不要设置全局 `CLAUDE_CODE_SUBAGENT_MODEL` 覆盖各 agent 的 `model`；否则会绕过每个 agent 的模型选择。主模型和子 agent 示例同样受生产验收模型名单约束。

参考官方文档：[Claude LLM gateway](https://code.claude.com/docs/en/llm-gateway)、[Claude 子代理](https://code.claude.com/docs/en/subagents)。

## 本轮待上架清单

2026-09-11 已通过后台与模型广场只读核对，共 29 条公开模型路由，见 [路由清单](composite-source-routes.example.json)。此文件尚未写入生产。

| 来源组 | 路由数量 | 公开名称规则 |
| --- | ---: | --- |
| gpt-pro #12（0.16） | 5 | 原模型名加 `-016` |
| gpt pro 20x 号池-稳定 #19（0.23） | 5 | 原模型名加 `-023` |
| gpt-luna专用 #21 | 1 | `gpt-luna` |
| Gemini #3 | 4 | 3.8 flash、3.8 flash high/low、3.7 flash 原名 |
| 国产模型 #23 | 11 | 原组当前 11 款模型原名 |
| GPT image 2 /2.5 生图 #5 | 3 | image-2、2.5 sunburst/flare 原名 |

`gpt-luna-016` 继承 gpt-pro 的阶梯和缓存写入价，`gpt-luna` 继承 Luna 专用组，两者不能因为基础倍率相同而合并。#19 Terra 的逐模型价格覆盖也必须保留。生图组目前各尺寸均为 $0.05/张，以实际来源组最新价目为准。

## 本地验证记录

- 前端 287 个测试文件、2091 个用例通过，类型检查和涉及文件 lint 通过。
- 前端生产构建通过，Go embed 服务端构建通过。
- 后端 Composite、模型广场、Gemini Responses 定向测试通过；路由、handler、admin handler、repository 定向测试通过。
- 新增迁移的结构约束测试通过。
- 覆盖来源组停用拒绝、自引用/订阅/专属组拒绝、源白名单、同名线路别名改写、源倍率继承、缓存/分时/阶梯价卡目录、Gemini JSON/SSE 工具调用往返和上游 HTTP 错误。
- 本地测试使用模拟上游；生产真实调用、实际扣费对账和上架仍待服务器登录后执行。

## 回退边界

迁移只增加可空字段与约束，不能通过恢复旧数据库备份回退，以免覆盖线上消费和订单。若新增复合组已开放，回退应用前先停用该新增组，保留原来源组；旧代码会忽略 source_group_id，无法承诺新组仍可调用。恢复本功能版本并重新验收后再启用新组。
