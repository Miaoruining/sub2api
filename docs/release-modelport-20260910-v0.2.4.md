# ModelPort v0.2.4 合并与生产发布

2026 年 9 月 10 日完成。北京时间 02:54:13 切入候选实例，03:05:03 完成正式回切及候选排空。发布状态为 `finalized`。

## 代码和镜像

- 上游稳定版：`v0.2.4`（`5de5e2bed035d43591a2e10e51f420ef6a84eb98`）；合并到上游随后同步版本号的 `origin/main`（`98d86915b`）。
- 应用冻结提交：`34f71d10bece5fa341ec405d67ef5db0972ff256`。
- 发布辅助工具最终提交：`2d70c82fd265d48bf420e663ea480050bfe34e19`，只影响部署工具，不改变应用镜像。
- 已推送个人仓库 `fork/feature/gemini-anthropic-compat`，未推送上游或主分支。
- 镜像标签：`mrn666/sub2api:deploy-34f71d10b-amd64`。
- Registry index：`sha256:f4a4517868d6d9fff89ae2dfc43533b5650c3bbb391c5c66a4581b739b7371ac`。
- 正式部署的 linux/amd64 manifest：`sha256:9f3a71335ea47a50c2a01b131a42538a3b525f051d2fda08b4e5f370fcf7171c`；生产 Compose 按此摘要固定镜像。
- 服务器二进制确认版本 `0.2.4`、应用提交 `34f71d10bece5fa341ec405d67ef5db0972ff256`，构建时间 `2026-09-09T18:33:43Z`。

本地此前已提交的模型广场、统一密钥路由、按实际分组计费、抽奖、拼团、渠道状态和 SEO 改动一并保留。六处冲突保留本地 poolScope、拼团额度中间件、Gemini countTokens 分类与自动路由，同时合入上游 MiniMax、Grok 媒体资格及缓存广播等改动。上游支持新平台不等于自动启用生产分组；本次没有修改业务开关、价格、余额或用户密钥。

## 验证

- 前端全量：286 个测试文件、2,084 项测试通过。
- `pnpm build` 通过，包含国际化完整性、TypeScript 和 Vite 构建；既有大 chunk、Browserslist 提示保留。
- 后端 `go test -tags unit` 覆盖 service、handler、server、repository、migrations，通过。
- `go test -tags embed ./internal/web` 通过。
- PostgreSQL 隔离集成测试覆盖抽奖、拼团、自动分组数据库约束及 GatewayRouting，通过。未使用生产数据库执行测试。
- 发布工具 Python 编译和纯函数检查通过；现场发现 `ss` 参数拆分错误后已修正并提交。首次 promote 在替换正式容器前安全退出，未强停旧请求。修正版完成连接排空检查。
- 上游 `237_add_minimax_platform.sql` 与本地 `237_daily_lottery.sql` 均已在生产迁移表确认；迁移按完整文件名记账，不因前缀相同而冲突。

## 备份与生产保护

备份及发布状态目录：`/opt/modelport/deploy/modelport-releases/v024-34f71d10b`。目录受限权限，保存数据库归档、Compose、环境配置、应用配置、Caddy、脱敏容器信息及分组摘要。凭据、数据库与客户记录未进入 Git。

- 数据库归档：95,739,441 字节，文件权限 0600，`pg_restore -l` 校验通过。
- 10 个有效分组整行聚合摘要在备份、候选、正式和最终阶段均为 `cd25af90f657cf0dab18be08a5e2a436`。
- PostgreSQL 启动时间保持 `2026-08-26T15:20:14.643305855Z`；Redis 保持 `2026-08-26T15:20:14.642884788Z`，均 healthy，未重启。
- Caddy 仅切换 ModelPort 的 8080/8081，最终文件摘要与备份一致，权限保持 0644；同机 ModelPass 的 5080 未修改，原容器持续运行。
- 正式实例 healthy；候选连接归零后正常停止，容器保留为 exited，未强制删除。
- 最终检查正式实例日志，大小写精确匹配 `ERROR/FATAL/PANIC` 为 0。最初不区分大小写的宽泛匹配包含普通 error 字段，不作为错误级别计数。

## 线上验收

- `modelport.top`、`api.modelport.top`、`www.modelport.top` HTTPS `/health` 均返回 200。
- 浏览器保持现有登录态；拼团大厅商品与订阅入口正常，抽奖页八格布局及每日 09:00–11:00 文案正常，管理员开关行为保留。
- 验收仅查看页面，没有抽奖、创建付费订单、改变余额或调用付费上游。
- 首页和 7 个 `/learn` 指南均为完整 HTML、200、正确 canonical；浏览器可阅读指南正文。
- robots 为 text/plain、sitemap 为 application/xml，均 200；私有页面保持 noindex。
- 不存在页面返回 404，`/index.html` 返回 308 到根路径，未带密钥的 `/v1/models` 返回 401。
- Search Console 所有权及提交尚未确认，本次上线不代表 Google 已收录或获得排名。

## 回退

保留旧镜像 `mrn666/sub2api:deploy-4dc66e838-amd64` 和上述备份。需要回退时，优先在候选端口验证旧应用，再按相同排空切流流程回退应用镜像；不要直接恢复旧数据库覆盖上线后新增的消费、充值和订单。MiniMax 迁移是平台约束的前向扩展，本次没有自动回滚数据库。
