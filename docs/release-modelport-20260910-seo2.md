# ModelPort SEO 第二阶段发布记录

应用版本 `0.2.4`，冻结提交 `68d990b230d355d1c051b415d5792921e1bb5390`。本次只改变公开 SEO 内容、模板及测试；没有数据库迁移、收费、抽奖、拼团、鉴权或依赖配置变更。

发布已完成：北京时间 09:28:42 切入候选实例，09:33:49 完成回切和排空，状态 `finalized`。正式容器已确认版本及提交一致，候选正常停止并保留。

## 镜像与备份

- 标签：`mrn666/sub2api:deploy-68d990b23-amd64`。
- Registry index：`sha256:2d93c3149ebe2fb17b164d85e853f7abbd6d0e1d0b93ec538102fe8dd947d006`。
- 实际部署的 linux/amd64 manifest：`sha256:bf586e45f922ce5704ff1b83049e383fb896817caa56fe3996cb7038c7429c38`。
- 构建时间：`2026-09-10T01:12:26Z`。
- 发布工具沿用已审核的 `2d70c82fd` 版本，服务器文件 SHA-256 为 `8d73cbf3523644270fd0e4eff1272f25baf3764e7453026fd0eb6eb0fc521b3a`。
- 生产备份及阶段状态：`/opt/modelport/deploy/modelport-releases/seo2-68d990b23`。包含数据库归档、配置与脱敏审计信息，数据库归档已通过 `pg_restore -l` 验证，不进入 Git。
- 数据库归档大小 106,347,224 字节，权限 0600。

构建完成后，Buildx 上传阶段停滞；取消上传后复用同一份本地镜像执行 `docker push`，最终推送成功并核对远端 manifest。没有因为重试更换代码或镜像摘要。

## 验证

- 发布前再次运行 `go test -race -tags embed ./internal/web`，通过；工作树干净。
- 镜像内前端国际化检查、TypeScript 与 Vite 构建通过，保留既有大 chunk/Browserslist 提示。
- 候选实例健康、版本和镜像校验通过。两个新页面返回 200，站点地图 10 项，包含 BreadcrumbList。
- 10 个有效分组摘要仍为 `cd25af90f657cf0dab18be08a5e2a436`。
- 外网逐页校验首页与 9 个指南的状态、canonical 和 JSON-LD；robots 正常，不存在页面 404，匿名 `/v1/models` 401。
- 三个域名 `modelport.top`、`api.modelport.top`、`www.modelport.top` 的 HTTPS `/health` 均为 200。
- 浏览器确认新路由指南正文、面包屑，以及到排错指南的内链可用。没有进行抽奖、充值、购买或付费模型调用。
- 最终应用、PostgreSQL、Redis 均 healthy；数据库和 Redis 启动时间仍为 2026-08-26，未重启。同机 ModelPass 容器仍持续运行，5080 配置未改。
- 最终 Caddy 配置摘要与备份一致，回到 8080。旧实例及候选存在连接时，脚本暂缓操作并重试排空，没有硬停活动连接。
- 正式实例启动后日志大小写精确匹配 `ERROR/FATAL/PANIC` 为 0。

## Google Search Console

当前账号进入欢迎页面，ModelPort 网域所有权未验证。Google 生成 DNS TXT 验证要求后，用户尝试验证，页面提示找不到令牌。随后对 DNSPod 两台权威 DNS（janet/cuttlefish）及公共解析器检查，根域 TXT 未返回验证记录。

已请用户核对 DNSPod 的记录类型、主机记录、启用状态和保存结果，并保留 Search Console 页面供继续操作。没有擅自修改 DNS，没有报告站点所有权验证成功，没有提交站点地图或请求收录。需要完成验证后再继续，不以公开页面上线替代 Google 收录结果。

## 回退

上一应用为 `34f71d10bece5fa341ec405d67ef5db0972ff256`，对应 amd64 digest `sha256:9f3a71335ea47a50c2a01b131a42538a3b525f051d2fda08b4e5f370fcf7171c`。如需回退，仅按候选验收和排空流程回退应用，不用旧数据库备份覆盖期间新增的消费、充值和订单。
