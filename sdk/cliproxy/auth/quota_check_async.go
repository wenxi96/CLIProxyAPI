package auth

import (
	"context"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
)

const quotaCheckTimeout = 45 * time.Second

func isRuntimeOnlyAuth(auth *Auth) bool {
	if auth == nil || auth.Attributes == nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(auth.Attributes["runtime_only"]), "true")
}

func (m *Manager) autoDisableAuthFileOnZeroQuotaEnabled() bool {
	cfg := m.CurrentConfig()
	return cfg != nil && cfg.QuotaExceeded.AutoDisableAuthFileOnZeroQuota
}

func (m *Manager) tryEnqueueQuotaCheck(authID string) {
	if m == nil || !m.autoDisableAuthFileOnZeroQuotaEnabled() {
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
	if checker == nil || !m.autoDisableAuthFileOnZeroQuotaEnabled() {
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
	if !result.Exhausted {
		return
	}

	m.applyAutoDisableFromQuotaCheck(authID, result)
}

func (m *Manager) applyAutoDisableFromQuotaCheck(authID string, result QuotaCheckResult) {
	if m == nil || !result.Exhausted || !m.autoDisableAuthFileOnZeroQuotaEnabled() {
		return
	}

	var authSnapshot *Auth
	var persistSnapshot *Auth

	m.mu.Lock()
	current := m.auths[authID]
	if current == nil || current.Disabled || isRuntimeOnlyAuth(current) {
		m.mu.Unlock()
		return
	}

	now := time.Now()
	current.Disabled = true
	current.Status = StatusDisabled
	current.StatusMessage = autoDisabledQuotaStatusMessage
	current.Unavailable = false
	current.LastError = nil
	current.NextRetryAfter = time.Time{}
	current.ModelStates = nil
	current.Quota = QuotaState{
		Exceeded: true,
		Reason:   "quota_exhausted",
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
		m.hook.OnAuthUpdated(context.Background(), authSnapshot.Clone())
	}
}
