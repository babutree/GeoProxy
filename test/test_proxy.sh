#!/bin/bash

# GoProxy 持续测试脚本 - 类似 ping 命令的简洁输出
# 按 Ctrl+C 停止测试
# 用法: GOPROXY_AUTH_USERNAME=acct GOPROXY_AUTH_PASSWORD=... ./test_proxy.sh [端口号，默认7802]
# 可选: GOPROXY_AUTH_REGION=us GOPROXY_AUTH_SESSION=browser

PROXY_HOST="${PROXY_HOST:-127.0.0.1}"
PROXY_PORT="${1:-7802}"
TEST_URL="http://ip-api.com/json/?fields=countryCode,query"
DELAY=1

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
    
    # 将国家代码转换为 emoji（使用 Unicode 区域指示符）
    # 每个字母转换为对应的区域指示符号字符
    local first="${country_code:0:1}"
    local second="${country_code:1:1}"
    
    # A=127462, 所以 A->🇦 就是 127462，B->🇧 就是 127463
    # 使用 printf 和 Unicode 编码
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

# 测试 HTTP 代理
test_http_proxy() {
    echo "PROXY $PROXY_HOST:$PROXY_PORT ($TEST_URL): continuous mode"
    echo ""
    
    while true; do
        total=$((total + 1))
        
        # 使用 HTTP 代理发送请求
        start_time=$(get_ms_time)
        response=$(curl -x "http://${PROXY_HOST}:${PROXY_PORT}" \
						--config "$CURL_AUTH_CONFIG" \
                       -s \
                       -w "\n%{http_code}" \
                       --connect-timeout 10 \
                       --max-time 15 \
                       "${TEST_URL}" 2>&1)
        end_time=$(get_ms_time)
        elapsed=$((end_time - start_time))
        
        # 分离响应体和状态码
        http_code=$(echo "$response" | tail -n 1)
        body=$(echo "$response" | sed '$d')
        
        if [ "$http_code" = "200" ]; then
            exit_ip=$(echo "$body" | grep -o '"query":"[^"]*"' | cut -d'"' -f4)
            country_code=$(echo "$body" | grep -o '"countryCode":"[^"]*"' | cut -d'"' -f4)
            
            if [ -n "$exit_ip" ]; then
                flag=$(country_to_emoji "$country_code")
                echo "proxy from $flag $exit_ip: seq=$total time=${elapsed}ms"
                success=$((success + 1))
            else
                echo "proxy #$total: parse error"
                fail=$((fail + 1))
            fi
        else
            echo "proxy #$total: request failed (HTTP $http_code)"
            fail=$((fail + 1))
        fi
        
        sleep $DELAY
    done
}

# 主函数
require_proxy_auth
setup_curl_auth_config
test_http_proxy
