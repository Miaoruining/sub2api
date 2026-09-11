# ModelPort 首屏与 Logo 性能优化

## 变更

2026-09-11 从504ccf4fc创建fix/web-load-performance，保留此前静默上下文归档、订阅补充包及API协议灰度实现。

- 42b53493a：Airwallex SDK及其airtracker依赖独立分包，保持支付页动态加载；为尚未挂载的#app预留首屏，避免下方公开简介先占据页首、随后被应用推走。
- d2df1024ca83482559e9bb7cd39ed113b8fa6e5d：PNG/JPEG Logo自动生成256px与48px PNG缩略图，保留原数据和原图路由。服务端favicon标记对应的应用Logo，前端启动与监听器不会把小favicon覆盖成大Logo；用户更换Logo时正常更新。

缩略图路径为`/branding/logo/<原图SHA256>-v1-256.png`和`...-v1-48.png`。只在派生图显著较小时使用，不放大，保持比例与透明度。GIF/WebP等不派生，继续原图短URL；SVG和外链保持原行为。有输入大小/像素预算、有界缓存和设置失效机制。若今后修改编码算法，需提升路径版本，保证immutable缓存语义。

## 验证

- 后端web完整测试与Branding目标race测试通过。
- 前端完整构建（i18n、类型检查、Vite）通过；Airwallex/Stripe相关7项与branding3项回归通过，相关ESLint通过。
- 线上原图1254×1254、193382B；应用图256×256、25896B；favicon48×48、2256B。两种派生资源合计比原图少85.44%。
- 源站与www CDN的图片GET/HEAD/304、错误hash404、原图SHA、三域名health，共30项公网验证通过。两种派生图均观察到CDN HIT。
- 独立Chrome冷启动观测：总传输693012B→440421B（减少36.45%），首页第三方支付请求归零，CLS1.00→0。
- 单次LCP755ms→915ms，未证明LCP时间提升；首次可见内容也因预留首屏而改变。这些是本机实验室观测，不代表全国三网或真实用户总体。此次收益是减少传输、移除无关请求和消除布局位移，不能表述为“访问速度提升36%”。
- 手机390px视口无横向溢出，线上Logo正常显示，favicon保持48px。

## 发布

镜像：`mrn666/sub2api:deploy-d2df1024c-amd64@sha256:30037442879b13956a07c734d42713650c6243ad2e4ceb37b60e14c70114c002`。

服务器发布工具：`/usr/local/libexec/modelport-release`，SHA256 `8d73cbf3523644270fd0e4eff1272f25baf3764e7453026fd0eb6eb0fc521b3a`。发布ID：`web-perf-d2df1024c`。备份位于`/opt/modelport/deploy/modelport-releases/web-perf-d2df1024c/`，含配置和数据库备份；不公开备份内容。

回退基线：`mrn666/sub2api:deploy-783407234-amd64@sha256:ef77ff7b9f5994fa5b117ade09ef488e355ec91251c87a8e13f374af7c7a477a`。回退应复用分阶段发布流程部署旧镜像，保留数据库，不整库恢复。

发布已finalized，正式端口8080提供服务、候选容器已停止；promote/finalize各等待2条连接自然结束后重试通过。17模型分组校验一致，数据库/Redis未重启，ModelPass 5080未触及。应用重建会影响连接池冷启动，API协议试验对照需要把此发布时间作为边界。

## 后续验证

今晚20:30安排一次公开三网配对复测。无ICP、月费尽量0且上限30元的约束保持不变；本轮没有购买服务或修改DNS，等待晚高峰证据再决定运营商分流。API沿用既有小范围HTTP/1.1试验，不由网页CDN结果外推SSE性能。

详细原始证据保存在项目根目录`artifacts/site-optimization-phase2/`，包括前后trace、Resource Timing、公开HTTP验证、构建日志与API仅聚合诊断。
