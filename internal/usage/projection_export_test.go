package usage

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestPinUsageExportKeepsHighWatermarkAndImmutableRows(t *testing.T) {
	stats := NewRequestStatistics()
	baseTime := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	first := RequestDetail{
		RequestID: "export-immutable-1",
		Timestamp: baseTime,
		Endpoint:  "POST /v1/responses",
		Model:     "model-before",
		Provider:  "provider-before",
		AuthIndex: "auth-before",
		Source:    "source-before",
		Tokens:    RequestTokenStats{InputTokens: 2, OutputTokens: 1, TotalTokens: 3, TokenUsageSource: TokenUsageSourceProvider},
	}
	if _, err := stats.MergeSnapshotWithError(StatisticsSnapshot{APIs: map[string]APISnapshot{
		first.Endpoint: {Models: map[string]ModelSnapshot{first.Model: {Details: []RequestDetail{first}}}},
	}}); err != nil {
		t.Fatalf("seed first detail: %v", err)
	}

	lease, estimate, err := stats.PinUsageExport(context.Background(), UsageExportQuery{
		From: baseTime.Add(-time.Minute),
		To:   baseTime.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("PinUsageExport() error = %v", err)
	}
	defer lease.Release()
	if estimate.EventCount != 1 || estimate.SnapshotMaxSequence == 0 || estimate.GenerationID == 0 {
		t.Fatalf("estimate = %+v", estimate)
	}

	second := first
	second.RequestID = "export-immutable-2"
	second.Timestamp = baseTime.Add(30 * time.Second)
	second.Model = "model-after"
	second.Source = "source-after"
	if _, err := stats.MergeSnapshotWithError(StatisticsSnapshot{APIs: map[string]APISnapshot{
		second.Endpoint: {Models: map[string]ModelSnapshot{second.Model: {Details: []RequestDetail{second}}}},
	}}); err != nil {
		t.Fatalf("append second detail: %v", err)
	}

	rewrite := first
	rewrite.Model = "model-rewritten"
	rewrite.Source = "source-rewritten"
	rewrite.Tokens = RequestTokenStats{InputTokens: 20, OutputTokens: 10, TotalTokens: 30, TokenUsageSource: TokenUsageSourceProvider}
	if _, err := stats.MergeSnapshotWithError(StatisticsSnapshot{APIs: map[string]APISnapshot{
		rewrite.Endpoint: {Models: map[string]ModelSnapshot{rewrite.Model: {Details: []RequestDetail{rewrite}}}},
	}}); err != nil {
		t.Fatalf("rewrite first detail: %v", err)
	}

	page, err := lease.NextPage(context.Background(), 0, 500)
	if err != nil {
		t.Fatalf("NextPage() error = %v", err)
	}
	if len(page.Rows) != 1 || page.HasMore || page.NextPosition != 1 {
		t.Fatalf("page = %+v", page)
	}
	row := page.Rows[0]
	if row.Detail.RequestID != first.RequestID || row.Detail.Model != first.Model || row.Detail.Source != UsageSourceKeyV1(first) || row.Detail.Tokens.TotalTokens != first.Tokens.TotalTokens {
		t.Fatalf("pinned row changed = %+v", row)
	}
	if page.SnapshotMaxSequence != estimate.SnapshotMaxSequence || page.GenerationID != estimate.GenerationID {
		t.Fatalf("page identity = %+v estimate=%+v", page, estimate)
	}
}

func TestUsageExportLeaseReleaseIsIdempotentAndPageIsBounded(t *testing.T) {
	stats := NewRequestStatistics()
	baseTime := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	details := make([]RequestDetail, 0, 3)
	for index := 0; index < 3; index++ {
		details = append(details, RequestDetail{
			RequestID: "export-page-" + string(rune('1'+index)),
			Timestamp: baseTime.Add(time.Duration(index) * time.Second),
			Endpoint:  "POST /v1/responses",
			Model:     "page-model",
			Provider:  "page-provider",
			AuthIndex: "page-auth",
			Source:    "page-source",
			Tokens:    RequestTokenStats{InputTokens: 1, OutputTokens: 1, TotalTokens: 2, TokenUsageSource: TokenUsageSourceProvider},
		})
	}
	if _, err := stats.MergeSnapshotWithError(StatisticsSnapshot{APIs: map[string]APISnapshot{
		"POST /v1/responses": {Models: map[string]ModelSnapshot{"page-model": {Details: details}}},
	}}); err != nil {
		t.Fatalf("seed details: %v", err)
	}

	lease, _, err := stats.PinUsageExport(context.Background(), UsageExportQuery{})
	if err != nil {
		t.Fatalf("PinUsageExport() error = %v", err)
	}
	page, err := lease.NextPage(context.Background(), 0, 2)
	if err != nil {
		t.Fatalf("first NextPage() error = %v", err)
	}
	if len(page.Rows) != 2 || !page.HasMore || page.NextPosition != 2 {
		t.Fatalf("first page = %+v", page)
	}
	page, err = lease.NextPage(context.Background(), page.NextPosition, 2)
	if err != nil {
		t.Fatalf("second NextPage() error = %v", err)
	}
	if len(page.Rows) != 1 || page.HasMore || page.NextPosition != 3 {
		t.Fatalf("second page = %+v", page)
	}
	lease.Release()
	lease.Release()
	if _, err := lease.NextPage(context.Background(), 0, 1); err == nil {
		t.Fatal("NextPage() after Release() returned nil error")
	}
}

func TestUsageExportLeaseAppendKeepsPinnedPrefixReadable(t *testing.T) {
	stats := NewRequestStatistics()
	baseTime := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	first := RequestDetail{
		RequestID: "export-append-prefix",
		Timestamp: baseTime,
		Endpoint:  "POST /v1/responses",
		Model:     "append-model",
		Provider:  "append-provider",
		AuthIndex: "append-auth",
		Source:    "append-source",
		Tokens:    RequestTokenStats{InputTokens: 2, OutputTokens: 1, TotalTokens: 3, TokenUsageSource: TokenUsageSourceProvider},
	}
	if _, err := stats.MergeSnapshotWithError(StatisticsSnapshot{APIs: map[string]APISnapshot{
		first.Endpoint: {Models: map[string]ModelSnapshot{first.Model: {Details: []RequestDetail{first}}}},
	}}); err != nil {
		t.Fatalf("seed first detail: %v", err)
	}

	lease, estimate, err := stats.PinUsageExport(context.Background(), UsageExportQuery{
		From: baseTime.Add(-time.Minute),
		To:   baseTime.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("pin export: %v", err)
	}
	defer lease.Release()
	if estimate.EventCount != 1 {
		t.Fatalf("estimate event count = %d, want 1", estimate.EventCount)
	}

	second := first
	second.RequestID = "export-append-after"
	second.Timestamp = baseTime.Add(30 * time.Second)
	if _, err := stats.MergeSnapshotWithError(StatisticsSnapshot{APIs: map[string]APISnapshot{
		second.Endpoint: {Models: map[string]ModelSnapshot{second.Model: {Details: []RequestDetail{second}}}},
	}}); err != nil {
		t.Fatalf("append second detail: %v", err)
	}

	page, err := lease.NextPage(context.Background(), 0, 500)
	if err != nil {
		t.Fatalf("pinned prefix page: %v", err)
	}
	if len(page.Rows) != 1 || page.Rows[0].Detail.RequestID != first.RequestID {
		t.Fatalf("pinned prefix page = %+v", page)
	}
}

func TestUsageExportLeaseSurvivesLiveProjectionReplacement(t *testing.T) {
	stats := NewRequestStatistics()
	baseTime := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	detail := RequestDetail{
		RequestID: "export-pointer-replacement",
		Timestamp: baseTime,
		Endpoint:  "POST /v1/responses",
		Model:     "pointer-model",
		Provider:  "pointer-provider",
		AuthIndex: "pointer-auth",
		Source:    "pointer-source",
		Tokens:    RequestTokenStats{InputTokens: 2, OutputTokens: 1, TotalTokens: 3, TokenUsageSource: TokenUsageSourceProvider},
	}
	if _, err := stats.MergeSnapshotWithError(StatisticsSnapshot{APIs: map[string]APISnapshot{
		detail.Endpoint: {Models: map[string]ModelSnapshot{detail.Model: {Details: []RequestDetail{detail}}}},
	}}); err != nil {
		t.Fatalf("seed detail: %v", err)
	}

	lease, _, err := stats.PinUsageExport(context.Background(), UsageExportQuery{})
	if err != nil {
		t.Fatalf("pin export: %v", err)
	}
	defer lease.Release()

	stats.mu.Lock()
	stats.projection = stats.projection.Clone()
	stats.mu.Unlock()

	page, err := lease.NextPage(context.Background(), 0, 1)
	if err != nil {
		t.Fatalf("pinned page after live projection replacement: %v", err)
	}
	if len(page.Rows) != 1 || page.Rows[0].Detail.RequestID != detail.RequestID {
		t.Fatalf("pinned page after live projection replacement = %+v", page)
	}
}

func TestUsageExportLeaseSurvivesPinnedEventRemoval(t *testing.T) {
	stats := NewRequestStatistics()
	detail := RequestDetail{
		RequestID: "export-removal",
		Timestamp: time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC),
		Endpoint:  "POST /v1/responses",
		Model:     "removal-model",
		Provider:  "removal-provider",
		AuthIndex: "removal-auth",
		Source:    "removal-source",
		Tokens:    RequestTokenStats{InputTokens: 1, OutputTokens: 1, TotalTokens: 2, TokenUsageSource: TokenUsageSourceProvider},
	}
	if _, err := stats.MergeSnapshotWithError(StatisticsSnapshot{APIs: map[string]APISnapshot{
		detail.Endpoint: {Models: map[string]ModelSnapshot{detail.Model: {Details: []RequestDetail{detail}}}},
	}}); err != nil {
		t.Fatalf("seed detail: %v", err)
	}
	lease, _, err := stats.PinUsageExport(context.Background(), UsageExportQuery{})
	if err != nil {
		t.Fatalf("pin export: %v", err)
	}
	defer lease.Release()
	page, err := lease.NextPage(context.Background(), 0, 1)
	if err != nil || len(page.Rows) != 1 {
		t.Fatalf("initial page = %+v, err=%v", page, err)
	}
	if !stats.projection.RemoveEvent(page.Rows[0].Event.StableEventID) {
		t.Fatal("RemoveEvent() returned false")
	}
	page, err = lease.NextPage(context.Background(), 0, 1)
	if err != nil || len(page.Rows) != 1 || page.Rows[0].Detail.RequestID != detail.RequestID {
		t.Fatalf("pinned row after removal = %+v, err=%v", page, err)
	}
}

func TestLookupEventDetailsForExportRejectsVersionDrift(t *testing.T) {
	stats := NewRequestStatistics()
	detail := RequestDetail{
		RequestID: "export-version-drift",
		Timestamp: time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC),
		Endpoint:  "POST /v1/responses",
		Model:     "drift-model",
		Provider:  "drift-provider",
		AuthIndex: "drift-auth",
		Source:    "drift-source",
		Tokens:    RequestTokenStats{InputTokens: 1, OutputTokens: 1, TotalTokens: 2, TokenUsageSource: TokenUsageSourceProvider},
	}
	if _, err := stats.MergeSnapshotWithError(StatisticsSnapshot{APIs: map[string]APISnapshot{
		detail.Endpoint: {Models: map[string]ModelSnapshot{detail.Model: {Details: []RequestDetail{detail}}}},
	}}); err != nil {
		t.Fatalf("seed detail: %v", err)
	}
	lease, _, err := stats.PinUsageExport(context.Background(), UsageExportQuery{})
	if err != nil {
		t.Fatalf("pin export: %v", err)
	}
	defer lease.Release()
	page, err := lease.NextPage(context.Background(), 0, 1)
	if err != nil || len(page.Rows) != 1 {
		t.Fatalf("initial page = %+v, err=%v", page, err)
	}

	stats.mu.Lock()
	locationKey := stats.detailEventLocations[page.Rows[0].Event.StableEventID]
	location := stats.detailLocations[locationKey]
	location.modelStats.Details[location.index].Tokens = RequestTokenStats{InputTokens: 9, OutputTokens: 4, TotalTokens: 13, TokenUsageSource: TokenUsageSourceProvider}
	stats.mu.Unlock()
	if _, err := stats.LookupEventDetailsForExport([]EventRef{page.Rows[0].Event}); !errors.Is(err, ErrUsageExportGenerationChanged) {
		t.Fatalf("version drift error = %v, want %v", err, ErrUsageExportGenerationChanged)
	}
}

func TestUsageExportLeaseAcquireRejectsAtExpiry(t *testing.T) {
	stats := NewRequestStatistics()
	baseTime := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	detail := RequestDetail{
		RequestID: "export-expiry",
		Timestamp: baseTime,
		Endpoint:  "POST /v1/responses",
		Model:     "expiry-model",
		Provider:  "expiry-provider",
		Source:    "expiry-source",
		Tokens:    RequestTokenStats{InputTokens: 1, OutputTokens: 1, TotalTokens: 2, TokenUsageSource: TokenUsageSourceProvider},
	}
	if _, err := stats.MergeSnapshotWithError(StatisticsSnapshot{APIs: map[string]APISnapshot{
		detail.Endpoint: {Models: map[string]ModelSnapshot{detail.Model: {Details: []RequestDetail{detail}}}},
	}}); err != nil {
		t.Fatalf("seed detail: %v", err)
	}

	now := baseTime
	lease, _, err := stats.PinUsageExportWithClock(context.Background(), UsageExportQuery{}, func() time.Time { return now }, time.Minute)
	if err != nil {
		t.Fatalf("pin export: %v", err)
	}
	now = baseTime.Add(time.Minute)
	if acquired := lease.Acquire(); acquired != nil {
		acquired.Release()
		t.Fatal("Acquire() succeeded at the exact generation expiry")
	}
	if _, err := lease.NextPage(context.Background(), 0, 1); !errors.Is(err, ErrUsageExportLeaseExpired) {
		t.Fatalf("NextPage() at expiry error = %v, want %v", err, ErrUsageExportLeaseExpired)
	}
	lease.Release()
}
