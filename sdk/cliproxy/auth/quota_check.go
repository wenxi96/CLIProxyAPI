package auth

import (
	"context"
	"strings"

	internalconfig "github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

const (
	ClassificationOK             = "ok"
	ClassificationNoQuota        = "no_quota"
	ClassificationInvalidated401 = "invalidated_401"
	ClassificationAPIError       = "api_error"
	ClassificationRequestFailed  = "request_failed"
	ClassificationUnsupported    = "unsupported_provider"
	ClassificationUnknown        = "unknown"

	autoDisabledQuotaStatusMessage          = "auto_disabled_quota_exhausted"
	autoDisabledQuotaThresholdStatusMessage = "auto_disabled_quota_threshold"
)

// QuotaCheckResult captures the minimal outcome needed by runtime auto-disable logic.
type QuotaCheckResult struct {
	Classification   string
	RemainingPercent *int
	ErrorMessage     string
	StatusCode       int
	Exhausted        bool
}

// QuotaChecker confirms whether a credential with real quota inspection support is exhausted.
type QuotaChecker interface {
	Supports(auth *Auth) bool
	Check(ctx context.Context, auth *Auth) (QuotaCheckResult, error)
}

// SetQuotaChecker registers the runtime quota checker used by the async auto-disable queue.
func (m *Manager) SetQuotaChecker(checker QuotaChecker) {
	if m == nil {
		return
	}
	m.quotaCheckerMu.Lock()
	m.quotaChecker = checker
	m.quotaCheckerMu.Unlock()
	if m.scheduler != nil {
		m.scheduler.setQuotaSupportEvaluator(func(auth *Auth) bool {
			if checker == nil {
				return false
			}
			return checker.Supports(auth)
		})
		m.syncScheduler()
	}
	m.reconcileActiveQuotaRefresh()
}

func (m *Manager) getQuotaChecker() QuotaChecker {
	if m == nil {
		return nil
	}
	m.quotaCheckerMu.RLock()
	defer m.quotaCheckerMu.RUnlock()
	return m.quotaChecker
}

// CurrentConfig returns the latest runtime config snapshot.
func (m *Manager) CurrentConfig() *internalconfig.Config {
	if m == nil {
		return nil
	}
	cfg, _ := m.runtimeConfig.Load().(*internalconfig.Config)
	return cfg
}

func shouldEnqueueQuotaCheck(result Result) bool {
	if result.Success || strings.TrimSpace(result.AuthID) == "" || result.Error == nil {
		return false
	}
	switch statusCodeFromResult(result.Error) {
	case 402, 403, 429:
		return hasQuotaSignal(result.Error.Message)
	default:
		return false
	}
}

func hasQuotaSignal(message string) bool {
	lower := strings.ToLower(strings.TrimSpace(message))
	if lower == "" {
		return false
	}
	for _, token := range []string{
		"quota",
		"usage limit",
		"usage_limit",
		"insufficient_quota",
		"credit",
		"credits exhausted",
		"billing",
	} {
		if strings.Contains(lower, token) {
			return true
		}
	}
	return false
}

// shouldAutoDisable checks whether a quota check result should trigger auto-disable.
// It returns (shouldDisable bool, reason string).
// When result.Exhausted is true, reason is "exhausted".
// When threshold is hit (remaining_percent <= threshold), reason is "threshold".
// Exhausted takes priority over threshold.
func shouldAutoDisable(result QuotaCheckResult, threshold int) (bool, string) {
	if result.Exhausted {
		return true, "exhausted"
	}
	if threshold > 0 && result.RemainingPercent != nil && *result.RemainingPercent <= threshold {
		return true, "threshold"
	}
	return false, ""
}

// effectiveAutoDisableThreshold returns the effective threshold from config.
// Returns 0 if config is nil or threshold is not set.
func effectiveAutoDisableThreshold(cfg *internalconfig.Config) int {
	if cfg == nil {
		return 0
	}
	threshold := cfg.QuotaExceeded.AutoDisableAuthFileQuotaThresholdPercent
	if threshold < 0 {
		return 0
	}
	if threshold > internalconfig.MaxAutoDisableQuotaThresholdPercent {
		return internalconfig.MaxAutoDisableQuotaThresholdPercent
	}
	return threshold
}
