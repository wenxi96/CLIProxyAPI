package management

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/usage"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	coreusage "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
)

type snapshotForbiddenUsageStatistics struct {
	authUsage      map[string]usage.AuthUsageSnapshot
	authUsageErr   error
	authUsageCalls int
}

func (s *snapshotForbiddenUsageStatistics) Snapshot() usage.StatisticsSnapshot {
	panic("ListAuthFiles must not call Snapshot")
}

func (s *snapshotForbiddenUsageStatistics) ListAuthRequestsWithError(string, usage.AuthRequestFilter) (usage.AuthRequestPage, error) {
	panic("unexpected ListAuthRequestsWithError call")
}

func (s *snapshotForbiddenUsageStatistics) MergeSnapshotWithError(usage.StatisticsSnapshot) (usage.MergeResult, error) {
	panic("unexpected MergeSnapshotWithError call")
}

func (s *snapshotForbiddenUsageStatistics) QueryProjectionAuthUsage() (map[string]usage.AuthUsageSnapshot, error) {
	s.authUsageCalls++
	return s.authUsage, s.authUsageErr
}

func (s *snapshotForbiddenUsageStatistics) ProjectionMetadata() (uint64, uint64, uint64, error) {
	panic("unexpected ProjectionMetadata call")
}

func (s *snapshotForbiddenUsageStatistics) QueryProjectionEvents(
	time.Time,
	time.Time,
	int,
	uint64,
	uint64,
	map[string][]string,
) (usage.ProjectionSnapshot, bool, uint64, uint64, string, error) {
	panic("unexpected QueryProjectionEvents call")
}

func (s *snapshotForbiddenUsageStatistics) LookupEventDetails([]string) (map[string]usage.RequestDetail, error) {
	panic("unexpected LookupEventDetails call")
}

func (s *snapshotForbiddenUsageStatistics) QueryProjectionCatalog() (usage.ProjectionSnapshot, error) {
	panic("unexpected QueryProjectionCatalog call")
}

func (s *snapshotForbiddenUsageStatistics) QueryProjectionSummaryView(time.Time, time.Time, time.Time) (usage.ProjectionSummaryView, error) {
	panic("unexpected QueryProjectionSummaryView call")
}

func TestListAuthFiles_IncludesRecentRequestsBuckets(t *testing.T) {
	t.Setenv("MANAGEMENT_PASSWORD", "")

	manager := coreauth.NewManager(nil, nil, nil)
	record := &coreauth.Auth{
		ID:       "runtime-only-auth-1",
		Provider: "codex",
		Attributes: map[string]string{
			"runtime_only": "true",
		},
		Metadata: map[string]any{
			"type": "codex",
		},
	}
	if _, errRegister := manager.Register(context.Background(), record); errRegister != nil {
		t.Fatalf("failed to register auth record: %v", errRegister)
	}

	h := NewHandlerWithoutConfigFilePath(&config.Config{AuthDir: t.TempDir()}, manager)
	h.tokenStore = &memoryAuthStore{}

	rec := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodGet, "/v0/management/auth-files", nil)
	ginCtx.Request = req

	h.ListAuthFiles(ginCtx)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected list status %d, got %d with body %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	var payload map[string]any
	if errUnmarshal := json.Unmarshal(rec.Body.Bytes(), &payload); errUnmarshal != nil {
		t.Fatalf("failed to decode list payload: %v", errUnmarshal)
	}
	filesRaw, ok := payload["files"].([]any)
	if !ok {
		t.Fatalf("expected files array, payload: %#v", payload)
	}
	if len(filesRaw) != 1 {
		t.Fatalf("expected 1 auth entry, got %d", len(filesRaw))
	}

	fileEntry, ok := filesRaw[0].(map[string]any)
	if !ok {
		t.Fatalf("expected file entry object, got %#v", filesRaw[0])
	}

	if _, ok := fileEntry["success"].(float64); !ok {
		t.Fatalf("expected success number, got %#v", fileEntry["success"])
	}
	if _, ok := fileEntry["failed"].(float64); !ok {
		t.Fatalf("expected failed number, got %#v", fileEntry["failed"])
	}

	recentRaw, ok := fileEntry["recent_requests"].([]any)
	if !ok {
		t.Fatalf("expected recent_requests array, got %#v", fileEntry["recent_requests"])
	}
	if len(recentRaw) != 20 {
		t.Fatalf("expected 20 recent_requests buckets, got %d", len(recentRaw))
	}
	for idx, item := range recentRaw {
		bucket, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("expected bucket object at %d, got %#v", idx, item)
		}
		if _, ok := bucket["time"].(string); !ok {
			t.Fatalf("expected bucket time string at %d, got %#v", idx, bucket["time"])
		}
		if _, ok := bucket["success"].(float64); !ok {
			t.Fatalf("expected bucket success number at %d, got %#v", idx, bucket["success"])
		}
		if _, ok := bucket["failed"].(float64); !ok {
			t.Fatalf("expected bucket failed number at %d, got %#v", idx, bucket["failed"])
		}
	}
}

func TestListAuthFilesIncludesUsageSummary(t *testing.T) {
	t.Setenv("MANAGEMENT_PASSWORD", "")

	authIndex := "auth-index_123.~"
	manager := coreauth.NewManager(nil, nil, nil)
	record := &coreauth.Auth{
		ID:       "runtime-only-auth-usage",
		Index:    authIndex,
		Provider: "codex",
		Attributes: map[string]string{
			"runtime_only": "true",
		},
		Metadata: map[string]any{
			"type": "codex",
		},
	}
	if _, errRegister := manager.Register(context.Background(), record); errRegister != nil {
		t.Fatalf("failed to register auth record: %v", errRegister)
	}

	stats := usage.NewRequestStatistics()
	lastRequestAt := time.Date(2026, 7, 3, 11, 30, 0, 0, time.UTC)
	stats.Record(context.Background(), coreusage.Record{
		APIKey:      "POST /v1/responses",
		Model:       "gpt-5-mini",
		RequestedAt: lastRequestAt,
		AuthIndex:   authIndex,
		Detail: coreusage.Detail{
			InputTokens:  10,
			OutputTokens: 5,
		},
	})

	h := NewHandlerWithoutConfigFilePath(&config.Config{AuthDir: t.TempDir()}, manager)
	h.tokenStore = &memoryAuthStore{}
	h.SetUsageStatistics(stats)

	rec := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(rec)
	ginCtx.Request = httptest.NewRequest(http.MethodGet, "/v0/management/auth-files", nil)

	h.ListAuthFiles(ginCtx)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected list status %d, got %d with body %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	var payload map[string]any
	if errUnmarshal := json.Unmarshal(rec.Body.Bytes(), &payload); errUnmarshal != nil {
		t.Fatalf("failed to decode list payload: %v", errUnmarshal)
	}
	filesRaw, ok := payload["files"].([]any)
	if !ok || len(filesRaw) != 1 {
		t.Fatalf("expected one file entry, payload: %#v", payload)
	}
	fileEntry, ok := filesRaw[0].(map[string]any)
	if !ok {
		t.Fatalf("expected file entry object, got %#v", filesRaw[0])
	}
	if fileEntry["usage_availability"] != "available" {
		t.Fatalf("usage_availability = %#v, want available", fileEntry["usage_availability"])
	}
	usageRaw, ok := fileEntry["usage"].(map[string]any)
	if !ok {
		t.Fatalf("expected usage summary, got %#v", fileEntry["usage"])
	}
	if usageRaw["total_requests"] != float64(1) || usageRaw["success_count"] != float64(1) || usageRaw["failure_count"] != float64(0) {
		t.Fatalf("usage counts = %#v, want total=1 success=1 failure=0", usageRaw)
	}
	tokens, ok := usageRaw["tokens"].(map[string]any)
	if !ok {
		t.Fatalf("expected usage tokens object, got %#v", usageRaw["tokens"])
	}
	if tokens["total_tokens"] != float64(15) {
		t.Fatalf("usage total_tokens = %#v, want 15", tokens["total_tokens"])
	}
	if usageRaw["estimated_cost_usd"] != nil {
		t.Fatalf("estimated_cost_usd = %#v, want nil", usageRaw["estimated_cost_usd"])
	}
	lastRequestRaw, ok := usageRaw["last_request_at"].(string)
	if !ok || lastRequestRaw == "" {
		t.Fatalf("last_request_at missing from usage summary: %#v", usageRaw)
	}
}

func TestListAuthFilesUsesProjectionAuthUsageWithoutSnapshot(t *testing.T) {
	t.Setenv("MANAGEMENT_PASSWORD", "")
	const authIndex = "auth-index-no-snapshot"
	manager := coreauth.NewManager(nil, nil, nil)
	record := &coreauth.Auth{
		ID:       "runtime-only-auth-no-snapshot",
		Index:    authIndex,
		Provider: "codex",
		Attributes: map[string]string{
			"runtime_only": "true",
		},
		Metadata: map[string]any{"type": "codex"},
	}
	if _, err := manager.Register(context.Background(), record); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	stats := &snapshotForbiddenUsageStatistics{authUsage: map[string]usage.AuthUsageSnapshot{
		authIndex: {
			AuthIndex:     authIndex,
			TotalRequests: 3,
			SuccessCount:  2,
			FailureCount:  1,
			Tokens:        usage.TokenStats{TotalTokens: 42},
		},
	}}
	h := NewHandlerWithoutConfigFilePath(&config.Config{AuthDir: t.TempDir()}, manager)
	h.tokenStore = &memoryAuthStore{}
	h.usageStats = stats

	recorder := httptest.NewRecorder()
	ginContext, _ := gin.CreateTestContext(recorder)
	ginContext.Request = httptest.NewRequest(http.MethodGet, "/v0/management/auth-files", nil)
	h.ListAuthFiles(ginContext)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if stats.authUsageCalls != 1 {
		t.Fatalf("QueryProjectionAuthUsage calls = %d, want 1", stats.authUsageCalls)
	}
	var payload struct {
		Files []map[string]any `json:"files"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode auth files: %v", err)
	}
	if len(payload.Files) != 1 || payload.Files[0]["usage_availability"] != "available" {
		t.Fatalf("available projection payload = %+v", payload.Files)
	}
	usagePayload, ok := payload.Files[0]["usage"].(map[string]any)
	if !ok || usagePayload["total_requests"] != float64(3) {
		t.Fatalf("projection usage payload = %#v", payload.Files[0]["usage"])
	}
}

func TestListAuthFilesProjectionUnavailablePreservesMetadata(t *testing.T) {
	t.Setenv("MANAGEMENT_PASSWORD", "")
	manager := coreauth.NewManager(nil, nil, nil)
	record := &coreauth.Auth{
		ID:       "runtime-only-auth-unavailable",
		Index:    "Auth-Unavailable_123.~",
		Provider: "codex",
		Attributes: map[string]string{
			"runtime_only": "true",
		},
		Metadata: map[string]any{"type": "codex"},
	}
	if _, err := manager.Register(context.Background(), record); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	h := NewHandlerWithoutConfigFilePath(&config.Config{AuthDir: t.TempDir()}, manager)
	h.tokenStore = &memoryAuthStore{}
	stats := &snapshotForbiddenUsageStatistics{authUsageErr: usage.ErrProjectionUnavailable}
	h.usageStats = stats

	recorder := httptest.NewRecorder()
	ginContext, _ := gin.CreateTestContext(recorder)
	ginContext.Request = httptest.NewRequest(http.MethodGet, "/v0/management/auth-files", nil)
	h.ListAuthFiles(ginContext)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if stats.authUsageCalls != 1 {
		t.Fatalf("QueryProjectionAuthUsage calls = %d, want 1", stats.authUsageCalls)
	}
	var payload struct {
		Files []map[string]any `json:"files"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode auth files: %v", err)
	}
	if len(payload.Files) != 1 || payload.Files[0]["auth_index"] != record.Index ||
		payload.Files[0]["usage_availability"] != "unavailable" {
		t.Fatalf("auth metadata = %+v", payload.Files)
	}
	if _, exists := payload.Files[0]["usage"]; exists {
		t.Fatalf("unavailable projection returned stale usage: %+v", payload.Files[0])
	}
}
