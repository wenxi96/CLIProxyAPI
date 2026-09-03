package management

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/usage"
)

func TestUsageSummaryReturnsExactSeriesHealthAndEmpty304(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stats := usage.NewRequestStatistics()
	base := time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)
	details := []usage.RequestDetail{
		{RequestID: "req-summary-first", Timestamp: base.Add(20 * time.Minute), Endpoint: "POST /v1/responses", Model: "gpt-first", Provider: "openai", AuthIndex: "auth-first", Source: "auth-first", Tokens: usage.RequestTokenStats{InputTokens: 3, TotalTokens: 3, TokenUsageSource: usage.TokenUsageSourceProvider}},
		{RequestID: "req-summary-second", Timestamp: base.Add(80 * time.Minute), Endpoint: "POST /v1/messages", Model: "claude-second", Provider: "anthropic", AuthIndex: "auth-second", Source: "auth-second", Tokens: usage.RequestTokenStats{OutputTokens: 5, TotalTokens: 5, TokenUsageSource: usage.TokenUsageSourceProvider}},
		{RequestID: "req-summary-tail", Timestamp: base.Add(106 * time.Minute), Endpoint: "POST /v1/responses", Model: "gpt-tail", Provider: "openai", AuthIndex: "auth-first", Source: "auth-first", Tokens: usage.RequestTokenStats{ReasoningTokens: 7, TotalTokens: 7, TokenUsageSource: usage.TokenUsageSourceProvider}},
	}
	snapshot := usage.StatisticsSnapshot{APIs: make(map[string]usage.APISnapshot)}
	for _, detail := range details {
		api := snapshot.APIs[detail.Endpoint]
		if api.Models == nil {
			api.Models = make(map[string]usage.ModelSnapshot)
		}
		api.Models[detail.Model] = usage.ModelSnapshot{Details: []usage.RequestDetail{detail}}
		snapshot.APIs[detail.Endpoint] = api
	}
	if _, err := stats.MergeSnapshotWithError(snapshot); err != nil {
		t.Fatalf("MergeSnapshotWithError() error = %v", err)
	}
	to := base.Add(107 * time.Minute)
	h := &Handler{usageStats: stats, usageNow: func() time.Time { return to.Add(time.Minute) }}
	router := gin.New()
	router.GET("/v0/management/usage/summary", h.GetUsageSummary)
	query := url.Values{
		"from":     {base.Add(15 * time.Minute).Format(time.RFC3339Nano)},
		"to":       {to.Format(time.RFC3339Nano)},
		"timezone": {"UTC"},
	}
	path := "/v0/management/usage/summary?" + query.Encode()
	first := httptest.NewRecorder()
	router.ServeHTTP(first, httptest.NewRequest(http.MethodGet, path, nil))
	if first.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", first.Code, first.Body.String())
	}
	if first.Header().Get("Cache-Control") != "no-store" || first.Header().Get("ETag") == "" {
		t.Fatalf("conditional headers = %+v", first.Header())
	}
	var payload struct {
		SchemaVersion         int          `json:"schema_version"`
		BillablePolicyVersion string       `json:"billable_policy_version"`
		Range                 usageRangeV1 `json:"range"`
		TokenCoverage         struct {
			Details      int64 `json:"details"`
			WithAnyUsage int64 `json:"with_any_usage"`
		} `json:"token_coverage"`
		Totals struct {
			Requests int64 `json:"requests"`
			Tokens   struct {
				TotalTokens int64 `json:"total_tokens"`
			} `json:"tokens"`
		} `json:"totals"`
		Models []struct {
			ID string `json:"id"`
		} `json:"models"`
		Sources []struct {
			ID string `json:"id"`
		} `json:"sources"`
		PricingGroups []struct {
			BillablePolicyVersion string `json:"billable_policy_version"`
			ProviderCoverage      string `json:"provider_coverage"`
		} `json:"pricing_groups"`
		Series struct {
			Granularity  string `json:"series_granularity"`
			PointCount   int    `json:"series_point_count"`
			Availability string `json:"series_availability"`
			Requests     []struct {
				Complete bool  `json:"complete"`
				Requests int64 `json:"requests"`
			} `json:"requests"`
			Health            []json.RawMessage `json:"health"`
			HealthPartialTail *struct {
				Complete bool `json:"complete"`
				Tokens   struct {
					TotalTokens int64 `json:"total_tokens"`
				} `json:"tokens"`
			} `json:"health_partial_tail"`
		} `json:"series"`
		HealthRange struct {
			BucketCount        int  `json:"bucket_count"`
			PartialTailPresent bool `json:"partial_tail_present"`
		} `json:"health_range"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode summary: %v", err)
	}
	if payload.SchemaVersion != 1 || payload.BillablePolicyVersion != usage.BillablePolicyVersionV1 ||
		payload.Range.Kind != "absolute" || !payload.Range.Complete ||
		payload.Totals.Requests != 3 || payload.Totals.Tokens.TotalTokens != 15 {
		t.Fatalf("summary facts = %+v", payload)
	}
	if payload.TokenCoverage.Details != 3 || payload.TokenCoverage.WithAnyUsage != 3 {
		t.Fatalf("top-level token coverage = %+v", payload.TokenCoverage)
	}
	if len(payload.Models) != 3 || payload.Models[0].ID != "claude-second" || payload.Models[1].ID != "gpt-first" || payload.Models[2].ID != "gpt-tail" {
		t.Fatalf("sorted models = %+v", payload.Models)
	}
	if len(payload.Sources) != 2 || payload.Sources[0].ID == "" || payload.Sources[1].ID == "" || payload.Sources[0].ID >= payload.Sources[1].ID {
		t.Fatalf("sorted sources = %+v", payload.Sources)
	}
	if len(payload.PricingGroups) != 3 {
		t.Fatalf("pricing groups = %+v", payload.PricingGroups)
	}
	for _, group := range payload.PricingGroups {
		if group.BillablePolicyVersion != usage.BillablePolicyVersionV1 || group.ProviderCoverage != "complete" {
			t.Fatalf("pricing group policy/provider coverage = %+v", group)
		}
	}
	if payload.Series.Granularity != "hour" || payload.Series.PointCount != 2 || payload.Series.Availability != "available" ||
		len(payload.Series.Requests) != 2 || payload.Series.Requests[0].Complete || payload.Series.Requests[1].Complete ||
		payload.Series.Requests[0].Requests != 1 || payload.Series.Requests[1].Requests != 2 {
		t.Fatalf("series = %+v", payload.Series)
	}
	if len(payload.Series.Health) != 672 || payload.HealthRange.BucketCount != 672 || !payload.HealthRange.PartialTailPresent ||
		payload.Series.HealthPartialTail == nil || payload.Series.HealthPartialTail.Complete ||
		payload.Series.HealthPartialTail.Tokens.TotalTokens != 7 {
		t.Fatalf("health = range:%+v series:%+v", payload.HealthRange, payload.Series)
	}

	conditionalRequest := httptest.NewRequest(http.MethodGet, path, nil)
	conditionalRequest.Header.Set("If-None-Match", first.Header().Get("ETag"))
	conditional := httptest.NewRecorder()
	router.ServeHTTP(conditional, conditionalRequest)
	if conditional.Code != http.StatusNotModified || conditional.Body.Len() != 0 {
		t.Fatalf("conditional response = status:%d body:%q", conditional.Code, conditional.Body.String())
	}
}

func TestUsageSummaryDegradesOnlyMainSeriesAtPointLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stats := usage.NewRequestStatistics()
	detail := usage.RequestDetail{
		RequestID: "req-summary-long-range", Timestamp: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Endpoint: "POST /v1/responses", Model: "gpt-5", Provider: "openai", AuthIndex: "auth-long-range",
		Source: "auth-long-range", Tokens: usage.RequestTokenStats{InputTokens: 9, TotalTokens: 9, TokenUsageSource: usage.TokenUsageSourceProvider},
	}
	if _, err := stats.MergeSnapshotWithError(usage.StatisticsSnapshot{APIs: map[string]usage.APISnapshot{
		detail.Endpoint: {Models: map[string]usage.ModelSnapshot{detail.Model: {Details: []usage.RequestDetail{detail}}}},
	}}); err != nil {
		t.Fatalf("MergeSnapshotWithError() error = %v", err)
	}

	h := &Handler{usageStats: stats, usageNow: func() time.Time { return time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC) }}
	router := gin.New()
	router.GET("/v0/management/usage/summary", h.GetUsageSummary)
	query := url.Values{
		"from":     {time.Date(1600, 1, 1, 0, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)},
		"to":       {time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)},
		"timezone": {"UTC"},
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v0/management/usage/summary?"+query.Encode(), nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
	var payload struct {
		Totals struct {
			Requests int64 `json:"requests"`
		} `json:"totals"`
		Series struct {
			Granularity  string `json:"series_granularity"`
			PointCount   int    `json:"series_point_count"`
			Availability string `json:"series_availability"`
			Error        struct {
				Code               string       `json:"code"`
				RequestedRange     usageRangeV1 `json:"requested_range"`
				MinimumGranularity string       `json:"minimum_granularity"`
			} `json:"series_error"`
			Requests []json.RawMessage `json:"requests"`
			Tokens   []json.RawMessage `json:"tokens"`
			Health   []json.RawMessage `json:"health"`
		} `json:"series"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode summary: %v", err)
	}
	if payload.Totals.Requests != 1 || payload.Series.Granularity != "year" || payload.Series.PointCount != 0 ||
		payload.Series.Availability != "unavailable" || len(payload.Series.Requests) != 0 || len(payload.Series.Tokens) != 0 ||
		len(payload.Series.Health) != 672 || payload.Series.Error.Code != "series_point_limit_exceeded" ||
		payload.Series.Error.MinimumGranularity != "year" || payload.Series.Error.RequestedRange.Kind != "absolute" {
		t.Fatalf("long-range summary = %+v", payload)
	}
}

func TestUsageSummaryAllWindowNormalizesRangeFromDataset(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Date(2026, 8, 10, 18, 0, 0, 123, time.UTC)
	earliest := now.Add(-48 * time.Hour)

	for _, testCase := range []struct {
		name     string
		populate func(*testing.T, *usage.RequestStatistics)
		wantFrom time.Time
	}{
		{
			name: "dataset earliest timestamp",
			populate: func(t *testing.T, stats *usage.RequestStatistics) {
				t.Helper()
				detail := usage.RequestDetail{
					RequestID: "req-summary-all", Timestamp: earliest, Endpoint: "POST /v1/responses",
					Model: "gpt-5", Provider: "openai", AuthIndex: "auth-all", Source: "auth-all",
					Tokens: usage.RequestTokenStats{InputTokens: 1, TotalTokens: 1, TokenUsageSource: usage.TokenUsageSourceProvider},
				}
				if _, err := stats.MergeSnapshotWithError(usage.StatisticsSnapshot{APIs: map[string]usage.APISnapshot{
					detail.Endpoint: {Models: map[string]usage.ModelSnapshot{detail.Model: {Details: []usage.RequestDetail{detail}}}},
				}}); err != nil {
					t.Fatalf("MergeSnapshotWithError() error = %v", err)
				}
			},
			wantFrom: earliest,
		},
		{name: "empty dataset", populate: func(*testing.T, *usage.RequestStatistics) {}, wantFrom: now},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			stats := usage.NewRequestStatistics()
			testCase.populate(t, stats)
			h := &Handler{usageStats: stats, usageNow: func() time.Time { return now }}
			router := gin.New()
			router.GET("/v0/management/usage/summary", h.GetUsageSummary)

			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v0/management/usage/summary?window=all&timezone=UTC", nil))
			if response.Code != http.StatusOK {
				t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
			}
			var payload struct {
				Range usageRangeV1 `json:"range"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
				t.Fatalf("decode summary: %v", err)
			}
			if payload.Range.Kind != "rolling" || payload.Range.Window != "all" ||
				payload.Range.From != testCase.wantFrom.Format(time.RFC3339Nano) ||
				payload.Range.To != now.Format(time.RFC3339Nano) {
				t.Fatalf("all range = %+v, want from=%s to=%s", payload.Range, testCase.wantFrom.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
			}
		})
	}
}

func TestUsageSummaryAbsoluteETagIgnoresAppendOutsideRange(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stats := usage.NewRequestStatistics()
	base := time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)
	inside := usage.RequestDetail{
		RequestID: "req-summary-etag-inside", Timestamp: base.Add(15 * time.Minute), Endpoint: "POST /v1/responses",
		Model: "gpt-5", Provider: "openai", AuthIndex: "auth-summary-etag", Source: "auth-summary-etag",
		Tokens: usage.RequestTokenStats{InputTokens: 1, TotalTokens: 1, TokenUsageSource: usage.TokenUsageSourceProvider},
	}
	if _, err := stats.MergeSnapshotWithError(usage.StatisticsSnapshot{APIs: map[string]usage.APISnapshot{
		inside.Endpoint: {Models: map[string]usage.ModelSnapshot{inside.Model: {Details: []usage.RequestDetail{inside}}}},
	}}); err != nil {
		t.Fatalf("MergeSnapshotWithError(inside) error = %v", err)
	}

	to := base.Add(time.Hour)
	h := &Handler{usageStats: stats, usageNow: func() time.Time { return to.Add(time.Hour) }}
	router := gin.New()
	router.GET("/v0/management/usage/summary", h.GetUsageSummary)
	query := url.Values{
		"from": {base.Format(time.RFC3339Nano)},
		"to":   {to.Format(time.RFC3339Nano)},
	}
	path := "/v0/management/usage/summary?" + query.Encode()
	first := httptest.NewRecorder()
	router.ServeHTTP(first, httptest.NewRequest(http.MethodGet, path, nil))
	if first.Code != http.StatusOK || first.Header().Get("ETag") == "" {
		t.Fatalf("first summary = status:%d headers:%+v body:%s", first.Code, first.Header(), first.Body.String())
	}

	outside := inside
	outside.RequestID = "req-summary-etag-outside"
	outside.Timestamp = to.Add(time.Minute)
	if _, err := stats.MergeSnapshotWithError(usage.StatisticsSnapshot{APIs: map[string]usage.APISnapshot{
		outside.Endpoint: {Models: map[string]usage.ModelSnapshot{outside.Model: {Details: []usage.RequestDetail{outside}}}},
	}}); err != nil {
		t.Fatalf("MergeSnapshotWithError(outside) error = %v", err)
	}

	conditional := httptest.NewRequest(http.MethodGet, path, nil)
	conditional.Header.Set("If-None-Match", first.Header().Get("ETag"))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, conditional)
	if response.Code != http.StatusNotModified || response.Body.Len() != 0 ||
		response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("ETag") != first.Header().Get("ETag") {
		t.Fatalf("outside-range conditional response = status:%d headers:%+v body:%q", response.Code, response.Header(), response.Body.String())
	}
}

func TestUsageSummaryAndEventsRejectInvalidQueries(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stats := usage.NewRequestStatistics()
	h := &Handler{usageStats: stats}
	router := gin.New()
	router.GET("/v0/management/usage/summary", h.GetUsageSummary)
	router.GET("/v0/management/usage/events", h.GetUsageEvents)

	for _, path := range []string{
		"/v0/management/usage/summary?window=unknown",
		"/v0/management/usage/summary?window=24h&from=2026-08-10T00:00:00Z",
		"/v0/management/usage/summary?from=2026-08-10T01:00:00Z&to=2026-08-10T01:00:00Z",
		"/v0/management/usage/events?window=unknown",
		"/v0/management/usage/events?from=2026-08-10T01:00:00Z&to=2026-08-10T01:00:00Z",
		"/v0/management/usage/events?limit=0",
	} {
		t.Run(path, func(t *testing.T) {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
			if response.Code != http.StatusBadRequest || response.Header().Get("Cache-Control") != "no-store" ||
				!strings.Contains(response.Body.String(), "invalid_query") {
				t.Fatalf("invalid query response = status:%d headers:%+v body:%s", response.Code, response.Header(), response.Body.String())
			}
		})
	}
}
