#!/usr/bin/env bash
set -euo pipefail

# GeoProxy SOCKS5 代理测试脚本
# 用法: GEOPROXY_AUTH_USERNAME=username GEOPROXY_AUTH_PASSWORD=... ./test_socks5.sh [端口号，默认7801] [测试轮数，默认持续运行]
# 可选: GEOPROXY_AUTH_REGION=us GEOPROXY_AUTH_SESSION=browser
# 可选: GEOPROXY_PROBE_URLS=$'https://target-a.example/\nhttps://target-b.example/'

PROXY_HOST="${PROXY_HOST:-127.0.0.1}"
PROXY_PORT="${1:-7801}"
MAX_COUNT="${2:-0}" # 0 = 持续运行
DELAY="${GEOPROXY_PROBE_DELAY:-1}"
CONNECT_TIMEOUT="${GEOPROXY_PROBE_CONNECT_TIMEOUT:-5}"
REQUEST_TIMEOUT="${GEOPROXY_PROBE_TIMEOUT:-10}"

DEFAULT_TARGETS=(
    "https://api.ipify.org/"
    "https://checkip.amazonaws.com/"
)
TARGETS=()
CURL_AUTH_CONFIG=""

total_rounds=0
successful_rounds=0
failed_rounds=0
total_attempts=0

require_proxy_auth() {
    if [[ -z "${GEOPROXY_AUTH_USERNAME:-}" || -z "${GEOPROXY_AUTH_PASSWORD:-}" ]]; then
        echo "缺少代理认证信息。" >&2
        echo "请通过 GEOPROXY_AUTH_USERNAME 和 GEOPROXY_AUTH_PASSWORD 提供首次启动日志或 WebUI 设置中的认证信息。" >&2
        echo "可选路由参数: GEOPROXY_AUTH_REGION=us GEOPROXY_AUTH_SESSION=browser" >&2
        exit 2
    fi
}

require_non_negative_integer() {
    local name="$1"
    local value="$2"
    if [[ ! "$value" =~ ^[0-9]+$ ]]; then
        echo "${name} 必须是非负整数，实际为: ${value}" >&2
        exit 2
    fi
}

proxy_auth_username() {
    local username="$GEOPROXY_AUTH_USERNAME"
    if [[ -n "${GEOPROXY_AUTH_REGION:-}" ]]; then
        username="${username}-region-${GEOPROXY_AUTH_REGION}"
    fi
    if [[ -n "${GEOPROXY_AUTH_SESSION:-}" ]]; then
        username="${username}-session-${GEOPROXY_AUTH_SESSION}"
    fi
    printf '%s\n' "$username"
}

setup_curl_auth_config() {
    local old_umask
    old_umask=$(umask)
    umask 077
    CURL_AUTH_CONFIG=$(mktemp "${TMPDIR:-/tmp}/GeoProxy-curl-auth.XXXXXX")
    umask "$old_umask"
    printf 'proxy-user = "%s:%s"\n' "$(proxy_auth_username)" "$GEOPROXY_AUTH_PASSWORD" >"$CURL_AUTH_CONFIG"
}

cleanup() {
    if [[ -n "$CURL_AUTH_CONFIG" ]]; then
        rm -f -- "$CURL_AUTH_CONFIG"
    fi
}

load_targets() {
    local target
    if [[ -n "${GEOPROXY_PROBE_URLS:-}" ]]; then
        while IFS= read -r target || [[ -n "$target" ]]; do
            target="${target%$'\r'}"
            if [[ -z "$target" ]]; then
                continue
            fi
            TARGETS+=("$target")
        done <<<"$GEOPROXY_PROBE_URLS"
    else
        TARGETS=("${DEFAULT_TARGETS[@]}")
    fi

    if (( ${#TARGETS[@]} == 0 )); then
        echo "GEOPROXY_PROBE_URLS 未提供有效目标；每行必须包含一个 HTTPS URL。" >&2
        exit 2
    fi
    for target in "${TARGETS[@]}"; do
        if [[ "$target" != https://* ]]; then
            echo "探针目标必须使用 HTTPS: ${target}" >&2
            exit 2
        fi
    done
}

get_ms_time() {
    python3 -c 'import time; print(int(time.time() * 1000))'
}

is_success_status() {
    [[ "$1" =~ ^[23][0-9][0-9]$ ]]
}

probe_target() {
    local round_number="$1"
    local target="$2"
    local start_time end_time elapsed curl_output curl_exit http_code details

    start_time=$(get_ms_time)
    total_attempts=$((total_attempts + 1))
    if curl_output=$(curl --silent --show-error --insecure \
        --socks5-hostname "${PROXY_HOST}:${PROXY_PORT}" \
        --config "$CURL_AUTH_CONFIG" \
        --output /dev/null \
        --write-out $'\n%{http_code}' \
        --connect-timeout "$CONNECT_TIMEOUT" \
        --max-time "$REQUEST_TIMEOUT" \
        "$target" 2>&1); then
        curl_exit=0
    else
        curl_exit=$?
    fi
    end_time=$(get_ms_time)
    elapsed=$((end_time - start_time))

    http_code="${curl_output##*$'\n'}"
    details="${curl_output%$'\n'*}"
    details="${details//$'\n'/ }"

    if (( curl_exit != 0 )); then
        if [[ -z "$details" ]]; then
            details="无详细信息"
        fi
        echo "失败 round=${round_number} target=${target} 代理链或传输失败: curl=${curl_exit}, ${details} time=${elapsed}ms" >&2
        return 1
    fi
    if ! is_success_status "$http_code"; then
        echo "失败 round=${round_number} target=${target} 目标站失败: HTTP ${http_code} time=${elapsed}ms" >&2
        return 1
    fi

    echo "成功 round=${round_number} target=${target} HTTP ${http_code} time=${elapsed}ms"
    return 0
}

probe_round() {
    local round_number="$1"
    local attempt target_index target

    for (( attempt = 0; attempt < ${#TARGETS[@]}; attempt++ )); do
        target_index=$(( (round_number - 1 + attempt) % ${#TARGETS[@]} ))
        target="${TARGETS[$target_index]}"
        if probe_target "$round_number" "$target"; then
            return 0
        fi
    done

    echo "失败 round=${round_number} 全部目标失败 (${#TARGETS[@]} 个目标)" >&2
    return 1
}

print_summary() {
    echo "---"
    echo "${total_rounds} 轮探针，${successful_rounds} 轮成功，${failed_rounds} 轮全部目标失败，共 ${total_attempts} 次目标请求"
}

finish() {
    print_summary
    if (( failed_rounds > 0 )); then
        exit 1
    fi
    exit 0
}

handle_interrupt() {
    echo ""
    finish
}

trap cleanup EXIT
trap handle_interrupt INT
trap 'exit 143' TERM

require_non_negative_integer "测试轮数" "$MAX_COUNT"
require_proxy_auth
load_targets
setup_curl_auth_config

echo "SOCKS5 代理 ${PROXY_HOST}:${PROXY_PORT}: $([[ "$MAX_COUNT" -eq 0 ]] && echo '持续模式' || echo "${MAX_COUNT} 轮")"
echo "目标数: ${#TARGETS[@]}（每轮任一目标成功即成功）"
echo ""

while true; do
    total_rounds=$((total_rounds + 1))
    if probe_round "$total_rounds"; then
        successful_rounds=$((successful_rounds + 1))
    else
        failed_rounds=$((failed_rounds + 1))
    fi

    if (( MAX_COUNT > 0 && total_rounds >= MAX_COUNT )); then
        break
    fi
    sleep "$DELAY"
done

finish
