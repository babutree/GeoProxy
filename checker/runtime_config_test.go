package checker

import (
	"testing"

	"github.com/babutree/GeoProxy/config"
	"github.com/babutree/GeoProxy/storage"
)

type batchSizeRecordingStore struct {
	batchSize int
}

func (s *batchSizeRecordingStore) GetBatchForHealthCheck(batchSize int) ([]storage.Proxy, error) {
	s.batchSize = batchSize
	return nil, nil
}

func (*batchSizeRecordingStore) UpdateProxyExitInfo(int64, string, string, int, float64, string, bool, int, string) error {
	return nil
}

func (*batchSizeRecordingStore) RecordProxyUseByID(int64, bool) error { return nil }

func (*batchSizeRecordingStore) RecordProxyFailureByIDWithStatus(int64, int) (bool, error) {
	return false, nil
}

func TestRunOnceUsesCurrentConfigSnapshot(t *testing.T) {
	previous := config.Get()
	t.Cleanup(func() { config.SetGlobal(previous) })
	live := &config.Config{HealthCheckBatchSize: 7}
	config.SetGlobal(live)

	store := &batchSizeRecordingStore{}
	healthChecker := newHealthCheckerForTest(
		store,
		resultValidator{},
		&config.Config{HealthCheckBatchSize: 1},
	)
	healthChecker.RunOnce()

	if store.batchSize != live.HealthCheckBatchSize {
		t.Fatalf("GetBatchForHealthCheck(%d), want current config value %d", store.batchSize, live.HealthCheckBatchSize)
	}
}

func (*batchSizeRecordingStore) ApplyProbeObservation(storage.RouteIdentity, storage.ExitObservation) error {
	return nil
}

func (*batchSizeRecordingStore) RecordProbeFailure(storage.RouteIdentity, storage.ExitObservation, int) (bool, error) {
	return false, nil
}
func (*batchSizeRecordingStore) DisableRouteForPolicy(storage.RouteIdentity) error {
	return nil
}
