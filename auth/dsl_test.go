package auth

import (
	"reflect"
	"testing"
)

func TestParseUsername(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want ParsedUsername
	}{
		{
			name: "base only",
			raw:  "username",
			want: ParsedUsername{Base: "username"},
		},
		{
			name: "valid region normalizes case",
			raw:  "username-region-US",
			want: ParsedUsername{Base: "username", Region: "us"},
		},
		{
			name: "valid session",
			raw:  "username-session-xy_12-A",
			want: ParsedUsername{Base: "username", Session: "xy_12-A"},
		},
		{
			name: "valid region and session",
			raw:  "username-region-jp-session-abc123",
			want: ParsedUsername{Base: "username", Region: "jp", Session: "abc123"},
		},
		{
			name: "hyphens before dsl marker stay in base",
			raw:  "team-username-region-hk-session-s1",
			want: ParsedUsername{Base: "team-username", Region: "hk", Session: "s1"},
		},
		{
			name: "node pins entrance address",
			raw:  "username-node-1.2.3.4:7801",
			want: ParsedUsername{Base: "username", Node: "1.2.3.4:7801"},
		},
		{
			name: "node with region and session in canonical order",
			raw:  "username-region-us-node-1.2.3.4:7801-session-s1",
			want: ParsedUsername{Base: "username", Region: "us", Node: "1.2.3.4:7801", Session: "s1"},
		},
		{
			name: "node with hostname",
			raw:  "username-node-node-a.example.com:1080",
			want: ParsedUsername{Base: "username", Node: "node-a.example.com:1080"},
		},
		{
			name: "node key stable identity (base64url wire)",
			// 将 NodeKey "trojan:a.example.com:443:deadbeef" 编码为 base64url。
			raw:  "username-node-key-" + EncodeNodeKeyPin("trojan:a.example.com:443:deadbeef"),
			want: ParsedUsername{Base: "username", Node: "key-trojan:a.example.com:443:deadbeef"},
		},
		{
			name: "node key with region and session and marker-like hostname",
			// 主机名含 -session- 不得误切；wire 为 base64url，解析后还原原文。
			raw:  "username-region-us-node-key-" + EncodeNodeKeyPin("vless:cdn-session-01.example.com:443:abcd1234") + "-session-s1",
			want: ParsedUsername{Base: "username", Region: "us", Node: "key-vless:cdn-session-01.example.com:443:abcd1234", Session: "s1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseUsername(tt.raw)
			if err != nil {
				t.Fatalf("ParseUsername(%q) returned error: %v", tt.raw, err)
			}

			if got.Base != tt.want.Base || got.Region != tt.want.Region || got.Session != tt.want.Session || got.Node != tt.want.Node || !reflect.DeepEqual(got.Unlock, tt.want.Unlock) {
				t.Fatalf("ParseUsername(%q) = %#v, want %#v", tt.raw, got, tt.want)
			}
		})
	}
}

func TestParseUsernameRejectsMalformedDSL(t *testing.T) {
	longSession := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	tests := []struct {
		name string
		raw  string
	}{
		{name: "empty username", raw: ""},
		{name: "empty base before region", raw: "-region-us"},
		{name: "empty base before session", raw: "-session-x"},
		{name: "invalid region length", raw: "username-region-usa"},
		{name: "invalid region characters", raw: "username-region-u1"},
		{name: "missing region value", raw: "username-region-"},
		{name: "missing session value", raw: "username-session-"},
		{name: "invalid session character", raw: "username-session-abc.def"},
		{name: "session too long", raw: "username-session-" + longSession},
		{name: "session before region is invalid", raw: "username-session-x-region-us"},
		{name: "duplicate region is invalid", raw: "username-region-us-region-jp"},
		{name: "duplicate session is invalid", raw: "username-session-x-session-y"},
		{name: "unknown suffix after region is invalid", raw: "username-region-us-extra-x"},
		{name: "missing node value", raw: "username-node-"},
		{name: "node without port", raw: "username-node-1.2.3.4"},
		{name: "node with non-numeric port", raw: "username-node-1.2.3.4:abc"},
		{name: "node before unlock is invalid", raw: "username-node-1.2.3.4:8080-unlock-gpt"},
		{name: "duplicate node is invalid", raw: "username-node-1.2.3.4:8080-node-5.6.7.8:1080"},
		{name: "empty node key", raw: "username-node-key-"},
		{name: "node key with illegal wire char", raw: "username-node-key-bad/key"},
		{name: "node key invalid base64url payload", raw: "username-node-key-@@@@"},
		{name: "node key with invalid base64url length", raw: "username-node-key-A"},
		{name: "node key with forbidden padding", raw: "username-node-key-Zm8="},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := ParseUsername(tt.raw); err == nil {
				t.Fatalf("ParseUsername(%q) returned nil error", tt.raw)
			}
		})
	}
}
