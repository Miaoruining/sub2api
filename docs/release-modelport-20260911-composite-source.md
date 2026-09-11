# 复合子代理分组发布记录（2026-09-11）

## 应用发布

- 发布号：`composite-034dafdc4`。
- 应用提交：`034dafdc4356048e5a617b57712b32f7e296ab1d`。
- 镜像：`mrn666/sub2api:deploy-034dafdc4-amd64`。
- linux/amd64 清单：`sha256:2c85b37577fb8366c6344f3327a019d665a7f1f20a0ac0f73a6adae778dd7c3a`。
- 镜像索引：`sha256:9733e64cc714411603582aa48753ea6e1ddb1c0494f5db40ee1fc1c8911beb7a`。
- 起始生产提交为 `d2df1024ca83482559e9bb7cd39ed113b8fa6e5d`；两项既有前端性能优化已保留。
- 完成备份、候选健康检查、Caddy 切流、正式晋升和候选排空；首次排空遇到 4 条连接，保留连接等待，重试后正常完成。
- 发布过程中 17 个既有分组摘要保持 `3b5aecd64cc6ab46bc4dd68213b8c3e6`；PostgreSQL、Redis 未重启，5080 端口未改。
- 迁移 `247_composite_route_source_groups.sql` 已应用，按迁移执行器 TrimSpace 规则计算的校验和为 `c01c9dc5e2ac57519a998b19cc8627efd8336c8e2481f9589fb159aded719aaf`。

## 本地验收

- 模型广场相关 8 个 spec 共 52 个测试通过。
- 既有 Airwallex 延迟加载测试 1 个通过；刷新本地旧生成物后验证，没有修改已上线的性能逻辑。
- Composite 路由测试 3 个顶层测试（含子测试 10 个）通过，模型广场 handler 11 个测试通过。
- Docker 内前端生产构建、Go embed 交叉编译通过；镜像实际二进制提交号匹配。

## 分组配置与真实调用

应用已发布；分组 #27「复合子代理」已创建并启用，29 条精确来源路由、29 项模型白名单完整匹配。原来源组配置快照在写入新组前后保持一致。模型广场已展示来源倍率、阶梯价、DeepSeek 高峰价与图片单价。

配置工具使用服务器内现有管理 API Key，凭据仅在内存，通过 loopback 正式管理 API 操作。来源组只读，新增组采用精确 29 条模型路由并继承来源计费。

参考：[设计和接入说明](composite-source-groups.md)、[29 条路由清单](composite-source-routes.example.json)。

### 真实调用阶段记录

专用管理员测试 Key #88，限额 $1，凭据仅在服务器内存中读取；验收完成后已通过后台禁用，未改其他用户密钥。下列结果为生产 HTTP/SSE 短请求，非完整开发会话压力测试。

| 模型 / 协议 | 来源组 | 实际扣费（美元） | 结果 |
| --- | ---: | ---: | --- |
| gpt-5.6-sol-016 / Responses | 12 | 0.000036 | completed，15 输入/5 输出，倍率 0.16 |
| gpt-6-astra-023 / Responses | 19 | 0.000092 | completed，15 输入/5 输出，倍率 0.23 |
| deepseek-v4.1-flash / Messages | 23 | 0.0000752192 | end_turn，32 输入/37 输出，符合工作日高峰 ×2 |
| glm-5.3 / Responses | 23 | 0.000043056 | completed，18 输入/3 输出 |
| kimi-k3 / Messages | 23 | 0.00093408 | end_turn，88 输入/38 输出 |
| MiniMax-M3 / Responses | 23 | 0.000024352 | completed，34 输入/128 缓存读取/2 输出 |

Luna 首次 Responses 返回 502，ops #982658 证实上游为 403 access forbidden。Gemini 3.8 Responses 与 image-2 Images 首次请求在 120 秒内未完成，探针 connection_error，暂不列为验收通过。

GLM-5.3 Messages 的 echo_probe 工具调用及结果续接完整成功，两条 usage 均归因来源组 #23；Sol-016 Responses 第一轮工具调用成功，第二轮上游返回 Too many pending requests（502，ops #982661），未将其误报为完整往返成功。

后续复核：

- `gpt-5.6-sol-023` Responses 工具往返通过，两次扣费 $0.0002944 / $0.00061525，均来源 #19、倍率 0.23。
- `glm-5.3` Responses 工具往返通过，两次扣费 $0.000354112 / $0.000221296，均来源 #23、倍率 0.16。
- `gpt-image-2` 将探针等待上限延长为 300 秒后复核通过，HTTP 200、恰好一张图片、usage #45064 归因来源 #5，实际扣费 $0.05。

- `gpt-5.6-sol-023` Messages 工具往返通过，两次扣费 $0.00031395 / $0.00025645，均来源 #19、倍率 0.23。这验证了 GPT 主模型与 GLM 子模型都可以经同一个组使用 Claude Messages 和 Codex Responses 协议。

## 收尾与未通过项目

- 最终配置检查仍为 #27、29/29 路由，六个来源组配置快照与创建前一致。
- GPT 稳定线路与 GLM 已分别通过 Messages/Responses 工具往返；Codex 使用 HTTP/SSE，配置 `supports_websockets = false`。未执行完整 Claude Code/Codex 客户端长任务压力测试。
- Gemini 3.8 Responses 首次 120 秒未完成，3.7 Messages 复核 80 秒未完成，均 connection_error；未声称 Gemini 实时可用。入口权限检查会快速返回 4xx，代码审查未发现缺失入口权限；仍需排查来源账号/上游响应耗时。
- Luna 专用来源 #21（高老板）上游返回 403。保留原路线及价格，没有擅自更换供应商或改凭据。
- 生图首次探针超时后服务端继续完成生成，因此两次图片请求各扣 $0.05，合计 $0.10；这不属于失败请求重复收费，而是两次已完成生成。所有验收合计约 $0.1077。
- 专用 Key #88 已停用；本轮后台探针作业均已结束。
- 上架不等于保证所有来源实时健康，Luna 与 Gemini 的上述问题仍需处理。
