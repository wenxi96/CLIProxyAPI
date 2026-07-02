package authquota

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestServiceSupportsRecognizedProviders(t *testing.T) {
	svc := NewService(Options{})

	if !svc.Supports(&coreauth.Auth{Provider: "codex"}) {
		t.Fatal("expected codex to be supported")
	}
	if svc.Supports(&coreauth.Auth{Provider: "qwen"}) {
		t.Fatal("expected qwen to be unsupported")
	}
}

func TestServiceCheckCodexReturnsExhaustedWhenRemainingIsZero(t *testing.T) {
	var calls atomic.Int32
	svc := NewService(Options{
		ConfigProvider: func() *config.Config { return &config.Config{} },
		TransportProvider: func(auth *coreauth.Auth, cfg *config.Config) http.RoundTripper {
			return roundTripFunc(func(req *http.Request) (*http.Response, error) {
				calls.Add(1)
				if req.URL.String() != codexUsageURL && req.URL.String() != codexRateLimitResetCreditsURL {
					t.Fatalf("unexpected url %q", req.URL.String())
				}
				if got := req.Header.Get("Authorization"); got != "Bearer token-1" {
					t.Fatalf("unexpected authorization header %q", got)
				}
				if got := req.Header.Get("Chatgpt-Account-Id"); got != "acct-1" {
					t.Fatalf("unexpected account header %q", got)
				}
				if req.URL.String() == codexRateLimitResetCreditsURL {
					return &http.Response{
						StatusCode: http.StatusOK,
						Header:     make(http.Header),
						Body: io.NopCloser(strings.NewReader(`{
							"available_count":1,
							"credits":[{"id":"credit-1","reset_type":"codex_rate_limits","status":"available","granted_at":"2026-07-01T00:00:00Z","expires_at":"2026-08-01T00:00:00Z"}]
						}`)),
					}, nil
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body: io.NopCloser(strings.NewReader(`{
						"plan_type":"free",
						"subscription_active_until":"2026-11-30T00:00:00Z",
						"rate_limit":{
							"primary_window":{"used_percent":100,"limit_window_seconds":18000,"reset_after_seconds":1200},
							"secondary_window":{"used_percent":60,"limit_window_seconds":604800,"reset_after_seconds":7200}
						}
					}`)),
				}, nil
			})
		},
	})

	result, err := svc.Check(context.Background(), &coreauth.Auth{
		Provider: "codex",
		Metadata: map[string]any{
			"account_id":   "acct-1",
			"access_token": "token-1",
		},
	})
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if !result.Exhausted {
		t.Fatalf("expected exhausted result, got %#v", result)
	}
	if result.Classification != ClassificationNoQuota {
		t.Fatalf("expected classification %q, got %q", ClassificationNoQuota, result.Classification)
	}
	if result.RemainingPercent == nil || *result.RemainingPercent != 0 {
		t.Fatalf("expected remaining percent 0, got %#v", result.RemainingPercent)
	}
	if calls.Load() != 2 {
		t.Fatalf("expected usage and reset credits requests, got %d calls", calls.Load())
	}
	rawDetails, err := json.Marshal(result.Details)
	if err != nil {
		t.Fatalf("marshal details: %v", err)
	}
	var details struct {
		PlanType         string                               `json:"plan_type"`
		Subscription     string                               `json:"subscription_active_until"`
		Windows          []coreauth.QuotaWindow               `json:"windows"`
		ResetCredits     []coreauth.CodexRateLimitResetCredit `json:"rate_limit_reset_credits"`
		ResetCreditCount int                                  `json:"rate_limit_reset_credits_available_count"`
		ResetCreditError string                               `json:"rate_limit_reset_credits_error"`
	}
	if err := json.Unmarshal(rawDetails, &details); err != nil {
		t.Fatalf("decode details: %v", err)
	}
	if details.PlanType != "free" || details.Subscription != "2026-11-30T00:00:00Z" {
		t.Fatalf("unexpected codex details metadata: %#v", details)
	}
	if len(details.Windows) != 2 || details.Windows[0].LimitWindow == nil || *details.Windows[0].LimitWindow != 18000 {
		t.Fatalf("unexpected codex windows: %#v", details.Windows)
	}
	if len(details.ResetCredits) != 1 || details.ResetCredits[0].ID != "credit-1" || details.ResetCreditCount != 1 || details.ResetCreditError != "" {
		t.Fatalf("unexpected codex reset credit details: %#v", details)
	}
}

func TestClassifyAPIResponse_DoesNotTreatGeneric429AsNoQuota(t *testing.T) {
	classification, message, statusCode := classifyAPIResponse(APICallResponse{
		StatusCode: http.StatusTooManyRequests,
		Body:       `{"error":{"message":"rate limit exceeded"}}`,
	})
	if classification != ClassificationAPIError {
		t.Fatalf("expected classification %q, got %q", ClassificationAPIError, classification)
	}
	if message != "rate limit exceeded" {
		t.Fatalf("expected error message to be preserved, got %q", message)
	}
	if statusCode != http.StatusTooManyRequests {
		t.Fatalf("expected status %d, got %d", http.StatusTooManyRequests, statusCode)
	}
}

func TestClassifyAPIResponse_TreatsQuota429AsNoQuota(t *testing.T) {
	classification, _, _ := classifyAPIResponse(APICallResponse{
		StatusCode: http.StatusTooManyRequests,
		Body:       `{"error":{"message":"The usage limit has been reached"}}`,
	})
	if classification != ClassificationNoQuota {
		t.Fatalf("expected classification %q, got %q", ClassificationNoQuota, classification)
	}
}

func TestResolveCodexAccountID_AcceptsLegacyChatGPTKeys(t *testing.T) {
	auth := &coreauth.Auth{
		Metadata: map[string]any{
			"chatgpt_account_id": "acct-legacy",
		},
	}
	if got := resolveCodexAccountID(auth); got != "acct-legacy" {
		t.Fatalf("expected legacy account id, got %q", got)
	}
}

func TestServiceTransportFor_EmptyProxyUsesDirectTransport(t *testing.T) {
	svc := NewService(Options{
		ConfigProvider: func() *config.Config { return &config.Config{} },
	})

	rt := svc.transportFor(&coreauth.Auth{})
	if rt == nil {
		t.Fatal("expected non-nil transport")
	}
	if _, ok := rt.(*http.Transport); !ok {
		t.Fatalf("expected *http.Transport, got %T", rt)
	}
}

func TestServiceExecuteAPICallWithoutExplicitTransportProviderDoesNotPanic(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer token-1" {
			t.Fatalf("unexpected authorization header %q", got)
		}
		if got := r.Header.Get("Chatgpt-Account-Id"); got != "acct-1" {
			t.Fatalf("unexpected account header %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"rate_limit":{
				"primary_window":{"used_percent":100,"limit_window_seconds":18000,"reset_after_seconds":1200}
			}
		}`))
	}))
	defer server.Close()

	svc := NewService(Options{
		ConfigProvider: func() *config.Config { return &config.Config{} },
	})

	resp, err := svc.executeAPICall(context.Background(), &coreauth.Auth{
		Provider: "codex",
		Metadata: map[string]any{
			"account_id":   "acct-1",
			"access_token": "token-1",
		},
	}, APICallRequest{
		Method: "GET",
		URL:    server.URL,
		Header: map[string]string{
			"Authorization":      "Bearer $TOKEN$",
			"Chatgpt-Account-Id": "acct-1",
		},
	})
	if err != nil {
		t.Fatalf("executeAPICall() error = %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %#v", resp)
	}
}
