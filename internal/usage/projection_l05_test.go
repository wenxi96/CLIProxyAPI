package usage

import (
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

type projectionSummaryQuerier interface {
	QuerySummary(from, to time.Time) (ProjectionSnapshot, error)
}

type projectionCatalogQuerier interface {
	QueryCatalog() (ProjectionSnapshot, error)
}

type projectionEventsQuerier interface {
	QueryEvents(from, to time.Time, limit int, snapshotMaxSequence, physicalScanPosition uint64, filters map[string][]string) (ProjectionSnapshot, bool, uint64, uint64, string, error)
}

func TestUsageSourceKeyV1PreservesEmptyValue(t *testing.T) {
	if got := UsageSourceKeyV1(RequestDetail{}); got != "" {
		t.Fatalf("UsageSourceKeyV1() = %q, want empty", got)
	}
}

func TestUsageRequestStatisticsL05BudgetErrorPropagatesToProjectionQueries(t *testing.T) {
	stats := NewRequestStatistics()
	config := DefaultMutationCoordinatorConfig()
	config.ProjectionBudget.MaxFactRows = 1
	stats.coordinator = NewMutationCoordinatorWithConfig(stats, config)
	fixtures := projectionCapacityFixtures(2)

	if err := stats.RecordWithError(fixtures[0].ctx, fixtures[0].record); err != nil {
		t.Fatalf("first RecordWithError() error = %v", err)
	}
	if err := stats.RecordWithError(fixtures[1].ctx, fixtures[1].record); !errors.Is(err, ErrProjectionBudgetExceeded) {
		t.Fatalf("second RecordWithError() error = %v, want ErrProjectionBudgetExceeded", err)
	}

	assertBudgetError := func(name string, err error) {
		t.Helper()
		if !errors.Is(err, ErrProjectionBudgetExceeded) {
			t.Fatalf("%s error = %v, want ErrProjectionBudgetExceeded", name, err)
		}
	}
	_, _, _, err := stats.ProjectionMetadata()
	assertBudgetError("ProjectionMetadata", err)
	_, err = stats.QueryProjectionCatalog()
	assertBudgetError("QueryProjectionCatalog", err)
	_, err = stats.QueryProjectionAuthUsage()
	assertBudgetError("QueryProjectionAuthUsage", err)
	_, err = stats.QueryProjectionSummaryView(time.Time{}, fixtures[0].detail.Timestamp.Add(time.Hour), fixtures[0].detail.Timestamp.Add(time.Hour))
	assertBudgetError("QueryProjectionSummaryView", err)
	_, _, _, _, _, err = stats.QueryProjectionEvents(time.Time{}, time.Time{}, 1, 0, 0, nil)
	assertBudgetError("QueryProjectionEvents", err)
	_, err = stats.ListAuthRequestsWithError("auth-budget", AuthRequestFilter{Limit: 1})
	assertBudgetError("ListAuthRequestsWithError", err)
}

func TestUsageRequestStatisticsL05LegacyAuthOffsetUsesProjectionPosting(t *testing.T) {
	stats := NewRequestStatistics()
	base := time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)
	details := []RequestDetail{
		{RequestID: "req-auth-offset-first", Timestamp: base, Endpoint: "POST /v1/responses", Model: "gpt-5", Provider: "openai", AuthIndex: "auth-offset", Source: "auth-offset"},
		{RequestID: "req-auth-offset-second", Timestamp: base.Add(time.Minute), Endpoint: "POST /v1/messages", Model: "claude-sonnet", Provider: "anthropic", AuthIndex: "auth-offset", Source: "auth-offset"},
		{RequestID: "req-auth-offset-other", Timestamp: base.Add(2 * time.Minute), Endpoint: "POST /v1/responses", Model: "gpt-other", Provider: "openai", AuthIndex: "auth-other", Source: "auth-other"},
	}
	snapshot := StatisticsSnapshot{APIs: make(map[string]APISnapshot)}
	for _, detail := range details {
		api := snapshot.APIs[detail.Endpoint]
		if api.Models == nil {
			api.Models = make(map[string]ModelSnapshot)
		}
		api.Models[detail.Model] = ModelSnapshot{Details: []RequestDetail{detail}}
		snapshot.APIs[detail.Endpoint] = api
	}
	if _, err := stats.MergeSnapshotWithError(snapshot); err != nil {
		t.Fatalf("MergeSnapshotWithError() error = %v", err)
	}

	stats.mu.Lock()
	stats.apis = nil
	stats.mu.Unlock()

	page := stats.ListAuthRequests("auth-offset", AuthRequestFilter{Limit: 10})
	if page.Total != 2 || len(page.Items) != 2 ||
		page.Items[0].RequestID != "req-auth-offset-second" || page.Items[1].RequestID != "req-auth-offset-first" {
		t.Fatalf("projection-backed auth page = %+v", page)
	}
}

func TestUsageProjectionL05SourceIDSeparatesSafeCoordinates(t *testing.T) {
	projection := NewUsageProjection()
	timestamp := time.Date(2026, 8, 10, 8, 0, 0, 0, time.UTC)
	first := projectionTestDetail("req-source-openai", timestamp, "openai", "gpt-5", RequestTokenStats{InputTokens: 10, TotalTokens: 10})
	first.AuthIndex = "auth-shared"
	first.Source = "sk-live-secret-that-must-not-leak"
	first.ExecutorType = "OpenAIExecutor"
	second := first
	second.RequestID = "req-source-anthropic"
	second.Model = "claude-sonnet"
	second.ExecutorType = "AnthropicExecutor"

	if result := projection.ApplyDetail(first.Endpoint, first); !result.Added {
		t.Fatalf("first source detail was not added: %+v", result)
	}
	if result := projection.ApplyDetail(second.Endpoint, second); !result.Added {
		t.Fatalf("second source detail was not added: %+v", result)
	}

	if firstKey, secondKey := UsageSourceKeyV1(first), UsageSourceKeyV1(second); firstKey != "auth-shared" || secondKey != firstKey {
		t.Fatalf("source keys = %q/%q, want shared auth-safe key", firstKey, secondKey)
	}
	snapshot := projection.Snapshot()
	if len(snapshot.Catalog.Sources) != 2 {
		t.Fatalf("catalog source count = %d, want 2 coordinate-specific source ids", len(snapshot.Catalog.Sources))
	}

	sourceIDs := make([]string, 0, len(snapshot.Catalog.Sources))
	for sourceID := range snapshot.Catalog.Sources {
		if sourceID == "auth-shared" {
			t.Fatalf("source id reused source key: %q", sourceID)
		}
		if strings.Contains(sourceID, "sk-live-secret") {
			t.Fatalf("source id leaked raw source: %q", sourceID)
		}
		sourceIDs = append(sourceIDs, sourceID)
	}
	sort.Strings(sourceIDs)

	pricing, err := projection.ResolvePricing(ProjectionPricingQuery{
		DimensionType: "source",
		DimensionIDs:  []string{sourceIDs[1], sourceIDs[0], sourceIDs[1]},
	})
	if err != nil {
		t.Fatalf("ResolvePricing() error = %v", err)
	}
	want := make(map[string]PricingAggregate)
	for _, sourceID := range sourceIDs {
		resolved, resolveErr := projection.ResolvePricing(ProjectionPricingQuery{
			DimensionType: "source",
			DimensionIDs:  []string{sourceID},
		})
		if resolveErr != nil {
			t.Fatalf("ResolvePricing(%q) error = %v", sourceID, resolveErr)
		}
		for key, aggregate := range resolved.PricingGroups {
			want[key] = aggregate
		}
	}
	if !reflect.DeepEqual(pricing.PricingGroups, want) {
		t.Fatalf("multi-source pricing mismatch: got=%#v want=%#v", pricing.PricingGroups, want)
	}
	if pricing.ScannedFactRows != 2 {
		t.Fatalf("multi-source scan count = %d, want 2 without duplicate-id rescans", pricing.ScannedFactRows)
	}
}

func TestUsageProjectionL05BudgetUsesSourceIDCoordinate(t *testing.T) {
	projection := NewUsageProjection()
	first := projectionTestDetail(
		"req-source-budget-first",
		time.Date(2026, 8, 10, 8, 0, 0, 0, time.UTC),
		"openai",
		"gpt-5",
		RequestTokenStats{InputTokens: 1, TotalTokens: 1},
	)
	first.AuthIndex = "auth-source-budget"
	first.Source = "raw-source-value"
	first.AuthType = "api_key"
	first.ExecutorType = "OpenAIExecutor"
	if result := projection.ApplyDetail(first.Endpoint, first); !result.Added {
		t.Fatalf("first source detail was not added: %+v", result)
	}

	budget := projection.Budget()
	budget.MaxRegistryEntries = projection.StorageMetrics().RegistryEntries
	projection.SetBudget(budget)

	second := first
	second.RequestID = "req-source-budget-second"
	second.Timestamp = second.Timestamp.Add(time.Minute)
	if err := projection.CheckDetailBudget(second.Endpoint, second, ProjectionIdentity{}); err != nil {
		t.Fatalf("same source-id budget preflight error = %v", err)
	}
}

func TestUsageProjectionL05SummaryUsesExactHalfOpenRange(t *testing.T) {
	projection := NewUsageProjection()
	base := time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)
	details := []RequestDetail{
		projectionTestDetail("req-before", base, "openai", "gpt-before", RequestTokenStats{InputTokens: 1, TotalTokens: 1}),
		projectionTestDetail("req-first", base.Add(20*time.Minute), "openai", "gpt-first", RequestTokenStats{InputTokens: 10, TotalTokens: 10}),
		projectionTestDetail("req-second", base.Add(50*time.Minute), "anthropic", "claude-second", RequestTokenStats{OutputTokens: 20, TotalTokens: 20}),
		projectionTestDetail("req-at-to", base.Add(time.Hour), "openai", "gpt-after", RequestTokenStats{InputTokens: 100, TotalTokens: 100}),
	}
	details[1].Endpoint = "POST /v1/responses"
	details[1].AuthIndex = "auth-first"
	details[1].Source = "auth-first"
	details[1].ExecutorType = "OpenAIExecutor"
	details[2].Endpoint = "POST /v1/messages"
	details[2].AuthIndex = "auth-second"
	details[2].Source = "auth-second"
	details[2].ExecutorType = "AnthropicExecutor"
	for _, detail := range details {
		if result := projection.ApplyDetail(detail.Endpoint, detail); !result.Added {
			t.Fatalf("detail %q was not added: %+v", detail.RequestID, result)
		}
	}

	querier, ok := any(projection).(projectionSummaryQuerier)
	if !ok {
		t.Fatal("UsageProjection.QuerySummary is not implemented")
	}
	summary, err := querier.QuerySummary(base.Add(15*time.Minute), base.Add(time.Hour))
	if err != nil {
		t.Fatalf("QuerySummary() error = %v", err)
	}
	if summary.Totals.TotalRequests != 2 || summary.Totals.SuccessCount != 2 || summary.Totals.FailureCount != 0 {
		t.Fatalf("summary totals = %+v, want two in-range requests", summary.Totals)
	}
	if summary.Totals.Tokens.InputTokens != 10 || summary.Totals.Tokens.OutputTokens != 20 || summary.Totals.Tokens.TotalTokens != 30 {
		t.Fatalf("summary tokens = %+v, want exact half-open range totals", summary.Totals.Tokens)
	}
	if len(summary.Events) != 0 || len(summary.Postings) != 0 {
		t.Fatalf("lightweight summary leaked event payloads: events=%d postings=%d", len(summary.Events), len(summary.Postings))
	}

	payload, err := json.Marshal(summary)
	if err != nil {
		t.Fatalf("marshal summary: %v", err)
	}
	var dimensions struct {
		APIs      map[string]Aggregate `json:"apis"`
		Models    map[string]Aggregate `json:"models"`
		Providers map[string]Aggregate `json:"providers"`
		Auths     map[string]Aggregate `json:"auths"`
		Sources   map[string]Aggregate `json:"sources"`
	}
	if err := json.Unmarshal(payload, &dimensions); err != nil {
		t.Fatalf("unmarshal summary dimensions: %v", err)
	}
	if dimensions.APIs["POST /v1/responses"].TotalRequests != 1 || dimensions.APIs["POST /v1/messages"].TotalRequests != 1 {
		t.Fatalf("API dimensions = %+v, want exact in-range counts", dimensions.APIs)
	}
	if dimensions.Models["gpt-first"].TotalRequests != 1 || dimensions.Models["claude-second"].TotalRequests != 1 {
		t.Fatalf("model dimensions = %+v, want exact in-range counts", dimensions.Models)
	}
	if len(dimensions.Sources) != 2 {
		t.Fatalf("source dimensions = %d, want 2 coordinate-specific ids", len(dimensions.Sources))
	}
}

func TestUsageProjectionL05SummaryViewLimitsCandidatesToRangeAndHealthWindow(t *testing.T) {
	projection := NewUsageProjection()
	base := time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)
	for index := 0; index < 128; index++ {
		detail := projectionTestDetail(
			"req-summary-historical-"+strconv.Itoa(index),
			base.AddDate(0, 0, -30).Add(time.Duration(index)*time.Minute),
			"openai",
			"gpt-historical",
			RequestTokenStats{InputTokens: 1, TotalTokens: 1},
		)
		if result := projection.ApplyDetail(detail.Endpoint, detail); !result.Added {
			t.Fatalf("historical detail %d was not added: %+v", index, result)
		}
	}
	for index := 0; index < 2; index++ {
		detail := projectionTestDetail(
			"req-summary-current-"+strconv.Itoa(index),
			base.Add(time.Duration(index+1)*15*time.Minute),
			"openai",
			"gpt-current",
			RequestTokenStats{InputTokens: 1, TotalTokens: 1},
		)
		if result := projection.ApplyDetail(detail.Endpoint, detail); !result.Added {
			t.Fatalf("current detail %d was not added: %+v", index, result)
		}
	}

	budget := projection.Budget()
	budget.MaxScannedFactRows = 2
	projection.SetBudget(budget)

	view, err := projection.QuerySummaryView(base, base.Add(time.Hour), base.Add(time.Hour))
	if err != nil {
		t.Fatalf("QuerySummaryView() error = %v, want range-local candidate scan", err)
	}
	if view.Summary.Totals.TotalRequests != 2 || view.Summary.Totals.Tokens.TotalTokens != 2 {
		t.Fatalf("summary totals = %+v, want two current details", view.Summary.Totals)
	}
	summary, err := projection.QuerySummary(base, base.Add(time.Hour))
	if err != nil {
		t.Fatalf("QuerySummary() error = %v, want range-local candidate scan", err)
	}
	if summary.Totals.TotalRequests != 2 || summary.Totals.Tokens.TotalTokens != 2 {
		t.Fatalf("query summary totals = %+v, want two current details", summary.Totals)
	}
	events, _, _, _, _, err := projection.QueryEvents(base, base.Add(time.Hour), 10, 0, 0, nil)
	if err != nil {
		t.Fatalf("QueryEvents() error = %v, want range-local candidate scan", err)
	}
	if len(events.Events) != 2 {
		t.Fatalf("range events = %d, want two current details", len(events.Events))
	}
	authFrom, authTo := base, base.Add(time.Hour)
	authReferences, err := projection.queryAuthRequestRefs("auth-projection", AuthRequestFilter{
		From: &authFrom, To: &authTo, Limit: 10,
	})
	if err != nil {
		t.Fatalf("queryAuthRequestRefs() error = %v, want range-local candidate scan", err)
	}
	if len(authReferences) != 2 {
		t.Fatalf("range auth refs = %d, want two current details", len(authReferences))
	}
	inclusiveAt := base.Add(15 * time.Minute)
	inclusiveReferences, err := projection.queryAuthRequestRefs("auth-projection", AuthRequestFilter{
		From: &inclusiveAt, To: &inclusiveAt, Limit: 10,
	})
	if err != nil || len(inclusiveReferences) != 1 {
		t.Fatalf("inclusive auth refs = %d err=%v, want one exact-boundary detail", len(inclusiveReferences), err)
	}
	emptyFrom, emptyTo := base.Add(time.Hour), base
	emptyReferences, err := projection.queryAuthRequestRefs("auth-projection", AuthRequestFilter{
		From: &emptyFrom, To: &emptyTo, Limit: 10,
	})
	if err != nil || len(emptyReferences) != 0 {
		t.Fatalf("reversed auth range refs = %d err=%v, want empty legacy result", len(emptyReferences), err)
	}
	cloned := projection.Clone()
	clonedView, err := cloned.QuerySummaryView(base, base.Add(time.Hour), base.Add(time.Hour))
	if err != nil {
		t.Fatalf("cloned QuerySummaryView() error = %v", err)
	}
	if !reflect.DeepEqual(clonedView.Summary.Totals, view.Summary.Totals) {
		t.Fatalf("cloned summary totals = %+v, want %+v", clonedView.Summary.Totals, view.Summary.Totals)
	}
}

func TestUsageProjectionL05SummaryViewUsesCompleteHourRollups(t *testing.T) {
	projection := NewUsageProjection()
	base := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	for index := 0; index < 6; index++ {
		detail := projectionTestDetail(
			"req-summary-rollup-"+strconv.Itoa(index),
			base.Add(time.Duration(index)*time.Hour+15*time.Minute),
			"openai",
			"gpt-rollup",
			RequestTokenStats{InputTokens: 1, TotalTokens: 1},
		)
		if result := projection.ApplyDetail(detail.Endpoint, detail); !result.Added {
			t.Fatalf("ApplyDetail(%d) = %+v, want added", index, result)
		}
	}

	view, err := projection.QuerySummaryView(base, base.Add(6*time.Hour), base.Add(6*time.Hour))
	if err != nil {
		t.Fatalf("QuerySummaryView() error = %v, want complete-hour rollups", err)
	}
	if view.Summary.Totals.TotalRequests != 6 || len(view.Series) != 6 {
		t.Fatalf("summary view = totals:%+v series:%d, want six rollup-backed requests", view.Summary.Totals, len(view.Series))
	}
	for index, point := range view.Series {
		if !point.Complete || point.Totals.TotalRequests != 1 {
			t.Fatalf("series point %d = %+v, want complete one-request rollup", index, point)
		}
	}
}

func TestUsageProjectionL05CatalogIsLightweightAndAllTime(t *testing.T) {
	projection := NewUsageProjection()
	first := projectionTestDetail("req-catalog-first", time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), "openai", "gpt-5", RequestTokenStats{InputTokens: 1, TotalTokens: 1})
	first.AuthIndex = "Auth-Catalog"
	first.Source = "raw-secret-not-for-catalog"
	first.ExecutorType = "OpenAIExecutor"
	second := projectionTestDetail("req-catalog-second", time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC), "anthropic", "claude-sonnet", RequestTokenStats{OutputTokens: 1, TotalTokens: 1})
	second.AuthIndex = "auth-second"
	second.Source = "auth-second"
	second.ExecutorType = "AnthropicExecutor"
	for _, detail := range []RequestDetail{first, second} {
		if result := projection.ApplyDetail(detail.Endpoint, detail); !result.Added {
			t.Fatalf("catalog detail %q was not added: %+v", detail.RequestID, result)
		}
	}

	querier, ok := any(projection).(projectionCatalogQuerier)
	if !ok {
		t.Fatal("UsageProjection.QueryCatalog is not implemented")
	}
	catalog, err := querier.QueryCatalog()
	if err != nil {
		t.Fatalf("QueryCatalog() error = %v", err)
	}
	if len(catalog.Events) != 0 || len(catalog.Postings) != 0 || len(catalog.Hours) != 0 {
		t.Fatalf("catalog copied heavy projection data: events=%d postings=%d hours=%d", len(catalog.Events), len(catalog.Postings), len(catalog.Hours))
	}
	if len(catalog.Catalog.Models) != 2 || len(catalog.Catalog.PriceKeys) != 2 || len(catalog.Catalog.Sources) != 2 {
		t.Fatalf("catalog sizes = models:%d prices:%d sources:%d, want all-time 2/2/2",
			len(catalog.Catalog.Models), len(catalog.Catalog.PriceKeys), len(catalog.Catalog.Sources))
	}
	firstSourceID := UsageSourceIDV1(first)
	if entry := catalog.Catalog.Sources[firstSourceID]; entry.Label != "Auth-Catalog" {
		t.Fatalf("catalog source entry = %+v, want safe source key label", entry)
	}
	for sourceID, entry := range catalog.Catalog.Sources {
		if strings.Contains(sourceID, "raw-secret") || strings.Contains(entry.Label, "raw-secret") {
			t.Fatalf("catalog leaked raw source: id=%q entry=%+v", sourceID, entry)
		}
	}
}

func TestUsageProjectionL05BulkEventsRetainBatchOrdinal(t *testing.T) {
	stats := NewRequestStatistics()
	timestamp := time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC)
	first := projectionTestDetail("req-batch-first", timestamp, "openai", "gpt-first", RequestTokenStats{InputTokens: 1, TotalTokens: 1})
	second := projectionTestDetail("req-batch-second", timestamp, "openai", "gpt-second", RequestTokenStats{OutputTokens: 1, TotalTokens: 1})
	snapshot := StatisticsSnapshot{APIs: map[string]APISnapshot{
		"POST /v1/responses": {
			Models: map[string]ModelSnapshot{
				first.Model:  {Details: []RequestDetail{first}},
				second.Model: {Details: []RequestDetail{second}},
			},
		},
	}}
	result, err := stats.MergeSnapshotWithError(snapshot)
	if err != nil {
		t.Fatalf("MergeSnapshotWithError() error = %v", err)
	}
	if result.Added != 2 {
		t.Fatalf("merge result = %+v, want two added events", result)
	}

	payload, err := json.Marshal(stats.ProjectionSnapshot().Events)
	if err != nil {
		t.Fatalf("marshal projection events: %v", err)
	}
	var events []struct {
		BatchOrdinal uint64 `json:"batch_ordinal"`
	}
	if err := json.Unmarshal(payload, &events); err != nil {
		t.Fatalf("unmarshal projection events: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("event count = %d, want 2", len(events))
	}
	ordinals := []uint64{events[0].BatchOrdinal, events[1].BatchOrdinal}
	sort.Slice(ordinals, func(i, j int) bool { return ordinals[i] < ordinals[j] })
	if !reflect.DeepEqual(ordinals, []uint64{0, 1}) {
		t.Fatalf("batch ordinals = %v, want [0 1]", ordinals)
	}
}

func TestUsageProjectionL05EventsCursorIsAppendStableAndSourceOR(t *testing.T) {
	projection := NewUsageProjection()
	timestamp := time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)
	first := projectionTestDetail("req-events-first", timestamp, "openai", "gpt-first", RequestTokenStats{InputTokens: 1, TotalTokens: 1})
	first.AuthIndex = "auth-first"
	first.Source = "auth-first"
	first.ExecutorType = "OpenAIExecutor"
	second := projectionTestDetail("req-events-second", timestamp, "anthropic", "claude-second", RequestTokenStats{OutputTokens: 1, TotalTokens: 1})
	second.AuthIndex = "auth-second"
	second.Source = "auth-second"
	second.ExecutorType = "AnthropicExecutor"
	third := projectionTestDetail("req-events-third", timestamp.Add(-time.Minute), "openai", "gpt-third", RequestTokenStats{InputTokens: 2, TotalTokens: 2})
	third.AuthIndex = "auth-first"
	third.Source = "auth-first"
	third.ExecutorType = "OpenAIExecutor"

	apply := func(detail RequestDetail, sequence, batchOrdinal, sourceOrdinal uint64) ProjectionMutationResult {
		seed := CanonicalIdentitySeedV1(detail, sourceOrdinal)
		canonicalIdentity := CanonicalEventIdentityV1(seed)
		return projection.ApplyDetailWithIdentity(detail.Endpoint, detail, ProjectionIdentity{
			CanonicalIdentitySeed:  seed,
			CanonicalEventIdentity: canonicalIdentity,
			StableEventID:          StableEventIDV1(canonicalIdentity),
			Sequence:               sequence,
			BatchOrdinal:           batchOrdinal,
			SourceGroupKey:         SourceGroupKeyV1(detail),
			SourceGroupOrdinal:     sourceOrdinal,
		})
	}
	if result := apply(first, 10, 0, 0); !result.Added {
		t.Fatalf("first event was not added: %+v", result)
	}
	if result := apply(second, 10, 1, 0); !result.Added {
		t.Fatalf("second event was not added: %+v", result)
	}
	if result := apply(third, 20, 0, 1); !result.Added {
		t.Fatalf("third event was not added: %+v", result)
	}

	querier, ok := any(projection).(projectionEventsQuerier)
	if !ok {
		t.Fatal("UsageProjection.QueryEvents is not implemented")
	}
	sources := []string{UsageSourceIDV1(second), UsageSourceIDV1(first), UsageSourceIDV1(second)}
	page, hasMore, nextPosition, snapshotMaxSequence, postingID, err := querier.QueryEvents(
		timestamp.Add(-time.Hour), timestamp.Add(time.Hour), 2, 0, 0, map[string][]string{"source": sources},
	)
	if err != nil {
		t.Fatalf("QueryEvents(first page) error = %v", err)
	}
	if !hasMore || nextPosition == 0 || snapshotMaxSequence != 20 || postingID == "" {
		t.Fatalf("first page metadata = has_more:%v next:%d max:%d posting:%q", hasMore, nextPosition, snapshotMaxSequence, postingID)
	}
	if len(page.Events) != 2 || page.Events[0].StableEventID != StableEventIDV1(CanonicalEventIdentityV1(CanonicalIdentitySeedV1(second, 0))) ||
		page.Events[1].StableEventID != StableEventIDV1(CanonicalEventIdentityV1(CanonicalIdentitySeedV1(first, 0))) {
		t.Fatalf("first page order = %+v, want timestamp/sequence/batch ordinal descending", page.Events)
	}

	appended := projectionTestDetail("req-events-appended", timestamp.Add(30*time.Minute), "openai", "gpt-appended", RequestTokenStats{InputTokens: 100, TotalTokens: 100})
	appended.AuthIndex = "auth-first"
	appended.Source = "auth-first"
	appended.ExecutorType = "OpenAIExecutor"
	if result := apply(appended, 21, 0, 2); !result.Added {
		t.Fatalf("appended event was not added: %+v", result)
	}

	secondPage, secondHasMore, _, secondMaxSequence, secondPostingID, err := querier.QueryEvents(
		timestamp.Add(-time.Hour), timestamp.Add(time.Hour), 2, snapshotMaxSequence, nextPosition, map[string][]string{"source": sources},
	)
	if err != nil {
		t.Fatalf("QueryEvents(second page) error = %v", err)
	}
	if secondHasMore || secondMaxSequence != snapshotMaxSequence || secondPostingID != postingID {
		t.Fatalf("second page metadata = has_more:%v max:%d posting:%q", secondHasMore, secondMaxSequence, secondPostingID)
	}
	if len(secondPage.Events) != 1 || secondPage.Events[0].StableEventID != StableEventIDV1(CanonicalEventIdentityV1(CanonicalIdentitySeedV1(third, 1))) {
		t.Fatalf("second page = %+v, want only pre-snapshot third event", secondPage.Events)
	}
}

func TestUsageProjectionL05EventsCursorReusesFrozenPageOrderAfterAppend(t *testing.T) {
	projection := NewUsageProjection()
	base := time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)
	first := projectionTestDetail("req-events-cache-first", base.Add(2*time.Minute), "openai", "gpt-first", RequestTokenStats{InputTokens: 1, TotalTokens: 1})
	first.AuthIndex = "auth-events-cache"
	first.Source = first.AuthIndex
	first.ExecutorType = "OpenAIExecutor"
	second := first
	second.RequestID = "req-events-cache-second"
	second.Timestamp = base.Add(time.Minute)
	nonMatching := first
	nonMatching.RequestID = "req-events-cache-nonmatching"
	nonMatching.Timestamp = base
	nonMatching.AuthIndex = "auth-events-cache-other"
	nonMatching.Source = nonMatching.AuthIndex

	for _, detail := range []RequestDetail{first, second, nonMatching} {
		if result := projection.ApplyDetail(detail.Endpoint, detail); !result.Added {
			t.Fatalf("ApplyDetail(%q) = %+v, want added event", detail.RequestID, result)
		}
	}

	from := base.Add(-time.Hour)
	to := base.Add(time.Hour)
	filters := map[string][]string{"source": {UsageSourceIDV1(first)}}
	firstPage, hasMore, nextPosition, snapshotMaxSequence, postingID, err := projection.QueryEvents(from, to, 1, 0, 0, filters)
	if err != nil {
		t.Fatalf("QueryEvents(first page) error = %v", err)
	}
	if len(firstPage.Events) != 1 || firstPage.Events[0].StableEventID == "" || !hasMore || nextPosition != 1 || postingID == "" {
		t.Fatalf("first page = %+v has_more=%t next=%d posting=%q", firstPage.Events, hasMore, nextPosition, postingID)
	}
	if snapshotMaxSequence != 2 {
		t.Fatalf("snapshot max sequence = %d, want matching high-water 2", snapshotMaxSequence)
	}

	for index := 0; index < 3; index++ {
		appended := first
		appended.RequestID = "req-events-cache-appended-" + strconv.Itoa(index)
		appended.Timestamp = base.Add(time.Duration(index+3) * time.Minute)
		if result := projection.ApplyDetail(appended.Endpoint, appended); !result.Added {
			t.Fatalf("ApplyDetail(appended %d) = %+v, want added event", index, result)
		}
	}

	budget := projection.Budget()
	budget.MaxScannedFactRows = 2
	projection.SetBudget(budget)

	secondPage, secondHasMore, _, secondMaxSequence, secondPostingID, err := projection.QueryEvents(
		from, to, 1, snapshotMaxSequence, nextPosition, filters,
	)
	if err != nil {
		t.Fatalf("QueryEvents(second page) error = %v, want frozen cursor order reuse", err)
	}
	if secondHasMore || secondMaxSequence != snapshotMaxSequence || secondPostingID != postingID {
		t.Fatalf("second page metadata = has_more:%t max:%d posting:%q", secondHasMore, secondMaxSequence, secondPostingID)
	}
	if len(secondPage.Events) != 1 || secondPage.Events[0].StableEventID == firstPage.Events[0].StableEventID {
		t.Fatalf("second page = %+v, want remaining pre-snapshot event", secondPage.Events)
	}
}

func TestUsageRequestStatisticsL05EventDetailLookupIsBounded(t *testing.T) {
	stats := NewRequestStatistics()
	base := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	first := projectionTestDetail("req-detail-first", base, "openai", "gpt-first", RequestTokenStats{InputTokens: 1, TotalTokens: 1})
	second := projectionTestDetail("req-detail-second", base.Add(time.Minute), "anthropic", "claude-second", RequestTokenStats{OutputTokens: 2, TotalTokens: 2})
	snapshot := StatisticsSnapshot{APIs: map[string]APISnapshot{
		"POST /v1/responses": {
			Models: map[string]ModelSnapshot{
				first.Model:  {Details: []RequestDetail{first}},
				second.Model: {Details: []RequestDetail{second}},
			},
		},
	}}
	if _, err := stats.MergeSnapshotWithError(snapshot); err != nil {
		t.Fatalf("MergeSnapshotWithError() error = %v", err)
	}
	events, _, _, _, _, err := stats.projection.QueryEvents(base.Add(-time.Hour), base.Add(time.Hour), 10, 0, 0, nil)
	if err != nil {
		t.Fatalf("QueryEvents() error = %v", err)
	}
	if len(events.Events) != 2 {
		t.Fatalf("event count = %d, want 2", len(events.Events))
	}
	ids := []string{events.Events[1].StableEventID, events.Events[0].StableEventID, events.Events[1].StableEventID, "missing"}
	details, err := stats.LookupEventDetails(ids)
	if err != nil {
		t.Fatalf("LookupEventDetails() error = %v", err)
	}
	if len(details) != 2 || details[events.Events[0].StableEventID].RequestID == "" || details[events.Events[1].StableEventID].RequestID == "" {
		t.Fatalf("detail lookup = %+v, want two selected details", details)
	}
	mutated := details[events.Events[0].StableEventID]
	mutated.RequestID = "mutated"
	details[events.Events[0].StableEventID] = mutated
	again, err := stats.LookupEventDetails([]string{events.Events[0].StableEventID})
	if err != nil {
		t.Fatalf("LookupEventDetails(second) error = %v", err)
	}
	if again[events.Events[0].StableEventID].RequestID == "mutated" {
		t.Fatal("detail lookup returned a mutable canonical alias")
	}

	tooMany := make([]string, 501)
	for index := range tooMany {
		tooMany[index] = events.Events[0].StableEventID
	}
	if _, err := stats.LookupEventDetails(tooMany); !errors.Is(err, ErrProjectionQueryInvalid) {
		t.Fatalf("LookupEventDetails(too many) error = %v, want %v", err, ErrProjectionQueryInvalid)
	}
}

func TestUsageProjectionL05AuthIndexPreservesPublicCoordinate(t *testing.T) {
	projection := NewUsageProjection()
	detail := projectionTestDetail("req-auth-coordinate", time.Date(2026, 8, 10, 13, 0, 0, 0, time.UTC), "openai", "gpt-5", RequestTokenStats{InputTokens: 1, TotalTokens: 1})
	detail.AuthIndex = "Auth-Mixed_123.~"
	detail.Source = "Auth-Mixed_123.~"
	detail.ExecutorType = "OpenAIExecutor"
	if result := projection.ApplyDetail(detail.Endpoint, detail); !result.Added {
		t.Fatalf("detail was not added: %+v", result)
	}

	summary, err := projection.QuerySummary(time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("QuerySummary() error = %v", err)
	}
	if summary.Auths[detail.AuthIndex].TotalRequests != 1 {
		t.Fatalf("summary auth coordinates = %+v, want exact %q", summary.Auths, detail.AuthIndex)
	}
	events, _, _, _, _, err := projection.QueryEvents(time.Time{}, time.Time{}, 10, 0, 0, map[string][]string{"auth_index": {detail.AuthIndex}})
	if err != nil {
		t.Fatalf("QueryEvents() error = %v", err)
	}
	if len(events.Events) != 1 || events.Events[0].AuthIndex != detail.AuthIndex {
		t.Fatalf("event auth coordinate = %+v, want exact %q", events.Events, detail.AuthIndex)
	}
}

func TestUsageRequestStatisticsL05AuthUsageUsesProjectionFacts(t *testing.T) {
	stats := NewRequestStatistics()
	base := time.Date(2026, 8, 10, 14, 0, 0, 0, time.UTC)
	first := projectionTestDetail("req-auth-usage-first", base, "openai", "gpt-5", RequestTokenStats{InputTokens: 3, TotalTokens: 3})
	first.AuthIndex = "Auth-Usage_123.~"
	first.Source = first.AuthIndex
	second := first
	second.RequestID = "req-auth-usage-second"
	second.Timestamp = base.Add(time.Minute)
	second.Failed = true
	second.Tokens = RequestTokenStats{OutputTokens: 7, TotalTokens: 7, TokenUsageSource: TokenUsageSourceProvider}
	if _, err := stats.MergeSnapshotWithError(StatisticsSnapshot{APIs: map[string]APISnapshot{
		"POST /v1/responses": {Models: map[string]ModelSnapshot{
			first.Model: {Details: []RequestDetail{first, second}},
		}},
	}}); err != nil {
		t.Fatalf("MergeSnapshotWithError() error = %v", err)
	}

	auths, err := stats.QueryProjectionAuthUsage()
	if err != nil {
		t.Fatalf("QueryProjectionAuthUsage() error = %v", err)
	}
	got, ok := auths[first.AuthIndex]
	if !ok || got.TotalRequests != 2 || got.SuccessCount != 1 || got.FailureCount != 1 ||
		got.Tokens.InputTokens != 3 || got.Tokens.OutputTokens != 7 || got.Tokens.TotalTokens != 10 {
		t.Fatalf("auth usage = %+v, want exact projection facts", got)
	}
	if got.LastRequestAt == nil || !got.LastRequestAt.Equal(second.Timestamp) || got.FirstRequestAt == nil || !got.FirstRequestAt.Equal(first.Timestamp) {
		t.Fatalf("auth time range = first:%v last:%v", got.FirstRequestAt, got.LastRequestAt)
	}
}

func TestUsageProjectionL05AuthUsageUsesYearRollupsAndAuthPostings(t *testing.T) {
	projection := NewUsageProjection()
	first := projectionTestDetail(
		"req-auth-rollup-first",
		time.Date(2025, 12, 31, 23, 0, 0, 0, time.UTC),
		"openai",
		"gpt-5",
		RequestTokenStats{InputTokens: 2, TotalTokens: 2},
	)
	first.AuthIndex = "auth-rollup"
	first.Source = first.AuthIndex
	second := first
	second.RequestID = "req-auth-rollup-second"
	second.Timestamp = time.Date(2026, 1, 1, 1, 0, 0, 0, time.UTC)
	second.Failed = true
	second.Tokens = RequestTokenStats{OutputTokens: 3, TotalTokens: 3, TokenUsageSource: TokenUsageSourceProvider}
	for _, detail := range []RequestDetail{first, second} {
		if result := projection.ApplyDetail(detail.Endpoint, detail); !result.Added {
			t.Fatalf("ApplyDetail(%q) = %+v, want added event", detail.RequestID, result)
		}
	}

	projection.mu.Lock()
	projection.postings.rows[projectionPostingKey{Dimension: postingDimensionAll}] = nil
	projection.mu.Unlock()

	auths, err := projection.QueryAuthUsage()
	if err != nil {
		t.Fatalf("QueryAuthUsage() error = %v", err)
	}
	got, ok := auths[first.AuthIndex]
	if !ok || got.TotalRequests != 2 || got.SuccessCount != 1 || got.FailureCount != 1 ||
		got.Tokens.InputTokens != 2 || got.Tokens.OutputTokens != 3 || got.Tokens.TotalTokens != 5 {
		t.Fatalf("auth usage = %+v, want year-rollup aggregate", got)
	}
	if got.FirstRequestAt == nil || !got.FirstRequestAt.Equal(first.Timestamp) ||
		got.LastRequestAt == nil || !got.LastRequestAt.Equal(second.Timestamp) {
		t.Fatalf("auth timestamp bounds = first:%v last:%v", got.FirstRequestAt, got.LastRequestAt)
	}
}

func TestUsageProjectionL05SummaryViewBuildsPartialSeriesAndHealthTail(t *testing.T) {
	projection := NewUsageProjection()
	base := time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)
	details := []RequestDetail{
		projectionTestDetail("req-series-first", base.Add(20*time.Minute), "openai", "gpt-first", RequestTokenStats{InputTokens: 3, TotalTokens: 3}),
		projectionTestDetail("req-series-second", base.Add(80*time.Minute), "anthropic", "claude-second", RequestTokenStats{OutputTokens: 5, TotalTokens: 5}),
		projectionTestDetail("req-series-tail", base.Add(106*time.Minute), "openai", "gpt-tail", RequestTokenStats{ReasoningTokens: 7, TotalTokens: 7}),
	}
	for _, detail := range details {
		if result := projection.ApplyDetail(detail.Endpoint, detail); !result.Added {
			t.Fatalf("detail %q was not added: %+v", detail.RequestID, result)
		}
	}
	from := base.Add(15 * time.Minute)
	to := base.Add(107 * time.Minute)
	view, err := projection.QuerySummaryView(from, to, to)
	if err != nil {
		t.Fatalf("QuerySummaryView() error = %v", err)
	}
	if view.Summary.Totals.TotalRequests != 3 || view.Summary.Totals.Tokens.TotalTokens != 15 {
		t.Fatalf("summary totals = %+v", view.Summary.Totals)
	}
	if view.SeriesGranularity != "hour" || len(view.Series) != 2 || view.SeriesAvailability != "available" {
		t.Fatalf("series metadata = granularity:%q count:%d availability:%q error:%q",
			view.SeriesGranularity, len(view.Series), view.SeriesAvailability, view.SeriesError)
	}
	if view.Series[0].Complete || view.Series[1].Complete || view.Series[0].Totals.TotalRequests != 1 || view.Series[1].Totals.TotalRequests != 2 {
		t.Fatalf("series points = %+v", view.Series)
	}
	if len(view.Health) != 672 || view.HealthAlignedTo != base.Add(105*time.Minute) ||
		view.HealthObservationTo != to || view.HealthFrom != view.HealthAlignedTo.Add(-7*24*time.Hour) {
		t.Fatalf("health range = count:%d from:%s aligned:%s observation:%s",
			len(view.Health), view.HealthFrom, view.HealthAlignedTo, view.HealthObservationTo)
	}
	if view.HealthPartialTail == nil || view.HealthPartialTail.Complete ||
		view.HealthPartialTail.TotalRequests != 1 || view.HealthPartialTail.Tokens.TotalTokens != 7 ||
		!view.HealthPartialTail.IntervalStart.Equal(view.HealthAlignedTo) || !view.HealthPartialTail.IntervalEnd.Equal(to) {
		t.Fatalf("health partial tail = %+v", view.HealthPartialTail)
	}
	var firstCompleteHealth, secondCompleteHealth *ProjectionHealthPoint
	for index := range view.Health {
		point := &view.Health[index]
		switch {
		case point.IntervalStart.Equal(base.Add(15 * time.Minute)):
			firstCompleteHealth = point
		case point.IntervalStart.Equal(base.Add(75 * time.Minute)):
			secondCompleteHealth = point
		}
	}
	if firstCompleteHealth == nil || firstCompleteHealth.Tokens.TotalTokens != 3 ||
		secondCompleteHealth == nil || secondCompleteHealth.Tokens.TotalTokens != 5 {
		t.Fatalf("complete health tokens = first:%+v second:%+v", firstCompleteHealth, secondCompleteHealth)
	}
}
