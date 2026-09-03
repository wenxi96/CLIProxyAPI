package usage

import "time"

// BillableTokenComponents is the normalized v1 pricing fact set. Amounts are
// token counts, not prices; the client-side price catalog remains authoritative
// for monetary calculations.
type BillableTokenComponents struct {
	InputTokens             int64 `json:"input_tokens"`
	OutputTokens            int64 `json:"output_tokens"`
	ReasoningTokens         int64 `json:"reasoning_tokens"`
	CacheReadTokens         int64 `json:"cache_read_tokens"`
	CacheCreationTokens     int64 `json:"cache_creation_tokens"`
	UnclassifiedCacheTokens int64 `json:"unclassified_cache_tokens"`
}

func (components BillableTokenComponents) Total() int64 {
	return components.InputTokens + components.OutputTokens + components.ReasoningTokens +
		components.CacheReadTokens + components.CacheCreationTokens + components.UnclassifiedCacheTokens
}

// ComponentMask identifies which billable token components are non-zero.
type ComponentMask uint8

const (
	ComponentInput ComponentMask = 1 << iota
	ComponentOutput
	ComponentReasoning
	ComponentCacheRead
	ComponentCacheCreation
	ComponentUnclassifiedCache
)

func (components BillableTokenComponents) Mask() ComponentMask {
	var mask ComponentMask
	if components.InputTokens > 0 {
		mask |= ComponentInput
	}
	if components.OutputTokens > 0 {
		mask |= ComponentOutput
	}
	if components.ReasoningTokens > 0 {
		mask |= ComponentReasoning
	}
	if components.CacheReadTokens > 0 {
		mask |= ComponentCacheRead
	}
	if components.CacheCreationTokens > 0 {
		mask |= ComponentCacheCreation
	}
	if components.UnclassifiedCacheTokens > 0 {
		mask |= ComponentUnclassifiedCache
	}
	return mask
}

type DetailClassification string

const (
	DetailClassPriceable            DetailClassification = "priceable"
	DetailClassUnknownUsage         DetailClassification = "unknown_usage"
	DetailClassKnownTotalOnly       DetailClassification = "known_total_only"
	DetailClassZeroBillableComplete DetailClassification = "zero_billable_complete"
)

// TokenCoverageAggregate tracks usage-source coverage separately from token
// totals so missing usage is not silently converted into zero.
type TokenCoverageAggregate struct {
	DetailCount        int64 `json:"detail_count"`
	KnownUsageCount    int64 `json:"known_usage_count"`
	MissingUsageCount  int64 `json:"missing_usage_count"`
	ProviderUsageCount int64 `json:"provider_usage_count"`
	ComputedUsageCount int64 `json:"computed_usage_count"`
}

// TokenFactsAggregate stores mergeable token counters for a bucket or facet.
type TokenFactsAggregate struct {
	InputTokens         int64 `json:"input_tokens"`
	OutputTokens        int64 `json:"output_tokens"`
	ReasoningTokens     int64 `json:"reasoning_tokens"`
	CachedTokens        int64 `json:"cached_tokens"`
	CacheReadTokens     int64 `json:"cache_read_tokens"`
	CacheCreationTokens int64 `json:"cache_creation_tokens"`
	TotalTokens         int64 `json:"total_tokens"`
}

// PricingAggregate is the dimension-local pricing fact set consumed by the
// summary DTO. It does not contain price amounts or raw credentials.
type PricingAggregate struct {
	BillablePolicyVersion           string                  `json:"billable_policy_version"`
	Provider                        string                  `json:"provider"`
	Model                           string                  `json:"model"`
	PriceKey                        string                  `json:"price_key"`
	ProviderRawPresence             string                  `json:"provider_raw_presence"`
	BillableTokens                  BillableTokenComponents `json:"billable_tokens"`
	ComponentMaskCounts             map[ComponentMask]int64 `json:"component_mask_counts"`
	PriceableDetailCount            int64                   `json:"priceable_detail_count"`
	UnknownUsageDetailCount         int64                   `json:"unknown_usage_detail_count"`
	KnownTotalOnlyDetailCount       int64                   `json:"known_total_only_detail_count"`
	ZeroBillableCompleteDetailCount int64                   `json:"zero_billable_complete_detail_count"`
	UnclassifiedCacheDetailCount    int64                   `json:"unclassified_cache_detail_count"`
}

// Aggregate contains the mergeable facts shared by global and dimension
// buckets. PricingGroups are repeated per dimension to prevent global counts
// from being reused for a filtered dimension.
type Aggregate struct {
	TotalRequests int64                       `json:"total_requests"`
	SuccessCount  int64                       `json:"success_count"`
	FailureCount  int64                       `json:"failure_count"`
	LatencyMsSum  int64                       `json:"latency_ms_sum"`
	Tokens        TokenFactsAggregate         `json:"tokens"`
	TokenCoverage TokenCoverageAggregate      `json:"token_coverage"`
	PricingGroups map[string]PricingAggregate `json:"pricing_groups"`
	Facets        map[string]int64            `json:"facets"`
}

type HealthBucket struct {
	IntervalStart time.Time `json:"interval_start"`
	IntervalEnd   time.Time `json:"interval_end"`
	TotalRequests int64     `json:"total_requests"`
	SuccessCount  int64     `json:"success_count"`
	FailureCount  int64     `json:"failure_count"`
	LatencyMsSum  int64     `json:"latency_ms_sum"`
}

// EventRef is intentionally small. Posting lists carry identity and ordering
// metadata, while canonical details remain owned by RequestStatistics.
type EventRef struct {
	StableEventID          string    `json:"stable_event_id"`
	CanonicalEventIdentity string    `json:"canonical_event_identity"`
	Sequence               uint64    `json:"sequence"`
	BatchOrdinal           uint64    `json:"batch_ordinal"`
	Version                uint64    `json:"version"`
	Timestamp              time.Time `json:"timestamp"`
	API                    string    `json:"api"`
	Model                  string    `json:"model"`
	Provider               string    `json:"provider"`
	AuthIndex              string    `json:"auth_index"`
	SourceID               string    `json:"source_id"`
	Failed                 bool      `json:"failed"`
}

type CatalogEntry struct {
	ID            string `json:"id"`
	Label         string `json:"label,omitempty"`
	PriceKey      string `json:"price_key,omitempty"`
	Provider      string `json:"provider,omitempty"`
	ProviderState string `json:"provider_state,omitempty"`
}

type ProjectionCatalog struct {
	Models    map[string]CatalogEntry `json:"models"`
	PriceKeys map[string]CatalogEntry `json:"price_keys"`
	Sources   map[string]CatalogEntry `json:"sources"`
}

type HourBucket struct {
	IntervalStart time.Time            `json:"interval_start"`
	IntervalEnd   time.Time            `json:"interval_end"`
	Totals        Aggregate            `json:"totals"`
	APIs          map[string]Aggregate `json:"apis"`
	Models        map[string]Aggregate `json:"models"`
	Providers     map[string]Aggregate `json:"providers"`
	Auths         map[string]Aggregate `json:"auths"`
	Sources       map[string]Aggregate `json:"sources"`
}

type ProjectionSnapshot struct {
	SchemaVersion   string                  `json:"schema_version"`
	DatasetEpoch    uint64                  `json:"dataset_epoch"`
	Revision        uint64                  `json:"revision"`
	RewriteRevision uint64                  `json:"rewrite_revision"`
	Totals          Aggregate               `json:"totals"`
	APIs            map[string]Aggregate    `json:"apis,omitempty"`
	Models          map[string]Aggregate    `json:"models,omitempty"`
	Providers       map[string]Aggregate    `json:"providers,omitempty"`
	Auths           map[string]Aggregate    `json:"auths,omitempty"`
	Sources         map[string]Aggregate    `json:"sources,omitempty"`
	Facets          map[string]int64        `json:"facets,omitempty"`
	Hours           map[string]HourBucket   `json:"hours"`
	Days            map[string]HourBucket   `json:"days"`
	Weeks           map[string]HourBucket   `json:"weeks"`
	Months          map[string]HourBucket   `json:"months"`
	Years           map[string]HourBucket   `json:"years"`
	Health          map[string]HealthBucket `json:"health"`
	Postings        map[string][]EventRef   `json:"postings"`
	Catalog         ProjectionCatalog       `json:"catalog"`
	Events          []EventRef              `json:"events"`
}

type ProjectionSeriesPoint struct {
	IntervalStart time.Time `json:"interval_start"`
	IntervalEnd   time.Time `json:"interval_end"`
	Complete      bool      `json:"complete"`
	Totals        Aggregate `json:"totals"`
}

type ProjectionHealthPoint struct {
	IntervalStart time.Time           `json:"interval_start"`
	IntervalEnd   time.Time           `json:"interval_end"`
	Complete      bool                `json:"complete"`
	TotalRequests int64               `json:"total_requests"`
	SuccessCount  int64               `json:"success_count"`
	FailureCount  int64               `json:"failure_count"`
	LatencyMsSum  int64               `json:"latency_ms_sum"`
	Tokens        TokenFactsAggregate `json:"tokens"`
}

type ProjectionSummaryView struct {
	Summary             ProjectionSnapshot      `json:"summary"`
	RangeFrom           time.Time               `json:"range_from"`
	RangeTo             time.Time               `json:"range_to"`
	SeriesGranularity   string                  `json:"series_granularity"`
	SeriesAvailability  string                  `json:"series_availability"`
	SeriesError         string                  `json:"series_error,omitempty"`
	Series              []ProjectionSeriesPoint `json:"series"`
	Health              []ProjectionHealthPoint `json:"health"`
	HealthPartialTail   *ProjectionHealthPoint  `json:"health_partial_tail,omitempty"`
	HealthObservationTo time.Time               `json:"health_observation_to"`
	HealthAlignedTo     time.Time               `json:"health_aligned_to"`
	HealthFrom          time.Time               `json:"health_from"`
}

type ProjectionMutationResult struct {
	StableEventID          string `json:"stable_event_id"`
	CanonicalEventIdentity string `json:"canonical_event_identity"`
	Version                uint64 `json:"version"`
	Added                  bool   `json:"added"`
	Enriched               bool   `json:"enriched"`
	Skipped                bool   `json:"skipped"`
	RewriteRevision        uint64 `json:"rewrite_revision"`
}

// ProjectionIdentity carries the immutable identity assigned by the mutation
// coordinator. A zero value keeps the projection's standalone allocation
// behavior for legacy callers and tests.
type ProjectionIdentity struct {
	CanonicalIdentitySeed  string
	CanonicalEventIdentity string
	StableEventID          string
	Sequence               uint64
	BatchOrdinal           uint64
	SourceGroupKey         string
	SourceGroupOrdinal     uint64
}

type projectionContribution struct {
	RequestID       string
	DetailRole      string
	DetailSequence  string
	API             string
	Endpoint        string
	Model           string
	Provider        string
	AuthIndex       string
	SourceID        string
	SourceKey       string
	PriceKey        string
	ProviderState   string
	Timestamp       time.Time
	Failed          bool
	LatencyMs       int64
	Tokens          RequestTokenStats
	Billable        BillableTokenComponents
	Classification  DetailClassification
	ComponentMask   ComponentMask
	APIID           projectionStringID
	ModelID         projectionStringID
	ProviderID      projectionStringID
	AuthID          projectionStringID
	SourceIDValue   projectionStringID
	PriceKeyID      projectionStringID
	ProviderStateID projectionStringID
	FacetIDs        [5]projectionStringID
	EventRef        EventRef
}

type projectionEvent struct {
	CanonicalIdentitySeed  string
	CanonicalEventIdentity string
	StableEventID          string
	SourceGroupKey         string
	SourceGroupOrdinal     uint64
	CanonicalDetailHash    string
	Version                uint64
	FactRowID              projectionFactRowID
}
