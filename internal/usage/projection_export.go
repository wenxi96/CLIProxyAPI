package usage

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"hash"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const usageExportPageLimit = 500
const defaultUsageExportLeaseTTL = 5 * time.Minute

var usageExportGenerationCounter atomic.Uint64

var (
	ErrUsageExportLeaseExpired      = errors.New("usage export lease expired")
	ErrUsageExportLeaseReleased     = errors.New("usage export lease released")
	ErrUsageExportInvalidPosition   = errors.New("usage export position invalid")
	ErrUsageExportGenerationChanged = errors.New("usage export generation changed")
)

// UsageExportQuery is the projection-owned subset of an events export query.
// Filters are normalized by the caller and are copied when a lease is pinned.
type UsageExportQuery struct {
	From    time.Time
	To      time.Time
	Filters map[string][]string
}

// UsageExportEstimate identifies the immutable generation selected for export.
type UsageExportEstimate struct {
	GenerationID        uint64
	DatasetEpoch        uint64
	Revision            uint64
	RewriteRevision     uint64
	SnapshotMaxSequence uint64
	EventCount          uint64
	FactsHash           string
	ExpiresAt           time.Time
	MatchedSourceIDs    map[string]uint64
	MatchedModels       map[string]uint64
	ServerJSONBytes     uint64
	ServerCSVBytes      uint64
	ServerJSONComplete  bool
	ServerCSVComplete   bool
}

// UsageExportRow owns a projection event reference and a value copy of the
// canonical detail materialized for the current bounded page.
type UsageExportRow struct {
	Event  EventRef
	Detail RequestDetail
}

// UsageExportPage is a bounded page from one pinned generation.
type UsageExportPage struct {
	Rows                []UsageExportRow
	HasMore             bool
	NextPosition        uint64
	GenerationID        uint64
	DatasetEpoch        uint64
	Revision            uint64
	RewriteRevision     uint64
	SnapshotMaxSequence uint64
}

type usageExportGeneration struct {
	id                  uint64
	datasetEpoch        uint64
	revision            uint64
	rewriteRevision     uint64
	snapshotMaxSequence uint64
	stats               *RequestStatistics // detached immutable export state
	projection          *UsageProjection
	query               UsageExportQuery
	eventCount          uint64
	matchedSourceIDs    map[string]uint64
	matchedModels       map[string]uint64
	factsHash           string
	serverJSONBytes     uint64
	serverCSVBytes      uint64
	serverJSONComplete  bool
	serverCSVComplete   bool
	createdAt           time.Time
	expiresAt           time.Time
	clock               func() time.Time
}

// UsageExportLease pins a generation until Release or expiry. The lease is
// safe for concurrent readers; Release is idempotent.
type UsageExportLease struct {
	state    *usageExportLeaseState
	released atomic.Bool
}

type usageExportLeaseState struct {
	mu         sync.Mutex
	generation *usageExportGeneration
	refs       int
}

// PinUsageExport pins a generation using the process clock and the default
// lease TTL. Callers that own a request-level clock/TTL should use
// PinUsageExportAt so snapshot and generation expiry share one time source.
func (s *RequestStatistics) PinUsageExport(ctx context.Context, query UsageExportQuery) (*UsageExportLease, UsageExportEstimate, error) {
	return s.PinUsageExportWithClock(ctx, query, time.Now, defaultUsageExportLeaseTTL)
}

// PinUsageExportAt pins a generation using an explicit clock instant and TTL.
// The clock is retained by the generation so Acquire and NextPage use the
// same expiry semantics as the manager that created the snapshot.
func (s *RequestStatistics) PinUsageExportAt(ctx context.Context, query UsageExportQuery, now time.Time, ttl time.Duration) (*UsageExportLease, UsageExportEstimate, error) {
	return s.PinUsageExportWithClock(ctx, query, func() time.Time { return now }, ttl)
}

// PinUsageExportWithClock is the request-manager integration point. Both the
// manager snapshot and the generation lease use the supplied clock, avoiding
// test/runtime drift between snapshot expiry and page reads.
func (s *RequestStatistics) PinUsageExportWithClock(ctx context.Context, query UsageExportQuery, clock func() time.Time, ttl time.Duration) (*UsageExportLease, UsageExportEstimate, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, UsageExportEstimate{}, err
	}
	if !query.From.IsZero() {
		query.From = query.From.UTC()
	}
	if !query.To.IsZero() {
		query.To = query.To.UTC()
	}
	if !query.From.IsZero() && !query.To.IsZero() && !query.From.Before(query.To) {
		return nil, UsageExportEstimate{}, ErrProjectionQueryInvalid
	}
	filters, err := normalizeProjectionEventFilters(query.Filters)
	if err != nil {
		return nil, UsageExportEstimate{}, err
	}

	if s == nil {
		return nil, UsageExportEstimate{}, ErrProjectionUnavailable
	}
	if clock == nil {
		clock = time.Now
	}
	var snapshotMaxSequence, revision, rewriteRevision, datasetEpoch, eventCount uint64
	var matchedSourceIDs, matchedModels map[string]uint64
	var pinnedProjection *UsageProjection
	var pinnedStats *RequestStatistics
	var normalizedQuery UsageExportQuery
	var factsHash string
	var serverJSONBytes, serverCSVBytes uint64
	serverJSONComplete, serverCSVComplete := false, false
	for attempt := 0; attempt < 3; attempt++ {
		// Each CAS attempt owns a fresh counter. A retry must never add the
		// same immutable candidate to the previous attempt's totals.
		serverJSONBytes, serverCSVBytes = 0, 0
		serverJSONComplete, serverCSVComplete = true, true
		s.mu.RLock()
		if s.projectionUnavailable || s.projection == nil {
			err := projectionQueryUnavailableError(s.projectionUnavailableCode)
			if s.projection == nil && !s.projectionUnavailable {
				err = ErrProjectionUnavailable
			}
			s.mu.RUnlock()
			return nil, UsageExportEstimate{}, err
		}
		liveProjection := s.projection
		pinnedProjection = liveProjection
		liveProjection.mu.RLock()
		candidateEpoch, candidateRevision, candidateRewrite := liveProjection.datasetEpoch, liveProjection.revision, liveProjection.rewriteRevision
		candidateMaxSequence := uint64(0)
		if liveProjection.nextSequence > 0 {
			candidateMaxSequence = liveProjection.nextSequence - 1
		}
		liveProjection.mu.RUnlock()
		s.mu.RUnlock()

		candidateSourceIDs := make(map[string]uint64)
		candidateModels := make(map[string]uint64)
		position := uint64(0)
		candidateEventCount := uint64(0)
		candidateFactsHash := sha256.New()
		valid := true
		for {
			if err := ctx.Err(); err != nil {
				return nil, UsageExportEstimate{}, err
			}
			page, hasMore, nextPosition, _, _, queryErr := liveProjection.QueryEvents(query.From, query.To, usageExportPageLimit, candidateMaxSequence, position, filters)
			if queryErr != nil {
				return nil, UsageExportEstimate{}, queryErr
			}
			for _, event := range page.Events {
				candidateEventCount++
				candidateSourceIDs[event.SourceID]++
				candidateModels[event.Model]++
				if err := ctx.Err(); err != nil {
					return nil, UsageExportEstimate{}, err
				}
				writeUsageExportEventFactsHash(candidateFactsHash, event)
				jsonBytes, csvBytes, counterOK := liveProjection.usageExportServerRowBytesV1(event)
				if !counterOK {
					serverJSONComplete = false
					serverCSVComplete = false
					continue
				}
				if !addUsageExportBound(&serverJSONBytes, jsonBytes) {
					serverJSONComplete = false
				}
				if !addUsageExportBound(&serverCSVBytes, csvBytes) {
					serverCSVComplete = false
				}
			}
			if !valid {
				break
			}
			if !hasMore {
				break
			}
			position = nextPosition
		}
		if !valid {
			continue
		}

		s.mu.RLock()
		if s.projection != liveProjection {
			s.mu.RUnlock()
			continue
		}
		// Keep the metadata check and detached clone under one projection read
		// lock. A mutation cannot land between the final CAS check and clone,
		// so the counters/facts hash above describe the exact pinned graph.
		liveProjection.mu.RLock()
		stable := liveProjection.datasetEpoch == candidateEpoch && liveProjection.revision == candidateRevision && liveProjection.rewriteRevision == candidateRewrite
		if stable {
			pinnedProjection = liveProjection.cloneLocked()
		}
		liveProjection.mu.RUnlock()
		if !stable {
			s.mu.RUnlock()
			continue
		}
		pinnedStats = &RequestStatistics{}
		pinnedStats.projection = pinnedProjection
		s.mu.RUnlock()
		matchedSourceIDs = candidateSourceIDs
		matchedModels = candidateModels
		snapshotMaxSequence = candidateMaxSequence
		datasetEpoch, revision, rewriteRevision = candidateEpoch, candidateRevision, candidateRewrite
		eventCount = candidateEventCount
		normalizedQuery = UsageExportQuery{From: query.From, To: query.To, Filters: cloneProjectionEventFilters(filters)}
		factsHash = hex.EncodeToString(candidateFactsHash.Sum(nil))
		if candidateEventCount == 0 {
			serverJSONBytes = 2 // []
			serverCSVBytes = uint64(len(usageExportCSVHeaderV1))
		} else {
			if !addUsageExportBound(&serverJSONBytes, 2+candidateEventCount-1) {
				serverJSONComplete = false
			}
			if !addUsageExportBound(&serverCSVBytes, uint64(len(usageExportCSVHeaderV1))) {
				serverCSVComplete = false
			}
		}
		break
	}
	if matchedSourceIDs == nil {
		return nil, UsageExportEstimate{}, ErrProjectionCASConflict
	}
	if snapshotMaxSequence == 0 {
		// Empty generations have no matching high-watermark.
		snapshotMaxSequence = 0
	}
	generationID := usageExportGenerationCounter.Add(1)
	if generationID == 0 {
		generationID = usageExportGenerationCounter.Add(1)
	}
	now := clock().UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if ttl <= 0 {
		ttl = defaultUsageExportLeaseTTL
	}
	generation := &usageExportGeneration{
		id:                  generationID,
		datasetEpoch:        datasetEpoch,
		revision:            revision,
		rewriteRevision:     rewriteRevision,
		snapshotMaxSequence: snapshotMaxSequence,
		stats:               pinnedStats,
		projection:          pinnedProjection,
		query:               normalizedQuery,
		eventCount:          eventCount,
		matchedSourceIDs:    matchedSourceIDs,
		matchedModels:       matchedModels,
		serverJSONBytes:     serverJSONBytes,
		serverCSVBytes:      serverCSVBytes,
		serverJSONComplete:  serverJSONComplete,
		serverCSVComplete:   serverCSVComplete,
		factsHash:           factsHash,
		createdAt:           now,
		expiresAt:           now.Add(ttl),
		clock:               clock,
	}
	lease := &UsageExportLease{state: &usageExportLeaseState{generation: generation, refs: 1}}
	return lease, usageExportEstimateFromGeneration(generation), nil
}

func usageExportEstimateFromGeneration(generation *usageExportGeneration) UsageExportEstimate {
	if generation == nil {
		return UsageExportEstimate{}
	}
	return UsageExportEstimate{
		GenerationID:        generation.id,
		DatasetEpoch:        generation.datasetEpoch,
		Revision:            generation.revision,
		RewriteRevision:     generation.rewriteRevision,
		SnapshotMaxSequence: generation.snapshotMaxSequence,
		EventCount:          generation.eventCount,
		FactsHash:           generation.factsHash,
		ExpiresAt:           generation.expiresAt,
		MatchedSourceIDs:    cloneUsageExportCounts(generation.matchedSourceIDs),
		MatchedModels:       cloneUsageExportCounts(generation.matchedModels),
		ServerJSONBytes:     generation.serverJSONBytes,
		ServerCSVBytes:      generation.serverCSVBytes,
		ServerJSONComplete:  generation.serverJSONComplete,
		ServerCSVComplete:   generation.serverCSVComplete,
	}
}

func cloneUsageExportCounts(values map[string]uint64) map[string]uint64 {
	if values == nil {
		return map[string]uint64{}
	}
	result := make(map[string]uint64, len(values))
	for key, value := range values {
		result[key] = value
	}
	return result
}

// NextPage returns at most the fixed export page limit. The caller's limit is
// intentionally bounded so a handler cannot retain an unbounded page.
func (lease *UsageExportLease) NextPage(ctx context.Context, position uint64, limit int) (UsageExportPage, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return UsageExportPage{}, err
	}
	if lease == nil || lease.state == nil {
		return UsageExportPage{}, ErrUsageExportLeaseReleased
	}
	if lease.released.Load() {
		return UsageExportPage{}, ErrUsageExportLeaseReleased
	}
	lease.state.mu.Lock()
	defer lease.state.mu.Unlock()
	if lease.state.generation == nil {
		return UsageExportPage{}, ErrUsageExportLeaseReleased
	}
	generation := lease.state.generation
	clock := generation.clock
	if clock == nil {
		clock = func() time.Time { return time.Now().UTC() }
	}
	if !clock().UTC().Before(generation.expiresAt) {
		return UsageExportPage{}, ErrUsageExportLeaseExpired
	}
	if limit < 1 || limit > usageExportPageLimit {
		return UsageExportPage{}, ErrUsageExportInvalidPosition
	}
	if generation.stats == nil || generation.projection == nil {
		return UsageExportPage{}, ErrProjectionUnavailable
	}
	projectionPage, hasMore, nextPosition, _, _, err := generation.projection.QueryEvents(
		generation.query.From, generation.query.To, limit, generation.snapshotMaxSequence, position, generation.query.Filters,
	)
	if err != nil {
		return UsageExportPage{}, err
	}
	details, err := generation.projection.lookupExportDetails(projectionPage.Events)
	if err != nil {
		return UsageExportPage{}, err
	}
	pageRows := make([]UsageExportRow, 0, len(projectionPage.Events))
	for _, event := range projectionPage.Events {
		detail, ok := details[event.StableEventID]
		if !ok || UsageSourceIDV1(detail) != event.SourceID {
			return UsageExportPage{}, ErrProjectionUnavailable
		}
		pageRows = append(pageRows, UsageExportRow{Event: event, Detail: detail})
	}
	return UsageExportPage{
		Rows:                pageRows,
		HasMore:             hasMore,
		NextPosition:        nextPosition,
		GenerationID:        generation.id,
		DatasetEpoch:        generation.datasetEpoch,
		Revision:            generation.revision,
		RewriteRevision:     generation.rewriteRevision,
		SnapshotMaxSequence: generation.snapshotMaxSequence,
	}, nil
}

// Release releases the lease. It is safe to call multiple times.
func (lease *UsageExportLease) Release() {
	if lease == nil || lease.state == nil || !lease.released.CompareAndSwap(false, true) {
		return
	}
	lease.state.mu.Lock()
	if lease.state.refs > 0 {
		lease.state.refs--
	}
	if lease.state.refs == 0 {
		lease.state.generation = nil
	}
	lease.state.mu.Unlock()
}

// Acquire returns another reference to the same pinned generation.
func (lease *UsageExportLease) Acquire() *UsageExportLease {
	if lease == nil || lease.state == nil || lease.released.Load() {
		return nil
	}
	lease.state.mu.Lock()
	defer lease.state.mu.Unlock()
	if lease.state.generation == nil {
		return nil
	}
	generation := lease.state.generation
	clock := generation.clock
	if clock == nil {
		clock = func() time.Time { return time.Now().UTC() }
	}
	if !clock().UTC().Before(generation.expiresAt) {
		return nil
	}
	lease.state.refs++
	return &UsageExportLease{state: lease.state}
}

func writeUsageExportEventFactsHash(dst hash.Hash, event EventRef) {
	if dst == nil {
		return
	}
	encoded, err := json.Marshal(event)
	if err != nil {
		return
	}
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(encoded)))
	_, _ = dst.Write(length[:])
	_, _ = dst.Write(encoded)
}

func normalizeUsageExportFilterValues(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func cloneProjectionEventFilters(filters map[string][]string) map[string][]string {
	result := make(map[string][]string, len(filters))
	for key, values := range filters {
		result[key] = append([]string(nil), values...)
	}
	return result
}
