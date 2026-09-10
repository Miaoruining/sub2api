# 站点 Logo 静态化与缓存

管理端上传的 `site_logo` 原本以 data URI 保存。嵌入式前端会将它同时写入 `window.__APP_CONFIG__` 和 favicon，较大的图片因此重复进入每次返回的 HTML。

`backend/internal/web/branding_assets.go` 在 HTML 注入时将受支持的 PNG、JPEG、GIF、WebP 替换为 `/branding/logo/<sha256>.<ext>`。配置存储和公开设置 API 保持原值，前端初始化优先读取 HTML 中的短地址。图片返回原始字节，不做重采样或转码。

图片路由仅接受 GET 和 HEAD，提供 ETag、`Cache-Control: public, max-age=31536000, immutable` 及 `X-Content-Type-Options: nosniff`。解码后限制为 300 KiB，并核对实际 MIME。未知哈希、非法路径返回 404，其他方法返回 405；SVG、外部 URL、相对 URL 及无效 data URI 维持既有渲染行为。

更新 Logo 后，既有配置更新回调清除 HTML 与公开设置快照缓存，新内容生成新哈希地址。源站不保留旧图片副本，旧哈希在快照失效后返回 404；浏览器和 CDN 已缓存的旧字节可继续按原缓存策略复用。

## 验证

`go test -tags embed ./internal/web` 覆盖 HTML 缩小、favicon 与应用配置地址一致、图片字节一致、GET/HEAD/304、错误路径和方法、Logo 更新后的地址变化。测试需要 `internal/web/dist`，正常生产构建由标准 Dockerfile 的前端阶段生成；独立工作树测试可临时提供嵌入夹具，测试结束后清理。

发布时应验证真实配置对应的图片哈希、浏览器加载状态和 CDN 缓存响应。分别记录 HTML 与首次图片下载量，不能将 HTML 的缩小比例等同于整个网页或三网时延的提升比例。

## 回退

本功能不修改数据库设置，不增加数据库迁移。可单独 revert Logo 功能提交并重建应用。如果发布版本同时包含其他业务变更，应保留那些变更，避免用旧镜像覆盖它们。不要用旧数据库备份覆盖期间产生的消费、订单或配置。

## ModelPort 发布记录

功能原提交为 `ffc78ffae1b6f8f98a66e8872021e398e6d00f8e`，已推送至 `fix/public-logo-assets`。统一发布分支纳入为 `d6a4a529eccb1b6ccf02a85cc5e1aee73d255aa6`，应用冻结提交为 `8d9ea6b91e6e40d15807f324e3092a78b224d427`。

上线前首页为 529,102 字节，gzip 传输 475,405 字节；原始 PNG 为 193,382 字节，SHA-256 为 `61fb611d13a721bbdb283e747f85d1535e65284cab67933c4057c095f04147bd`。

2026-09-11 已完成候选与正式实例的公网验收。正式镜像为 `mrn666/sub2api:deploy-8d9ea6b91-amd64@sha256:5964700d58b18b871b458f838cef149c1419fab74d5c798f1003b14874e7bd93`，Caddy 已回到正式 8080。

| 指标 | 优化前 | 优化后 |
| --- | ---: | ---: |
| HTML 原始字节 | 529,102 | 13,536 |
| HTML gzip 传输字节 | 475,405 | 6,199–6,200 |
| 首次 HTML 与 Logo 合计字节 | 475,405 | 199,581–199,582 |

HTML 原始体积减少 97.44%；首次 HTML 与 Logo 合计传输减少约 58.02%。原图内容和 1254×1254 尺寸一致。root 与 www 的原始 HTML 中 favicon 和应用配置均使用同一短地址，浏览器图片加载成功。两入口的 GET/HEAD/304、错误哈希 404 均正常；CDN 图片及新 JS 均观察到缓存命中。root、api、www 三个入口的健康检查均为 200。

同轮在阿里云 CDN 开启 Gzip。固定旧 JS 压缩前为 186,482 字节，开启后约 56 KB 且解压哈希与源站一致；统一版本的新 JS 为 186,482 字节，最终验证传输 57,808 字节，root 与 www 解压哈希一致。复用现有 CDN，未新增收费节点或调整 DNS。
