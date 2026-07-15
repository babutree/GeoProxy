package auth

import (
	"reflect"
	"testing"
)

func TestParseUsernameUnlockFilters(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want ParsedUsername
	}{
		{
			name: "gpt only",
			raw:  "acct-unlock-gpt",
			want: ParsedUsername{Base: "acct", Unlock: []string{"openai"}},
		},
		{
			name: "openai alias",
			raw:  "acct-unlock-openai",
			want: ParsedUsername{Base: "acct", Unlock: []string{"openai"}},
		},
		{
			name: "claude",
			raw:  "acct-unlock-claude",
			want: ParsedUsername{Base: "acct", Unlock: []string{"claude"}},
		},
		{
			name: "cf only",
			raw:  "acct-unlock-cf",
			want: ParsedUsername{Base: "acct", Unlock: []string{"cf"}},
		},
		{
			name: "all expands to five requirements",
			raw:  "acct-unlock-all",
			want: ParsedUsername{Base: "acct", Unlock: []string{"openai", "claude", "grok", "gemini", "cf"}},
		},
		{
			name: "region then unlock then session",
			raw:  "acct-region-us-unlock-gpt-session-abc",
			want: ParsedUsername{Base: "acct", Region: "us", Session: "abc", Unlock: []string{"openai"}},
		},
		{
			name: "region unlock all",
			raw:  "acct-region-jp-unlock-all",
			want: ParsedUsername{Base: "acct", Region: "jp", Unlock: []string{"openai", "claude", "grok", "gemini", "cf"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseUsername(tt.raw)
			if err != nil {
				t.Fatalf("ParseUsername(%q) error = %v", tt.raw, err)
			}
			if got.Base != tt.want.Base || got.Region != tt.want.Region || got.Session != tt.want.Session {
				t.Fatalf("ParseUsername(%q) = %#v, want %#v", tt.raw, got, tt.want)
			}
			if !reflect.DeepEqual(got.Unlock, tt.want.Unlock) {
				t.Fatalf("Unlock = %#v, want %#v", got.Unlock, tt.want.Unlock)
			}
		})
	}
}

func TestParseUsernameRejectsInvalidUnlock(t *testing.T) {
	for _, raw := range []string{
		"acct-unlock-",
		"acct-unlock-netflix",
		"acct-unlock-gpt-unlock-cf", // 单次仅允许一个 unlock 段
		"acct-session-x-unlock-gpt", // 顺序：unlock 必须在 session 之前
	} {
		if _, err := ParseUsername(raw); err == nil {
			t.Fatalf("ParseUsername(%q) expected error", raw)
		}
	}
}
