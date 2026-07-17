package webui

import "testing"

func TestSessionRouteLabelSkipsUnknownAndEmpty(t *testing.T) {
	cases := []struct {
		region, sid, want string
	}{
		{"jp", "app01", "region-jp-session-app01"},
		{"US", "web-7f", "region-us-session-web-7f"},
		{"unknown", "x", "session-x"},
		{"", "only", "session-only"},
		{"  ", "sp", "session-sp"},
		{"jp", "", ""},
		{"unknown", "", ""},
	}
	for _, tc := range cases {
		got := sessionRouteLabel(tc.region, tc.sid)
		if got != tc.want {
			t.Fatalf("sessionRouteLabel(%q,%q)=%q, want %q", tc.region, tc.sid, got, tc.want)
		}
	}
}
