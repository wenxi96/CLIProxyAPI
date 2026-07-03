package auth

import (
	"testing"
	"time"
)

func TestActiveQuotaRefreshPoolTouchAddsAndUpdatesLastUsed(t *testing.T) {
	pool := newActiveQuotaRefreshPool(10 * time.Minute)
	start := time.Unix(100, 0)
	later := start.Add(30 * time.Second)

	pool.touch("auth-1", start)
	entry, ok := pool.snapshot("auth-1")
	if !ok {
		t.Fatal("expected auth to be added")
	}
	if !entry.lastUsedAt.Equal(start) {
		t.Fatalf("lastUsedAt = %v, want %v", entry.lastUsedAt, start)
	}
	if !entry.nextCheckAt.Equal(start) {
		t.Fatalf("nextCheckAt = %v, want %v", entry.nextCheckAt, start)
	}

	pool.touch("auth-1", later)
	entry, ok = pool.snapshot("auth-1")
	if !ok {
		t.Fatal("expected auth to remain in pool")
	}
	if !entry.lastUsedAt.Equal(later) {
		t.Fatalf("lastUsedAt = %v, want %v", entry.lastUsedAt, later)
	}
}

func TestActiveQuotaRefreshPoolDueRemovesExpiredEntries(t *testing.T) {
	pool := newActiveQuotaRefreshPool(10 * time.Minute)
	start := time.Unix(100, 0)
	pool.touch("auth-1", start)

	due := pool.due(start.Add(10*time.Minute+time.Second), 1)
	if len(due) != 0 {
		t.Fatalf("due = %v, want empty for expired auth", due)
	}
	if got := pool.len(); got != 0 {
		t.Fatalf("pool len = %d, want 0", got)
	}
}

func TestActiveQuotaRefreshPoolDueMarksInFlightAndDeduplicates(t *testing.T) {
	pool := newActiveQuotaRefreshPool(10 * time.Minute)
	start := time.Unix(100, 0)
	pool.touch("auth-1", start)

	first := pool.due(start, 1)
	if len(first) != 1 || first[0] != "auth-1" {
		t.Fatalf("first due = %v, want [auth-1]", first)
	}
	second := pool.due(start, 1)
	if len(second) != 0 {
		t.Fatalf("second due = %v, want empty while in-flight", second)
	}

	entry, ok := pool.snapshot("auth-1")
	if !ok {
		t.Fatal("expected auth to remain in pool")
	}
	if !entry.inFlight {
		t.Fatal("expected entry to be marked in-flight")
	}
}

func TestActiveQuotaRefreshPoolDueRespectsGlobalRunningLimit(t *testing.T) {
	pool := newActiveQuotaRefreshPool(10 * time.Minute)
	start := time.Unix(100, 0)
	pool.touch("auth-1", start)
	pool.touch("auth-2", start)

	first := pool.due(start, 1)
	if len(first) != 1 {
		t.Fatalf("first due = %v, want one auth", first)
	}
	if pool.running != 1 {
		t.Fatalf("running = %d, want 1", pool.running)
	}
	second := pool.due(start.Add(time.Second), 1)
	if len(second) != 0 {
		t.Fatalf("second due = %v, want empty while worker is occupied", second)
	}

	pool.markComplete(first[0], QuotaCheckResult{RemainingPercent: intPtr(80)}, 40, start.Add(2*time.Second))
	if pool.running != 0 {
		t.Fatalf("running = %d, want 0 after completion", pool.running)
	}
	third := pool.due(start.Add(3*time.Second), 1)
	if len(third) != 1 {
		t.Fatalf("third due = %v, want one auth after worker is released", third)
	}
	if third[0] == first[0] {
		t.Fatalf("third due = %v, want a different auth because the first was rescheduled", third)
	}
}

func TestActiveQuotaRefreshPoolCompleteSchedulesNextCheckByDelta(t *testing.T) {
	tests := []struct {
		name      string
		remaining int
		threshold int
		want      time.Duration
	}{
		{name: "one percent above threshold", remaining: 41, threshold: 40, want: 120 * time.Second},
		{name: "fifteen percent above threshold", remaining: 55, threshold: 40, want: 120 * time.Second},
		{name: "thirty percent above threshold", remaining: 70, threshold: 40, want: 180 * time.Second},
		{name: "far above threshold", remaining: 71, threshold: 40, want: 300 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pool := newActiveQuotaRefreshPool(10 * time.Minute)
			start := time.Unix(100, 0)
			checkedAt := start.Add(time.Second)
			pool.touch("auth-1", start)
			if due := pool.due(start, 1); len(due) != 1 {
				t.Fatalf("due = %v, want one auth", due)
			}

			pool.markComplete("auth-1", QuotaCheckResult{RemainingPercent: intPtr(tt.remaining)}, tt.threshold, checkedAt)

			entry, ok := pool.snapshot("auth-1")
			if !ok {
				t.Fatal("expected auth to remain in pool")
			}
			if entry.inFlight {
				t.Fatal("expected in-flight to be cleared")
			}
			if !entry.lastCheckedAt.Equal(checkedAt) {
				t.Fatalf("lastCheckedAt = %v, want %v", entry.lastCheckedAt, checkedAt)
			}
			if got := entry.nextCheckAt.Sub(checkedAt); got != tt.want {
				t.Fatalf("next interval = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestActiveQuotaRefreshPoolRemovesUnsupportedNilRemainingAndFailures(t *testing.T) {
	tests := []struct {
		name   string
		apply  func(pool *activeQuotaRefreshPool, now time.Time)
		authID string
	}{
		{
			name: "unsupported",
			apply: func(pool *activeQuotaRefreshPool, now time.Time) {
				pool.markComplete("auth-1", QuotaCheckResult{Classification: ClassificationUnsupported}, 40, now)
			},
			authID: "auth-1",
		},
		{
			name: "nil remaining non-exhausted",
			apply: func(pool *activeQuotaRefreshPool, now time.Time) {
				pool.markComplete("auth-1", QuotaCheckResult{}, 40, now)
			},
			authID: "auth-1",
		},
		{
			name: "failed query",
			apply: func(pool *activeQuotaRefreshPool, now time.Time) {
				pool.markFailed("auth-1")
			},
			authID: "auth-1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pool := newActiveQuotaRefreshPool(10 * time.Minute)
			start := time.Unix(100, 0)
			pool.touch(tt.authID, start)
			if due := pool.due(start, 1); len(due) != 1 {
				t.Fatalf("due = %v, want one auth", due)
			}

			tt.apply(pool, start.Add(time.Second))

			if _, ok := pool.snapshot(tt.authID); ok {
				t.Fatal("expected auth to be removed")
			}
		})
	}
}

func TestActiveQuotaRefreshPoolExhaustedWithoutRemainingStaysScheduledUntilManagerRemoves(t *testing.T) {
	pool := newActiveQuotaRefreshPool(10 * time.Minute)
	start := time.Unix(100, 0)
	checkedAt := start.Add(time.Second)
	pool.touch("auth-1", start)
	if due := pool.due(start, 1); len(due) != 1 {
		t.Fatalf("due = %v, want one auth", due)
	}

	pool.markComplete("auth-1", QuotaCheckResult{Exhausted: true}, 40, checkedAt)

	entry, ok := pool.snapshot("auth-1")
	if !ok {
		t.Fatal("expected exhausted auth to remain until Manager disable removal is integrated")
	}
	if got := entry.nextCheckAt.Sub(checkedAt); got != activeQuotaRefreshFarThresholdInterval {
		t.Fatalf("next interval = %v, want %v", got, activeQuotaRefreshFarThresholdInterval)
	}
}
