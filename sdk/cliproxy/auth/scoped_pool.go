package auth

import (
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	internalconfig "github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	log "github.com/sirupsen/logrus"
)

type quotaSupportEvaluator func(*Auth) bool

type scopedPoolRuntimeConfig struct {
	strategy  string
	enabled   bool
	defaults  internalconfig.RoutingScopedPoolProviderConfig
	providers map[string]internalconfig.RoutingScopedPoolProviderConfig
}

type scopedPoolProviderState struct {
	providerKey   string
	auths         map[string]*scopedPoolAuthState
	activeAuthIDs []string
	lastDigest    string
	lastLogAt     time.Time
}

type scopedPoolAuthState struct {
	authID             string
	authIndex          string
	providerKey        string
	priority           int
	disabled           bool
	unavailable        bool
	runtimeOnly        bool
	supportsQuotaCheck bool
	remainingPercent   *int
	lastQuotaCheckedAt time.Time
	consecutiveErrors  int
	recentTimeoutCount int
	penaltyScore       int
	penaltyUntil       time.Time
	lastSelectedAt     time.Time
	lastPoolEventAt    time.Time
	lastTransitionAt   time.Time
	inPool             bool
	state              PoolState
	reason             PoolReason
}

// ScopedPoolManager maintains provider-local active pool state on top of the
// existing auth scheduling pipeline. It never changes routing unless a provider
// explicitly enables scoped-pool and the global strategy stays on round-robin.
type ScopedPoolManager struct {
	mu            sync.Mutex
	runtime       scopedPoolRuntimeConfig
	providers     map[string]*scopedPoolProviderState
	authProviders map[string]string
	quotaSupports quotaSupportEvaluator
}

func newScopedPoolManager() *ScopedPoolManager {
	return &ScopedPoolManager{
		runtime: scopedPoolRuntimeConfig{
			strategy: "round-robin",
			defaults: internalconfig.DefaultRoutingScopedPoolProviderConfig(),
		},
		providers:     make(map[string]*scopedPoolProviderState),
		authProviders: make(map[string]string),
	}
}

func normalizeScopedPoolRuntimeConfig(strategy string, cfg internalconfig.RoutingScopedPoolConfig) scopedPoolRuntimeConfig {
	normalized := internalconfig.NormalizeRoutingScopedPoolConfig(cfg)
	out := scopedPoolRuntimeConfig{
		strategy:  "round-robin",
		enabled:   internalconfig.IsRoutingScopedPoolEnabled(normalized),
		defaults:  normalized.Defaults,
		providers: make(map[string]internalconfig.RoutingScopedPoolProviderConfig, len(normalized.Providers)),
	}
	switch strings.ToLower(strings.TrimSpace(strategy)) {
	case "fill-first", "fillfirst", "ff":
		out.strategy = "fill-first"
	default:
		out.strategy = "round-robin"
	}
	for key, value := range normalized.Providers {
		out.providers[strings.ToLower(strings.TrimSpace(key))] = value
	}
	return out
}

func scopedPoolEnabledForAuth(cfg *internalconfig.Config, auth *Auth) bool {
	if cfg == nil || auth == nil {
		return false
	}
	providerKey := scopedPoolProviderKey(auth)
	if providerKey == "" {
		return false
	}
	runtime := normalizeScopedPoolRuntimeConfig(cfg.Routing.Strategy, cfg.Routing.ScopedPool)
	providerCfg, ok := runtime.providers[providerKey]
	if !ok || !providerCfg.Enabled {
		return false
	}
	if !runtime.enabled {
		return false
	}
	return runtime.strategy == "round-robin"
}

func (m *ScopedPoolManager) SetConfig(strategy string, cfg internalconfig.RoutingScopedPoolConfig) {
	if m == nil {
		return
	}
	now := time.Now()
	m.mu.Lock()
	m.runtime = normalizeScopedPoolRuntimeConfig(strategy, cfg)
	for providerKey := range m.providers {
		m.refreshProviderLocked(providerKey, now, nil)
	}
	m.mu.Unlock()
}

func (m *ScopedPoolManager) SetQuotaSupportEvaluator(evaluator quotaSupportEvaluator) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.quotaSupports = evaluator
	m.mu.Unlock()
}

func (m *ScopedPoolManager) Rebuild(auths []*Auth) {
	if m == nil {
		return
	}
	now := time.Now()
	m.mu.Lock()

	previousStates := make(map[string]scopedPoolAuthState)
	for _, providerState := range m.providers {
		if providerState == nil {
			continue
		}
		for authID, state := range providerState.auths {
			if state == nil {
				continue
			}
			previousStates[authID] = *state
		}
	}

	m.providers = make(map[string]*scopedPoolProviderState)
	m.authProviders = make(map[string]string)

	for _, auth := range auths {
		if auth == nil {
			continue
		}
		authID := strings.TrimSpace(auth.ID)
		if authID == "" {
			continue
		}
		auth.EnsureIndex()
		providerKey := scopedPoolProviderKey(auth)
		if providerKey == "" {
			continue
		}

		restored := scopedPoolAuthState{
			authID:      authID,
			providerKey: providerKey,
			state:       PoolStateUnmanaged,
			reason:      PoolReasonNotEnabled,
		}
		if previous, ok := previousStates[authID]; ok {
			restored = previous
		}

		restored.authID = authID
		restored.providerKey = providerKey
		restored.authIndex = strings.TrimSpace(auth.Index)
		restored.priority = authPriority(auth)
		restored.disabled = auth.Disabled || auth.Status == StatusDisabled
		restored.unavailable = auth.Unavailable
		restored.runtimeOnly = isRuntimeOnlyAuth(auth)
		if m.quotaSupports != nil {
			restored.supportsQuotaCheck = m.quotaSupports(auth)
		} else {
			restored.supportsQuotaCheck = false
		}
		if restored.disabled {
			restored.inPool = false
		}

		providerState := m.ensureProviderLocked(providerKey)
		stateCopy := restored
		providerState.auths[authID] = &stateCopy
		m.authProviders[authID] = providerKey
	}

	for providerKey := range m.providers {
		m.refreshProviderLocked(providerKey, now, nil)
	}
	m.mu.Unlock()
}

func (m *ScopedPoolManager) SyncAuth(auth *Auth) {
	if m == nil || auth == nil {
		return
	}
	now := time.Now()
	m.mu.Lock()
	providerKey, previousProvider := m.upsertAuthLocked(auth)
	if previousProvider != "" && previousProvider != providerKey {
		m.refreshProviderLocked(previousProvider, now, nil)
	}
	if providerKey != "" {
		m.refreshProviderLocked(providerKey, now, nil)
	}
	m.mu.Unlock()
}

func (m *ScopedPoolManager) RemoveAuth(authID string) {
	if m == nil {
		return
	}
	authID = strings.TrimSpace(authID)
	if authID == "" {
		return
	}
	now := time.Now()
	m.mu.Lock()
	providerKey := m.authProviders[authID]
	delete(m.authProviders, authID)
	if providerKey != "" {
		if providerState := m.providers[providerKey]; providerState != nil {
			delete(providerState.auths, authID)
			m.refreshProviderLocked(providerKey, now, nil)
		}
	}
	m.mu.Unlock()
}

func (m *ScopedPoolManager) FilterCandidates(provider string, candidates []*Auth) []*Auth {
	if m == nil || len(candidates) == 0 {
		return candidates
	}
	providerKey := strings.ToLower(strings.TrimSpace(provider))
	now := time.Now()
	preferredIDs := make(map[string]struct{}, len(candidates))

	m.mu.Lock()
	for _, candidate := range candidates {
		if candidate == nil {
			continue
		}
		if providerKey == "" {
			providerKey = scopedPoolProviderKey(candidate)
		}
		preferredIDs[strings.TrimSpace(candidate.ID)] = struct{}{}
		m.upsertAuthLocked(candidate)
	}
	if providerKey == "" {
		m.mu.Unlock()
		return candidates
	}
	_, _, effective, _ := m.resolveProviderLocked(providerKey)
	if !effective {
		m.mu.Unlock()
		return candidates
	}
	m.refreshProviderLocked(providerKey, now, preferredIDs)
	providerState := m.providers[providerKey]
	allowed := make(map[string]struct{}, len(providerState.activeAuthIDs))
	for _, authID := range providerState.activeAuthIDs {
		if _, ok := preferredIDs[authID]; ok {
			allowed[authID] = struct{}{}
		}
	}
	m.mu.Unlock()

	if len(allowed) == 0 {
		return nil
	}
	filtered := make([]*Auth, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate == nil {
			continue
		}
		if _, ok := allowed[candidate.ID]; ok {
			filtered = append(filtered, candidate)
		}
	}
	return filtered
}

func (m *ScopedPoolManager) MarkSelected(auth *Auth) {
	if m == nil || auth == nil {
		return
	}
	now := time.Now()
	m.mu.Lock()
	providerKey, previousProvider := m.upsertAuthLocked(auth)
	if previousProvider != "" && previousProvider != providerKey {
		m.refreshProviderLocked(previousProvider, now, nil)
	}
	if providerKey == "" {
		m.mu.Unlock()
		return
	}
	if providerState := m.providers[providerKey]; providerState != nil {
		if state := providerState.auths[auth.ID]; state != nil {
			state.lastSelectedAt = now
		}
	}
	m.refreshProviderLocked(providerKey, now, nil)
	m.mu.Unlock()
}

func (m *ScopedPoolManager) MarkResult(auth *Auth, result Result) {
	if m == nil || auth == nil || strings.TrimSpace(result.AuthID) == "" {
		return
	}
	now := time.Now()
	m.mu.Lock()
	providerKey, previousProvider := m.upsertAuthLocked(auth)
	if previousProvider != "" && previousProvider != providerKey {
		m.refreshProviderLocked(previousProvider, now, nil)
	}
	if providerKey == "" {
		m.mu.Unlock()
		return
	}
	providerState := m.providers[providerKey]
	if providerState == nil {
		m.mu.Unlock()
		return
	}
	state := providerState.auths[result.AuthID]
	if state == nil {
		m.mu.Unlock()
		return
	}

	resolvedCfg, configured, effective, _ := m.resolveProviderLocked(providerKey)
	if result.Success {
		state.consecutiveErrors = 0
		state.recentTimeoutCount = 0
		if configured && effective && state.inPool && state.reason != PoolReasonLowQuota {
			m.setStateLocked(state, true, PoolStateInPool, PoolReasonHealthy, now)
		}
		m.refreshProviderLocked(providerKey, now, nil)
		m.mu.Unlock()
		return
	}

	if !configured || !effective || !shouldCountScopedPoolFailure(result.Error) {
		m.refreshProviderLocked(providerKey, now, nil)
		m.mu.Unlock()
		return
	}

	state.consecutiveErrors++
	timeoutFailure := isScopedPoolTimeoutError(result.Error)
	if timeoutFailure {
		state.recentTimeoutCount++
	}

	if state.consecutiveErrors >= resolvedCfg.ConsecutiveErrorThreshold {
		state.penaltyScore++
		state.penaltyUntil = now.Add(time.Duration(resolvedCfg.PenaltyWindowSeconds) * time.Second)
		state.consecutiveErrors = 0
		state.inPool = false
		reason := PoolReasonConsecutiveErrors
		if timeoutFailure {
			reason = PoolReasonRequestTimeout
		}
		m.setStateLocked(state, false, PoolStateEjected, reason, now)
	}

	m.refreshProviderLocked(providerKey, now, nil)
	m.mu.Unlock()
}

func (m *ScopedPoolManager) ApplyQuotaCheck(authID string, result QuotaCheckResult) {
	if m == nil {
		return
	}
	authID = strings.TrimSpace(authID)
	if authID == "" {
		return
	}
	now := time.Now()
	m.mu.Lock()
	providerKey := m.authProviders[authID]
	if providerKey == "" {
		m.mu.Unlock()
		return
	}
	providerState := m.providers[providerKey]
	if providerState == nil {
		m.mu.Unlock()
		return
	}
	state := providerState.auths[authID]
	if state == nil {
		m.mu.Unlock()
		return
	}

	if result.Classification == ClassificationUnsupported {
		state.supportsQuotaCheck = false
		state.remainingPercent = nil
		state.lastQuotaCheckedAt = now
		m.refreshProviderLocked(providerKey, now, nil)
		m.mu.Unlock()
		return
	}

	state.supportsQuotaCheck = true
	state.lastQuotaCheckedAt = now
	if result.RemainingPercent != nil {
		remaining := *result.RemainingPercent
		state.remainingPercent = &remaining
	}

	resolvedCfg, configured, effective, _ := m.resolveProviderLocked(providerKey)
	if configured && effective {
		if result.Exhausted || (state.remainingPercent != nil && *state.remainingPercent < resolvedCfg.QuotaThresholdPercent) {
			state.inPool = false
			m.setStateLocked(state, false, PoolStateEjected, PoolReasonLowQuota, now)
		}
	}

	m.refreshProviderLocked(providerKey, now, nil)
	m.mu.Unlock()
}

func (m *ScopedPoolManager) Snapshot() PoolSnapshot {
	if m == nil {
		return PoolSnapshot{
			GeneratedAt: time.Now(),
			Strategy:    "round-robin",
			Providers:   map[string]PoolProviderSnapshot{},
			Auths:       map[string]PoolAuthSnapshot{},
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	snapshot := PoolSnapshot{
		GeneratedAt: time.Now(),
		Strategy:    m.runtime.strategy,
		Providers:   make(map[string]PoolProviderSnapshot),
		Auths:       make(map[string]PoolAuthSnapshot),
	}

	for providerKey, providerCfg := range m.runtime.providers {
		providerState := m.providers[providerKey]
		providerSnapshot := m.buildProviderSnapshotLocked(providerKey, providerState, providerCfg)
		snapshot.Providers[providerKey] = providerSnapshot
		for authID, authSnapshot := range providerSnapshot.Auths {
			snapshot.Auths[authID] = authSnapshot
		}
	}
	for providerKey, providerState := range m.providers {
		if _, exists := snapshot.Providers[providerKey]; exists {
			continue
		}
		providerSnapshot := m.buildProviderSnapshotLocked(providerKey, providerState, internalconfig.RoutingScopedPoolProviderConfig{})
		snapshot.Providers[providerKey] = providerSnapshot
		for authID, authSnapshot := range providerSnapshot.Auths {
			snapshot.Auths[authID] = authSnapshot
		}
	}

	return snapshot
}

func (m *ScopedPoolManager) buildProviderSnapshotLocked(providerKey string, providerState *scopedPoolProviderState, providerCfg internalconfig.RoutingScopedPoolProviderConfig) PoolProviderSnapshot {
	_, configured, effective, reason := m.resolveProviderLocked(providerKey)
	snapshot := PoolProviderSnapshot{
		Provider:   providerKey,
		Configured: configured,
		Effective:  effective,
		Reason:     reason,
		Limit:      providerCfg.Limit,
		Auths:      make(map[string]PoolAuthSnapshot),
	}
	if effective && snapshot.Limit <= 0 {
		snapshot.Limit = m.runtime.defaults.Limit
	}
	if providerState == nil {
		return snapshot
	}
	snapshot.ActiveAuthIDs = append(snapshot.ActiveAuthIDs, providerState.activeAuthIDs...)
	for authID, state := range providerState.auths {
		if state == nil {
			continue
		}
		authSnapshot := PoolAuthSnapshot{
			AuthID:             authID,
			AuthIndex:          state.authIndex,
			Provider:           state.providerKey,
			Configured:         configured,
			PoolEnabled:        effective,
			InPool:             state.inPool,
			State:              state.state,
			Reason:             state.reason,
			RuntimeOnly:        state.runtimeOnly,
			Disabled:           state.disabled,
			SupportsQuotaCheck: state.supportsQuotaCheck,
			RemainingPercent:   copyOptionalInt(state.remainingPercent),
			LastQuotaCheckedAt: state.lastQuotaCheckedAt,
			ConsecutiveErrors:  state.consecutiveErrors,
			RecentTimeoutCount: state.recentTimeoutCount,
			PenaltyScore:       state.penaltyScore,
			PenaltyUntil:       state.penaltyUntil,
			LastSelectedAt:     state.lastSelectedAt,
			LastPoolEventAt:    state.lastPoolEventAt,
			LastTransitionAt:   state.lastTransitionAt,
		}
		snapshot.Auths[authID] = authSnapshot
		snapshot.CandidateCount++
		switch authSnapshot.State {
		case PoolStateInPool:
			snapshot.ActiveCount++
		case PoolStateStandby:
			snapshot.StandbyCount++
		case PoolStatePenalized:
			snapshot.PenalizedCount++
		case PoolStateEjected:
			snapshot.EjectedCount++
		case PoolStateDisabled:
			snapshot.DisabledCount++
		}
	}
	return snapshot
}

func copyOptionalInt(value *int) *int {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}

func (m *ScopedPoolManager) upsertAuthLocked(auth *Auth) (string, string) {
	if auth == nil {
		return "", ""
	}
	authID := strings.TrimSpace(auth.ID)
	if authID == "" {
		return "", ""
	}
	auth.EnsureIndex()
	providerKey := scopedPoolProviderKey(auth)
	previousProvider := m.authProviders[authID]
	if providerKey == "" {
		if previousProvider != "" {
			delete(m.authProviders, authID)
			if providerState := m.providers[previousProvider]; providerState != nil {
				delete(providerState.auths, authID)
			}
		}
		return "", previousProvider
	}

	if previousProvider != "" && previousProvider != providerKey {
		if providerState := m.providers[previousProvider]; providerState != nil {
			delete(providerState.auths, authID)
		}
	}
	m.authProviders[authID] = providerKey

	providerState := m.ensureProviderLocked(providerKey)
	state := providerState.auths[authID]
	if state == nil {
		state = &scopedPoolAuthState{
			authID:      authID,
			providerKey: providerKey,
			state:       PoolStateUnmanaged,
			reason:      PoolReasonNotEnabled,
		}
		providerState.auths[authID] = state
	}

	state.providerKey = providerKey
	state.authIndex = strings.TrimSpace(auth.Index)
	state.priority = authPriority(auth)
	state.disabled = auth.Disabled || auth.Status == StatusDisabled
	state.unavailable = auth.Unavailable
	state.runtimeOnly = isRuntimeOnlyAuth(auth)
	if m.quotaSupports != nil {
		state.supportsQuotaCheck = m.quotaSupports(auth)
	}
	if state.disabled {
		state.inPool = false
	}

	return providerKey, previousProvider
}

func (m *ScopedPoolManager) ensureProviderLocked(providerKey string) *scopedPoolProviderState {
	if m.providers == nil {
		m.providers = make(map[string]*scopedPoolProviderState)
	}
	providerState := m.providers[providerKey]
	if providerState == nil {
		providerState = &scopedPoolProviderState{
			providerKey: providerKey,
			auths:       make(map[string]*scopedPoolAuthState),
		}
		m.providers[providerKey] = providerState
	}
	return providerState
}

func (m *ScopedPoolManager) resolveProviderLocked(providerKey string) (internalconfig.RoutingScopedPoolProviderConfig, bool, bool, PoolReason) {
	cfg := m.runtime.defaults
	providerCfg, ok := m.runtime.providers[providerKey]
	if ok {
		cfg = providerCfg
	}
	configured := ok && providerCfg.Enabled
	if !configured {
		return cfg, false, false, PoolReasonNotEnabled
	}
	if !m.runtime.enabled {
		return cfg, true, false, PoolReasonNotEnabled
	}
	if m.runtime.strategy != "round-robin" {
		return cfg, true, false, PoolReasonStrategyIncompatible
	}
	return cfg, true, true, PoolReasonHealthy
}

func (m *ScopedPoolManager) refreshProviderLocked(providerKey string, now time.Time, preferredIDs map[string]struct{}) {
	providerState := m.providers[providerKey]
	if providerState == nil {
		return
	}
	resolvedCfg, configured, effective, inactiveReason := m.resolveProviderLocked(providerKey)
	activeBefore := make(map[string]struct{}, len(providerState.activeAuthIDs))
	for _, authID := range providerState.activeAuthIDs {
		activeBefore[authID] = struct{}{}
	}

	if !configured || !effective {
		providerState.activeAuthIDs = nil
		for _, state := range providerState.auths {
			if state == nil {
				continue
			}
			switch {
			case state.disabled:
				m.setStateLocked(state, false, PoolStateDisabled, PoolReasonDisabled, now)
			default:
				m.setStateLocked(state, false, PoolStateUnmanaged, inactiveReason, now)
			}
		}
		m.logProviderRefreshLocked(providerState, resolvedCfg, now)
		return
	}

	eligible := make([]*scopedPoolAuthState, 0, len(providerState.auths))
	hasPreferredActive := false
	for _, state := range providerState.auths {
		if state == nil {
			continue
		}
		switch {
		case state.disabled:
			m.setStateLocked(state, false, PoolStateDisabled, PoolReasonDisabled, now)
			continue
		case state.unavailable:
			m.setStateLocked(state, false, PoolStateEjected, PoolReasonUnavailable, now)
			continue
		case state.penaltyUntil.After(now):
			reason := state.reason
			if reason != PoolReasonConsecutiveErrors && reason != PoolReasonRequestTimeout {
				reason = PoolReasonPenaltyWindow
			}
			m.setStateLocked(state, false, PoolStatePenalized, reason, now)
			continue
		case state.supportsQuotaCheck && state.remainingPercent != nil && *state.remainingPercent < resolvedCfg.QuotaThresholdPercent:
			m.setStateLocked(state, false, PoolStateEjected, PoolReasonLowQuota, now)
			continue
		default:
			eligible = append(eligible, state)
			if preferredIDs != nil {
				if _, preferred := preferredIDs[state.authID]; preferred {
					if _, active := activeBefore[state.authID]; active {
						hasPreferredActive = true
					}
				}
			}
		}
	}

	sort.Slice(eligible, func(i, j int) bool {
		left := eligible[i]
		right := eligible[j]
		if preferredIDs != nil && !hasPreferredActive {
			_, leftPreferred := preferredIDs[left.authID]
			_, rightPreferred := preferredIDs[right.authID]
			if leftPreferred != rightPreferred {
				return leftPreferred
			}
		}
		_, leftActive := activeBefore[left.authID]
		_, rightActive := activeBefore[right.authID]
		if leftActive != rightActive {
			return leftActive
		}
		if left.priority != right.priority {
			return left.priority > right.priority
		}
		if left.penaltyScore != right.penaltyScore {
			return left.penaltyScore < right.penaltyScore
		}
		if left.lastSelectedAt.IsZero() != right.lastSelectedAt.IsZero() {
			return left.lastSelectedAt.IsZero()
		}
		if !left.lastSelectedAt.Equal(right.lastSelectedAt) {
			return left.lastSelectedAt.Before(right.lastSelectedAt)
		}
		return left.authID < right.authID
	})

	limit := resolvedCfg.Limit
	if limit <= 0 {
		limit = internalconfig.DefaultScopedPoolLimit
	}
	if limit > len(eligible) {
		limit = len(eligible)
	}

	selectedIDs := make([]string, 0, limit)
	selected := make(map[string]struct{}, limit)
	for _, state := range eligible[:limit] {
		selected[state.authID] = struct{}{}
		selectedIDs = append(selectedIDs, state.authID)
		m.setStateLocked(state, true, PoolStateInPool, PoolReasonHealthy, now)
	}
	for _, state := range eligible[limit:] {
		m.setStateLocked(state, false, PoolStateStandby, PoolReasonPoolFull, now)
	}

	providerState.activeAuthIDs = selectedIDs
	m.logProviderRefreshLocked(providerState, resolvedCfg, now)
}

func (m *ScopedPoolManager) setStateLocked(state *scopedPoolAuthState, inPool bool, poolState PoolState, reason PoolReason, now time.Time) {
	if state == nil {
		return
	}
	if state.inPool == inPool && state.state == poolState && state.reason == reason {
		return
	}
	state.inPool = inPool
	state.state = poolState
	state.reason = reason
	state.lastTransitionAt = now
	state.lastPoolEventAt = now
}

func (m *ScopedPoolManager) logProviderRefreshLocked(providerState *scopedPoolProviderState, providerCfg internalconfig.RoutingScopedPoolProviderConfig, now time.Time) {
	if providerState == nil {
		return
	}
	activeIDs := append([]string(nil), providerState.activeAuthIDs...)
	sort.Strings(activeIDs)
	counts := struct {
		active    int
		standby   int
		penalized int
		ejected   int
		disabled  int
	}{}
	for _, state := range providerState.auths {
		if state == nil {
			continue
		}
		switch state.state {
		case PoolStateInPool:
			counts.active++
		case PoolStateStandby:
			counts.standby++
		case PoolStatePenalized:
			counts.penalized++
		case PoolStateEjected:
			counts.ejected++
		case PoolStateDisabled:
			counts.disabled++
		}
	}
	digest := strings.Join(activeIDs, ",") +
		"|active=" + itoa(counts.active) +
		"|standby=" + itoa(counts.standby) +
		"|penalized=" + itoa(counts.penalized) +
		"|ejected=" + itoa(counts.ejected) +
		"|disabled=" + itoa(counts.disabled)
	if digest != providerState.lastDigest {
		providerState.lastDigest = digest
		providerState.lastLogAt = now
		log.WithFields(log.Fields{
			"provider":        providerState.providerKey,
			"limit":           providerCfg.Limit,
			"active_count":    counts.active,
			"standby_count":   counts.standby,
			"penalized_count": counts.penalized,
			"ejected_count":   counts.ejected,
			"disabled_count":  counts.disabled,
			"active_auth_ids": activeIDs,
		}).Info("scoped pool refreshed")
		return
	}
	throttle := time.Duration(providerCfg.IdleLogThrottleSeconds) * time.Second
	if throttle <= 0 {
		throttle = time.Duration(internalconfig.DefaultScopedPoolIdleLogSec) * time.Second
	}
	if providerState.lastLogAt.IsZero() || now.Sub(providerState.lastLogAt) >= throttle {
		providerState.lastLogAt = now
		log.WithFields(log.Fields{
			"provider":        providerState.providerKey,
			"limit":           providerCfg.Limit,
			"active_count":    counts.active,
			"standby_count":   counts.standby,
			"penalized_count": counts.penalized,
			"ejected_count":   counts.ejected,
			"disabled_count":  counts.disabled,
			"active_auth_ids": activeIDs,
		}).Info("scoped pool unchanged")
	}
}

func scopedPoolProviderKey(auth *Auth) string {
	if auth == nil {
		return ""
	}
	if auth.Attributes != nil {
		if providerKey := strings.TrimSpace(auth.Attributes["provider_key"]); providerKey != "" {
			return strings.ToLower(providerKey)
		}
		if compatName := strings.TrimSpace(auth.Attributes["compat_name"]); compatName != "" {
			return strings.ToLower(compatName)
		}
	}
	return strings.ToLower(strings.TrimSpace(auth.Provider))
}

func shouldCountScopedPoolFailure(err *Error) bool {
	if err == nil {
		return false
	}
	if isRequestScopedNotFoundResultError(err) || isModelSupportResultError(err) {
		return false
	}
	switch statusCodeFromResult(err) {
	case 401, 402, 403, 408, 429, 500, 502, 503, 504:
		return true
	default:
		return false
	}
}

func isScopedPoolTimeoutError(err *Error) bool {
	if err == nil {
		return false
	}
	if statusCodeFromResult(err) == 408 {
		return true
	}
	lower := strings.ToLower(strings.TrimSpace(err.Message))
	return strings.Contains(lower, "timeout") || strings.Contains(lower, "deadline exceeded")
}

func itoa(value int) string {
	return strconv.Itoa(value)
}
