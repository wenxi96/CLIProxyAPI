package auth

import (
	"strings"
	"time"

	internalconfig "github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

// RuntimeConfigState is an opaque in-memory snapshot of manager state changed
// by a runtime config hook. It deliberately excludes persistence operations so
// rollback cannot repeat a blocked or failed cooldown-store write.
type RuntimeConfigState struct {
	selector                  Selector
	cooldownStore             CooldownStateStore
	pendingCooldownStateStore CooldownStateStore
	runtimeConfig             *internalconfig.Config
	requestRetry              int32
	maxRetryCredentials       int32
	maxRetryInterval          int64
	oauthModelAlias           *oauthModelAliasTable
	cooldownRecords           []CooldownStateRecord
}

// SnapshotRuntimeConfigState captures the manager fields mutated by a runtime
// config apply. The returned value is immutable and can be restored after any
// later hook failure without invoking external persistence.
func (m *Manager) SnapshotRuntimeConfigState() *RuntimeConfigState {
	if m == nil {
		return nil
	}

	m.configCooldownMu.Lock()
	m.mu.RLock()
	selector := m.selector
	cooldownStore := m.cooldownStore
	pendingCooldownStateStore := m.pendingCooldownStateStore
	m.mu.RUnlock()
	runtimeConfig, _ := m.runtimeConfig.Load().(*internalconfig.Config)
	oauthModelAlias, _ := m.oauthModelAlias.Load().(*oauthModelAliasTable)
	snapshot := &RuntimeConfigState{
		selector:                  selector,
		cooldownStore:             cooldownStore,
		pendingCooldownStateStore: pendingCooldownStateStore,
		runtimeConfig:             runtimeConfig.CloneForRuntime(),
		requestRetry:              m.requestRetry.Load(),
		maxRetryCredentials:       m.maxRetryCredentials.Load(),
		maxRetryInterval:          m.maxRetryInterval.Load(),
		oauthModelAlias:           oauthModelAlias,
		cooldownRecords:           m.cooldownStateRecordsSnapshot(),
	}
	m.configCooldownMu.Unlock()
	return snapshot
}

// RestoreRuntimeConfigState restores only in-memory manager state. It does not
// call CooldownStateStore.Save or any other external side effect.
func (m *Manager) RestoreRuntimeConfigState(snapshot *RuntimeConfigState) {
	if m == nil || snapshot == nil {
		return
	}

	selector := snapshot.selector
	if selector == nil {
		selector = &RoundRobinSelector{}
	}
	runtimeConfig := snapshot.runtimeConfig.CloneForRuntime()
	if runtimeConfig == nil {
		runtimeConfig = &internalconfig.Config{}
	}
	oauthModelAlias := snapshot.oauthModelAlias
	if oauthModelAlias == nil {
		oauthModelAlias = &oauthModelAliasTable{}
	}

	m.configCooldownMu.Lock()
	m.runtimeConfig.Store(runtimeConfig)
	m.requestRetry.Store(snapshot.requestRetry)
	m.maxRetryCredentials.Store(snapshot.maxRetryCredentials)
	m.maxRetryInterval.Store(snapshot.maxRetryInterval)
	m.oauthModelAlias.Store(oauthModelAlias)
	m.mu.Lock()
	m.selector = selector
	m.cooldownStore = snapshot.cooldownStore
	m.pendingCooldownStateStore = snapshot.pendingCooldownStateStore
	m.mu.Unlock()
	m.configCooldownMu.Unlock()

	m.restoreRuntimeConfigCooldownRecords(snapshot.cooldownRecords)
	if m.scheduler != nil {
		m.scheduler.setSelector(selector)
		m.scheduler.setScopedPoolConfig(runtimeConfig.Routing.Strategy, runtimeConfig.Routing.ScopedPool)
		m.syncScheduler()
	}
	m.rebuildAPIKeyModelAliasFromRuntimeConfig()
	m.reconcileActiveQuotaRefresh()
}

func (m *Manager) restoreRuntimeConfigCooldownRecords(records []CooldownStateRecord) {
	if m == nil || len(records) == 0 {
		return
	}

	now := time.Now()
	authLevelRecords := make([]CooldownStateRecord, 0)
	snapshotsByID := make(map[string]*Auth)

	m.mu.Lock()
	for _, record := range records {
		if strings.TrimSpace(record.Model) == "" {
			authLevelRecords = append(authLevelRecords, record)
			continue
		}
		if m.restoreCooldownRecordLocked(record, now) {
			if auth := m.auths[strings.TrimSpace(record.AuthID)]; auth != nil {
				snapshotsByID[auth.ID] = auth.Clone()
			}
		}
	}
	for _, record := range authLevelRecords {
		if m.restoreCooldownRecordLocked(record, now) {
			if auth := m.auths[strings.TrimSpace(record.AuthID)]; auth != nil {
				snapshotsByID[auth.ID] = auth.Clone()
			}
		}
	}
	m.mu.Unlock()

	if m.scheduler != nil {
		for _, snapshot := range snapshotsByID {
			m.scheduler.upsertAuth(snapshot)
		}
	}
}
