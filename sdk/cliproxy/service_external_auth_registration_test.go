package cliproxy

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/config"
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

func TestConfigAPIKeyRegistrationSkipsLifecycleFeedback(t *testing.T) {
	service := &Service{
		cfg: &config.Config{
			OpenAICompatibility: []config.OpenAICompatibility{
				{
					Name:    "startup-test",
					BaseURL: "https://example.invalid/v1",
					APIKeyEntries: []config.OpenAICompatibilityAPIKey{
						{APIKey: "test-key"},
					},
					Models: []config.OpenAICompatibilityModel{
						{Name: "upstream-model", Alias: "startup-test-model"},
					},
				},
			},
		},
	}
	service.coreManager = coreauth.NewManager(nil, &coreauth.RoundRobinSelector{}, authMaintenanceHook{
		next:    coreauth.NoopHook{},
		service: service,
	})
	service.coreManager.SetConfig(service.cfg)

	done := make(chan struct{})
	go func() {
		service.registerConfigAPIKeyAuths(context.Background(), service.cfg)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("config API key registration did not return; possible lifecycle feedback loop")
	}

	auths := service.coreManager.List()
	if len(auths) != 1 {
		t.Fatalf("registered auth count = %d, want 1", len(auths))
	}
	if auths[0].Provider != "openai-compatible-startup-test" {
		t.Fatalf("registered provider = %q, want openai-compatible-startup-test", auths[0].Provider)
	}

	GlobalModelRegistry().UnregisterClient(auths[0].ID)
}
