package usage

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/logging"
	coreusage "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
)

type projectionHydrateFixture struct {
	state              projectionGenerationState
	sections           projectionV2Sections
	sidecar            IdentitySidecar
	canonicalSnapshot  StatisticsSnapshot
	projectionSnapshot ProjectionSnapshot
}

type factRowTestLayout struct {
	timestampPresentOffset  int
	timestampNanosOffset    int
	stringIDOffset          int
	failedOffset            int
	usageSourceOffset       int
	classificationOffset    int
	componentMaskOffset     int
	activeOffset            int
	stableEventIDStart      int
	stableEventIDLength     int
	canonicalIdentityStart  int
	canonicalIdentityLength int
}

func TestProjectionHydrateV2RejectsDuplicateRegistryValue(t *testing.T) {
	var facts bytes.Buffer
	writeProjectionUint64(&facts, 2)
	writeProjectionString(&facts, "duplicate")
	writeProjectionString(&facts, "duplicate")
	writeProjectionUint64(&facts, 0)

	projection := NewUsageProjection()
	if err := projection.unmarshalFactsV2Locked(bytes.NewReader(facts.Bytes())); !errors.Is(err, ErrProjectionRestoreUnavailable) {
		t.Fatalf("duplicate registry hydrate error = %v, want ErrProjectionRestoreUnavailable", err)
	}
}

func TestProjectionHydrateV2RejectsTrailingSectionBytes(t *testing.T) {
	fixture := newProjectionHydrateFixture(t, 2)
	sections := cloneProjectionV2Sections(fixture.sections)
	sections.Facts = append(sections.Facts, 0xff)

	projection := NewUsageProjection()
	if err := projection.hydrateFromGenerationV2(fixture.state, sections, fixture.sidecar, fixture.canonicalSnapshot); !errors.Is(err, ErrProjectionRestoreUnavailable) {
		t.Fatalf("trailing facts hydrate error = %v, want ErrProjectionRestoreUnavailable", err)
	}
}

func TestProjectionGenerationDirectHydrateAllowsEmptyAuthIndex(t *testing.T) {
	stats := NewRequestStatistics()
	record := coreusage.Record{
		APIKey:      "empty-auth-index",
		Model:       "gpt-5",
		Provider:    "openai",
		RequestedAt: time.Date(2026, 8, 11, 10, 0, 0, 0, time.UTC),
		Detail:      coreusage.Detail{InputTokens: 2, OutputTokens: 1, TotalTokens: 3},
	}
	if err := stats.RecordWithError(context.Background(), record); err != nil {
		t.Fatalf("record empty auth fixture: %v", err)
	}
	withAuth := record
	withAuth.APIKey = "present-auth-index"
	withAuth.AuthIndex = "hydrated-auth"
	withAuth.RequestedAt = withAuth.RequestedAt.Add(time.Second)
	if err := stats.RecordWithError(context.Background(), withAuth); err != nil {
		t.Fatalf("record present auth fixture: %v", err)
	}

	path := filepath.Join(t.TempDir(), StatisticsFileName)
	if err := SaveProjectionGeneration(path, stats); err != nil {
		t.Fatalf("save empty auth fixture: %v", err)
	}

	restored := NewRequestStatistics()
	loaded, result, err := RestoreRequestStatistics(path, restored)
	if err != nil || !loaded || result.Added != 2 {
		t.Fatalf("restore empty auth fixture loaded=%t result=%+v err=%v", loaded, result, err)
	}
	projection := restored.ProjectionSnapshot()
	if len(projection.Events) != 2 {
		t.Fatalf("restored events = %#v, want two events", projection.Events)
	}
	seenEmptyAuth := false
	for _, event := range projection.Events {
		seenEmptyAuth = seenEmptyAuth || event.AuthIndex == ""
	}
	if !seenEmptyAuth {
		t.Fatalf("restored events = %#v, want empty auth index", projection.Events)
	}
	auths, err := restored.QueryProjectionAuthUsage()
	if err != nil || len(auths) != 1 || auths["hydrated-auth"].TotalRequests != 1 {
		t.Fatalf("restored auth usage = %#v err=%v, want only hydrated-auth", auths, err)
	}
}

func TestProjectionHydrateV2RejectsInvalidFactEncodings(t *testing.T) {
	fixture := newProjectionHydrateFixture(t, 1)
	layout, registryCount := parseFactSectionLayout(t, fixture.sections.Facts)
	row := layout[0]

	tests := []struct {
		name   string
		mutate func([]byte)
	}{
		{name: "timestamp-present", mutate: func(data []byte) { data[row.timestampPresentOffset] = 2 }},
		{name: "timestamp-nanos", mutate: func(data []byte) { binary.BigEndian.PutUint32(data[row.timestampNanosOffset:], 1_000_000_000) }},
		{name: "string-id", mutate: func(data []byte) { binary.BigEndian.PutUint32(data[row.stringIDOffset:], uint32(registryCount+1)) }},
		{name: "required-string-id-zero", mutate: func(data []byte) { binary.BigEndian.PutUint32(data[row.stringIDOffset:], 0) }},
		{name: "failed", mutate: func(data []byte) { data[row.failedOffset] = 2 }},
		{name: "usage-source", mutate: func(data []byte) { data[row.usageSourceOffset] = factUsageSourceComputed + 1 }},
		{name: "classification", mutate: func(data []byte) { data[row.classificationOffset] = factClassificationZeroBillableComplete + 1 }},
		{name: "component-mask", mutate: func(data []byte) { data[row.componentMaskOffset] = 0x80 }},
		{name: "active", mutate: func(data []byte) { data[row.activeOffset] = 2 }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			sections := cloneProjectionV2Sections(fixture.sections)
			test.mutate(sections.Facts)
			projection := NewUsageProjection()
			if err := projection.hydrateFromGenerationV2(fixture.state, sections, fixture.sidecar, fixture.canonicalSnapshot); !errors.Is(err, ErrProjectionRestoreUnavailable) {
				t.Fatalf("invalid %s hydrate error = %v, want ErrProjectionRestoreUnavailable", test.name, err)
			}
		})
	}
}

func TestProjectionHydrateV2RejectsInvalidPostingEncodings(t *testing.T) {
	fixture := newProjectionHydrateFixture(t, 1)
	_, registryCount := parseFactSectionLayout(t, fixture.sections.Facts)

	tests := []struct {
		name   string
		mutate func([]byte)
	}{
		{name: "key-count", mutate: func(data []byte) { binary.BigEndian.PutUint64(data, DefaultProjectionBudgetV2().MaxPostingRefs+1) }},
		{name: "dimension", mutate: func(data []byte) { data[8] = byte(postingDimensionSource + 1) }},
		{name: "value-id", mutate: func(data []byte) {
			data[8] = byte(postingDimensionAPI)
			binary.BigEndian.PutUint32(data[9:], uint32(registryCount+1))
		}},
		{name: "required-value-id-zero", mutate: func(data []byte) {
			data[8] = byte(postingDimensionAPI)
			binary.BigEndian.PutUint32(data[9:], 0)
		}},
		{name: "row-id-zero", mutate: func(data []byte) { binary.BigEndian.PutUint32(data[21:], 0) }},
		{name: "row-id-out-of-range", mutate: func(data []byte) { binary.BigEndian.PutUint32(data[21:], 2) }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			sections := cloneProjectionV2Sections(fixture.sections)
			test.mutate(sections.Postings)
			projection := NewUsageProjection()
			if err := projection.hydrateFromGenerationV2(fixture.state, sections, fixture.sidecar, fixture.canonicalSnapshot); !errors.Is(err, ErrProjectionRestoreUnavailable) {
				t.Fatalf("invalid posting %s hydrate error = %v, want ErrProjectionRestoreUnavailable", test.name, err)
			}
		})
	}
}

func TestProjectionHydrateV2RejectsIdentityAndCoordinateDrift(t *testing.T) {
	fixture := newProjectionHydrateFixture(t, 2)
	layout, _ := parseFactSectionLayout(t, fixture.sections.Facts)

	t.Run("duplicate-stable-event-id", func(t *testing.T) {
		sections := cloneProjectionV2Sections(fixture.sections)
		copy(layoutBytesFor(sections.Facts, layout[1].stableEventIDStart, layout[1].stableEventIDLength), layoutBytesFor(fixture.sections.Facts, layout[0].stableEventIDStart, layout[0].stableEventIDLength))
		projection := NewUsageProjection()
		if err := projection.hydrateFromGenerationV2(fixture.state, sections, fixture.sidecar, fixture.canonicalSnapshot); !errors.Is(err, ErrProjectionRestoreUnavailable) {
			t.Fatalf("duplicate stable id hydrate error = %v, want ErrProjectionRestoreUnavailable", err)
		}
	})

	t.Run("duplicate-canonical-identity", func(t *testing.T) {
		sections := cloneProjectionV2Sections(fixture.sections)
		copy(layoutBytesFor(sections.Facts, layout[1].canonicalIdentityStart, layout[1].canonicalIdentityLength), layoutBytesFor(fixture.sections.Facts, layout[0].canonicalIdentityStart, layout[0].canonicalIdentityLength))
		projection := NewUsageProjection()
		if err := projection.hydrateFromGenerationV2(fixture.state, sections, fixture.sidecar, fixture.canonicalSnapshot); !errors.Is(err, ErrProjectionRestoreUnavailable) {
			t.Fatalf("duplicate canonical identity hydrate error = %v, want ErrProjectionRestoreUnavailable", err)
		}
	})

	t.Run("sidecar-canonical-mismatch", func(t *testing.T) {
		sidecar := cloneIdentitySidecarForHydrateTest(fixture.sidecar)
		sidecar.Entries[1].CanonicalEventIdentity = sidecar.Entries[0].CanonicalEventIdentity
		projection := NewUsageProjection()
		if err := projection.hydrateFromGenerationV2(fixture.state, fixture.sections, sidecar, fixture.canonicalSnapshot); !errors.Is(err, ErrProjectionRestoreUnavailable) {
			t.Fatalf("sidecar canonical mismatch error = %v, want ErrProjectionRestoreUnavailable", err)
		}
	})

	t.Run("coordinate-mismatch", func(t *testing.T) {
		sidecar := cloneIdentitySidecarForHydrateTest(fixture.sidecar)
		sidecar.Entries[0].APIName = "mismatched-api"
		projection := NewUsageProjection()
		if err := projection.hydrateFromGenerationV2(fixture.state, fixture.sections, sidecar, fixture.canonicalSnapshot); !errors.Is(err, ErrProjectionRestoreUnavailable) {
			t.Fatalf("coordinate mismatch error = %v, want ErrProjectionRestoreUnavailable", err)
		}
	})
}

func TestProjectionHydrateV2RequiresExactMetricsAndCanonicalTotals(t *testing.T) {
	fixture := newProjectionHydrateFixture(t, 2)

	tests := []struct {
		name  string
		state projectionGenerationState
		snap  StatisticsSnapshot
	}{
		{name: "zero-fact-metric", state: func() projectionGenerationState {
			state := fixture.state
			state.StorageMetrics.FactRows = 0
			return state
		}(), snap: fixture.canonicalSnapshot},
		{name: "rollup-cell-metric", state: func() projectionGenerationState {
			state := fixture.state
			state.StorageMetrics.RollupCells++
			return state
		}(), snap: fixture.canonicalSnapshot},
		{name: "canonical-total", state: fixture.state, snap: func() StatisticsSnapshot {
			snapshot := fixture.canonicalSnapshot
			snapshot.TotalRequests++
			return snapshot
		}()},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			projection := NewUsageProjection()
			if err := projection.hydrateFromGenerationV2(test.state, fixture.sections, fixture.sidecar, test.snap); !errors.Is(err, ErrProjectionRestoreUnavailable) {
				t.Fatalf("%s hydrate error = %v, want ErrProjectionRestoreUnavailable", test.name, err)
			}
		})
	}
}

func TestProjectionGenerationDirectHydrateMatchesSnapshotsAndKeepsBudget(t *testing.T) {
	stats := NewRequestStatistics()
	result, err := stats.MergeSnapshotWithError(projectionCapacitySnapshot(projectionCapacityFixtures(20)))
	if err != nil || result.Added != 20 {
		t.Fatalf("build hydrate fixture result=%+v err=%v", result, err)
	}
	wantCanonical := stats.Snapshot()
	wantProjection := stats.ProjectionSnapshot()
	wantMetrics := stats.projection.StorageMetrics()

	path := filepath.Join(t.TempDir(), StatisticsFileName)
	if err := SaveProjectionGeneration(path, stats); err != nil {
		t.Fatalf("save projection generation: %v", err)
	}

	budget := DefaultProjectionBudgetV2()
	budget.MaxFactRows = 100
	restored := NewRequestStatistics()
	restored.coordinator = NewMutationCoordinatorWithConfig(restored, MutationCoordinatorConfig{ProjectionBudget: budget})
	loaded, restoredResult, err := RestoreRequestStatistics(path, restored)
	if err != nil || !loaded || restoredResult.Added != 20 {
		t.Fatalf("direct hydrate loaded=%t result=%+v err=%v", loaded, restoredResult, err)
	}
	if got := restored.ReplayFallbackCount(); got != 0 {
		t.Fatalf("direct hydrate replay fallback count = %d, want 0", got)
	}
	if got := restored.projection.Budget(); got != budget.normalized() {
		t.Fatalf("hydrated budget = %+v, want %+v", got, budget.normalized())
	}
	if got := restored.Snapshot(); !reflect.DeepEqual(got, wantCanonical) {
		t.Fatalf("restored canonical snapshot drifted\n got: %#v\nwant: %#v", got, wantCanonical)
	}
	if got := restored.ProjectionSnapshot(); !reflect.DeepEqual(got, wantProjection) {
		t.Fatalf("restored projection snapshot drifted\n got: %#v\nwant: %#v", got, wantProjection)
	}
	if got := restored.projection.StorageMetrics(); got != wantMetrics {
		t.Fatalf("restored metrics = %+v, want %+v", got, wantMetrics)
	}
	from := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(time.Hour)
	wantRange, err := stats.projection.QuerySummary(from, to)
	if err != nil {
		t.Fatalf("source range summary: %v", err)
	}
	gotRange, err := restored.projection.QuerySummary(from, to)
	if err != nil {
		t.Fatalf("hydrated range summary: %v", err)
	}
	if !reflect.DeepEqual(gotRange, wantRange) {
		t.Fatalf("hydrated range summary drifted\n got: %#v\nwant: %#v", gotRange, wantRange)
	}
}

func TestProjectionGenerationDirectHydrateHonorsConfiguredBudget(t *testing.T) {
	stats := NewRequestStatistics()
	result, err := stats.MergeSnapshotWithError(projectionCapacitySnapshot(projectionCapacityFixtures(2)))
	if err != nil || result.Added != 2 {
		t.Fatalf("build budget fixture result=%+v err=%v", result, err)
	}
	path := filepath.Join(t.TempDir(), StatisticsFileName)
	if err := SaveProjectionGeneration(path, stats); err != nil {
		t.Fatalf("save projection generation: %v", err)
	}

	budget := DefaultProjectionBudgetV2()
	budget.MaxFactRows = 1
	restored := NewRequestStatistics()
	restored.coordinator = NewMutationCoordinatorWithConfig(restored, MutationCoordinatorConfig{ProjectionBudget: budget})
	loaded, _, err := RestoreRequestStatistics(path, restored)
	if loaded || !errors.Is(err, ErrProjectionRestoreUnavailable) {
		t.Fatalf("over-budget hydrate loaded=%t err=%v, want fail-closed", loaded, err)
	}
	if got := restored.Snapshot().TotalRequests; got != 0 {
		t.Fatalf("over-budget hydrate published %d requests, want 0", got)
	}
}

func TestProjectionGenerationDirectHydratePreservesEnrichmentIdentity(t *testing.T) {
	requestedAt := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	ctx := logging.WithRequestID(context.Background(), "req-direct-hydrate-enrich")
	base := coreusage.Record{
		APIKey:      "hydrate-enrich-key",
		Model:       "gpt-5",
		Provider:    "openai",
		AuthIndex:   "hydrate-enrich-auth",
		RequestedAt: requestedAt,
		Detail:      coreusage.Detail{InputTokens: 2, OutputTokens: 1, TotalTokens: 3},
	}
	stats := NewRequestStatistics()
	if err := stats.RecordWithError(ctx, base); err != nil {
		t.Fatalf("record base hydrate enrichment fixture: %v", err)
	}
	originalEvents := stats.ProjectionSnapshot().Events
	if len(originalEvents) != 1 {
		t.Fatalf("base event count = %d, want 1", len(originalEvents))
	}
	path := filepath.Join(t.TempDir(), StatisticsFileName)
	if err := SaveProjectionGeneration(path, stats); err != nil {
		t.Fatalf("save hydrate enrichment fixture: %v", err)
	}

	restored := NewRequestStatistics()
	if loaded, _, err := RestoreRequestStatistics(path, restored); err != nil || !loaded {
		t.Fatalf("restore hydrate enrichment fixture loaded=%t err=%v", loaded, err)
	}
	enriched := base
	enriched.Detail = coreusage.Detail{InputTokens: 4, OutputTokens: 2, TotalTokens: 6}
	if err := restored.RecordWithError(ctx, enriched); err != nil {
		t.Fatalf("record post-hydrate enrichment: %v", err)
	}
	if err := restored.RecordWithError(ctx, enriched); err != nil {
		t.Fatalf("repeat post-hydrate enrichment: %v", err)
	}

	projection := restored.ProjectionSnapshot()
	if len(projection.Events) != 1 || projection.Events[0].StableEventID != originalEvents[0].StableEventID || projection.Events[0].Version != 2 {
		t.Fatalf("post-hydrate enrichment events = %#v, want one version-2 event %q", projection.Events, originalEvents[0].StableEventID)
	}
	snapshot := restored.Snapshot()
	if snapshot.TotalRequests != 1 || snapshot.TotalTokens != 6 {
		t.Fatalf("post-hydrate enrichment snapshot = requests=%d tokens=%d, want 1/6", snapshot.TotalRequests, snapshot.TotalTokens)
	}
}

func TestProjectionGenerationSemanticCorruptionFallsBackToPrevious(t *testing.T) {
	stats, path, previous, current := persistTwoHydrateGenerations(t)
	_ = stats
	corruptProjectionFactsWithTrailingByte(t, path, current)

	restored := NewRequestStatistics()
	loaded, result, err := RestoreRequestStatistics(path, restored)
	if err != nil || !loaded || result.Added != 1 {
		t.Fatalf("semantic fallback loaded=%t result=%+v err=%v", loaded, result, err)
	}
	if got := restored.Snapshot().TotalRequests; got != 1 {
		t.Fatalf("semantic fallback restored %d requests, want previous generation 1", got)
	}
	if got := restored.ReplayFallbackCount(); got != 0 {
		t.Fatalf("semantic v2 fallback replay count = %d, want 0", got)
	}
	if previous.Generation == current.Generation {
		t.Fatal("semantic fallback fixture did not create two generations")
	}
}

func TestProjectionGenerationSemanticCorruptionInAllGenerationsFailsClosed(t *testing.T) {
	_, path, previous, current := persistTwoHydrateGenerations(t)
	corruptProjectionFactsWithTrailingByte(t, path, current)
	corruptProjectionFactsWithTrailingByte(t, path, previous)

	restored := NewRequestStatistics()
	loaded, _, err := RestoreRequestStatistics(path, restored)
	if loaded || !errors.Is(err, ErrProjectionRestoreUnavailable) {
		t.Fatalf("all-generation semantic corruption loaded=%t err=%v, want fail-closed", loaded, err)
	}
	if got := restored.Snapshot().TotalRequests; got != 0 {
		t.Fatalf("failed semantic restore published %d requests, want 0", got)
	}
}

func TestProjectionGenerationRestoreMarkerFailureDoesNotPublishHydratedState(t *testing.T) {
	stats := NewRequestStatistics()
	result, err := stats.MergeSnapshotWithError(projectionCapacitySnapshot(projectionCapacityFixtures(1)))
	if err != nil || result.Added != 1 {
		t.Fatalf("build marker failure fixture result=%+v err=%v", result, err)
	}
	path := filepath.Join(t.TempDir(), StatisticsFileName)
	if err := SaveProjectionGeneration(path, stats); err != nil {
		t.Fatalf("save projection generation: %v", err)
	}

	restored := NewRequestStatistics()
	installCompletedAndActiveBulkMarkers(t, restored.coordinator.Journal())
	loaded, _, err := RestoreRequestStatistics(path, restored)
	if loaded || err == nil {
		t.Fatalf("marker-conflict restore loaded=%t err=%v, want failure", loaded, err)
	}
	if got := restored.Snapshot().TotalRequests; got != 0 {
		t.Fatalf("marker-conflict restore published %d requests before commit", got)
	}
}

func newProjectionHydrateFixture(t *testing.T, count int) projectionHydrateFixture {
	t.Helper()
	stats := NewRequestStatistics()
	result, err := stats.MergeSnapshotWithError(projectionCapacitySnapshot(projectionCapacityFixtures(count)))
	if err != nil || result.Added != int64(count) {
		t.Fatalf("build projection hydrate fixture result=%+v err=%v", result, err)
	}
	state, payload, err := stats.projection.marshalGenerationStateV2()
	if err != nil {
		t.Fatalf("marshal projection hydrate fixture: %v", err)
	}
	decodedState, sections, err := decodeProjectionGenerationStateV2(payload)
	if err != nil {
		t.Fatalf("decode projection hydrate fixture: %v", err)
	}
	if decodedState != state {
		t.Fatalf("decoded projection state = %+v, want %+v", decodedState, state)
	}
	return projectionHydrateFixture{
		state:              decodedState,
		sections:           sections,
		sidecar:            stats.IdentitySidecarSnapshot(),
		canonicalSnapshot:  stats.Snapshot(),
		projectionSnapshot: stats.ProjectionSnapshot(),
	}
}

func cloneProjectionV2Sections(sections projectionV2Sections) projectionV2Sections {
	return projectionV2Sections{
		Facts:         append([]byte(nil), sections.Facts...),
		Postings:      append([]byte(nil), sections.Postings...),
		ScalarRollups: append([]byte(nil), sections.ScalarRollups...),
	}
}

func cloneIdentitySidecarForHydrateTest(sidecar IdentitySidecar) IdentitySidecar {
	result := sidecar
	result.TerminalIntents = append([]IdentitySidecarTerminalIntent(nil), sidecar.TerminalIntents...)
	result.Entries = append([]IdentitySidecarEntry(nil), sidecar.Entries...)
	result.SourceOrdinals = make(map[string]uint64, len(sidecar.SourceOrdinals))
	for key, value := range sidecar.SourceOrdinals {
		result.SourceOrdinals[key] = value
	}
	if sidecar.BulkTransaction != nil {
		marker := *sidecar.BulkTransaction
		result.BulkTransaction = &marker
	}
	return result
}

func parseFactSectionLayout(t *testing.T, facts []byte) ([]factRowTestLayout, uint64) {
	t.Helper()
	offset := 0
	registryCount := readTestUint64(t, facts, &offset)
	for range registryCount {
		length := readTestUint64(t, facts, &offset)
		offset += int(length)
		if offset > len(facts) {
			t.Fatal("fact registry layout exceeds section")
		}
	}
	rowCount := readTestUint64(t, facts, &offset)
	rows := make([]factRowTestLayout, 0, rowCount)
	for range rowCount {
		start := offset
		layout := factRowTestLayout{timestampPresentOffset: offset}
		offset++
		offset += 8
		layout.timestampNanosOffset = offset
		offset += 4
		offset += 4 * 8
		layout.stringIDOffset = offset
		offset += 7 * 4
		offset += len((usageFactStoreV1{}).facetIDs) * 4
		offset += factTokenColumnCount * 8
		offset += factBillableColumnCount * 8
		layout.failedOffset = offset
		offset++
		layout.usageSourceOffset = offset
		offset++
		layout.classificationOffset = offset
		offset++
		layout.componentMaskOffset = offset
		offset++
		layout.activeOffset = offset
		offset++
		stableLength := readTestUint64(t, facts, &offset)
		stableStart := offset
		offset += int(stableLength)
		canonicalLength := readTestUint64(t, facts, &offset)
		canonicalStart := offset
		offset += int(canonicalLength)
		if offset > len(facts) {
			t.Fatal("fact row layout exceeds section")
		}
		layout.stableEventIDStart = stableStart
		layout.stableEventIDLength = int(stableLength)
		layout.canonicalIdentityStart = canonicalStart
		layout.canonicalIdentityLength = int(canonicalLength)
		if layout.timestampPresentOffset != start {
			t.Fatal("fact row layout start drifted")
		}
		rows = append(rows, layout)
	}
	if offset != len(facts) {
		t.Fatalf("fact section layout consumed %d bytes, want %d", offset, len(facts))
	}
	return rows, registryCount
}

func readTestUint64(t *testing.T, data []byte, offset *int) uint64 {
	t.Helper()
	if *offset < 0 || *offset+8 > len(data) {
		t.Fatal("test uint64 read exceeds section")
	}
	value := binary.BigEndian.Uint64(data[*offset : *offset+8])
	*offset += 8
	return value
}

func layoutBytesFor(data []byte, start, length int) []byte {
	if length == 0 {
		return nil
	}
	return data[start : start+length]
}

func persistTwoHydrateGenerations(t *testing.T) (*RequestStatistics, string, ProjectionGenerationManifest, ProjectionGenerationManifest) {
	t.Helper()
	stats := NewRequestStatistics()
	first := projectionCapacitySnapshot(projectionCapacityFixtures(1))
	if result, err := stats.MergeSnapshotWithError(first); err != nil || result.Added != 1 {
		t.Fatalf("build first generation result=%+v err=%v", result, err)
	}
	path := filepath.Join(t.TempDir(), StatisticsFileName)
	if err := SaveProjectionGeneration(path, stats); err != nil {
		t.Fatalf("save first generation: %v", err)
	}
	previous := readProjectionManifest(t, path)
	second := projectionCapacityFixtures(2)[1]
	if err := stats.RecordWithError(second.ctx, second.record); err != nil {
		t.Fatalf("build second generation: %v", err)
	}
	if err := SaveProjectionGeneration(path, stats); err != nil {
		t.Fatalf("save second generation: %v", err)
	}
	current := readProjectionManifest(t, path)
	return stats, path, previous, current
}

func corruptProjectionFactsWithTrailingByte(t *testing.T, path string, manifest ProjectionGenerationManifest) {
	t.Helper()
	projectionPath := filepath.Join(filepath.Dir(path), manifest.ProjectionFile)
	payload, err := os.ReadFile(projectionPath)
	if err != nil {
		t.Fatalf("read projection payload: %v", err)
	}
	state, sections, err := decodeProjectionGenerationStateV2(payload)
	if err != nil {
		t.Fatalf("decode projection payload before semantic corruption: %v", err)
	}
	sections.Facts = append(sections.Facts, 0xff)
	state.FactsSHA256 = projectionSectionSHA256(sections.Facts)
	payload = encodeProjectionGenerationStateV2ForTest(state, sections)
	if err := os.WriteFile(projectionPath, payload, 0o600); err != nil {
		t.Fatalf("write semantically corrupt projection payload: %v", err)
	}
	manifest.ProjectionSHA256 = sha256Hex(payload)
	manifest.FactsSHA256 = state.FactsSHA256
	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatalf("marshal semantically corrupt manifest: %v", err)
	}
	manifestData = append(manifestData, '\n')
	if err := os.WriteFile(projectionGenerationManifestFilePath(path, manifest.Generation), manifestData, 0o600); err != nil {
		t.Fatalf("write immutable semantically corrupt manifest: %v", err)
	}
	current := readProjectionManifest(t, path)
	if current.Generation == manifest.Generation {
		if err := os.WriteFile(projectionGenerationManifestPath(path), manifestData, 0o600); err != nil {
			t.Fatalf("write pointer semantically corrupt manifest: %v", err)
		}
	}
}

func encodeProjectionGenerationStateV2ForTest(state projectionGenerationState, sections projectionV2Sections) []byte {
	var payload bytes.Buffer
	payload.Write(projectionStorageMagicV2)
	writeProjectionString(&payload, state.SchemaVersion)
	writeProjectionUint64(&payload, state.DatasetEpoch)
	writeProjectionUint64(&payload, state.Revision)
	writeProjectionUint64(&payload, state.RewriteRevision)
	writeProjectionStorageMetrics(&payload, state.StorageMetrics)
	for _, section := range []struct {
		name string
		hash string
		data []byte
	}{
		{name: "facts", hash: projectionSectionSHA256(sections.Facts), data: sections.Facts},
		{name: "postings", hash: projectionSectionSHA256(sections.Postings), data: sections.Postings},
		{name: "scalar_rollups", hash: projectionSectionSHA256(sections.ScalarRollups), data: sections.ScalarRollups},
	} {
		writeProjectionString(&payload, section.name)
		writeProjectionString(&payload, section.hash)
		writeProjectionBytes(&payload, section.data)
	}
	return payload.Bytes()
}

func installCompletedAndActiveBulkMarkers(t *testing.T, journal *MutationJournal) {
	t.Helper()
	completedPayload := mutationSHA256Parts("existing-completed-payload")
	if err := journal.BeginBulkTransaction(MutationBatchMarker{
		BatchID:       "existing:1",
		BatchSequence: 1,
		Operation:     MutationOperationImportMerge,
		DatasetEpoch:  1,
		TotalIntents:  1,
		PayloadSHA256: completedPayload,
	}); err != nil {
		t.Fatalf("begin completed marker: %v", err)
	}
	if _, err := journal.AdvanceBulkChunk("existing:1", 0, 1, mutationSHA256Parts("existing-completed-chunk")); err != nil {
		t.Fatalf("advance completed marker: %v", err)
	}
	if _, err := journal.CompleteBulkTransaction("existing:1", MutationTerminalCommitted, "", mutationSHA256Parts("existing-completed-manifest")); err != nil {
		t.Fatalf("complete existing marker: %v", err)
	}
	if err := journal.BeginBulkTransaction(MutationBatchMarker{
		BatchID:       "existing:2",
		BatchSequence: 2,
		Operation:     MutationOperationImportMerge,
		DatasetEpoch:  1,
		TotalIntents:  1,
		PayloadSHA256: mutationSHA256Parts("existing-active-payload"),
	}); err != nil {
		t.Fatalf("begin active marker: %v", err)
	}
}
