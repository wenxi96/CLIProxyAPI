package main

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

func TestApplyHomeRuntimeDefaultsPreservesConfiguredPort(t *testing.T) {
	cfg := applyHomeRuntimeDefaults(&config.Config{Port: 9000}, config.HomeConfig{
		Enabled: true,
		Host:    "127.0.0.1",
		Port:    6379,
	})

	if cfg.Port != 9000 {
		t.Fatalf("Port = %d, want 9000", cfg.Port)
	}
	if !cfg.Home.Enabled {
		t.Fatal("Home.Enabled = false, want true")
	}
	if !cfg.UsageStatisticsEnabled {
		t.Fatal("UsageStatisticsEnabled = false, want true")
	}
}

func TestApplyHomeRuntimeDefaultsUsesDefaultPortWhenMissing(t *testing.T) {
	cfg := applyHomeRuntimeDefaults(&config.Config{}, config.HomeConfig{
		Enabled: true,
		Host:    "127.0.0.1",
		Port:    6379,
	})

	if cfg.Port != 8317 {
		t.Fatalf("Port = %d, want 8317", cfg.Port)
	}
}
