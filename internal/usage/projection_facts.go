package usage

import "time"

type projectionFactRowID uint32

const (
	factTokenInput = iota
	factTokenOutput
	factTokenReasoning
	factTokenCached
	factTokenCacheRead
	factTokenCacheCreation
	factTokenTotal
	factTokenColumnCount
)

const (
	factBillableInput = iota
	factBillableOutput
	factBillableReasoning
	factBillableCacheRead
	factBillableCacheCreation
	factBillableUnclassifiedCache
	factBillableColumnCount
)

const (
	factUsageSourceUnknown uint8 = iota
	factUsageSourceProvider
	factUsageSourceMissing
	factUsageSourceComputed
)

const (
	factClassificationUnknown uint8 = iota
	factClassificationPriceable
	factClassificationUnknownUsage
	factClassificationKnownTotalOnly
	factClassificationZeroBillableComplete
)

// usageFactStoreV1 keeps event facts in generation-local columns. String
// values are registry ids; row ids are stable within a generation and postings
// never retain canonical detail bytes.
type usageFactStoreV1 struct {
	timestampSeconds         []int64
	timestampNanoseconds     []int32
	timestampPresent         []uint8
	sequences                []uint64
	batchOrdinals            []uint64
	versions                 []uint64
	latencyMs                []int64
	apiIDs                   []projectionStringID
	modelIDs                 []projectionStringID
	providerIDs              []projectionStringID
	authIDs                  []projectionStringID
	sourceIDs                []projectionStringID
	priceKeyIDs              []projectionStringID
	providerStateIDs         []projectionStringID
	facetIDs                 [5][]projectionStringID
	tokenColumns             [factTokenColumnCount][]int64
	billableColumns          [factBillableColumnCount][]int64
	failed                   []uint8
	usageSources             []uint8
	classifications          []uint8
	componentMasks           []ComponentMask
	stableEventIDs           []string
	canonicalEventIdentities []string
	activeBits               []uint64
	activeCount              uint64
	retainedStringBytes      uint64
}

func (store *usageFactStoreV1) append(contribution projectionContribution) projectionFactRowID {
	if store == nil {
		return 0
	}
	index := len(store.timestampSeconds)
	timestamp := contribution.Timestamp.UTC()
	if timestamp.IsZero() {
		store.timestampSeconds = append(store.timestampSeconds, 0)
		store.timestampNanoseconds = append(store.timestampNanoseconds, 0)
		store.timestampPresent = append(store.timestampPresent, 0)
	} else {
		store.timestampSeconds = append(store.timestampSeconds, timestamp.Unix())
		store.timestampNanoseconds = append(store.timestampNanoseconds, int32(timestamp.Nanosecond()))
		store.timestampPresent = append(store.timestampPresent, 1)
	}
	store.sequences = append(store.sequences, contribution.EventRef.Sequence)
	store.batchOrdinals = append(store.batchOrdinals, contribution.EventRef.BatchOrdinal)
	store.versions = append(store.versions, contribution.EventRef.Version)
	store.latencyMs = append(store.latencyMs, contribution.LatencyMs)
	store.apiIDs = append(store.apiIDs, contribution.APIID)
	store.modelIDs = append(store.modelIDs, contribution.ModelID)
	store.providerIDs = append(store.providerIDs, contribution.ProviderID)
	store.authIDs = append(store.authIDs, contribution.AuthID)
	store.sourceIDs = append(store.sourceIDs, contribution.SourceIDValue)
	store.priceKeyIDs = append(store.priceKeyIDs, contribution.PriceKeyID)
	store.providerStateIDs = append(store.providerStateIDs, contribution.ProviderStateID)
	for column := range store.facetIDs {
		store.facetIDs[column] = append(store.facetIDs[column], contribution.FacetIDs[column])
	}
	tokens := [...]int64{
		contribution.Tokens.InputTokens,
		contribution.Tokens.OutputTokens,
		contribution.Tokens.ReasoningTokens,
		contribution.Tokens.CachedTokens,
		contribution.Tokens.CacheReadTokens,
		contribution.Tokens.CacheCreationTokens,
		contribution.Tokens.TotalTokens,
	}
	for column := range store.tokenColumns {
		store.tokenColumns[column] = append(store.tokenColumns[column], tokens[column])
	}
	billable := [...]int64{
		contribution.Billable.InputTokens,
		contribution.Billable.OutputTokens,
		contribution.Billable.ReasoningTokens,
		contribution.Billable.CacheReadTokens,
		contribution.Billable.CacheCreationTokens,
		contribution.Billable.UnclassifiedCacheTokens,
	}
	for column := range store.billableColumns {
		store.billableColumns[column] = append(store.billableColumns[column], billable[column])
	}
	store.failed = append(store.failed, boolByte(contribution.Failed))
	store.usageSources = append(store.usageSources, encodeFactUsageSource(contribution.Tokens.TokenUsageSource))
	store.classifications = append(store.classifications, encodeFactClassification(contribution.Classification))
	store.componentMasks = append(store.componentMasks, contribution.ComponentMask)
	store.stableEventIDs = append(store.stableEventIDs, contribution.EventRef.StableEventID)
	store.canonicalEventIdentities = append(store.canonicalEventIdentities, contribution.EventRef.CanonicalEventIdentity)
	store.retainedStringBytes = saturatingAddUint64(store.retainedStringBytes, uint64(len(contribution.EventRef.StableEventID))+projectionStringHeaderBytes)
	store.retainedStringBytes = saturatingAddUint64(store.retainedStringBytes, uint64(len(contribution.EventRef.CanonicalEventIdentity))+projectionStringHeaderBytes)
	store.setActive(index, true)
	return projectionFactRowID(index + 1)
}

func (store *usageFactStoreV1) contribution(rowID projectionFactRowID, registry projectionStringRegistry) (projectionContribution, bool) {
	index, ok := store.index(rowID)
	if !ok {
		return projectionContribution{}, false
	}
	timestamp, _ := store.timestamp(rowID)
	apiName := registry.value(store.apiIDs[index])
	model := registry.value(store.modelIDs[index])
	provider := registry.value(store.providerIDs[index])
	authIndex := registry.value(store.authIDs[index])
	sourceID := registry.value(store.sourceIDs[index])
	priceKey := registry.value(store.priceKeyIDs[index])
	providerState := registry.value(store.providerStateIDs[index])
	facets := [5]projectionStringID{}
	for column := range facets {
		facets[column] = store.facetIDs[column][index]
	}
	tokens := RequestTokenStats{
		InputTokens:         store.tokenColumns[factTokenInput][index],
		OutputTokens:        store.tokenColumns[factTokenOutput][index],
		ReasoningTokens:     store.tokenColumns[factTokenReasoning][index],
		CachedTokens:        store.tokenColumns[factTokenCached][index],
		CacheReadTokens:     store.tokenColumns[factTokenCacheRead][index],
		CacheCreationTokens: store.tokenColumns[factTokenCacheCreation][index],
		TotalTokens:         store.tokenColumns[factTokenTotal][index],
		TokenUsageSource:    decodeFactUsageSource(store.usageSources[index]),
	}
	billable := BillableTokenComponents{
		InputTokens:             store.billableColumns[factBillableInput][index],
		OutputTokens:            store.billableColumns[factBillableOutput][index],
		ReasoningTokens:         store.billableColumns[factBillableReasoning][index],
		CacheReadTokens:         store.billableColumns[factBillableCacheRead][index],
		CacheCreationTokens:     store.billableColumns[factBillableCacheCreation][index],
		UnclassifiedCacheTokens: store.billableColumns[factBillableUnclassifiedCache][index],
	}
	failed := store.failed[index] != 0
	return projectionContribution{
		API:             apiName,
		Model:           model,
		Provider:        provider,
		AuthIndex:       authIndex,
		SourceID:        sourceID,
		PriceKey:        priceKey,
		ProviderState:   providerState,
		Timestamp:       timestamp,
		Failed:          failed,
		LatencyMs:       store.latencyMs[index],
		Tokens:          tokens,
		Billable:        billable,
		Classification:  decodeFactClassification(store.classifications[index]),
		ComponentMask:   store.componentMasks[index],
		APIID:           store.apiIDs[index],
		ModelID:         store.modelIDs[index],
		ProviderID:      store.providerIDs[index],
		AuthID:          store.authIDs[index],
		SourceIDValue:   store.sourceIDs[index],
		PriceKeyID:      store.priceKeyIDs[index],
		ProviderStateID: store.providerStateIDs[index],
		FacetIDs:        facets,
		EventRef: EventRef{
			StableEventID:          store.stableEventIDs[index],
			CanonicalEventIdentity: store.canonicalEventIdentities[index],
			Sequence:               store.sequences[index],
			BatchOrdinal:           store.batchOrdinals[index],
			Version:                store.versions[index],
			Timestamp:              timestamp,
			API:                    apiName,
			Model:                  model,
			Provider:               provider,
			AuthIndex:              authIndex,
			SourceID:               sourceID,
			Failed:                 failed,
		},
	}, true
}

func (store usageFactStoreV1) timestamp(rowID projectionFactRowID) (time.Time, bool) {
	index, ok := store.index(rowID)
	if !ok {
		return time.Time{}, false
	}
	if store.timestampPresent[index] == 0 {
		return time.Time{}, true
	}
	return time.Unix(store.timestampSeconds[index], int64(store.timestampNanoseconds[index])).UTC(), true
}

func (store *usageFactStoreV1) isActive(rowID projectionFactRowID) bool {
	index, ok := store.index(rowID)
	if !ok {
		return false
	}
	word := index / 64
	bit := uint(index % 64)
	return word < len(store.activeBits) && store.activeBits[word]&(uint64(1)<<bit) != 0
}

func (store *usageFactStoreV1) sequence(rowID projectionFactRowID) uint64 {
	index, ok := store.index(rowID)
	if !ok {
		return 0
	}
	return store.sequences[index]
}

func (store *usageFactStoreV1) tombstone(rowID projectionFactRowID) bool {
	index, ok := store.index(rowID)
	if !ok || !store.isActive(rowID) {
		return false
	}
	store.setActive(index, false)
	return true
}

func (store *usageFactStoreV1) setActive(index int, active bool) {
	word := index / 64
	for len(store.activeBits) <= word {
		store.activeBits = append(store.activeBits, 0)
	}
	mask := uint64(1) << uint(index%64)
	wasActive := store.activeBits[word]&mask != 0
	if active {
		store.activeBits[word] |= mask
		if !wasActive {
			store.activeCount++
		}
		return
	}
	store.activeBits[word] &^= mask
	if wasActive {
		store.activeCount--
	}
}

func (store *usageFactStoreV1) index(rowID projectionFactRowID) (int, bool) {
	if store == nil || rowID == 0 {
		return 0, false
	}
	index := int(rowID - 1)
	return index, index >= 0 && index < len(store.timestampSeconds)
}

func (store *usageFactStoreV1) eachActive(visit func(projectionFactRowID)) {
	if store == nil || visit == nil {
		return
	}
	for index := range store.timestampSeconds {
		rowID := projectionFactRowID(index + 1)
		if store.isActive(rowID) {
			visit(rowID)
		}
	}
}

func (store usageFactStoreV1) clone() usageFactStoreV1 {
	result := usageFactStoreV1{
		timestampSeconds:         append([]int64(nil), store.timestampSeconds...),
		timestampNanoseconds:     append([]int32(nil), store.timestampNanoseconds...),
		timestampPresent:         append([]uint8(nil), store.timestampPresent...),
		sequences:                append([]uint64(nil), store.sequences...),
		batchOrdinals:            append([]uint64(nil), store.batchOrdinals...),
		versions:                 append([]uint64(nil), store.versions...),
		latencyMs:                append([]int64(nil), store.latencyMs...),
		apiIDs:                   append([]projectionStringID(nil), store.apiIDs...),
		modelIDs:                 append([]projectionStringID(nil), store.modelIDs...),
		providerIDs:              append([]projectionStringID(nil), store.providerIDs...),
		authIDs:                  append([]projectionStringID(nil), store.authIDs...),
		sourceIDs:                append([]projectionStringID(nil), store.sourceIDs...),
		priceKeyIDs:              append([]projectionStringID(nil), store.priceKeyIDs...),
		providerStateIDs:         append([]projectionStringID(nil), store.providerStateIDs...),
		failed:                   append([]uint8(nil), store.failed...),
		usageSources:             append([]uint8(nil), store.usageSources...),
		classifications:          append([]uint8(nil), store.classifications...),
		componentMasks:           append([]ComponentMask(nil), store.componentMasks...),
		stableEventIDs:           append([]string(nil), store.stableEventIDs...),
		canonicalEventIdentities: append([]string(nil), store.canonicalEventIdentities...),
		activeBits:               append([]uint64(nil), store.activeBits...),
		activeCount:              store.activeCount,
		retainedStringBytes:      store.retainedStringBytes,
	}
	for column := range store.facetIDs {
		result.facetIDs[column] = append([]projectionStringID(nil), store.facetIDs[column]...)
	}
	for column := range store.tokenColumns {
		result.tokenColumns[column] = append([]int64(nil), store.tokenColumns[column]...)
	}
	for column := range store.billableColumns {
		result.billableColumns[column] = append([]int64(nil), store.billableColumns[column]...)
	}
	return result
}

func boolByte(value bool) uint8 {
	if value {
		return 1
	}
	return 0
}

func encodeFactUsageSource(value string) uint8 {
	switch value {
	case TokenUsageSourceProvider:
		return factUsageSourceProvider
	case TokenUsageSourceMissing:
		return factUsageSourceMissing
	case "":
		return factUsageSourceUnknown
	default:
		return factUsageSourceComputed
	}
}

func decodeFactUsageSource(value uint8) string {
	switch value {
	case factUsageSourceProvider:
		return TokenUsageSourceProvider
	case factUsageSourceMissing:
		return TokenUsageSourceMissing
	case factUsageSourceComputed:
		return "computed_usage"
	default:
		return ""
	}
}

func encodeFactClassification(value DetailClassification) uint8 {
	switch value {
	case DetailClassPriceable:
		return factClassificationPriceable
	case DetailClassUnknownUsage:
		return factClassificationUnknownUsage
	case DetailClassKnownTotalOnly:
		return factClassificationKnownTotalOnly
	case DetailClassZeroBillableComplete:
		return factClassificationZeroBillableComplete
	default:
		return factClassificationUnknown
	}
}

func decodeFactClassification(value uint8) DetailClassification {
	switch value {
	case factClassificationPriceable:
		return DetailClassPriceable
	case factClassificationUnknownUsage:
		return DetailClassUnknownUsage
	case factClassificationKnownTotalOnly:
		return DetailClassKnownTotalOnly
	case factClassificationZeroBillableComplete:
		return DetailClassZeroBillableComplete
	default:
		return ""
	}
}
