export type GuideLocale = 'zh' | 'en'

export interface GuideAction {
  label: string
  href: string
  external?: boolean
  primary?: boolean
}

export interface GuideStep {
  title: string
  description: string
  code?: string
  note?: string
}

export interface GuideArticle {
  slug: string
  category: 'start' | 'downloads' | 'configuration' | 'troubleshooting'
  title: string
  summary: string
  duration: string
  os?: 'macOS' | 'Windows' | 'All'
  keywords: string[]
  requirements: string[]
  steps: GuideStep[]
  success: string[]
  troubleshooting?: Array<{ title: string; solution: string }>
  actions: GuideAction[]
}

export interface GuideCategory {
  id: GuideArticle['category']
  title: string
  description: string
}

const zhCategories: GuideCategory[] = [
  { id: 'start', title: '快速开始', description: '从创建密钥到完成第一次请求' },
  { id: 'downloads', title: '客户端下载', description: 'Codex 桌面版与 CC Switch' },
  { id: 'configuration', title: '中转站配置', description: '选择分组并连接 ModelPort' },
  { id: 'troubleshooting', title: '故障排查', description: '常见错误与恢复方法' }
]

const enCategories: GuideCategory[] = [
  { id: 'start', title: 'Quick start', description: 'Create a key and send your first request' },
  { id: 'downloads', title: 'Downloads', description: 'Codex desktop and CC Switch' },
  { id: 'configuration', title: 'Configuration', description: 'Connect your client to ModelPort' },
  { id: 'troubleshooting', title: 'Troubleshooting', description: 'Common errors and recovery steps' }
]

const zhArticles: GuideArticle[] = [
  {
    slug: 'quick-start',
    category: 'start',
    title: '5 分钟发出第一条请求',
    summary: '创建 API 密钥、选择分组，并在客户端中完成一次最小验证。',
    duration: '约 5 分钟',
    os: 'All',
    keywords: ['新手', 'API Key', '分组', '第一次请求'],
    requirements: ['已登录 ModelPort', '账户有可用余额或有效订阅', '已安装 Codex 桌面版或 CC Switch'],
    steps: [
      { title: '创建 API 密钥', description: '进入“API 密钥”页面创建一个新密钥。建议按设备或用途命名，后续更容易停用和排查。' },
      { title: '选择可用分组', description: '在密钥配置中选择与你要使用的模型匹配的分组。若不确定，先选择站点标记为推荐的通用分组。' },
      { title: '选择配置方式', description: 'Codex 桌面版使用“使用密钥”生成配置；CC Switch 用户可直接选择“一键导入”。不要把真实密钥粘贴到公开聊天或截图中。' },
      { title: '完全重启客户端', description: '保存配置后彻底退出客户端，再重新打开。仅关闭窗口可能不会重新加载配置。' },
      { title: '发送最小测试消息', description: '新建任务并发送一句简短消息，例如“只回复 ok”。确认能够开始输出，再进行正式工作。' }
    ],
    success: ['客户端成功返回文本', '站点“使用记录”中出现本次请求', '请求使用的模型和分组符合预期'],
    troubleshooting: [
      { title: '返回 401', solution: '检查密钥是否完整、是否已启用，并确认配置文件没有多余空格。' },
      { title: '返回 429', solution: '检查余额、订阅额度、并发限制与分组限速。' }
    ],
    actions: [
      { label: '去创建 API 密钥', href: '/keys', primary: true },
      { label: '查看使用记录', href: '/usage' }
    ]
  },
  {
    slug: 'codex-desktop-macos',
    category: 'downloads',
    title: 'Codex 桌面版：macOS',
    summary: '下载官方桌面应用，在应用内进入 Codex，再连接 ModelPort。',
    duration: '约 6 分钟',
    os: 'macOS',
    keywords: ['Codex', 'macOS', 'Mac', '桌面版', '下载安装'],
    requirements: ['一台受支持的 Mac', '可登录的 ChatGPT 账号，或应用支持的 API Key 登录方式', '一个可用的 ModelPort API 密钥'],
    steps: [
      { title: '从 OpenAI 官方页面下载', description: '打开官方桌面应用页面，下载 macOS 版本。ModelPort 不提供安装包镜像，也不会要求你从网盘下载。' },
      { title: '安装并首次启动', description: '完成安装后打开桌面应用。macOS 若提示权限请求，请按实际功能需要授权；不确定时可以先保留较严格的权限。' },
      { title: '登录并选择 Codex', description: '登录后，在应用的产品选择器中选择 Codex。官方界面名称可能随版本调整，以应用内当前选项为准。' },
      { title: '打开一个测试项目', description: '选择一个不含敏感资料的测试文件夹，创建新任务，先确认桌面应用本身能够正常工作。' },
      { title: '接入 ModelPort', description: '回到站点“API 密钥”页面，打开“使用密钥”，复制 Codex 配置。保存到对应配置目录后，完全退出并重新打开桌面应用。', note: '“官方下载”和“接入 ModelPort”是两个独立步骤；ModelPort 不修改 OpenAI 官方客户端。' },
      { title: '验证新任务', description: '重新创建一个任务并发送“只回复 ok”。成功输出后，再到使用记录核对模型与计费。' }
    ],
    success: ['桌面应用能够正常启动并登录', '可以在应用内选择 Codex', '接入后新任务能正常输出且站点产生使用记录'],
    troubleshooting: [
      { title: '配置后仍走旧地址', solution: '彻底退出桌面应用后重新打开，并确认修改的是当前用户的配置目录。' },
      { title: '应用无法读取项目', solution: '在 macOS 系统设置中检查文件与文件夹权限，只授权需要使用的目录。' }
    ],
    actions: [
      { label: '打开 OpenAI 官方下载页', href: 'https://learn.chatgpt.com/docs/app', external: true, primary: true },
      { label: '配置 ModelPort 密钥', href: '/keys' }
    ]
  },
  {
    slug: 'codex-desktop-windows',
    category: 'downloads',
    title: 'Codex 桌面版：Windows',
    summary: '使用官方安装器或 winget 安装，并优先从 Windows 原生环境开始。',
    duration: '约 8 分钟',
    os: 'Windows',
    keywords: ['Codex', 'Windows', '桌面版', 'winget', 'PowerShell', 'WSL'],
    requirements: ['Windows 电脑', 'Microsoft Store 服务或可用的官方安装器', '一个可用的 ModelPort API 密钥'],
    steps: [
      { title: '下载安装桌面应用', description: '优先使用 OpenAI 官方 Windows 页面提供的安装器。受管设备可让管理员按官方企业部署说明安装。' },
      { title: '安装器不可用时使用 winget', description: '在 PowerShell 中运行官方命令。它会从 Microsoft Store 源安装桌面应用。', code: 'winget install --id 9PLM9XGG6VKS -s msstore' },
      { title: '登录并选择 Codex', description: '首次使用建议保持“Windows 原生”智能体环境。只有项目明确位于 WSL2 中时，再切换到 WSL；WSL2 不是普通用户的必选项。' },
      { title: '准备推荐工具', description: 'Git 会影响代码审查等功能，建议安装。Node.js、Python、.NET SDK 和 GitHub CLI 可按项目需要补充，不阻塞首次测试。' },
      { title: '接入 ModelPort', description: '在“API 密钥 → 使用密钥”中复制 Windows 配置，并确认配置写入当前用户目录。', code: '%USERPROFILE%\\.codex' },
      { title: '完全重启并验证', description: '从系统托盘彻底退出桌面应用，再重新打开并新建任务。发送“只回复 ok”，然后核对站点使用记录。' }
    ],
    success: ['桌面应用能正常启动并进入 Codex', 'Windows 原生环境能打开本地项目', 'ModelPort 请求能正常输出并产生使用记录'],
    troubleshooting: [
      { title: 'winget 找不到软件包', solution: '更新 App Installer 和 Microsoft Store 源，或改用官方网页安装器。' },
      { title: 'Git 功能不可用', solution: '在 Windows 原生环境安装 Git，然后彻底重启桌面应用。' },
      { title: 'WSL 与 Windows 配置不一致', solution: '优先完成 Windows 原生配置；如必须使用 WSL2，再单独同步 .codex 目录。' }
    ],
    actions: [
      { label: '打开 Windows 官方下载页', href: 'https://learn.chatgpt.com/docs/windows/windows-app', external: true, primary: true },
      { label: '配置 ModelPort 密钥', href: '/keys' }
    ]
  },
  {
    slug: 'ccswitch-download',
    category: 'downloads',
    title: '下载并安装 CC Switch',
    summary: '从官方渠道安装 CC Switch，为一键导入供应商配置做准备。',
    duration: '约 5 分钟',
    os: 'All',
    keywords: ['CC Switch', 'ccswitch', '下载安装', 'GitHub Releases'],
    requirements: ['Windows、macOS 或 Linux 电脑', '能够访问 CC Switch 官方发行页'],
    steps: [
      { title: '进入官方发行页', description: '只使用 CC Switch 官网、官方 GitHub 仓库或 Releases。不要安装来源不明的二次打包版本。' },
      { title: '选择系统对应安装包', description: '根据你的操作系统和处理器架构选择安装包。版本号以官方最新稳定版为准。' },
      { title: '首次启动', description: '启动后检查供应商列表是否能够正常显示。macOS 安全提示和 Windows 协议弹窗应按系统提示处理。' },
      { title: '返回 ModelPort 导入', description: '打开“API 密钥”页面，选择目标密钥后点击 CC Switch 一键导入。浏览器询问是否打开 CC Switch 时选择允许。' }
    ],
    success: ['CC Switch 能正常启动', '浏览器能够询问是否打开 ccswitch:// 链接', '导入确认后供应商列表出现 ModelPort 配置'],
    troubleshooting: [
      { title: '点击一键导入无反应', solution: '确认 CC Switch 已启动且系统已注册 ccswitch:// 协议；不行时使用弹窗中的手动配置。' }
    ],
    actions: [
      { label: '打开 CC Switch 最新发行版', href: 'https://github.com/farion1231/cc-switch/releases/latest', external: true, primary: true },
      { label: '去一键导入', href: '/keys' }
    ]
  },
  {
    slug: 'relay-configuration',
    category: 'configuration',
    title: '配置 ModelPort 中转站',
    summary: '理解 API 地址、密钥、模型和分组之间的关系。',
    duration: '约 4 分钟',
    os: 'All',
    keywords: ['中转站', 'Base URL', 'API Key', '模型', '分组'],
    requirements: ['一个已启用的 API 密钥', '对应分组有可用模型', '账户余额或订阅额度充足'],
    steps: [
      { title: '确认 API 地址', description: '使用密钥弹窗会根据当前站点地址生成客户端配置。不要自行把路径重复拼接成 /v1/v1。' },
      { title: '确认密钥与分组', description: '密钥决定权限和可用分组；分组决定可请求的模型、倍率、并发和限速。' },
      { title: '使用实际模型 ID', description: '模型名称必须与模型广场或密钥弹窗显示一致。展示名称不一定等于请求中的模型 ID。' },
      { title: '保存并重启客户端', description: '应用可能只在启动时读取配置。保存后完全退出，再新建任务验证。' },
      { title: '核对使用记录', description: '请求成功后，在使用记录中核对模型、Token、缓存与扣费。发现异常时先停用对应密钥。' }
    ],
    success: ['请求地址没有重复路径', '使用记录中的分组和模型正确', '输入、输出与缓存计费均可追踪'],
    actions: [
      { label: '打开 API 密钥', href: '/keys', primary: true },
      { label: '查看模型广场', href: '/model-plaza?embedded=1' },
      { label: '查看使用记录', href: '/usage' }
    ]
  },
  {
    slug: 'ccswitch-configuration',
    category: 'configuration',
    title: 'CC Switch 一键导入',
    summary: '把当前密钥和可用模型安全地导入 CC Switch。',
    duration: '约 3 分钟',
    os: 'All',
    keywords: ['CC Switch', '一键导入', '供应商', 'ccswitch://'],
    requirements: ['已安装并启动 CC Switch', '浏览器允许打开外部应用', '一个可用的 ModelPort 密钥'],
    steps: [
      { title: '选择需要使用的密钥', description: '进入 API 密钥页面，在目标密钥的操作菜单中选择 CC Switch 导入。每个设备建议使用独立密钥。' },
      { title: '检查导入预览', description: '确认应用类型、供应商名称、API 端点和被遮蔽的密钥。不要在截图中暴露完整密钥。' },
      { title: '允许浏览器打开 CC Switch', description: '点击导入后，浏览器会请求打开外部应用。选择允许，并在 CC Switch 中再次确认。' },
      { title: '启用新供应商', description: '在 CC Switch 供应商列表中找到刚导入的 ModelPort 配置，确认它已启用并被目标客户端使用。' },
      { title: '完全重启目标客户端', description: '退出 Codex 或 Claude Code 等目标客户端并重新打开，然后创建新任务测试。' }
    ],
    success: ['CC Switch 供应商列表出现新配置', '目标客户端重启后使用新供应商', '测试请求正常输出并出现在使用记录中'],
    troubleshooting: [
      { title: '确认后没有新增配置', solution: '升级 CC Switch 到最新稳定版，重新注册协议；仍失败时改用导入弹窗提供的手动配置。' },
      { title: '客户端仍使用旧供应商', solution: '在 CC Switch 中显式启用新供应商，并完全退出目标客户端后重试。' }
    ],
    actions: [
      { label: '选择密钥并导入', href: '/keys', primary: true },
      { label: '查看使用记录', href: '/usage' }
    ]
  },
  {
    slug: 'common-errors',
    category: 'troubleshooting',
    title: '常见错误快速排查',
    summary: '按 401、403、404、429、502 和流中断快速定位问题。',
    duration: '约 4 分钟',
    os: 'All',
    keywords: ['401', '403', '404', '429', '502', 'stream disconnected', '首字延迟'],
    requirements: ['记录错误发生时间', '保留请求 ID（如有）', '提交截图前遮蔽密钥、邮箱与请求正文'],
    steps: [
      { title: '401：密钥无效', description: '确认密钥完整、已启用、未过期，并检查 Authorization 格式。' },
      { title: '403：权限或上游拒绝', description: '确认密钥允许当前分组和模型；若多用户同时发生，再检查上游账号状态。' },
      { title: '404：模型或路径不存在', description: '核对模型 ID 和 API 路径，特别注意不要重复添加 /v1。' },
      { title: '429：额度或限速', description: '检查余额、订阅额度、RPM/TPM、并发限制。短时间重试不要无限循环。' },
      { title: '502：网关或上游连接失败', description: '记录准确时间并重试一次。仅单个用户发生时同时排查本地网络与 CC Switch；大面积发生时检查站点上游监控。' },
      { title: '流中断或首字延迟高', description: '区分首次连接耗时与生成耗时，使用新任务做最小请求，并对比不同分组。' }
    ],
    success: ['能够判断问题属于客户端、站点还是上游', '提交给客服的信息不包含敏感数据', '恢复后新任务能够持续输出'],
    actions: [
      { label: '查看使用记录', href: '/usage', primary: true },
      { label: '检查可用模型', href: '/model-plaza?embedded=1' }
    ]
  }
]

const enArticles: GuideArticle[] = zhArticles.map((article) => ({
  ...article,
  title: article.slug === 'codex-desktop-macos'
    ? 'Codex desktop for macOS'
    : article.slug === 'codex-desktop-windows'
      ? 'Codex desktop for Windows'
      : article.title,
  summary: article.slug.startsWith('codex-desktop')
    ? 'Install the official desktop app, open Codex, and connect ModelPort.'
    : article.summary
}))

export function getGuideCatalog(locale: string) {
  const language: GuideLocale = locale.toLowerCase().startsWith('zh') ? 'zh' : 'en'
  return {
    categories: language === 'zh' ? zhCategories : enCategories,
    articles: language === 'zh' ? zhArticles : enArticles
  }
}
