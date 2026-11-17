import argparse
import contextlib
import json
import os
import re
import secrets
import sys
import time
import unicodedata
import urllib.request
from pathlib import Path
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import parse_qs, urlsplit

import sms_receiver


DEFAULT_HOST = "127.0.0.1"
DEFAULT_PORT = 9010
COUNTRY_KEY_ALIASES = {
    "america": "usa",
    "england": "england",
    "greatbritain": "england",
    "russia": "russianfederation",
    "southkorea": "korea",
    "uk": "england",
    "unitedkingdom": "england",
    "unitedstates": "usa",
    "unitedstatesofamerica": "usa",
    "us": "usa",
    "usa": "usa",
}


def json_response(ok, data=None, error=None):
    payload = {"ok": bool(ok)}
    if data:
        payload.update(data)
    if error:
        payload["error"] = str(error)
    return payload


def configured_api_token(config=None):
    config = config or sms_receiver.load_config()
    gateway = config.get("gateway") if isinstance(config.get("gateway"), dict) else {}
    return (
        os.environ.get("SMS_GATEWAY_TOKEN")
        or gateway.get("apiToken")
        or config.get("apiToken")
        or config.get("gatewayApiToken")
        or ""
    )


def request_api_token(handler):
    auth = handler.headers.get("Authorization", "").strip()
    if auth.lower().startswith("bearer "):
        return auth.split(None, 1)[1].strip()
    if auth.lower().startswith("token "):
        return auth.split(None, 1)[1].strip()
    header_token = handler.headers.get("X-Api-Key") or handler.headers.get("X-SMS-Gateway-Token")
    if header_token:
        return header_token.strip()
    query = parse_qs(urlsplit(handler.path).query)
    values = query.get("token") or []
    return values[0].strip() if values else ""


def is_authorized(handler):
    expected = configured_api_token()
    if not expected:
        return True
    supplied = request_api_token(handler)
    return bool(supplied) and secrets.compare_digest(str(supplied), str(expected))


def parse_json_payload(raw):
    if not raw:
        return {}
    if isinstance(raw, dict):
        return dict(raw)
    try:
        data = json.loads(raw)
    except json.JSONDecodeError as exc:
        raise ValueError(f"invalid JSON payload: {exc}") from exc
    if not isinstance(data, dict):
        raise ValueError("JSON payload must be an object")
    return data


def split_items(value):
    if value is None or value == "":
        return []
    if isinstance(value, list):
        return [str(item).strip() for item in value if str(item).strip()]
    return [item.strip() for item in re.split(r"[,，、\s]+", str(value)) if item.strip()]


def parse_countries(value):
    countries = []
    for item in split_items(value):
        try:
            countries.append(int(item))
        except ValueError:
            pass
    return countries


def parse_country_values(value):
    return split_items(value)


def normalize_country_key(value):
    text = unicodedata.normalize("NFKD", str(value or ""))
    text = text.encode("ascii", "ignore").decode("ascii").lower()
    text = text.replace("&", "and")
    text = re.sub(r"[^a-z0-9]+", "", text)
    return COUNTRY_KEY_ALIASES.get(text, text)


def readable_country_name(value):
    text = str(value or "").strip()
    if not text:
        return ""
    text = text.replace("_", " ").replace("-", " ")
    text = re.sub(r"(?<=[a-z])(?=and[a-z])", " ", text, flags=re.I)
    return " ".join(part.capitalize() for part in text.split())


def as_float(value, default=0):
    if value is None or value == "":
        return default
    try:
        return float(value)
    except (TypeError, ValueError):
        return default


def as_int(value, default=0):
    if value is None or value == "":
        return default
    try:
        return int(value)
    except (TypeError, ValueError):
        return default


def as_bool(value, default=False):
    if value is None or value == "":
        return default
    if isinstance(value, bool):
        return value
    if isinstance(value, (int, float)):
        return value != 0
    text = str(value).strip().lower()
    if text in ("1", "true", "yes", "y", "on"):
        return True
    if text in ("0", "false", "no", "n", "off"):
        return False
    return default


def masked_secret(value):
    text = str(value or "").strip()
    if not text:
        return ""
    tail = text[-4:] if len(text) > 4 else text
    return f"******{tail}"


def atomic_write_json(path, data):
    target = Path(path)
    target.parent.mkdir(parents=True, exist_ok=True)
    tmp = target.with_suffix(target.suffix + ".tmp")
    tmp.write_text(json.dumps(data, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    tmp.replace(target)


def first_value(source, names, default=None):
    for name in names:
        if name in source and source[name] not in (None, ""):
            return source[name]
    return default


def default_countries(config):
    return config.get("countries") or config.get("smsBowerCountries") or config.get("heroSmsCountries") or []


def five_sim_default_countries(config):
    five = config.get("fiveSim") if isinstance(config.get("fiveSim"), dict) else {}
    value = five.get("countries") or five.get("country") or config.get("fiveSimCountries") or config.get("fiveSimCountry") or "england"
    return split_items(value)


def provider_order(config, provider_name="", provider_names=None):
    if provider_name:
        return [str(provider_name).lower()]
    names = split_items(provider_names) if provider_names else []
    if not names:
        names = split_items(config.get("providerOrder") or ["smsbower", "herosms", "5sim"])
    return [str(name).lower() for name in names]


def make_provider_args(config, payload):
    five = payload.get("fiveSim") if isinstance(payload.get("fiveSim"), dict) else {}
    config_five = config.get("fiveSim") if isinstance(config.get("fiveSim"), dict) else {}
    return argparse.Namespace(
        proxy=payload["proxy"] if "proxy" in payload else None,
        five_country=first_value(payload, ["fiveCountry", "five_country"], five.get("country") or ""),
        five_operator=first_value(payload, ["fiveOperator", "five_operator"], five.get("operator") or ""),
        five_product=first_value(payload, ["fiveProduct", "five_product"], five.get("product") or ""),
    )


def runtime(config, payload):
    args = make_provider_args(config, payload)
    providers = sms_receiver.make_providers(config, args)
    return args, providers


def allocate(payload):
    config = sms_receiver.load_config()
    _, providers = runtime(config, payload)
    service = first_value(payload, ["service"], config.get("service") or config.get("heroSmsService") or "dr")
    raw_countries = first_value(payload, ["countries"], default_countries(config))
    countries = parse_country_values(raw_countries)
    min_price = as_float(first_value(payload, ["minPrice", "min_price"], config.get("minPrice")), 0)
    max_price = as_float(first_value(payload, ["maxPrice", "max_price"], config.get("maxPrice")), 0)
    requested_provider = first_value(payload, ["provider"], "")
    requested_providers = first_value(payload, ["providers", "providerOrder", "provider_order"], [])

    errors = {}
    for name in provider_order(config, requested_provider, requested_providers):
        if name not in providers:
            continue
        provider = providers[name]
        try:
            with contextlib.redirect_stdout(sys.stderr):
                provider_service = service_for_provider(config, name, service)
                provider_countries = countries_for_provider(config, name, countries, service, providers)
                if not provider_countries:
                    errors[name] = "no mapped countries for provider"
                    continue
                order = provider.buy_number(provider_service, provider_countries, min_price=min_price, max_price=max_price)
            order["service"] = provider_service
            sms_receiver.append_order({"event": "allocated", **order})
            return {"order": order}
        except Exception as exc:
            errors[name] = str(exc)

    raise RuntimeError(f"failed to allocate number: {errors}")


def service_for_provider(config, provider_name, service):
    if provider_name == "5sim":
        five = config.get("fiveSim") if isinstance(config.get("fiveSim"), dict) else {}
        return five.get("product") or config.get("fiveSimProduct") or "whatsapp"
    return service


def countries_for_provider(config, provider_name, countries, service=None, providers=None):
    mapped = mapped_country_values(provider_name, countries, service, providers)
    if mapped:
        return mapped
    if provider_name == "5sim":
        slugs = [str(item).strip().lower() for item in countries if str(item).strip() and not str(item).strip().isdigit()]
        return slugs or five_sim_default_countries(config)
    ids = []
    for item in countries:
        try:
            ids.append(int(str(item).strip()))
        except (TypeError, ValueError):
            continue
    return ids or parse_countries(default_countries(config))


def mapped_country_values(provider_name, countries, service=None, providers=None):
    refs = unified_country_refs(countries)
    if not refs:
        return []
    country_map = unified_country_map(providers or {}, service or "wa")
    mapped = []
    seen = set()
    for ref in refs:
        country = country_map.get(ref)
        if not country:
            continue
        provider_values = country.get("providers") or {}
        value = provider_values.get(provider_name)
        if value in (None, ""):
            continue
        key = str(value)
        if key in seen:
            continue
        seen.add(key)
        mapped.append(value)
    return mapped


def unified_country_refs(countries):
    refs = []
    seen = set()
    for item in countries:
        value = str(item).strip()
        if not value:
            continue
        if value.lower().startswith("country:"):
            ref = value.split(":", 1)[1].strip()
        else:
            normalized = normalize_country_key(value)
            ref = normalized if normalized else value
        if not ref or ref in seen:
            continue
        seen.add(ref)
        refs.append(ref)
    return refs


def country_rows(provider_name, country_data, price_data, service):
    rows_by_id = {}
    country_data = parse_provider_json(country_data)
    price_data = parse_provider_json(price_data)
    if isinstance(country_data, dict):
        iterable = country_data.items()
    elif isinstance(country_data, list):
        iterable = enumerate(country_data)
    else:
        iterable = []
    for key, value in iterable:
        row = value if isinstance(value, dict) else {}
        country_id = as_int(row.get("id") if row else key, 0)
        if country_id <= 0:
            country_id = as_int(key, 0)
        if country_id <= 0:
            continue
        names = [
            row.get("eng"),
            row.get("name"),
            row.get("country"),
            row.get("chn"),
            row.get("rus"),
            str(value) if not isinstance(value, dict) else "",
        ]
        name = next((str(item).strip() for item in names if str(item or "").strip()), "")
        rows_by_id[country_id] = {"provider": provider_name, "id": country_id, "name": name}

    prices = price_data if isinstance(price_data, dict) else {}
    for raw_country_id, service_prices in prices.items():
        country_id = as_int(raw_country_id, 0)
        if country_id <= 0 or not isinstance(service_prices, dict):
            continue
        service_data = service_prices.get(service)
        if not isinstance(service_data, dict):
            continue
        row = rows_by_id.setdefault(country_id, {"provider": provider_name, "id": country_id, "name": ""})
        row["count"] = as_int(service_data.get("count"), 0)
        cost = as_float(service_data.get("cost"), 0)
        if cost > 0:
            row["cost"] = cost

    return sorted(rows_by_id.values(), key=lambda item: (str(item.get("name") or "").lower(), int(item.get("id") or 0)))


def five_sim_country_rows(provider):
    data = provider.public_request("/v1/guest/prices?product=whatsapp")
    data = parse_provider_json(data)
    product = data.get("whatsapp") if isinstance(data, dict) else {}
    if not isinstance(product, dict):
        return []
    rows = []
    for slug, operators in product.items():
        slug = str(slug).strip()
        if not slug:
            continue
        count = 0
        cost = 0
        if isinstance(operators, dict):
            for operator_data in operators.values():
                if not isinstance(operator_data, dict):
                    continue
                count += as_int(operator_data.get("count"), 0)
                operator_cost = as_float(operator_data.get("cost"), 0)
                if operator_cost > 0 and (cost <= 0 or operator_cost < cost):
                    cost = operator_cost
        row = {"provider": "5sim", "value": slug, "name": country_slug_label(slug)}
        if count > 0:
            row["count"] = count
        if cost > 0:
            row["cost"] = cost
        rows.append(row)
    return sorted(rows, key=lambda item: str(item.get("name") or "").lower())


def provider_country_rows(provider_name, provider, service):
    if provider_name == "5sim":
        return five_sim_country_rows(provider)
    country_data = provider.request("getCountries")
    try:
        price_data = provider.prices(service)
    except Exception:
        price_data = {}
    return country_rows(provider_name, country_data, price_data, service)


def unified_country_rows(rows_by_provider):
    rows_by_key = {}
    for provider_name, rows in rows_by_provider.items():
        for row in rows:
            name = str(row.get("name") or "").strip()
            value = row.get("value")
            key = normalize_country_key(name or value)
            if not key:
                continue
            item = rows_by_key.setdefault(
                key,
                {
                    "provider": "country",
                    "value": f"country:{key}",
                    "name": readable_country_name(name or value or key),
                    "providers": {},
                    "count": 0,
                },
            )
            if name and (not item.get("name") or normalize_country_key(item.get("name")) == key):
                item["name"] = readable_country_name(name)
            provider_values = item.setdefault("providers", {})
            provider_value = value if provider_name == "5sim" else row.get("id")
            if provider_value not in (None, ""):
                provider_values[provider_name] = provider_value
            count = as_int(row.get("count"), 0)
            if count > 0:
                item["count"] = as_int(item.get("count"), 0) + count
            cost = as_float(row.get("cost"), 0)
            if cost > 0 and (as_float(item.get("cost"), 0) <= 0 or cost < as_float(item.get("cost"), 0)):
                item["cost"] = cost

    return sorted(rows_by_key.values(), key=lambda item: str(item.get("name") or "").lower())


def unified_country_map(providers, service):
    rows_by_provider = {}
    for provider_name, provider in providers.items():
        try:
            with contextlib.redirect_stdout(sys.stderr):
                rows_by_provider[provider_name] = provider_country_rows(provider_name, provider, service)
        except Exception:
            rows_by_provider[provider_name] = []
    country_map = {}
    for row in unified_country_rows(rows_by_provider):
        value = str(row.get("value") or "")
        key = value.split(":", 1)[1] if value.startswith("country:") else normalize_country_key(value)
        aliases = {key, normalize_country_key(row.get("name"))}
        providers_map = row.get("providers") if isinstance(row.get("providers"), dict) else {}
        for provider_name, provider_value in providers_map.items():
            provider_key = f"{provider_name}:{provider_value}"
            aliases.add(provider_key)
            if provider_name == "smsbower":
                aliases.add(str(provider_value))
        for alias in aliases:
            if alias:
                country_map[alias] = row
    return country_map


def country_slug_label(value):
    return str(value).replace("_", " ").replace("-", " ").title()


def parse_provider_json(value):
    if isinstance(value, str) and value.strip().startswith(("{", "[")):
        try:
            return json.loads(value)
        except json.JSONDecodeError:
            return value
    return value


def countries(payload):
    config = sms_receiver.load_config()
    _, providers = runtime(config, payload)
    service = first_value(payload, ["service"], config.get("service") or config.get("heroSmsService") or "dr")
    requested_provider = first_value(payload, ["provider"], "")
    requested_providers = first_value(payload, ["providers", "providerOrder", "provider_order"], [])
    rows_by_provider = {}
    errors = {}
    for name in provider_order(config, requested_provider, requested_providers):
        if name not in providers:
            continue
        provider = providers[name]
        try:
            with contextlib.redirect_stdout(sys.stderr):
                rows_by_provider[name] = provider_country_rows(name, provider, service)
        except Exception as exc:
            errors[name] = str(exc)
    rows = unified_country_rows(rows_by_provider)
    return {"countries": rows, "service": service, "errors": errors}


def provider_for_order(config, payload):
    _, providers = runtime(config, payload)
    nested_order = payload.get("order") if isinstance(payload.get("order"), dict) else {}
    provider_name = first_value(payload, ["provider"], nested_order.get("provider") or "")
    order_id = first_value(payload, ["id", "orderId", "activationId"], nested_order.get("id") or "")
    if not provider_name:
        raise ValueError("provider is required")
    if not order_id:
        raise ValueError("id/orderId is required")
    provider_name = str(provider_name).lower()
    if provider_name not in providers:
        raise ValueError(f"unsupported provider: {provider_name}")
    return provider_name, providers[provider_name], str(order_id)


def mark_ready(payload):
    config = sms_receiver.load_config()
    provider_name, provider, order_id = provider_for_order(config, payload)
    with contextlib.redirect_stdout(sys.stderr):
        provider.ready(order_id)
    sms_receiver.append_order({"event": "ready", "provider": provider_name, "id": order_id})
    return {"provider": provider_name, "id": order_id}


def get_code(payload):
    config = sms_receiver.load_config()
    provider_name, provider, order_id = provider_for_order(config, payload)
    with contextlib.redirect_stdout(sys.stderr):
        code = provider.get_code(order_id)
    return {"provider": provider_name, "id": order_id, "code": code, "received": bool(code)}


def wait_code(payload):
    config = sms_receiver.load_config()
    provider_name, provider, order_id = provider_for_order(config, payload)
    timeout = as_int(first_value(payload, ["timeout", "timeoutSeconds"], config.get("timeoutSeconds")), 180)
    interval = as_int(first_value(payload, ["interval", "pollIntervalSeconds"], config.get("pollIntervalSeconds")), 5)
    should_ready = as_bool(first_value(payload, ["ready", "markReady"], False), False)
    cancel_on_failure = as_bool(first_value(payload, ["cancelOnFailure", "cancel_on_failure"], False), False)

    with contextlib.redirect_stdout(sys.stderr):
        try:
            if should_ready:
                provider.ready(order_id)
            code = sms_receiver.wait_for_code(provider, order_id, interval, timeout)
        except Exception:
            if cancel_on_failure:
                provider.cancel(order_id)
                sms_receiver.append_order({"event": "canceled", "provider": provider_name, "id": order_id})
            raise
    sms_receiver.append_order({"event": "code", "provider": provider_name, "id": order_id, "code": "present"})
    return {"provider": provider_name, "id": order_id, "code": code, "received": True}


def finish(payload):
    config = sms_receiver.load_config()
    provider_name, provider, order_id = provider_for_order(config, payload)
    with contextlib.redirect_stdout(sys.stderr):
        provider.finish(order_id)
    sms_receiver.append_order({"event": "finished", "provider": provider_name, "id": order_id})
    return {"provider": provider_name, "id": order_id}


def cancel(payload):
    config = sms_receiver.load_config()
    provider_name, provider, order_id = provider_for_order(config, payload)
    with contextlib.redirect_stdout(sys.stderr):
        provider.cancel(order_id)
    sms_receiver.append_order({"event": "canceled", "provider": provider_name, "id": order_id})
    return {"provider": provider_name, "id": order_id}


def health(_payload=None):
    config = sms_receiver.load_config()
    keys = {
        "smsbower": bool(config.get("smsBowerApiKey") or config.get("smsbowerApiKey")),
        "herosms": bool(config.get("heroSmsApiKey")),
        "5sim": bool(config.get("fiveSimApiKey")),
    }
    return {
        "service": config.get("service") or config.get("heroSmsService") or "dr",
        "providerOrder": config.get("providerOrder") or ["smsbower", "herosms", "5sim"],
        "countries": default_countries(config),
        "keysConfigured": keys,
        "authEnabled": bool(configured_api_token(config)),
    }


def settings(_payload=None):
    config = sms_receiver.load_config()
    provider_order_value = provider_order(config)
    providers = {
        "smsbower": provider_settings(config.get("smsBowerApiKey") or config.get("smsbowerApiKey"), "smsbower", provider_order_value),
        "herosms": provider_settings(config.get("heroSmsApiKey"), "herosms", provider_order_value),
        "5sim": provider_settings(config.get("fiveSimApiKey"), "5sim", provider_order_value),
    }
    return {
        "settings": {
            "proxy": str(config.get("proxy") or ""),
            "providerOrder": provider_order_value,
            "service": config.get("service") or config.get("heroSmsService") or "wa",
            "countries": default_countries(config),
            "maxPrice": as_float(config.get("maxPrice"), 0.35),
            "timeoutSeconds": as_int(config.get("timeoutSeconds"), 180),
            "providers": providers,
            "fiveSim": config.get("fiveSim") if isinstance(config.get("fiveSim"), dict) else {},
        }
    }


def provider_settings(api_key, name, provider_order_value):
    key = str(api_key or "").strip()
    return {
        "enabled": name in provider_order_value,
        "apiKeyConfigured": bool(key),
        "apiKeyMasked": masked_secret(key),
    }


def save_settings(payload):
    requested = payload.get("settings") if isinstance(payload.get("settings"), dict) else payload
    local = sms_receiver.load_json(sms_receiver.CONFIG_PATH)
    if not isinstance(local, dict):
        local = {}

    if "proxy" in requested:
        local["proxy"] = str(requested.get("proxy") or "").strip()
    if "service" in requested:
        service = str(requested.get("service") or "").strip()
        if service:
            local["service"] = service
    if "providerOrder" in requested or "provider_order" in requested:
        order = split_items(requested.get("providerOrder") or requested.get("provider_order"))
        allowed = {"smsbower", "herosms", "5sim"}
        normalized = []
        for name in order:
            provider_name = str(name).strip().lower()
            if provider_name in allowed and provider_name not in normalized:
                normalized.append(provider_name)
        if normalized:
            local["providerOrder"] = normalized
    if "maxPrice" in requested or "max_price" in requested:
        local["maxPrice"] = as_float(first_value(requested, ["maxPrice", "max_price"], 0), 0)
    if "timeoutSeconds" in requested or "timeout_seconds" in requested:
        timeout = as_int(first_value(requested, ["timeoutSeconds", "timeout_seconds"], 180), 180)
        if timeout > 0:
            local["timeoutSeconds"] = timeout

    providers_payload = requested.get("providers") if isinstance(requested.get("providers"), dict) else {}
    apply_provider_key(local, providers_payload.get("smsbower"), "smsBowerApiKey")
    apply_provider_key(local, providers_payload.get("herosms"), "heroSmsApiKey")
    apply_provider_key(local, providers_payload.get("5sim"), "fiveSimApiKey")

    atomic_write_json(sms_receiver.CONFIG_PATH, local)
    return settings()


def test_proxy(payload):
    requested = payload.get("sms_receiver") if isinstance(payload.get("sms_receiver"), dict) else payload
    config = sms_receiver.load_config()
    mode = str(requested.get("mode") or requested.get("proxyMode") or requested.get("proxy_mode") or "proxy").strip().lower()
    proxy = str(requested.get("proxy") if "proxy" in requested else config.get("proxy") or "").strip()
    target = str(requested.get("target") or "https://api.ipify.org?format=json").strip()
    if mode not in ("direct", "proxy"):
        raise ValueError("mode must be direct or proxy")
    if mode == "proxy" and not proxy:
        return {"result": {"ok": False, "mode": mode, "proxyConfigured": False, "target": target, "error": "proxy is empty"}}
    started = time.monotonic()
    opener = urllib.request.build_opener()
    if mode == "proxy":
        opener = urllib.request.build_opener(urllib.request.ProxyHandler({"http": proxy, "https": proxy}))
    try:
        req = urllib.request.Request(target, headers={"User-Agent": "SmsGateway/1.0", "Accept": "application/json,text/plain,*/*"})
        with opener.open(req, timeout=as_int(requested.get("timeout"), 12)) as resp:
            raw = resp.read(4096).decode("utf-8", errors="replace").strip()
        result = {
            "ok": True,
            "mode": mode,
            "proxyConfigured": bool(proxy),
            "target": target,
            "elapsedMs": int((time.monotonic() - started) * 1000),
        }
        ip = parse_test_ip(raw)
        if ip:
            result["ip"] = ip
        return {"result": result}
    except Exception as exc:
        return {
            "result": {
                "ok": False,
                "mode": mode,
                "proxyConfigured": bool(proxy),
                "target": target,
                "elapsedMs": int((time.monotonic() - started) * 1000),
                "error": str(exc),
            }
        }


def balance(payload):
    config = sms_receiver.load_config()
    _, providers = runtime(config, payload)
    requested_provider = first_value(payload, ["provider"], "")
    requested_providers = first_value(payload, ["providers", "providerOrder", "provider_order"], [])
    results = {}
    for name in provider_order(config, requested_provider, requested_providers):
        if name not in providers:
            continue
        try:
            data = providers[name].balance()
            results[name] = normalize_balance_result(name, data)
        except Exception as exc:
            results[name] = {"ok": False, "provider": name, "error": str(exc)}
    return {"balances": results}


def normalize_balance_result(provider_name, data):
    if isinstance(data, dict):
        balance_value = first_value(data, ["balance", "money", "funds", "amount"], "")
        currency = first_value(data, ["currency", "currencyCode", "currency_code"], "")
        return {
            "ok": True,
            "provider": provider_name,
            "balance": format_balance_value(balance_value),
            "currency": str(currency or "").strip(),
        }
    text = str(data or "").strip()
    if text.startswith("ACCESS_BALANCE:"):
        text = text.split(":", 1)[1].strip()
    return {"ok": True, "provider": provider_name, "balance": format_balance_value(text)}


def format_balance_value(value):
    text = str(value or "").strip()
    if not text:
        return ""
    try:
        return f"{float(text):.4f}".rstrip("0").rstrip(".")
    except ValueError:
        return text


def parse_test_ip(raw):
    if not raw:
        return ""
    try:
        data = json.loads(raw)
        if isinstance(data, dict):
            return str(data.get("ip") or data.get("origin") or "").strip()
    except json.JSONDecodeError:
        pass
    match = re.search(r"\b(?:\d{1,3}\.){3}\d{1,3}\b", raw)
    return match.group(0) if match else raw[:80]


def apply_provider_key(local, patch, config_key):
    if not isinstance(patch, dict):
        return
    if as_bool(patch.get("clearApiKey") or patch.get("clear_api_key"), False):
        local[config_key] = ""
        return
    if "apiKey" in patch or "api_key" in patch:
        api_key = str(first_value(patch, ["apiKey", "api_key"], "") or "").strip()
        if api_key:
            local[config_key] = api_key


def docs(_payload=None):
    return {
        "endpoints": {
            "GET /health": "Check local gateway config.",
            "POST /allocate": "Buy a number. Body: provider/service/countries/minPrice/maxPrice.",
            "POST /countries": "List numeric country IDs for SMSBower/HeroSMS with country names.",
            "POST /test-proxy": "Test direct or configured proxy connectivity from this gateway.",
            "POST /balance": "Check configured provider balances.",
            "POST /ready": "Mark activation ready after target site sends SMS.",
            "POST /code": "Check code once. Body: provider/id.",
            "POST /wait-code": "Poll until code. Body: provider/id/timeout/interval/markReady.",
            "POST /finish": "Finish successful order. Body: provider/id.",
            "POST /cancel": "Cancel/refund order. Body: provider/id.",
        },
        "auth": "When apiToken or SMS_GATEWAY_TOKEN is configured, send Authorization: Bearer <token> or X-Api-Key: <token>.",
        "exampleAllocate": {
            "provider": "smsbower",
            "service": "wa",
            "countries": [33, 151, 16],
            "maxPrice": 0.35,
        },
        "exampleWaitCode": {
            "provider": "smsbower",
            "id": "123456789",
            "markReady": True,
            "timeout": 180,
            "interval": 5,
        },
    }


ACTIONS = {
    "allocate": allocate,
    "countries": countries,
    "settings": settings,
    "save-settings": save_settings,
    "test-proxy": test_proxy,
    "balance": balance,
    "ready": mark_ready,
    "code": get_code,
    "wait-code": wait_code,
    "finish": finish,
    "cancel": cancel,
    "health": health,
    "docs": docs,
}


def run_action(action, payload=None):
    if action not in ACTIONS:
        return json_response(False, error=f"unknown action: {action}")
    try:
        return json_response(True, ACTIONS[action](payload or {}))
    except Exception as exc:
        return json_response(False, error=exc)


class GatewayHandler(BaseHTTPRequestHandler):
    server_version = "SmsGateway/1.0"

    def log_message(self, fmt, *args):
        sys.stderr.write("[http] " + (fmt % args) + "\n")

    def send_json(self, status, payload):
        raw = json.dumps(payload, ensure_ascii=False).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json; charset=utf-8")
        self.send_header("Content-Length", str(len(raw)))
        self.send_header("Access-Control-Allow-Origin", "*")
        self.send_header("Access-Control-Allow-Headers", "Content-Type")
        self.send_header("Access-Control-Allow-Methods", "GET,POST,OPTIONS")
        self.end_headers()
        self.wfile.write(raw)

    def read_payload(self):
        length = int(self.headers.get("Content-Length") or 0)
        if length <= 0:
            return {}
        raw = self.rfile.read(length).decode("utf-8", errors="replace")
        return parse_json_payload(raw)

    def authorize(self):
        if is_authorized(self):
            return True
        self.send_json(401, json_response(False, error="unauthorized: missing or invalid API token"))
        return False

    def do_OPTIONS(self):
        self.send_json(200, {"ok": True})

    def do_GET(self):
        if not self.authorize():
            return
        path = self.path.split("?", 1)[0].strip("/")
        action = path or "health"
        if action not in ("health", "docs"):
            self.send_json(404, json_response(False, error=f"unknown GET endpoint: /{path}"))
            return
        payload = run_action(action, {})
        self.send_json(200 if payload.get("ok") else 500, payload)

    def do_POST(self):
        if not self.authorize():
            return
        path = self.path.split("?", 1)[0].strip("/")
        action = path.replace("_", "-")
        if action not in ACTIONS:
            self.send_json(404, json_response(False, error=f"unknown POST endpoint: /{path}"))
            return
        try:
            request_payload = self.read_payload()
        except Exception as exc:
            self.send_json(400, json_response(False, error=exc))
            return
        payload = run_action(action, request_payload)
        self.send_json(200 if payload.get("ok") else 500, payload)


def serve(host, port):
    server = ThreadingHTTPServer((host, port), GatewayHandler)
    print(f"[sms-gateway] listening on http://{host}:{port}")
    print("[sms-gateway] docs: GET /docs")
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        print("\n[sms-gateway] stopping")
    finally:
        server.server_close()


def build_payload(args):
    payload = parse_json_payload(args.json_payload)
    for src_name, dst_name in [
        ("provider", "provider"),
        ("service", "service"),
        ("countries", "countries"),
        ("min_price", "minPrice"),
        ("max_price", "maxPrice"),
        ("id", "id"),
        ("timeout", "timeout"),
        ("interval", "interval"),
        ("proxy", "proxy"),
        ("five_country", "fiveCountry"),
        ("five_operator", "fiveOperator"),
        ("five_product", "fiveProduct"),
    ]:
        value = getattr(args, src_name, None)
        if value not in (None, ""):
            payload[dst_name] = value
    if getattr(args, "mark_ready", False):
        payload["markReady"] = True
    return payload


def default_gateway_host():
    config = sms_receiver.load_config()
    gateway = config.get("gateway") if isinstance(config.get("gateway"), dict) else {}
    return gateway.get("host") or DEFAULT_HOST


def default_gateway_port():
    config = sms_receiver.load_config()
    gateway = config.get("gateway") if isinstance(config.get("gateway"), dict) else {}
    return int(gateway.get("port") or DEFAULT_PORT)


def main():
    parser = argparse.ArgumentParser(description="Local SMS gateway for other projects")
    sub = parser.add_subparsers(dest="command", required=True)

    serve_parser = sub.add_parser("serve", help="start local HTTP API server")
    serve_parser.add_argument("--host", default=default_gateway_host())
    serve_parser.add_argument("--port", type=int, default=default_gateway_port())

    common_commands = ["health", "docs", "allocate", "countries", "balance", "ready", "code", "wait-code", "finish", "cancel"]
    for command in common_commands:
        p = sub.add_parser(command)
        p.add_argument("--json", dest="json_payload", default="")
        p.add_argument("--provider", default="")
        p.add_argument("--service", default="")
        p.add_argument("--countries", default="")
        p.add_argument("--min-price", type=float, default=None)
        p.add_argument("--max-price", type=float, default=None)
        p.add_argument("--id", default="")
        p.add_argument("--timeout", type=int, default=None)
        p.add_argument("--interval", type=int, default=None)
        p.add_argument("--proxy", default="")
        p.add_argument("--five-country", default="")
        p.add_argument("--five-operator", default="")
        p.add_argument("--five-product", default="")
        p.add_argument("--mark-ready", action="store_true")

    args = parser.parse_args()
    if args.command == "serve":
        serve(args.host, args.port)
        return

    result = run_action(args.command, build_payload(args))
    print(json.dumps(result, ensure_ascii=False))
    if not result.get("ok"):
        raise SystemExit(1)


if __name__ == "__main__":
    main()
