package management

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/usage"
)

func TestLegacyUsageExportAndImportHTTPContractRemainsStable(t *testing.T) {
	h := newUsageExportTestHandler(t)
	router := gin.New()
	router.GET("/v0/management/usage/export", h.ExportUsageStatistics)
	router.POST("/v0/management/usage/import", h.ImportUsageStatistics)

	get := httptest.NewRecorder()
	router.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/v0/management/usage/export", nil))
	if get.Code != http.StatusOK {
		t.Fatalf("legacy export status = %d body=%s", get.Code, get.Body.String())
	}
	var exported map[string]json.RawMessage
	if err := json.Unmarshal(get.Body.Bytes(), &exported); err != nil {
		t.Fatalf("decode legacy export: %v", err)
	}
	if len(exported) != 3 {
		t.Fatalf("legacy export fields = %#v", exported)
	}
	for _, key := range []string{"version", "exported_at", "usage"} {
		if _, ok := exported[key]; !ok {
			t.Fatalf("legacy export missing %q: %s", key, get.Body.String())
		}
	}
	if strings.Contains(get.Body.String(), "export_snapshot_id") || strings.Contains(get.Body.String(), "record_type") {
		t.Fatalf("legacy export leaked v2 fields: %s", get.Body.String())
	}

	for _, version := range []int{0, 1, 2} {
		body := `{"version":` + strconv.Itoa(version) + `,"usage":{"apis":{}}}`
		post := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/v0/management/usage/import", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(post, req)
		if post.Code != http.StatusOK {
			t.Fatalf("legacy import version=%d status=%d body=%s", version, post.Code, post.Body.String())
		}
		var result map[string]json.RawMessage
		if err := json.Unmarshal(post.Body.Bytes(), &result); err != nil {
			t.Fatalf("decode legacy import version=%d: %v", version, err)
		}
		for _, key := range []string{"added", "skipped", "enriched", "total_requests", "failed_requests"} {
			if _, ok := result[key]; !ok {
				t.Fatalf("legacy import version=%d missing %q: %s", version, key, post.Body.String())
			}
		}
	}

	badJSON := httptest.NewRecorder()
	router.ServeHTTP(badJSON, httptest.NewRequest(http.MethodPost, "/v0/management/usage/import", strings.NewReader("{")))
	if badJSON.Code != http.StatusBadRequest || !strings.Contains(badJSON.Body.String(), "invalid json") {
		t.Fatalf("legacy invalid json = %d %s", badJSON.Code, badJSON.Body.String())
	}
	unsupported := httptest.NewRecorder()
	router.ServeHTTP(unsupported, httptest.NewRequest(http.MethodPost, "/v0/management/usage/import", strings.NewReader(`{"version":99,"usage":{"apis":{}}}`)))
	if unsupported.Code != http.StatusBadRequest || !strings.Contains(unsupported.Body.String(), "unsupported version") {
		t.Fatalf("legacy unsupported version = %d %s", unsupported.Code, unsupported.Body.String())
	}
}

func TestUsageExportSnapshotManagerExpiresAndReleasesStoredLease(t *testing.T) {
	now := time.Date(2026, 8, 11, 0, 0, 0, 0, time.UTC)
	manager := newUsageExportSnapshotManager(func() time.Time { return now })
	manager.ttl = time.Minute
	stats := usage.NewRequestStatistics()
	lease, _, err := stats.PinUsageExportAt(context.Background(), usage.UsageExportQuery{}, now, time.Minute)
	if err != nil {
		t.Fatalf("pin export: %v", err)
	}
	snapshot, err := manager.create(usageExportSnapshot{Lease: lease, ExpiresAt: now.Add(time.Minute)})
	if err != nil {
		lease.Release()
		t.Fatalf("create snapshot: %v", err)
	}
	lease.Release()
	if len(manager.items) != 1 {
		t.Fatalf("manager items before expiry = %d", len(manager.items))
	}
	now = now.Add(time.Minute)
	if removed := manager.cleanupExpired(); removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}
	if len(manager.items) != 0 {
		t.Fatalf("manager items after expiry = %d", len(manager.items))
	}
	if _, err := manager.get(snapshot.ID); !errors.Is(err, errUsageExportSnapshotMismatch) {
		t.Fatalf("expired snapshot lookup error = %v", err)
	}
}

func TestUsageExportEstimatePropagatesRequestCancellation(t *testing.T) {
	h := newUsageExportTestHandler(t)
	query := usageEventsParsedQuery{from: time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC), to: time.Date(2026, 8, 11, 0, 0, 0, 0, time.UTC), filters: map[string][]string{}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := h.buildUsageExportSnapshotFromQueryContext(ctx, query, usageEventsCanonicalQueryV1{}, "cancel", "json", 1, 1, 1)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled estimate error = %v", err)
	}
}

func TestUsageEventsExportEstimateCreatesPinnedSnapshot(t *testing.T) {
	h := newUsageExportTestHandler(t)
	router := gin.New()
	router.GET("/v0/management/usage/events/export/estimate", h.EstimateUsageEventsExport)

	req := httptest.NewRequest(http.MethodGet, "/v0/management/usage/events/export/estimate?from=2026-08-10T00:00:00Z&to=2026-08-11T00:00:00Z&format=json&export_schema_version=usage-events-v2", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", res.Code, res.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode estimate: %v", err)
	}
	for _, key := range []string{"export_snapshot_id", "dataset_epoch", "snapshot_max_sequence", "rewrite_revision", "normalized_query", "snapshot_facts_hash", "export_snapshot_expires_at", "server_bytes_upper_bound", "server_bytes_upper_bound_complete", "event_count_upper_bound", "event_count_exact", "format", "export_schema_version", "export_derivation_profile"} {
		if _, ok := payload[key]; !ok {
			t.Fatalf("estimate missing %q: %s", key, res.Body.String())
		}
	}
	if payload["export_snapshot_id"] == "" || payload["export_schema_version"] != "usage-events-v2" {
		t.Fatalf("estimate identity = %#v", payload)
	}
	if got, ok := payload["server_bytes_upper_bound"].(float64); !ok || got <= 0 {
		t.Fatalf("estimate server byte bound = %#v", payload["server_bytes_upper_bound"])
	}
	if complete, ok := payload["server_bytes_upper_bound_complete"].(bool); !ok || !complete {
		t.Fatalf("estimate server byte completeness = %#v", payload["server_bytes_upper_bound_complete"])
	}
	if complete, ok := payload["bytes_upper_bound_complete"].(bool); !ok || complete {
		t.Fatalf("estimate combined completeness = %#v", payload["bytes_upper_bound_complete"])
	}
}

func TestUsageEventsExportWritesNDJSONEventsAndEndControl(t *testing.T) {
	h := newUsageExportTestHandler(t)
	router := gin.New()
	router.GET("/v0/management/usage/events/export", h.ExportUsageEvents)

	req := httptest.NewRequest(http.MethodGet, "/v0/management/usage/events/export?from=2026-08-10T00:00:00Z&to=2026-08-11T00:00:00Z&format=json&export_schema_version=usage-events-v2", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != http.StatusOK || res.Header().Get("Content-Type") != "application/x-ndjson" {
		t.Fatalf("status/content-type = %d/%q body=%s", res.Code, res.Header().Get("Content-Type"), res.Body.String())
	}
	if res.Header().Get("X-Usage-Export-Snapshot") == "" {
		t.Fatal("missing export snapshot header")
	}
	scanner := bufio.NewScanner(strings.NewReader(res.Body.String()))
	var records []map[string]any
	for scanner.Scan() {
		var record map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			t.Fatalf("decode NDJSON line %q: %v", scanner.Text(), err)
		}
		records = append(records, record)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan NDJSON: %v", err)
	}
	if len(records) != 2 || records[0]["record_type"] != "event" || records[1]["record_type"] != "end" || records[1]["complete"] != true {
		t.Fatalf("records = %#v", records)
	}
	if records[1]["export_snapshot_id"] == "" {
		t.Fatalf("end control record missing snapshot id: %#v", records[1])
	}
	if records[1]["server_bytes_upper_bound_complete"] != true || records[1]["bytes_upper_bound_complete"] != false {
		t.Fatalf("end framing fields = %#v", records[1])
	}
	for _, forbidden := range []string{"stable_event_id", `"sequence":`, "canonical_event_identity", "raw-secret"} {
		if strings.Contains(res.Body.String(), forbidden) {
			t.Fatalf("export leaked %q: %s", forbidden, res.Body.String())
		}
	}
}

func TestUsageEventsExportRejectsExpiredEstimateBeforeWriting(t *testing.T) {
	h := newUsageExportTestHandler(t)
	h.usageExportSnapshots.ttl = time.Millisecond
	h.usageExportSnapshots.now = time.Now
	router := gin.New()
	router.GET("/v0/management/usage/events/export/estimate", h.EstimateUsageEventsExport)
	router.GET("/v0/management/usage/events/export", h.ExportUsageEvents)

	estRequest := func(path string) *httptest.ResponseRecorder {
		res := httptest.NewRecorder()
		router.ServeHTTP(res, httptest.NewRequest(http.MethodGet, path, nil))
		return res
	}
	estimate := estRequest("/v0/management/usage/events/export/estimate?from=2026-08-10T00:00:00Z&to=2026-08-11T00:00:00Z&format=json&export_schema_version=usage-events-v2")
	if estimate.Code != http.StatusOK {
		t.Fatalf("estimate = %d %s", estimate.Code, estimate.Body.String())
	}
	var payload struct {
		SnapshotID string `json:"export_snapshot_id"`
	}
	if err := json.Unmarshal(estimate.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode estimate: %v", err)
	}
	if payload.SnapshotID == "" {
		t.Fatal("estimate did not return snapshot id")
	}
	time.Sleep(3 * time.Millisecond)
	res := estRequest("/v0/management/usage/events/export?from=2026-08-10T00:00:00Z&to=2026-08-11T00:00:00Z&format=json&export_schema_version=usage-events-v2&export_snapshot_id=" + url.QueryEscape(payload.SnapshotID))
	if res.Code != http.StatusGone || !strings.Contains(res.Body.String(), "export_snapshot_expired") || res.Body.Len() == 0 {
		t.Fatalf("expired export = %d %s", res.Code, res.Body.String())
	}
}

func TestUsageEventV2AllowlistDoesNotExposeInternalFields(t *testing.T) {
	type dto struct {
		SchemaVersion int    `json:"schema_version"`
		RecordType    string `json:"record_type"`
		Thinking      any    `json:"thinking"`
		Coverage      string `json:"thinking_coverage"`
	}
	value := dto{SchemaVersion: 1, RecordType: "event", Coverage: "unavailable_legacy"}
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "stable_event_id") || !strings.Contains(string(encoded), `"thinking":null`) {
		t.Fatalf("allowlist fixture = %s", encoded)
	}
}

func TestUsageExportJSONFrameUpperBoundCoversFinalArray(t *testing.T) {
	h := newUsageExportTestHandler(t)
	query := usageEventsParsedQuery{from: time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC), to: time.Date(2026, 8, 11, 0, 0, 0, 0, time.UTC), filters: map[string][]string{}}
	snapshot, err := h.buildUsageExportSnapshotFromQuery(query, usageEventsCanonicalQueryV1{}, "frame-test", "json", 1, 1, 1)
	if err != nil {
		t.Fatalf("build snapshot: %v", err)
	}
	if !snapshot.ServerBytesComplete || snapshot.ServerBytes == 0 {
		t.Fatalf("server frame = %+v", snapshot)
	}
	defer snapshot.Lease.Release()
	lease := snapshot.Lease.Acquire()
	if lease == nil {
		t.Fatal("snapshot lease was not retained")
	}
	defer lease.Release()
	var rows []json.RawMessage
	position := uint64(0)
	for {
		page, pageErr := lease.NextPage(context.Background(), position, 500)
		if pageErr != nil {
			t.Fatalf("page: %v", pageErr)
		}
		for _, row := range page.Rows {
			encoded, marshalErr := json.Marshal(buildUsageEventDTOV1(row.Event, row.Detail))
			if marshalErr != nil {
				t.Fatalf("marshal row: %v", marshalErr)
			}
			rows = append(rows, encoded)
		}
		if !page.HasMore {
			break
		}
		position = page.NextPosition
	}
	if len(rows) != int(snapshot.EventCount) {
		t.Fatalf("event rows=%d estimate=%d", len(rows), snapshot.EventCount)
	}
	if snapshot.ServerBytes != snapshot.ServerJSONBytes || !snapshot.ServerJSONComplete {
		t.Fatalf("json server frame = %+v", snapshot)
	}
}

func TestUsageExportCSVFormatUsesCompleteSchemaCounter(t *testing.T) {
	h := newUsageExportTestHandler(t)
	query := usageEventsParsedQuery{from: time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC), to: time.Date(2026, 8, 11, 0, 0, 0, 0, time.UTC), filters: map[string][]string{}}
	snapshot, err := h.buildUsageExportSnapshotFromQuery(query, usageEventsCanonicalQueryV1{}, "frame-csv", "csv", 1, 1, 1)
	if err != nil {
		t.Fatalf("build snapshot: %v", err)
	}
	if !snapshot.ServerBytesComplete || snapshot.ServerBytes < uint64(len(usageEventsExportCSVHeaderV1)) {
		t.Fatalf("csv frame = %+v", snapshot)
	}
	if !strings.HasSuffix(usageEventsExportCSVHeaderV1, "\r\n") {
		t.Fatalf("csv header missing CRLF: %q", usageEventsExportCSVHeaderV1)
	}
	snapshot.Lease.Release()
}

func TestUsageEventsExportEstimateSelectsCSVServerCounter(t *testing.T) {
	h := newUsageExportTestHandler(t)
	router := gin.New()
	router.GET("/v0/management/usage/events/export/estimate", h.EstimateUsageEventsExport)
	request := httptest.NewRequest(http.MethodGet, "/v0/management/usage/events/export/estimate?from=2026-08-10T00:00:00Z&to=2026-08-11T00:00:00Z&format=csv&export_schema_version=usage-events-v2", nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("CSV estimate status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		Format         string `json:"format"`
		ServerBytes    uint64 `json:"server_bytes_upper_bound"`
		ServerComplete bool   `json:"server_bytes_upper_bound_complete"`
		Profile        string `json:"export_derivation_profile"`
		Schema         string `json:"export_schema_version"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode CSV estimate: %v", err)
	}
	if payload.Format != "csv" || !payload.ServerComplete || payload.ServerBytes < uint64(len(usageEventsExportCSVHeaderV1)) {
		t.Fatalf("CSV estimate framing = %+v", payload)
	}
	if payload.Profile != usageEventsExportProfileV1 || payload.Schema != usageEventsExportSchemaV2 {
		t.Fatalf("CSV estimate identity = %+v", payload)
	}
}

func TestUsageExportAllowsPinnedSnapshotAfterLiveRewrite(t *testing.T) {
	h := newUsageExportTestHandler(t)
	router := gin.New()
	router.GET("/v0/management/usage/events/export/estimate", h.EstimateUsageEventsExport)
	router.GET("/v0/management/usage/events/export", h.ExportUsageEvents)

	query := "from=2026-08-10T00:00:00Z&to=2026-08-11T00:00:00Z&format=json&export_schema_version=usage-events-v2"
	estimate := httptest.NewRecorder()
	router.ServeHTTP(estimate, httptest.NewRequest(http.MethodGet, "/v0/management/usage/events/export/estimate?"+query, nil))
	if estimate.Code != http.StatusOK {
		t.Fatalf("estimate status = %d body=%s", estimate.Code, estimate.Body.String())
	}
	var payload struct {
		SnapshotID string `json:"export_snapshot_id"`
	}
	if err := json.Unmarshal(estimate.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode estimate: %v", err)
	}
	if payload.SnapshotID == "" {
		t.Fatal("estimate did not return snapshot id")
	}

	rewritten := usage.RequestDetail{
		RequestID: "export-request",
		Timestamp: time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC),
		Endpoint:  "POST /v1/responses",
		Model:     "gpt-export-rewritten",
		Provider:  "openai",
		AuthIndex: "auth-export",
		Source:    "raw-secret-rewritten",
		Tokens:    usage.RequestTokenStats{InputTokens: 8, OutputTokens: 5, TotalTokens: 13, TokenUsageSource: usage.TokenUsageSourceProvider},
	}
	if _, err := h.usageStats.(*usage.RequestStatistics).MergeSnapshotWithError(usage.StatisticsSnapshot{APIs: map[string]usage.APISnapshot{
		rewritten.Endpoint: {Models: map[string]usage.ModelSnapshot{rewritten.Model: {Details: []usage.RequestDetail{rewritten}}}},
	}}); err != nil {
		t.Fatalf("rewrite usage: %v", err)
	}

	export := httptest.NewRecorder()
	exportPath := "/v0/management/usage/events/export?" + query + "&export_snapshot_id=" + url.QueryEscape(payload.SnapshotID)
	router.ServeHTTP(export, httptest.NewRequest(http.MethodGet, exportPath, nil))
	if export.Code != http.StatusOK {
		t.Fatalf("pinned export status = %d body=%s", export.Code, export.Body.String())
	}
	if !strings.Contains(export.Body.String(), `"record_type":"end"`) || strings.Contains(export.Body.String(), "gpt-export-rewritten") {
		t.Fatalf("pinned export mixed live rewrite: %s", export.Body.String())
	}
}

func TestUsageExportRejectsMutableFallbackWhenLeaseUnavailable(t *testing.T) {
	h := newUsageExportTestHandler(t)
	h.usageStats = usageStatisticsWithoutExportLease{usageStatistics: h.usageStats}
	router := gin.New()
	router.GET("/v0/management/usage/events/export", h.ExportUsageEvents)
	request := httptest.NewRequest(http.MethodGet, "/v0/management/usage/events/export?from=2026-08-10T00:00:00Z&to=2026-08-11T00:00:00Z&format=json&export_schema_version=usage-events-v2", nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("fallback status = %d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestUsageEventsExportCancellationDoesNotWriteErrorSentinel(t *testing.T) {
	h := newUsageExportTestHandler(t)
	router := gin.New()
	router.GET("/v0/management/usage/events/export", h.ExportUsageEvents)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest(http.MethodGet, "/v0/management/usage/events/export?from=2026-08-10T00:00:00Z&to=2026-08-11T00:00:00Z&format=json&export_schema_version=usage-events-v2", nil).WithContext(ctx)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if strings.Contains(res.Body.String(), `"record_type":"error"`) {
		t.Fatalf("cancelled export emitted an application error sentinel: %s", res.Body.String())
	}
}

type usageStatisticsWithoutExportLease struct{ usageStatistics }

func newUsageExportTestHandler(t *testing.T) *Handler {
	t.Helper()
	stats := usage.NewRequestStatistics()
	detail := usage.RequestDetail{
		RequestID:    "export-request",
		Timestamp:    time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC),
		Endpoint:     "POST /v1/responses?secret=yes",
		Model:        "gpt-export",
		Provider:     "openai",
		ExecutorType: "OpenAIExecutor",
		AuthType:     "api_key",
		AuthIndex:    "auth-export",
		Source:       "raw-secret",
		Tokens:       usage.RequestTokenStats{InputTokens: 4, OutputTokens: 3, TotalTokens: 7, TokenUsageSource: usage.TokenUsageSourceProvider},
	}
	if _, err := stats.MergeSnapshotWithError(usage.StatisticsSnapshot{APIs: map[string]usage.APISnapshot{
		"POST /v1/responses": {Models: map[string]usage.ModelSnapshot{"gpt-export": {Details: []usage.RequestDetail{detail}}}},
	}}); err != nil {
		t.Fatalf("seed usage: %v", err)
	}
	now := time.Date(2026, 8, 11, 0, 0, 0, 0, time.UTC)
	return &Handler{
		usageStats:           stats,
		usageNow:             func() time.Time { return now },
		usageExportSnapshots: newUsageExportSnapshotManager(func() time.Time { return now }),
	}
}
