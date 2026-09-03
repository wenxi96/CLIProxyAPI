package usage

import (
	"context"
	"errors"
	"sync"
	"time"
)

var (
	ErrProjectionRebuildInProgress = errors.New("usage projection rebuild is already in progress")
	ErrProjectionCASConflict       = errors.New("usage projection rebuild CAS conflict")
	ErrProjectionUnavailable       = errors.New("projection_unavailable")
)

type rebuildFinalizeGate struct {
	mu      sync.Mutex
	active  bool
	openCh  chan struct{}
	queueCh chan struct{}
	limit   int
}

func newRebuildFinalizeGate(limit int) *rebuildFinalizeGate {
	if limit <= 0 {
		limit = 1
	}
	return &rebuildFinalizeGate{limit: limit}
}

func (g *rebuildFinalizeGate) begin() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.active {
		return ErrProjectionRebuildInProgress
	}
	g.active = true
	g.openCh = make(chan struct{})
	g.queueCh = make(chan struct{}, g.limit)
	return nil
}

func (g *rebuildFinalizeGate) wait(ctx context.Context) (func(), error) {
	return g.waitN(ctx, 1)
}

func (g *rebuildFinalizeGate) waitN(ctx context.Context, slots int) (func(), error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if slots <= 0 {
		return func() {}, nil
	}
	g.mu.Lock()
	if !g.active {
		g.mu.Unlock()
		return func() {}, nil
	}
	openCh := g.openCh
	queueCh := g.queueCh
	limit := g.limit
	g.mu.Unlock()
	if slots > limit {
		return nil, ErrMutationJournalFull
	}
	acquired := 0
	releaseAcquired := func() {
		for index := 0; index < acquired; index++ {
			<-queueCh
		}
	}
	for acquired < slots {
		select {
		case queueCh <- struct{}{}:
			acquired++
		case <-ctx.Done():
			releaseAcquired()
			return nil, ctx.Err()
		}
	}
	if err := ctx.Err(); err != nil {
		releaseAcquired()
		return nil, err
	}
	select {
	case <-openCh:
		if err := ctx.Err(); err != nil {
			releaseAcquired()
			return nil, err
		}
		return func() { releaseAcquired() }, nil
	case <-ctx.Done():
		releaseAcquired()
		return nil, ctx.Err()
	}
}

func (g *rebuildFinalizeGate) end() {
	g.mu.Lock()
	if g.active {
		close(g.openCh)
		g.active = false
	}
	g.mu.Unlock()
}

func (g *rebuildFinalizeGate) activeState() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.active
}

// RebuildCandidate records the immutable base observed before an out-of-lock
// rebuild and owns the candidate projection until a bounded CAS commit.
type RebuildCandidate struct {
	BaseRevision      uint64
	BaseJournalHead   uint64
	DatasetEpoch      uint64
	StartedAt         time.Time
	MaxSuffixIntents  int
	SuffixReplayCount int
	CASRetryCount     int
	Rebased           bool
	Projection        *UsageProjection
	replayedSequences map[uint64]struct{}
}
