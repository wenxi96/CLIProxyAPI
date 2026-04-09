package authquota

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
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
	svc := NewService(Options{
		ConfigProvider: func() *config.Config { return &config.Config{} },
		TransportProvider: func(auth *coreauth.Auth, cfg *config.Config) http.RoundTripper {
			return roundTripFunc(func(req *http.Request) (*http.Response, error) {
				if req.URL.String() != codexUsageURL {
					t.Fatalf("unexpected url %q", req.URL.String())
				}
				if got := req.Header.Get("Authorization"); got != "Bearer token-1" {
					t.Fatalf("unexpected authorization header %q", got)
				}
				if got := req.Header.Get("Chatgpt-Account-Id"); got != "acct-1" {
					t.Fatalf("unexpected account header %q", got)
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body: io.NopCloser(strings.NewReader(`{
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
}

func TestClassifyAPIResponse_DoesNotTreatGeneric429AsNoQuota(t *testing.T) {
	classification, message, statusCode := classifyAPIResponse(apiCallResponse{
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
	classification, _, _ := classifyAPIResponse(apiCallResponse{
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
	}, apiCallRequest{
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
