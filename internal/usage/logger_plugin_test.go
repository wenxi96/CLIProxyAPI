package usage

import (
	"context"
	"net/http"
	"net/http/httptest"
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
	details := snapshot.APIs["test-key"].Models["gpt-5.4"].Details
	if len(details) != 1 {
		t.Fatalf("details len = %d, want 1", len(details))
	}
	if details[0].LatencyMs != 1500 {
		t.Fatalf("latency_ms = %d, want 1500", details[0].LatencyMs)
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
			details := snapshot.APIs["test-key"].Models["gpt-5.4"].Details
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
	details := snapshot.APIs["test-key"].Models["gpt-5.4"].Details
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
	if authAlpha.Tokens.TotalTokens != 207 {
		t.Fatalf("auth-alpha total_tokens = %d, want 207", authAlpha.Tokens.TotalTokens)
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
	if model.Tokens.TotalTokens != 207 {
		t.Fatalf("auth-alpha model total_tokens = %d, want 207", model.Tokens.TotalTokens)
	}
	if _, ok := snapshot.Auths[""]; ok {
		t.Fatalf("empty auth_index should not be aggregated")
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
	if authSnapshot.Tokens.TotalTokens != 33 {
		t.Fatalf("auth total_tokens = %d, want input+output+reasoning only (33)", authSnapshot.Tokens.TotalTokens)
	}
	detail := stats.Snapshot().APIs["test-key"].Models["gpt-5-mini"].Details[0]
	if detail.Tokens.TotalTokens != 33 {
		t.Fatalf("detail total_tokens = %d, want 33", detail.Tokens.TotalTokens)
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
							Timestamp: timestamp,
							LatencyMs: 0,
							Source:    "user@example.com",
							AuthIndex: "0",
							Tokens: TokenStats{
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
							Timestamp: timestamp,
							LatencyMs: 2500,
							Source:    "user@example.com",
							AuthIndex: "0",
							Tokens: TokenStats{
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
	details := snapshot.APIs["test-key"].Models["gpt-5.4"].Details
	if len(details) != 1 {
		t.Fatalf("details len = %d, want 1", len(details))
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
								Tokens: TokenStats{
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
								Tokens: TokenStats{
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

	details := stats.Snapshot().APIs["test-key"].Models["gpt-5.4"].Details
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
