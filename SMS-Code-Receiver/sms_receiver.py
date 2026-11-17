import argparse
import json
import os
import re
import sys
import time
import urllib.parse
import urllib.request
from pathlib import Path


ROOT = Path(__file__).resolve().parent
CONFIG_PATH = ROOT / "config.json"
DATA_DIR = ROOT / "data"
ORDERS_LOG = DATA_DIR / "orders.jsonl"


def load_json(path):
    if not path or not Path(path).exists():
        return {}
    return json.loads(Path(path).read_text(encoding="utf-8-sig"))


def deep_merge(base, extra):
    result = dict(base or {})
    for key, value in (extra or {}).items():
        if isinstance(value, dict) and isinstance(result.get(key), dict):
            result[key] = deep_merge(result[key], value)
        elif value is not None:
            result[key] = value
    return result


def load_config():
    local = load_json(CONFIG_PATH)
    source = load_json(local.get("sourceConfigPath"))
    merged = deep_merge(source, local)
    return merged


def split_csv(value):
    if value is None:
        return []
    if isinstance(value, list):
        return [str(item).strip() for item in value if str(item).strip()]
    return [item.strip() for item in re.split(r"[,，、\s]+", str(value)) if item.strip()]


def numbers(value):
    out = []
    for item in split_csv(value):
        try:
            out.append(int(item))
        except ValueError:
            pass
    return out


def optional_float(value, default=None):
    if value is None or value == "":
        return default
    try:
        return float(value)
    except ValueError:
        return default


def parse_price(value):
    if value is None or value == "":
        return None
    if isinstance(value, (int, float)):
        return float(value)
    match = re.search(r"-?\d+(?:[.,]\d+)?", str(value))
    if not match:
        return None
    try:
        return float(match.group(0).replace(",", "."))
    except ValueError:
        return None


def cancel_for_price_limit(provider, order):
    order_id = order.get("id")
    if not order_id:
        return ""
    try:
        provider.cancel(order_id)
        return "，已自动取消订单"
    except Exception as exc:
        return f"，自动取消失败: {exc}"


def enforce_order_price(provider, order, max_price=0):
    limit = optional_float(max_price, 0) or 0
    if limit <= 0:
        return order
    price = parse_price(order.get("price"))
    provider_name = order.get("provider") or getattr(provider, "name", "provider")
    if price is None:
        suffix = cancel_for_price_limit(provider, order)
        raise RuntimeError(
            f"WRONG_MAX_PRICE: {provider_name} did not return price, cannot verify maxPrice ${limit:.3f}{suffix}"
        )
    if price > limit + 1e-9:
        suffix = cancel_for_price_limit(provider, order)
        raise RuntimeError(
            f"WRONG_MAX_PRICE: {provider_name} price ${price:.3f} exceeds maxPrice ${limit:.3f}{suffix}"
        )
    return order


def request_json_or_text(url, params=None, headers=None, proxy=None, timeout=30):
    if params:
        url = f"{url}?{urllib.parse.urlencode(params)}"
    req_headers = {"User-Agent": "Mozilla/5.0", "Accept": "application/json,text/plain,*/*"}
    req_headers.update(headers or {})
    req = urllib.request.Request(url, headers=req_headers)
    opener = urllib.request.build_opener()
    if proxy:
        opener = urllib.request.build_opener(
            urllib.request.ProxyHandler({"http": proxy, "https": proxy})
        )
    with opener.open(req, timeout=timeout) as resp:
        raw = resp.read().decode("utf-8", errors="replace")
    try:
        return json.loads(raw)
    except json.JSONDecodeError:
        return raw.strip()


def append_order(row):
    DATA_DIR.mkdir(exist_ok=True)
    payload = {"at": time.strftime("%Y-%m-%dT%H:%M:%S%z"), **row}
    with ORDERS_LOG.open("a", encoding="utf-8") as fh:
        fh.write(json.dumps(payload, ensure_ascii=False) + "\n")


class SmsActivateProvider:
    def __init__(self, name, api_key, base_url, proxy=None):
        self.name = name
        self.api_key = api_key
        self.base_url = base_url
        self.proxy = proxy

    def request(self, action, **params):
        if not self.api_key:
            raise RuntimeError(f"{self.name} API key is not configured")
        return request_json_or_text(
            self.base_url,
            {"api_key": self.api_key, "action": action, **params},
            proxy=self.proxy,
        )

    def balance(self):
        return self.request("getBalance")

    def prices(self, service):
        return self.request("getPrices", service=service)

    def normalize_error(self, data):
        if data == "NO_BALANCE":
            return "余额不足 / NO_BALANCE"
        if data == "BAD_KEY":
            return "API Key 无效 / BAD_KEY"
        if data in ("BAD_SERVICE", "WRONG_SERVICE"):
            return "服务代码不支持 / WRONG_SERVICE"
        if data == "NO_NUMBERS":
            return "当前国家没有号码 / NO_NUMBERS"
        if data == "NO_ACTIVATION":
            return "订单不存在或已关闭 / NO_ACTIVATION"
        if data == "BAD_STATUS":
            return "订单状态不允许该操作 / BAD_STATUS"
        if str(data).startswith("WRONG_MAX_PRICE"):
            return f"价格上限无效 / {data}"
        return str(data)

    def buy_number(self, service, countries, min_price=0, max_price=0):
        last_error = ""
        country_pool = countries or [16]
        for country in country_pool:
            params = {"service": service, "country": country}
            if max_price and max_price > 0:
                params["maxPrice"] = max_price
            if min_price and min_price > 0:
                params["minPrice"] = min_price
            print(f"[{self.name}] request number service={service} country={country} maxPrice={max_price or 'none'}")
            data = self.request("getNumberV2", **params)
            if isinstance(data, str):
                last_error = f"{country}:{self.normalize_error(data)}"
                print(f"[{self.name}] {last_error}")
                continue
            activation_id = data.get("activationId") or data.get("id")
            phone = data.get("phoneNumber") or data.get("number")
            if not activation_id or not phone:
                last_error = f"{country}: incomplete response {data}"
                print(f"[{self.name}] {last_error}")
                continue
            if not str(phone).startswith("+"):
                phone = f"+{phone}"
            order = {
                "provider": self.name,
                "id": str(activation_id),
                "phone": str(phone),
                "country": str(country),
                "price": data.get("activationCost") or data.get("cost"),
            }
            return enforce_order_price(self, order, max_price)
        raise RuntimeError(f"{self.name} failed to get number: {last_error}")

    def expect_set_status(self, action, data, expected, accepted=()):
        accepted_values = {expected, *accepted}
        if isinstance(data, str) and data in accepted_values:
            return data
        if isinstance(data, dict):
            markers = [
                data.get("status"),
                data.get("message"),
                data.get("response"),
                data.get("data"),
            ]
            if any(str(value) in accepted_values for value in markers if value is not None):
                return data
            if data.get("success") is True or data.get("ok") is True:
                return data
        raise RuntimeError(f"{self.name} {action} failed: {self.normalize_error(data)}")

    def ready(self, order_id):
        data = self.request("setStatus", id=order_id, status=1)
        return self.expect_set_status("ready", data, "ACCESS_READY")

    def get_code(self, order_id):
        data = self.request("getStatusV2" if self.name == "herosms" else "getStatus", id=order_id)
        if isinstance(data, str):
            if data.startswith("STATUS_OK:"):
                return data.split(":", 1)[1].strip()
            if data == "STATUS_CANCEL":
                raise RuntimeError("activation canceled")
            return None
        sms_code = (((data or {}).get("sms") or {}).get("code"))
        return str(sms_code).strip() if sms_code else None

    def finish(self, order_id):
        data = self.request("setStatus", id=order_id, status=6)
        return self.expect_set_status("finish", data, "ACCESS_ACTIVATION", accepted=("NO_ACTIVATION",))

    def cancel(self, order_id):
        data = self.request("setStatus", id=order_id, status=8)
        return self.expect_set_status("cancel", data, "ACCESS_CANCEL", accepted=("STATUS_CANCEL", "NO_ACTIVATION"))


class FiveSim:
    def __init__(self, api_key, country="england", operator="any", product="whatsapp", proxy=None):
        self.name = "5sim"
        self.api_key = api_key
        self.country = country
        self.operator = operator
        self.product = product
        self.proxy = proxy

    def headers(self):
        return {"Authorization": f"Bearer {self.api_key}", "Accept": "application/json"}

    def request(self, path):
        if not self.api_key:
            raise RuntimeError("5sim API key is not configured")
        return request_json_or_text(f"https://5sim.net{path}", headers=self.headers(), proxy=self.proxy)

    def public_request(self, path):
        return request_json_or_text(f"https://5sim.net{path}", headers={"Accept": "application/json"}, proxy=self.proxy)

    def balance(self):
        return self.request("/v1/user/profile")

    def buy_number(self, service, countries, min_price=0, max_price=0):
        country_pool = [str(country).strip() for country in (countries or [self.country]) if str(country).strip()]
        if not country_pool:
            country_pool = [self.country]
        last_error = ""
        for country in country_pool:
            try:
                order = self.buy_number_for_country(country)
                return enforce_order_price(self, order, max_price)
            except Exception as exc:
                last_error = f"{country}: {exc}"
                print(f"[5sim] {last_error}")
        raise RuntimeError(f"5sim failed to get number: {last_error}")

    def buy_number_for_country(self, country):
        data = self.request(
            f"/v1/user/buy/activation/{urllib.parse.quote(country)}/"
            f"{urllib.parse.quote(self.operator)}/{urllib.parse.quote(self.product)}"
        )
        if not isinstance(data, dict):
            raise RuntimeError(f"5sim response: {data}")
        order_id = data.get("id") or data.get("activationId") or data.get("order_id")
        phone = data.get("phone") or data.get("number") or data.get("phoneNumber")
        if not order_id or not phone:
            raise RuntimeError(f"5sim incomplete response: {data}")
        if not str(phone).startswith("+"):
            phone = f"+{phone}"
        order = {
            "provider": "5sim",
            "id": str(order_id),
            "phone": str(phone),
            "country": country,
            "price": data.get("price") or data.get("cost"),
        }
        return order

    def ready(self, order_id):
        return None

    def get_code(self, order_id):
        data = self.request(f"/v1/user/check/{urllib.parse.quote(str(order_id))}")
        for item in reversed(data.get("sms") or data.get("messages") or []):
            direct = item.get("code") or item.get("sms_code") or item.get("otp")
            if direct:
                return str(direct).strip()
            text = str(item.get("text") or item.get("message") or item.get("body") or "")
            match = re.search(r"\b(\d{4,8})\b", text)
            if match:
                return match.group(1)
        return None

    def finish(self, order_id):
        return self.request(f"/v1/user/finish/{urllib.parse.quote(str(order_id))}")

    def cancel(self, order_id):
        return self.request(f"/v1/user/cancel/{urllib.parse.quote(str(order_id))}")


def make_providers(config, args):
    proxy = args.proxy if args.proxy is not None else config.get("proxy")
    five = config.get("fiveSim") or {}
    return {
        "smsbower": SmsActivateProvider(
            "smsbower",
            config.get("smsBowerApiKey") or config.get("smsbowerApiKey") or "",
            "https://smsbower.page/stubs/handler_api.php",
            proxy=proxy,
        ),
        "herosms": SmsActivateProvider(
            "herosms",
            config.get("heroSmsApiKey") or "",
            "https://hero-sms.com/stubs/handler_api.php",
            proxy=proxy,
        ),
        "5sim": FiveSim(
            config.get("fiveSimApiKey") or "",
            country=args.five_country or five.get("country") or config.get("fiveSimCountry") or "england",
            operator=args.five_operator or five.get("operator") or config.get("fiveSimOperator") or "any",
            product=args.five_product or five.get("product") or config.get("fiveSimProduct") or "whatsapp",
            proxy=proxy,
        ),
    }


def print_prices(provider, service):
    data = provider.prices(service)
    rows = []
    for country, country_data in (data or {}).items():
        if not str(country).isdigit() or not isinstance(country_data, dict):
            continue
        service_data = country_data.get(service)
        if not isinstance(service_data, dict):
            continue
        rows.append((str(country), int(service_data.get("count") or 0), float(service_data.get("cost") or 0)))
    rows.sort(key=lambda row: (-row[1], row[2]))
    print("ID     Count      Cost")
    for country, count, cost in rows[:120]:
        print(f"{country:<6} {count:>8}   ${cost:.3f}")
    if len(rows) > 120:
        print(f"... {len(rows) - 120} more rows")


def wait_for_code(provider, order_id, interval, timeout):
    deadline = time.time() + timeout
    attempt = 0
    while time.time() < deadline:
        attempt += 1
        code = provider.get_code(order_id)
        if code:
            return code
        remaining = max(0, int(deadline - time.time()))
        print(f"[wait] no code yet ({attempt}), remaining {remaining}s")
        time.sleep(interval)
    raise TimeoutError(f"code timeout after {timeout}s")


def main():
    config = load_config()
    parser = argparse.ArgumentParser(description="Standalone SMS code receiver")
    parser.add_argument("--provider", choices=["smsbower", "herosms", "5sim"], default="")
    parser.add_argument("--service", default=config.get("service") or config.get("heroSmsService") or "dr")
    parser.add_argument("--countries", default=",".join(map(str, config.get("countries") or config.get("smsBowerCountries") or config.get("heroSmsCountries") or [])))
    parser.add_argument("--min-price", type=float, default=optional_float(config.get("minPrice"), optional_float(config.get("smsBowerMinPrice"), 0)))
    parser.add_argument("--max-price", type=float, default=optional_float(config.get("maxPrice"), optional_float(config.get("smsBowerMaxPrice"), 0)))
    parser.add_argument("--timeout", type=int, default=int(config.get("timeoutSeconds") or 180))
    parser.add_argument("--interval", type=int, default=int(config.get("pollIntervalSeconds") or 5))
    parser.add_argument("--proxy", default=None, help="Proxy URL. Empty string disables proxy.")
    parser.add_argument("--balance", action="store_true")
    parser.add_argument("--prices", action="store_true")
    parser.add_argument("--five-country", default="")
    parser.add_argument("--five-operator", default="")
    parser.add_argument("--five-product", default="")
    args = parser.parse_args()

    providers = make_providers(config, args)
    order = [args.provider] if args.provider else list(config.get("providerOrder") or ["smsbower", "herosms", "5sim"])
    order = [name.lower() for name in order if name.lower() in providers]
    countries = numbers(args.countries)

    if args.balance:
        for name in order:
            provider = providers[name]
            try:
                print(f"[{name}] balance/profile: {provider.balance()}")
            except Exception as exc:
                print(f"[{name}] balance failed: {exc}")
        return

    if args.prices:
        for name in order:
            if name == "5sim":
                print("[5sim] prices list is not implemented in this tool")
                continue
            print(f"\n[{name}] service={args.service}")
            print_prices(providers[name], args.service)
        return

    last_error = None
    selected_provider = None
    selected_order = None
    for name in order:
        try:
            selected_provider = providers[name]
            selected_order = selected_provider.buy_number(
                args.service,
                countries,
                min_price=args.min_price,
                max_price=args.max_price,
            )
            break
        except Exception as exc:
            last_error = exc
            print(f"[{name}] failed: {exc}")

    if not selected_order or not selected_provider:
        raise SystemExit(f"failed to get number: {last_error}")

    append_order({"event": "allocated", **selected_order})
    print("\n=== NUMBER READY ===")
    print(f"provider : {selected_order['provider']}")
    print(f"order id : {selected_order['id']}")
    print(f"country  : {selected_order.get('country') or '-'}")
    print(f"price    : {selected_order.get('price') or '-'}")
    print(f"phone    : {selected_order['phone']}")
    print("====================\n")

    input("Fill this phone on your target website, send SMS, then press Enter here...")
    selected_provider.ready(selected_order["id"])

    try:
        code = wait_for_code(selected_provider, selected_order["id"], args.interval, args.timeout)
        append_order({"event": "code", **selected_order, "code": "present"})
        print("\n=== SMS CODE ===")
        print(code)
        print("================\n")
        action = input("Press Enter to finish order, or type c to cancel/refund: ").strip().lower()
        if action == "c":
            selected_provider.cancel(selected_order["id"])
            append_order({"event": "canceled", **selected_order})
            print("[done] order canceled")
        else:
            selected_provider.finish(selected_order["id"])
            append_order({"event": "finished", **selected_order})
            print("[done] order finished")
    except KeyboardInterrupt:
        print("\n[user] interrupted, canceling order...")
        selected_provider.cancel(selected_order["id"])
        append_order({"event": "canceled", **selected_order, "reason": "keyboard_interrupt"})
    except Exception as exc:
        append_order({"event": "failed", **selected_order, "error": str(exc)})
        print(f"[error] {exc}")
        action = input("Type c to cancel/refund this order, or Enter to leave it: ").strip().lower()
        if action == "c":
            selected_provider.cancel(selected_order["id"])
            append_order({"event": "canceled", **selected_order, "reason": "after_error"})
            print("[done] order canceled")


if __name__ == "__main__":
    main()
