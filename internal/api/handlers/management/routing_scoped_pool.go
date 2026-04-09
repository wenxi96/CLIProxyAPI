package management

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
)

// GetRoutingScopedPoolStatus returns the runtime provider-local scoped-pool snapshot.
func (h *Handler) GetRoutingScopedPoolStatus(c *gin.Context) {
	if h == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "handler not initialized"})
		return
	}
	if h.authManager == nil {
		c.JSON(http.StatusOK, gin.H{
			"strategy":  "round-robin",
			"generated": false,
			"providers": gin.H{},
			"auths":     gin.H{},
		})
		return
	}
	snapshot := h.authManager.ScopedPoolSnapshot()
	c.JSON(http.StatusOK, gin.H{
		"strategy":    snapshot.Strategy,
		"generated":   true,
		"generatedAt": snapshot.GeneratedAt,
		"providers":   snapshot.Providers,
		"auths":       snapshot.Auths,
		"config": gin.H{
			"scoped-pool": config.NormalizeRoutingScopedPoolConfig(h.cfg.Routing.ScopedPool),
		},
	})
}
