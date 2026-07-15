// Package usage provides usage tracking and logging functionality for the CLI Proxy API server.
// It includes plugins for monitoring API usage, token consumption, and other metrics
// to help with observability and billing purposes.
package usage

import (
	"context"
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

func init() {
	statisticsEnabled.Store(true)
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
	if !statisticsEnabled.Load() {
		return
	}
	if p == nil || p.stats == nil {
		return
	}
	p.stats.Record(ctx, record)
}

// SetStatisticsEnabled toggles whether in-memory statistics are recorded.
func SetStatisticsEnabled(enabled bool) { statisticsEnabled.Store(enabled) }

// StatisticsEnabled reports the current recording state.
func StatisticsEnabled() bool { return statisticsEnabled.Load() }

// RequestStatistics maintains aggregated request metrics in memory.
type RequestStatistics struct {
	mu sync.RWMutex

	totalRequests  int64
	successCount   int64
	failureCount   int64
	totalTokens    int64
	changeCount    uint64
	persistedCount uint64

	apis map[string]*apiStats

	requestsByDay   map[string]int64
	requestsByHour  map[int]int64
	tokensByDay     map[string]int64
	tokensByHour    map[int]int64
	detailLocations map[string]detailLocation
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
	Limit  int
	Offset int
	Model  string
	Failed *bool
	From   *time.Time
	To     *time.Time
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
	return &RequestStatistics{
		apis:            make(map[string]*apiStats),
		requestsByDay:   make(map[string]int64),
		requestsByHour:  make(map[int]int64),
		tokensByDay:     make(map[string]int64),
		tokensByHour:    make(map[int]int64),
		detailLocations: make(map[string]detailLocation),
	}
}

// Record ingests a new usage record and updates the aggregates.
func (s *RequestStatistics) Record(ctx context.Context, record coreusage.Record) {
	if s == nil {
		return
	}
	if !statisticsEnabled.Load() {
		return
	}
	detail := CanonicalRequestDetail(ctx, record)
	statsKey := safeAPIIdentifier(ctx, record, detail)

	s.mu.Lock()
	defer s.mu.Unlock()

	s.upsertDetailLocked(statsKey, detail.Model, detail)
}

type detailUpsertStatus int

const (
	detailUpsertSkipped detailUpsertStatus = iota
	detailUpsertAdded
	detailUpsertEnriched
)

type detailLocation struct {
	apiName    string
	modelName  string
	stats      *apiStats
	modelStats *modelStats
	index      int
}

func (s *RequestStatistics) upsertDetailLocked(apiName, model string, detail RequestDetail) detailUpsertStatus {
	detail = normalizeRequestDetail(detail, detail.Provider)
	apiName = safeImportedAPIName(apiName, detail)
	model = defaultIfEmpty(model, detail.Model)

	s.ensureDetailLocationsLocked()
	identity := detailIdentityKey(apiName, model, detail)
	if existing, ok := s.detailLocations[identity]; ok && existing.modelStats != nil && existing.index >= 0 && existing.index < len(existing.modelStats.Details) {
		current := existing.modelStats.Details[existing.index]
		if !shouldEnrichDetail(current, detail) {
			return detailUpsertSkipped
		}
		merged := mergeEnrichedDetail(current, detail)
		existing.modelStats.Details[existing.index] = merged
		s.applyTokenDeltaLocked(existing.stats, existing.modelStats, current, merged)
		s.applyOutcomeDeltaLocked(current, merged)
		delete(s.detailLocations, identity)
		s.detailLocations[detailIdentityKey(existing.apiName, existing.modelName, merged)] = existing
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
	s.addDetailLocked(apiName, stats, model, detail)
	return detailUpsertAdded
}

func (s *RequestStatistics) ensureDetailLocationsLocked() {
	if s.detailLocations != nil {
		return
	}
	s.detailLocations = make(map[string]detailLocation)
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
				if _, exists := s.detailLocations[identity]; !exists {
					s.detailLocations[identity] = detailLocation{
						apiName:    apiName,
						modelName:  modelName,
						stats:      stats,
						modelStats: modelStatsValue,
						index:      index,
					}
				}
			}
		}
	}
}

func (s *RequestStatistics) addDetailLocked(apiName string, stats *apiStats, model string, detail RequestDetail) {
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
	s.detailLocations[detailIdentityKey(apiName, model, detail)] = detailLocation{
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
			copy(requestDetails, modelStatsValue.Details)
			for index := range requestDetails {
				requestDetails[index].EstimatedCostUSD = cloneFloat64Ptr(requestDetails[index].EstimatedCostUSD)
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

// ListAuthRequests returns a filtered, timestamp-descending page of request
// details for one auth_index.
func (s *RequestStatistics) ListAuthRequests(authIndex string, filter AuthRequestFilter) AuthRequestPage {
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
		return page
	}

	s.mu.RLock()
	items := s.collectAuthRequestDetailsLocked(authIndex, filter)
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
		return page
	}
	end := filter.Offset + filter.Limit
	if end > len(items) {
		end = len(items)
	}
	page.Items = make([]AuthRequestDetail, 0, end-filter.Offset)
	for _, item := range items[filter.Offset:end] {
		page.Items = append(page.Items, item.detail)
	}
	return page
}

func (s *RequestStatistics) collectAuthRequestDetailsLocked(authIndex string, filter AuthRequestFilter) []authRequestListItem {
	items := make([]authRequestListItem, 0)
	modelFilter := strings.TrimSpace(filter.Model)
	for endpoint, stats := range s.apis {
		if stats == nil {
			continue
		}
		for modelName, modelStatsValue := range stats.Models {
			if modelStatsValue == nil {
				continue
			}
			if modelFilter != "" && modelName != modelFilter {
				continue
			}
			for _, detail := range modelStatsValue.Details {
				detail = normalizeRequestDetail(detail, detail.Provider)
				if strings.TrimSpace(detail.AuthIndex) != authIndex {
					continue
				}
				if filter.Failed != nil && detail.Failed != *filter.Failed {
					continue
				}
				if filter.From != nil && detail.Timestamp.Before(*filter.From) {
					continue
				}
				if filter.To != nil && detail.Timestamp.After(*filter.To) {
					continue
				}
				if detail.Endpoint == "" {
					detail.Endpoint = endpoint
				}
				if detail.Model == "" || detail.Model == "unknown" {
					detail.Model = modelName
				}
				items = append(items, authRequestListItem{apiBucket: endpoint, detail: detail})
			}
		}
	}
	return items
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
// Existing data is preserved and duplicate request details are skipped.
func (s *RequestStatistics) MergeSnapshot(snapshot StatisticsSnapshot) MergeResult {
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
				detail.Endpoint = safeImportedEndpoint(apiName, detail.Endpoint)
				if detail.Model == "" {
					detail.Model = modelName
				}
				detail = normalizeRequestDetail(detail, detail.Provider)
				targetAPIName := safeImportedAPIName(apiName, detail)
				switch s.upsertDetailLocked(targetAPIName, modelName, detail) {
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
