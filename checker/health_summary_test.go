package checker

import (
	"errors"
	"testing"

	"github.com/babutree/GeoProxy/config"
	"github.com/babutree/GeoProxy/storage"
	"github.com/babutree/GeoProxy/validator"
)

// 地域拒绝是「策略禁用」，与连续失败触发的「系统禁用」语义不同：
// 前者不启动禁用回收时钟，后者启动。汇总必须分开计数——否则一批被地理策略
// 拒绝的节点只会体现在 updated 里，日志中的「禁用N」显示为 0，
// 既掩盖策略变更的实际影响规模，也让运维无法从日志判断这批节点为何消失。
func TestCheckBatchCountsPolicyDisabledSeparately(t *testing.T) {
	proxies := []storage.Proxy{
		{ID: 1, Address: "geo-1.example:8080", Protocol: "http"},
		{ID: 2, Address: "geo-2.example:8080", Protocol: "http"},
		{ID: 3, Address: "broken.example:8080", Protocol: "http"},
	}
	store := &countingStore{batch: proxies, failureDisabled: true}
	v := resultValidator{results: []validator.Result{
		{Proxy: proxies[0], FailureReason: validator.FailureGeoRejected},
		{Proxy: proxies[1], FailureReason: validator.FailureGeoRejected},
		{Proxy: proxies[2], FailureReason: validator.FailureConnectivity},
	}}
	cfg := &config.Config{HealthCheckBatchSize: 10, HealthIntervalMinutes: 1}
	hc := newHealthCheckerForTest(store, v, cfg)

	summary := hc.checkBatch(proxies)

	if summary.policyDisabled != 2 {
		t.Fatalf("policyDisabled = %d, want 2 (geo-rejected nodes must be counted separately)", summary.policyDisabled)
	}
	if summary.disabled != 1 {
		t.Fatalf("disabled = %d, want 1 (only authoritative failure-threshold disable)", summary.disabled)
	}
	if got := store.policyCalls.Load(); got != 2 {
		t.Fatalf("DisableRouteForPolicy calls = %d, want 2", got)
	}
	if summary.valid != 0 {
		t.Fatalf("valid = %d, want 0", summary.valid)
	}
}

// 策略禁用写库失败时不得计数：汇总只反映已提交的存储状态，
// 禁止把失败的写入伪装成已生效的策略变更。
func TestCheckBatchSkipsPolicyCountWhenDisableFails(t *testing.T) {
	proxies := []storage.Proxy{{ID: 1, Address: "geo.example:8080", Protocol: "http"}}
	store := &policyFailingStore{countingStore: countingStore{batch: proxies}}
	v := resultValidator{results: []validator.Result{
		{Proxy: proxies[0], FailureReason: validator.FailureGeoRejected},
	}}
	cfg := &config.Config{HealthCheckBatchSize: 10, HealthIntervalMinutes: 1}
	hc := newHealthCheckerForTest(store, v, cfg)

	summary := hc.checkBatch(proxies)

	if summary.policyDisabled != 0 {
		t.Fatalf("policyDisabled = %d, want 0 when DisableRouteForPolicy fails", summary.policyDisabled)
	}
	if summary.updated != 0 {
		t.Fatalf("updated = %d, want 0 when policy disable fails before write-back", summary.updated)
	}
}

type policyFailingStore struct {
	countingStore
}

func (s *policyFailingStore) DisableRouteForPolicy(storage.RouteIdentity) error {
	s.policyCalls.Add(1)
	return errPolicyDisable
}

var errPolicyDisable = errors.New("policy disable failed")
