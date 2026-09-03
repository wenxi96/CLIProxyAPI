package management

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/usage"
	coreusage "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
)

func TestUsageCatalogReturnsLightweightFactsAndEmpty304(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stats := usage.NewRequestStatistics()
	detail := usage.RequestDetail{
		RequestID:    "req-catalog-handler",
		Timestamp:    time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC),
		Endpoint:     "POST /v1/responses",
		Model:        "gpt-5",
		Provider:     "openai",
		ExecutorType: "OpenAIExecutor",
		AuthIndex:    "auth-catalog",
		Source:       "auth-catalog",
		Tokens: usage.RequestTokenStats{
			InputTokens:      10,
			TotalTokens:      10,
			TokenUsageSource: usage.TokenUsageSourceProvider,
		},
	}
	if _, err := stats.MergeSnapshotWithError(usage.StatisticsSnapshot{APIs: map[string]usage.APISnapshot{
		detail.Endpoint: {Models: map[string]usage.ModelSnapshot{detail.Model: {Details: []usage.RequestDetail{detail}}}},
	}}); err != nil {
		t.Fatalf("MergeSnapshotWithError() error = %v", err)
	}

	h := &Handler{usageStats: stats, usageNow: func() time.Time {
		return time.Date(2026, 8, 10, 11, 0, 0, 0, time.UTC)
	}}
	router := gin.New()
	router.GET("/v0/management/usage/catalog", h.GetUsageCatalog)

	first := httptest.NewRecorder()
	router.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/v0/management/usage/catalog", nil))
	if first.Code != http.StatusOK {
		t.Fatalf("first status = %d body=%s", first.Code, first.Body.String())
	}
	if got := first.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
	etag := first.Header().Get("ETag")
	if !strings.HasPrefix(etag, `W/"`) || first.Header().Get("X-Usage-Revision") == "" {
		t.Fatalf("conditional headers = ETag:%q revision:%q", etag, first.Header().Get("X-Usage-Revision"))
	}
	var payload struct {
		SchemaVersion         int    `json:"schema_version"`
		BillablePolicyVersion string `json:"billable_policy_version"`
		SourceKeyAlgorithm    string `json:"source_key_algorithm"`
		Models                []struct {
			ID string `json:"id"`
		} `json:"models"`
		PriceKeys []struct {
			ID string `json:"id"`
		} `json:"price_keys"`
		Sources []struct {
			SourceID  string `json:"source_id"`
			SourceKey string `json:"source_key"`
		} `json:"sources"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode catalog: %v", err)
	}
	if payload.SchemaVersion != 1 || payload.BillablePolicyVersion != usage.BillablePolicyVersionV1 ||
		payload.SourceKeyAlgorithm != usage.UsageSourceKeyVersionV1 {
		t.Fatalf("catalog versions = %+v", payload)
	}
	if len(payload.Models) != 1 || payload.Models[0].ID != "gpt-5" || len(payload.PriceKeys) != 1 ||
		payload.PriceKeys[0].ID != "openai:gpt-5" || len(payload.Sources) != 1 ||
		payload.Sources[0].SourceID == "" || payload.Sources[0].SourceKey != "auth-catalog" {
		t.Fatalf("catalog facts = %+v", payload)
	}

	previousEnabled := usage.StatisticsEnabled()
	usage.SetStatisticsEnabled(true)
	t.Cleanup(func() { usage.SetStatisticsEnabled(previousEnabled) })
	if err := stats.RecordWithError(context.Background(), coreusage.Record{
		Provider:     detail.Provider,
		ExecutorType: detail.ExecutorType,
		Model:        detail.Model,
		AuthIndex:    detail.AuthIndex,
		Source:       detail.Source,
		RequestedAt:  detail.Timestamp.Add(time.Hour),
		ContextCarrier: &coreusage.RecordContextCarrier{
			RequestID: "req-catalog-append",
			Endpoint:  detail.Endpoint,
		},
		Detail: coreusage.Detail{InputTokens: 1, TotalTokens: 1},
	}); err != nil {
		t.Fatalf("RecordWithError() error = %v", err)
	}

	secondRequest := httptest.NewRequest(http.MethodGet, "/v0/management/usage/catalog", nil)
	secondRequest.Header.Set("If-None-Match", etag)
	second := httptest.NewRecorder()
	router.ServeHTTP(second, secondRequest)
	if second.Code != http.StatusNotModified || second.Body.Len() != 0 {
		t.Fatalf("conditional response = status:%d body:%q", second.Code, second.Body.String())
	}
	if second.Header().Get("Cache-Control") != "no-store" || second.Header().Get("ETag") != etag {
		t.Fatalf("304 headers = %+v", second.Header())
	}
}
