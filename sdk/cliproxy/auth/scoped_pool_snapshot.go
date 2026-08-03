package auth

import "time"

// ScopedPoolSnapshot returns the provider-local scoped-pool runtime status.
func (m *Manager) ScopedPoolSnapshot() PoolSnapshot {
	if m == nil || m.scheduler == nil {
		return PoolSnapshot{
			GeneratedAt: time.Now(),
			Strategy:    "round-robin",
			Providers:   map[string]PoolProviderSnapshot{},
			Auths:       map[string]PoolAuthSnapshot{},
		}
	}
	return m.scheduler.scopedPoolSnapshot()
}
