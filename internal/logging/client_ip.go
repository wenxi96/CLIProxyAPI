package logging

import (
	"strings"

	"github.com/gin-gonic/gin"
)

// ResolveClientIP keeps HTTP logs and usage statistics on the same client IP parsing rules.
// Reuse Gin's ClientIP logic so proxied requests report the same client_ip across surfaces.
func ResolveClientIP(c *gin.Context) string {
	if c == nil || c.Request == nil {
		return ""
	}
	return strings.TrimSpace(c.ClientIP())
}
