package usage

import (
	"errors"
	"strings"
	"time"
	"unsafe"
)

var ErrProjectionQueryInvalid = errors.New("usage projection query invalid")

type ProjectionPricingQuery struct {
	From          time.Time
	To            time.Time
	DimensionType string
	DimensionIDs  []string
}

type ProjectionPricingResult struct {
	PricingGroups   map[string]PricingAggregate
	ScannedFactRows uint64
	ScratchBytes    uint64
}

type projectionPricingSnapshot struct {
	dimensionType string
	from          time.Time
	to            time.Time
	rows          []projectionFactRowID
	facts         usageFactStoreV1
	registry      projectionStringRegistry
	budget        ProjectionBudgetV2
	scratchBytes  uint64
}

// ResolvePricing materializes pricing groups from compact fact rows selected
// by one posting dimension and an exact half-open time range. It never reads
// canonical RequestDetail values.
func (projection *UsageProjection) ResolvePricing(query ProjectionPricingQuery) (ProjectionPricingResult, error) {
	if projection == nil {
		return ProjectionPricingResult{}, ErrProjectionUnavailable
	}
	snapshot, err := projection.capturePricingSnapshot(query)
	if err != nil {
		return ProjectionPricingResult{}, err
	}
	return snapshot.resolve()
}

func (projection *UsageProjection) capturePricingSnapshot(query ProjectionPricingQuery) (projectionPricingSnapshot, error) {
	snapshot := projectionPricingSnapshot{}
	query.DimensionType = strings.ToLower(strings.TrimSpace(query.DimensionType))
	if query.DimensionType == "" {
		query.DimensionType = "global"
	}
	if query.DimensionType != "global" && query.DimensionType != "api" && query.DimensionType != "model" &&
		query.DimensionType != "provider" && query.DimensionType != "auth" && query.DimensionType != "source" {
		return snapshot, ErrProjectionQueryInvalid
	}
	if query.DimensionType == "global" && len(query.DimensionIDs) > 0 {
		return snapshot, ErrProjectionQueryInvalid
	}
	if !query.From.IsZero() {
		query.From = query.From.UTC()
	}
	if !query.To.IsZero() {
		query.To = query.To.UTC()
	}
	if !query.From.IsZero() && !query.To.IsZero() && !query.From.Before(query.To) {
		return snapshot, ErrProjectionQueryInvalid
	}

	projection.mu.RLock()
	if err := projection.validateStorageBudgetLocked(projection.storageMetricsLocked()); err != nil {
		projection.mu.RUnlock()
		return snapshot, err
	}

	candidateRows := projection.pricingCandidateRowCountLocked(query)
	budget := projection.budget.normalized()
	if candidateRows > budget.MaxScannedFactRows {
		projection.mu.RUnlock()
		return snapshot, &ProjectionBudgetError{Dimension: projectionBudgetScannedFactRows, Limit: budget.MaxScannedFactRows, Observed: candidateRows}
	}
	worstPricingGroups := candidateRows
	if worstPricingGroups > budget.MaxPricingGroups {
		projection.mu.RUnlock()
		return snapshot, &ProjectionBudgetError{Dimension: projectionBudgetPricingGroups, Limit: budget.MaxPricingGroups, Observed: worstPricingGroups}
	}
	rowBytes := saturatingMulUint64(candidateRows, uint64(unsafe.Sizeof(projectionFactRowID(0))))
	activeBitsBytes := saturatingMulUint64(uint64(len(projection.facts.activeBits)), uint64(unsafe.Sizeof(uint64(0))))
	scratchBytes := saturatingAddUint64(rowBytes, activeBitsBytes)
	scratchBytes = saturatingAddUint64(scratchBytes, saturatingMulUint64(worstPricingGroups, 384))
	if scratchBytes > budget.MaxQueryScratchBytes {
		projection.mu.RUnlock()
		return snapshot, &ProjectionBudgetError{Dimension: projectionBudgetQueryScratchBytes, Limit: budget.MaxQueryScratchBytes, Observed: scratchBytes}
	}

	snapshot.dimensionType = query.DimensionType
	snapshot.from = query.From
	snapshot.to = query.To
	snapshot.rows = make([]projectionFactRowID, 0, int(candidateRows))
	if query.DimensionType == "global" {
		snapshot.rows = append(snapshot.rows, projection.postings.rows[projectionPostingKey{Dimension: postingDimensionAll}]...)
	} else {
		postingDimension := projectionPricingPostingDimension(query.DimensionType)
		for index, dimensionID := range query.DimensionIDs {
			dimensionID = strings.TrimSpace(dimensionID)
			if dimensionID == "" || projectionPricingDimensionIDSeenEarlier(query.DimensionIDs, index, dimensionID) {
				continue
			}
			registryID := projection.stringRegistry.byValue[dimensionID]
			if registryID == 0 {
				continue
			}
			snapshot.rows = append(snapshot.rows, projection.postings.rows[projectionPostingKey{Dimension: postingDimension, Value: registryID}]...)
		}
	}

	// Fact columns are append-only within a generation, so copied slice headers
	// safely pin the existing row prefix. Active bits are the only in-place
	// mutable column and must be copied to preserve one query generation.
	snapshot.facts = projection.facts
	snapshot.facts.activeBits = append([]uint64(nil), projection.facts.activeBits...)
	snapshot.registry = projection.stringRegistry
	snapshot.registry.byValue = nil
	snapshot.budget = budget
	snapshot.scratchBytes = scratchBytes
	projection.mu.RUnlock()
	return snapshot, nil
}

func (snapshot projectionPricingSnapshot) resolve() (ProjectionPricingResult, error) {
	result := ProjectionPricingResult{
		PricingGroups: make(map[string]PricingAggregate),
		ScratchBytes:  snapshot.scratchBytes,
	}
	for _, rowID := range snapshot.rows {
		result.ScannedFactRows++
		if !snapshot.facts.isActive(rowID) {
			continue
		}
		contribution, ok := snapshot.facts.contribution(rowID, snapshot.registry)
		if !ok || !projectionPricingTimestampMatches(contribution.Timestamp, snapshot.from, snapshot.to) {
			continue
		}
		dimensionID := projectionPricingDimensionValue(snapshot.dimensionType, contribution)
		key := pricingGroupKeyV1(snapshot.dimensionType, dimensionID, contribution.PriceKey, contribution.ProviderState)
		if _, exists := result.PricingGroups[key]; !exists && uint64(len(result.PricingGroups)) >= snapshot.budget.MaxPricingGroups {
			return ProjectionPricingResult{}, &ProjectionBudgetError{
				Dimension: projectionBudgetPricingGroups,
				Limit:     snapshot.budget.MaxPricingGroups,
				Observed:  uint64(len(result.PricingGroups)) + 1,
			}
		}
		aggregate := Aggregate{PricingGroups: result.PricingGroups}
		addPricingContribution(&aggregate, snapshot.dimensionType, dimensionID, contribution)
		result.PricingGroups = aggregate.PricingGroups
	}
	return result, nil
}

func (projection *UsageProjection) pricingCandidateRowCountLocked(query ProjectionPricingQuery) uint64 {
	if query.DimensionType == "global" {
		return uint64(len(projection.postings.rows[projectionPostingKey{Dimension: postingDimensionAll}]))
	}
	dimension := projectionPricingPostingDimension(query.DimensionType)
	count := uint64(0)
	for index, dimensionID := range query.DimensionIDs {
		dimensionID = strings.TrimSpace(dimensionID)
		if dimensionID == "" || projectionPricingDimensionIDSeenEarlier(query.DimensionIDs, index, dimensionID) {
			continue
		}
		registryID := projection.stringRegistry.byValue[dimensionID]
		if registryID == 0 {
			continue
		}
		count = saturatingAddUint64(count, uint64(len(projection.postings.rows[projectionPostingKey{Dimension: dimension, Value: registryID}])))
	}
	return count
}

func projectionPricingDimensionIDSeenEarlier(values []string, index int, normalized string) bool {
	for previous := 0; previous < index; previous++ {
		if strings.TrimSpace(values[previous]) == normalized {
			return true
		}
	}
	return false
}

func projectionPricingPostingDimension(dimensionType string) projectionPostingDimension {
	switch dimensionType {
	case "api":
		return postingDimensionAPI
	case "model":
		return postingDimensionModel
	case "provider":
		return postingDimensionProvider
	case "auth":
		return postingDimensionAuth
	case "source":
		return postingDimensionSource
	default:
		return postingDimensionAll
	}
}

func projectionPricingDimensionValue(dimensionType string, contribution projectionContribution) string {
	switch dimensionType {
	case "api":
		return contribution.API
	case "model":
		return contribution.Model
	case "provider":
		return contribution.Provider
	case "auth":
		return contribution.AuthIndex
	case "source":
		return contribution.SourceID
	default:
		return ""
	}
}

func projectionPricingTimestampMatches(timestamp, from, to time.Time) bool {
	if !from.IsZero() && timestamp.Before(from) {
		return false
	}
	return to.IsZero() || timestamp.Before(to)
}

func (projection *UsageProjection) populateSnapshotPricing(snapshot *ProjectionSnapshot) {
	if projection == nil || snapshot == nil {
		return
	}
	projection.facts.eachActive(func(rowID projectionFactRowID) {
		contribution, ok := projection.facts.contribution(rowID, projection.stringRegistry)
		if !ok {
			return
		}
		addPricingContribution(&snapshot.Totals, "global", "", contribution)
		applyPricingToSnapshotBucket(snapshot.Hours, bucketKey(hourStart(contribution.Timestamp)), contribution)
		applyPricingToSnapshotBucket(snapshot.Days, bucketKey(dayStart(contribution.Timestamp)), contribution)
		applyPricingToSnapshotBucket(snapshot.Weeks, bucketKey(weekStart(contribution.Timestamp)), contribution)
		applyPricingToSnapshotBucket(snapshot.Months, bucketKey(monthStart(contribution.Timestamp)), contribution)
		applyPricingToSnapshotBucket(snapshot.Years, bucketKey(yearStart(contribution.Timestamp)), contribution)
	})
}

func applyPricingToSnapshotBucket(buckets map[string]HourBucket, key string, contribution projectionContribution) {
	bucket, ok := buckets[key]
	if !ok {
		return
	}
	addPricingContribution(&bucket.Totals, "global", "", contribution)
	applyPricingToDimension(bucket.APIs, contribution.API, "api", contribution)
	applyPricingToDimension(bucket.Models, contribution.Model, "model", contribution)
	applyPricingToDimension(bucket.Providers, contribution.Provider, "provider", contribution)
	applyPricingToDimension(bucket.Auths, contribution.AuthIndex, "auth", contribution)
	applyPricingToDimension(bucket.Sources, contribution.SourceID, "source", contribution)
	buckets[key] = bucket
}

func applyPricingToDimension(values map[string]Aggregate, id, dimensionType string, contribution projectionContribution) {
	if id == "" {
		return
	}
	aggregate, ok := values[id]
	if !ok {
		return
	}
	addPricingContribution(&aggregate, dimensionType, id, contribution)
	values[id] = aggregate
}

func addPricingContribution(aggregate *Aggregate, dimensionType, dimensionID string, contribution projectionContribution) {
	if aggregate == nil {
		return
	}
	if aggregate.PricingGroups == nil {
		aggregate.PricingGroups = make(map[string]PricingAggregate)
	}
	key := pricingGroupKeyV1(dimensionType, dimensionID, contribution.PriceKey, contribution.ProviderState)
	pricing := aggregate.PricingGroups[key]
	if pricing.BillablePolicyVersion == "" {
		pricing.BillablePolicyVersion = BillablePolicyVersionV1
		pricing.Provider = contribution.Provider
		pricing.Model = contribution.Model
		pricing.PriceKey = contribution.PriceKey
		pricing.ProviderRawPresence = contribution.ProviderState
		pricing.ComponentMaskCounts = make(map[ComponentMask]int64)
	}
	pricing.BillableTokens.InputTokens += contribution.Billable.InputTokens
	pricing.BillableTokens.OutputTokens += contribution.Billable.OutputTokens
	pricing.BillableTokens.ReasoningTokens += contribution.Billable.ReasoningTokens
	pricing.BillableTokens.CacheReadTokens += contribution.Billable.CacheReadTokens
	pricing.BillableTokens.CacheCreationTokens += contribution.Billable.CacheCreationTokens
	pricing.BillableTokens.UnclassifiedCacheTokens += contribution.Billable.UnclassifiedCacheTokens
	pricing.ComponentMaskCounts[contribution.ComponentMask]++
	switch contribution.Classification {
	case DetailClassPriceable:
		pricing.PriceableDetailCount++
	case DetailClassUnknownUsage:
		pricing.UnknownUsageDetailCount++
	case DetailClassKnownTotalOnly:
		pricing.KnownTotalOnlyDetailCount++
	case DetailClassZeroBillableComplete:
		pricing.ZeroBillableCompleteDetailCount++
	}
	if contribution.Billable.UnclassifiedCacheTokens > 0 {
		pricing.UnclassifiedCacheDetailCount++
	}
	aggregate.PricingGroups[key] = pricing
}
