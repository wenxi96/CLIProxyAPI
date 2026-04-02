package management

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
)

func waitForBatchCheckStarts(t *testing.T, started <-chan string, expected int) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for i := 0; i < expected; i++ {
		select {
		case <-started:
		case <-deadline:
			t.Fatalf("timeout waiting for %d batch check worker(s) to start", expected)
		}
	}
}

func trackBatchCheckMaxInFlight(maxInFlight *atomic.Int32, current int32) {
	for {
		previous := maxInFlight.Load()
		if current <= previous {
			return
		}
		if maxInFlight.CompareAndSwap(previous, current) {
			return
		}
	}
}

func TestCreateBatchCheckJob_CreatesPendingOrRunningJob(t *testing.T) {
	t.Setenv("MANAGEMENT_PASSWORD", "")
	gin.SetMode(gin.TestMode)

	manager := coreauth.NewManager(nil, nil, nil)
	auth := &coreauth.Auth{
		ID:       "codex-1",
		Provider: "codex",
		FileName: "codex-alpha.json",
		Status:   coreauth.StatusActive,
		Metadata: map[string]any{
			"chatgpt_account_id": "acct-1",
			"access_token":       "token-1",
		},
	}
	if _, err := manager.Register(context.Background(), auth); err != nil {
		t.Fatalf("register auth: %v", err)
	}

	h := NewHandlerWithoutConfigFilePath(&config.Config{}, manager)
	block := make(chan struct{})
	h.apiCallExecutor = func(_ context.Context, _ *coreauth.Auth, _ apiCallRequest) (apiCallResponse, error) {
		<-block
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
		"/v0/management/auth-files/batch-check-jobs",
		bytes.NewReader([]byte(`{"names":["codex-alpha.json"]}`)),
	)
	ctx.Request.Header.Set("Content-Type", "application/json")

	h.CreateBatchCheckJob(ctx)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusAccepted, rec.Code, rec.Body.String())
	}

	var payload struct {
		JobID  string `json:"job_id"`
		Status string `json:"status"`
		Scope  struct {
			RequestedCount  int  `json:"requested_count"`
			IncludeDisabled bool `json:"include_disabled"`
		} `json:"scope"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if payload.JobID == "" {
		t.Fatal("expected non-empty job_id")
	}
	if payload.Status != authFileBatchCheckJobStatusPending && payload.Status != authFileBatchCheckJobStatusRunning {
		t.Fatalf("expected pending or running status, got %q", payload.Status)
	}
	if payload.Scope.RequestedCount != 1 {
		t.Fatalf("expected requested_count=1, got %d", payload.Scope.RequestedCount)
	}
	if payload.Scope.IncludeDisabled {
		t.Fatal("expected include_disabled=false by default")
	}

	close(block)
}

func TestCreateBatchCheckJob_UsesRequestedConcurrency(t *testing.T) {
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
		"/v0/management/auth-files/batch-check-jobs",
		bytes.NewReader([]byte(`{"concurrency":3}`)),
	)
	ctx.Request.Header.Set("Content-Type", "application/json")

	h.CreateBatchCheckJob(ctx)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusAccepted, rec.Code, rec.Body.String())
	}

	var payload struct {
		Scope struct {
			Concurrency int `json:"concurrency"`
		} `json:"scope"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Scope.Concurrency != 3 {
		t.Fatalf("expected scope concurrency=3, got %d", payload.Scope.Concurrency)
	}

	waitForBatchCheckStarts(t, started, 3)

	select {
	case name := <-started:
		t.Fatalf("expected only 3 workers before release, but got extra start for %s", name)
	case <-time.After(150 * time.Millisecond):
	}

	close(release)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if maxInFlight.Load() == 3 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("expected max in-flight workers=3, got %d", maxInFlight.Load())
}

func TestResolveBatchCheckConcurrency_DefaultFormulaAndValidation(t *testing.T) {
	expectedDefault := runtime.NumCPU() / 2
	if expectedDefault < authFileBatchCheckDefaultConcurrencyMin {
		expectedDefault = authFileBatchCheckDefaultConcurrencyMin
	}
	if expectedDefault > authFileBatchCheckDefaultConcurrencyMax {
		expectedDefault = authFileBatchCheckDefaultConcurrencyMax
	}

	gotDefault, err := resolveBatchCheckConcurrency(0)
	if err != nil {
		t.Fatalf("resolve default concurrency: %v", err)
	}
	if gotDefault != expectedDefault {
		t.Fatalf("expected default concurrency=%d, got %d", expectedDefault, gotDefault)
	}

	if _, err := resolveBatchCheckConcurrency(-1); err == nil {
		t.Fatal("expected negative concurrency to fail validation")
	}
	if _, err := resolveBatchCheckConcurrency(authFileBatchCheckMaxConcurrency + 1); err == nil {
		t.Fatal("expected concurrency above max to fail validation")
	}

	gotCustom, err := resolveBatchCheckConcurrency(4)
	if err != nil {
		t.Fatalf("resolve custom concurrency: %v", err)
	}
	if gotCustom != 4 {
		t.Fatalf("expected custom concurrency=4, got %d", gotCustom)
	}
}

func TestGetBatchCheckJob_ReturnsProgressWhileRunning(t *testing.T) {
	t.Setenv("MANAGEMENT_PASSWORD", "")
	gin.SetMode(gin.TestMode)

	manager := coreauth.NewManager(nil, nil, nil)
	auth := &coreauth.Auth{
		ID:       "codex-1",
		Provider: "codex",
		FileName: "codex-alpha.json",
		Status:   coreauth.StatusActive,
		Metadata: map[string]any{
			"chatgpt_account_id": "acct-1",
			"access_token":       "token-1",
		},
	}
	if _, err := manager.Register(context.Background(), auth); err != nil {
		t.Fatalf("register auth: %v", err)
	}

	h := NewHandlerWithoutConfigFilePath(&config.Config{}, manager)
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	h.apiCallExecutor = func(_ context.Context, _ *coreauth.Auth, _ apiCallRequest) (apiCallResponse, error) {
		select {
		case started <- struct{}{}:
		default:
		}
		<-release
		return apiCallResponse{
			StatusCode: http.StatusOK,
			Body: `{
				"plan_type":"pro",
				"rate_limit":{"primary_window":{"used_percent":25}}
			}`,
		}, nil
	}

	createRec := httptest.NewRecorder()
	createCtx, _ := gin.CreateTestContext(createRec)
	createCtx.Request = httptest.NewRequest(
		http.MethodPost,
		"/v0/management/auth-files/batch-check-jobs",
		bytes.NewReader([]byte(`{"names":["codex-alpha.json"]}`)),
	)
	createCtx.Request.Header.Set("Content-Type", "application/json")
	h.CreateBatchCheckJob(createCtx)

	var createPayload struct {
		JobID string `json:"job_id"`
	}
	if err := json.Unmarshal(createRec.Body.Bytes(), &createPayload); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if createPayload.JobID == "" {
		t.Fatal("expected non-empty job_id")
	}

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for batch check job to start")
	}

	getRec := httptest.NewRecorder()
	getCtx, _ := gin.CreateTestContext(getRec)
	getCtx.Params = gin.Params{{Key: "id", Value: createPayload.JobID}}
	getCtx.Request = httptest.NewRequest(
		http.MethodGet,
		"/v0/management/auth-files/batch-check-jobs/"+createPayload.JobID,
		nil,
	)

	h.GetBatchCheckJob(getCtx)

	if getRec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusOK, getRec.Code, getRec.Body.String())
	}

	var payload struct {
		Status   string `json:"status"`
		Progress struct {
			Total           int    `json:"total"`
			Completed       int    `json:"completed"`
			Checked         int    `json:"checked"`
			Skipped         int    `json:"skipped"`
			Percent         int    `json:"percent"`
			CurrentName     string `json:"current_name"`
			CurrentProvider string `json:"current_provider"`
		} `json:"progress"`
	}
	if err := json.Unmarshal(getRec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode get response: %v", err)
	}

	if payload.Status != authFileBatchCheckJobStatusRunning {
		t.Fatalf("expected running status, got %q", payload.Status)
	}
	if payload.Progress.Total != 1 {
		t.Fatalf("expected total=1, got %d", payload.Progress.Total)
	}
	if payload.Progress.Completed != 0 {
		t.Fatalf("expected completed=0 while running, got %d", payload.Progress.Completed)
	}
	if payload.Progress.Checked != 0 {
		t.Fatalf("expected checked=0 while running, got %d", payload.Progress.Checked)
	}
	if payload.Progress.Skipped != 0 {
		t.Fatalf("expected skipped=0 while running, got %d", payload.Progress.Skipped)
	}
	if payload.Progress.Percent != 0 {
		t.Fatalf("expected percent=0 while first item still running, got %d", payload.Progress.Percent)
	}
	if payload.Progress.CurrentName != "codex-alpha.json" {
		t.Fatalf("expected current_name=codex-alpha.json, got %q", payload.Progress.CurrentName)
	}
	if payload.Progress.CurrentProvider != "codex" {
		t.Fatalf("expected current_provider=codex, got %q", payload.Progress.CurrentProvider)
	}

	close(release)
}

func TestGetBatchCheckJob_EncodesEmptyCollectionsAsArrays(t *testing.T) {
	t.Setenv("MANAGEMENT_PASSWORD", "")
	gin.SetMode(gin.TestMode)

	manager := coreauth.NewManager(nil, nil, nil)
	auth := &coreauth.Auth{
		ID:       "codex-1",
		Provider: "codex",
		FileName: "codex-alpha.json",
		Status:   coreauth.StatusActive,
		Metadata: map[string]any{
			"chatgpt_account_id": "acct-1",
			"access_token":       "token-1",
		},
	}
	if _, err := manager.Register(context.Background(), auth); err != nil {
		t.Fatalf("register auth: %v", err)
	}

	h := NewHandlerWithoutConfigFilePath(&config.Config{}, manager)
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	h.apiCallExecutor = func(_ context.Context, _ *coreauth.Auth, _ apiCallRequest) (apiCallResponse, error) {
		select {
		case started <- struct{}{}:
		default:
		}
		<-release
		return apiCallResponse{
			StatusCode: http.StatusOK,
			Body: `{
				"plan_type":"pro",
				"rate_limit":{"primary_window":{"used_percent":25}}
			}`,
		}, nil
	}

	createRec := httptest.NewRecorder()
	createCtx, _ := gin.CreateTestContext(createRec)
	createCtx.Request = httptest.NewRequest(
		http.MethodPost,
		"/v0/management/auth-files/batch-check-jobs",
		bytes.NewReader([]byte(`{"names":["codex-alpha.json"]}`)),
	)
	createCtx.Request.Header.Set("Content-Type", "application/json")
	h.CreateBatchCheckJob(createCtx)

	var createPayload struct {
		JobID string `json:"job_id"`
	}
	if err := json.Unmarshal(createRec.Body.Bytes(), &createPayload); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for running state")
	}

	getRec := httptest.NewRecorder()
	getCtx, _ := gin.CreateTestContext(getRec)
	getCtx.Params = gin.Params{{Key: "id", Value: createPayload.JobID}}
	getCtx.Request = httptest.NewRequest(
		http.MethodGet,
		"/v0/management/auth-files/batch-check-jobs/"+createPayload.JobID,
		nil,
	)

	h.GetBatchCheckJob(getCtx)

	var payload map[string]any
	if err := json.Unmarshal(getRec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode job payload: %v", err)
	}

	if _, ok := payload["results"].([]any); !ok {
		t.Fatalf("expected results to be encoded as array, got %#v", payload["results"])
	}
	if _, ok := payload["skipped"].([]any); !ok {
		t.Fatalf("expected skipped to be encoded as array, got %#v", payload["skipped"])
	}

	close(release)
}

func TestGetBatchCheckJob_ReturnsCompletedSummaryAndResults(t *testing.T) {
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
	disabledAuth := &coreauth.Auth{
		ID:       "kimi-1",
		Provider: "kimi",
		FileName: "kimi.json",
		Status:   coreauth.StatusDisabled,
		Disabled: true,
		Metadata: map[string]any{
			"access_token": "token-kimi",
		},
	}
	if _, err := manager.Register(context.Background(), codexAuth); err != nil {
		t.Fatalf("register codex auth: %v", err)
	}
	if _, err := manager.Register(context.Background(), disabledAuth); err != nil {
		t.Fatalf("register kimi auth: %v", err)
	}

	h := NewHandlerWithoutConfigFilePath(&config.Config{}, manager)
	h.apiCallExecutor = func(_ context.Context, auth *coreauth.Auth, _ apiCallRequest) (apiCallResponse, error) {
		switch auth.FileName {
		case "codex-alpha.json":
			return apiCallResponse{
				StatusCode: http.StatusOK,
				Body: `{
					"plan_type":"pro",
					"rate_limit":{"primary_window":{"used_percent":20}}
				}`,
			}, nil
		default:
			t.Fatalf("unexpected auth %q", auth.FileName)
			return apiCallResponse{}, nil
		}
	}

	createRec := httptest.NewRecorder()
	createCtx, _ := gin.CreateTestContext(createRec)
	createCtx.Request = httptest.NewRequest(
		http.MethodPost,
		"/v0/management/auth-files/batch-check-jobs",
		bytes.NewReader([]byte(`{"names":["codex-alpha.json","kimi.json","missing.json"]}`)),
	)
	createCtx.Request.Header.Set("Content-Type", "application/json")
	h.CreateBatchCheckJob(createCtx)

	if createRec.Code != http.StatusAccepted {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusAccepted, createRec.Code, createRec.Body.String())
	}

	var createPayload struct {
		JobID string `json:"job_id"`
	}
	if err := json.Unmarshal(createRec.Body.Bytes(), &createPayload); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	var payload struct {
		Status   string `json:"status"`
		Progress struct {
			Total     int `json:"total"`
			Completed int `json:"completed"`
			Checked   int `json:"checked"`
			Skipped   int `json:"skipped"`
			Success   int `json:"success"`
			Failed    int `json:"failed"`
			Percent   int `json:"percent"`
		} `json:"progress"`
		Summary struct {
			CheckedCount         int            `json:"checked_count"`
			AvailableCount       int            `json:"available_count"`
			SkippedCount         int            `json:"skipped_count"`
			ClassificationCounts map[string]int `json:"classification_counts"`
		} `json:"summary"`
		Results []struct {
			Name           string `json:"name"`
			Classification string `json:"classification"`
			Available      bool   `json:"available"`
		} `json:"results"`
		Skipped []struct {
			Name   string `json:"name"`
			Reason string `json:"reason"`
		} `json:"skipped"`
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		getRec := httptest.NewRecorder()
		getCtx, _ := gin.CreateTestContext(getRec)
		getCtx.Params = gin.Params{{Key: "id", Value: createPayload.JobID}}
		getCtx.Request = httptest.NewRequest(
			http.MethodGet,
			"/v0/management/auth-files/batch-check-jobs/"+createPayload.JobID,
			nil,
		)

		h.GetBatchCheckJob(getCtx)

		if getRec.Code != http.StatusOK {
			t.Fatalf("expected status %d, got %d with body %s", http.StatusOK, getRec.Code, getRec.Body.String())
		}
		if err := json.Unmarshal(getRec.Body.Bytes(), &payload); err != nil {
			t.Fatalf("decode get response: %v", err)
		}
		if payload.Status == authFileBatchCheckJobStatusCompleted {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting for completed job, last payload: %s", getRec.Body.String())
		}
		time.Sleep(20 * time.Millisecond)
	}

	if payload.Progress.Total != 3 {
		t.Fatalf("expected total=3, got %d", payload.Progress.Total)
	}
	if payload.Progress.Completed != 3 {
		t.Fatalf("expected completed=3, got %d", payload.Progress.Completed)
	}
	if payload.Progress.Checked != 1 {
		t.Fatalf("expected checked=1, got %d", payload.Progress.Checked)
	}
	if payload.Progress.Skipped != 2 {
		t.Fatalf("expected skipped=2, got %d", payload.Progress.Skipped)
	}
	if payload.Progress.Success != 1 {
		t.Fatalf("expected success=1, got %d", payload.Progress.Success)
	}
	if payload.Progress.Failed != 0 {
		t.Fatalf("expected failed=0, got %d", payload.Progress.Failed)
	}
	if payload.Progress.Percent != 100 {
		t.Fatalf("expected percent=100, got %d", payload.Progress.Percent)
	}

	if payload.Summary.CheckedCount != 1 {
		t.Fatalf("expected checked_count=1, got %d", payload.Summary.CheckedCount)
	}
	if payload.Summary.AvailableCount != 1 {
		t.Fatalf("expected available_count=1, got %d", payload.Summary.AvailableCount)
	}
	if payload.Summary.SkippedCount != 2 {
		t.Fatalf("expected skipped_count=2, got %d", payload.Summary.SkippedCount)
	}
	if payload.Summary.ClassificationCounts[authFileBatchCheckClassificationOK] != 1 {
		t.Fatalf("expected ok count 1, got %#v", payload.Summary.ClassificationCounts)
	}
	if len(payload.Results) != 1 || payload.Results[0].Name != "codex-alpha.json" || !payload.Results[0].Available {
		t.Fatalf("unexpected results: %#v", payload.Results)
	}
	if len(payload.Skipped) != 2 {
		t.Fatalf("expected 2 skipped entries, got %d", len(payload.Skipped))
	}
}

func TestGetBatchCheckJob_ReturnsNotFoundForUnknownJob(t *testing.T) {
	t.Setenv("MANAGEMENT_PASSWORD", "")
	gin.SetMode(gin.TestMode)

	h := NewHandlerWithoutConfigFilePath(&config.Config{}, coreauth.NewManager(nil, nil, nil))

	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Params = gin.Params{{Key: "id", Value: "missing-job"}}
	ctx.Request = httptest.NewRequest(
		http.MethodGet,
		"/v0/management/auth-files/batch-check-jobs/missing-job",
		nil,
	)

	h.GetBatchCheckJob(ctx)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusNotFound, rec.Code, rec.Body.String())
	}
}
