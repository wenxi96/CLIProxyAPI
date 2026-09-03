package usage

import (
	"context"
	"fmt"
	"sort"
	"time"
)

const projectionCaptureChunkSize = 256

type projectionCaptureEvent struct {
	apiName             string
	modelName           string
	detailIndex         int
	detail              RequestDetail
	identity            ProjectionIdentity
	canonicalDetailHash string
	version             uint64
	contribution        projectionContribution
}

type projectionCaptureMetadata struct {
	datasetEpoch     uint64
	revision         uint64
	rewriteRevision  uint64
	nextSequence     uint64
	changeCount      uint64
	persistedCount   uint64
	baseJournalHead  uint64
	finalJournalHead uint64
	budget           ProjectionBudgetV2
	ordinals         map[string]uint64
	catalog          ProjectionCatalog
	terminalFrontier uint64
	terminalIntents  []IdentitySidecarTerminalIntent
	bulkTransaction  *MutationBatchMarker
}

type capturedProjectionGeneration struct {
	snapshot         StatisticsSnapshot
	projection       *UsageProjection
	sidecar          IdentitySidecar
	changeCount      uint64
	persistedCount   uint64
	baseJournalHead  uint64
	finalJournalHead uint64
	initialFenceHold time.Duration
	finalFenceHold   time.Duration
}

func captureProjectionGenerationState(ctx context.Context, stats *RequestStatistics) (capturedProjectionGeneration, error) {
	if stats == nil {
		projection := NewUsageProjection()
		return capturedProjectionGeneration{
			projection: projection,
			sidecar:    projection.identitySidecarSnapshot(),
		}, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	coordinator := stats.coordinator
	if coordinator == nil {
		stats.mu.RLock()
		state := stats.cloneStateLocked()
		stats.mu.RUnlock()
		captured := &RequestStatistics{}
		captured.adoptStateLocked(state)
		snapshot, changeCount, persistedCount := captured.SnapshotWithState()
		projection := captured.projection
		if projection == nil {
			projection = NewUsageProjection()
		}
		return capturedProjectionGeneration{
			snapshot:       snapshot,
			projection:     projection,
			sidecar:        projection.identitySidecarSnapshot(),
			changeCount:    changeCount,
			persistedCount: persistedCount,
		}, nil
	}

	initialFenceStartedAt := time.Now()
	session, err := coordinator.beginProjectionCapture(ctx)
	if err != nil {
		return capturedProjectionGeneration{}, err
	}
	defer session.finish()

	stats.mu.RLock()
	projection := stats.projection
	if projection == nil {
		stats.mu.RUnlock()
		return capturedProjectionGeneration{}, ErrProjectionUnavailable
	}
	projection.mu.RLock()
	baseEventIDs := make([]string, 0, len(projection.events))
	for stableEventID := range projection.events {
		baseEventIDs = append(baseEventIDs, stableEventID)
	}
	projection.mu.RUnlock()
	stats.mu.RUnlock()
	baseJournalHead := coordinator.journal.Head()
	if err := session.setBaseJournalHead(baseJournalHead); err != nil {
		return capturedProjectionGeneration{}, err
	}
	session.releaseFence()
	initialFenceHold := time.Since(initialFenceStartedAt)

	sort.Strings(baseEventIDs)
	events := make(map[string]*projectionCaptureEvent, len(baseEventIDs))
	for start := 0; start < len(baseEventIDs); start += projectionCaptureChunkSize {
		if err := ctx.Err(); err != nil {
			return capturedProjectionGeneration{}, err
		}
		end := start + projectionCaptureChunkSize
		if end > len(baseEventIDs) {
			end = len(baseEventIDs)
		}
		stats.mu.RLock()
		if stats.projection != projection {
			stats.mu.RUnlock()
			return capturedProjectionGeneration{}, ErrProjectionCASConflict
		}
		projection.mu.RLock()
		for _, stableEventID := range baseEventIDs[start:end] {
			event, ok := captureProjectionEventLocked(stats, projection, stableEventID)
			if !ok {
				projection.mu.RUnlock()
				stats.mu.RUnlock()
				return capturedProjectionGeneration{}, fmt.Errorf("%w: capture base event %q", ErrProjectionUnavailable, stableEventID)
			}
			events[stableEventID] = event
		}
		projection.mu.RUnlock()
		stats.mu.RUnlock()
	}

	finalFenceStartedAt := time.Now()
	if err := session.beginFinalFence(ctx); err != nil {
		return capturedProjectionGeneration{}, err
	}
	metadata := projectionCaptureMetadata{baseJournalHead: baseJournalHead}
	metadata.finalJournalHead = coordinator.journal.Head()
	if metadata.finalJournalHead < metadata.baseJournalHead || metadata.finalJournalHead-metadata.baseJournalHead > uint64(coordinator.config.MaxFinalizeReplayIntents) {
		return capturedProjectionGeneration{}, ErrProjectionUnavailable
	}
	suffix := coordinator.journal.EntriesAfter(metadata.baseJournalHead, false)
	if uint64(len(suffix)) != metadata.finalJournalHead-metadata.baseJournalHead {
		return capturedProjectionGeneration{}, ErrProjectionUnavailable
	}
	metadata.terminalFrontier, metadata.terminalIntents = coordinator.journal.TerminalSnapshot()
	if metadata.terminalFrontier != metadata.finalJournalHead {
		return capturedProjectionGeneration{}, ErrProjectionUnavailable
	}
	terminalStates := make(map[uint64]MutationTerminalState, len(metadata.terminalIntents))
	for _, terminal := range metadata.terminalIntents {
		terminalStates[terminal.Sequence] = terminal.State
	}
	if marker, ok := coordinator.journal.TerminalBulkTransactionSnapshot(); ok {
		metadata.bulkTransaction = &marker
	}
	stats.mu.RLock()
	if stats.projection != projection || stats.projectionUnavailable {
		stats.mu.RUnlock()
		return capturedProjectionGeneration{}, ErrProjectionUnavailable
	}
	projection.mu.RLock()
	for _, intent := range suffix {
		terminalState, terminal := terminalStates[intent.Sequence]
		if !terminal {
			projection.mu.RUnlock()
			stats.mu.RUnlock()
			return capturedProjectionGeneration{}, fmt.Errorf("%w: capture suffix intent %d is not terminal", ErrProjectionUnavailable, intent.Sequence)
		}
		if terminalState != MutationTerminalCommitted {
			continue
		}
		stableEventID := intent.StableEventID
		if canonicalStableEventID := projection.canonical[newProjectionIndexKey(projectionIndexCanonical, intent.CanonicalEventIdentity)]; canonicalStableEventID != "" {
			stableEventID = canonicalStableEventID
		}
		if intent.Operation == MutationOperationTombstone {
			if existing := events[stableEventID]; existing != nil && existing.identity.CanonicalEventIdentity == intent.CanonicalEventIdentity {
				delete(events, stableEventID)
			}
			continue
		}
		capturedEvent, ok := captureProjectionEventLocked(stats, projection, stableEventID)
		if !ok {
			projection.mu.RUnlock()
			stats.mu.RUnlock()
			return capturedProjectionGeneration{}, fmt.Errorf("%w: capture suffix event %q", ErrProjectionUnavailable, stableEventID)
		}
		if stableEventID != intent.StableEventID {
			if existing := events[intent.StableEventID]; existing != nil && existing.identity.CanonicalEventIdentity == intent.CanonicalEventIdentity {
				delete(events, intent.StableEventID)
			}
		}
		events[stableEventID] = capturedEvent
	}
	if len(events) != len(projection.events) {
		projection.mu.RUnlock()
		stats.mu.RUnlock()
		return capturedProjectionGeneration{}, fmt.Errorf("%w: captured events=%d live events=%d", ErrProjectionUnavailable, len(events), len(projection.events))
	}
	metadata.datasetEpoch = projection.datasetEpoch
	metadata.revision = projection.revision
	metadata.rewriteRevision = projection.rewriteRevision
	metadata.nextSequence = projection.nextSequence
	metadata.budget = projection.budget
	metadata.ordinals = make(map[string]uint64, len(projection.ordinals))
	for key, ordinal := range projection.ordinals {
		metadata.ordinals[key] = ordinal
	}
	metadata.catalog = cloneCatalog(projection.catalog)
	projection.mu.RUnlock()
	metadata.changeCount = stats.changeCount
	metadata.persistedCount = stats.persistedCount
	stats.mu.RUnlock()
	session.finish()
	finalFenceHold := time.Since(finalFenceStartedAt)
	if len(events) == 0 && len(baseEventIDs) != 0 {
		return capturedProjectionGeneration{}, ErrProjectionUnavailable
	}

	snapshot := buildProjectionCaptureSnapshot(events)
	capturedProjection, err := buildCapturedProjection(events, metadata)
	if err != nil {
		return capturedProjectionGeneration{}, err
	}
	sidecar := capturedProjection.identitySidecarSnapshot()
	sidecar.TerminalFrontier = metadata.terminalFrontier
	sidecar.TerminalIntents = metadata.terminalIntents
	if metadata.bulkTransaction != nil {
		marker := *metadata.bulkTransaction
		sidecar.BulkTransaction = &marker
	}
	return capturedProjectionGeneration{
		snapshot:         snapshot,
		projection:       capturedProjection,
		sidecar:          sidecar,
		changeCount:      metadata.changeCount,
		persistedCount:   metadata.persistedCount,
		baseJournalHead:  metadata.baseJournalHead,
		finalJournalHead: metadata.finalJournalHead,
		initialFenceHold: initialFenceHold,
		finalFenceHold:   finalFenceHold,
	}, nil
}

func captureProjectionEventLocked(stats *RequestStatistics, projection *UsageProjection, stableEventID string) (*projectionCaptureEvent, bool) {
	if stats == nil || projection == nil {
		return nil, false
	}
	event, ok := projection.events[stableEventID]
	if !ok {
		return nil, false
	}
	contribution, ok := projection.facts.contribution(event.FactRowID, projection.stringRegistry)
	if !ok {
		return nil, false
	}
	locationKey := stats.detailEventLocations[stableEventID]
	location, ok := stats.detailLocations[locationKey]
	if !ok || location.modelStats == nil || location.index < 0 || location.index >= len(location.modelStats.Details) {
		return nil, false
	}
	detail := cloneRequestDetail(location.modelStats.Details[location.index])
	if canonicalDetailHash(detail) != event.CanonicalDetailHash {
		return nil, false
	}
	return &projectionCaptureEvent{
		apiName:     location.apiName,
		modelName:   location.modelName,
		detailIndex: location.index,
		detail:      detail,
		identity: ProjectionIdentity{
			CanonicalIdentitySeed:  event.CanonicalIdentitySeed,
			CanonicalEventIdentity: event.CanonicalEventIdentity,
			StableEventID:          event.StableEventID,
			Sequence:               contribution.EventRef.Sequence,
			BatchOrdinal:           contribution.EventRef.BatchOrdinal,
			SourceGroupKey:         event.SourceGroupKey,
			SourceGroupOrdinal:     event.SourceGroupOrdinal,
		},
		canonicalDetailHash: event.CanonicalDetailHash,
		version:             event.Version,
		contribution:        contribution,
	}, true
}

func buildProjectionCaptureSnapshot(events map[string]*projectionCaptureEvent) StatisticsSnapshot {
	result := StatisticsSnapshot{
		APIs:           make(map[string]APISnapshot),
		RequestsByDay:  make(map[string]int64),
		RequestsByHour: make(map[string]int64),
		TokensByDay:    make(map[string]int64),
		TokensByHour:   make(map[string]int64),
	}
	type captureModelKey struct {
		apiName   string
		modelName string
	}
	grouped := make(map[captureModelKey][]*projectionCaptureEvent)
	for _, event := range events {
		key := captureModelKey{apiName: event.apiName, modelName: event.modelName}
		grouped[key] = append(grouped[key], event)
	}
	keys := make([]captureModelKey, 0, len(grouped))
	for key := range grouped {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].apiName != keys[j].apiName {
			return keys[i].apiName < keys[j].apiName
		}
		return keys[i].modelName < keys[j].modelName
	})
	for _, key := range keys {
		modelEvents := grouped[key]
		sort.SliceStable(modelEvents, func(i, j int) bool {
			if modelEvents[i].detailIndex != modelEvents[j].detailIndex {
				return modelEvents[i].detailIndex < modelEvents[j].detailIndex
			}
			if modelEvents[i].identity.Sequence != modelEvents[j].identity.Sequence {
				return modelEvents[i].identity.Sequence < modelEvents[j].identity.Sequence
			}
			return modelEvents[i].identity.StableEventID < modelEvents[j].identity.StableEventID
		})
		apiSnapshot := result.APIs[key.apiName]
		if apiSnapshot.Models == nil {
			apiSnapshot.Models = make(map[string]ModelSnapshot)
		}
		modelSnapshot := ModelSnapshot{Details: make([]RequestDetail, 0, len(modelEvents))}
		for _, event := range modelEvents {
			detail := cloneRequestDetail(event.detail)
			modelSnapshot.Details = append(modelSnapshot.Details, detail)
			modelSnapshot.TotalRequests++
			modelSnapshot.TotalTokens += detail.Tokens.TotalTokens
			apiSnapshot.TotalRequests++
			apiSnapshot.TotalTokens += detail.Tokens.TotalTokens
			result.TotalRequests++
			result.TotalTokens += detail.Tokens.TotalTokens
			if detail.Failed {
				result.FailureCount++
			} else {
				result.SuccessCount++
			}
			dayKey := detail.Timestamp.Format("2006-01-02")
			hourKey := formatHour(detail.Timestamp.Hour())
			result.RequestsByDay[dayKey]++
			result.RequestsByHour[hourKey]++
			result.TokensByDay[dayKey] += detail.Tokens.TotalTokens
			result.TokensByHour[hourKey] += detail.Tokens.TotalTokens
		}
		apiSnapshot.Models[key.modelName] = modelSnapshot
		result.APIs[key.apiName] = apiSnapshot
	}
	result.Auths = buildAuthUsageSnapshots(result.APIs)
	return result
}

func buildCapturedProjection(events map[string]*projectionCaptureEvent, metadata projectionCaptureMetadata) (*UsageProjection, error) {
	projection := NewUsageProjection()
	projection.budget = metadata.budget.normalized()
	ordered := make([]*projectionCaptureEvent, 0, len(events))
	for _, event := range events {
		ordered = append(ordered, event)
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].identity.Sequence != ordered[j].identity.Sequence {
			return ordered[i].identity.Sequence < ordered[j].identity.Sequence
		}
		return ordered[i].identity.StableEventID < ordered[j].identity.StableEventID
	})
	for _, event := range ordered {
		contribution := event.contribution
		contribution.APIID = projection.stringRegistry.intern(contribution.API)
		contribution.ModelID = projection.stringRegistry.intern(contribution.Model)
		contribution.ProviderID = projection.stringRegistry.intern(contribution.Provider)
		contribution.AuthID = projection.stringRegistry.intern(contribution.AuthIndex)
		contribution.SourceIDValue = projection.stringRegistry.intern(contribution.SourceID)
		contribution.PriceKeyID = projection.stringRegistry.intern(contribution.PriceKey)
		contribution.ProviderStateID = projection.stringRegistry.intern(contribution.ProviderState)
		contribution.FacetIDs = [5]projectionStringID{
			projection.stringRegistry.intern("api:" + contribution.API),
			projection.stringRegistry.intern("model:" + contribution.Model),
			projection.stringRegistry.intern("provider:" + contribution.Provider),
			projection.stringRegistry.intern("auth:" + contribution.AuthIndex),
			projection.stringRegistry.intern("source:" + contribution.SourceID),
		}
		rowID := projection.applyContributionLocked(contribution)
		projection.events[event.identity.StableEventID] = projectionEvent{
			CanonicalIdentitySeed:  event.identity.CanonicalIdentitySeed,
			CanonicalEventIdentity: event.identity.CanonicalEventIdentity,
			StableEventID:          event.identity.StableEventID,
			SourceGroupKey:         event.identity.SourceGroupKey,
			SourceGroupOrdinal:     event.identity.SourceGroupOrdinal,
			CanonicalDetailHash:    event.canonicalDetailHash,
			Version:                event.version,
			FactRowID:              rowID,
		}
		projection.canonical[newProjectionIndexKey(projectionIndexCanonical, event.identity.CanonicalEventIdentity)] = event.identity.StableEventID
		projection.coordinate[coordinateIndexKey(event.apiName, event.detail)] = event.identity.StableEventID
		projection.registerLookupLocked(event.apiName, event.detail, detailIdentityKey(event.apiName, event.modelName, event.detail), event.identity.StableEventID)
	}
	projection.datasetEpoch = metadata.datasetEpoch
	projection.revision = metadata.revision
	projection.rewriteRevision = metadata.rewriteRevision
	projection.nextSequence = metadata.nextSequence
	projection.ordinals = make(map[string]uint64, len(metadata.ordinals))
	for key, ordinal := range metadata.ordinals {
		projection.ordinals[key] = ordinal
	}
	projection.catalog = cloneCatalog(metadata.catalog)
	if err := projection.validateStorageBudgetLocked(projection.storageMetricsLocked()); err != nil {
		return nil, err
	}
	return projection, nil
}
