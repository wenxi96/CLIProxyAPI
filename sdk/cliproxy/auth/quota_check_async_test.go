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
			AutoDisableAuthFileOnZeroQuota: true,
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
			AutoDisableAuthFileOnZeroQuota: true,
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
			AutoDisableAuthFileOnZeroQuota: true,
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
	saved := store.lastAuth.Load()
	if saved == nil || !saved.Disabled {
		t.Fatalf("expected persisted auth to be disabled, got %#v", saved)
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

func TestMarkResult_DoesNotEnqueueQuotaCheckForTransientStreamError(t *testing.T) {
	checker := &quotaCheckerStub{}
	mgr := NewManager(nil, nil, nil)
	mgr.SetQuotaChecker(checker)
	mgr.SetConfig(&internalconfig.Config{
		QuotaExceeded: internalconfig.QuotaExceeded{
			AutoDisableAuthFileOnZeroQuota: true,
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
			AutoDisableAuthFileOnZeroQuota:           true,
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
			AutoDisableAuthFileOnZeroQuota:           true,
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
			AutoDisableAuthFileOnZeroQuota:           true,
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
			AutoDisableAuthFileOnZeroQuota:           true,
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
			AutoDisableAuthFileOnZeroQuota:           true,
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
			AutoDisableAuthFileOnZeroQuota:           true,
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
			AutoDisableAuthFileOnZeroQuota:           true,
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
			AutoDisableAuthFileOnZeroQuota:           true,
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
			AutoDisableAuthFileOnZeroQuota:           true,
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
