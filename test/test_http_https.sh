#!/bin/bash

# GoProxy HTTP 协议代理 HTTPS 访问测试脚本
# 随机访问多个 HTTPS 网站，验证 HTTP 代理的 CONNECT 隧道能力
# 用法: GOPROXY_AUTH_USERNAME=acct GOPROXY_AUTH_PASSWORD=... ./test_http_https.sh [端口号，默认7802] [测试次数，默认持续运行]
# 可选: GOPROXY_AUTH_REGION=us GOPROXY_AUTH_SESSION=browser
# 按 Ctrl+C 停止测试

PROXY_HOST="${PROXY_HOST:-127.0.0.1}"
PROXY_PORT="${1:-7802}"
MAX_COUNT="${2:-0}"  # 0 = 持续运行
DELAY=2

require_proxy_auth() {
    if [ -z "${GOPROXY_AUTH_USERNAME:-}" ] || [ -z "${GOPROXY_AUTH_PASSWORD:-}" ]; then
        echo "Missing proxy credentials." >&2
        echo "Set GOPROXY_AUTH_USERNAME and GOPROXY_AUTH_PASSWORD from the first-boot log or WebUI Settings." >&2
        echo "Optional: GOPROXY_AUTH_REGION=us GOPROXY_AUTH_SESSION=browser" >&2
        exit 2
    fi
}

proxy_auth_username() {
    local username="$GOPROXY_AUTH_USERNAME"
    if [ -n "${GOPROXY_AUTH_REGION:-}" ]; then
        username="${username}-region-${GOPROXY_AUTH_REGION}"
    fi
    if [ -n "${GOPROXY_AUTH_SESSION:-}" ]; then
        username="${username}-session-${GOPROXY_AUTH_SESSION}"
    fi
    echo "$username"
}

setup_curl_auth_config() {
    local old_umask
    old_umask=$(umask)
    umask 077
    CURL_AUTH_CONFIG=$(mktemp "${TMPDIR:-/tmp}/goproxy-curl-auth.XXXXXX")
    umask "$old_umask"
    printf 'proxy-user = "%s:%s"\n' "$(proxy_auth_username)" "$GOPROXY_AUTH_PASSWORD" > "$CURL_AUTH_CONFIG"
    trap 'rm -f "$CURL_AUTH_CONFIG"' EXIT INT TERM
}

# 测试目标（HTTPS 网站）
TARGETS=(
    "https://www.google.com"
    "https://www.openai.com"
    "https://www.github.com"
    "https://www.cloudflare.com"
    "https://httpbin.org/ip"
)

# 统计变量
total=0
success=0
fail=0

# 获取毫秒时间戳
get_ms_time() {
    python3 -c 'import time; print(int(time.time() * 1000))'
}

# 捕获 Ctrl+C 信号
trap ctrl_c INT
function ctrl_c() {
    echo ""
    echo "---"
    if [ $total -gt 0 ]; then
        loss_rate=$(awk "BEGIN {printf \"%.1f\", ($total - $success)/$total*100}")
        success_rate=$(awk "BEGIN {printf \"%.1f\", $success/$total*100}")
        echo "$total requests transmitted, $success succeeded, $fail failed, ${loss_rate}% loss, ${success_rate}% success rate"
    fi
    exit 0
}

echo "HTTP PROXY HTTPS TEST — $PROXY_HOST:$PROXY_PORT"
echo "targets: ${#TARGETS[@]} HTTPS sites"
echo ""

require_proxy_auth
setup_curl_auth_config

while true; do
    # 随机选择目标
    idx=$((RANDOM % ${#TARGETS[@]}))
    target="${TARGETS[$idx]}"

    total=$((total + 1))

    start_time=$(get_ms_time)
    response=$(curl -x "http://${PROXY_HOST}:${PROXY_PORT}" \
					--config "$CURL_AUTH_CONFIG" \
                   -s -k \
                   -o /dev/null \
                   -w "%{http_code}" \
                   --connect-timeout 10 \
                   --max-time 15 \
                   "${target}" 2>&1)
    end_time=$(get_ms_time)
    elapsed=$((end_time - start_time))

    if [[ "$response" =~ ^[23] ]]; then
        echo "✅ seq=$total ${target} -> HTTP $response time=${elapsed}ms"
        success=$((success + 1))
    else
        echo "❌ seq=$total ${target} -> HTTP $response time=${elapsed}ms"
        fail=$((fail + 1))
    fi

    # 达到指定次数则停止
    if [ "$MAX_COUNT" -gt 0 ] && [ "$total" -ge "$MAX_COUNT" ]; then
        ctrl_c
    fi

    sleep $DELAY
done
