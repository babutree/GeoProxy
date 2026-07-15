#!/usr/bin/env bash

set -euo pipefail

for script in test/test_proxy.sh test/test_socks5.sh test/test_http_https.sh; do
    if grep -Fq -- "--proxy-user" "$script"; then
        echo "$script passes proxy credentials in curl arguments" >&2
        exit 1
    fi
    if ! grep -Fq -- "CURL_AUTH_CONFIG" "$script"; then
        echo "$script does not create a curl auth config" >&2
        exit 1
    fi
    if ! grep -Fq -- '--config "$CURL_AUTH_CONFIG"' "$script"; then
        echo "$script does not pass the curl auth config" >&2
        exit 1
    fi
done
