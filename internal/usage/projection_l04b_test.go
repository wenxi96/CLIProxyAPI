package usage

import (
	"context"
	"errors"
	"reflect"
	"runtime"
	"testing"
	"time"
)

func TestUsageProjectionL04BHighDimensionStorageShape(t *testing.T) {
	fixtures := projectionCapacityFixtures(projectionCapacitySmokeEvents)
	projection := NewUsageProjection()
	for _, fixture := range fixtures {
		result := projection.ApplyDetail(fixture.apiName, fixture.detail)
		if !result.Added {
			t.Fatalf("projection fixture was not added: %+v", result)
		}
	}

	metrics := projection.StorageMetrics()
	if metrics.FactRows != uint64(len(fixtures)) || metrics.ActiveFactRows != uint64(len(fixtures)) {
		t.Fatalf("fact rows = total:%d active:%d, want %d", metrics.FactRows, metrics.ActiveFactRows, len(fixtures))
	}
	if metrics.PostingRefs != uint64(len(fixtures)*6) {
		t.Fatalf("posting refs = %d, want %d", metrics.PostingRefs, len(fixtures)*6)
	}
	if metrics.RollupPricingGroups != 0 {
		t.Fatalf("rollup pricing groups = %d, want 0", metrics.RollupPricingGroups)
	}
	if metrics.EstimatedProjectionBytes > 64<<20 {
		t.Fatalf("estimated projection bytes = %d, want <= 64 MiB", metrics.EstimatedProjectionBytes)
	}

	snapshot := projection.Snapshot()
	if len(snapshot.Totals.PricingGroups) == 0 {
		t.Fatal("query-time pricing resolver returned no pricing groups")
	}
}

func TestProjectionBudgetV2EstimateCoversRetainedHeap(t *testing.T) {
	fixtures := projectionCapacityFixtures(projectionCapacitySmokeEvents)
	projectionCapacityProjectionSink = nil
	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)

	projection := NewUsageProjection()
	for _, fixture := range fixtures {
		result := projection.ApplyDetail(fixture.apiName, fixture.detail)
		if !result.Added {
			t.Fatalf("projection fixture was not added: %+v", result)
		}
	}
	metrics := projection.StorageMetrics()
	projectionCapacityProjectionSink = projection
	defer func() { projectionCapacityProjectionSink = nil }()
	runtime.GC()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	runtime.KeepAlive(projection)

	if after.HeapAlloc <= before.HeapAlloc {
		t.Fatalf("retained heap did not increase: before=%d after=%d", before.HeapAlloc, after.HeapAlloc)
	}
	retainedHeapBytes := after.HeapAlloc - before.HeapAlloc
	if metrics.EstimatedProjectionBytes < retainedHeapBytes {
		t.Fatalf("estimated projection bytes = %d, retained heap bytes = %d", metrics.EstimatedProjectionBytes, retainedHeapBytes)
	}
	budget := projection.Budget()
	budget.MaxProjectionBytes = retainedHeapBytes - 1
	projection.SetBudget(budget)
	var budgetErr *ProjectionBudgetError
	if err := projection.ValidateStorageBudget(); !errors.As(err, &budgetErr) || budgetErr.Dimension != projectionBudgetProjectionBytes {
		t.Fatalf("ValidateStorageBudget() error = %v, want projection byte budget failure", err)
	}
}

func TestUsageProjectionL04BPreservesZeroTimestamp(t *testing.T) {
	projection := NewUsageProjection()
	detail := RequestDetail{
		RequestID: "legacy-zero",
		Timestamp: time.Time{},
		Endpoint:  "POST /v1/responses",
		Provider:  "openai",
		Model:     "gpt-5",
		AuthType:  "api_key",
		AuthIndex: "auth-zero",
		Source:    "auth-zero",
		Tokens:    RequestTokenStats{InputTokens: 1, TotalTokens: 1},
	}
	result := projection.ApplyDetail(detail.Endpoint, detail)
	if !result.Added {
		t.Fatalf("result = %+v", result)
	}
	snapshot := projection.Snapshot()
	if len(snapshot.Events) != 1 || !snapshot.Events[0].Timestamp.IsZero() {
		t.Fatalf("zero timestamp changed during fact storage: %+v", snapshot.Events)
	}
}

func TestUsageProjectionL04BPricingResolverIgnoresTombstonedFacts(t *testing.T) {
	projection := NewUsageProjection()
	detail := projectionCapacityFixtures(1)[0]
	first := projection.ApplyDetail(detail.apiName, detail.detail)
	if !first.Added {
		t.Fatalf("first result = %+v", first)
	}

	enriched := detail.detail
	enriched.Tokens.InputTokens += 100
	enriched.Tokens.TotalTokens += 100
	second := projection.ApplyDetail(detail.apiName, enriched)
	if !second.Enriched {
		t.Fatalf("second result = %+v", second)
	}

	metrics := projection.StorageMetrics()
	if metrics.FactRows != 2 || metrics.ActiveFactRows != 1 || metrics.TombstonedFactRows != 1 {
		t.Fatalf("fact lifecycle metrics = %+v", metrics)
	}
	resolved, err := projection.ResolvePricing(ProjectionPricingQuery{DimensionType: "global"})
	if err != nil {
		t.Fatalf("ResolvePricing() error = %v", err)
	}
	want := l04bPricingOracle([]projectionCapacityFixture{{apiName: detail.apiName, detail: enriched}}, "global", "", nil)
	if !reflect.DeepEqual(resolved.PricingGroups, want) {
		t.Fatalf("pricing resolver included tombstoned fact: got=%#v want=%#v", resolved.PricingGroups, want)
	}
}

func TestUsageProjectionL04BPricingResolverMatchesDetailOracle(t *testing.T) {
	fixtures := projectionCapacityFixtures(projectionCapacitySmokeEvents)
	projection := NewUsageProjection()
	for _, fixture := range fixtures {
		if result := projection.ApplyDetail(fixture.apiName, fixture.detail); !result.Added {
			t.Fatalf("projection fixture was not added: %+v", result)
		}
	}
	snapshot := projection.Snapshot()

	globalExpected := l04bPricingOracle(fixtures, "global", "", func(projectionCapacityFixture) bool { return true })
	if !reflect.DeepEqual(snapshot.Totals.PricingGroups, globalExpected) {
		t.Fatalf("global pricing resolver mismatch: got=%#v want=%#v", snapshot.Totals.PricingGroups, globalExpected)
	}

	first := fixtures[0]
	hourKey := bucketKey(hourStart(first.detail.Timestamp))
	apiExpected := l04bPricingOracle(fixtures, "api", first.apiName, func(fixture projectionCapacityFixture) bool {
		return fixture.apiName == first.apiName && bucketKey(hourStart(fixture.detail.Timestamp)) == hourKey
	})
	gotAPI := snapshot.Hours[hourKey].APIs[first.apiName].PricingGroups
	if !reflect.DeepEqual(gotAPI, apiExpected) {
		t.Fatalf("API pricing resolver mismatch: got=%#v want=%#v", gotAPI, apiExpected)
	}
}

func TestUsageProjectionL04BPricingResolverExactRange(t *testing.T) {
	fixtures := projectionCapacityFixtures(32)
	projection := newL04BTestProjection(t, fixtures)
	from := fixtures[5].detail.Timestamp
	to := fixtures[20].detail.Timestamp.Add(time.Nanosecond)

	result, err := projection.ResolvePricing(ProjectionPricingQuery{
		From:          from,
		To:            to,
		DimensionType: "global",
	})
	if err != nil {
		t.Fatalf("ResolvePricing() error = %v", err)
	}
	want := l04bPricingOracle(fixtures, "global", "", func(fixture projectionCapacityFixture) bool {
		return !fixture.detail.Timestamp.Before(from) && fixture.detail.Timestamp.Before(to)
	})
	if !reflect.DeepEqual(result.PricingGroups, want) {
		t.Fatalf("exact range pricing mismatch: got=%#v want=%#v", result.PricingGroups, want)
	}
	if result.ScannedFactRows != uint64(len(fixtures)) {
		t.Fatalf("global scan count = %d, want %d", result.ScannedFactRows, len(fixtures))
	}
	if result.ScratchBytes == 0 {
		t.Fatal("resolver did not report query scratch bytes")
	}
}

func TestUsageProjectionL04BPricingSnapshotIsImmutableDuringWrites(t *testing.T) {
	fixtures := projectionCapacityFixtures(2)
	projection := newL04BTestProjection(t, fixtures[:1])
	snapshot, err := projection.capturePricingSnapshot(ProjectionPricingQuery{DimensionType: "global"})
	if err != nil {
		t.Fatalf("capturePricingSnapshot() error = %v", err)
	}
	if result := projection.ApplyDetail(fixtures[1].apiName, fixtures[1].detail); !result.Added {
		t.Fatalf("concurrent fixture was not added: %+v", result)
	}

	captured, err := snapshot.resolve()
	if err != nil {
		t.Fatalf("captured pricing resolve error = %v", err)
	}
	wantCaptured := l04bPricingOracle(fixtures[:1], "global", "", nil)
	if !reflect.DeepEqual(captured.PricingGroups, wantCaptured) {
		t.Fatalf("captured pricing changed after write: got=%#v want=%#v", captured.PricingGroups, wantCaptured)
	}

	current, err := projection.ResolvePricing(ProjectionPricingQuery{DimensionType: "global"})
	if err != nil {
		t.Fatalf("current pricing resolve error = %v", err)
	}
	wantCurrent := l04bPricingOracle(fixtures, "global", "", nil)
	if !reflect.DeepEqual(current.PricingGroups, wantCurrent) {
		t.Fatalf("current pricing omitted appended fact: got=%#v want=%#v", current.PricingGroups, wantCurrent)
	}
}

func TestUsageProjectionL04BPricingSnapshotAllowsConcurrentEnrichment(t *testing.T) {
	fixtures := projectionCapacityFixtures(projectionCapacitySmokeEvents)
	projection := newL04BTestProjection(t, fixtures)
	snapshot, err := projection.capturePricingSnapshot(ProjectionPricingQuery{DimensionType: "global"})
	if err != nil {
		t.Fatalf("capturePricingSnapshot() error = %v", err)
	}
	enriched := fixtures[0].detail
	enriched.Tokens.InputTokens += 100
	enriched.Tokens.TotalTokens += 100

	type resolveOutcome struct {
		result ProjectionPricingResult
		err    error
	}
	start := make(chan struct{})
	resolved := make(chan resolveOutcome, 1)
	applied := make(chan ProjectionMutationResult, 1)
	go func() {
		<-start
		result, resolveErr := snapshot.resolve()
		resolved <- resolveOutcome{result: result, err: resolveErr}
	}()
	go func() {
		<-start
		applied <- projection.ApplyDetail(fixtures[0].apiName, enriched)
	}()
	close(start)

	resolveResult := <-resolved
	if resolveResult.err != nil {
		t.Fatalf("captured pricing resolve error = %v", resolveResult.err)
	}
	if result := <-applied; !result.Enriched {
		t.Fatalf("concurrent enrichment result = %+v", result)
	}
	wantCaptured := l04bPricingOracle(fixtures, "global", "", nil)
	if !reflect.DeepEqual(resolveResult.result.PricingGroups, wantCaptured) {
		t.Fatalf("captured pricing changed during enrichment: got=%#v want=%#v", resolveResult.result.PricingGroups, wantCaptured)
	}
}

func TestUsageProjectionL04BPricingResolverDimensionRange(t *testing.T) {
	fixtures := projectionCapacityFixtures(32)
	projection := newL04BTestProjection(t, fixtures)
	first := fixtures[0]
	from := first.detail.Timestamp
	to := from.Add(time.Hour)

	result, err := projection.ResolvePricing(ProjectionPricingQuery{
		From:          from,
		To:            to,
		DimensionType: "api",
		DimensionIDs:  []string{first.apiName},
	})
	if err != nil {
		t.Fatalf("ResolvePricing() error = %v", err)
	}
	want := l04bPricingOracle(fixtures, "api", first.apiName, func(fixture projectionCapacityFixture) bool {
		return fixture.apiName == first.apiName &&
			!fixture.detail.Timestamp.Before(from) && fixture.detail.Timestamp.Before(to)
	})
	if !reflect.DeepEqual(result.PricingGroups, want) {
		t.Fatalf("dimension range pricing mismatch: got=%#v want=%#v", result.PricingGroups, want)
	}
	if result.ScannedFactRows == 0 || result.ScannedFactRows >= uint64(len(fixtures)) {
		t.Fatalf("dimension scan count = %d, want a strict subset of %d", result.ScannedFactRows, len(fixtures))
	}
}

func TestUsageProjectionL04BPricingResolverMultiSourceOR(t *testing.T) {
	fixtures := projectionCapacityFixtures(32)
	projection := newL04BTestProjection(t, fixtures)
	firstSource := UsageSourceIDV1(fixtures[0].detail)
	secondSource := UsageSourceIDV1(fixtures[1].detail)
	ids := []string{firstSource, secondSource, firstSource}

	result, err := projection.ResolvePricing(ProjectionPricingQuery{
		DimensionType: "source",
		DimensionIDs:  ids,
	})
	if err != nil {
		t.Fatalf("ResolvePricing() error = %v", err)
	}
	want := make(map[string]PricingAggregate)
	for _, sourceID := range []string{firstSource, secondSource} {
		for key, pricing := range l04bPricingOracle(fixtures, "source", sourceID, func(fixture projectionCapacityFixture) bool {
			return UsageSourceIDV1(fixture.detail) == sourceID
		}) {
			want[key] = pricing
		}
	}
	if !reflect.DeepEqual(result.PricingGroups, want) {
		t.Fatalf("multi-source pricing mismatch: got=%#v want=%#v", result.PricingGroups, want)
	}
	if result.ScannedFactRows != 2 {
		t.Fatalf("multi-source scan count = %d, want 2 without duplicate-id rescans", result.ScannedFactRows)
	}
}

func TestUsageProjectionL04BPricingResolverBudgetFailClosed(t *testing.T) {
	fixtures := projectionCapacityFixtures(4)
	projection := newL04BTestProjection(t, fixtures)

	tests := []struct {
		name      string
		configure func(*ProjectionBudgetV2)
		dimension string
	}{
		{
			name: "scanned rows",
			configure: func(budget *ProjectionBudgetV2) {
				budget.MaxScannedFactRows = 1
			},
			dimension: projectionBudgetScannedFactRows,
		},
		{
			name: "scratch bytes",
			configure: func(budget *ProjectionBudgetV2) {
				budget.MaxQueryScratchBytes = 1
			},
			dimension: projectionBudgetQueryScratchBytes,
		},
		{
			name: "pricing groups",
			configure: func(budget *ProjectionBudgetV2) {
				budget.MaxPricingGroups = 1
			},
			dimension: projectionBudgetPricingGroups,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			budget := DefaultProjectionBudgetV2()
			testCase.configure(&budget)
			projection.SetBudget(budget)
			result, err := projection.ResolvePricing(ProjectionPricingQuery{DimensionType: "global"})
			if !errors.Is(err, ErrProjectionBudgetExceeded) {
				t.Fatalf("ResolvePricing() error = %v, want budget exceeded", err)
			}
			var budgetErr *ProjectionBudgetError
			if !errors.As(err, &budgetErr) || budgetErr.Dimension != testCase.dimension {
				t.Fatalf("budget error = %T/%v, want dimension %q", err, err, testCase.dimension)
			}
			if len(result.PricingGroups) != 0 {
				t.Fatalf("budget failure returned partial pricing groups: %#v", result.PricingGroups)
			}
		})
	}
}

func TestUsageProjectionL04BPricingResolverRejectsInvalidQuery(t *testing.T) {
	projection := newL04BTestProjection(t, projectionCapacityFixtures(2))
	tests := []ProjectionPricingQuery{
		{DimensionType: "unsupported"},
		{DimensionType: "global", DimensionIDs: []string{"unexpected"}},
		{DimensionType: "api", From: time.Unix(20, 0), To: time.Unix(10, 0)},
	}
	for _, query := range tests {
		if _, err := projection.ResolvePricing(query); !errors.Is(err, ErrProjectionQueryInvalid) {
			t.Fatalf("query=%+v error=%v, want ErrProjectionQueryInvalid", query, err)
		}
	}
}

func newL04BTestProjection(t *testing.T, fixtures []projectionCapacityFixture) *UsageProjection {
	t.Helper()
	projection := NewUsageProjection()
	for _, fixture := range fixtures {
		if result := projection.ApplyDetail(fixture.apiName, fixture.detail); !result.Added {
			t.Fatalf("projection fixture was not added: %+v", result)
		}
	}
	return projection
}

func TestProjectionBudgetV2RecordFailClosedRetainsCanonicalDetail(t *testing.T) {
	stats := NewRequestStatistics()
	config := DefaultMutationCoordinatorConfig()
	config.ProjectionBudget.MaxFactRows = 1
	stats.coordinator = NewMutationCoordinatorWithConfig(stats, config)
	fixtures := projectionCapacityFixtures(2)

	if err := stats.RecordWithError(fixtures[0].ctx, fixtures[0].record); err != nil {
		t.Fatalf("first record error = %v", err)
	}
	if err := stats.RecordWithError(fixtures[1].ctx, fixtures[1].record); !errors.Is(err, ErrProjectionBudgetExceeded) {
		t.Fatalf("second record error = %v, want ErrProjectionBudgetExceeded", err)
	}
	if got := stats.Snapshot().TotalRequests; got != 2 {
		t.Fatalf("canonical total requests = %d, want 2", got)
	}
	if got := len(stats.ProjectionSnapshot().Events); got != 1 {
		t.Fatalf("projection events = %d, want last complete generation with 1 event", got)
	}
	if available, code := stats.ProjectionAvailability(); available || code != "projection_budget_exceeded" {
		t.Fatalf("projection availability = %t/%q, want false/projection_budget_exceeded", available, code)
	}
}

func TestProjectionBudgetV2BulkCandidateIsNotPublished(t *testing.T) {
	stats := NewRequestStatistics()
	config := DefaultMutationCoordinatorConfig()
	config.ProjectionBudget.MaxFactRows = 2
	config.MaxBulkChunkIntents = 1
	stats.coordinator = NewMutationCoordinatorWithConfig(stats, config)

	result, err := stats.MergeSnapshotWithError(projectionCapacitySnapshot(projectionCapacityFixtures(3)))
	if !errors.Is(err, ErrProjectionBudgetExceeded) {
		t.Fatalf("bulk merge result=%+v error=%v, want ErrProjectionBudgetExceeded", result, err)
	}
	if got := stats.Snapshot().TotalRequests; got != 0 {
		t.Fatalf("live canonical total requests = %d, want unpublished candidate", got)
	}
	if got := len(stats.ProjectionSnapshot().Events); got != 0 {
		t.Fatalf("live projection events = %d, want unpublished candidate", got)
	}
	marker, ok := stats.coordinator.Journal().TerminalBulkTransactionSnapshot()
	if !ok || marker.TerminalState != MutationTerminalTombstone || marker.FailureCode != "projection_budget" {
		t.Fatalf("bulk terminal marker = %+v/%t", marker, ok)
	}
}

func TestProjectionBudgetV2QueryFailsBeforePricingMaterialization(t *testing.T) {
	projection := NewUsageProjection()
	fixtures := projectionCapacityFixtures(2)
	for _, fixture := range fixtures {
		if result := projection.ApplyDetail(fixture.apiName, fixture.detail); !result.Added {
			t.Fatalf("projection fixture was not added: %+v", result)
		}
	}
	budget := DefaultProjectionBudgetV2()
	budget.MaxScannedFactRows = 1
	projection.SetBudget(budget)

	if _, err := projection.SnapshotWithBudget(); !errors.Is(err, ErrProjectionBudgetExceeded) {
		t.Fatalf("SnapshotWithBudget() error = %v, want ErrProjectionBudgetExceeded", err)
	}
	if got := len(projection.Snapshot().Events); got != 2 {
		t.Fatalf("compatibility Snapshot() events = %d, want 2", got)
	}
}

func TestProjectionBudgetV2FinalizeRejectsOversizedCandidate(t *testing.T) {
	stats := NewRequestStatistics()
	fixtures := projectionCapacityFixtures(2)
	if err := stats.RecordWithError(fixtures[0].ctx, fixtures[0].record); err != nil {
		t.Fatalf("record base fixture: %v", err)
	}
	candidate, err := stats.coordinator.BeginRebuild(context.Background())
	if err != nil {
		t.Fatalf("BeginRebuild() error = %v", err)
	}
	if result := candidate.Projection.ApplyDetail(fixtures[1].apiName, fixtures[1].detail); !result.Added {
		t.Fatalf("candidate fixture was not added: %+v", result)
	}
	budget := DefaultProjectionBudgetV2()
	budget.MaxFactRows = 1
	candidate.Projection.SetBudget(budget)

	if err := stats.coordinator.FinalizeRebuild(context.Background(), candidate); !errors.Is(err, ErrProjectionBudgetExceeded) {
		t.Fatalf("FinalizeRebuild() error = %v, want ErrProjectionBudgetExceeded", err)
	}
	if stats.coordinator.gate.activeState() {
		t.Fatal("rebuild gate remained active after budget rejection")
	}
	if got := stats.Snapshot().TotalRequests; got != 1 {
		t.Fatalf("live total requests = %d, want 1", got)
	}
}

func l04bPricingOracle(fixtures []projectionCapacityFixture, dimensionType, dimensionID string, include func(projectionCapacityFixture) bool) map[string]PricingAggregate {
	result := make(map[string]PricingAggregate)
	for _, fixture := range fixtures {
		if include != nil && !include(fixture) {
			continue
		}
		detail := fixture.detail
		billable := GetBillableTokenComponents(detail)
		key := pricingGroupKeyV1(dimensionType, dimensionID, BuildPriceKey(detail.Provider, detail.Model), ProviderRawPresenceV1(detail.Provider))
		pricing := result[key]
		if pricing.BillablePolicyVersion == "" {
			pricing.BillablePolicyVersion = BillablePolicyVersionV1
			pricing.Provider = detail.Provider
			pricing.Model = detail.Model
			pricing.PriceKey = BuildPriceKey(detail.Provider, detail.Model)
			pricing.ProviderRawPresence = ProviderRawPresenceV1(detail.Provider)
			pricing.ComponentMaskCounts = make(map[ComponentMask]int64)
		}
		pricing.BillableTokens.InputTokens += billable.InputTokens
		pricing.BillableTokens.OutputTokens += billable.OutputTokens
		pricing.BillableTokens.ReasoningTokens += billable.ReasoningTokens
		pricing.BillableTokens.CacheReadTokens += billable.CacheReadTokens
		pricing.BillableTokens.CacheCreationTokens += billable.CacheCreationTokens
		pricing.BillableTokens.UnclassifiedCacheTokens += billable.UnclassifiedCacheTokens
		pricing.ComponentMaskCounts[billable.Mask()]++
		switch ClassifyDetailV1(detail) {
		case DetailClassPriceable:
			pricing.PriceableDetailCount++
		case DetailClassUnknownUsage:
			pricing.UnknownUsageDetailCount++
		case DetailClassKnownTotalOnly:
			pricing.KnownTotalOnlyDetailCount++
		case DetailClassZeroBillableComplete:
			pricing.ZeroBillableCompleteDetailCount++
		}
		if billable.UnclassifiedCacheTokens > 0 {
			pricing.UnclassifiedCacheDetailCount++
		}
		result[key] = pricing
	}
	return result
}
