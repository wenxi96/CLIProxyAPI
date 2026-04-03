package auth

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

type countingStore struct {
	saveCount atomic.Int32
}

func (s *countingStore) List(context.Context) ([]*Auth, error) { return nil, nil }

func (s *countingStore) Save(context.Context, *Auth) (string, error) {
	s.saveCount.Add(1)
	return "", nil
}

func (s *countingStore) Delete(context.Context, string) error { return nil }

type blockingStore struct {
	countingStore
	saveStarted chan struct{}
	release     chan struct{}
}

func (s *blockingStore) Save(ctx context.Context, auth *Auth) (string, error) {
	s.saveCount.Add(1)
	select {
	case s.saveStarted <- struct{}{}:
	default:
	}
	if s.release != nil {
		select {
		case <-s.release:
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	return "", nil
}

func TestWithSkipPersist_DisablesUpdatePersistence(t *testing.T) {
	store := &countingStore{}
	mgr := NewManager(store, nil, nil)
	auth := &Auth{
		ID:       "auth-1",
		Provider: "antigravity",
		Metadata: map[string]any{"type": "antigravity"},
	}

	if _, err := mgr.Update(context.Background(), auth); err != nil {
		t.Fatalf("Update returned error: %v", err)
	}
	if got := store.saveCount.Load(); got != 1 {
		t.Fatalf("expected 1 Save call, got %d", got)
	}

	ctxSkip := WithSkipPersist(context.Background())
	if _, err := mgr.Update(ctxSkip, auth); err != nil {
		t.Fatalf("Update(skipPersist) returned error: %v", err)
	}
	if got := store.saveCount.Load(); got != 1 {
		t.Fatalf("expected Save call count to remain 1, got %d", got)
	}
}

func TestWithSkipPersist_DisablesRegisterPersistence(t *testing.T) {
	store := &countingStore{}
	mgr := NewManager(store, nil, nil)
	auth := &Auth{
		ID:       "auth-1",
		Provider: "antigravity",
		Metadata: map[string]any{"type": "antigravity"},
	}

	if _, err := mgr.Register(WithSkipPersist(context.Background()), auth); err != nil {
		t.Fatalf("Register(skipPersist) returned error: %v", err)
	}
	if got := store.saveCount.Load(); got != 0 {
		t.Fatalf("expected 0 Save calls, got %d", got)
	}
}

func TestMarkResult_UsesAsyncPersistence(t *testing.T) {
	store := &blockingStore{
		saveStarted: make(chan struct{}, 1),
		release:     make(chan struct{}),
	}
	mgr := NewManager(store, nil, nil)
	auth := &Auth{
		ID:       "auth-1",
		Provider: "antigravity",
		Status:   StatusActive,
		Metadata: map[string]any{"type": "antigravity"},
	}

	if _, err := mgr.Register(WithSkipPersist(context.Background()), auth); err != nil {
		t.Fatalf("Register(skipPersist) returned error: %v", err)
	}

	done := make(chan struct{})
	go func() {
		mgr.MarkResult(context.Background(), Result{
			AuthID:   auth.ID,
			Provider: auth.Provider,
			Model:    "test-model",
			Success:  true,
		})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("MarkResult blocked on persistence; want async enqueue")
	}

	select {
	case <-store.saveStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("async persistence did not start")
	}

	close(store.release)
}

func TestWithSkipPersist_DisablesMarkResultPersistence(t *testing.T) {
	store := &blockingStore{
		saveStarted: make(chan struct{}, 1),
	}
	mgr := NewManager(store, nil, nil)
	auth := &Auth{
		ID:       "auth-1",
		Provider: "antigravity",
		Status:   StatusActive,
		Metadata: map[string]any{"type": "antigravity"},
	}

	if _, err := mgr.Register(WithSkipPersist(context.Background()), auth); err != nil {
		t.Fatalf("Register(skipPersist) returned error: %v", err)
	}

	mgr.MarkResult(WithSkipPersist(context.Background()), Result{
		AuthID:   auth.ID,
		Provider: auth.Provider,
		Model:    "test-model",
		Success:  true,
	})

	select {
	case <-store.saveStarted:
		t.Fatal("MarkResult(skipPersist) unexpectedly triggered persistence")
	case <-time.After(200 * time.Millisecond):
	}
}
