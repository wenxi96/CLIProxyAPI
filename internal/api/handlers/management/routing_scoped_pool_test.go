package management

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	"github.com/tidwall/gjson"
)

func testManagedAuth(id, provider string, priority int) *coreauth.Auth {
	auth := &coreauth.Auth{
		ID:       id,
		Provider: provider,
		Status:   coreauth.StatusActive,
		FileName: id + ".json",
		Attributes: map[string]string{
			"path": "/tmp/" + id + ".json",
		},
	}
	if priority > 0 {
		auth.Attributes["priority"] = strconv.Itoa(priority)
	}
	auth.EnsureIndex()
	return auth
}

func TestGetRoutingScopedPoolStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := &config.Config{
		Routing: config.RoutingConfig{
			Strategy: "round-robin",
			ScopedPool: config.RoutingScopedPoolConfig{
				Providers: map[string]config.RoutingScopedPoolProviderConfig{
					"codex": {Enabled: true, Limit: 1},
				},
			},
		},
	}
	manager := coreauth.NewManager(nil, nil, nil)
	manager.SetConfig(cfg)
	if _, err := manager.Register(context.Background(), testManagedAuth("codex-a", "codex", 2)); err != nil {
		t.Fatalf("register codex-a: %v", err)
	}
	if _, err := manager.Register(context.Background(), testManagedAuth("codex-b", "codex", 1)); err != nil {
		t.Fatalf("register codex-b: %v", err)
	}

	handler := &Handler{cfg: cfg, authManager: manager}
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v0/management/routing/scoped-pool/status", nil)

	handler.GetRoutingScopedPoolStatus(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	body := recorder.Body.String()
	if !gjson.Get(body, "providers.codex.effective").Bool() {
		t.Fatalf("expected codex effective in response: %s", body)
	}
	if gjson.Get(body, "providers.codex.active_count").Int() != 1 {
		t.Fatalf("expected active_count=1 in response: %s", body)
	}
}

func TestListAuthFiles_EmitsScopedPoolFields(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := &config.Config{
		Routing: config.RoutingConfig{
			Strategy: "round-robin",
			ScopedPool: config.RoutingScopedPoolConfig{
				Providers: map[string]config.RoutingScopedPoolProviderConfig{
					"codex": {Enabled: true, Limit: 1},
				},
			},
		},
	}
	manager := coreauth.NewManager(nil, nil, nil)
	manager.SetConfig(cfg)
	if _, err := manager.Register(context.Background(), testManagedAuth("codex-a", "codex", 2)); err != nil {
		t.Fatalf("register codex-a: %v", err)
	}
	if _, err := manager.Register(context.Background(), testManagedAuth("codex-b", "codex", 1)); err != nil {
		t.Fatalf("register codex-b: %v", err)
	}

	handler := &Handler{cfg: cfg, authManager: manager}
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v0/management/auth-files", nil)

	handler.ListAuthFiles(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	body := recorder.Body.String()
	if !gjson.Get(body, "files.0.pool_configured").Bool() {
		t.Fatalf("expected pool_configured on auth file entry: %s", body)
	}
	if !gjson.Get(body, "files.0.pool_enabled").Bool() {
		t.Fatalf("expected pool_enabled on auth file entry: %s", body)
	}
	if gjson.Get(body, "files.0.pool_state").String() == "" {
		t.Fatalf("expected pool_state on auth file entry: %s", body)
	}
}
