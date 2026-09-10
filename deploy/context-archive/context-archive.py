#!/usr/bin/env python3
"""每日上下文归档编排器（macOS Python 3.9，fail closed）。

该程序只通过生产机上的 forced-command helper 读取/删除固定范围的
request_context_archives 归档行；提示词/历史消息正文只存在于加密 APFS sparsebundle 内。
"""

from __future__ import annotations

import argparse
import csv
import datetime as dt
import fcntl
import hashlib
import io
import json
import os
import plistlib
import re
import shutil
import stat
import subprocess
import sys
import tempfile
from pathlib import Path
from typing import Any, Dict, Iterable, List, Optional, Sequence, Tuple


VERSION = "1.0"
EXTERNAL_ROOT = Path("/Volumes/MIAO")
EXTERNAL_AI = EXTERNAL_ROOT / "AI 数据"
SPARSEBUNDLE = EXTERNAL_AI / "context-archive.sparsebundle"
VOLUME_UUID = "6A660EF0-8100-3756-9152-6AED7A90C07E"
STATE_DIR = Path.home() / "Library" / "Application Support" / "ModelPort" / "context-archive"
CONFIG_FILE = STATE_DIR / "config.json"
STATE_FILE = STATE_DIR / "state.json"
LOCK_FILE = STATE_DIR / "run.lock"
LOG_FILE = STATE_DIR / "archive.log"
KEYCHAIN_SERVICE = "com.modelport.context-archive"
KEYCHAIN_ACCOUNT = "archive"
IMAGE_SIZE = "900g"
PART_BYTES = 512 * 1024 * 1024
MIN_EXTERNAL_FREE = 10 * 1024 * 1024 * 1024
MIN_IMAGE_FREE = 1 * 1024 * 1024 * 1024
MAX_MANIFEST_BYTES = 1024 * 1024
MAX_META_BYTES = 64 * 1024
MAX_ID = 9223372036854775807
EXIT_OK = 0
EXIT_USAGE = 64
EXIT_TEMPFAIL = 75

HEADER = (
    "id",
    "request_id",
    "protocol",
    "model",
    "stage",
    "context_payload",
    "content_hash",
    "message_count",
    "created_at",
)
ID_RE = re.compile(r"^[0-9]+$")
TARGET_RE = re.compile(r"^[A-Za-z0-9_.@:/-]+$")
REMOTE_PREFIX_RE = re.compile(r"^[A-Za-z0-9_.:/-]+$")


class ArchiveError(Exception):
    """不携带外部输出的、可安全记录的错误。"""

    def __init__(self, code: str) -> None:
        self.code = code
        super().__init__(code)


def now_bj() -> dt.datetime:
    try:
        from zoneinfo import ZoneInfo

        return dt.datetime.now(ZoneInfo("Asia/Shanghai"))
    except Exception:
        return dt.datetime.now(dt.timezone(dt.timedelta(hours=8)))


def utc_stamp() -> str:
    return dt.datetime.now(dt.timezone.utc).isoformat(timespec="seconds")


def metadata(value: Any) -> Any:
    if isinstance(value, Path):
        return str(value)
    if isinstance(value, (str, int, bool)) or value is None:
        return value
    return str(value)


def log_event(event: str, **fields: Any) -> None:
    """只写字段白名单；调用方不得传入正文、命令行或异常文本。"""
    allowed = {
        "stage",
        "status",
        "reason",
        "from_id",
        "to_id",
        "expected_count",
        "row_count",
        "logical_bytes",
        "compressed_bytes",
        "parts",
        "date",
    }
    item: Dict[str, Any] = {
        "ts": utc_stamp(),
        "event": event,
        "version": VERSION,
    }
    for key, value in fields.items():
        if key in allowed:
            item[key] = metadata(value)
    try:
        STATE_DIR.mkdir(mode=0o700, parents=True, exist_ok=True)
        os.chmod(STATE_DIR, 0o700)
        fd = os.open(str(LOG_FILE), os.O_WRONLY | os.O_APPEND | os.O_CREAT, 0o600)
        try:
            os.fchmod(fd, 0o600)
            line = (json.dumps(item, ensure_ascii=True, sort_keys=True) + "\n").encode("utf-8")
            os.write(fd, line)
        finally:
            os.close(fd)
    except OSError:
        # 记录失败不能改变 fail-closed 行为，也不能把任何上下文写到 stderr。
        pass


def safe_regular(path: Path, mode: Optional[int] = None) -> None:
    try:
        info = path.lstat()
    except OSError:
        raise ArchiveError("path_missing")
    if stat.S_ISLNK(info.st_mode) or not stat.S_ISREG(info.st_mode):
        raise ArchiveError("unsafe_path")
    if mode is not None and stat.S_IMODE(info.st_mode) != mode:
        raise ArchiveError("unsafe_permissions")


def safe_dir(path: Path, create: bool = False) -> None:
    try:
        info = path.lstat()
    except FileNotFoundError:
        if not create:
            raise ArchiveError("path_missing")
        try:
            path.mkdir(mode=0o700, parents=True, exist_ok=True)
        except OSError:
            raise ArchiveError("path_unavailable")
        info = path.lstat()
    except OSError:
        raise ArchiveError("path_unavailable")
    if stat.S_ISLNK(info.st_mode) or not stat.S_ISDIR(info.st_mode):
        raise ArchiveError("unsafe_path")
    try:
        os.chmod(path, 0o700)
    except OSError:
        raise ArchiveError("unsafe_permissions")


def run_capture(argv: Sequence[str], input_bytes: Optional[bytes] = None, timeout: int = 30) -> bytes:
    try:
        result = subprocess.run(
            list(argv),
            input=input_bytes,
            stdout=subprocess.PIPE,
            stderr=subprocess.DEVNULL,
            timeout=timeout,
            check=False,
            env=dict(os.environ, LC_ALL="C", LANG="C"),
        )
    except (OSError, subprocess.TimeoutExpired):
        raise ArchiveError("command_failed")
    if result.returncode != 0:
        raise ArchiveError("command_failed")
    return result.stdout


def read_json_file(path: Path, max_bytes: int) -> Dict[str, Any]:
    safe_regular(path)
    if path.stat().st_size > max_bytes:
        raise ArchiveError("metadata_too_large")
    try:
        with path.open("rb") as handle:
            value = json.loads(handle.read().decode("utf-8"))
    except (OSError, UnicodeError, ValueError):
        raise ArchiveError("metadata_invalid")
    if not isinstance(value, dict):
        raise ArchiveError("metadata_invalid")
    return value


def atomic_json(path: Path, value: Dict[str, Any]) -> None:
    safe_dir(path.parent, create=True)
    data = (json.dumps(value, ensure_ascii=True, sort_keys=True, separators=(",", ":")) + "\n").encode("utf-8")
    fd, temp_name = tempfile.mkstemp(prefix=".state.", dir=str(path.parent))
    temp_path = Path(temp_name)
    try:
        os.fchmod(fd, 0o600)
        with os.fdopen(fd, "wb") as handle:
            handle.write(data)
            handle.flush()
            os.fsync(handle.fileno())
        os.replace(str(temp_path), str(path))
        dir_fd = os.open(str(path.parent), os.O_RDONLY)
        try:
            os.fsync(dir_fd)
        finally:
            os.close(dir_fd)
    except OSError:
        try:
            temp_path.unlink()
        except OSError:
            pass
        raise ArchiveError("state_write_failed")


def default_state() -> Dict[str, Any]:
    return {
        "schema": 1,
        "last_deleted_id": 0,
        "last_success_date": "",
        "pending": None,
    }


def load_state() -> Dict[str, Any]:
    safe_dir(STATE_DIR, create=True)
    if not STATE_FILE.exists():
        return default_state()
    safe_regular(STATE_FILE, 0o600)
    state = read_json_file(STATE_FILE, MAX_MANIFEST_BYTES)
    if state.get("schema") != 1:
        raise ArchiveError("state_invalid")
    last_id = state.get("last_deleted_id")
    if not isinstance(last_id, int) or last_id < 0 or last_id > MAX_ID:
        raise ArchiveError("state_invalid")
    pending = state.get("pending")
    if pending is not None and not isinstance(pending, dict):
        raise ArchiveError("state_invalid")
    return state


def acquire_lock() -> Any:
    safe_dir(STATE_DIR, create=True)
    try:
        handle = LOCK_FILE.open("a+")
        os.fchmod(handle.fileno(), 0o600)
        fcntl.flock(handle.fileno(), fcntl.LOCK_EX | fcntl.LOCK_NB)
        return handle
    except BlockingIOError:
        raise ArchiveError("already_running")
    except OSError:
        raise ArchiveError("lock_failed")


def load_config() -> Dict[str, Any]:
    if not CONFIG_FILE.exists():
        raise ArchiveError("config_missing")
    safe_regular(CONFIG_FILE)
    if stat.S_IMODE(CONFIG_FILE.stat().st_mode) & 0o077:
        raise ArchiveError("config_permissions")
    config = read_json_file(CONFIG_FILE, 32 * 1024)
    target = config.get("ssh_target")
    if not isinstance(target, str) or not target or not TARGET_RE.fullmatch(target):
        raise ArchiveError("config_invalid")
    known_hosts = config.get("known_hosts")
    if not isinstance(known_hosts, str) or not known_hosts.startswith("/") or "\x00" in known_hosts:
        raise ArchiveError("config_invalid")
    remote_helper = config.get("remote_helper", "/usr/local/libexec/context-archive-remote")
    if not isinstance(remote_helper, str) or not REMOTE_PREFIX_RE.fullmatch(remote_helper):
        raise ArchiveError("config_invalid")
    identity = config.get("identity_file")
    if identity is not None and (not isinstance(identity, str) or not identity.startswith("/") or "\x00" in identity):
        raise ArchiveError("config_invalid")
    ssh_port = config.get("ssh_port", 22)
    if not isinstance(ssh_port, int) or isinstance(ssh_port, bool) or not 1 <= ssh_port <= 65535:
        raise ArchiveError("config_invalid")
    service = config.get("keychain_service", KEYCHAIN_SERVICE)
    account = config.get("keychain_account", KEYCHAIN_ACCOUNT)
    if not isinstance(service, str) or not isinstance(account, str) or not service or not account:
        raise ArchiveError("config_invalid")
    return {
        "ssh_target": target,
        "known_hosts": known_hosts,
        "remote_helper": remote_helper,
        "identity_file": identity,
        "ssh_port": ssh_port,
        "keychain_service": service,
        "keychain_account": account,
    }


def disk_free(path: Path) -> int:
    try:
        lines = run_capture(["/bin/df", "-Pk", str(path)], timeout=10).decode("ascii", "strict").splitlines()
        if len(lines) < 2:
            raise ValueError
        fields = lines[-1].split()
        if len(fields) < 4:
            raise ValueError
        return int(fields[3]) * 1024
    except (ArchiveError, UnicodeError, ValueError, IndexError):
        raise ArchiveError("disk_space_unavailable")


def diskutil_info(path: Path) -> Dict[str, Any]:
    try:
        value = plistlib.loads(run_capture(["/usr/sbin/diskutil", "info", "-plist", str(path)], timeout=20))
    except (ArchiveError, plistlib.InvalidFileException, ValueError, TypeError):
        raise ArchiveError("disk_unavailable")
    if not isinstance(value, dict):
        raise ArchiveError("disk_unavailable")
    return value


def first_value(value: Dict[str, Any], *names: str) -> Any:
    for name in names:
        if name in value:
            return value[name]
    return None


def preflight_external() -> None:
    info = diskutil_info(EXTERNAL_ROOT)
    mounted = first_value(info, "Mounted", "VolumeIsMounted")
    mountpoint = first_value(info, "MountPoint", "VolumeMountPoint")
    uuid = first_value(info, "VolumeUUID", "Volume UUID")
    # macOS 26 diskutil omits Mounted/VolumeIsMounted from plist output for
    # FAT volumes. An exact MountPoint plus the immutable VolumeUUID is the
    # authoritative mounted-volume check in that case.
    if (mounted is not None and mounted is not True) or mountpoint != str(EXTERNAL_ROOT) or uuid != VOLUME_UUID:
        raise ArchiveError("external_not_expected")
    safe_dir(EXTERNAL_AI, create=True)
    try:
        image_stat = SPARSEBUNDLE.lstat()
    except FileNotFoundError:
        image_stat = None
    except OSError:
        raise ArchiveError("path_unavailable")
    if image_stat is not None and stat.S_ISLNK(image_stat.st_mode):
        raise ArchiveError("unsafe_path")
    if disk_free(EXTERNAL_ROOT) < MIN_EXTERNAL_FREE:
        raise ArchiveError("external_space_low")


def keychain_password(config: Dict[str, Any]) -> bytes:
    try:
        result = subprocess.run(
            [
                "/usr/bin/security",
                "find-generic-password",
                "-s",
                config["keychain_service"],
                "-a",
                config["keychain_account"],
                "-w",
            ],
            stdout=subprocess.PIPE,
            stderr=subprocess.DEVNULL,
            timeout=15,
            check=False,
        )
    except (OSError, subprocess.TimeoutExpired):
        raise ArchiveError("keychain_unavailable")
    if result.returncode != 0:
        raise ArchiveError("keychain_unavailable")
    password = result.stdout.rstrip(b"\r\n")
    if not password:
        raise ArchiveError("keychain_empty")
    return password


class Attachment:
    def __init__(self, mountpoint: Path, device: str, owned: bool) -> None:
        self.mountpoint = mountpoint
        self.device = device
        self.owned = owned


def image_info_ok() -> None:
    if not SPARSEBUNDLE.exists():
        return
    if not SPARSEBUNDLE.is_dir():
        raise ArchiveError("image_invalid")
    try:
        info = plistlib.loads(run_capture(["/usr/bin/hdiutil", "imageinfo", "-plist", str(SPARSEBUNDLE)], timeout=30))
    except (ArchiveError, plistlib.InvalidFileException, ValueError, TypeError):
        raise ArchiveError("image_invalid")
    try:
        blob = json.dumps(info, ensure_ascii=True).lower()
    except (TypeError, ValueError):
        raise ArchiveError("image_invalid")
    if "sparsebundle" not in blob or "aes-256" not in blob:
        raise ArchiveError("image_not_encrypted_apfs")


def find_existing_attachment() -> Optional[Attachment]:
    try:
        info = plistlib.loads(run_capture(["/usr/bin/hdiutil", "info", "-plist"], timeout=30))
    except (ArchiveError, plistlib.InvalidFileException, ValueError, TypeError):
        return None
    for image in info.get("images", []):
        if not isinstance(image, dict):
            continue
        image_path = image.get("image-path")
        if image_path != str(SPARSEBUNDLE):
            continue
        for entity in image.get("system-entities", []):
            if not isinstance(entity, dict):
                continue
            mount = entity.get("mount-point")
            device = entity.get("dev-entry")
            if isinstance(mount, str) and isinstance(device, str):
                return Attachment(Path(mount), device, False)
    return None


def attach_image(password: bytes) -> Attachment:
    # 无论是否已有挂载，都先确认目标是 AES-256 sparsebundle；不能仅凭挂载点命名信任它。
    image_info_ok()
    existing = find_existing_attachment()
    if existing is not None:
        internal = diskutil_info(existing.mountpoint)
        fs = str(first_value(internal, "FilesystemType", "FilesystemPersonality", "FileSystemType") or "").lower()
        if "apfs" not in fs:
            raise ArchiveError("image_not_apfs")
        safe_dir(existing.mountpoint)
        return existing
    try:
        output = run_capture(
            ["/usr/bin/hdiutil", "attach", "-plist", "-nobrowse", "-stdinpass", str(SPARSEBUNDLE)],
            input_bytes=password + b"\n",
            timeout=120,
        )
        info = plistlib.loads(output)
    except (ArchiveError, plistlib.InvalidFileException, ValueError, TypeError):
        raise ArchiveError("image_attach_failed")
    for entity in info.get("system-entities", []):
        if not isinstance(entity, dict):
            continue
        mount = entity.get("mount-point")
        device = entity.get("dev-entry")
        if isinstance(mount, str) and isinstance(device, str):
            attachment = Attachment(Path(mount), device, True)
            internal = diskutil_info(attachment.mountpoint)
            fs = str(first_value(internal, "FilesystemType", "FilesystemPersonality", "FileSystemType") or "").lower()
            if "apfs" not in fs:
                raise ArchiveError("image_not_apfs")
            safe_dir(attachment.mountpoint)
            return attachment
    raise ArchiveError("image_mount_missing")


def create_image(password: bytes) -> None:
    if SPARSEBUNDLE.exists():
        image_info_ok()
        return
    if disk_free(EXTERNAL_ROOT) < MIN_EXTERNAL_FREE:
        raise ArchiveError("external_space_low")
    try:
        result = subprocess.run(
            [
                "/usr/bin/hdiutil",
                "create",
                "-quiet",
                "-nospotlight",
                "-type",
                "SPARSEBUNDLE",
                "-size",
                IMAGE_SIZE,
                "-fs",
                "APFS",
                "-volname",
                "ContextArchive",
                "-encryption",
                "AES-256",
                "-stdinpass",
                str(SPARSEBUNDLE),
            ],
            input=password + b"\n",
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
            timeout=300,
            check=False,
        )
    except (OSError, subprocess.TimeoutExpired):
        raise ArchiveError("image_create_failed")
    if result.returncode != 0:
        raise ArchiveError("image_create_failed")
    image_info_ok()


def detach_image(attachment: Attachment) -> None:
    try:
        result = subprocess.run(
            ["/usr/bin/hdiutil", "detach", attachment.device],
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
            timeout=120,
            check=False,
        )
    except (OSError, subprocess.TimeoutExpired):
        raise ArchiveError("image_detach_failed")
    if result.returncode != 0:
        raise ArchiveError("image_detach_failed")


def ssh_argv(config: Dict[str, Any], action: str, args: Iterable[int]) -> List[str]:
    command = config["remote_helper"] + " " + " ".join([action] + [str(int(x)) for x in args])
    argv = [
        "/usr/bin/ssh",
        "-T",
        "-oBatchMode=yes",
        "-oConnectTimeout=10",
        "-oServerAliveInterval=15",
        "-oServerAliveCountMax=3",
        "-oStrictHostKeyChecking=yes",
        "-oUserKnownHostsFile=" + config["known_hosts"],
        "-p",
        str(config["ssh_port"]),
    ]
    if config.get("identity_file"):
        argv.extend(["-i", config["identity_file"]])
    argv.extend([config["ssh_target"], command])
    return argv


def remote_json(config: Dict[str, Any], action: str, args: Iterable[int]) -> Dict[str, Any]:
    try:
        output = run_capture(ssh_argv(config, action, args), timeout=900 if action == "delete" else 60)
    except ArchiveError:
        raise ArchiveError("ssh_failed")
    if len(output) > MAX_META_BYTES:
        raise ArchiveError("remote_metadata_invalid")
    try:
        value = json.loads(output.decode("utf-8"))
    except (UnicodeError, ValueError):
        raise ArchiveError("remote_metadata_invalid")
    if not isinstance(value, dict):
        raise ArchiveError("remote_metadata_invalid")
    return value


def stream_export(config: Dict[str, Any], from_id: int, to_id: int, stage: Path) -> Dict[str, Any]:
    if stage.exists():
        if stage.is_symlink() or not stage.is_dir():
            raise ArchiveError("unsafe_path")
        shutil.rmtree(str(stage))
    stage.mkdir(mode=0o700, parents=False)
    ssh_proc: Optional[subprocess.Popen] = None
    parts: List[Dict[str, Any]] = []
    whole = hashlib.sha256()
    part_fp: Optional[Any] = None
    part_hash: Optional[Any] = None
    part_bytes = 0
    total = 0

    def close_part() -> None:
        nonlocal part_fp, part_hash, part_bytes
        if part_fp is None or part_hash is None:
            return
        try:
            part_fp.flush()
            os.fsync(part_fp.fileno())
            part_fp.close()
            temp = Path(part_fp.name)
            final = stage / ("payload-%06d.gz.part" % (len(parts) + 1))
            os.chmod(temp, 0o600)
            os.replace(str(temp), str(final))
            parts.append({"name": final.name, "bytes": part_bytes, "sha256": part_hash.hexdigest()})
        except OSError:
            raise ArchiveError("archive_write_failed")
        finally:
            part_fp = None
            part_hash = None
            part_bytes = 0

    try:
        env = dict(os.environ, LC_ALL="C", LANG="C")
        ssh_proc = subprocess.Popen(
            ssh_argv(config, "export", [from_id, to_id]),
            stdin=subprocess.DEVNULL,
            stdout=subprocess.PIPE,
            stderr=subprocess.DEVNULL,
            env=env,
        )
        assert ssh_proc.stdout is not None
        # helper 在生产机上先 gzip，再经 SSH 传输；本机只分片保存 gzip 流。
        while True:
            chunk = ssh_proc.stdout.read(1024 * 1024)
            if not chunk:
                break
            offset = 0
            while offset < len(chunk):
                if part_fp is None:
                    temp_fd, temp_name = tempfile.mkstemp(prefix=".payload.", dir=str(stage))
                    os.fchmod(temp_fd, 0o600)
                    part_fp = os.fdopen(temp_fd, "wb")
                    part_hash = hashlib.sha256()
                    part_bytes = 0
                take = min(PART_BYTES - part_bytes, len(chunk) - offset)
                segment = chunk[offset : offset + take]
                try:
                    part_fp.write(segment)
                except OSError:
                    raise ArchiveError("archive_write_failed")
                assert part_hash is not None
                part_hash.update(segment)
                whole.update(segment)
                part_bytes += take
                total += take
                offset += take
                if part_bytes == PART_BYTES:
                    close_part()
        close_part()
        ssh_rc = ssh_proc.wait(timeout=60)
        if ssh_rc != 0:
            raise ArchiveError("export_failed")
    except (OSError, subprocess.TimeoutExpired, BrokenPipeError):
        raise ArchiveError("export_failed")
    finally:
        if part_fp is not None:
            try:
                part_fp.close()
            except OSError:
                pass
        for proc in (ssh_proc,):
            if proc is not None and proc.poll() is None:
                try:
                    proc.terminate()
                    proc.wait(timeout=5)
                except (OSError, subprocess.TimeoutExpired):
                    try:
                        proc.kill()
                    except OSError:
                        pass
    if not parts or total == 0:
        raise ArchiveError("export_empty")
    return {
        "parts": parts,
        "compressed_bytes": total,
        "compressed_sha256": whole.hexdigest(),
    }


class PartReader(io.RawIOBase):
    def __init__(self, paths: Sequence[Path]) -> None:
        super().__init__()
        self.paths = list(paths)
        self.index = 0
        self.current: Optional[Any] = None

    def readable(self) -> bool:
        return True

    def read(self, size: int = -1) -> bytes:
        output = bytearray()
        remaining = size
        while remaining != 0 and self.index < len(self.paths):
            if self.current is None:
                self.current = self.paths[self.index].open("rb")
            want = -1 if remaining < 0 else remaining
            data = self.current.read(want)
            if data:
                output.extend(data)
                if remaining >= 0:
                    remaining -= len(data)
            else:
                self.current.close()
                self.current = None
                self.index += 1
        return bytes(output)

    def close(self) -> None:
        if self.current is not None:
            self.current.close()
            self.current = None
        super().close()


def validate_manifest_shape(manifest: Dict[str, Any]) -> None:
    if manifest.get("schema") != 1 or tuple(manifest.get("header", [])) != HEADER:
        raise ArchiveError("manifest_invalid")
    for key in ("from_id", "to_id", "expected_count", "first_id", "last_id", "logical_bytes", "compressed_bytes"):
        value = manifest.get(key)
        if not isinstance(value, int) or value < 0 or value > MAX_ID * 2:
            raise ArchiveError("manifest_invalid")
    if manifest["from_id"] > manifest["to_id"] or manifest["expected_count"] == 0:
        raise ArchiveError("manifest_invalid")
    if manifest["first_id"] < 0 or manifest["last_id"] < manifest["first_id"]:
        raise ArchiveError("manifest_invalid")
    parts = manifest.get("parts")
    if not isinstance(parts, list) or not parts:
        raise ArchiveError("manifest_invalid")
    if not isinstance(manifest.get("compressed_sha256"), str) or not re.fullmatch(r"[0-9a-f]{64}", manifest["compressed_sha256"]):
        raise ArchiveError("manifest_invalid")


def validate_batch(batch: Path) -> Dict[str, Any]:
    safe_dir(batch)
    manifest_path = batch / "manifest.json"
    safe_regular(manifest_path, 0o600)
    manifest = read_json_file(manifest_path, MAX_MANIFEST_BYTES)
    validate_manifest_shape(manifest)
    part_paths: List[Path] = []
    whole = hashlib.sha256()
    total = 0
    for part in manifest["parts"]:
        if not isinstance(part, dict):
            raise ArchiveError("manifest_invalid")
        name = part.get("name")
        size = part.get("bytes")
        digest = part.get("sha256")
        if not isinstance(name, str) or not re.fullmatch(r"payload-[0-9]{6}\.gz\.part", name):
            raise ArchiveError("manifest_invalid")
        if not isinstance(size, int) or size <= 0 or not isinstance(digest, str) or not re.fullmatch(r"[0-9a-f]{64}", digest):
            raise ArchiveError("manifest_invalid")
        path = batch / name
        safe_regular(path, 0o600)
        if path.stat().st_size != size:
            raise ArchiveError("hash_mismatch")
        hasher = hashlib.sha256()
        try:
            with path.open("rb") as handle:
                while True:
                    chunk = handle.read(1024 * 1024)
                    if not chunk:
                        break
                    hasher.update(chunk)
                    whole.update(chunk)
        except OSError:
            raise ArchiveError("archive_read_failed")
        if hasher.hexdigest() != digest:
            raise ArchiveError("hash_mismatch")
        total += size
        part_paths.append(path)
    if total != manifest["compressed_bytes"] or whole.hexdigest() != manifest["compressed_sha256"]:
        raise ArchiveError("hash_mismatch")

    count = 0
    first: Optional[int] = None
    last: Optional[int] = None
    previous: Optional[int] = None
    logical_bytes = 0
    reader = PartReader(part_paths)
    try:
        with io.TextIOWrapper(__import__("gzip").GzipFile(fileobj=reader, mode="rb"), encoding="utf-8", newline="") as text:
            rows = csv.reader(text)
            try:
                header = tuple(next(rows))
            except StopIteration:
                raise ArchiveError("header_invalid")
            if header != HEADER:
                raise ArchiveError("header_invalid")
            for row in rows:
                if len(row) != len(HEADER):
                    raise ArchiveError("row_invalid")
                try:
                    event_id = int(row[0], 10)
                except (ValueError, TypeError):
                    raise ArchiveError("row_invalid")
                if event_id < 0 or event_id > MAX_ID or not ID_RE.fullmatch(row[0]):
                    raise ArchiveError("row_invalid")
                if not (manifest["from_id"] < event_id <= manifest["to_id"]):
                    raise ArchiveError("range_invalid")
                if previous is not None and event_id <= previous:
                    raise ArchiveError("order_invalid")
                previous = event_id
                first = event_id if first is None else first
                last = event_id
                count += 1
                logical_bytes += sum(len(value.encode("utf-8")) for value in row)
                if count > manifest["expected_count"]:
                    raise ArchiveError("row_count_invalid")
    except ArchiveError:
        raise
    except (OSError, UnicodeError, csv.Error, EOFError, __import__("gzip").BadGzipFile):
        raise ArchiveError("gzip_invalid")
    finally:
        reader.close()
    if count != manifest["expected_count"] or first != manifest["first_id"] or last != manifest["last_id"]:
        raise ArchiveError("row_count_invalid")
    if manifest["logical_bytes"] != logical_bytes:
        raise ArchiveError("logical_bytes_invalid")
    return manifest


def write_manifest(stage: Path, snapshot: Dict[str, Any], exported: Dict[str, Any]) -> Dict[str, Any]:
    required = ("to_id", "row_count", "first_id", "last_id", "logical_bytes")
    if any(not isinstance(snapshot.get(k), int) for k in required):
        raise ArchiveError("remote_metadata_invalid")
    if snapshot["row_count"] <= 0 or snapshot["to_id"] < snapshot["from_id"]:
        raise ArchiveError("remote_metadata_invalid")
    manifest: Dict[str, Any] = {
        "schema": 1,
        "created_at": utc_stamp(),
        "from_id": snapshot["from_id"],
        "to_id": snapshot["to_id"],
        "expected_count": snapshot["row_count"],
        "first_id": snapshot["first_id"],
        "last_id": snapshot["last_id"],
        "logical_bytes": snapshot["logical_bytes"],
        "compressed_bytes": exported["compressed_bytes"],
        "compressed_sha256": exported["compressed_sha256"],
        "header": list(HEADER),
        "parts": exported["parts"],
    }
    atomic_json(stage / "manifest.json", manifest)
    return manifest


def seal_stage(stage: Path, final: Path) -> None:
    if final.exists():
        validate_batch(final)
        return
    safe_dir(stage)
    manifest = stage / "manifest.json"
    safe_regular(manifest, 0o600)
    try:
        os.replace(str(stage), str(final))
        dir_fd = os.open(str(final.parent), os.O_RDONLY)
        try:
            os.fsync(dir_fd)
        finally:
            os.close(dir_fd)
    except OSError:
        raise ArchiveError("archive_seal_failed")


def image_paths(mount: Path) -> Tuple[Path, Path]:
    batches = mount / "batches"
    staging = mount / ".staging"
    safe_dir(batches, create=True)
    safe_dir(staging, create=True)
    return batches, staging


def batch_id(from_id: int, to_id: int) -> str:
    return "%020d-%020d" % (from_id, to_id)


def due_today(state: Dict[str, Any], current: dt.datetime) -> bool:
    if state.get("last_success_date") == current.date().isoformat():
        return False
    return (current.hour, current.minute, current.second) >= (3, 30, 0)


def check_snapshot(snapshot: Dict[str, Any], from_id: int) -> None:
    if snapshot.get("schema") != 1 or snapshot.get("from_id") != from_id:
        raise ArchiveError("remote_metadata_invalid")
    for key in ("to_id", "row_count", "first_id", "last_id", "logical_bytes"):
        value = snapshot.get(key)
        if not isinstance(value, int) or value < 0 or value > MAX_ID * 2:
            raise ArchiveError("remote_metadata_invalid")
    if snapshot["to_id"] < from_id:
        raise ArchiveError("remote_metadata_invalid")
    if snapshot["row_count"] == 0:
        if snapshot["to_id"] != from_id or snapshot.get("first_id") not in (None, 0) or snapshot.get("last_id") not in (None, 0):
            raise ArchiveError("remote_metadata_invalid")
    else:
        if snapshot["first_id"] <= from_id or snapshot["first_id"] > snapshot["last_id"] or snapshot["last_id"] > snapshot["to_id"]:
            raise ArchiveError("remote_metadata_invalid")


def ensure_space(mount: Path, logical_bytes: int) -> None:
    free = disk_free(mount)
    needed = min(MAX_ID * 2, max(MIN_IMAGE_FREE, logical_bytes + MIN_IMAGE_FREE))
    if free < needed:
        raise ArchiveError("image_space_low")


def run_once(dry_run: bool = False) -> int:
    lock = acquire_lock()
    try:
        config = load_config()
        state = load_state()
        current = now_bj()
        pending = state.get("pending")
        if pending is None and not dry_run and not due_today(state, current):
            log_event("skip", status="not_due", date=current.date().isoformat())
            return EXIT_OK
        preflight_external()
        if dry_run and pending is not None:
            # dry-run 永不触碰 Keychain、sparsebundle、导出流或远端 delete。
            log_event(
                "dry_run",
                status="pending",
                from_id=pending.get("from_id"),
                to_id=pending.get("to_id"),
                expected_count=pending.get("expected_count"),
            )
            return EXIT_OK
        snapshot: Optional[Dict[str, Any]] = None
        if pending is None:
            from_id = state["last_deleted_id"]
            snapshot = remote_json(config, "snapshot", [from_id])
            check_snapshot(snapshot, from_id)
            log_event("snapshot", status="ok", from_id=from_id, to_id=snapshot["to_id"], expected_count=snapshot["row_count"], logical_bytes=snapshot["logical_bytes"])
            if dry_run:
                log_event("dry_run", status="ok", from_id=from_id, to_id=snapshot["to_id"], expected_count=snapshot["row_count"])
                return EXIT_OK
            if snapshot["row_count"] == 0:
                state["last_success_date"] = current.date().isoformat()
                atomic_json(STATE_FILE, state)
                log_event("complete", status="empty", date=current.date().isoformat())
                return EXIT_OK
            pending = {
                "from_id": from_id,
                "to_id": snapshot["to_id"],
                "expected_count": snapshot["row_count"],
                "first_id": snapshot["first_id"],
                "last_id": snapshot["last_id"],
                "logical_bytes": snapshot["logical_bytes"],
                "phase": "snapshot",
            }
            state["pending"] = pending
            atomic_json(STATE_FILE, state)
        else:
            required = ("from_id", "to_id", "expected_count", "first_id", "last_id", "logical_bytes")
            if any(not isinstance(pending.get(k), int) for k in required):
                raise ArchiveError("state_invalid")
            if pending["from_id"] < 0 or pending["from_id"] >= pending["to_id"] or pending["expected_count"] <= 0:
                raise ArchiveError("state_invalid")
        from_id = int(pending["from_id"])
        to_id = int(pending["to_id"])
        expected = int(pending["expected_count"])
        bid = batch_id(from_id, to_id)
        password = keychain_password(config)
        create_image(password)
        attachment = attach_image(password)
        detached = False
        try:
            batches, staging = image_paths(attachment.mountpoint)
            final = batches / bid
            if final.exists():
                manifest = validate_batch(final)
            else:
                ensure_space(attachment.mountpoint, int(pending["logical_bytes"]))
                stage = staging / bid
                exported = stream_export(config, from_id, to_id, stage)
                manifest = write_manifest(stage, pending, exported)
                validate_batch(stage)
                seal_stage(stage, final)
                manifest = validate_batch(final)
            if (
                manifest["from_id"] != from_id
                or manifest["to_id"] != to_id
                or manifest["expected_count"] != expected
                or manifest["first_id"] != pending["first_id"]
                or manifest["last_id"] != pending["last_id"]
                or manifest["logical_bytes"] != pending["logical_bytes"]
            ):
                raise ArchiveError("manifest_range_invalid")
            pending["phase"] = "sealed"
            state["pending"] = pending
            atomic_json(STATE_FILE, state)
            os.sync()
            detach_image(attachment)
            detached = True
        finally:
            if not detached:
                # 不使用 hdiutil -force；未成功卸载时绝不执行远端 delete。
                try:
                    detach_image(attachment)
                except ArchiveError:
                    pass
        # 在 delete 前再次确认外接卷仍在、UUID 正确且有保留空间；期间被拔盘也 fail closed。
        preflight_external()
        delete_result = remote_json(config, "delete", [from_id, to_id, expected])
        status = delete_result.get("status")
        if status not in ("DELETED", "ALREADY_EMPTY") or delete_result.get("expected_count") != expected:
            raise ArchiveError("remote_delete_rejected")
        state["last_deleted_id"] = to_id
        state["last_success_date"] = current.date().isoformat()
        state["pending"] = None
        atomic_json(STATE_FILE, state)
        log_event("complete", status=status.lower(), from_id=from_id, to_id=to_id, expected_count=expected)
        return EXIT_OK
    except ArchiveError as error:
        log_event("failure", status="failed", reason=error.code)
        return EXIT_OK if error.code == "already_running" else EXIT_TEMPFAIL
    except Exception:
        # 防止意外异常把外部命令输出带入日志/终端；此分支同样不触发远端删除。
        log_event("failure", status="failed", reason="internal_error")
        return EXIT_TEMPFAIL
    finally:
        lock.close()


def print_status() -> int:
    try:
        state = load_state()
    except ArchiveError as error:
        print(json.dumps({"status": "error", "reason": error.code}, ensure_ascii=True))
        return EXIT_TEMPFAIL
    pending = state.get("pending")
    result = {
        "version": VERSION,
        "last_deleted_id": state.get("last_deleted_id"),
        "last_success_date": state.get("last_success_date"),
        "pending": pending,
    }
    print(json.dumps(result, ensure_ascii=True, sort_keys=True))
    return EXIT_OK


def main(argv: Optional[Sequence[str]] = None) -> int:
    parser = argparse.ArgumentParser(description="context archive operator")
    sub = parser.add_subparsers(dest="command")
    sub.add_parser("run")
    sub.add_parser("status")
    dry = sub.add_parser("dry-run")
    dry.set_defaults(dry_run=True)
    args = parser.parse_args(argv)
    if args.command == "status":
        return print_status()
    if args.command == "dry-run":
        return run_once(dry_run=True)
    if args.command == "run":
        return run_once(dry_run=False)
    parser.print_usage(sys.stderr)
    return EXIT_USAGE


if __name__ == "__main__":
    sys.exit(main())
