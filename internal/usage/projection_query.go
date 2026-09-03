package usage

import (
	"encoding/hex"
	"sort"
	"strconv"
	"strings"
	"time"
	"unsafe"
)

type projectionSummaryPricing struct {
	totals    Aggregate
	apis      map[string]Aggregate
	models    map[string]Aggregate
	providers map[string]Aggregate
	auths     map[string]Aggregate
	sources   map[string]Aggregate
}

type projectionEventsSnapshot struct {
	from                time.Time
	to                  time.Time
	rows                []projectionFactRowID
	facts               usageFactStoreV1
	registry            projectionStringRegistry
	filters             map[string][]string
	datasetEpoch        uint64
	revision            uint64
	rewriteRevision     uint64
	snapshotMaxSequence uint64
	postingID           string
	cacheKey            string
	cacheHit            bool
	candidateRows       uint64
	scratchBytes        uint64
}

// QueryCatalog returns the all-time projection-owned registries without
// materializing event rows, postings, rollups, or canonical details.
func (p *UsageProjection) QueryCatalog() (ProjectionSnapshot, error) {
	if p == nil {
		return ProjectionSnapshot{}, ErrProjectionUnavailable
	}
	p.mu.RLock()
	if err := p.validateStorageBudgetLocked(p.storageMetricsLocked()); err != nil {
		p.mu.RUnlock()
		return ProjectionSnapshot{}, err
	}
	result := ProjectionSnapshot{
		SchemaVersion:   projectionSchemaVersion,
		DatasetEpoch:    p.datasetEpoch,
		Revision:        p.revision,
		RewriteRevision: p.rewriteRevision,
		Catalog:         cloneCatalog(p.catalog),
	}
	p.mu.RUnlock()
	return result, nil
}

// QueryAuthUsage builds the all-time auth metadata view from yearly scalar
// rollups and auth postings. It does not rebuild aggregates from canonical
// details or the full legacy snapshot.
func (p *UsageProjection) QueryAuthUsage() (map[string]AuthUsageSnapshot, error) {
	if p == nil {
		return nil, ErrProjectionUnavailable
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	if err := p.validateStorageBudgetLocked(p.storageMetricsLocked()); err != nil {
		return nil, err
	}
	result := make(map[string]AuthUsageSnapshot)
	authIDs := make(map[projectionStringID]struct{})
	for _, bucket := range p.years {
		for authID, aggregate := range bucket.Auths {
			if authID == 0 || aggregate.counters.value(compactAggregateTotalRequests) <= 0 {
				continue
			}
			authIndex := strings.TrimSpace(p.stringRegistry.value(authID))
			if authIndex == "" {
				continue
			}
			snapshot := result[authIndex]
			if snapshot.AuthIndex == "" {
				snapshot.AuthIndex = authIndex
			}
			addCompactAggregateToAuthUsage(&snapshot, aggregate)
			result[authIndex] = snapshot
			authIDs[authID] = struct{}{}
		}
	}

	candidateRows := uint64(0)
	for authID := range authIDs {
		candidateRows = saturatingAddUint64(candidateRows, uint64(len(p.postings.rows[projectionPostingKey{
			Dimension: postingDimensionAuth,
			Value:     authID,
		}])))
	}
	budget := p.budget.normalized()
	if candidateRows > budget.MaxScannedFactRows {
		return nil, &ProjectionBudgetError{
			Dimension: projectionBudgetScannedFactRows,
			Limit:     budget.MaxScannedFactRows,
			Observed:  candidateRows,
		}
	}
	rowBytes := saturatingMulUint64(candidateRows, uint64(unsafe.Sizeof(projectionFactRowID(0))))
	scratchBytes := saturatingAddUint64(rowBytes, saturatingMulUint64(uint64(len(result)), 256))
	if scratchBytes > budget.MaxQueryScratchBytes {
		return nil, &ProjectionBudgetError{
			Dimension: projectionBudgetQueryScratchBytes,
			Limit:     budget.MaxQueryScratchBytes,
			Observed:  scratchBytes,
		}
	}
	for authID := range authIDs {
		postingRows := p.postings.rows[projectionPostingKey{Dimension: postingDimensionAuth, Value: authID}]
		authIndex := strings.TrimSpace(p.stringRegistry.value(authID))
		auth := result[authIndex]
		for _, rowID := range postingRows {
			if !p.facts.isActive(rowID) {
				continue
			}
			timestamp, ok := p.facts.timestamp(rowID)
			if !ok || timestamp.IsZero() {
				continue
			}
			if auth.FirstRequestAt == nil || timestamp.Before(*auth.FirstRequestAt) {
				first := timestamp
				auth.FirstRequestAt = &first
			}
			if auth.LastRequestAt == nil || timestamp.After(*auth.LastRequestAt) {
				last := timestamp
				auth.LastRequestAt = &last
			}
		}
		result[authIndex] = auth
	}
	return result, nil
}

func addCompactAggregateToAuthUsage(snapshot *AuthUsageSnapshot, aggregate compactAggregate) {
	if snapshot == nil {
		return
	}
	snapshot.TotalRequests += aggregate.counters.value(compactAggregateTotalRequests)
	snapshot.SuccessCount += aggregate.counters.value(compactAggregateSuccessCount)
	snapshot.FailureCount += aggregate.counters.value(compactAggregateFailureCount)
	snapshot.Tokens.InputTokens += aggregate.counters.value(compactAggregateInputTokens)
	snapshot.Tokens.OutputTokens += aggregate.counters.value(compactAggregateOutputTokens)
	snapshot.Tokens.ReasoningTokens += aggregate.counters.value(compactAggregateReasoningTokens)
	snapshot.Tokens.CachedTokens += aggregate.counters.value(compactAggregateCachedTokens)
	snapshot.Tokens.TotalTokens += aggregate.counters.value(compactAggregateTotalTokens)
}

// QuerySummary returns exact scalar and pricing facts for a half-open range.
// It reads compact fact columns only and never copies canonical details.
func (p *UsageProjection) QuerySummary(from, to time.Time) (ProjectionSnapshot, error) {
	if p == nil {
		return ProjectionSnapshot{}, ErrProjectionUnavailable
	}
	if !from.IsZero() {
		from = from.UTC()
	}
	if !to.IsZero() {
		to = to.UTC()
	}
	if !from.IsZero() && !to.IsZero() && !from.Before(to) {
		return ProjectionSnapshot{}, ErrProjectionQueryInvalid
	}

	p.mu.RLock()
	if err := p.validateStorageBudgetLocked(p.storageMetricsLocked()); err != nil {
		p.mu.RUnlock()
		return ProjectionSnapshot{}, err
	}
	rows := p.selectTimeRangeCandidateRowsLocked(projectionTimeRange{from: from, to: to})
	candidateRows := uint64(len(rows))
	budget := p.budget.normalized()
	if candidateRows > budget.MaxScannedFactRows {
		p.mu.RUnlock()
		return ProjectionSnapshot{}, &ProjectionBudgetError{
			Dimension: projectionBudgetScannedFactRows,
			Limit:     budget.MaxScannedFactRows,
			Observed:  candidateRows,
		}
	}
	rowBytes := saturatingMulUint64(candidateRows, uint64(unsafe.Sizeof(projectionFactRowID(0))))
	activeBitsBytes := saturatingMulUint64(uint64(len(p.facts.activeBits)), uint64(unsafe.Sizeof(uint64(0))))
	scratchBytes := saturatingAddUint64(rowBytes, activeBitsBytes)
	scratchBytes = saturatingAddUint64(scratchBytes, saturatingMulUint64(candidateRows, 384))
	if scratchBytes > budget.MaxQueryScratchBytes {
		p.mu.RUnlock()
		return ProjectionSnapshot{}, &ProjectionBudgetError{
			Dimension: projectionBudgetQueryScratchBytes,
			Limit:     budget.MaxQueryScratchBytes,
			Observed:  scratchBytes,
		}
	}

	rowSnapshot := append([]projectionFactRowID(nil), rows...)
	facts := p.facts
	facts.activeBits = append([]uint64(nil), p.facts.activeBits...)
	registry := p.stringRegistry
	registry.byValue = nil
	datasetEpoch := p.datasetEpoch
	revision := p.revision
	rewriteRevision := p.rewriteRevision
	p.mu.RUnlock()

	accumulator := newCompactHourBucket(from, to)
	pricing := projectionSummaryPricing{
		apis:      make(map[string]Aggregate),
		models:    make(map[string]Aggregate),
		providers: make(map[string]Aggregate),
		auths:     make(map[string]Aggregate),
		sources:   make(map[string]Aggregate),
	}
	for _, rowID := range rowSnapshot {
		if !facts.isActive(rowID) {
			continue
		}
		contribution, ok := facts.contribution(rowID, registry)
		if !ok || !projectionPricingTimestampMatches(contribution.Timestamp, from, to) {
			continue
		}
		accumulator.applyContribution(contribution, 1)
		addPricingContribution(&pricing.totals, "global", "", contribution)
		addProjectionSummaryPricing(pricing.apis, "api", contribution.API, contribution)
		addProjectionSummaryPricing(pricing.models, "model", contribution.Model, contribution)
		addProjectionSummaryPricing(pricing.providers, "provider", contribution.Provider, contribution)
		addProjectionSummaryPricing(pricing.auths, "auth", contribution.AuthIndex, contribution)
		addProjectionSummaryPricing(pricing.sources, "source", contribution.SourceID, contribution)
	}

	materializer := &UsageProjection{stringRegistry: registry}
	bucket := materializer.materializeHourBucket(accumulator)
	result := ProjectionSnapshot{
		SchemaVersion:   projectionSchemaVersion,
		DatasetEpoch:    datasetEpoch,
		Revision:        revision,
		RewriteRevision: rewriteRevision,
		Totals:          bucket.Totals,
		APIs:            bucket.APIs,
		Models:          bucket.Models,
		Providers:       bucket.Providers,
		Auths:           bucket.Auths,
		Sources:         bucket.Sources,
		Facets:          bucket.Totals.Facets,
		Hours:           make(map[string]HourBucket),
		Days:            make(map[string]HourBucket),
		Weeks:           make(map[string]HourBucket),
		Months:          make(map[string]HourBucket),
		Years:           make(map[string]HourBucket),
		Health:          make(map[string]HealthBucket),
		Postings:        make(map[string][]EventRef),
		Catalog: ProjectionCatalog{
			Models:    make(map[string]CatalogEntry),
			PriceKeys: make(map[string]CatalogEntry),
			Sources:   make(map[string]CatalogEntry),
		},
	}
	result.Totals.PricingGroups = pricing.totals.PricingGroups
	mergeProjectionSummaryPricing(result.APIs, pricing.apis)
	mergeProjectionSummaryPricing(result.Models, pricing.models)
	mergeProjectionSummaryPricing(result.Providers, pricing.providers)
	mergeProjectionSummaryPricing(result.Auths, pricing.auths)
	mergeProjectionSummaryPricing(result.Sources, pricing.sources)
	return result, nil
}

// QueryEvents returns one immutable event page from compact fact rows. The
// first page pins the maximum sequence in its matching event set; later pages
// reuse it so newly appended events cannot move an existing cursor view.
func (p *UsageProjection) QueryEvents(
	from, to time.Time,
	limit int,
	snapshotMaxSequence, physicalScanPosition uint64,
	filters map[string][]string,
) (ProjectionSnapshot, bool, uint64, uint64, string, error) {
	if p == nil {
		return ProjectionSnapshot{}, false, 0, 0, "", ErrProjectionUnavailable
	}
	snapshot, err := p.captureEventsSnapshot(from, to, snapshotMaxSequence, filters)
	if err != nil {
		return ProjectionSnapshot{}, false, 0, 0, "", err
	}
	if limit < 1 || limit > 500 {
		return ProjectionSnapshot{}, false, 0, snapshot.snapshotMaxSequence, snapshot.postingID, ErrProjectionQueryInvalid
	}

	orderedRows := snapshot.rows
	if !snapshot.cacheHit {
		orderedRows = make([]projectionFactRowID, 0, len(snapshot.rows))
		for _, rowID := range snapshot.rows {
			if !snapshot.facts.isActive(rowID) ||
				(snapshot.snapshotMaxSequence != 0 && snapshot.facts.sequence(rowID) > snapshot.snapshotMaxSequence) {
				continue
			}
			contribution, ok := snapshot.facts.contribution(rowID, snapshot.registry)
			if !ok || !projectionPricingTimestampMatches(contribution.Timestamp, snapshot.from, snapshot.to) ||
				!projectionEventFiltersMatch(contribution, snapshot.filters) {
				continue
			}
			orderedRows = append(orderedRows, rowID)
		}
		sort.Slice(orderedRows, func(i, j int) bool {
			return projectionEventRowBefore(snapshot.facts, orderedRows[i], orderedRows[j])
		})
		if snapshot.snapshotMaxSequence == 0 {
			for _, rowID := range orderedRows {
				sequence := snapshot.facts.sequence(rowID)
				if sequence > snapshot.snapshotMaxSequence {
					snapshot.snapshotMaxSequence = sequence
				}
			}
			snapshot.cacheKey = projectionEventsPageCacheKey(
				snapshot.datasetEpoch, snapshot.rewriteRevision, snapshot.snapshotMaxSequence,
				snapshot.from, snapshot.to, snapshot.filters,
			)
		}
		p.eventPageCache.put(snapshot.cacheKey, orderedRows, snapshot.postingID, snapshot.candidateRows, snapshot.scratchBytes)
	}

	if physicalScanPosition > uint64(len(orderedRows)) {
		return ProjectionSnapshot{}, false, 0, snapshot.snapshotMaxSequence, snapshot.postingID, ErrProjectionQueryInvalid
	}
	start := int(physicalScanPosition)
	end := start + limit
	if end > len(orderedRows) {
		end = len(orderedRows)
	}
	hasMore := end < len(orderedRows)
	nextPosition := uint64(end)
	result := ProjectionSnapshot{
		SchemaVersion:   projectionSchemaVersion,
		DatasetEpoch:    snapshot.datasetEpoch,
		Revision:        snapshot.revision,
		RewriteRevision: snapshot.rewriteRevision,
		Events:          make([]EventRef, 0, end-start),
	}
	for _, rowID := range orderedRows[start:end] {
		contribution, ok := snapshot.facts.contribution(rowID, snapshot.registry)
		if !ok {
			return ProjectionSnapshot{}, false, 0, snapshot.snapshotMaxSequence, snapshot.postingID, ErrProjectionUnavailable
		}
		result.Events = append(result.Events, contribution.EventRef)
	}
	return result, hasMore, nextPosition, snapshot.snapshotMaxSequence, snapshot.postingID, nil
}

// queryAuthRequestRefs resolves legacy auth-offset candidates from the auth
// posting list. It intentionally returns only immutable event references; the
// caller performs the bounded canonical-detail lookup needed to preserve the
// historical response schema and sorting.
func (p *UsageProjection) queryAuthRequestRefs(authIndex string, filter AuthRequestFilter) ([]EventRef, error) {
	authIndex = strings.TrimSpace(authIndex)
	if p == nil || authIndex == "" {
		return nil, ErrProjectionUnavailable
	}
	filters := map[string][]string{"auth_index": {authIndex}}
	if model := strings.TrimSpace(filter.Model); model != "" {
		filters["model"] = []string{model}
	}
	if filter.Failed != nil {
		filters["failed"] = []string{strconv.FormatBool(*filter.Failed)}
	}
	from, to := projectionAuthRequestFilterRange(filter)
	if !from.IsZero() && !to.IsZero() && !from.Before(to) {
		return []EventRef{}, nil
	}
	snapshot, err := p.captureEventsSnapshot(from, to, 0, filters)
	if err != nil {
		return nil, err
	}
	result := make([]EventRef, 0, len(snapshot.rows))
	for _, rowID := range snapshot.rows {
		if !snapshot.facts.isActive(rowID) {
			continue
		}
		contribution, ok := snapshot.facts.contribution(rowID, snapshot.registry)
		if !ok || !projectionEventFiltersMatch(contribution, snapshot.filters) ||
			!projectionAuthRequestTimeMatches(contribution.Timestamp, filter) {
			continue
		}
		result = append(result, contribution.EventRef)
	}
	return result, nil
}

func projectionAuthRequestTimeMatches(timestamp time.Time, filter AuthRequestFilter) bool {
	if filter.From != nil && timestamp.Before(filter.From.UTC()) {
		return false
	}
	if filter.To == nil {
		return true
	}
	boundary := filter.To.UTC()
	if filter.ToExclusive {
		return timestamp.Before(boundary)
	}
	return !timestamp.After(boundary)
}

func projectionAuthRequestFilterRange(filter AuthRequestFilter) (time.Time, time.Time) {
	var from time.Time
	if filter.From != nil {
		from = filter.From.UTC()
	}
	var to time.Time
	if filter.To != nil {
		to = filter.To.UTC()
		if !filter.ToExclusive {
			to = to.Add(time.Nanosecond)
		}
	}
	return from, to
}

func (p *UsageProjection) captureEventsSnapshot(
	from, to time.Time,
	snapshotMaxSequence uint64,
	filters map[string][]string,
) (projectionEventsSnapshot, error) {
	result := projectionEventsSnapshot{}
	if !from.IsZero() {
		from = from.UTC()
	}
	if !to.IsZero() {
		to = to.UTC()
	}
	if !from.IsZero() && !to.IsZero() && !from.Before(to) {
		return result, ErrProjectionQueryInvalid
	}
	normalizedFilters, err := normalizeProjectionEventFilters(filters)
	if err != nil {
		return result, err
	}

	p.mu.RLock()
	if err := p.validateStorageBudgetLocked(p.storageMetricsLocked()); err != nil {
		p.mu.RUnlock()
		return result, err
	}
	budget := p.budget.normalized()
	currentMaxSequence := uint64(0)
	if p.nextSequence > 0 {
		currentMaxSequence = p.nextSequence - 1
	}
	if snapshotMaxSequence > currentMaxSequence {
		p.mu.RUnlock()
		return result, ErrProjectionQueryInvalid
	}
	cacheKey := ""
	if snapshotMaxSequence != 0 {
		cacheKey = projectionEventsPageCacheKey(p.datasetEpoch, p.rewriteRevision, snapshotMaxSequence, from, to, normalizedFilters)
	}
	if cacheKey != "" {
		if cached, ok := p.eventPageCache.get(cacheKey); ok {
			if cached.candidateRows > budget.MaxScannedFactRows {
				p.mu.RUnlock()
				return result, &ProjectionBudgetError{
					Dimension: projectionBudgetScannedFactRows,
					Limit:     budget.MaxScannedFactRows,
					Observed:  cached.candidateRows,
				}
			}
			if cached.scratchBytes > budget.MaxQueryScratchBytes {
				p.mu.RUnlock()
				return result, &ProjectionBudgetError{
					Dimension: projectionBudgetQueryScratchBytes,
					Limit:     budget.MaxQueryScratchBytes,
					Observed:  cached.scratchBytes,
				}
			}
			result = projectionEventsSnapshot{
				from:                from,
				to:                  to,
				rows:                cached.rows,
				facts:               p.facts,
				registry:            p.stringRegistry,
				filters:             normalizedFilters,
				datasetEpoch:        p.datasetEpoch,
				revision:            p.revision,
				rewriteRevision:     p.rewriteRevision,
				snapshotMaxSequence: snapshotMaxSequence,
				postingID:           cached.postingID,
				cacheKey:            cacheKey,
				cacheHit:            true,
				candidateRows:       cached.candidateRows,
				scratchBytes:        cached.scratchBytes,
			}
			result.facts.activeBits = append([]uint64(nil), p.facts.activeBits...)
			result.registry.byValue = nil
			p.mu.RUnlock()
			return result, nil
		}
	}

	rows, postingID := p.selectEventsCandidateRowsLocked(from, to, normalizedFilters)
	candidateRows := uint64(len(rows))
	if candidateRows > budget.MaxScannedFactRows {
		p.mu.RUnlock()
		return result, &ProjectionBudgetError{
			Dimension: projectionBudgetScannedFactRows,
			Limit:     budget.MaxScannedFactRows,
			Observed:  candidateRows,
		}
	}
	rowBytes := saturatingMulUint64(candidateRows, uint64(unsafe.Sizeof(projectionFactRowID(0))))
	activeBitsBytes := saturatingMulUint64(uint64(len(p.facts.activeBits)), uint64(unsafe.Sizeof(uint64(0))))
	scratchBytes := saturatingAddUint64(rowBytes, activeBitsBytes)
	scratchBytes = saturatingAddUint64(scratchBytes, saturatingMulUint64(candidateRows, 384))
	if scratchBytes > budget.MaxQueryScratchBytes {
		p.mu.RUnlock()
		return result, &ProjectionBudgetError{
			Dimension: projectionBudgetQueryScratchBytes,
			Limit:     budget.MaxQueryScratchBytes,
			Observed:  scratchBytes,
		}
	}

	result = projectionEventsSnapshot{
		from:                from,
		to:                  to,
		rows:                append([]projectionFactRowID(nil), rows...),
		facts:               p.facts,
		registry:            p.stringRegistry,
		filters:             normalizedFilters,
		datasetEpoch:        p.datasetEpoch,
		revision:            p.revision,
		rewriteRevision:     p.rewriteRevision,
		snapshotMaxSequence: snapshotMaxSequence,
		postingID:           postingID,
		cacheKey:            cacheKey,
		candidateRows:       candidateRows,
		scratchBytes:        scratchBytes,
	}
	result.facts.activeBits = append([]uint64(nil), p.facts.activeBits...)
	result.registry.byValue = nil
	p.mu.RUnlock()
	return result, nil
}

func (p *UsageProjection) selectEventsCandidateRowsLocked(from, to time.Time, filters map[string][]string) ([]projectionFactRowID, string) {
	type indexedFilter struct {
		name      string
		dimension projectionPostingDimension
	}
	indexed := [...]indexedFilter{
		{name: "api", dimension: postingDimensionAPI},
		{name: "model", dimension: postingDimensionModel},
		{name: "provider", dimension: postingDimensionProvider},
		{name: "auth_index", dimension: postingDimensionAuth},
		{name: "source", dimension: postingDimensionSource},
	}

	bestRows := p.postings.rows[projectionPostingKey{Dimension: postingDimensionAll}]
	bestName := "all"
	var bestValues []string
	for _, candidate := range indexed {
		values := filters[candidate.name]
		if len(values) == 0 {
			continue
		}
		rows := make([]projectionFactRowID, 0)
		for _, value := range values {
			registryID := p.stringRegistry.byValue[value]
			if registryID == 0 {
				continue
			}
			rows = append(rows, p.postings.rows[projectionPostingKey{Dimension: candidate.dimension, Value: registryID}]...)
		}
		if bestName == "all" || len(rows) < len(bestRows) {
			bestRows = rows
			bestName = candidate.name
			bestValues = values
		}
	}
	postingID := projectionEventsPostingID(bestName, bestValues)
	if from.IsZero() && to.IsZero() {
		return bestRows, postingID
	}

	timeRows := p.selectTimeRangeCandidateRowsLocked(projectionTimeRange{from: from, to: to})
	if len(timeRows) <= len(bestRows) {
		return p.filterEventRowsByFiltersLocked(timeRows, filters), postingID
	}
	return p.filterEventRowsByTimeLocked(bestRows, from, to), postingID
}

func (p *UsageProjection) filterEventRowsByFiltersLocked(rows []projectionFactRowID, filters map[string][]string) []projectionFactRowID {
	result := make([]projectionFactRowID, 0, len(rows))
	for _, rowID := range rows {
		contribution, ok := p.facts.contribution(rowID, p.stringRegistry)
		if ok && projectionEventFiltersMatch(contribution, filters) {
			result = append(result, rowID)
		}
	}
	return result
}

func (p *UsageProjection) filterEventRowsByTimeLocked(rows []projectionFactRowID, from, to time.Time) []projectionFactRowID {
	result := make([]projectionFactRowID, 0, len(rows))
	for _, rowID := range rows {
		timestamp, ok := p.facts.timestamp(rowID)
		if ok && projectionPricingTimestampMatches(timestamp, from, to) {
			result = append(result, rowID)
		}
	}
	return result
}

func projectionEventsPostingID(name string, values []string) string {
	if name == "all" {
		return "events:v1:all"
	}
	fields := make([]string, 0, len(values)+1)
	fields = append(fields, name)
	fields = append(fields, values...)
	key := newProjectionIndexKeyFromFields(0, "usage-events-posting-v1", fields...)
	return "events:v1:" + name + ":" + hex.EncodeToString(key.Digest[:])
}

func projectionEventsPageCacheKey(
	datasetEpoch, rewriteRevision, snapshotMaxSequence uint64,
	from, to time.Time,
	filters map[string][]string,
) string {
	fields := []string{
		strconv.FormatUint(datasetEpoch, 10),
		strconv.FormatUint(rewriteRevision, 10),
		strconv.FormatUint(snapshotMaxSequence, 10),
		projectionEventPageCacheTime(from),
		projectionEventPageCacheTime(to),
	}
	for _, name := range []string{"api", "model", "provider", "auth_index", "source", "failed"} {
		values := filters[name]
		fields = append(fields, name, strconv.Itoa(len(values)))
		fields = append(fields, values...)
	}
	key := newProjectionIndexKeyFromFields(0, "usage-events-page-cache-v1", fields...)
	return hex.EncodeToString(key.Digest[:])
}

func projectionEventPageCacheTime(value time.Time) string {
	if value.IsZero() {
		return "zero-time"
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func projectionEventRowBefore(facts usageFactStoreV1, left, right projectionFactRowID) bool {
	leftIndex, leftOK := facts.index(left)
	rightIndex, rightOK := facts.index(right)
	if !leftOK || !rightOK {
		return leftOK && !rightOK
	}
	leftTimestamp, _ := facts.timestamp(left)
	rightTimestamp, _ := facts.timestamp(right)
	if !leftTimestamp.Equal(rightTimestamp) {
		return leftTimestamp.After(rightTimestamp)
	}
	if facts.sequences[leftIndex] != facts.sequences[rightIndex] {
		return facts.sequences[leftIndex] > facts.sequences[rightIndex]
	}
	if facts.batchOrdinals[leftIndex] != facts.batchOrdinals[rightIndex] {
		return facts.batchOrdinals[leftIndex] > facts.batchOrdinals[rightIndex]
	}
	if facts.stableEventIDs[leftIndex] != facts.stableEventIDs[rightIndex] {
		return facts.stableEventIDs[leftIndex] > facts.stableEventIDs[rightIndex]
	}
	return left < right
}

func normalizeProjectionEventFilters(filters map[string][]string) (map[string][]string, error) {
	result := make(map[string][]string, len(filters))
	for rawName, rawValues := range filters {
		name := strings.ToLower(strings.TrimSpace(rawName))
		switch name {
		case "api", "model", "provider", "auth_index", "source", "failed":
		default:
			return nil, ErrProjectionQueryInvalid
		}
		seen := make(map[string]struct{}, len(rawValues))
		values := make([]string, 0, len(rawValues))
		for _, rawValue := range rawValues {
			value := strings.TrimSpace(rawValue)
			if name == "failed" {
				parsed, err := strconv.ParseBool(value)
				if err != nil {
					return nil, ErrProjectionQueryInvalid
				}
				value = strconv.FormatBool(parsed)
			}
			if value == "" {
				continue
			}
			if _, ok := seen[value]; ok {
				continue
			}
			seen[value] = struct{}{}
			values = append(values, value)
		}
		sort.Strings(values)
		if len(values) > 0 {
			result[name] = values
		}
	}
	return result, nil
}

func projectionEventFiltersMatch(contribution projectionContribution, filters map[string][]string) bool {
	values := map[string]string{
		"api":        contribution.API,
		"model":      contribution.Model,
		"provider":   contribution.Provider,
		"auth_index": contribution.AuthIndex,
		"source":     contribution.SourceID,
	}
	for name, actual := range values {
		accepted := filters[name]
		if len(accepted) > 0 && !projectionSortedStringsContain(accepted, actual) {
			return false
		}
	}
	if accepted := filters["failed"]; len(accepted) > 0 &&
		!projectionSortedStringsContain(accepted, strconv.FormatBool(contribution.Failed)) {
		return false
	}
	return true
}

func projectionSortedStringsContain(values []string, value string) bool {
	index := sort.SearchStrings(values, value)
	return index < len(values) && values[index] == value
}

func addProjectionSummaryPricing(target map[string]Aggregate, dimensionType, dimensionID string, contribution projectionContribution) {
	aggregate := target[dimensionID]
	addPricingContribution(&aggregate, dimensionType, dimensionID, contribution)
	target[dimensionID] = aggregate
}

func mergeProjectionSummaryPricing(target, pricing map[string]Aggregate) {
	for key, pricingAggregate := range pricing {
		aggregate := target[key]
		aggregate.PricingGroups = pricingAggregate.PricingGroups
		target[key] = aggregate
	}
}
