package usage

import (
	"bytes"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	projectionFactRowMinimumEncodedBytes       = uint64(218)
	projectionPostingKeyMinimumBytes           = uint64(13)
	projectionFacetEntryMinimumBytes           = uint64(12)
	projectionAggregateEntryMinimumBytes       = uint64(4 + compactAggregateCounterCount*8)
	projectionBucketMinimumEncodedBytes        = uint64(8 + 16 + compactAggregateCounterCount*8 + 6*8)
	projectionHealthMinimumEncodedBytes        = uint64(8 + 6*8)
	projectionCatalogEntryMinimumBytes         = uint64(6 * 8)
	projectionFactAuthStringIDColumn           = 3
	projectionAllPostingDimensions       uint8 = (1 << 6) - 1
	projectionAllComponentMask           uint8 = uint8(ComponentInput | ComponentOutput | ComponentReasoning | ComponentCacheRead | ComponentCacheCreation | ComponentUnclassifiedCache)
)

// hydrateFromGenerationV2 directly populates this UsageProjection from the
// checksum-validated v2 facts/postings/scalar-rollup sections plus the
// identity sidecar and the canonical snapshot. A normal v2 restart uses it
// instead of replaying canonical details through ApplyDetailWithIdentity, so
// restart cost is bounded by the persisted section sizes rather than
// O(history) per-detail contribution building. The caller must pass a fresh
// UsageProjection (NewUsageProjection).
func (p *UsageProjection) hydrateFromGenerationV2(state projectionGenerationState, sections projectionV2Sections, sidecar IdentitySidecar, snapshot StatisticsSnapshot) error {
	if p == nil {
		return fmt.Errorf("%w: hydrate nil projection", ErrProjectionRestoreUnavailable)
	}
	if !sidecar.valid() {
		return fmt.Errorf("%w: hydrate invalid identity sidecar", ErrProjectionRestoreUnavailable)
	}
	if len(sections.Facts) == 0 || len(sections.Postings) == 0 || len(sections.ScalarRollups) == 0 {
		return fmt.Errorf("%w: hydrate incomplete v2 projection sections", ErrProjectionRestoreUnavailable)
	}
	if err := validateCanonicalSnapshotTotals(snapshot); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	if err := p.validatePersistedMetricsBudgetLocked(state.StorageMetrics); err != nil {
		return err
	}
	if err := decodeProjectionSectionV2("facts", sections.Facts, p.unmarshalFactsV2Locked); err != nil {
		return err
	}
	p.rebuildTimeIndexLocked()
	if err := decodeProjectionSectionV2("postings", sections.Postings, p.unmarshalPostingsV2Locked); err != nil {
		return err
	}
	if err := decodeProjectionSectionV2("scalar rollups", sections.ScalarRollups, p.unmarshalScalarRollupsV2Locked); err != nil {
		return err
	}

	p.datasetEpoch = state.DatasetEpoch
	p.revision = state.Revision
	p.rewriteRevision = state.RewriteRevision
	p.rollupCells = hydratedRollupCellCountLocked(p)

	if err := p.rebuildIdentityIndexesLocked(sidecar, snapshot); err != nil {
		return err
	}
	p.nextSequence = sidecar.NextSequence
	for key, ordinal := range sidecar.SourceOrdinals {
		p.ordinals[key] = ordinal
	}

	if err := p.validateHydratedStateLocked(state.StorageMetrics, sidecar, snapshot); err != nil {
		return err
	}
	return nil
}

func decodeProjectionSectionV2(name string, data []byte, decode func(*bytes.Reader) error) error {
	reader := bytes.NewReader(data)
	if err := decode(reader); err != nil {
		return err
	}
	if reader.Len() != 0 {
		return fmt.Errorf("%w: trailing %s section bytes", ErrProjectionRestoreUnavailable, name)
	}
	return nil
}

func (p *UsageProjection) validatePersistedMetricsBudgetLocked(metrics ProjectionStorageMetrics) error {
	budget := p.budget.normalized()
	checks := []struct {
		name  string
		value uint64
		limit uint64
	}{
		{name: projectionBudgetFactRows, value: metrics.FactRows, limit: budget.MaxFactRows},
		{name: projectionBudgetPostingRefs, value: metrics.PostingRefs, limit: budget.MaxPostingRefs},
		{name: projectionBudgetRegistryEntries, value: metrics.RegistryEntries, limit: budget.MaxRegistryEntries},
		{name: projectionBudgetRollupCells, value: metrics.RollupCells, limit: budget.MaxRollupCells},
		{name: projectionBudgetProjectionBytes, value: metrics.EstimatedProjectionBytes, limit: budget.MaxProjectionBytes},
	}
	for _, check := range checks {
		if check.value > check.limit {
			return fmt.Errorf("%w: persisted %s %d exceeds limit %d", ErrProjectionRestoreUnavailable, check.name, check.value, check.limit)
		}
	}
	return nil
}

func (p *UsageProjection) unmarshalFactsV2Locked(reader *bytes.Reader) error {
	valueCount, err := readProjectionUint64(reader)
	if err != nil {
		return fmt.Errorf("%w: decode facts registry count: %v", ErrProjectionRestoreUnavailable, err)
	}
	valueCapacity, err := boundedProjectionCount(reader, valueCount, 8, p.budget.normalized().MaxRegistryEntries, "facts registry")
	if err != nil {
		return err
	}
	registry := newProjectionStringRegistry()
	registry.values = make([]string, 0, valueCapacity)
	registry.byValue = make(map[string]projectionStringID, valueCapacity)
	for index := uint64(0); index < valueCount; index++ {
		value, errValue := readProjectionString(reader)
		if errValue != nil {
			return fmt.Errorf("%w: decode facts registry value: %v", ErrProjectionRestoreUnavailable, errValue)
		}
		if value == "" {
			return fmt.Errorf("%w: empty facts registry value at %d", ErrProjectionRestoreUnavailable, index)
		}
		if _, duplicate := registry.byValue[value]; duplicate {
			return fmt.Errorf("%w: duplicate facts registry value at %d", ErrProjectionRestoreUnavailable, index)
		}
		id := projectionStringID(len(registry.values) + 1)
		registry.values = append(registry.values, value)
		registry.byValue[value] = id
		registry.retainedBytes = saturatingAddUint64(registry.retainedBytes, uint64(len(value))+projectionStringHeaderBytes)
	}
	p.stringRegistry = registry

	rowCount, err := readProjectionUint64(reader)
	if err != nil {
		return fmt.Errorf("%w: decode facts row count: %v", ErrProjectionRestoreUnavailable, err)
	}
	rowCapacity, err := boundedProjectionCount(reader, rowCount, projectionFactRowMinimumEncodedBytes, p.budget.normalized().MaxFactRows, "fact rows")
	if err != nil {
		return err
	}
	store := newUsageFactStoreV1ForHydrate(rowCapacity)
	for index := uint64(0); index < rowCount; index++ {
		present, err := readProjectionByte(reader)
		if err != nil {
			return fmt.Errorf("%w: decode facts present: %v", ErrProjectionRestoreUnavailable, err)
		}
		if present > 1 {
			return fmt.Errorf("%w: invalid facts timestamp presence %d", ErrProjectionRestoreUnavailable, present)
		}
		store.timestampPresent = append(store.timestampPresent, present)
		seconds, err := readProjectionInt64(reader)
		if err != nil {
			return fmt.Errorf("%w: decode facts timestamp: %v", ErrProjectionRestoreUnavailable, err)
		}
		store.timestampSeconds = append(store.timestampSeconds, seconds)
		nanos, err := readProjectionUint32(reader)
		if err != nil {
			return fmt.Errorf("%w: decode facts nanos: %v", ErrProjectionRestoreUnavailable, err)
		}
		if nanos >= uint32(time.Second) || (present == 0 && (seconds != 0 || nanos != 0)) {
			return fmt.Errorf("%w: invalid facts timestamp seconds=%d nanos=%d present=%d", ErrProjectionRestoreUnavailable, seconds, nanos, present)
		}
		store.timestampNanoseconds = append(store.timestampNanoseconds, int32(nanos))
		sequence, err := readProjectionUint64(reader)
		if err != nil {
			return fmt.Errorf("%w: decode facts sequence: %v", ErrProjectionRestoreUnavailable, err)
		}
		if sequence == 0 {
			return fmt.Errorf("%w: zero facts sequence", ErrProjectionRestoreUnavailable)
		}
		store.sequences = append(store.sequences, sequence)
		batchOrdinal, err := readProjectionUint64(reader)
		if err != nil {
			return fmt.Errorf("%w: decode facts batch ordinal: %v", ErrProjectionRestoreUnavailable, err)
		}
		store.batchOrdinals = append(store.batchOrdinals, batchOrdinal)
		version, err := readProjectionUint64(reader)
		if err != nil {
			return fmt.Errorf("%w: decode facts version: %v", ErrProjectionRestoreUnavailable, err)
		}
		if version == 0 {
			return fmt.Errorf("%w: zero facts version", ErrProjectionRestoreUnavailable)
		}
		store.versions = append(store.versions, version)
		latency, err := readProjectionInt64(reader)
		if err != nil {
			return fmt.Errorf("%w: decode facts latency: %v", ErrProjectionRestoreUnavailable, err)
		}
		if latency < 0 {
			return fmt.Errorf("%w: negative facts latency %d", ErrProjectionRestoreUnavailable, latency)
		}
		store.latencyMs = append(store.latencyMs, latency)
		stringIDs := [7]projectionStringID{}
		for column := range stringIDs {
			id, err := readProjectionUint32(reader)
			if err != nil {
				return fmt.Errorf("%w: decode facts string id: %v", ErrProjectionRestoreUnavailable, err)
			}
			if !validProjectionStringID(id, valueCount, column == projectionFactAuthStringIDColumn) {
				return fmt.Errorf("%w: invalid facts string id %d", ErrProjectionRestoreUnavailable, id)
			}
			stringIDs[column] = projectionStringID(id)
		}
		store.apiIDs = append(store.apiIDs, stringIDs[0])
		store.modelIDs = append(store.modelIDs, stringIDs[1])
		store.providerIDs = append(store.providerIDs, stringIDs[2])
		store.authIDs = append(store.authIDs, stringIDs[3])
		store.sourceIDs = append(store.sourceIDs, stringIDs[4])
		store.priceKeyIDs = append(store.priceKeyIDs, stringIDs[5])
		store.providerStateIDs = append(store.providerStateIDs, stringIDs[6])
		for column := range store.facetIDs {
			id, err := readProjectionUint32(reader)
			if err != nil {
				return fmt.Errorf("%w: decode facts facet id: %v", ErrProjectionRestoreUnavailable, err)
			}
			if !validProjectionStringID(id, valueCount, false) {
				return fmt.Errorf("%w: invalid facts facet id %d", ErrProjectionRestoreUnavailable, id)
			}
			store.facetIDs[column] = append(store.facetIDs[column], projectionStringID(id))
		}
		for column := range store.tokenColumns {
			value, err := readProjectionInt64(reader)
			if err != nil {
				return fmt.Errorf("%w: decode facts token column: %v", ErrProjectionRestoreUnavailable, err)
			}
			if value < 0 {
				return fmt.Errorf("%w: negative facts token column %d", ErrProjectionRestoreUnavailable, value)
			}
			store.tokenColumns[column] = append(store.tokenColumns[column], value)
		}
		billable := BillableTokenComponents{}
		for column := range store.billableColumns {
			value, err := readProjectionInt64(reader)
			if err != nil {
				return fmt.Errorf("%w: decode facts billable column: %v", ErrProjectionRestoreUnavailable, err)
			}
			if value < 0 {
				return fmt.Errorf("%w: negative facts billable column %d", ErrProjectionRestoreUnavailable, value)
			}
			store.billableColumns[column] = append(store.billableColumns[column], value)
			switch column {
			case factBillableInput:
				billable.InputTokens = value
			case factBillableOutput:
				billable.OutputTokens = value
			case factBillableReasoning:
				billable.ReasoningTokens = value
			case factBillableCacheRead:
				billable.CacheReadTokens = value
			case factBillableCacheCreation:
				billable.CacheCreationTokens = value
			case factBillableUnclassifiedCache:
				billable.UnclassifiedCacheTokens = value
			}
		}
		failed, err := readProjectionByte(reader)
		if err != nil {
			return fmt.Errorf("%w: decode facts failed: %v", ErrProjectionRestoreUnavailable, err)
		}
		if failed > 1 {
			return fmt.Errorf("%w: invalid facts failed flag %d", ErrProjectionRestoreUnavailable, failed)
		}
		store.failed = append(store.failed, failed)
		usageSource, err := readProjectionByte(reader)
		if err != nil {
			return fmt.Errorf("%w: decode facts usage source: %v", ErrProjectionRestoreUnavailable, err)
		}
		if usageSource > factUsageSourceComputed {
			return fmt.Errorf("%w: invalid facts usage source %d", ErrProjectionRestoreUnavailable, usageSource)
		}
		store.usageSources = append(store.usageSources, usageSource)
		classification, err := readProjectionByte(reader)
		if err != nil {
			return fmt.Errorf("%w: decode facts classification: %v", ErrProjectionRestoreUnavailable, err)
		}
		if classification > factClassificationZeroBillableComplete {
			return fmt.Errorf("%w: invalid facts classification %d", ErrProjectionRestoreUnavailable, classification)
		}
		store.classifications = append(store.classifications, classification)
		componentMask, err := readProjectionByte(reader)
		if err != nil {
			return fmt.Errorf("%w: decode facts component mask: %v", ErrProjectionRestoreUnavailable, err)
		}
		if componentMask&^projectionAllComponentMask != 0 || ComponentMask(componentMask) != billable.Mask() {
			return fmt.Errorf("%w: invalid facts component mask %d", ErrProjectionRestoreUnavailable, componentMask)
		}
		store.componentMasks = append(store.componentMasks, ComponentMask(componentMask))
		active, err := readProjectionByte(reader)
		if err != nil {
			return fmt.Errorf("%w: decode facts active: %v", ErrProjectionRestoreUnavailable, err)
		}
		if active > 1 {
			return fmt.Errorf("%w: invalid facts active flag %d", ErrProjectionRestoreUnavailable, active)
		}
		store.setActive(int(index), active != 0)
		stableEventID, err := readProjectionString(reader)
		if err != nil {
			return fmt.Errorf("%w: decode facts stable event id: %v", ErrProjectionRestoreUnavailable, err)
		}
		if strings.TrimSpace(stableEventID) == "" {
			return fmt.Errorf("%w: empty facts stable event id", ErrProjectionRestoreUnavailable)
		}
		store.stableEventIDs = append(store.stableEventIDs, stableEventID)
		store.retainedStringBytes = saturatingAddUint64(store.retainedStringBytes, uint64(len(stableEventID))+projectionStringHeaderBytes)
		canonicalIdentity, err := readProjectionString(reader)
		if err != nil {
			return fmt.Errorf("%w: decode facts canonical identity: %v", ErrProjectionRestoreUnavailable, err)
		}
		if strings.TrimSpace(canonicalIdentity) == "" {
			return fmt.Errorf("%w: empty facts canonical identity", ErrProjectionRestoreUnavailable)
		}
		store.canonicalEventIdentities = append(store.canonicalEventIdentities, canonicalIdentity)
		store.retainedStringBytes = saturatingAddUint64(store.retainedStringBytes, uint64(len(canonicalIdentity))+projectionStringHeaderBytes)
	}
	p.facts = store
	return nil
}

func newUsageFactStoreV1ForHydrate(capacity int) usageFactStoreV1 {
	store := usageFactStoreV1{
		timestampSeconds:         make([]int64, 0, capacity),
		timestampNanoseconds:     make([]int32, 0, capacity),
		timestampPresent:         make([]uint8, 0, capacity),
		sequences:                make([]uint64, 0, capacity),
		batchOrdinals:            make([]uint64, 0, capacity),
		versions:                 make([]uint64, 0, capacity),
		latencyMs:                make([]int64, 0, capacity),
		apiIDs:                   make([]projectionStringID, 0, capacity),
		modelIDs:                 make([]projectionStringID, 0, capacity),
		providerIDs:              make([]projectionStringID, 0, capacity),
		authIDs:                  make([]projectionStringID, 0, capacity),
		sourceIDs:                make([]projectionStringID, 0, capacity),
		priceKeyIDs:              make([]projectionStringID, 0, capacity),
		providerStateIDs:         make([]projectionStringID, 0, capacity),
		failed:                   make([]uint8, 0, capacity),
		usageSources:             make([]uint8, 0, capacity),
		classifications:          make([]uint8, 0, capacity),
		componentMasks:           make([]ComponentMask, 0, capacity),
		stableEventIDs:           make([]string, 0, capacity),
		canonicalEventIdentities: make([]string, 0, capacity),
		activeBits:               make([]uint64, 0, (capacity+63)/64),
	}
	for column := range store.facetIDs {
		store.facetIDs[column] = make([]projectionStringID, 0, capacity)
	}
	for column := range store.tokenColumns {
		store.tokenColumns[column] = make([]int64, 0, capacity)
	}
	for column := range store.billableColumns {
		store.billableColumns[column] = make([]int64, 0, capacity)
	}
	return store
}

func (p *UsageProjection) unmarshalPostingsV2Locked(reader *bytes.Reader) error {
	keyCount, err := readProjectionUint64(reader)
	if err != nil {
		return fmt.Errorf("%w: decode postings key count: %v", ErrProjectionRestoreUnavailable, err)
	}
	keyCapacity, err := boundedProjectionCount(reader, keyCount, projectionPostingKeyMinimumBytes, p.budget.normalized().MaxPostingRefs, "posting keys")
	if err != nil {
		return err
	}
	postings := newPostingRegistryV1()
	postings.rows = make(map[projectionPostingKey][]projectionFactRowID, keyCapacity)
	remainingRefs := p.budget.normalized().MaxPostingRefs
	for index := uint64(0); index < keyCount; index++ {
		dimension, err := readProjectionByte(reader)
		if err != nil {
			return fmt.Errorf("%w: decode postings dimension: %v", ErrProjectionRestoreUnavailable, err)
		}
		value, err := readProjectionUint32(reader)
		if err != nil {
			return fmt.Errorf("%w: decode postings value: %v", ErrProjectionRestoreUnavailable, err)
		}
		if projectionPostingDimension(dimension) < postingDimensionAll || projectionPostingDimension(dimension) > postingDimensionSource {
			return fmt.Errorf("%w: invalid posting dimension %d", ErrProjectionRestoreUnavailable, dimension)
		}
		postingKeyDimension := projectionPostingDimension(dimension)
		if postingKeyDimension == postingDimensionAll {
			if value != 0 {
				return fmt.Errorf("%w: all posting has value %d", ErrProjectionRestoreUnavailable, value)
			}
		} else if !validProjectionStringID(value, uint64(len(p.stringRegistry.values)), postingKeyDimension == postingDimensionAuth) {
			return fmt.Errorf("%w: invalid posting value id %d", ErrProjectionRestoreUnavailable, value)
		}
		rowCount, err := readProjectionUint64(reader)
		if err != nil {
			return fmt.Errorf("%w: decode postings row count: %v", ErrProjectionRestoreUnavailable, err)
		}
		if rowCount == 0 {
			return fmt.Errorf("%w: empty posting list", ErrProjectionRestoreUnavailable)
		}
		rowCapacity, err := boundedProjectionCount(reader, rowCount, 4, remainingRefs, "posting rows")
		if err != nil {
			return err
		}
		key := projectionPostingKey{Dimension: postingKeyDimension, Value: projectionStringID(value)}
		if _, duplicate := postings.rows[key]; duplicate {
			return fmt.Errorf("%w: duplicate posting key", ErrProjectionRestoreUnavailable)
		}
		rows := make([]projectionFactRowID, 0, rowCapacity)
		previousRowID := projectionFactRowID(0)
		for rowIndex := uint64(0); rowIndex < rowCount; rowIndex++ {
			rowID, err := readProjectionUint32(reader)
			if err != nil {
				return fmt.Errorf("%w: decode postings row id: %v", ErrProjectionRestoreUnavailable, err)
			}
			factRowID := projectionFactRowID(rowID)
			if factRowID == 0 || uint64(factRowID) > uint64(len(p.facts.timestampSeconds)) || factRowID <= previousRowID {
				return fmt.Errorf("%w: invalid posting row id %d", ErrProjectionRestoreUnavailable, rowID)
			}
			rows = append(rows, factRowID)
			previousRowID = factRowID
		}
		postings.rows[key] = rows
		postings.refs += uint64(len(rows))
		remainingRefs -= uint64(len(rows))
	}
	p.postings = postings
	return nil
}

func (p *UsageProjection) unmarshalScalarRollupsV2Locked(reader *bytes.Reader) error {
	totals, err := readCompactAggregateV2(reader)
	if err != nil {
		return err
	}
	p.totals = totals
	registryEntries := uint64(len(p.stringRegistry.values))
	rollupLimit := p.budget.normalized().MaxRollupCells
	facets, err := readProjectionFacetMapV2(reader, registryEntries, rollupLimit)
	if err != nil {
		return err
	}
	p.facets = facets
	for _, target := range []struct {
		kind    string
		buckets *map[string]compactHourBucket
	}{
		{kind: "hour", buckets: &p.hours},
		{kind: "day", buckets: &p.days},
		{kind: "week", buckets: &p.weeks},
		{kind: "month", buckets: &p.months},
		{kind: "year", buckets: &p.years},
	} {
		loaded, err := readProjectionBucketMapV2(reader, target.kind, registryEntries, rollupLimit)
		if err != nil {
			return err
		}
		*target.buckets = loaded
	}
	health, err := readProjectionHealthMapV2(reader, rollupLimit)
	if err != nil {
		return err
	}
	p.health = health
	catalog, err := readProjectionCatalogV2(reader, p.budget.normalized().MaxRegistryEntries)
	if err != nil {
		return err
	}
	p.catalog = catalog
	return nil
}

// rebuildIdentityIndexesLocked reconstructs the events map, canonical index
// and coordinate/lookup indexes from the active fact rows, identity sidecar
// and canonical snapshot. The facts store carries stable event id, canonical
// identity and version per row; the sidecar carries the canonical identity
// seed, source group key/ordinal and canonical detail hash; the canonical
// snapshot carries the request coordinates (request id/endpoint/role/sequence)
// needed for coordinate and logical/request lookup. Active fact rows and
// sidecar entries must agree one-to-one or hydrate fails closed.
func (p *UsageProjection) rebuildIdentityIndexesLocked(sidecar IdentitySidecar, snapshot StatisticsSnapshot) error {
	p.events = make(map[string]projectionEvent)
	p.canonical = make(map[projectionIndexKey]string)
	p.coordinate = make(map[projectionIndexKey]string)
	p.lookup = make(map[projectionIndexKey]string)
	p.exportDetails = make(map[string]RequestDetail)
	entriesByKey, err := validatedSidecarEntriesByDetailKey(sidecar)
	if err != nil {
		return err
	}
	sidecarByStableEventID := make(map[string]IdentitySidecarEntry, len(sidecar.Entries))
	for _, entry := range sidecar.Entries {
		sidecarByStableEventID[entry.StableEventID] = entry
	}
	activeCount := uint64(0)
	seenStableEventIDs := make(map[string]struct{}, len(sidecar.Entries))
	seenCanonicalIdentities := make(map[string]struct{}, len(sidecar.Entries))
	for index := 0; index < len(p.facts.timestampSeconds); index++ {
		rowID := projectionFactRowID(index + 1)
		if !p.facts.isActive(rowID) {
			continue
		}
		activeCount++
		stableEventID := p.facts.stableEventIDs[index]
		canonicalIdentity := p.facts.canonicalEventIdentities[index]
		if _, duplicate := seenStableEventIDs[stableEventID]; duplicate {
			return fmt.Errorf("%w: duplicate active stable event id %q", ErrProjectionRestoreUnavailable, stableEventID)
		}
		if _, duplicate := seenCanonicalIdentities[canonicalIdentity]; duplicate {
			return fmt.Errorf("%w: duplicate active canonical identity %q", ErrProjectionRestoreUnavailable, canonicalIdentity)
		}
		entry, ok := sidecarByStableEventID[stableEventID]
		if !ok {
			return fmt.Errorf("%w: active fact row %d has no sidecar identity", ErrProjectionRestoreUnavailable, index)
		}
		if entry.CanonicalEventIdentity != canonicalIdentity {
			return fmt.Errorf("%w: active fact row %d canonical identity mismatch", ErrProjectionRestoreUnavailable, index)
		}
		if entry.Sequence != p.facts.sequences[index] {
			return fmt.Errorf("%w: active fact row %d sequence %d != sidecar %d", ErrProjectionRestoreUnavailable, index, p.facts.sequences[index], entry.Sequence)
		}
		if entry.BatchOrdinal != p.facts.batchOrdinals[index] {
			return fmt.Errorf("%w: active fact row %d batch ordinal %d != sidecar %d", ErrProjectionRestoreUnavailable, index, p.facts.batchOrdinals[index], entry.BatchOrdinal)
		}
		seenStableEventIDs[stableEventID] = struct{}{}
		seenCanonicalIdentities[canonicalIdentity] = struct{}{}
		p.events[stableEventID] = projectionEvent{
			CanonicalIdentitySeed:  entry.CanonicalIdentitySeed,
			CanonicalEventIdentity: canonicalIdentity,
			StableEventID:          stableEventID,
			SourceGroupKey:         entry.SourceGroupKey,
			SourceGroupOrdinal:     entry.SourceGroupOrdinal,
			CanonicalDetailHash:    entry.CanonicalDetailHash,
			Version:                p.facts.versions[index],
			FactRowID:              rowID,
		}
		canonicalKey := newProjectionIndexKey(projectionIndexCanonical, canonicalIdentity)
		if existing := p.canonical[canonicalKey]; existing != "" && existing != stableEventID {
			return fmt.Errorf("%w: canonical identity index collision", ErrProjectionRestoreUnavailable)
		}
		p.canonical[canonicalKey] = stableEventID
	}
	if activeCount != uint64(len(sidecar.Entries)) {
		return fmt.Errorf("%w: active fact rows %d != sidecar entries %d", ErrProjectionRestoreUnavailable, activeCount, len(sidecar.Entries))
	}
	if uint64(len(p.events)) != activeCount || uint64(len(p.canonical)) != activeCount {
		return fmt.Errorf("%w: hydrated identity index cardinality mismatch", ErrProjectionRestoreUnavailable)
	}
	return p.rebuildCoordinateLookupLocked(entriesByKey, snapshot)
}

// rebuildCoordinateLookupLocked repopulates the coordinate and lookup indexes
// from the canonical snapshot details and the sidecar identity index. These
// indexes are required for live enrichment dedup after a restart; they are
// derived from the request coordinates in the canonical snapshot (which the v2
// projection sections do not carry) matched to sidecar identities by the same
// deterministic occurrence ordering used by the replay path.
func (p *UsageProjection) rebuildCoordinateLookupLocked(entriesByKey map[string][]IdentitySidecarEntry, snapshot StatisticsSnapshot) error {
	occurrences := make(map[string]uint64, len(p.events))
	usedStableEventIDs := make(map[string]struct{}, len(p.events))
	detailCount := 0
	apiNames := make([]string, 0, len(snapshot.APIs))
	for apiName := range snapshot.APIs {
		apiNames = append(apiNames, apiName)
	}
	sort.Strings(apiNames)
	for _, apiName := range apiNames {
		apiSnapshot := snapshot.APIs[apiName]
		modelNames := make([]string, 0, len(apiSnapshot.Models))
		for modelName := range apiSnapshot.Models {
			modelNames = append(modelNames, modelName)
		}
		sort.Strings(modelNames)
		for _, modelName := range modelNames {
			for _, rawDetail := range apiSnapshot.Models[modelName].Details {
				detailCount++
				detail := normalizeRequestDetail(rawDetail, rawDetail.Provider)
				detail.Endpoint = safeImportedEndpoint(apiName, detail.Endpoint)
				if detail.Model == "" {
					detail.Model = modelName
				}
				targetAPIName := safeImportedAPIName(apiName, detail)
				key := sidecarDetailKey(targetAPIName, modelName, detail)
				occurrence := occurrences[key]
				occurrences[key] = occurrence + 1
				candidates := entriesByKey[key]
				if occurrence >= uint64(len(candidates)) {
					return fmt.Errorf("%w: canonical detail has no sidecar identity", ErrProjectionRestoreUnavailable)
				}
				entry := candidates[occurrence]
				if _, duplicate := usedStableEventIDs[entry.StableEventID]; duplicate {
					return fmt.Errorf("%w: sidecar identity reused by canonical details", ErrProjectionRestoreUnavailable)
				}
				if _, ok := p.events[entry.StableEventID]; !ok {
					return fmt.Errorf("%w: canonical detail sidecar identity is not active", ErrProjectionRestoreUnavailable)
				}
				usedStableEventIDs[entry.StableEventID] = struct{}{}
				p.exportDetails[entry.StableEventID] = cloneRequestDetail(detail)
				coordinateKey := coordinateIndexKey(targetAPIName, detail)
				p.coordinate[coordinateKey] = entry.StableEventID
				logicalKey := logicalLookupIndexKey(detailIdentityKey(targetAPIName, modelName, detail))
				if existing := p.lookup[logicalKey]; existing == "" || existing == entry.StableEventID {
					p.lookup[logicalKey] = entry.StableEventID
				}
				if detail.RequestID != "" {
					requestKey := requestLookupIndexKey(targetAPIName, detail)
					if existing := p.lookup[requestKey]; existing == "" || existing == entry.StableEventID {
						p.lookup[requestKey] = entry.StableEventID
					}
				}
			}
		}
	}
	if detailCount != len(p.events) {
		return fmt.Errorf("%w: canonical detail count %d != active events %d", ErrProjectionRestoreUnavailable, detailCount, len(p.events))
	}
	if len(usedStableEventIDs) != len(p.events) {
		return fmt.Errorf("%w: not all active identities were matched to canonical details", ErrProjectionRestoreUnavailable)
	}
	return nil
}

// validateHydratedStateLocked fails closed when the hydrated stores do not
// match the persisted storage metrics or sidecar metadata. This prevents a
// partially decoded or drifted generation from being published as truth.
func (p *UsageProjection) validateHydratedStateLocked(metrics ProjectionStorageMetrics, sidecar IdentitySidecar, snapshot StatisticsSnapshot) error {
	if sidecar.DatasetEpoch != p.datasetEpoch {
		return fmt.Errorf("%w: sidecar dataset epoch %d != hydrated %d", ErrProjectionRestoreUnavailable, sidecar.DatasetEpoch, p.datasetEpoch)
	}
	if sidecar.CanonicalDetailGeneration != p.revision {
		return fmt.Errorf("%w: sidecar canonical detail generation %d != hydrated revision %d", ErrProjectionRestoreUnavailable, sidecar.CanonicalDetailGeneration, p.revision)
	}
	if sidecar.NextSequence == 0 || sidecar.NextSequence != p.nextSequence {
		return fmt.Errorf("%w: invalid hydrated next sequence %d", ErrProjectionRestoreUnavailable, sidecar.NextSequence)
	}
	maxSequence := uint64(0)
	for _, sequence := range p.facts.sequences {
		if sequence > maxSequence {
			maxSequence = sequence
		}
	}
	if maxSequence >= p.nextSequence {
		return fmt.Errorf("%w: hydrated next sequence %d does not follow max fact sequence %d", ErrProjectionRestoreUnavailable, p.nextSequence, maxSequence)
	}
	if err := p.validateHydratedPostingsLocked(); err != nil {
		return err
	}
	if err := p.validateHydratedScalarStateLocked(snapshot); err != nil {
		return err
	}
	if err := p.validateHydratedCatalogLocked(); err != nil {
		return err
	}
	actualMetrics := p.storageMetricsLocked()
	if actualMetrics != metrics {
		return fmt.Errorf("%w: hydrated storage metrics %+v != persisted %+v", ErrProjectionRestoreUnavailable, actualMetrics, metrics)
	}
	if err := p.validateStorageBudgetLocked(actualMetrics); err != nil {
		return fmt.Errorf("%w: %v", ErrProjectionRestoreUnavailable, err)
	}
	return nil
}

func readCompactAggregateV2(reader *bytes.Reader) (compactAggregate, error) {
	aggregate := compactAggregate{}
	for index := 0; index < compactAggregateCounterCount; index++ {
		value, err := readProjectionInt64(reader)
		if err != nil {
			return compactAggregate{}, fmt.Errorf("%w: decode aggregate counter: %v", ErrProjectionRestoreUnavailable, err)
		}
		aggregate.counters.add(index, value)
	}
	if err := validateCompactAggregateV2(aggregate); err != nil {
		return compactAggregate{}, err
	}
	return aggregate, nil
}

func readProjectionFacetMapV2(reader *bytes.Reader, registryEntries, limit uint64) (map[projectionStringID]int64, error) {
	length, err := readProjectionUint64(reader)
	if err != nil {
		return nil, fmt.Errorf("%w: decode facet map length: %v", ErrProjectionRestoreUnavailable, err)
	}
	capacity, err := boundedProjectionCount(reader, length, projectionFacetEntryMinimumBytes, limit, "facet map")
	if err != nil {
		return nil, err
	}
	result := make(map[projectionStringID]int64, capacity)
	for index := uint64(0); index < length; index++ {
		key, err := readProjectionUint32(reader)
		if err != nil {
			return nil, fmt.Errorf("%w: decode facet map key: %v", ErrProjectionRestoreUnavailable, err)
		}
		if !validProjectionStringID(key, registryEntries, false) {
			return nil, fmt.Errorf("%w: invalid facet map key %d", ErrProjectionRestoreUnavailable, key)
		}
		if _, duplicate := result[projectionStringID(key)]; duplicate {
			return nil, fmt.Errorf("%w: duplicate facet map key %d", ErrProjectionRestoreUnavailable, key)
		}
		value, err := readProjectionInt64(reader)
		if err != nil {
			return nil, fmt.Errorf("%w: decode facet map value: %v", ErrProjectionRestoreUnavailable, err)
		}
		if value <= 0 {
			return nil, fmt.Errorf("%w: invalid facet map value %d", ErrProjectionRestoreUnavailable, value)
		}
		result[projectionStringID(key)] = value
	}
	return result, nil
}

func readProjectionAggregateMapV2(reader *bytes.Reader, registryEntries, limit uint64) (map[projectionStringID]compactAggregate, error) {
	length, err := readProjectionUint64(reader)
	if err != nil {
		return nil, fmt.Errorf("%w: decode aggregate map length: %v", ErrProjectionRestoreUnavailable, err)
	}
	capacity, err := boundedProjectionCount(reader, length, projectionAggregateEntryMinimumBytes, limit, "aggregate map")
	if err != nil {
		return nil, err
	}
	result := make(map[projectionStringID]compactAggregate, capacity)
	for index := uint64(0); index < length; index++ {
		key, err := readProjectionUint32(reader)
		if err != nil {
			return nil, fmt.Errorf("%w: decode aggregate map key: %v", ErrProjectionRestoreUnavailable, err)
		}
		if !validProjectionStringID(key, registryEntries, false) {
			return nil, fmt.Errorf("%w: invalid aggregate map key %d", ErrProjectionRestoreUnavailable, key)
		}
		if _, duplicate := result[projectionStringID(key)]; duplicate {
			return nil, fmt.Errorf("%w: duplicate aggregate map key %d", ErrProjectionRestoreUnavailable, key)
		}
		aggregate, err := readCompactAggregateV2(reader)
		if err != nil {
			return nil, err
		}
		if aggregate.counters.value(compactAggregateTotalRequests) <= 0 {
			return nil, fmt.Errorf("%w: empty aggregate map value", ErrProjectionRestoreUnavailable)
		}
		result[projectionStringID(key)] = aggregate
	}
	return result, nil
}

func readProjectionBucketMapV2(reader *bytes.Reader, kind string, registryEntries, limit uint64) (map[string]compactHourBucket, error) {
	length, err := readProjectionUint64(reader)
	if err != nil {
		return nil, fmt.Errorf("%w: decode bucket map length: %v", ErrProjectionRestoreUnavailable, err)
	}
	capacity, err := boundedProjectionCount(reader, length, projectionBucketMinimumEncodedBytes, limit, kind+" bucket map")
	if err != nil {
		return nil, err
	}
	result := make(map[string]compactHourBucket, capacity)
	for index := uint64(0); index < length; index++ {
		key, err := readProjectionString(reader)
		if err != nil {
			return nil, fmt.Errorf("%w: decode bucket map key: %v", ErrProjectionRestoreUnavailable, err)
		}
		if _, duplicate := result[key]; duplicate {
			return nil, fmt.Errorf("%w: duplicate %s bucket key %q", ErrProjectionRestoreUnavailable, kind, key)
		}
		parsedStart, parseErr := time.Parse(time.RFC3339Nano, key)
		if parseErr != nil || bucketKey(parsedStart) != key {
			return nil, fmt.Errorf("%w: invalid %s bucket key %q", ErrProjectionRestoreUnavailable, kind, key)
		}
		start, expectedEnd := rollupWindow(parsedStart, kind)
		if !start.Equal(parsedStart) {
			return nil, fmt.Errorf("%w: unaligned %s bucket key %q", ErrProjectionRestoreUnavailable, kind, key)
		}
		startNanos, err := readProjectionInt64(reader)
		if err != nil {
			return nil, fmt.Errorf("%w: decode bucket interval start: %v", ErrProjectionRestoreUnavailable, err)
		}
		endNanos, err := readProjectionInt64(reader)
		if err != nil {
			return nil, fmt.Errorf("%w: decode bucket interval end: %v", ErrProjectionRestoreUnavailable, err)
		}
		if startNanos != start.UnixNano() || endNanos != expectedEnd.UnixNano() {
			return nil, fmt.Errorf("%w: invalid %s bucket interval %q", ErrProjectionRestoreUnavailable, kind, key)
		}
		totals, err := readCompactAggregateV2(reader)
		if err != nil {
			return nil, err
		}
		if totals.counters.value(compactAggregateTotalRequests) <= 0 {
			return nil, fmt.Errorf("%w: empty %s bucket %q", ErrProjectionRestoreUnavailable, kind, key)
		}
		facets, err := readProjectionFacetMapV2(reader, registryEntries, limit)
		if err != nil {
			return nil, err
		}
		bucket := compactHourBucket{
			IntervalStart: start,
			IntervalEnd:   expectedEnd,
			Totals:        totals,
			Facets:        facets,
		}
		apis, err := readProjectionAggregateMapV2(reader, registryEntries, limit)
		if err != nil {
			return nil, err
		}
		bucket.APIs = apis
		models, err := readProjectionAggregateMapV2(reader, registryEntries, limit)
		if err != nil {
			return nil, err
		}
		bucket.Models = models
		providers, err := readProjectionAggregateMapV2(reader, registryEntries, limit)
		if err != nil {
			return nil, err
		}
		bucket.Providers = providers
		auths, err := readProjectionAggregateMapV2(reader, registryEntries, limit)
		if err != nil {
			return nil, err
		}
		bucket.Auths = auths
		sources, err := readProjectionAggregateMapV2(reader, registryEntries, limit)
		if err != nil {
			return nil, err
		}
		bucket.Sources = sources
		result[key] = bucket
	}
	return result, nil
}

func readProjectionHealthMapV2(reader *bytes.Reader, limit uint64) (map[string]HealthBucket, error) {
	length, err := readProjectionUint64(reader)
	if err != nil {
		return nil, fmt.Errorf("%w: decode health map length: %v", ErrProjectionRestoreUnavailable, err)
	}
	capacity, err := boundedProjectionCount(reader, length, projectionHealthMinimumEncodedBytes, limit, "health map")
	if err != nil {
		return nil, err
	}
	result := make(map[string]HealthBucket, capacity)
	for index := uint64(0); index < length; index++ {
		key, err := readProjectionString(reader)
		if err != nil {
			return nil, fmt.Errorf("%w: decode health map key: %v", ErrProjectionRestoreUnavailable, err)
		}
		if _, duplicate := result[key]; duplicate {
			return nil, fmt.Errorf("%w: duplicate health bucket key %q", ErrProjectionRestoreUnavailable, key)
		}
		start, parseErr := time.Parse(time.RFC3339Nano, key)
		if parseErr != nil || bucketKey(start) != key {
			return nil, fmt.Errorf("%w: invalid health bucket key %q", ErrProjectionRestoreUnavailable, key)
		}
		start = start.UTC()
		expectedEnd := start.Add(healthBucketDuration)
		startNanos, err := readProjectionInt64(reader)
		if err != nil {
			return nil, fmt.Errorf("%w: decode health interval start: %v", ErrProjectionRestoreUnavailable, err)
		}
		endNanos, err := readProjectionInt64(reader)
		if err != nil {
			return nil, fmt.Errorf("%w: decode health interval end: %v", ErrProjectionRestoreUnavailable, err)
		}
		total, err := readProjectionInt64(reader)
		if err != nil {
			return nil, fmt.Errorf("%w: decode health total requests: %v", ErrProjectionRestoreUnavailable, err)
		}
		success, err := readProjectionInt64(reader)
		if err != nil {
			return nil, fmt.Errorf("%w: decode health success count: %v", ErrProjectionRestoreUnavailable, err)
		}
		failure, err := readProjectionInt64(reader)
		if err != nil {
			return nil, fmt.Errorf("%w: decode health failure count: %v", ErrProjectionRestoreUnavailable, err)
		}
		latency, err := readProjectionInt64(reader)
		if err != nil {
			return nil, fmt.Errorf("%w: decode health latency sum: %v", ErrProjectionRestoreUnavailable, err)
		}
		if startNanos != start.UnixNano() || endNanos != expectedEnd.UnixNano() || total <= 0 || success < 0 || failure < 0 || latency < 0 || success+failure != total {
			return nil, fmt.Errorf("%w: invalid health bucket %q", ErrProjectionRestoreUnavailable, key)
		}
		result[key] = HealthBucket{
			IntervalStart: start,
			IntervalEnd:   expectedEnd,
			TotalRequests: total,
			SuccessCount:  success,
			FailureCount:  failure,
			LatencyMsSum:  latency,
		}
	}
	return result, nil
}

func readProjectionCatalogV2(reader *bytes.Reader, limit uint64) (ProjectionCatalog, error) {
	catalog := ProjectionCatalog{
		Models:    make(map[string]CatalogEntry),
		PriceKeys: make(map[string]CatalogEntry),
		Sources:   make(map[string]CatalogEntry),
	}
	for _, target := range []*map[string]CatalogEntry{&catalog.Models, &catalog.PriceKeys, &catalog.Sources} {
		length, err := readProjectionUint64(reader)
		if err != nil {
			return ProjectionCatalog{}, fmt.Errorf("%w: decode catalog length: %v", ErrProjectionRestoreUnavailable, err)
		}
		if _, err := boundedProjectionCount(reader, length, projectionCatalogEntryMinimumBytes, limit, "catalog"); err != nil {
			return ProjectionCatalog{}, err
		}
		for index := uint64(0); index < length; index++ {
			key, err := readProjectionString(reader)
			if err != nil {
				return ProjectionCatalog{}, fmt.Errorf("%w: decode catalog key: %v", ErrProjectionRestoreUnavailable, err)
			}
			id, err := readProjectionString(reader)
			if err != nil {
				return ProjectionCatalog{}, fmt.Errorf("%w: decode catalog id: %v", ErrProjectionRestoreUnavailable, err)
			}
			label, err := readProjectionString(reader)
			if err != nil {
				return ProjectionCatalog{}, fmt.Errorf("%w: decode catalog label: %v", ErrProjectionRestoreUnavailable, err)
			}
			priceKey, err := readProjectionString(reader)
			if err != nil {
				return ProjectionCatalog{}, fmt.Errorf("%w: decode catalog price key: %v", ErrProjectionRestoreUnavailable, err)
			}
			provider, err := readProjectionString(reader)
			if err != nil {
				return ProjectionCatalog{}, fmt.Errorf("%w: decode catalog provider: %v", ErrProjectionRestoreUnavailable, err)
			}
			providerState, err := readProjectionString(reader)
			if err != nil {
				return ProjectionCatalog{}, fmt.Errorf("%w: decode catalog provider state: %v", ErrProjectionRestoreUnavailable, err)
			}
			if strings.TrimSpace(key) == "" || id != key {
				return ProjectionCatalog{}, fmt.Errorf("%w: invalid catalog identity %q/%q", ErrProjectionRestoreUnavailable, key, id)
			}
			if _, duplicate := (*target)[key]; duplicate {
				return ProjectionCatalog{}, fmt.Errorf("%w: duplicate catalog key %q", ErrProjectionRestoreUnavailable, key)
			}
			(*target)[key] = CatalogEntry{ID: id, Label: label, PriceKey: priceKey, Provider: provider, ProviderState: providerState}
		}
	}
	return catalog, nil
}

func boundedProjectionCount(reader *bytes.Reader, count, minimumBytes, limit uint64, label string) (int, error) {
	if count > limit {
		return 0, fmt.Errorf("%w: %s count %d exceeds limit %d", ErrProjectionRestoreUnavailable, label, count, limit)
	}
	if minimumBytes > 0 && count > uint64(reader.Len())/minimumBytes {
		return 0, fmt.Errorf("%w: %s count %d exceeds remaining payload", ErrProjectionRestoreUnavailable, label, count)
	}
	maxInt := uint64(^uint(0) >> 1)
	if count > maxInt {
		return 0, fmt.Errorf("%w: %s count %d exceeds platform capacity", ErrProjectionRestoreUnavailable, label, count)
	}
	return int(count), nil
}

func validProjectionStringID(id uint32, registryEntries uint64, allowZero bool) bool {
	if id == 0 {
		return allowZero
	}
	return uint64(id) <= registryEntries
}

func validatedSidecarEntriesByDetailKey(sidecar IdentitySidecar) (map[string][]IdentitySidecarEntry, error) {
	if !sidecar.valid() || sidecar.NextSequence == 0 {
		return nil, fmt.Errorf("%w: invalid identity sidecar metadata", ErrProjectionRestoreUnavailable)
	}
	entriesByKey := make(map[string][]IdentitySidecarEntry, len(sidecar.Entries))
	seenStableEventIDs := make(map[string]struct{}, len(sidecar.Entries))
	seenCanonicalIdentities := make(map[string]struct{}, len(sidecar.Entries))
	seenCanonicalSeeds := make(map[string]struct{}, len(sidecar.Entries))
	seenSequences := make(map[uint64]struct{}, len(sidecar.Entries))
	seenOccurrences := make(map[string]struct{}, len(sidecar.Entries))
	for _, entry := range sidecar.Entries {
		entry.APIName = strings.TrimSpace(entry.APIName)
		entry.ModelName = strings.TrimSpace(entry.ModelName)
		entry.CanonicalDetailHash = strings.ToLower(strings.TrimSpace(entry.CanonicalDetailHash))
		entry.CanonicalIdentitySeed = strings.ToLower(strings.TrimSpace(entry.CanonicalIdentitySeed))
		entry.CanonicalEventIdentity = strings.ToLower(strings.TrimSpace(entry.CanonicalEventIdentity))
		entry.StableEventID = strings.TrimSpace(entry.StableEventID)
		entry.SourceGroupKey = strings.ToLower(strings.TrimSpace(entry.SourceGroupKey))
		if entry.APIName == "" || entry.ModelName == "" || !isMutationSHA256(entry.CanonicalDetailHash) ||
			!isMutationSHA256(entry.CanonicalIdentitySeed) || !isMutationSHA256(entry.CanonicalEventIdentity) ||
			!strings.HasPrefix(entry.StableEventID, "event:") || entry.SourceGroupKey == "" || entry.Sequence == 0 {
			return nil, fmt.Errorf("%w: incomplete identity sidecar entry", ErrProjectionRestoreUnavailable)
		}
		if CanonicalEventIdentityV1(entry.CanonicalIdentitySeed) != entry.CanonicalEventIdentity {
			return nil, fmt.Errorf("%w: sidecar canonical identity derivation mismatch", ErrProjectionRestoreUnavailable)
		}
		if nextOrdinal, ok := sidecar.SourceOrdinals[entry.SourceGroupKey]; !ok || nextOrdinal <= entry.SourceGroupOrdinal {
			return nil, fmt.Errorf("%w: sidecar source ordinal frontier mismatch", ErrProjectionRestoreUnavailable)
		}
		if _, duplicate := seenStableEventIDs[entry.StableEventID]; duplicate {
			return nil, fmt.Errorf("%w: duplicate sidecar stable event id", ErrProjectionRestoreUnavailable)
		}
		if _, duplicate := seenCanonicalIdentities[entry.CanonicalEventIdentity]; duplicate {
			return nil, fmt.Errorf("%w: duplicate sidecar canonical identity", ErrProjectionRestoreUnavailable)
		}
		if _, duplicate := seenCanonicalSeeds[entry.CanonicalIdentitySeed]; duplicate {
			return nil, fmt.Errorf("%w: duplicate sidecar canonical seed", ErrProjectionRestoreUnavailable)
		}
		if _, duplicate := seenSequences[entry.Sequence]; duplicate {
			return nil, fmt.Errorf("%w: duplicate sidecar sequence", ErrProjectionRestoreUnavailable)
		}
		key := sidecarDetailKeyFromHash(entry.APIName, entry.ModelName, entry.CanonicalDetailHash)
		occurrenceKey := key + "\x00" + formatUint(entry.Occurrence)
		if _, duplicate := seenOccurrences[occurrenceKey]; duplicate {
			return nil, fmt.Errorf("%w: duplicate sidecar detail occurrence", ErrProjectionRestoreUnavailable)
		}
		seenStableEventIDs[entry.StableEventID] = struct{}{}
		seenCanonicalIdentities[entry.CanonicalEventIdentity] = struct{}{}
		seenCanonicalSeeds[entry.CanonicalIdentitySeed] = struct{}{}
		seenSequences[entry.Sequence] = struct{}{}
		seenOccurrences[occurrenceKey] = struct{}{}
		entriesByKey[key] = append(entriesByKey[key], entry)
	}
	for key := range entriesByKey {
		entries := entriesByKey[key]
		sort.Slice(entries, func(i, j int) bool {
			if entries[i].Occurrence != entries[j].Occurrence {
				return entries[i].Occurrence < entries[j].Occurrence
			}
			if entries[i].Sequence != entries[j].Sequence {
				return entries[i].Sequence < entries[j].Sequence
			}
			return entries[i].BatchOrdinal < entries[j].BatchOrdinal
		})
		for index := range entries {
			if entries[index].Occurrence != uint64(index) {
				return nil, fmt.Errorf("%w: non-contiguous sidecar occurrences", ErrProjectionRestoreUnavailable)
			}
		}
		entriesByKey[key] = entries
	}
	return entriesByKey, nil
}

func validateCanonicalSnapshotTotals(snapshot StatisticsSnapshot) error {
	var totalRequests, successCount, failureCount, totalTokens int64
	requestsByDay := make(map[string]int64)
	requestsByHour := make(map[string]int64)
	tokensByDay := make(map[string]int64)
	tokensByHour := make(map[string]int64)
	for apiName, apiSnapshot := range snapshot.APIs {
		if strings.TrimSpace(apiName) == "" {
			return fmt.Errorf("%w: empty canonical API name", ErrProjectionRestoreUnavailable)
		}
		var apiRequests, apiTokens int64
		for modelName, modelSnapshot := range apiSnapshot.Models {
			if strings.TrimSpace(modelName) == "" {
				return fmt.Errorf("%w: empty canonical model name", ErrProjectionRestoreUnavailable)
			}
			modelRequests := int64(len(modelSnapshot.Details))
			modelTokens := int64(0)
			for _, detail := range modelSnapshot.Details {
				modelTokens += detail.Tokens.TotalTokens
				if detail.Failed {
					failureCount++
				} else {
					successCount++
				}
				dayKey := detail.Timestamp.Format("2006-01-02")
				hourKey := formatHour(detail.Timestamp.Hour())
				requestsByDay[dayKey]++
				requestsByHour[hourKey]++
				tokensByDay[dayKey] += detail.Tokens.TotalTokens
				tokensByHour[hourKey] += detail.Tokens.TotalTokens
			}
			if modelSnapshot.TotalRequests != modelRequests || modelSnapshot.TotalTokens != modelTokens {
				return fmt.Errorf("%w: canonical model totals mismatch", ErrProjectionRestoreUnavailable)
			}
			apiRequests += modelRequests
			apiTokens += modelTokens
		}
		if apiSnapshot.TotalRequests != apiRequests || apiSnapshot.TotalTokens != apiTokens {
			return fmt.Errorf("%w: canonical API totals mismatch", ErrProjectionRestoreUnavailable)
		}
		totalRequests += apiRequests
		totalTokens += apiTokens
	}
	if snapshot.TotalRequests != totalRequests || snapshot.SuccessCount != successCount || snapshot.FailureCount != failureCount ||
		snapshot.TotalTokens != totalTokens || successCount+failureCount != totalRequests {
		return fmt.Errorf("%w: canonical snapshot totals mismatch", ErrProjectionRestoreUnavailable)
	}
	for _, comparison := range []struct {
		name     string
		actual   map[string]int64
		expected map[string]int64
	}{
		{name: "requests by day", actual: snapshot.RequestsByDay, expected: requestsByDay},
		{name: "requests by hour", actual: snapshot.RequestsByHour, expected: requestsByHour},
		{name: "tokens by day", actual: snapshot.TokensByDay, expected: tokensByDay},
		{name: "tokens by hour", actual: snapshot.TokensByHour, expected: tokensByHour},
	} {
		if !equalStringInt64Map(comparison.actual, comparison.expected) {
			return fmt.Errorf("%w: canonical %s mismatch", ErrProjectionRestoreUnavailable, comparison.name)
		}
	}
	return nil
}

func equalStringInt64Map(left, right map[string]int64) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		if right[key] != value {
			return false
		}
	}
	return true
}

func hydratedRollupCellCountLocked(projection *UsageProjection) uint64 {
	if projection == nil {
		return 0
	}
	cells := uint64(1)
	for _, buckets := range []map[string]compactHourBucket{projection.hours, projection.days, projection.weeks, projection.months, projection.years} {
		for _, bucket := range buckets {
			cells = saturatingAddUint64(cells, uint64(1+len(bucket.APIs)+len(bucket.Models)+len(bucket.Providers)+len(bucket.Auths)+len(bucket.Sources)))
		}
	}
	return cells
}

func validateCompactAggregateV2(aggregate compactAggregate) error {
	for index := 0; index < compactAggregateCounterCount; index++ {
		if aggregate.counters.value(index) < 0 {
			return fmt.Errorf("%w: negative aggregate counter %d", ErrProjectionRestoreUnavailable, index)
		}
	}
	total := aggregate.counters.value(compactAggregateTotalRequests)
	success := aggregate.counters.value(compactAggregateSuccessCount)
	failure := aggregate.counters.value(compactAggregateFailureCount)
	details := aggregate.counters.value(compactAggregateDetailCount)
	missing := aggregate.counters.value(compactAggregateMissingUsageCount)
	known := aggregate.counters.value(compactAggregateKnownUsageCount)
	provider := aggregate.counters.value(compactAggregateProviderUsageCount)
	computed := aggregate.counters.value(compactAggregateComputedUsageCount)
	if success+failure != total || details != total || missing+known != details || provider+computed != known {
		return fmt.Errorf("%w: aggregate invariant mismatch", ErrProjectionRestoreUnavailable)
	}
	return nil
}

func compactAggregateEqualV2(left, right compactAggregate) bool {
	for index := 0; index < compactAggregateCounterCount; index++ {
		if left.counters.value(index) != right.counters.value(index) {
			return false
		}
	}
	return true
}

func addCompactAggregateV2(target *compactAggregate, value compactAggregate) {
	for index := 0; index < compactAggregateCounterCount; index++ {
		target.counters.add(index, value.counters.value(index))
	}
}

func equalProjectionFacetMapV2(left, right map[projectionStringID]int64) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		if right[key] != value {
			return false
		}
	}
	return true
}

func (p *UsageProjection) validateHydratedPostingsLocked() error {
	factRows := len(p.facts.timestampSeconds)
	if p.postings.refs != uint64(factRows)*6 {
		return fmt.Errorf("%w: posting refs %d != fact rows %d * 6", ErrProjectionRestoreUnavailable, p.postings.refs, factRows)
	}
	masks := make([]uint8, factRows)
	for key, rows := range p.postings.rows {
		if key.Dimension < postingDimensionAll || key.Dimension > postingDimensionSource {
			return fmt.Errorf("%w: invalid hydrated posting dimension", ErrProjectionRestoreUnavailable)
		}
		bit := uint8(1 << uint8(key.Dimension))
		for _, rowID := range rows {
			index, ok := p.facts.index(rowID)
			if !ok {
				return fmt.Errorf("%w: hydrated posting row is out of range", ErrProjectionRestoreUnavailable)
			}
			if masks[index]&bit != 0 {
				return fmt.Errorf("%w: duplicate posting dimension for fact row", ErrProjectionRestoreUnavailable)
			}
			if expected := p.expectedPostingValueLocked(index, key.Dimension); expected != key.Value {
				return fmt.Errorf("%w: posting value does not match fact row", ErrProjectionRestoreUnavailable)
			}
			masks[index] |= bit
		}
	}
	for _, mask := range masks {
		if mask != projectionAllPostingDimensions {
			return fmt.Errorf("%w: incomplete posting dimensions for fact row", ErrProjectionRestoreUnavailable)
		}
	}
	return nil
}

func (p *UsageProjection) expectedPostingValueLocked(index int, dimension projectionPostingDimension) projectionStringID {
	switch dimension {
	case postingDimensionAll:
		return 0
	case postingDimensionAPI:
		return p.facts.apiIDs[index]
	case postingDimensionModel:
		return p.facts.modelIDs[index]
	case postingDimensionProvider:
		return p.facts.providerIDs[index]
	case postingDimensionAuth:
		return p.facts.authIDs[index]
	case postingDimensionSource:
		return p.facts.sourceIDs[index]
	default:
		return 0
	}
}

func (p *UsageProjection) validateHydratedScalarStateLocked(snapshot StatisticsSnapshot) error {
	expectedTotals := compactAggregate{}
	expectedFacets := make(map[projectionStringID]int64)
	p.facts.eachActive(func(rowID projectionFactRowID) {
		contribution, ok := p.facts.contribution(rowID, p.stringRegistry)
		if !ok {
			return
		}
		applyAggregateDelta(&expectedTotals, contribution, 1)
		applyFacetDelta(&expectedFacets, contribution, 1)
	})
	if !compactAggregateEqualV2(expectedTotals, p.totals) || !equalProjectionFacetMapV2(expectedFacets, p.facets) {
		return fmt.Errorf("%w: scalar totals do not match active fact rows", ErrProjectionRestoreUnavailable)
	}
	if p.totals.counters.value(compactAggregateTotalRequests) != snapshot.TotalRequests ||
		p.totals.counters.value(compactAggregateSuccessCount) != snapshot.SuccessCount ||
		p.totals.counters.value(compactAggregateFailureCount) != snapshot.FailureCount ||
		p.totals.counters.value(compactAggregateTotalTokens) != snapshot.TotalTokens {
		return fmt.Errorf("%w: projection totals do not match canonical snapshot", ErrProjectionRestoreUnavailable)
	}
	for _, rollup := range []struct {
		kind    string
		buckets map[string]compactHourBucket
	}{
		{kind: "hour", buckets: p.hours},
		{kind: "day", buckets: p.days},
		{kind: "week", buckets: p.weeks},
		{kind: "month", buckets: p.months},
		{kind: "year", buckets: p.years},
	} {
		if err := p.validateHydratedRollupMapLocked(rollup.kind, rollup.buckets); err != nil {
			return err
		}
	}
	var healthTotal, healthSuccess, healthFailure, healthLatency int64
	for key, bucket := range p.health {
		if bucketKey(bucket.IntervalStart) != key || !bucket.IntervalEnd.Equal(bucket.IntervalStart.Add(healthBucketDuration)) ||
			bucket.TotalRequests <= 0 || bucket.SuccessCount < 0 || bucket.FailureCount < 0 || bucket.LatencyMsSum < 0 ||
			bucket.SuccessCount+bucket.FailureCount != bucket.TotalRequests {
			return fmt.Errorf("%w: invalid hydrated health bucket", ErrProjectionRestoreUnavailable)
		}
		healthTotal += bucket.TotalRequests
		healthSuccess += bucket.SuccessCount
		healthFailure += bucket.FailureCount
		healthLatency += bucket.LatencyMsSum
	}
	if healthTotal != p.totals.counters.value(compactAggregateTotalRequests) ||
		healthSuccess != p.totals.counters.value(compactAggregateSuccessCount) ||
		healthFailure != p.totals.counters.value(compactAggregateFailureCount) ||
		healthLatency != p.totals.counters.value(compactAggregateLatencyMsSum) {
		return fmt.Errorf("%w: health rollup totals mismatch", ErrProjectionRestoreUnavailable)
	}
	return nil
}

func (p *UsageProjection) validateHydratedRollupMapLocked(kind string, buckets map[string]compactHourBucket) error {
	sum := compactAggregate{}
	combinedFacets := make(map[projectionStringID]int64)
	expectedAuths := p.expectedHydratedAuthRollupsLocked(kind)
	for key, bucket := range buckets {
		start, end := rollupWindow(bucket.IntervalStart, kind)
		if bucketKey(start) != key || !start.Equal(bucket.IntervalStart) || !end.Equal(bucket.IntervalEnd) {
			return fmt.Errorf("%w: invalid hydrated %s bucket interval", ErrProjectionRestoreUnavailable, kind)
		}
		if err := validateCompactAggregateV2(bucket.Totals); err != nil {
			return err
		}
		for _, dimension := range []struct {
			values   map[projectionStringID]compactAggregate
			expected compactAggregate
		}{
			{values: bucket.APIs, expected: bucket.Totals},
			{values: bucket.Models, expected: bucket.Totals},
			{values: bucket.Providers, expected: bucket.Totals},
			{values: bucket.Auths, expected: expectedAuths[key]},
			{values: bucket.Sources, expected: bucket.Totals},
		} {
			dimensionTotal := compactAggregate{}
			for id, aggregate := range dimension.values {
				if id == 0 || int(id) > len(p.stringRegistry.values) {
					return fmt.Errorf("%w: invalid hydrated dimension id", ErrProjectionRestoreUnavailable)
				}
				if err := validateCompactAggregateV2(aggregate); err != nil {
					return err
				}
				addCompactAggregateV2(&dimensionTotal, aggregate)
			}
			if !compactAggregateEqualV2(dimensionTotal, dimension.expected) {
				return fmt.Errorf("%w: hydrated %s dimension totals mismatch", ErrProjectionRestoreUnavailable, kind)
			}
		}
		for id, count := range bucket.Facets {
			if id == 0 || int(id) > len(p.stringRegistry.values) || count <= 0 {
				return fmt.Errorf("%w: invalid hydrated %s facet", ErrProjectionRestoreUnavailable, kind)
			}
			combinedFacets[id] += count
		}
		addCompactAggregateV2(&sum, bucket.Totals)
	}
	for key := range expectedAuths {
		if _, ok := buckets[key]; !ok {
			return fmt.Errorf("%w: hydrated %s auth rollup bucket missing", ErrProjectionRestoreUnavailable, kind)
		}
	}
	if !compactAggregateEqualV2(sum, p.totals) || !equalProjectionFacetMapV2(combinedFacets, p.facets) {
		return fmt.Errorf("%w: hydrated %s rollup totals mismatch", ErrProjectionRestoreUnavailable, kind)
	}
	return nil
}

func (p *UsageProjection) expectedHydratedAuthRollupsLocked(kind string) map[string]compactAggregate {
	expected := make(map[string]compactAggregate)
	p.facts.eachActive(func(rowID projectionFactRowID) {
		contribution, ok := p.facts.contribution(rowID, p.stringRegistry)
		if !ok || contribution.AuthID == 0 {
			return
		}
		start, _ := rollupWindow(contribution.Timestamp, kind)
		key := bucketKey(start)
		aggregate := expected[key]
		applyAggregateDelta(&aggregate, contribution, 1)
		expected[key] = aggregate
	})
	return expected
}

func (p *UsageProjection) validateHydratedCatalogLocked() error {
	for key, entry := range p.catalog.Models {
		if key == "" || entry.ID != key {
			return fmt.Errorf("%w: invalid hydrated model catalog entry", ErrProjectionRestoreUnavailable)
		}
	}
	for key, entry := range p.catalog.PriceKeys {
		if key == "" || entry.ID != key {
			return fmt.Errorf("%w: invalid hydrated price catalog entry", ErrProjectionRestoreUnavailable)
		}
	}
	for key, entry := range p.catalog.Sources {
		if key == "" || entry.ID != key {
			return fmt.Errorf("%w: invalid hydrated source catalog entry", ErrProjectionRestoreUnavailable)
		}
	}
	for index := range p.facts.timestampSeconds {
		model := p.stringRegistry.value(p.facts.modelIDs[index])
		priceKey := p.stringRegistry.value(p.facts.priceKeyIDs[index])
		source := p.stringRegistry.value(p.facts.sourceIDs[index])
		if _, ok := p.catalog.Models[model]; !ok {
			return fmt.Errorf("%w: fact model missing from catalog", ErrProjectionRestoreUnavailable)
		}
		if _, ok := p.catalog.PriceKeys[priceKey]; !ok {
			return fmt.Errorf("%w: fact price key missing from catalog", ErrProjectionRestoreUnavailable)
		}
		if _, ok := p.catalog.Sources[source]; !ok {
			return fmt.Errorf("%w: fact source missing from catalog", ErrProjectionRestoreUnavailable)
		}
	}
	return nil
}

// restoreProjectionGeneration tries to directly hydrate the persisted v2
// projection. On hydrate failure the checksum-valid generation is semantically
// invalid; it must NOT replay that same generation's canonical details.
// Instead it falls back to the previous committed generation and retries. If no
// valid committed generation remains, it fails closed. It returns (true, result,
// nil) on a successful direct hydrate, (false, _, nil) when there are no v2
// sections (legacy v1 generation, caller must replay), or (_, _, err) when no
// valid generation can be hydrated (fail-closed).
func (s *RequestStatistics) restoreProjectionGeneration(generation projectionGeneration, path string) (projectionGeneration, bool, MergeResult, error) {
	if s == nil {
		return generation, false, MergeResult{}, nil
	}
	tried := make(map[uint64]struct{})
	tried[generation.Manifest.Generation] = struct{}{}
	for {
		if len(generation.Sections.Facts) == 0 && len(generation.Sections.Postings) == 0 && len(generation.Sections.ScalarRollups) == 0 {
			return generation, false, MergeResult{}, nil
		}
		projection := NewUsageProjection()
		projection.SetBudget(s.restoreProjectionBudget())
		if err := projection.hydrateFromGenerationV2(generation.Projection, generation.Sections, generation.Sidecar, generation.Snapshot); err == nil {
			result := MergeResult{Added: int64(generation.Projection.StorageMetrics.ActiveFactRows)}
			if adoptErr := s.adoptHydratedGeneration(projection, generation); adoptErr != nil {
				return generation, false, result, fmt.Errorf("%w: %v", ErrProjectionRestoreUnavailable, adoptErr)
			}
			return generation, true, result, nil
		} else {
			// Semantic hydrate failure: treat the generation as invalid and fall
			// back to the previous committed generation. Never replay this same
			// generation's canonical details, which would mask the corruption.
			failedGeneration := generation.Manifest.Generation
			previous, previousErr := latestValidProjectionGenerationExcludingSet(path, tried)
			if previousErr != nil || previous.Manifest.Generation == 0 {
				return generation, false, MergeResult{}, fmt.Errorf("%w: generation %d hydrate failed and no valid previous generation: %v", ErrProjectionRestoreUnavailable, failedGeneration, err)
			}
			if _, seen := tried[previous.Manifest.Generation]; seen {
				return generation, false, MergeResult{}, fmt.Errorf("%w: generation %d hydrate failed and previous generation already tried: %v", ErrProjectionRestoreUnavailable, failedGeneration, err)
			}
			tried[previous.Manifest.Generation] = struct{}{}
			generation = previous
			continue
		}
	}
}

func (s *RequestStatistics) restoreProjectionBudget() ProjectionBudgetV2 {
	if s == nil {
		return DefaultProjectionBudgetV2()
	}
	if s.coordinator != nil {
		return s.coordinator.config.ProjectionBudget.normalized()
	}
	s.mu.RLock()
	projection := s.projection
	s.mu.RUnlock()
	if projection != nil {
		return projection.Budget()
	}
	return DefaultProjectionBudgetV2()
}

// adoptHydratedGeneration publishes the hydrated projection and adopts the
// canonical snapshot into the live RequestStatistics state (apis, day/hour
// counters, change/persisted state, detail location indexes) without replaying
// canonical details through ApplyDetail. The detail location indexes are
// rebuilt by ensureDetailLocationsLocked from the adopted apis plus the
// projection's coordinate/lookup indexes restored during hydrate.
func (s *RequestStatistics) adoptHydratedGeneration(projection *UsageProjection, generation projectionGeneration) error {
	if s == nil {
		return nil
	}
	state := s.buildCanonicalStateFromSnapshot(generation.Snapshot, projection)
	if s.coordinator != nil {
		if err := s.coordinator.recordRestoredGeneration(generation.Sidecar, generation.Projection.StorageMetrics.ActiveFactRows, generation.Manifest.Generation); err != nil {
			return err
		}
	}
	s.mu.Lock()
	s.adoptStateLocked(state)
	s.identityMetadataReady = true
	s.identityMetadataGeneration = generation.Sidecar.Generation
	s.mu.Unlock()
	return nil
}

// buildCanonicalStateFromSnapshot constructs a requestStatisticsState directly
// from the canonical snapshot plus the hydrated projection. It does not call
// ApplyDetail/ApplySnapshot, so the projection stores are not rebuilt by replay.
func (s *RequestStatistics) buildCanonicalStateFromSnapshot(snapshot StatisticsSnapshot, projection *UsageProjection) requestStatisticsState {
	apis := make(map[string]*apiStats, len(snapshot.APIs))
	for apiName, apiSnapshot := range snapshot.APIs {
		api := &apiStats{
			TotalRequests: apiSnapshot.TotalRequests,
			TotalTokens:   apiSnapshot.TotalTokens,
			Models:        make(map[string]*modelStats, len(apiSnapshot.Models)),
		}
		for modelName, modelSnapshot := range apiSnapshot.Models {
			details := make([]RequestDetail, len(modelSnapshot.Details))
			for index, detail := range modelSnapshot.Details {
				details[index] = cloneRequestDetail(detail)
			}
			api.Models[modelName] = &modelStats{
				TotalRequests: modelSnapshot.TotalRequests,
				TotalTokens:   modelSnapshot.TotalTokens,
				Details:       details,
			}
		}
		apis[apiName] = api
	}

	requestsByHour := make(map[int]int64, len(snapshot.RequestsByHour))
	for key, value := range snapshot.RequestsByHour {
		if hour, parseErr := strconv.Atoi(key); parseErr == nil && hour >= 0 && hour < 24 {
			requestsByHour[hour] = value
		}
	}
	tokensByHour := make(map[int]int64, len(snapshot.TokensByHour))
	for key, value := range snapshot.TokensByHour {
		if hour, parseErr := strconv.Atoi(key); parseErr == nil && hour >= 0 && hour < 24 {
			tokensByHour[hour] = value
		}
	}

	changeVersion := uint64(snapshot.TotalRequests)
	if projection != nil {
		projection.mu.RLock()
		if projection.facts.activeCount > changeVersion {
			changeVersion = projection.facts.activeCount
		}
		projection.mu.RUnlock()
	}
	return requestStatisticsState{
		totalRequests:              snapshot.TotalRequests,
		successCount:               snapshot.SuccessCount,
		failureCount:               snapshot.FailureCount,
		totalTokens:                snapshot.TotalTokens,
		changeCount:                changeVersion,
		persistedCount:             changeVersion,
		apis:                       apis,
		requestsByDay:              cloneInt64Map(snapshot.RequestsByDay),
		requestsByHour:             requestsByHour,
		tokensByDay:                cloneInt64Map(snapshot.TokensByDay),
		tokensByHour:               tokensByHour,
		projection:                 projection,
		projectionUnavailable:      false,
		projectionUnavailableCode:  "",
		identityMetadataReady:      true,
		identityMetadataGeneration: 0,
	}
}
