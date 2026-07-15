package logging

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestResolveClientIP_DirectConnection(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name       string
		remoteAddr string
		wantIP     string
	}{
		{name: "ipv4", remoteAddr: "203.0.113.10:54321", wantIP: "203.0.113.10"},
		{name: "ipv6", remoteAddr: "[2001:db8::1]:443", wantIP: "2001:db8::1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			req := httptest.NewRequest(http.MethodGet, "/v1/chat/completions", nil)
			req.RemoteAddr = tt.remoteAddr
			ctx.Request = req

			if got := ResolveClientIP(ctx); got != tt.wantIP {
				t.Fatalf("ResolveClientIP() = %q, want %q", got, tt.wantIP)
			}
		})
	}
}

func TestResolveClientIP_NilContext(t *testing.T) {
	if got := ResolveClientIP(nil); got != "" {
		t.Fatalf("ResolveClientIP(nil) = %q, want empty", got)
	}
}

func TestClientIPFromContextUsesSnapshotBeforeGin(t *testing.T) {
	gin.SetMode(gin.TestMode)

	recorder := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(recorder)
	req := httptest.NewRequest(http.MethodGet, "/v1/chat/completions", nil)
	req.RemoteAddr = "203.0.113.10:54321"
	ginCtx.Request = req

	ctx := context.WithValue(context.Background(), "gin", ginCtx)
	ctx = WithClientIP(ctx, "198.51.100.8")

	if got := ClientIPFromContext(ctx); got != "198.51.100.8" {
		t.Fatalf("ClientIPFromContext() = %q, want snapshot", got)
	}
}

func TestClientIPFromContextFallbackAndEmpty(t *testing.T) {
	if got := ClientIPFromContext(context.Background()); got != "" {
		t.Fatalf("ClientIPFromContext(background) = %q, want empty", got)
	}

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(recorder)
	req := httptest.NewRequest(http.MethodGet, "/v1/chat/completions", nil)
	req.RemoteAddr = "203.0.113.10:54321"
	ginCtx.Request = req

	ctx := context.WithValue(context.Background(), "gin", ginCtx)
	if got := ClientIPFromContext(ctx); got != "203.0.113.10" {
		t.Fatalf("ClientIPFromContext(gin) = %q, want fallback IP", got)
	}
}
