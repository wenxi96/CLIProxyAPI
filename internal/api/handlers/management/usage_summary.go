package management

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/usage"
)

type usageTokenFactsV1 struct {
	InputTokens         int64 `json:"input_tokens"`
	OutputTokens        int64 `json:"output_tokens"`
	ReasoningTokens     int64 `json:"reasoning_tokens"`
	CachedTokens        int64 `json:"cached_tokens"`
	CacheReadTokens     int64 `json:"cache_read_tokens"`
	CacheCreationTokens int64 `json:"cache_creation_tokens"`
	TotalTokens         int64 `json:"total_tokens"`
}

type usageTokenCoverageV1 struct {
	Details           int64 `json:"details"`
	WithAnyUsage      int64 `json:"with_any_usage"`
	UnknownUsage      int64 `json:"unknown_usage"`
	KnownTotalOnly    int64 `json:"known_total_only"`
	UnclassifiedCache int64 `json:"unclassified_cache"`
}

type usageSummaryTotalsV1 struct {
	Requests     int64                `json:"requests"`
	Success      int64                `json:"success"`
	Failure      int64                `json:"failure"`
	LatencyMsSum int64                `json:"latency_ms_sum"`
	Tokens       usageTokenFactsV1    `json:"tokens"`
	Coverage     usageTokenCoverageV1 `json:"token_coverage"`
}

type usagePricingGroupV1 struct {
	Key                             string                     `json:"key"`
	PriceKey                        string                     `json:"price_key"`
	BillablePolicyVersion           string                     `json:"billable_policy_version"`
	Provider                        string                     `json:"provider"`
	ProviderRawPresence             string                     `json:"provider_raw_presence"`
	ProviderCoverage                string                     `json:"provider_coverage"`
	Model                           string                     `json:"model"`
	BillableTokens                  usageEventBillableTokensV1 `json:"billable_tokens"`
	ComponentCounts                 map[string]int64           `json:"component_counts"`
	PriceableDetailCount            int64                      `json:"priceable_detail_count"`
	UnknownUsageDetailCount         int64                      `json:"unknown_usage_detail_count"`
	KnownTotalOnlyDetailCount       int64                      `json:"known_total_only_detail_count"`
	ZeroBillableCompleteDetailCount int64                      `json:"zero_billable_complete_detail_count"`
	UnclassifiedCacheDetailCount    int64                      `json:"unclassified_cache_detail_count"`
}

type usageSummaryDimensionV1 struct {
	ID            string                `json:"id"`
	Label         string                `json:"label"`
	Requests      int64                 `json:"requests"`
	Success       int64                 `json:"success"`
	Failure       int64                 `json:"failure"`
	LatencyMsSum  int64                 `json:"latency_ms_sum"`
	Tokens        usageTokenFactsV1     `json:"tokens"`
	TokenCoverage usageTokenCoverageV1  `json:"token_coverage"`
	PricingGroups []usagePricingGroupV1 `json:"pricing_groups"`
}

type usageFacetValueV1 struct {
	ID    string `json:"id"`
	Count int64  `json:"count"`
}

type usageSummaryFacetsV1 struct {
	Models    []usageFacetValueV1 `json:"models"`
	Providers []usageFacetValueV1 `json:"providers"`
	Sources   []usageFacetValueV1 `json:"sources"`
	Auths     []usageFacetValueV1 `json:"auths"`
	Failed    []bool              `json:"failed"`
}

type usageSeriesRequestPointV1 struct {
	Start        string `json:"start"`
	End          string `json:"end"`
	Complete     bool   `json:"complete"`
	Requests     int64  `json:"requests"`
	Success      int64  `json:"success"`
	Failure      int64  `json:"failure"`
	LatencyMsSum int64  `json:"latency_ms_sum"`
}

type usageSeriesTokenPointV1 struct {
	Start    string            `json:"start"`
	End      string            `json:"end"`
	Complete bool              `json:"complete"`
	Tokens   usageTokenFactsV1 `json:"tokens"`
}

type usageHealthPointV1 struct {
	Start        string            `json:"start"`
	End          string            `json:"end"`
	Complete     bool              `json:"complete"`
	Requests     int64             `json:"requests"`
	Success      int64             `json:"success"`
	Failure      int64             `json:"failure"`
	LatencyMsSum int64             `json:"latency_ms_sum"`
	Tokens       usageTokenFactsV1 `json:"tokens"`
}

type usageSummarySeriesV1 struct {
	Granularity       string                      `json:"series_granularity"`
	PointCount        int                         `json:"series_point_count"`
	Availability      string                      `json:"series_availability"`
	Error             any                         `json:"series_error"`
	Requests          []usageSeriesRequestPointV1 `json:"requests"`
	Tokens            []usageSeriesTokenPointV1   `json:"tokens"`
	Health            []usageHealthPointV1        `json:"health"`
	HealthPartialTail *usageHealthPointV1         `json:"health_partial_tail"`
}

type usageSeriesErrorV1 struct {
	Code               string       `json:"code"`
	RequestedRange     usageRangeV1 `json:"requested_range"`
	MinimumGranularity string       `json:"minimum_granularity"`
}

type usageHealthRangeV1 struct {
	ObservationTo      string `json:"observation_to"`
	From               string `json:"from"`
	To                 string `json:"to"`
	AlignedTo          string `json:"aligned_to"`
	BucketSeconds      int64  `json:"bucket_seconds"`
	BucketCount        int    `json:"bucket_count"`
	PartialTailPresent bool   `json:"partial_tail_present"`
}

type usageSummaryFactsV1 struct {
	SchemaVersion         int                       `json:"schema_version"`
	BillablePolicyVersion string                    `json:"billable_policy_version"`
	DatasetEpoch          string                    `json:"dataset_epoch"`
	Revision              uint64                    `json:"revision"`
	Range                 usageRangeV1              `json:"range"`
	Totals                usageSummaryTotalsV1      `json:"totals"`
	TokenCoverage         usageTokenCoverageV1      `json:"token_coverage"`
	APIs                  []usageSummaryDimensionV1 `json:"apis"`
	Models                []usageSummaryDimensionV1 `json:"models"`
	Providers             []usageSummaryDimensionV1 `json:"providers"`
	Auths                 []usageSummaryDimensionV1 `json:"auths"`
	Sources               []usageSummaryDimensionV1 `json:"sources"`
	Facets                usageSummaryFacetsV1      `json:"facets"`
	Series                usageSummarySeriesV1      `json:"series"`
	HealthRange           usageHealthRangeV1        `json:"health_range"`
	HealthWatermark       string                    `json:"health_watermark"`
	PricingGroups         []usagePricingGroupV1     `json:"pricing_groups"`
}

type usageSummaryResponseV1 struct {
	usageSummaryFactsV1
	GeneratedAt time.Time `json:"generated_at"`
}

// GetUsageSummary returns an exact compact-fact summary with bounded series
// and a fixed seven-day health representation.
func (h *Handler) GetUsageSummary(c *gin.Context) {
	setUsageNoStore(c)
	if h == nil || h.usageStats == nil {
		writeUsageProjectionError(c, usage.ErrProjectionUnavailable)
		return
	}
	if err := h.ensureUsageTokenState(); err != nil {
		writeUsageProjectionError(c, err)
		return
	}
	datasetEpoch, _, _, err := h.usageStats.ProjectionMetadata()
	if err != nil {
		writeUsageProjectionError(c, err)
		return
	}
	from, to, rangeDTO, err := h.resolveUsageSummaryRange(c, datasetEpoch)
	if err != nil {
		if err == errUsageWindowAnchorExpired {
			writeUsageQueryError(c, http.StatusConflict, "window_anchor_expired")
			return
		}
		writeUsageQueryError(c, http.StatusBadRequest, "invalid_query")
		return
	}
	view, err := h.usageStats.QueryProjectionSummaryView(from, to, to)
	if err != nil {
		writeUsageEventsProjectionError(c, err)
		return
	}
	if view.Summary.DatasetEpoch != datasetEpoch {
		if rangeDTO.Kind == "rolling" {
			writeUsageQueryError(c, http.StatusConflict, "window_anchor_expired")
		} else {
			writeUsageProjectionError(c, usage.ErrProjectionUnavailable)
		}
		return
	}
	if rangeDTO.Kind == "rolling" && rangeDTO.Window == "all" {
		rangeDTO.From = canonicalUsageQueryTime(view.RangeFrom)
		rangeDTO.To = canonicalUsageQueryTime(view.RangeTo)
	}
	facts, err := buildUsageSummaryFactsV1(view, rangeDTO)
	if err != nil {
		writeUsageProjectionError(c, err)
		return
	}
	etagFacts := facts
	etagFacts.Revision = 0
	etag, err := usageRepresentationETag("summary-v1", etagFacts)
	if err != nil {
		writeUsageProjectionError(c, err)
		return
	}
	setUsageConditionalHeaders(c, etag, view.Summary.Revision)
	if usageIfNoneMatch(c.GetHeader("If-None-Match"), etag) {
		c.Status(http.StatusNotModified)
		return
	}
	c.JSON(http.StatusOK, usageSummaryResponseV1{usageSummaryFactsV1: facts, GeneratedAt: h.usageCurrentTime()})
}

func (h *Handler) resolveUsageSummaryRange(c *gin.Context, datasetEpoch uint64) (time.Time, time.Time, usageRangeV1, error) {
	timezone := strings.TrimSpace(c.Query("timezone"))
	if timezone == "" {
		timezone = "UTC"
	}
	if _, err := time.LoadLocation(timezone); err != nil {
		return time.Time{}, time.Time{}, usageRangeV1{}, err
	}
	window := strings.ToLower(strings.TrimSpace(c.Query("window")))
	anchorToken := strings.TrimSpace(c.Query("anchor"))
	rawFrom := strings.TrimSpace(c.Query("from"))
	rawTo := strings.TrimSpace(c.Query("to"))
	if window != "" && (rawFrom != "" || rawTo != "") {
		return time.Time{}, time.Time{}, usageRangeV1{}, errUsageTokenInvalid
	}
	if window == "" && rawFrom == "" && rawTo == "" {
		window = "24h"
	}
	if window != "" {
		var anchor usageWindowAnchorV1
		var err error
		if anchorToken == "" {
			anchorToken, anchor, err = h.usageWindowAnchors.mint(datasetEpoch, window, timezone)
		} else {
			anchor, err = h.usageWindowAnchors.resolve(anchorToken, datasetEpoch, h.usageCurrentTime())
		}
		if err != nil {
			return time.Time{}, time.Time{}, usageRangeV1{}, err
		}
		if anchor.Window != window || anchor.Timezone != timezone {
			return time.Time{}, time.Time{}, usageRangeV1{}, errUsageTokenInvalid
		}
		return anchor.From, anchor.To, usageRangeV1{
			Kind: "rolling", Window: window, From: canonicalUsageQueryTime(anchor.From), To: canonicalUsageQueryTime(anchor.To),
			Timezone: timezone, WindowAnchor: anchorToken, AnchorMintedAt: canonicalUsageQueryTime(anchor.MintedAt),
			AnchorNonce: anchor.AnchorNonce, AnchorExpiresAt: canonicalUsageQueryTime(anchor.ExpiresAt), Complete: true,
		}, nil
	}
	if anchorToken != "" || rawFrom == "" || rawTo == "" {
		return time.Time{}, time.Time{}, usageRangeV1{}, errUsageTokenInvalid
	}
	from, err := parseUsageRequestTime(rawFrom)
	if err != nil {
		return time.Time{}, time.Time{}, usageRangeV1{}, err
	}
	to, err := parseUsageRequestTime(rawTo)
	if err != nil || !from.Before(to) {
		return time.Time{}, time.Time{}, usageRangeV1{}, errUsageTokenInvalid
	}
	from = from.UTC()
	to = to.UTC()
	return from, to, usageRangeV1{
		Kind: "absolute", From: canonicalUsageQueryTime(from), To: canonicalUsageQueryTime(to), Timezone: timezone, Complete: true,
	}, nil
}

func buildUsageSummaryFactsV1(view usage.ProjectionSummaryView, rangeDTO usageRangeV1) (usageSummaryFactsV1, error) {
	totals := usageSummaryTotalsFromAggregate(view.Summary.Totals)
	series := usageSummarySeriesV1{
		Granularity: view.SeriesGranularity, PointCount: len(view.Series), Availability: view.SeriesAvailability,
		Requests: make([]usageSeriesRequestPointV1, 0, len(view.Series)),
		Tokens:   make([]usageSeriesTokenPointV1, 0, len(view.Series)),
		Health:   make([]usageHealthPointV1, 0, len(view.Health)),
	}
	if view.SeriesError != "" {
		series.Error = usageSeriesErrorV1{
			Code:               view.SeriesError,
			RequestedRange:     rangeDTO,
			MinimumGranularity: "year",
		}
	}
	for _, point := range view.Series {
		series.Requests = append(series.Requests, usageSeriesRequestPointV1{
			Start: canonicalUsageQueryTime(point.IntervalStart), End: canonicalUsageQueryTime(point.IntervalEnd), Complete: point.Complete,
			Requests: point.Totals.TotalRequests, Success: point.Totals.SuccessCount, Failure: point.Totals.FailureCount,
			LatencyMsSum: point.Totals.LatencyMsSum,
		})
		series.Tokens = append(series.Tokens, usageSeriesTokenPointV1{
			Start: canonicalUsageQueryTime(point.IntervalStart), End: canonicalUsageQueryTime(point.IntervalEnd),
			Complete: point.Complete, Tokens: usageTokenFactsFromAggregate(point.Totals.Tokens),
		})
	}
	for _, point := range view.Health {
		series.Health = append(series.Health, usageHealthPointFromProjection(point))
	}
	if view.HealthPartialTail != nil {
		partial := usageHealthPointFromProjection(*view.HealthPartialTail)
		series.HealthPartialTail = &partial
	}
	healthRange := usageHealthRangeV1{
		ObservationTo: canonicalUsageQueryTime(view.HealthObservationTo),
		From:          canonicalUsageQueryTime(view.HealthFrom), To: canonicalUsageQueryTime(view.HealthAlignedTo),
		AlignedTo: canonicalUsageQueryTime(view.HealthAlignedTo), BucketSeconds: 900,
		BucketCount: len(view.Health), PartialTailPresent: view.HealthPartialTail != nil,
	}
	healthWatermark, err := usageRepresentationETag("health-v1", struct {
		DatasetEpoch uint64               `json:"dataset_epoch"`
		Range        usageHealthRangeV1   `json:"range"`
		Health       []usageHealthPointV1 `json:"health"`
		Partial      *usageHealthPointV1  `json:"partial"`
	}{view.Summary.DatasetEpoch, healthRange, series.Health, series.HealthPartialTail})
	if err != nil {
		return usageSummaryFactsV1{}, err
	}
	return usageSummaryFactsV1{
		SchemaVersion:         1,
		BillablePolicyVersion: usage.BillablePolicyVersionV1,
		DatasetEpoch:          usageDatasetEpochID(view.Summary.DatasetEpoch),
		Revision:              view.Summary.Revision,
		Range:                 rangeDTO,
		Totals:                totals,
		TokenCoverage:         totals.Coverage,
		APIs:                  usageSummaryDimensions(view.Summary.APIs),
		Models:                usageSummaryDimensions(view.Summary.Models),
		Providers:             usageSummaryDimensions(view.Summary.Providers),
		Auths:                 usageSummaryDimensions(view.Summary.Auths),
		Sources:               usageSummaryDimensions(view.Summary.Sources),
		Facets:                usageSummaryFacets(view.Summary.Facets), Series: series, HealthRange: healthRange,
		HealthWatermark: healthWatermark, PricingGroups: usagePricingGroups(view.Summary.Totals.PricingGroups),
	}, nil
}

func usageSummaryTotalsFromAggregate(aggregate usage.Aggregate) usageSummaryTotalsV1 {
	knownTotalOnly, unclassified := usagePricingCoverageCounts(aggregate.PricingGroups)
	return usageSummaryTotalsV1{
		Requests: aggregate.TotalRequests, Success: aggregate.SuccessCount, Failure: aggregate.FailureCount,
		LatencyMsSum: aggregate.LatencyMsSum, Tokens: usageTokenFactsFromAggregate(aggregate.Tokens),
		Coverage: usageTokenCoverageV1{
			Details: aggregate.TokenCoverage.DetailCount, WithAnyUsage: aggregate.TokenCoverage.KnownUsageCount,
			UnknownUsage: aggregate.TokenCoverage.MissingUsageCount, KnownTotalOnly: knownTotalOnly, UnclassifiedCache: unclassified,
		},
	}
}

func usageSummaryDimensions(values map[string]usage.Aggregate) []usageSummaryDimensionV1 {
	result := make([]usageSummaryDimensionV1, 0, len(values))
	for id, aggregate := range values {
		totals := usageSummaryTotalsFromAggregate(aggregate)
		result = append(result, usageSummaryDimensionV1{
			ID: id, Label: id, Requests: totals.Requests, Success: totals.Success, Failure: totals.Failure,
			LatencyMsSum: totals.LatencyMsSum, Tokens: totals.Tokens, TokenCoverage: totals.Coverage,
			PricingGroups: usagePricingGroups(aggregate.PricingGroups),
		})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}

func usagePricingGroups(values map[string]usage.PricingAggregate) []usagePricingGroupV1 {
	result := make([]usagePricingGroupV1, 0, len(values))
	for key, aggregate := range values {
		result = append(result, usagePricingGroupV1{
			Key: key, PriceKey: aggregate.PriceKey, BillablePolicyVersion: aggregate.BillablePolicyVersion,
			Provider: aggregate.Provider, ProviderRawPresence: aggregate.ProviderRawPresence,
			ProviderCoverage: usageProviderCoverage(aggregate.ProviderRawPresence), Model: aggregate.Model,
			BillableTokens: usageEventBillableTokensV1{
				Input: aggregate.BillableTokens.InputTokens, Output: aggregate.BillableTokens.OutputTokens,
				Reasoning: aggregate.BillableTokens.ReasoningTokens, CacheRead: aggregate.BillableTokens.CacheReadTokens,
				CacheCreation: aggregate.BillableTokens.CacheCreationTokens, UnsplitCache: aggregate.BillableTokens.UnclassifiedCacheTokens,
			},
			ComponentCounts:      usageComponentCounts(aggregate.ComponentMaskCounts),
			PriceableDetailCount: aggregate.PriceableDetailCount, UnknownUsageDetailCount: aggregate.UnknownUsageDetailCount,
			KnownTotalOnlyDetailCount:       aggregate.KnownTotalOnlyDetailCount,
			ZeroBillableCompleteDetailCount: aggregate.ZeroBillableCompleteDetailCount,
			UnclassifiedCacheDetailCount:    aggregate.UnclassifiedCacheDetailCount,
		})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Key < result[j].Key })
	return result
}

func usageProviderCoverage(rawPresence string) string {
	switch rawPresence {
	case "present":
		return "complete"
	case "indeterminate_legacy":
		return "legacy_indeterminate"
	case "missing":
		return "legacy_missing"
	case "literal_unknown":
		return "unknown"
	case "mixed":
		return "partial"
	default:
		return "partial"
	}
}

func usageComponentCounts(values map[usage.ComponentMask]int64) map[string]int64 {
	result := make(map[string]int64, len(values))
	for mask, count := range values {
		parts := make([]string, 0, 6)
		for _, component := range []struct {
			mask usage.ComponentMask
			name string
		}{
			{usage.ComponentInput, "input"}, {usage.ComponentOutput, "output"}, {usage.ComponentReasoning, "reasoning"},
			{usage.ComponentCacheRead, "cache_read"}, {usage.ComponentCacheCreation, "cache_creation"},
			{usage.ComponentUnclassifiedCache, "unclassified_cache"},
		} {
			if mask&component.mask != 0 {
				parts = append(parts, component.name)
			}
		}
		name := strings.Join(parts, "|")
		if name == "" {
			name = "none"
		}
		result[name] = count
	}
	return result
}

func usagePricingCoverageCounts(values map[string]usage.PricingAggregate) (knownTotalOnly, unclassified int64) {
	for _, aggregate := range values {
		knownTotalOnly += aggregate.KnownTotalOnlyDetailCount
		unclassified += aggregate.UnclassifiedCacheDetailCount
	}
	return knownTotalOnly, unclassified
}

func usageSummaryFacets(values map[string]int64) usageSummaryFacetsV1 {
	result := usageSummaryFacetsV1{Failed: []bool{true, false}}
	for key, count := range values {
		for _, target := range []struct {
			prefix string
			values *[]usageFacetValueV1
		}{
			{"model:", &result.Models}, {"provider:", &result.Providers}, {"source:", &result.Sources}, {"auth:", &result.Auths},
		} {
			if strings.HasPrefix(key, target.prefix) {
				*target.values = append(*target.values, usageFacetValueV1{ID: strings.TrimPrefix(key, target.prefix), Count: count})
				break
			}
		}
	}
	for _, values := range []*[]usageFacetValueV1{&result.Models, &result.Providers, &result.Sources, &result.Auths} {
		sort.Slice(*values, func(i, j int) bool { return (*values)[i].ID < (*values)[j].ID })
	}
	return result
}

func usageTokenFactsFromAggregate(tokens usage.TokenFactsAggregate) usageTokenFactsV1 {
	return usageTokenFactsV1{
		InputTokens: tokens.InputTokens, OutputTokens: tokens.OutputTokens, ReasoningTokens: tokens.ReasoningTokens,
		CachedTokens: tokens.CachedTokens, CacheReadTokens: tokens.CacheReadTokens,
		CacheCreationTokens: tokens.CacheCreationTokens, TotalTokens: tokens.TotalTokens,
	}
}

func usageHealthPointFromProjection(point usage.ProjectionHealthPoint) usageHealthPointV1 {
	return usageHealthPointV1{
		Start: canonicalUsageQueryTime(point.IntervalStart), End: canonicalUsageQueryTime(point.IntervalEnd), Complete: point.Complete,
		Requests: point.TotalRequests, Success: point.SuccessCount, Failure: point.FailureCount,
		LatencyMsSum: point.LatencyMsSum, Tokens: usageTokenFactsFromAggregate(point.Tokens),
	}
}

func marshalUsageSummaryFacts(value usageSummaryFactsV1) ([]byte, error) {
	return json.Marshal(value)
}
