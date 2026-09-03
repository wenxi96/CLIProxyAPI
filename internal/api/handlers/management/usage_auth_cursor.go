package management

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/usage"
)

type usageAuthCursorSnapshotV1 struct {
	DatasetEpoch    string `json:"dataset_epoch"`
	MaxSequence     uint64 `json:"max_sequence"`
	RewriteRevision uint64 `json:"rewrite_revision"`
}

// usageAuthCursorItemV1 is the versioned public shape for the new auth cursor
// path. Keep it separate from RequestDetail so persistence fields cannot leak
// into this additive API when the canonical detail grows.
type usageAuthCursorItemV1 struct {
	SchemaVersion       int                     `json:"schema_version"`
	RecordType          string                  `json:"record_type"`
	RequestID           string                  `json:"request_id"`
	ClientIP            string                  `json:"client_ip"`
	Timestamp           time.Time               `json:"timestamp"`
	Endpoint            string                  `json:"endpoint"`
	Model               string                  `json:"model"`
	Provider            string                  `json:"provider"`
	ProviderRawPresence string                  `json:"provider_raw_presence"`
	ExecutorType        string                  `json:"executor_type"`
	AuthType            string                  `json:"auth_type"`
	ModelAlias          string                  `json:"model_alias"`
	Source              string                  `json:"source"`
	SourceID            string                  `json:"source_id"`
	SourceKey           string                  `json:"source_key"`
	AuthIndex           string                  `json:"auth_index"`
	DetailRole          string                  `json:"detail_role"`
	DetailSequence      string                  `json:"detail_sequence,omitempty"`
	Failed              bool                    `json:"failed"`
	Generate            *bool                   `json:"generate,omitempty"`
	LatencyMs           int64                   `json:"latency_ms"`
	EstimatedCostUSD    *float64                `json:"estimated_cost_usd"`
	Tokens              usage.RequestTokenStats `json:"tokens"`
}

type usageAuthCursorFactsV1 struct {
	AuthIndex      string                    `json:"auth_index"`
	PaginationMode string                    `json:"pagination_mode"`
	Items          []usageAuthCursorItemV1   `json:"items"`
	NextCursor     string                    `json:"next_cursor,omitempty"`
	HasMore        bool                      `json:"has_more"`
	Snapshot       usageAuthCursorSnapshotV1 `json:"snapshot"`
}

type usageAuthCursorResponseV1 struct {
	usageAuthCursorFactsV1
	GeneratedAt time.Time `json:"generated_at"`
}

func (h *Handler) getUsageAuthRequestsCursor(c *gin.Context, authIndex, cursorToken string) {
	setUsageNoStore(c)
	if err := h.ensureUsageTokenState(); err != nil {
		writeUsageProjectionError(c, err)
		return
	}
	datasetEpoch, _, rewriteRevision, err := h.usageStats.ProjectionMetadata()
	if err != nil {
		writeUsageProjectionError(c, err)
		return
	}
	filter, identity, err := parseUsageAuthCursorQuery(c, authIndex)
	if err != nil {
		writeUsageQueryError(c, http.StatusBadRequest, "invalid_query")
		return
	}
	queryHash, err := usageEventsQueryHashV1(identity)
	if err != nil {
		writeUsageQueryError(c, http.StatusBadRequest, "invalid_query")
		return
	}

	var cursor usageEventsCursorV1
	if cursorToken != "" {
		cursor, err = h.usageTokenCodec.decodeAuthCursor(cursorToken)
		if err != nil {
			writeUsageQueryError(c, http.StatusBadRequest, "invalid_cursor")
			return
		}
		if cursor.DatasetEpoch != datasetEpoch {
			writeUsageQueryError(c, http.StatusGone, "dataset_epoch_gone")
			return
		}
		if cursor.QueryHash != queryHash || cursor.RewriteRevision != rewriteRevision {
			writeUsageQueryError(c, http.StatusConflict, "cursor_expired")
			return
		}
	}

	page, hasMore, nextPosition, snapshotMaxSequence, postingID, err := h.usageStats.QueryProjectionEvents(
		filter.from, filter.to, filter.limit, cursor.SnapshotMaxSequence, cursor.PhysicalScanPosition, filter.filters,
	)
	if err != nil {
		writeUsageEventsProjectionError(c, err)
		return
	}
	if page.DatasetEpoch != datasetEpoch {
		writeUsageQueryError(c, http.StatusGone, "dataset_epoch_gone")
		return
	}
	if cursorToken != "" && (page.RewriteRevision != cursor.RewriteRevision || postingID != cursor.PostingListID) {
		writeUsageQueryError(c, http.StatusConflict, "cursor_expired")
		return
	}

	stableEventIDs := make([]string, 0, len(page.Events))
	for _, event := range page.Events {
		stableEventIDs = append(stableEventIDs, event.StableEventID)
	}
	details, err := h.usageStats.LookupEventDetails(stableEventIDs)
	if err != nil {
		writeUsageProjectionError(c, err)
		return
	}
	items := make([]usageAuthCursorItemV1, 0, len(page.Events))
	for _, event := range page.Events {
		detail, ok := details[event.StableEventID]
		if !ok || strings.TrimSpace(event.AuthIndex) != authIndex ||
			strings.TrimSpace(detail.AuthIndex) != authIndex || usage.UsageSourceIDV1(detail) != event.SourceID {
			writeUsageProjectionError(c, usage.ErrProjectionUnavailable)
			return
		}
		items = append(items, buildUsageAuthCursorItemV1(event, detail))
	}
	currentEpoch, _, currentRewrite, err := h.usageStats.ProjectionMetadata()
	if err != nil || currentEpoch != page.DatasetEpoch || currentRewrite != page.RewriteRevision {
		writeUsageQueryError(c, http.StatusConflict, "cursor_expired")
		return
	}

	nextCursor := ""
	if hasMore {
		nextCursor, err = h.usageTokenCodec.encodeAuthCursor(usageEventsCursorV1{
			DatasetEpoch: page.DatasetEpoch, QueryHash: queryHash, SnapshotMaxSequence: snapshotMaxSequence,
			RewriteRevision: page.RewriteRevision, PostingListID: postingID, PhysicalScanPosition: nextPosition,
		})
		if err != nil {
			writeUsageProjectionError(c, err)
			return
		}
	}
	facts := usageAuthCursorFactsV1{
		AuthIndex: authIndex, PaginationMode: "cursor_v1", Items: items, NextCursor: nextCursor, HasMore: hasMore,
		Snapshot: usageAuthCursorSnapshotV1{
			DatasetEpoch: usageDatasetEpochID(page.DatasetEpoch), MaxSequence: snapshotMaxSequence,
			RewriteRevision: page.RewriteRevision,
		},
	}
	etag, err := usageRepresentationETag("auth-cursor-v1", facts)
	if err != nil {
		writeUsageProjectionError(c, err)
		return
	}
	setUsageConditionalHeaders(c, etag, page.Revision)
	if usageIfNoneMatch(c.GetHeader("If-None-Match"), etag) {
		c.Status(http.StatusNotModified)
		return
	}
	c.JSON(http.StatusOK, usageAuthCursorResponseV1{usageAuthCursorFactsV1: facts, GeneratedAt: h.usageCurrentTime()})
}

func buildUsageAuthCursorItemV1(event usage.EventRef, detail usage.RequestDetail) usageAuthCursorItemV1 {
	return usageAuthCursorItemV1{
		SchemaVersion:       1,
		RecordType:          "auth_request",
		RequestID:           detail.RequestID,
		ClientIP:            safeUsageEventClientIP(detail.ClientIP),
		Timestamp:           detail.Timestamp.UTC(),
		Endpoint:            safeUsageEventEndpoint(detail.Endpoint),
		Model:               detail.Model,
		Provider:            detail.Provider,
		ProviderRawPresence: usage.ProviderRawPresenceV1(detail.Provider),
		ExecutorType:        detail.ExecutorType,
		AuthType:            detail.AuthType,
		ModelAlias:          detail.ModelAlias,
		Source:              usage.UsageSourceKeyV1(detail),
		SourceID:            event.SourceID,
		SourceKey:           usage.UsageSourceKeyV1(detail),
		AuthIndex:           detail.AuthIndex,
		DetailRole:          detail.DetailRole,
		DetailSequence:      detail.DetailSequence,
		Failed:              detail.Failed,
		Generate:            detail.Generate,
		LatencyMs:           detail.LatencyMs,
		EstimatedCostUSD:    detail.EstimatedCostUSD,
		Tokens:              detail.Tokens,
	}
}

type usageAuthCursorQuery struct {
	from    time.Time
	to      time.Time
	limit   int
	filters map[string][]string
}

func parseUsageAuthCursorQuery(c *gin.Context, authIndex string) (usageAuthCursorQuery, usageEventsQueryIdentity, error) {
	query := usageAuthCursorQuery{limit: 50, filters: map[string][]string{"auth_index": {authIndex}}}
	if rawLimit := strings.TrimSpace(c.Query("limit")); rawLimit != "" {
		limit, err := strconv.Atoi(rawLimit)
		if err != nil || limit < 1 || limit > 500 {
			return query, usageEventsQueryIdentity{}, errUsageTokenInvalid
		}
		query.limit = limit
	}
	if rawFrom := strings.TrimSpace(c.Query("from")); rawFrom != "" {
		from, err := parseUsageRequestTime(rawFrom)
		if err != nil {
			return query, usageEventsQueryIdentity{}, err
		}
		query.from = from.UTC()
	}
	if rawTo := strings.TrimSpace(c.Query("to")); rawTo != "" {
		to, err := parseUsageRequestTime(rawTo)
		if err != nil {
			return query, usageEventsQueryIdentity{}, err
		}
		query.to = to.UTC()
	}
	if !query.from.IsZero() && !query.to.IsZero() && !query.from.Before(query.to) {
		return query, usageEventsQueryIdentity{}, errUsageTokenInvalid
	}
	model := strings.TrimSpace(c.Query("model"))
	if model != "" {
		query.filters["model"] = []string{model}
	}
	failed := ""
	if rawFailed := strings.TrimSpace(c.Query("failed")); rawFailed != "" {
		parsed, err := strconv.ParseBool(rawFailed)
		if err != nil {
			return query, usageEventsQueryIdentity{}, err
		}
		failed = strconv.FormatBool(parsed)
		query.filters["failed"] = []string{failed}
	}
	identity := usageEventsQueryIdentity{
		From: query.from, To: query.to, Timezone: "UTC", Limit: query.limit,
		Models: query.filters["model"], Auths: []string{authIndex}, Failed: failed,
	}
	return query, identity, nil
}

func writeUsageAuthCursorError(c *gin.Context, err error) {
	if errors.Is(err, usage.ErrProjectionQueryInvalid) {
		writeUsageQueryError(c, http.StatusBadRequest, "invalid_query")
		return
	}
	writeUsageProjectionError(c, err)
}
