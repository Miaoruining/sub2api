# Messages 上下文与续接对照验收

## 范围

2026-09-13，在用户授权下从 AirPaper 生产主机发出最多 6 次合成短请求，使用现有 ModelPort Luna 配置及腾讯云选择性 CONNECT 中转。未修改生产配置，未重跑论文任务，未传输论文、用户数据或工作区文件。本记录不含密钥、响应签名或推理内容。

本次共发出 **6 次客户端请求**，未追加失败重试；网关内部可能另有重试。客户端均采用非流式响应，因此不能替代真实 CLI 流式长任务验收。

## 实测

测试在首轮写入合成测试码 `ORCHID-742`，要求先调用 `check_ready` 工具，收到 `READY` 后仅返回该码。

| 请求 | 路径 / 条件 | HTTP | 耗时 | 结果 |
| --- | --- | --- | --- | --- |
| 1 | Responses 首轮，强制工具调用、store=true | 200 | 2.92s | 正确返回 function_call |
| 2 | Responses，仅 previous_response_id 与工具结果 | 400 | 7.71s | No tool call found for function call output |
| 3 | Responses，完整首轮 user、function_call、工具结果，不附加续接 ID | 200 | 2.43s | 正确返回 ORCHID-742 |
| 4 | Messages 首轮，强制工具调用 | 200 | 5.15s | 正确返回 tool_use |
| 5 | Messages，提交完整 user、assistant tool_use、user tool_result | 200 | 9.80s | 错误返回 How can I help? |
| 6 | Messages，新会话，17 条历史，首条包含测试码 | 200 | 6.31s | 错误表示首条没有测试码 |

请求 5 复现了任务 46 的泛化问候症状。请求 2/3 表明此线路不能依赖续接 ID 代替完整输入。请求 6 与生产旧版尾部 12 条裁剪风险吻合。HTTP 200、端点探活、非零输出 Token 均不足以作为多轮任务正确性的验收标准。

## 代码对应风险

- 原 `openAICompatContinuationEnabled` 只根据 APIKey 账户类型与 GPT 模型名称启用服务端续接，没有验证该线路是否能恢复历史。
- 有 response ID 时，Messages 转换后只发送最新轮输入；即使客户端提交完整历史，原始任务也可能不再发给上游。
- 无 response ID 时，旧版 full replay guard 仍会裁剪尾部 12 条，丢弃首轮指令；本地提交 `f42fff52e` 已取消该裁剪，尚未部署。
- 正常续接与裁剪分支仅有 DEBUG 字段，生产 INFO 级别的历史日志无法追溯事故每一轮的分支，因此不能宣称已从历史请求体逐跳证明全部事故。

## 修复原则与边界

Messages 兼容路径默认保留完整历史，不因模型名称自动信任第三方服务端续接。不得通过吞掉工具错误、伪造产物、把问候标记为论文完成来放宽验收。

本次修复不改变原生 Responses API 的调用合同，不改变 Codex/OAuth 账户路径，不调整全局生产队列。完整历史可能增加请求体与未命中缓存 Token；优先保证指令与工具结果完整，后续节省 Token 应通过经过验证的缓存或显式上下文压缩实现。

本轮已将 `openAICompatContinuationEnabled` 在 Messages 兼容路径中设为关闭，并更新旧行为断言，验证旧 session binding、多工具历史、完整重放和 OAuth/Codex 回归。

Docker Go 1.27 定向验证：`go test ./internal/service -run '^TestForwardAsAnthropic_' -count=1`，退出码 0；service 包编译及该组测试通过。`git diff --check` 通过。没有声称全部 service 测试通过。

仍需独立发布，并在授权下验证真实流式多轮工作流；本次六次对照不等于生产修复已验收。
