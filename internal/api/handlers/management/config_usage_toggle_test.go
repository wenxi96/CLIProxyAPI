package management

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

func TestPutUsageStatisticsEnabledWaitsForRuntimeTransaction(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("usage-statistics-enabled: false\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	h := NewHandler(&config.Config{UsageStatisticsEnabled: false}, configPath, nil)
	called := false
	h.SetConfigRuntimeTxnHook(func(_ context.Context, candidate *config.Config) error {
		called = true
		if !candidate.UsageStatisticsEnabled {
			t.Fatal("runtime hook received old config")
		}
		return errors.New("restore failed")
	})

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPut, "/v0/management/usage-statistics-enabled", strings.NewReader(`{"value":true}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	h.PutUsageStatisticsEnabled(ctx)

	if !called {
		t.Fatal("runtime transaction hook was not called synchronously")
	}
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body=%s", recorder.Code, recorder.Body.String())
	}
	if h.cfg.UsageStatisticsEnabled {
		t.Fatal("handler config changed after failed runtime transaction")
	}
}
