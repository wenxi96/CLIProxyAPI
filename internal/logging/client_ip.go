package logging

import (
	"strings"

	"github.com/gin-gonic/gin"
)

// ResolveClientIP 统一主 HTTP 日志与 usage 统计的客户端 IP 口径。
// 这里直接复用 Gin 的 ClientIP 解析规则，确保日志与管理接口中的
// client_ip 字段在代理/转发场景下保持一致。
func ResolveClientIP(c *gin.Context) string {
	if c == nil || c.Request == nil {
		return ""
	}
	return strings.TrimSpace(c.ClientIP())
}
