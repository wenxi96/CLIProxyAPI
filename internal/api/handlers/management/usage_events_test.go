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
	"github.com/router-for-me/CLIProxyAPI/v7/internal/usage"
	coreusage "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
)

func TestUsageEventsAbsoluteCursorIsOpaqueStableAndConditional(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stats := usage.NewRequestStatistics()
	base := time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)
	first := usage.RequestDetail{
		RequestID:    "req-events-http-first",
		ClientIP:     "127.0.0.1",
		Timestamp:    base,
		Endpoint:     "POST /v1/responses?must_not_leak=yes",
		Model:        "gpt-first",
		Provider:     "openai",
		ExecutorType: "OpenAIExecutor",
		AuthType:     "api_key",
		AuthIndex:    "auth-events",
		Source:       "raw-secret-must-not-leak",
		Tokens: usage.RequestTokenStats{
			InputTokens:      10,
			TotalTokens:      10,
			TokenUsageSource: usage.TokenUsageSourceProvider,
		},
	}
	second := first
	second.RequestID = "req-events-http-second"
	second.Timestamp = base.Add(time.Minute)
	second.Model = "gpt-second"
	second.AuthIndex = "auth-events-second"
	second.Source = "raw-second-source"
	second.Tokens = usage.RequestTokenStats{OutputTokens: 20, TotalTokens: 20, TokenUsageSource: usage.TokenUsageSourceProvider}
	if _, err := stats.MergeSnapshotWithError(usage.StatisticsSnapshot{APIs: map[string]usage.APISnapshot{
		"POST /v1/responses": {Models: map[string]usage.ModelSnapshot{
			first.Model:  {Details: []usage.RequestDetail{first}},
			second.Model: {Details: []usage.RequestDetail{second}},
		}},
	}}); err != nil {
		t.Fatalf("MergeSnapshotWithError() error = %v", err)
	}

	codec, err := newUsageTokenCodecWithKey([]byte("unit-test-usage-token-key-32-bytes"))
	if err != nil {
		t.Fatalf("newUsageTokenCodecWithKey() error = %v", err)
	}
	now := base.Add(2 * time.Hour)
	h := &Handler{
		usageStats:         stats,
		usageNow:           func() time.Time { return now },
		usageTokenCodec:    codec,
		usageWindowAnchors: newUsageWindowAnchorManager(codec, func() time.Time { return now }, nil),
	}
	router := gin.New()
	router.GET("/v0/management/usage/events", h.GetUsageEvents)

	sourceID := usage.UsageSourceIDV1(first)
	secondSourceID := usage.UsageSourceIDV1(second)
	query := url.Values{
		"from":   {base.Add(-time.Hour).Format(time.RFC3339Nano)},
		"to":     {base.Add(time.Hour).Format(time.RFC3339Nano)},
		"limit":  {"1"},
		"source": {sourceID, secondSourceID, sourceID},
	}
	path := "/v0/management/usage/events?" + query.Encode()
	firstResponse := httptest.NewRecorder()
	router.ServeHTTP(firstResponse, httptest.NewRequest(http.MethodGet, path, nil))
	if firstResponse.Code != http.StatusOK {
		t.Fatalf("first status = %d body=%s", firstResponse.Code, firstResponse.Body.String())
	}
	if firstResponse.Header().Get("Cache-Control") != "no-store" || firstResponse.Header().Get("ETag") == "" {
		t.Fatalf("first conditional headers = %+v", firstResponse.Header())
	}
	var firstPage struct {
		Items      []map[string]any `json:"items"`
		NextCursor string           `json:"next_cursor"`
		HasMore    bool             `json:"has_more"`
		Snapshot   struct {
			DatasetEpoch    string `json:"dataset_epoch"`
			MaxSequence     uint64 `json:"max_sequence"`
			RewriteRevision uint64 `json:"rewrite_revision"`
		} `json:"snapshot"`
	}
	if err := json.Unmarshal(firstResponse.Body.Bytes(), &firstPage); err != nil {
		t.Fatalf("decode first page: %v", err)
	}
	if len(firstPage.Items) != 1 || firstPage.Items[0]["request_id"] != second.RequestID || !firstPage.HasMore ||
		firstPage.NextCursor == "" || firstPage.Snapshot.DatasetEpoch == "" || firstPage.Snapshot.MaxSequence == 0 {
		t.Fatalf("first page = %+v", firstPage)
	}
	for _, forbidden := range []string{"stable_event_id", "sequence", "batch_ordinal", "canonical_event_identity", "source"} {
		if _, exists := firstPage.Items[0][forbidden]; exists {
			t.Fatalf("public event leaked %q: %+v", forbidden, firstPage.Items[0])
		}
	}
	if strings.Contains(firstResponse.Body.String(), "raw-secret-must-not-leak") ||
		strings.Contains(firstResponse.Body.String(), "must_not_leak") {
		t.Fatalf("public event leaked raw source or endpoint query: %s", firstResponse.Body.String())
	}

	secondQuery := url.Values{
		"from":   query["from"],
		"to":     query["to"],
		"limit":  {"1"},
		"source": query["source"],
		"cursor": {firstPage.NextCursor},
	}
	secondResponse := httptest.NewRecorder()
	router.ServeHTTP(secondResponse, httptest.NewRequest(http.MethodGet, "/v0/management/usage/events?"+secondQuery.Encode(), nil))
	if secondResponse.Code != http.StatusOK {
		t.Fatalf("second status = %d body=%s", secondResponse.Code, secondResponse.Body.String())
	}
	var secondPage struct {
		Items   []map[string]any `json:"items"`
		HasMore bool             `json:"has_more"`
	}
	if err := json.Unmarshal(secondResponse.Body.Bytes(), &secondPage); err != nil {
		t.Fatalf("decode second page: %v", err)
	}
	if len(secondPage.Items) != 1 || secondPage.Items[0]["request_id"] != first.RequestID || secondPage.HasMore {
		t.Fatalf("second page = %+v", secondPage)
	}

	conditional := httptest.NewRequest(http.MethodGet, path, nil)
	conditional.Header.Set("If-None-Match", firstResponse.Header().Get("ETag"))
	conditionalResponse := httptest.NewRecorder()
	router.ServeHTTP(conditionalResponse, conditional)
	if conditionalResponse.Code != http.StatusNotModified || conditionalResponse.Body.Len() != 0 {
		t.Fatalf("conditional response = status:%d body:%q", conditionalResponse.Code, conditionalResponse.Body.String())
	}

	tamperedQuery := secondQuery
	tamperedIndex := len(firstPage.NextCursor) / 2
	tamperedChar := byte('A')
	if firstPage.NextCursor[tamperedIndex] == tamperedChar {
		tamperedChar = 'B'
	}
	tamperedToken := firstPage.NextCursor[:tamperedIndex] +
		string(tamperedChar) +
		firstPage.NextCursor[tamperedIndex+1:]
	tamperedQuery.Set("cursor", tamperedToken)
	tamperedResponse := httptest.NewRecorder()
	router.ServeHTTP(tamperedResponse, httptest.NewRequest(http.MethodGet, "/v0/management/usage/events?"+tamperedQuery.Encode(), nil))
	if tamperedResponse.Code != http.StatusBadRequest || !strings.Contains(tamperedResponse.Body.String(), "invalid_cursor") {
		t.Fatalf("tampered response = status:%d body:%s", tamperedResponse.Code, tamperedResponse.Body.String())
	}

	staleRewriteCursor, err := h.usageTokenCodec.decodeEventsCursor(firstPage.NextCursor)
	if err != nil {
		t.Fatalf("decode cursor for rewrite expiry: %v", err)
	}
	staleRewriteCursor.RewriteRevision++
	staleRewriteToken, err := h.usageTokenCodec.encodeEventsCursor(staleRewriteCursor)
	if err != nil {
		t.Fatalf("encode stale rewrite cursor: %v", err)
	}
	staleRewriteQuery := secondQuery
	staleRewriteQuery.Set("cursor", staleRewriteToken)
	staleRewriteResponse := httptest.NewRecorder()
	router.ServeHTTP(staleRewriteResponse, httptest.NewRequest(http.MethodGet, "/v0/management/usage/events?"+staleRewriteQuery.Encode(), nil))
	if staleRewriteResponse.Code != http.StatusConflict || !strings.Contains(staleRewriteResponse.Body.String(), "cursor_expired") {
		t.Fatalf("stale rewrite response = status:%d body:%s", staleRewriteResponse.Code, staleRewriteResponse.Body.String())
	}

	staleEpochCursor, err := h.usageTokenCodec.decodeEventsCursor(firstPage.NextCursor)
	if err != nil {
		t.Fatalf("decode cursor for epoch expiry: %v", err)
	}
	staleEpochCursor.DatasetEpoch++
	staleEpochToken, err := h.usageTokenCodec.encodeEventsCursor(staleEpochCursor)
	if err != nil {
		t.Fatalf("encode stale epoch cursor: %v", err)
	}
	staleEpochQuery := secondQuery
	staleEpochQuery.Set("cursor", staleEpochToken)
	staleEpochResponse := httptest.NewRecorder()
	router.ServeHTTP(staleEpochResponse, httptest.NewRequest(http.MethodGet, "/v0/management/usage/events?"+staleEpochQuery.Encode(), nil))
	if staleEpochResponse.Code != http.StatusGone || !strings.Contains(staleEpochResponse.Body.String(), "dataset_epoch_gone") {
		t.Fatalf("stale epoch response = status:%d body:%s", staleEpochResponse.Code, staleEpochResponse.Body.String())
	}
}

func TestSafeUsageEventClientIPNormalizesAndRejectsUnsafeValues(t *testing.T) {
	if got := safeUsageEventClientIP(" 2001:0db8:0:0:0:0:0:1 "); got != "2001:db8::1" {
		t.Fatalf("normalized IPv6 = %q, want canonical address", got)
	}
	for _, value := range []string{"", "not-an-ip", "198.51.100.1:443", "https://example.com"} {
		if got := safeUsageEventClientIP(value); got != "" {
			t.Fatalf("unsafe client IP %q normalized to %q", value, got)
		}
	}
}

func TestUsageEventV1LatencyUsesNullForMissing(t *testing.T) {
	event := usage.EventRef{StableEventID: "latency-event"}

	missing := buildUsageEventDTOV1(event, usage.RequestDetail{})
	encoded, err := json.Marshal(missing)
	if err != nil {
		t.Fatalf("marshal missing latency event: %v", err)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &payload); err != nil {
		t.Fatalf("decode missing latency event: %v", err)
	}
	if got := string(payload["latency_ms"]); got != "null" {
		t.Fatalf("missing latency_ms = %s, want null", got)
	}

	present := usage.RequestDetail{LatencyMs: 1200}
	presentPayload := buildUsageEventDTOV1(event, present)
	if presentPayload.LatencyMs == nil || *presentPayload.LatencyMs != 1200 {
		t.Fatalf("present latency_ms = %#v, want pointer to 1200", presentPayload.LatencyMs)
	}
}

func TestUsageEventsRollingAnchorPinsRangeAndExpires(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stats := usage.NewRequestStatistics()
	now := time.Date(2026, 8, 10, 15, 0, 0, 123, time.UTC)
	detail := usage.RequestDetail{
		RequestID: "req-events-anchor", Timestamp: now.Add(-time.Hour), Endpoint: "POST /v1/responses",
		Model: "gpt-5", Provider: "openai", AuthIndex: "auth-anchor", Source: "auth-anchor",
		Tokens: usage.RequestTokenStats{InputTokens: 1, TotalTokens: 1, TokenUsageSource: usage.TokenUsageSourceProvider},
	}
	if _, err := stats.MergeSnapshotWithError(usage.StatisticsSnapshot{APIs: map[string]usage.APISnapshot{
		detail.Endpoint: {Models: map[string]usage.ModelSnapshot{detail.Model: {Details: []usage.RequestDetail{detail}}}},
	}}); err != nil {
		t.Fatalf("MergeSnapshotWithError() error = %v", err)
	}
	codec, err := newUsageTokenCodecWithKey([]byte("unit-test-usage-token-key-32-bytes"))
	if err != nil {
		t.Fatalf("newUsageTokenCodecWithKey() error = %v", err)
	}
	h := &Handler{usageStats: stats, usageNow: func() time.Time { return now }, usageTokenCodec: codec}
	h.usageWindowAnchors = newUsageWindowAnchorManager(codec, func() time.Time { return now }, nil)
	router := gin.New()
	router.GET("/v0/management/usage/events", h.GetUsageEvents)

	first := httptest.NewRecorder()
	router.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/v0/management/usage/events?window=24h&timezone=UTC", nil))
	if first.Code != http.StatusOK {
		t.Fatalf("mint status = %d body=%s", first.Code, first.Body.String())
	}
	var payload struct {
		Range usageRangeV1 `json:"range"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode mint response: %v", err)
	}
	if payload.Range.WindowAnchor == "" || payload.Range.AnchorNonce == "" || payload.Range.Kind != "rolling" ||
		payload.Range.From == "" || payload.Range.To == "" || payload.Range.AnchorExpiresAt == "" {
		t.Fatalf("minted range = %+v", payload.Range)
	}
	previousEnabled := usage.StatisticsEnabled()
	usage.SetStatisticsEnabled(true)
	t.Cleanup(func() { usage.SetStatisticsEnabled(previousEnabled) })
	if err := stats.RecordWithError(context.Background(), coreusage.Record{
		Provider:    detail.Provider,
		Model:       detail.Model,
		AuthIndex:   detail.AuthIndex,
		Source:      detail.Source,
		RequestedAt: now.Add(time.Hour),
		ContextCarrier: &coreusage.RecordContextCarrier{
			RequestID: "req-events-outside-pinned-range",
			Endpoint:  detail.Endpoint,
		},
		Detail: coreusage.Detail{InputTokens: 2, TotalTokens: 2},
	}); err != nil {
		t.Fatalf("RecordWithError() error = %v", err)
	}

	now = now.Add(time.Minute)
	pinnedPath := "/v0/management/usage/events?window=24h&timezone=UTC&anchor=" + url.QueryEscape(payload.Range.WindowAnchor)
	pinned := httptest.NewRequest(http.MethodGet, pinnedPath, nil)
	pinned.Header.Set("If-None-Match", first.Header().Get("ETag"))
	pinnedResponse := httptest.NewRecorder()
	router.ServeHTTP(pinnedResponse, pinned)
	if pinnedResponse.Code != http.StatusNotModified || pinnedResponse.Body.Len() != 0 {
		t.Fatalf("pinned response = status:%d body:%q", pinnedResponse.Code, pinnedResponse.Body.String())
	}
	if err := stats.RecordWithError(context.Background(), coreusage.Record{
		Provider:    detail.Provider,
		Model:       detail.Model,
		AuthIndex:   detail.AuthIndex,
		Source:      detail.Source,
		RequestedAt: now.Add(-30 * time.Minute),
		ContextCarrier: &coreusage.RecordContextCarrier{
			RequestID: "req-events-inside-pinned-range",
			Endpoint:  detail.Endpoint,
		},
		Detail: coreusage.Detail{InputTokens: 3, TotalTokens: 3},
	}); err != nil {
		t.Fatalf("RecordWithError(range event) error = %v", err)
	}
	lateRangeEvent := httptest.NewRequest(http.MethodGet, pinnedPath, nil)
	lateRangeEvent.Header.Set("If-None-Match", first.Header().Get("ETag"))
	lateRangeResponse := httptest.NewRecorder()
	router.ServeHTTP(lateRangeResponse, lateRangeEvent)
	if lateRangeResponse.Code != http.StatusOK {
		t.Fatalf("range event response = status:%d body:%q, want 200", lateRangeResponse.Code, lateRangeResponse.Body.String())
	}

	expiresAt, err := time.Parse(time.RFC3339Nano, payload.Range.AnchorExpiresAt)
	if err != nil {
		t.Fatalf("parse expires_at: %v", err)
	}
	now = expiresAt
	expired := httptest.NewRecorder()
	router.ServeHTTP(expired, httptest.NewRequest(http.MethodGet, pinnedPath, nil))
	if expired.Code != http.StatusConflict || !strings.Contains(expired.Body.String(), "window_anchor_expired") {
		t.Fatalf("expired response = status:%d body:%s", expired.Code, expired.Body.String())
	}
}
