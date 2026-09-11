#!/usr/bin/env python3
"""只读数据库取专用 key，对复合组执行安全生产验收探针。"""
from __future__ import annotations

import argparse
import json
import re
import subprocess
import sys
import time
import uuid
from datetime import datetime, timezone
from pathlib import Path
from urllib.error import HTTPError, URLError
from urllib.parse import urlsplit
from urllib.request import (HTTPRedirectHandler, ProxyHandler, Request,
                            build_opener)

ROOT = Path(__file__).resolve().parents[1]
DEFAULT_ROUTES = ROOT / "docs" / "composite-source-routes.example.json"
MAX_BODY = 8 * 1024 * 1024
NORMAL_STOPS = {"end_turn", "stop_sequence", "max_tokens"}
TOOL_NAME = "echo_probe"
TOOL_ARGUMENTS = {"status": "ok"}


def echo_tool(protocol: str) -> dict:
    schema = {"type": "object", "properties": {"status": {"type": "string", "enum": ["ok"]}},
              "required": ["status"], "additionalProperties": False}
    if protocol == "messages":
        return {"name": TOOL_NAME, "description": "Return the probe status.", "input_schema": schema}
    return {"type": "function", "name": TOOL_NAME, "description": "Return the probe status.",
            "parameters": schema, "strict": True}


def tool_arguments() -> str:
    return json.dumps(TOOL_ARGUMENTS, separators=(",", ":"))


class NoRedirect(HTTPRedirectHandler):
    def redirect_request(self, request, *args, **kwargs):
        return None


OPENER = build_opener(ProxyHandler({}), NoRedirect())


def fail(message: str) -> None:
    print(f"FAIL {message}", file=sys.stderr)
    raise SystemExit(2)


def psql(args: list[str], sql: str) -> str:
    cmd = ["docker", "exec", "-i", "sub2api-postgres", "psql", "-U", "sub2api", "-d", "sub2api",
           "-AtX", "-q", "-v", "ON_ERROR_STOP=1", *args]
    try:
        result = subprocess.run(cmd, input=sql, text=True, capture_output=True, check=False)
    except OSError as exc:
        fail(f"database command unavailable ({type(exc).__name__})")
    if result.returncode:
        fail(f"database query failed (exit={result.returncode})")
    return result.stdout


def read_key(name: str, group_id: int) -> tuple[int, str]:
    sql = """
SELECT ak.id, ak.key
FROM api_keys ak JOIN users u ON u.id=ak.user_id JOIN groups g ON g.id=ak.group_id
WHERE ak.name=:'key_name' AND ak.group_id=:group_id
  AND ak.status='active' AND ak.deleted_at IS NULL
  AND u.role='admin' AND u.status='active' AND u.deleted_at IS NULL
  AND g.status='active' AND g.deleted_at IS NULL
"""
    out = psql(["-v", f"key_name={name}", "-v", f"group_id={group_id}", "-F", "\t"], sql)
    rows = [line.split("\t", 1) for line in out.splitlines() if line.strip()]
    if len(rows) != 1 or len(rows[0]) != 2 or not rows[0][0].isdigit() or not rows[0][1]:
        fail("key name/group did not resolve to exactly one active admin key")
    return int(rows[0][0]), rows[0][1]


def load_routes(path: str) -> set[str]:
    try:
        raw = json.loads(Path(path).read_text(encoding="utf-8"))
        routes = raw.get("routes", raw) if isinstance(raw, dict) else raw
        models = [str(route["public_model"]) for route in routes]
    except (OSError, ValueError, TypeError, KeyError) as exc:
        fail(f"invalid routes file ({type(exc).__name__})")
    expected = set(models)
    if len(models) != 29 or len(expected) != 29 or any(not model for model in models):
        fail(f"routes file must contain 29 unique public_model entries (got={len(models)})")
    return expected


def http_call(base: str, path: str, key: str, payload: dict | None, timeout: float,
              accept: str = "application/json") -> tuple[int | None, bytes]:
    body = None if payload is None else json.dumps(payload, separators=(",", ":")).encode()
    headers = {"Authorization": f"Bearer {key}", "x-api-key": key, "Accept": accept,
               "anthropic-version": "2023-06-01",
               "X-Request-ID": str(uuid.uuid4()), "X-Client-Request-ID": str(uuid.uuid4())}
    if body is not None:
        headers["Content-Type"] = "application/json"
    request = Request(base.rstrip("/") + path, data=body, headers=headers,
                      method="GET" if body is None else "POST")
    try:
        with OPENER.open(request, timeout=timeout) as response:
            return response.status, response.read(MAX_BODY + 1)
    except HTTPError as exc:
        try:
            exc.read(MAX_BODY + 1)
        except OSError:
            pass
        return exc.code, b""
    except (URLError, OSError, TimeoutError):
        return None, b""


def sse_events(body: bytes):
    if len(body) > MAX_BODY:
        raise ValueError("stream body exceeds safety limit")
    text = body.decode("utf-8")
    for frame in re.split(r"\r?\n\r?\n", text):
        data = [line[5:].lstrip() for line in frame.splitlines() if line.startswith("data:")]
        joined = "\n".join(data).strip()
        if joined and joined != "[DONE]":
            yield json.loads(joined)


def safe_usage(raw: dict) -> dict:
    allowed = {"input_tokens", "output_tokens", "total_tokens", "prompt_tokens",
               "completion_tokens", "cached_tokens", "cache_creation_input_tokens",
               "cache_read_input_tokens", "image_tokens"}
    if not isinstance(raw, dict):
        return {}
    return {key: value for key, value in raw.items()
            if key in allowed and isinstance(value, (int, float))}


def parse_messages(body: bytes) -> tuple[bool, str, dict]:
    stop, stopped, usage = None, False, {}
    try:
        for event in sse_events(body):
            if not isinstance(event, dict):
                return False, "invalid_sse_event", usage
            kind = event.get("type")
            if kind == "error":
                return False, "error_event", usage
            if kind == "message_start":
                usage.update(safe_usage((event.get("message") or {}).get("usage") or {}))
            elif kind == "message_delta":
                stop = (event.get("delta") or {}).get("stop_reason") or stop
                usage.update(safe_usage(event.get("usage") or {}))
            elif kind == "message_stop":
                stopped = True
    except (UnicodeDecodeError, ValueError, json.JSONDecodeError):
        return False, "invalid_sse", usage
    if not stopped:
        return False, "missing_message_stop", usage
    if stop not in NORMAL_STOPS:
        return False, f"unexpected_stop:{stop or 'missing'}", usage
    return True, stop, usage


def parse_messages_tool_call(body: bytes) -> tuple[bool, str, dict, dict]:
    stop, stopped, usage = None, False, {}
    call = {"id": "", "name": "", "arguments": ""}
    try:
        for event in sse_events(body):
            if not isinstance(event, dict):
                return False, "invalid_sse_event", usage, call
            kind = event.get("type")
            if kind == "error":
                return False, "error_event", usage, call
            if kind == "message_start":
                usage.update(safe_usage((event.get("message") or {}).get("usage") or {}))
            elif kind == "content_block_start":
                block = event.get("content_block") or {}
                if block.get("type") == "tool_use":
                    call["id"] = block.get("id") or ""
                    call["name"] = block.get("name") or ""
                    if isinstance(block.get("input"), dict) and block["input"]:
                        call["arguments"] = json.dumps(block["input"], separators=(",", ":"))
            elif kind == "content_block_delta":
                delta = event.get("delta") or {}
                if delta.get("type") == "input_json_delta":
                    call["arguments"] += delta.get("partial_json") or ""
            elif kind == "message_delta":
                stop = (event.get("delta") or {}).get("stop_reason") or stop
                usage.update(safe_usage(event.get("usage") or {}))
            elif kind == "message_stop":
                stopped = True
    except (TypeError, UnicodeDecodeError, ValueError, json.JSONDecodeError):
        return False, "invalid_sse", usage, call
    if not stopped:
        return False, "missing_message_stop", usage, call
    if stop != "tool_use":
        return False, f"unexpected_tool_stop:{stop or 'missing'}", usage, call
    if not call["id"] or not call["name"] or call["name"] != TOOL_NAME:
        return False, "unexpected_tool_call", usage, call
    try:
        if json.loads(call["arguments"] or "{}") != TOOL_ARGUMENTS:
            return False, "tool_arguments_mismatch", usage, call
    except (TypeError, ValueError, json.JSONDecodeError):
        return False, "invalid_tool_arguments", usage, call
    return True, stop, usage, call


def parse_responses(body: bytes) -> tuple[bool, str, dict]:
    terminal, usage = None, {}
    try:
        for event in sse_events(body):
            if not isinstance(event, dict):
                return False, "invalid_sse_event", usage
            kind = event.get("type")
            response = event.get("response") or {}
            if kind == "response.completed":
                terminal = "completed"
                usage.update(safe_usage(response.get("usage") or {}))
            elif kind in {"response.failed", "response.incomplete", "response.cancelled", "response.canceled"}:
                return False, kind, usage
    except (UnicodeDecodeError, ValueError, json.JSONDecodeError):
        return False, "invalid_sse", usage
    if terminal != "completed":
        return False, "missing_response_completed", usage
    return True, terminal, usage


def parse_responses_tool_call(body: bytes) -> tuple[bool, str, dict, dict]:
    terminal, usage, output_index = None, {}, None
    call = {"id": "", "call_id": "", "name": "", "arguments": ""}
    try:
        for event in sse_events(body):
            if not isinstance(event, dict):
                return False, "invalid_sse_event", usage, call
            kind = event.get("type")
            item = event.get("item") or {}
            if kind in {"response.output_item.added", "response.output_item.done"} and item.get("type") == "function_call":
                output_index = event.get("output_index", output_index)
                for field in ("id", "call_id", "name"):
                    if item.get(field):
                        call[field] = item[field]
                if item.get("arguments"):
                    call["arguments"] = item["arguments"]
            elif kind in {"response.function_call_arguments.delta", "response.function_call_arguments.done"}:
                if output_index is None or event.get("output_index") == output_index:
                    for field in ("call_id", "name"):
                        if event.get(field):
                            call[field] = event[field]
                    if kind.endswith(".delta"):
                        call["arguments"] += event.get("delta") or ""
                    elif event.get("arguments") is not None:
                        call["arguments"] = event["arguments"]
            elif kind == "response.completed":
                terminal = "completed"
                response = event.get("response") or {}
                usage.update(safe_usage(response.get("usage") or {}))
                if not call["name"]:
                    for output in response.get("output") or []:
                        if output.get("type") == "function_call":
                            for field in ("id", "call_id", "name", "arguments"):
                                if output.get(field):
                                    call[field] = output[field]
                            break
            elif kind in {"response.failed", "response.incomplete", "response.cancelled", "response.canceled"}:
                return False, kind, usage, call
    except (TypeError, UnicodeDecodeError, ValueError, json.JSONDecodeError):
        return False, "invalid_sse", usage, call
    if terminal != "completed":
        return False, "missing_response_completed", usage, call
    if not call["id"] and not call["call_id"]:
        return False, "missing_tool_call_id", usage, call
    if not call["name"] or call["name"] != TOOL_NAME:
        return False, "unexpected_tool_call", usage, call
    try:
        if json.loads(call["arguments"] or "{}") != TOOL_ARGUMENTS:
            return False, "tool_arguments_mismatch", usage, call
    except (TypeError, ValueError, json.JSONDecodeError):
        return False, "invalid_tool_arguments", usage, call
    return True, terminal, usage, call


def usage_rows(key_id: int, model: str, since: datetime) -> list[dict]:
    sql = """
SELECT json_build_object(
 'id',id,'group_id',group_id,'requested_model',COALESCE(requested_model,''),
 'model',model,'upstream_model',COALESCE(upstream_model,''),
 'upstream_response_model',COALESCE(upstream_response_model,''),
 'rate_multiplier',rate_multiplier,'total_cost',total_cost,'actual_cost',actual_cost,
 'input_tokens',input_tokens,'output_tokens',output_tokens,
 'cache_creation_tokens',cache_creation_tokens,'cache_read_tokens',cache_read_tokens,'image_count',image_count)
FROM usage_logs
WHERE api_key_id=:api_key_id AND created_at >= :'since'::timestamptz
  AND (model=:'model' OR requested_model=:'model' OR upstream_model=:'model')
ORDER BY id DESC LIMIT 5
"""
    out = psql(["-v", f"api_key_id={key_id}", "-v", f"since={since.isoformat()}",
                "-v", f"model={model}"], sql)
    rows = []
    for line in out.splitlines():
        if line.strip():
            try:
                rows.append(json.loads(line))
            except json.JSONDecodeError:
                fail("usage log query returned invalid safe projection")
    return rows


def wait_usage(key_id: int, model: str, since: datetime, seconds: float) -> list[dict]:
    deadline = time.monotonic() + seconds
    while True:
        rows = usage_rows(key_id, model, since)
        if rows or time.monotonic() >= deadline:
            return rows
        time.sleep(0.5)


def request_payload(model: str, protocol: str) -> dict | None:
    if protocol == "messages":
        return {"model": model, "max_tokens": 64, "stream": True,
                "messages": [{"role": "user", "content": "Reply OK"}]}
    if protocol == "responses":
        return {"model": model, "max_output_tokens": 128, "stream": True,
                "store": False, "instructions": "Reply briefly.",
                "input": [{"role": "user", "content": [{"type": "input_text", "text": "Reply OK"}]}]}
    if protocol == "images":
        return {"model": model, "prompt": "a simple blue circle on a white background",
                "n": 1, "size": "1024x1024"}
    return None


def run_tool_roundtrip(base: str, key: str, model: str, protocol: str, timeout: float) -> tuple[str, dict, dict]:
    prompt = 'Call echo_probe exactly once with the JSON arguments {"status":"ok"}.'
    args_json = tool_arguments()
    if protocol == "messages":
        original = {"role": "user", "content": prompt}
        first = {"model": model, "max_tokens": 128, "stream": True, "messages": [original],
                 "tools": [echo_tool(protocol)], "tool_choice": {"type": "tool", "name": TOOL_NAME}}
        parser_fn = parse_messages_tool_call
    elif protocol == "responses":
        original = {"role": "user", "content": [{"type": "input_text", "text": prompt}]}
        first = {"model": model, "max_output_tokens": 128, "stream": True, "store": False,
                 "instructions": "Use the declared echo_probe tool once.", "input": [original],
                 "tools": [echo_tool(protocol)], "tool_choice": {"type": "function", "name": TOOL_NAME}}
        parser_fn = parse_responses_tool_call
    else:
        fail(f"--tool-roundtrip only supports messages/responses (protocol={protocol})")
    code, body = http_call(base, "/v1/" + protocol, key, first, timeout, "text/event-stream")
    parsed = parser_fn(body) if code == 200 else (False, "http_error", {}, {})
    ok, terminal, usage, call = parsed
    if not ok:
        fail(f"model={model} protocol={protocol} tool_call HTTP={code if code is not None else 'connection_error'} terminal={terminal}")
    call_id = call.get("id") or call.get("call_id")
    if protocol == "messages":
        second = {"model": model, "max_tokens": 128, "stream": True,
                  "messages": [original, {"role": "assistant", "content": [{"type": "tool_use", "id": call_id,
                  "name": call["name"], "input": TOOL_ARGUMENTS}]}, {"role": "user", "content": [{
                  "type": "tool_result", "tool_use_id": call_id, "content": args_json}]}],
                  "tools": [echo_tool(protocol)]}
        second_parser = parse_messages
    else:
        function_call = {"type": "function_call", "call_id": call.get("call_id") or call_id,
                         "name": call["name"], "arguments": args_json}
        if call.get("id"):
            function_call["id"] = call["id"]
        second = {"model": model, "max_output_tokens": 128, "stream": True, "store": False,
                  "instructions": "Reply briefly after the tool result.", "input": [original, function_call,
                  {"type": "function_call_output", "call_id": call.get("call_id") or call_id,
                   "output": args_json}], "tools": [echo_tool(protocol)], "tool_choice": "none"}
        second_parser = parse_responses
    code, body = http_call(base, "/v1/" + protocol, key, second, timeout, "text/event-stream")
    final = second_parser(body) if code == 200 else (False, "http_error", {})
    if not final[0]:
        fail(f"model={model} protocol={protocol} tool_result HTTP={code if code is not None else 'connection_error'} terminal={final[1]}")
    return final[1], final[2], call


def image_count(body: bytes) -> int | None:
    try:
        if len(body) > MAX_BODY:
            return None
        data = (json.loads(body) or {}).get("data")
        return len(data) if isinstance(data, list) else None
    except (ValueError, TypeError, AttributeError):
        return None


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--key-name", required=True, help="专用短期限额 key 的精确名称")
    parser.add_argument("--group-id", required=True, type=int)
    parser.add_argument("--base-url", default="http://127.0.0.1:8080")
    parser.add_argument("--routes-file", default=str(DEFAULT_ROUTES))
    parser.add_argument("--model", action="append", default=[])
    parser.add_argument("--protocol", action="append", choices=["messages", "responses", "images"], default=[])
    parser.add_argument("--tool-roundtrip", action="store_true", help="执行 echo_probe 工具调用与结果续接往返")
    parser.add_argument("--timeout", type=float, default=120.0)
    parser.add_argument("--log-wait", type=float, default=10.0)
    args = parser.parse_args()
    if args.group_id <= 0 or len(args.model) != len(args.protocol):
        parser.error("--group-id 必须为正数，且每个 --model 必须对应同序 --protocol")
    if args.tool_roundtrip and any(protocol == "images" for protocol in args.protocol):
        parser.error("--tool-roundtrip 仅支持 messages/responses")
    parts = urlsplit(args.base_url)
    if parts.scheme not in {"http", "https"} or parts.hostname not in {"localhost", "127.0.0.1", "::1"}:
        parser.error("--base-url 仅允许 localhost/127.0.0.1/::1，避免凭据随重定向或代理外发")
    expected = load_routes(args.routes_file)
    key_id, key = read_key(args.key_name, args.group_id)
    print(f"key_id={key_id} group_id={args.group_id}")
    code, body = http_call(args.base_url, "/v1/models", key, None, args.timeout)
    if code != 200:
        fail(f"models HTTP {code if code is not None else 'connection_error'}")
    try:
        document = json.loads(body)
        data = document.get("data") if isinstance(document, dict) else None
        listed = {str(item["id"]) for item in data} if isinstance(data, list) else set()
    except (ValueError, TypeError, KeyError):
        fail("models returned invalid JSON shape")
    missing, extra = sorted(expected - listed), sorted(listed - expected)
    if not isinstance(data, list) or len(data) != 29 or len(listed) != 29 or missing or extra:
        fail(f"models alias mismatch count={len(listed)} missing={missing} extra={extra}")
    print("models status=ok count=29")
    paths = {"messages": "/v1/messages", "responses": "/v1/responses", "images": "/v1/images/generations"}
    for model, protocol in zip(args.model, args.protocol):
        started = datetime.now(timezone.utc)
        if args.tool_roundtrip:
            terminal, usage, call = run_tool_roundtrip(args.base_url, key, model, protocol, args.timeout)
            print(f"model={model} protocol={protocol} tool_roundtrip=ok tool_id={call.get('id') or call.get('call_id')} "
                  f"tool_name={call['name']} tool_arguments={json.dumps(TOOL_ARGUMENTS, sort_keys=True)} terminal={terminal} "
                  f"usage={json.dumps(usage, sort_keys=True)}")
            rows = wait_usage(key_id, model, started, args.log_wait)
            if not rows:
                fail(f"model={model} protocol={protocol} usage_log=missing")
            print(f"usage api_key_id={key_id} model={model} rows={json.dumps(rows, sort_keys=True, separators=(',', ':'))}")
            continue
        body_payload = request_payload(model, protocol)
        if body_payload is None:
            fail(f"unsupported protocol={protocol}")
        code, body = http_call(args.base_url, paths[protocol], key, body_payload, args.timeout,
                               "text/event-stream" if protocol != "images" else "application/json")
        if protocol == "images":
            count = image_count(body) if code == 200 else None
            if code != 200 or count != 1:
                fail(f"model={model} protocol=images HTTP={code if code is not None else 'connection_error'}")
            print(f"model={model} protocol=images http=200 data_count={count}")
        else:
            parser_fn = parse_messages if protocol == "messages" else parse_responses
            ok, terminal, usage = parser_fn(body) if code == 200 else (False, "http_error", {})
            if not ok:
                fail(f"model={model} protocol={protocol} HTTP={code if code is not None else 'connection_error'} terminal={terminal}")
            print(f"model={model} protocol={protocol} http=200 terminal={terminal} usage={json.dumps(usage, sort_keys=True)}")
        rows = wait_usage(key_id, model, started, args.log_wait)
        if not rows:
            fail(f"model={model} protocol={protocol} usage_log=missing")
        print(f"usage api_key_id={key_id} model={model} rows={json.dumps(rows, sort_keys=True, separators=(',', ':'))}")


if __name__ == "__main__":
    main()
