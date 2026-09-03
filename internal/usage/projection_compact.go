package usage

import "time"

type projectionStringID uint32
type projectionPricingGroupID uint32

type projectionStringRegistry struct {
	byValue       map[string]projectionStringID
	values        []string
	retainedBytes uint64
}

func newProjectionStringRegistry() projectionStringRegistry {
	return projectionStringRegistry{byValue: make(map[string]projectionStringID)}
}

func (registry *projectionStringRegistry) intern(value string) projectionStringID {
	if value == "" {
		return 0
	}
	if registry.byValue == nil {
		registry.byValue = make(map[string]projectionStringID)
	}
	if id := registry.byValue[value]; id != 0 {
		return id
	}
	id := projectionStringID(len(registry.values) + 1)
	registry.values = append(registry.values, value)
	registry.byValue[value] = id
	registry.retainedBytes = saturatingAddUint64(registry.retainedBytes, uint64(len(value))+projectionStringHeaderBytes)
	return id
}

func (registry projectionStringRegistry) value(id projectionStringID) string {
	if id == 0 || int(id) > len(registry.values) {
		return ""
	}
	return registry.values[int(id)-1]
}

func (registry projectionStringRegistry) clone() projectionStringRegistry {
	result := newProjectionStringRegistry()
	result.values = append(result.values, registry.values...)
	result.retainedBytes = registry.retainedBytes
	for value, id := range registry.byValue {
		result.byValue[value] = id
	}
	return result
}

type projectionPricingGroupDefinition struct {
	Key                 string
	Provider            string
	Model               string
	PriceKey            string
	ProviderRawPresence string
}

type projectionPricingGroupRegistry struct {
	byKey       map[string]projectionPricingGroupID
	definitions []projectionPricingGroupDefinition
}

func newProjectionPricingGroupRegistry() projectionPricingGroupRegistry {
	return projectionPricingGroupRegistry{byKey: make(map[string]projectionPricingGroupID)}
}

func (registry *projectionPricingGroupRegistry) intern(definition projectionPricingGroupDefinition) projectionPricingGroupID {
	if definition.Key == "" {
		return 0
	}
	if registry.byKey == nil {
		registry.byKey = make(map[string]projectionPricingGroupID)
	}
	if id := registry.byKey[definition.Key]; id != 0 {
		return id
	}
	id := projectionPricingGroupID(len(registry.definitions) + 1)
	registry.definitions = append(registry.definitions, definition)
	registry.byKey[definition.Key] = id
	return id
}

func (registry projectionPricingGroupRegistry) definition(id projectionPricingGroupID) (projectionPricingGroupDefinition, bool) {
	if id == 0 || int(id) > len(registry.definitions) {
		return projectionPricingGroupDefinition{}, false
	}
	return registry.definitions[int(id)-1], true
}

func (registry projectionPricingGroupRegistry) clone() projectionPricingGroupRegistry {
	result := newProjectionPricingGroupRegistry()
	result.definitions = append(result.definitions, registry.definitions...)
	for key, id := range registry.byKey {
		result.byKey[key] = id
	}
	return result
}

type compactComponentMaskCounts struct {
	primaryMask  ComponentMask
	primaryCount int64
	extra        *compactComponentMaskCount
}

type compactComponentMaskCount struct {
	mask  ComponentMask
	count int64
	next  *compactComponentMaskCount
}

func (counts *compactComponentMaskCounts) add(mask ComponentMask, delta int64) {
	if counts == nil || delta == 0 {
		return
	}
	if counts.primaryCount == 0 && counts.extra == nil {
		counts.primaryMask = mask
		counts.primaryCount = delta
		if counts.primaryCount == 0 {
			counts.primaryMask = 0
		}
		return
	}
	if counts.primaryMask == mask {
		counts.primaryCount += delta
		if counts.primaryCount == 0 {
			if counts.extra == nil {
				counts.primaryMask = 0
			} else {
				counts.primaryMask = counts.extra.mask
				counts.primaryCount = counts.extra.count
				counts.extra = counts.extra.next
			}
		}
		return
	}
	for entry := &counts.extra; *entry != nil; entry = &(*entry).next {
		if (*entry).mask != mask {
			continue
		}
		(*entry).count += delta
		if (*entry).count == 0 {
			*entry = (*entry).next
		}
		return
	}
	if counts.primaryCount == 0 {
		counts.primaryMask = mask
		counts.primaryCount = delta
		return
	}
	counts.extra = &compactComponentMaskCount{mask: mask, count: delta, next: counts.extra}
}

func (counts compactComponentMaskCounts) clone() compactComponentMaskCounts {
	result := compactComponentMaskCounts{primaryMask: counts.primaryMask, primaryCount: counts.primaryCount}
	tail := &result.extra
	for entry := counts.extra; entry != nil; entry = entry.next {
		*tail = &compactComponentMaskCount{mask: entry.mask, count: entry.count}
		tail = &(*tail).next
	}
	return result
}

func (counts compactComponentMaskCounts) materialize() map[ComponentMask]int64 {
	result := make(map[ComponentMask]int64)
	if counts.primaryCount != 0 {
		result[counts.primaryMask] = counts.primaryCount
	}
	for entry := counts.extra; entry != nil; entry = entry.next {
		if entry.count != 0 {
			result[entry.mask] = entry.count
		}
	}
	return result
}

const compactPricingCounterCount = 11

const (
	compactPricingInputTokens = iota
	compactPricingOutputTokens
	compactPricingReasoningTokens
	compactPricingCacheReadTokens
	compactPricingCacheCreationTokens
	compactPricingUnclassifiedCacheTokens
	compactPricingPriceableDetailCount
	compactPricingUnknownUsageDetailCount
	compactPricingKnownTotalOnlyDetailCount
	compactPricingZeroBillableCompleteDetailCount
	compactPricingUnclassifiedCacheDetailCount
)

type compactPricingCounters struct {
	values       [compactPricingCounterCount]uint32
	overflowMask uint16
	overflow     *[compactPricingCounterCount]uint64
}

func (c *compactPricingCounters) add(index int, delta int64) {
	if c == nil || delta == 0 || index < 0 || index >= compactPricingCounterCount {
		return
	}
	current := c.value(index)
	next := current + delta
	if next >= 0 && next <= int64(^uint32(0)) {
		c.values[index] = uint32(next)
		if c.overflowMask&(1<<index) != 0 {
			c.overflowMask &^= 1 << index
			if c.overflow != nil {
				c.overflow[index] = 0
			}
		}
		return
	}
	if c.overflow == nil {
		c.overflow = &[compactPricingCounterCount]uint64{}
	}
	c.overflow[index] = uint64(next)
	c.overflowMask |= 1 << index
}

func (c compactPricingCounters) value(index int) int64 {
	if index < 0 || index >= compactPricingCounterCount {
		return 0
	}
	if c.overflowMask&(1<<index) != 0 && c.overflow != nil {
		return int64(c.overflow[index])
	}
	return int64(c.values[index])
}

func (c compactPricingCounters) clone() compactPricingCounters {
	result := c
	if c.overflow != nil {
		copyOfOverflow := *c.overflow
		result.overflow = &copyOfOverflow
	}
	return result
}

type compactPricingAggregate struct {
	counters            compactPricingCounters
	ComponentMaskCounts compactComponentMaskCounts
}

func (pricing compactPricingAggregate) billableTokens() BillableTokenComponents {
	return BillableTokenComponents{
		InputTokens:             pricing.counters.value(compactPricingInputTokens),
		OutputTokens:            pricing.counters.value(compactPricingOutputTokens),
		ReasoningTokens:         pricing.counters.value(compactPricingReasoningTokens),
		CacheReadTokens:         pricing.counters.value(compactPricingCacheReadTokens),
		CacheCreationTokens:     pricing.counters.value(compactPricingCacheCreationTokens),
		UnclassifiedCacheTokens: pricing.counters.value(compactPricingUnclassifiedCacheTokens),
	}
}

const compactPricingGroupInlineLimit = 32

type compactPricingGroupEntry struct {
	id      projectionPricingGroupID
	pricing compactPricingAggregate
}

type compactPricingGroupOverflow struct {
	additional     []compactPricingGroupEntry
	additionalByID map[projectionPricingGroupID]compactPricingAggregate
}

// compactPricingGroups stores the first group inline and keeps a short exact
// slice for the common sparse-rollup case. It promotes to a map only when a
// single dimension aggregate genuinely has many price groups, avoiding a Go
// map bucket allocation for every one-entry aggregate.
type compactPricingGroups struct {
	primaryID projectionPricingGroupID
	primary   compactPricingAggregate
	overflow  *compactPricingGroupOverflow
}

func (groups *compactPricingGroups) get(id projectionPricingGroupID) (compactPricingAggregate, bool) {
	if groups == nil || id == 0 {
		return compactPricingAggregate{}, false
	}
	if groups.primaryID == id {
		return groups.primary, true
	}
	if groups.overflow == nil {
		return compactPricingAggregate{}, false
	}
	if groups.overflow.additionalByID != nil {
		pricing, ok := groups.overflow.additionalByID[id]
		return pricing, ok
	}
	for _, entry := range groups.overflow.additional {
		if entry.id == id {
			return entry.pricing, true
		}
	}
	return compactPricingAggregate{}, false
}

func (groups *compactPricingGroups) set(id projectionPricingGroupID, pricing compactPricingAggregate) {
	if groups == nil || id == 0 {
		return
	}
	if groups.primaryID == 0 || groups.primaryID == id {
		groups.primaryID = id
		groups.primary = pricing
		return
	}
	if groups.overflow == nil {
		groups.overflow = &compactPricingGroupOverflow{
			additional: []compactPricingGroupEntry{{id: id, pricing: pricing}},
		}
		return
	}
	if groups.overflow.additionalByID != nil {
		groups.overflow.additionalByID[id] = pricing
		return
	}
	for index := range groups.overflow.additional {
		if groups.overflow.additional[index].id == id {
			groups.overflow.additional[index].pricing = pricing
			return
		}
	}
	groups.overflow.additional = append(groups.overflow.additional, compactPricingGroupEntry{id: id, pricing: pricing})
	if len(groups.overflow.additional) <= compactPricingGroupInlineLimit {
		return
	}
	groups.overflow.additionalByID = make(map[projectionPricingGroupID]compactPricingAggregate, len(groups.overflow.additional))
	for _, entry := range groups.overflow.additional {
		groups.overflow.additionalByID[entry.id] = entry.pricing
	}
	groups.overflow.additional = nil
}

func (groups *compactPricingGroups) delete(id projectionPricingGroupID) {
	if groups == nil || id == 0 {
		return
	}
	if groups.primaryID == id {
		if groups.overflow == nil {
			groups.primaryID = 0
			groups.primary = compactPricingAggregate{}
			return
		}
		if groups.overflow.additionalByID != nil {
			for nextID, nextPricing := range groups.overflow.additionalByID {
				groups.primaryID = nextID
				groups.primary = nextPricing
				delete(groups.overflow.additionalByID, nextID)
				break
			}
			groups.compactAdditionalMap()
			if groups.overflow != nil && len(groups.overflow.additional) == 0 {
				groups.overflow = nil
			}
			return
		}
		if len(groups.overflow.additional) == 0 {
			groups.primaryID = 0
			groups.primary = compactPricingAggregate{}
			groups.overflow = nil
			return
		}
		last := len(groups.overflow.additional) - 1
		groups.primaryID = groups.overflow.additional[last].id
		groups.primary = groups.overflow.additional[last].pricing
		groups.removeAdditionalAt(last)
		return
	}
	if groups.overflow == nil {
		return
	}
	if groups.overflow.additionalByID != nil {
		if _, ok := groups.overflow.additionalByID[id]; ok {
			delete(groups.overflow.additionalByID, id)
			groups.compactAdditionalMap()
		}
		return
	}
	for index := range groups.overflow.additional {
		if groups.overflow.additional[index].id == id {
			groups.removeAdditionalAt(index)
			return
		}
	}
}

func (groups *compactPricingGroups) removeAdditionalAt(index int) {
	if groups == nil || groups.overflow == nil || index < 0 || index >= len(groups.overflow.additional) {
		return
	}
	last := len(groups.overflow.additional) - 1
	if index != last {
		moved := groups.overflow.additional[last]
		groups.overflow.additional[index] = moved
	}
	groups.overflow.additional[last] = compactPricingGroupEntry{}
	groups.overflow.additional = groups.overflow.additional[:last]
	if len(groups.overflow.additional) == 0 {
		groups.overflow = nil
	}
}

func (groups *compactPricingGroups) compactAdditionalMap() {
	if groups == nil || groups.overflow == nil || len(groups.overflow.additionalByID) > compactPricingGroupInlineLimit {
		return
	}
	if len(groups.overflow.additionalByID) == 0 {
		groups.overflow = nil
		return
	}
	groups.overflow.additional = make([]compactPricingGroupEntry, 0, len(groups.overflow.additionalByID))
	for id, pricing := range groups.overflow.additionalByID {
		groups.overflow.additional = append(groups.overflow.additional, compactPricingGroupEntry{id: id, pricing: pricing})
	}
	groups.overflow.additionalByID = nil
}

func (groups compactPricingGroups) len() int {
	count := 0
	if groups.overflow != nil {
		if groups.overflow.additionalByID != nil {
			count = len(groups.overflow.additionalByID)
		} else {
			count = len(groups.overflow.additional)
		}
	}
	if groups.primaryID != 0 {
		count++
	}
	return count
}

func (groups compactPricingGroups) each(visit func(projectionPricingGroupID, compactPricingAggregate)) {
	if visit == nil {
		return
	}
	if groups.primaryID != 0 {
		visit(groups.primaryID, groups.primary)
	}
	if groups.overflow == nil {
		return
	}
	if groups.overflow.additionalByID != nil {
		for id, pricing := range groups.overflow.additionalByID {
			visit(id, pricing)
		}
		return
	}
	for _, entry := range groups.overflow.additional {
		visit(entry.id, entry.pricing)
	}
}

func (groups compactPricingGroups) clone() compactPricingGroups {
	result := compactPricingGroups{primaryID: groups.primaryID, primary: cloneCompactPricingAggregate(groups.primary)}
	if groups.overflow != nil {
		result.overflow = &compactPricingGroupOverflow{
			additional: make([]compactPricingGroupEntry, len(groups.overflow.additional)),
		}
		for index, entry := range groups.overflow.additional {
			result.overflow.additional[index] = compactPricingGroupEntry{id: entry.id, pricing: cloneCompactPricingAggregate(entry.pricing)}
		}
		if len(groups.overflow.additionalByID) > 0 {
			result.overflow.additionalByID = make(map[projectionPricingGroupID]compactPricingAggregate, len(groups.overflow.additionalByID))
			for id, pricing := range groups.overflow.additionalByID {
				result.overflow.additionalByID[id] = cloneCompactPricingAggregate(pricing)
			}
		}
	}
	return result
}

const compactAggregateCounterCount = 16

const (
	compactAggregateTotalRequests = iota
	compactAggregateSuccessCount
	compactAggregateFailureCount
	compactAggregateLatencyMsSum
	compactAggregateInputTokens
	compactAggregateOutputTokens
	compactAggregateReasoningTokens
	compactAggregateCachedTokens
	compactAggregateCacheReadTokens
	compactAggregateCacheCreationTokens
	compactAggregateTotalTokens
	compactAggregateDetailCount
	compactAggregateMissingUsageCount
	compactAggregateKnownUsageCount
	compactAggregateProviderUsageCount
	compactAggregateComputedUsageCount
)

type compactAggregateCounters struct {
	values       [compactAggregateCounterCount]uint32
	overflow     *[compactAggregateCounterCount]uint64
	overflowMask uint16
}

func (c *compactAggregateCounters) add(index int, delta int64) {
	if c == nil || delta == 0 || index < 0 || index >= compactAggregateCounterCount {
		return
	}
	current := c.value(index)
	next := current + delta
	if next >= 0 && next <= int64(^uint32(0)) {
		c.values[index] = uint32(next)
		if c.overflowMask&(1<<index) != 0 {
			c.overflowMask &^= 1 << index
			if c.overflow != nil {
				c.overflow[index] = 0
			}
		}
		return
	}
	if c.overflow == nil {
		c.overflow = &[compactAggregateCounterCount]uint64{}
	}
	c.overflow[index] = uint64(next)
	c.overflowMask |= 1 << index
}

func (c compactAggregateCounters) value(index int) int64 {
	if index < 0 || index >= compactAggregateCounterCount {
		return 0
	}
	if c.overflowMask&(1<<index) != 0 && c.overflow != nil {
		return int64(c.overflow[index])
	}
	return int64(c.values[index])
}

func (c compactAggregateCounters) clone() compactAggregateCounters {
	result := c
	if c.overflow != nil {
		copyOfOverflow := *c.overflow
		result.overflow = &copyOfOverflow
	}
	return result
}

type compactAggregate struct {
	counters compactAggregateCounters
}

type compactHourBucket struct {
	IntervalStart time.Time
	IntervalEnd   time.Time
	Totals        compactAggregate
	Facets        map[projectionStringID]int64
	APIs          map[projectionStringID]compactAggregate
	Models        map[projectionStringID]compactAggregate
	Providers     map[projectionStringID]compactAggregate
	Auths         map[projectionStringID]compactAggregate
	Sources       map[projectionStringID]compactAggregate
}

type projectionPostingDimension uint8

const (
	postingDimensionAll projectionPostingDimension = iota
	postingDimensionAPI
	postingDimensionModel
	postingDimensionProvider
	postingDimensionAuth
	postingDimensionSource
)

type projectionPostingKey struct {
	Dimension projectionPostingDimension
	Value     projectionStringID
}

func cloneCompactPricingAggregate(pricing compactPricingAggregate) compactPricingAggregate {
	result := pricing
	result.counters = pricing.counters.clone()
	result.ComponentMaskCounts = pricing.ComponentMaskCounts.clone()
	return result
}

func cloneCompactAggregate(aggregate compactAggregate) compactAggregate {
	result := aggregate
	result.counters = aggregate.counters.clone()
	return result
}

func cloneCompactAggregateMap(values map[projectionStringID]compactAggregate) map[projectionStringID]compactAggregate {
	if len(values) == 0 {
		return nil
	}
	result := make(map[projectionStringID]compactAggregate, len(values))
	for id, aggregate := range values {
		result[id] = cloneCompactAggregate(aggregate)
	}
	return result
}

func cloneProjectionFacets(values map[projectionStringID]int64) map[projectionStringID]int64 {
	if len(values) == 0 {
		return nil
	}
	result := make(map[projectionStringID]int64, len(values))
	for id, count := range values {
		result[id] = count
	}
	return result
}

func cloneCompactHourBucket(bucket compactHourBucket) compactHourBucket {
	result := bucket
	result.Totals = cloneCompactAggregate(bucket.Totals)
	if len(bucket.Facets) > 0 {
		result.Facets = make(map[projectionStringID]int64, len(bucket.Facets))
		for id, count := range bucket.Facets {
			result.Facets[id] = count
		}
	}
	result.APIs = cloneCompactAggregateMap(bucket.APIs)
	result.Models = cloneCompactAggregateMap(bucket.Models)
	result.Providers = cloneCompactAggregateMap(bucket.Providers)
	result.Auths = cloneCompactAggregateMap(bucket.Auths)
	result.Sources = cloneCompactAggregateMap(bucket.Sources)
	return result
}

func (projection *UsageProjection) materializeAggregate(aggregate compactAggregate, facets map[projectionStringID]int64) Aggregate {
	result := Aggregate{
		TotalRequests: aggregate.counters.value(compactAggregateTotalRequests),
		SuccessCount:  aggregate.counters.value(compactAggregateSuccessCount),
		FailureCount:  aggregate.counters.value(compactAggregateFailureCount),
		LatencyMsSum:  aggregate.counters.value(compactAggregateLatencyMsSum),
		Tokens: TokenFactsAggregate{
			InputTokens:         aggregate.counters.value(compactAggregateInputTokens),
			OutputTokens:        aggregate.counters.value(compactAggregateOutputTokens),
			ReasoningTokens:     aggregate.counters.value(compactAggregateReasoningTokens),
			CachedTokens:        aggregate.counters.value(compactAggregateCachedTokens),
			CacheReadTokens:     aggregate.counters.value(compactAggregateCacheReadTokens),
			CacheCreationTokens: aggregate.counters.value(compactAggregateCacheCreationTokens),
			TotalTokens:         aggregate.counters.value(compactAggregateTotalTokens),
		},
		TokenCoverage: TokenCoverageAggregate{
			DetailCount:        aggregate.counters.value(compactAggregateDetailCount),
			MissingUsageCount:  aggregate.counters.value(compactAggregateMissingUsageCount),
			KnownUsageCount:    aggregate.counters.value(compactAggregateKnownUsageCount),
			ProviderUsageCount: aggregate.counters.value(compactAggregateProviderUsageCount),
			ComputedUsageCount: aggregate.counters.value(compactAggregateComputedUsageCount),
		},
		PricingGroups: make(map[string]PricingAggregate),
		Facets:        make(map[string]int64, len(facets)),
	}
	for id, count := range facets {
		if value := projection.stringRegistry.value(id); value != "" {
			result.Facets[value] = count
		}
	}
	return result
}

func (projection *UsageProjection) materializeAggregateMap(values map[projectionStringID]compactAggregate) map[string]Aggregate {
	result := make(map[string]Aggregate, len(values))
	for id, aggregate := range values {
		if value := projection.stringRegistry.value(id); value != "" {
			result[value] = projection.materializeAggregate(aggregate, nil)
		}
	}
	return result
}

func (projection *UsageProjection) materializeHourBucket(bucket compactHourBucket) HourBucket {
	return HourBucket{
		IntervalStart: bucket.IntervalStart,
		IntervalEnd:   bucket.IntervalEnd,
		Totals:        projection.materializeAggregate(bucket.Totals, bucket.Facets),
		APIs:          projection.materializeAggregateMap(bucket.APIs),
		Models:        projection.materializeAggregateMap(bucket.Models),
		Providers:     projection.materializeAggregateMap(bucket.Providers),
		Auths:         projection.materializeAggregateMap(bucket.Auths),
		Sources:       projection.materializeAggregateMap(bucket.Sources),
	}
}

func (projection *UsageProjection) postingKeyString(key projectionPostingKey) string {
	value := projection.stringRegistry.value(key.Value)
	switch key.Dimension {
	case postingDimensionAll:
		return "all"
	case postingDimensionAPI:
		return "api:" + value
	case postingDimensionModel:
		return "model:" + value
	case postingDimensionProvider:
		return "provider:" + value
	case postingDimensionAuth:
		return "auth:" + value
	case postingDimensionSource:
		return "source:" + value
	default:
		return ""
	}
}
