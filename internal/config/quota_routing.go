package config

import (
	"encoding/json"
	"strings"

	"gopkg.in/yaml.v3"
)

// UnmarshalYAML preserves legacy quota-exceeded aliases while retaining explicit booleans.
func (q *QuotaExceeded) UnmarshalYAML(value *yaml.Node) error {
	type quotaExceededYAML struct {
		SwitchProject                        *bool                    `yaml:"switch-project"`
		SwitchPreviewModel                   *bool                    `yaml:"switch-preview-model"`
		AutoDisableAuthFileOnLowQuota        *bool                    `yaml:"auto-disable-auth-file-on-low-quota"`
		AutoDisableAuthFileOnZeroQuotaLegacy *bool                    `yaml:"auto-disable-auth-file-on-zero-quota"`
		AutoDisableAuthFileQuotaThreshold    int                      `yaml:"auto-disable-auth-file-quota-threshold-percent"`
		AntigravityCredits                   bool                     `yaml:"antigravity-credits"`
		ActiveQuotaRefresh                   ActiveQuotaRefreshConfig `yaml:"active-quota-refresh"`
	}
	var raw quotaExceededYAML
	if errDecode := value.Decode(&raw); errDecode != nil {
		return errDecode
	}
	if raw.SwitchProject != nil {
		q.SwitchProject = *raw.SwitchProject
	}
	if raw.SwitchPreviewModel != nil {
		q.SwitchPreviewModel = *raw.SwitchPreviewModel
	}
	if raw.AutoDisableAuthFileOnLowQuota != nil {
		q.AutoDisableAuthFileOnLowQuota = *raw.AutoDisableAuthFileOnLowQuota
	} else if raw.AutoDisableAuthFileOnZeroQuotaLegacy != nil {
		q.AutoDisableAuthFileOnLowQuota = *raw.AutoDisableAuthFileOnZeroQuotaLegacy
	}
	q.AutoDisableAuthFileQuotaThresholdPercent = raw.AutoDisableAuthFileQuotaThreshold
	q.AntigravityCredits = raw.AntigravityCredits
	q.ActiveQuotaRefresh = raw.ActiveQuotaRefresh
	return nil
}

// UnmarshalJSON preserves legacy quota-exceeded aliases while retaining explicit booleans.
func (q *QuotaExceeded) UnmarshalJSON(data []byte) error {
	type quotaExceededJSON struct {
		SwitchProject                        *bool                    `json:"switch-project"`
		SwitchPreviewModel                   *bool                    `json:"switch-preview-model"`
		AutoDisableAuthFileOnLowQuota        *bool                    `json:"auto-disable-auth-file-on-low-quota"`
		AutoDisableAuthFileOnLowQuotaCamel   *bool                    `json:"autoDisableAuthFileOnLowQuota"`
		AutoDisableAuthFileOnZeroQuotaLegacy *bool                    `json:"auto-disable-auth-file-on-zero-quota"`
		AutoDisableAuthFileOnZeroQuotaCamel  *bool                    `json:"autoDisableAuthFileOnZeroQuota"`
		AutoDisableAuthFileQuotaThreshold    int                      `json:"auto-disable-auth-file-quota-threshold-percent"`
		AntigravityCredits                   bool                     `json:"antigravity-credits"`
		ActiveQuotaRefresh                   ActiveQuotaRefreshConfig `json:"active-quota-refresh"`
	}
	var raw quotaExceededJSON
	if errUnmarshal := json.Unmarshal(data, &raw); errUnmarshal != nil {
		return errUnmarshal
	}
	if raw.SwitchProject != nil {
		q.SwitchProject = *raw.SwitchProject
	}
	if raw.SwitchPreviewModel != nil {
		q.SwitchPreviewModel = *raw.SwitchPreviewModel
	}
	switch {
	case raw.AutoDisableAuthFileOnLowQuota != nil:
		q.AutoDisableAuthFileOnLowQuota = *raw.AutoDisableAuthFileOnLowQuota
	case raw.AutoDisableAuthFileOnLowQuotaCamel != nil:
		q.AutoDisableAuthFileOnLowQuota = *raw.AutoDisableAuthFileOnLowQuotaCamel
	case raw.AutoDisableAuthFileOnZeroQuotaLegacy != nil:
		q.AutoDisableAuthFileOnLowQuota = *raw.AutoDisableAuthFileOnZeroQuotaLegacy
	case raw.AutoDisableAuthFileOnZeroQuotaCamel != nil:
		q.AutoDisableAuthFileOnLowQuota = *raw.AutoDisableAuthFileOnZeroQuotaCamel
	}
	q.AutoDisableAuthFileQuotaThresholdPercent = raw.AutoDisableAuthFileQuotaThreshold
	q.AntigravityCredits = raw.AntigravityCredits
	q.ActiveQuotaRefresh = raw.ActiveQuotaRefresh
	return nil
}

// SanitizeRouting normalizes routing strategy and scoped-pool settings.
func (cfg *Config) SanitizeRouting() {
	if cfg == nil {
		return
	}
	switch strings.ToLower(strings.TrimSpace(cfg.Routing.Strategy)) {
	case "", "round-robin", "roundrobin", "rr":
		cfg.Routing.Strategy = "round-robin"
	case "weighted-round-robin", "weightedroundrobin", "wrr":
		cfg.Routing.Strategy = "weighted-round-robin"
	case "fill-first", "fillfirst", "ff":
		cfg.Routing.Strategy = "fill-first"
	default:
		cfg.Routing.Strategy = strings.TrimSpace(cfg.Routing.Strategy)
	}
	cfg.Routing.ScopedPool = NormalizeRoutingScopedPoolConfig(cfg.Routing.ScopedPool)
}

// SanitizeQuotaExceeded normalizes quota-exceeded settings.
func (cfg *Config) SanitizeQuotaExceeded() {
	if cfg == nil {
		return
	}
	if cfg.QuotaExceeded.AutoDisableAuthFileQuotaThresholdPercent < 0 {
		cfg.QuotaExceeded.AutoDisableAuthFileQuotaThresholdPercent = 0
	}
	if cfg.QuotaExceeded.AutoDisableAuthFileQuotaThresholdPercent > MaxAutoDisableQuotaThresholdPercent {
		cfg.QuotaExceeded.AutoDisableAuthFileQuotaThresholdPercent = MaxAutoDisableQuotaThresholdPercent
	}
	cfg.QuotaExceeded.ActiveQuotaRefresh = NormalizeActiveQuotaRefreshConfig(cfg.QuotaExceeded.ActiveQuotaRefresh)
}

// NormalizeActiveQuotaRefreshConfig normalizes background quota refresh settings.
func NormalizeActiveQuotaRefreshConfig(cfg ActiveQuotaRefreshConfig) ActiveQuotaRefreshConfig {
	if cfg.ScanIntervalSeconds <= 0 {
		cfg.ScanIntervalSeconds = DefaultActiveQuotaRefreshScanSec
	} else if cfg.ScanIntervalSeconds < MinActiveQuotaRefreshScanSec {
		cfg.ScanIntervalSeconds = MinActiveQuotaRefreshScanSec
	}
	if cfg.ActiveTTLSeconds <= 0 {
		cfg.ActiveTTLSeconds = DefaultActiveQuotaRefreshTTLSec
	} else if cfg.ActiveTTLSeconds < MinActiveQuotaRefreshTTLSec {
		cfg.ActiveTTLSeconds = MinActiveQuotaRefreshTTLSec
	}
	if cfg.Workers < 1 {
		cfg.Workers = DefaultActiveQuotaRefreshWorkers
	}
	return cfg
}

// DefaultRoutingScopedPoolProviderConfig returns normalized defaults for scoped-pool entries.
func DefaultRoutingScopedPoolProviderConfig() RoutingScopedPoolProviderConfig {
	return RoutingScopedPoolProviderConfig{
		Limit:                     DefaultScopedPoolLimit,
		ConsecutiveErrorThreshold: DefaultScopedPoolErrorLimit,
		PenaltyWindowSeconds:      DefaultScopedPoolPenaltySec,
		QuotaSnapshotTTLSeconds:   DefaultScopedPoolQuotaTTLSec,
		IdleLogThrottleSeconds:    DefaultScopedPoolIdleLogSec,
	}
}

func cloneOptionalBool(value *bool) *bool {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func hasEnabledRoutingScopedPoolProvider(providers map[string]RoutingScopedPoolProviderConfig) bool {
	for _, provider := range providers {
		if provider.Enabled {
			return true
		}
	}
	return false
}

// IsRoutingScopedPoolEnabled reports whether scoped-pool should be considered globally enabled.
func IsRoutingScopedPoolEnabled(cfg RoutingScopedPoolConfig) bool {
	if cfg.Enabled != nil {
		return *cfg.Enabled
	}
	return hasEnabledRoutingScopedPoolProvider(cfg.Providers)
}

// NormalizeRoutingScopedPoolConfig normalizes defaults and provider-specific scoped-pool settings.
func NormalizeRoutingScopedPoolConfig(cfg RoutingScopedPoolConfig) RoutingScopedPoolConfig {
	defaults := normalizeRoutingScopedPoolProviderConfig(cfg.Defaults, DefaultRoutingScopedPoolProviderConfig())
	defaults.Enabled = cfg.Defaults.Enabled
	enabled := cloneOptionalBool(cfg.Enabled)
	if len(cfg.Providers) == 0 {
		return RoutingScopedPoolConfig{Enabled: enabled, Defaults: defaults}
	}
	providers := make(map[string]RoutingScopedPoolProviderConfig, len(cfg.Providers))
	for rawKey, rawValue := range cfg.Providers {
		key := strings.ToLower(strings.TrimSpace(rawKey))
		if key == "" {
			continue
		}
		normalized := normalizeRoutingScopedPoolProviderConfig(rawValue, defaults)
		normalized.Enabled = rawValue.Enabled
		providers[key] = normalized
	}
	if len(providers) == 0 {
		return RoutingScopedPoolConfig{Enabled: enabled, Defaults: defaults}
	}
	if enabled == nil && hasEnabledRoutingScopedPoolProvider(providers) {
		inferred := true
		enabled = &inferred
	}
	return RoutingScopedPoolConfig{Enabled: enabled, Defaults: defaults, Providers: providers}
}

func normalizeRoutingScopedPoolProviderConfig(cfg RoutingScopedPoolProviderConfig, fallback RoutingScopedPoolProviderConfig) RoutingScopedPoolProviderConfig {
	normalized := fallback
	normalized.Enabled = cfg.Enabled
	if cfg.Limit > 0 {
		normalized.Limit = cfg.Limit
	}
	if normalized.Limit <= 0 {
		normalized.Limit = DefaultScopedPoolLimit
	}
	normalized.QuotaThresholdPercent = cfg.QuotaThresholdPercent
	if normalized.QuotaThresholdPercent < 0 {
		normalized.QuotaThresholdPercent = 0
	}
	if normalized.QuotaThresholdPercent > MaxScopedPoolQuotaPercent {
		normalized.QuotaThresholdPercent = MaxScopedPoolQuotaPercent
	}
	if cfg.ConsecutiveErrorThreshold > 0 {
		normalized.ConsecutiveErrorThreshold = cfg.ConsecutiveErrorThreshold
	}
	if normalized.ConsecutiveErrorThreshold <= 0 {
		normalized.ConsecutiveErrorThreshold = DefaultScopedPoolErrorLimit
	}
	if cfg.PenaltyWindowSeconds > 0 {
		normalized.PenaltyWindowSeconds = cfg.PenaltyWindowSeconds
	}
	if normalized.PenaltyWindowSeconds <= 0 {
		normalized.PenaltyWindowSeconds = DefaultScopedPoolPenaltySec
	}
	if cfg.QuotaSnapshotTTLSeconds > 0 {
		normalized.QuotaSnapshotTTLSeconds = cfg.QuotaSnapshotTTLSeconds
	}
	if normalized.QuotaSnapshotTTLSeconds <= 0 {
		normalized.QuotaSnapshotTTLSeconds = DefaultScopedPoolQuotaTTLSec
	}
	if cfg.IdleLogThrottleSeconds > 0 {
		normalized.IdleLogThrottleSeconds = cfg.IdleLogThrottleSeconds
	}
	if normalized.IdleLogThrottleSeconds <= 0 {
		normalized.IdleLogThrottleSeconds = DefaultScopedPoolIdleLogSec
	}
	return normalized
}
