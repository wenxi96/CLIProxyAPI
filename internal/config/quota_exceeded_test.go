package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestQuotaExceededLowQuotaLegacyAlias(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	original := `quota-exceeded:
  auto-disable-auth-file-on-zero-quota: true
  auto-disable-auth-file-quota-threshold-percent: 10
`
	if err := os.WriteFile(configPath, []byte(original), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := LoadConfigOptional(configPath, false)
	if err != nil {
		t.Fatalf("LoadConfigOptional() error = %v", err)
	}
	if !cfg.QuotaExceeded.AutoDisableAuthFileOnLowQuota {
		t.Fatal("legacy zero-quota key did not populate low-quota auto-disable")
	}
	if !cfg.QuotaExceeded.SwitchProject {
		t.Fatal("legacy alias-only config should keep switch-project default enabled")
	}
	if !cfg.QuotaExceeded.SwitchPreviewModel {
		t.Fatal("legacy alias-only config should keep switch-preview-model default enabled")
	}

	if err := SaveConfigPreserveComments(configPath, cfg); err != nil {
		t.Fatalf("SaveConfigPreserveComments() error = %v", err)
	}
	updated, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read updated config: %v", err)
	}
	text := string(updated)
	if strings.Contains(text, "auto-disable-auth-file-on-zero-quota") {
		t.Fatalf("updated config still contains legacy key:\n%s", text)
	}
	if !strings.Contains(text, "auto-disable-auth-file-on-low-quota: true") {
		t.Fatalf("updated config did not write low-quota key:\n%s", text)
	}
}

func TestQuotaExceededExplicitSwitchOverridesDefaults(t *testing.T) {
	cfg, err := ParseConfigBytes([]byte(`quota-exceeded:
  switch-project: false
  switch-preview-model: false
  auto-disable-auth-file-on-low-quota: true
`))
	if err != nil {
		t.Fatalf("ParseConfigBytes() error = %v", err)
	}
	if cfg.QuotaExceeded.SwitchProject {
		t.Fatal("explicit switch-project false should override default")
	}
	if cfg.QuotaExceeded.SwitchPreviewModel {
		t.Fatal("explicit switch-preview-model false should override default")
	}
	if !cfg.QuotaExceeded.AutoDisableAuthFileOnLowQuota {
		t.Fatal("low-quota key did not populate auto-disable switch")
	}
}

func TestQuotaExceededLowQuotaKeyTakesPrecedenceOverLegacyAlias(t *testing.T) {
	cfg, err := ParseConfigBytes([]byte(`quota-exceeded:
  auto-disable-auth-file-on-low-quota: false
  auto-disable-auth-file-on-zero-quota: true
  auto-disable-auth-file-quota-threshold-percent: 99
`))
	if err != nil {
		t.Fatalf("ParseConfigBytes() error = %v", err)
	}
	if cfg.QuotaExceeded.AutoDisableAuthFileOnLowQuota {
		t.Fatal("low-quota key should take precedence over legacy zero-quota alias")
	}
	if got := cfg.QuotaExceeded.AutoDisableAuthFileQuotaThresholdPercent; got != MaxAutoDisableQuotaThresholdPercent {
		t.Fatalf("threshold = %d, want %d", got, MaxAutoDisableQuotaThresholdPercent)
	}
}
