package webui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"goproxy/config"
)

func TestV1OccupancyRequiresAPIKey(t *testing.T) {
	plain := "occ-readonly-key"
	server := newReadOnlyAPITestServer(t, []config.APIKey{{
		ID:   "k-occ",
		Name: "occ",
		Hash: testAPIKeyHash(plain),
	}}, 60)
	if err := server.storage.AddManualProxy("203.0.113.70:8080", "http", "us", ""); err != nil {
		t.Fatalf("AddManualProxy() error = %v", err)
	}
	proxy, err := server.storage.GetProxyByAddress("203.0.113.70:8080")
	if err != nil {
		t.Fatalf("GetProxyByAddress() error = %v", err)
	}
	server.affinity.SetProxy("v1-occ-unauth", proxy.ID, proxy.Address, "us")

	cases := []struct {
		name   string
		header func(*http.Request)
	}{
		{name: "missing", header: func(*http.Request) {}},
		{name: "bad_bearer", header: func(r *http.Request) {
			r.Header.Set("Authorization", "Bearer wrong-key")
		}},
		{name: "bad_x_api_key", header: func(r *http.Request) {
			r.Header.Set("X-API-Key", "wrong-key")
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/occupancy", nil)
			tc.header(req)
			rec := httptest.NewRecorder()
			server.routes().ServeHTTP(rec, req)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusUnauthorized, rec.Body.String())
			}
		})
	}
}

func TestV1OccupancyReturnsRealCooldownWithoutPassword(t *testing.T) {
	plain := "occ-cooldown-key"
	server := newReadOnlyAPITestServer(t, []config.APIKey{{
		ID:   "k-occ-cd",
		Name: "occ-cd",
		Hash: testAPIKeyHash(plain),
	}}, 60)
	server.cfg.MaxSessionsPerProxy = 2
	if err := server.storage.AddManualProxy("203.0.113.71:8080", "http", "us", ""); err != nil {
		t.Fatalf("AddManualProxy() error = %v", err)
	}
	proxy, err := server.storage.GetProxyByAddress("203.0.113.71:8080")
	if err != nil {
		t.Fatalf("GetProxyByAddress() error = %v", err)
	}
	server.affinity.SetProxy("v1-occ-a", proxy.ID, proxy.Address, "us")
	server.affinity.SetCooldown(proxy.ID, time.Now().Add(120*time.Second))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/occupancy", nil)
	req.Header.Set("Authorization", "Bearer "+plain)
	rec := httptest.NewRecorder()
	server.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	body := rec.Body.String()
	for _, bad := range []string{`"password"`, `"proxy_auth_password"`, `"username"`, `"user"`, `"pass"`} {
		if strings.Contains(body, bad) {
			t.Fatalf("occupancy response must not contain credential field %s: %s", bad, body)
		}
	}

	type occupancyRow struct {
		ProxyID                  int64  `json:"proxy_id"`
		Address                  string `json:"address"`
		ActiveSessions           int    `json:"active_sessions"`
		MaxSessions              int    `json:"max_sessions"`
		CooldownRemainingSeconds int64  `json:"cooldown_remaining_seconds"`
	}
	var rows []occupancyRow
	if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
		t.Fatalf("decode occupancy response: %v; body=%s", err, body)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1: %#v", len(rows), rows)
	}
	row := rows[0]
	if row.ProxyID != proxy.ID {
		t.Fatalf("proxy_id = %d, want %d", row.ProxyID, proxy.ID)
	}
	if row.Address != proxy.Address {
		t.Fatalf("address = %q, want %q", row.Address, proxy.Address)
	}
	if row.ActiveSessions != 1 {
		t.Fatalf("active_sessions = %d, want 1", row.ActiveSessions)
	}
	if row.MaxSessions != 2 {
		t.Fatalf("max_sessions = %d, want 2", row.MaxSessions)
	}
	if row.CooldownRemainingSeconds <= 0 || row.CooldownRemainingSeconds > 120 {
		t.Fatalf("cooldown_remaining_seconds = %d, want in (0,120]", row.CooldownRemainingSeconds)
	}
}

func TestV1OccupancyNilAffinityReturnsEmptyArray(t *testing.T) {
	plain := "occ-nil-affinity-key"
	server := newReadOnlyAPITestServer(t, []config.APIKey{{
		ID:   "k-occ-nil",
		Hash: testAPIKeyHash(plain),
	}}, 60)
	server.affinity = nil

	req := httptest.NewRequest(http.MethodGet, "/api/v1/occupancy", nil)
	req.Header.Set("X-API-Key", plain)
	rec := httptest.NewRecorder()
	server.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var rows []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
		t.Fatalf("decode occupancy response: %v; body=%s", err, rec.Body.String())
	}
	if len(rows) != 0 {
		t.Fatalf("len(rows) = %d, want 0: %#v", len(rows), rows)
	}
}

func TestV1OccupancyHidesLoopbackAddress(t *testing.T) {
	plain := "occ-loopback-key"
	server := newReadOnlyAPITestServer(t, []config.APIKey{{
		ID:   "k-occ-lb",
		Hash: testAPIKeyHash(plain),
	}}, 60)
	server.affinity.SetProxy("v1-occ-lb", 99, "127.0.0.1:1080", "local")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/occupancy", nil)
	req.Header.Set("Authorization", "Bearer "+plain)
	rec := httptest.NewRecorder()
	server.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, "127.0.0.1") {
		t.Fatalf("external occupancy must not expose loopback address: %s", body)
	}
	type occupancyRow struct {
		ProxyID int64  `json:"proxy_id"`
		Address string `json:"address"`
		Note    string `json:"note"`
	}
	var rows []occupancyRow
	if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
		t.Fatalf("decode occupancy response: %v; body=%s", err, body)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1: %#v", len(rows), rows)
	}
	if rows[0].Address != "" && rows[0].Address != "gateway-local" {
		t.Fatalf("address = %q, want empty or gateway-local", rows[0].Address)
	}
}

// occupancyRowForMasking is a local decode target for the masking tests below.
type occupancyRowForMasking struct {
	ProxyID int64  `json:"proxy_id"`
	Address string `json:"address"`
	Note    string `json:"note"`
}

// TestV1OccupancyHidesPrivateAndInternalAddresses verifies that
// the read-only /api/v1/occupancy endpoint must not leak private / internal
// bind addresses (RFC1918, CGNAT, link-local, IPv6 ULA / link-local / loopback)
// to external API-key callers. Each address must be redacted to "gateway-local"
// with a note, and the raw host must never appear anywhere in the response body.
func TestV1OccupancyHidesPrivateAndInternalAddresses(t *testing.T) {
	cases := []struct {
		name    string
		address string
		// hostFragment is the sensitive substring that must NOT appear in the body.
		hostFragment string
	}{
		{name: "rfc1918_10", address: "10.0.0.5:1080", hostFragment: "10.0.0.5"},
		{name: "rfc1918_172", address: "172.16.4.9:1080", hostFragment: "172.16.4.9"},
		{name: "rfc1918_192", address: "192.168.1.100:1080", hostFragment: "192.168.1.100"},
		{name: "cgnat_100_64", address: "100.64.3.7:1080", hostFragment: "100.64.3.7"},
		{name: "linklocal_169_254", address: "169.254.10.20:1080", hostFragment: "169.254.10.20"},
		{name: "ipv6_loopback", address: "[::1]:1080", hostFragment: "::1"},
		{name: "ipv6_ula_fc00", address: "[fc00::1]:1080", hostFragment: "fc00::1"},
		{name: "ipv6_ula_fd12", address: "[fd12:3456:789a::1]:1080", hostFragment: "fd12:3456:789a::1"},
		{name: "ipv6_linklocal_fe80", address: "[fe80::1]:1080", hostFragment: "fe80::1"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plain := "occ-priv-key-" + tc.name
			server := newReadOnlyAPITestServer(t, []config.APIKey{{
				ID:   "k-occ-priv-" + tc.name,
				Hash: testAPIKeyHash(plain),
			}}, 60)
			server.affinity.SetProxy("v1-occ-priv-"+tc.name, 501, tc.address, "local")

			req := httptest.NewRequest(http.MethodGet, "/api/v1/occupancy", nil)
			req.Header.Set("Authorization", "Bearer "+plain)
			rec := httptest.NewRecorder()
			server.routes().ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
			}
			body := rec.Body.String()
			if strings.Contains(body, tc.hostFragment) {
				t.Fatalf("external occupancy must not expose private/internal address %q: %s", tc.hostFragment, body)
			}

			var rows []occupancyRowForMasking
			if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
				t.Fatalf("decode occupancy response: %v; body=%s", err, body)
			}
			if len(rows) != 1 {
				t.Fatalf("len(rows) = %d, want 1: %#v", len(rows), rows)
			}
			if rows[0].Address != "" && rows[0].Address != "gateway-local" {
				t.Fatalf("address = %q, want empty or gateway-local", rows[0].Address)
			}
			if rows[0].Note == "" {
				t.Fatalf("expected a redaction note for masked address, got empty; row=%#v", rows[0])
			}
		})
	}
}

// TestV1OccupancyShowsPublicAddress is the positive (non-masked) case: a public
// bind address must remain visible to external callers unchanged.
func TestV1OccupancyShowsPublicAddress(t *testing.T) {
	plain := "occ-public-key"
	server := newReadOnlyAPITestServer(t, []config.APIKey{{
		ID:   "k-occ-pub",
		Hash: testAPIKeyHash(plain),
	}}, 60)
	const publicAddr = "203.0.113.90:8080"
	server.affinity.SetProxy("v1-occ-pub", 601, publicAddr, "us")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/occupancy", nil)
	req.Header.Set("Authorization", "Bearer "+plain)
	rec := httptest.NewRecorder()
	server.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var rows []occupancyRowForMasking
	if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
		t.Fatalf("decode occupancy response: %v; body=%s", err, rec.Body.String())
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1: %#v", len(rows), rows)
	}
	if rows[0].Address != publicAddr {
		t.Fatalf("public address = %q, want %q (public addresses must not be masked)", rows[0].Address, publicAddr)
	}
	if rows[0].Note != "" {
		t.Fatalf("public address must not carry a redaction note, got %q", rows[0].Note)
	}
}

// TestAdminOccupancyStillExposesPrivateAddress guards the endpoint split:
// the admin endpoint (/api/proxy-occupancy) intentionally shows the real bind
// address. Masking must be applied only to the read-only /api/v1/occupancy path,
// so the admin path must remain UNCHANGED and continue to reveal RFC1918/IPv6 ULA.
func TestAdminOccupancyStillExposesPrivateAddress(t *testing.T) {
	server := newTestServer(t)
	server.cfg.MaxSessionsPerProxy = 3
	server.affinity.SetProxy("admin-priv-v4", 701, "10.1.2.3:1080", "local")
	server.affinity.SetProxy("admin-priv-v6", 702, "[fc00::9]:1080", "local")

	req := authenticatedJSONRequest(http.MethodGet, "/api/proxy-occupancy", "")
	rec := httptest.NewRecorder()
	server.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "10.1.2.3") {
		t.Fatalf("admin occupancy must still expose real RFC1918 address 10.1.2.3: %s", body)
	}
	if !strings.Contains(body, "fc00::9") {
		t.Fatalf("admin occupancy must still expose real IPv6 ULA address fc00::9: %s", body)
	}
	if strings.Contains(body, "gateway-local") {
		t.Fatalf("admin occupancy must NOT be redacted with gateway-local: %s", body)
	}
}
