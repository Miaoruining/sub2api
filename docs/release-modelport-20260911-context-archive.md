# ModelPort 静默上下文归档发布记录（2026-09-11）

## 发布结果

- 生产发布号：`context-archive-783407234`
- 应用镜像：`mrn666/sub2api:deploy-783407234-amd64`
- 镜像提交：`783407234c3ea74ea6119256c5e3c7489e68e8a9`
- 镜像清单摘要：`sha256:ef77ff7b9f5994fa5b117ade09ef488e355ec91251c87a8e13f374af7c7a477a`
- 发布方式：候选容器验证、Caddy 无中断切流、正式容器晋升、长连接自然排空、候选容器停止。
- 发布后状态：正式容器健康，Caddy 指向 `127.0.0.1:8080`，17 个业务分组摘要保持不变，数据库与 Redis 未重建。

## 数据边界

归档表 `request_context_archives` 只保存请求中携带的消息数组：

- 保留 `role` 与 `content`，保持原请求顺序；
- 支持 system、developer、user、assistant、tool 和 model 消息；
- 不保存模型响应、用户名、邮箱、用户 ID、API Key、密钥 ID、分组、IP、usage 或请求选项；
- 不向用户页面、通知中心、API 响应或错误信息增加任何归档提示；
- 归档异步、超时且 fail-open，归档异常不改变用户请求结果。

## 每日归档

- 本机目标：`/Volumes/MIAO/AI 数据/context-archive.sparsebundle`
- 外接卷通过固定挂载点和 Volume UUID 双重校验。
- sparsebundle 使用 AES-256 加密，内部为区分大小写的日志式 HFS+，以兼容当前 FAT32 外接卷。
- gzip 流按 512 MiB 分片，并校验 SHA-256、CSV 表头、行数、首末 ID、递增顺序和范围。
- 只有本地封存、复读校验和安全卸载全部成功后，才删除服务器上的同一精确 ID 范围。
- 任一步失败都保留服务器记录并在后续周期重试。
- LaunchAgent 每 30 分钟唤醒一次；北京时间 03:30 后每天最多成功归档一次，外接盘未连接时静默失败并保留服务端数据。

## 验收证据

- 合成金丝雀记录完成“服务器快照 → 加密卷写入 → 完整性校验 → 安全卸载 → 服务端精确删除”，本机状态推进至 `last_deleted_id=1`。
- 删除后远端快照为 `row_count=0`，加密卷未残留挂载。
- LaunchAgent 已加载，首次执行退出码为 0。
- 专用 SSH 仅允许归档 helper 的固定动作；任意命令返回 126，部署用临时登录已撤销。
- `https://modelport.top/health`、`https://api.modelport.top/health`、`https://www.modelport.top/health` 均返回 HTTP 200。
- 发布后 20 分钟应用日志中 ERROR/FATAL/PANIC 精确级别计数为 0。
- 服务器根盘为 77 GiB，已用 21 GiB，可用 57 GiB；应用、PostgreSQL、Redis 当次内存分别约 43 MiB、700 MiB、8 MiB。

## 源码记录

- `978c2b533 feat(archive): 静默归档请求上下文`
- `783407234 fix(archive): 兼容磁盘信息缺少挂载标记`
- `c20bb03e2 fix(archive): 修正归档快照聚合查询`
- `268d944ef fix(archive): 兼容外接 FAT32 加密归档`

