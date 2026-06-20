package management

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

// Quota exceeded toggles
func (h *Handler) GetSwitchProject(c *gin.Context) {
	c.JSON(200, gin.H{"switch-project": h.cfg.QuotaExceeded.SwitchProject})
}
func (h *Handler) PutSwitchProject(c *gin.Context) {
	h.updateBoolField(c, func(v bool) { h.cfg.QuotaExceeded.SwitchProject = v })
}

func (h *Handler) GetSwitchPreviewModel(c *gin.Context) {
	c.JSON(200, gin.H{"switch-preview-model": h.cfg.QuotaExceeded.SwitchPreviewModel})
}
func (h *Handler) PutSwitchPreviewModel(c *gin.Context) {
	h.updateBoolField(c, func(v bool) { h.cfg.QuotaExceeded.SwitchPreviewModel = v })
}

func (h *Handler) GetAutoDisableAuthFileOnZeroQuota(c *gin.Context) {
	c.JSON(200, gin.H{"auto-disable-auth-file-on-zero-quota": h.cfg.QuotaExceeded.AutoDisableAuthFileOnZeroQuota})
}
func (h *Handler) PutAutoDisableAuthFileOnZeroQuota(c *gin.Context) {
	h.updateBoolField(c, func(v bool) { h.cfg.QuotaExceeded.AutoDisableAuthFileOnZeroQuota = v })
}

// Auto-disable auth file quota threshold percent
func (h *Handler) GetAutoDisableAuthFileQuotaThresholdPercent(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"auto-disable-auth-file-quota-threshold-percent": h.cfg.QuotaExceeded.AutoDisableAuthFileQuotaThresholdPercent})
}

func (h *Handler) PutAutoDisableAuthFileQuotaThresholdPercent(c *gin.Context) {
	h.updateIntFieldClamped(c, func(v int) { h.cfg.QuotaExceeded.AutoDisableAuthFileQuotaThresholdPercent = v }, 0, config.MaxAutoDisableQuotaThresholdPercent)
}

func (h *Handler) PatchAutoDisableAuthFileQuotaThresholdPercent(c *gin.Context) {
	h.updateIntFieldClamped(c, func(v int) { h.cfg.QuotaExceeded.AutoDisableAuthFileQuotaThresholdPercent = v }, 0, config.MaxAutoDisableQuotaThresholdPercent)
}

// updateIntFieldClamped updates an integer field with clamping to the specified range.
func (h *Handler) updateIntFieldClamped(c *gin.Context, set func(int), minVal, maxVal int) {
	var body struct {
		Value *int `json:"value"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.Value == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	v := *body.Value
	if v < minVal {
		v = minVal
	}
	if v > maxVal {
		v = maxVal
	}
	set(v)
	h.persist(c)
}

// ResetQuota clears quota/cooldown routing state for one auth index.
func (h *Handler) ResetQuota(c *gin.Context) {
	if h.authManager == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "core auth manager unavailable"})
		return
	}

	var req struct {
		AuthIndex string `json:"auth_index"`
	}
	if errBindJSON := c.ShouldBindJSON(&req); errBindJSON != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	authIndex := strings.TrimSpace(req.AuthIndex)
	if authIndex == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "auth_index is required"})
		return
	}

	auth := h.authByIndex(authIndex)
	if auth == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "auth not found"})
		return
	}

	updated, models, errReset := h.authManager.ResetQuota(c.Request.Context(), auth.ID)
	if errReset != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to reset quota: %v", errReset)})
		return
	}
	if updated == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "auth not found"})
		return
	}
	updated.EnsureIndex()

	c.JSON(http.StatusOK, gin.H{
		"status":     "ok",
		"auth_index": updated.Index,
		"models":     models,
	})
}
