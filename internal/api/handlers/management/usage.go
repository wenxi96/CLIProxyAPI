package management

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/redisqueue"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/usage"
)

type usageExportPayload struct {
	Version    int                      `json:"version"`
	ExportedAt time.Time                `json:"exported_at"`
	Usage      usage.StatisticsSnapshot `json:"usage"`
}

type usageImportPayload struct {
	Version int                      `json:"version"`
	Usage   usage.StatisticsSnapshot `json:"usage"`
}

type usageQueueRecord []byte

func (r usageQueueRecord) MarshalJSON() ([]byte, error) {
	if json.Valid(r) {
		return append([]byte(nil), r...), nil
	}
	return json.Marshal(string(r))
}

// GetUsageStatistics returns the in-memory request statistics snapshot.
func (h *Handler) GetUsageStatistics(c *gin.Context) {
	var snapshot usage.StatisticsSnapshot
	if h != nil && h.usageStats != nil {
		snapshot = h.usageStats.Snapshot()
	}
	c.JSON(http.StatusOK, gin.H{
		"usage":           snapshot,
		"failed_requests": snapshot.FailureCount,
	})
}

// GetUsageAuthRequests returns paginated request details for one auth_index.
func (h *Handler) GetUsageAuthRequests(c *gin.Context) {
	if h == nil || h.usageStats == nil {
		_, cursorPresent := c.GetQuery("cursor")
		if strings.TrimSpace(c.Query("pagination_mode")) == "cursor_v1" || cursorPresent {
			setUsageNoStore(c)
			writeUsageProjectionError(c, usage.ErrProjectionUnavailable)
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": "usage statistics unavailable"})
		return
	}
	authIndex := strings.TrimSpace(c.Param("auth_index"))
	if authIndex == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "auth_index is required"})
		return
	}
	paginationMode := strings.TrimSpace(c.Query("pagination_mode"))
	cursorValue, cursorPresent := c.GetQuery("cursor")
	if paginationMode == "cursor_v1" || cursorPresent {
		if paginationMode != "" && paginationMode != "cursor_v1" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid pagination mode"})
			return
		}
		if _, offsetPresent := c.GetQuery("offset"); offsetPresent || strings.TrimSpace(c.Query("to_exclusive")) != "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "cursor mode cannot be combined with offset or to_exclusive"})
			return
		}
		h.getUsageAuthRequestsCursor(c, authIndex, strings.TrimSpace(cursorValue))
		return
	}
	if paginationMode != "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid pagination mode"})
		return
	}

	filter, errFilter := parseAuthRequestFilter(c)
	if errFilter != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": errFilter.Error()})
		return
	}

	page, errPage := h.usageStats.ListAuthRequestsWithError(authIndex, filter)
	if errPage != nil {
		setUsageNoStore(c)
		writeUsageAuthCursorError(c, errPage)
		return
	}
	if filter.ToExclusive {
		setUsageNoStore(c)
		c.JSON(http.StatusOK, struct {
			usage.AuthRequestPage
			PaginationMode string `json:"pagination_mode"`
		}{AuthRequestPage: page, PaginationMode: "offset_v1_exclusive"})
		return
	}
	c.JSON(http.StatusOK, page)
}

// ExportUsageStatistics returns a complete usage snapshot for backup/migration.
func (h *Handler) ExportUsageStatistics(c *gin.Context) {
	var snapshot usage.StatisticsSnapshot
	if h != nil && h.usageStats != nil {
		snapshot = h.usageStats.Snapshot()
	}
	c.JSON(http.StatusOK, usageExportPayload{
		Version:    2,
		ExportedAt: time.Now().UTC(),
		Usage:      snapshot,
	})
}

// ImportUsageStatistics merges a previously exported usage snapshot into memory.
func (h *Handler) ImportUsageStatistics(c *gin.Context) {
	if h == nil || h.usageStats == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "usage statistics unavailable"})
		return
	}

	data, err := c.GetRawData()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read request body"})
		return
	}

	var payload usageImportPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
		return
	}
	if payload.Version != 0 && payload.Version != 1 && payload.Version != 2 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported version"})
		return
	}

	result, mergeErr := h.usageStats.MergeSnapshotWithError(payload.Usage)
	if mergeErr != nil {
		status, code := usageImportErrorResponse(mergeErr)
		c.JSON(status, gin.H{"error": code, "code": code})
		return
	}
	snapshot := h.usageStats.Snapshot()
	c.JSON(http.StatusOK, gin.H{
		"added":           result.Added,
		"skipped":         result.Skipped,
		"enriched":        result.Enriched,
		"total_requests":  snapshot.TotalRequests,
		"failed_requests": snapshot.FailureCount,
	})
}

func usageImportErrorResponse(err error) (int, string) {
	switch {
	case errors.Is(err, usage.ErrProjectionRebuildInProgress):
		return http.StatusConflict, "import_in_progress"
	case errors.Is(err, usage.ErrProjectionUnavailable),
		errors.Is(err, usage.ErrProjectionCASConflict),
		errors.Is(err, usage.ErrMutationJournalFull),
		errors.Is(err, usage.ErrMutationJournalBudget):
		return http.StatusServiceUnavailable, "projection_unavailable"
	default:
		return http.StatusServiceUnavailable, "projection_unavailable"
	}
}

// GetUsageQueue pops queued usage records from the usage queue.
func (h *Handler) GetUsageQueue(c *gin.Context) {
	if h == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "handler unavailable"})
		return
	}

	count, errCount := parseUsageQueueCount(c.Query("count"))
	if errCount != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": errCount.Error()})
		return
	}

	items := redisqueue.PopOldest(count)
	records := make([]usageQueueRecord, 0, len(items))
	for _, item := range items {
		records = append(records, usageQueueRecord(append([]byte(nil), item...)))
	}

	c.JSON(http.StatusOK, records)
}

func parseUsageQueueCount(value string) (int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 1, nil
	}
	count, errCount := strconv.Atoi(value)
	if errCount != nil || count <= 0 {
		return 0, errors.New("count must be a positive integer")
	}
	return count, nil
}

func parseAuthRequestFilter(c *gin.Context) (usage.AuthRequestFilter, error) {
	filter := usage.AuthRequestFilter{
		Limit:  50,
		Offset: 0,
		Model:  strings.TrimSpace(c.Query("model")),
	}

	if rawLimit := strings.TrimSpace(c.Query("limit")); rawLimit != "" {
		limit, errLimit := strconv.Atoi(rawLimit)
		if errLimit != nil || limit <= 0 {
			return filter, errors.New("limit must be a positive integer")
		}
		if limit > 500 {
			limit = 500
		}
		filter.Limit = limit
	}

	if rawOffset := strings.TrimSpace(c.Query("offset")); rawOffset != "" {
		offset, errOffset := strconv.Atoi(rawOffset)
		if errOffset != nil || offset < 0 {
			return filter, errors.New("offset must be a non-negative integer")
		}
		filter.Offset = offset
	}

	if rawFailed := strings.TrimSpace(c.Query("failed")); rawFailed != "" {
		failed, errFailed := strconv.ParseBool(rawFailed)
		if errFailed != nil {
			return filter, errors.New("failed must be true or false")
		}
		filter.Failed = &failed
	}

	if rawFrom := strings.TrimSpace(c.Query("from")); rawFrom != "" {
		from, errFrom := parseUsageRequestTime(rawFrom)
		if errFrom != nil {
			return filter, errors.New("from must be RFC3339 or unix seconds")
		}
		filter.From = &from
	}

	if rawTo := strings.TrimSpace(c.Query("to")); rawTo != "" {
		to, errTo := parseUsageRequestTime(rawTo)
		if errTo != nil {
			return filter, errors.New("to must be RFC3339 or unix seconds")
		}
		filter.To = &to
	}
	if rawToExclusive := strings.TrimSpace(c.Query("to_exclusive")); rawToExclusive != "" {
		toExclusive, errToExclusive := strconv.ParseBool(rawToExclusive)
		if errToExclusive != nil {
			return filter, errors.New("to_exclusive must be true or false")
		}
		if toExclusive && filter.To == nil {
			return filter, errors.New("to_exclusive requires to")
		}
		filter.ToExclusive = toExclusive
	}

	return filter, nil
}

func parseUsageRequestTime(value string) (time.Time, error) {
	if ts, err := time.Parse(time.RFC3339, value); err == nil {
		return ts, nil
	}
	unix, errUnix := strconv.ParseInt(value, 10, 64)
	if errUnix != nil {
		return time.Time{}, errUnix
	}
	return time.Unix(unix, 0), nil
}
