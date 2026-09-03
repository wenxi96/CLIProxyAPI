package usage

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/logging"
	coreusage "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
)

func TestMutationJournalSequencesAndTerminalOutcomes(t *testing.T) {
	journal := NewMutationJournal(2)
	first, err := journal.Append(MutationIntent{Operation: MutationOperationRecord})
	if err != nil || first.Sequence != 1 {
		t.Fatalf("first append = %+v, %v; want sequence 1", first, err)
	}
	duplicate, err := journal.Append(MutationIntent{Operation: MutationOperationRecord, IdempotencyKey: first.IdempotencyKey})
	if err != nil || duplicate.Sequence != first.Sequence {
		t.Fatalf("idempotent append = %+v, %v; want sequence %d", duplicate, err, first.Sequence)
	}
	second, err := journal.Append(MutationIntent{Operation: MutationOperationImportMerge, IdempotencyKey: "batch:one:1"})
	if err != nil || second.Sequence != 2 || journal.PendingCount() != 2 {
		t.Fatalf("second append = %+v, %v; pending=%d", second, err, journal.PendingCount())
	}
	if err := journal.MarkTerminal(first.Sequence, MutationTerminalCommitted, nil); err != nil {
		t.Fatalf("mark first terminal: %v", err)
	}
	if _, ok := journal.Outcome(first.Sequence); !ok {
		t.Fatal("terminal outcome missing")
	}
	if err := journal.MarkTerminal(first.Sequence, MutationTerminalTombstone, errors.New("late failure")); !errors.Is(err, ErrMutationAlreadyTerminal) {
		t.Fatalf("second terminal mark = %v, want ErrMutationAlreadyTerminal", err)
	}
	if err := journal.MarkTerminal(second.Sequence, MutationTerminalTombstone, errors.New("projection unavailable")); err != nil {
		t.Fatalf("mark second tombstone: %v", err)
	}
	if journal.PendingCount() != 0 || journal.Head() != 2 {
		t.Fatalf("journal terminal state pending=%d head=%d", journal.PendingCount(), journal.Head())
	}
}

func TestMutationJournalBulkTransactionFrontierAndChecksum(t *testing.T) {
	journal := NewMutationJournal(8)
	payloadSHA256 := mutationSHA256Parts("bulk-payload")
	if err := journal.BeginBulkTransaction(MutationBatchMarker{
		BatchID:       "import:1",
		BatchSequence: 1,
		Operation:     MutationOperationImportMerge,
		BaseRevision:  7,
		DatasetEpoch:  3,
		TotalIntents:  4,
		PayloadSHA256: payloadSHA256,
	}); err != nil {
		t.Fatalf("begin bulk transaction: %v", err)
	}
	if _, err := journal.AdvanceBulkChunk("import:1", 1, 2, mutationSHA256Parts("wrong-frontier")); !errors.Is(err, ErrMutationBatchFrontier) {
		t.Fatalf("out-of-order chunk error = %v, want ErrMutationBatchFrontier", err)
	}
	firstChunk := mutationSHA256Parts("chunk-0-2")
	first, err := journal.AdvanceBulkChunk("import:1", 0, 2, firstChunk)
	if err != nil {
		t.Fatalf("advance first chunk: %v", err)
	}
	if first.FrontierOrdinal != 2 || first.ChunkCount != 1 || first.LastChunkSHA256 != firstChunk || !isMutationSHA256(first.ChunkChecksumChain) {
		t.Fatalf("first chunk marker = %+v", first)
	}
	if _, err := journal.CompleteBulkTransaction("import:1", MutationTerminalCommitted, "", mutationSHA256Parts("manifest")); !errors.Is(err, ErrMutationBatchIncomplete) {
		t.Fatalf("incomplete commit error = %v, want ErrMutationBatchIncomplete", err)
	}
	second, err := journal.AdvanceBulkChunk("import:1", 2, 4, mutationSHA256Parts("chunk-2-4"))
	if err != nil {
		t.Fatalf("advance second chunk: %v", err)
	}
	if second.ChunkChecksumChain == first.ChunkChecksumChain {
		t.Fatal("chunk checksum chain did not advance")
	}
	terminal, err := journal.CompleteBulkTransaction("import:1", MutationTerminalCommitted, "", mutationSHA256Parts("manifest"))
	if err != nil {
		t.Fatalf("complete bulk transaction: %v", err)
	}
	if terminal.TerminalState != MutationTerminalCommitted || terminal.FrontierOrdinal != 4 || !isMutationSHA256(terminal.MarkerSHA256) {
		t.Fatalf("terminal bulk marker = %+v", terminal)
	}
	if restored, ok := journal.TerminalBulkTransactionSnapshot(); !ok || restored.MarkerSHA256 != terminal.MarkerSHA256 {
		t.Fatalf("terminal marker snapshot = %+v/%t, want %s", restored, ok, terminal.MarkerSHA256)
	}
	corrupt := terminal
	corrupt.FrontierOrdinal--
	if err := NewMutationJournal(8).RestoreBulkTransaction(corrupt); !errors.Is(err, ErrMutationBatchChecksum) {
		t.Fatalf("corrupt marker restore error = %v, want ErrMutationBatchChecksum", err)
	}
}

func TestMutationCoordinatorReusesIdentityForEnrichment(t *testing.T) {
	stats := NewRequestStatistics()
	timestamp := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	record := coreusage.Record{
		Provider:               "openai",
		Model:                  "gpt-5",
		AuthType:               "api_key",
		AuthIndex:              "auth-mutation",
		AdmissionDiscriminator: "reporter:enrichment",
		RequestedAt:            timestamp,
		Detail:                 coreusage.Detail{},
	}
	ctx := context.Background()
	if err := stats.RecordWithError(ctx, record); err != nil {
		t.Fatalf("first record: %v", err)
	}
	record.Detail = coreusage.Detail{InputTokens: 20, OutputTokens: 20, TotalTokens: 40}
	if err := stats.RecordWithError(ctx, record); err != nil {
		t.Fatalf("second record: %v", err)
	}
	snapshot := stats.ProjectionSnapshot()
	if snapshot.Totals.TotalRequests != 1 || snapshot.Totals.Tokens.TotalTokens != 40 || len(snapshot.Events) != 1 {
		t.Fatalf("enrichment allocated a second event: totals=%+v events=%d", snapshot.Totals, len(snapshot.Events))
	}
	if stats.coordinator.Journal().Head() != 2 {
		t.Fatalf("journal head = %d, want two admitted intents", stats.coordinator.Journal().Head())
	}
}

func TestMutationCoordinatorReusesPendingIdentityBeforeApply(t *testing.T) {
	stats := NewRequestStatistics()
	coordinator := stats.coordinator
	timestamp := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	baseContext := logging.WithEndpoint(context.Background(), "POST /v1/responses")
	firstContext := logging.WithRequestID(baseContext, "pending-coordinate")
	secondContext := logging.WithRequestID(baseContext, "pending-coordinate")
	first := coreusage.Record{
		Provider:               "openai",
		Model:                  "gpt-5",
		AuthType:               "api_key",
		AuthIndex:              "pending-auth",
		AdmissionDiscriminator: "reporter:pending",
		RequestedAt:            timestamp,
		Detail:                 coreusage.Detail{InputTokens: 1, TotalTokens: 1},
	}
	second := first
	second.Detail = coreusage.Detail{InputTokens: 10, OutputTokens: 5, TotalTokens: 15}

	coordinator.applyMu.Lock()
	errorsCh := make(chan error, 2)
	go func() { errorsCh <- stats.RecordWithError(firstContext, first) }()
	waitForJournalHead(t, coordinator, 1)
	go func() { errorsCh <- stats.RecordWithError(secondContext, second) }()
	waitForJournalHead(t, coordinator, 2)
	coordinator.applyMu.Unlock()

	for index := 0; index < 2; index++ {
		if err := <-errorsCh; err != nil {
			t.Fatalf("record %d: %v", index, err)
		}
	}
	snapshot := stats.ProjectionSnapshot()
	if len(snapshot.Events) != 1 || snapshot.Events[0].Sequence != 1 {
		t.Fatalf("pending admission allocated duplicate identity: events=%+v", snapshot.Events)
	}
	if snapshot.Totals.TotalRequests != 1 || snapshot.Totals.Tokens.TotalTokens != 15 {
		t.Fatalf("pending enrichment totals = %+v, want one request with 15 tokens", snapshot.Totals)
	}
}

func TestMutationCoordinatorBatchReusesPendingIdentity(t *testing.T) {
	stats := NewRequestStatistics()
	detail := RequestDetail{
		RequestID: "batch-duplicate",
		Timestamp: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		Endpoint:  "POST /v1/responses",
		Model:     "gpt-5",
		Provider:  "openai",
		AuthType:  "api_key",
		AuthIndex: "batch-auth",
		Tokens:    RequestTokenStats{InputTokens: 2, TotalTokens: 2},
	}
	snapshot := StatisticsSnapshot{APIs: map[string]APISnapshot{
		"api": {Models: map[string]ModelSnapshot{"gpt-5": {Details: []RequestDetail{detail, detail}}}},
	}}
	result, err := stats.MergeSnapshotWithError(snapshot)
	if err != nil {
		t.Fatalf("batch duplicate import: %v", err)
	}
	if result.Added != 1 || result.Skipped != 1 || result.Enriched != 0 {
		t.Fatalf("batch duplicate result = %+v, want added=1 skipped=1", result)
	}
	if events := stats.ProjectionSnapshot().Events; len(events) != 1 {
		t.Fatalf("batch duplicate allocated %d events, want one", len(events))
	}
}

func TestMutationCoordinatorBatchKeepsDistinctLogicalIdentitiesWithSharedCoordinate(t *testing.T) {
	stats := NewRequestStatistics()
	timestamp := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	openAI := RequestDetail{
		RequestID: "batch-shared-coordinate", Timestamp: timestamp, Endpoint: "POST /v1/responses",
		Model: "gpt-5", Provider: "openai", AuthType: "api_key", AuthIndex: "batch-shared-auth",
		Tokens: RequestTokenStats{InputTokens: 1, TotalTokens: 1},
	}
	azure := openAI
	azure.Model = "gpt-5-deployed"
	azure.Provider = "azure-openai"
	azure.Tokens = RequestTokenStats{InputTokens: 2, TotalTokens: 2}
	result, err := stats.MergeSnapshotWithError(StatisticsSnapshot{APIs: map[string]APISnapshot{
		"POST /v1/responses": {Models: map[string]ModelSnapshot{
			"gpt-5":          {Details: []RequestDetail{openAI}},
			"gpt-5-deployed": {Details: []RequestDetail{azure}},
		}},
	}})
	if err != nil {
		t.Fatalf("batch merge: %v", err)
	}
	if result.Added != 2 || result.Enriched != 0 || result.Skipped != 0 {
		t.Fatalf("batch result = %+v, want two independent additions", result)
	}
	snapshot := stats.ProjectionSnapshot()
	if len(snapshot.Events) != 2 || snapshot.Totals.TotalRequests != 2 || snapshot.Totals.Tokens.TotalTokens != 3 {
		t.Fatalf("shared-coordinate batch collapsed events: events=%d totals=%+v", len(snapshot.Events), snapshot.Totals)
	}
}

func TestMutationCoordinatorSecondBatchDoesNotRekeySharedCoordinate(t *testing.T) {
	stats := NewRequestStatistics()
	timestamp := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	first := RequestDetail{
		RequestID: "cross-batch-shared-coordinate", Timestamp: timestamp, Endpoint: "POST /v1/responses",
		Model: "gpt-5", Provider: "openai", AuthType: "api_key", AuthIndex: "cross-batch-auth",
		Tokens: RequestTokenStats{InputTokens: 1, TotalTokens: 1},
	}
	second := first
	second.Model = "gpt-5-deployed"
	second.Provider = "azure-openai"
	second.Tokens = RequestTokenStats{InputTokens: 2, TotalTokens: 2}
	merge := func(detail RequestDetail, model string) MergeResult {
		result, err := stats.MergeSnapshotWithError(StatisticsSnapshot{APIs: map[string]APISnapshot{
			"POST /v1/responses": {Models: map[string]ModelSnapshot{model: {Details: []RequestDetail{detail}}}},
		}})
		if err != nil {
			t.Fatalf("merge %s: %v", model, err)
		}
		return result
	}
	if result := merge(first, "gpt-5"); result.Added != 1 || result.Enriched != 0 {
		t.Fatalf("first batch result = %+v, want one addition", result)
	}
	if result := merge(second, "gpt-5-deployed"); result.Added != 1 || result.Enriched != 0 {
		t.Fatalf("second batch result = %+v, want independent addition", result)
	}
	snapshot := stats.ProjectionSnapshot()
	if len(snapshot.Events) != 2 || snapshot.Totals.TotalRequests != 2 || snapshot.Totals.Tokens.TotalTokens != 3 {
		t.Fatalf("cross-batch shared coordinate rekeyed an old event: events=%d totals=%+v", len(snapshot.Events), snapshot.Totals)
	}
}

func TestMutationCoordinatorDoesNotReuseUnseededIdentityAcrossModelOrProvider(t *testing.T) {
	stats := NewRequestStatistics()
	timestamp := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	ctx := logging.WithEndpoint(logging.WithRequestID(context.Background(), "same-coordinate"), "POST /v1/responses")
	first := coreusage.Record{
		Provider: "openai", Model: "gpt-5", AuthType: "api_key", AuthIndex: "same-auth",
		RequestedAt: timestamp, Detail: coreusage.Detail{InputTokens: 1, TotalTokens: 1},
	}
	second := first
	second.Provider = "azure-openai"
	second.Model = "gpt-5-deployed"
	second.Detail = coreusage.Detail{InputTokens: 2, TotalTokens: 2}
	if err := stats.RecordWithError(ctx, first); err != nil {
		t.Fatalf("first record: %v", err)
	}
	if err := stats.RecordWithError(ctx, second); err != nil {
		t.Fatalf("second record: %v", err)
	}
	snapshot := stats.ProjectionSnapshot()
	if len(snapshot.Events) != 2 || snapshot.Totals.TotalRequests != 2 {
		t.Fatalf("unseeded records were merged: events=%d totals=%+v", len(snapshot.Events), snapshot.Totals)
	}
	legacy := stats.Snapshot()
	if legacy.TotalRequests != 2 || legacy.TotalTokens != 3 {
		t.Fatalf("legacy aggregate diverged from unseeded projection events: snapshot=%+v", legacy)
	}
}

func TestMutationCoordinatorRekeysTheRequestedAdmissionWhenCoordinatesCollide(t *testing.T) {
	stats := NewRequestStatistics()
	timestamp := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	ctx := logging.WithEndpoint(logging.WithRequestID(context.Background(), "colliding-coordinate"), "POST /v1/responses")
	first := coreusage.Record{
		Provider: "openai", Model: "gpt-5", AuthType: "api_key", AuthIndex: "colliding-auth",
		AdmissionDiscriminator: "reporter:first", RequestedAt: timestamp,
		Detail: coreusage.Detail{InputTokens: 1, TotalTokens: 1},
	}
	second := first
	second.AdmissionDiscriminator = "reporter:second"
	second.Detail = coreusage.Detail{InputTokens: 2, TotalTokens: 2}
	if err := stats.RecordWithError(ctx, first); err != nil {
		t.Fatalf("first record: %v", err)
	}
	if err := stats.RecordWithError(ctx, second); err != nil {
		t.Fatalf("second record: %v", err)
	}
	rekey := first
	rekey.Model = "gpt-5-deployed"
	rekey.Provider = "azure-openai"
	rekey.Detail = coreusage.Detail{InputTokens: 3, TotalTokens: 3}
	if err := stats.RecordWithError(ctx, rekey); err != nil {
		t.Fatalf("rekey record: %v", err)
	}
	legacy := stats.Snapshot()
	if legacy.TotalRequests != 2 || legacy.TotalTokens != 5 {
		t.Fatalf("colliding admissions were merged incorrectly: snapshot=%+v", legacy)
	}
	projection := stats.ProjectionSnapshot()
	if len(projection.Events) != 2 || projection.Totals.Tokens.TotalTokens != 5 {
		t.Fatalf("colliding projection events = %+v", projection)
	}
}

func TestMutationCoordinatorUnseededBatchAllocatesNewIdentityAfterStrictLiveRecords(t *testing.T) {
	stats := NewRequestStatistics()
	timestamp := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	ctx := logging.WithEndpoint(logging.WithRequestID(context.Background(), "batch-after-strict"), "POST /v1/responses")
	first := coreusage.Record{
		Provider: "openai", Model: "gpt-5", AuthType: "api_key", AuthIndex: "batch-after-auth",
		RequestedAt: timestamp, Detail: coreusage.Detail{InputTokens: 1, TotalTokens: 1},
	}
	second := first
	second.Provider = "azure-openai"
	second.Model = "gpt-5-deployed"
	second.Detail = coreusage.Detail{InputTokens: 2, TotalTokens: 2}
	if err := stats.RecordWithError(ctx, first); err != nil {
		t.Fatalf("first strict record: %v", err)
	}
	if err := stats.RecordWithError(ctx, second); err != nil {
		t.Fatalf("second strict record: %v", err)
	}
	batchDetail := RequestDetail{
		RequestID: "batch-after-strict", Timestamp: timestamp, Endpoint: "POST /v1/responses",
		Model: "gpt-5", Provider: "openai", AuthType: "api_key", AuthIndex: "batch-after-auth",
		Tokens: RequestTokenStats{InputTokens: 3, TotalTokens: 3},
	}
	before := stats.ProjectionSnapshot()
	identity, identityOK := stats.coordinator.existingIdentity("POST /v1/responses", batchDetail)
	if !identityOK {
		t.Fatal("exact logical identity was not found before unseeded import")
	}
	foundExact := false
	for _, event := range before.Events {
		if event.Model == "gpt-5" && event.Provider == "openai" {
			foundExact = true
			if identity.StableEventID != event.StableEventID {
				t.Fatalf("logical lookup selected the wrong strict event: identity=%+v event=%+v", identity, event)
			}
		}
	}
	if !foundExact {
		t.Fatalf("strict projection did not contain the expected openai/gpt-5 event: %+v", before.Events)
	}
	result, err := stats.MergeSnapshotWithError(StatisticsSnapshot{APIs: map[string]APISnapshot{
		"POST /v1/responses": {Models: map[string]ModelSnapshot{"gpt-5": {Details: []RequestDetail{batchDetail}}}},
	}})
	if err != nil {
		t.Fatalf("batch merge: %v", err)
	}
	if result.Enriched != 0 || result.Added != 1 {
		t.Fatalf("batch result = %+v, want one new admission", result)
	}
	legacy := stats.Snapshot()
	if legacy.TotalRequests != 3 || legacy.TotalTokens != 6 {
		t.Fatalf("unseeded batch changed existing strict events: snapshot=%+v", legacy)
	}
	if events := stats.ProjectionSnapshot().Events; len(events) != 3 || stats.ProjectionSnapshot().Totals.Tokens.TotalTokens != 6 {
		t.Fatalf("batch projection diverged: events=%d snapshot=%+v", len(events), stats.ProjectionSnapshot())
	}
}

func TestMutationCoordinatorUnseededBatchSameLogicalDetailAllocatesNewIdentityAcrossBatches(t *testing.T) {
	stats := NewRequestStatistics()
	detail := RequestDetail{
		RequestID: "unseeded-cross-batch", Timestamp: time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC),
		Endpoint: "POST /v1/responses", Model: "gpt-5", Provider: "openai", AuthType: "api_key", AuthIndex: "cross-batch-same-logical",
		Tokens: RequestTokenStats{InputTokens: 1, TotalTokens: 1},
	}
	merge := func(tokens int64) MergeResult {
		detail.Tokens = RequestTokenStats{InputTokens: tokens, TotalTokens: tokens}
		result, err := stats.MergeSnapshotWithError(StatisticsSnapshot{APIs: map[string]APISnapshot{
			"POST /v1/responses": {Models: map[string]ModelSnapshot{"gpt-5": {Details: []RequestDetail{detail}}}},
		}})
		if err != nil {
			t.Fatalf("merge tokens=%d: %v", tokens, err)
		}
		return result
	}
	if result := merge(1); result.Added != 1 || result.Enriched != 0 {
		t.Fatalf("first batch result = %+v, want one addition", result)
	}
	if result := merge(2); result.Added != 1 || result.Enriched != 0 {
		t.Fatalf("second batch result = %+v, want a new admission", result)
	}
	snapshot := stats.ProjectionSnapshot()
	if len(snapshot.Events) != 2 || snapshot.Totals.TotalRequests != 2 || snapshot.Totals.Tokens.TotalTokens != 3 {
		t.Fatalf("same logical unseeded imports were merged: events=%d totals=%+v", len(snapshot.Events), snapshot.Totals)
	}
}

func TestMutationCoordinatorAppliesLiveIntentsInSequenceOrder(t *testing.T) {
	stats := NewRequestStatistics()
	coordinator := stats.coordinator
	base := RequestDetail{
		RequestID: "ordered-enrichment", Timestamp: time.Date(2026, 8, 1, 1, 0, 0, 0, time.UTC),
		Endpoint: "POST /v1/responses", Model: "gpt-5", Provider: "openai", AuthType: "api_key", AuthIndex: "ordered-auth",
		Tokens: RequestTokenStats{InputTokens: 1, OutputTokens: 1, ReasoningTokens: 2, TotalTokens: 4, ReasoningCostMode: ReasoningCostIncludedInOutput},
	}
	firstIntent, firstIdentity, err := coordinator.admit(base, base.Endpoint, MutationOperationRecord, "", 0, 0, coreusage.Record{}, true, 0)
	if err != nil {
		t.Fatalf("admit first ordered intent: %v", err)
	}
	second := base
	second.Provider = "azure-openai"
	secondIntent, secondIdentity, err := coordinator.admit(second, second.Endpoint, MutationOperationRecord, "", 0, 0, coreusage.Record{}, true, 0)
	if err != nil {
		t.Fatalf("admit second ordered intent: %v", err)
	}

	secondDone := make(chan error, 1)
	go func() {
		_, applyErr := coordinator.applyIntent(secondIntent.APIName, secondIntent, secondIdentity)
		secondDone <- applyErr
	}()
	select {
	case err := <-secondDone:
		t.Fatalf("second intent applied before first sequence: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	if _, err := coordinator.applyIntent(firstIntent.APIName, firstIntent, firstIdentity); err != nil {
		t.Fatalf("apply first ordered intent: %v", err)
	}
	if err := coordinator.markTerminal(firstIntent, MutationTerminalCommitted, nil); err != nil {
		t.Fatalf("terminal first ordered intent: %v", err)
	}
	select {
	case err := <-secondDone:
		if err != nil {
			t.Fatalf("apply second ordered intent: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("second ordered intent did not apply after first sequence")
	}
	if err := coordinator.markTerminal(secondIntent, MutationTerminalCommitted, nil); err != nil {
		t.Fatalf("terminal second ordered intent: %v", err)
	}

	snapshot := stats.ProjectionSnapshot()
	var pricing PricingAggregate
	var ok bool
	for _, candidate := range snapshot.Totals.PricingGroups {
		if candidate.PriceKey == BuildPriceKey("azure-openai", "gpt-5") {
			pricing = candidate
			ok = true
			break
		}
	}
	if !ok {
		t.Fatalf("ordered pricing group missing: %+v", snapshot.Totals.PricingGroups)
	}
	if pricing.Provider != "azure-openai" {
		t.Fatalf("sequence-order enrichment was not retained: pricing=%+v snapshot=%+v", pricing, stats.Snapshot())
	}
}

func TestMutationCoordinatorRejectsOversizedIntentBeforeSequence(t *testing.T) {
	stats := NewRequestStatistics()
	stats.coordinator = NewMutationCoordinatorWithConfig(stats, MutationCoordinatorConfig{
		JournalEntryLimit:        2,
		JournalBudgetBytes:       512,
		MaxSerializedIntentBytes: 512,
		GateQueueLimit:           2,
	})
	model := strings.Repeat("m", 2048)
	err := stats.RecordWithError(context.Background(), coreusage.Record{Provider: "openai", Model: model})
	if !errors.Is(err, ErrMutationJournalBudget) {
		t.Fatalf("oversized intent error = %v, want ErrMutationJournalBudget", err)
	}
	if head := stats.coordinator.Journal().Head(); head != 0 {
		t.Fatalf("oversized intent allocated sequence %d", head)
	}
}

func TestMutationCoordinatorCompactsTerminalJournalBeforeAdmission(t *testing.T) {
	stats := NewRequestStatistics()
	stats.coordinator = NewMutationCoordinatorWithConfig(stats, MutationCoordinatorConfig{
		JournalEntryLimit:        2,
		JournalBudgetBytes:       2 * 4096,
		MaxSerializedIntentBytes: 4096,
		GateQueueLimit:           2,
	})
	for index := 0; index < 3; index++ {
		err := stats.RecordWithError(context.Background(), coreusage.Record{
			Provider:    "openai",
			Model:       fmt.Sprintf("gpt-5-%d", index),
			AuthIndex:   "compact-auth",
			RequestedAt: time.Date(2026, 8, 1, 12, 0, index, 0, time.UTC),
			Detail:      coreusage.Detail{InputTokens: 1, TotalTokens: 1},
		})
		if err != nil {
			t.Fatalf("record %d: %v", index, err)
		}
	}
	if got := stats.Snapshot().TotalRequests; got != 3 {
		t.Fatalf("snapshot requests = %d, want three records after compaction", got)
	}
	if got := stats.coordinator.Journal().Head(); got != 3 {
		t.Fatalf("journal head = %d, want monotonic head 3", got)
	}
	usedBytes, _, _, _ := stats.coordinator.Journal().BudgetStats()
	if usedBytes > 2*4096 {
		t.Fatalf("journal used bytes = %d, exceeded configured budget", usedBytes)
	}
}

func TestMutationCoordinatorBoundsAdmissionIdentityCache(t *testing.T) {
	stats := NewRequestStatistics()
	stats.coordinator = NewMutationCoordinatorWithConfig(stats, MutationCoordinatorConfig{
		JournalEntryLimit:           8,
		JournalBudgetBytes:          8 * 4096,
		MaxSerializedIntentBytes:    4096,
		AdmissionIdentityCacheLimit: 2,
	})
	for index := 0; index < 6; index++ {
		ctx := logging.WithRequestID(context.Background(), fmt.Sprintf("cache-bound-%d", index))
		ctx = logging.WithEndpoint(ctx, "POST /v1/responses")
		err := stats.RecordWithError(ctx, coreusage.Record{
			Provider:               "openai",
			Model:                  "gpt-5",
			AuthIndex:              "cache-bound-auth",
			AdmissionDiscriminator: fmt.Sprintf("reporter:%d", index),
			RequestedAt:            time.Date(2026, 8, 1, 12, 0, index, 0, time.UTC),
			Detail:                 coreusage.Detail{InputTokens: 1, TotalTokens: 1},
		})
		if err != nil {
			t.Fatalf("record %d: %v", index, err)
		}
	}
	stats.coordinator.mu.Lock()
	cacheSize := len(stats.coordinator.identityByAdmission)
	stats.coordinator.mu.Unlock()
	if cacheSize > 2 {
		t.Fatalf("admission identity cache size = %d, want at most 2", cacheSize)
	}
	if got := stats.Snapshot().TotalRequests; got != 6 {
		t.Fatalf("snapshot requests = %d, want six records", got)
	}
}

func TestMutationCoordinatorCarriesIdentityAcrossProviderAndModelEnrichment(t *testing.T) {
	stats := NewRequestStatistics()
	timestamp := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	ctx := logging.WithEndpoint(logging.WithRequestID(context.Background(), "rekey-request"), "POST /v1/responses")
	if err := stats.RecordWithError(ctx, coreusage.Record{
		Provider:               "openai",
		Model:                  "gpt-5",
		AuthType:               "api_key",
		AuthIndex:              "auth-rekey",
		AdmissionDiscriminator: "reporter:rekey",
		RequestedAt:            timestamp,
		Detail:                 coreusage.Detail{InputTokens: 1, TotalTokens: 1},
	}); err != nil {
		t.Fatalf("initial record: %v", err)
	}
	if err := stats.RecordWithError(ctx, coreusage.Record{
		Provider:               "azure-openai",
		Model:                  "gpt-5-deployed",
		AuthType:               "api_key",
		AuthIndex:              "auth-rekey",
		AdmissionDiscriminator: "reporter:rekey",
		RequestedAt:            timestamp,
		Detail:                 coreusage.Detail{InputTokens: 10, OutputTokens: 5, TotalTokens: 15},
	}); err != nil {
		t.Fatalf("enrichment record: %v", err)
	}
	snapshot := stats.ProjectionSnapshot()
	if len(snapshot.Events) != 1 || snapshot.Totals.TotalRequests != 1 || snapshot.Totals.Tokens.TotalTokens != 15 {
		t.Fatalf("provider/model rekey split event: events=%d totals=%+v", len(snapshot.Events), snapshot.Totals)
	}
	legacy := stats.Snapshot()
	apiSnapshot := legacy.APIs["POST /v1/responses"]
	if _, oldModelStillPresent := apiSnapshot.Models["gpt-5"]; oldModelStillPresent {
		t.Fatalf("old model bucket survived provider/model rekey: %+v", apiSnapshot.Models)
	}
	modelSnapshot, ok := apiSnapshot.Models["gpt-5-deployed"]
	if !ok || modelSnapshot.TotalRequests != 1 || modelSnapshot.TotalTokens != 15 || len(modelSnapshot.Details) != 1 {
		t.Fatalf("rekeyed model bucket = %+v, want one detail with 15 tokens", apiSnapshot.Models)
	}
	if modelSnapshot.Details[0].Model != "gpt-5-deployed" || modelSnapshot.Details[0].Provider != "azure-openai" {
		t.Fatalf("rekeyed detail = %+v, want new model/provider", modelSnapshot.Details[0])
	}
}

func TestMutationCoordinatorDoesNotMergeSameRequestIDAcrossAuthScopes(t *testing.T) {
	stats := NewRequestStatistics()
	timestamp := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	ctx := logging.WithRequestID(logging.WithEndpoint(context.Background(), "POST /v1/responses"), "shared-request-id")
	first := coreusage.Record{
		Provider: "openai", Model: "gpt-5", AuthType: "api_key", AuthIndex: "auth-one", RequestedAt: timestamp,
		Detail: coreusage.Detail{InputTokens: 1, TotalTokens: 1},
	}
	second := first
	second.AuthIndex = "auth-two"
	second.Detail = coreusage.Detail{InputTokens: 2, TotalTokens: 2}
	if err := stats.RecordWithError(ctx, first); err != nil {
		t.Fatalf("first scoped record: %v", err)
	}
	if err := stats.RecordWithError(ctx, second); err != nil {
		t.Fatalf("second scoped record: %v", err)
	}
	snapshot := stats.ProjectionSnapshot()
	if len(snapshot.Events) != 2 || snapshot.Totals.TotalRequests != 2 {
		t.Fatalf("same request id crossed auth scopes: events=%d totals=%+v", len(snapshot.Events), snapshot.Totals)
	}
}

func TestCanonicalIdentitySeedZeroTimestampIsStable(t *testing.T) {
	detail := RequestDetail{RequestID: "zero-time", Endpoint: "POST /v1/responses", Model: "gpt-5", Provider: "openai"}
	first := CanonicalIdentitySeedV1(detail, 7)
	second := CanonicalIdentitySeedV1(detail, 7)
	if first == "" || first != second {
		t.Fatalf("zero timestamp identity seeds differ: %q/%q", first, second)
	}
	firstSort := CanonicalSortKeyV1("api", detail, 2)
	secondSort := CanonicalSortKeyV1("api", detail, 2)
	if string(firstSort) != string(secondSort) {
		t.Fatal("zero timestamp sort keys differ")
	}
}

func TestMutationCoordinatorReusesIdentityAcrossMutableProviderAndModel(t *testing.T) {
	stats := NewRequestStatistics()
	timestamp := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	ctx := logging.WithRequestID(context.Background(), "req-mutable-enrichment")
	ctx = logging.WithEndpoint(ctx, "POST /v1/responses")
	first := coreusage.Record{Provider: "openai", Model: "gpt-5", AuthIndex: "auth-mutable", AdmissionDiscriminator: "reporter:mutable", RequestedAt: timestamp, Detail: coreusage.Detail{InputTokens: 1, TotalTokens: 1}}
	if err := stats.RecordWithError(ctx, first); err != nil {
		t.Fatalf("first record: %v", err)
	}
	second := first
	second.Provider = "openai-compatible"
	second.Model = "gpt-5-mini"
	second.Detail = coreusage.Detail{InputTokens: 10, TotalTokens: 10}
	if err := stats.RecordWithError(ctx, second); err != nil {
		t.Fatalf("enrichment record: %v", err)
	}
	snapshot := stats.ProjectionSnapshot()
	if snapshot.Totals.TotalRequests != 1 || snapshot.Totals.Tokens.TotalTokens != 10 || len(snapshot.Events) != 1 {
		t.Fatalf("mutable enrichment allocated a second event: totals=%+v events=%d", snapshot.Totals, len(snapshot.Events))
	}
}

func TestMutationCoordinatorFinalizeRebuildHandlesNilContext(t *testing.T) {
	coordinator := NewMutationCoordinator(NewRequestStatistics())
	candidate, err := coordinator.BeginRebuild(nil)
	if err != nil {
		t.Fatalf("begin rebuild: %v", err)
	}
	if err := coordinator.FinalizeRebuild(nil, candidate); err != nil {
		t.Fatalf("finalize rebuild with nil context: %v", err)
	}
	if coordinator.RebuildActive() {
		t.Fatal("rebuild gate remained active after finalize")
	}
}

func TestMutationCoordinatorGateCancellationDoesNotAdmit(t *testing.T) {
	coordinator := NewMutationCoordinatorWithConfig(NewRequestStatistics(), MutationCoordinatorConfig{GateQueueLimit: 1})
	candidate, err := coordinator.BeginRebuild(context.Background())
	if err != nil {
		t.Fatalf("begin rebuild: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = coordinator.ApplyRecord(ctx, coreusage.Record{Provider: "openai", Model: "gpt-5"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled admission = %v, want context.Canceled", err)
	}
	if coordinator.Journal().Head() != 0 {
		t.Fatalf("cancelled admission allocated sequence: head=%d", coordinator.Journal().Head())
	}
	if err := coordinator.FinalizeRebuild(context.Background(), candidate); err != nil {
		t.Fatalf("finalize rebuild: %v", err)
	}
}

func TestMutationCoordinatorFinalizeWaitsForInFlightIntent(t *testing.T) {
	coordinator := NewMutationCoordinator(NewRequestStatistics())
	coordinator.beginInFlight()
	candidate, err := coordinator.BeginRebuild(context.Background())
	if err != nil {
		coordinator.endInFlight()
		t.Fatalf("begin rebuild: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- coordinator.FinalizeRebuild(context.Background(), candidate) }()
	select {
	case err := <-done:
		t.Fatalf("finalize returned before in-flight intent terminalized: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	coordinator.endInFlight()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("finalize after in-flight terminal: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("finalize did not complete after in-flight intent terminalized")
	}
}

func TestMutationCoordinatorFinalizeRecordsCASRebase(t *testing.T) {
	stats := NewRequestStatistics()
	coordinator := NewMutationCoordinator(stats)
	candidate, err := coordinator.BeginRebuild(context.Background())
	if err != nil {
		t.Fatalf("begin rebuild: %v", err)
	}
	detail := normalizeRequestDetail(RequestDetail{
		Timestamp: time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC),
		Endpoint:  "POST /v1/responses",
		Model:     "gpt-5",
		Provider:  "openai",
		AuthType:  "api_key",
		AuthIndex: "auth-rebase",
		Tokens:    RequestTokenStats{InputTokens: 1, TotalTokens: 1},
	}, "openai")
	intent, identity, err := coordinator.admit(detail, "POST /v1/responses", MutationOperationRecord, "", 0, 0, coreusage.Record{}, false, 0)
	if err != nil {
		coordinator.abortRebuild(candidate)
		t.Fatalf("admit suffix intent: %v", err)
	}
	if _, err := coordinator.applyIntent("POST /v1/responses", intent, identity); err != nil {
		coordinator.abortRebuild(candidate)
		t.Fatalf("apply suffix intent: %v", err)
	}
	if err := coordinator.markTerminal(intent, MutationTerminalCommitted, nil); err != nil {
		coordinator.abortRebuild(candidate)
		t.Fatalf("mark suffix terminal: %v", err)
	}
	if err := coordinator.FinalizeRebuild(context.Background(), candidate); err != nil {
		t.Fatalf("finalize rebase: %v", err)
	}
	if candidate.CASRetryCount != 1 || !candidate.Rebased || candidate.SuffixReplayCount != 1 {
		t.Fatalf("candidate CAS metadata = %+v, want one bounded rebase with one suffix", candidate)
	}
	if events := stats.ProjectionSnapshot().Events; len(events) != 1 || events[0].Sequence != intent.Sequence {
		t.Fatalf("finalized projection events = %+v, want the replayed intent committed", events)
	}
}

func TestMutationCoordinatorProjectionCaptureKeepsSuffixDuringJournalCompaction(t *testing.T) {
	stats := NewRequestStatistics()
	stats.coordinator = NewMutationCoordinatorWithConfig(stats, MutationCoordinatorConfig{
		JournalEntryLimit:        3,
		JournalBudgetBytes:       3 * 4096,
		MaxSerializedIntentBytes: 4096,
		GateQueueLimit:           3,
		MaxFinalizeReplayIntents: 2,
	})
	coordinator := stats.coordinator
	applyRecord := func(index int) {
		t.Helper()
		record := coreusage.Record{
			Provider:               "openai",
			Model:                  "gpt-5",
			APIKey:                 fmt.Sprintf("capture-journal-%d", index),
			AdmissionDiscriminator: fmt.Sprintf("capture-journal-%d", index),
			RequestedAt:            time.Date(2026, 8, 9, 22, 0, index, 0, time.UTC),
			Detail:                 coreusage.Detail{InputTokens: 1, TotalTokens: 1},
		}
		if err := coordinator.ApplyRecord(context.Background(), record); err != nil {
			t.Fatalf("apply record %d: %v", index, err)
		}
	}

	applyRecord(0)
	applyRecord(1)
	session, err := coordinator.beginProjectionCapture(context.Background())
	if err != nil {
		t.Fatalf("begin projection capture: %v", err)
	}
	defer session.finish()
	baseHead := coordinator.journal.Head()
	if baseHead != 2 {
		t.Fatalf("base journal head = %d, want 2", baseHead)
	}
	if err := session.setBaseJournalHead(baseHead); err != nil {
		t.Fatalf("set capture base journal head: %v", err)
	}
	session.releaseFence()

	applyRecord(2)
	applyRecord(3)
	suffix := coordinator.journal.EntriesAfter(baseHead, false)
	if len(suffix) != 2 || suffix[0].Sequence != 3 || suffix[1].Sequence != 4 {
		t.Fatalf("capture journal suffix = %+v, want sequences 3 and 4", suffix)
	}
	retained := coordinator.journal.EntriesAfter(0, false)
	if len(retained) != 2 {
		t.Fatalf("retained journal entries = %d, want only the two capture suffix intents", len(retained))
	}
}

func TestMutationCoordinatorBatchGateCountsIntentSlots(t *testing.T) {
	stats := NewRequestStatistics()
	stats.coordinator = NewMutationCoordinatorWithConfig(stats, MutationCoordinatorConfig{
		JournalEntryLimit:        4,
		JournalBudgetBytes:       4 * 1024,
		MaxSerializedIntentBytes: 1024,
		GateQueueLimit:           1,
	})
	candidate, err := stats.coordinator.BeginRebuild(context.Background())
	if err != nil {
		t.Fatalf("begin rebuild: %v", err)
	}
	snapshot := StatisticsSnapshot{APIs: map[string]APISnapshot{
		"api": {Models: map[string]ModelSnapshot{
			"model": {Details: []RequestDetail{
				{RequestID: "batch-1", Timestamp: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), Model: "model", Provider: "openai"},
				{RequestID: "batch-2", Timestamp: time.Date(2026, 8, 1, 0, 1, 0, 0, time.UTC), Model: "model", Provider: "openai"},
			}},
		}},
	}}
	release, _, err := stats.coordinator.enterBatch(context.Background(), len(sortedSnapshotDetails(snapshot)))
	if release != nil {
		defer release()
	}
	if !errors.Is(err, ErrMutationJournalFull) {
		t.Fatalf("batch gate error = %v, want ErrMutationJournalFull", err)
	}
	if got := stats.coordinator.Journal().Head(); got != 0 {
		t.Fatalf("batch gate allocated sequence = %d, want zero", got)
	}
	if err := stats.coordinator.FinalizeRebuild(context.Background(), candidate); err != nil {
		t.Fatalf("finalize after rejected batch: %v", err)
	}
}

func TestMutationCoordinatorRejectsOversizedBatchWithoutWaitingForInFlight(t *testing.T) {
	stats := NewRequestStatistics()
	stats.coordinator = NewMutationCoordinatorWithConfig(stats, MutationCoordinatorConfig{
		JournalEntryLimit:        1,
		JournalBudgetBytes:       16384,
		MaxSerializedIntentBytes: 16384,
		GateQueueLimit:           1,
	})
	coordinator := stats.coordinator
	coordinator.applyMu.Lock()
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- stats.RecordWithError(context.Background(), coreusage.Record{
			Provider:    "openai",
			Model:       "gpt-5",
			RequestedAt: time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC),
			Detail:      coreusage.Detail{InputTokens: 1, TotalTokens: 1},
		})
	}()
	waitForJournalHead(t, coordinator, 1)

	snapshot := StatisticsSnapshot{APIs: map[string]APISnapshot{
		"api": {Models: map[string]ModelSnapshot{
			"model": {Details: []RequestDetail{
				{RequestID: "oversized-batch-1", Timestamp: time.Date(2026, 8, 1, 12, 1, 0, 0, time.UTC), Model: "model", Provider: "openai"},
				{RequestID: "oversized-batch-2", Timestamp: time.Date(2026, 8, 1, 12, 2, 0, 0, time.UTC), Model: "model", Provider: "openai"},
			}},
		}},
	}}
	secondDone := make(chan error, 1)
	go func() {
		_, err := coordinator.ApplySnapshot(context.Background(), snapshot)
		secondDone <- err
	}()

	var secondErr error
	timedOut := false
	select {
	case secondErr = <-secondDone:
	case <-time.After(500 * time.Millisecond):
		timedOut = true
	}
	coordinator.applyMu.Unlock()
	if err := <-firstDone; err != nil {
		t.Fatalf("first record: %v", err)
	}
	if timedOut {
		secondErr = <-secondDone
		t.Fatal("oversized batch waited for in-flight work instead of failing immediately")
	}
	if !errors.Is(secondErr, ErrMutationJournalFull) {
		t.Fatalf("oversized batch error = %v, want ErrMutationJournalFull", secondErr)
	}
}

func TestMutationCoordinatorRejectsConcurrentSnapshotBatch(t *testing.T) {
	stats := NewRequestStatistics()
	stats.coordinator = NewMutationCoordinatorWithConfig(stats, MutationCoordinatorConfig{
		JournalEntryLimit:        4,
		JournalBudgetBytes:       4 * 16384,
		MaxSerializedIntentBytes: 16384,
		GateQueueLimit:           4,
	})
	coordinator := stats.coordinator
	coordinator.applyMu.Lock()
	snapshot := StatisticsSnapshot{APIs: map[string]APISnapshot{
		"api": {Models: map[string]ModelSnapshot{
			"model": {Details: []RequestDetail{{
				RequestID: "exclusive-import", Timestamp: time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC), Model: "model", Provider: "openai",
			}}},
		}},
	}}
	firstDone := make(chan error, 1)
	go func() {
		_, err := coordinator.ApplySnapshot(context.Background(), snapshot)
		firstDone <- err
	}()
	waitForJournalHead(t, coordinator, 1)

	secondDone := make(chan error, 1)
	go func() {
		_, err := coordinator.ApplySnapshot(context.Background(), snapshot)
		secondDone <- err
	}()
	select {
	case err := <-secondDone:
		if !errors.Is(err, ErrProjectionRebuildInProgress) {
			t.Fatalf("concurrent snapshot error = %v, want ErrProjectionRebuildInProgress", err)
		}
	case <-time.After(500 * time.Millisecond):
		coordinator.applyMu.Unlock()
		<-firstDone
		t.Fatal("concurrent snapshot waited instead of failing fast")
	}
	coordinator.applyMu.Unlock()
	if err := <-firstDone; err != nil {
		t.Fatalf("first snapshot: %v", err)
	}
}

func TestMutationCoordinatorWaitsForPendingJournalBeforeAdmission(t *testing.T) {
	stats := NewRequestStatistics()
	stats.coordinator = NewMutationCoordinatorWithConfig(stats, MutationCoordinatorConfig{
		JournalEntryLimit:        1,
		JournalBudgetBytes:       16384,
		MaxSerializedIntentBytes: 16384,
		GateQueueLimit:           1,
	})
	coordinator := stats.coordinator
	coordinator.applyMu.Lock()
	baseContext := logging.WithRequestID(context.Background(), "pending-journal")
	first := coreusage.Record{
		Provider: "openai", Model: "gpt-5", AuthIndex: "pending-journal-auth",
		AdmissionDiscriminator: "pending-journal-event",
		RequestedAt:            time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC),
		Detail:                 coreusage.Detail{InputTokens: 1, TotalTokens: 1},
	}
	firstDone := make(chan error, 1)
	go func() { firstDone <- stats.RecordWithError(baseContext, first) }()
	waitForJournalHead(t, coordinator, 1)

	second := first
	second.Detail = coreusage.Detail{InputTokens: 2, TotalTokens: 2}
	secondDone := make(chan error, 1)
	go func() { secondDone <- stats.RecordWithError(baseContext, second) }()
	select {
	case err := <-secondDone:
		t.Fatalf("second record completed while first intent was pending: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	coordinator.applyMu.Unlock()

	if err := <-firstDone; err != nil {
		t.Fatalf("first record: %v", err)
	}
	if err := <-secondDone; err != nil {
		t.Fatalf("second record: %v", err)
	}
	snapshot := stats.Snapshot()
	if snapshot.TotalRequests != 1 || snapshot.TotalTokens != 2 {
		t.Fatalf("pending journal snapshot = requests:%d tokens:%d, want 1/2", snapshot.TotalRequests, snapshot.TotalTokens)
	}
	if events := stats.ProjectionSnapshot().Events; len(events) != 1 {
		t.Fatalf("pending journal projection events = %d, want one", len(events))
	}
	if available, _ := stats.ProjectionAvailability(); !available {
		t.Fatal("pending journal admission incorrectly used canonical fallback")
	}
}

func TestMutationCoordinatorFinalizeMakesBoundedRecordLoadProgress(t *testing.T) {
	stats := NewRequestStatistics()
	candidate, err := stats.coordinator.BeginRebuild(context.Background())
	if err != nil {
		t.Fatalf("begin rebuild: %v", err)
	}
	const recordCount = 16
	errorsCh := make(chan error, recordCount)
	var workers sync.WaitGroup
	workers.Add(recordCount)
	for index := 0; index < recordCount; index++ {
		index := index
		go func() {
			defer workers.Done()
			recordContext := logging.WithRequestID(context.Background(), fmt.Sprintf("progress-%d", index))
			errorsCh <- stats.RecordWithError(recordContext, coreusage.Record{
				Provider:    "openai",
				Model:       "gpt-5",
				AuthIndex:   "progress-auth",
				RequestedAt: time.Date(2026, 8, 1, 12, 0, index, 0, time.UTC),
				Detail:      coreusage.Detail{InputTokens: 1, TotalTokens: 1},
			})
		}()
	}
	finalizeDone := make(chan error, 1)
	go func() { finalizeDone <- stats.coordinator.FinalizeRebuild(context.Background(), candidate) }()
	select {
	case err := <-finalizeDone:
		if err != nil {
			t.Fatalf("finalize under record load: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("finalize did not make bounded record load progress")
	}
	workers.Wait()
	close(errorsCh)
	for recordErr := range errorsCh {
		if recordErr != nil {
			t.Fatalf("record under finalize gate: %v", recordErr)
		}
	}
	if got := stats.Snapshot().TotalRequests; got != recordCount {
		t.Fatalf("record count after finalize = %d, want %d", got, recordCount)
	}
}

func TestMutationCoordinatorSnapshotAdmissionSortIsDeterministic(t *testing.T) {
	snapshot := StatisticsSnapshot{APIs: map[string]APISnapshot{
		"api-b": {Models: map[string]ModelSnapshot{
			"model-z": {Details: []RequestDetail{{RequestID: "req-z", Timestamp: time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC), Provider: "openai", Model: "model-z", Tokens: RequestTokenStats{InputTokens: 2, TotalTokens: 2}}}},
		}},
		"api-a": {Models: map[string]ModelSnapshot{
			"model-a": {Details: []RequestDetail{{RequestID: "req-a", Timestamp: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), Provider: "openai", Model: "model-a", Tokens: RequestTokenStats{InputTokens: 1, TotalTokens: 1}}}},
		}},
	}}
	left := NewRequestStatistics()
	right := NewRequestStatistics()
	if _, err := left.coordinator.ApplySnapshot(context.Background(), snapshot); err != nil {
		t.Fatalf("left snapshot apply: %v", err)
	}
	if _, err := right.coordinator.ApplySnapshot(context.Background(), snapshot); err != nil {
		t.Fatalf("right snapshot apply: %v", err)
	}
	if left.coordinator.Journal().PendingCount() != 0 || right.coordinator.Journal().PendingCount() != 0 {
		t.Fatalf("snapshot import left non-terminal intents: pending=%d/%d", left.coordinator.Journal().PendingCount(), right.coordinator.Journal().PendingCount())
	}
	leftEvents := left.ProjectionSnapshot().Events
	rightEvents := right.ProjectionSnapshot().Events
	if len(leftEvents) != len(rightEvents) || len(leftEvents) != 2 {
		t.Fatalf("event lengths = %d/%d, want two", len(leftEvents), len(rightEvents))
	}
	for index := range leftEvents {
		if leftEvents[index].StableEventID != rightEvents[index].StableEventID || leftEvents[index].Sequence != rightEvents[index].Sequence {
			t.Fatalf("event[%d] differs across identical imports: left=%+v right=%+v", index, leftEvents[index], rightEvents[index])
		}
	}
}

func TestMutationCoordinatorImportJournalFailureDoesNotPartiallyCommit(t *testing.T) {
	stats := NewRequestStatistics()
	stats.coordinator = NewMutationCoordinatorWithConfig(stats, MutationCoordinatorConfig{
		JournalEntryLimit:        1,
		JournalBudgetBytes:       4096,
		MaxSerializedIntentBytes: 1024,
		GateQueueLimit:           1,
	})
	snapshot := StatisticsSnapshot{APIs: map[string]APISnapshot{
		"api": {Models: map[string]ModelSnapshot{
			"model": {Details: []RequestDetail{
				{RequestID: "import-1", Timestamp: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), Model: "model", Provider: "openai"},
				{RequestID: "import-2", Timestamp: time.Date(2026, 8, 1, 0, 1, 0, 0, time.UTC), Model: "model", Provider: "openai"},
			}},
		}},
	}}
	result, err := stats.MergeSnapshotWithError(snapshot)
	if !errors.Is(err, ErrMutationJournalFull) {
		t.Fatalf("import error = %v, want ErrMutationJournalFull", err)
	}
	if result.Added != 0 || result.Enriched != 0 || result.Skipped != 0 {
		t.Fatalf("failed import result = %+v, want zero result", result)
	}
	if got := stats.Snapshot().TotalRequests; got != 0 {
		t.Fatalf("failed import changed total requests = %d, want zero", got)
	}
	if got := stats.coordinator.Journal().Head(); got != 0 {
		t.Fatalf("failed import allocated journal sequence = %d, want zero", got)
	}
}

func TestMutationCoordinatorBulkSnapshotExceedsJournalLimit(t *testing.T) {
	stats := NewRequestStatistics()
	stats.coordinator = NewMutationCoordinatorWithConfig(stats, MutationCoordinatorConfig{
		JournalEntryLimit:        4,
		JournalBudgetBytes:       4 * 4096,
		MaxSerializedIntentBytes: 4096,
		MaxBulkChunkIntents:      2,
		MaxBulkChunkBytes:        16 * 1024,
		GateQueueLimit:           2,
	})
	const detailCount = 10
	snapshot := bulkTestSnapshot(detailCount)
	result, err := stats.MergeSnapshotWithError(snapshot)
	if err != nil {
		t.Fatalf("bulk snapshot error: %v", err)
	}
	if result.Added != detailCount || result.Enriched != 0 || result.Skipped != 0 {
		t.Fatalf("bulk result = %+v, want added=%d", result, detailCount)
	}
	if got := stats.Snapshot().TotalRequests; got != detailCount {
		t.Fatalf("bulk total requests = %d, want %d", got, detailCount)
	}
	if got := len(stats.ProjectionSnapshot().Events); got != detailCount {
		t.Fatalf("bulk projection events = %d, want %d", got, detailCount)
	}
	if pending := stats.coordinator.Journal().PendingCount(); pending != 0 {
		t.Fatalf("bulk left %d pending journal intents", pending)
	}
	marker, ok := stats.coordinator.Journal().TerminalBulkTransactionSnapshot()
	if !ok || marker.TerminalState != MutationTerminalCommitted || marker.FrontierOrdinal != detailCount || marker.ChunkCount != 5 || !isMutationSHA256(marker.ManifestSHA256) {
		t.Fatalf("bulk terminal marker = %+v/%t", marker, ok)
	}
}

func TestMutationCoordinatorBulkSnapshotTenThousandRecords(t *testing.T) {
	stats := NewRequestStatistics()
	result, err := stats.MergeSnapshotWithError(bulkTestSnapshot(10_000))
	if err != nil {
		t.Fatalf("10k bulk snapshot error: %v", err)
	}
	if result.Added != 10_000 || stats.Snapshot().TotalRequests != 10_000 {
		t.Fatalf("10k bulk result = %+v total=%d", result, stats.Snapshot().TotalRequests)
	}
	if pending := stats.coordinator.Journal().PendingCount(); pending != 0 {
		t.Fatalf("10k bulk left %d pending journal intents", pending)
	}
}

func TestMutationCoordinatorBulkSnapshotFailureKeepsLiveState(t *testing.T) {
	stats := NewRequestStatistics()
	stats.coordinator = NewMutationCoordinatorWithConfig(stats, MutationCoordinatorConfig{
		JournalEntryLimit:        4,
		JournalBudgetBytes:       4 * 4096,
		MaxSerializedIntentBytes: 4096,
		MaxBulkChunkIntents:      2,
		MaxBulkChunkBytes:        16 * 1024,
		GateQueueLimit:           2,
	})
	snapshot := bulkTestSnapshot(4)
	items := sortedSnapshotDetails(snapshot)
	// Force an admission-budget failure in the second chunk after the first
	// chunk has already been staged and terminalized.
	items[2].detail.RequestID = strings.Repeat("oversized-request-id-", 512)
	snapshot = snapshotFromMutationItems(items)
	result, err := stats.MergeSnapshotWithError(snapshot)
	if !errors.Is(err, ErrMutationJournalBudget) {
		t.Fatalf("bulk failure = %v, want ErrMutationJournalBudget", err)
	}
	if result != (MergeResult{}) {
		t.Fatalf("failed bulk result = %+v, want zero", result)
	}
	if got := stats.Snapshot().TotalRequests; got != 0 {
		t.Fatalf("failed bulk changed live total requests = %d, want zero", got)
	}
	if got := len(stats.ProjectionSnapshot().Events); got != 0 {
		t.Fatalf("failed bulk published %d projection events", got)
	}
	if pending := stats.coordinator.Journal().PendingCount(); pending != 0 {
		t.Fatalf("failed bulk left %d pending journal intents", pending)
	}
	marker, ok := stats.coordinator.Journal().TerminalBulkTransactionSnapshot()
	if !ok || marker.TerminalState != MutationTerminalTombstone || marker.FrontierOrdinal != 2 || marker.FailureCode != "journal_budget" {
		t.Fatalf("failed bulk marker = %+v/%t", marker, ok)
	}
}

func TestMutationCoordinatorBulkCancellationRollsBackCheckpoint(t *testing.T) {
	stats := NewRequestStatistics()
	stats.coordinator = NewMutationCoordinatorWithConfig(stats, MutationCoordinatorConfig{
		JournalEntryLimit:        4,
		JournalBudgetBytes:       4 * 4096,
		MaxSerializedIntentBytes: 4096,
		MaxBulkChunkIntents:      2,
		MaxBulkChunkBytes:        16 * 1024,
		GateQueueLimit:           2,
	})
	ctx, cancel := context.WithCancel(context.Background())
	stats.coordinator.bulkChunkDoneHook = func(marker MutationBatchMarker) {
		if marker.ChunkCount == 1 {
			cancel()
		}
	}
	result, err := stats.coordinator.ApplySnapshot(ctx, bulkTestSnapshot(6))
	if !errors.Is(err, context.Canceled) || result != (MergeResult{}) {
		t.Fatalf("cancelled bulk result=%+v err=%v", result, err)
	}
	if got := stats.Snapshot().TotalRequests; got != 0 {
		t.Fatalf("cancelled bulk published %d requests", got)
	}
	if len(stats.coordinator.sourceOrdinals) != 0 || len(stats.coordinator.pendingIdentity) != 0 || len(stats.coordinator.identityByAdmission) != 0 {
		t.Fatalf("cancelled bulk retained identity checkpoint: ordinals=%d pending=%d cache=%d", len(stats.coordinator.sourceOrdinals), len(stats.coordinator.pendingIdentity), len(stats.coordinator.identityByAdmission))
	}
	if pending := stats.coordinator.Journal().PendingCount(); pending != 0 {
		t.Fatalf("cancelled bulk left %d pending intents", pending)
	}
	marker, ok := stats.coordinator.Journal().TerminalBulkTransactionSnapshot()
	if !ok || marker.TerminalState != MutationTerminalTombstone || marker.FrontierOrdinal != 2 || marker.FailureCode != "context_canceled" {
		t.Fatalf("cancelled bulk marker = %+v/%t", marker, ok)
	}
}

func TestMutationCoordinatorRecordWaitsForBulkPublication(t *testing.T) {
	stats := NewRequestStatistics()
	stats.coordinator = NewMutationCoordinatorWithConfig(stats, MutationCoordinatorConfig{
		JournalEntryLimit:        8,
		JournalBudgetBytes:       8 * 4096,
		MaxSerializedIntentBytes: 4096,
		MaxBulkChunkIntents:      2,
		MaxBulkChunkBytes:        16 * 1024,
		GateQueueLimit:           4,
	})
	chunkDone := make(chan struct{})
	releaseChunk := make(chan struct{})
	var once sync.Once
	stats.coordinator.bulkChunkDoneHook = func(marker MutationBatchMarker) {
		if marker.ChunkCount == 1 {
			once.Do(func() { close(chunkDone) })
			<-releaseChunk
		}
	}
	bulkDone := make(chan struct {
		result MergeResult
		err    error
	}, 1)
	go func() {
		result, err := stats.coordinator.ApplySnapshot(context.Background(), bulkTestSnapshot(4))
		bulkDone <- struct {
			result MergeResult
			err    error
		}{result: result, err: err}
	}()
	select {
	case <-chunkDone:
	case <-time.After(time.Second):
		t.Fatal("bulk transaction did not reach first chunk")
	}
	recordDone := make(chan error, 1)
	go func() {
		recordDone <- stats.RecordWithError(logging.WithRequestID(context.Background(), "after-bulk"), coreusage.Record{
			Provider:    "openai",
			Model:       "gpt-5",
			AuthIndex:   "after-bulk-auth",
			RequestedAt: time.Date(2026, 8, 1, 2, 0, 0, 0, time.UTC),
			Detail:      coreusage.Detail{TotalTokens: 1},
		})
	}()
	select {
	case err := <-recordDone:
		t.Fatalf("record completed before bulk publication: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(releaseChunk)
	bulk := <-bulkDone
	if bulk.err != nil || bulk.result.Added != 4 {
		t.Fatalf("bulk publication result=%+v err=%v", bulk.result, bulk.err)
	}
	if err := <-recordDone; err != nil {
		t.Fatalf("record after bulk: %v", err)
	}
	if got := stats.Snapshot().TotalRequests; got != 5 {
		t.Fatalf("requests after bulk and record = %d, want 5", got)
	}
}

func TestMutationCoordinatorBulkWaitsForInFlightRecordAndIncludesIt(t *testing.T) {
	stats := NewRequestStatistics()
	stats.coordinator = NewMutationCoordinatorWithConfig(stats, MutationCoordinatorConfig{
		JournalEntryLimit:        8,
		JournalBudgetBytes:       8 * 4096,
		MaxSerializedIntentBytes: 4096,
		MaxBulkChunkIntents:      2,
		MaxBulkChunkBytes:        16 * 1024,
		GateQueueLimit:           4,
	})
	stats.coordinator.applyMu.Lock()
	recordDone := make(chan error, 1)
	go func() {
		recordDone <- stats.RecordWithError(logging.WithRequestID(context.Background(), "before-bulk"), coreusage.Record{
			Provider:    "openai",
			Model:       "gpt-5",
			AuthIndex:   "before-bulk-auth",
			RequestedAt: time.Date(2026, 8, 1, 1, 0, 0, 0, time.UTC),
			Detail:      coreusage.Detail{TotalTokens: 1},
		})
	}()
	waitForJournalHead(t, stats.coordinator, 1)
	bulkDone := make(chan struct {
		result MergeResult
		err    error
	}, 1)
	go func() {
		result, err := stats.coordinator.ApplySnapshot(context.Background(), bulkTestSnapshot(4))
		bulkDone <- struct {
			result MergeResult
			err    error
		}{result: result, err: err}
	}()
	waitForExclusiveBatch(t, stats.coordinator)
	stats.coordinator.applyMu.Unlock()
	if err := <-recordDone; err != nil {
		t.Fatalf("in-flight record: %v", err)
	}
	bulk := <-bulkDone
	if bulk.err != nil || bulk.result.Added != 4 {
		t.Fatalf("bulk after in-flight record result=%+v err=%v", bulk.result, bulk.err)
	}
	if got := stats.Snapshot().TotalRequests; got != 5 {
		t.Fatalf("bulk staging baseline omitted in-flight record: requests=%d", got)
	}
	if pending := stats.coordinator.Journal().PendingCount(); pending != 0 {
		t.Fatalf("bulk after in-flight record left %d pending intents", pending)
	}
}

func bulkTestSnapshot(count int) StatisticsSnapshot {
	items := make([]RequestDetail, 0, count)
	for index := 0; index < count; index++ {
		items = append(items, RequestDetail{
			RequestID: fmt.Sprintf("bulk-%04d", index),
			Timestamp: time.Date(2026, 8, 1, 0, index%60, index, 0, time.UTC),
			Endpoint:  "POST /v1/responses",
			Model:     "gpt-5",
			Provider:  "openai",
			AuthType:  "api_key",
			AuthIndex: fmt.Sprintf("bulk-auth-%d", index%4),
			Tokens:    RequestTokenStats{InputTokens: 1, TotalTokens: 1},
		})
	}
	return StatisticsSnapshot{APIs: map[string]APISnapshot{
		"POST /v1/responses": {Models: map[string]ModelSnapshot{"gpt-5": {Details: items}}},
	}}
}

func snapshotFromMutationItems(items []snapshotMutationDetail) StatisticsSnapshot {
	details := make([]RequestDetail, 0, len(items))
	for _, item := range items {
		details = append(details, item.detail)
	}
	return StatisticsSnapshot{APIs: map[string]APISnapshot{
		"POST /v1/responses": {Models: map[string]ModelSnapshot{"gpt-5": {Details: details}}},
	}}
}

func TestMutationCoordinatorJournalBudgetRetainsCanonicalRecord(t *testing.T) {
	stats := NewRequestStatistics()
	stats.coordinator = NewMutationCoordinatorWithConfig(stats, MutationCoordinatorConfig{
		JournalEntryLimit:        1,
		JournalBudgetBytes:       1024,
		MaxSerializedIntentBytes: 256,
		GateQueueLimit:           1,
	})
	requestID := strings.Repeat("oversized-request-id-", 32)
	ctx := logging.WithRequestID(context.Background(), requestID)
	record := coreusage.Record{
		Provider:    "openai",
		Model:       "gpt-5",
		AuthIndex:   "budget-auth",
		RequestedAt: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		Detail:      coreusage.Detail{InputTokens: 3, OutputTokens: 4, TotalTokens: 7},
	}
	if err := stats.RecordWithError(ctx, record); !errors.Is(err, ErrMutationJournalBudget) {
		t.Fatalf("oversized record error = %v, want ErrMutationJournalBudget", err)
	}
	snapshot := stats.Snapshot()
	if snapshot.TotalRequests != 1 || snapshot.TotalTokens != 7 {
		t.Fatalf("canonical fallback snapshot = requests:%d tokens:%d, want 1/7", snapshot.TotalRequests, snapshot.TotalTokens)
	}
	if len(snapshot.APIs) != 1 {
		t.Fatalf("canonical fallback APIs = %#v, want one API entry", snapshot.APIs)
	}
	if available, code := stats.ProjectionAvailability(); available || code != "journal_budget_exceeded" {
		t.Fatalf("projection availability = %t/%q, want false/journal_budget_exceeded", available, code)
	}
	if events := stats.ProjectionSnapshot().Events; len(events) != 0 {
		t.Fatalf("canonical fallback mutated projection with %d events", len(events))
	}
}

func TestCanonicalFallbackReusesStableLocation(t *testing.T) {
	stats := NewRequestStatistics()
	ctx := logging.WithRequestID(context.Background(), "stable-fallback")
	record := coreusage.Record{
		Provider:    "openai",
		Model:       "gpt-5",
		AuthIndex:   "stable-fallback-auth",
		RequestedAt: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		Detail:      coreusage.Detail{InputTokens: 1, TotalTokens: 1},
	}
	stats.Record(ctx, record)
	enriched := record
	enriched.Detail = coreusage.Detail{InputTokens: 2, OutputTokens: 3, TotalTokens: 5}
	stats.retainCanonicalRecord(ctx, enriched, ErrMutationJournalBudget)

	snapshot := stats.Snapshot()
	if snapshot.TotalRequests != 1 || snapshot.TotalTokens != 5 {
		t.Fatalf("stable fallback snapshot = requests:%d tokens:%d, want 1/5", snapshot.TotalRequests, snapshot.TotalTokens)
	}
	projection := stats.ProjectionSnapshot()
	if len(projection.Events) != 1 || projection.Totals.Tokens.TotalTokens != 1 {
		t.Fatalf("stable fallback changed projection = %+v, want original one-token event", projection)
	}
}

func waitForJournalHead(t *testing.T, coordinator *MutationCoordinator, want uint64) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if coordinator.Journal().Head() >= want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("journal head did not reach %d; got %d", want, coordinator.Journal().Head())
}

func waitForExclusiveBatch(t *testing.T, coordinator *MutationCoordinator) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		coordinator.mu.Lock()
		active := coordinator.activeBatch
		coordinator.mu.Unlock()
		if active {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("exclusive bulk transaction did not become active")
}
