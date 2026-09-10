# 2026-09-10 生图协议适配发布记录

## 根因与范围

线上 `gpt-image-2` 的 Responses 请求被改写成文本驱动模型 `gpt-5.6-luna`，而生图专用上游未配置该模型，因此返回 404，备用账号又因能力不匹配被排除，最终对客户端返回 502。近 24 小时同组有 43 条同类错误。

独立对照验证：同一上游的 `/v1/responses` 返回 502，但 `/v1/images/generations` 返回 200 和有效图片。单纯修改模型名称不能修复接口差异。

## 修复

- 增加账号级显式开关 `extra.openai_responses_image_transport=images`，默认关闭，仅在专用图片模型上将 Responses 请求桥接到原生 Images 接口。
- 无参考图走 generations，有参考图走 multipart edits；保留质量、尺寸等参数，输出 Responses JSON 或带 `response.completed` 的 SSE。
- 保留图片数量与原有计费逻辑。上游 404 在未发送客户端响应前交给故障切换。
- 修复原有图片 Base64 padding 归一化错误，避免带 `=` 的合法图片数据被错误拒绝。
- 对图片大小和数量设置边界，远程图片复用已有 SSRF 防护；不支持的服务端存储会话引用明确报错，不伪装成功。
- 仅开启账号 15 的新开关；未更改分组、价格、模型映射或普通文本账号能力。

## 版本与回归

- 提交：`0a5c79ba6ba9ef3553ad72db2fa5fccfaa5909c0`，分支 `feature/gemini-anthropic-compat`，推送至自有 fork。
- 镜像：`mrn666/sub2api:deploy-0a5c79ba6-amd64@sha256:12759a21bcd13790539701d8f7132c3ba7613c5aa3601c34c8adfd5de53a6bb5`。
- 应用版本仍为 0.2.4。该镜像同时包含当前分支先前已提交的池模式重试预算修复 `1bd7a2143`，已审查并纳入重试回归。
- `go test -tags unit ./internal/service -run 'Test.*(Retry|NativeImages|Images|ImageBase64|ImageOnlyModel)' -count=1` 通过，耗时 49.025 秒。
- Luna 独立执行桥接定向测试通过，耗时 1.047 秒。

## 生产验证与发布

使用管理员独立 $1 限额测试密钥，不使用客户密钥、不重放客户图片或正文。

| 测试 | HTTP | 时间 | 结果 | 本站扣费 |
| --- | --- | --- | --- | --- |
| 候选 `/responses` 流式生成 | 200 | 20.3 秒 | 1 张有效图片，完整 completed 事件 | $0.05 |
| 候选 `/responses` 流式参考图编辑 | 200 | 21.6 秒 | 1 张有效图片，完整 completed 事件 | $0.05 |
| 正式域名 `/responses` 高质量流式生成 | 200 | 44.6 秒 | 1 张有效图片，完整 completed 事件 | $0.05 |

两条 usage 记录均为 `gpt-image-2`、账号 15、分组 5、`image_count=1`、`stream=true`。

发布采用备份 → 候选 8081 → Caddy 平滑切流 → 旧连接排空 → 正式实例更新 → 回切与候选排空流程。首次发布编号 `0a5c79ba6` 因完整/短提交号格式不一致触发版本检查失败，候选被清理，线上实例未动；核实镜像短提交号后使用 `0a5c79ba6-r2` 重新执行，候选版本与健康检查通过。

发布前分组摘要为 `12:32229990ba2b7fc02e6bc0220e847788`，候选和切流阶段一致。双实例时可用内存约 2.7 GiB，既有 swap 保留，未重启数据库或 Redis。

公网高质量测试通过，三次本站验收共扣费 $0.15，独立测试密钥已通过站点界面停用，数据库复核状态为 inactive、累计用量 $0.15。首次 promote 等待 60 秒后仍有 1 条旧连接，发布工具安全退出并保留候选承接流量，未强停在途请求；再次执行后连接自然排空，正式实例更新完成。

最终 `finalize` 成功，Caddy 回到 `127.0.0.1:8080`，候选连接排空后停止。正式实例镜像 digest 与目标一致，健康状态 healthy，保留 `GOMEMLIMIT=1GiB`。分组摘要仍为 `12:32229990ba2b7fc02e6bc0220e847788`，数据库与 Redis 启动时间未变。

## 回退信息

- 发布备份目录：`/opt/modelport/deploy/modelport-releases/0a5c79ba6-r2`。
- 原账号开关备份：`/opt/modelport/backups/image-bridge-20260910-account15.json`，原状态为字段不存在。
- 原镜像：`mrn666/sub2api:deploy-5b44ea751-amd64@sha256:43dbcb09f8dce9bd73ebdb9ae47d6f1fd9d61919f60f0df21cd54ecf1d3654d3`。
- 如需回退，应平滑部署原镜像并仅恢复账号 15 的该字段、投递 `account_changed` 调度事件；不要恢复整库或覆盖其他账号设置。旧代码忽略此新字段。

验收覆盖 API 生成、参考图编辑与流式完成协议，不代表已经在客户 Windows 桌面端逐项操作验收。
