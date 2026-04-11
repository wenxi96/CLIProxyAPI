package auth

import (
	"context"
	"net/http"
	"strconv"
	"testing"

	internalconfig "github.com/router-for-me/CLIProxyAPI/v6/internal/config"
)

func testScopedPoolAuth(id, provider string, priority int) *Auth {
	auth := &Auth{
		ID:       id,
		Provider: provider,
		Status:   StatusActive,
		Attributes: map[string]string{
			"path": "/tmp/" + id + ".json",
		},
	}
	if priority != 0 {
		auth.Attributes["priority"] = strconv.Itoa(priority)
	}
	auth.EnsureIndex()
	return auth
}

func TestScopedPoolManager_FilterCandidates_ProviderLocalAndEnabledOnly(t *testing.T) {
	manager := newScopedPoolManager()
	manager.SetConfig("round-robin", internalconfig.RoutingScopedPoolConfig{
		Providers: map[string]internalconfig.RoutingScopedPoolProviderConfig{
			"codex": {Enabled: true, Limit: 1},
		},
	})

	codexA := testScopedPoolAuth("codex-a", "codex", 0)
	codexB := testScopedPoolAuth("codex-b", "codex", 0)
	claudeA := testScopedPoolAuth("claude-a", "claude", 0)
	claudeB := testScopedPoolAuth("claude-b", "claude", 0)

	manager.SyncAuth(codexA)
	manager.SyncAuth(codexB)
	manager.SyncAuth(claudeA)
	manager.SyncAuth(claudeB)

	filteredCodex := manager.FilterCandidates("codex", []*Auth{codexA, codexB})
	if len(filteredCodex) != 1 {
		t.Fatalf("expected 1 codex candidate, got %d", len(filteredCodex))
	}

	filteredClaude := manager.FilterCandidates("claude", []*Auth{claudeA, claudeB})
	if len(filteredClaude) != 2 {
		t.Fatalf("expected scoped-pool bypass for claude, got %d candidates", len(filteredClaude))
	}

	snapshot := manager.Snapshot()
	codexProvider := snapshot.Providers["codex"]
	if !codexProvider.Effective {
		t.Fatalf("expected codex scoped-pool effective")
	}
	if codexProvider.ActiveCount != 1 || codexProvider.StandbyCount != 1 {
		t.Fatalf("unexpected codex counts: active=%d standby=%d", codexProvider.ActiveCount, codexProvider.StandbyCount)
	}
	claudeProvider := snapshot.Providers["claude"]
	if claudeProvider.Effective {
		t.Fatalf("expected claude scoped-pool disabled")
	}
}

func TestScopedPoolManager_MarkResult_EjectsAfterThresholdAndRefills(t *testing.T) {
	manager := newScopedPoolManager()
	manager.SetConfig("round-robin", internalconfig.RoutingScopedPoolConfig{
		Providers: map[string]internalconfig.RoutingScopedPoolProviderConfig{
			"codex": {
				Enabled:                   true,
				Limit:                     1,
				ConsecutiveErrorThreshold: 2,
				PenaltyWindowSeconds:      300,
			},
		},
	})

	codexA := testScopedPoolAuth("codex-a", "codex", 2)
	codexB := testScopedPoolAuth("codex-b", "codex", 1)
	manager.SyncAuth(codexA)
	manager.SyncAuth(codexB)
	manager.MarkSelected(codexA)

	failResult := Result{
		AuthID:   codexA.ID,
		Provider: "codex",
		Model:    "gpt-5-codex",
		Success:  false,
		Error:    &Error{HTTPStatus: http.StatusGatewayTimeout, Message: "upstream timeout"},
	}
	manager.MarkResult(codexA, failResult)
	manager.MarkResult(codexA, failResult)

	filtered := manager.FilterCandidates("codex", []*Auth{codexA, codexB})
	if len(filtered) != 1 || filtered[0].ID != codexB.ID {
		t.Fatalf("expected codex-b to replace penalized codex-a, got %+v", filtered)
	}

	snapshot := manager.Snapshot()
	authA := snapshot.Auths[codexA.ID]
	if authA.State != PoolStatePenalized {
		t.Fatalf("expected codex-a penalized, got %s", authA.State)
	}
	if authA.Reason != PoolReasonRequestTimeout {
		t.Fatalf("expected timeout reason, got %s", authA.Reason)
	}
	authB := snapshot.Auths[codexB.ID]
	if !authB.InPool || authB.State != PoolStateInPool {
		t.Fatalf("expected codex-b in pool, got state=%s in_pool=%v", authB.State, authB.InPool)
	}
}

func TestScopedPoolManager_ApplyQuotaCheck_EjectsLowQuota(t *testing.T) {
	manager := newScopedPoolManager()
	manager.SetQuotaSupportEvaluator(func(auth *Auth) bool {
		return auth != nil && auth.Provider == "codex"
	})
	manager.SetConfig("round-robin", internalconfig.RoutingScopedPoolConfig{
		Providers: map[string]internalconfig.RoutingScopedPoolProviderConfig{
			"codex": {
				Enabled:               true,
				Limit:                 1,
				QuotaThresholdPercent: 10,
			},
		},
	})

	codexA := testScopedPoolAuth("codex-a", "codex", 2)
	codexB := testScopedPoolAuth("codex-b", "codex", 1)
	manager.SyncAuth(codexA)
	manager.SyncAuth(codexB)

	remaining := 5
	manager.ApplyQuotaCheck(codexA.ID, QuotaCheckResult{
		Classification:   ClassificationNoQuota,
		RemainingPercent: &remaining,
		Exhausted:        false,
	})

	filtered := manager.FilterCandidates("codex", []*Auth{codexA, codexB})
	if len(filtered) != 1 || filtered[0].ID != codexB.ID {
		t.Fatalf("expected low-quota codex-a to be ejected, got %+v", filtered)
	}

	snapshot := manager.Snapshot()
	authA := snapshot.Auths[codexA.ID]
	if authA.Reason != PoolReasonLowQuota {
		t.Fatalf("expected low_quota reason, got %s", authA.Reason)
	}
}

func TestScopedPoolManager_FillFirstDisablesEffectivePool(t *testing.T) {
	manager := newScopedPoolManager()
	manager.SetConfig("fill-first", internalconfig.RoutingScopedPoolConfig{
		Providers: map[string]internalconfig.RoutingScopedPoolProviderConfig{
			"codex": {Enabled: true, Limit: 1},
		},
	})

	codexA := testScopedPoolAuth("codex-a", "codex", 0)
	codexB := testScopedPoolAuth("codex-b", "codex", 0)
	manager.SyncAuth(codexA)
	manager.SyncAuth(codexB)

	filtered := manager.FilterCandidates("codex", []*Auth{codexA, codexB})
	if len(filtered) != 2 {
		t.Fatalf("expected fill-first to bypass scoped-pool, got %d candidates", len(filtered))
	}

	snapshot := manager.Snapshot()
	provider := snapshot.Providers["codex"]
	if provider.Effective {
		t.Fatalf("expected scoped-pool ineffective under fill-first")
	}
	if provider.Reason != PoolReasonStrategyIncompatible {
		t.Fatalf("expected strategy_incompatible reason, got %s", provider.Reason)
	}
}

func TestScopedPoolManager_GlobalDisablePreservesConfiguredButNotEffective(t *testing.T) {
	manager := newScopedPoolManager()
	disabled := false
	manager.SetConfig("round-robin", internalconfig.RoutingScopedPoolConfig{
		Enabled: &disabled,
		Providers: map[string]internalconfig.RoutingScopedPoolProviderConfig{
			"codex": {Enabled: true, Limit: 1},
		},
	})

	codexA := testScopedPoolAuth("codex-a", "codex", 0)
	codexB := testScopedPoolAuth("codex-b", "codex", 0)
	manager.SyncAuth(codexA)
	manager.SyncAuth(codexB)

	filtered := manager.FilterCandidates("codex", []*Auth{codexA, codexB})
	if len(filtered) != 2 {
		t.Fatalf("expected global disable to bypass scoped-pool, got %d candidates", len(filtered))
	}

	snapshot := manager.Snapshot()
	provider := snapshot.Providers["codex"]
	if !provider.Configured {
		t.Fatalf("expected codex provider to remain configured")
	}
	if provider.Effective {
		t.Fatalf("expected codex provider to be ineffective when scoped-pool is globally disabled")
	}
	if provider.Reason != PoolReasonNotEnabled {
		t.Fatalf("expected not_enabled reason, got %s", provider.Reason)
	}
}

func TestManagerScopedPoolSnapshot_IsAccessible(t *testing.T) {
	manager := NewManager(nil, nil, nil)
	manager.SetConfig(&internalconfig.Config{
		Routing: internalconfig.RoutingConfig{
			Strategy: "round-robin",
			ScopedPool: internalconfig.RoutingScopedPoolConfig{
				Providers: map[string]internalconfig.RoutingScopedPoolProviderConfig{
					"codex": {Enabled: true, Limit: 1},
				},
			},
		},
	})
	auth := testScopedPoolAuth("codex-a", "codex", 0)
	if _, err := manager.Register(context.Background(), auth); err != nil {
		t.Fatalf("register auth: %v", err)
	}
	snapshot := manager.ScopedPoolSnapshot()
	if !snapshot.Providers["codex"].Effective {
		t.Fatalf("expected manager scoped-pool snapshot to include effective codex provider")
	}
}

func TestAuthSchedulerRebuild_RemovesScopedPoolStaleAuths(t *testing.T) {
	scheduler := newAuthScheduler(nil)
	scheduler.setScopedPoolConfig("round-robin", internalconfig.RoutingScopedPoolConfig{
		Providers: map[string]internalconfig.RoutingScopedPoolProviderConfig{
			"codex": {Enabled: true, Limit: 1},
		},
	})

	codexA := testScopedPoolAuth("codex-a", "codex", 2)
	codexB := testScopedPoolAuth("codex-b", "codex", 1)

	scheduler.rebuild([]*Auth{codexA, codexB})
	initial := scheduler.scopedPoolSnapshot()
	if initial.Providers["codex"].CandidateCount != 2 {
		t.Fatalf("expected initial candidate_count=2, got %d", initial.Providers["codex"].CandidateCount)
	}

	scheduler.rebuild([]*Auth{codexA})
	rebuilt := scheduler.scopedPoolSnapshot()
	if rebuilt.Providers["codex"].CandidateCount != 1 {
		t.Fatalf("expected rebuilt candidate_count=1, got %d", rebuilt.Providers["codex"].CandidateCount)
	}
	if _, exists := rebuilt.Auths[codexB.ID]; exists {
		t.Fatalf("expected stale auth %s to be removed from scoped-pool snapshot", codexB.ID)
	}
}
