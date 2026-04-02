package management

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
)

func TestBatchCheckAuthFiles_SummarizesResults(t *testing.T) {
	t.Setenv("MANAGEMENT_PASSWORD", "")
	gin.SetMode(gin.TestMode)

	manager := coreauth.NewManager(nil, nil, nil)

	codexAuth := &coreauth.Auth{
		ID:       "codex-1",
		Provider: "codex",
		FileName: "codex-alpha.json",
		Status:   coreauth.StatusActive,
		Metadata: map[string]any{
			"chatgpt_account_id": "acct-1",
			"access_token":       "token-1",
		},
	}
	claudeAuth := &coreauth.Auth{
		ID:       "claude-1",
		Provider: "claude",
		FileName: "claude-beta.json",
		Status:   coreauth.StatusActive,
		Metadata: map[string]any{
			"access_token": "token-2",
		},
	}
	if _, err := manager.Register(context.Background(), codexAuth); err != nil {
		t.Fatalf("register codex auth: %v", err)
	}
	if _, err := manager.Register(context.Background(), claudeAuth); err != nil {
		t.Fatalf("register claude auth: %v", err)
	}

	h := NewHandlerWithoutConfigFilePath(&config.Config{}, manager)
	h.apiCallExecutor = func(_ context.Context, auth *coreauth.Auth, req apiCallRequest) (apiCallResponse, error) {
		switch auth.FileName {
		case "codex-alpha.json":
			return apiCallResponse{
				StatusCode: http.StatusOK,
				Body: `{
					"plan_type":"pro",
					"rate_limit":{
						"primary_window":{"used_percent":20,"limit_window_seconds":18000,"reset_after_seconds":1200},
						"secondary_window":{"used_percent":40,"limit_window_seconds":604800,"reset_after_seconds":7200}
					}
				}`,
			}, nil
		case "claude-beta.json":
			if req.URL == claudeProfileURL {
				return apiCallResponse{
					StatusCode: http.StatusOK,
					Body:       `{"account":{"has_claude_pro":true}}`,
				}, nil
			}
			return apiCallResponse{
				StatusCode: http.StatusOK,
				Body: `{
					"five_hour":{"utilization":100,"resets_at":"2026-03-31T03:00:00Z"},
					"seven_day":{"utilization":35,"resets_at":"2026-04-02T03:00:00Z"}
				}`,
			}, nil
		default:
			t.Fatalf("unexpected auth file %q", auth.FileName)
			return apiCallResponse{}, nil
		}
	}

	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v0/management/auth-files/batch-check", bytes.NewReader([]byte(`{}`)))
	ctx.Request.Header.Set("Content-Type", "application/json")

	h.BatchCheckAuthFiles(ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	var payload struct {
		Summary struct {
			CheckedCount           int            `json:"checked_count"`
			AvailableCount         int            `json:"available_count"`
			AvailableProviderCount int            `json:"available_provider_count"`
			SkippedCount           int            `json:"skipped_count"`
			AverageRemaining       *int           `json:"average_remaining_percent"`
			ClassificationCounts   map[string]int `json:"classification_counts"`
			BucketCounts           map[string]int `json:"bucket_counts"`
		} `json:"summary"`
		Aggregate struct {
			CapacityOverview struct {
				RemainingTotal         int      `json:"remaining_total"`
				TotalCapacity          int      `json:"total_capacity"`
				RemainingPercent       float64  `json:"remaining_percent"`
				UsedTotal              int      `json:"used_total"`
				UsedPercent            float64  `json:"used_percent"`
				EquivalentFullAccounts float64  `json:"equivalent_full_accounts"`
				AverageRemaining       *float64 `json:"average_remaining"`
				MedianRemaining        *float64 `json:"median_remaining"`
				UnknownRemainingCount  int      `json:"unknown_remaining_count"`
			} `json:"capacity_overview"`
			RiskOverview struct {
				Invalidated401Count   int `json:"invalidated_401_count"`
				NoQuotaCount          int `json:"no_quota_count"`
				APIErrorCount         int `json:"api_error_count"`
				RequestFailedCount    int `json:"request_failed_count"`
				ExhaustedCount        int `json:"exhausted_count"`
				LowRemaining129Count  int `json:"low_remaining_1_29_count"`
				MidLowRemaining149Cnt int `json:"mid_low_remaining_1_49_count"`
			} `json:"risk_overview"`
			ScopeOverview struct {
				EnabledCount  int `json:"enabled_count"`
				DisabledCount int `json:"disabled_count"`
			} `json:"scope_overview"`
			ActionCandidates struct {
				DisableExhaustedNames []string `json:"disable_exhausted_names"`
				ReenableNames         []string `json:"reenable_names"`
				ReenableThreshold     string   `json:"reenable_threshold_bucket"`
			} `json:"action_candidates"`
		} `json:"aggregate"`
		Results []struct {
			Name             string `json:"name"`
			Classification   string `json:"classification"`
			Available        bool   `json:"available"`
			RemainingPercent *int   `json:"remaining_percent"`
			Bucket           string `json:"bucket"`
			ErrorMessage     string `json:"error_message"`
		} `json:"results"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if payload.Summary.CheckedCount != 2 {
		t.Fatalf("expected checked_count=2, got %d", payload.Summary.CheckedCount)
	}
	if payload.Summary.AvailableCount != 1 {
		t.Fatalf("expected available_count=1, got %d", payload.Summary.AvailableCount)
	}
	if payload.Summary.AvailableProviderCount != 1 {
		t.Fatalf("expected available_provider_count=1, got %d", payload.Summary.AvailableProviderCount)
	}
	if payload.Summary.SkippedCount != 0 {
		t.Fatalf("expected skipped_count=0, got %d", payload.Summary.SkippedCount)
	}
	if payload.Summary.ClassificationCounts[authFileBatchCheckClassificationOK] != 1 {
		t.Fatalf("expected ok count 1, got %#v", payload.Summary.ClassificationCounts)
	}
	if payload.Summary.ClassificationCounts[authFileBatchCheckClassificationNoQuota] != 1 {
		t.Fatalf("expected no_quota count 1, got %#v", payload.Summary.ClassificationCounts)
	}
	if payload.Summary.AverageRemaining == nil || *payload.Summary.AverageRemaining != 30 {
		t.Fatalf("expected average_remaining_percent=30, got %#v", payload.Summary.AverageRemaining)
	}
	if payload.Aggregate.CapacityOverview.RemainingTotal != 60 {
		t.Fatalf("expected remaining_total=60, got %d", payload.Aggregate.CapacityOverview.RemainingTotal)
	}
	if payload.Aggregate.CapacityOverview.TotalCapacity != 200 {
		t.Fatalf("expected total_capacity=200, got %d", payload.Aggregate.CapacityOverview.TotalCapacity)
	}
	if payload.Aggregate.CapacityOverview.RemainingPercent != 30 {
		t.Fatalf("expected remaining_percent=30, got %v", payload.Aggregate.CapacityOverview.RemainingPercent)
	}
	if payload.Aggregate.CapacityOverview.EquivalentFullAccounts != 0.6 {
		t.Fatalf("expected equivalent_full_accounts=0.6, got %v", payload.Aggregate.CapacityOverview.EquivalentFullAccounts)
	}
	if payload.Aggregate.CapacityOverview.MedianRemaining == nil || *payload.Aggregate.CapacityOverview.MedianRemaining != 30 {
		t.Fatalf("expected median_remaining=30, got %#v", payload.Aggregate.CapacityOverview.MedianRemaining)
	}
	if payload.Aggregate.RiskOverview.NoQuotaCount != 1 {
		t.Fatalf("expected no_quota_count=1, got %d", payload.Aggregate.RiskOverview.NoQuotaCount)
	}
	if payload.Aggregate.RiskOverview.ExhaustedCount != 1 {
		t.Fatalf("expected exhausted_count=1, got %d", payload.Aggregate.RiskOverview.ExhaustedCount)
	}
	if payload.Aggregate.ScopeOverview.EnabledCount != 2 || payload.Aggregate.ScopeOverview.DisabledCount != 0 {
		t.Fatalf("unexpected scope counts: %#v", payload.Aggregate.ScopeOverview)
	}
	if len(payload.Aggregate.ActionCandidates.DisableExhaustedNames) != 1 || payload.Aggregate.ActionCandidates.DisableExhaustedNames[0] != "claude-beta.json" {
		t.Fatalf("unexpected disable_exhausted_names: %#v", payload.Aggregate.ActionCandidates.DisableExhaustedNames)
	}
	if len(payload.Aggregate.ActionCandidates.ReenableNames) != 0 {
		t.Fatalf("expected empty reenable_names, got %#v", payload.Aggregate.ActionCandidates.ReenableNames)
	}
	if payload.Aggregate.ActionCandidates.ReenableThreshold != "danger" {
		t.Fatalf("expected reenable_threshold_bucket=danger, got %q", payload.Aggregate.ActionCandidates.ReenableThreshold)
	}

	if len(payload.Results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(payload.Results))
	}

	if payload.Results[0].Name != "claude-beta.json" || payload.Results[0].Classification != authFileBatchCheckClassificationNoQuota {
		t.Fatalf("unexpected first result: %#v", payload.Results[0])
	}
	if payload.Results[1].Name != "codex-alpha.json" || payload.Results[1].Classification != authFileBatchCheckClassificationOK {
		t.Fatalf("unexpected second result: %#v", payload.Results[1])
	}
	if payload.Results[1].RemainingPercent == nil || *payload.Results[1].RemainingPercent != 60 {
		t.Fatalf("expected codex remaining=60, got %#v", payload.Results[1].RemainingPercent)
	}
	if payload.Results[1].Bucket != "usable" {
		t.Fatalf("expected codex bucket usable, got %q", payload.Results[1].Bucket)
	}
	if payload.Results[1].ErrorMessage != "" {
		t.Fatalf("expected empty codex error_message on success, got %q", payload.Results[1].ErrorMessage)
	}
}

func TestBatchCheckAuthFiles_UsesRequestedConcurrency(t *testing.T) {
	t.Setenv("MANAGEMENT_PASSWORD", "")
	gin.SetMode(gin.TestMode)

	manager := coreauth.NewManager(nil, nil, nil)
	for index := 1; index <= 4; index++ {
		auth := &coreauth.Auth{
			ID:       "codex-" + string(rune('0'+index)),
			Provider: "codex",
			FileName: "codex-" + string(rune('a'+index-1)) + ".json",
			Status:   coreauth.StatusActive,
			Metadata: map[string]any{
				"chatgpt_account_id": "acct-1",
				"access_token":       "token-1",
			},
		}
		if _, err := manager.Register(context.Background(), auth); err != nil {
			t.Fatalf("register auth %d: %v", index, err)
		}
	}

	h := NewHandlerWithoutConfigFilePath(&config.Config{}, manager)
	release := make(chan struct{})
	started := make(chan string, 8)
	var inFlight atomic.Int32
	var maxInFlight atomic.Int32
	h.apiCallExecutor = func(_ context.Context, auth *coreauth.Auth, _ apiCallRequest) (apiCallResponse, error) {
		current := inFlight.Add(1)
		trackBatchCheckMaxInFlight(&maxInFlight, current)
		started <- auth.FileName
		<-release
		inFlight.Add(-1)
		return apiCallResponse{
			StatusCode: http.StatusOK,
			Body: `{
				"plan_type":"pro",
				"rate_limit":{"primary_window":{"used_percent":20}}
			}`,
		}, nil
	}

	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(
		http.MethodPost,
		"/v0/management/auth-files/batch-check",
		bytes.NewReader([]byte(`{"concurrency":3}`)),
	)
	ctx.Request.Header.Set("Content-Type", "application/json")

	done := make(chan struct{})
	go func() {
		h.BatchCheckAuthFiles(ctx)
		close(done)
	}()

	waitForBatchCheckStarts(t, started, 3)

	select {
	case name := <-started:
		t.Fatalf("expected only 3 workers before release, but got extra start for %s", name)
	case <-time.After(150 * time.Millisecond):
	}

	close(release)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for batch check handler to finish")
	}

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusOK, rec.Code, rec.Body.String())
	}
	if maxInFlight.Load() != 3 {
		t.Fatalf("expected max in-flight workers=3, got %d", maxInFlight.Load())
	}
}

func TestBatchCheckAuthFiles_RejectsInvalidConcurrency(t *testing.T) {
	t.Setenv("MANAGEMENT_PASSWORD", "")
	gin.SetMode(gin.TestMode)

	h := NewHandlerWithoutConfigFilePath(&config.Config{}, coreauth.NewManager(nil, nil, nil))

	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(
		http.MethodPost,
		"/v0/management/auth-files/batch-check",
		bytes.NewReader([]byte(`{"concurrency":99}`)),
	)
	ctx.Request.Header.Set("Content-Type", "application/json")

	h.BatchCheckAuthFiles(ctx)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusBadRequest, rec.Code, rec.Body.String())
	}
}

func TestBatchCheckAuthFiles_SkipsUnsupportedAndDisabledByDefault(t *testing.T) {
	t.Setenv("MANAGEMENT_PASSWORD", "")
	gin.SetMode(gin.TestMode)

	manager := coreauth.NewManager(nil, nil, nil)
	unsupported := &coreauth.Auth{
		ID:       "qwen-1",
		Provider: "qwen",
		FileName: "qwen.json",
		Status:   coreauth.StatusActive,
	}
	disabled := &coreauth.Auth{
		ID:       "kimi-1",
		Provider: "kimi",
		FileName: "kimi.json",
		Status:   coreauth.StatusDisabled,
		Disabled: true,
	}
	if _, err := manager.Register(context.Background(), unsupported); err != nil {
		t.Fatalf("register unsupported auth: %v", err)
	}
	if _, err := manager.Register(context.Background(), disabled); err != nil {
		t.Fatalf("register disabled auth: %v", err)
	}

	h := NewHandlerWithoutConfigFilePath(&config.Config{}, manager)
	calls := 0
	h.apiCallExecutor = func(_ context.Context, _ *coreauth.Auth, _ apiCallRequest) (apiCallResponse, error) {
		calls++
		return apiCallResponse{}, nil
	}

	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v0/management/auth-files/batch-check", bytes.NewReader([]byte(`{}`)))
	ctx.Request.Header.Set("Content-Type", "application/json")

	h.BatchCheckAuthFiles(ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusOK, rec.Code, rec.Body.String())
	}
	if calls != 0 {
		t.Fatalf("expected no api calls for skipped auths, got %d", calls)
	}

	var payload struct {
		Summary struct {
			CheckedCount int `json:"checked_count"`
			SkippedCount int `json:"skipped_count"`
		} `json:"summary"`
		Skipped []struct {
			Name   string `json:"name"`
			Reason string `json:"reason"`
		} `json:"skipped"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if payload.Summary.CheckedCount != 0 {
		t.Fatalf("expected checked_count=0, got %d", payload.Summary.CheckedCount)
	}
	if payload.Summary.SkippedCount != 2 {
		t.Fatalf("expected skipped_count=2, got %d", payload.Summary.SkippedCount)
	}
	if len(payload.Skipped) != 2 {
		t.Fatalf("expected 2 skipped entries, got %d", len(payload.Skipped))
	}
}

func TestBatchCheckAuthFiles_IncludeDisabledAndClassifiesInvalidated401(t *testing.T) {
	t.Setenv("MANAGEMENT_PASSWORD", "")
	gin.SetMode(gin.TestMode)

	manager := coreauth.NewManager(nil, nil, nil)
	auth := &coreauth.Auth{
		ID:       "kimi-1",
		Provider: "kimi",
		FileName: "kimi.json",
		Status:   coreauth.StatusDisabled,
		Disabled: true,
		Metadata: map[string]any{
			"access_token": "token-kimi",
		},
	}
	if _, err := manager.Register(context.Background(), auth); err != nil {
		t.Fatalf("register kimi auth: %v", err)
	}

	h := NewHandlerWithoutConfigFilePath(&config.Config{}, manager)
	h.apiCallExecutor = func(_ context.Context, auth *coreauth.Auth, _ apiCallRequest) (apiCallResponse, error) {
		if auth.FileName != "kimi.json" {
			t.Fatalf("unexpected auth %q", auth.FileName)
		}
		return apiCallResponse{
			StatusCode: http.StatusUnauthorized,
			Body:       `{"error":{"message":"unauthorized"}}`,
		}, nil
	}

	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v0/management/auth-files/batch-check", bytes.NewReader([]byte(`{"include_disabled":true}`)))
	ctx.Request.Header.Set("Content-Type", "application/json")

	h.BatchCheckAuthFiles(ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	var payload struct {
		Summary struct {
			CheckedCount         int            `json:"checked_count"`
			ClassificationCounts map[string]int `json:"classification_counts"`
		} `json:"summary"`
		Results []struct {
			Classification string `json:"classification"`
			StatusCode     int    `json:"status_code"`
		} `json:"results"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if payload.Summary.CheckedCount != 1 {
		t.Fatalf("expected checked_count=1, got %d", payload.Summary.CheckedCount)
	}
	if payload.Summary.ClassificationCounts[authFileBatchCheckClassificationInvalidated401] != 1 {
		t.Fatalf("expected invalidated_401 count 1, got %#v", payload.Summary.ClassificationCounts)
	}
	if len(payload.Results) != 1 || payload.Results[0].Classification != authFileBatchCheckClassificationInvalidated401 {
		t.Fatalf("unexpected results: %#v", payload.Results)
	}
	if payload.Results[0].StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected status_code 401, got %d", payload.Results[0].StatusCode)
	}
}

func TestBuildBatchCheckAggregate_ReenableNamesOnlyIncludesRecoveredOK(t *testing.T) {
	results := []authFileBatchCheckResult{
		{
			Name:           "disabled-recovered.json",
			Provider:       "codex",
			Disabled:       true,
			Classification: authFileBatchCheckClassificationOK,
			Bucket:         "full",
		},
		{
			Name:             "disabled-noquota.json",
			Provider:         "claude",
			Disabled:         true,
			Classification:   authFileBatchCheckClassificationNoQuota,
			Bucket:           "full",
			RemainingPercent: intPtr(0),
		},
	}

	aggregate := buildBatchCheckAggregate(results, nil)

	if len(aggregate.ActionCandidates.ReenableNames) != 1 {
		t.Fatalf("expected 1 reenable candidate, got %#v", aggregate.ActionCandidates.ReenableNames)
	}
	if aggregate.ActionCandidates.ReenableNames[0] != "disabled-recovered.json" {
		t.Fatalf("unexpected reenable candidate list: %#v", aggregate.ActionCandidates.ReenableNames)
	}
	if aggregate.RiskOverview.NoQuotaCount != 1 {
		t.Fatalf("expected no_quota_count=1, got %d", aggregate.RiskOverview.NoQuotaCount)
	}
	if aggregate.RiskOverview.ExhaustedCount != 1 {
		t.Fatalf("expected exhausted_count=1, got %d", aggregate.RiskOverview.ExhaustedCount)
	}
}

func intPtr(value int) *int {
	return &value
}
