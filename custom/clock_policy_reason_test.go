package custom

import (
	"testing"
	"time"

	"github.com/babutree/GeoProxy/storage"
	"github.com/babutree/GeoProxy/validator"
)

type staticProbeResultValidator struct {
	result validator.Result
}

func (v staticProbeResultValidator) ValidateOne(storage.Proxy) (bool, time.Duration, string, string, validator.RiskInfo) {
	return v.result.Valid, v.result.Latency, v.result.ExitIP, v.result.ExitLocation, v.result.Risk
}

func (v staticProbeResultValidator) ValidateOneResult(proxy storage.Proxy) validator.Result {
	result := v.result
	result.Proxy = proxy
	return result
}

func (v staticProbeResultValidator) ValidateStream(proxies []storage.Proxy) <-chan validator.Result {
	results := make(chan validator.Result, len(proxies))
	for _, proxy := range proxies {
		result := v.result
		result.Proxy = proxy
		results <- result
	}
	close(results)
	return results
}

func TestValidateCustomProxiesGeoRejectionDoesNotStartSystemDisableClock(t *testing.T) {
	store := newTestStorage(t)
	subID, err := store.AddSubscription("geo-reason", "", writeSubscriptionFile(t, "proxies: []"), "auto", 60, "")
	if err != nil {
		t.Fatalf("AddSubscription: %v", err)
	}
	const address = "geo-reason.example:8080"
	if err := store.AddProxyWithSource(address, "socks5", storage.SourceSubscription, subID); err != nil {
		t.Fatalf("AddProxyWithSource: %v", err)
	}
	proxy, err := store.GetProxyByIdentity(address, storage.SourceSubscription, subID)
	if err != nil {
		t.Fatalf("GetProxyByIdentity(before): %v", err)
	}
	m := &Manager{
		storage: store,
		validator: staticProbeResultValidator{result: validator.Result{
			Valid:         false,
			Latency:       45 * time.Millisecond,
			ExitIP:        "198.51.100.44",
			ExitLocation:  "JP Tokyo",
			Risk:          validator.UnknownRisk(),
			FailureReason: validator.FailureGeoRejected,
		}},
	}

	if got := m.validateCustomProxies([]storage.Proxy{*proxy}, subID); got != 0 {
		t.Fatalf("valid count = %d, want 0", got)
	}
	after, err := store.GetProxyByIdentity(address, storage.SourceSubscription, subID)
	if err != nil {
		t.Fatalf("GetProxyByIdentity(after): %v", err)
	}
	if after.Status != "disabled" || after.ExitIP != "198.51.100.44" || after.ExitLocation != "JP Tokyo" {
		t.Fatalf("policy-rejected proxy = %#v, want disabled with trusted exit metadata", after)
	}
	if !after.DisabledAt.IsZero() {
		t.Fatalf("geo rejection started system disable clock: %v", after.DisabledAt)
	}
}
