package usage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	coreusage "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
)

func TestSnapshotDigestV1MatchesCanonicalSortEncoding(t *testing.T) {
	snapshot := projectionCapacitySnapshot(projectionCapacityFixturesForSpan(64, projectionCapacitySpanSparse365d))
	want := snapshotDigestV1CanonicalReference(snapshot)
	if got := SnapshotDigestV1(snapshot); got != want {
		t.Fatalf("snapshot digest = %q, want canonical reference %q", got, want)
	}
}

func snapshotDigestV1CanonicalReference(snapshot StatisticsSnapshot) string {
	items := sortedSnapshotDetails(snapshot)
	hasher := sha256.New()
	_, _ = hasher.Write([]byte(snapshotDigestVersionV1))
	for _, item := range items {
		writeDigestPart(hasher, item.apiName)
		writeDigestPart(hasher, item.modelName)
		writeDigestBytes(hasher, canonicalDetailBytes(item.detail))
	}
	return hex.EncodeToString(hasher.Sum(nil))
}

func TestVerifyIdentitySidecarChecksumSupportsCanonicalFastPathAndFormattedFallback(t *testing.T) {
	sidecar := newIdentitySidecar()
	sidecar.Generation = 7
	sidecar.DatasetEpoch = 3
	checksumInput, err := json.Marshal(sidecar)
	if err != nil {
		t.Fatalf("marshal checksum input: %v", err)
	}
	sidecar.Checksum = sha256Hex(checksumInput)
	canonical, err := json.Marshal(sidecar)
	if err != nil {
		t.Fatalf("marshal canonical sidecar: %v", err)
	}
	if !verifyIdentitySidecarChecksum(canonical, sidecar) {
		t.Fatal("canonical sidecar checksum fast path rejected valid data")
	}
	formatted, err := json.MarshalIndent(sidecar, "", "  ")
	if err != nil {
		t.Fatalf("marshal formatted sidecar: %v", err)
	}
	if !verifyIdentitySidecarChecksum(formatted, sidecar) {
		t.Fatal("formatted sidecar checksum fallback rejected valid data")
	}
	sidecar.Checksum = strings.Repeat("0", 64)
	if verifyIdentitySidecarChecksum(canonical, sidecar) {
		t.Fatal("invalid sidecar checksum was accepted")
	}
}

func TestProjectionGenerationCaptureIsAtomicAcrossConcurrentRecord(t *testing.T) {
	stats := NewRequestStatistics()
	base := coreusage.Record{
		APIKey:      "persist-key",
		Model:       "gpt-5",
		Provider:    "openai",
		RequestedAt: time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC),
		Detail:      coreusage.Detail{InputTokens: 1, TotalTokens: 1},
	}
	stats.Record(context.Background(), base)

	path := filepath.Join(t.TempDir(), StatisticsFileName)
	err := saveProjectionGenerationWithCaptureHook(path, stats, func() {
		concurrent := base
		concurrent.APIKey = "persist-key-2"
		concurrent.RequestedAt = base.RequestedAt.Add(time.Second)
		stats.Record(context.Background(), concurrent)
	})
	if err != nil {
		t.Fatalf("save projection generation: %v", err)
	}

	generation, err := LoadProjectionGeneration(path)
	if err != nil {
		t.Fatalf("load projection generation: %v", err)
	}
	detailCount := len(sortedSnapshotDetails(generation.Snapshot))
	if len(generation.Sidecar.Entries) != detailCount {
		t.Fatalf("captured generation mixed revisions: details=%d sidecar_entries=%d", detailCount, len(generation.Sidecar.Entries))
	}
}

func TestProjectionGenerationCaptureKeepsRecordAdmissionUnder100Milliseconds(t *testing.T) {
	stats := NewRequestStatistics()
	const detailCount = 20_000
	result, err := stats.MergeSnapshotWithError(projectionCapacitySnapshot(projectionCapacityFixturesForSpan(detailCount, projectionCapacitySpanSparse365d)))
	if err != nil || result.Added != detailCount {
		t.Fatalf("build capture fixture result=%+v err=%v", result, err)
	}

	path := filepath.Join(t.TempDir(), StatisticsFileName)
	persistDone := make(chan error, 1)
	go func() {
		persistDone <- SaveProjectionGeneration(path, stats)
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
			t.Fatal("projection generation capture did not enter its admission fence")
		}
		time.Sleep(100 * time.Microsecond)
	}

	record := coreusage.Record{
		APIKey:      "capture-progress-record",
		Model:       "gpt-5",
		Provider:    "openai",
		RequestedAt: time.Date(2026, 8, 9, 21, 0, 0, 0, time.UTC),
		Detail:      coreusage.Detail{InputTokens: 1, TotalTokens: 1},
	}
	startedAt := time.Now()
	if err := stats.RecordWithError(context.Background(), record); err != nil {
		t.Fatalf("record during projection capture: %v", err)
	}
	admissionWait := time.Since(startedAt)
	if err := <-persistDone; err != nil {
		t.Fatalf("persist projection generation: %v", err)
	}
	if admissionWait >= 100*time.Millisecond {
		t.Fatalf("record admission waited %s during projection capture, want <100ms", admissionWait)
	}

	generation, err := LoadProjectionGeneration(path)
	if err != nil {
		t.Fatalf("load projection generation: %v", err)
	}
	capturedDetailCount := len(sortedSnapshotDetails(generation.Snapshot))
	if capturedDetailCount != detailCount+1 {
		t.Fatalf("captured generation details = %d, want base plus concurrent record = %d", capturedDetailCount, detailCount+1)
	}
	if len(generation.Sidecar.Entries) != capturedDetailCount {
		t.Fatalf("captured generation mixed revisions: details=%d sidecar_entries=%d", capturedDetailCount, len(generation.Sidecar.Entries))
	}
	restored := NewRequestStatistics()
	loaded, restoreResult, err := RestoreRequestStatistics(path, restored)
	if err != nil || !loaded {
		t.Fatalf("restore captured generation loaded=%t result=%+v err=%v", loaded, restoreResult, err)
	}
	if got := restored.ReplayFallbackCount(); got != 0 {
		t.Fatalf("captured generation replay fallback count = %d, want 0", got)
	}
	if got, want := len(restored.ProjectionSnapshot().Events), len(generation.Sidecar.Entries); got != want {
		t.Fatalf("restored projection events = %d, want captured sidecar entries %d", got, want)
	}
}

func TestPersistRequestStatisticsSerializesConcurrentWriters(t *testing.T) {
	stats := NewRequestStatistics()
	const detailCount = 10_000
	result, err := stats.MergeSnapshotWithError(projectionCapacitySnapshot(projectionCapacityFixtures(detailCount)))
	if err != nil || result.Added != detailCount {
		t.Fatalf("build concurrent persistence fixture result=%+v err=%v", result, err)
	}

	path := filepath.Join(t.TempDir(), StatisticsFileName)
	start := make(chan struct{})
	type persistResult struct {
		saved bool
		err   error
	}
	results := make(chan persistResult, 2)
	var ready sync.WaitGroup
	ready.Add(2)
	for range 2 {
		go func() {
			ready.Done()
			<-start
			saved, persistErr := PersistRequestStatistics(path, stats)
			results <- persistResult{saved: saved, err: persistErr}
		}()
	}
	ready.Wait()
	close(start)

	savedCount := 0
	for range 2 {
		persisted := <-results
		if persisted.err != nil {
			t.Fatalf("concurrent persistence error = %v", persisted.err)
		}
		if persisted.saved {
			savedCount++
		}
	}
	if savedCount != 1 {
		t.Fatalf("concurrent persistence saved count = %d, want 1", savedCount)
	}
	if _, err := LoadProjectionGeneration(path); err != nil {
		t.Fatalf("load concurrently persisted generation: %v", err)
	}
}

func TestProjectionGenerationPreservesIdentityAcrossRestore(t *testing.T) {
	stats := NewRequestStatistics()
	stats.Record(context.Background(), coreusage.Record{
		APIKey:      "persist-key",
		Model:       "gpt-5",
		Provider:    "OpenAI",
		RequestedAt: time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC),
		Detail: coreusage.Detail{
			InputTokens:  10,
			OutputTokens: 5,
			TotalTokens:  15,
		},
	})
	originalEvents := stats.ProjectionSnapshot().Events
	if len(originalEvents) != 1 {
		t.Fatalf("original event count = %d, want 1", len(originalEvents))
	}

	path := filepath.Join(t.TempDir(), StatisticsFileName)
	if saved, err := PersistRequestStatistics(path, stats); err != nil || !saved {
		t.Fatalf("PersistRequestStatistics() saved=%t err=%v", saved, err)
	}
	if _, err := os.Stat(projectionGenerationManifestPath(path)); err != nil {
		t.Fatalf("generation manifest missing: %v", err)
	}

	restored := NewRequestStatistics()
	loaded, result, err := RestoreRequestStatistics(path, restored)
	if err != nil || !loaded || result.Added != 1 {
		t.Fatalf("restore loaded=%t result=%+v err=%v", loaded, result, err)
	}
	restoredEvents := restored.ProjectionSnapshot().Events
	if len(restoredEvents) != 1 || restoredEvents[0].StableEventID != originalEvents[0].StableEventID {
		t.Fatalf("restored identity = %#v, want %q", restoredEvents, originalEvents[0].StableEventID)
	}

	loaded, result, err = RestoreRequestStatistics(path, restored)
	if err != nil || !loaded || result.Skipped != 1 || result.Added != 0 {
		t.Fatalf("same-digest restore loaded=%t result=%+v err=%v", loaded, result, err)
	}
	if got := restored.Snapshot().TotalRequests; got != 1 {
		t.Fatalf("same-digest restore total requests = %d, want 1", got)
	}
	restored.Record(context.Background(), coreusage.Record{
		APIKey:      "persist-key",
		Model:       "gpt-5",
		Provider:    "OpenAI",
		RequestedAt: time.Date(2026, 8, 6, 12, 0, 1, 0, time.UTC),
		Detail:      coreusage.Detail{InputTokens: 2, OutputTokens: 1, TotalTokens: 3},
	})
	newEvents := restored.ProjectionSnapshot().Events
	if len(newEvents) != 2 {
		t.Fatalf("events after post-restore record = %d, want 2", len(newEvents))
	}
	seenOriginal := false
	seenNew := false
	for _, event := range newEvents {
		switch event.StableEventID {
		case originalEvents[0].StableEventID:
			seenOriginal = true
		default:
			seenNew = true
			if event.Sequence <= originalEvents[0].Sequence {
				t.Fatalf("post-restore sequence=%d, want > %d", event.Sequence, originalEvents[0].Sequence)
			}
		}
	}
	if !seenOriginal || !seenNew {
		t.Fatalf("post-restore event identities were not preserved: %#v", newEvents)
	}
}

func TestProjectionGenerationUsesCompactProjectionPayloadV2(t *testing.T) {
	stats := NewRequestStatistics()
	fixtures := projectionCapacityFixtures(projectionCapacitySmokeEvents)
	for _, fixture := range fixtures {
		if err := stats.RecordWithError(fixture.ctx, fixture.record); err != nil {
			t.Fatalf("record fixture: %v", err)
		}
	}

	path := filepath.Join(t.TempDir(), StatisticsFileName)
	if err := SaveProjectionGeneration(path, stats); err != nil {
		t.Fatalf("SaveProjectionGeneration() error = %v", err)
	}
	manifest := readProjectionManifest(t, path)
	if manifest.SchemaVersion != "usage_projection_generation_manifest_v2" {
		t.Fatalf("manifest schema = %q, want v2", manifest.SchemaVersion)
	}
	if ext := filepath.Ext(manifest.ProjectionFile); ext != ".bin" {
		t.Fatalf("projection file = %q, want compact binary payload", manifest.ProjectionFile)
	}
	payload, err := os.ReadFile(filepath.Join(filepath.Dir(path), manifest.ProjectionFile))
	if err != nil {
		t.Fatalf("read compact projection payload: %v", err)
	}
	state, _, err := decodeProjectionGenerationStateV2(payload)
	if err != nil {
		t.Fatalf("decode compact projection payload: %v", err)
	}
	metrics := stats.projection.StorageMetrics()
	if state.StorageMetrics.FactRows != metrics.FactRows || state.StorageMetrics.PostingRefs != metrics.PostingRefs || state.StorageMetrics.RollupCells != metrics.RollupCells {
		t.Fatalf("persisted storage metrics = %+v, want %+v", state.StorageMetrics, metrics)
	}
	if state.FactsSHA256 == "" || state.PostingsSHA256 == "" || state.ScalarRollupSHA256 == "" {
		t.Fatalf("projection section checksums are incomplete: %+v", state)
	}
	if len(payload) >= 16<<20 {
		t.Fatalf("compact projection payload = %d bytes, want < 16 MiB", len(payload))
	}
}

func TestProjectionGenerationStateV2RejectsSectionCorruption(t *testing.T) {
	projection := NewUsageProjection()
	fixture := projectionCapacityFixtures(1)[0]
	if result := projection.ApplyDetail(fixture.apiName, fixture.detail); !result.Added {
		t.Fatalf("projection fixture was not added: %+v", result)
	}
	_, payload, err := projection.marshalGenerationStateV2()
	if err != nil {
		t.Fatalf("marshal compact projection payload: %v", err)
	}
	payload[len(payload)-1] ^= 0xff
	if _, _, err := decodeProjectionGenerationStateV2(payload); !errors.Is(err, ErrProjectionRestoreUnavailable) {
		t.Fatalf("decode corrupted projection error = %v, want ErrProjectionRestoreUnavailable", err)
	}
}

func TestProjectionGenerationLoadsLegacyV1JSONProjection(t *testing.T) {
	stats := NewRequestStatistics()
	stats.Record(context.Background(), coreusage.Record{
		APIKey:      "legacy-v1",
		Model:       "gpt-5",
		Provider:    "openai",
		RequestedAt: time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC),
		Detail:      coreusage.Detail{InputTokens: 2, OutputTokens: 1, TotalTokens: 3},
	})
	path := filepath.Join(t.TempDir(), StatisticsFileName)
	if err := SaveProjectionGeneration(path, stats); err != nil {
		t.Fatalf("SaveProjectionGeneration() error = %v", err)
	}

	manifest := readProjectionManifest(t, path)
	legacyProjection, err := json.Marshal(stats.ProjectionSnapshot())
	if err != nil {
		t.Fatalf("marshal legacy projection: %v", err)
	}
	manifest.SchemaVersion = projectionGenerationManifestVersionV1
	manifest.ProjectionFile = projectionGenerationFileBase(path, manifest.Generation, "projection.json")
	manifest.ProjectionSHA256 = sha256Hex(legacyProjection)
	manifest.ProjectionSchema = ""
	manifest.FactsSHA256 = ""
	manifest.PostingsSHA256 = ""
	manifest.ScalarRollupSHA256 = ""
	if err := os.WriteFile(filepath.Join(filepath.Dir(path), manifest.ProjectionFile), legacyProjection, 0o600); err != nil {
		t.Fatalf("write legacy projection: %v", err)
	}
	manifestData, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal legacy manifest: %v", err)
	}
	if err := os.WriteFile(projectionGenerationManifestPath(path), manifestData, 0o600); err != nil {
		t.Fatalf("write legacy pointer manifest: %v", err)
	}
	if err := os.WriteFile(projectionGenerationManifestFilePath(path, manifest.Generation), manifestData, 0o600); err != nil {
		t.Fatalf("write legacy immutable manifest: %v", err)
	}

	generation, err := LoadProjectionGeneration(path)
	if err != nil {
		t.Fatalf("LoadProjectionGeneration() legacy v1 error = %v", err)
	}
	if generation.Projection.Revision != manifest.Revision || generation.Projection.DatasetEpoch != manifest.DatasetEpoch {
		t.Fatalf("legacy projection metadata = %+v, want revision=%d epoch=%d", generation.Projection, manifest.Revision, manifest.DatasetEpoch)
	}
}

func TestProjectionGenerationPersistsBulkTransactionFrontier(t *testing.T) {
	stats := NewRequestStatistics()
	stats.coordinator = NewMutationCoordinatorWithConfig(stats, MutationCoordinatorConfig{
		JournalEntryLimit:        8,
		JournalBudgetBytes:       8 * 4096,
		MaxSerializedIntentBytes: 4096,
		MaxBulkChunkIntents:      2,
		MaxBulkChunkBytes:        16 * 1024,
		GateQueueLimit:           4,
	})
	result, err := stats.MergeSnapshotWithError(bulkTestSnapshot(10))
	if err != nil || result.Added != 10 {
		t.Fatalf("bulk merge result=%+v err=%v", result, err)
	}
	path := filepath.Join(t.TempDir(), StatisticsFileName)
	if saved, persistErr := PersistRequestStatistics(path, stats); persistErr != nil || !saved {
		t.Fatalf("persist bulk generation saved=%t err=%v", saved, persistErr)
	}
	generation, err := LoadProjectionGeneration(path)
	if err != nil {
		t.Fatalf("load bulk generation: %v", err)
	}
	marker := generation.Sidecar.BulkTransaction
	if marker == nil || marker.TerminalState != MutationTerminalCommitted || marker.FrontierOrdinal != 10 || marker.ChunkCount != 5 || !isMutationSHA256(marker.ManifestSHA256) {
		t.Fatalf("persisted bulk marker = %+v", marker)
	}
	restored := NewRequestStatistics()
	restored.coordinator = NewMutationCoordinatorWithConfig(restored, MutationCoordinatorConfig{
		JournalEntryLimit:        8,
		JournalBudgetBytes:       8 * 4096,
		MaxSerializedIntentBytes: 4096,
		MaxBulkChunkIntents:      2,
		MaxBulkChunkBytes:        16 * 1024,
		GateQueueLimit:           4,
	})
	loaded, restoredResult, err := RestoreRequestStatistics(path, restored)
	if err != nil || !loaded || restoredResult.Added != 10 {
		t.Fatalf("restore bulk generation loaded=%t result=%+v err=%v", loaded, restoredResult, err)
	}
	restoredMarker, ok := restored.coordinator.Journal().TerminalBulkTransactionSnapshot()
	if !ok || restoredMarker.Operation != MutationOperationRestore || restoredMarker.TerminalState != MutationTerminalCommitted || restoredMarker.PreviousMarkerSHA256 != marker.MarkerSHA256 || restoredMarker.BatchSequence <= marker.BatchSequence {
		t.Fatalf("restored bulk marker = %+v/%t, want restore chained after %s", restoredMarker, ok, marker.MarkerSHA256)
	}
	wantChunkSHA256 := mutationRestoreChunkChecksum(generation.Sidecar, generation.Manifest.Generation)
	if restoredMarker.LastChunkSHA256 != wantChunkSHA256 || restoredMarker.LastChunkSHA256 == generation.Sidecar.PayloadSHA256 {
		t.Fatalf("restored chunk checksum = %q, want domain-separated %q", restoredMarker.LastChunkSHA256, wantChunkSHA256)
	}
	if got := restored.coordinator.Journal().Head(); got != generation.Sidecar.NextSequence-1 {
		t.Fatalf("restored journal head = %d, want %d", got, generation.Sidecar.NextSequence-1)
	}
	if got := restored.coordinator.nextApplySeq; got != generation.Sidecar.NextSequence {
		t.Fatalf("restored next apply sequence = %d, want %d", got, generation.Sidecar.NextSequence)
	}
}

func TestProjectionGenerationCorruptCurrentFallsBackToPrevious(t *testing.T) {
	stats := NewRequestStatistics()
	stats.Record(context.Background(), coreusage.Record{
		APIKey:      "persist-key",
		Model:       "gpt-5",
		RequestedAt: time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC),
		Detail:      coreusage.Detail{TotalTokens: 1},
	})
	path := filepath.Join(t.TempDir(), StatisticsFileName)
	if _, err := PersistRequestStatistics(path, stats); err != nil {
		t.Fatalf("PersistRequestStatistics() error = %v", err)
	}
	firstManifest := readProjectionManifest(t, path)
	stats.Record(context.Background(), coreusage.Record{
		APIKey:      "persist-key",
		Model:       "gpt-5",
		RequestedAt: time.Date(2026, 8, 6, 12, 0, 1, 0, time.UTC),
		Detail:      coreusage.Detail{TotalTokens: 2},
	})
	if _, err := PersistRequestStatistics(path, stats); err != nil {
		t.Fatalf("second PersistRequestStatistics() error = %v", err)
	}
	currentManifest := readProjectionManifest(t, path)
	if currentManifest.ParentGeneration != firstManifest.Generation {
		t.Fatalf("current parent generation = %d, want %d", currentManifest.ParentGeneration, firstManifest.Generation)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(path), currentManifest.SidecarFile), []byte("corrupt"), 0o600); err != nil {
		t.Fatalf("corrupt sidecar: %v", err)
	}

	restored := NewRequestStatistics()
	loaded, result, err := RestoreRequestStatistics(path, restored)
	if err != nil || !loaded || result.Added != 1 {
		t.Fatalf("fallback restore loaded=%t result=%+v err=%v", loaded, result, err)
	}
	if got := restored.Snapshot().TotalRequests; got != 1 {
		t.Fatalf("fallback restored total requests = %d, want previous generation total 1", got)
	}
	loadedGeneration, err := LoadProjectionGeneration(path)
	if err != nil || loadedGeneration.Manifest.Generation != firstManifest.Generation {
		t.Fatalf("loaded fallback generation = %d err=%v, want %d", loadedGeneration.Manifest.Generation, err, firstManifest.Generation)
	}
}

func TestProjectionGenerationCorruptCurrentProjectionFallsBackToPrevious(t *testing.T) {
	stats := NewRequestStatistics()
	stats.Record(context.Background(), coreusage.Record{
		APIKey:      "persist-key",
		Model:       "gpt-5",
		RequestedAt: time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC),
		Detail:      coreusage.Detail{TotalTokens: 1},
	})
	path := filepath.Join(t.TempDir(), StatisticsFileName)
	if _, err := PersistRequestStatistics(path, stats); err != nil {
		t.Fatalf("PersistRequestStatistics() error = %v", err)
	}
	firstManifest := readProjectionManifest(t, path)
	stats.Record(context.Background(), coreusage.Record{
		APIKey:      "persist-key",
		Model:       "gpt-5",
		RequestedAt: time.Date(2026, 8, 6, 12, 0, 1, 0, time.UTC),
		Detail:      coreusage.Detail{TotalTokens: 2},
	})
	if _, err := PersistRequestStatistics(path, stats); err != nil {
		t.Fatalf("second PersistRequestStatistics() error = %v", err)
	}
	currentManifest := readProjectionManifest(t, path)
	if err := os.WriteFile(filepath.Join(filepath.Dir(path), currentManifest.ProjectionFile), []byte("corrupt"), 0o600); err != nil {
		t.Fatalf("corrupt projection payload: %v", err)
	}

	generation, err := LoadProjectionGeneration(path)
	if err != nil || generation.Manifest.Generation != firstManifest.Generation {
		t.Fatalf("loaded fallback generation = %d err=%v, want %d", generation.Manifest.Generation, err, firstManifest.Generation)
	}
}

func TestProjectionGenerationSingleCorruptionFailsClosed(t *testing.T) {
	stats := NewRequestStatistics()
	stats.Record(context.Background(), coreusage.Record{
		APIKey:      "persist-key",
		Model:       "gpt-5",
		RequestedAt: time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC),
		Detail:      coreusage.Detail{TotalTokens: 1},
	})
	path := filepath.Join(t.TempDir(), StatisticsFileName)
	if _, err := PersistRequestStatistics(path, stats); err != nil {
		t.Fatalf("PersistRequestStatistics() error = %v", err)
	}
	manifest := readProjectionManifest(t, path)
	if err := os.WriteFile(filepath.Join(filepath.Dir(path), manifest.SidecarFile), []byte("corrupt"), 0o600); err != nil {
		t.Fatalf("corrupt sidecar: %v", err)
	}
	_, _, err := RestoreRequestStatistics(path, NewRequestStatistics())
	if !errors.Is(err, ErrProjectionRestoreUnavailable) {
		t.Fatalf("restore error = %v, want ErrProjectionRestoreUnavailable", err)
	}
}

func TestProjectionGenerationHalfWrittenPointerUsesCommittedManifest(t *testing.T) {
	stats := NewRequestStatistics()
	stats.Record(context.Background(), coreusage.Record{
		APIKey: "persist-key", Model: "gpt-5",
		RequestedAt: time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC),
		Detail:      coreusage.Detail{TotalTokens: 1},
	})
	path := filepath.Join(t.TempDir(), StatisticsFileName)
	if _, err := PersistRequestStatistics(path, stats); err != nil {
		t.Fatalf("PersistRequestStatistics() error = %v", err)
	}
	committed := readProjectionManifest(t, path)
	if err := os.WriteFile(projectionGenerationManifestPath(path), []byte(`{"schema_version":`), 0o600); err != nil {
		t.Fatalf("write torn pointer manifest: %v", err)
	}
	generation, err := LoadProjectionGeneration(path)
	if err != nil || generation.Manifest.Generation != committed.Generation {
		t.Fatalf("torn pointer load generation=%d err=%v, want %d", generation.Manifest.Generation, err, committed.Generation)
	}
}

func TestProjectionGenerationGCKeepsCurrentAndPrevious(t *testing.T) {
	stats := NewRequestStatistics()
	path := filepath.Join(t.TempDir(), StatisticsFileName)
	for index := 0; index < 3; index++ {
		stats.Record(context.Background(), coreusage.Record{
			APIKey: "persist-key", Model: "gpt-5",
			RequestedAt: time.Date(2026, 8, 6, 12, 0, index, 0, time.UTC),
			Detail:      coreusage.Detail{TotalTokens: int64(index + 1)},
		})
		if _, err := PersistRequestStatistics(path, stats); err != nil {
			t.Fatalf("persist generation %d: %v", index, err)
		}
	}
	manifestPaths, err := filepath.Glob(filepath.Join(filepath.Dir(path), filepath.Base(path)+".generation-*.manifest"))
	if err != nil {
		t.Fatalf("glob manifests: %v", err)
	}
	if len(manifestPaths) != 2 {
		t.Fatalf("retained immutable manifests = %d, want current+previous", len(manifestPaths))
	}
	projectionPaths, err := filepath.Glob(filepath.Join(filepath.Dir(path), filepath.Base(path)+".generation-*.projection.bin"))
	if err != nil {
		t.Fatalf("glob projection payloads: %v", err)
	}
	if len(projectionPaths) != 2 {
		t.Fatalf("retained projection payloads = %d, want current+previous", len(projectionPaths))
	}
	current := readProjectionManifest(t, path)
	kept := map[uint64]bool{}
	for _, manifestPath := range manifestPaths {
		data, readErr := os.ReadFile(manifestPath)
		if readErr != nil {
			t.Fatalf("read immutable manifest: %v", readErr)
		}
		var manifest ProjectionGenerationManifest
		if decodeErr := json.Unmarshal(data, &manifest); decodeErr != nil {
			t.Fatalf("decode immutable manifest: %v", decodeErr)
		}
		kept[manifest.Generation] = true
	}
	if !kept[current.Generation] || !kept[current.ParentGeneration] {
		t.Fatalf("kept generations = %#v, want current=%d previous=%d", kept, current.Generation, current.ParentGeneration)
	}
}

func TestProjectionGenerationCleansCrashTempFiles(t *testing.T) {
	stats := NewRequestStatistics()
	stats.Record(context.Background(), coreusage.Record{
		APIKey: "persist-key", Model: "gpt-5",
		RequestedAt: time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC),
		Detail:      coreusage.Detail{TotalTokens: 1},
	})
	path := filepath.Join(t.TempDir(), StatisticsFileName)
	if err := SaveProjectionGeneration(path, stats); err != nil {
		t.Fatalf("SaveProjectionGeneration() error = %v", err)
	}
	tempPath := filepath.Join(filepath.Dir(path), filepath.Base(path)+".generation-999.projection.bin.tmp-crash")
	if err := os.WriteFile(tempPath, []byte("partial"), 0o600); err != nil {
		t.Fatalf("write crash temp file: %v", err)
	}
	if _, err := LoadProjectionGeneration(path); err != nil {
		t.Fatalf("LoadProjectionGeneration() error = %v", err)
	}
	if _, err := os.Stat(tempPath); !os.IsNotExist(err) {
		t.Fatalf("crash temp file still exists: %v", err)
	}
}

func TestProjectionGenerationLargeSnapshotRestartUsesCompactPayload(t *testing.T) {
	const detailCount = 10_000
	stats := NewRequestStatistics()
	result, err := stats.MergeSnapshotWithError(projectionCapacitySnapshot(projectionCapacityFixtures(detailCount)))
	if err != nil || result.Added != detailCount {
		t.Fatalf("build large snapshot result=%+v err=%v", result, err)
	}

	path := filepath.Join(t.TempDir(), StatisticsFileName)
	if err := SaveProjectionGeneration(path, stats); err != nil {
		t.Fatalf("SaveProjectionGeneration() error = %v", err)
	}
	manifest := readProjectionManifest(t, path)
	projectionInfo, err := os.Stat(filepath.Join(filepath.Dir(path), manifest.ProjectionFile))
	if err != nil {
		t.Fatalf("stat compact projection payload: %v", err)
	}
	if projectionInfo.Size() >= 32<<20 {
		t.Fatalf("compact projection payload = %d bytes, want < 32 MiB", projectionInfo.Size())
	}

	restored := NewRequestStatistics()
	loaded, restoredResult, err := RestoreRequestStatistics(path, restored)
	if err != nil || !loaded || restoredResult.Added != detailCount {
		t.Fatalf("large restart loaded=%t result=%+v err=%v", loaded, restoredResult, err)
	}
	metrics := restored.projection.StorageMetrics()
	if metrics.FactRows != detailCount || metrics.PostingRefs != detailCount*6 {
		t.Fatalf("restored storage metrics = %+v", metrics)
	}
}

func readProjectionManifest(t *testing.T, path string) ProjectionGenerationManifest {
	t.Helper()
	data, err := os.ReadFile(projectionGenerationManifestPath(path))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var manifest ProjectionGenerationManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	return manifest
}

func TestProjectionGenerationEmptyMetadataMatchesManifest(t *testing.T) {
	stats := NewRequestStatistics()
	path := filepath.Join(t.TempDir(), StatisticsFileName)
	if err := SaveProjectionGeneration(path, stats); err != nil {
		t.Fatalf("SaveProjectionGeneration() error = %v", err)
	}
	generation, err := LoadProjectionGeneration(path)
	if err != nil {
		t.Fatalf("LoadProjectionGeneration() error = %v", err)
	}
	if generation.Manifest.Generation == 0 || generation.Sidecar.Generation != generation.Manifest.Generation {
		t.Fatalf("generation metadata = manifest=%d sidecar=%d", generation.Manifest.Generation, generation.Sidecar.Generation)
	}
}

func TestStatisticsReadyGateRejectsRecordsUntilOpened(t *testing.T) {
	previousEnabled := StatisticsEnabled()
	previousReady := StatisticsReady()
	SetStatisticsEnabled(true)
	SetStatisticsReady(false)
	t.Cleanup(func() {
		SetStatisticsEnabled(previousEnabled)
		SetStatisticsReady(previousReady)
	})

	stats := NewRequestStatistics()
	stats.Record(context.Background(), coreusage.Record{
		APIKey:      "ready-gate",
		Model:       "gpt-5",
		RequestedAt: time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC),
		Detail:      coreusage.Detail{TotalTokens: 1},
	})
	if got := stats.Snapshot().TotalRequests; got != 0 {
		t.Fatalf("record admitted before restore ready gate: total_requests=%d", got)
	}
	SetStatisticsReady(true)
	stats.Record(context.Background(), coreusage.Record{
		APIKey:      "ready-gate",
		Model:       "gpt-5",
		RequestedAt: time.Date(2026, 8, 6, 12, 0, 1, 0, time.UTC),
		Detail:      coreusage.Detail{TotalTokens: 1},
	})
	if got := stats.Snapshot().TotalRequests; got != 1 {
		t.Fatalf("record after restore ready gate total_requests=%d, want 1", got)
	}
}
