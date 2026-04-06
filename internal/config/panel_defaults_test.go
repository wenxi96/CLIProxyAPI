package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigOptional_DefaultPanelGitHubRepository(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	configYAML := []byte(`
remote-management:
  allow-remote: false
  secret-key: ""
`)
	if err := os.WriteFile(configPath, configYAML, 0o600); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	cfg, err := LoadConfigOptional(configPath, false)
	if err != nil {
		t.Fatalf("LoadConfigOptional() error = %v", err)
	}

	if got := cfg.RemoteManagement.PanelGitHubRepository; got != DefaultPanelGitHubRepository {
		t.Fatalf("PanelGitHubRepository = %q, want %q", got, DefaultPanelGitHubRepository)
	}
}
