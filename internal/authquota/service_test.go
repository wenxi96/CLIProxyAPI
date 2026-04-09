package authquota

import (
	"context"
	"io"
	"net/http"
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
			"chatgpt_account_id": "acct-1",
			"access_token":       "token-1",
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
