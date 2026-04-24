package cliproxy

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPI/v6/sdk/config"
)

func TestExternalAuthRegistrationTriggersModelRegistration(t *testing.T) {
	authDir := t.TempDir()
	service := &Service{
		cfg: &config.Config{AuthDir: authDir},
	}
	service.coreManager = coreauth.NewManager(nil, &coreauth.RoundRobinSelector{}, authMaintenanceHook{
		next:    coreauth.NoopHook{},
		service: service,
	})
	service.coreManager.SetConfig(service.cfg)

	service.ensureAuthUpdateQueue(context.Background())
	t.Cleanup(func() {
		if service.authQueueStop != nil {
			service.authQueueStop()
		}
	})

	auth := &coreauth.Auth{
		ID:       "external-codex-auth",
		Provider: "codex",
		FileName: "external-codex-auth.json",
		Status:   coreauth.StatusActive,
		Attributes: map[string]string{
			"path": filepath.Join(authDir, "external-codex-auth.json"),
		},
	}

	reg := GlobalModelRegistry()
	reg.UnregisterClient(auth.ID)
	t.Cleanup(func() {
		reg.UnregisterClient(auth.ID)
	})

	if _, err := service.coreManager.Register(context.Background(), auth); err != nil {
		t.Fatalf("register auth: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		if reg.ClientSupportsModel(auth.ID, "gpt-5.4") {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("expected external auth registration to publish gpt-5.4")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
