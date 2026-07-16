package redisqueue

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	internallogging "github.com/router-for-me/CLIProxyAPI/v7/internal/logging"
	internalusage "github.com/router-for-me/CLIProxyAPI/v7/internal/usage"
	coreusage "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
)

func TestUsageQueuePluginPayloadIncludesStableFieldsAndSuccess(t *testing.T) {
	withEnabledQueue(t, func() {
		ctx := internallogging.WithRequestID(context.Background(), "ctx-request-id")
		ctx = internallogging.WithEndpoint(ctx, "POST /v1/chat/completions")
		ctx = internallogging.WithResponseStatusHolder(ctx)
		internallogging.SetResponseStatus(ctx, http.StatusOK)
		responseHeaders := http.Header{}
		responseHeaders.Add("X-Upstream-Request-Id", "upstream-req-1")
		responseHeaders.Add("Retry-After", "30")
		responseHeaders.Add("Authorization", "Bearer raw-access-token")
		responseHeaders.Add("Set-Cookie", "session=raw-cookie")
		responseHeaders.Add("X-Api-Key", "sk-raw-response-key")
		responseHeaders.Add("X-Trace-Token", "raw-trace-token")
		responseHeaders.Add("Location", "https://example.com/callback?signature=raw-location-signature")
		responseHeaders.Add("WWW-Authenticate", `Bearer token="raw-auth-challenge"`)
		responseHeaders.Add("X-Upstream-Error", "upstream token=raw-error-token")
		responseHeaders.Add("X-Signed-Metadata", "signature=raw-header-signature")
		responseHeaders.Add("X-Credential", "raw-header-credential")
		responseHeaders.Add("X-Password", "raw-header-password")
		responseHeaders.Add("X-Codex-Api-Key", "plainsecret123")
		responseHeaders.Add("X-Codex-Turn-State", `{"turn_id":"sensitive-turn"}`)
		responseHeaders.Add("X-RateLimit-Api-Key", "plainsecret123")
		responseHeaders.Add("Tracestate", "vendor=plainsecret123")

		plugin := &usageQueuePlugin{}
		plugin.HandleUsage(ctx, coreusage.Record{
			Provider:            "openai",
			ExecutorType:        "KimiExecutor",
			Model:               "gpt-5.4",
			Alias:               "client-gpt",
			APIKey:              "test-key",
			AuthIndex:           "0",
			AuthType:            "apikey",
			Source:              "user@example.com",
			ReasoningEffort:     "medium",
			ServiceTier:         "auto",
			ResponseServiceTier: "default",
			Generate:            coreusage.GenerateFlag(true),
			RequestedAt:         time.Date(2026, 4, 25, 0, 0, 0, 0, time.UTC),
			Latency:             1500 * time.Millisecond,
			Detail: coreusage.Detail{
				InputTokens:  10,
				OutputTokens: 20,
				TotalTokens:  30,
			},
			ResponseHeaders: responseHeaders.Clone(),
		})
		responseHeaders.Set("Retry-After", "999")

		payload := popSinglePayload(t)
		requireStringField(t, payload, "provider", "openai")
		requireStringField(t, payload, "executor_type", "KimiExecutor")
		requireStringField(t, payload, "model", "gpt-5.4")
		requireStringField(t, payload, "alias", "client-gpt")
		requireStringField(t, payload, "endpoint", "POST /v1/chat/completions")
		requireStringField(t, payload, "auth_type", "apikey")
		requireStringField(t, payload, "model_alias", "client-gpt")
		requireMissingField(t, payload, "user_api_key")
		requireMissingField(t, payload, "api_key")
		requireStringField(t, payload, "api_key_hash", internalusage.APIKeyHash("test-key"))
		requireStringField(t, payload, "request_id", "ctx-request-id")
		requireStringField(t, payload, "reasoning_effort", "medium")
		requireStringField(t, payload, "service_tier", "auto")
		requireMissingField(t, payload, "request_service_tier")
		requireStringField(t, payload, "response_service_tier", "default")
		requireHeaderField(t, payload, "response_headers", "X-Upstream-Request-Id", []string{"upstream-req-1"})
		requireHeaderField(t, payload, "response_headers", "Retry-After", []string{"30"})
		requireMissingHeaderField(t, payload, "response_headers", "Authorization")
		requireMissingHeaderField(t, payload, "response_headers", "Set-Cookie")
		requireMissingHeaderField(t, payload, "response_headers", "X-Api-Key")
		requireMissingHeaderField(t, payload, "response_headers", "X-Trace-Token")
		requireMissingHeaderField(t, payload, "response_headers", "Location")
		requireMissingHeaderField(t, payload, "response_headers", "WWW-Authenticate")
		requireMissingHeaderField(t, payload, "response_headers", "X-Upstream-Error")
		requireMissingHeaderField(t, payload, "response_headers", "X-Signed-Metadata")
		requireMissingHeaderField(t, payload, "response_headers", "X-Credential")
		requireMissingHeaderField(t, payload, "response_headers", "X-Password")
		requireMissingHeaderField(t, payload, "response_headers", "X-Codex-Api-Key")
		requireMissingHeaderField(t, payload, "response_headers", "X-Codex-Turn-State")
		requireMissingHeaderField(t, payload, "response_headers", "X-RateLimit-Api-Key")
		requireMissingHeaderField(t, payload, "response_headers", "Tracestate")
		requireBoolField(t, payload, "failed", false)
		requireBoolField(t, payload, "generate", true)
		requireFailField(t, payload, http.StatusOK, "")
		data, errMarshal := json.Marshal(payload)
		if errMarshal != nil {
			t.Fatalf("marshal payload: %v", errMarshal)
		}
		for _, secret := range []string{
			"raw-access-token",
			"raw-cookie",
			"sk-raw-response-key",
			"raw-trace-token",
			"raw-location-signature",
			"raw-auth-challenge",
			"raw-error-token",
			"raw-header-signature",
			"raw-header-credential",
			"raw-header-password",
		} {
			if strings.Contains(string(data), secret) {
				t.Fatalf("queue payload leaked %q: %s", secret, data)
			}
		}
	})
}

func TestUsageQueuePluginPreservesExplicitProviderZeroUsage(t *testing.T) {
	withEnabledQueue(t, func() {
		ctx := internallogging.WithRequestID(context.Background(), "req-zero-usage")
		ctx = internallogging.WithEndpoint(ctx, "POST /v1/responses")
		plugin := &usageQueuePlugin{}
		plugin.HandleUsage(ctx, coreusage.Record{
			Provider:      "openai",
			Model:         "gpt-5.4",
			AuthIndex:     "auth-zero-usage",
			RequestedAt:   time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC),
			UsageObserved: true,
		})

		payload := popSinglePayload(t)
		raw, ok := payload["tokens"]
		if !ok {
			t.Fatal("payload missing tokens")
		}
		var tokens internalusage.RequestTokenStats
		if err := json.Unmarshal(raw, &tokens); err != nil {
			t.Fatalf("unmarshal tokens: %v", err)
		}
		if tokens.TokenUsageSource != internalusage.TokenUsageSourceProvider || tokens.TotalTokens != 0 {
			t.Fatalf("tokens = %+v, want explicit provider-reported zero usage", tokens)
		}
	})
}

func TestUsageQueuePluginPayloadIncludesGenerateFalse(t *testing.T) {
	withEnabledQueue(t, func() {
		ctx := internallogging.WithResponseStatusHolder(context.Background())
		internallogging.SetResponseStatus(ctx, http.StatusOK)

		(&usageQueuePlugin{}).HandleUsage(ctx, coreusage.Record{
			Provider: "openai",
			Model:    "gpt-5.4",
			Generate: coreusage.GenerateFlag(false),
			Detail: coreusage.Detail{
				InputTokens: 1,
				TotalTokens: 1,
			},
		})

		payload := popSinglePayload(t)
		requireBoolField(t, payload, "generate", false)
	})
}

func TestUsageQueuePluginPayloadDefaultsGenerateTrueWhenOmitted(t *testing.T) {
	withEnabledQueue(t, func() {
		ctx := internallogging.WithResponseStatusHolder(context.Background())
		internallogging.SetResponseStatus(ctx, http.StatusOK)

		// Legacy callers construct usage.Record without Generate; omission must publish as true.
		(&usageQueuePlugin{}).HandleUsage(ctx, coreusage.Record{
			Provider: "openai",
			Model:    "gpt-5.4",
			Detail: coreusage.Detail{
				InputTokens: 1,
				TotalTokens: 1,
			},
		})

		payload := popSinglePayload(t)
		requireBoolField(t, payload, "generate", true)
	})
}

func TestUsageQueuePluginEmitsSingleCanonicalAutoTier(t *testing.T) {
	withEnabledQueue(t, func() {
		ctx := coreusage.WithServiceTier(context.Background(), coreusage.AutoServiceTier)
		ctx = internallogging.WithResponseStatusHolder(ctx)
		internallogging.SetResponseStatus(ctx, http.StatusOK)

		(&usageQueuePlugin{}).HandleUsage(ctx, coreusage.Record{
			Provider: "openai",
			Model:    "gpt-5.4",
			Detail: coreusage.Detail{
				InputTokens: 1,
				TotalTokens: 1,
			},
		})

		payload := popSinglePayload(t)
		requireStringField(t, payload, "service_tier", "auto")
		requireMissingField(t, payload, "request_service_tier")
	})
}

func TestUsageQueuePluginAcceptsDeprecatedRequestTierRecordField(t *testing.T) {
	withEnabledQueue(t, func() {
		ctx := internallogging.WithResponseStatusHolder(context.Background())
		internallogging.SetResponseStatus(ctx, http.StatusOK)

		(&usageQueuePlugin{}).HandleUsage(ctx, coreusage.Record{
			Provider:           "openai",
			Model:              "gpt-5.4",
			RequestServiceTier: "priority",
			Detail:             coreusage.Detail{InputTokens: 1, TotalTokens: 1},
		})

		payload := popSinglePayload(t)
		requireStringField(t, payload, "service_tier", "priority")
		requireMissingField(t, payload, "request_service_tier")
	})
}

func TestUsageQueuePluginAsyncUsesRecordResponseHeaders(t *testing.T) {
	withEnabledQueue(t, func() {
		ctx := internallogging.WithRequestID(context.Background(), "ctx-request-id")
		ctx = internallogging.WithEndpoint(ctx, "POST /v1/chat/completions")
		ctx = internallogging.WithResponseStatusHolder(ctx)
		ctx = internallogging.WithResponseHeadersHolder(ctx)
		internallogging.SetResponseStatus(ctx, http.StatusOK)
		initialHeaders := http.Header{}
		initialHeaders.Set("X-Upstream-Request-Id", "upstream-req-1")
		internallogging.SetResponseHeaders(ctx, initialHeaders)

		mgr := coreusage.NewManager(16)
		defer mgr.Stop()

		mgr.Register(pluginFunc(func(ctx context.Context, _ coreusage.Record) {
			nextHeaders := http.Header{}
			nextHeaders.Set("X-Upstream-Request-Id", "upstream-req-2")
			internallogging.SetResponseHeaders(ctx, nextHeaders)
		}))
		mgr.Register(&usageQueuePlugin{})

		mgr.Publish(ctx, coreusage.Record{
			Provider:    "openai",
			Model:       "gpt-5.4",
			Alias:       "client-gpt",
			APIKey:      "test-key",
			AuthIndex:   "0",
			AuthType:    "apikey",
			Source:      "user@example.com",
			RequestedAt: time.Date(2026, 4, 25, 0, 0, 0, 0, time.UTC),
			Latency:     1500 * time.Millisecond,
			Detail: coreusage.Detail{
				InputTokens:  10,
				OutputTokens: 20,
				TotalTokens:  30,
			},
			ResponseHeaders: internallogging.GetResponseHeaders(ctx),
		})

		payload := waitForSinglePayload(t, 2*time.Second)
		requireHeaderField(t, payload, "response_headers", "X-Upstream-Request-Id", []string{"upstream-req-1"})
	})
}

func TestUsageQueuePluginPayloadIncludesStableFieldsAndFailureAndGinRequestID(t *testing.T) {
	withEnabledQueue(t, func() {
		ctx := internallogging.WithRequestID(context.Background(), "gin-request-id")
		ctx = internallogging.WithEndpoint(ctx, "GET /v1/responses")
		ctx = internallogging.WithResponseStatusHolder(ctx)
		internallogging.SetResponseStatus(ctx, http.StatusInternalServerError)

		plugin := &usageQueuePlugin{}
		plugin.HandleUsage(ctx, coreusage.Record{
			Provider:    "openai",
			Model:       "gpt-5.4-mini",
			Alias:       "client-mini",
			APIKey:      "test-key",
			AuthIndex:   "0",
			AuthType:    "apikey",
			Source:      "user@example.com",
			RequestedAt: time.Date(2026, 4, 25, 0, 0, 0, 0, time.UTC),
			Latency:     2500 * time.Millisecond,
			Fail: coreusage.Failure{
				StatusCode: http.StatusInternalServerError,
				Body:       "upstream failed",
			},
			Detail: coreusage.Detail{
				InputTokens:  10,
				OutputTokens: 20,
				TotalTokens:  30,
			},
		})

		payload := popSinglePayload(t)
		requireStringField(t, payload, "provider", "openai")
		requireStringField(t, payload, "model", "gpt-5.4-mini")
		requireStringField(t, payload, "alias", "client-mini")
		requireStringField(t, payload, "endpoint", "GET /v1/responses")
		requireStringField(t, payload, "auth_type", "apikey")
		requireMissingField(t, payload, "user_api_key")
		requireStringField(t, payload, "request_id", "gin-request-id")
		requireBoolField(t, payload, "failed", true)
		requireFailField(t, payload, http.StatusInternalServerError, "upstream failed")
	})
}

func TestUsageQueuePluginRedactsFailureBody(t *testing.T) {
	withEnabledQueue(t, func() {
		ctx := internallogging.WithRequestID(context.Background(), "gin-request-id")
		ctx = internallogging.WithEndpoint(ctx, "GET /v1/responses")
		ctx = internallogging.WithResponseStatusHolder(ctx)
		internallogging.SetResponseStatus(ctx, http.StatusInternalServerError)

		plugin := &usageQueuePlugin{}
		plugin.HandleUsage(ctx, coreusage.Record{
			Provider:    "openai",
			Model:       "gpt-5.4-mini",
			Alias:       "client-mini",
			APIKey:      "test-key",
			AuthIndex:   "0",
			AuthType:    "apikey",
			Source:      "user@example.com",
			RequestedAt: time.Date(2026, 4, 25, 0, 0, 0, 0, time.UTC),
			Latency:     2500 * time.Millisecond,
			Fail: coreusage.Failure{
				StatusCode: http.StatusInternalServerError,
				Body:       `{"error":{"message":"bad key sk-raw-failure-key"},"authorization":"Basic raw-basic-token","token":"raw-json-token","x-api-token":"raw-x-token","cookie":"session=raw-cookie","total_tokens":12}`,
			},
		})

		payload := popSinglePayload(t)
		requireBoolField(t, payload, "failed", true)
		requireFailBodyExcludes(t, payload, "sk-raw-failure-key", "raw-basic-token", "raw-json-token", "raw-x-token", "raw-cookie")
	})
}

func TestUsageQueuePluginAsyncIgnoresRecycledGinContext(t *testing.T) {
	withEnabledQueue(t, func() {
		ginCtx := newTestGinContext(t, http.MethodPost, "/v1/chat/completions", http.StatusOK)
		ctx := context.WithValue(context.Background(), "gin", ginCtx)
		ctx = internallogging.WithRequestID(ctx, "ctx-request-id")
		ctx = internallogging.WithEndpoint(ctx, "POST /v1/chat/completions")
		ctx = internallogging.WithClientIP(ctx, "198.51.100.22")
		ctx = internallogging.WithResponseStatusHolder(ctx)
		internallogging.SetResponseStatus(ctx, http.StatusInternalServerError)

		mgr := coreusage.NewManager(16)
		defer mgr.Stop()

		mgr.Register(pluginFunc(func(_ context.Context, _ coreusage.Record) {
			ginCtx.Request = httptest.NewRequest(http.MethodGet, "http://example.com/v1/responses", nil)
			ginCtx.Status(http.StatusOK)
		}))
		mgr.Register(&usageQueuePlugin{})

		mgr.Publish(ctx, coreusage.Record{
			Provider:    "openai",
			Model:       "gpt-5.4",
			Alias:       "client-gpt",
			APIKey:      "test-key",
			AuthIndex:   "0",
			AuthType:    "apikey",
			Source:      "user@example.com",
			RequestedAt: time.Date(2026, 4, 25, 0, 0, 0, 0, time.UTC),
			Latency:     1500 * time.Millisecond,
			Fail: coreusage.Failure{
				StatusCode: http.StatusBadGateway,
				Body:       "bad gateway",
			},
			Detail: coreusage.Detail{
				InputTokens:  10,
				OutputTokens: 20,
				TotalTokens:  30,
			},
		})

		payload := waitForSinglePayload(t, 2*time.Second)
		requireStringField(t, payload, "endpoint", "POST /v1/chat/completions")
		requireStringField(t, payload, "alias", "client-gpt")
		requireMissingField(t, payload, "user_api_key")
		requireMissingField(t, payload, "api_key")
		requireStringField(t, payload, "request_id", "ctx-request-id")
		requireStringField(t, payload, "client_ip", "198.51.100.22")
		requireBoolField(t, payload, "failed", true)
		requireFailField(t, payload, http.StatusBadGateway, "bad gateway")
	})
}

func withEnabledQueue(t *testing.T, fn func()) {
	t.Helper()

	prevQueueEnabled := Enabled()
	prevUsageEnabled := UsageStatisticsEnabled()

	SetEnabled(false)
	SetEnabled(true)
	SetUsageStatisticsEnabled(true)

	defer func() {
		SetEnabled(false)
		SetEnabled(prevQueueEnabled)
		SetUsageStatisticsEnabled(prevUsageEnabled)
	}()

	fn()
}

func newTestGinContext(t *testing.T, method, path string, status int) *gin.Context {
	t.Helper()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(recorder)
	ginCtx.Request = httptest.NewRequest(method, "http://example.com"+path, nil)
	if status != 0 {
		ginCtx.Status(status)
	}
	return ginCtx
}

func popSinglePayload(t *testing.T) map[string]json.RawMessage {
	t.Helper()

	items := PopOldest(10)
	if len(items) != 1 {
		t.Fatalf("PopOldest() items = %d, want 1", len(items))
	}

	var payload map[string]json.RawMessage
	if err := json.Unmarshal(items[0], &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	return payload
}

func waitForSinglePayload(t *testing.T, timeout time.Duration) map[string]json.RawMessage {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		items := PopOldest(10)
		if len(items) == 0 {
			time.Sleep(10 * time.Millisecond)
			continue
		}
		if len(items) != 1 {
			t.Fatalf("PopOldest() items = %d, want 1", len(items))
		}
		var payload map[string]json.RawMessage
		if err := json.Unmarshal(items[0], &payload); err != nil {
			t.Fatalf("unmarshal payload: %v", err)
		}
		return payload
	}
	t.Fatalf("timeout waiting for queued payload")
	return nil
}

func requireStringField(t *testing.T, payload map[string]json.RawMessage, key, want string) {
	t.Helper()

	raw, ok := payload[key]
	if !ok {
		t.Fatalf("payload missing %q", key)
	}
	var got string
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal %q: %v", key, err)
	}
	if got != want {
		t.Fatalf("%s = %q, want %q", key, got, want)
	}
}

func requireMissingField(t *testing.T, payload map[string]json.RawMessage, key string) {
	t.Helper()

	if _, ok := payload[key]; ok {
		t.Fatalf("payload unexpectedly contains %q", key)
	}
}

type pluginFunc func(context.Context, coreusage.Record)

func (fn pluginFunc) HandleUsage(ctx context.Context, record coreusage.Record) {
	fn(ctx, record)
}

func requireBoolField(t *testing.T, payload map[string]json.RawMessage, key string, want bool) {
	t.Helper()

	raw, ok := payload[key]
	if !ok {
		t.Fatalf("payload missing %q", key)
	}
	var got bool
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal %q: %v", key, err)
	}
	if got != want {
		t.Fatalf("%s = %t, want %t", key, got, want)
	}
}

func requireFailField(t *testing.T, payload map[string]json.RawMessage, wantStatus int, wantBody string) {
	t.Helper()

	raw, ok := payload["fail"]
	if !ok {
		t.Fatalf("payload missing %q", "fail")
	}
	var got struct {
		StatusCode int    `json:"status_code"`
		Body       string `json:"body"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal fail: %v", err)
	}
	if got.StatusCode != wantStatus || got.Body != wantBody {
		t.Fatalf("fail = {status_code:%d body:%q}, want {status_code:%d body:%q}", got.StatusCode, got.Body, wantStatus, wantBody)
	}
}

func requireFailBodyExcludes(t *testing.T, payload map[string]json.RawMessage, secrets ...string) {
	t.Helper()

	raw, ok := payload["fail"]
	if !ok {
		t.Fatalf("payload missing %q", "fail")
	}
	var got struct {
		Body string `json:"body"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal fail: %v", err)
	}
	if !strings.Contains(got.Body, "[redacted]") {
		t.Fatalf("fail body = %q, want redacted marker", got.Body)
	}
	for _, secret := range secrets {
		if strings.Contains(got.Body, secret) {
			t.Fatalf("fail body leaked %q: %s", secret, got.Body)
		}
	}
}

func requireHeaderField(t *testing.T, payload map[string]json.RawMessage, field, key string, want []string) {
	t.Helper()

	raw, ok := payload[field]
	if !ok {
		t.Fatalf("payload missing %q", field)
	}
	var headers map[string][]string
	if err := json.Unmarshal(raw, &headers); err != nil {
		t.Fatalf("unmarshal %q: %v", field, err)
	}
	got, ok := headers[key]
	if !ok {
		t.Fatalf("%s missing header %q", field, key)
	}
	if len(got) != len(want) {
		t.Fatalf("%s[%q] = %v, want %v", field, key, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s[%q] = %v, want %v", field, key, got, want)
		}
	}
}

func requireMissingHeaderField(t *testing.T, payload map[string]json.RawMessage, field, key string) {
	t.Helper()

	raw, ok := payload[field]
	if !ok {
		return
	}
	var headers map[string][]string
	if err := json.Unmarshal(raw, &headers); err != nil {
		t.Fatalf("unmarshal %q: %v", field, err)
	}
	if _, ok := headers[key]; ok {
		t.Fatalf("%s unexpectedly contains header %q", field, key)
	}
}
