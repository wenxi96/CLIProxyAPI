package logging

import (
	"context"
	"strings"

	"github.com/gin-gonic/gin"
)

type clientIPKey struct{}

// ResolveClientIP keeps HTTP logs and usage statistics on the same client IP parsing rules.
// Reuse Gin's ClientIP logic so proxied requests report the same client_ip across surfaces.
func ResolveClientIP(c *gin.Context) string {
	if c == nil || c.Request == nil {
		return ""
	}
	return strings.TrimSpace(c.ClientIP())
}

// WithClientIP stores an immutable client IP snapshot in ctx.
func WithClientIP(ctx context.Context, clientIP string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	clientIP = strings.TrimSpace(clientIP)
	if clientIP == "" {
		return ctx
	}
	return context.WithValue(ctx, clientIPKey{}, clientIP)
}

// ClientIPFromContext returns the immutable client IP snapshot, falling back to Gin when needed.
func ClientIPFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if clientIP, ok := ctx.Value(clientIPKey{}).(string); ok {
		if trimmed := strings.TrimSpace(clientIP); trimmed != "" {
			return trimmed
		}
	}
	ginCtx, ok := ctx.Value("gin").(*gin.Context)
	if !ok || ginCtx == nil {
		return ""
	}
	return ResolveClientIP(ginCtx)
}
