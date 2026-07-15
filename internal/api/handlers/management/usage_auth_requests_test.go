package management

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/logging"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/usage"
	coreusage "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
)

func TestGetUsageAuthRequestsFiltersAndPaginates(t *testing.T) {
	gin.SetMode(gin.TestMode)

	authIndex := "auth-index_123.~"
	stats := usage.NewRequestStatistics()
	base := time.Date(2026, 7, 3, 10, 0, 0, 0, time.UTC)
	chatContext := logging.WithEndpoint(context.Background(), "POST /v1/chat/completions")
	responsesContext := logging.WithEndpoint(context.Background(), "POST /v1/responses")
	stats.Record(chatContext, coreusage.Record{
		Model:       "gpt-5-mini",
		RequestedAt: base,
		Latency:     1200 * time.Millisecond,
		Source:      "t:codex",
		AuthIndex:   authIndex,
		Detail: coreusage.Detail{
			InputTokens:     3,
			OutputTokens:    4,
			ReasoningTokens: 1,
			CachedTokens:    100,
		},
	})
	stats.Record(chatContext, coreusage.Record{
		Model:       "gpt-5-mini",
		RequestedAt: base.Add(time.Minute),
		Failed:      true,
		AuthIndex:   authIndex,
		Detail: coreusage.Detail{
			TotalTokens: 15,
		},
	})
	stats.Record(responsesContext, coreusage.Record{
		Model:       "gpt-5-mini",
		RequestedAt: base.Add(2 * time.Minute),
		AuthIndex:   authIndex,
		Detail: coreusage.Detail{
			TotalTokens: 5,
		},
	})
	stats.Record(responsesContext, coreusage.Record{
		Model:       "gpt-5-nano",
		RequestedAt: base.Add(3 * time.Minute),
		AuthIndex:   authIndex,
		Detail: coreusage.Detail{
			TotalTokens: 2,
		},
	})

	h := &Handler{usageStats: stats}
	router := gin.New()
	router.GET("/v0/management/usage/auths/:auth_index/requests", h.GetUsageAuthRequests)

	query := url.Values{}
	query.Set("limit", "1")
	query.Set("offset", "1")
	query.Set("model", "gpt-5-mini")
	query.Set("failed", "false")
	query.Set("from", base.Format(time.RFC3339))
	query.Set("to", base.Add(2*time.Minute).Format(time.RFC3339))
	req := httptest.NewRequest(http.MethodGet, "/v0/management/usage/auths/"+url.PathEscape(authIndex)+"/requests?"+query.Encode(), nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var payload usage.AuthRequestPage
	if errUnmarshal := json.Unmarshal(rec.Body.Bytes(), &payload); errUnmarshal != nil {
		t.Fatalf("unmarshal response: %v", errUnmarshal)
	}
	if payload.AuthIndex != authIndex {
		t.Fatalf("auth_index = %q, want %q", payload.AuthIndex, authIndex)
	}
	if payload.Total != 2 || payload.Limit != 1 || payload.Offset != 1 {
		t.Fatalf("page = total:%d limit:%d offset:%d, want 2/1/1", payload.Total, payload.Limit, payload.Offset)
	}
	if len(payload.Items) != 1 {
		t.Fatalf("items len = %d, want 1", len(payload.Items))
	}
	item := payload.Items[0]
	if !item.Timestamp.Equal(base) {
		t.Fatalf("item timestamp = %v, want %v", item.Timestamp, base)
	}
	if item.Endpoint != "POST /v1/chat/completions" || item.Model != "gpt-5-mini" || item.Failed {
		t.Fatalf("item = %+v, want chat completions gpt-5-mini success", item)
	}
	if item.ModelAlias != "gpt-5-mini" || item.DetailRole != usage.DetailRolePrimary {
		t.Fatalf("item model_alias/detail_role = %q/%q, want model alias and primary role", item.ModelAlias, item.DetailRole)
	}
	if item.EstimatedCostUSD != nil {
		t.Fatalf("estimated_cost_usd = %v, want nil", *item.EstimatedCostUSD)
	}
	if item.LatencyMs != 1200 {
		t.Fatalf("latency_ms = %d, want 1200", item.LatencyMs)
	}
	if item.Tokens.TotalTokens != 7 {
		t.Fatalf("total_tokens = %d, want 7 without cached-token or reasoning double counting", item.Tokens.TotalTokens)
	}
	if item.Tokens.ComputedTotalTokens != 7 || item.Tokens.TokenUsageSource != usage.TokenUsageSourceProvider {
		t.Fatalf("tokens = %+v, want computed total and provider usage source", item.Tokens)
	}
}

func TestGetUsageAuthRequestsRejectsInvalidFilter(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h := &Handler{usageStats: usage.NewRequestStatistics()}
	router := gin.New()
	router.GET("/v0/management/usage/auths/:auth_index/requests", h.GetUsageAuthRequests)

	req := httptest.NewRequest(http.MethodGet, "/v0/management/usage/auths/auth-index/requests?failed=maybe", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}
