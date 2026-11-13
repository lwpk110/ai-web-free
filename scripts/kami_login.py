#!/usr/bin/env python3
"""
ChatGPT 卡密账号 (邮箱----密码----2FA密钥) 自动登录与 Token 提取工具
适配 ai-web-free 反代号池，支持：
1. 独立 CLI 运行：解析卡密 -> Playwright 登录提取 accessToken & session_token -> 输出 sub2api JSON 或直接推送至 ai-web-free
2. 被 ai-web-free Go 后端作为子进程调用（--stdin-json --json-output）
3. 查看实时 2FA 动态码（--mode totp）与清洗文本（--mode clean）
"""

import argparse
import base64
import hashlib
import hmac
import json
import os
import struct
import sys
import time
import urllib.error
import urllib.request
from datetime import datetime, timezone


def generate_totp(secret: str) -> str:
    """使用原生标准库计算 30 秒周期的 6 位 TOTP 动态码"""
    secret = secret.strip().replace(" ", "").upper()
    missing_padding = len(secret) % 8
    if missing_padding != 0:
        secret += "=" * (8 - missing_padding)
    try:
        key = base64.b32decode(secret)
    except Exception as e:
        return f"ERR({e})"

    counter = int(time.time() // 30)
    msg = struct.pack(">Q", counter)
    h = hmac.new(key, msg, hashlib.sha1).digest()
    offset = h[19] & 0x0F
    code = (struct.unpack(">I", h[offset : offset + 4])[0] & 0x7FFFFFFF) % 1000000
    return f"{code:06d}"


def parse_kami_file(file_path: str):
    """解析卡密文件，提取 email, password, totp_secret"""
    accounts = []
    if not os.path.exists(file_path):
        raise FileNotFoundError(f"文件未找到: {file_path}")

    with open(file_path, "r", encoding="utf-8") as f:
        for line_no, raw_line in enumerate(f, start=1):
            line = raw_line.strip()
            if not line or line.startswith("#"):
                continue

            parts = [p.strip() for p in line.split("----")]
            if len(parts) == 3:
                email, password, totp_secret = parts
            elif len(parts) == 1:
                sub_parts = line.split()
                if len(sub_parts) == 3:
                    email, password, totp_secret = sub_parts
                else:
                    continue
            else:
                continue

            if email and password and totp_secret:
                accounts.append({
                    "email": email,
                    "password": password,
                    "totp_secret": totp_secret,
                    "line_no": line_no,
                })
    return accounts


def build_empty_sub2api_json(accounts, concurrency=10, priority=1):
    """构建 Sub2API 标准空白数据包"""
    now_iso = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%S.000Z")
    data = {
        "type": "sub2api-data",
        "version": 1,
        "exported_at": now_iso,
        "proxies": [],
        "accounts": [],
    }

    for acc in accounts:
        data["accounts"].append({
            "name": acc["email"],
            "platform": "openai",
            "type": "oauth",
            "concurrency": concurrency,
            "priority": priority,
            "rate_multiplier": 1,
            "auto_pause_on_expired": True,
            "credentials": {
                "refresh_token": "",
                "access_token": "",
                "session_token": "",
                "id_token": "",
                "expires_at": 0,
            },
            "extra": {
                "email": acc["email"],
                "password": acc["password"],
                "totp_secret": acc["totp_secret"],
            },
        })
    return data


def _extract_jwt_meta(access_token: str):
    chatgpt_account_id = ""
    expires_at = int(time.time()) + 10 * 86400
    try:
        jwt_parts = access_token.split(".")
        if len(jwt_parts) == 3:
            padded = jwt_parts[1] + "=" * ((4 - len(jwt_parts[1]) % 4) % 4)
            payload = json.loads(base64.urlsafe_b64decode(padded).decode("utf-8"))
            auth_info = payload.get("https://api.openai.com/auth", {})
            if auth_info.get("chatgpt_account_id"):
                chatgpt_account_id = auth_info["chatgpt_account_id"]
            if payload.get("exp"):
                expires_at = int(payload["exp"])
    except Exception:
        pass
    return chatgpt_account_id, expires_at


def _login_single_account(context, page, account_info, timeout=120, quiet=False):
    email = account_info["email"]
    password = account_info["password"]
    totp_secret = account_info["totp_secret"]

    def log(msg):
        if not quiet:
            print(msg, flush=True)

    log(f"\n{'='*55}\n[*] 正在处理账号: {email}\n{'='*55}")

    captured = {
        "access_token": None,
        "session_data": {},
        "user_id": "",
    }

    def on_request(req):
        auth = req.headers.get("authorization", "")
        if auth.startswith("Bearer ey"):
            token = auth.replace("Bearer ", "").strip()
            if len(token) > 50 and not captured["access_token"]:
                log("[✔] [网络监听] 截获 Authorization Bearer Token")
                captured["access_token"] = token

    def on_response(resp):
        if "/api/auth/session" in resp.url and resp.status == 200:
            try:
                data = resp.json()
                if "accessToken" in data:
                    log("[✔] [网络监听] 截获 /api/auth/session 响应")
                    captured["access_token"] = data["accessToken"]
                    captured["session_data"] = data
                    if data.get("user", {}).get("id"):
                        captured["user_id"] = data["user"]["id"]
            except Exception:
                pass

    page.on("request", on_request)
    page.on("response", on_response)

    log("[*] 正在打开 ChatGPT 登录页...")
    page.goto("https://chatgpt.com/auth/login", wait_until="commit", timeout=45000)

    try:
        login_btn = page.locator('button:has-text("Log in")', has_text="Log in")
        if login_btn.count() > 0 and login_btn.first.is_visible():
            login_btn.first.click()
            time.sleep(2)
    except Exception:
        pass

    log("[*] 等待邮箱输入框...")
    try:
        email_input = page.wait_for_selector(
            'input[name="username"], input[type="email"], #email-input',
            timeout=20000,
        )
        email_input.fill(email)
        time.sleep(0.5)
        page.locator('button[type="submit"], button:has-text("Continue")').first.click()
    except Exception as e:
        log(f"[!] 邮箱输入提示: {e}")

    log("[*] 等待密码输入框...")
    try:
        password_input = page.wait_for_selector(
            'input[name="password"], input[type="password"]',
            timeout=20000,
        )
        password_input.fill(password)
        time.sleep(0.5)
        page.locator('button[type="submit"], button:has-text("Continue")').first.click()
    except Exception as e:
        log(f"[!] 密码提交提示: {e}")

    log("[*] 检查是否需要 2FA 验证码...")
    time.sleep(3)
    try:
        totp_loc = page.locator(
            'input[inputmode="numeric"], input[name="code"], input[autocomplete="one-time-code"], input[placeholder*="code" i]'
        ).first
        totp_loc.wait_for(state="visible", timeout=10000)
        if totp_loc.is_visible():
            code = generate_totp(totp_secret)
            log(f"[+] 自动计算 2FA 动态码: {code}，正在填入...")
            totp_loc.click()
            totp_loc.fill("")
            page.keyboard.type(code, delay=60)
            time.sleep(0.5)
            page.keyboard.press("Enter")
            time.sleep(0.5)

            for sel in [
                'button[type="submit"]',
                'button:has-text("Continue")',
                'button:has-text("Verify")',
                'button:has-text("继续")',
                'button:has-text("验证")',
                'button[name="action"][value="default"]',
            ]:
                try:
                    btn = page.locator(sel).first
                    if btn.count() > 0 and btn.is_visible():
                        btn.click()
                        break
                except Exception:
                    pass
    except Exception as e:
        log(f"[-] 2FA 输入步骤提示: {e}")

    log("[*] 正在等待进入 ChatGPT... (如遇 Cloudflare 人机验证，请在浏览器中手动点击通过)")
    start_time = time.time()
    last_url = ""

    while time.time() - start_time < timeout:
        if captured["access_token"]:
            break

        current_url = page.url
        if current_url != last_url:
            log(f"[*] 当前页面 URL: {current_url}")
            last_url = current_url

        if "mfa-challenge" in current_url:
            try:
                page.keyboard.press("Enter")
                btn = page.locator('button[type="submit"], button:has-text("Continue"), button:has-text("Verify")').first
                if btn.count() > 0 and btn.is_visible():
                    btn.click()
            except Exception:
                pass

        if "chatgpt.com" in current_url and ("auth0" not in current_url):
            try:
                session_raw = page.evaluate("""
                    async () => {
                        try {
                            const res = await fetch('/api/auth/session');
                            if (res.ok) { return await res.json(); }
                            return null;
                        } catch (e) { return null; }
                    }
                """)
                if session_raw and "accessToken" in session_raw:
                    captured["access_token"] = session_raw["accessToken"]
                    captured["session_data"] = session_raw
                    if session_raw.get("user", {}).get("id"):
                        captured["user_id"] = session_raw["user"]["id"]
                    break
            except Exception:
                pass

            if time.time() - start_time > 15 and not captured["access_token"]:
                try:
                    new_tab = context.new_page()
                    new_tab.goto("https://chatgpt.com/api/auth/session", timeout=15000)
                    body_text = new_tab.inner_text("body")
                    new_tab.close()
                    if "accessToken" in body_text:
                        parsed = json.loads(body_text)
                        captured["access_token"] = parsed["accessToken"]
                        captured["session_data"] = parsed
                        if parsed.get("user", {}).get("id"):
                            captured["user_id"] = parsed["user"]["id"]
                        break
                except Exception:
                    pass

        time.sleep(2)

    if not captured["access_token"]:
        time.sleep(2)

    if not captured["access_token"]:
        raise RuntimeError(f"未能获取到账号 {email} 的 Token (等待超时，当前 URL: {page.url})")

    # 额外提取 __Secure-next-auth.session-token Cookie（供 ai-web-free 自动续期）
    session_cookie = ""
    try:
        for ck in context.cookies(["https://chatgpt.com"]):
            if ck.get("name") == "__Secure-next-auth.session-token" and ck.get("value"):
                session_cookie = ck["value"]
                break
    except Exception:
        pass

    session_data = captured["session_data"] or {}
    session_data["accessToken"] = captured["access_token"]
    session_data["sessionToken"] = session_cookie
    if not session_data.get("user"):
        session_data["user"] = {"id": captured["user_id"] or f"user_{email.split('@')[0]}"}
    return session_data


def run_batch_login(accounts_list, proxy_url=None, headless=False, target_account=None, force=False, quiet=False):
    try:
        from playwright.sync_api import sync_playwright
    except ImportError:
        msg = "缺少 playwright 库，请先运行: pip install playwright && playwright install chromium"
        if quiet:
            return {"ok": False, "error": msg, "results": []}
        print(f"[!] {msg}", file=sys.stderr)
        sys.exit(1)

    if "DISPLAY" not in os.environ and not headless:
        os.environ["DISPLAY"] = ":0"

    proxy_cfg = {"server": proxy_url} if proxy_url else None
    results = []

    with sync_playwright() as p:
        browser = p.chromium.launch(
            headless=headless,
            proxy=proxy_cfg,
            args=["--no-sandbox", "--disable-blink-features=AutomationControlled"],
        )

        for acc in accounts_list:
            email = acc.get("email") or acc.get("name", "")
            if target_account and target_account.lower() not in email.lower():
                continue

            if not force and acc.get("access_token"):
                exp_ts = acc.get("expires_at", 0)
                if exp_ts > int(time.time()) + 300:
                    results.append({"email": email, "ok": True, "skipped": True, "access_token": acc["access_token"]})
                    continue

            password = acc.get("password", "")
            totp_secret = acc.get("totp_secret", "")
            if not password or not totp_secret:
                results.append({"email": email, "ok": False, "error": "缺少密码或 2FA 密钥"})
                continue

            acc_proxy = acc.get("proxy") or proxy_url
            ctx_kwargs = {
                "user_agent": "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0.0.0 Safari/537.36",
                "viewport": {"width": 1280, "height": 800},
            }
            if acc_proxy:
                ctx_kwargs["proxy"] = {"server": acc_proxy}

            context = browser.new_context(**ctx_kwargs)
            page = context.new_page()
            try:
                session_data = _login_single_account(
                    context, page, {"email": email, "password": password, "totp_secret": totp_secret}, quiet=quiet
                )
                access_token = session_data["accessToken"]
                session_token = session_data.get("sessionToken", "")
                chatgpt_account_id, expires_at = _extract_jwt_meta(access_token)
                if not chatgpt_account_id:
                    chatgpt_account_id = session_data.get("user", {}).get("id", "")

                results.append({
                    "email": email,
                    "ok": True,
                    "access_token": access_token,
                    "session_token": session_token,
                    "chatgpt_account_id": chatgpt_account_id,
                    "expires_at": expires_at,
                })
            except Exception as e:
                results.append({"email": email, "ok": False, "error": str(e)})
            finally:
                context.close()

        browser.close()

    return {"ok": True, "results": results}


def push_to_ai_web_free(server_url: str, sub2api_data: dict):
    url = server_url.rstrip("/") + "/admin/api/v1/providers/chatgpt/accounts/import"
    payload = json.dumps({
        "credential_type": "sub2api_json",
        "raw_text": json.dumps(sub2api_data, ensure_ascii=False),
        "persist": True,
    }).encode("utf-8")
    req = urllib.request.Request(
        url,
        data=payload,
        headers={"Content-Type": "application/json"},
        method="POST",
    )
    with urllib.request.urlopen(req, timeout=30) as resp:
        return json.loads(resp.read().decode("utf-8"))


def main():
    parser = argparse.ArgumentParser(description="ChatGPT 卡密账号登录与 ai-web-free 导入工具")
    parser.add_argument("input", nargs="?", default=None, help="输入文件路径 (.txt 卡密或 .json 数据包)")
    parser.add_argument("-o", "--output", default=None, help="输出文件路径")
    parser.add_argument("-m", "--mode", choices=["auto", "json-only", "clean", "totp"], default="auto")
    parser.add_argument("--proxy", default=None, help="代理地址")
    parser.add_argument("--headless", action="store_true", help="无头模式运行")
    parser.add_argument("--account", default=None, help="仅处理指定邮箱账号")
    parser.add_argument("--force", action="store_true", help="强制重新登录刷新")
    parser.add_argument("--push-url", default=None, help="提取完成后直接推送到 ai-web-free 服务地址 (如 http://127.0.0.1:8080)")
    parser.add_argument("--stdin-json", action="store_true", help="从 stdin 读取 JSON 账号列表（供后端调用）")
    parser.add_argument("--json-output", action="store_true", help="以 JSON 格式输出结果到 stdout（供后端调用）")

    args = parser.parse_args()
    proxy_url = args.proxy or os.environ.get("HTTPS_PROXY") or os.environ.get("HTTP_PROXY")

    # 后端子进程模式：stdin JSON -> stdout JSON
    if args.stdin_json:
        raw_input = sys.stdin.read()
        payload = json.loads(raw_input) if raw_input.strip() else {}
        accounts_list = payload.get("accounts", [])
        proxy_url = payload.get("proxy") or proxy_url
        res = run_batch_login(
            accounts_list,
            proxy_url=proxy_url,
            headless=args.headless,
            target_account=args.account,
            force=True,
            quiet=args.json_output,
        )
        print(json.dumps(res, ensure_ascii=False))
        return

    if not args.input:
        print("请提供卡密 txt 或 sub2api json 文件路径", file=sys.stderr)
        sys.exit(1)

    if args.mode == "totp":
        accounts = parse_kami_file(args.input)
        remaining_sec = 30 - int(time.time() % 30)
        print(f"\n当前时间 2FA 验证码 (剩余有效秒数: {remaining_sec}s):")
        print("-" * 75)
        for i, acc in enumerate(accounts, start=1):
            print(f"{i:<4} {acc['email']:<36} {generate_totp(acc['totp_secret']):<12} {acc['password']}")
        print("-" * 75)
        return

    if args.mode == "clean":
        accounts = parse_kami_file(args.input)
        out_str = "\n".join(f"{a['email']}----{a['password']}----{a['totp_secret']}" for a in accounts) + "\n"
        out_file = args.output or os.path.splitext(args.input)[0] + "_clean.txt"
        with open(out_file, "w", encoding="utf-8") as f:
            f.write(out_str)
        print(f"[✔] 已生成清洗文本: {out_file}")
        return

    if args.input.endswith(".json"):
        with open(args.input, "r", encoding="utf-8") as f:
            data = json.load(f)
        target_json = args.input
    else:
        accounts = parse_kami_file(args.input)
        data = build_empty_sub2api_json(accounts)
        target_json = args.output or os.path.splitext(args.input)[0] + "_sub2api.json"
        with open(target_json, "w", encoding="utf-8") as f:
            json.dump(data, f, ensure_ascii=False, indent=2)
        print(f"[✔] 基础 JSON 数据包已生成: {target_json}")

    if args.mode == "json-only":
        return

    flat_accounts = []
    for item in data.get("accounts", []):
        extra = item.get("extra", {})
        creds = item.get("credentials", {})
        flat_accounts.append({
            "email": extra.get("email") or item.get("name", ""),
            "password": extra.get("password", ""),
            "totp_secret": extra.get("totp_secret", ""),
            "access_token": creds.get("access_token", ""),
            "expires_at": creds.get("expires_at", 0),
        })

    batch_res = run_batch_login(
        flat_accounts,
        proxy_url=proxy_url,
        headless=args.headless,
        target_account=args.account,
        force=args.force,
        quiet=args.json_output,
    )

    by_email = {r["email"]: r for r in batch_res.get("results", []) if r.get("ok")}
    for item in data.get("accounts", []):
        email = item.get("extra", {}).get("email") or item.get("name", "")
        if email in by_email and by_email[email].get("access_token"):
            r = by_email[email]
            item["credentials"]["access_token"] = r["access_token"]
            if r.get("session_token"):
                item["credentials"]["session_token"] = r["session_token"]
            if r.get("chatgpt_account_id"):
                item["credentials"]["chatgpt_account_id"] = r["chatgpt_account_id"]
            if r.get("expires_at"):
                item["credentials"]["expires_at"] = r["expires_at"]

    with open(target_json, "w", encoding="utf-8") as f:
        json.dump(data, f, ensure_ascii=False, indent=2)

    if args.json_output:
        print(json.dumps(batch_res, ensure_ascii=False))
    else:
        print(f"[✔] 已写回 Token 至: {target_json}")

    if args.push_url:
        try:
            push_res = push_to_ai_web_free(args.push_url, data)
            print(f"[✔] 已推送至 ai-web-free 号池: {push_res}")
        except Exception as e:
            print(f"[!] 推送至 ai-web-free 失败: {e}", file=sys.stderr)


if __name__ == "__main__":
    main()
