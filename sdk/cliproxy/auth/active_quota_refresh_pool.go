package auth

import (
	"sync"
	"time"
)

const (
	activeQuotaRefreshNearThresholdInterval = 120 * time.Second
	activeQuotaRefreshMidThresholdInterval  = 180 * time.Second
	activeQuotaRefreshFarThresholdInterval  = 300 * time.Second
)

type activeQuotaRefreshPool struct {
	mu      sync.Mutex
	ttl     time.Duration
	running int
	entries map[string]*activeQuotaRefreshEntry
}

type activeQuotaRefreshEntry struct {
	authID        string
	lastUsedAt    time.Time
	lastCheckedAt time.Time
	nextCheckAt   time.Time
	inFlight      bool
}

func newActiveQuotaRefreshPool(ttl time.Duration) *activeQuotaRefreshPool {
	return &activeQuotaRefreshPool{
		ttl:     ttl,
		entries: make(map[string]*activeQuotaRefreshEntry),
	}
}

func (p *activeQuotaRefreshPool) touch(authID string, now time.Time) {
	if p == nil || authID == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	entry := p.entries[authID]
	if entry == nil {
		entry = &activeQuotaRefreshEntry{
			authID:      authID,
			nextCheckAt: now,
		}
		p.entries[authID] = entry
	}
	entry.lastUsedAt = now
	if entry.nextCheckAt.IsZero() {
		entry.nextCheckAt = now
	}
}

func (p *activeQuotaRefreshPool) remove(authID string) {
	if p == nil || authID == "" {
		return
	}
	p.mu.Lock()
	entry := p.entries[authID]
	if entry != nil && entry.inFlight && p.running > 0 {
		p.running--
	}
	delete(p.entries, authID)
	p.mu.Unlock()
}

func (p *activeQuotaRefreshPool) due(now time.Time, limit int) []string {
	if p == nil || limit == 0 {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	if limit > 0 && p.running >= limit {
		return nil
	}
	var due []string
	for authID, entry := range p.entries {
		if p.isExpiredLocked(entry, now) {
			if entry.inFlight && p.running > 0 {
				p.running--
			}
			delete(p.entries, authID)
			continue
		}
		if entry.inFlight || now.Before(entry.nextCheckAt) {
			continue
		}
		entry.inFlight = true
		p.running++
		due = append(due, authID)
		if limit > 0 && len(due) >= limit {
			break
		}
		if limit > 0 && p.running >= limit {
			break
		}
	}
	return due
}

func (p *activeQuotaRefreshPool) markComplete(authID string, result QuotaCheckResult, threshold int, now time.Time) {
	if p == nil || authID == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	entry := p.entries[authID]
	if entry == nil {
		return
	}
	if entry.inFlight && p.running > 0 {
		p.running--
	}
	if result.Classification == ClassificationUnsupported || (result.RemainingPercent == nil && !result.Exhausted) {
		delete(p.entries, authID)
		return
	}
	entry.inFlight = false
	entry.lastCheckedAt = now
	if result.RemainingPercent != nil {
		entry.nextCheckAt = now.Add(nextActiveQuotaRefreshInterval(*result.RemainingPercent, threshold))
		return
	}
	entry.nextCheckAt = now.Add(activeQuotaRefreshFarThresholdInterval)
}

func (p *activeQuotaRefreshPool) markFailed(authID string) {
	if p == nil || authID == "" {
		return
	}
	p.remove(authID)
}

func (p *activeQuotaRefreshPool) len() int {
	if p == nil {
		return 0
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.entries)
}

func (p *activeQuotaRefreshPool) snapshot(authID string) (activeQuotaRefreshEntry, bool) {
	if p == nil || authID == "" {
		return activeQuotaRefreshEntry{}, false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	entry := p.entries[authID]
	if entry == nil {
		return activeQuotaRefreshEntry{}, false
	}
	return *entry, true
}

func (p *activeQuotaRefreshPool) isExpiredLocked(entry *activeQuotaRefreshEntry, now time.Time) bool {
	if p.ttl <= 0 || entry == nil || entry.lastUsedAt.IsZero() {
		return false
	}
	return now.Sub(entry.lastUsedAt) > p.ttl
}

func nextActiveQuotaRefreshInterval(remainingPercent, thresholdPercent int) time.Duration {
	delta := remainingPercent - thresholdPercent
	switch {
	case delta <= 15:
		return activeQuotaRefreshNearThresholdInterval
	case delta <= 30:
		return activeQuotaRefreshMidThresholdInterval
	default:
		return activeQuotaRefreshFarThresholdInterval
	}
}
