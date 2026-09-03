package usage

import (
	"strings"
	"time"
)

const maxProjectionEventDetailLookup = 500

// QueryProjectionCatalog returns the current all-time lightweight catalog.
func (s *RequestStatistics) QueryProjectionCatalog() (ProjectionSnapshot, error) {
	projection, err := s.projectionForQuery()
	if err != nil {
		return ProjectionSnapshot{}, err
	}
	return projection.QueryCatalog()
}

// QueryProjectionAuthUsage returns the projection-backed auth metadata view.
func (s *RequestStatistics) QueryProjectionAuthUsage() (map[string]AuthUsageSnapshot, error) {
	projection, err := s.projectionForQuery()
	if err != nil {
		return nil, err
	}
	return projection.QueryAuthUsage()
}

// ProjectionMetadata returns the current projection generation identity.
func (s *RequestStatistics) ProjectionMetadata() (datasetEpoch, revision, rewriteRevision uint64, err error) {
	projection, err := s.projectionForQuery()
	if err != nil {
		return 0, 0, 0, err
	}
	datasetEpoch, revision, rewriteRevision = projection.metadata()
	return datasetEpoch, revision, rewriteRevision, nil
}

// QueryProjectionEvents returns one compact-fact event page.
func (s *RequestStatistics) QueryProjectionEvents(
	from, to time.Time,
	limit int,
	snapshotMaxSequence, physicalScanPosition uint64,
	filters map[string][]string,
) (ProjectionSnapshot, bool, uint64, uint64, string, error) {
	projection, err := s.projectionForQuery()
	if err != nil {
		return ProjectionSnapshot{}, false, 0, 0, "", err
	}
	return projection.QueryEvents(from, to, limit, snapshotMaxSequence, physicalScanPosition, filters)
}

// QueryProjectionSummaryView returns one exact summary/series/health view.
func (s *RequestStatistics) QueryProjectionSummaryView(from, to, observationTo time.Time) (ProjectionSummaryView, error) {
	projection, err := s.projectionForQuery()
	if err != nil {
		return ProjectionSummaryView{}, err
	}
	return projection.QuerySummaryView(from, to, observationTo)
}

func (s *RequestStatistics) projectionForQuery() (*UsageProjection, error) {
	if s == nil {
		return nil, ErrProjectionUnavailable
	}
	s.mu.RLock()
	projection := s.projection
	unavailable := s.projectionUnavailable
	unavailableCode := s.projectionUnavailableCode
	s.mu.RUnlock()
	if unavailable {
		return nil, projectionQueryUnavailableError(unavailableCode)
	}
	if projection == nil {
		return nil, ErrProjectionUnavailable
	}
	return projection, nil
}

func projectionQueryUnavailableError(code string) error {
	if code == "projection_budget_exceeded" {
		return ErrProjectionBudgetExceeded
	}
	return ErrProjectionUnavailable
}

// LookupEventDetails copies only the canonical details selected by a
// projection page. It never rebuilds or scans the canonical detail index on a
// query path.
func (s *RequestStatistics) LookupEventDetails(stableEventIDs []string) (map[string]RequestDetail, error) {
	if s == nil {
		return nil, ErrProjectionUnavailable
	}
	if len(stableEventIDs) > maxProjectionEventDetailLookup {
		return nil, ErrProjectionQueryInvalid
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.projectionUnavailable {
		return nil, projectionQueryUnavailableError(s.projectionUnavailableCode)
	}
	if s.projection == nil || s.detailLocations == nil || s.detailEventLocations == nil {
		return nil, ErrProjectionUnavailable
	}

	result := make(map[string]RequestDetail, len(stableEventIDs))
	for _, rawStableEventID := range stableEventIDs {
		stableEventID := strings.TrimSpace(rawStableEventID)
		if stableEventID == "" {
			continue
		}
		if _, exists := result[stableEventID]; exists {
			continue
		}
		locationKey := s.detailEventLocations[stableEventID]
		location, ok := s.detailLocations[locationKey]
		if !ok || location.modelStats == nil || location.index < 0 || location.index >= len(location.modelStats.Details) {
			continue
		}
		result[stableEventID] = cloneRequestDetail(location.modelStats.Details[location.index])
	}
	return result, nil
}

// LookupEventDetailsForExport resolves a bounded export page and verifies that
// each event reference still names the same immutable version and canonical
// detail hash. Appends after the export high-watermark are allowed; enrichment,
// removal, or generation replacement fails closed instead of mixing a stale
// EventRef with a live detail.
func (s *RequestStatistics) LookupEventDetailsForExport(events []EventRef) (map[string]RequestDetail, error) {
	if s == nil {
		return nil, ErrProjectionUnavailable
	}
	if len(events) > maxProjectionEventDetailLookup {
		return nil, ErrProjectionQueryInvalid
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.projectionUnavailable {
		return nil, projectionQueryUnavailableError(s.projectionUnavailableCode)
	}
	projection := s.projection
	if projection == nil || s.detailLocations == nil || s.detailEventLocations == nil {
		return nil, ErrProjectionUnavailable
	}

	projection.mu.RLock()
	defer projection.mu.RUnlock()
	result := make(map[string]RequestDetail, len(events))
	for _, eventRef := range events {
		stableEventID := strings.TrimSpace(eventRef.StableEventID)
		if stableEventID == "" {
			return nil, ErrUsageExportGenerationChanged
		}
		currentEvent, ok := projection.events[stableEventID]
		if !ok || currentEvent.CanonicalEventIdentity != eventRef.CanonicalEventIdentity || currentEvent.Version != eventRef.Version {
			return nil, ErrUsageExportGenerationChanged
		}
		contribution, ok := projection.facts.contribution(currentEvent.FactRowID, projection.stringRegistry)
		if !ok || contribution.EventRef.Version != eventRef.Version || contribution.EventRef.CanonicalEventIdentity != eventRef.CanonicalEventIdentity {
			return nil, ErrUsageExportGenerationChanged
		}
		locationKey := s.detailEventLocations[stableEventID]
		location, ok := s.detailLocations[locationKey]
		if !ok || location.modelStats == nil || location.index < 0 || location.index >= len(location.modelStats.Details) {
			return nil, ErrUsageExportGenerationChanged
		}
		detail := cloneRequestDetail(location.modelStats.Details[location.index])
		if canonicalDetailHash(detail) != currentEvent.CanonicalDetailHash {
			return nil, ErrUsageExportGenerationChanged
		}
		result[stableEventID] = detail
	}
	return result, nil
}
