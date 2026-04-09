package auth

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	internalconfig "github.com/router-for-me/CLIProxyAPI/v6/internal/config"
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
