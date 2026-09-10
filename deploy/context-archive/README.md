# 每日上下文归档运维

本目录提供一个 macOS 本机编排器、生产机 SSH forced-command helper 和
LaunchAgent 模板。它只处理生产 PostgreSQL 独立归档表
`public.request_context_archives`，不依赖 `prompt_audit_jobs`/`prompt_audit_events`，
不读取线上服务接口，不停止或重启线上服务，也不修改后端代码。

## 安全不变量

归档的唯一删除顺序是：

```text
snapshot(from_id)
  -> export(from_id, to_id)
  -> gzip/分片落盘
  -> SHA-256、gzip、CSV header、行数、首末 id、排序和范围校验
  -> APFS sparsebundle 安全卸载
  -> delete(from_id, to_id, expected_count)
```

任何一步失败都返回临时失败并保留远端数据。外接盘未挂载、卷 UUID 不匹配、SSH
不可达、空间不足、Keychain 不可用、gzip/哈希/CSV 校验失败或 sparsebundle 无法
安全卸载时，都不会调用远端 `delete`。远端删除在一个数据库事务内锁定候选行，
只有精确行数等于 `expected_count` 才删除；断线重试时同一范围已为空会返回
`ALREADY_EMPTY`，因此不会重复删除。

日志只写时间、阶段、状态、范围、数量和字节数等元数据；不会写 CSV 行、提示词、
SSH/psql 输出或异常文本。状态、锁和日志位于本机 APFS 的
`~/Library/Application Support/ModelPort/context-archive/`，权限分别为目录 700、文件 600。

## 文件

| 文件 | 用途 |
| --- | --- |
| `context-archive.py` | 本机调度、Keychain/sparsebundle、压缩、校验和删除编排；仅 Python 3.9 标准库 |
| `context-archive-remote.py` | 生产机 forced-command；只允许 `snapshot`、`export`、`delete` |
| `context-archive-psql` | 生产机固定 PostgreSQL 容器通道，不接受客户端参数 |
| `sshd_config_context_archive` | 独立 22222 端口、仅公钥的 SSHD 配置 |
| `context-archive-sshd.service` | 独立 SSHD 的 systemd 单元 |
| `context-archive.sudoers` | 只允许专用用户执行固定 forced-command helper |
| `com.modelport.context-archive.plist` | 每 30 分钟唤醒的 LaunchAgent 模板 |

## 与现有运维习惯和 macOS 工具的衔接

现有 `deploy` 备份脚本采用 `set -euo pipefail`、`umask 077`、`gzip -9`、`gzip -t`
和 `shasum -a 256`；本实现保留“严格失败即退出、压缩后校验、SHA-256”的习惯，
并因需要跨进程流式分片而改用 Python `hashlib`、显式 `0600` 和本机 `flock`。
现有 release 脚本的原子写入/fsync 习惯也用于状态与 manifest。当前 macOS 提供
`/usr/bin/python3` 3.9.6、`ssh`、`hdiutil`、`diskutil`、`df`、`gzip`、`shasum`、
`lockf`、`launchctl` 和 `plutil`；本实现只依赖其中的 Python 3.9 标准库加
`hdiutil`/`diskutil`/`ssh`/`gzip`。本机没有 `shellcheck`，且交付物是 Python，故以
`py_compile` 和 `plutil -lint` 验证。

## 外接盘和加密容器

当前目标是 `/Volumes/MIAO/AI 数据`，并校验卷 UUID
`6A660EF0-8100-3756-9152-6AED7A90C07E`。该卷是 FAT32，不能把 FAT32 文件权限当作
访问控制，因此本程序不会把明文归档、临时 gzip 或 manifest 写在 FAT32 根目录。
唯一外部对象是：

```text
/Volumes/MIAO/AI 数据/context-archive.sparsebundle/
```

首次运行时由 `hdiutil create -type SPARSEBUNDLE -fs APFS -encryption AES-256`
创建 900 GB 的稀疏包；归档实际写入其中的 APFS 卷
`batches/<from-id>-<to-id>/`。单个 gzip 分片上限 512 MiB，避免 FAT32 的单文件
上限。程序会通过 `hdiutil imageinfo` 检查 sparsebundle/AES-256，并通过
`diskutil` 检查已挂载的内部文件系统为 APFS。

密码必须预先存入 macOS Keychain Generic Password，Service 默认
`com.modelport.context-archive`、Account 默认 `archive`（可在本机配置中改名）。
请用“钥匙串访问”建立该条目，或在交互式安全终端中录入；不要将密码放入仓库、
LaunchAgent、shell history、环境变量或外接盘。LaunchAgent 运行时若登录钥匙串
锁定，任务会 fail closed，解锁后下一个 30 分钟周期会重试。

## 本机配置和接口

先创建本机目录和权限：

```sh
mkdir -p "$HOME/Library/Application Support/ModelPort/context-archive"
chmod 700 "$HOME/Library/Application Support/ModelPort/context-archive"
```

创建 `~/Library/Application Support/ModelPort/context-archive/config.json`，内容只放连接元数据，
然后执行 `chmod 600`：

```json
{
  "ssh_target": "archive@prod.example",
  "ssh_port": 22222,
  "known_hosts": "/Users/mrn/.ssh/known_hosts",
  "identity_file": "/Users/mrn/.ssh/context_archive_ed25519",
  "remote_helper": "/usr/local/libexec/context-archive-remote",
  "keychain_service": "com.modelport.context-archive",
  "keychain_account": "archive"
}
```

`ssh_target` 只接受无空格的 `user@host`/SSH alias；`known_hosts` 必须使用严格主机
密钥校验，程序强制 `BatchMode`、连接超时和 `-T`，不接受交互式密码。若使用 SSH
agent，可省略 `identity_file`。配置不存放在本目录，避免把部署环境写入版本库。

支持的接口：

```sh
/usr/bin/python3 "/Users/mrn/Library/Application Support/ModelPort/context-archive/context-archive.py" dry-run
/usr/bin/python3 "/Users/mrn/Library/Application Support/ModelPort/context-archive/context-archive.py" run
/usr/bin/python3 "/Users/mrn/Library/Application Support/ModelPort/context-archive/context-archive.py" status
```

`dry-run` 会取得只含计数、范围和字节数的 snapshot，不创建容器、不读取 Keychain、
不导出、不写入归档、不删除远端；适合先验证卷、known_hosts 和 SSH。Keychain 读取
只发生在实际 `run` 的 attach/create 阶段。`run` 每个北京时间
自然日最多成功一次，默认 03:30 之后执行；当天此前被睡眠/关机错过时，登录后的
`RunAtLoad` 或下一个周期会补跑。已有未完成 batch 时不受时间门控影响，会优先续跑。
成功 batch 永久保留，未提供本地保留期删除功能。

## 生产机 helper 安装

将 `context-archive-remote.py` 安装为生产机的
`/usr/local/libexec/context-archive-remote`，root 拥有、模式 0755。它内置的
`PSQL` 指向 `/usr/local/libexec/context-archive-psql`；该 wrapper 也应 root 拥有、
模式 0755，使用固定的 `/usr/bin/psql -X -qAt -v ON_ERROR_STOP=1 --no-password`
和生产数据库的 Unix socket/数据库名。wrapper 不应接受或拼接来自客户端的参数，
密码使用生产机 root-only `.pgpass`（0600）或本地认证，不放入 helper、仓库或 SSH
参数。若生产机 psql 路径不同，只在远程受控部署时调整 wrapper，不要让客户端传入
可变 SQL/DSN。

推荐为专用只读/删除角色建立固定权限：

* `SELECT`：`public.request_context_archives` 的 `id, request_id, protocol, model, stage, context_payload, content_hash, message_count, created_at`；
* `DELETE`：`public.request_context_archives` 精确范围内的行（如采用固定 root-owned `SECURITY DEFINER` 删除函数，则 helper 只需执行该函数）；
* 不授予任意表的 DDL、超级用户、复制或线上业务写权限。

按部署后的绝对路径配置 `authorized_keys`，例如：

```text
command="/usr/local/libexec/context-archive-remote",no-pty,no-agent-forwarding,no-port-forwarding,no-X11-forwarding ssh-ed25519 AAAA... context-archive
```

`SSH_ORIGINAL_COMMAND` 只接受以下严格形式（数字为十进制、无前导命令注入）：

```text
snapshot <from_id>
export <from_id> <to_id>
delete <from_id> <to_id> <expected_count>
```

也兼容客户端传入固定 helper 前缀的形式；不会接受 `psql`、shell、管道、重定向、
额外参数或任意 SQL。snapshot 使用同一 `REPEATABLE READ READ ONLY` 事务得到
归档表的高水位；export 只输出固定列并按 `a.id ASC` 排序，并在生产机 helper
内先通过 `gzip -9n` 压缩后才写入 SSH stdout（本机不接收明文 CSV）。delete 使用同一
事务的临时候选表和 `FOR UPDATE`，精确检查范围和期望数量后再删除。新插入的归档行
ID 不会落入已经固定的高水位范围；JSONB 的 `context_payload` 由 PostgreSQL 的
CSV writer 正确引用/转义，客户端再按 UTF-8 CSV 解析。

## LaunchAgent

审阅并按当前用户路径安装模板：

```sh
cp /Users/mrn/Documents/项目/中转站/sub2api/deploy/context-archive/com.modelport.context-archive.plist \
  "$HOME/Library/LaunchAgents/com.modelport.context-archive.plist"
plutil -lint "$HOME/Library/LaunchAgents/com.modelport.context-archive.plist"
launchctl bootstrap "gui/$(id -u)" "$HOME/Library/LaunchAgents/com.modelport.context-archive.plist"
```

模板每 1800 秒唤醒一次，标准输出和错误均为 `/dev/null`，不让 launchd 创建默认
0644 日志；脚本自己的日志为 600。卸载时使用对应的 `launchctl bootout`，不会触碰
线上服务。

## 故障和恢复

| 条件 | 行为 |
| --- | --- |
| `/Volumes/MIAO` 未挂载或 UUID/挂载点不符 | 失败，不创建/删除任何远端数据 |
| FAT32 可用空间低、APFS 内部空间低或 Keychain 锁定 | 失败，不 delete |
| SSH/导出/gzip/CSV/哈希/首末 ID/行数失败 | batch 不封存或保留未完成状态，下个周期重试，不 delete |
| sparsebundle 无法 attach/detach | 失败；不使用 `hdiutil -force`，不 delete |
| delete SSH 断线 | 状态保持 pending；下次以相同范围重试，远端已提交时得到 `ALREADY_EMPTY` |
| 进程在封存后崩溃 | 下次按确定 batch id 重新校验 manifest，不重复导出；校验成功且安全卸载后才 delete |
| 本机锁已被占用 | 当前运行退出，避免并发导出/删除 |

## 检查

本目录脚本使用 Python 3.9 语法和标准库；可以运行：

```sh
/usr/bin/python3 -m py_compile \
  /Users/mrn/Documents/项目/中转站/sub2api/deploy/context-archive/context-archive.py \
  /Users/mrn/Documents/项目/中转站/sub2api/deploy/context-archive/context-archive-remote.py
plutil -lint /Users/mrn/Documents/项目/中转站/sub2api/deploy/context-archive/com.modelport.context-archive.plist
```

仓库当前机器没有 `shellcheck`；两个可执行文件均为 Python，不适用 shellcheck。
首次真实运行前必须使用 `dry-run`，并在维护窗口用测试 SSH key 验证三种动作和
`COUNT_MISMATCH` 分支。不要在生产机上用线上业务账号或生产数据做“清理测试”。
