package usage

import (
	"sort"
	"time"
	"unsafe"
)

const projectionSeriesPointLimit = 366

type projectionSummaryViewSnapshot struct {
	from            time.Time
	to              time.Time
	observationTo   time.Time
	rows            []projectionFactRowID
	pricingRows     []projectionFactRowID
	rollupIntervals []projectionSummaryRollupInterval
	facts           usageFactStoreV1
	registry        projectionStringRegistry
	datasetEpoch    uint64
	revision        uint64
	rewriteRevision uint64
}

type projectionSummaryRollupInterval struct {
	start  time.Time
	end    time.Time
	bucket compactHourBucket
}

type projectionSeriesInterval struct {
	start    time.Time
	end      time.Time
	complete bool
}

// QuerySummaryView builds exact summary, series, and health facts from one
// compact-fact generation. Canonical request details are never read.
func (p *UsageProjection) QuerySummaryView(from, to, observationTo time.Time) (ProjectionSummaryView, error) {
	snapshot, err := p.captureSummaryViewSnapshot(from, to, observationTo)
	if err != nil {
		return ProjectionSummaryView{}, err
	}

	seriesFrom := snapshot.from
	if seriesFrom.IsZero() {
		for _, rowID := range snapshot.pricingRows {
			if !snapshot.facts.isActive(rowID) {
				continue
			}
			contribution, ok := snapshot.facts.contribution(rowID, snapshot.registry)
			if !ok || contribution.Timestamp.IsZero() ||
				(!snapshot.to.IsZero() && !contribution.Timestamp.Before(snapshot.to)) {
				continue
			}
			if seriesFrom.IsZero() || contribution.Timestamp.Before(seriesFrom) {
				seriesFrom = contribution.Timestamp
			}
		}
		if seriesFrom.IsZero() {
			seriesFrom = snapshot.to
		}
	}
	granularity, intervals, seriesAvailable := selectProjectionSeriesIntervals(seriesFrom, snapshot.to)

	summaryBucket := newCompactHourBucket(snapshot.from, snapshot.to)
	pricing := projectionSummaryPricing{
		apis:      make(map[string]Aggregate),
		models:    make(map[string]Aggregate),
		providers: make(map[string]Aggregate),
		auths:     make(map[string]Aggregate),
		sources:   make(map[string]Aggregate),
	}
	seriesAggregates := make([]compactAggregate, len(intervals))
	for _, interval := range snapshot.rollupIntervals {
		addCompactHourBucket(&summaryBucket, interval.bucket)
		for index, seriesInterval := range intervals {
			if !interval.start.Before(seriesInterval.start) && !interval.end.After(seriesInterval.end) {
				addCompactAggregate(&seriesAggregates[index], interval.bucket.Totals)
				break
			}
		}
	}

	alignedTo := snapshot.observationTo.UTC().Truncate(healthBucketDuration)
	healthFrom := alignedTo.Add(-7 * 24 * time.Hour)
	healthAggregates := make([]compactAggregate, 7*24*4)
	var healthPartial compactAggregate

	for _, rowID := range snapshot.rows {
		if !snapshot.facts.isActive(rowID) {
			continue
		}
		contribution, ok := snapshot.facts.contribution(rowID, snapshot.registry)
		if !ok {
			continue
		}
		if !contribution.Timestamp.Before(healthFrom) && contribution.Timestamp.Before(alignedTo) {
			index := int(contribution.Timestamp.Sub(healthFrom) / healthBucketDuration)
			if index >= 0 && index < len(healthAggregates) {
				applyAggregateDelta(&healthAggregates[index], contribution, 1)
			}
		}
		if !contribution.Timestamp.Before(alignedTo) && contribution.Timestamp.Before(snapshot.observationTo) && alignedTo.Before(snapshot.observationTo) {
			applyAggregateDelta(&healthPartial, contribution, 1)
		}
	}

	for _, rowID := range snapshot.pricingRows {
		if !snapshot.facts.isActive(rowID) {
			continue
		}
		contribution, ok := snapshot.facts.contribution(rowID, snapshot.registry)
		if !ok {
			continue
		}
		if projectionPricingTimestampMatches(contribution.Timestamp, snapshot.from, snapshot.to) {
			if !projectionSummaryTimestampCoveredByHourRollup(contribution.Timestamp, snapshot.rollupIntervals) {
				summaryBucket.applyContribution(contribution, 1)
			}
			addPricingContribution(&pricing.totals, "global", "", contribution)
			addProjectionSummaryPricing(pricing.apis, "api", contribution.API, contribution)
			addProjectionSummaryPricing(pricing.models, "model", contribution.Model, contribution)
			addProjectionSummaryPricing(pricing.providers, "provider", contribution.Provider, contribution)
			addProjectionSummaryPricing(pricing.auths, "auth", contribution.AuthIndex, contribution)
			addProjectionSummaryPricing(pricing.sources, "source", contribution.SourceID, contribution)
			if len(intervals) > 0 && !projectionSummaryTimestampCoveredByHourRollup(contribution.Timestamp, snapshot.rollupIntervals) {
				index := sort.Search(len(intervals), func(index int) bool {
					return contribution.Timestamp.Before(intervals[index].end)
				})
				if index < len(intervals) && !contribution.Timestamp.Before(intervals[index].start) {
					applyAggregateDelta(&seriesAggregates[index], contribution, 1)
				}
			}
		}

	}
	materializer := &UsageProjection{stringRegistry: snapshot.registry}
	bucket := materializer.materializeHourBucket(summaryBucket)
	summary := ProjectionSnapshot{
		SchemaVersion:   projectionSchemaVersion,
		DatasetEpoch:    snapshot.datasetEpoch,
		Revision:        snapshot.revision,
		RewriteRevision: snapshot.rewriteRevision,
		Totals:          bucket.Totals,
		APIs:            bucket.APIs,
		Models:          bucket.Models,
		Providers:       bucket.Providers,
		Auths:           bucket.Auths,
		Sources:         bucket.Sources,
		Facets:          bucket.Totals.Facets,
	}
	summary.Totals.PricingGroups = pricing.totals.PricingGroups
	mergeProjectionSummaryPricing(summary.APIs, pricing.apis)
	mergeProjectionSummaryPricing(summary.Models, pricing.models)
	mergeProjectionSummaryPricing(summary.Providers, pricing.providers)
	mergeProjectionSummaryPricing(summary.Auths, pricing.auths)
	mergeProjectionSummaryPricing(summary.Sources, pricing.sources)

	view := ProjectionSummaryView{
		Summary:             summary,
		RangeFrom:           seriesFrom,
		RangeTo:             snapshot.to,
		SeriesGranularity:   granularity,
		SeriesAvailability:  "available",
		Series:              make([]ProjectionSeriesPoint, 0, len(intervals)),
		Health:              make([]ProjectionHealthPoint, 0, len(healthAggregates)),
		HealthObservationTo: snapshot.observationTo,
		HealthAlignedTo:     alignedTo,
		HealthFrom:          healthFrom,
	}
	if !seriesAvailable {
		view.SeriesAvailability = "unavailable"
		view.SeriesError = "series_point_limit_exceeded"
		view.Series = []ProjectionSeriesPoint{}
	} else {
		for index, interval := range intervals {
			view.Series = append(view.Series, ProjectionSeriesPoint{
				IntervalStart: interval.start,
				IntervalEnd:   interval.end,
				Complete:      interval.complete,
				Totals:        materializer.materializeAggregate(seriesAggregates[index], nil),
			})
		}
	}
	for index, aggregate := range healthAggregates {
		start := healthFrom.Add(time.Duration(index) * healthBucketDuration)
		view.Health = append(view.Health, projectionHealthPointFromAggregate(materializer, start, start.Add(healthBucketDuration), true, aggregate))
	}
	if alignedTo.Before(snapshot.observationTo) && !compactAggregateIsZero(healthPartial) {
		partial := projectionHealthPointFromAggregate(materializer, alignedTo, snapshot.observationTo, false, healthPartial)
		view.HealthPartialTail = &partial
	}
	return view, nil
}

func (p *UsageProjection) captureSummaryViewSnapshot(from, to, observationTo time.Time) (projectionSummaryViewSnapshot, error) {
	result := projectionSummaryViewSnapshot{}
	if p == nil {
		return result, ErrProjectionUnavailable
	}
	if !from.IsZero() {
		from = from.UTC()
	}
	if !to.IsZero() {
		to = to.UTC()
	}
	if !observationTo.IsZero() {
		observationTo = observationTo.UTC()
	}
	if !from.IsZero() && !to.IsZero() && !from.Before(to) {
		return result, ErrProjectionQueryInvalid
	}
	if to.IsZero() {
		return result, ErrProjectionQueryInvalid
	}
	if observationTo.IsZero() {
		observationTo = to
	}

	p.mu.RLock()
	if err := p.validateStorageBudgetLocked(p.storageMetricsLocked()); err != nil {
		p.mu.RUnlock()
		return result, err
	}
	alignedTo := observationTo.UTC().Truncate(healthBucketDuration)
	rollupIntervals, boundaryRanges := projectionSummaryHourRollupPlan(from, to)
	for index := range rollupIntervals {
		rollupIntervals[index].bucket = p.hours[bucketKey(rollupIntervals[index].start)]
	}
	boundaryRanges = append(boundaryRanges, projectionTimeRange{from: alignedTo.Add(-7 * 24 * time.Hour), to: observationTo})
	rows := p.selectTimeRangeCandidateRowsLocked(boundaryRanges...)
	pricingRows := p.selectTimeRangeCandidateRowsLocked(projectionTimeRange{from: from, to: to})
	candidateRows := uint64(len(rows))
	budget := p.budget.normalized()
	if candidateRows > budget.MaxScannedFactRows {
		p.mu.RUnlock()
		return result, &ProjectionBudgetError{Dimension: projectionBudgetScannedFactRows, Limit: budget.MaxScannedFactRows, Observed: candidateRows}
	}
	pricingCandidateRows := uint64(len(pricingRows))
	if pricingCandidateRows > budget.MaxScannedFactRows {
		p.mu.RUnlock()
		return result, &ProjectionBudgetError{Dimension: projectionBudgetScannedFactRows, Limit: budget.MaxScannedFactRows, Observed: pricingCandidateRows}
	}
	worstPricingGroups := saturatingMulUint64(pricingCandidateRows, 6)
	if worstPricingGroups > budget.MaxPricingGroups {
		p.mu.RUnlock()
		return result, &ProjectionBudgetError{Dimension: projectionBudgetPricingGroups, Limit: budget.MaxPricingGroups, Observed: worstPricingGroups}
	}
	rowBytes := saturatingMulUint64(candidateRows, uint64(unsafe.Sizeof(projectionFactRowID(0))))
	rowBytes = saturatingAddUint64(rowBytes, saturatingMulUint64(pricingCandidateRows, uint64(unsafe.Sizeof(projectionFactRowID(0)))))
	activeBitsBytes := saturatingMulUint64(uint64(len(p.facts.activeBits)), uint64(unsafe.Sizeof(uint64(0))))
	scratchBytes := saturatingAddUint64(rowBytes, activeBitsBytes)
	scratchBytes = saturatingAddUint64(scratchBytes, saturatingMulUint64(worstPricingGroups, 384))
	if scratchBytes > budget.MaxQueryScratchBytes {
		p.mu.RUnlock()
		return result, &ProjectionBudgetError{Dimension: projectionBudgetQueryScratchBytes, Limit: budget.MaxQueryScratchBytes, Observed: scratchBytes}
	}
	result = projectionSummaryViewSnapshot{
		from: from, to: to, observationTo: observationTo,
		rows: append([]projectionFactRowID(nil), rows...), pricingRows: append([]projectionFactRowID(nil), pricingRows...),
		rollupIntervals: rollupIntervals, facts: p.facts, registry: p.stringRegistry,
		datasetEpoch: p.datasetEpoch, revision: p.revision, rewriteRevision: p.rewriteRevision,
	}
	result.facts.activeBits = append([]uint64(nil), p.facts.activeBits...)
	result.registry.byValue = nil
	p.mu.RUnlock()
	return result, nil
}

func addCompactAggregate(target *compactAggregate, value compactAggregate) {
	if target == nil {
		return
	}
	for index := 0; index < compactAggregateCounterCount; index++ {
		target.counters.add(index, value.counters.value(index))
	}
}

func addCompactHourBucket(target *compactHourBucket, value compactHourBucket) {
	if target == nil {
		return
	}
	addCompactAggregate(&target.Totals, value.Totals)
	addProjectionFacetCounts(&target.Facets, value.Facets)
	addCompactAggregateMap(&target.APIs, value.APIs)
	addCompactAggregateMap(&target.Models, value.Models)
	addCompactAggregateMap(&target.Providers, value.Providers)
	addCompactAggregateMap(&target.Auths, value.Auths)
	addCompactAggregateMap(&target.Sources, value.Sources)
}

func addProjectionFacetCounts(target *map[projectionStringID]int64, values map[projectionStringID]int64) {
	if len(values) == 0 {
		return
	}
	if *target == nil {
		*target = make(map[projectionStringID]int64, len(values))
	}
	for id, count := range values {
		(*target)[id] += count
	}
}

func addCompactAggregateMap(target *map[projectionStringID]compactAggregate, values map[projectionStringID]compactAggregate) {
	if len(values) == 0 {
		return
	}
	if *target == nil {
		*target = make(map[projectionStringID]compactAggregate, len(values))
	}
	for id, value := range values {
		aggregate := (*target)[id]
		addCompactAggregate(&aggregate, value)
		(*target)[id] = aggregate
	}
}

func projectionSummaryTimestampCoveredByHourRollup(timestamp time.Time, intervals []projectionSummaryRollupInterval) bool {
	if len(intervals) == 0 || timestamp.IsZero() {
		return false
	}
	timestamp = timestamp.UTC()
	return !timestamp.Before(intervals[0].start) && timestamp.Before(intervals[len(intervals)-1].end)
}

func projectionSummaryHourRollupPlan(from, to time.Time) ([]projectionSummaryRollupInterval, []projectionTimeRange) {
	if from.IsZero() || to.IsZero() || !from.Before(to) {
		return nil, []projectionTimeRange{{from: from, to: to}}
	}
	firstFullHour := hourStart(from)
	if !from.Equal(firstFullHour) {
		firstFullHour = firstFullHour.Add(time.Hour)
	}
	lastFullHour := hourStart(to)
	rollups := make([]projectionSummaryRollupInterval, 0)
	for start := firstFullHour; start.Before(lastFullHour); start = start.Add(time.Hour) {
		end := start.Add(time.Hour)
		rollups = append(rollups, projectionSummaryRollupInterval{start: start, end: end})
	}
	boundaries := make([]projectionTimeRange, 0, 2)
	if from.Before(firstFullHour) {
		boundaries = append(boundaries, projectionTimeRange{from: from, to: firstFullHour})
	}
	if lastFullHour.Before(to) {
		boundaries = append(boundaries, projectionTimeRange{from: lastFullHour, to: to})
	}
	return rollups, boundaries
}

func selectProjectionSeriesIntervals(from, to time.Time) (string, []projectionSeriesInterval, bool) {
	if from.IsZero() || to.IsZero() || !from.Before(to) {
		return "hour", []projectionSeriesInterval{}, true
	}
	for _, granularity := range []string{"hour", "day", "week", "month", "year"} {
		intervals, withinLimit := projectionSeriesIntervals(from, to, granularity, projectionSeriesPointLimit)
		if withinLimit {
			return granularity, intervals, true
		}
	}
	return "year", []projectionSeriesInterval{}, false
}

func projectionSeriesIntervals(from, to time.Time, granularity string, limit int) ([]projectionSeriesInterval, bool) {
	start, _ := rollupWindow(from, granularity)
	result := make([]projectionSeriesInterval, 0)
	for start.Before(to) {
		bucketStart, bucketEnd := rollupWindow(start, granularity)
		pointStart := bucketStart
		if pointStart.Before(from) {
			pointStart = from
		}
		pointEnd := bucketEnd
		if pointEnd.After(to) {
			pointEnd = to
		}
		if pointStart.Before(pointEnd) {
			result = append(result, projectionSeriesInterval{
				start: pointStart, end: pointEnd,
				complete: pointStart.Equal(bucketStart) && pointEnd.Equal(bucketEnd),
			})
			if len(result) > limit {
				return nil, false
			}
		}
		if !bucketEnd.After(start) {
			return nil, false
		}
		start = bucketEnd
	}
	return result, true
}

func projectionHealthPointFromAggregate(materializer *UsageProjection, start, end time.Time, complete bool, aggregate compactAggregate) ProjectionHealthPoint {
	materialized := materializer.materializeAggregate(aggregate, nil)
	return ProjectionHealthPoint{
		IntervalStart: start,
		IntervalEnd:   end,
		Complete:      complete,
		TotalRequests: materialized.TotalRequests,
		SuccessCount:  materialized.SuccessCount,
		FailureCount:  materialized.FailureCount,
		LatencyMsSum:  materialized.LatencyMsSum,
		Tokens:        materialized.Tokens,
	}
}
