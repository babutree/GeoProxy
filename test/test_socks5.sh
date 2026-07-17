#!/bin/bash
###
 # @LastEditTime: 2026-03-29 23:26:29
 # @Description: ...
 # @Date: 2026-03-29 23:14:32
 # @Author: isboyjc
 # @LastEditors: isboyjc
### 

# GeoProxy SOCKS5 代理测试脚本
# 用法: GEOPROXY_AUTH_USERNAME=username GEOPROXY_AUTH_PASSWORD=... ./test_socks5.sh [端口号，默认7801]
# 可选: GEOPROXY_AUTH_REGION=us GEOPROXY_AUTH_SESSION=browser

PROXY_HOST="${PROXY_HOST:-127.0.0.1}"
PROXY_PORT="${1:-7801}"
TEST_URL="https://httpbin.org/ip"
DELAY=1

require_proxy_auth() {
    if [ -z "${GEOPROXY_AUTH_USERNAME:-}" ] || [ -z "${GEOPROXY_AUTH_PASSWORD:-}" ]; then
        echo "Missing proxy credentials." >&2
        echo "Set GEOPROXY_AUTH_USERNAME and GEOPROXY_AUTH_PASSWORD from the first-boot log or WebUI Settings." >&2
        echo "Optional: GEOPROXY_AUTH_REGION=us GEOPROXY_AUTH_SESSION=browser" >&2
        exit 2
    fi
}

proxy_auth_username() {
    local username="$GEOPROXY_AUTH_USERNAME"
    if [ -n "${GEOPROXY_AUTH_REGION:-}" ]; then
        username="${username}-region-${GEOPROXY_AUTH_REGION}"
    fi
    if [ -n "${GEOPROXY_AUTH_SESSION:-}" ]; then
        username="${username}-session-${GEOPROXY_AUTH_SESSION}"
    fi
    echo "$username"
}

setup_curl_auth_config() {
    local old_umask
    old_umask=$(umask)
    umask 077
    CURL_AUTH_CONFIG=$(mktemp "${TMPDIR:-/tmp}/GeoProxy-curl-auth.XXXXXX")
    umask "$old_umask"
    printf 'proxy-user = "%s:%s"\n' "$(proxy_auth_username)" "$GEOPROXY_AUTH_PASSWORD" > "$CURL_AUTH_CONFIG"
    trap 'rm -f "$CURL_AUTH_CONFIG"' EXIT INT TERM
}

# 统计变量
total=0
success=0
fail=0

# 获取毫秒时间戳（兼容 macOS 和 Linux）
get_ms_time() {
    python3 -c 'import time; print(int(time.time() * 1000))'
}

# 国家代码转 emoji 旗帜
country_to_emoji() {
    local country_code="$1"
    if [ -z "$country_code" ] || [ "$country_code" = "null" ]; then
        echo "🌐"
        return
    fi
    
    local first="${country_code:0:1}"
    local second="${country_code:1:1}"
    python3 -c "print(chr(127462 + ord('$first') - ord('A')) + chr(127462 + ord('$second') - ord('A')))"
}

# 捕获 Ctrl+C 信号
trap ctrl_c INT
function ctrl_c() {
    echo ""
    echo "---"
    loss_rate=$(awk "BEGIN {printf \"%.1f\", ($total - $success)/$total*100}")
    echo "$total requests transmitted, $success received, $((total - success)) failed, ${loss_rate}% packet loss"
    exit 0
}

echo "SOCKS5 PROXY ${PROXY_HOST}:${PROXY_PORT}: continuous mode"
echo ""

require_proxy_auth
setup_curl_auth_config

while true; do
    total=$((total + 1))
    
    # 使用 curl 的 SOCKS5 支持；-k 用于避免上游 TLS 验证差异影响连通性测试
    start=$(get_ms_time)
    response=$(curl -s -k --socks5-hostname "${PROXY_HOST}:${PROXY_PORT}" --config "$CURL_AUTH_CONFIG" "${TEST_URL}" --max-time 10 2>&1)
    end=$(get_ms_time)
    latency=$((end - start))
    
    if echo "$response" | grep -q '"origin"'; then
        success=$((success + 1))
        origin=$(echo "$response" | grep -o '"origin":"[^"]*"' | cut -d'"' -f4 | cut -d',' -f1)
        country=$(curl -s "http://ip-api.com/json/${origin}?fields=countryCode" 2>/dev/null | grep -o '"countryCode":"[^"]*"' | cut -d'"' -f4)
        emoji=$(country_to_emoji "$country")
        echo "socks5 #${total}: ${origin} ${emoji} ${country} (${latency}ms)"
    else
        fail=$((fail + 1))
        echo "socks5 #${total}: request failed"
    fi
    
    sleep $DELAY
done
