package usage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	coreusage "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
)

const projectionCapacitySmokeEvents = 4_000
const projectionCaptureAdmissionWaitThreshold = 100 * time.Millisecond

const (
	projectionCapacitySpanDense24h   = "24h-dense"
	projectionCapacitySpanSparse365d = "365d-sparse"
)

type projectionCapacityFixture struct {
	apiName string
	detail  RequestDetail
	ctx     context.Context
	record  coreusage.Record
}

var (
	projectionCapacityProjectionSink *UsageProjection
	projectionCapacityStatisticsSink *RequestStatistics
	projectionCapacityPricingSink    ProjectionPricingResult
)

func BenchmarkUsageProjectionHighDimension4000(b *testing.B) {
	benchmarkUsageProjectionHighDimension(b, projectionCapacitySmokeEvents, projectionCapacitySpanDense24h)
}

func BenchmarkUsageProjectionHighDimension60000(b *testing.B) {
	benchmarkUsageProjectionHighDimension(b, 60_000, projectionCapacitySpanDense24h)
}

func BenchmarkUsageProjectionHighDimension60000Sparse365d(b *testing.B) {
	benchmarkUsageProjectionHighDimension(b, 60_000, projectionCapacitySpanSparse365d)
}

func BenchmarkUsageProjectionHighDimension250000(b *testing.B) {
	benchmarkUsageProjectionHighDimension(b, 250_000, projectionCapacitySpanDense24h)
}

func BenchmarkUsageProjectionHighDimension250000Sparse365d(b *testing.B) {
	benchmarkUsageProjectionHighDimension(b, 250_000, projectionCapacitySpanSparse365d)
}

func benchmarkUsageProjectionHighDimension(b *testing.B, eventCount int, span string) {
	fixtures := projectionCapacityFixturesForSpan(eventCount, span)
	projectionCapacityProjectionSink = nil
	projectionCapacityStatisticsSink = nil
	baseline := projectionCapacityHeapSnapshot()
	var measured *UsageProjection
	b.ReportAllocs()
	b.SetBytes(int64(len(fixtures)))
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		projection := NewUsageProjection()
		for _, fixture := range fixtures {
			result := projection.ApplyDetail(fixture.apiName, fixture.detail)
			if !result.Added {
				b.Fatalf("projection fixture was not added: %+v", result)
			}
		}
		if got := len(projection.events); got != len(fixtures) {
			b.Fatalf("projection events = %d, want %d", got, len(fixtures))
		}
		projectionCapacityProjectionSink = projection
		measured = projection
	}
	b.StopTimer()
	retained := projectionCapacityHeapSnapshot()
	buckets, aggregates, pricingGroups, facets, postingRefs := projectionCapacityMetrics(measured)
	singlePricingGroupAggregates, inlinePricingGroupAggregates, promotedPricingGroupAggregates, maxPricingGroups := projectionCapacityPricingGroupMetrics(measured)
	pricingGroupsGT8, pricingGroupsGT16, pricingGroupsGT32, pricingGroupsGT64 := projectionCapacityPricingGroupThresholdMetrics(measured)
	b.ReportMetric(float64(len(fixtures)), "events/op")
	b.ReportMetric(float64(buckets), "buckets/op")
	b.ReportMetric(float64(aggregates), "aggregates/op")
	b.ReportMetric(float64(pricingGroups), "pricing-groups/op")
	b.ReportMetric(float64(singlePricingGroupAggregates), "pricing-single-aggregates/op")
	b.ReportMetric(float64(inlinePricingGroupAggregates), "pricing-inline-aggregates/op")
	b.ReportMetric(float64(promotedPricingGroupAggregates), "pricing-promoted-aggregates/op")
	b.ReportMetric(float64(maxPricingGroups), "pricing-groups-max/op")
	b.ReportMetric(float64(unsafe.Sizeof(compactAggregate{})), "compact-aggregate-bytes")
	b.ReportMetric(float64(unsafe.Sizeof(compactPricingAggregate{})), "compact-pricing-bytes")
	b.ReportMetric(float64(unsafe.Sizeof(compactPricingGroupEntry{})), "compact-pricing-entry-bytes")
	b.ReportMetric(float64(pricingGroupsGT8), "pricing-groups-gt8-aggregates/op")
	b.ReportMetric(float64(pricingGroupsGT16), "pricing-groups-gt16-aggregates/op")
	b.ReportMetric(float64(pricingGroupsGT32), "pricing-groups-gt32-aggregates/op")
	b.ReportMetric(float64(pricingGroupsGT64), "pricing-groups-gt64-aggregates/op")
	b.ReportMetric(float64(facets), "facet-entries/op")
	b.ReportMetric(float64(postingRefs), "posting-refs/op")
	storage := measured.StorageMetrics()
	b.ReportMetric(float64(storage.FactRows), "fact-rows/op")
	b.ReportMetric(float64(storage.ActiveFactRows), "active-fact-rows/op")
	b.ReportMetric(float64(storage.RollupCells), "rollup-cells/op")
	b.ReportMetric(float64(storage.RegistryEntries), "registry-entries/op")
	b.ReportMetric(float64(storage.EstimatedProjectionBytes), "estimated-projection-bytes/op")
	reportProjectionCapacityHeap(b, baseline, retained)
}

func BenchmarkUsageProjectionHighDimensionSnapshot4000(b *testing.B) {
	fixtures := projectionCapacityFixtures(projectionCapacitySmokeEvents)
	projection := NewUsageProjection()
	for _, fixture := range fixtures {
		result := projection.ApplyDetail(fixture.apiName, fixture.detail)
		if !result.Added {
			b.Fatalf("projection fixture was not added: %+v", result)
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		snapshot := projection.Snapshot()
		if snapshot.Totals.TotalRequests != int64(len(fixtures)) || len(snapshot.Totals.PricingGroups) == 0 {
			b.Fatalf("snapshot totals = %+v", snapshot.Totals)
		}
	}
}

func BenchmarkUsageProjectionPricing4000(b *testing.B) {
	benchmarkUsageProjectionPricing(b, projectionCapacitySmokeEvents, projectionCapacitySpanDense24h)
}

func BenchmarkUsageProjectionPricing60000(b *testing.B) {
	benchmarkUsageProjectionPricing(b, 60_000, projectionCapacitySpanDense24h)
}

func BenchmarkUsageProjectionPricing60000Sparse365d(b *testing.B) {
	benchmarkUsageProjectionPricing(b, 60_000, projectionCapacitySpanSparse365d)
}

func BenchmarkUsageProjectionPricing250000(b *testing.B) {
	benchmarkUsageProjectionPricing(b, 250_000, projectionCapacitySpanDense24h)
}

func BenchmarkUsageProjectionPricing250000Sparse365d(b *testing.B) {
	benchmarkUsageProjectionPricing(b, 250_000, projectionCapacitySpanSparse365d)
}

func BenchmarkUsageProjectionConcurrentPricing60000(b *testing.B) {
	const appendedEvents = 1_000
	projection := NewUsageProjection()
	for _, fixture := range projectionCapacityFixturesForSpan(60_000, projectionCapacitySpanSparse365d) {
		if result := projection.ApplyDetail(fixture.apiName, fixture.detail); !result.Added {
			b.Fatalf("projection fixture was not added: %+v", result)
		}
	}
	appends := projectionCapacityFixturesForSpan(appendedEvents, projectionCapacitySpanDense24h)
	for index := range appends {
		appends[index].detail.RequestID = fmt.Sprintf("concurrent-pricing-%06d", index)
		appends[index].detail.Timestamp = appends[index].detail.Timestamp.AddDate(0, 0, 366)
	}

	var queryCount atomic.Uint64
	queryErrors := make(chan error, 1)
	stopQueries := make(chan struct{})
	queriesDone := make(chan struct{})
	go func() {
		defer close(queriesDone)
		for {
			select {
			case <-stopQueries:
				return
			default:
			}
			if _, err := projection.ResolvePricing(ProjectionPricingQuery{DimensionType: "global"}); err != nil {
				select {
				case queryErrors <- err:
				default:
				}
				return
			}
			queryCount.Add(1)
		}
	}()

	durations := make([]time.Duration, 0, b.N*len(appends))
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		for _, fixture := range appends {
			startedAt := time.Now()
			if result := projection.ApplyDetail(fixture.apiName, fixture.detail); !result.Added && !result.Skipped {
				b.Fatalf("concurrent projection fixture was not applied: %+v", result)
			}
			durations = append(durations, time.Since(startedAt))
		}
	}
	b.StopTimer()
	close(stopQueries)
	<-queriesDone
	select {
	case err := <-queryErrors:
		b.Fatalf("concurrent pricing query: %v", err)
	default:
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	b.ReportMetric(float64(queryCount.Load()), "concurrent-pricing-queries/op")
	if len(durations) > 0 {
		b.ReportMetric(float64(durations[percentileIndex(len(durations), 0.95)])/float64(time.Millisecond), "concurrent-apply-p95-ms")
		b.ReportMetric(float64(durations[percentileIndex(len(durations), 0.99)])/float64(time.Millisecond), "concurrent-apply-p99-ms")
	}
}

func benchmarkUsageProjectionPricing(b *testing.B, eventCount int, span string) {
	const samplesPerIteration = 10
	fixtures := projectionCapacityFixturesForSpan(eventCount, span)
	projection := NewUsageProjection()
	for _, fixture := range fixtures {
		if result := projection.ApplyDetail(fixture.apiName, fixture.detail); !result.Added {
			b.Fatalf("projection fixture was not added: %+v", result)
		}
	}
	query := ProjectionPricingQuery{DimensionType: "global"}
	durations := make([]time.Duration, 0, b.N*samplesPerIteration)
	var measured ProjectionPricingResult

	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		for sample := 0; sample < samplesPerIteration; sample++ {
			startedAt := time.Now()
			result, err := projection.ResolvePricing(query)
			if err != nil {
				b.Fatalf("resolve pricing: %v", err)
			}
			durations = append(durations, time.Since(startedAt))
			measured = result
			projectionCapacityPricingSink = result
		}
	}
	b.StopTimer()
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	b.ReportMetric(float64(eventCount), "events/op")
	b.ReportMetric(float64(samplesPerIteration), "pricing-query-samples/op")
	b.ReportMetric(float64(measured.ScannedFactRows), "pricing-scanned-rows/op")
	b.ReportMetric(float64(measured.ScratchBytes), "pricing-scratch-bytes/op")
	b.ReportMetric(float64(len(measured.PricingGroups)), "pricing-groups/op")
	if len(durations) > 0 {
		b.ReportMetric(float64(durations[percentileIndex(len(durations), 0.95)])/float64(time.Millisecond), "pricing-query-p95-ms")
		b.ReportMetric(float64(durations[percentileIndex(len(durations), 0.99)])/float64(time.Millisecond), "pricing-query-p99-ms")
	}
}

func BenchmarkRequestStatisticsHighDimension4000(b *testing.B) {
	benchmarkRequestStatisticsHighDimension(b, projectionCapacitySmokeEvents, projectionCapacitySpanDense24h)
}

func BenchmarkRequestStatisticsHighDimension60000(b *testing.B) {
	benchmarkRequestStatisticsHighDimension(b, 60_000, projectionCapacitySpanDense24h)
}

func BenchmarkRequestStatisticsHighDimension60000Sparse365d(b *testing.B) {
	benchmarkRequestStatisticsHighDimension(b, 60_000, projectionCapacitySpanSparse365d)
}

func BenchmarkRequestStatisticsHighDimension250000(b *testing.B) {
	benchmarkRequestStatisticsHighDimension(b, 250_000, projectionCapacitySpanDense24h)
}

func BenchmarkRequestStatisticsHighDimension250000Sparse365d(b *testing.B) {
	benchmarkRequestStatisticsHighDimension(b, 250_000, projectionCapacitySpanSparse365d)
}

func benchmarkRequestStatisticsHighDimension(b *testing.B, eventCount int, span string) {
	fixtures := projectionCapacityFixturesForSpan(eventCount, span)
	projectionCapacityProjectionSink = nil
	projectionCapacityStatisticsSink = nil
	baseline := projectionCapacityHeapSnapshot()
	var recordDurations []time.Duration
	b.ReportAllocs()
	b.SetBytes(int64(len(fixtures)))
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		stats := NewRequestStatistics()
		recordDurations = recordDurations[:0]
		for _, fixture := range fixtures {
			startedAt := time.Now()
			if err := stats.RecordWithError(fixture.ctx, fixture.record); err != nil {
				b.Fatalf("record fixture: %v", err)
			}
			recordDurations = append(recordDurations, time.Since(startedAt))
		}
		if got := stats.totalRequests; got != int64(len(fixtures)) {
			b.Fatalf("total requests = %d, want %d", got, len(fixtures))
		}
		projectionCapacityStatisticsSink = stats
	}
	b.StopTimer()
	retained := projectionCapacityHeapSnapshot()
	sort.Slice(recordDurations, func(i, j int) bool { return recordDurations[i] < recordDurations[j] })
	b.ReportMetric(float64(len(fixtures)), "events/op")
	if len(recordDurations) > 0 {
		b.ReportMetric(float64(recordDurations[percentileIndex(len(recordDurations), 0.95)])/float64(time.Millisecond), "record-p95-ms")
		b.ReportMetric(float64(recordDurations[percentileIndex(len(recordDurations), 0.99)])/float64(time.Millisecond), "record-p99-ms")
	}
	reportProjectionCapacityHeap(b, baseline, retained)
}

func BenchmarkRequestStatisticsHighDimensionRestore4000(b *testing.B) {
	benchmarkRequestStatisticsHighDimensionRestore(b, projectionCapacitySmokeEvents, projectionCapacitySpanDense24h)
}

func BenchmarkRequestStatisticsHighDimensionRestore10000(b *testing.B) {
	benchmarkRequestStatisticsHighDimensionRestore(b, 10_000, projectionCapacitySpanDense24h)
}

func BenchmarkRequestStatisticsHighDimensionRestore60000(b *testing.B) {
	benchmarkRequestStatisticsHighDimensionRestore(b, 60_000, projectionCapacitySpanDense24h)
}

func BenchmarkRequestStatisticsHighDimensionRestore60000Sparse365d(b *testing.B) {
	benchmarkRequestStatisticsHighDimensionRestore(b, 60_000, projectionCapacitySpanSparse365d)
}

func BenchmarkRequestStatisticsHighDimensionRestore250000(b *testing.B) {
	benchmarkRequestStatisticsHighDimensionRestore(b, 250_000, projectionCapacitySpanDense24h)
}

func BenchmarkRequestStatisticsHighDimensionRestore250000Sparse365d(b *testing.B) {
	benchmarkRequestStatisticsHighDimensionRestore(b, 250_000, projectionCapacitySpanSparse365d)
}

func BenchmarkProjectionGenerationPersist60000Sparse365d(b *testing.B) {
	fixtures := projectionCapacityFixturesForSpan(60_000, projectionCapacitySpanSparse365d)
	stats := NewRequestStatistics()
	result, err := stats.MergeSnapshotWithError(projectionCapacitySnapshot(fixtures))
	if err != nil || result.Added != int64(len(fixtures)) {
		b.Fatalf("build persistence fixture result=%+v err=%v", result, err)
	}
	root := b.TempDir()
	lastPath := ""
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		lastPath = filepath.Join(root, fmt.Sprintf("usage-%d.json", iteration))
		if err := SaveProjectionGeneration(lastPath, stats); err != nil {
			b.Fatalf("persist projection generation: %v", err)
		}
	}
	b.StopTimer()
	reportProjectionGenerationPayloadSize(b, lastPath)
}

func BenchmarkProjectionGenerationCaptureAdmissionWait60000Sparse365d(b *testing.B) {
	benchmarkProjectionGenerationCaptureAdmissionWait(b, 60_000, projectionCapacitySpanSparse365d)
}

func BenchmarkProjectionGenerationCaptureAdmissionWait250000Sparse365d(b *testing.B) {
	benchmarkProjectionGenerationCaptureAdmissionWait(b, 250_000, projectionCapacitySpanSparse365d)
}

func benchmarkProjectionGenerationCaptureAdmissionWait(b *testing.B, eventCount int, span string) {
	fixtures := projectionCapacityFixturesForSpan(eventCount, span)
	stats := NewRequestStatistics()
	result, err := stats.MergeSnapshotWithError(projectionCapacitySnapshot(fixtures))
	if err != nil || result.Added != int64(len(fixtures)) {
		b.Fatalf("build persistence fixture result=%+v err=%v", result, err)
	}
	recordFixture := projectionCapacityFixturesForSpan(1, projectionCapacitySpanDense24h)[0]
	root := b.TempDir()
	waits := make([]time.Duration, 0, b.N)
	initialFenceHolds := make([]time.Duration, 0, b.N)
	finalFenceHolds := make([]time.Duration, 0, b.N)
	persistDurations := make([]time.Duration, 0, b.N)
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		path := filepath.Join(root, fmt.Sprintf("usage-%d.json", iteration))
		type capturePersistResult struct {
			capture capturedProjectionGeneration
			err     error
		}
		persistDone := make(chan capturePersistResult, 1)
		persistStartedAt := time.Now()
		go func() {
			captured := capturedProjectionGeneration{}
			errPersist := saveProjectionGenerationWithCaptureResult(path, stats, nil, &captured)
			persistDone <- capturePersistResult{capture: captured, err: errPersist}
		}()
		deadline := time.Now().Add(10 * time.Second)
		for {
			stats.coordinator.mu.Lock()
			active := stats.coordinator.activeCapture != nil
			stats.coordinator.mu.Unlock()
			if active {
				break
			}
			if time.Now().After(deadline) {
				b.Fatal("projection generation capture did not enter the exclusive batch")
			}
			time.Sleep(100 * time.Microsecond)
		}
		initialRecord := recordFixture.record
		initialRecord.AdmissionDiscriminator = fmt.Sprintf("capture-initial-%d", iteration)
		initialRecord.RequestedAt = initialRecord.RequestedAt.Add(time.Duration(iteration*2) * time.Nanosecond)
		admissionStartedAt := time.Now()
		if err := stats.RecordWithError(recordFixture.ctx, initialRecord); err != nil {
			b.Fatalf("record during projection capture: %v", err)
		}
		waits = append(waits, time.Since(admissionStartedAt))
		persisted := <-persistDone
		if persisted.err != nil {
			b.Fatalf("persist projection generation: %v", persisted.err)
		}
		initialFenceHolds = append(initialFenceHolds, persisted.capture.initialFenceHold)
		finalFenceHolds = append(finalFenceHolds, persisted.capture.finalFenceHold)
		persistDurations = append(persistDurations, time.Since(persistStartedAt))
	}
	b.StopTimer()
	sort.Slice(waits, func(i, j int) bool { return waits[i] < waits[j] })
	sort.Slice(initialFenceHolds, func(i, j int) bool { return initialFenceHolds[i] < initialFenceHolds[j] })
	sort.Slice(finalFenceHolds, func(i, j int) bool { return finalFenceHolds[i] < finalFenceHolds[j] })
	sort.Slice(persistDurations, func(i, j int) bool { return persistDurations[i] < persistDurations[j] })
	if len(waits) > 0 {
		p95 := waits[percentileIndex(len(waits), 0.95)]
		if p95 >= projectionCaptureAdmissionWaitThreshold {
			b.Fatalf("capture admission wait p95 = %s, want <%s", p95, projectionCaptureAdmissionWaitThreshold)
		}
		b.ReportMetric(float64(p95)/float64(time.Millisecond), "capture-admission-wait-p95-ms")
		b.ReportMetric(float64(waits[len(waits)-1])/float64(time.Millisecond), "capture-admission-wait-max-ms")
		b.ReportMetric(float64(persistDurations[percentileIndex(len(persistDurations), 0.95)])/float64(time.Millisecond), "persist-p95-ms")
	}
	if len(initialFenceHolds) > 0 {
		initialFenceP95 := initialFenceHolds[percentileIndex(len(initialFenceHolds), 0.95)]
		if initialFenceP95 >= projectionCaptureAdmissionWaitThreshold {
			b.Fatalf("capture initial fence hold p95 = %s, want <%s", initialFenceP95, projectionCaptureAdmissionWaitThreshold)
		}
		b.ReportMetric(float64(initialFenceP95)/float64(time.Millisecond), "capture-initial-fence-hold-p95-ms")
		b.ReportMetric(float64(initialFenceHolds[len(initialFenceHolds)-1])/float64(time.Millisecond), "capture-initial-fence-hold-max-ms")
	}
	if len(finalFenceHolds) > 0 {
		finalFenceP95 := finalFenceHolds[percentileIndex(len(finalFenceHolds), 0.95)]
		if finalFenceP95 >= projectionCaptureAdmissionWaitThreshold {
			b.Fatalf("capture final fence hold p95 = %s, want <%s", finalFenceP95, projectionCaptureAdmissionWaitThreshold)
		}
		b.ReportMetric(float64(finalFenceP95)/float64(time.Millisecond), "capture-final-fence-hold-p95-ms")
		b.ReportMetric(float64(finalFenceHolds[len(finalFenceHolds)-1])/float64(time.Millisecond), "capture-final-fence-hold-max-ms")
	}
}

func BenchmarkUsageProjectionClone60000Sparse365d(b *testing.B) {
	projection := NewUsageProjection()
	for _, fixture := range projectionCapacityFixturesForSpan(60_000, projectionCapacitySpanSparse365d) {
		if result := projection.ApplyDetail(fixture.apiName, fixture.detail); !result.Added {
			b.Fatalf("projection fixture was not added: %+v", result)
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		cloned := projection.Clone()
		if cloned.StorageMetrics().FactRows != 60_000 {
			b.Fatalf("cloned projection rows = %d, want 60000", cloned.StorageMetrics().FactRows)
		}
		projectionCapacityProjectionSink = cloned
	}
	b.StopTimer()
	projectionCapacityProjectionSink = nil
}

func BenchmarkRequestStatisticsSnapshot60000Sparse365d(b *testing.B) {
	fixtures := projectionCapacityFixturesForSpan(60_000, projectionCapacitySpanSparse365d)
	stats := NewRequestStatistics()
	result, err := stats.MergeSnapshotWithError(projectionCapacitySnapshot(fixtures))
	if err != nil || result.Added != int64(len(fixtures)) {
		b.Fatalf("build persistence fixture result=%+v err=%v", result, err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		snapshot, _, _ := stats.SnapshotWithState()
		if snapshot.TotalRequests != int64(len(fixtures)) {
			b.Fatalf("snapshot requests = %d, want %d", snapshot.TotalRequests, len(fixtures))
		}
		runtime.KeepAlive(snapshot)
	}
}

func BenchmarkProjectionGenerationRestart60000Sparse365d(b *testing.B) {
	benchmarkProjectionGenerationRestart(b, 60_000, projectionCapacitySpanSparse365d)
}

func BenchmarkProjectionGenerationRestart250000Sparse365d(b *testing.B) {
	benchmarkProjectionGenerationRestart(b, 250_000, projectionCapacitySpanSparse365d)
}

func BenchmarkProjectionGenerationCrashRestart60000Sparse365d(b *testing.B) {
	benchmarkProjectionGenerationCrashRestart(b, 60_000, projectionCapacitySpanSparse365d)
}

func BenchmarkProjectionGenerationCrashRestart250000Sparse365d(b *testing.B) {
	benchmarkProjectionGenerationCrashRestart(b, 250_000, projectionCapacitySpanSparse365d)
}

func benchmarkProjectionGenerationRestart(b *testing.B, eventCount int, span string) {
	fixtures := projectionCapacityFixturesForSpan(eventCount, span)
	stats := NewRequestStatistics()
	result, err := stats.MergeSnapshotWithError(projectionCapacitySnapshot(fixtures))
	if err != nil || result.Added != int64(len(fixtures)) {
		b.Fatalf("build restart fixture result=%+v err=%v", result, err)
	}
	path := filepath.Join(b.TempDir(), StatisticsFileName)
	if err := SaveProjectionGeneration(path, stats); err != nil {
		b.Fatalf("persist restart generation: %v", err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		restored := NewRequestStatistics()
		loaded, restoredResult, restoreErr := RestoreRequestStatistics(path, restored)
		if restoreErr != nil || !loaded || restoredResult.Added != int64(len(fixtures)) {
			b.Fatalf("restart generation loaded=%t result=%+v err=%v", loaded, restoredResult, restoreErr)
		}
		if replayCount := restored.ReplayFallbackCount(); replayCount != 0 {
			b.Fatalf("restart generation replay fallback count = %d, want 0", replayCount)
		}
		projectionCapacityStatisticsSink = restored
	}
	b.StopTimer()
	reportProjectionGenerationPayloadSize(b, path)
}

func benchmarkProjectionGenerationCrashRestart(b *testing.B, eventCount int, span string) {
	b.StopTimer()
	fixtures := projectionCapacityFixturesForSpan(eventCount, span)
	stats := NewRequestStatistics()
	result, err := stats.MergeSnapshotWithError(projectionCapacitySnapshot(fixtures))
	if err != nil || result.Added != int64(eventCount) {
		b.Fatalf("build crash restart fixture result=%+v err=%v", result, err)
	}
	path := filepath.Join(b.TempDir(), StatisticsFileName)
	if err := SaveProjectionGeneration(path, stats); err != nil {
		b.Fatalf("persist previous crash generation: %v", err)
	}
	previous := readProjectionManifestForBenchmark(b, path)
	extraContext := coreusage.WithRecordContextCarrier(context.Background(), coreusage.RecordContextCarrier{
		RequestID:  fmt.Sprintf("capacity-crash-extra-%d", eventCount),
		Endpoint:   "POST /v1/responses/crash-extra",
		AuthIndex:  "capacity-crash-auth",
		AuthType:   "api_key",
		DetailRole: DetailRolePrimary,
		Success:    true,
		HasSuccess: true,
		Generate:   true,
	})
	if err := stats.RecordWithError(extraContext, coreusage.Record{
		Provider:      "openai",
		ExecutorType:  "openai-executor",
		Model:         "capacity-crash-extra",
		AuthIndex:     "capacity-crash-auth",
		AuthType:      "api_key",
		RequestedAt:   time.Date(2027, 8, 1, 0, 0, 0, int(eventCount), time.UTC),
		UsageObserved: true,
		Detail:        coreusage.Detail{InputTokens: 1, TotalTokens: 1},
	}); err != nil {
		b.Fatalf("append current crash generation detail: %v", err)
	}
	if err := SaveProjectionGeneration(path, stats); err != nil {
		b.Fatalf("persist current crash generation: %v", err)
	}
	current := readProjectionManifestForBenchmark(b, path)
	if current.ParentGeneration != previous.Generation {
		b.Fatalf("crash generation parent = %d, want %d", current.ParentGeneration, previous.Generation)
	}
	fixtures = nil
	stats = nil
	runtime.GC()

	pointerPath := projectionGenerationManifestPath(path)
	pointerData, err := os.ReadFile(pointerPath)
	if err != nil {
		b.Fatalf("read current pointer manifest: %v", err)
	}
	if err := os.WriteFile(pointerPath, []byte("{"), 0o600); err != nil {
		b.Fatalf("write half pointer manifest: %v", err)
	}
	halfPointerStart := time.Now()
	restored := NewRequestStatistics()
	loaded, restoreResult, err := RestoreRequestStatistics(path, restored)
	halfPointerDuration := time.Since(halfPointerStart)
	if err != nil || !loaded || restoreResult.Added != int64(eventCount+1) || restored.ReplayFallbackCount() != 0 {
		b.Fatalf("half pointer restore loaded=%t result=%+v replay=%d err=%v", loaded, restoreResult, restored.ReplayFallbackCount(), err)
	}
	if err := os.WriteFile(pointerPath, pointerData, 0o600); err != nil {
		b.Fatalf("restore current pointer manifest: %v", err)
	}
	restored = nil
	runtime.GC()

	currentProjectionPath := filepath.Join(filepath.Dir(path), current.ProjectionFile)
	currentProjectionBackup := currentProjectionPath + ".capacity-backup"
	if err := os.Rename(currentProjectionPath, currentProjectionBackup); err != nil {
		b.Fatalf("backup current projection: %v", err)
	}
	if err := os.WriteFile(currentProjectionPath, []byte("corrupt"), 0o600); err != nil {
		b.Fatalf("corrupt current projection: %v", err)
	}
	fallbackStart := time.Now()
	restored = NewRequestStatistics()
	loaded, restoreResult, err = RestoreRequestStatistics(path, restored)
	fallbackDuration := time.Since(fallbackStart)
	if err != nil || !loaded || restoreResult.Added != int64(eventCount) || restored.ReplayFallbackCount() != 0 {
		b.Fatalf("previous fallback restore loaded=%t result=%+v replay=%d err=%v", loaded, restoreResult, restored.ReplayFallbackCount(), err)
	}
	if err := os.Remove(currentProjectionPath); err != nil {
		b.Fatalf("remove corrupt current projection: %v", err)
	}
	if err := os.Rename(currentProjectionBackup, currentProjectionPath); err != nil {
		b.Fatalf("restore current projection payload: %v", err)
	}
	restored = nil
	runtime.GC()

	corruptProjectionSectionChecksumForBenchmark(b, path, current)
	corruptProjectionSectionChecksumForBenchmark(b, path, previous)
	failClosedStart := time.Now()
	restored = NewRequestStatistics()
	loaded, restoreResult, err = RestoreRequestStatistics(path, restored)
	failClosedDuration := time.Since(failClosedStart)
	if loaded || !errors.Is(err, ErrProjectionRestoreUnavailable) || restored.Snapshot().TotalRequests != 0 {
		b.Fatalf("checksum mismatch restore loaded=%t result=%+v requests=%d err=%v", loaded, restoreResult, restored.Snapshot().TotalRequests, err)
	}

	b.ReportMetric(float64(eventCount), "events/op")
	b.ReportMetric(halfPointerDuration.Seconds(), "half-pointer-sec/op")
	b.ReportMetric(fallbackDuration.Seconds(), "previous-fallback-sec/op")
	b.ReportMetric(failClosedDuration.Seconds(), "checksum-fail-closed-sec/op")
}

func readProjectionManifestForBenchmark(b *testing.B, path string) ProjectionGenerationManifest {
	b.Helper()
	data, err := os.ReadFile(projectionGenerationManifestPath(path))
	if err != nil {
		b.Fatalf("read projection manifest: %v", err)
	}
	var manifest ProjectionGenerationManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		b.Fatalf("decode projection manifest: %v", err)
	}
	return manifest
}

func corruptProjectionSectionChecksumForBenchmark(b *testing.B, path string, manifest ProjectionGenerationManifest) {
	b.Helper()
	projectionPath := filepath.Join(filepath.Dir(path), manifest.ProjectionFile)
	payload, err := os.ReadFile(projectionPath)
	if err != nil {
		b.Fatalf("read projection payload for checksum corruption: %v", err)
	}
	_, sections, err := decodeProjectionGenerationStateV2(payload)
	if err != nil {
		b.Fatalf("decode projection payload for checksum corruption: %v", err)
	}
	if len(sections.Facts) == 0 {
		b.Fatal("projection facts section is empty")
	}
	factsOffset := bytes.Index(payload, sections.Facts)
	if factsOffset < 0 {
		b.Fatal("projection facts section offset was not found")
	}
	payload[factsOffset+len(sections.Facts)/2] ^= 0x01
	if err := os.WriteFile(projectionPath, payload, 0o600); err != nil {
		b.Fatalf("write projection checksum corruption: %v", err)
	}
	manifest.ProjectionSHA256 = sha256Hex(payload)
	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		b.Fatalf("marshal checksum-corrupt manifest: %v", err)
	}
	manifestData = append(manifestData, '\n')
	if err := os.WriteFile(projectionGenerationManifestFilePath(path, manifest.Generation), manifestData, 0o600); err != nil {
		b.Fatalf("write checksum-corrupt immutable manifest: %v", err)
	}
	current := readProjectionManifestForBenchmark(b, path)
	if current.Generation == manifest.Generation {
		if err := os.WriteFile(projectionGenerationManifestPath(path), manifestData, 0o600); err != nil {
			b.Fatalf("write checksum-corrupt pointer manifest: %v", err)
		}
	}
}

func reportProjectionGenerationPayloadSize(b *testing.B, path string) {
	b.Helper()
	manifestData, err := os.ReadFile(projectionGenerationManifestPath(path))
	if err != nil {
		b.Fatalf("read projection manifest: %v", err)
	}
	var manifest ProjectionGenerationManifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		b.Fatalf("decode projection manifest: %v", err)
	}
	info, err := os.Stat(filepath.Join(filepath.Dir(path), manifest.ProjectionFile))
	if err != nil {
		b.Fatalf("stat projection payload: %v", err)
	}
	b.ReportMetric(float64(info.Size()), "projection-payload-bytes/op")
}

func benchmarkRequestStatisticsHighDimensionRestore(b *testing.B, eventCount int, span string) {
	fixtures := projectionCapacityFixturesForSpan(eventCount, span)
	snapshot := projectionCapacitySnapshot(fixtures)
	projectionCapacityProjectionSink = nil
	projectionCapacityStatisticsSink = nil
	baseline := projectionCapacityHeapSnapshot()
	var measured *RequestStatistics
	b.ReportAllocs()
	b.SetBytes(int64(len(fixtures)))
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		stats := NewRequestStatistics()
		result, err := stats.MergeSnapshotWithError(snapshot)
		if err != nil {
			b.Fatalf("restore fixture: %v", err)
		}
		if result.Added != int64(len(fixtures)) || stats.totalRequests != int64(len(fixtures)) {
			b.Fatalf("restore result = %+v total=%d, want %d", result, stats.totalRequests, len(fixtures))
		}
		projectionCapacityStatisticsSink = stats
		measured = stats
	}
	b.StopTimer()
	retained := projectionCapacityHeapSnapshot()
	b.ReportMetric(float64(len(fixtures)), "events/op")
	reportProjectionCapacityHeap(b, baseline, retained)
	if measured != nil && measured.coordinator != nil {
		_, _, highWatermark, overflowCount := measured.coordinator.Journal().BudgetStats()
		b.ReportMetric(float64(highWatermark), "journal-high-water-bytes/op")
		b.ReportMetric(float64(overflowCount), "journal-overflows/op")
		if marker, ok := measured.coordinator.Journal().TerminalBulkTransactionSnapshot(); ok {
			b.ReportMetric(float64(marker.ChunkCount), "bulk-chunks/op")
		}
	}
}

func projectionCapacityHeapSnapshot() runtime.MemStats {
	runtime.GC()
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	return stats
}

func reportProjectionCapacityHeap(b *testing.B, before, after runtime.MemStats) {
	b.Helper()
	if after.HeapAlloc >= before.HeapAlloc {
		b.ReportMetric(float64(after.HeapAlloc-before.HeapAlloc), "retained-heap-bytes/op")
	}
	if after.HeapInuse >= before.HeapInuse {
		b.ReportMetric(float64(after.HeapInuse-before.HeapInuse), "retained-heap-inuse-bytes/op")
	}
}

func projectionCapacityFixtures(count int) []projectionCapacityFixture {
	return projectionCapacityFixturesForSpan(count, projectionCapacitySpanDense24h)
}

func projectionCapacityFixturesForSpan(count int, span string) []projectionCapacityFixture {
	providers := []string{
		"openai",
		"claude",
		"gemini",
		"vertex",
		"aistudio",
		"antigravity",
		"gemini-interactions",
		"interactions",
	}
	start := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	fixtures := make([]projectionCapacityFixture, 0, count)
	for index := 0; index < count; index++ {
		provider := providers[index%len(providers)]
		model := fmt.Sprintf("model-%03d", index%162)
		authIndex := fmt.Sprintf("auth-%04d", index%1044)
		source := fmt.Sprintf("source-%02d@example.test", index%16)
		apiName := fmt.Sprintf("POST /v1/responses/%02d", index%8)
		requestID := fmt.Sprintf("capacity-%06d", index)
		timestamp := projectionCapacityTimestamp(start, index, count, span)
		failed := index%17 == 0
		tokens := RequestTokenStats{
			InputTokens:         int64(100 + index%31),
			OutputTokens:        int64(20 + index%13),
			ReasoningTokens:     int64(index % 7),
			CachedTokens:        int64(index % 19),
			CacheReadTokens:     int64(index % 11),
			CacheCreationTokens: int64(index % 5),
			TotalTokens:         int64(120 + index%47),
			TokenUsageSource:    TokenUsageSourceProvider,
		}
		detail := normalizeRequestDetail(RequestDetail{
			RequestID:    requestID,
			Timestamp:    timestamp,
			Endpoint:     apiName,
			Model:        model,
			Provider:     provider,
			ExecutorType: provider + "-executor",
			AuthType:     "api_key",
			Source:       source,
			AuthIndex:    authIndex,
			DetailRole:   DetailRolePrimary,
			Failed:       failed,
			LatencyMs:    int64(25 + index%700),
			Tokens:       tokens,
		}, provider)
		carrier := coreusage.RecordContextCarrier{
			RequestID:  requestID,
			Endpoint:   apiName,
			ModelAlias: model,
			DetailRole: DetailRolePrimary,
			Source:     source,
			AuthIndex:  authIndex,
			AuthType:   "api_key",
			Success:    !failed,
			HasSuccess: true,
			Generate:   true,
		}
		record := coreusage.Record{
			Provider:      provider,
			ExecutorType:  provider + "-executor",
			Model:         model,
			Alias:         model,
			AuthIndex:     authIndex,
			AuthType:      "api_key",
			Source:        source,
			RequestedAt:   timestamp,
			Latency:       time.Duration(detail.LatencyMs) * time.Millisecond,
			Failed:        failed,
			UsageObserved: true,
			Detail: coreusage.Detail{
				InputTokens:         tokens.InputTokens,
				OutputTokens:        tokens.OutputTokens,
				ReasoningTokens:     tokens.ReasoningTokens,
				CachedTokens:        tokens.CachedTokens,
				CacheReadTokens:     tokens.CacheReadTokens,
				CacheCreationTokens: tokens.CacheCreationTokens,
				TotalTokens:         tokens.TotalTokens,
			},
		}
		fixtures = append(fixtures, projectionCapacityFixture{
			apiName: apiName,
			detail:  detail,
			ctx:     coreusage.WithRecordContextCarrier(context.Background(), carrier),
			record:  record,
		})
	}
	return fixtures
}

func projectionCapacityTimestamp(start time.Time, index, count int, span string) time.Time {
	if span == projectionCapacitySpanSparse365d {
		return start.AddDate(0, 0, index%365).Add(time.Duration(index/365) * time.Nanosecond)
	}
	return start.Add(time.Duration(index*86_400/count) * time.Second)
}

func projectionCapacitySnapshot(fixtures []projectionCapacityFixture) StatisticsSnapshot {
	snapshot := StatisticsSnapshot{APIs: make(map[string]APISnapshot)}
	for _, fixture := range fixtures {
		api := snapshot.APIs[fixture.apiName]
		if api.Models == nil {
			api.Models = make(map[string]ModelSnapshot)
		}
		model := api.Models[fixture.detail.Model]
		model.Details = append(model.Details, fixture.detail)
		api.Models[fixture.detail.Model] = model
		snapshot.APIs[fixture.apiName] = api
	}
	return snapshot
}

func percentileIndex(length int, percentile float64) int {
	if length <= 1 {
		return 0
	}
	index := int(float64(length-1) * percentile)
	if index < 0 {
		return 0
	}
	if index >= length {
		return length - 1
	}
	return index
}

func projectionCapacityMetrics(projection *UsageProjection) (buckets, aggregates, pricingGroups, facets, postingRefs int) {
	if projection == nil {
		return 0, 0, 0, 0, 0
	}
	countAggregate := func(aggregate compactAggregate) {
		aggregates++
	}
	countAggregate(projection.totals)
	facets += len(projection.facets)
	for _, bucketMap := range []map[string]compactHourBucket{projection.hours, projection.days, projection.weeks, projection.months, projection.years} {
		buckets += len(bucketMap)
		for _, bucket := range bucketMap {
			countAggregate(bucket.Totals)
			facets += len(bucket.Facets)
			for _, dimensions := range []map[projectionStringID]compactAggregate{bucket.APIs, bucket.Models, bucket.Providers, bucket.Auths, bucket.Sources} {
				for _, aggregate := range dimensions {
					countAggregate(aggregate)
				}
			}
		}
	}
	for _, postings := range projection.postings.rows {
		postingRefs += len(postings)
	}
	return buckets, aggregates, pricingGroups, facets, postingRefs
}

func projectionCapacityPricingGroupMetrics(projection *UsageProjection) (single, inline, promoted, max int) {
	return 0, 0, 0, 0
}

func projectionCapacityPricingGroupThresholdMetrics(projection *UsageProjection) (gt8, gt16, gt32, gt64 int) {
	return 0, 0, 0, 0
}
