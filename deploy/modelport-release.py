#!/usr/bin/env python3
"""ModelPort 的分阶段、可审查 Docker 发布辅助工具。

本工具只针对固定的 ModelPort 单节点目录工作，必须以 root 运行。它不读取或
打印 .env 的内容，不接受任意路径，不执行数据库回滚，也不会触碰 127.0.0.1:5080。
每个阶段把无密钥状态写入 /opt/modelport/deploy/modelport-releases/<release-id>/，
便于重试和人工审查。

典型流程（image 必须是完整的 registry digest 引用）：

  modelport-release.py backup \
    --release-id 34f71d10b --image mrn666/sub2api:deploy-34f71d10b-amd64@sha256:... \
    --commit 34f71d10bece5fa341ec405d67ef5db0972ff256
  modelport-release.py candidate --release-id 34f71d10b
  modelport-release.py switch-candidate --release-id 34f71d10b
  modelport-release.py promote --release-id 34f71d10b
  modelport-release.py finalize --release-id 34f71d10b
  modelport-release.py status --release-id 34f71d10b

只依赖 Python 标准库和宿主机上的 docker/caddy/nsenter/ss/pg_restore。
"""

from __future__ import annotations

import argparse
import datetime as _dt
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import stat
import subprocess
import sys
import tempfile
import time
from typing import Any, Mapping, Sequence
from urllib.error import URLError
from urllib.request import Request, build_opener, ProxyHandler


# 固定 ModelPort 生产路径；不提供路径覆盖参数，避免误操作其他服务。
MODELPORT_ROOT = Path("/opt/modelport")
DEPLOY_DIR = MODELPORT_ROOT / "deploy"
COMPOSE_FILE = DEPLOY_DIR / "docker-compose.local.yml"
ENV_FILE = DEPLOY_DIR / ".env"
DATA_DIR = DEPLOY_DIR / "data"
CONFIG_FILE = DATA_DIR / "config.yaml"
CADDY_FILE = Path("/etc/caddy/Caddyfile")
RELEASES_DIR = DEPLOY_DIR / "modelport-releases"

APP_CONTAINER = "sub2api"
POSTGRES_CONTAINER = "sub2api-postgres"
REDIS_CONTAINER = "sub2api-redis"
CANDIDATE_PORT = 8081
APP_PORT = 8080
OTHER_APP_PORT = 5080
DATABASE_USER = "sub2api"
DATABASE_NAME = "sub2api"

RELEASE_ID_RE = re.compile(r"^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$")
COMMIT_RE = re.compile(r"^[0-9a-fA-F]{7,64}$")
IMAGE_RE = re.compile(
    r"^[A-Za-z0-9](?:[A-Za-z0-9._:/-]*[A-Za-z0-9])?@sha256:[0-9a-f]{64}$"
)
ENDPOINT_8080 = "127.0.0.1:8080"
ENDPOINT_8081 = "127.0.0.1:8081"
ENDPOINT_5080 = "127.0.0.1:5080"


class ReleaseError(RuntimeError):
    """可安全显示给操作员的错误；不携带命令输出或密钥。"""


class CommandError(ReleaseError):
    pass


def utc_now() -> str:
    return _dt.datetime.now(_dt.timezone.utc).replace(microsecond=0).isoformat()


def validate_release_id(value: str) -> str:
    if not RELEASE_ID_RE.fullmatch(value):
        raise ReleaseError("release-id 只允许 1-64 位字母、数字、点、下划线和短横线")
    return value


def validate_commit(value: str) -> str:
    if not COMMIT_RE.fullmatch(value):
        raise ReleaseError("commit 必须是 7-64 位十六进制 Git commit")
    return value.lower()


def validate_image(value: str) -> str:
    if not IMAGE_RE.fullmatch(value):
        raise ReleaseError("image 必须是 name@sha256:<64 位 digest> 的完整引用")
    return value


def release_dir(release_id: str) -> Path:
    return RELEASES_DIR / validate_release_id(release_id)


def state_path(release_id: str) -> Path:
    return release_dir(release_id) / "state.json"


def sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def ensure_mode(path: Path, mode: int) -> None:
    os.chmod(path, mode)


def copy_restricted(source: Path, destination: Path, mode: int = 0o600) -> None:
    """复制普通文件并明确设置受限权限；拒绝符号链接，避免备份越界。"""
    try:
        source_stat = source.lstat()
    except FileNotFoundError as exc:
        raise ReleaseError(f"缺少必要文件: {source}") from exc
    if not stat.S_ISREG(source_stat.st_mode):
        raise ReleaseError(f"不是普通文件，拒绝备份: {source}")
    if destination.is_symlink():
        raise ReleaseError(f"目标文件是符号链接，拒绝覆盖: {destination}")
    destination.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
    shutil.copyfile(source, destination, follow_symlinks=False)
    ensure_mode(destination, mode)


def write_failure_marker(directory: Path, stage: str, exc: BaseException) -> None:
    """保留失败现场，但不把命令输出、环境变量或异常详情写入磁盘。"""
    try:
        directory.mkdir(mode=0o700, parents=True, exist_ok=True)
        marker = {
            "stage": stage,
            "failed_at": utc_now(),
            "error_type": type(exc).__name__,
            "note": "partial evidence retained; inspect manually before retry",
        }
        atomic_write(
            directory / "FAILED.json",
            (json.dumps(marker, ensure_ascii=False, indent=2) + "\n").encode("utf-8"),
            0o600,
        )
    except (OSError, ReleaseError):
        pass


def atomic_write(path: Path, content: bytes, mode: int, uid: int | None = None, gid: int | None = None) -> None:
    """同目录 fsync 后原子替换，并尽量保留原文件属主。"""
    path.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
    fd, temporary_name = tempfile.mkstemp(prefix=f".{path.name}.", dir=str(path.parent))
    temporary = Path(temporary_name)
    try:
        os.fchmod(fd, mode)
        if uid is not None and gid is not None and os.geteuid() == 0:
            os.fchown(fd, uid, gid)
        with os.fdopen(fd, "wb") as stream:
            stream.write(content)
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(temporary, path)
        directory_fd = os.open(path.parent, os.O_DIRECTORY)
        try:
            os.fsync(directory_fd)
        finally:
            os.close(directory_fd)
    except BaseException:
        try:
            os.close(fd)
        except OSError:
            pass
        try:
            temporary.unlink()
        except FileNotFoundError:
            pass
        raise


def _display_command(argv: Sequence[str]) -> str:
    # argv 从不包含旧容器 env（候选使用临时 env-file），所以可安全显示结构。
    return " ".join(str(part) for part in argv)


def run_command(
    argv: Sequence[str],
    *,
    cwd: Path | None = None,
    input_bytes: bytes | None = None,
    timeout: float | None = 60,
) -> bytes:
    """运行命令但丢弃 stderr；错误只显示安全的命令结构和退出码。"""
    try:
        completed = subprocess.run(
            list(argv),
            cwd=str(cwd) if cwd else None,
            input=input_bytes,
            stdout=subprocess.PIPE,
            stderr=subprocess.DEVNULL,
            timeout=timeout,
            check=False,
        )
    except FileNotFoundError as exc:
        raise CommandError(f"缺少命令: {argv[0]}") from exc
    except subprocess.TimeoutExpired as exc:
        raise CommandError(f"命令超时（{int(timeout or 0)} 秒）: {_display_command(argv)}") from exc
    if completed.returncode != 0:
        raise CommandError(f"命令退出码 {completed.returncode}: {_display_command(argv)}")
    return completed.stdout


def run_json_command(argv: Sequence[str], *, timeout: float | None = 60) -> Any:
    output = run_command(argv, timeout=timeout)
    try:
        return json.loads(output.decode("utf-8"))
    except (UnicodeDecodeError, json.JSONDecodeError) as exc:
        raise CommandError(f"命令返回的 JSON 无效: {_display_command(argv)}") from exc


def docker_inspect(container: str) -> dict[str, Any]:
    payload = run_json_command(["docker", "inspect", container])
    if not isinstance(payload, list) or len(payload) != 1 or not isinstance(payload[0], dict):
        raise ReleaseError(f"容器 inspect 结果异常: {container}")
    return payload[0]


def container_exists(container: str) -> bool:
    try:
        docker_inspect(container)
        return True
    except ReleaseError:
        return False


def container_running(inspect: Mapping[str, Any]) -> bool:
    state = inspect.get("State")
    return isinstance(state, Mapping) and bool(state.get("Running"))


def started_at(inspect: Mapping[str, Any]) -> str:
    state = inspect.get("State")
    if not isinstance(state, Mapping):
        return ""
    value = state.get("StartedAt")
    return str(value) if value else ""


def image_reference(inspect: Mapping[str, Any]) -> str:
    config = inspect.get("Config")
    if not isinstance(config, Mapping):
        return ""
    value = config.get("Image")
    return str(value) if value else ""


def sanitize_inspect(inspect: Mapping[str, Any]) -> dict[str, Any]:
    """保留发布审计所需字段，明确丢弃 Config.Env、Labels、Args 等敏感字段。"""
    state = inspect.get("State")
    state_map = state if isinstance(state, Mapping) else {}
    health = state_map.get("Health")
    health_map = health if isinstance(health, Mapping) else None
    config = inspect.get("Config")
    config_map = config if isinstance(config, Mapping) else {}
    host = inspect.get("HostConfig")
    host_map = host if isinstance(host, Mapping) else {}
    network_settings = inspect.get("NetworkSettings")
    networks = network_settings.get("Networks", {}) if isinstance(network_settings, Mapping) else {}
    safe_networks: dict[str, Any] = {}
    if isinstance(networks, Mapping):
        for name, value in networks.items():
            if not isinstance(value, Mapping):
                continue
            safe_networks[str(name)] = {
                "network_id": value.get("NetworkID", ""),
                "ip": value.get("IPAddress", ""),
                "gateway": value.get("Gateway", ""),
                "aliases": [str(alias) for alias in value.get("Aliases", []) if isinstance(alias, str)],
            }
    safe_mounts: list[dict[str, Any]] = []
    mounts = inspect.get("Mounts", [])
    if isinstance(mounts, list):
        for mount in mounts:
            if not isinstance(mount, Mapping):
                continue
            safe_mounts.append(
                {
                    "type": mount.get("Type", ""),
                    "name": mount.get("Name", ""),
                    "source": mount.get("Source", ""),
                    "destination": mount.get("Destination", ""),
                    "rw": bool(mount.get("RW", False)),
                    "mode": mount.get("Mode", ""),
                    "propagation": mount.get("Propagation", ""),
                }
            )
    port_bindings = host_map.get("PortBindings", {})
    safe_ports: dict[str, Any] = {}
    if isinstance(port_bindings, Mapping):
        for port, bindings in port_bindings.items():
            if not isinstance(bindings, list):
                continue
            safe_ports[str(port)] = [
                {"host_ip": item.get("HostIp", ""), "host_port": item.get("HostPort", "")}
                for item in bindings
                if isinstance(item, Mapping)
            ]
    return {
        "id": str(inspect.get("Id", "")),
        "name": str(inspect.get("Name", "")).lstrip("/"),
        "image": image_reference(inspect),
        "state": {
            "status": state_map.get("Status", ""),
            "running": bool(state_map.get("Running", False)),
            "started_at": state_map.get("StartedAt", ""),
            "finished_at": state_map.get("FinishedAt", ""),
            "exit_code": state_map.get("ExitCode", 0),
            "health": {
                "status": health_map.get("Status", "") if health_map else "",
                "failing_streak": health_map.get("FailingStreak", 0) if health_map else 0,
            },
        },
        "config": {
            "working_dir": config_map.get("WorkingDir", ""),
            "user": config_map.get("User", ""),
            "healthcheck": config_map.get("Healthcheck"),
        },
        "host": {
            "network_mode": host_map.get("NetworkMode", ""),
            "restart_policy": host_map.get("RestartPolicy", {}),
            "privileged": bool(host_map.get("Privileged", False)),
            "readonly_rootfs": bool(host_map.get("ReadonlyRootfs", False)),
            "security_opt": host_map.get("SecurityOpt", []),
            "ports": safe_ports,
        },
        "mounts": safe_mounts,
        "networks": safe_networks,
    }


def image_digest_present(image: str) -> bool:
    refs = run_json_command(["docker", "image", "inspect", "--format", "{{json .RepoDigests}}", image])
    if not isinstance(refs, list):
        return False
    repository, digest = image.split("@", 1)
    # Docker RepoDigests 按规范省略 tag（repo@sha256:...），而 CLI 引用可以
    # 是 repo:tag@sha256:...；比较规范化后的仓库名，仍要求 digest 完全一致。
    last_slash = repository.rfind("/")
    last_colon = repository.rfind(":")
    if last_colon > last_slash:
        repository = repository[:last_colon]
    expected = f"{repository}@{digest}"
    return expected in {str(ref) for ref in refs}


def pull_exact_image(image: str) -> None:
    run_command(["docker", "pull", image], timeout=600)
    if not image_digest_present(image):
        raise ReleaseError("docker pull 后未验证到要求的精确 RepoDigest")


def parse_compose_sub2api_image(content: bytes | str) -> str:
    """不依赖 YAML 库，读取固定服务的唯一顶层 image 字段。"""
    text = content.decode("utf-8") if isinstance(content, bytes) else content
    lines = text.splitlines()
    service_re = re.compile(r"^(?P<indent> {2})sub2api:\s*(?:#.*)?$")
    image_re = re.compile(r"^(?P<indent> {4})image:\s*(?P<value>[^\r\n#]+?)(?P<suffix>\s*(?:#.*)?)$")
    service_index = next((i for i, line in enumerate(lines) if service_re.fullmatch(line)), None)
    if service_index is None:
        raise ReleaseError("compose 中找不到固定的 services.sub2api")
    found: list[str] = []
    for line in lines[service_index + 1 :]:
        if line and not line.startswith(" "):
            break
        if line.startswith("  ") and not line.startswith("    "):
            break
        match = image_re.fullmatch(line)
        if match:
            found.append(match.group("value").strip().strip("\"'"))
    if len(found) != 1 or not found[0]:
        raise ReleaseError("compose 的 services.sub2api 必须有且只有一个顶层 image")
    return found[0]


def replace_compose_sub2api_image(content: bytes, image: str) -> bytes:
    """只改 services.sub2api 的 image 行；其余字节（包括注释）保持不变。"""
    validate_image(image)
    text = content.decode("utf-8")
    lines = text.splitlines(keepends=True)
    service_re = re.compile(r"^ {2}sub2api:\s*(?:#.*)?(?:\r?\n)?$")
    image_re = re.compile(r"^( {4}image:\s*)([^\r\n#]+?)(\s*(?:#.*)?)(\r?\n)?$")
    in_service = False
    replacements = 0
    output: list[str] = []
    for line in lines:
        if service_re.fullmatch(line):
            in_service = True
        elif in_service and line and not line.startswith(" "):
            in_service = False
        elif in_service and line.startswith("  ") and not line.startswith("    "):
            in_service = False
        if in_service:
            match = image_re.fullmatch(line)
            if match:
                suffix = match.group(3)
                newline = match.group(4) or ""
                line = f"{match.group(1)}{image}{suffix}{newline}"
                replacements += 1
        output.append(line)
    if replacements != 1:
        raise ReleaseError("compose 的 services.sub2api image 行数量不是 1，拒绝修改")
    updated = "".join(output).encode("utf-8")
    # 额外断言解析结果，防止替换因格式误判而改变了别的字段。
    if parse_compose_sub2api_image(updated) != image:
        raise ReleaseError("compose image 修改后的结构校验失败")
    return updated


def replace_caddy_upstream(content: bytes, old: str, new: str) -> bytes:
    """精确替换一个应用端点，保证 5080 及其他字节不变。"""
    if old not in (ENDPOINT_8080, ENDPOINT_8081) or new not in (ENDPOINT_8080, ENDPOINT_8081):
        raise ReleaseError("Caddy 端点不在允许的 8080/8081 范围")
    if old == new:
        raise ReleaseError("Caddy 新旧端点不能相同")
    old_count = content.count(old.encode("ascii"))
    new_count = content.count(new.encode("ascii"))
    if old_count != 1 or new_count != 0:
        raise ReleaseError(f"Caddy 端点状态异常（old={old_count}, new={new_count}），拒绝切换")
    updated = content.replace(old.encode("ascii"), new.encode("ascii"))
    if updated.count(ENDPOINT_5080.encode("ascii")) != content.count(ENDPOINT_5080.encode("ascii")):
        raise ReleaseError("Caddy 5080 端点发生变化，拒绝切换")
    if updated != content.replace(old.encode("ascii"), new.encode("ascii")):
        raise ReleaseError("Caddy 修改不是精确端点替换")
    return updated


def caddy_upstream(content: bytes) -> str:
    count_8080 = content.count(ENDPOINT_8080.encode("ascii"))
    count_8081 = content.count(ENDPOINT_8081.encode("ascii"))
    if count_8080 == 1 and count_8081 == 0:
        return ENDPOINT_8080
    if count_8081 == 1 and count_8080 == 0:
        return ENDPOINT_8081
    return "unknown"


def version_matches(output: bytes | str, commit: str) -> bool:
    text = output.decode("utf-8", errors="replace") if isinstance(output, bytes) else output
    return validate_commit(commit).lower() in text.lower()


def parse_groups_digest(output: bytes | str) -> dict[str, Any]:
    text = output.decode("utf-8", errors="replace") if isinstance(output, bytes) else output
    value = text.strip()
    parts = value.split("|")
    if len(parts) != 2 or not parts[0].isdigit() or not re.fullmatch(r"[0-9a-f]{32}", parts[1]):
        raise ReleaseError("groups 聚合 digest 返回格式异常")
    return {"count": int(parts[0]), "digest": parts[1]}


GROUPS_DIGEST_SQL = (
    "SELECT COUNT(*)::text || '|' || "
    "MD5(COALESCE(STRING_AGG(row_to_json(g)::text, E'\\n' ORDER BY g.id), '')) "
    "FROM (SELECT * FROM groups WHERE deleted_at IS NULL ORDER BY id) AS g;"
)


def groups_digest() -> dict[str, Any]:
    output = run_command(
        [
            "docker",
            "exec",
            POSTGRES_CONTAINER,
            "psql",
            "-U",
            DATABASE_USER,
            "-d",
            DATABASE_NAME,
            "-Atqc",
            GROUPS_DIGEST_SQL,
        ],
        timeout=60,
    )
    return parse_groups_digest(output)


def service_started_times() -> dict[str, str]:
    services: dict[str, str] = {}
    for name in (POSTGRES_CONTAINER, REDIS_CONTAINER):
        inspect = docker_inspect(name)
        if not container_running(inspect):
            raise ReleaseError(f"{name} 未运行，拒绝继续")
        started = started_at(inspect)
        if not started:
            raise ReleaseError(f"{name} 缺少 StartedAt，拒绝继续")
        services[name] = started
    return services


def assert_service_started_times(expected: Mapping[str, str]) -> None:
    current = service_started_times()
    for name, value in expected.items():
        if current.get(name) != value:
            raise ReleaseError(f"检测到 {name} 启动时间变化；脚本不会重启或自动恢复数据库/Redis")


def read_state(release_id: str) -> dict[str, Any]:
    path = state_path(release_id)
    try:
        with path.open("r", encoding="utf-8") as stream:
            state = json.load(stream)
    except FileNotFoundError as exc:
        raise ReleaseError(f"找不到发布状态，请先执行 backup: {path}") from exc
    except (OSError, json.JSONDecodeError) as exc:
        raise ReleaseError(f"发布状态不可读或已损坏: {path}") from exc
    if not isinstance(state, dict):
        raise ReleaseError("发布状态不是对象")
    return state


def write_state(release_id: str, state: Mapping[str, Any]) -> None:
    directory = release_dir(release_id)
    directory.mkdir(mode=0o700, parents=True, exist_ok=True)
    payload = json.dumps(state, ensure_ascii=False, indent=2, sort_keys=True).encode("utf-8") + b"\n"
    atomic_write(state_path(release_id), payload, 0o600)


def update_state(release_id: str, state: dict[str, Any], **updates: Any) -> dict[str, Any]:
    state.update(updates)
    state["updated_at"] = utc_now()
    write_state(release_id, state)
    return state


def require_target_args(state: Mapping[str, Any], image: str | None, commit: str | None) -> None:
    if image is not None and validate_image(image) != state.get("image"):
        raise ReleaseError("命令行 image 与该 release 的状态不一致")
    if commit is not None and validate_commit(commit) != str(state.get("commit", "")).lower():
        raise ReleaseError("命令行 commit 与该 release 的状态不一致")


def ensure_release_args(args: argparse.Namespace) -> tuple[str, str | None, str | None]:
    release_id = validate_release_id(args.release_id)
    image = validate_image(args.image) if getattr(args, "image", None) else None
    commit = validate_commit(args.commit) if getattr(args, "commit", None) else None
    return release_id, image, commit


def backup_release(release_id: str, image: str, commit: str) -> None:
    validate_image(image)
    validate_commit(commit)
    directory = release_dir(release_id)
    if directory.exists():
        if state_path(release_id).exists():
            state = read_state(release_id)
            if state.get("phase") == "backup" and state.get("image") == image and state.get("commit") == commit:
                print(f"OK stage=backup release={release_id} idempotent=true")
                return
        raise ReleaseError("该 release 目录已存在（可能含失败证据），拒绝覆盖；请人工审查后使用新 release-id")
    if not COMPOSE_FILE.is_file() or not ENV_FILE.is_file() or not CADDY_FILE.is_file():
        raise ReleaseError("compose、.env、Caddyfile 任一必要文件缺失，拒绝生成不完整备份")
    if not CONFIG_FILE.is_file() or CONFIG_FILE.is_symlink():
        raise ReleaseError("缺少 /opt/modelport/deploy/data/config.yaml，拒绝生成不完整备份")
    compose_content = COMPOSE_FILE.read_bytes()
    # 先验证结构，避免备份一个无法由 promote 安全更新的 compose。
    compose_image = parse_compose_sub2api_image(compose_content)
    old_app = docker_inspect(APP_CONTAINER)
    if not container_running(old_app):
        raise ReleaseError("正式 sub2api 未运行，拒绝备份")
    old_services = service_started_times()
    old_groups = groups_digest()

    directory.mkdir(mode=0o700, parents=True, exist_ok=False)
    try:
        copy_restricted(COMPOSE_FILE, directory / COMPOSE_FILE.name)
        copy_restricted(ENV_FILE, directory / ENV_FILE.name)
        copy_restricted(CADDY_FILE, directory / CADDY_FILE.name)
        config_present = True
        copy_restricted(CONFIG_FILE, directory / "data" / CONFIG_FILE.name)

        # pg_dump 的二进制内容只写受限文件；pg_restore -l 只验证退出码，不保存/打印清单。
        dump_path = directory / "database.dump"
        try:
            with dump_path.open("wb") as dump:
                completed = subprocess.run(
                    ["docker", "exec", POSTGRES_CONTAINER, "pg_dump", "-U", DATABASE_USER, "-d", DATABASE_NAME, "-Fc"],
                    stdout=dump,
                    stderr=subprocess.DEVNULL,
                    timeout=600,
                    check=False,
                )
        except FileNotFoundError as exc:
            dump_path.unlink(missing_ok=True)
            raise ReleaseError("缺少 docker 命令") from exc
        except subprocess.TimeoutExpired as exc:
            dump_path.unlink(missing_ok=True)
            raise ReleaseError("pg_dump 超时") from exc
        ensure_mode(dump_path, 0o600)
        if completed.returncode != 0:
            dump_path.unlink(missing_ok=True)
            raise ReleaseError(f"pg_dump 退出码 {completed.returncode}")
        with dump_path.open("rb") as dump:
            listed = subprocess.run(
                ["docker", "exec", "-i", POSTGRES_CONTAINER, "pg_restore", "-l"],
                stdin=dump,
                stdout=subprocess.DEVNULL,
                stderr=subprocess.DEVNULL,
                timeout=120,
                check=False,
            )
        if listed.returncode != 0:
            raise ReleaseError(f"pg_restore -l 退出码 {listed.returncode}")

        sanitized = {
            "created_at": utc_now(),
            "containers": {
                APP_CONTAINER: sanitize_inspect(old_app),
                POSTGRES_CONTAINER: sanitize_inspect(docker_inspect(POSTGRES_CONTAINER)),
                REDIS_CONTAINER: sanitize_inspect(docker_inspect(REDIS_CONTAINER)),
            },
        }
        atomic_write(
            directory / "docker-inspect.json",
            (json.dumps(sanitized, ensure_ascii=False, indent=2, sort_keys=True) + "\n").encode("utf-8"),
            0o600,
        )
        atomic_write(
            directory / "groups-digest.json",
            (json.dumps(old_groups, ensure_ascii=False, indent=2, sort_keys=True) + "\n").encode("utf-8"),
            0o600,
        )
        state = {
            "schema": 1,
            "release_id": release_id,
            "phase": "backup",
            "created_at": utc_now(),
            "updated_at": utc_now(),
            "image": image,
            "commit": commit,
            "compose_image_before": compose_image,
            "old_container": APP_CONTAINER,
            "candidate_container": f"sub2api-candidate-{release_id}",
            "old_app_id": str(old_app.get("Id", "")),
            "old_app_image": image_reference(old_app),
            "old_app_started_at": started_at(old_app),
            "services_started_at": old_services,
            "groups_before": old_groups,
            "caddy_before_sha256": hashlib.sha256(CADDY_FILE.read_bytes()).hexdigest(),
            "config_present": config_present,
            "artifacts": {
                "compose": COMPOSE_FILE.name,
                "env": ENV_FILE.name,
                "caddy": CADDY_FILE.name,
                "config": "data/config.yaml" if config_present else None,
                "database_dump": "database.dump",
                "docker_inspect": "docker-inspect.json",
                "groups_digest": "groups-digest.json",
            },
            "database_dump_sha256": sha256_file(dump_path),
        }
        write_state(release_id, state)
    except BaseException as exc:
        # 保留已复制的备份、数据库 dump 及失败标记，便于人工审计；不自动重试/覆盖。
        write_failure_marker(directory, "backup", exc)
        raise
    print(
        f"OK stage=backup release={release_id} image_before={compose_image} "
        f"groups={old_groups['count']}:{old_groups['digest']} db_redis_unchanged=true"
    )


def health_status(inspect: Mapping[str, Any]) -> str:
    state = inspect.get("State")
    if not isinstance(state, Mapping):
        return ""
    health = state.get("Health")
    if not isinstance(health, Mapping):
        return ""
    return str(health.get("Status", ""))


def http_health(port: int) -> bool:
    opener = build_opener(ProxyHandler({}))
    request = Request(f"http://127.0.0.1:{port}/health", method="GET")
    try:
        with opener.open(request, timeout=5) as response:
            return int(response.status) == 200
    except (OSError, URLError, ValueError):
        return False


def container_version(container: str) -> bytes | None:
    try:
        return run_command(["docker", "exec", container, "/app/sub2api", "-version"], timeout=15)
    except ReleaseError:
        return None


def wait_healthy_version(container: str, port: int, commit: str, timeout_seconds: int) -> dict[str, Any]:
    validate_commit(commit)
    deadline = time.monotonic() + timeout_seconds
    last_health = ""
    while time.monotonic() < deadline:
        try:
            inspect = docker_inspect(container)
        except ReleaseError:
            inspect = None
        if inspect is not None:
            last_health = health_status(inspect)
            if not container_running(inspect):
                status = str(inspect.get("State", {}).get("Status", ""))
                if status in {"exited", "dead"}:
                    raise ReleaseError(f"{container} 已退出（health={last_health or 'none'}）")
                time.sleep(1)
                continue
            version = container_version(container)
            if (
                last_health in ("", "healthy")
                and http_health(port)
                and version is not None
                and version_matches(version, commit)
            ):
                return {
                    "healthy": True,
                    "health_status": last_health or "endpoint-200",
                    "version_matches": True,
                    "started_at": started_at(inspect),
                    "image": image_reference(inspect),
                }
        time.sleep(2)
    raise ReleaseError(f"{container} 健康/版本检查超时（最后 health={last_health or 'unknown'}）")


def verify_candidate(state: Mapping[str, Any], timeout_seconds: int = 180) -> tuple[dict[str, Any], dict[str, Any]]:
    candidate = str(state.get("candidate_container", ""))
    image = validate_image(str(state.get("image", "")))
    commit = validate_commit(str(state.get("commit", "")))
    if not candidate or not container_exists(candidate):
        raise ReleaseError("候选容器不存在")
    inspected = docker_inspect(candidate)
    if not container_running(inspected) or image_reference(inspected) != image:
        raise ReleaseError("候选容器未运行或 image 不匹配")
    if not image_digest_present(image):
        raise ReleaseError("候选 image 未通过精确 digest 校验")
    health = wait_healthy_version(candidate, CANDIDATE_PORT, commit, timeout_seconds)
    assert_service_started_times(state.get("services_started_at", {}))
    digest = groups_digest()
    if digest != state.get("groups_before"):
        raise ReleaseError("候选校验时 groups 聚合 digest 改变")
    return health, digest


def mount_argument(mount: Mapping[str, Any]) -> str:
    mount_type = str(mount.get("Type", ""))
    destination = str(mount.get("Destination", ""))
    if mount_type not in {"bind", "volume", "tmpfs"} or not destination.startswith("/"):
        raise ReleaseError("正式容器包含脚本不支持的 mount 类型，拒绝启动候选")
    if mount_type == "tmpfs":
        parts = ["type=tmpfs", f"target={destination}"]
    else:
        source = str(mount.get("Source", "") or mount.get("Name", ""))
        if not source:
            raise ReleaseError("正式容器 mount 缺少 source/name")
        parts = [f"type={mount_type}", f"source={source}", f"target={destination}"]
    if not bool(mount.get("RW", False)):
        parts.append("readonly")
    propagation = str(mount.get("Propagation", ""))
    if mount_type == "bind" and propagation in {"private", "rprivate", "shared", "rshared", "slave", "rslave"}:
        parts.append(f"bind-propagation={propagation}")
    return ",".join(parts)


def health_arguments(healthcheck: Any) -> list[str]:
    if not isinstance(healthcheck, Mapping):
        return []
    test = healthcheck.get("Test")
    if isinstance(test, list) and test and test[0] == "NONE":
        return ["--no-healthcheck"]
    if not isinstance(test, list) or not test:
        return []
    # Docker inspect 的 CMD 数组在这里转换成单一 shell 命令；内容来自现有镜像配置，
    # 不含旧容器 env，且使用 argv 传给 docker，不经过宿主 shell。
    command = " ".join(str(part) for part in test[1:] if isinstance(part, (str, int, float)))
    if not command:
        return []
    result = ["--health-cmd", command]
    duration_names = (
        ("Interval", "--health-interval"),
        ("Timeout", "--health-timeout"),
        ("StartPeriod", "--health-start-period"),
    )
    for key, flag in duration_names:
        value = healthcheck.get(key)
        if isinstance(value, int) and value > 0:
            result.extend([flag, f"{value}ns"])
    retries = healthcheck.get("Retries")
    if isinstance(retries, int) and retries >= 0:
        result.extend(["--health-retries", str(retries)])
    return result


def candidate_run_args(old: Mapping[str, Any], candidate: str, env_file: Path, image: str) -> list[str]:
    config = old.get("Config")
    config_map = config if isinstance(config, Mapping) else {}
    host = old.get("HostConfig")
    host_map = host if isinstance(host, Mapping) else {}
    network_settings = old.get("NetworkSettings")
    network_map = network_settings if isinstance(network_settings, Mapping) else {}
    networks = network_map.get("Networks", {})
    if not isinstance(networks, Mapping) or not networks:
        raise ReleaseError("正式容器没有可复用的 bridge network")
    network_mode = str(host_map.get("NetworkMode", ""))
    if network_mode in {"host", "none"} or network_mode.startswith("container:"):
        raise ReleaseError("正式容器使用不适合候选隔离的 network mode")
    first_network = next(iter(networks))
    args: list[str] = [
        "docker",
        "run",
        "-d",
        "--name",
        candidate,
        "--restart=no",
        "--env-file",
        str(env_file),
        "--publish",
        f"127.0.0.1:{CANDIDATE_PORT}:{APP_PORT}",
        "--network",
        str(first_network),
        "--label",
        f"com.modelport.release={candidate.removeprefix('sub2api-candidate-')}",
    ]
    mounts = old.get("Mounts", [])
    if not isinstance(mounts, list):
        raise ReleaseError("正式容器 mount 信息异常")
    for mount in mounts:
        if isinstance(mount, Mapping):
            args.extend(["--mount", mount_argument(mount)])
    working_dir = config_map.get("WorkingDir")
    if isinstance(working_dir, str) and working_dir:
        args.extend(["--workdir", working_dir])
    user = config_map.get("User")
    if isinstance(user, str) and user:
        args.extend(["--user", user])
    entrypoint = config_map.get("Entrypoint")
    if isinstance(entrypoint, list) and entrypoint:
        args.extend(["--entrypoint", str(entrypoint[0])])
    elif isinstance(entrypoint, str) and entrypoint:
        args.extend(["--entrypoint", entrypoint])
    if bool(host_map.get("Privileged", False)):
        raise ReleaseError("正式容器为 privileged，候选复制被安全策略拒绝")
    security_opt = host_map.get("SecurityOpt", [])
    if isinstance(security_opt, list):
        for value in security_opt:
            if isinstance(value, str):
                args.extend(["--security-opt", value])
    if bool(host_map.get("ReadonlyRootfs", False)):
        args.append("--read-only")
    args.extend(health_arguments(config_map.get("Healthcheck")))
    args.append(image)
    command = config_map.get("Cmd")
    if isinstance(command, list):
        args.extend(str(part) for part in command)
    elif isinstance(command, str) and command:
        args.append(command)
    return args


def write_candidate_env(old: Mapping[str, Any], path: Path) -> None:
    config = old.get("Config")
    config_map = config if isinstance(config, Mapping) else {}
    env = config_map.get("Env")
    if not isinstance(env, list):
        raise ReleaseError("正式容器缺少环境变量列表，拒绝启动候选")
    with path.open("w", encoding="utf-8", newline="\n") as stream:
        os.chmod(path, 0o600)
        for item in env:
            if not isinstance(item, str) or "\n" in item or "\r" in item:
                raise ReleaseError("正式容器 env 含不支持的换行内容，拒绝启动候选")
            stream.write(item)
            stream.write("\n")
        stream.flush()
        os.fsync(stream.fileno())


def connect_extra_networks(old: Mapping[str, Any], candidate: str) -> None:
    network_settings = old.get("NetworkSettings")
    network_map = network_settings if isinstance(network_settings, Mapping) else {}
    networks = network_map.get("Networks", {})
    if not isinstance(networks, Mapping):
        return
    first = next(iter(networks), None)
    for network in networks:
        if network != first:
            run_command(["docker", "network", "connect", str(network), candidate], timeout=60)


def candidate_release(release_id: str, image: str | None, commit: str | None, health_timeout: int) -> None:
    state = read_state(release_id)
    require_target_args(state, image, commit)
    target_image = str(state.get("image", ""))
    target_commit = str(state.get("commit", ""))
    validate_image(target_image)
    validate_commit(target_commit)
    phase = str(state.get("phase", ""))
    if phase not in {"backup", "candidate", "switched", "promoted", "finalized"}:
        raise ReleaseError(f"当前阶段 {phase} 不允许启动候选")
    candidate = str(state.get("candidate_container", ""))
    if not candidate:
        raise ReleaseError("发布状态缺少 candidate container")
    if phase in {"candidate", "switched", "promoted", "finalized"}:
        if not container_exists(candidate):
            if phase == "finalized":
                print(f"OK stage=candidate release={release_id} idempotent=true candidate=stopped")
                return
            raise ReleaseError("状态显示候选已创建但容器不存在，拒绝自动重建")
        candidate_inspect = docker_inspect(candidate)
        if phase == "finalized" and not container_running(candidate_inspect):
            print(f"OK stage=candidate release={release_id} idempotent=true candidate=stopped")
            return
        check, digest = verify_candidate(state, health_timeout)
        print(
            f"OK stage=candidate release={release_id} idempotent=true "
            f"health={check['health_status']} groups={digest['count']}:{digest['digest']}"
        )
        return

    old = docker_inspect(APP_CONTAINER)
    if not container_running(old):
        raise ReleaseError("正式容器在候选启动前已停止，拒绝继续")
    if container_exists(candidate):
        raise ReleaseError("候选容器名已存在但状态未记录，拒绝删除或覆盖")
    pull_exact_image(target_image)
    directory = release_dir(release_id)
    temporary_env: Path | None = None
    try:
        fd, temporary_name = tempfile.mkstemp(prefix=".candidate-env-", dir=str(directory), text=True)
        os.close(fd)
        temporary_env = Path(temporary_name)
        write_candidate_env(old, temporary_env)
        args = candidate_run_args(old, candidate, temporary_env, target_image)
        try:
            run_command(args, timeout=120)
            connect_extra_networks(old, candidate)
            check = wait_healthy_version(candidate, CANDIDATE_PORT, target_commit, health_timeout)
            digest = groups_digest()
            if digest != state.get("groups_before"):
                raise ReleaseError("候选启动后 groups 聚合 digest 改变")
        except BaseException:
            # 候选尚未交付流量，失败时只清理本次候选；旧正式容器保持运行。
            try:
                run_command(["docker", "rm", "-f", candidate], timeout=60)
            except ReleaseError:
                pass
            raise
    finally:
        if temporary_env is not None:
            temporary_env.unlink(missing_ok=True)
    update_state(
        release_id,
        state,
        phase="candidate",
        candidate_id=str(docker_inspect(candidate).get("Id", "")),
        candidate_started_at=started_at(docker_inspect(candidate)),
        groups_candidate=digest,
        candidate_health=check,
    )
    print(
        f"OK stage=candidate release={release_id} health={check['health_status']} "
        f"version_matches=true groups={digest['count']}:{digest['digest']} old_kept=true"
    )


def caddy_reload_with_content(release_id: str, state: dict[str, Any], old_endpoint: str, new_endpoint: str) -> None:
    if not CADDY_FILE.is_file():
        raise ReleaseError(f"Caddyfile 不存在: {CADDY_FILE}")
    original = CADDY_FILE.read_bytes()
    updated = replace_caddy_upstream(original, old_endpoint, new_endpoint)
    source_stat = CADDY_FILE.stat()
    if stat.S_IMODE(source_stat.st_mode) != 0o644:
        raise ReleaseError("Caddyfile 当前权限不是 0644，拒绝切换（避免 reload 后服务用户不可读）")
    if old_endpoint == ENDPOINT_8080 and hashlib.sha256(original).hexdigest() != state.get("caddy_before_sha256"):
        raise ReleaseError("Caddy 切换前内容与 backup 不一致，疑似人工漂移")
    temporary = CADDY_FILE.with_name(f".Caddyfile.{release_id}.candidate")
    copy_restricted(CADDY_FILE, temporary, 0o644)
    try:
        atomic_write(temporary, updated, 0o644, source_stat.st_uid, source_stat.st_gid)
        run_command(["caddy", "validate", "--config", str(temporary), "--adapter", "caddyfile"], timeout=60)
        atomic_write(CADDY_FILE, updated, 0o644, source_stat.st_uid, source_stat.st_gid)
        try:
            # 明确不带 --force；Caddy 会热加载新配置。
            run_command(["caddy", "reload", "--config", str(CADDY_FILE), "--adapter", "caddyfile"], timeout=60)
        except BaseException:
            # 只恢复 Caddy 文本，不触碰数据库/容器；失败时保持原流量路径。
            atomic_write(CADDY_FILE, original, 0o644, source_stat.st_uid, source_stat.st_gid)
            try:
                run_command(["caddy", "reload", "--config", str(CADDY_FILE), "--adapter", "caddyfile"], timeout=60)
            except ReleaseError:
                pass
            raise
    finally:
        temporary.unlink(missing_ok=True)
    update_state(
        release_id,
        state,
        caddy_upstream=new_endpoint,
        caddy_last_sha256=hashlib.sha256(updated).hexdigest(),
        caddy_last_transition=f"{old_endpoint}->{new_endpoint}",
    )


def switch_candidate(release_id: str, image: str | None = None, commit: str | None = None) -> None:
    state = read_state(release_id)
    require_target_args(state, image, commit)
    phase = str(state.get("phase", ""))
    if phase in {"candidate", "switched", "promoted"}:
        check, digest = verify_candidate(state)
        candidate = str(state.get("candidate_container", ""))
    else:
        check, digest, candidate = None, None, str(state.get("candidate_container", ""))
    if phase in {"switched", "promoted", "finalized"}:
        current = caddy_upstream(CADDY_FILE.read_bytes())
        if phase == "finalized":
            if current != ENDPOINT_8080:
                raise ReleaseError("状态已 finalized 但 Caddy 不是 8080，拒绝重复切换")
        elif current != ENDPOINT_8081:
            raise ReleaseError("状态已切候选但 Caddy 不是 8081，拒绝重复切换")
        print(f"OK stage=switch-candidate release={release_id} idempotent=true upstream={current}")
        return
    if phase != "candidate":
        raise ReleaseError(f"当前阶段 {phase} 不允许切换候选")
    caddy_reload_with_content(release_id, state, ENDPOINT_8080, ENDPOINT_8081)
    update_state(release_id, state, phase="switched", switched_at=utc_now())
    print(
        f"OK stage=switch-candidate release={release_id} upstream={ENDPOINT_8081} "
        f"health={check['health_status']} groups={digest['count']}:{digest['digest']} port5080_untouched=true"
    )


def established_connections(container: str, port: int = APP_PORT) -> int:
    inspect = docker_inspect(container)
    state = inspect.get("State")
    if not isinstance(state, Mapping) or not state.get("Running"):
        return 0
    pid = state.get("Pid")
    try:
        pid_int = int(pid)
    except (TypeError, ValueError) as exc:
        raise ReleaseError(f"{container} 缺少有效网络命名空间 PID") from exc
    if pid_int <= 0:
        return 0
    output = run_command(
        ["nsenter", "--target", str(pid_int), "--net", "ss", "-Htan", f"state established sport = :{port}"],
        timeout=15,
    )
    count = 0
    for raw_line in output.decode("utf-8", errors="replace").splitlines():
        line = raw_line.strip()
        if not line:
            continue
        fields = line.split()
        # ss -Htan 的第一列是状态；LISTEN 不算活动请求。
        if fields and fields[0].upper() != "LISTEN":
            count += 1
    return count


def wait_connections_zero(container: str, timeout_seconds: int) -> int:
    if timeout_seconds < 0 or timeout_seconds > 60:
        raise ReleaseError("排空等待必须在 0-60 秒内")
    deadline = time.monotonic() + timeout_seconds
    last = established_connections(container)
    while last > 0 and time.monotonic() < deadline:
        time.sleep(2)
        last = established_connections(container)
    if last != 0:
        raise ReleaseError(f"{container} 仍有 {last} 条 8080 活动连接，保留候选且不硬停")
    return last


def current_compose_content() -> bytes:
    try:
        return COMPOSE_FILE.read_bytes()
    except OSError as exc:
        raise ReleaseError(f"compose 不可读: {COMPOSE_FILE}") from exc


def promote_release(release_id: str, drain_timeout: int, image: str | None = None, commit: str | None = None) -> None:
    state = read_state(release_id)
    require_target_args(state, image, commit)
    phase = str(state.get("phase", ""))
    if phase in {"promoted", "finalized"}:
        assert_service_started_times(state.get("services_started_at", {}))
        print(f"OK stage=promote release={release_id} idempotent=true phase={phase}")
        return
    if phase not in {"switched", "promote-pending"}:
        raise ReleaseError(f"当前阶段 {phase} 不允许 promote")
    target_image = validate_image(str(state.get("image", "")))
    target_commit = validate_commit(str(state.get("commit", "")))
    if caddy_upstream(CADDY_FILE.read_bytes()) != ENDPOINT_8081:
        raise ReleaseError("promote 前 Caddy 不是候选 8081，拒绝停止/替换正式容器")
    candidate_health, candidate_digest = verify_candidate(state)
    assert_service_started_times(state.get("services_started_at", {}))
    old_container = str(state.get("old_container", APP_CONTAINER))
    if container_exists(old_container) and container_running(docker_inspect(old_container)):
        wait_connections_zero(old_container, drain_timeout)
    update_state(
        release_id,
        state,
        phase="promote-pending",
        promote_started_at=utc_now(),
        candidate_health_before_promote=candidate_health,
        groups_candidate_before_promote=candidate_digest,
    )

    current = current_compose_content()
    current_image = parse_compose_sub2api_image(current)
    if current_image != target_image:
        updated = replace_compose_sub2api_image(current, target_image)
        source_stat = COMPOSE_FILE.stat()
        atomic_write(COMPOSE_FILE, updated, stat.S_IMODE(source_stat.st_mode), source_stat.st_uid, source_stat.st_gid)
    if parse_compose_sub2api_image(current_compose_content()) != target_image:
        raise ReleaseError("compose 未持久化到目标 image")
    run_command(
        ["docker", "compose", "-f", str(COMPOSE_FILE), "up", "-d", "--no-deps", "--no-build", APP_CONTAINER],
        cwd=DEPLOY_DIR,
        timeout=600,
    )
    formal = wait_healthy_version(APP_CONTAINER, APP_PORT, target_commit, 180)
    if not image_digest_present(target_image):
        raise ReleaseError("正式容器启动后 image digest 校验失败")
    assert_service_started_times(state.get("services_started_at", {}))
    digest = groups_digest()
    if digest != state.get("groups_before"):
        raise ReleaseError("正式容器启动后 groups 聚合 digest 改变")
    update_state(release_id, state, phase="promoted", formal_health=formal, groups_promoted=digest)
    print(
        f"OK stage=promote release={release_id} formal_health={formal['health_status']} "
        f"version_matches=true groups={digest['count']}:{digest['digest']} db_redis_unchanged=true"
    )


def stop_candidate(candidate: str) -> None:
    if not container_exists(candidate):
        return
    inspect = docker_inspect(candidate)
    if container_running(inspect):
        run_command(["docker", "stop", "-t", "30", candidate], timeout=60)
    if container_running(docker_inspect(candidate)):
        raise ReleaseError("候选容器 stop 后仍在运行，保留容器供人工处理")


def finalize_release(release_id: str, drain_timeout: int, image: str | None = None, commit: str | None = None) -> None:
    state = read_state(release_id)
    require_target_args(state, image, commit)
    phase = str(state.get("phase", ""))
    if phase == "finalized":
        current = caddy_upstream(CADDY_FILE.read_bytes())
        if current != ENDPOINT_8080:
            raise ReleaseError("状态已 finalized 但 Caddy 不是 8080")
        assert_service_started_times(state.get("services_started_at", {}))
        if container_exists(str(state.get("candidate_container", ""))) and container_running(
            docker_inspect(str(state.get("candidate_container", "")))
        ):
            raise ReleaseError("状态已 finalized 但候选容器仍在运行")
        print(f"OK stage=finalize release={release_id} idempotent=true candidate=stopped")
        return
    if phase not in {"promoted", "finalize-pending"}:
        raise ReleaseError(f"当前阶段 {phase} 不允许 finalize")
    target_commit = validate_commit(str(state.get("commit", "")))
    assert_service_started_times(state.get("services_started_at", {}))
    formal = wait_healthy_version(APP_CONTAINER, APP_PORT, target_commit, 180)
    digest = groups_digest()
    if digest != state.get("groups_before"):
        raise ReleaseError("正式切回前 groups 聚合 digest 改变")
    current_upstream = caddy_upstream(CADDY_FILE.read_bytes())
    if current_upstream not in {ENDPOINT_8080, ENDPOINT_8081}:
        raise ReleaseError("Caddy 当前既不是正式 8080 也不是候选 8081，拒绝回切")
    update_state(release_id, state, phase="finalize-pending", formal_health=formal)
    if current_upstream == ENDPOINT_8081:
        caddy_reload_with_content(release_id, state, ENDPOINT_8081, ENDPOINT_8080)
    candidate = str(state.get("candidate_container", ""))
    try:
        wait_connections_zero(candidate, drain_timeout)
        stop_candidate(candidate)
    except BaseException:
        # Caddy 已回正式；候选保留，下一次 finalize 可继续排空/stop。
        raise
    assert_service_started_times(state.get("services_started_at", {}))
    final_caddy = CADDY_FILE.read_bytes()
    expected_caddy_sha = state.get("caddy_before_sha256")
    if expected_caddy_sha and hashlib.sha256(final_caddy).hexdigest() != expected_caddy_sha:
        raise ReleaseError("最终 Caddyfile 与 backup 不一致，拒绝标记 finalized")
    update_state(
        release_id,
        state,
        phase="finalized",
        finalized_at=utc_now(),
        groups_final=digest,
        candidate_stopped=True,
        caddy_final_sha256=hashlib.sha256(final_caddy).hexdigest(),
    )
    print(
        f"OK stage=finalize release={release_id} upstream={ENDPOINT_8080} candidate=stopped "
        f"groups={digest['count']}:{digest['digest']} db_redis_unchanged=true"
    )


def status_release(release_id: str) -> None:
    state = read_state(release_id)
    candidate = str(state.get("candidate_container", ""))
    candidate_state = "absent"
    if candidate and container_exists(candidate):
        inspected = docker_inspect(candidate)
        candidate_state = str(inspected.get("State", {}).get("Status", "unknown"))
    formal_state = "absent"
    if container_exists(APP_CONTAINER):
        inspected = docker_inspect(APP_CONTAINER)
        formal_state = str(inspected.get("State", {}).get("Status", "unknown"))
    compose_image = "unreadable"
    try:
        compose_image = parse_compose_sub2api_image(current_compose_content())
    except ReleaseError:
        pass
    caddy_state = "unreadable"
    if CADDY_FILE.is_file():
        caddy_state = caddy_upstream(CADDY_FILE.read_bytes())
    services = state.get("services_started_at", {})
    services_unchanged = False
    try:
        services_unchanged = service_started_times() == services
    except ReleaseError:
        pass
    groups = groups_digest()
    print(
        f"release={release_id} phase={state.get('phase', 'unknown')} "
        f"image={state.get('image', 'unknown')} commit={state.get('commit', 'unknown')} "
        f"formal={formal_state} candidate={candidate_state} caddy={caddy_state} "
        f"compose_image={compose_image} groups={groups['count']}:{groups['digest']} "
        f"db_redis_unchanged={str(services_unchanged).lower()}"
    )


def positive_int(value: str) -> int:
    try:
        parsed = int(value)
    except ValueError as exc:
        raise argparse.ArgumentTypeError("必须是整数") from exc
    if parsed < 0:
        raise argparse.ArgumentTypeError("不能为负数")
    return parsed


def parser() -> argparse.ArgumentParser:
    root = argparse.ArgumentParser(description="ModelPort 固定路径安全分阶段发布辅助工具")
    subparsers = root.add_subparsers(dest="stage", required=True)

    backup = subparsers.add_parser("backup", help="备份 compose/.env/config/Caddy/DB 和无密钥审计信息")
    backup.add_argument("--release-id", required=True)
    backup.add_argument("--image", required=True)
    backup.add_argument("--commit", required=True)

    candidate = subparsers.add_parser("candidate", help="拉取精确 digest 并启动 8081 候选")
    candidate.add_argument("--release-id", required=True)
    candidate.add_argument("--image")
    candidate.add_argument("--commit")
    candidate.add_argument("--health-timeout", type=positive_int, default=180)

    switch = subparsers.add_parser("switch-candidate", help="Caddy 8080 精确切换到 8081")
    switch.add_argument("--release-id", required=True)
    switch.add_argument("--image")
    switch.add_argument("--commit")

    promote = subparsers.add_parser("promote", help="排空旧 8080 并仅更新 Compose 应用 image")
    promote.add_argument("--release-id", required=True)
    promote.add_argument("--image")
    promote.add_argument("--commit")
    promote.add_argument("--drain-timeout", type=positive_int, default=60)

    finalize = subparsers.add_parser("finalize", help="确认正式版本、Caddy 回 8080、排空并停止候选")
    finalize.add_argument("--release-id", required=True)
    finalize.add_argument("--image")
    finalize.add_argument("--commit")
    finalize.add_argument("--drain-timeout", type=positive_int, default=60)

    status = subparsers.add_parser("status", help="输出不含密钥的阶段摘要")
    status.add_argument("--release-id", required=True)
    return root


def require_root() -> None:
    if hasattr(os, "geteuid") and os.geteuid() != 0:
        raise ReleaseError("该工具固定面向 ModelPort 生产路径，必须以 root 运行")


def main(argv: Sequence[str] | None = None) -> int:
    args = parser().parse_args(argv)
    try:
        require_root()
        release_id, image, commit = ensure_release_args(args)
        if args.stage == "backup":
            assert image is not None and commit is not None
            backup_release(release_id, image, commit)
        elif args.stage == "candidate":
            candidate_release(release_id, image, commit, args.health_timeout)
        elif args.stage == "switch-candidate":
            switch_candidate(release_id, image, commit)
        elif args.stage == "promote":
            if args.drain_timeout > 60:
                raise ReleaseError("旧容器排空等待上限为 60 秒")
            promote_release(release_id, args.drain_timeout, image, commit)
        elif args.stage == "finalize":
            if args.drain_timeout > 60:
                raise ReleaseError("候选排空等待上限为 60 秒")
            finalize_release(release_id, args.drain_timeout, image, commit)
        elif args.stage == "status":
            status_release(release_id)
        else:
            raise ReleaseError("未知阶段")
        return 0
    except KeyboardInterrupt:
        print("ERROR: 用户中断；当前容器和数据库未由脚本自动回滚", file=sys.stderr)
        return 130
    except ReleaseError as exc:
        print(f"ERROR stage={getattr(args, 'stage', 'unknown')} release={getattr(args, 'release_id', '?')}: {exc}", file=sys.stderr)
        return 2
    except (OSError, subprocess.SubprocessError) as exc:
        print(
            f"ERROR stage={getattr(args, 'stage', 'unknown')} release={getattr(args, 'release_id', '?')}: "
            "系统操作失败（未执行自动数据库回滚）",
            file=sys.stderr,
        )
        return 2


if __name__ == "__main__":
    raise SystemExit(main())
