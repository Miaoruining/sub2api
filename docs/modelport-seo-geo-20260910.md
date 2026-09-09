# ModelPort SEO / GEO 第一阶段

日期：2026-09-10。此文记录实现与上线验收要求，不代表已被 Google 收录或获得排名。

## 目标与边界

让搜索引擎和生成式搜索能读取真实、完整的产品说明。优先建立“ModelPort”品牌词，以及“AI API 中转站”“Codex 中转站接入”“Claude Code API 接入”“模型分组倍率”等相关主题的内容基础。广义“中转站”同时包含物流等其他搜索意图，不能保证获得前几名。

不使用关键词堆砌、隐藏文字、虚假评价、批量重复落地页或购买垃圾外链。公开说明对普通访问者与爬虫一致；不根据 User-Agent 提供不同正文。不修改模型广场的登录开关，也不暴露账户信息。

## 上线前基线

2026-09-10 只读 HTTP 检查发现：

- 首页响应主要为 SPA 外壳，缺少描述与 canonical，正文依赖 JavaScript。
- `/robots.txt`、`/sitemap.xml` 和随机不存在路径都返回首页 HTML / HTTP 200。
- 部分别名域也提供相同页面，需要统一 canonical，避免信任任意请求 Host。

## 本轮实现范围

- 固定可信主域 `https://modelport.top`，仅 ModelPort 站点且非后台模式启用公开 SEO。
- 真实文本 robots、XML sitemap；地图只列首页和明确的公开指南。
- 7 个直接返回完整 HTML 的页面：`/learn`、`/learn/api-relay`、`/learn/pricing`、`/learn/codex`、`/learn/claude-code`、`/learn/group-buy`、`/learn/lottery`。
- 标题、描述、canonical、Open Graph、与可见内容一致的结构化数据。
- 原 SPA 首页保留；无自定义首页时加入可见介绍及普通链接。公开指南不加载前端框架、外部字体或图片，采用轻量响应式排版。
- 私有 SPA 路径标记 noindex；未知路径返回真实 404；API 鉴权和权限保持原样。
- 价格文章解释算法，不发布个性化价格，不把过去截图当作永久报价。活动说明不公开概率与奖池。

## 发布后的验收清单

1. 浏览器和无 JavaScript 的 HTTP 客户端均能看到公开指南正文。
2. `/robots.txt` 返回 200 / text/plain，`/sitemap.xml` 返回 200 / XML；地图每个 URL 均可访问。
3. 首页与 `/home` canonical 归一至主域根路径；查询参数和恶意 Host 不进入 canonical。
4. 任意不存在路径与不存在静态资源返回 404，不是首页 200。
5. `/keys`、`/usage`、`/profile`、`/admin/dashboard` 等私有路径仍鉴权且 noindex；公开 HTML 不含密钥、邮箱、余额或个人价格。
6. 原首页、登录、OAuth 回调、支付、模型广场、后台、API 请求不发生路由回归。
7. 如 CDN 或反向代理另有 HTML 缓存，确认不覆盖新响应状态、robots 或内容类型；不得缓存带个人数据的接口。

## Google Search Console：需站点所有者参与

代码发布不等于提交收录。使用站点所有者的 Google 账户进入 Search Console，添加 `modelport.top` 网域资源并按其提供的 DNS TXT 记录验证所有权；没有验证值时不能编造或擅自修改 DNS。

验证后提交 `https://modelport.top/sitemap.xml`，使用 URL 检查工具分别检查首页和指南页，查看实时抓取、Google 选定的 canonical、索引许可和最终渲染正文。对核心页面请求编入索引；请求不是收录或排名保证。不要重复批量提交同一 URL。

目前没有确认 Search Console 所有权、提交结果、索引报告或搜索表现数据。上线后应由实际报告确认，不以 `site:` 搜索结果代替索引报告。

## 本地验证记录

- 前端 SEO 单元测试 6 项、路由测试 52 项通过，TypeScript 检查通过。
- `pnpm build` 通过；既有大 chunk 与 Browserslist 数据过期提示未在本轮修改依赖解决。
- `go test -tags embed ./internal/web` 和对应 `-race` 检查通过。
- `go test ./internal/web` 非嵌入版本可编译（该版本无测试）；`go build -tags embed ./cmd/server` 通过。
- 7 页正文、内部指南链接和 JSON-LD 解析有自动化验证；已在浏览器检查指南模板的实际排版。
- SEO 实现轮次未部署；随后于 2026-09-10 随 v0.2.4 部署上线，应用提交 `34f71d10b`。线上首页、7 个指南、robots、sitemap、canonical、404、私有页 noindex 及 API 鉴权检查通过，详见 `release-modelport-20260910-v0.2.4.md`。尚未向 Search Console 提交站点地图。

## 后续内容与衡量

- 每次真实功能/规则更新同步对应指南，只有内容实质变化才修改日期。
- 基于真实问题补充错误码、经过验证的客户端配置与计费案例，不复制厂商文档和其他站点介绍。
- 从可公开访问的产品入口增加相关指南链接，保持所有重要页面可由普通 `<a>` 到达。
- 结合 Search Console 的查询词、展示量、点击量、CTR、平均排名和页面索引情况评估，不只观察单次手动搜索。
- 将品牌词、API 中转类词与物流等无关词分开分析；按照有用信息和真实转化改进标题与内容。
- 争取真实用户案例和相关技术社区的自然引用，避免伪造第三方背书与批量购买链接。
- 别名域的网页 301 可以在单独评估后实施；不要对 API 地址全站重定向，以免破坏客户端 POST 请求。

## GEO 说明与来源

Google 说明：AI 搜索功能仍以基础 SEO、可收录内容和可靠信息为基础，没有保证引用的特殊标记；不需要通过额外“AI 文本文件”获得资格。结构化数据用于帮助理解，不保证富媒体结果或排名。

- [Google AI 功能与网站](https://developers.google.com/search/docs/appearance/ai-features)
- [Google AI 搜索优化说明](https://developers.google.com/search/docs/fundamentals/ai-optimization-guide)
- [Google Search Essentials](https://developers.google.com/search/docs/essentials)
- [Codex 官方高级配置](https://learn.chatgpt.com/docs/config-file/config-advanced)
