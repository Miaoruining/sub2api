#!/usr/bin/env python3
"""生产机 SSH forced-command helper。

安装到生产机后由 authorized_keys 的 command= 强制调用。本文件不接受 SQL、表名、
列名或任意 shell 参数；SSH_ORIGINAL_COMMAND 只允许 snapshot/export/delete。
"""

from __future__ import annotations

import json
import os
import re
import stat
import subprocess
import sys
from typing import List, Optional, Tuple


PSQL = "/usr/local/libexec/context-archive-psql"
MAX_ID = 9223372036854775807
ARG_RE = re.compile(r"^[0-9]+$")
PREFIXES = ("", "context-archive-remote", "/usr/local/libexec/context-archive-remote")


def fail() -> int:
    # 不向 SSH 客户端返回数据库错误、SQL 或数据正文。
    sys.stderr.write("context archive command rejected\n")
    return 126


def parse_command(raw: Optional[str]) -> Optional[Tuple[str, List[int]]]:
    if not isinstance(raw, str) or "\x00" in raw:
        return None
    words = raw.split(" ")
    if any(not word for word in words):
        return None
    if words and words[0] in PREFIXES:
        words = words[1:]
    if len(words) not in (2, 3, 4):
        return None
    action = words[0] if words else ""
    if action not in ("snapshot", "export", "delete"):
        return None
    # snapshot 只接收 from；export 接收 from/to；delete 另加 expected count。
    expected_args = {"snapshot": 1, "export": 2, "delete": 3}[action]
    if len(words) - 1 != expected_args:
        return None
    values: List[int] = []
    for word in words[1:]:
        if not ARG_RE.fullmatch(word):
            return None
        try:
            value = int(word, 10)
        except ValueError:
            return None
        if value < 0 or value > MAX_ID:
            return None
        values.append(value)
    if action in ("export", "delete") and values[0] >= values[1]:
        return None
    if action == "delete" and values[2] <= 0:
        return None
    return action, values


def psql(sql: str) -> int:
    # 该 wrapper 必须是 root-owned、0755，内部使用 root-only .pgpass/Unix socket。
    if not trusted_executable(PSQL):
        return 127
    try:
        result = subprocess.run(
            [PSQL],
            input=sql.encode("utf-8"),
            stdout=subprocess.PIPE,
            stderr=subprocess.DEVNULL,
            env=dict(os.environ, LC_ALL="C", LANG="C", PGAPPNAME="context-archive", PGCLIENTENCODING="UTF8"),
            timeout=900,
            check=False,
        )
    except (OSError, subprocess.TimeoutExpired, BrokenPipeError):
        return 1
    if result.returncode != 0:
        return result.returncode
    output = result.stdout
    if len(output) > 64 * 1024:
        return 1
    # snapshot/delete 的 stdout 是一行元数据 JSON，绝不转发任意数据库输出。
    lines = output.splitlines()
    if len(lines) != 1:
        return 1
    try:
        value = json.loads(lines[0].decode("utf-8"))
    except (UnicodeError, ValueError):
        return 1
    if not isinstance(value, dict):
        return 1
    print(json.dumps(value, ensure_ascii=True, separators=(",", ":")))
    return 0


def psql_gzip(sql: str) -> int:
    """在生产机压缩后才进入 SSH，避免明文 CSV 经过网络传输。"""
    if not trusted_executable(PSQL):
        return 127
    psql_proc: Optional[subprocess.Popen] = None
    gzip_proc: Optional[subprocess.Popen] = None
    try:
        env = dict(os.environ, LC_ALL="C", LANG="C", PGAPPNAME="context-archive", PGCLIENTENCODING="UTF8")
        psql_proc = subprocess.Popen(
            [PSQL],
            stdin=subprocess.PIPE,
            stdout=subprocess.PIPE,
            stderr=subprocess.DEVNULL,
            env=env,
        )
        assert psql_proc.stdin is not None and psql_proc.stdout is not None
        gzip_proc = subprocess.Popen(
            ["/usr/bin/gzip", "-9n", "-c"],
            stdin=psql_proc.stdout,
            stdout=sys.stdout.buffer,
            stderr=subprocess.DEVNULL,
            env=env,
        )
        psql_proc.stdout.close()
        psql_proc.stdin.write(sql.encode("utf-8"))
        psql_proc.stdin.close()
        gzip_rc = gzip_proc.wait(timeout=3600)
        psql_rc = psql_proc.wait(timeout=60)
        return 0 if gzip_rc == 0 and psql_rc == 0 else 1
    except (OSError, subprocess.TimeoutExpired, BrokenPipeError):
        return 1
    finally:
        for process in (gzip_proc, psql_proc):
            if process is not None and process.poll() is None:
                try:
                    process.terminate()
                    process.wait(timeout=5)
                except (OSError, subprocess.TimeoutExpired):
                    try:
                        process.kill()
                    except OSError:
                        pass


def trusted_executable(path: str) -> bool:
    try:
        info = os.lstat(path)
    except OSError:
        return False
    return (
        stat.S_ISREG(info.st_mode)
        and info.st_uid == 0
        and not (info.st_mode & 0o022)
        and bool(info.st_mode & 0o111)
    )


def snapshot(from_id: int) -> int:
    sql = r"""
\set ON_ERROR_STOP on
\pset tuples_only on
\pset format unaligned
BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;
WITH high AS (
  SELECT GREATEST({from_id}::bigint, COALESCE(MAX(a.id), 0)::bigint) AS to_id
  FROM public.request_context_archives a
), rows_in_range AS (
  SELECT a.id, a.request_id, a.protocol, a.model, a.stage,
         a.context_payload, a.content_hash, a.message_count, a.created_at
  FROM public.request_context_archives a
  CROSS JOIN high h
  WHERE a.id > {from_id}::bigint AND a.id <= h.to_id
)
SELECT json_build_object(
  'schema', 1,
  'from_id', {from_id}::bigint,
  'to_id', h.to_id,
  'row_count', COUNT(r.id),
  'first_id', COALESCE(MIN(r.id), 0),
  'last_id', COALESCE(MAX(r.id), 0),
  'logical_bytes', COALESCE(SUM(octet_length(
      COALESCE(r.id::text, '') || COALESCE(r.request_id::text, '') || COALESCE(r.protocol::text, '')
      || COALESCE(r.model::text, '') || COALESCE(r.stage::text, '')
      || COALESCE(r.context_payload::text, '') || COALESCE(r.content_hash::text, '')
      || COALESCE(r.message_count::text, '') || COALESCE(r.created_at::text, '')
  )), 0)
)::text
FROM high h LEFT JOIN rows_in_range r ON TRUE
GROUP BY h.to_id;
COMMIT;
""".format(from_id=from_id)
    return psql(sql)


def export(from_id: int, to_id: int) -> int:
    sql = r"""
\set ON_ERROR_STOP on
\pset tuples_only on
BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;
COPY (
  SELECT a.id, a.request_id, a.protocol, a.model, a.stage,
         a.context_payload, a.content_hash, a.message_count, a.created_at
  FROM public.request_context_archives a
  WHERE a.id > {from_id}::bigint AND a.id <= {to_id}::bigint
  ORDER BY a.id ASC
) TO STDOUT WITH (FORMAT csv, HEADER true, FORCE_QUOTE *);
COMMIT;
""".format(from_id=from_id, to_id=to_id)
    return psql_gzip(sql)


def delete(from_id: int, to_id: int, expected: int) -> int:
    # 候选行在同一事务内锁定、计数、删除；计数不符时只提交 no-op 结果。
    sql = r"""
\set ON_ERROR_STOP on
\pset tuples_only on
\pset format unaligned
BEGIN;
SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '10min';
CREATE TEMP TABLE ca_candidates ON COMMIT DROP AS
  SELECT a.id
  FROM public.request_context_archives a
  WHERE a.id > {from_id}::bigint AND a.id <= {to_id}::bigint
  FOR UPDATE;
CREATE TEMP TABLE ca_result(status text, deleted_rows bigint)
  ON COMMIT PRESERVE ROWS;
INSERT INTO ca_result VALUES ('PENDING', 0);
DO $do$
DECLARE
  n bigint;
  d bigint;
BEGIN
  SELECT COUNT(*) INTO n FROM ca_candidates;
  IF n = 0 THEN
    UPDATE ca_result SET status = 'ALREADY_EMPTY';
  ELSIF n <> {expected}::bigint THEN
    UPDATE ca_result SET status = 'COUNT_MISMATCH';
  ELSE
    DELETE FROM public.request_context_archives a
      USING ca_candidates c
      WHERE a.id = c.id;
    GET DIAGNOSTICS d = ROW_COUNT;
    IF d <> {expected}::bigint THEN
      RAISE EXCEPTION 'archive delete row count changed';
    END IF;
    UPDATE ca_result SET status = 'DELETED', deleted_rows = d;
  END IF;
END
$do$;
SELECT json_build_object(
  'status', status,
  'deleted_rows', deleted_rows,
  'expected_count', {expected}::bigint
)::text FROM ca_result;
COMMIT;
""".format(from_id=from_id, to_id=to_id, expected=expected)
    return psql(sql)


def main() -> int:
    command = parse_command(os.environ.get("SSH_ORIGINAL_COMMAND"))
    if command is None:
        return fail()
    action, args = command
    if action == "snapshot":
        return snapshot(args[0])
    if action == "export":
        return export(args[0], args[1])
    if action == "delete":
        return delete(args[0], args[1], args[2])
    return fail()


if __name__ == "__main__":
    sys.exit(main())
