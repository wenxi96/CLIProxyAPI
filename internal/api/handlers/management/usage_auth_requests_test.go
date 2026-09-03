package management

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/logging"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/usage"
	coreusage "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
)

type usageAuthCursorPublicItemV1 struct {
	SchemaVersion int       `json:"schema_version"`
	RecordType    string    `json:"record_type"`
	Timestamp     time.Time `json:"timestamp"`
}

type usageAuthCursorStatisticsStub struct {
	datasetEpoch    uint64
	revision        uint64
	rewriteRevision uint64
	page            usage.ProjectionSnapshot
	details         map[string]usage.RequestDetail
}

func (s *usageAuthCursorStatisticsStub) Snapshot() usage.StatisticsSnapshot {
	return usage.StatisticsSnapshot{}
}

func (s *usageAuthCursorStatisticsStub) ListAuthRequestsWithError(string, usage.AuthRequestFilter) (usage.AuthRequestPage, error) {
	return usage.AuthRequestPage{}, usage.ErrProjectionUnavailable
}

func (s *usageAuthCursorStatisticsStub) MergeSnapshotWithError(usage.StatisticsSnapshot) (usage.MergeResult, error) {
	return usage.MergeResult{}, usage.ErrProjectionUnavailable
}

func (s *usageAuthCursorStatisticsStub) QueryProjectionAuthUsage() (map[string]usage.AuthUsageSnapshot, error) {
	return nil, usage.ErrProjectionUnavailable
}

func (s *usageAuthCursorStatisticsStub) ProjectionMetadata() (uint64, uint64, uint64, error) {
	return s.datasetEpoch, s.revision, s.rewriteRevision, nil
}

func (s *usageAuthCursorStatisticsStub) QueryProjectionEvents(
	time.Time,
	time.Time,
	int,
	uint64,
	uint64,
	map[string][]string,
) (usage.ProjectionSnapshot, bool, uint64, uint64, string, error) {
	return s.page, false, 0, s.page.Events[0].Sequence, "auth-posting", nil
}

func (s *usageAuthCursorStatisticsStub) LookupEventDetails([]string) (map[string]usage.RequestDetail, error) {
	return s.details, nil
}

func (s *usageAuthCursorStatisticsStub) QueryProjectionCatalog() (usage.ProjectionSnapshot, error) {
	return usage.ProjectionSnapshot{}, usage.ErrProjectionUnavailable
}

func (s *usageAuthCursorStatisticsStub) QueryProjectionSummaryView(time.Time, time.Time, time.Time) (usage.ProjectionSummaryView, error) {
	return usage.ProjectionSummaryView{}, usage.ErrProjectionUnavailable
}

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

func TestGetUsageAuthRequestsCursorReturnsProjectionUnavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &Handler{}
	router := gin.New()
	router.GET("/v0/management/usage/auths/:auth_index/requests", h.GetUsageAuthRequests)

	record := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v0/management/usage/auths/auth-index/requests?pagination_mode=cursor_v1", nil)
	router.ServeHTTP(record, req)
	if record.Code != http.StatusServiceUnavailable || !strings.Contains(record.Body.String(), "projection_unavailable") {
		t.Fatalf("cursor unavailable response = status:%d body:%s", record.Code, record.Body.String())
	}
}

func TestGetUsageAuthRequestsCursorUsesVersionedPublicItems(t *testing.T) {
	gin.SetMode(gin.TestMode)
	authIndex := "auth-cursor-public"
	base := time.Date(2026, 8, 11, 9, 0, 0, 0, time.UTC)
	generate := true
	estimatedCost := 1.25
	detail := usage.RequestDetail{
		RequestID:        "req-auth-cursor-public",
		ClientIP:         "198.51.100.7:443",
		Timestamp:        base,
		Endpoint:         "POST /v1/responses?must_not_leak=yes",
		Model:            "gpt-5",
		Provider:         "openai",
		ExecutorType:     "OpenAIExecutor",
		AuthType:         "api_key",
		ModelAlias:       "gpt-5-public",
		Source:           "raw-secret-must-not-leak",
		AuthIndex:        authIndex,
		DetailRole:       usage.DetailRolePrimary,
		DetailSequence:   "1",
		Generate:         &generate,
		EstimatedCostUSD: &estimatedCost,
		Tokens: usage.RequestTokenStats{
			InputTokens:      4,
			TotalTokens:      4,
			TokenUsageSource: usage.TokenUsageSourceProvider,
		},
	}
	stats := usage.NewRequestStatistics()
	if _, err := stats.MergeSnapshotWithError(usage.StatisticsSnapshot{APIs: map[string]usage.APISnapshot{
		detail.Endpoint: {Models: map[string]usage.ModelSnapshot{
			detail.Model: {Details: []usage.RequestDetail{detail}},
		}},
	}}); err != nil {
		t.Fatalf("MergeSnapshotWithError() error = %v", err)
	}

	h := &Handler{usageStats: stats, usageNow: func() time.Time { return base.Add(time.Hour) }}
	router := gin.New()
	router.GET("/v0/management/usage/auths/:auth_index/requests", h.GetUsageAuthRequests)
	path := "/v0/management/usage/auths/" + url.PathEscape(authIndex) + "/requests?" + url.Values{
		"pagination_mode": {"cursor_v1"},
		"from":            {base.Add(-time.Hour).Format(time.RFC3339Nano)},
		"to":              {base.Add(time.Hour).Format(time.RFC3339Nano)},
	}.Encode()
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
	var payload struct {
		PaginationMode string           `json:"pagination_mode"`
		Items          []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode cursor response: %v", err)
	}
	if payload.PaginationMode != "cursor_v1" || len(payload.Items) != 1 {
		t.Fatalf("cursor payload = %+v", payload)
	}
	item := payload.Items[0]
	if item["schema_version"] != float64(1) || item["record_type"] != "auth_request" ||
		item["source"] != authIndex || item["source_key"] != authIndex || item["source_id"] == "" ||
		item["endpoint"] != "POST /v1/responses" {
		t.Fatalf("versioned auth cursor item = %+v", item)
	}
	for _, forbidden := range []string{"stable_event_id", "sequence", "batch_ordinal", "canonical_event_identity"} {
		if _, exists := item[forbidden]; exists {
			t.Fatalf("auth cursor leaked %q: %+v", forbidden, item)
		}
	}
	if strings.Contains(response.Body.String(), "raw-secret-must-not-leak") ||
		strings.Contains(response.Body.String(), "must_not_leak") {
		t.Fatalf("auth cursor leaked raw source or endpoint query: %s", response.Body.String())
	}
}

func TestGetUsageAuthRequestsCursorRejectsProjectionDetailSourceMismatch(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const authIndex = "auth-cursor-source-mismatch"
	base := time.Date(2026, 8, 11, 10, 0, 0, 0, time.UTC)
	detail := usage.RequestDetail{
		RequestID: "req-auth-cursor-source-mismatch", Timestamp: base, Endpoint: "POST /v1/responses",
		Model: "gpt-5", Provider: "openai", AuthType: "api_key", ExecutorType: "OpenAIExecutor",
		AuthIndex: authIndex, Source: authIndex, DetailRole: usage.DetailRolePrimary,
	}
	stub := &usageAuthCursorStatisticsStub{
		datasetEpoch: 1,
		revision:     1,
		page: usage.ProjectionSnapshot{
			DatasetEpoch: 1,
			Revision:     1,
			Events: []usage.EventRef{{
				StableEventID: "event-mismatch", Sequence: 1, Timestamp: base, AuthIndex: authIndex,
				SourceID: "source:v1:mismatched",
			}},
		},
		details: map[string]usage.RequestDetail{"event-mismatch": detail},
	}
	h := &Handler{usageStats: stub, usageNow: func() time.Time { return base.Add(time.Hour) }}
	router := gin.New()
	router.GET("/v0/management/usage/auths/:auth_index/requests", h.GetUsageAuthRequests)
	path := "/v0/management/usage/auths/" + url.PathEscape(authIndex) + "/requests?" + url.Values{
		"pagination_mode": {"cursor_v1"},
		"from":            {base.Add(-time.Hour).Format(time.RFC3339Nano)},
		"to":              {base.Add(time.Hour).Format(time.RFC3339Nano)},
	}.Encode()
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
	if response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), "projection_unavailable") {
		t.Fatalf("mismatched auth cursor response = status:%d body:%s", response.Code, response.Body.String())
	}
}

func TestGetUsageAuthRequestsSupportsExclusiveOffsetAndCursorV1(t *testing.T) {
	gin.SetMode(gin.TestMode)
	authIndex := "Auth-Cursor_123.~"
	stats := usage.NewRequestStatistics()
	base := time.Date(2026, 8, 10, 16, 0, 0, 0, time.UTC)
	requestContext := logging.WithEndpoint(context.Background(), "POST /v1/responses")
	for index, timestamp := range []time.Time{base, base.Add(time.Minute)} {
		stats.Record(requestContext, coreusage.Record{
			Model:       "gpt-5",
			RequestedAt: timestamp,
			AuthIndex:   authIndex,
			Source:      authIndex,
			Detail:      coreusage.Detail{InputTokens: int64(index + 1)},
		})
	}
	h := &Handler{usageStats: stats, usageNow: func() time.Time { return base.Add(time.Hour) }}
	router := gin.New()
	router.GET("/v0/management/usage/auths/:auth_index/requests", h.GetUsageAuthRequests)
	basePath := "/v0/management/usage/auths/" + url.PathEscape(authIndex) + "/requests"

	inclusive := httptest.NewRecorder()
	router.ServeHTTP(inclusive, httptest.NewRequest(http.MethodGet, basePath+"?to="+url.QueryEscape(base.Format(time.RFC3339Nano)), nil))
	if inclusive.Code != http.StatusOK {
		t.Fatalf("inclusive status = %d body=%s", inclusive.Code, inclusive.Body.String())
	}
	var inclusivePage usage.AuthRequestPage
	if err := json.Unmarshal(inclusive.Body.Bytes(), &inclusivePage); err != nil {
		t.Fatalf("decode inclusive page: %v", err)
	}
	if inclusivePage.Total != 1 {
		t.Fatalf("inclusive total = %d, want 1", inclusivePage.Total)
	}

	exclusive := httptest.NewRecorder()
	router.ServeHTTP(exclusive, httptest.NewRequest(http.MethodGet, basePath+"?to="+url.QueryEscape(base.Format(time.RFC3339Nano))+"&to_exclusive=true", nil))
	if exclusive.Code != http.StatusOK {
		t.Fatalf("exclusive status = %d body=%s", exclusive.Code, exclusive.Body.String())
	}
	var exclusivePage struct {
		PaginationMode string                `json:"pagination_mode"`
		Total          int                   `json:"total"`
		Items          []usage.RequestDetail `json:"items"`
	}
	if err := json.Unmarshal(exclusive.Body.Bytes(), &exclusivePage); err != nil {
		t.Fatalf("decode exclusive page: %v", err)
	}
	if exclusivePage.PaginationMode != "offset_v1_exclusive" || exclusivePage.Total != 0 || len(exclusivePage.Items) != 0 {
		t.Fatalf("exclusive page = %+v", exclusivePage)
	}

	query := url.Values{
		"pagination_mode": {"cursor_v1"},
		"from":            {base.Add(-time.Hour).Format(time.RFC3339Nano)},
		"to":              {base.Add(time.Hour).Format(time.RFC3339Nano)},
		"limit":           {"1"},
	}
	first := httptest.NewRecorder()
	router.ServeHTTP(first, httptest.NewRequest(http.MethodGet, basePath+"?"+query.Encode(), nil))
	if first.Code != http.StatusOK {
		t.Fatalf("cursor first status = %d body=%s", first.Code, first.Body.String())
	}
	var firstCursorPage struct {
		PaginationMode string                        `json:"pagination_mode"`
		Items          []usageAuthCursorPublicItemV1 `json:"items"`
		NextCursor     string                        `json:"next_cursor"`
		HasMore        bool                          `json:"has_more"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &firstCursorPage); err != nil {
		t.Fatalf("decode cursor first page: %v", err)
	}
	if firstCursorPage.PaginationMode != "cursor_v1" || len(firstCursorPage.Items) != 1 ||
		firstCursorPage.Items[0].SchemaVersion != 1 || firstCursorPage.Items[0].RecordType != "auth_request" ||
		!firstCursorPage.Items[0].Timestamp.Equal(base.Add(time.Minute)) || firstCursorPage.NextCursor == "" || !firstCursorPage.HasMore {
		t.Fatalf("cursor first page = %+v", firstCursorPage)
	}
	query.Set("cursor", firstCursorPage.NextCursor)
	second := httptest.NewRecorder()
	router.ServeHTTP(second, httptest.NewRequest(http.MethodGet, basePath+"?"+query.Encode(), nil))
	if second.Code != http.StatusOK {
		t.Fatalf("cursor second status = %d body=%s", second.Code, second.Body.String())
	}
	var secondCursorPage struct {
		Items   []usageAuthCursorPublicItemV1 `json:"items"`
		HasMore bool                          `json:"has_more"`
	}
	if err := json.Unmarshal(second.Body.Bytes(), &secondCursorPage); err != nil {
		t.Fatalf("decode cursor second page: %v", err)
	}
	if len(secondCursorPage.Items) != 1 || secondCursorPage.Items[0].SchemaVersion != 1 ||
		secondCursorPage.Items[0].RecordType != "auth_request" || !secondCursorPage.Items[0].Timestamp.Equal(base) || secondCursorPage.HasMore {
		t.Fatalf("cursor second page = %+v", secondCursorPage)
	}

	mixed := httptest.NewRecorder()
	router.ServeHTTP(mixed, httptest.NewRequest(http.MethodGet, basePath+"?pagination_mode=cursor_v1&offset=1", nil))
	if mixed.Code != http.StatusBadRequest {
		t.Fatalf("mixed mode status = %d body=%s", mixed.Code, mixed.Body.String())
	}

	staleRewriteCursor, err := h.usageTokenCodec.decodeAuthCursor(firstCursorPage.NextCursor)
	if err != nil {
		t.Fatalf("decode auth cursor for rewrite expiry: %v", err)
	}
	staleRewriteCursor.RewriteRevision++
	staleRewriteToken, err := h.usageTokenCodec.encodeAuthCursor(staleRewriteCursor)
	if err != nil {
		t.Fatalf("encode stale auth rewrite cursor: %v", err)
	}
	staleRewriteQuery := query
	staleRewriteQuery.Set("cursor", staleRewriteToken)
	staleRewrite := httptest.NewRecorder()
	router.ServeHTTP(staleRewrite, httptest.NewRequest(http.MethodGet, basePath+"?"+staleRewriteQuery.Encode(), nil))
	if staleRewrite.Code != http.StatusConflict || !strings.Contains(staleRewrite.Body.String(), "cursor_expired") {
		t.Fatalf("stale auth rewrite response = status:%d body:%s", staleRewrite.Code, staleRewrite.Body.String())
	}

	staleEpochCursor, err := h.usageTokenCodec.decodeAuthCursor(firstCursorPage.NextCursor)
	if err != nil {
		t.Fatalf("decode auth cursor for epoch expiry: %v", err)
	}
	staleEpochCursor.DatasetEpoch++
	staleEpochToken, err := h.usageTokenCodec.encodeAuthCursor(staleEpochCursor)
	if err != nil {
		t.Fatalf("encode stale auth epoch cursor: %v", err)
	}
	staleEpochQuery := query
	staleEpochQuery.Set("cursor", staleEpochToken)
	staleEpoch := httptest.NewRecorder()
	router.ServeHTTP(staleEpoch, httptest.NewRequest(http.MethodGet, basePath+"?"+staleEpochQuery.Encode(), nil))
	if staleEpoch.Code != http.StatusGone || !strings.Contains(staleEpoch.Body.String(), "dataset_epoch_gone") {
		t.Fatalf("stale auth epoch response = status:%d body:%s", staleEpoch.Code, staleEpoch.Body.String())
	}
}
