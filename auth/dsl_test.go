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
			raw:  "acct",
			want: ParsedUsername{Base: "acct"},
		},
		{
			name: "valid region normalizes case",
			raw:  "acct-region-US",
			want: ParsedUsername{Base: "acct", Region: "us"},
		},
		{
			name: "valid session",
			raw:  "acct-session-xy_12-A",
			want: ParsedUsername{Base: "acct", Session: "xy_12-A"},
		},
		{
			name: "valid region and session",
			raw:  "acct-region-jp-session-abc123",
			want: ParsedUsername{Base: "acct", Region: "jp", Session: "abc123"},
		},
		{
			name: "hyphens before dsl marker stay in base",
			raw:  "team-acct-region-hk-session-s1",
			want: ParsedUsername{Base: "team-acct", Region: "hk", Session: "s1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseUsername(tt.raw)
			if err != nil {
				t.Fatalf("ParseUsername(%q) returned error: %v", tt.raw, err)
			}

			if got.Base != tt.want.Base || got.Region != tt.want.Region || got.Session != tt.want.Session || !reflect.DeepEqual(got.Unlock, tt.want.Unlock) {
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
		{name: "invalid region length", raw: "acct-region-usa"},
		{name: "invalid region characters", raw: "acct-region-u1"},
		{name: "missing region value", raw: "acct-region-"},
		{name: "missing session value", raw: "acct-session-"},
		{name: "invalid session character", raw: "acct-session-abc.def"},
		{name: "session too long", raw: "acct-session-" + longSession},
		{name: "session before region is invalid", raw: "acct-session-x-region-us"},
		{name: "duplicate region is invalid", raw: "acct-region-us-region-jp"},
		{name: "duplicate session is invalid", raw: "acct-session-x-session-y"},
		{name: "unknown suffix after region is invalid", raw: "acct-region-us-extra-x"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := ParseUsername(tt.raw); err == nil {
				t.Fatalf("ParseUsername(%q) returned nil error", tt.raw)
			}
		})
	}
}
