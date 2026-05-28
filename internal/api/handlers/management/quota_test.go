package management

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

func TestQuotaExceededAutoDisableConfigEndpoints(t *testing.T) {
	gin.SetMode(gin.TestMode)

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configPath, []byte("quota-exceeded:\n  auto-disable-auth-file-on-zero-quota: false\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg := &config.Config{}
	h := NewHandler(cfg, configPath, nil)

	recGet := httptest.NewRecorder()
	ctxGet, _ := gin.CreateTestContext(recGet)
	ctxGet.Request = httptest.NewRequest(http.MethodGet, "/v0/management/quota-exceeded/auto-disable-auth-file-on-zero-quota", nil)
	h.GetAutoDisableAuthFileOnZeroQuota(ctxGet)

	if recGet.Code != http.StatusOK {
		t.Fatalf("GET status = %d, body=%s", recGet.Code, recGet.Body.String())
	}

	recPut := httptest.NewRecorder()
	ctxPut, _ := gin.CreateTestContext(recPut)
	ctxPut.Request = httptest.NewRequest(http.MethodPut, "/v0/management/quota-exceeded/auto-disable-auth-file-on-zero-quota", bytes.NewReader([]byte(`{"value":true}`)))
	ctxPut.Request.Header.Set("Content-Type", "application/json")
	h.PutAutoDisableAuthFileOnZeroQuota(ctxPut)

	if recPut.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body=%s", recPut.Code, recPut.Body.String())
	}
	if !cfg.QuotaExceeded.AutoDisableAuthFileOnZeroQuota {
		t.Fatal("expected config to be updated to true")
	}

	var payload map[string]any
	if err := json.Unmarshal(recPut.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode PUT response: %v", err)
	}
	if payload["status"] != "ok" {
		t.Fatalf("unexpected PUT response %#v", payload)
	}
}

func TestQuotaExceededAutoDisableThresholdConfigEndpoints(t *testing.T) {
	gin.SetMode(gin.TestMode)

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configPath, []byte("quota-exceeded:\n  auto-disable-auth-file-quota-threshold-percent: 0\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg := &config.Config{}
	h := NewHandler(cfg, configPath, nil)

	recPut := httptest.NewRecorder()
	ctxPut, _ := gin.CreateTestContext(recPut)
	ctxPut.Request = httptest.NewRequest(http.MethodPut, "/v0/management/quota-exceeded/auto-disable-auth-file-quota-threshold-percent", bytes.NewReader([]byte(`{"value":99}`)))
	ctxPut.Request.Header.Set("Content-Type", "application/json")
	h.PutAutoDisableAuthFileQuotaThresholdPercent(ctxPut)

	if recPut.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body=%s", recPut.Code, recPut.Body.String())
	}
	if got := cfg.QuotaExceeded.AutoDisableAuthFileQuotaThresholdPercent; got != config.MaxAutoDisableQuotaThresholdPercent {
		t.Fatalf("threshold = %d, want %d", got, config.MaxAutoDisableQuotaThresholdPercent)
	}

	recGet := httptest.NewRecorder()
	ctxGet, _ := gin.CreateTestContext(recGet)
	ctxGet.Request = httptest.NewRequest(http.MethodGet, "/v0/management/quota-exceeded/auto-disable-auth-file-quota-threshold-percent", nil)
	h.GetAutoDisableAuthFileQuotaThresholdPercent(ctxGet)
	if recGet.Code != http.StatusOK {
		t.Fatalf("GET status = %d, body=%s", recGet.Code, recGet.Body.String())
	}
	var getPayload map[string]int
	if err := json.Unmarshal(recGet.Body.Bytes(), &getPayload); err != nil {
		t.Fatalf("decode GET response: %v", err)
	}
	if got := getPayload["auto-disable-auth-file-quota-threshold-percent"]; got != config.MaxAutoDisableQuotaThresholdPercent {
		t.Fatalf("GET threshold = %d, want %d", got, config.MaxAutoDisableQuotaThresholdPercent)
	}

	recPatch := httptest.NewRecorder()
	ctxPatch, _ := gin.CreateTestContext(recPatch)
	ctxPatch.Request = httptest.NewRequest(http.MethodPatch, "/v0/management/quota-exceeded/auto-disable-auth-file-quota-threshold-percent", bytes.NewReader([]byte(`{"value":-1}`)))
	ctxPatch.Request.Header.Set("Content-Type", "application/json")
	h.PatchAutoDisableAuthFileQuotaThresholdPercent(ctxPatch)
	if recPatch.Code != http.StatusOK {
		t.Fatalf("PATCH status = %d, body=%s", recPatch.Code, recPatch.Body.String())
	}
	if got := cfg.QuotaExceeded.AutoDisableAuthFileQuotaThresholdPercent; got != 0 {
		t.Fatalf("threshold after PATCH = %d, want 0", got)
	}

	loaded, err := config.LoadConfigOptional(configPath, false)
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	if got := loaded.QuotaExceeded.AutoDisableAuthFileQuotaThresholdPercent; got != 0 {
		t.Fatalf("persisted threshold = %d, want 0", got)
	}
}
