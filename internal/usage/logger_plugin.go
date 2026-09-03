// Package usage provides usage tracking and logging functionality for the CLI Proxy API server.
// It includes plugins for monitoring API usage, token consumption, and other metrics
// to help with observability and billing purposes.
package usage

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/logging"
	coreusage "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
)

var statisticsEnabled atomic.Bool
var statisticsReady atomic.Bool

func init() {
	statisticsEnabled.Store(true)
	statisticsReady.Store(true)
	coreusage.RegisterPlugin(NewLoggerPlugin())
}

// LoggerPlugin collects in-memory request statistics for usage analysis.
// It implements coreusage.Plugin to receive usage records emitted by the runtime.
type LoggerPlugin struct {
	stats *RequestStatistics
}

// NewLoggerPlugin constructs a new logger plugin instance.
//
// Returns:
//   - *LoggerPlugin: A new logger plugin instance wired to the shared statistics store.
func NewLoggerPlugin() *LoggerPlugin { return &LoggerPlugin{stats: defaultRequestStatistics} }

// HandleUsage implements coreusage.Plugin.
// It updates the in-memory statistics store whenever a usage record is received.
//
// Parameters:
//   - ctx: The context for the usage record
//   - record: The usage record to aggregate
func (p *LoggerPlugin) HandleUsage(ctx context.Context, record coreusage.Record) {
	_ = p.HandleUsageOutcome(ctx, record)
}

// IsAuthoritativeUsageSink marks the logger as the canonical usage sink. The
// manager rejects a second authoritative sink so one accepted item cannot be
// committed twice.
func (p *LoggerPlugin) IsAuthoritativeUsageSink() bool { return p != nil }

// HandleUsageOutcome lets the mutation coordinator report admission failures
// to the manager while retaining the legacy void Plugin interface.
func (p *LoggerPlugin) HandleUsageOutcome(ctx context.Context, record coreusage.Record) error {
	if !statisticsEnabled.Load() || !statisticsReady.Load() {
		return nil
	}
	if p == nil || p.stats == nil {
		return nil
	}
	return p.stats.RecordWithError(ctx, record)
}

// SetStatisticsEnabled toggles whether in-memory statistics are recorded.
func SetStatisticsEnabled(enabled bool) { statisticsEnabled.Store(enabled) }

// StatisticsEnabled reports the current recording state.
func StatisticsEnabled() bool { return statisticsEnabled.Load() }

// SetStatisticsReady controls the restore ready gate independently from the
// configured enabled flag. Records arriving before a startup/runtime restore
// completes are acknowledged by the legacy sink but are not admitted.
func SetStatisticsReady(ready bool) { statisticsReady.Store(ready) }

// StatisticsReady reports whether usage ingestion may be admitted.
func StatisticsReady() bool { return statisticsReady.Load() }

// RequestStatistics maintains aggregated request metrics in memory.
type RequestStatistics struct {
	mu            sync.RWMutex
	persistenceMu sync.Mutex

	totalRequests  int64
	successCount   int64
	failureCount   int64
	totalTokens    int64
	changeCount    uint64
	persistedCount uint64

	apis map[string]*apiStats

	requestsByDay              map[string]int64
	requestsByHour             map[int]int64
	tokensByDay                map[string]int64
	tokensByHour               map[int]int64
	detailLocations            map[string]detailLocation
	detailEventLocations       map[string]string
	projection                 *UsageProjection
	coordinator                *MutationCoordinator
	projectionUnavailable      bool
	projectionUnavailableCode  string
	identityMetadataReady      bool
	identityMetadataGeneration uint64
	// replayFallbackCount tracks how many restore/import calls fell back to the
	// canonical-detail replay path. A normal v2 restart that directly hydrates
	// the persisted projection leaves this at zero; tests assert on it to prove
	// details were not replayed.
	replayFallbackCount atomic.Int64
}

// apiStats holds aggregated metrics for a single API key.
type apiStats struct {
	TotalRequests int64
	TotalTokens   int64
	Models        map[string]*modelStats
}

// modelStats holds aggregated metrics for a specific model within an API.
type modelStats struct {
	TotalRequests int64
	TotalTokens   int64
	Details       []RequestDetail
}

// RequestDetail stores the canonical request context and token facts for a single request.
type RequestDetail struct {
	RequestID        string            `json:"request_id"`
	ClientIP         string            `json:"client_ip"`
	Timestamp        time.Time         `json:"timestamp"`
	Endpoint         string            `json:"endpoint"`
	Model            string            `json:"model"`
	Provider         string            `json:"provider"`
	ExecutorType     string            `json:"executor_type"`
	AuthType         string            `json:"auth_type"`
	ModelAlias       string            `json:"model_alias"`
	Source           string            `json:"source"`
	AuthIndex        string            `json:"auth_index"`
	DetailRole       string            `json:"detail_role"`
	DetailSequence   string            `json:"detail_sequence,omitempty"`
	Failed           bool              `json:"failed"`
	Generate         *bool             `json:"generate,omitempty"`
	LatencyMs        int64             `json:"latency_ms"`
	EstimatedCostUSD *float64          `json:"estimated_cost_usd"`
	Tokens           RequestTokenStats `json:"tokens"`
}

// TokenStats captures aggregate token usage counters.
type TokenStats struct {
	InputTokens     int64 `json:"input_tokens"`
	OutputTokens    int64 `json:"output_tokens"`
	ReasoningTokens int64 `json:"reasoning_tokens"`
	CachedTokens    int64 `json:"cached_tokens"`
	TotalTokens     int64 `json:"total_tokens"`
}

// RequestTokenStats captures request-level token facts and normalization metadata.
type RequestTokenStats struct {
	InputTokens         int64  `json:"input_tokens"`
	OutputTokens        int64  `json:"output_tokens"`
	ReasoningTokens     int64  `json:"reasoning_tokens"`
	CachedTokens        int64  `json:"cached_tokens"`
	CacheReadTokens     int64  `json:"cache_read_tokens"`
	CacheCreationTokens int64  `json:"cache_creation_tokens"`
	TotalTokens         int64  `json:"total_tokens"`
	ReportedTotalTokens int64  `json:"reported_total_tokens"`
	ComputedTotalTokens int64  `json:"computed_total_tokens"`
	TokenUsageSource    string `json:"token_usage_source"`
	CacheSplitStatus    string `json:"cache_split_status"`
	ReasoningCostMode   string `json:"reasoning_cost_mode"`
}

// StatisticsSnapshot represents an immutable view of the aggregated metrics.
type StatisticsSnapshot struct {
	TotalRequests int64 `json:"total_requests"`
	SuccessCount  int64 `json:"success_count"`
	FailureCount  int64 `json:"failure_count"`
	TotalTokens   int64 `json:"total_tokens"`

	APIs  map[string]APISnapshot       `json:"apis"`
	Auths map[string]AuthUsageSnapshot `json:"auths,omitempty"`

	RequestsByDay  map[string]int64 `json:"requests_by_day"`
	RequestsByHour map[string]int64 `json:"requests_by_hour"`
	TokensByDay    map[string]int64 `json:"tokens_by_day"`
	TokensByHour   map[string]int64 `json:"tokens_by_hour"`
}

// AuthUsageSnapshot summarises usage for a single credential auth_index.
type AuthUsageSnapshot struct {
	AuthIndex        string                       `json:"auth_index"`
	TotalRequests    int64                        `json:"total_requests"`
	SuccessCount     int64                        `json:"success_count"`
	FailureCount     int64                        `json:"failure_count"`
	Tokens           TokenStats                   `json:"tokens"`
	EstimatedCostUSD *float64                     `json:"estimated_cost_usd"`
	FirstRequestAt   *time.Time                   `json:"first_request_at,omitempty"`
	LastRequestAt    *time.Time                   `json:"last_request_at,omitempty"`
	Models           map[string]AuthModelSnapshot `json:"models,omitempty"`
}

// AuthModelSnapshot summarises usage for a model under a single auth_index.
type AuthModelSnapshot struct {
	TotalRequests    int64      `json:"total_requests"`
	SuccessCount     int64      `json:"success_count"`
	FailureCount     int64      `json:"failure_count"`
	Tokens           TokenStats `json:"tokens"`
	EstimatedCostUSD *float64   `json:"estimated_cost_usd"`
}

// APISnapshot summarises metrics for a single API key.
type APISnapshot struct {
	TotalRequests int64                    `json:"total_requests"`
	TotalTokens   int64                    `json:"total_tokens"`
	Models        map[string]ModelSnapshot `json:"models"`
}

// ModelSnapshot summarises metrics for a specific model.
type ModelSnapshot struct {
	TotalRequests int64           `json:"total_requests"`
	TotalTokens   int64           `json:"total_tokens"`
	Details       []RequestDetail `json:"details"`
}

// AuthRequestFilter constrains auth_index detail lookups.
type AuthRequestFilter struct {
	Limit       int
	Offset      int
	Model       string
	Failed      *bool
	From        *time.Time
	To          *time.Time
	ToExclusive bool
}

// AuthRequestPage contains a page of request details for one auth_index.
type AuthRequestPage struct {
	AuthIndex string              `json:"auth_index"`
	Total     int                 `json:"total"`
	Limit     int                 `json:"limit"`
	Offset    int                 `json:"offset"`
	Items     []AuthRequestDetail `json:"items"`
}

// AuthRequestDetail stores one paginated canonical request detail.
type AuthRequestDetail = RequestDetail

type authRequestListItem struct {
	apiBucket string
	detail    AuthRequestDetail
}

var defaultRequestStatistics = NewRequestStatistics()

// GetRequestStatistics returns the shared statistics store.
func GetRequestStatistics() *RequestStatistics { return defaultRequestStatistics }

// NewRequestStatistics constructs an empty statistics store.
func NewRequestStatistics() *RequestStatistics {
	stats := &RequestStatistics{
		apis:                 make(map[string]*apiStats),
		requestsByDay:        make(map[string]int64),
		requestsByHour:       make(map[int]int64),
		tokensByDay:          make(map[string]int64),
		tokensByHour:         make(map[int]int64),
		detailLocations:      make(map[string]detailLocation),
		detailEventLocations: make(map[string]string),
		projection:           NewUsageProjection(),
	}
	stats.coordinator = NewMutationCoordinator(stats)
	return stats
}

// cloneForExport returns a detached value graph for a pinned export. The
// caller must not mutate the returned store; its projection and canonical
// detail locations are independent from the live statistics store.
func (s *RequestStatistics) cloneForExport() (*RequestStatistics, error) {
	if s == nil {
		return nil, ErrProjectionUnavailable
	}
	s.mu.RLock()
	state := s.cloneStateLocked()
	unavailable := s.projectionUnavailable
	unavailableCode := s.projectionUnavailableCode
	s.mu.RUnlock()
	cloned := &RequestStatistics{
		apis:                      make(map[string]*apiStats),
		requestsByDay:             make(map[string]int64),
		requestsByHour:            make(map[int]int64),
		tokensByDay:               make(map[string]int64),
		tokensByHour:              make(map[int]int64),
		detailLocations:           make(map[string]detailLocation),
		detailEventLocations:      make(map[string]string),
		projectionUnavailable:     unavailable,
		projectionUnavailableCode: unavailableCode,
	}
	cloned.mu.Lock()
	cloned.adoptStateLocked(state)
	cloned.mu.Unlock()
	return cloned, nil
}

// Record ingests a new usage record and updates the aggregates.
func (s *RequestStatistics) Record(ctx context.Context, record coreusage.Record) {
	if strings.TrimSpace(record.CanonicalIdentitySeed) == "" && strings.TrimSpace(record.AdmissionDiscriminator) == "" {
		record.AdmissionDiscriminator = legacyRecordAdmissionDiscriminator(ctx, record)
	}
	_ = s.RecordWithError(ctx, record)
}

// RecordWithError admits a record through the mutation coordinator. The
// legacy Record method intentionally discards the error for compatibility.
func (s *RequestStatistics) RecordWithError(ctx context.Context, record coreusage.Record) error {
	if s == nil {
		return nil
	}
	if !statisticsEnabled.Load() || !statisticsReady.Load() {
		return nil
	}
	if strings.TrimSpace(record.CanonicalIdentitySeed) == "" && strings.TrimSpace(record.AdmissionDiscriminator) == "" {
		record.AdmissionDiscriminator = legacyRecordAdmissionDiscriminator(ctx, record)
	}
	if s.coordinator != nil {
		err := s.coordinator.ApplyRecord(ctx, record)
		if err != nil && s.canonicalFallbackEligible(err) {
			s.retainCanonicalRecord(ctx, record, err)
		}
		return err
	}
	s.recordDirect(ctx, record, ProjectionIdentity{})
	return nil
}

func (s *RequestStatistics) canonicalFallbackEligible(err error) bool {
	if errors.Is(err, ErrMutationJournalBudget) || errors.Is(err, ErrProjectionUnavailable) {
		return true
	}
	if !errors.Is(err, ErrMutationJournalFull) || s == nil || s.coordinator == nil || s.coordinator.Journal() == nil {
		return false
	}
	return s.coordinator.Journal().PendingCount() == 0
}

// retainCanonicalRecord preserves the legacy canonical detail view when the
// projection coordinator cannot admit an intent. This path never mutates the
// projection; it marks the generation unavailable so a rebuild can consume the
// retained detail later instead of silently losing the record.
func (s *RequestStatistics) retainCanonicalRecord(ctx context.Context, record coreusage.Record, err error) {
	if s == nil {
		return
	}
	detail := CanonicalRequestDetail(ctx, record)
	apiName := safeAPIIdentifier(ctx, record, detail)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.upsertCanonicalDetailLocked(apiName, detail.Model, detail, detail.Generate != nil)
	s.projectionUnavailable = true
	s.projectionUnavailableCode = projectionUnavailableCode(err)
}

func projectionUnavailableCode(err error) string {
	switch {
	case errors.Is(err, ErrProjectionBudgetExceeded):
		return "projection_budget_exceeded"
	case errors.Is(err, ErrMutationJournalBudget):
		return "journal_budget_exceeded"
	case errors.Is(err, ErrMutationJournalFull):
		return "journal_full"
	default:
		return "projection_unavailable"
	}
}

// ProjectionAvailability reports whether the current projection generation
// can serve projection-backed queries and why a canonical fallback was used.
func (s *RequestStatistics) ProjectionAvailability() (available bool, code string) {
	if s == nil {
		return false, "projection_unavailable"
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.projectionUnavailable {
		return false, s.projectionUnavailableCode
	}
	return true, ""
}

func (s *RequestStatistics) WaitForMutationIdle(ctx context.Context) error {
	if s == nil || s.coordinator == nil {
		return nil
	}
	return s.coordinator.WaitForIdle(ctx)
}

func (s *RequestStatistics) recordDirect(ctx context.Context, record coreusage.Record, identity ProjectionIdentity) {
	detail := CanonicalRequestDetail(ctx, record)
	statsKey := safeAPIIdentifier(ctx, record, detail)

	s.mu.Lock()
	defer s.mu.Unlock()

	s.ensureProjectionLocked()
	s.upsertDetailLockedWithIdentity(statsKey, detail.Model, detail, detail.Generate != nil, identity)
}

type detailUpsertStatus int

const (
	detailUpsertSkipped detailUpsertStatus = iota
	detailUpsertAdded
	detailUpsertEnriched
)

type detailLocation struct {
	apiName       string
	modelName     string
	stats         *apiStats
	modelStats    *modelStats
	index         int
	stableEventID string
}

func (s *RequestStatistics) upsertDetailLocked(apiName, model string, detail RequestDetail) detailUpsertStatus {
	return s.upsertDetailLockedWithGenerateAndIdentity(apiName, model, detail, detail.Generate != nil, ProjectionIdentity{})
}

func (s *RequestStatistics) upsertDetailLockedWithGenerate(apiName, model string, detail RequestDetail, incomingGenerateExplicit bool) detailUpsertStatus {
	return s.upsertDetailLockedWithGenerateAndIdentity(apiName, model, detail, incomingGenerateExplicit, ProjectionIdentity{})
}

func (s *RequestStatistics) upsertDetailLockedWithIdentity(apiName, model string, detail RequestDetail, incomingGenerateExplicit bool, identity ProjectionIdentity) detailUpsertStatus {
	return s.upsertDetailLockedWithGenerateAndIdentity(apiName, model, detail, incomingGenerateExplicit, identity)
}

func (s *RequestStatistics) upsertDetailLockedWithIdentityBudget(apiName, model string, detail RequestDetail, incomingGenerateExplicit bool, identity ProjectionIdentity) (detailUpsertStatus, error) {
	if s == nil {
		return detailUpsertSkipped, ErrProjectionUnavailable
	}
	s.ensureProjectionLocked()
	if err := s.projection.CheckDetailBudget(apiName, detail, identity); err != nil {
		return detailUpsertSkipped, err
	}
	return s.upsertDetailLockedWithIdentity(apiName, model, detail, incomingGenerateExplicit, identity), nil
}

func (s *RequestStatistics) upsertDetailLockedWithGenerateAndIdentity(apiName, model string, detail RequestDetail, incomingGenerateExplicit bool, projectionIdentity ProjectionIdentity) detailUpsertStatus {
	return s.upsertDetailLockedWithGenerateAndIdentityMode(apiName, model, detail, incomingGenerateExplicit, projectionIdentity, true)
}

func (s *RequestStatistics) upsertCanonicalDetailLocked(apiName, model string, detail RequestDetail, incomingGenerateExplicit bool) detailUpsertStatus {
	return s.upsertDetailLockedWithGenerateAndIdentityMode(apiName, model, detail, incomingGenerateExplicit, ProjectionIdentity{}, false)
}

func (s *RequestStatistics) upsertDetailLockedWithGenerateAndIdentityMode(apiName, model string, detail RequestDetail, incomingGenerateExplicit bool, projectionIdentity ProjectionIdentity, applyProjection bool) detailUpsertStatus {
	if applyProjection {
		s.ensureProjectionLocked()
	}
	detail = normalizeRequestDetail(detail, detail.Provider)
	apiName = safeImportedAPIName(apiName, detail)
	model = defaultIfEmpty(model, detail.Model)

	s.ensureDetailLocationsLocked()
	baseDetailKey := detailIdentityKey(apiName, model, detail)
	detailKey := baseDetailKey
	if projectionIdentity.StableEventID != "" {
		detailKey = stableDetailLocationKey(baseDetailKey, projectionIdentity.StableEventID)
	}
	existing, ok := s.detailLocations[detailKey]
	if !ok && !applyProjection && projectionIdentity.StableEventID == "" {
		for candidateKey, candidate := range s.detailLocations {
			if candidate.modelStats == nil || candidate.index < 0 || candidate.index >= len(candidate.modelStats.Details) {
				continue
			}
			candidateDetail := candidate.modelStats.Details[candidate.index]
			if detailIdentityKey(candidate.apiName, candidate.modelName, candidateDetail) != baseDetailKey {
				continue
			}
			detailKey = candidateKey
			existing = candidate
			ok = true
			break
		}
	}
	if ok && projectionIdentity.StableEventID != "" {
		candidateStableEventID := existing.stableEventID
		if candidateStableEventID == "" && existing.modelStats != nil && existing.index >= 0 && existing.index < len(existing.modelStats.Details) {
			if candidateIdentity, candidateIdentityOK := s.projection.ExistingIdentity(existing.apiName, existing.modelStats.Details[existing.index]); candidateIdentityOK {
				candidateStableEventID = candidateIdentity.StableEventID
			}
		}
		if candidateStableEventID != projectionIdentity.StableEventID {
			existing = detailLocation{}
			ok = false
		}
	}
	if !ok && projectionIdentity.StableEventID != "" {
		if candidateKey := s.detailEventLocations[projectionIdentity.StableEventID]; candidateKey != "" {
			if candidate, exists := s.detailLocations[candidateKey]; exists {
				detailKey = candidateKey
				existing = candidate
				ok = true
			}
		}
	}
	if ok && existing.modelStats != nil && existing.index >= 0 && existing.index < len(existing.modelStats.Details) {
		current := existing.modelStats.Details[existing.index]
		if !shouldEnrichDetailWithGenerate(current, detail, incomingGenerateExplicit) {
			return detailUpsertSkipped
		}
		merged := mergeEnrichedDetailWithGenerate(current, detail, incomingGenerateExplicit)
		targetModel := defaultIfEmpty(model, merged.Model)
		if targetModel == existing.modelName {
			existing.modelStats.Details[existing.index] = merged
			s.applyTokenDeltaLocked(existing.stats, existing.modelStats, current, merged)
		} else {
			oldModelStats := existing.modelStats
			oldIndex := existing.index
			delete(s.detailLocations, detailKey)
			if existing.stableEventID != "" && s.detailEventLocations[existing.stableEventID] == detailKey {
				delete(s.detailEventLocations, existing.stableEventID)
			}
			oldModelStats.Details = append(oldModelStats.Details[:oldIndex], oldModelStats.Details[oldIndex+1:]...)
			oldModelStats.TotalRequests--
			oldModelStats.TotalTokens -= current.Tokens.TotalTokens
			for locationKey, location := range s.detailLocations {
				if location.modelStats == oldModelStats && location.index > oldIndex {
					location.index--
					s.detailLocations[locationKey] = location
				}
			}
			if len(oldModelStats.Details) == 0 {
				if occupant, ok := existing.stats.Models[existing.modelName]; ok && occupant == oldModelStats {
					delete(existing.stats.Models, existing.modelName)
				}
			}
			newModelStats := existing.stats.Models[targetModel]
			if newModelStats == nil {
				newModelStats = &modelStats{}
				existing.stats.Models[targetModel] = newModelStats
			}
			newIndex := len(newModelStats.Details)
			newModelStats.Details = append(newModelStats.Details, merged)
			newModelStats.TotalRequests++
			newModelStats.TotalTokens += merged.Tokens.TotalTokens
			s.applyTokenDeltaLocked(existing.stats, nil, current, merged)
			existing.modelStats = newModelStats
			existing.modelName = targetModel
			existing.index = newIndex
		}
		s.applyOutcomeDeltaLocked(current, merged)
		if applyProjection && s.projection != nil {
			projectionResult := s.projection.ApplyDetailWithIdentity(existing.apiName, merged, projectionIdentity)
			if existing.stableEventID == "" {
				existing.stableEventID = projectionResult.StableEventID
			}
		}
		delete(s.detailLocations, detailKey)
		newBaseKey := detailIdentityKey(existing.apiName, existing.modelName, merged)
		newKey := newBaseKey
		if existing.stableEventID != "" {
			newKey = stableDetailLocationKey(newBaseKey, existing.stableEventID)
		}
		if occupant, occupied := s.detailLocations[newKey]; occupied && (occupant.modelStats != existing.modelStats || occupant.index != existing.index) {
			newKey = fmt.Sprintf("%s\x00index:%d", newBaseKey, existing.index)
		}
		s.detailLocations[newKey] = existing
		if existing.stableEventID != "" {
			s.detailEventLocations[existing.stableEventID] = newKey
		}
		s.markChangedLocked()
		return detailUpsertEnriched
	}

	stats, ok := s.apis[apiName]
	if !ok || stats == nil {
		stats = &apiStats{Models: make(map[string]*modelStats)}
		s.apis[apiName] = stats
	} else if stats.Models == nil {
		stats.Models = make(map[string]*modelStats)
	}
	locationKey := s.addDetailLocked(apiName, stats, model, detail)
	if applyProjection && s.projection != nil {
		result := s.projection.ApplyDetailWithIdentity(apiName, detail, projectionIdentity)
		if location, ok := s.detailLocations[locationKey]; ok {
			location.stableEventID = result.StableEventID
			if projectionIdentity.StableEventID != "" {
				delete(s.detailLocations, locationKey)
				locationKey = stableDetailLocationKey(detailIdentityKey(apiName, model, detail), result.StableEventID)
			}
			s.detailLocations[locationKey] = location
			if result.StableEventID != "" {
				s.detailEventLocations[result.StableEventID] = locationKey
			}
		}
	}
	return detailUpsertAdded
}

func (s *RequestStatistics) ensureProjectionLocked() {
	if s == nil || s.projection != nil {
		return
	}
	s.projection = NewUsageProjection()
	if s.coordinator != nil {
		s.projection.budget = s.coordinator.config.ProjectionBudget.normalized()
	}
	for apiName, stats := range s.apis {
		if stats == nil {
			continue
		}
		for _, modelStatsValue := range stats.Models {
			if modelStatsValue == nil {
				continue
			}
			for _, detail := range modelStatsValue.Details {
				s.projection.ApplyDetail(apiName, normalizeRequestDetail(detail, detail.Provider))
			}
		}
	}
}

func (s *RequestStatistics) ensureDetailLocationsLocked() {
	if s.detailLocations != nil {
		if s.detailEventLocations == nil {
			s.detailEventLocations = make(map[string]string)
			for key, location := range s.detailLocations {
				if location.stableEventID != "" {
					s.detailEventLocations[location.stableEventID] = key
				}
			}
		}
		return
	}
	s.detailLocations = make(map[string]detailLocation)
	s.detailEventLocations = make(map[string]string)
	for apiName, stats := range s.apis {
		if stats == nil {
			continue
		}
		for modelName, modelStatsValue := range stats.Models {
			if modelStatsValue == nil {
				continue
			}
			for index, detail := range modelStatsValue.Details {
				identity := detailIdentityKey(apiName, modelName, detail)
				locationKey := identity
				if _, exists := s.detailLocations[locationKey]; exists {
					locationKey = fmt.Sprintf("%s\x00index:%d", identity, index)
				}
				if _, exists := s.detailLocations[locationKey]; !exists {
					stableEventID := ""
					if s.projection != nil {
						if projectionIdentity, ok := s.projection.ExistingIdentity(apiName, detail); ok {
							stableEventID = projectionIdentity.StableEventID
						}
					}
					s.detailLocations[locationKey] = detailLocation{
						apiName:       apiName,
						modelName:     modelName,
						stats:         stats,
						modelStats:    modelStatsValue,
						index:         index,
						stableEventID: stableEventID,
					}
					if stableEventID != "" {
						s.detailEventLocations[stableEventID] = locationKey
					}
				}
			}
		}
	}
}

func (s *RequestStatistics) addDetailLocked(apiName string, stats *apiStats, model string, detail RequestDetail) string {
	totalTokens := detail.Tokens.TotalTokens
	stats.TotalRequests++
	stats.TotalTokens += totalTokens
	modelStatsValue, ok := stats.Models[model]
	if !ok {
		modelStatsValue = &modelStats{}
		stats.Models[model] = modelStatsValue
	}
	modelStatsValue.TotalRequests++
	modelStatsValue.TotalTokens += totalTokens
	detailIndex := len(modelStatsValue.Details)
	modelStatsValue.Details = append(modelStatsValue.Details, detail)
	baseKey := detailIdentityKey(apiName, model, detail)
	locationKey := baseKey
	if _, exists := s.detailLocations[locationKey]; exists {
		locationKey = fmt.Sprintf("%s\x00index:%d", baseKey, detailIndex)
	}
	s.detailLocations[locationKey] = detailLocation{
		apiName:    apiName,
		modelName:  model,
		stats:      stats,
		modelStats: modelStatsValue,
		index:      detailIndex,
	}

	s.totalRequests++
	if detail.Failed {
		s.failureCount++
	} else {
		s.successCount++
	}
	s.totalTokens += totalTokens

	dayKey := detail.Timestamp.Format("2006-01-02")
	hourKey := detail.Timestamp.Hour()
	s.requestsByDay[dayKey]++
	s.requestsByHour[hourKey]++
	s.tokensByDay[dayKey] += totalTokens
	s.tokensByHour[hourKey] += totalTokens
	s.markChangedLocked()
	return locationKey
}

func stableDetailLocationKey(baseKey, stableEventID string) string {
	return baseKey + "\x00event:" + stableEventID
}

func (s *RequestStatistics) applyTokenDeltaLocked(stats *apiStats, modelStatsValue *modelStats, oldDetail, newDetail RequestDetail) {
	delta := newDetail.Tokens.TotalTokens - oldDetail.Tokens.TotalTokens
	if delta == 0 {
		return
	}
	if stats != nil {
		stats.TotalTokens += delta
	}
	if modelStatsValue != nil {
		modelStatsValue.TotalTokens += delta
	}
	s.totalTokens += delta
	if !oldDetail.Timestamp.IsZero() {
		dayKey := oldDetail.Timestamp.Format("2006-01-02")
		hourKey := oldDetail.Timestamp.Hour()
		s.tokensByDay[dayKey] += delta
		s.tokensByHour[hourKey] += delta
	}
}

func (s *RequestStatistics) applyOutcomeDeltaLocked(oldDetail, newDetail RequestDetail) {
	if oldDetail.Failed == newDetail.Failed {
		return
	}
	if oldDetail.Failed {
		s.failureCount--
		s.successCount++
		return
	}
	s.successCount--
	s.failureCount++
}

// Snapshot returns a copy of the aggregated metrics for external consumption.
func (s *RequestStatistics) Snapshot() StatisticsSnapshot {
	result, _, _ := s.SnapshotWithState()
	return result
}

// SnapshotWithState returns a copy of the aggregated metrics together with the
// current mutation and persisted counters.
func (s *RequestStatistics) SnapshotWithState() (StatisticsSnapshot, uint64, uint64) {
	result := StatisticsSnapshot{}
	if s == nil {
		return result, 0, 0
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.snapshotWithStateLocked()
}

// snapshotWithStateLocked builds the canonical detail snapshot assuming s.mu
// is held for reading.
func (s *RequestStatistics) snapshotWithStateLocked() (StatisticsSnapshot, uint64, uint64) {
	result := StatisticsSnapshot{}
	if s == nil {
		return result, 0, 0
	}

	result.TotalRequests = s.totalRequests
	result.SuccessCount = s.successCount
	result.FailureCount = s.failureCount
	result.TotalTokens = s.totalTokens

	result.APIs = make(map[string]APISnapshot, len(s.apis))
	for apiName, stats := range s.apis {
		apiSnapshot := APISnapshot{
			TotalRequests: stats.TotalRequests,
			TotalTokens:   stats.TotalTokens,
			Models:        make(map[string]ModelSnapshot, len(stats.Models)),
		}
		for modelName, modelStatsValue := range stats.Models {
			requestDetails := make([]RequestDetail, len(modelStatsValue.Details))
			for index, detail := range modelStatsValue.Details {
				requestDetails[index] = cloneRequestDetail(detail)
			}
			apiSnapshot.Models[modelName] = ModelSnapshot{
				TotalRequests: modelStatsValue.TotalRequests,
				TotalTokens:   modelStatsValue.TotalTokens,
				Details:       requestDetails,
			}
		}
		result.APIs[apiName] = apiSnapshot
	}
	result.Auths = buildAuthUsageSnapshots(result.APIs)

	result.RequestsByDay = make(map[string]int64, len(s.requestsByDay))
	for k, v := range s.requestsByDay {
		result.RequestsByDay[k] = v
	}

	result.RequestsByHour = make(map[string]int64, len(s.requestsByHour))
	for hour, v := range s.requestsByHour {
		key := formatHour(hour)
		result.RequestsByHour[key] = v
	}

	result.TokensByDay = make(map[string]int64, len(s.tokensByDay))
	for k, v := range s.tokensByDay {
		result.TokensByDay[k] = v
	}

	result.TokensByHour = make(map[string]int64, len(s.tokensByHour))
	for hour, v := range s.tokensByHour {
		key := formatHour(hour)
		result.TokensByHour[key] = v
	}

	return result, s.changeCount, s.persistedCount
}

// ProjectionSnapshot returns an immutable value copy of the current
// projection generation. It is intentionally separate from Snapshot so legacy
// callers continue to receive the frozen full-detail JSON shape.
func (s *RequestStatistics) ProjectionSnapshot() ProjectionSnapshot {
	if s == nil {
		return ProjectionSnapshot{SchemaVersion: projectionSchemaVersion}
	}
	s.mu.RLock()
	projection := s.projection
	s.mu.RUnlock()
	if projection == nil {
		return ProjectionSnapshot{SchemaVersion: projectionSchemaVersion}
	}
	return projection.Snapshot()
}

// ListAuthRequests returns a filtered, timestamp-descending page of request
// details for one auth_index.
func (s *RequestStatistics) ListAuthRequests(authIndex string, filter AuthRequestFilter) AuthRequestPage {
	page, _ := s.ListAuthRequestsWithError(authIndex, filter)
	return page
}

// ListAuthRequestsWithError preserves the legacy offset response shape while
// resolving candidates through the projection's auth posting list. It never
// scans the complete canonical API/model/detail hierarchy on a query path.
func (s *RequestStatistics) ListAuthRequestsWithError(authIndex string, filter AuthRequestFilter) (AuthRequestPage, error) {
	authIndex = strings.TrimSpace(authIndex)
	if filter.Limit <= 0 {
		filter.Limit = 50
	}
	if filter.Limit > 500 {
		filter.Limit = 500
	}
	if filter.Offset < 0 {
		filter.Offset = 0
	}

	page := AuthRequestPage{
		AuthIndex: authIndex,
		Limit:     filter.Limit,
		Offset:    filter.Offset,
		Items:     []AuthRequestDetail{},
	}
	if s == nil || authIndex == "" {
		return page, nil
	}

	s.mu.RLock()
	if s.projectionUnavailable {
		err := projectionQueryUnavailableError(s.projectionUnavailableCode)
		s.mu.RUnlock()
		return page, err
	}
	if s.projection == nil || s.detailLocations == nil || s.detailEventLocations == nil {
		s.mu.RUnlock()
		return page, ErrProjectionUnavailable
	}
	references, err := s.projection.queryAuthRequestRefs(authIndex, filter)
	if err != nil {
		s.mu.RUnlock()
		return page, err
	}
	items := make([]authRequestListItem, 0, len(references))
	for _, reference := range references {
		locationKey := s.detailEventLocations[reference.StableEventID]
		location, ok := s.detailLocations[locationKey]
		if !ok || location.modelStats == nil || location.index < 0 || location.index >= len(location.modelStats.Details) {
			s.mu.RUnlock()
			return page, ErrProjectionUnavailable
		}
		detail := cloneRequestDetail(location.modelStats.Details[location.index])
		detail = normalizeRequestDetail(detail, detail.Provider)
		if !authRequestDetailMatches(detail, authIndex, filter) {
			s.mu.RUnlock()
			return page, ErrProjectionUnavailable
		}
		if detail.Endpoint == "" {
			detail.Endpoint = location.apiName
		}
		if detail.Model == "" || detail.Model == "unknown" {
			detail.Model = location.modelName
		}
		items = append(items, authRequestListItem{apiBucket: location.apiName, detail: detail})
	}
	s.mu.RUnlock()

	sort.SliceStable(items, func(i, j int) bool {
		leftDetail := items[i].detail
		rightDetail := items[j].detail
		if !leftDetail.Timestamp.Equal(rightDetail.Timestamp) {
			return leftDetail.Timestamp.After(rightDetail.Timestamp)
		}
		if items[i].apiBucket != items[j].apiBucket {
			return items[i].apiBucket < items[j].apiBucket
		}
		leftIdentity := detailIdentityKey(items[i].apiBucket, leftDetail.Model, leftDetail)
		rightIdentity := detailIdentityKey(items[j].apiBucket, rightDetail.Model, rightDetail)
		if leftIdentity != rightIdentity {
			return leftIdentity < rightIdentity
		}
		return detailFactsHash(leftDetail) < detailFactsHash(rightDetail)
	})

	page.Total = len(items)
	if filter.Offset >= len(items) {
		return page, nil
	}
	end := filter.Offset + filter.Limit
	if end > len(items) {
		end = len(items)
	}
	page.Items = make([]AuthRequestDetail, 0, end-filter.Offset)
	for _, item := range items[filter.Offset:end] {
		page.Items = append(page.Items, item.detail)
	}
	return page, nil
}

func authRequestDetailMatches(detail AuthRequestDetail, authIndex string, filter AuthRequestFilter) bool {
	if strings.TrimSpace(detail.AuthIndex) != authIndex {
		return false
	}
	if model := strings.TrimSpace(filter.Model); model != "" && detail.Model != model {
		return false
	}
	if filter.Failed != nil && detail.Failed != *filter.Failed {
		return false
	}
	return projectionAuthRequestTimeMatches(detail.Timestamp, filter)
}

func buildAuthUsageSnapshots(apis map[string]APISnapshot) map[string]AuthUsageSnapshot {
	auths := make(map[string]AuthUsageSnapshot)
	for _, apiSnapshot := range apis {
		for modelName, modelSnapshot := range apiSnapshot.Models {
			modelName = strings.TrimSpace(modelName)
			if modelName == "" {
				modelName = "unknown"
			}
			for _, detail := range modelSnapshot.Details {
				detail = normalizeRequestDetail(detail, detail.Provider)
				authIndex := strings.TrimSpace(detail.AuthIndex)
				if authIndex == "" {
					continue
				}
				authSnapshot := auths[authIndex]
				if authSnapshot.AuthIndex == "" {
					authSnapshot.AuthIndex = authIndex
					authSnapshot.Models = make(map[string]AuthModelSnapshot)
				}
				addDetailToAuthUsage(&authSnapshot, modelName, detail)
				auths[authIndex] = authSnapshot
			}
		}
	}
	if len(auths) == 0 {
		return nil
	}
	return auths
}

func addDetailToAuthUsage(authSnapshot *AuthUsageSnapshot, modelName string, detail RequestDetail) {
	if authSnapshot == nil {
		return
	}
	authSnapshot.TotalRequests++
	if detail.Failed {
		authSnapshot.FailureCount++
	} else {
		authSnapshot.SuccessCount++
	}
	authSnapshot.Tokens = addRequestTokens(authSnapshot.Tokens, detail.Tokens, detail.Provider)
	updateAuthTimeRange(authSnapshot, detail.Timestamp)

	modelSnapshot := authSnapshot.Models[modelName]
	modelSnapshot.TotalRequests++
	if detail.Failed {
		modelSnapshot.FailureCount++
	} else {
		modelSnapshot.SuccessCount++
	}
	modelSnapshot.Tokens = addRequestTokens(modelSnapshot.Tokens, detail.Tokens, detail.Provider)
	authSnapshot.Models[modelName] = modelSnapshot
}

func updateAuthTimeRange(authSnapshot *AuthUsageSnapshot, timestamp time.Time) {
	if timestamp.IsZero() {
		return
	}
	ts := timestamp
	if authSnapshot.FirstRequestAt == nil || ts.Before(*authSnapshot.FirstRequestAt) {
		first := ts
		authSnapshot.FirstRequestAt = &first
	}
	if authSnapshot.LastRequestAt == nil || ts.After(*authSnapshot.LastRequestAt) {
		last := ts
		authSnapshot.LastRequestAt = &last
	}
}

type MergeResult struct {
	Added    int64 `json:"added"`
	Skipped  int64 `json:"skipped"`
	Enriched int64 `json:"enriched,omitempty"`
}

// MergeSnapshot merges an exported statistics snapshot into the current store.
// Existing data is preserved and duplicate request details are skipped. The
// legacy signature intentionally retains its historical error-free API.
func (s *RequestStatistics) MergeSnapshot(snapshot StatisticsSnapshot) MergeResult {
	result, _ := s.MergeSnapshotLegacyWithError(snapshot)
	return result
}

// MergeSnapshotWithError is the import/restore entry point that preserves
// projection admission failures for callers that can return machine-readable
// status. A failed batch is never reported as a successful MergeResult.
func (s *RequestStatistics) MergeSnapshotWithError(snapshot StatisticsSnapshot) (MergeResult, error) {
	if s != nil && s.coordinator != nil {
		result, err := s.coordinator.ApplySnapshot(context.Background(), snapshot)
		return result, err
	}
	return s.mergeSnapshotDirect(snapshot), nil
}

// MergeSnapshotWithIdentitySidecar restores a durable generation through the
// strict coordinator path while carrying the previously assigned identities. It
// is the canonical-detail replay fallback used when a v2 generation cannot be
// directly hydrated (legacy v1 generation or hydrate failure).
func (s *RequestStatistics) MergeSnapshotWithIdentitySidecar(snapshot StatisticsSnapshot, sidecar IdentitySidecar) (MergeResult, error) {
	if s != nil {
		s.replayFallbackCount.Add(1)
		if s.coordinator != nil {
			return s.coordinator.ApplySnapshotWithIdentitySidecar(context.Background(), snapshot, sidecar)
		}
	}
	return s.mergeSnapshotDirect(snapshot), nil
}

// ReplayFallbackCount returns how many restore/import calls used the
// canonical-detail replay path. A normal v2 restart that directly hydrates the
// persisted projection leaves this at zero.
func (s *RequestStatistics) ReplayFallbackCount() int64 {
	if s == nil {
		return 0
	}
	return s.replayFallbackCount.Load()
}

func (s *RequestStatistics) applyProjectionMetadata(sidecar IdentitySidecar, snapshot projectionGenerationState) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureProjectionLocked()
	s.projection.applyPersistedMetadata(sidecar, snapshot)
	s.identityMetadataReady = true
	s.identityMetadataGeneration = sidecar.Generation
	if s.coordinator != nil {
		s.coordinator.applyPersistedMetadata(sidecar)
	}
}

func (s *RequestStatistics) hasIdentityMetadata(generation uint64) bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.identityMetadataReady && s.identityMetadataGeneration == generation
}

// MergeSnapshotLegacyWithError keeps the historical public merge/restore
// semantics for unseeded snapshots while still routing through the mutation
// coordinator. It is intentionally separate from MergeSnapshotWithError so
// strict runtime imports can enforce fresh batch admissions.
func (s *RequestStatistics) MergeSnapshotLegacyWithError(snapshot StatisticsSnapshot) (MergeResult, error) {
	if s != nil {
		s.replayFallbackCount.Add(1)
		if s.coordinator != nil {
			result, err := s.coordinator.ApplySnapshotLegacy(context.Background(), snapshot)
			return result, err
		}
	}
	return s.mergeSnapshotDirect(snapshot), nil
}

func (s *RequestStatistics) mergeSnapshotDirect(snapshot StatisticsSnapshot) MergeResult {
	result := MergeResult{}
	if s == nil {
		return result
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for apiName, apiSnapshot := range snapshot.APIs {
		apiName = strings.TrimSpace(apiName)
		if apiName == "" {
			continue
		}
		for modelName, modelSnapshot := range apiSnapshot.Models {
			modelName = strings.TrimSpace(modelName)
			if modelName == "" {
				modelName = "unknown"
			}
			for _, detail := range modelSnapshot.Details {
				incomingGenerateExplicit := detail.Generate != nil
				detail.Endpoint = safeImportedEndpoint(apiName, detail.Endpoint)
				if detail.Model == "" {
					detail.Model = modelName
				}
				detail = normalizeRequestDetail(detail, detail.Provider)
				targetAPIName := safeImportedAPIName(apiName, detail)
				switch s.upsertDetailLockedWithGenerate(targetAPIName, modelName, detail, incomingGenerateExplicit) {
				case detailUpsertAdded:
					result.Added++
				case detailUpsertEnriched:
					result.Enriched++
				default:
					result.Skipped++
				}
			}
		}
	}

	return result
}

// HasPendingPersistence reports whether the in-memory snapshot contains changes
// that have not been durably persisted yet.
func (s *RequestStatistics) HasPendingPersistence() bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.changeCount != s.persistedCount
}

// MarkPersisted advances the persisted counter to the provided snapshot
// version. Newer in-memory changes remain pending.
func (s *RequestStatistics) MarkPersisted(version uint64) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if version > s.changeCount {
		version = s.changeCount
	}
	if version > s.persistedCount {
		s.persistedCount = version
	}
}

// MarkAllPersisted marks the current in-memory state as already persisted.
func (s *RequestStatistics) MarkAllPersisted() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.persistedCount = s.changeCount
}

func (s *RequestStatistics) markChangedLocked() {
	s.changeCount++
}

func resolveSuccess(ctx context.Context) bool {
	if ctx == nil {
		return true
	}
	if status := logging.GetResponseStatus(ctx); status != 0 {
		return status < httpStatusBadRequest
	}
	ginCtx, ok := ctx.Value("gin").(*gin.Context)
	if !ok || ginCtx == nil {
		return true
	}
	status := ginCtx.Writer.Status()
	if status == 0 {
		return true
	}
	return status < httpStatusBadRequest
}

const httpStatusBadRequest = 400

func normaliseLatency(latency time.Duration) int64 {
	if latency <= 0 {
		return 0
	}
	return latency.Milliseconds()
}

func formatHour(hour int) string {
	if hour < 0 {
		hour = 0
	}
	hour = hour % 24
	return fmt.Sprintf("%02d", hour)
}
