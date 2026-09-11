#!/usr/bin/env python3
"""通过正式 admin API 配置已审查的复合来源组；源组只读。"""
from __future__ import annotations
import argparse, hashlib, json, subprocess, sys
from pathlib import Path
from typing import Any
from urllib.error import HTTPError, URLError
from urllib.parse import urlencode, urlsplit
from urllib.request import HTTPRedirectHandler, Request, ProxyHandler, build_opener

GROUP_NAME = "复合子代理"
ROUTE_COUNT = 29
PLATFORMS = {"anthropic", "openai", "gemini", "antigravity", "grok", "kimi", "zhipu", "deepseek", "minimax"}
ROUTE_FIELDS = ("source_group_id", "public_model", "match_type", "target_platform", "upstream_model", "endpoint", "priority", "enabled", "notes")
ENDPOINTS = {"any", "messages", "count_tokens", "responses", "chat_completions", "embeddings", "images", "gemini"}
EXPECTED_SOURCE_NAMES = {12: "gpt-pro", 19: "gpt pro 20x 号池-稳定", 21: "gpt-luna专用", 3: "Gemini", 23: "国产模型", 5: "GPT image 2 /2.5 生图"}

class ConfigError(Exception):
    pass

class NoRedirect(HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        raise URLError("redirect refused")

def read_admin_api_key() -> str:
    command = ["docker", "exec", "sub2api-postgres", "psql", "-U", "sub2api", "-d", "sub2api", "-Atc", "SELECT value FROM settings WHERE key='admin_api_key'"]
    try:
        result = subprocess.run(command, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, check=False, timeout=15)
    except (OSError, subprocess.TimeoutExpired) as exc:
        raise ConfigError("无法读取 admin_api_key") from exc
    if result.returncode != 0:
        raise ConfigError("无法读取 admin_api_key")
    try:
        values = [v.strip() for v in result.stdout.decode("utf-8").splitlines() if v.strip()]
    except UnicodeDecodeError as exc:
        raise ConfigError("admin_api_key 返回内容无效") from exc
    if len(values) != 1:
        raise ConfigError("admin_api_key 不存在或返回行数不唯一")
    return values[0]

class AdminAPI:
    def __init__(self, base_url: str, key: str, timeout: float):
        base_url = base_url.rstrip("/"); parsed = urlsplit(base_url)
        if parsed.scheme not in ("http", "https") or parsed.hostname not in {"127.0.0.1", "localhost", "::1"} or parsed.username or parsed.password:
            raise ConfigError("--base-url 仅允许 loopback 地址")
        self.base = base_url if base_url.endswith("/api/v1") else base_url + "/api/v1"
        self.key, self.timeout = key, timeout; self.opener = build_opener(NoRedirect(), ProxyHandler({}))

    def request(self, method: str, path: str, body: dict[str, Any] | None = None) -> Any:
        data = None if body is None else json.dumps(body, ensure_ascii=False).encode("utf-8")
        request = Request(self.base + (path if path.startswith("/") else "/" + path), data=data, method=method, headers={"Accept": "application/json", "Content-Type": "application/json", "X-API-Key": self.key})
        try:
            with self.opener.open(request, timeout=self.timeout) as response: raw = response.read()
        except HTTPError as exc:
            raise ConfigError(f"管理 API {method} {path} 返回 HTTP {exc.code}") from exc
        except URLError as exc:
            raise ConfigError(f"无法连接管理 API {method} {path}") from exc
        try:
            result = json.loads(raw.decode("utf-8")) if raw else None
        except (UnicodeDecodeError, json.JSONDecodeError) as exc:
            raise ConfigError(f"管理 API {method} {path} 返回非 JSON") from exc
        if isinstance(result, dict) and "code" in result:
            if result.get("code") != 0: raise ConfigError(f"管理 API {method} {path} 返回错误 code={result.get('code')}")
            return result.get("data")
        return result

    def groups(self) -> list[dict[str, Any]]:
        result = self.request("GET", "/admin/groups/all?" + urlencode({"include_inactive": "true"}))
        if not isinstance(result, list): raise ConfigError("/admin/groups/all 返回格式无效")
        return [v for v in result if isinstance(v, dict)]

    def routes(self, group_id: int) -> list[dict[str, Any]]:
        result = self.request("GET", f"/admin/groups/{group_id}/composite-routes")
        if not isinstance(result, list): raise ConfigError(f"分组 {group_id} 路由返回格式无效")
        return [v for v in result if isinstance(v, dict)]

    def preview(self, group_id: int, model: str) -> dict[str, Any]:
        result = self.request("POST", f"/admin/groups/{group_id}/composite-routes/preview", {"model": model, "endpoint": "any"})
        if not isinstance(result, dict): raise ConfigError(f"来源分组 {group_id} preview 返回格式无效")
        return result

def load_spec(path: Path) -> list[dict[str, Any]]:
    try: document = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, UnicodeDecodeError, json.JSONDecodeError) as exc: raise ConfigError(f"无法读取路由清单: {path}") from exc
    if not isinstance(document, dict) or document.get("group_name") != GROUP_NAME: raise ConfigError("路由清单 group_name 不匹配")
    routes = document.get("routes")
    if not isinstance(routes, list) or len(routes) != ROUTE_COUNT: raise ConfigError(f"路由清单必须精确包含 {ROUTE_COUNT} 条")
    seen: set[str] = set(); checked: list[dict[str, Any]] = []
    for index, route in enumerate(routes, 1):
        try:
            if not isinstance(route, dict) or any(field not in route for field in ROUTE_FIELDS): raise ValueError("字段不完整")
            public, upstream, source_id = str(route["public_model"]).strip(), str(route["upstream_model"]).strip(), int(route["source_group_id"])
            if not public or public in seen or not upstream or source_id <= 0 or route["match_type"] != "exact" or route["enabled"] is not True: raise ValueError("模型名、source_group_id 或 exact/enabled 无效")
            if route["target_platform"] not in PLATFORMS or route["endpoint"] not in ENDPOINTS: raise ValueError("target_platform 或 endpoint 无效")
            checked.append({"source_group_id": source_id, "public_model": public, "match_type": "exact", "target_platform": route["target_platform"], "upstream_model": upstream, "endpoint": route["endpoint"], "priority": int(route["priority"]), "enabled": True, "notes": str(route["notes"])})
            seen.add(public)
        except (TypeError, ValueError, KeyError) as exc: raise ConfigError(f"路由清单第 {index} 条无效") from exc
    # 当前验收清单只含 deepseek-v4.1-flash，防止两个已禁用旧条目回流。
    if [r["public_model"] for r in checked if r["target_platform"] == "deepseek"] != ["deepseek-v4.1-flash"]: raise ConfigError("路由清单含非验收 DeepSeek 模型")
    return checked

def group_allowlist(group: dict[str, Any]) -> tuple[bool, list[str]]:
    value = group.get("model_allowlist") or {}
    if not isinstance(value, dict): return False, []
    return bool(value.get("enabled")), [str(v).strip() for v in (value.get("models") or [])]

def is_allowed(enabled: bool, models: list[str], model: str) -> bool:
    if not enabled: return True
    model = model.lower()
    return any((entry := value.lower()) == "*" or (entry.endswith("*") and model.startswith(entry[:-1])) or entry == model for value in models)

def source_snapshot(group: dict[str, Any]) -> str:
    enabled, models = group_allowlist(group)
    # 包含倍率、逐模型/图片/视频/峰值价及时间配置；排除 account/usage/count 等消费字段。
    safe = {k: group.get(k) for k in ("id", "name", "platform", "status", "is_exclusive", "subscription_type", "rate_multiplier", "long_context_pricing_enabled", "model_pricing", "image_rate_independent", "image_rate_multiplier", "image_price_1k", "image_price_2k", "image_price_4k", "video_rate_independent", "video_rate_multiplier", "video_price_480p", "video_price_720p", "video_price_1080p", "peak_rate_enabled", "peak_start", "peak_end", "peak_rate_multiplier")}
    safe.update({"allowlist_enabled": enabled, "allowlist_models": models})
    return hashlib.sha256(json.dumps(safe, ensure_ascii=False, sort_keys=True).encode()).hexdigest()[:12]

def find_target(groups: list[dict[str, Any]]) -> dict[str, Any] | None:
    matches = [g for g in groups if str(g.get("name", "")).strip() == GROUP_NAME]
    if len(matches) > 1: raise ConfigError(f"发现多个同名分组 {GROUP_NAME}，拒绝继续")
    if matches and matches[0].get("platform") != "composite": raise ConfigError(f"同名分组 {GROUP_NAME} 不是 composite，拒绝继续")
    return matches[0] if matches else None

def validate_sources(api: AdminAPI, spec: list[dict[str, Any]], groups: list[dict[str, Any]], target_id: int | None) -> list[dict[str, Any]]:
    by_id = {int(g["id"]): g for g in groups if str(g.get("id", "")).isdigit()}; source_ids = sorted({r["source_group_id"] for r in spec})
    if target_id in source_ids: raise ConfigError("目标分组 ID 出现在 source_group_id 中，拒绝自引用")
    summaries = []
    for source_id in source_ids:
        source = by_id.get(source_id)
        if source is None: raise ConfigError(f"来源分组 {source_id} 不存在")
        expected_name = EXPECTED_SOURCE_NAMES.get(source_id)
        if expected_name is None or str(source.get("name", "")).strip() != expected_name: raise ConfigError(f"来源分组 {source_id} 名称与本轮清单不符")
        if source.get("status") != "active" or bool(source.get("is_exclusive")) or source.get("subscription_type") != "standard": raise ConfigError(f"来源分组 {source_id} 必须是 active、standard、public")
        enabled, models = group_allowlist(source)
        if enabled and not models: raise ConfigError(f"来源分组 {source_id} 白名单已启用但为空")
        if source.get("platform") == "composite":
            routes = api.routes(source_id)
            if any(r.get("source_group_id") is not None for r in routes): raise ConfigError(f"来源复合分组 {source_id} 含多层 source route")
        elif source.get("platform") not in PLATFORMS: raise ConfigError(f"来源分组 {source_id} 平台无效")
        summaries.append({"id": source_id, "name": str(source["name"]), "platform": source.get("platform"), "status": source.get("status"), "public": not bool(source.get("is_exclusive")), "allowlist": f"{len(models)}{'+' if enabled else ''}", "snapshot": source_snapshot(source)})
    for route in spec:
        source = by_id[route["source_group_id"]]; enabled, models = group_allowlist(source)
        if not is_allowed(enabled, models, route["upstream_model"]): raise ConfigError(f"来源分组 {route['source_group_id']} 白名单不允许 {route['upstream_model']}")
        if source.get("platform") != "composite":
            if source.get("platform") != route["target_platform"]: raise ConfigError(f"来源分组 {route['source_group_id']} 平台与路由不匹配")
        else:
            decision = api.preview(route["source_group_id"], route["upstream_model"])
            if not decision.get("matched") or decision.get("target_platform") != route["target_platform"] or decision.get("upstream_model") != route["upstream_model"]: raise ConfigError(f"来源复合分组 {route['source_group_id']} preview 映射不符")
    return summaries

def group_payload(spec: list[dict[str, Any]]) -> dict[str, Any]:
    return {"name": GROUP_NAME, "description": "主模型与子代理可混用多家模型；GPT -016/-023 自选线路，各模型沿用原分组价格。", "platform": "composite", "rate_multiplier": 1, "is_exclusive": False, "subscription_type": "standard", "allow_messages_dispatch": True, "allow_image_generation": True, "model_allowlist": {"enabled": True, "models": [r["public_model"] for r in spec]}}

def validate_group(group: dict[str, Any], spec: list[dict[str, Any]]) -> None:
    if group.get("platform") != "composite" or bool(group.get("is_exclusive")) or group.get("subscription_type") != "standard" or float(group.get("rate_multiplier", 0)) != 1: raise ConfigError("目标分组必须是 public composite、standard 且倍率为 1")
    if not bool(group.get("allow_messages_dispatch")) or not bool(group.get("allow_image_generation")): raise ConfigError("目标分组必须开启 messages dispatch 与 image generation")
    enabled, models = group_allowlist(group)
    if not enabled or models != [r["public_model"] for r in spec]: raise ConfigError("目标分组白名单不是精确 29 条验收模型")

def route_equal(actual: dict[str, Any], expected: dict[str, Any]) -> bool: return all(actual.get(field) == expected[field] for field in ROUTE_FIELDS)

def validate_routes(actual: list[dict[str, Any]], spec: list[dict[str, Any]], allow_missing: bool) -> list[dict[str, Any]]:
    expected = {r["public_model"]: r for r in spec}; seen: set[str] = set()
    for route in actual:
        public = str(route.get("public_model", "")).strip()
        if public in seen or public not in expected or not route_equal(route, expected[public]): raise ConfigError("目标分组存在重复、意外或冲突路由，拒绝继续")
        seen.add(public)
    missing = [r for r in spec if r["public_model"] not in seen]
    if missing and not allow_missing: raise ConfigError(f"目标分组缺少 {len(missing)} 条路由")
    return missing

def report(group: dict[str, Any] | None, routes: list[dict[str, Any]], spec: list[dict[str, Any]], sources: list[dict[str, Any]]) -> None:
    print(f"group_id={'none' if group is None else group.get('id')} route_count={len(routes)}/{len(spec)}")
    for source in sources: print("source id={id} name={name} platform={platform} status={status} public={public} allowlist={allowlist} snapshot={snapshot}".format(**source))
    for route in spec: print(f"route {route['public_model']} -> {route['target_platform']}/{route['upstream_model']} source={route['source_group_id']} pricing=source-inherited")

def configure(api: AdminAPI, spec: list[dict[str, Any]]) -> None:
    groups = api.groups(); target = find_target(groups); sources = validate_sources(api, spec, groups, int(target["id"]) if target else None)
    if target is None:
        created = api.request("POST", "/admin/groups", group_payload(spec))
        if not isinstance(created, dict) or not str(created.get("id", "")).isdigit(): raise ConfigError("创建目标分组返回内容缺少 ID")
        target_id = int(created["id"]); api.request("PUT", f"/admin/groups/{target_id}", {"status": "inactive"})
    else:
        target_id = int(target["id"]); validate_group(target, spec)
    target = find_target(api.groups())
    if target is None: raise ConfigError("创建后无法读取目标分组")
    validate_group(target, spec); missing = validate_routes(api.routes(target_id), spec, True)
    if target.get("status") == "active": api.request("PUT", f"/admin/groups/{target_id}", {"status": "inactive"})
    for route in missing: api.request("POST", f"/admin/groups/{target_id}/composite-routes", route)
    target = find_target(api.groups())
    if target is None: raise ConfigError("配置后无法读取目标分组")
    actual = api.routes(int(target["id"])); validate_group(target, spec); validate_routes(actual, spec, False); report(target, actual, spec, sources)

def check(api: AdminAPI, spec: list[dict[str, Any]]) -> None:
    groups = api.groups(); target = find_target(groups); sources = validate_sources(api, spec, groups, int(target["id"]) if target else None); actual = []
    if target is not None: validate_group(target, spec); actual = api.routes(int(target["id"])); validate_routes(actual, spec, False)
    report(target, actual, spec, sources)

def activate(api: AdminAPI, spec: list[dict[str, Any]]) -> None:
    groups = api.groups(); target = find_target(groups)
    if target is None: raise ConfigError("目标分组不存在，先执行 --configure")
    target_id = int(target["id"]); validate_group(target, spec); sources = validate_sources(api, spec, groups, target_id); actual = api.routes(target_id); validate_routes(actual, spec, False)
    if target.get("status") != "active": api.request("PUT", f"/admin/groups/{target_id}", {"status": "active"}); target = find_target(api.groups())
    report(target, actual, spec, sources)

def main() -> int:
    parser = argparse.ArgumentParser(description="配置复合来源分组（默认只连本机管理 API）"); mode = parser.add_mutually_exclusive_group(required=True)
    mode.add_argument("--check", action="store_true", help="只读核对"); mode.add_argument("--configure", action="store_true", help="创建/补齐目标组与路由"); mode.add_argument("--activate", action="store_true", help="核对通过后启用目标组")
    parser.add_argument("--base-url", default="http://127.0.0.1:8081"); parser.add_argument("--routes", type=Path, default=Path(__file__).resolve().parents[1] / "docs" / "composite-source-routes.example.json"); parser.add_argument("--timeout", type=float, default=15.0)
    args = parser.parse_args()
    try:
        spec = load_spec(args.routes); api = AdminAPI(args.base_url, read_admin_api_key(), args.timeout)
        if args.check: check(api, spec)
        elif args.configure: configure(api, spec)
        else: activate(api, spec)
        return 0
    except ConfigError as exc: print(f"错误: {exc}", file=sys.stderr); return 2

if __name__ == "__main__": raise SystemExit(main())
