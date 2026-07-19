#!/usr/bin/env bash
set -euo pipefail

script_path="${1:-}"
scenario="${2:-}"

fail() {
    printf '行为测试失败: %s\n' "$*" >&2
    exit 1
}

if [[ -z "$script_path" || ! -f "$script_path" ]]; then
    fail "被测脚本不存在: ${script_path:-<empty>}"
fi

script_name="${script_path##*/}"
case "$script_name" in
    test_socks5.sh)
        proxy_port=7801
        expected_proxy_flag="--socks5-hostname"
        ;;
    test_http_https.sh)
        proxy_port=7802
        expected_proxy_flag="--proxy"
        ;;
    *)
        fail "不支持的被测脚本: $script_name"
        ;;
esac

work_dir=$(/usr/bin/mktemp -d "${TMPDIR:-/tmp}/geoproxy-probe-behavior.XXXXXX")
cleanup() {
    /bin/rm -rf -- "$work_dir"
}
trap cleanup EXIT

fake_bin="$work_dir/bin"
fake_tmp="$work_dir/tmp"
curl_log="$work_dir/fake-curl.log"
/bin/mkdir -p "$fake_bin" "$fake_tmp"
: >"$curl_log"

first_url="https://first.probe.invalid/status"
second_url="https://second.probe.invalid/status"
unknown_url="https://unknown.probe.invalid/status"

/bin/cat >"$fake_bin/curl" <<'FAKE_CURL'
#!/bin/bash
set -euo pipefail

url=""
saw_proxy_flag=0
for argument in "$@"; do
    if [[ "$argument" == "$FAKE_EXPECTED_PROXY_FLAG" ]]; then
        saw_proxy_flag=1
    fi
    case "$argument" in
        http://* | https://*) url="$argument" ;;
    esac
done

printf 'fake-curl\t%s\t%s\t%s\n' "$FAKE_CURL_SCENARIO" "$url" "$saw_proxy_flag" >>"$FAKE_CURL_LOG"

if (( saw_proxy_flag != 1 )); then
    printf 'fake curl 缺少代理参数 %s\n' "$FAKE_EXPECTED_PROXY_FLAG" >&2
    printf '\n000'
    exit 95
fi

emit_http() {
    printf '\n%s' "$1"
    exit 0
}

emit_transport() {
    local code="$1"
    printf 'curl: (%s) fake transport failure for %s\n' "$code" "$url" >&2
    printf '\n000'
    exit "$code"
}

case "$url" in
    "$FAKE_FIRST_URL") target_position="first" ;;
    "$FAKE_SECOND_URL") target_position="second" ;;
    *)
        printf 'fake curl 拒绝未知目标: %s\n' "${url:-<empty>}" >&2
        printf '\n000'
        exit 96
        ;;
esac

case "$FAKE_CURL_SCENARIO" in
    fallback_2xx)
        if [[ "$target_position" == "first" ]]; then
            emit_transport 28
        fi
        emit_http 204
        ;;
    fallback_3xx)
        if [[ "$target_position" == "first" ]]; then
            emit_transport 28
        fi
        emit_http 302
        ;;
    http_fail)
        if [[ "$target_position" == "first" ]]; then
            emit_http 404
        fi
        emit_http 503
        ;;
    transport_fail)
        if [[ "$target_position" == "first" ]]; then
            emit_transport 7
        fi
        emit_transport 28
        ;;
    *)
        printf 'fake curl 收到未知场景: %s\n' "$FAKE_CURL_SCENARIO" >&2
        printf '\n000'
        exit 94
        ;;
esac
FAKE_CURL

/bin/cat >"$fake_bin/python3" <<'FAKE_PYTHON'
#!/bin/bash
printf '1000\n'
FAKE_PYTHON

/bin/cat >"$fake_bin/mktemp" <<'FAKE_MKTEMP'
#!/bin/bash
set -euo pipefail
template="${1:?missing template}"
path="${template%XXXXXX}fixture"
: >"$path"
printf '%s\n' "$path"
FAKE_MKTEMP

/bin/cat >"$fake_bin/rm" <<'FAKE_RM'
#!/bin/bash
exec /bin/rm "$@"
FAKE_RM

/bin/cat >"$fake_bin/sleep" <<'FAKE_SLEEP'
#!/bin/bash
printf '有限轮数测试不应调用 sleep\n' >&2
exit 98
FAKE_SLEEP

/bin/chmod +x "$fake_bin/curl" "$fake_bin/python3" "$fake_bin/mktemp" "$fake_bin/rm" "$fake_bin/sleep"

targets="$first_url"$'\n'"$second_url"
round_count=1
expected_status=0
expected_calls=2
expected_urls=("$first_url" "$second_url")
required_fragments=()

case "$scenario" in
    fallback_2xx)
        required_fragments=("代理链或传输失败" "成功 round=1" "HTTP 204" "共 2 次目标请求")
        ;;
    fallback_3xx)
        required_fragments=("代理链或传输失败" "成功 round=1" "HTTP 302" "共 2 次目标请求")
        ;;
    http_fail)
        expected_status=1
        required_fragments=("目标站失败: HTTP 404" "目标站失败: HTTP 503" "全部目标失败")
        ;;
    transport_fail)
        expected_status=1
        required_fragments=("代理链或传输失败: curl=7" "代理链或传输失败: curl=28" "全部目标失败")
        ;;
    invalid_target)
        targets="http://invalid.probe.invalid/status"
        expected_status=2
        expected_calls=0
        expected_urls=()
        required_fragments=("探针目标必须使用 HTTPS")
        ;;
    invalid_count)
        round_count="not-a-number"
        expected_status=2
        expected_calls=0
        expected_urls=()
        required_fragments=("测试轮数 必须是非负整数")
        ;;
    unknown_target)
        targets="$unknown_url"
        expected_status=1
        expected_calls=1
        expected_urls=("$unknown_url")
        required_fragments=("代理链或传输失败: curl=96" "全部目标失败")
        ;;
    *)
        fail "未知行为测试场景: $scenario"
        ;;
esac

set +e
output=$(/usr/bin/env -i \
    PATH="$fake_bin" \
    TMPDIR="$fake_tmp" \
    GEOPROXY_AUTH_USERNAME="fixture-user" \
    GEOPROXY_AUTH_PASSWORD="fixture-password" \
    GEOPROXY_PROBE_URLS="$targets" \
    GEOPROXY_PROBE_DELAY=0 \
    GEOPROXY_PROBE_CONNECT_TIMEOUT=1 \
    GEOPROXY_PROBE_TIMEOUT=1 \
    FAKE_CURL_SCENARIO="$scenario" \
    FAKE_CURL_LOG="$curl_log" \
    FAKE_FIRST_URL="$first_url" \
    FAKE_SECOND_URL="$second_url" \
    FAKE_EXPECTED_PROXY_FLAG="$expected_proxy_flag" \
    /bin/bash "$script_path" "$proxy_port" "$round_count" 2>&1)
actual_status=$?
set -e

if (( actual_status != expected_status )); then
    printf '%s\n' "$output" >&2
    fail "$script_name/$scenario 退出码期望 $expected_status，实际 $actual_status"
fi

for fragment in "${required_fragments[@]}"; do
    if [[ "$output" != *"$fragment"* ]]; then
        printf '%s\n' "$output" >&2
        fail "$script_name/$scenario 缺少输出: $fragment"
    fi
done

logged_urls=()
while IFS=$'\t' read -r marker logged_scenario logged_url saw_proxy_flag; do
    if [[ -z "$marker" ]]; then
        continue
    fi
    if [[ "$marker" != "fake-curl" || "$logged_scenario" != "$scenario" ]]; then
        fail "fake curl 日志格式或场景错误: $marker/$logged_scenario"
    fi
    if [[ "$saw_proxy_flag" != "1" ]]; then
        fail "fake curl 未看到预期代理参数: $expected_proxy_flag"
    fi
    logged_urls+=("$logged_url")
done <"$curl_log"

if (( ${#logged_urls[@]} != expected_calls )); then
    fail "$script_name/$scenario fake curl 调用次数期望 $expected_calls，实际 ${#logged_urls[@]}"
fi
for (( index = 0; index < expected_calls; index++ )); do
    if [[ "${logged_urls[$index]}" != "${expected_urls[$index]}" ]]; then
        fail "$script_name/$scenario 第 $((index + 1)) 个目标期望 ${expected_urls[$index]}，实际 ${logged_urls[$index]}"
    fi
done

printf 'PASS %s %s fake-curl-calls=%d\n' "$script_name" "$scenario" "$expected_calls"
