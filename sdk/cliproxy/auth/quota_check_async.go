package auth

import (
	"context"
	"strings"
	"time"

	internalconfig "github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	log "github.com/sirupsen/logrus"
)

const quotaCheckTimeout = 45 * time.Second

func isRuntimeOnlyAuth(auth *Auth) bool {
	if auth == nil || auth.Attributes == nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(auth.Attributes["runtime_only"]), "true")
}

func (m *Manager) autoDisableAuthFileOnLowQuotaEnabled() bool {
	cfg := m.CurrentConfig()
	return cfg != nil && cfg.QuotaExceeded.AutoDisableAuthFileOnLowQuota
}

func (m *Manager) activeQuotaRefreshConfig() internalconfig.ActiveQuotaRefreshConfig {
	cfg := m.CurrentConfig()
	if cfg == nil {
		return internalconfig.NormalizeActiveQuotaRefreshConfig(internalconfig.ActiveQuotaRefreshConfig{})
	}
	return internalconfig.NormalizeActiveQuotaRefreshConfig(cfg.QuotaExceeded.ActiveQuotaRefresh)
}

func (m *Manager) reconcileActiveQuotaRefresh() {
	if m == nil {
		return
	}
	cfg := m.activeQuotaRefreshConfig()
	if !cfg.Enabled || m.getQuotaChecker() == nil {
		m.stopActiveQuotaRefresh()
		return
	}
	m.startActiveQuotaRefresh(cfg)
}

func (m *Manager) startActiveQuotaRefresh(cfg internalconfig.ActiveQuotaRefreshConfig) {
	if m == nil {
		return
	}
	scanInterval := time.Duration(cfg.ScanIntervalSeconds) * time.Second
	if scanInterval <= 0 {
		scanInterval = time.Duration(internalconfig.DefaultActiveQuotaRefreshScanSec) * time.Second
	}
	ttl := time.Duration(cfg.ActiveTTLSeconds) * time.Second
	if ttl <= 0 {
		ttl = time.Duration(internalconfig.DefaultActiveQuotaRefreshTTLSec) * time.Second
	}
	workers := cfg.Workers
	if workers < 1 {
		workers = internalconfig.DefaultActiveQuotaRefreshWorkers
	}

	m.activeQuotaMu.Lock()
	if m.activeQuotaCancel != nil &&
		m.activeQuotaPool != nil &&
		m.activeQuotaPool.ttl == ttl &&
		m.activeQuotaScan == scanInterval &&
		m.activeQuotaWorkers == workers {
		m.activeQuotaMu.Unlock()
		return
	}
	if m.activeQuotaCancel != nil {
		m.activeQuotaCancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	pool := newActiveQuotaRefreshPool(ttl)
	m.activeQuotaPool = pool
	m.activeQuotaCancel = cancel
	m.activeQuotaScan = scanInterval
	m.activeQuotaWorkers = workers
	m.activeQuotaMu.Unlock()

	go m.runActiveQuotaRefresh(ctx, pool, scanInterval, workers)
}

func (m *Manager) stopActiveQuotaRefresh() {
	if m == nil {
		return
	}
	m.activeQuotaMu.Lock()
	cancel := m.activeQuotaCancel
	m.activeQuotaCancel = nil
	m.activeQuotaPool = nil
	m.activeQuotaScan = 0
	m.activeQuotaWorkers = 0
	m.activeQuotaMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (m *Manager) touchActiveQuotaRefresh(authID string) {
	if m == nil || strings.TrimSpace(authID) == "" {
		return
	}
	cfg := m.activeQuotaRefreshConfig()
	if !cfg.Enabled {
		return
	}
	checker := m.getQuotaChecker()
	if checker == nil {
		return
	}
	snapshot, ok := m.quotaCheckSnapshot(authID, checker)
	if !ok || snapshot == nil {
		return
	}
	m.reconcileActiveQuotaRefresh()

	m.activeQuotaMu.Lock()
	pool := m.activeQuotaPool
	m.activeQuotaMu.Unlock()
	if pool == nil {
		return
	}
	pool.touch(authID, time.Now())
}

func (m *Manager) removeActiveQuotaRefresh(authID string) {
	if m == nil || strings.TrimSpace(authID) == "" {
		return
	}
	m.activeQuotaMu.Lock()
	pool := m.activeQuotaPool
	m.activeQuotaMu.Unlock()
	if pool == nil {
		return
	}
	pool.remove(authID)
}

func (m *Manager) runActiveQuotaRefresh(ctx context.Context, pool *activeQuotaRefreshPool, scanInterval time.Duration, workers int) {
	if m == nil || pool == nil {
		return
	}
	if scanInterval <= 0 {
		scanInterval = time.Duration(internalconfig.DefaultActiveQuotaRefreshScanSec) * time.Second
	}
	if workers < 1 {
		workers = internalconfig.DefaultActiveQuotaRefreshWorkers
	}
	ticker := time.NewTicker(scanInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			m.scanActiveQuotaRefresh(ctx, pool, now, workers)
		}
	}
}

func (m *Manager) scanActiveQuotaRefresh(ctx context.Context, pool *activeQuotaRefreshPool, now time.Time, workers int) {
	if m == nil || pool == nil {
		return
	}
	for _, authID := range pool.due(now, workers) {
		go m.runActiveQuotaRefreshCheck(ctx, pool, authID)
	}
}

func (m *Manager) runActiveQuotaRefreshCheck(parent context.Context, pool *activeQuotaRefreshPool, authID string) {
	checker := m.getQuotaChecker()
	if checker == nil {
		pool.markFailed(authID)
		return
	}
	snapshot, ok := m.quotaCheckSnapshot(authID, checker)
	if !ok || snapshot == nil {
		pool.markFailed(authID)
		return
	}

	ctx, cancel := context.WithTimeout(parent, quotaCheckTimeout)
	defer cancel()

	result, err := checker.Check(ctx, snapshot)
	if err != nil {
		log.WithError(err).Warnf("auth manager: active quota refresh failed for %s", authID)
		pool.markFailed(authID)
		return
	}
	disabled := m.ApplyQuotaCheckResult(authID, result)
	if disabled {
		pool.remove(authID)
		return
	}
	threshold := effectiveAutoDisableThreshold(m.CurrentConfig())
	pool.markComplete(authID, result, threshold, time.Now())
}

func (m *Manager) tryEnqueueQuotaCheck(authID string) {
	if m == nil {
		return
	}
	authID = strings.TrimSpace(authID)
	if authID == "" {
		return
	}
	checker := m.getQuotaChecker()
	if checker == nil {
		return
	}

	snapshot, ok := m.quotaCheckSnapshot(authID, checker)
	if !ok || snapshot == nil {
		return
	}
	if !m.autoDisableAuthFileOnLowQuotaEnabled() && !scopedPoolEnabledForAuth(m.CurrentConfig(), snapshot) {
		return
	}

	m.quotaCheckStartOnce.Do(func() {
		m.quotaCheckWake = make(chan struct{}, 1)
		go m.quotaCheckWorker()
	})

	m.quotaCheckMu.Lock()
	if m.quotaCheckPending == nil {
		m.quotaCheckPending = make(map[string]struct{})
	}
	if m.quotaCheckRunning == nil {
		m.quotaCheckRunning = make(map[string]struct{})
	}
	if _, exists := m.quotaCheckPending[authID]; exists {
		m.quotaCheckMu.Unlock()
		return
	}
	if _, exists := m.quotaCheckRunning[authID]; exists {
		m.quotaCheckMu.Unlock()
		return
	}
	m.quotaCheckPending[authID] = struct{}{}
	m.quotaCheckMu.Unlock()

	select {
	case m.quotaCheckWake <- struct{}{}:
	default:
	}
}

func (m *Manager) quotaCheckWorker() {
	for range m.quotaCheckWake {
		for {
			authID := m.takePendingQuotaCheck()
			if authID == "" {
				break
			}
			m.runQuotaCheck(authID)
		}
	}
}

func (m *Manager) takePendingQuotaCheck() string {
	if m == nil {
		return ""
	}
	m.quotaCheckMu.Lock()
	defer m.quotaCheckMu.Unlock()
	for authID := range m.quotaCheckPending {
		delete(m.quotaCheckPending, authID)
		if m.quotaCheckRunning == nil {
			m.quotaCheckRunning = make(map[string]struct{})
		}
		m.quotaCheckRunning[authID] = struct{}{}
		return authID
	}
	return ""
}

func (m *Manager) finishQuotaCheck(authID string) {
	if m == nil {
		return
	}
	m.quotaCheckMu.Lock()
	delete(m.quotaCheckRunning, authID)
	m.quotaCheckMu.Unlock()
}

func (m *Manager) quotaCheckSnapshot(authID string, checker QuotaChecker) (*Auth, bool) {
	if m == nil || checker == nil {
		return nil, false
	}
	m.mu.RLock()
	current := m.auths[authID]
	if current == nil {
		m.mu.RUnlock()
		return nil, false
	}
	snapshot := current.Clone()
	m.mu.RUnlock()
	if snapshot == nil || snapshot.Disabled || isRuntimeOnlyAuth(snapshot) {
		return nil, false
	}
	if !checker.Supports(snapshot) {
		return nil, false
	}
	return snapshot, true
}

func (m *Manager) runQuotaCheck(authID string) {
	defer m.finishQuotaCheck(authID)

	checker := m.getQuotaChecker()
	if checker == nil {
		return
	}
	snapshot, ok := m.quotaCheckSnapshot(authID, checker)
	if !ok || snapshot == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), quotaCheckTimeout)
	defer cancel()

	result, err := checker.Check(ctx, snapshot)
	if err != nil {
		log.WithError(err).Warnf("auth manager: quota check failed for %s", authID)
		return
	}
	m.ApplyQuotaCheckResult(authID, result)
}

// ApplyQuotaCheckResult applies an already-fetched quota check result to the
// scheduler and auto-disable state without performing another quota request.
// It returns true when the result actually disabled an auth.
func (m *Manager) ApplyQuotaCheckResult(authID string, result QuotaCheckResult) bool {
	if m == nil {
		return false
	}
	if m.scheduler != nil {
		m.scheduler.applyScopedPoolQuotaCheck(authID, result)
	}

	cfg := m.CurrentConfig()
	threshold := effectiveAutoDisableThreshold(cfg)
	if shouldDisable, _ := shouldAutoDisable(result, threshold); shouldDisable {
		return m.applyAutoDisableFromQuotaCheck(authID, result)
	}
	return false
}

func (m *Manager) applyAutoDisableFromQuotaCheck(authID string, result QuotaCheckResult) bool {
	if m == nil || !m.autoDisableAuthFileOnLowQuotaEnabled() {
		return false
	}

	cfg := m.CurrentConfig()
	threshold := effectiveAutoDisableThreshold(cfg)
	shouldDisable, reason := shouldAutoDisable(result, threshold)
	if !shouldDisable {
		return false
	}

	var authSnapshot *Auth
	var persistSnapshot *Auth

	m.mu.Lock()
	current := m.auths[authID]
	if current == nil || current.Disabled || isRuntimeOnlyAuth(current) {
		m.mu.Unlock()
		return false
	}

	// Determine status message based on trigger reason
	statusMessage := autoDisabledQuotaStatusMessage
	quotaReason := "quota_exhausted"
	if reason == "threshold" {
		statusMessage = autoDisabledQuotaThresholdStatusMessage
		quotaReason = "quota_threshold"
	}

	now := time.Now()
	current.Disabled = true
	current.Status = StatusDisabled
	current.StatusMessage = statusMessage
	current.Unavailable = false
	current.LastError = nil
	current.NextRetryAfter = time.Time{}
	current.ModelStates = nil
	current.Quota = QuotaState{
		Exceeded: true,
		Reason:   quotaReason,
	}
	current.UpdatedAt = now
	PrepareMetadataForPersistence(current)

	if m.store != nil {
		persistSnapshot = current.Clone()
	}
	authSnapshot = current.Clone()
	m.mu.Unlock()

	if m.scheduler != nil && authSnapshot != nil {
		m.scheduler.upsertAuth(authSnapshot)
	}
	if persistSnapshot != nil {
		m.enqueuePersist(persistSnapshot)
	}
	if authSnapshot != nil {
		fields := log.Fields{
			"auth_id":        authID,
			"classification": result.Classification,
			"status_code":    result.StatusCode,
			"reason":         reason,
		}
		if result.RemainingPercent != nil {
			fields["remaining_percent"] = *result.RemainingPercent
		}
		log.WithFields(fields).Warn("auth manager: auto disabled auth after quota check")
		m.hook.OnAuthUpdated(context.Background(), authSnapshot.Clone())
	}
	m.removeActiveQuotaRefresh(authID)
	return authSnapshot != nil
}
