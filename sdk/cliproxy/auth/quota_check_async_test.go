package auth

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	internalconfig "github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

type quotaCheckerStub struct {
	callCount atomic.Int32
	started   chan struct{}
	release   chan struct{}
	result    QuotaCheckResult
	err       error
}

func (s *quotaCheckerStub) Supports(auth *Auth) bool {
	return auth != nil && auth.Provider == "codex"
}

func (s *quotaCheckerStub) Check(ctx context.Context, auth *Auth) (QuotaCheckResult, error) {
	s.callCount.Add(1)
	if s.started != nil {
		select {
		case s.started <- struct{}{}:
		default:
		}
	}
	if s.release != nil {
		select {
		case <-s.release:
		case <-ctx.Done():
			return QuotaCheckResult{}, ctx.Err()
		}
	}
	return s.result, s.err
}

type snapshotStore struct {
	saveCount atomic.Int32
	lastAuth  atomic.Pointer[Auth]
}

func (s *snapshotStore) List(context.Context) ([]*Auth, error) { return nil, nil }

func (s *snapshotStore) Save(_ context.Context, auth *Auth) (string, error) {
	s.saveCount.Add(1)
	s.lastAuth.Store(auth.Clone())
	return "", nil
}

func (s *snapshotStore) Delete(context.Context, string) error { return nil }

func TestMarkResult_EnqueuesQuotaCheckAsynchronously(t *testing.T) {
	checker := &quotaCheckerStub{
		started: make(chan struct{}, 1),
		release: make(chan struct{}),
	}
	mgr := NewManager(nil, nil, nil)
	mgr.SetQuotaChecker(checker)
	mgr.SetConfig(&internalconfig.Config{
		QuotaExceeded: internalconfig.QuotaExceeded{
			AutoDisableAuthFileOnLowQuota: true,
		},
	})

	auth := &Auth{
		ID:       "auth-1",
		Provider: "codex",
		Status:   StatusActive,
		Metadata: map[string]any{
			"chatgpt_account_id": "acct-1",
			"access_token":       "token-1",
		},
	}
	if _, err := mgr.Register(WithSkipPersist(context.Background()), auth); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	done := make(chan struct{})
	go func() {
		mgr.MarkResult(context.Background(), Result{
			AuthID:   auth.ID,
			Provider: auth.Provider,
			Model:    "gpt-5",
			Success:  false,
			Error:    &Error{HTTPStatus: 429, Message: "quota exceeded"},
		})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("MarkResult() blocked on quota check")
	}

	select {
	case <-checker.started:
	case <-time.After(2 * time.Second):
		t.Fatal("quota check did not start")
	}

	close(checker.release)
}

func TestMarkResult_DeduplicatesConcurrentQuotaChecksPerAuth(t *testing.T) {
	checker := &quotaCheckerStub{
		started: make(chan struct{}, 1),
		release: make(chan struct{}),
	}
	mgr := NewManager(nil, nil, nil)
	mgr.SetQuotaChecker(checker)
	mgr.SetConfig(&internalconfig.Config{
		QuotaExceeded: internalconfig.QuotaExceeded{
			AutoDisableAuthFileOnLowQuota: true,
		},
	})

	auth := &Auth{
		ID:       "auth-1",
		Provider: "codex",
		Status:   StatusActive,
		Metadata: map[string]any{
			"chatgpt_account_id": "acct-1",
			"access_token":       "token-1",
		},
	}
	if _, err := mgr.Register(WithSkipPersist(context.Background()), auth); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	failResult := Result{
		AuthID:   auth.ID,
		Provider: auth.Provider,
		Model:    "gpt-5",
		Success:  false,
		Error:    &Error{HTTPStatus: 429, Message: "quota exceeded"},
	}
	mgr.MarkResult(context.Background(), failResult)
	mgr.MarkResult(context.Background(), failResult)

	select {
	case <-checker.started:
	case <-time.After(2 * time.Second):
		t.Fatal("quota check did not start")
	}

	time.Sleep(150 * time.Millisecond)
	if got := checker.callCount.Load(); got != 1 {
		t.Fatalf("expected one in-flight quota check, got %d", got)
	}

	close(checker.release)
}

func TestMarkResult_AutoDisablesAuthAfterConfirmedZeroQuota(t *testing.T) {
	store := &snapshotStore{}
	checker := &quotaCheckerStub{
		result: QuotaCheckResult{
			Exhausted:        true,
			Classification:   ClassificationNoQuota,
			RemainingPercent: intPtr(0),
		},
	}
	mgr := NewManager(store, nil, nil)
	mgr.SetQuotaChecker(checker)
	mgr.SetConfig(&internalconfig.Config{
		QuotaExceeded: internalconfig.QuotaExceeded{
			AutoDisableAuthFileOnLowQuota: true,
		},
	})

	auth := &Auth{
		ID:       "auth-1",
		Provider: "codex",
		Status:   StatusActive,
		Metadata: map[string]any{
			"chatgpt_account_id": "acct-1",
			"access_token":       "token-1",
		},
	}
	if _, err := mgr.Register(WithSkipPersist(context.Background()), auth); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	mgr.MarkResult(context.Background(), Result{
		AuthID:   auth.ID,
		Provider: auth.Provider,
		Model:    "gpt-5",
		Success:  false,
		Error:    &Error{HTTPStatus: 429, Message: "quota exceeded"},
	})

	deadline := time.Now().Add(2 * time.Second)
	for {
		current, ok := mgr.GetByID(auth.ID)
		if ok && current != nil && current.Disabled {
			if current.Status != StatusDisabled {
				t.Fatalf("expected disabled status, got %q", current.Status)
			}
			if current.StatusMessage != autoDisabledQuotaStatusMessage {
				t.Fatalf("expected status message %q, got %q", autoDisabledQuotaStatusMessage, current.StatusMessage)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("auth was not auto disabled in time")
		}
		time.Sleep(20 * time.Millisecond)
	}

	if got := store.saveCount.Load(); got == 0 {
		t.Fatal("expected auto-disable to trigger persistence")
	}
	waitForPersistedDisabledAuth(t, store)
}

func TestApplyQuotaCheckResultReturnsFalseWhenAutoDisableDisabled(t *testing.T) {
	mgr := NewManager(nil, nil, nil)
	mgr.SetConfig(&internalconfig.Config{})

	auth := &Auth{
		ID:       "auth-apply-result-disabled-config",
		Provider: "codex",
		Status:   StatusActive,
		Metadata: map[string]any{
			"chatgpt_account_id": "acct-1",
			"access_token":       "token-1",
		},
	}
	if _, err := mgr.Register(WithSkipPersist(context.Background()), auth); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	disabled := mgr.ApplyQuotaCheckResult(auth.ID, QuotaCheckResult{
		Exhausted:        true,
		Classification:   ClassificationNoQuota,
		RemainingPercent: intPtr(0),
	})
	if disabled {
		t.Fatal("ApplyQuotaCheckResult() reported disabled while auto-disable config is off")
	}
	current, ok := mgr.GetByID(auth.ID)
	if !ok || current == nil {
		t.Fatal("expected auth to remain registered")
	}
	if current.Disabled {
		t.Fatal("auth should not be disabled when auto-disable config is off")
	}
}

func TestMarkResult_DoesNotEnqueueQuotaCheckWhenConfigDisabled(t *testing.T) {
	checker := &quotaCheckerStub{}
	mgr := NewManager(nil, nil, nil)
	mgr.SetQuotaChecker(checker)
	mgr.SetConfig(&internalconfig.Config{})

	auth := &Auth{
		ID:       "auth-1",
		Provider: "codex",
		Status:   StatusActive,
		Metadata: map[string]any{
			"chatgpt_account_id": "acct-1",
			"access_token":       "token-1",
		},
	}
	if _, err := mgr.Register(WithSkipPersist(context.Background()), auth); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	mgr.MarkResult(context.Background(), Result{
		AuthID:   auth.ID,
		Provider: auth.Provider,
		Model:    "gpt-5",
		Success:  false,
		Error:    &Error{HTTPStatus: 429, Message: "quota exceeded"},
	})

	time.Sleep(150 * time.Millisecond)
	if got := checker.callCount.Load(); got != 0 {
		t.Fatalf("expected no quota checks when config disabled, got %d", got)
	}
}

func TestMarkResult_DoesNotEnqueueQuotaCheckAfterSuccessfulRequestWhenThresholdEnabled(t *testing.T) {
	checker := &quotaCheckerStub{}
	mgr := NewManager(nil, nil, nil)
	mgr.SetQuotaChecker(checker)
	mgr.SetConfig(&internalconfig.Config{
		QuotaExceeded: internalconfig.QuotaExceeded{
			AutoDisableAuthFileOnLowQuota:            true,
			AutoDisableAuthFileQuotaThresholdPercent: 10,
		},
	})

	auth := &Auth{
		ID:       "auth-success-threshold",
		Provider: "codex",
		Status:   StatusActive,
		Metadata: map[string]any{
			"chatgpt_account_id": "acct-1",
			"access_token":       "token-1",
		},
	}
	if _, err := mgr.Register(WithSkipPersist(context.Background()), auth); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	mgr.MarkResult(context.Background(), Result{
		AuthID:   auth.ID,
		Provider: auth.Provider,
		Model:    "gpt-5",
		Success:  true,
	})

	time.Sleep(150 * time.Millisecond)
	if got := checker.callCount.Load(); got != 0 {
		t.Fatalf("expected successful request not to trigger quota checks, got %d", got)
	}
}

func TestMarkResult_ActiveQuotaRefreshTouchesSuccessfulRuntimeAuth(t *testing.T) {
	checker := &quotaCheckerStub{
		started: make(chan struct{}, 1),
		result: QuotaCheckResult{
			Classification:   ClassificationOK,
			RemainingPercent: intPtr(41),
		},
	}
	mgr := NewManager(nil, nil, nil)
	mgr.SetQuotaChecker(checker)
	mgr.SetConfig(&internalconfig.Config{
		QuotaExceeded: internalconfig.QuotaExceeded{
			AutoDisableAuthFileOnLowQuota:            true,
			AutoDisableAuthFileQuotaThresholdPercent: 40,
			ActiveQuotaRefresh: internalconfig.ActiveQuotaRefreshConfig{
				Enabled:             true,
				ScanIntervalSeconds: 1,
				ActiveTTLSeconds:    60,
				Workers:             1,
			},
		},
	})
	defer mgr.StopAutoRefresh()

	auth := &Auth{
		ID:       "auth-active-quota-refresh-success",
		Provider: "codex",
		Status:   StatusActive,
		Metadata: map[string]any{
			"chatgpt_account_id": "acct-1",
			"access_token":       "token-1",
		},
	}
	if _, err := mgr.Register(WithSkipPersist(context.Background()), auth); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	mgr.MarkResult(context.Background(), Result{
		AuthID:   auth.ID,
		Provider: auth.Provider,
		Model:    "gpt-5",
		Success:  true,
	})

	if got := checker.callCount.Load(); got != 0 {
		t.Fatalf("successful request should only touch active pool synchronously, got %d calls", got)
	}

	mgr.activeQuotaMu.Lock()
	pool := mgr.activeQuotaPool
	mgr.activeQuotaMu.Unlock()
	if pool == nil {
		t.Fatal("expected active quota refresh pool")
	}
	if _, ok := pool.snapshot(auth.ID); !ok {
		t.Fatal("expected successful runtime auth to be touched into active pool")
	}

	mgr.scanActiveQuotaRefresh(context.Background(), pool, time.Now(), 1)
	select {
	case <-checker.started:
	case <-time.After(2 * time.Second):
		t.Fatal("active quota refresh did not start")
	}
	if got := checker.callCount.Load(); got != 1 {
		t.Fatalf("quota checks = %d, want 1", got)
	}
}

func TestMarkResult_ActiveQuotaRefreshDisabledDoesNotTouchPool(t *testing.T) {
	checker := &quotaCheckerStub{
		started: make(chan struct{}, 1),
		result: QuotaCheckResult{
			Classification:   ClassificationOK,
			RemainingPercent: intPtr(41),
		},
	}
	mgr := NewManager(nil, nil, nil)
	mgr.SetQuotaChecker(checker)
	mgr.SetConfig(&internalconfig.Config{
		QuotaExceeded: internalconfig.QuotaExceeded{
			AutoDisableAuthFileOnLowQuota:            true,
			AutoDisableAuthFileQuotaThresholdPercent: 40,
			ActiveQuotaRefresh: internalconfig.ActiveQuotaRefreshConfig{
				Enabled:             false,
				ScanIntervalSeconds: 1,
				ActiveTTLSeconds:    60,
				Workers:             1,
			},
		},
	})
	defer mgr.StopAutoRefresh()

	auth := &Auth{
		ID:       "auth-active-quota-refresh-disabled",
		Provider: "codex",
		Status:   StatusActive,
		Metadata: map[string]any{
			"chatgpt_account_id": "acct-1",
			"access_token":       "token-1",
		},
	}
	if _, err := mgr.Register(WithSkipPersist(context.Background()), auth); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	mgr.MarkResult(context.Background(), Result{
		AuthID:   auth.ID,
		Provider: auth.Provider,
		Model:    "gpt-5",
		Success:  true,
	})

	mgr.activeQuotaMu.Lock()
	pool := mgr.activeQuotaPool
	cancel := mgr.activeQuotaCancel
	mgr.activeQuotaMu.Unlock()
	if pool != nil || cancel != nil {
		t.Fatal("active quota refresh pool should not start when disabled")
	}
	if got := checker.callCount.Load(); got != 0 {
		t.Fatalf("quota checks = %d, want 0", got)
	}
}

func TestActiveQuotaRefreshRemovesAuthAfterThresholdAutoDisable(t *testing.T) {
	store := &snapshotStore{}
	checker := &quotaCheckerStub{
		started: make(chan struct{}, 1),
		result: QuotaCheckResult{
			Classification:   ClassificationOK,
			RemainingPercent: intPtr(40),
		},
	}
	mgr := NewManager(store, nil, nil)
	mgr.SetQuotaChecker(checker)
	mgr.SetConfig(&internalconfig.Config{
		QuotaExceeded: internalconfig.QuotaExceeded{
			AutoDisableAuthFileOnLowQuota:            true,
			AutoDisableAuthFileQuotaThresholdPercent: 40,
			ActiveQuotaRefresh: internalconfig.ActiveQuotaRefreshConfig{
				Enabled:             true,
				ScanIntervalSeconds: 30,
				ActiveTTLSeconds:    600,
				Workers:             1,
			},
		},
	})
	defer mgr.StopAutoRefresh()

	auth := &Auth{
		ID:       "auth-active-quota-refresh-threshold",
		Provider: "codex",
		Status:   StatusActive,
		Metadata: map[string]any{
			"chatgpt_account_id": "acct-1",
			"access_token":       "token-1",
		},
	}
	if _, err := mgr.Register(WithSkipPersist(context.Background()), auth); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	mgr.MarkResult(context.Background(), Result{
		AuthID:   auth.ID,
		Provider: auth.Provider,
		Model:    "gpt-5",
		Success:  true,
	})

	mgr.activeQuotaMu.Lock()
	pool := mgr.activeQuotaPool
	mgr.activeQuotaMu.Unlock()
	if pool == nil {
		t.Fatal("expected active quota refresh pool")
	}
	mgr.scanActiveQuotaRefresh(context.Background(), pool, time.Now(), 1)
	select {
	case <-checker.started:
	case <-time.After(2 * time.Second):
		t.Fatal("active quota refresh did not start")
	}

	waitForDisabledAuth(t, mgr, auth.ID, autoDisabledQuotaThresholdStatusMessage)
	if _, ok := pool.snapshot(auth.ID); ok {
		t.Fatal("expected disabled auth to be removed from active quota refresh pool")
	}
	waitForStoreSave(t, store)
}

func TestActiveQuotaRefreshUpdatesScopedPoolQuotaSnapshot(t *testing.T) {
	checker := &quotaCheckerStub{
		started: make(chan struct{}, 1),
		result: QuotaCheckResult{
			Classification:   ClassificationOK,
			RemainingPercent: intPtr(45),
		},
	}
	mgr := NewManager(nil, &RoundRobinSelector{}, nil)
	mgr.SetQuotaChecker(checker)
	mgr.SetConfig(&internalconfig.Config{
		QuotaExceeded: internalconfig.QuotaExceeded{
			AutoDisableAuthFileOnLowQuota:            true,
			AutoDisableAuthFileQuotaThresholdPercent: 40,
			ActiveQuotaRefresh: internalconfig.ActiveQuotaRefreshConfig{
				Enabled:             true,
				ScanIntervalSeconds: 30,
				ActiveTTLSeconds:    600,
				Workers:             1,
			},
		},
		Routing: internalconfig.RoutingConfig{
			Strategy: "round-robin",
			ScopedPool: internalconfig.RoutingScopedPoolConfig{
				Providers: map[string]internalconfig.RoutingScopedPoolProviderConfig{
					"codex": {Enabled: true, Limit: 1, QuotaThresholdPercent: 50},
				},
			},
		},
	})
	defer mgr.StopAutoRefresh()

	auth := &Auth{
		ID:       "auth-active-quota-refresh-scoped-pool",
		Provider: "codex",
		Status:   StatusActive,
		Metadata: map[string]any{
			"chatgpt_account_id": "acct-1",
			"access_token":       "token-1",
		},
	}
	if _, err := mgr.Register(WithSkipPersist(context.Background()), auth); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	mgr.MarkResult(context.Background(), Result{
		AuthID:   auth.ID,
		Provider: auth.Provider,
		Model:    "gpt-5",
		Success:  true,
	})

	mgr.activeQuotaMu.Lock()
	pool := mgr.activeQuotaPool
	mgr.activeQuotaMu.Unlock()
	if pool == nil {
		t.Fatal("expected active quota refresh pool")
	}
	mgr.scanActiveQuotaRefresh(context.Background(), pool, time.Now(), 1)
	select {
	case <-checker.started:
	case <-time.After(2 * time.Second):
		t.Fatal("active quota refresh did not start")
	}

	poolAuth := waitForScopedPoolAuthState(t, mgr, auth.ID, PoolStateEjected, PoolReasonLowQuota)
	if poolAuth.State != PoolStateEjected {
		t.Fatalf("scoped-pool state = %q, want %q", poolAuth.State, PoolStateEjected)
	}
	if poolAuth.Reason != PoolReasonLowQuota {
		t.Fatalf("scoped-pool reason = %q, want %q", poolAuth.Reason, PoolReasonLowQuota)
	}
	current, ok := mgr.GetByID(auth.ID)
	if !ok || current == nil {
		t.Fatal("expected auth to remain registered")
	}
	if current.Disabled {
		t.Fatal("auth should not be disabled when active refresh result is above auto-disable threshold")
	}
}

func TestActiveQuotaRefreshRestartsWhenScanIntervalChanges(t *testing.T) {
	checker := &quotaCheckerStub{
		result: QuotaCheckResult{
			Classification:   ClassificationOK,
			RemainingPercent: intPtr(80),
		},
	}
	mgr := NewManager(nil, nil, nil)
	mgr.SetQuotaChecker(checker)
	mgr.SetConfig(&internalconfig.Config{
		QuotaExceeded: internalconfig.QuotaExceeded{
			ActiveQuotaRefresh: internalconfig.ActiveQuotaRefreshConfig{
				Enabled:             true,
				ScanIntervalSeconds: 30,
				ActiveTTLSeconds:    600,
				Workers:             1,
			},
		},
	})
	defer mgr.StopAutoRefresh()

	mgr.activeQuotaMu.Lock()
	firstPool := mgr.activeQuotaPool
	firstScan := mgr.activeQuotaScan
	mgr.activeQuotaMu.Unlock()
	if firstPool == nil {
		t.Fatal("expected initial active quota refresh pool")
	}
	if firstScan != 30*time.Second {
		t.Fatalf("initial scan interval = %v, want 30s", firstScan)
	}

	mgr.SetConfig(&internalconfig.Config{
		QuotaExceeded: internalconfig.QuotaExceeded{
			ActiveQuotaRefresh: internalconfig.ActiveQuotaRefreshConfig{
				Enabled:             true,
				ScanIntervalSeconds: 60,
				ActiveTTLSeconds:    600,
				Workers:             1,
			},
		},
	})

	mgr.activeQuotaMu.Lock()
	secondPool := mgr.activeQuotaPool
	secondScan := mgr.activeQuotaScan
	mgr.activeQuotaMu.Unlock()
	if secondPool == nil {
		t.Fatal("expected active quota refresh pool after config update")
	}
	if secondPool == firstPool {
		t.Fatal("expected active quota refresh pool to restart after scan interval changed")
	}
	if secondScan != time.Minute {
		t.Fatalf("updated scan interval = %v, want 1m", secondScan)
	}
}

func TestMarkResult_DoesNotEnqueueQuotaCheckForTransientStreamError(t *testing.T) {
	checker := &quotaCheckerStub{}
	mgr := NewManager(nil, nil, nil)
	mgr.SetQuotaChecker(checker)
	mgr.SetConfig(&internalconfig.Config{
		QuotaExceeded: internalconfig.QuotaExceeded{
			AutoDisableAuthFileOnLowQuota: true,
		},
	})

	auth := &Auth{
		ID:       "auth-1",
		Provider: "codex",
		Status:   StatusActive,
		Metadata: map[string]any{
			"chatgpt_account_id": "acct-1",
			"access_token":       "token-1",
		},
	}
	if _, err := mgr.Register(WithSkipPersist(context.Background()), auth); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	mgr.MarkResult(context.Background(), Result{
		AuthID:   auth.ID,
		Provider: auth.Provider,
		Model:    "gpt-5",
		Success:  false,
		Error:    &Error{HTTPStatus: 408, Message: "stream disconnected before completion"},
	})

	time.Sleep(150 * time.Millisecond)
	if got := checker.callCount.Load(); got != 0 {
		t.Fatalf("expected no quota checks for transient stream errors, got %d", got)
	}
}

func intPtr(v int) *int {
	return &v
}

func TestMarkResult_AutoDisablesAuthOnThresholdHit(t *testing.T) {
	store := &snapshotStore{}
	checker := &quotaCheckerStub{
		result: QuotaCheckResult{
			Exhausted:        false,
			Classification:   ClassificationOK,
			RemainingPercent: intPtr(8),
		},
	}
	mgr := NewManager(store, nil, nil)
	mgr.SetQuotaChecker(checker)
	mgr.SetConfig(&internalconfig.Config{
		QuotaExceeded: internalconfig.QuotaExceeded{
			AutoDisableAuthFileOnLowQuota:            true,
			AutoDisableAuthFileQuotaThresholdPercent: 10,
		},
	})

	auth := &Auth{
		ID:       "auth-threshold",
		Provider: "codex",
		Status:   StatusActive,
		Metadata: map[string]any{
			"chatgpt_account_id": "acct-1",
			"access_token":       "token-1",
		},
	}
	if _, err := mgr.Register(WithSkipPersist(context.Background()), auth); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	mgr.MarkResult(context.Background(), Result{
		AuthID:   auth.ID,
		Provider: auth.Provider,
		Model:    "gpt-5",
		Success:  false,
		Error:    &Error{HTTPStatus: 429, Message: "quota exceeded"},
	})

	deadline := time.Now().Add(2 * time.Second)
	for {
		current, ok := mgr.GetByID(auth.ID)
		if ok && current != nil && current.Disabled {
			if current.StatusMessage != autoDisabledQuotaThresholdStatusMessage {
				t.Fatalf("expected status message %q, got %q", autoDisabledQuotaThresholdStatusMessage, current.StatusMessage)
			}
			if current.Quota.Reason != "quota_threshold" {
				t.Fatalf("expected quota reason %q, got %q", "quota_threshold", current.Quota.Reason)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("auth was not auto disabled in time")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestMarkResult_DoesNotDisableOnThresholdWhenAboveThreshold(t *testing.T) {
	store := &snapshotStore{}
	checker := &quotaCheckerStub{
		result: QuotaCheckResult{
			Exhausted:        false,
			Classification:   ClassificationOK,
			RemainingPercent: intPtr(15),
		},
	}
	mgr := NewManager(store, nil, nil)
	mgr.SetQuotaChecker(checker)
	mgr.SetConfig(&internalconfig.Config{
		QuotaExceeded: internalconfig.QuotaExceeded{
			AutoDisableAuthFileOnLowQuota:            true,
			AutoDisableAuthFileQuotaThresholdPercent: 10,
		},
	})

	auth := &Auth{
		ID:       "auth-above-threshold",
		Provider: "codex",
		Status:   StatusActive,
		Metadata: map[string]any{
			"chatgpt_account_id": "acct-1",
			"access_token":       "token-1",
		},
	}
	if _, err := mgr.Register(WithSkipPersist(context.Background()), auth); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	mgr.MarkResult(context.Background(), Result{
		AuthID:   auth.ID,
		Provider: auth.Provider,
		Model:    "gpt-5",
		Success:  false,
		Error:    &Error{HTTPStatus: 429, Message: "quota exceeded"},
	})

	waitForQuotaCheckCalls(t, checker, 1)
	waitForQuotaCheckIdle(t, mgr, auth.ID)
	time.Sleep(300 * time.Millisecond)
	current, ok := mgr.GetByID(auth.ID)
	if ok && current != nil && current.Disabled {
		t.Fatal("auth should not be disabled when remaining percent is above threshold")
	}
}

func TestMarkResult_DoesNotDisableOnThresholdWhenRemainingPercentNil(t *testing.T) {
	store := &snapshotStore{}
	checker := &quotaCheckerStub{
		result: QuotaCheckResult{
			Exhausted:        false,
			Classification:   ClassificationOK,
			RemainingPercent: nil,
		},
	}
	mgr := NewManager(store, nil, nil)
	mgr.SetQuotaChecker(checker)
	mgr.SetConfig(&internalconfig.Config{
		QuotaExceeded: internalconfig.QuotaExceeded{
			AutoDisableAuthFileOnLowQuota:            true,
			AutoDisableAuthFileQuotaThresholdPercent: 10,
		},
	})

	auth := &Auth{
		ID:       "auth-nil-percent",
		Provider: "codex",
		Status:   StatusActive,
		Metadata: map[string]any{
			"chatgpt_account_id": "acct-1",
			"access_token":       "token-1",
		},
	}
	if _, err := mgr.Register(WithSkipPersist(context.Background()), auth); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	mgr.MarkResult(context.Background(), Result{
		AuthID:   auth.ID,
		Provider: auth.Provider,
		Model:    "gpt-5",
		Success:  false,
		Error:    &Error{HTTPStatus: 429, Message: "quota exceeded"},
	})

	waitForQuotaCheckCalls(t, checker, 1)
	waitForQuotaCheckIdle(t, mgr, auth.ID)
	time.Sleep(300 * time.Millisecond)
	current, ok := mgr.GetByID(auth.ID)
	if ok && current != nil && current.Disabled {
		t.Fatal("auth should not be disabled when remaining percent is nil and not exhausted")
	}
}

func TestMarkResult_DisablesOnZeroThresholdOnlyWhenExhausted(t *testing.T) {
	store := &snapshotStore{}
	checker := &quotaCheckerStub{
		result: QuotaCheckResult{
			Exhausted:        false,
			Classification:   ClassificationOK,
			RemainingPercent: intPtr(0),
		},
	}
	mgr := NewManager(store, nil, nil)
	mgr.SetQuotaChecker(checker)
	mgr.SetConfig(&internalconfig.Config{
		QuotaExceeded: internalconfig.QuotaExceeded{
			AutoDisableAuthFileOnLowQuota:            true,
			AutoDisableAuthFileQuotaThresholdPercent: 0, // Zero threshold = legacy behavior
		},
	})

	auth := &Auth{
		ID:       "auth-zero-threshold",
		Provider: "codex",
		Status:   StatusActive,
		Metadata: map[string]any{
			"chatgpt_account_id": "acct-1",
			"access_token":       "token-1",
		},
	}
	if _, err := mgr.Register(WithSkipPersist(context.Background()), auth); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	mgr.MarkResult(context.Background(), Result{
		AuthID:   auth.ID,
		Provider: auth.Provider,
		Model:    "gpt-5",
		Success:  false,
		Error:    &Error{HTTPStatus: 429, Message: "quota exceeded"},
	})

	waitForQuotaCheckCalls(t, checker, 1)
	waitForQuotaCheckIdle(t, mgr, auth.ID)
	time.Sleep(300 * time.Millisecond)
	current, ok := mgr.GetByID(auth.ID)
	if ok && current != nil && current.Disabled {
		t.Fatal("auth should not be disabled with zero threshold when not exhausted")
	}
}

func TestShouldAutoDisable(t *testing.T) {
	tests := []struct {
		name      string
		result    QuotaCheckResult
		threshold int
		want      bool
		reason    string
	}{
		{
			name:      "exhausted returns exhausted reason",
			result:    QuotaCheckResult{Exhausted: true, RemainingPercent: intPtr(0)},
			threshold: 10,
			want:      true,
			reason:    "exhausted",
		},
		{
			name:      "exhausted takes priority over threshold",
			result:    QuotaCheckResult{Exhausted: true, RemainingPercent: intPtr(5)},
			threshold: 10,
			want:      true,
			reason:    "exhausted",
		},
		{
			name:      "threshold hit when remaining equals threshold",
			result:    QuotaCheckResult{Exhausted: false, RemainingPercent: intPtr(10)},
			threshold: 10,
			want:      true,
			reason:    "threshold",
		},
		{
			name:      "threshold hit when remaining below threshold",
			result:    QuotaCheckResult{Exhausted: false, RemainingPercent: intPtr(5)},
			threshold: 10,
			want:      true,
			reason:    "threshold",
		},
		{
			name:      "no disable when remaining above threshold",
			result:    QuotaCheckResult{Exhausted: false, RemainingPercent: intPtr(15)},
			threshold: 10,
			want:      false,
			reason:    "",
		},
		{
			name:      "no disable when threshold is zero and not exhausted",
			result:    QuotaCheckResult{Exhausted: false, RemainingPercent: intPtr(0)},
			threshold: 0,
			want:      false,
			reason:    "",
		},
		{
			name:      "no disable when remaining percent is nil and not exhausted",
			result:    QuotaCheckResult{Exhausted: false, RemainingPercent: nil},
			threshold: 10,
			want:      false,
			reason:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, reason := shouldAutoDisable(tt.result, tt.threshold)
			if got != tt.want {
				t.Errorf("shouldAutoDisable() = %v, want %v", got, tt.want)
			}
			if reason != tt.reason {
				t.Errorf("shouldAutoDisable() reason = %q, want %q", reason, tt.reason)
			}
		})
	}
}

func TestEffectiveAutoDisableThresholdClampsRuntimeConfig(t *testing.T) {
	tests := []struct {
		name string
		cfg  *internalconfig.Config
		want int
	}{
		{name: "nil config", cfg: nil, want: 0},
		{
			name: "negative threshold clamps to zero",
			cfg: &internalconfig.Config{QuotaExceeded: internalconfig.QuotaExceeded{
				AutoDisableAuthFileQuotaThresholdPercent: -5,
			}},
			want: 0,
		},
		{
			name: "in range threshold",
			cfg: &internalconfig.Config{QuotaExceeded: internalconfig.QuotaExceeded{
				AutoDisableAuthFileQuotaThresholdPercent: 10,
			}},
			want: 10,
		},
		{
			name: "high threshold clamps to max",
			cfg: &internalconfig.Config{QuotaExceeded: internalconfig.QuotaExceeded{
				AutoDisableAuthFileQuotaThresholdPercent: 99,
			}},
			want: internalconfig.MaxAutoDisableQuotaThresholdPercent,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := effectiveAutoDisableThreshold(tt.cfg); got != tt.want {
				t.Fatalf("effectiveAutoDisableThreshold() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestMarkResult_UsesUpdatedThresholdConfigForNextQuotaCheck(t *testing.T) {
	store := &snapshotStore{}
	checker := &quotaCheckerStub{
		result: QuotaCheckResult{
			Exhausted:        false,
			Classification:   ClassificationOK,
			RemainingPercent: intPtr(8),
		},
	}
	mgr := NewManager(store, nil, nil)
	mgr.SetQuotaChecker(checker)
	mgr.SetConfig(&internalconfig.Config{
		QuotaExceeded: internalconfig.QuotaExceeded{
			AutoDisableAuthFileOnLowQuota:            true,
			AutoDisableAuthFileQuotaThresholdPercent: 0,
		},
	})

	auth := &Auth{
		ID:       "auth-dynamic-threshold",
		Provider: "codex",
		Status:   StatusActive,
		Metadata: map[string]any{
			"chatgpt_account_id": "acct-1",
			"access_token":       "token-1",
		},
	}
	if _, err := mgr.Register(WithSkipPersist(context.Background()), auth); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	failResult := Result{
		AuthID:   auth.ID,
		Provider: auth.Provider,
		Model:    "gpt-5",
		Success:  false,
		Error:    &Error{HTTPStatus: 429, Message: "quota exceeded"},
	}
	mgr.MarkResult(context.Background(), failResult)
	waitForQuotaCheckCalls(t, checker, 1)
	waitForQuotaCheckIdle(t, mgr, auth.ID)

	current, ok := mgr.GetByID(auth.ID)
	if ok && current != nil && current.Disabled {
		t.Fatal("auth should not be disabled before threshold is enabled")
	}

	mgr.SetConfig(&internalconfig.Config{
		QuotaExceeded: internalconfig.QuotaExceeded{
			AutoDisableAuthFileOnLowQuota:            true,
			AutoDisableAuthFileQuotaThresholdPercent: 10,
		},
	})
	mgr.MarkResult(context.Background(), failResult)

	waitForDisabledAuth(t, mgr, auth.ID, autoDisabledQuotaThresholdStatusMessage)
	waitForQuotaCheckCalls(t, checker, 2)
}

func TestMarkResult_DeduplicatesConcurrentThresholdQuotaChecks(t *testing.T) {
	checker := &quotaCheckerStub{
		started: make(chan struct{}, 1),
		release: make(chan struct{}),
		result: QuotaCheckResult{
			Exhausted:        false,
			Classification:   ClassificationOK,
			RemainingPercent: intPtr(8),
		},
	}
	mgr := NewManager(nil, nil, nil)
	mgr.SetQuotaChecker(checker)
	mgr.SetConfig(&internalconfig.Config{
		QuotaExceeded: internalconfig.QuotaExceeded{
			AutoDisableAuthFileOnLowQuota:            true,
			AutoDisableAuthFileQuotaThresholdPercent: 10,
		},
	})

	auth := &Auth{
		ID:       "auth-concurrent-threshold",
		Provider: "codex",
		Status:   StatusActive,
		Metadata: map[string]any{
			"chatgpt_account_id": "acct-1",
			"access_token":       "token-1",
		},
	}
	if _, err := mgr.Register(WithSkipPersist(context.Background()), auth); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	failResult := Result{
		AuthID:   auth.ID,
		Provider: auth.Provider,
		Model:    "gpt-5",
		Success:  false,
		Error:    &Error{HTTPStatus: 429, Message: "quota exceeded"},
	}
	mgr.MarkResult(context.Background(), failResult)
	mgr.MarkResult(context.Background(), failResult)

	select {
	case <-checker.started:
	case <-time.After(2 * time.Second):
		t.Fatal("quota check did not start")
	}

	time.Sleep(150 * time.Millisecond)
	if got := checker.callCount.Load(); got != 1 {
		t.Fatalf("expected one in-flight quota check, got %d", got)
	}
	close(checker.release)

	waitForDisabledAuth(t, mgr, auth.ID, autoDisabledQuotaThresholdStatusMessage)
	if got := checker.callCount.Load(); got != 1 {
		t.Fatalf("expected one quota check after disable, got %d", got)
	}
}

func TestMarkResult_AutoDisableThresholdAppliesWhenFillFirstDisablesScopedPool(t *testing.T) {
	store := &snapshotStore{}
	checker := &quotaCheckerStub{
		result: QuotaCheckResult{
			Exhausted:        false,
			Classification:   ClassificationOK,
			RemainingPercent: intPtr(8),
		},
	}
	mgr := NewManager(store, &FillFirstSelector{}, nil)
	mgr.SetQuotaChecker(checker)
	mgr.SetConfig(&internalconfig.Config{
		QuotaExceeded: internalconfig.QuotaExceeded{
			AutoDisableAuthFileOnLowQuota:            true,
			AutoDisableAuthFileQuotaThresholdPercent: 10,
		},
		Routing: internalconfig.RoutingConfig{
			Strategy: "fill-first",
			ScopedPool: internalconfig.RoutingScopedPoolConfig{
				Providers: map[string]internalconfig.RoutingScopedPoolProviderConfig{
					"codex": {Enabled: true, Limit: 1, QuotaThresholdPercent: 20},
				},
			},
		},
	})

	auth := &Auth{
		ID:       "auth-fill-first-threshold",
		Provider: "codex",
		Status:   StatusActive,
		Metadata: map[string]any{
			"chatgpt_account_id": "acct-1",
			"access_token":       "token-1",
		},
	}
	if _, err := mgr.Register(WithSkipPersist(context.Background()), auth); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	snapshot := mgr.ScopedPoolSnapshot()
	if snapshot.Providers["codex"].Effective {
		t.Fatal("scoped-pool should not be effective under fill-first")
	}

	mgr.MarkResult(context.Background(), Result{
		AuthID:   auth.ID,
		Provider: auth.Provider,
		Model:    "gpt-5",
		Success:  false,
		Error:    &Error{HTTPStatus: 429, Message: "quota exceeded"},
	})

	waitForDisabledAuth(t, mgr, auth.ID, autoDisabledQuotaThresholdStatusMessage)
	if got := store.saveCount.Load(); got == 0 {
		t.Fatal("expected threshold auto-disable to trigger persistence under fill-first")
	}
}

func TestMarkResult_AutoDisableThresholdTakesPriorityOverScopedPoolLowQuota(t *testing.T) {
	checker := &quotaCheckerStub{
		result: QuotaCheckResult{
			Exhausted:        false,
			Classification:   ClassificationOK,
			RemainingPercent: intPtr(8),
		},
	}
	mgr := NewManager(nil, &RoundRobinSelector{}, nil)
	mgr.SetQuotaChecker(checker)
	mgr.SetConfig(&internalconfig.Config{
		QuotaExceeded: internalconfig.QuotaExceeded{
			AutoDisableAuthFileOnLowQuota:            true,
			AutoDisableAuthFileQuotaThresholdPercent: 10,
		},
		Routing: internalconfig.RoutingConfig{
			Strategy: "round-robin",
			ScopedPool: internalconfig.RoutingScopedPoolConfig{
				Providers: map[string]internalconfig.RoutingScopedPoolProviderConfig{
					"codex": {Enabled: true, Limit: 1, QuotaThresholdPercent: 20},
				},
			},
		},
	})

	auth := &Auth{
		ID:       "auth-round-robin-threshold",
		Provider: "codex",
		Status:   StatusActive,
		Metadata: map[string]any{
			"chatgpt_account_id": "acct-1",
			"access_token":       "token-1",
		},
	}
	if _, err := mgr.Register(WithSkipPersist(context.Background()), auth); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	mgr.MarkResult(context.Background(), Result{
		AuthID:   auth.ID,
		Provider: auth.Provider,
		Model:    "gpt-5",
		Success:  false,
		Error:    &Error{HTTPStatus: 429, Message: "quota exceeded"},
	})

	waitForDisabledAuth(t, mgr, auth.ID, autoDisabledQuotaThresholdStatusMessage)
	snapshot := mgr.ScopedPoolSnapshot()
	poolAuth := snapshot.Auths[auth.ID]
	if poolAuth.State != PoolStateDisabled {
		t.Fatalf("scoped-pool state = %q, want %q", poolAuth.State, PoolStateDisabled)
	}
	if poolAuth.Reason != PoolReasonDisabled {
		t.Fatalf("scoped-pool reason = %q, want %q", poolAuth.Reason, PoolReasonDisabled)
	}
}

func waitForQuotaCheckCalls(t *testing.T, checker *quotaCheckerStub, want int32) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if checker.callCount.Load() >= want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("quota check call count = %d, want at least %d", checker.callCount.Load(), want)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func waitForQuotaCheckIdle(t *testing.T, mgr *Manager, authID string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		mgr.quotaCheckMu.Lock()
		_, pending := mgr.quotaCheckPending[authID]
		_, running := mgr.quotaCheckRunning[authID]
		mgr.quotaCheckMu.Unlock()
		if !pending && !running {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("quota check did not become idle in time")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func waitForDisabledAuth(t *testing.T, mgr *Manager, authID, wantStatusMessage string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		current, ok := mgr.GetByID(authID)
		if ok && current != nil && current.Disabled {
			if current.StatusMessage != wantStatusMessage {
				t.Fatalf("status message = %q, want %q", current.StatusMessage, wantStatusMessage)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("auth was not auto disabled in time")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func waitForStoreSave(t *testing.T, store *snapshotStore) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if store.saveCount.Load() > 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("expected persistence save to run in time")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func waitForPersistedDisabledAuth(t *testing.T, store *snapshotStore) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		saved := store.lastAuth.Load()
		if saved != nil && saved.Disabled {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected persisted auth to be disabled, got %#v", saved)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func waitForScopedPoolAuthState(t *testing.T, mgr *Manager, authID string, wantState PoolState, wantReason PoolReason) PoolAuthSnapshot {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		snapshot := mgr.ScopedPoolSnapshot()
		poolAuth, ok := snapshot.Auths[authID]
		if ok && poolAuth.State == wantState && poolAuth.Reason == wantReason {
			return poolAuth
		}
		if time.Now().After(deadline) {
			if ok {
				t.Fatalf("scoped-pool auth state = %q/%q, want %q/%q", poolAuth.State, poolAuth.Reason, wantState, wantReason)
			}
			t.Fatalf("scoped-pool auth %q not found", authID)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
