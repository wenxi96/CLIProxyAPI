package usage

import (
	"fmt"
	"strings"
	"time"
	"unsafe"
)

const (
	projectionStringHeaderBytes        = uint64(unsafe.Sizeof(""))
	projectionFactRowBudgetBytes       = uint64(224)
	projectionActiveEventBudgetBytes   = uint64(640)
	projectionPostingRefBudgetBytes    = uint64(8)
	projectionRollupCellBudgetBytes    = uint64(256)
	projectionRegistryEntryBudgetBytes = uint64(64)
	projectionBudgetFactRows           = "fact_rows"
	projectionBudgetPostingRefs        = "posting_refs"
	projectionBudgetRegistryEntries    = "registry_entries"
	projectionBudgetRollupCells        = "rollup_cells"
	projectionBudgetProjectionBytes    = "projection_bytes"
	projectionBudgetScannedFactRows    = "scanned_fact_rows"
	projectionBudgetQueryScratchBytes  = "query_scratch_bytes"
	projectionBudgetPricingGroups      = "pricing_groups"
)

var ErrProjectionBudgetExceeded = fmt.Errorf("%w: projection budget exceeded", ErrProjectionUnavailable)

type ProjectionBudgetError struct {
	Dimension string
	Limit     uint64
	Observed  uint64
}

func (err *ProjectionBudgetError) Error() string {
	if err == nil {
		return ErrProjectionBudgetExceeded.Error()
	}
	return fmt.Sprintf("%s: %s observed=%d limit=%d", ErrProjectionBudgetExceeded, err.Dimension, err.Observed, err.Limit)
}

func (err *ProjectionBudgetError) Unwrap() error { return ErrProjectionBudgetExceeded }

type ProjectionBudgetV2 struct {
	MaxProjectionBytes   uint64
	MaxFactRows          uint64
	MaxPostingRefs       uint64
	MaxRegistryEntries   uint64
	MaxRollupCells       uint64
	MaxQueryScratchBytes uint64
	MaxScannedFactRows   uint64
	MaxPricingGroups     uint64
}

func DefaultProjectionBudgetV2() ProjectionBudgetV2 {
	return ProjectionBudgetV2{
		MaxProjectionBytes:   768 << 20,
		MaxFactRows:          500_000,
		MaxPostingRefs:       3_000_000,
		MaxRegistryEntries:   1_000_000,
		MaxRollupCells:       2_000_000,
		MaxQueryScratchBytes: 1 << 30,
		MaxScannedFactRows:   500_000,
		MaxPricingGroups:     2_000_000,
	}
}

func (budget ProjectionBudgetV2) normalized() ProjectionBudgetV2 {
	defaults := DefaultProjectionBudgetV2()
	if budget.MaxProjectionBytes == 0 {
		budget.MaxProjectionBytes = defaults.MaxProjectionBytes
	}
	if budget.MaxFactRows == 0 {
		budget.MaxFactRows = defaults.MaxFactRows
	}
	if budget.MaxPostingRefs == 0 {
		budget.MaxPostingRefs = defaults.MaxPostingRefs
	}
	if budget.MaxRegistryEntries == 0 {
		budget.MaxRegistryEntries = defaults.MaxRegistryEntries
	}
	if budget.MaxRollupCells == 0 {
		budget.MaxRollupCells = defaults.MaxRollupCells
	}
	if budget.MaxQueryScratchBytes == 0 {
		budget.MaxQueryScratchBytes = defaults.MaxQueryScratchBytes
	}
	if budget.MaxScannedFactRows == 0 {
		budget.MaxScannedFactRows = defaults.MaxScannedFactRows
	}
	if budget.MaxPricingGroups == 0 {
		budget.MaxPricingGroups = defaults.MaxPricingGroups
	}
	return budget
}

type ProjectionStorageMetrics struct {
	FactRows                 uint64
	ActiveFactRows           uint64
	TombstonedFactRows       uint64
	PostingRefs              uint64
	RegistryEntries          uint64
	RollupCells              uint64
	RollupPricingGroups      uint64
	EstimatedProjectionBytes uint64
}

func (projection *UsageProjection) SetBudget(budget ProjectionBudgetV2) {
	if projection == nil {
		return
	}
	projection.mu.Lock()
	projection.budget = budget.normalized()
	projection.mu.Unlock()
}

func (projection *UsageProjection) Budget() ProjectionBudgetV2 {
	if projection == nil {
		return DefaultProjectionBudgetV2()
	}
	projection.mu.RLock()
	budget := projection.budget.normalized()
	projection.mu.RUnlock()
	return budget
}

func (projection *UsageProjection) StorageMetrics() ProjectionStorageMetrics {
	if projection == nil {
		return ProjectionStorageMetrics{}
	}
	projection.mu.RLock()
	defer projection.mu.RUnlock()
	return projection.storageMetricsLocked()
}

func (projection *UsageProjection) ValidateStorageBudget() error {
	if projection == nil {
		return nil
	}
	projection.mu.RLock()
	defer projection.mu.RUnlock()
	return projection.validateStorageBudgetLocked(projection.storageMetricsLocked())
}

func (projection *UsageProjection) storageMetricsLocked() ProjectionStorageMetrics {
	if projection == nil {
		return ProjectionStorageMetrics{}
	}
	metrics := ProjectionStorageMetrics{
		FactRows:            uint64(len(projection.facts.timestampSeconds)),
		ActiveFactRows:      projection.facts.activeCount,
		PostingRefs:         projection.postings.refs,
		RegistryEntries:     uint64(len(projection.stringRegistry.values)),
		RollupCells:         uint64(projectionScalarRollupCellsLocked(projection)),
		RollupPricingGroups: 0,
	}
	metrics.TombstonedFactRows = metrics.FactRows - metrics.ActiveFactRows
	metrics.EstimatedProjectionBytes = estimateProjectionBytesLocked(projection, metrics, 0)
	return metrics
}

func (projection *UsageProjection) validateStorageBudgetLocked(metrics ProjectionStorageMetrics) error {
	budget := projection.budget.normalized()
	checks := []struct {
		dimension string
		observed  uint64
		limit     uint64
	}{
		{projectionBudgetFactRows, metrics.FactRows, budget.MaxFactRows},
		{projectionBudgetPostingRefs, metrics.PostingRefs, budget.MaxPostingRefs},
		{projectionBudgetRegistryEntries, metrics.RegistryEntries, budget.MaxRegistryEntries},
		{projectionBudgetRollupCells, metrics.RollupCells, budget.MaxRollupCells},
		{projectionBudgetProjectionBytes, metrics.EstimatedProjectionBytes, budget.MaxProjectionBytes},
	}
	for _, check := range checks {
		if check.observed > check.limit {
			return &ProjectionBudgetError{Dimension: check.dimension, Limit: check.limit, Observed: check.observed}
		}
	}
	return nil
}

func (projection *UsageProjection) CheckDetailBudget(apiName string, detail RequestDetail, identity ProjectionIdentity) error {
	if projection == nil {
		return ErrProjectionUnavailable
	}
	detail = normalizeRequestDetailPreserveTimestamp(detail, detail.Provider)
	apiName = safeImportedAPIName(strings.TrimSpace(apiName), detail)
	if apiName == "" {
		apiName = "unknown"
	}
	projection.mu.RLock()
	defer projection.mu.RUnlock()

	metrics := projection.storageMetricsLocked()
	stableEventID, existingEvent := projection.budgetEventLocked(apiName, detail, identity)
	if existingEvent != nil && existingEvent.CanonicalDetailHash == canonicalDetailHash(detail) {
		return projection.validateStorageBudgetLocked(metrics)
	}

	metrics.FactRows++
	if existingEvent == nil || existingEvent.CanonicalEventIdentity != strings.TrimSpace(identity.CanonicalEventIdentity) {
		metrics.ActiveFactRows++
	}
	metrics.TombstonedFactRows = metrics.FactRows - metrics.ActiveFactRows
	metrics.PostingRefs = saturatingAddUint64(metrics.PostingRefs, 6)

	authIndex := normalizedIdentityPart(detail.AuthIndex)
	sourceID := UsageSourceIDV1(detail)
	priceKey := BuildPriceKey(detail.Provider, detail.Model)
	providerState := ProviderRawPresenceV1(detail.Provider)
	registryValues := []string{
		apiName, detail.Model, detail.Provider, authIndex, sourceID, priceKey, providerState,
		"api:" + apiName, "model:" + detail.Model, "provider:" + detail.Provider,
		"auth:" + authIndex, "source:" + sourceID,
	}
	newRegistryBytes := uint64(0)
	newRegistryValues := make(map[string]struct{}, len(registryValues))
	for _, value := range registryValues {
		if value == "" || projection.stringRegistry.byValue[value] != 0 {
			continue
		}
		if _, duplicate := newRegistryValues[value]; duplicate {
			continue
		}
		newRegistryValues[value] = struct{}{}
		newRegistryBytes = saturatingAddUint64(newRegistryBytes, uint64(len(value))+projectionStringHeaderBytes)
	}
	metrics.RegistryEntries = saturatingAddUint64(metrics.RegistryEntries, uint64(len(newRegistryValues)))

	rollupDelta := projection.prospectiveRollupCellDeltaLocked(apiName, detail.Model, detail.Provider, authIndex, sourceID, detail.Timestamp)
	metrics.RollupCells = saturatingAddUint64(metrics.RollupCells, rollupDelta)
	additionalRetainedBytes := newRegistryBytes
	additionalRetainedBytes = saturatingAddUint64(additionalRetainedBytes, uint64(len(stableEventID))+projectionStringHeaderBytes)
	canonicalIdentity := strings.TrimSpace(identity.CanonicalEventIdentity)
	if canonicalIdentity == "" {
		seed := strings.TrimSpace(identity.CanonicalIdentitySeed)
		if seed == "" {
			seed = CanonicalIdentitySeedV1(detail, projection.ordinals[SourceGroupKeyV1(detail)])
		}
		canonicalIdentity = CanonicalEventIdentityV1(seed)
	}
	additionalRetainedBytes = saturatingAddUint64(additionalRetainedBytes, uint64(len(canonicalIdentity))+projectionStringHeaderBytes)
	additionalRetainedBytes = saturatingAddUint64(additionalRetainedBytes, projection.timeIndex.prospectiveBytes(detail.Timestamp))
	metrics.EstimatedProjectionBytes = estimateProjectionBytesLocked(projection, metrics, additionalRetainedBytes)
	return projection.validateStorageBudgetLocked(metrics)
}

func (projection *UsageProjection) budgetEventLocked(apiName string, detail RequestDetail, identity ProjectionIdentity) (string, *projectionEvent) {
	stableEventID := strings.TrimSpace(identity.StableEventID)
	canonicalIdentity := strings.TrimSpace(identity.CanonicalEventIdentity)
	if stableEventID == "" && canonicalIdentity != "" {
		stableEventID = projection.canonical[newProjectionIndexKey(projectionIndexCanonical, canonicalIdentity)]
	}
	if stableEventID == "" {
		logicalIdentity := detailIdentityKey(apiName, detail.Model, detail)
		stableEventID = projection.lookup[logicalLookupIndexKey(logicalIdentity)]
		if stableEventID == "" {
			stableEventID = projection.lookup[requestLookupIndexKey(apiName, detail)]
		}
	}
	if stableEventID == "" {
		seed := strings.TrimSpace(identity.CanonicalIdentitySeed)
		if seed == "" {
			seed = CanonicalIdentitySeedV1(detail, projection.ordinals[SourceGroupKeyV1(detail)])
		}
		canonicalIdentity = CanonicalEventIdentityV1(seed)
		stableEventID = StableEventIDV1(canonicalIdentity)
	}
	event, ok := projection.events[stableEventID]
	if !ok {
		return stableEventID, nil
	}
	copyOfEvent := event
	return stableEventID, &copyOfEvent
}

func (projection *UsageProjection) prospectiveRollupCellDeltaLocked(apiName, model, provider, authIndex, sourceID string, timestamp time.Time) uint64 {
	values := [5]string{apiName, model, provider, authIndex, sourceID}
	starts := []struct {
		buckets map[string]compactHourBucket
		start   time.Time
	}{
		{projection.hours, hourStart(timestamp)},
		{projection.days, dayStart(timestamp)},
		{projection.weeks, weekStart(timestamp)},
		{projection.months, monthStart(timestamp)},
		{projection.years, yearStart(timestamp)},
	}
	delta := uint64(0)
	for _, entry := range starts {
		bucket, exists := entry.buckets[bucketKey(entry.start)]
		if !exists {
			delta++
		}
		dimensionMaps := [5]map[projectionStringID]compactAggregate{bucket.APIs, bucket.Models, bucket.Providers, bucket.Auths, bucket.Sources}
		for index, value := range values {
			if value == "" {
				continue
			}
			id := projection.stringRegistry.byValue[value]
			if id == 0 {
				delta++
				continue
			}
			if _, present := dimensionMaps[index][id]; !present {
				delta++
			}
		}
	}
	return delta
}

func (projection *UsageProjection) ValidateQueryBudget() error {
	if projection == nil {
		return ErrProjectionUnavailable
	}
	projection.mu.RLock()
	defer projection.mu.RUnlock()
	return projection.validateQueryBudgetLocked()
}

func (projection *UsageProjection) validateQueryBudgetLocked() error {
	budget := projection.budget.normalized()
	scannedRows := projection.facts.activeCount
	if scannedRows > budget.MaxScannedFactRows {
		return &ProjectionBudgetError{Dimension: projectionBudgetScannedFactRows, Limit: budget.MaxScannedFactRows, Observed: scannedRows}
	}
	pricingGroups := saturatingMulUint64(scannedRows, 31)
	if pricingGroups > budget.MaxPricingGroups {
		return &ProjectionBudgetError{Dimension: projectionBudgetPricingGroups, Limit: budget.MaxPricingGroups, Observed: pricingGroups}
	}
	scratchBytes := saturatingAddUint64(projection.storageMetricsLocked().EstimatedProjectionBytes, saturatingMulUint64(pricingGroups, 384))
	if scratchBytes > budget.MaxQueryScratchBytes {
		return &ProjectionBudgetError{Dimension: projectionBudgetQueryScratchBytes, Limit: budget.MaxQueryScratchBytes, Observed: scratchBytes}
	}
	return nil
}

func projectionScalarRollupCellsLocked(projection *UsageProjection) int {
	if projection == nil {
		return 0
	}
	return int(projection.rollupCells)
}

func estimateProjectionBytesLocked(projection *UsageProjection, metrics ProjectionStorageMetrics, additionalRetainedBytes uint64) uint64 {
	if projection == nil {
		return 0
	}
	// The projection budget covers retained Go heap, not only serialized column
	// payloads. These rounded unit costs include slice capacity, map buckets,
	// identity indexes, facet maps, and posting-list headers observed by the
	// production-equivalent capacity gate.
	bytes := saturatingMulUint64(metrics.FactRows, projectionFactRowBudgetBytes)
	bytes = saturatingAddUint64(bytes, saturatingMulUint64(metrics.ActiveFactRows, projectionActiveEventBudgetBytes))
	bytes = saturatingAddUint64(bytes, projection.facts.retainedStringBytes)
	bytes = saturatingAddUint64(bytes, additionalRetainedBytes)
	bytes = saturatingAddUint64(bytes, saturatingMulUint64(metrics.PostingRefs, projectionPostingRefBudgetBytes))
	bytes = saturatingAddUint64(bytes, saturatingMulUint64(metrics.RollupCells, projectionRollupCellBudgetBytes))
	bytes = saturatingAddUint64(bytes, projection.stringRegistry.retainedBytes)
	bytes = saturatingAddUint64(bytes, saturatingMulUint64(metrics.RegistryEntries, projectionRegistryEntryBudgetBytes))
	bytes = saturatingAddUint64(bytes, projection.timeIndex.estimatedBytes())
	return addProjectionBudgetSafetyMargin(bytes)
}

func addProjectionBudgetSafetyMargin(value uint64) uint64 {
	margin := value / 8
	if value%8 != 0 {
		margin++
	}
	return saturatingAddUint64(value, margin)
}

func saturatingAddUint64(left, right uint64) uint64 {
	result := left + right
	if result < left {
		return ^uint64(0)
	}
	return result
}

func saturatingMulUint64(left, right uint64) uint64 {
	if left == 0 || right == 0 {
		return 0
	}
	if left > ^uint64(0)/right {
		return ^uint64(0)
	}
	return left * right
}
