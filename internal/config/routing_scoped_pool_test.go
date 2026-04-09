package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNormalizeRoutingScopedPoolConfig(t *testing.T) {
	normalized := NormalizeRoutingScopedPoolConfig(RoutingScopedPoolConfig{
		Defaults: RoutingScopedPoolProviderConfig{
			Limit:                     -1,
			QuotaThresholdPercent:     99,
			ConsecutiveErrorThreshold: 0,
			PenaltyWindowSeconds:      0,
			QuotaSnapshotTTLSeconds:   0,
			IdleLogThrottleSeconds:    0,
		},
		Providers: map[string]RoutingScopedPoolProviderConfig{
			" Codex ": {
				Enabled:                   true,
				Limit:                     2,
				QuotaThresholdPercent:     -5,
				ConsecutiveErrorThreshold: 7,
			},
		},
	})

	if normalized.Defaults.Limit != DefaultScopedPoolLimit {
		t.Fatalf("expected default limit %d, got %d", DefaultScopedPoolLimit, normalized.Defaults.Limit)
	}
	if normalized.Defaults.QuotaThresholdPercent != MaxScopedPoolQuotaPercent {
		t.Fatalf("expected default quota threshold clamp to %d, got %d", MaxScopedPoolQuotaPercent, normalized.Defaults.QuotaThresholdPercent)
	}
	codex, ok := normalized.Providers["codex"]
	if !ok {
		t.Fatalf("expected normalized provider key 'codex'")
	}
	if !codex.Enabled {
		t.Fatalf("expected codex enabled")
	}
	if codex.Limit != 2 {
		t.Fatalf("expected codex limit 2, got %d", codex.Limit)
	}
	if codex.QuotaThresholdPercent != 0 {
		t.Fatalf("expected quota threshold lower clamp to 0, got %d", codex.QuotaThresholdPercent)
	}
	if codex.ConsecutiveErrorThreshold != 7 {
		t.Fatalf("expected custom error threshold 7, got %d", codex.ConsecutiveErrorThreshold)
	}
	if codex.PenaltyWindowSeconds != DefaultScopedPoolPenaltySec {
		t.Fatalf("expected penalty fallback %d, got %d", DefaultScopedPoolPenaltySec, codex.PenaltyWindowSeconds)
	}
}

func TestSaveConfigPreserveComments_DisablesScopedPoolProvider(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	original := `routing:
  strategy: "round-robin"
  scoped-pool:
    defaults:
      enabled: false
      limit: 1
    providers:
      codex:
        enabled: true
        limit: 1
      claude:
        enabled: true
        limit: 1
`
	if err := os.WriteFile(configPath, []byte(original), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	cfg.Routing.ScopedPool = NormalizeRoutingScopedPoolConfig(RoutingScopedPoolConfig{
		Defaults: cfg.Routing.ScopedPool.Defaults,
		Providers: map[string]RoutingScopedPoolProviderConfig{
			"codex": {
				Enabled: false,
				Limit:   1,
			},
			"claude": {
				Enabled: true,
				Limit:   1,
			},
		},
	})

	if err := SaveConfigPreserveComments(configPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	reloaded, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}

	codex, ok := reloaded.Routing.ScopedPool.Providers["codex"]
	if !ok {
		t.Fatalf("expected codex provider after reload")
	}
	if codex.Enabled {
		t.Fatalf("expected codex scoped-pool to remain disabled after reload")
	}
	if codex.Limit != 1 {
		t.Fatalf("expected codex limit 1 after reload, got %d", codex.Limit)
	}

	claude, ok := reloaded.Routing.ScopedPool.Providers["claude"]
	if !ok || !claude.Enabled {
		t.Fatalf("expected claude provider to remain enabled after reload")
	}
}
