package webui

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestAPISubscriptionDeleteRemovesManagedSubscriptionFile(t *testing.T) {
	server := newTestServer(t)
	dataDir := t.TempDir()
	t.Setenv("DATA_DIR", dataDir)
	filePath := filepath.Join(dataDir, "subscriptions", "sub_delete.yaml")
	if err := os.MkdirAll(filepath.Dir(filePath), 0700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filePath, []byte("credential-bearing subscription"), 0600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	subID, err := server.storage.AddSubscription("managed-file", "", filePath, "auto", 60, "")
	if err != nil {
		t.Fatalf("AddSubscription() error = %v", err)
	}

	rec := httptest.NewRecorder()
	server.routes().ServeHTTP(rec, authenticatedJSONRequest(http.MethodPost, "/api/subscription/delete", fmt.Sprintf("{\"id\":%d}", subID)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if _, err := os.Stat(filePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("managed subscription file remains after delete: %v", err)
	}
}

func TestAPISubscriptionDeleteRetainsManagedFileWhenDatabaseDeleteFails(t *testing.T) {
	server := newTestServer(t)
	dataDir := t.TempDir()
	t.Setenv("DATA_DIR", dataDir)
	filePath := filepath.Join(dataDir, "subscriptions", "sub_rollback.yaml")
	if err := os.MkdirAll(filepath.Dir(filePath), 0700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filePath, []byte("must remain on rollback"), 0600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	subID, err := server.storage.AddSubscription("rollback-file", "", filePath, "auto", 60, "")
	if err != nil {
		t.Fatalf("AddSubscription() error = %v", err)
	}
	trigger := fmt.Sprintf("CREATE TRIGGER fail_managed_file_subscription_delete BEFORE DELETE ON subscriptions WHEN OLD.id = %d BEGIN SELECT RAISE(ABORT, 'forced file subscription delete failure'); END", subID)
	if _, err := server.storage.GetDB().Exec(trigger); err != nil {
		t.Fatalf("create trigger: %v", err)
	}

	rec := httptest.NewRecorder()
	server.routes().ServeHTTP(rec, authenticatedJSONRequest(http.MethodPost, "/api/subscription/delete", fmt.Sprintf("{\"id\":%d}", subID)))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusInternalServerError, rec.Body.String())
	}
	if _, err := os.Stat(filePath); err != nil {
		t.Fatalf("managed file removed despite database rollback: %v", err)
	}
}

func TestAPISubscriptionDeleteDoesNotRemoveExternalFilePath(t *testing.T) {
	server := newTestServer(t)
	t.Setenv("DATA_DIR", t.TempDir())
	externalPath := filepath.Join(t.TempDir(), "not-managed.yaml")
	if err := os.WriteFile(externalPath, []byte("must not be removed"), 0600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	subID, err := server.storage.AddSubscription("external-file", "", externalPath, "auto", 60, "")
	if err != nil {
		t.Fatalf("AddSubscription() error = %v", err)
	}

	rec := httptest.NewRecorder()
	server.routes().ServeHTTP(rec, authenticatedJSONRequest(http.MethodPost, "/api/subscription/delete", fmt.Sprintf("{\"id\":%d}", subID)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if _, err := os.Stat(externalPath); err != nil {
		t.Fatalf("external subscription path was removed: %v", err)
	}
}
