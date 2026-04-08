package auth

import (
	"context"

	internalconfig "github.com/router-for-me/CLIProxyAPI/v6/internal/config"
)

const (
	ClassificationOK             = "ok"
	ClassificationNoQuota        = "no_quota"
	ClassificationInvalidated401 = "invalidated_401"
	ClassificationAPIError       = "api_error"
	ClassificationRequestFailed  = "request_failed"
	ClassificationUnsupported    = "unsupported_provider"
	ClassificationUnknown        = "unknown"

	autoDisabledQuotaStatusMessage = "auto_disabled_quota_exhausted"
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
