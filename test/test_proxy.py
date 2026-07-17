#!/usr/bin/env python3
"""
GeoProxy 持续测试脚本 - 类似 ping 命令的简洁输出
按 Ctrl+C 停止测试
"""

import requests
import time
import sys
import signal
import os
from urllib.parse import quote
from requests.exceptions import RequestException

# 配置
PROXY_HOST = os.getenv("PROXY_HOST", "127.0.0.1")
PROXY_PORT = int(sys.argv[1]) if len(sys.argv) > 1 and sys.argv[1].isdigit() else 7802
TEST_URL = "http://ip-api.com/json/?fields=countryCode,query"
DELAY_SECONDS = 1

# 统计变量
total_count = 0
success_count = 0


def country_to_emoji(country_code):
    """将国家代码转换为 emoji 旗帜"""
    if not country_code or country_code == "null":
        return "🌐"
    
    # 将国家代码转换为区域指示符号
    # A=127462, 所以 'US' -> 🇺🇸
    try:
        first = ord(country_code[0].upper()) - ord('A') + 127462
        second = ord(country_code[1].upper()) - ord('A') + 127462
        return chr(first) + chr(second)
    except:
        return "🌐"


def signal_handler(sig, frame):
    """处理 Ctrl+C 信号"""
    print()
    print("---")
    loss_count = total_count - success_count
    loss_rate = 0.0
    if total_count > 0:
        loss_rate = loss_count / total_count * 100
    print(f"{total_count} requests transmitted, {success_count} received, {loss_count} failed, {loss_rate:.1f}% packet loss")
    sys.exit(0)


def proxy_auth_username():
    """Build the proxy username DSL from GEOPROXY_AUTH_* env vars."""
    username = os.getenv("GEOPROXY_AUTH_USERNAME", "").strip()
    region = os.getenv("GEOPROXY_AUTH_REGION", "").strip()
    session = os.getenv("GEOPROXY_AUTH_SESSION", "").strip()
    if region:
        username = f"{username}-region-{region}"
    if session:
        username = f"{username}-session-{session}"
    return username


def require_proxy_auth():
    """Return proxy auth credentials or exit before entering continuous mode."""
    base_username = os.getenv("GEOPROXY_AUTH_USERNAME", "").strip()
    password = os.getenv("GEOPROXY_AUTH_PASSWORD", "")
    if not base_username or not password:
        print("Missing proxy credentials.", file=sys.stderr)
        print("Set GEOPROXY_AUTH_USERNAME and GEOPROXY_AUTH_PASSWORD from the first-boot log or WebUI Settings.", file=sys.stderr)
        print("Optional: GEOPROXY_AUTH_REGION=us GEOPROXY_AUTH_SESSION=browser", file=sys.stderr)
        sys.exit(2)
    return proxy_auth_username(), password


def test_http_proxy_continuous():
    """持续测试 HTTP 代理"""
    global total_count, success_count
    
    username, password = require_proxy_auth()
    proxy_url = f"http://{quote(username, safe='')}:{quote(password, safe='')}@{PROXY_HOST}:{PROXY_PORT}"
    proxies = {
        "http": proxy_url,
        "https": proxy_url,
    }
    
    print(f"PROXY {PROXY_HOST}:{PROXY_PORT} ({TEST_URL}): continuous mode")
    print()
    
    # 注册信号处理
    signal.signal(signal.SIGINT, signal_handler)
    
    while True:
        total_count += 1
        
        try:
            start_time = time.time()
            response = requests.get(
                TEST_URL,
                proxies=proxies,
                timeout=15,
            )
            elapsed = int((time.time() - start_time) * 1000)
            
            if response.status_code == 200:
                data = response.json()
                exit_ip = data.get("query", "Unknown")
                country_code = data.get("countryCode", "")
                flag = country_to_emoji(country_code)
                print(f"proxy from {flag} {exit_ip}: seq={total_count} time={elapsed}ms")
                success_count += 1
            else:
                print(f"proxy #{total_count}: request failed (HTTP {response.status_code})")
                
        except RequestException as e:
            error_msg = str(e).split(':')[0]
            print(f"proxy #{total_count}: {error_msg}")
        
        time.sleep(DELAY_SECONDS)


if __name__ == "__main__":
    # --check-auth：仅校验代理认证环境变量后立即退出，不进入持续请求循环。
    # 缺失凭据时 require_proxy_auth 打印报错并以退出码 2 结束；便于 CI/dry-run。
    if "--check-auth" in sys.argv[1:]:
        username, _ = require_proxy_auth()
        print(f"proxy auth OK: username={username}")
        sys.exit(0)
    test_http_proxy_continuous()
