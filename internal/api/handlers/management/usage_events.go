package management

import (
	"errors"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/usage"
)

type usageEventsParsedQuery struct {
	from        time.Time
	to          time.Time
	window      string
	timezone    string
	anchorToken string
	anchor      usageWindowAnchorV1
	limit       int
	cursorToken string
	filters     map[string][]string
	identity    usageEventsQueryIdentity
	rangeDTO    usageRangeV1
}

type usageRangeV1 struct {
	Kind            string `json:"kind"`
	Window          string `json:"window,omitempty"`
	From            string `json:"from,omitempty"`
	To              string `json:"to"`
	Timezone        string `json:"timezone"`
	WindowAnchor    string `json:"window_anchor,omitempty"`
	AnchorMintedAt  string `json:"anchor_minted_at,omitempty"`
	AnchorNonce     string `json:"anchor_nonce,omitempty"`
	AnchorExpiresAt string `json:"anchor_expires_at,omitempty"`
	Complete        bool   `json:"complete"`
}

type usageEventBillableTokensV1 struct {
	Input         int64 `json:"input"`
	Output        int64 `json:"output"`
	Reasoning     int64 `json:"reasoning"`
	CacheRead     int64 `json:"cache_read"`
	CacheCreation int64 `json:"cache_creation"`
	UnsplitCache  int64 `json:"unsplit_cache"`
}

type usageEventTokensV1 struct {
	InputTokens         int64  `json:"input_tokens"`
	OutputTokens        int64  `json:"output_tokens"`
	ReasoningTokens     int64  `json:"reasoning_tokens"`
	CachedTokens        int64  `json:"cached_tokens"`
	CacheReadTokens     int64  `json:"cache_read_tokens"`
	CacheCreationTokens int64  `json:"cache_creation_tokens"`
	TotalTokens         int64  `json:"total_tokens"`
	ReportedTotalTokens int64  `json:"reported_total_tokens"`
	ComputedTotalTokens int64  `json:"computed_total_tokens"`
	TokenUsageSource    string `json:"token_usage_source"`
	CacheSplitStatus    string `json:"cache_split_status"`
	ReasoningCostMode   string `json:"reasoning_cost_mode"`
}

type usageEventDTOV1 struct {
	SchemaVersion         int                        `json:"schema_version"`
	RecordType            string                     `json:"record_type"`
	RequestID             string                     `json:"request_id"`
	ClientIP              string                     `json:"client_ip"`
	Timestamp             time.Time                  `json:"timestamp"`
	Endpoint              string                     `json:"endpoint"`
	Model                 string                     `json:"model"`
	Provider              string                     `json:"provider"`
	ProviderRawPresence   string                     `json:"provider_raw_presence"`
	PriceKey              string                     `json:"price_key"`
	BillablePolicyVersion string                     `json:"billable_policy_version"`
	BillableTokens        usageEventBillableTokensV1 `json:"billable_tokens"`
	ExecutorType          string                     `json:"executor_type"`
	AuthType              string                     `json:"auth_type"`
	ModelAlias            string                     `json:"model_alias"`
	SourceID              string                     `json:"source_id"`
	SourceKey             string                     `json:"source_key"`
	AuthIndex             string                     `json:"auth_index"`
	DetailRole            string                     `json:"detail_role"`
	DetailSequence        string                     `json:"detail_sequence"`
	Failed                bool                       `json:"failed"`
	LatencyMs             *int64                     `json:"latency_ms"`
	Tokens                usageEventTokensV1         `json:"tokens"`
	Thinking              any                        `json:"thinking"`
	ThinkingCoverage      string                     `json:"thinking_coverage"`
}

type usageEventsSnapshotV1 struct {
	DatasetEpoch    string `json:"dataset_epoch"`
	MaxSequence     uint64 `json:"max_sequence"`
	RewriteRevision uint64 `json:"rewrite_revision"`
}

type usageEventsFactsV1 struct {
	SchemaVersion int                   `json:"schema_version"`
	DatasetEpoch  string                `json:"dataset_epoch"`
	Revision      uint64                `json:"revision"`
	Range         usageRangeV1          `json:"range"`
	Items         []usageEventDTOV1     `json:"items"`
	NextCursor    string                `json:"next_cursor,omitempty"`
	HasMore       bool                  `json:"has_more"`
	Snapshot      usageEventsSnapshotV1 `json:"snapshot"`
}

type usageEventsResponseV1 struct {
	usageEventsFactsV1
	GeneratedAt time.Time `json:"generated_at"`
}

// GetUsageEvents returns one immutable cursor page backed by compact facts and
// a bounded stable-id detail lookup.
func (h *Handler) GetUsageEvents(c *gin.Context) {
	setUsageNoStore(c)
	if h == nil || h.usageStats == nil {
		writeUsageProjectionError(c, usage.ErrProjectionUnavailable)
		return
	}
	if err := h.ensureUsageTokenState(); err != nil {
		writeUsageProjectionError(c, err)
		return
	}
	datasetEpoch, _, rewriteRevision, err := h.usageStats.ProjectionMetadata()
	if err != nil {
		writeUsageProjectionError(c, err)
		return
	}
	query, err := h.parseUsageEventsQuery(c, datasetEpoch)
	if err != nil {
		if errors.Is(err, errUsageWindowAnchorExpired) {
			writeUsageQueryError(c, http.StatusConflict, "window_anchor_expired")
			return
		}
		writeUsageQueryError(c, http.StatusBadRequest, "invalid_query")
		return
	}
	queryHash, err := usageEventsQueryHashV1(query.identity)
	if err != nil {
		writeUsageQueryError(c, http.StatusBadRequest, "invalid_query")
		return
	}

	var cursor usageEventsCursorV1
	if query.cursorToken != "" {
		cursor, err = h.usageTokenCodec.decodeEventsCursor(query.cursorToken)
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
		query.from, query.to, query.limit, cursor.SnapshotMaxSequence, cursor.PhysicalScanPosition, query.filters,
	)
	if err != nil {
		writeUsageEventsProjectionError(c, err)
		return
	}
	if page.DatasetEpoch != datasetEpoch {
		writeUsageQueryError(c, http.StatusGone, "dataset_epoch_gone")
		return
	}
	if query.window != "" && page.DatasetEpoch != query.anchor.DatasetEpoch {
		writeUsageQueryError(c, http.StatusConflict, "window_anchor_expired")
		return
	}
	if query.cursorToken != "" && (page.RewriteRevision != cursor.RewriteRevision || postingID != cursor.PostingListID) {
		writeUsageQueryError(c, http.StatusConflict, "cursor_expired")
		return
	}
	stableEventIDs := make([]string, 0, len(page.Events))
	for _, event := range page.Events {
		stableEventIDs = append(stableEventIDs, event.StableEventID)
	}
	details, err := h.usageStats.LookupEventDetails(stableEventIDs)
	if err != nil {
		writeUsageEventsProjectionError(c, err)
		return
	}
	items := make([]usageEventDTOV1, 0, len(page.Events))
	for _, event := range page.Events {
		detail, ok := details[event.StableEventID]
		if !ok || usage.UsageSourceIDV1(detail) != event.SourceID {
			writeUsageProjectionError(c, usage.ErrProjectionUnavailable)
			return
		}
		items = append(items, buildUsageEventDTOV1(event, detail))
	}
	currentEpoch, _, currentRewrite, err := h.usageStats.ProjectionMetadata()
	if err != nil || currentEpoch != page.DatasetEpoch || currentRewrite != page.RewriteRevision {
		writeUsageQueryError(c, http.StatusConflict, "cursor_expired")
		return
	}

	nextCursor := ""
	if hasMore {
		nextCursor, err = h.usageTokenCodec.encodeEventsCursor(usageEventsCursorV1{
			DatasetEpoch:         page.DatasetEpoch,
			QueryHash:            queryHash,
			SnapshotMaxSequence:  snapshotMaxSequence,
			RewriteRevision:      page.RewriteRevision,
			PostingListID:        postingID,
			PhysicalScanPosition: nextPosition,
		})
		if err != nil {
			writeUsageProjectionError(c, err)
			return
		}
	}
	facts := usageEventsFactsV1{
		SchemaVersion: 1,
		DatasetEpoch:  usageDatasetEpochID(page.DatasetEpoch),
		Revision:      page.Revision,
		Range:         query.rangeDTO,
		Items:         items,
		NextCursor:    nextCursor,
		HasMore:       hasMore,
		Snapshot: usageEventsSnapshotV1{
			DatasetEpoch:    usageDatasetEpochID(page.DatasetEpoch),
			MaxSequence:     snapshotMaxSequence,
			RewriteRevision: page.RewriteRevision,
		},
	}
	etagFacts := facts
	etagFacts.Revision = 0
	etag, err := usageRepresentationETag("events-v1", etagFacts)
	if err != nil {
		writeUsageProjectionError(c, err)
		return
	}
	setUsageConditionalHeaders(c, etag, page.Revision)
	if usageIfNoneMatch(c.GetHeader("If-None-Match"), etag) {
		c.Status(http.StatusNotModified)
		return
	}
	c.JSON(http.StatusOK, usageEventsResponseV1{usageEventsFactsV1: facts, GeneratedAt: h.usageCurrentTime()})
}

func (h *Handler) parseUsageEventsQuery(c *gin.Context, datasetEpoch uint64) (usageEventsParsedQuery, error) {
	query := usageEventsParsedQuery{
		limit:       50,
		timezone:    strings.TrimSpace(c.Query("timezone")),
		anchorToken: strings.TrimSpace(c.Query("anchor")),
		cursorToken: strings.TrimSpace(c.Query("cursor")),
		filters:     make(map[string][]string),
	}
	if query.timezone == "" {
		query.timezone = "UTC"
	}
	if _, err := time.LoadLocation(query.timezone); err != nil {
		return query, errUsageTokenInvalid
	}
	if rawLimit := strings.TrimSpace(c.Query("limit")); rawLimit != "" {
		limit, err := strconv.Atoi(rawLimit)
		if err != nil || limit < 1 || limit > 500 {
			return query, errUsageTokenInvalid
		}
		query.limit = limit
	}

	query.window = strings.ToLower(strings.TrimSpace(c.Query("window")))
	rawFrom := strings.TrimSpace(c.Query("from"))
	rawTo := strings.TrimSpace(c.Query("to"))
	if query.window != "" && (rawFrom != "" || rawTo != "") {
		return query, errUsageTokenInvalid
	}
	if query.window == "" && rawFrom == "" && rawTo == "" {
		query.window = "24h"
	}
	if query.window != "" {
		if query.cursorToken != "" && query.anchorToken == "" {
			return query, errUsageTokenInvalid
		}
		if query.anchorToken == "" {
			token, anchor, err := h.usageWindowAnchors.mint(datasetEpoch, query.window, query.timezone)
			if err != nil {
				return query, err
			}
			query.anchorToken = token
			query.anchor = anchor
		} else {
			anchor, err := h.usageWindowAnchors.resolve(query.anchorToken, datasetEpoch, h.usageCurrentTime())
			if err != nil {
				return query, err
			}
			if anchor.Window != query.window || anchor.Timezone != query.timezone {
				return query, errUsageTokenInvalid
			}
			query.anchor = anchor
		}
		query.from = query.anchor.From
		query.to = query.anchor.To
		query.rangeDTO = usageRangeV1{
			Kind:            "rolling",
			Window:          query.window,
			From:            canonicalUsageQueryTime(query.from),
			To:              canonicalUsageQueryTime(query.to),
			Timezone:        query.timezone,
			WindowAnchor:    query.anchorToken,
			AnchorMintedAt:  canonicalUsageQueryTime(query.anchor.MintedAt),
			AnchorNonce:     query.anchor.AnchorNonce,
			AnchorExpiresAt: canonicalUsageQueryTime(query.anchor.ExpiresAt),
			Complete:        true,
		}
	} else {
		if query.anchorToken != "" || rawFrom == "" || rawTo == "" {
			return query, errUsageTokenInvalid
		}
		from, err := parseUsageRequestTime(rawFrom)
		if err != nil {
			return query, err
		}
		to, err := parseUsageRequestTime(rawTo)
		if err != nil || !from.Before(to) {
			return query, errUsageTokenInvalid
		}
		query.from = from.UTC()
		query.to = to.UTC()
		query.rangeDTO = usageRangeV1{
			Kind: "absolute", From: canonicalUsageQueryTime(query.from), To: canonicalUsageQueryTime(query.to),
			Timezone: query.timezone, Complete: true,
		}
	}

	for _, filter := range []struct {
		queryName string
		filterKey string
	}{
		{queryName: "api", filterKey: "api"},
		{queryName: "model", filterKey: "model"},
		{queryName: "provider", filterKey: "provider"},
		{queryName: "source", filterKey: "source"},
		{queryName: "auth_index", filterKey: "auth_index"},
	} {
		values := normalizedUsageQueryValues(c.QueryArray(filter.queryName))
		if len(values) > 0 {
			query.filters[filter.filterKey] = values
		}
	}
	failedValues := normalizedUsageQueryValues(c.QueryArray("failed"))
	if len(failedValues) > 1 {
		return query, errUsageTokenInvalid
	}
	failed := ""
	if len(failedValues) == 1 {
		parsed, err := strconv.ParseBool(failedValues[0])
		if err != nil {
			return query, errUsageTokenInvalid
		}
		failed = strconv.FormatBool(parsed)
		query.filters["failed"] = []string{failed}
	}
	query.identity = usageEventsQueryIdentity{
		From: query.from, To: query.to, Window: query.window, WindowAnchorID: query.anchor.AnchorNonce,
		Timezone: query.timezone, Limit: query.limit,
		APIs: query.filters["api"], Models: query.filters["model"], Providers: query.filters["provider"],
		Sources: query.filters["source"], Auths: query.filters["auth_index"], Failed: failed,
	}
	return query, nil
}

func buildUsageEventDTOV1(event usage.EventRef, detail usage.RequestDetail) usageEventDTOV1 {
	billable := usage.GetBillableTokenComponents(detail)
	return usageEventDTOV1{
		SchemaVersion:         1,
		RecordType:            "event",
		RequestID:             detail.RequestID,
		ClientIP:              safeUsageEventClientIP(detail.ClientIP),
		Timestamp:             detail.Timestamp.UTC(),
		Endpoint:              safeUsageEventEndpoint(detail.Endpoint),
		Model:                 detail.Model,
		Provider:              detail.Provider,
		ProviderRawPresence:   usage.ProviderRawPresenceV1(detail.Provider),
		PriceKey:              usage.BuildPriceKey(detail.Provider, detail.Model),
		BillablePolicyVersion: usage.BillablePolicyVersionV1,
		BillableTokens: usageEventBillableTokensV1{
			Input: billable.InputTokens, Output: billable.OutputTokens, Reasoning: billable.ReasoningTokens,
			CacheRead: billable.CacheReadTokens, CacheCreation: billable.CacheCreationTokens,
			UnsplitCache: billable.UnclassifiedCacheTokens,
		},
		ExecutorType:   detail.ExecutorType,
		AuthType:       detail.AuthType,
		ModelAlias:     detail.ModelAlias,
		SourceID:       event.SourceID,
		SourceKey:      usage.UsageSourceKeyV1(detail),
		AuthIndex:      detail.AuthIndex,
		DetailRole:     detail.DetailRole,
		DetailSequence: detail.DetailSequence,
		Failed:         detail.Failed,
		LatencyMs:      usageEventLatencyMsV1(detail),
		Tokens: usageEventTokensV1{
			InputTokens: detail.Tokens.InputTokens, OutputTokens: detail.Tokens.OutputTokens,
			ReasoningTokens: detail.Tokens.ReasoningTokens, CachedTokens: detail.Tokens.CachedTokens,
			CacheReadTokens: detail.Tokens.CacheReadTokens, CacheCreationTokens: detail.Tokens.CacheCreationTokens,
			TotalTokens: detail.Tokens.TotalTokens, ReportedTotalTokens: detail.Tokens.ReportedTotalTokens,
			ComputedTotalTokens: detail.Tokens.ComputedTotalTokens, TokenUsageSource: detail.Tokens.TokenUsageSource,
			CacheSplitStatus: detail.Tokens.CacheSplitStatus, ReasoningCostMode: detail.Tokens.ReasoningCostMode,
		},
		Thinking:         nil,
		ThinkingCoverage: "unavailable_legacy",
	}
}

func usageEventLatencyMsV1(detail usage.RequestDetail) *int64 {
	if detail.LatencyMs <= 0 {
		return nil
	}
	latencyMs := detail.LatencyMs
	return &latencyMs
}

func safeUsageEventClientIP(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	address, err := netip.ParseAddr(value)
	if err != nil {
		return ""
	}
	return address.String()
}

func safeUsageEventEndpoint(endpoint string) string {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return ""
	}
	parts := strings.Fields(endpoint)
	if len(parts) >= 2 {
		method := strings.ToUpper(parts[0])
		path := parts[1]
		if parsed, err := url.Parse(path); err == nil && parsed.Path != "" {
			return method + " " + parsed.EscapedPath()
		}
		return method
	}
	if parsed, err := url.Parse(endpoint); err == nil && parsed.Path != "" {
		return parsed.EscapedPath()
	}
	return ""
}

func writeUsageEventsProjectionError(c *gin.Context, err error) {
	if errors.Is(err, usage.ErrProjectionQueryInvalid) {
		writeUsageQueryError(c, http.StatusBadRequest, "invalid_query")
		return
	}
	writeUsageProjectionError(c, err)
}

func writeUsageQueryError(c *gin.Context, status int, code string) {
	c.JSON(status, gin.H{"error": code, "code": code})
}
