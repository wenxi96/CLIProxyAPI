package usage

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/logging"
	coreusage "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
)

func TestRequestStatisticsRecordIncludesLatency(t *testing.T) {
	stats := NewRequestStatistics()
	stats.Record(context.Background(), coreusage.Record{
		APIKey:      "test-key",
		Model:       "gpt-5.4",
		RequestedAt: time.Date(2026, 3, 20, 12, 0, 0, 0, time.UTC),
		Latency:     1500 * time.Millisecond,
		Detail: coreusage.Detail{
			InputTokens:  10,
			OutputTokens: 20,
			TotalTokens:  30,
		},
	})

	snapshot := stats.Snapshot()
	details := snapshot.APIs[redactedHash("test-key")].Models["gpt-5.4"].Details
	if len(details) != 1 {
		t.Fatalf("details len = %d, want 1", len(details))
	}
	if details[0].LatencyMs != 1500 {
		t.Fatalf("latency_ms = %d, want 1500", details[0].LatencyMs)
	}
}

func TestRequestStatisticsRecordIncludesCanonicalDetailV2(t *testing.T) {
	stats := NewRequestStatistics()
	requestedAt := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	ctx := logging.WithRequestID(context.Background(), "req-v2")
	ctx = logging.WithEndpoint(ctx, "POST /v1/chat/completions")
	ctx = logging.WithClientIP(ctx, "203.0.113.5")

	stats.Record(ctx, coreusage.Record{
		Provider:     "openai",
		ExecutorType: "OpenAIExecutor",
		Model:        "gpt-5",
		Alias:        "client-gpt",
		Source:       "sk-raw-source-secret",
		AuthIndex:    "auth-1",
		AuthType:     "api_key",
		RequestedAt:  requestedAt,
		Latency:      1234 * time.Millisecond,
		Detail: coreusage.Detail{
			InputTokens:     100,
			OutputTokens:    50,
			ReasoningTokens: 10,
			CachedTokens:    30,
			CacheReadTokens: 30,
			TotalTokens:     150,
		},
	})

	snapshot := stats.Snapshot()
	details := snapshot.APIs["POST /v1/chat/completions"].Models["gpt-5"].Details
	if len(details) != 1 {
		t.Fatalf("details len = %d, want 1", len(details))
	}
	detail := details[0]
	if detail.RequestID != "req-v2" || detail.ClientIP != "203.0.113.5" || !detail.Timestamp.Equal(requestedAt) {
		t.Fatalf("request context = %+v, want request_id/client_ip/timestamp populated", detail)
	}
	if detail.Endpoint != "POST /v1/chat/completions" || detail.Model != "gpt-5" || detail.Provider != "openai" || detail.ExecutorType != "OpenAIExecutor" {
		t.Fatalf("provider context = %+v, want canonical endpoint/model/provider/executor", detail)
	}
	if detail.AuthType != "api_key" || detail.ModelAlias != "client-gpt" || detail.AuthIndex != "auth-1" || detail.Source != "auth-1" {
		t.Fatalf("auth context = %+v, want safe source from auth_index", detail)
	}
	if detail.DetailRole != DetailRolePrimary {
		t.Fatalf("detail_role = %q, want %q", detail.DetailRole, DetailRolePrimary)
	}
	if detail.EstimatedCostUSD != nil {
		t.Fatalf("estimated_cost_usd = %v, want nil", *detail.EstimatedCostUSD)
	}
	if detail.Tokens.InputTokens != 100 ||
		detail.Tokens.OutputTokens != 50 ||
		detail.Tokens.ReasoningTokens != 10 ||
		detail.Tokens.CachedTokens != 30 ||
		detail.Tokens.CacheReadTokens != 30 ||
		detail.Tokens.CacheCreationTokens != 0 ||
		detail.Tokens.TotalTokens != 150 ||
		detail.Tokens.ReportedTotalTokens != 150 ||
		detail.Tokens.ComputedTotalTokens != 150 ||
		detail.Tokens.TokenUsageSource != TokenUsageSourceProvider ||
		detail.Tokens.CacheSplitStatus != CacheSplitReadOnly ||
		detail.Tokens.ReasoningCostMode != ReasoningCostIncludedInOutput {
		t.Fatalf("tokens = %+v, want v2 normalized token facts", detail.Tokens)
	}

	authTokensJSON, errMarshal := json.Marshal(snapshot.Auths["auth-1"].Tokens)
	if errMarshal != nil {
		t.Fatalf("marshal auth tokens: %v", errMarshal)
	}
	if strings.Contains(string(authTokensJSON), "reported_total_tokens") ||
		strings.Contains(string(authTokensJSON), "computed_total_tokens") ||
		strings.Contains(string(authTokensJSON), "token_usage_source") {
		t.Fatalf("aggregate tokens leaked request-only fields: %s", authTokensJSON)
	}
}

func TestRequestStatisticsPreservesExplicitProviderZeroUsage(t *testing.T) {
	stats := NewRequestStatistics()
	ctx := logging.WithRequestID(context.Background(), "req-zero-usage")
	ctx = logging.WithEndpoint(ctx, "POST /v1/responses")
	stats.Record(ctx, coreusage.Record{
		Provider:      "openai",
		Model:         "gpt-5.4",
		AuthIndex:     "auth-zero-usage",
		RequestedAt:   time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC),
		UsageObserved: true,
	})

	detail := stats.Snapshot().APIs["POST /v1/responses"].Models["gpt-5.4"].Details[0]
	if detail.Tokens.TokenUsageSource != TokenUsageSourceProvider || detail.Tokens.TotalTokens != 0 {
		t.Fatalf("tokens = %+v, want explicit provider-reported zero usage", detail.Tokens)
	}
}

func TestEstimatedCostUSDUsesValueCopyAndSurvivesEnrichment(t *testing.T) {
	cost := 1.25
	detail := RequestDetail{
		RequestID:        "req-cost-copy",
		Timestamp:        time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC),
		Endpoint:         "POST /v1/responses",
		Model:            "gpt-5.4",
		Provider:         "openai",
		AuthIndex:        "auth-cost-copy",
		EstimatedCostUSD: &cost,
	}
	stats := NewRequestStatistics()
	stats.MergeSnapshot(StatisticsSnapshot{APIs: map[string]APISnapshot{
		"POST /v1/responses": {Models: map[string]ModelSnapshot{
			"gpt-5.4": {Details: []RequestDetail{detail}},
		}},
	}})
	cost = 9.99
	first := stats.Snapshot()
	firstDetail := first.APIs["POST /v1/responses"].Models["gpt-5.4"].Details[0]
	if firstDetail.EstimatedCostUSD == nil || *firstDetail.EstimatedCostUSD != 1.25 {
		t.Fatalf("stored estimated cost = %v, want 1.25", firstDetail.EstimatedCostUSD)
	}
	*firstDetail.EstimatedCostUSD = 7.77
	secondDetail := stats.Snapshot().APIs["POST /v1/responses"].Models["gpt-5.4"].Details[0]
	if secondDetail.EstimatedCostUSD == nil || *secondDetail.EstimatedCostUSD != 1.25 {
		t.Fatalf("snapshot mutation changed stored estimated cost: %v", secondDetail.EstimatedCostUSD)
	}

	existing := secondDetail
	incoming := existing
	incoming.EstimatedCostUSD = nil
	incoming.Tokens = RequestTokenStats{InputTokens: 2, TotalTokens: 2}
	merged := mergeEnrichedDetail(existing, incoming)
	if merged.EstimatedCostUSD == nil || *merged.EstimatedCostUSD != 1.25 {
		t.Fatalf("enriched estimated cost = %v, want existing 1.25 preserved", merged.EstimatedCostUSD)
	}

	existingWithoutCost := secondDetail
	existingWithoutCost.EstimatedCostUSD = nil
	updatedCost := 2.5
	incomingCostOnly := existingWithoutCost
	incomingCostOnly.EstimatedCostUSD = &updatedCost
	if !shouldEnrichDetail(existingWithoutCost, incomingCostOnly) {
		t.Fatal("cost-only update should enrich existing detail")
	}
	costMerged := mergeEnrichedDetail(existingWithoutCost, incomingCostOnly)
	if costMerged.EstimatedCostUSD == nil || *costMerged.EstimatedCostUSD != 2.5 {
		t.Fatalf("cost-only enrichment = %v, want 2.5", costMerged.EstimatedCostUSD)
	}
}

func TestGeminiInteractionsUsesSeparateReasoningFacts(t *testing.T) {
	tokens := normaliseDetail(coreusage.Detail{
		InputTokens:     10,
		OutputTokens:    20,
		ReasoningTokens: 5,
	}, "gemini-interactions")

	if tokens.ReasoningCostMode != ReasoningCostSeparate {
		t.Fatalf("reasoning_cost_mode = %q, want %q", tokens.ReasoningCostMode, ReasoningCostSeparate)
	}
	if tokens.ComputedTotalTokens != 35 || tokens.TotalTokens != 35 {
		t.Fatalf("tokens = %+v, want separate reasoning included in computed total", tokens)
	}
}

func TestSafeSourceIdentifierPrefersAuthIndexAndRedactsOpaqueFallback(t *testing.T) {
	const opaque = "0123456789abcdef0123456789abcdef"
	if got := safeSourceIdentifier(opaque, "auth-safe"); got != "auth-safe" {
		t.Fatalf("safeSourceIdentifier() = %q, want auth index", got)
	}
	if got := safeSourceIdentifier(opaque, ""); got != redactedHash(opaque) {
		t.Fatalf("safeSourceIdentifier() = %q, want redacted opaque source", got)
	}
	if got := safeSourceIdentifier("user@example.com", ""); got != "user@example.com" {
		t.Fatalf("safeSourceIdentifier() = %q, want email identifier preserved", got)
	}
	const forgedRedacted = "redacted:raw-secret-value"
	if got := safeSourceIdentifier(forgedRedacted, ""); got != redactedHash(forgedRedacted) {
		t.Fatalf("safeSourceIdentifier() = %q, want malformed redacted prefix hashed", got)
	}
	const validRedacted = "redacted:0123456789abcdef"
	if got := safeSourceIdentifier(validRedacted, ""); got != validRedacted {
		t.Fatalf("safeSourceIdentifier() = %q, want valid redacted hash preserved", got)
	}
}

func TestRequestStatisticsMaintainsIncrementalDetailLocationIndex(t *testing.T) {
	stats := NewRequestStatistics()
	ctx := logging.WithRequestID(context.Background(), "req-index")
	ctx = logging.WithEndpoint(ctx, "POST /v1/responses")
	record := coreusage.Record{
		Provider:    "openai",
		Model:       "gpt-5.4",
		AuthIndex:   "auth-index",
		RequestedAt: time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC),
	}

	stats.Record(ctx, record)
	record.Detail = coreusage.Detail{InputTokens: 2, OutputTokens: 3, TotalTokens: 5}
	stats.Record(ctx, record)

	if len(stats.detailLocations) != 1 {
		t.Fatalf("detail location index size = %d, want 1 after enrich", len(stats.detailLocations))
	}
	snapshot := stats.Snapshot()
	if snapshot.TotalRequests != 1 || snapshot.TotalTokens != 5 {
		t.Fatalf("snapshot = %+v, want one enriched request with 5 tokens", snapshot)
	}
}

func TestRequestStatisticsKeepsSameRequestIDDifferentAttemptTimestamps(t *testing.T) {
	stats := NewRequestStatistics()
	firstTimestamp := time.Date(2026, 7, 12, 23, 0, 0, 0, time.UTC)
	secondTimestamp := firstTimestamp.Add(2 * time.Hour)
	ctx := logging.WithRequestID(context.Background(), "req-cross-day-enrich")
	ctx = logging.WithEndpoint(ctx, "POST /v1/responses")
	record := coreusage.Record{
		Provider:    "openai",
		Model:       "gpt-5.4",
		AuthIndex:   "auth-cross-day",
		RequestedAt: firstTimestamp,
	}

	stats.Record(ctx, record)
	record.RequestedAt = secondTimestamp
	record.Detail = coreusage.Detail{InputTokens: 2, OutputTokens: 3, TotalTokens: 5}
	stats.Record(ctx, record)

	snapshot := stats.Snapshot()
	details := snapshot.APIs["POST /v1/responses"].Models["gpt-5.4"].Details
	if len(details) != 2 || snapshot.TotalRequests != 2 {
		t.Fatalf("details/requests = %d/%d, want two upstream attempts", len(details), snapshot.TotalRequests)
	}
	if snapshot.RequestsByDay["2026-07-12"] != 1 || snapshot.RequestsByDay["2026-07-13"] != 1 {
		t.Fatalf("requests_by_day = %#v, want one request per attempt day", snapshot.RequestsByDay)
	}
	if snapshot.TokensByDay["2026-07-12"] != 0 || snapshot.TokensByDay["2026-07-13"] != 5 {
		t.Fatalf("tokens_by_day = %#v, want tokens on the second attempt day", snapshot.TokensByDay)
	}
}

func TestListAuthRequestsUsesStableOrderForEqualTimestamps(t *testing.T) {
	stats := NewRequestStatistics()
	requestedAt := time.Date(2026, 7, 14, 10, 0, 0, 0, time.UTC)
	for _, requestID := range []string{"req-b", "req-a"} {
		ctx := logging.WithRequestID(context.Background(), requestID)
		ctx = logging.WithEndpoint(ctx, "POST /v1/responses")
		stats.Record(ctx, coreusage.Record{
			Provider:    "openai",
			Model:       "gpt-5.4",
			AuthIndex:   "auth-stable-page",
			RequestedAt: requestedAt,
			Detail:      coreusage.Detail{InputTokens: 1, TotalTokens: 1},
		})
	}

	for range 20 {
		first := stats.ListAuthRequests("auth-stable-page", AuthRequestFilter{Limit: 1})
		second := stats.ListAuthRequests("auth-stable-page", AuthRequestFilter{Limit: 1, Offset: 1})
		if len(first.Items) != 1 || len(second.Items) != 1 {
			t.Fatalf("page sizes = %d/%d, want 1/1", len(first.Items), len(second.Items))
		}
		if first.Items[0].RequestID != "req-a" || second.Items[0].RequestID != "req-b" {
			t.Fatalf("page request IDs = %q/%q, want req-a/req-b", first.Items[0].RequestID, second.Items[0].RequestID)
		}
	}
}

func TestListAuthRequestsUsesAPIBucketAsEqualTimestampTieBreaker(t *testing.T) {
	stats := NewRequestStatistics()
	requestedAt := time.Date(2026, 7, 15, 11, 0, 0, 0, time.UTC)
	ctx := logging.WithRequestID(context.Background(), "req-cross-bucket")
	ctx = logging.WithEndpoint(ctx, "POST /v1/responses")
	for index, apiKey := range []string{"downstream-b", "downstream-a"} {
		stats.Record(ctx, coreusage.Record{
			APIKey:      apiKey,
			Provider:    "openai",
			Model:       "gpt-5.4",
			AuthIndex:   "auth-cross-bucket",
			RequestedAt: requestedAt,
			Detail:      coreusage.Detail{InputTokens: int64(index + 1), TotalTokens: int64(index + 1)},
		})
	}

	firstBucket := redactedHash("downstream-a")
	secondBucket := redactedHash("downstream-b")
	wantFirstTokens, wantSecondTokens := int64(2), int64(1)
	if secondBucket < firstBucket {
		wantFirstTokens, wantSecondTokens = wantSecondTokens, wantFirstTokens
	}
	for range 20 {
		first := stats.ListAuthRequests("auth-cross-bucket", AuthRequestFilter{Limit: 1})
		second := stats.ListAuthRequests("auth-cross-bucket", AuthRequestFilter{Limit: 1, Offset: 1})
		if len(first.Items) != 1 || len(second.Items) != 1 {
			t.Fatalf("page sizes = %d/%d, want 1/1", len(first.Items), len(second.Items))
		}
		if first.Items[0].Tokens.TotalTokens != wantFirstTokens || second.Items[0].Tokens.TotalTokens != wantSecondTokens {
			t.Fatalf("page token order = %d/%d, want %d/%d", first.Items[0].Tokens.TotalTokens, second.Items[0].Tokens.TotalTokens, wantFirstTokens, wantSecondTokens)
		}
	}
}

func TestSafeImportedAPINameRejectsMalformedRedactedPrefix(t *testing.T) {
	const forged = "redacted:raw-api-key"
	if got := safeImportedAPIName(forged, RequestDetail{}); got != redactedHash(forged) {
		t.Fatalf("safeImportedAPIName() = %q, want malformed redacted prefix hashed", got)
	}
	const valid = "redacted:0123456789abcdef"
	if got := safeImportedAPIName(valid, RequestDetail{}); got != valid {
		t.Fatalf("safeImportedAPIName() = %q, want valid redacted hash preserved", got)
	}
}

func TestRequestStatisticsPreservesDistinctAPIKeyDimensions(t *testing.T) {
	stats := NewRequestStatistics()
	ctx := logging.WithEndpoint(context.Background(), "POST /v1/responses")
	first := coreusage.Record{
		APIKey:      "downstream-key-a",
		Provider:    "openai",
		Model:       "gpt-5.4",
		RequestedAt: time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC),
		Detail:      coreusage.Detail{InputTokens: 1, TotalTokens: 1},
	}
	second := first
	second.APIKey = "downstream-key-b"
	second.RequestedAt = first.RequestedAt.Add(time.Second)

	stats.Record(ctx, first)
	stats.Record(ctx, second)

	snapshot := stats.Snapshot()
	if len(snapshot.APIs) != 2 {
		t.Fatalf("APIs len = %d, want distinct entries for two downstream keys", len(snapshot.APIs))
	}
	for _, apiKey := range []string{first.APIKey, second.APIKey} {
		if _, ok := snapshot.APIs[redactedHash(apiKey)]; !ok {
			t.Fatalf("missing redacted API dimension for %q: %#v", apiKey, snapshot.APIs)
		}
		if _, ok := snapshot.APIs[apiKey]; ok {
			t.Fatalf("raw API key %q exposed in snapshot", apiKey)
		}
	}
}

func TestRequestStatisticsHashesEndpointShapedAPIKey(t *testing.T) {
	stats := NewRequestStatistics()
	ctx := logging.WithEndpoint(context.Background(), "POST /v1/responses")
	stats.Record(ctx, coreusage.Record{
		APIKey:      "POST /secret-shaped-key",
		Provider:    "openai",
		Model:       "gpt-5.4",
		RequestedAt: time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC),
		Detail:      coreusage.Detail{InputTokens: 1, TotalTokens: 1},
	})

	snapshot := stats.Snapshot()
	if _, ok := snapshot.APIs[APIKeyHash("POST /secret-shaped-key")]; !ok {
		t.Fatalf("missing hashed endpoint-shaped API key: %#v", snapshot.APIs)
	}
	data, errMarshal := json.Marshal(snapshot)
	if errMarshal != nil {
		t.Fatalf("marshal snapshot: %v", errMarshal)
	}
	if strings.Contains(string(data), "secret-shaped-key") {
		t.Fatalf("snapshot leaked endpoint-shaped API key: %s", data)
	}
}

func TestNormalizeRequestTokensPreservesMoreCompleteCachedTotal(t *testing.T) {
	tokens := normaliseRequestTokens(RequestTokenStats{
		CachedTokens:    100,
		CacheReadTokens: 60,
	}, "openai")
	if tokens.CachedTokens != 100 {
		t.Fatalf("cached_tokens = %d, want existing complete total 100", tokens.CachedTokens)
	}
	if tokens.CacheSplitStatus != CacheSplitReadOnly {
		t.Fatalf("cache_split_status = %q, want %q", tokens.CacheSplitStatus, CacheSplitReadOnly)
	}
}

func TestNormalizeRequestTokensRebuildsCanonicalMetadata(t *testing.T) {
	tokens := normaliseRequestTokens(RequestTokenStats{
		InputTokens:       10,
		OutputTokens:      5,
		ReasoningTokens:   3,
		TokenUsageSource:  TokenUsageSourceMissing,
		CacheSplitStatus:  "forged",
		ReasoningCostMode: ReasoningCostSeparate,
	}, "openai")
	if tokens.TokenUsageSource != TokenUsageSourceProvider {
		t.Fatalf("token_usage_source = %q, want provider usage for positive facts", tokens.TokenUsageSource)
	}
	if tokens.CacheSplitStatus != CacheSplitNone {
		t.Fatalf("cache_split_status = %q, want %q", tokens.CacheSplitStatus, CacheSplitNone)
	}
	if tokens.ReasoningCostMode != ReasoningCostIncludedInOutput {
		t.Fatalf("reasoning_cost_mode = %q, want %q", tokens.ReasoningCostMode, ReasoningCostIncludedInOutput)
	}
	if tokens.TotalTokens != 15 {
		t.Fatalf("total_tokens = %d, want reasoning included in output total 15", tokens.TotalTokens)
	}
}

func TestRequestStatisticsRecordIncludesClientIP(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name       string
		remoteAddr string
		wantIP     string
	}{
		{name: "ipv4", remoteAddr: "203.0.113.10:54321", wantIP: "203.0.113.10"},
		{name: "ipv6", remoteAddr: "[2001:db8::1]:443", wantIP: "2001:db8::1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stats := NewRequestStatistics()
			recorder := httptest.NewRecorder()
			ginCtx, _ := gin.CreateTestContext(recorder)
			req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			req.RemoteAddr = tt.remoteAddr
			ginCtx.Request = req

			ctx := context.WithValue(context.Background(), "gin", ginCtx)
			stats.Record(ctx, coreusage.Record{
				APIKey:      "test-key",
				Model:       "gpt-5.4",
				RequestedAt: time.Date(2026, 3, 20, 12, 0, 0, 0, time.UTC),
				Detail: coreusage.Detail{
					InputTokens:  10,
					OutputTokens: 20,
					TotalTokens:  30,
				},
			})

			snapshot := stats.Snapshot()
			details := snapshot.APIs[redactedHash("test-key")].Models["gpt-5.4"].Details
			if len(details) != 1 {
				t.Fatalf("details len = %d, want 1", len(details))
			}
			if details[0].ClientIP != tt.wantIP {
				t.Fatalf("client_ip = %q, want %q", details[0].ClientIP, tt.wantIP)
			}
		})
	}
}

func TestRequestStatisticsRecordMatchesHTTPLogClientIP(t *testing.T) {
	gin.SetMode(gin.TestMode)

	stats := NewRequestStatistics()
	recorder := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(recorder)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req.RemoteAddr = "203.0.113.10:54321"
	req.Header.Set("X-Forwarded-For", "198.51.100.8")
	ginCtx.Request = req

	expectedIP := logging.ResolveClientIP(ginCtx)

	ctx := context.WithValue(context.Background(), "gin", ginCtx)
	stats.Record(ctx, coreusage.Record{
		APIKey:      "test-key",
		Model:       "gpt-5.4",
		RequestedAt: time.Date(2026, 3, 20, 12, 0, 0, 0, time.UTC),
		Detail: coreusage.Detail{
			InputTokens:  10,
			OutputTokens: 20,
			TotalTokens:  30,
		},
	})

	snapshot := stats.Snapshot()
	details := snapshot.APIs[redactedHash("test-key")].Models["gpt-5.4"].Details
	if len(details) != 1 {
		t.Fatalf("details len = %d, want 1", len(details))
	}
	if details[0].ClientIP != expectedIP {
		t.Fatalf("client_ip = %q, want same as HTTP log %q", details[0].ClientIP, expectedIP)
	}
}

func TestRequestStatisticsSnapshotIncludesAuthAggregation(t *testing.T) {
	stats := NewRequestStatistics()
	first := time.Date(2026, 7, 3, 9, 0, 0, 0, time.UTC)
	second := first.Add(2 * time.Minute)

	stats.Record(context.Background(), coreusage.Record{
		APIKey:      "POST /v1/chat/completions",
		Model:       "gpt-5-mini",
		RequestedAt: first,
		Source:      "t:codex",
		AuthIndex:   "auth-alpha",
		Detail: coreusage.Detail{
			InputTokens:     100,
			OutputTokens:    25,
			ReasoningTokens: 5,
			CachedTokens:    40,
		},
	})
	stats.Record(context.Background(), coreusage.Record{
		APIKey:      "POST /v1/responses",
		Model:       "gpt-5-mini",
		RequestedAt: second,
		Source:      "t:codex",
		AuthIndex:   "auth-alpha",
		Failed:      true,
		Detail: coreusage.Detail{
			CachedTokens: 77,
		},
	})
	stats.Record(context.Background(), coreusage.Record{
		APIKey:      "POST /v1/chat/completions",
		Model:       "gpt-5-nano",
		RequestedAt: second,
		AuthIndex:   "auth-beta",
		Detail: coreusage.Detail{
			InputTokens: 1,
			TotalTokens: 1,
		},
	})

	snapshot := stats.Snapshot()
	authAlpha, ok := snapshot.Auths["auth-alpha"]
	if !ok {
		t.Fatalf("missing auth-alpha aggregation: %#v", snapshot.Auths)
	}
	if authAlpha.TotalRequests != 2 || authAlpha.SuccessCount != 1 || authAlpha.FailureCount != 1 {
		t.Fatalf("auth-alpha counts = %+v, want total=2 success=1 failure=1", authAlpha)
	}
	if authAlpha.Tokens.InputTokens != 100 || authAlpha.Tokens.OutputTokens != 25 || authAlpha.Tokens.ReasoningTokens != 5 || authAlpha.Tokens.CachedTokens != 117 {
		t.Fatalf("auth-alpha tokens = %+v, want input=100 output=25 reasoning=5 cached=117", authAlpha.Tokens)
	}
	if authAlpha.Tokens.TotalTokens != 202 {
		t.Fatalf("auth-alpha total_tokens = %d, want 202", authAlpha.Tokens.TotalTokens)
	}
	if authAlpha.FirstRequestAt == nil || !authAlpha.FirstRequestAt.Equal(first) {
		t.Fatalf("first_request_at = %v, want %v", authAlpha.FirstRequestAt, first)
	}
	if authAlpha.LastRequestAt == nil || !authAlpha.LastRequestAt.Equal(second) {
		t.Fatalf("last_request_at = %v, want %v", authAlpha.LastRequestAt, second)
	}
	model := authAlpha.Models["gpt-5-mini"]
	if model.TotalRequests != 2 || model.SuccessCount != 1 || model.FailureCount != 1 {
		t.Fatalf("auth-alpha model counts = %+v, want total=2 success=1 failure=1", model)
	}
	if model.Tokens.TotalTokens != 202 {
		t.Fatalf("auth-alpha model total_tokens = %d, want 202", model.Tokens.TotalTokens)
	}
	if _, ok := snapshot.Auths[""]; ok {
		t.Fatalf("empty auth_index should not be aggregated")
	}
}

func TestRequestStatisticsSnapshotAggregatesSplitCacheTokens(t *testing.T) {
	stats := NewRequestStatistics()
	ctx := logging.WithEndpoint(context.Background(), "POST /v1/messages")
	stats.Record(ctx, coreusage.Record{
		Model:     "claude-sonnet",
		Provider:  "claude",
		AuthIndex: "auth-claude",
		Detail: coreusage.Detail{
			InputTokens:         100,
			OutputTokens:        20,
			CachedTokens:        30,
			CacheReadTokens:     30,
			CacheCreationTokens: 20,
		},
	})

	snapshot := stats.Snapshot()
	authSnapshot := snapshot.Auths["auth-claude"]
	if authSnapshot.Tokens.CachedTokens != 50 {
		t.Fatalf("auth cached_tokens = %d, want cache read + creation (50)", authSnapshot.Tokens.CachedTokens)
	}
	if authSnapshot.Tokens.TotalTokens != 170 {
		t.Fatalf("auth total_tokens = %d, want Claude additive total (170)", authSnapshot.Tokens.TotalTokens)
	}
	detail := snapshot.APIs["POST /v1/messages"].Models["claude-sonnet"].Details[0]
	if detail.Tokens.CachedTokens != 50 {
		t.Fatalf("detail cached_tokens = %d, want cache read + creation (50)", detail.Tokens.CachedTokens)
	}
}

func TestRequestStatisticsDoesNotAddCachedTokensToTotalWhenMainCountsExist(t *testing.T) {
	stats := NewRequestStatistics()
	stats.Record(context.Background(), coreusage.Record{
		APIKey:    "test-key",
		Model:     "gpt-5-mini",
		AuthIndex: "auth-cached",
		Detail: coreusage.Detail{
			InputTokens:     10,
			OutputTokens:    20,
			ReasoningTokens: 3,
			CachedTokens:    1000,
		},
	})

	authSnapshot := stats.Snapshot().Auths["auth-cached"]
	if authSnapshot.Tokens.TotalTokens != 30 {
		t.Fatalf("auth total_tokens = %d, want input+output only (30)", authSnapshot.Tokens.TotalTokens)
	}
	detail := stats.Snapshot().APIs[redactedHash("test-key")].Models["gpt-5-mini"].Details[0]
	if detail.Tokens.TotalTokens != 30 {
		t.Fatalf("detail total_tokens = %d, want 30", detail.Tokens.TotalTokens)
	}
}

func TestRequestStatisticsMergeSnapshotDedupIgnoresLatency(t *testing.T) {
	stats := NewRequestStatistics()
	timestamp := time.Date(2026, 3, 20, 12, 0, 0, 0, time.UTC)
	first := StatisticsSnapshot{
		APIs: map[string]APISnapshot{
			"test-key": {
				Models: map[string]ModelSnapshot{
					"gpt-5.4": {
						Details: []RequestDetail{{
							RequestID: "req-1",
							Timestamp: timestamp,
							LatencyMs: 0,
							Source:    "user@example.com",
							AuthIndex: "0",
							Tokens: RequestTokenStats{
								InputTokens:  10,
								OutputTokens: 20,
								TotalTokens:  30,
							},
						}},
					},
				},
			},
		},
	}
	second := StatisticsSnapshot{
		APIs: map[string]APISnapshot{
			"test-key": {
				Models: map[string]ModelSnapshot{
					"gpt-5.4": {
						Details: []RequestDetail{{
							RequestID: "req-1",
							Timestamp: timestamp,
							LatencyMs: 2500,
							Source:    "user@example.com",
							AuthIndex: "0",
							Tokens: RequestTokenStats{
								InputTokens:  10,
								OutputTokens: 20,
								TotalTokens:  30,
							},
						}},
					},
				},
			},
		},
	}

	result := stats.MergeSnapshot(first)
	if result.Added != 1 || result.Skipped != 0 {
		t.Fatalf("first merge = %+v, want added=1 skipped=0", result)
	}

	result = stats.MergeSnapshot(second)
	if result.Added != 0 || result.Skipped != 1 {
		t.Fatalf("second merge = %+v, want added=0 skipped=1", result)
	}

	snapshot := stats.Snapshot()
	details := snapshot.APIs[redactedHash("test-key")].Models["gpt-5.4"].Details
	if len(details) != 1 {
		t.Fatalf("details len = %d, want 1", len(details))
	}
}

func TestRequestStatisticsMergeSnapshotEnrichesEmptyFactsWithoutRequestCount(t *testing.T) {
	stats := NewRequestStatistics()
	timestamp := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	emptySnapshot := StatisticsSnapshot{
		APIs: map[string]APISnapshot{
			"POST /v1/chat/completions": {
				Models: map[string]ModelSnapshot{
					"gpt-5": {
						Details: []RequestDetail{{
							RequestID:    "req-enrich",
							Timestamp:    timestamp,
							Endpoint:     "POST /v1/chat/completions",
							Model:        "gpt-5",
							Provider:     "openai",
							ExecutorType: "OpenAIExecutor",
							AuthIndex:    "auth-enrich",
							DetailRole:   DetailRolePrimary,
						}},
					},
				},
			},
		},
	}
	enrichedSnapshot := StatisticsSnapshot{
		APIs: map[string]APISnapshot{
			"POST /v1/chat/completions": {
				Models: map[string]ModelSnapshot{
					"gpt-5": {
						Details: []RequestDetail{{
							RequestID:    "req-enrich",
							Timestamp:    timestamp,
							Endpoint:     "POST /v1/chat/completions",
							Model:        "gpt-5",
							Provider:     "openai",
							ExecutorType: "OpenAIExecutor",
							AuthIndex:    "auth-enrich",
							DetailRole:   DetailRolePrimary,
							Tokens: RequestTokenStats{
								InputTokens:         3,
								OutputTokens:        4,
								ReportedTotalTokens: 7,
							},
						}},
					},
				},
			},
		},
	}

	result := stats.MergeSnapshot(emptySnapshot)
	if result.Added != 1 || result.Skipped != 0 || result.Enriched != 0 {
		t.Fatalf("empty merge = %+v, want added=1", result)
	}
	result = stats.MergeSnapshot(enrichedSnapshot)
	if result.Added != 0 || result.Skipped != 0 || result.Enriched != 1 {
		t.Fatalf("enriched merge = %+v, want enriched=1", result)
	}

	snapshot := stats.Snapshot()
	if snapshot.TotalRequests != 1 || snapshot.TotalTokens != 7 {
		t.Fatalf("snapshot totals = requests:%d tokens:%d, want 1/7", snapshot.TotalRequests, snapshot.TotalTokens)
	}
	if snapshot.TokensByDay["2026-07-09"] != 7 || snapshot.TokensByHour["12"] != 7 {
		t.Fatalf("token buckets = day:%v hour:%v, want 7", snapshot.TokensByDay, snapshot.TokensByHour)
	}
	model := snapshot.APIs["POST /v1/chat/completions"].Models["gpt-5"]
	if model.TotalRequests != 1 || model.TotalTokens != 7 || len(model.Details) != 1 {
		t.Fatalf("model snapshot = %+v, want one enriched detail with 7 tokens", model)
	}
	authSnapshot := snapshot.Auths["auth-enrich"]
	if authSnapshot.TotalRequests != 1 || authSnapshot.Tokens.TotalTokens != 7 {
		t.Fatalf("auth snapshot = %+v, want one enriched request with 7 tokens", authSnapshot)
	}

	result = stats.MergeSnapshot(enrichedSnapshot)
	if result.Added != 0 || result.Enriched != 0 || result.Skipped != 1 {
		t.Fatalf("repeat enriched merge = %+v, want skipped=1", result)
	}
}

func TestRequestStatisticsRecordEnrichesEmptyFactsWithoutRequestCount(t *testing.T) {
	stats := NewRequestStatistics()
	ctx := logging.WithRequestID(context.Background(), "req-live-enrich")
	ctx = logging.WithEndpoint(ctx, "POST /v1/chat/completions")
	requestedAt := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	base := coreusage.Record{
		Provider:     "openai",
		ExecutorType: "OpenAIExecutor",
		Model:        "gpt-5",
		AuthIndex:    "auth-live-enrich",
		RequestedAt:  requestedAt,
	}

	stats.Record(ctx, base)
	enriched := base
	enriched.Detail = coreusage.Detail{
		InputTokens:  2,
		OutputTokens: 3,
		TotalTokens:  5,
	}
	stats.Record(ctx, enriched)

	snapshot := stats.Snapshot()
	if snapshot.TotalRequests != 1 || snapshot.TotalTokens != 5 {
		t.Fatalf("snapshot totals = requests:%d tokens:%d, want 1/5", snapshot.TotalRequests, snapshot.TotalTokens)
	}
	model := snapshot.APIs["POST /v1/chat/completions"].Models["gpt-5"]
	if model.TotalRequests != 1 || model.TotalTokens != 5 || len(model.Details) != 1 {
		t.Fatalf("model snapshot = %+v, want enriched single request", model)
	}
	if snapshot.TokensByDay["2026-07-09"] != 5 || snapshot.TokensByHour["12"] != 5 {
		t.Fatalf("token buckets = day:%v hour:%v, want 5", snapshot.TokensByDay, snapshot.TokensByHour)
	}
}

func TestRequestStatisticsRecordEnrichUpdatesOutcomeCounts(t *testing.T) {
	stats := NewRequestStatistics()
	ctx := logging.WithRequestID(context.Background(), "req-outcome-enrich")
	ctx = logging.WithEndpoint(ctx, "POST /v1/chat/completions")
	requestedAt := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	base := coreusage.Record{
		Provider:     "openai",
		ExecutorType: "OpenAIExecutor",
		Model:        "gpt-5",
		AuthIndex:    "auth-outcome-enrich",
		RequestedAt:  requestedAt,
		Failed:       true,
	}

	stats.Record(ctx, base)
	enriched := base
	enriched.Failed = false
	enriched.Detail = coreusage.Detail{
		InputTokens:  2,
		OutputTokens: 3,
		TotalTokens:  5,
	}
	stats.Record(ctx, enriched)

	snapshot := stats.Snapshot()
	if snapshot.TotalRequests != 1 || snapshot.SuccessCount != 0 || snapshot.FailureCount != 1 {
		t.Fatalf("snapshot counts = total:%d success:%d failure:%d, want sticky failure 1/0/1", snapshot.TotalRequests, snapshot.SuccessCount, snapshot.FailureCount)
	}
	authSnapshot := snapshot.Auths["auth-outcome-enrich"]
	if authSnapshot.TotalRequests != 1 || authSnapshot.SuccessCount != 0 || authSnapshot.FailureCount != 1 {
		t.Fatalf("auth counts = %+v, want sticky failure", authSnapshot)
	}
}

func TestRequestStatisticsRecordEnrichesWithoutRequestIDDifferentLatencyAndOutcome(t *testing.T) {
	stats := NewRequestStatistics()
	ctx := logging.WithEndpoint(context.Background(), "POST /v1beta/interactions")
	ctx = logging.WithClientIP(ctx, "198.51.100.9")
	requestedAt := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	base := coreusage.Record{
		Provider:     "interactions",
		ExecutorType: "InteractionsExecutor",
		Model:        "gemini-2.5-pro",
		AuthIndex:    "auth-live-no-request-id",
		RequestedAt:  requestedAt,
		Latency:      100 * time.Millisecond,
		Failed:       true,
	}

	stats.Record(ctx, base)
	enriched := base
	enriched.Latency = 2500 * time.Millisecond
	enriched.Failed = false
	enriched.Detail = coreusage.Detail{
		InputTokens:  2,
		OutputTokens: 3,
		TotalTokens:  5,
	}
	stats.Record(ctx, enriched)

	snapshot := stats.Snapshot()
	if snapshot.TotalRequests != 1 || snapshot.SuccessCount != 0 || snapshot.FailureCount != 1 || snapshot.TotalTokens != 5 {
		t.Fatalf("snapshot = total:%d success:%d failure:%d tokens:%d, want sticky failure 1/0/1/5", snapshot.TotalRequests, snapshot.SuccessCount, snapshot.FailureCount, snapshot.TotalTokens)
	}
	model := snapshot.APIs["POST /v1beta/interactions"].Models["gemini-2.5-pro"]
	if model.TotalRequests != 1 || model.TotalTokens != 5 || len(model.Details) != 1 {
		t.Fatalf("model snapshot = %+v, want enriched single request", model)
	}
	if model.Details[0].LatencyMs != 2500 || !model.Details[0].Failed {
		t.Fatalf("detail = %+v, want enriched latency and sticky failure", model.Details[0])
	}
}

func TestRequestStatisticsKeepsSameRequestDifferentDetailRoles(t *testing.T) {
	stats := NewRequestStatistics()
	timestamp := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	snapshot := StatisticsSnapshot{
		APIs: map[string]APISnapshot{
			"POST /v1/responses": {
				Models: map[string]ModelSnapshot{
					"gpt-5": {
						Details: []RequestDetail{
							{
								RequestID:    "req-multi-role",
								Timestamp:    timestamp,
								Endpoint:     "POST /v1/responses",
								Model:        "gpt-5",
								Provider:     "openai",
								ExecutorType: "CodexExecutor",
								AuthIndex:    "auth-role",
								DetailRole:   DetailRolePrimary,
							},
							{
								RequestID:    "req-multi-role",
								Timestamp:    timestamp,
								Endpoint:     "POST /v1/responses",
								Model:        "gpt-5",
								Provider:     "openai",
								ExecutorType: "CodexExecutor",
								AuthIndex:    "auth-role",
								DetailRole:   DetailRoleAdditional,
								Tokens: RequestTokenStats{
									InputTokens:         2,
									OutputTokens:        3,
									ReportedTotalTokens: 5,
								},
							},
						},
					},
				},
			},
		},
	}

	result := stats.MergeSnapshot(snapshot)
	if result.Added != 2 || result.Skipped != 0 || result.Enriched != 0 {
		t.Fatalf("merge = %+v, want two distinct role details", result)
	}
	details := stats.Snapshot().APIs["POST /v1/responses"].Models["gpt-5"].Details
	if len(details) != 2 {
		t.Fatalf("details len = %d, want primary and additional", len(details))
	}
	roles := map[string]bool{}
	for _, detail := range details {
		roles[detail.DetailRole] = true
	}
	if !roles[DetailRolePrimary] || !roles[DetailRoleAdditional] {
		t.Fatalf("roles = %#v, want primary and additional", roles)
	}
}

func TestRequestStatisticsMergeSnapshotKeepsDistinctClientIPs(t *testing.T) {
	stats := NewRequestStatistics()
	timestamp := time.Date(2026, 3, 20, 12, 0, 0, 0, time.UTC)
	snapshot := StatisticsSnapshot{
		APIs: map[string]APISnapshot{
			"test-key": {
				Models: map[string]ModelSnapshot{
					"gpt-5.4": {
						Details: []RequestDetail{
							{
								Timestamp: timestamp,
								Source:    "user@example.com",
								ClientIP:  "198.51.100.1",
								AuthIndex: "0",
								Tokens: RequestTokenStats{
									InputTokens:  10,
									OutputTokens: 20,
									TotalTokens:  30,
								},
							},
							{
								Timestamp: timestamp,
								Source:    "user@example.com",
								ClientIP:  "198.51.100.2",
								AuthIndex: "0",
								Tokens: RequestTokenStats{
									InputTokens:  10,
									OutputTokens: 20,
									TotalTokens:  30,
								},
							},
						},
					},
				},
			},
		},
	}

	result := stats.MergeSnapshot(snapshot)
	if result.Added != 2 || result.Skipped != 0 {
		t.Fatalf("merge = %+v, want added=2 skipped=0", result)
	}

	details := stats.Snapshot().APIs[redactedHash("test-key")].Models["gpt-5.4"].Details
	if len(details) != 2 {
		t.Fatalf("details len = %d, want 2", len(details))
	}

	seenIPs := make(map[string]bool, len(details))
	for _, detail := range details {
		seenIPs[detail.ClientIP] = true
	}
	if !seenIPs["198.51.100.1"] || !seenIPs["198.51.100.2"] {
		t.Fatalf("details client_ip set = %#v, want both client IPs preserved", seenIPs)
	}
}

func TestRequestStatisticsDoesNotExposeRawAPIKeyInSnapshot(t *testing.T) {
	stats := NewRequestStatistics()
	stats.Record(context.Background(), coreusage.Record{
		APIKey:      "sk-live-raw-api-key",
		Provider:    "openai",
		Model:       "gpt-5",
		Source:      "sk-live-raw-source",
		RequestedAt: time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC),
		Detail: coreusage.Detail{
			InputTokens:  1,
			OutputTokens: 1,
			TotalTokens:  2,
		},
	})

	snapshot := stats.Snapshot()
	if _, ok := snapshot.APIs["sk-live-raw-api-key"]; ok {
		t.Fatalf("raw API key was used as APIs map key")
	}
	data, errMarshal := json.Marshal(snapshot)
	if errMarshal != nil {
		t.Fatalf("marshal snapshot: %v", errMarshal)
	}
	payload := string(data)
	for _, secret := range []string{"sk-live-raw-api-key", "sk-live-raw-source"} {
		if strings.Contains(payload, secret) {
			t.Fatalf("snapshot leaked %q: %s", secret, payload)
		}
	}
}

func TestSanitizeSensitiveTextRedactsGenericTokensAndPreservesTokenCounters(t *testing.T) {
	jsonPayload := `{"token":"raw-json-token","x-api-token":"raw-x-token","password":"raw-password","private_key":"raw-private-key","message":"token=raw-message-token","total_tokens":12,"input_tokens":2}`
	got := SanitizeSensitiveText(jsonPayload)
	if !json.Valid([]byte(got)) {
		t.Fatalf("sanitized JSON is invalid: %s", got)
	}
	for _, secret := range []string{"raw-json-token", "raw-x-token", "raw-password", "raw-private-key", "raw-message-token"} {
		if strings.Contains(got, secret) {
			t.Fatalf("sanitized JSON leaked %q: %s", secret, got)
		}
	}
	for _, nonSecret := range []string{"total_tokens", "input_tokens", "12"} {
		if !strings.Contains(got, nonSecret) {
			t.Fatalf("sanitized JSON = %s, want to preserve %q", got, nonSecret)
		}
	}

	textPayload := "Authorization: Basic raw-basic-token token=raw-form-token x-api-token=raw-header-token password=raw-text-password total_tokens=12"
	got = SanitizeSensitiveText(textPayload)
	for _, secret := range []string{"raw-basic-token", "raw-form-token", "raw-header-token", "raw-text-password"} {
		if strings.Contains(got, secret) {
			t.Fatalf("sanitized text leaked %q: %s", secret, got)
		}
	}
	if !strings.Contains(got, "total_tokens=12") {
		t.Fatalf("sanitized text = %s, want total_tokens counter preserved", got)
	}
}

func TestRequestStatisticsUsesClientIPSnapshotBeforeGinFallback(t *testing.T) {
	gin.SetMode(gin.TestMode)

	stats := NewRequestStatistics()
	recorder := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(recorder)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req.RemoteAddr = "203.0.113.99:12345"
	ginCtx.Request = req

	ctx := context.WithValue(context.Background(), "gin", ginCtx)
	ctx = logging.WithClientIP(ctx, "198.51.100.7")

	stats.Record(ctx, coreusage.Record{
		Model:       "gpt-5.4",
		RequestedAt: time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC),
		Detail: coreusage.Detail{
			TotalTokens: 1,
		},
	})

	details := stats.Snapshot().APIs["POST /v1/chat/completions"].Models["gpt-5.4"].Details
	if len(details) != 1 {
		t.Fatalf("details len = %d, want 1", len(details))
	}
	if details[0].ClientIP != "198.51.100.7" {
		t.Fatalf("client_ip = %q, want immutable snapshot", details[0].ClientIP)
	}
}

func TestRequestStatisticsKeepsAdditionalSameModelSequences(t *testing.T) {
	stats := NewRequestStatistics()
	requestedAt := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	ctx := logging.WithRequestID(context.Background(), "req-additional-sequences")
	ctx = logging.WithEndpoint(ctx, "POST /backend-api/codex/responses")
	ctx = logging.WithUsageDetailRole(ctx, DetailRoleAdditional)

	firstCtx := logging.WithUsageDetailSequence(ctx, "1")
	secondCtx := logging.WithUsageDetailSequence(ctx, "2")
	base := coreusage.Record{
		Provider:     "codex",
		ExecutorType: "CodexExecutor",
		Model:        "gpt-image-2",
		AuthIndex:    "auth-additional",
		RequestedAt:  requestedAt,
	}

	first := base
	first.Detail = coreusage.Detail{InputTokens: 2, OutputTokens: 3, TotalTokens: 5}
	second := base
	second.Detail = coreusage.Detail{InputTokens: 4, OutputTokens: 3, TotalTokens: 7}

	stats.Record(firstCtx, first)
	stats.Record(secondCtx, second)

	snapshot := stats.Snapshot()
	model := snapshot.APIs["POST /backend-api/codex/responses"].Models["gpt-image-2"]
	if model.TotalRequests != 2 || model.TotalTokens != 12 || len(model.Details) != 2 {
		t.Fatalf("model snapshot = %+v, want two additional details totaling 12 tokens", model)
	}
	sequences := map[string]bool{}
	for _, detail := range model.Details {
		sequences[detail.DetailSequence] = true
	}
	if !sequences["1"] || !sequences["2"] {
		t.Fatalf("detail sequences = %#v, want 1 and 2", sequences)
	}
}
