package management

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/usage"
)

const (
	usageEventsExportSchemaV2  = "usage-events-v2"
	usageEventsExportProfileV1 = "usage-events-v2-client-v1"
	usageExportSnapshotTTL     = 5 * time.Minute
	usageExportBlobLimitBytes  = 64 << 20
)

var (
	errUsageExportSnapshotExpired  = errors.New("usage export snapshot expired")
	errUsageExportSnapshotMismatch = errors.New("usage export estimate snapshot mismatch")
)

type usageExportSnapshot struct {
	ID                  string
	DatasetEpoch        uint64
	SnapshotMaxSequence uint64
	Revision            uint64
	RewriteRevision     uint64
	Query               usageEventsParsedQuery
	CanonicalQuery      usageEventsCanonicalQueryV1
	QueryHash           string
	FactsHash           string
	ExpiresAt           time.Time
	Format              string
	SchemaVersion       string
	EventCount          uint64
	MatchedSourceIDs    map[string]uint64
	MatchedModels       map[string]uint64
	ServerBytes         uint64
	ServerBytesComplete bool
	ServerJSONBytes     uint64
	ServerCSVBytes      uint64
	ServerJSONComplete  bool
	ServerCSVComplete   bool
	Lease               *usage.UsageExportLease
}

type usageExportSnapshotManager struct {
	mu    sync.Mutex
	now   func() time.Time
	ttl   time.Duration
	items map[string]usageExportSnapshot
}

func newUsageExportSnapshotManager(now func() time.Time) *usageExportSnapshotManager {
	if now == nil {
		now = time.Now
	}
	return &usageExportSnapshotManager{
		now:   now,
		ttl:   usageExportSnapshotTTL,
		items: make(map[string]usageExportSnapshot),
	}
}

func (manager *usageExportSnapshotManager) create(snapshot usageExportSnapshot) (usageExportSnapshot, error) {
	if manager == nil {
		return usageExportSnapshot{}, errUsageExportSnapshotMismatch
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	now := manager.now().UTC()
	manager.cleanupExpiredLocked(now)
	var tokenBytes [18]byte
	if _, err := rand.Read(tokenBytes[:]); err != nil {
		return usageExportSnapshot{}, fmt.Errorf("generate usage export snapshot id: %w", err)
	}
	if manager.ttl <= 0 {
		manager.ttl = usageExportSnapshotTTL
	}
	if snapshot.ExpiresAt.IsZero() {
		snapshot.ExpiresAt = now.Add(manager.ttl)
	} else if !now.Before(snapshot.ExpiresAt) {
		if snapshot.Lease != nil {
			snapshot.Lease.Release()
		}
		return usageExportSnapshot{}, errUsageExportSnapshotExpired
	}
	snapshot.ID = base64.RawURLEncoding.EncodeToString(tokenBytes[:])
	if snapshot.MatchedSourceIDs == nil {
		snapshot.MatchedSourceIDs = map[string]uint64{}
	}
	if snapshot.MatchedModels == nil {
		snapshot.MatchedModels = map[string]uint64{}
	}
	stored := cloneUsageExportSnapshot(snapshot)
	if snapshot.Lease != nil {
		stored.Lease = snapshot.Lease.Acquire()
		if stored.Lease == nil {
			return usageExportSnapshot{}, errUsageExportSnapshotExpired
		}
	}
	manager.items[snapshot.ID] = stored
	return snapshot, nil
}

func (manager *usageExportSnapshotManager) currentTime() time.Time {
	if manager == nil || manager.now == nil {
		return time.Now().UTC()
	}
	return manager.now().UTC()
}

func (manager *usageExportSnapshotManager) leaseTTL() time.Duration {
	if manager == nil || manager.ttl <= 0 {
		return usageExportSnapshotTTL
	}
	return manager.ttl
}

func (manager *usageExportSnapshotManager) cleanupExpiredLocked(now time.Time) int {
	return manager.cleanupExpiredExceptLocked(now, "")
}

func (manager *usageExportSnapshotManager) cleanupExpiredExceptLocked(now time.Time, keepID string) int {
	if manager == nil {
		return 0
	}
	removed := 0
	for id, item := range manager.items {
		if id == keepID {
			continue
		}
		if now.Before(item.ExpiresAt) {
			continue
		}
		if item.Lease != nil {
			item.Lease.Release()
		}
		delete(manager.items, id)
		removed++
	}
	return removed
}

func (manager *usageExportSnapshotManager) cleanupExpired() int {
	if manager == nil {
		return 0
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	return manager.cleanupExpiredLocked(manager.now().UTC())
}

func (manager *usageExportSnapshotManager) get(id string) (usageExportSnapshot, error) {
	if manager == nil || strings.TrimSpace(id) == "" {
		return usageExportSnapshot{}, errUsageExportSnapshotMismatch
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	manager.cleanupExpiredExceptLocked(manager.now().UTC(), strings.TrimSpace(id))
	snapshot, ok := manager.items[strings.TrimSpace(id)]
	if !ok {
		return usageExportSnapshot{}, errUsageExportSnapshotMismatch
	}
	if !manager.now().UTC().Before(snapshot.ExpiresAt) {
		if snapshot.Lease != nil {
			snapshot.Lease.Release()
		}
		delete(manager.items, snapshot.ID)
		return usageExportSnapshot{}, errUsageExportSnapshotExpired
	}
	result := cloneUsageExportSnapshot(snapshot)
	if snapshot.Lease != nil {
		result.Lease = snapshot.Lease.Acquire()
		if result.Lease == nil {
			return usageExportSnapshot{}, errUsageExportSnapshotMismatch
		}
	}
	return result, nil
}

func cloneUsageExportSnapshot(snapshot usageExportSnapshot) usageExportSnapshot {
	snapshot.MatchedSourceIDs = cloneUint64Map(snapshot.MatchedSourceIDs)
	snapshot.MatchedModels = cloneUint64Map(snapshot.MatchedModels)
	snapshot.Query = cloneUsageExportParsedQuery(snapshot.Query)
	snapshot.CanonicalQuery = cloneUsageExportCanonicalQuery(snapshot.CanonicalQuery)
	// Lease references are acquired by manager create/get; a shallow copy here
	// is intentional and keeps the opaque token independent from the maps.
	return snapshot
}

func cloneUsageExportParsedQuery(query usageEventsParsedQuery) usageEventsParsedQuery {
	query.filters = cloneUint64StringSliceMap(query.filters)
	query.identity = cloneUsageExportQueryIdentity(query.identity)
	return query
}

func cloneUsageExportQueryIdentity(identity usageEventsQueryIdentity) usageEventsQueryIdentity {
	identity.APIs = append([]string(nil), identity.APIs...)
	identity.Models = append([]string(nil), identity.Models...)
	identity.Providers = append([]string(nil), identity.Providers...)
	identity.Sources = append([]string(nil), identity.Sources...)
	identity.Auths = append([]string(nil), identity.Auths...)
	return identity
}

func cloneUsageExportCanonicalQuery(query usageEventsCanonicalQueryV1) usageEventsCanonicalQueryV1 {
	query.APIs = append([]string(nil), query.APIs...)
	query.Models = append([]string(nil), query.Models...)
	query.Providers = append([]string(nil), query.Providers...)
	query.Sources = append([]string(nil), query.Sources...)
	query.Auths = append([]string(nil), query.Auths...)
	return query
}

func cloneUint64StringSliceMap(values map[string][]string) map[string][]string {
	if values == nil {
		return nil
	}
	result := make(map[string][]string, len(values))
	for key, items := range values {
		result[key] = append([]string(nil), items...)
	}
	return result
}

type usageExportStatistics interface {
	PinUsageExportWithClock(context.Context, usage.UsageExportQuery, func() time.Time, time.Duration) (*usage.UsageExportLease, usage.UsageExportEstimate, error)
}

func cloneUint64Map(values map[string]uint64) map[string]uint64 {
	if values == nil {
		return map[string]uint64{}
	}
	result := make(map[string]uint64, len(values))
	for key, value := range values {
		result[key] = value
	}
	return result
}

type usageExportEstimateDTOv1 struct {
	SchemaVersion                        int                         `json:"schema_version"`
	ExportSnapshotID                     string                      `json:"export_snapshot_id"`
	DatasetEpoch                         string                      `json:"dataset_epoch"`
	SnapshotMaxSequence                  uint64                      `json:"snapshot_max_sequence"`
	RewriteRevision                      uint64                      `json:"rewrite_revision"`
	NormalizedQuery                      usageEventsCanonicalQueryV1 `json:"normalized_query"`
	SnapshotFactsHash                    string                      `json:"snapshot_facts_hash"`
	ExportSnapshotExpiresAt              time.Time                   `json:"export_snapshot_expires_at"`
	Format                               string                      `json:"format"`
	ExportSchemaVersion                  string                      `json:"export_schema_version"`
	ExportDerivationProfile              string                      `json:"export_derivation_profile"`
	EstimatedEventCount                  uint64                      `json:"estimated_event_count"`
	EventCountUpperBound                 uint64                      `json:"event_count_upper_bound"`
	EventCountExact                      bool                        `json:"event_count_exact"`
	MatchedSourceIDCounts                map[string]uint64           `json:"matched_source_id_counts"`
	MatchedModelCounts                   map[string]uint64           `json:"matched_model_counts"`
	ServerBytesUpperBound                uint64                      `json:"server_bytes_upper_bound"`
	ServerBytesUpperBoundComplete        bool                        `json:"server_bytes_upper_bound_complete"`
	ClientDerivedBytesUpperBound         uint64                      `json:"client_derived_bytes_upper_bound"`
	ClientDerivedBytesUpperBoundComplete bool                        `json:"client_derived_bytes_upper_bound_complete"`
	EstimatedBytesUpperBound             uint64                      `json:"estimated_bytes_upper_bound"`
	BytesUpperBoundComplete              bool                        `json:"bytes_upper_bound_complete"`
	BlobLimitBytes                       uint64                      `json:"blob_limit_bytes"`
	CatalogFingerprint                   string                      `json:"catalog_fingerprint"`
	PriceSnapshotFingerprint             string                      `json:"price_snapshot_fingerprint"`
	ProfileFingerprint                   string                      `json:"profile_fingerprint"`
	Error                                string                      `json:"error,omitempty"`
}

type usageExportControlDTOv1 struct {
	SchemaVersion                        int    `json:"schema_version"`
	RecordType                           string `json:"record_type"`
	ExportSnapshotID                     string `json:"export_snapshot_id"`
	ExportSchemaVersion                  string `json:"export_schema_version"`
	ExportDerivationProfile              string `json:"export_derivation_profile"`
	Complete                             bool   `json:"complete"`
	Code                                 string `json:"code,omitempty"`
	EventCount                           uint64 `json:"event_count"`
	SnapshotFactsHash                    string `json:"snapshot_facts_hash"`
	ServerBytesUpperBound                uint64 `json:"server_bytes_upper_bound"`
	ServerBytesUpperBoundComplete        bool   `json:"server_bytes_upper_bound_complete"`
	ClientDerivedBytesUpperBound         uint64 `json:"client_derived_bytes_upper_bound"`
	ClientDerivedBytesUpperBoundComplete bool   `json:"client_derived_bytes_upper_bound_complete"`
	EstimatedBytesUpperBound             uint64 `json:"estimated_bytes_upper_bound"`
	BytesUpperBoundComplete              bool   `json:"bytes_upper_bound_complete"`
	BlobLimitBytes                       uint64 `json:"blob_limit_bytes"`
	CatalogFingerprint                   string `json:"catalog_fingerprint"`
	PriceSnapshotFingerprint             string `json:"price_snapshot_fingerprint"`
	ProfileFingerprint                   string `json:"profile_fingerprint"`
}

const usageExportCatalogFingerprintV1 = "projection-catalog-v1"
const usageExportPriceFingerprintV1 = "client-owned"

func usageExportServerBound(estimate usage.UsageExportEstimate, format string) (uint64, bool) {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "json":
		return estimate.ServerJSONBytes, estimate.ServerJSONComplete
	case "csv":
		return estimate.ServerCSVBytes, estimate.ServerCSVComplete
	default:
		return 0, false
	}
}

func usageExportServerBoundFromSnapshot(snapshot usageExportSnapshot) (uint64, bool) {
	switch strings.ToLower(strings.TrimSpace(snapshot.Format)) {
	case "json":
		return snapshot.ServerJSONBytes, snapshot.ServerJSONComplete
	case "csv":
		return snapshot.ServerCSVBytes, snapshot.ServerCSVComplete
	default:
		return 0, false
	}
}

func (h *Handler) exportSnapshotManager() *usageExportSnapshotManager {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.usageExportSnapshots == nil {
		h.usageExportSnapshots = newUsageExportSnapshotManager(h.usageCurrentTime)
	}
	return h.usageExportSnapshots
}

func (h *Handler) cleanupUsageExportSnapshots() {
	if manager := h.exportSnapshotManager(); manager != nil {
		manager.cleanupExpired()
	}
}

// EstimateUsageEventsExport creates a pinned immutable export view. The
// estimate is scoped to server-known event facts; client-derived price and
// display-label columns are intentionally marked incomplete here.
func (h *Handler) EstimateUsageEventsExport(c *gin.Context) {
	setUsageNoStore(c)
	h.cleanupUsageExportSnapshots()
	snapshot, err := h.buildUsageExportSnapshot(c, true)
	if err != nil {
		h.writeUsageExportRequestError(c, err)
		return
	}
	if snapshot.Lease != nil {
		defer snapshot.Lease.Release()
	}
	serverBytes, serverComplete := usageExportServerBoundFromSnapshot(snapshot)
	payload := usageExportEstimateDTOv1{
		SchemaVersion:                        1,
		ExportSnapshotID:                     snapshot.ID,
		DatasetEpoch:                         usageDatasetEpochID(snapshot.DatasetEpoch),
		SnapshotMaxSequence:                  snapshot.SnapshotMaxSequence,
		RewriteRevision:                      snapshot.RewriteRevision,
		NormalizedQuery:                      snapshot.CanonicalQuery,
		SnapshotFactsHash:                    snapshot.FactsHash,
		ExportSnapshotExpiresAt:              snapshot.ExpiresAt,
		Format:                               snapshot.Format,
		ExportSchemaVersion:                  snapshot.SchemaVersion,
		ExportDerivationProfile:              usageEventsExportProfileV1,
		EstimatedEventCount:                  snapshot.EventCount,
		EventCountUpperBound:                 snapshot.EventCount,
		EventCountExact:                      true,
		MatchedSourceIDCounts:                snapshot.MatchedSourceIDs,
		MatchedModelCounts:                   snapshot.MatchedModels,
		ServerBytesUpperBound:                serverBytes,
		ServerBytesUpperBoundComplete:        serverComplete,
		ClientDerivedBytesUpperBound:         0,
		ClientDerivedBytesUpperBoundComplete: false,
		EstimatedBytesUpperBound:             serverBytes,
		BytesUpperBoundComplete:              false,
		BlobLimitBytes:                       usageExportBlobLimitBytes,
		CatalogFingerprint:                   usageExportCatalogFingerprintV1,
		PriceSnapshotFingerprint:             usageExportPriceFingerprintV1,
		ProfileFingerprint:                   usageEventsExportProfileV1,
	}
	c.JSON(http.StatusOK, payload)
}

// ExportUsageEvents streams allowlisted event rows as NDJSON and terminates
// with an explicit control record. It never mutates the legacy export path.
func (h *Handler) ExportUsageEvents(c *gin.Context) {
	setUsageNoStore(c)
	if c == nil || c.Request == nil {
		return
	}
	if err := c.Request.Context().Err(); err != nil {
		return
	}
	if h == nil || h.usageStats == nil {
		h.writeUsageExportRequestError(c, usage.ErrProjectionUnavailable)
		return
	}
	if err := h.ensureUsageTokenState(); err != nil {
		h.writeUsageExportRequestError(c, err)
		return
	}
	datasetEpoch, revision, rewriteRevision, err := h.usageStats.ProjectionMetadata()
	if err != nil {
		h.writeUsageExportRequestError(c, err)
		return
	}
	query, canonical, queryHash, format, err := h.parseUsageExportQuery(c, datasetEpoch, false)
	if err != nil {
		h.writeUsageExportRequestError(c, err)
		return
	}
	manager := h.exportSnapshotManager()
	id := strings.TrimSpace(c.Query("export_snapshot_id"))
	var snapshot usageExportSnapshot
	if id != "" {
		snapshot, err = manager.get(id)
		if err != nil {
			h.writeUsageExportRequestError(c, err)
			return
		}
		if snapshot.DatasetEpoch != datasetEpoch ||
			snapshot.Format != format || snapshot.SchemaVersion != usageEventsExportSchemaV2 ||
			snapshot.QueryHash != queryHash {
			if snapshot.Lease != nil {
				snapshot.Lease.Release()
			}
			h.writeUsageExportRequestError(c, errUsageExportSnapshotMismatch)
			return
		}
	} else {
		snapshot, err = h.buildUsageExportSnapshotFromQueryContext(c.Request.Context(), query, canonical, queryHash, format, datasetEpoch, revision, rewriteRevision)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return
			}
			h.writeUsageExportRequestError(c, err)
			return
		}
	}
	if snapshot.Lease != nil {
		defer snapshot.Lease.Release()
	}
	if err := c.Request.Context().Err(); err != nil {
		return
	}
	if !h.exportSnapshotFresh(snapshot) {
		h.writeUsageExportRequestError(c, errUsageExportSnapshotExpired)
		return
	}
	if snapshot.Lease == nil {
		h.writeUsageExportRequestError(c, usage.ErrProjectionUnavailable)
		return
	}
	c.Header("Content-Type", "application/x-ndjson")
	c.Header("X-Usage-Export-Snapshot", snapshot.ID)
	c.Status(http.StatusOK)
	position := uint64(0)
	for {
		page, pageErr := snapshot.Lease.NextPage(c.Request.Context(), position, 500)
		if pageErr != nil {
			if code, ok := usageExportStreamErrorCode(pageErr); ok {
				serverBytes, serverComplete := usageExportServerBoundFromSnapshot(snapshot)
				_ = writeUsageExportNDJSON(c, usageExportControlDTOv1{
					SchemaVersion: 1, RecordType: "error", ExportSnapshotID: snapshot.ID, ExportSchemaVersion: usageEventsExportSchemaV2,
					ExportDerivationProfile: usageEventsExportProfileV1, Complete: false, Code: code,
					SnapshotFactsHash: snapshot.FactsHash, ServerBytesUpperBound: serverBytes,
					ServerBytesUpperBoundComplete: serverComplete, EstimatedBytesUpperBound: serverBytes,
					BlobLimitBytes: usageExportBlobLimitBytes, CatalogFingerprint: usageExportCatalogFingerprintV1,
					PriceSnapshotFingerprint: usageExportPriceFingerprintV1, ProfileFingerprint: usageEventsExportProfileV1,
				})
			}
			return
		}
		for _, row := range page.Rows {
			if err := writeUsageExportNDJSON(c, buildUsageEventDTOV1(row.Event, row.Detail)); err != nil {
				return
			}
		}
		if !page.HasMore {
			break
		}
		position = page.NextPosition
	}
	serverBytes, serverComplete := usageExportServerBoundFromSnapshot(snapshot)
	_ = writeUsageExportNDJSON(c, usageExportControlDTOv1{
		SchemaVersion: 1, RecordType: "end", ExportSnapshotID: snapshot.ID, ExportSchemaVersion: usageEventsExportSchemaV2,
		ExportDerivationProfile: usageEventsExportProfileV1, Complete: true,
		EventCount: snapshot.EventCount, SnapshotFactsHash: snapshot.FactsHash,
		ServerBytesUpperBound: serverBytes, ServerBytesUpperBoundComplete: serverComplete,
		EstimatedBytesUpperBound: serverBytes, BytesUpperBoundComplete: false,
		BlobLimitBytes: usageExportBlobLimitBytes, CatalogFingerprint: usageExportCatalogFingerprintV1,
		PriceSnapshotFingerprint: usageExportPriceFingerprintV1, ProfileFingerprint: usageEventsExportProfileV1,
	})
	return
}

func usageExportStreamErrorCode(err error) (string, bool) {
	switch {
	case err == nil:
		return "", false
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return "", false
	case errors.Is(err, usage.ErrUsageExportLeaseExpired):
		return "export_snapshot_expired", true
	case errors.Is(err, usage.ErrUsageExportLeaseReleased):
		return "projection_unavailable", true
	case errors.Is(err, usage.ErrUsageExportGenerationChanged):
		return "export_estimate_snapshot_mismatch", true
	case errors.Is(err, usage.ErrProjectionUnavailable), errors.Is(err, usage.ErrProjectionBudgetExceeded):
		return "projection_unavailable", true
	default:
		return "projection_unavailable", true
	}
}

func (h *Handler) buildUsageExportSnapshot(c *gin.Context, requireSchema bool) (usageExportSnapshot, error) {
	if h == nil || h.usageStats == nil {
		return usageExportSnapshot{}, usage.ErrProjectionUnavailable
	}
	datasetEpoch, revision, rewriteRevision, err := h.usageStats.ProjectionMetadata()
	if err != nil {
		return usageExportSnapshot{}, err
	}
	query, canonical, queryHash, format, err := h.parseUsageExportQuery(c, datasetEpoch, requireSchema)
	if err != nil {
		return usageExportSnapshot{}, err
	}
	return h.buildUsageExportSnapshotFromQueryContext(c.Request.Context(), query, canonical, queryHash, format, datasetEpoch, revision, rewriteRevision)
}

func (h *Handler) buildUsageExportSnapshotFromQuery(query usageEventsParsedQuery, canonical usageEventsCanonicalQueryV1, queryHash, format string, datasetEpoch, revision, rewriteRevision uint64) (usageExportSnapshot, error) {
	return h.buildUsageExportSnapshotFromQueryContext(context.Background(), query, canonical, queryHash, format, datasetEpoch, revision, rewriteRevision)
}

func (h *Handler) buildUsageExportSnapshotFromQueryContext(ctx context.Context, query usageEventsParsedQuery, canonical usageEventsCanonicalQueryV1, queryHash, format string, datasetEpoch, revision, rewriteRevision uint64) (usageExportSnapshot, error) {
	if stats, ok := h.usageStats.(usageExportStatistics); ok {
		manager := h.exportSnapshotManager()
		lease, estimate, err := stats.PinUsageExportWithClock(ctx, usage.UsageExportQuery{From: query.from, To: query.to, Filters: query.filters}, manager.currentTime, manager.leaseTTL())
		if err != nil {
			return usageExportSnapshot{}, err
		}
		serverBytes, serverComplete := usageExportServerBound(estimate, format)
		snapshot := usageExportSnapshot{
			DatasetEpoch:        estimate.DatasetEpoch,
			SnapshotMaxSequence: estimate.SnapshotMaxSequence,
			Revision:            estimate.Revision,
			RewriteRevision:     estimate.RewriteRevision,
			Query:               query,
			CanonicalQuery:      canonical,
			QueryHash:           queryHash,
			FactsHash:           estimate.FactsHash,
			Format:              format,
			SchemaVersion:       usageEventsExportSchemaV2,
			EventCount:          estimate.EventCount,
			ServerBytes:         serverBytes,
			ExpiresAt:           estimate.ExpiresAt,
			ServerBytesComplete: serverComplete,
			ServerJSONBytes:     estimate.ServerJSONBytes,
			ServerCSVBytes:      estimate.ServerCSVBytes,
			ServerJSONComplete:  estimate.ServerJSONComplete,
			ServerCSVComplete:   estimate.ServerCSVComplete,
			Lease:               lease,
		}
		if err := ctx.Err(); err != nil {
			lease.Release()
			return usageExportSnapshot{}, err
		}
		snapshot.MatchedSourceIDs = estimate.MatchedSourceIDs
		snapshot.MatchedModels = estimate.MatchedModels
		created, createErr := h.exportSnapshotManager().create(snapshot)
		if createErr != nil {
			lease.Release()
			return usageExportSnapshot{}, createErr
		}
		return created, nil
	}
	return usageExportSnapshot{}, usage.ErrProjectionUnavailable
}

type usageExportFrame struct {
	EventCount          uint64
	MatchedSourceIDs    map[string]uint64
	MatchedModels       map[string]uint64
	ServerBytes         uint64
	ServerBytesComplete bool
}

// collectUsageExportFrame is retained for tests and compatibility callers. It
// only computes exact event/source/model counts; byte bounds remain explicitly
// incomplete until projection-owned final-format counters are available.
func collectUsageExportFrame(ctx context.Context, lease *usage.UsageExportLease, format string) (usageExportFrame, error) {
	result := usageExportFrame{MatchedSourceIDs: map[string]uint64{}, MatchedModels: map[string]uint64{}}
	if lease == nil {
		return result, usage.ErrProjectionUnavailable
	}
	for position := uint64(0); ; {
		page, err := lease.NextPage(ctx, position, 500)
		if err != nil {
			return usageExportFrame{}, err
		}
		for _, row := range page.Rows {
			result.EventCount++
			result.MatchedSourceIDs[row.Event.SourceID]++
			result.MatchedModels[row.Event.Model]++
		}
		if !page.HasMore {
			break
		}
		position = page.NextPosition
	}
	_ = format
	return result, nil
}

func (h *Handler) parseUsageExportQuery(c *gin.Context, datasetEpoch uint64, requireSchema bool) (usageEventsParsedQuery, usageEventsCanonicalQueryV1, string, string, error) {
	query, err := h.parseUsageEventsQuery(c, datasetEpoch)
	if err != nil {
		return query, usageEventsCanonicalQueryV1{}, "", "", err
	}
	if strings.TrimSpace(query.cursorToken) != "" {
		return query, usageEventsCanonicalQueryV1{}, "", "", errUsageTokenInvalid
	}
	format := strings.ToLower(strings.TrimSpace(c.Query("format")))
	if format != "json" && format != "csv" {
		return query, usageEventsCanonicalQueryV1{}, "", "", errUsageTokenInvalid
	}
	schema := strings.TrimSpace(c.Query("export_schema_version"))
	if requireSchema && schema != usageEventsExportSchemaV2 {
		return query, usageEventsCanonicalQueryV1{}, "", "", errUsageTokenInvalid
	}
	if schema != "" && schema != usageEventsExportSchemaV2 {
		return query, usageEventsCanonicalQueryV1{}, "", "", errUsageTokenInvalid
	}
	canonical, err := canonicalUsageEventsQueryV1(query.identity)
	if err != nil {
		return query, usageEventsCanonicalQueryV1{}, "", "", err
	}
	payload, err := json.Marshal(canonical)
	if err != nil {
		return query, usageEventsCanonicalQueryV1{}, "", "", err
	}
	digest := sha256.Sum256(payload)
	return query, canonical, fmt.Sprintf("%x", digest[:]), format, nil
}

func (h *Handler) exportSnapshotFresh(snapshot usageExportSnapshot) bool {
	manager := h.exportSnapshotManager()
	if manager == nil {
		return false
	}
	manager.mu.Lock()
	now := manager.now().UTC()
	manager.mu.Unlock()
	return now.Before(snapshot.ExpiresAt)
}

func writeUsageExportNDJSON(c *gin.Context, value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	_, err = c.Writer.Write(encoded)
	if err == nil {
		c.Writer.Flush()
	}
	return err
}

func (h *Handler) writeUsageExportRequestError(c *gin.Context, err error) {
	status, code := usageExportErrorStatus(err)
	c.JSON(status, gin.H{"error": code, "code": code})
}

func usageExportErrorStatus(err error) (int, string) {
	switch {
	case errors.Is(err, errUsageExportSnapshotExpired):
		return http.StatusGone, "export_snapshot_expired"
	case errors.Is(err, errUsageExportSnapshotMismatch):
		return http.StatusConflict, "export_estimate_snapshot_mismatch"
	case errors.Is(err, usage.ErrProjectionQueryInvalid), errors.Is(err, errUsageTokenInvalid):
		return http.StatusBadRequest, "invalid_query"
	case errors.Is(err, usage.ErrProjectionUnavailable), errors.Is(err, usage.ErrProjectionBudgetExceeded):
		return http.StatusServiceUnavailable, "projection_unavailable"
	default:
		return http.StatusServiceUnavailable, "projection_unavailable"
	}
}

const usageEventsExportCSVHeaderV1 = "timestamp,model,source,source_raw,auth_index,result,latency_ms,thinking_intensity,thinking_mode,thinking_level,thinking_budget,input_tokens,output_tokens,reasoning_tokens,cache_read_tokens,cache_creation_tokens,cached_tokens,cache_ratio,total_tokens,reported_total_tokens,computed_total_tokens,input_cost_usd,output_cost_usd,cache_cost_usd,total_cost_usd,cost_status,missing_price_models,missing_price_components\r\n"

func saturatingAddUint64(left, right uint64) uint64 {
	if ^uint64(0)-left < right {
		return ^uint64(0)
	}
	return left + right
}
