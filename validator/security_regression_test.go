package validator

import (
	"net/http"
	"testing"
	"time"

	"github.com/babutree/GeoProxy/config"
)

func TestExitInfoRejectsPlaintextPrimaryWithoutHTTPSConfirmation(t *testing.T) {
	client := &http.Client{
		Transport: exitInfoRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			switch req.URL.String() {
			case primaryExitInfoURL:
				return exitInfoHTTPResponse(req, http.StatusOK, "{\"status\":\"success\",\"query\":\"203.0.113.45\",\"countryCode\":\"GB\",\"city\":\"London\"}"), nil
			case backupExitInfoURL:
				return exitInfoHTTPResponse(req, http.StatusBadGateway, "{}"), nil
			default:
				return exitInfoHTTPResponse(req, http.StatusNotFound, "{}"), nil
			}
		}),
		Timeout: time.Second,
	}

	if got := getExitIPInfo(client); got.OK {
		t.Fatalf("getExitIPInfo() trusted plaintext-only location: %#v", got)
	}
}

func TestValidatorClientsDisableIdleConnectionsAndUseContextDial(t *testing.T) {
	httpClient, err := newHTTPClient("127.0.0.1:8080", "", "", time.Second)
	if err != nil {
		t.Fatalf("newHTTPClient(): %v", err)
	}
	httpTransport := httpClient.Transport.(*http.Transport)
	if !httpTransport.DisableKeepAlives {
		t.Fatal("HTTP validator transport retains idle connections")
	}

	socksClient, err := newSOCKS5Client("127.0.0.1:1080", "", "", time.Second)
	if err != nil {
		t.Fatalf("newSOCKS5Client(): %v", err)
	}
	socksTransport := socksClient.Transport.(*http.Transport)
	if !socksTransport.DisableKeepAlives {
		t.Fatal("SOCKS5 validator transport retains idle connections")
	}
	if socksTransport.DialContext == nil || socksTransport.Dial != nil {
		t.Fatal("SOCKS5 validator transport does not propagate request context to dialing")
	}
}

func TestNewWithConfigUsesOneExplicitSnapshot(t *testing.T) {
	previous := config.Get()
	t.Cleanup(func() { config.SetGlobal(previous) })
	config.SetGlobal(&config.Config{
		ValidateConcurrency: 99,
		ValidateTimeout:     99,
		ValidateURL:         "https://global.invalid",
		MaxResponseMs:       9999,
		AllowedCountries:    []string{"US"},
	})

	snapshot := &config.Config{
		ValidateConcurrency: 3,
		ValidateTimeout:     7,
		ValidateURL:         "https://snapshot.invalid/204",
		MaxResponseMs:       321,
		AllowedCountries:    []string{"GB"},
	}
	got := NewWithConfig(snapshot)

	if got.concurrency != 3 || got.timeout != 7*time.Second || got.validateURL != snapshot.ValidateURL {
		t.Fatalf("validator runtime fields = concurrency:%d timeout:%v url:%q, want explicit snapshot", got.concurrency, got.timeout, got.validateURL)
	}
	if got.maxResponseMs != 321 || got.cfg != snapshot {
		t.Fatalf("validator policy snapshot = max:%d cfg:%p, want max:321 cfg:%p", got.maxResponseMs, got.cfg, snapshot)
	}
}
