package usage

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
	"sync"
	"time"
)

const projectionSchemaVersion = "usage-projection-v1"

type projectionIndexKey struct {
	Kind   uint8
	Digest [sha256.Size]byte
}

const (
	projectionIndexCanonical uint8 = iota + 1
	projectionIndexCoordinate
	projectionIndexLogicalLookup
	projectionIndexRequestLookup
)

func newProjectionIndexKey(kind uint8, value string) projectionIndexKey {
	return projectionIndexKey{Kind: kind, Digest: sha256.Sum256([]byte(value))}
}

func newProjectionIndexKeyFromFields(kind uint8, domain string, fields ...string) projectionIndexKey {
	encodedSize := 8 + len(domain)
	for _, field := range fields {
		encodedSize += 8 + len(field)
	}
	encoded := make([]byte, 0, encodedSize)
	encoded = appendLengthPrefixedString(encoded, domain)
	for _, field := range fields {
		encoded = appendLengthPrefixedString(encoded, field)
	}
	return projectionIndexKey{Kind: kind, Digest: sha256.Sum256(encoded)}
}

func appendLengthPrefixedString(dst []byte, value string) []byte {
	var length [8]byte
	putBigEndianUint64(length[:], uint64(len(value)))
	dst = append(dst, length[:]...)
	return append(dst, value...)
}

// UsageProjection is the in-process immutable-event projection owned by a
// RequestStatistics instance. Its public methods return value copies so query
// callers cannot retain mutable internal state.
type UsageProjection struct {
	mu sync.RWMutex

	datasetEpoch    uint64
	revision        uint64
	rewriteRevision uint64
	nextSequence    uint64
	budget          ProjectionBudgetV2
	rollupCells     uint64

	stringRegistry projectionStringRegistry
	facts          usageFactStoreV1
	totals         compactAggregate
	facets         map[projectionStringID]int64
	hours          map[string]compactHourBucket
	days           map[string]compactHourBucket
	weeks          map[string]compactHourBucket
	months         map[string]compactHourBucket
	years          map[string]compactHourBucket
	health         map[string]HealthBucket
	postings       postingRegistryV1
	timeIndex      projectionTimeIndexV1
	eventPageCache projectionEventPageCache
	catalog        ProjectionCatalog
	events         map[string]projectionEvent
	canonical      map[projectionIndexKey]string
	coordinate     map[projectionIndexKey]string
	lookup         map[projectionIndexKey]string
	ordinals       map[string]uint64
	exportDetails  map[string]RequestDetail
}

// NewUsageProjection constructs an empty projection generation.
func NewUsageProjection() *UsageProjection {
	return &UsageProjection{
		datasetEpoch:   1,
		nextSequence:   1,
		stringRegistry: newProjectionStringRegistry(),
		budget:         DefaultProjectionBudgetV2(),
		rollupCells:    1,
		facets:         make(map[projectionStringID]int64),
		hours:          make(map[string]compactHourBucket),
		days:           make(map[string]compactHourBucket),
		weeks:          make(map[string]compactHourBucket),
		months:         make(map[string]compactHourBucket),
		years:          make(map[string]compactHourBucket),
		health:         make(map[string]HealthBucket),
		postings:       newPostingRegistryV1(),
		timeIndex:      newProjectionTimeIndexV1(),
		catalog: ProjectionCatalog{
			Models:    make(map[string]CatalogEntry),
			PriceKeys: make(map[string]CatalogEntry),
			Sources:   make(map[string]CatalogEntry),
		},
		events:        make(map[string]projectionEvent),
		canonical:     make(map[projectionIndexKey]string),
		coordinate:    make(map[projectionIndexKey]string),
		lookup:        make(map[projectionIndexKey]string),
		ordinals:      make(map[string]uint64),
		exportDetails: make(map[string]RequestDetail),
	}
}

func (p *UsageProjection) metadata() (datasetEpoch, revision, rewriteRevision uint64) {
	if p == nil {
		return 0, 0, 0
	}
	p.mu.RLock()
	datasetEpoch = p.datasetEpoch
	revision = p.revision
	rewriteRevision = p.rewriteRevision
	p.mu.RUnlock()
	return datasetEpoch, revision, rewriteRevision
}

// ApplyDetail adds a new immutable event version or replaces the current live
// version for the same logical event. Enrichment revokes the old contribution
// before applying the new one, so all aggregate and posting-list views remain
// mergeable and internally consistent.
func (p *UsageProjection) ApplyDetail(apiName string, detail RequestDetail) ProjectionMutationResult {
	return p.ApplyDetailWithIdentity(apiName, detail, ProjectionIdentity{})
}

// ApplyDetailWithIdentity applies a detail using coordinator-assigned
// immutable identity metadata when present. Enrichment always keeps the
// existing event identity and sequence.
func (p *UsageProjection) ApplyDetailWithIdentity(apiName string, detail RequestDetail, identity ProjectionIdentity) ProjectionMutationResult {
	result := ProjectionMutationResult{}
	if p == nil {
		return result
	}
	detail = normalizeRequestDetailPreserveTimestamp(detail, detail.Provider)
	apiName = safeImportedAPIName(strings.TrimSpace(apiName), detail)
	if apiName == "" {
		apiName = "unknown"
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	logicalIdentity := detailIdentityKey(apiName, detail.Model, detail)
	explicitIdentity := strings.TrimSpace(identity.CanonicalIdentitySeed) != "" ||
		strings.TrimSpace(identity.CanonicalEventIdentity) != "" ||
		strings.TrimSpace(identity.StableEventID) != "" || identity.Sequence != 0 || identity.BatchOrdinal != 0
	groupKey := identity.SourceGroupKey
	if groupKey == "" {
		groupKey = SourceGroupKeyV1(detail)
	}
	ordinal := identity.SourceGroupOrdinal
	if identity.CanonicalIdentitySeed == "" {
		ordinal = p.ordinals[groupKey]
		p.ordinals[groupKey] = ordinal + 1
	} else if next := ordinal + 1; p.ordinals[groupKey] < next {
		p.ordinals[groupKey] = next
	}
	seed := strings.TrimSpace(identity.CanonicalIdentitySeed)
	if seed == "" {
		seed = CanonicalIdentitySeedV1(detail, ordinal)
	}
	canonicalIdentity := strings.TrimSpace(identity.CanonicalEventIdentity)
	if canonicalIdentity == "" {
		canonicalIdentity = CanonicalEventIdentityV1(seed)
	}
	requestedStableEventID := strings.TrimSpace(identity.StableEventID)
	if requestedStableEventID == "" {
		requestedStableEventID = StableEventIDV1(canonicalIdentity)
	}
	stableEventID := requestedStableEventID
	physicalCollision := false
	if existingID := p.canonical[newProjectionIndexKey(projectionIndexCanonical, canonicalIdentity)]; existingID != "" {
		stableEventID = existingID
	}
	if _, ok := p.events[stableEventID]; !ok && !explicitIdentity {
		if lookupID := p.lookup[logicalLookupIndexKey(logicalIdentity)]; lookupID != "" {
			stableEventID = lookupID
		} else if lookupID := p.lookup[requestLookupIndexKey(apiName, detail)]; lookupID != "" {
			stableEventID = lookupID
		}
	}
	if stableEventID != "" {
		if event, ok := p.events[stableEventID]; ok {
			if explicitIdentity && event.CanonicalEventIdentity != canonicalIdentity {
				physicalCollision = true
				stableEventID = collisionQualifiedStableEventID(requestedStableEventID, canonicalIdentity)
				for {
					if _, collision := p.events[stableEventID]; !collision {
						break
					}
					stableEventID = collisionQualifiedStableEventID(stableEventID, canonicalIdentity)
				}
			} else {
				canonicalHash := canonicalDetailHash(detail)
				if event.CanonicalDetailHash == canonicalHash {
					result.StableEventID = event.StableEventID
					result.CanonicalEventIdentity = event.CanonicalEventIdentity
					result.Version = event.Version
					result.Skipped = true
					result.RewriteRevision = p.rewriteRevision
					return result
				}
				oldContribution, ok := p.facts.contribution(event.FactRowID, p.stringRegistry)
				if !ok {
					result.Skipped = true
					result.RewriteRevision = p.rewriteRevision
					return result
				}
				p.revokeContributionLocked(event.FactRowID, oldContribution)
				ref := oldContribution.EventRef
				if explicitIdentity {
					ref.BatchOrdinal = identity.BatchOrdinal
				}
				contribution := p.buildContribution(detail, apiName, ref, event.Version+1)
				event.Version++
				contribution.EventRef.Version = event.Version
				event.FactRowID = p.applyContributionLocked(contribution)
				event.CanonicalDetailHash = canonicalHash
				p.events[stableEventID] = event
				p.exportDetails[stableEventID] = cloneRequestDetail(detail)
				p.coordinate[coordinateIndexKey(apiName, detail)] = stableEventID
				p.registerLookupLocked(apiName, detail, logicalIdentity, stableEventID)
				p.revision++
				p.rewriteRevision++
				result.StableEventID = stableEventID
				result.CanonicalEventIdentity = event.CanonicalEventIdentity
				result.Version = event.Version
				result.Enriched = true
				result.RewriteRevision = p.rewriteRevision
				return result
			}
		}
	}
	if _, collision := p.events[stableEventID]; collision {
		stableEventID = collisionQualifiedStableEventID(stableEventID, canonicalIdentity)
		for {
			if _, collision = p.events[stableEventID]; !collision {
				break
			}
			stableEventID = collisionQualifiedStableEventID(stableEventID, canonicalIdentity)
		}
	}
	sequence := identity.Sequence
	if sequence == 0 {
		sequence = p.nextSequence
	}
	if p.nextSequence <= sequence {
		p.nextSequence = sequence + 1
	}
	ref := EventRef{
		StableEventID:          stableEventID,
		CanonicalEventIdentity: canonicalIdentity,
		Sequence:               sequence,
		BatchOrdinal:           identity.BatchOrdinal,
		Version:                1,
		Timestamp:              detail.Timestamp.UTC(),
		API:                    apiName,
		Model:                  detail.Model,
		Provider:               detail.Provider,
		AuthIndex:              normalizedIdentityPart(detail.AuthIndex),
		SourceID:               UsageSourceIDV1(detail),
		Failed:                 detail.Failed,
	}
	contribution := p.buildContribution(detail, apiName, ref, 1)
	factRowID := p.applyContributionLocked(contribution)
	p.events[stableEventID] = projectionEvent{
		CanonicalIdentitySeed:  seed,
		CanonicalEventIdentity: canonicalIdentity,
		StableEventID:          stableEventID,
		SourceGroupKey:         groupKey,
		SourceGroupOrdinal:     ordinal,
		CanonicalDetailHash:    canonicalDetailHash(detail),
		Version:                1,
		FactRowID:              factRowID,
	}
	p.exportDetails[stableEventID] = cloneRequestDetail(detail)
	p.canonical[newProjectionIndexKey(projectionIndexCanonical, canonicalIdentity)] = stableEventID
	p.coordinate[coordinateIndexKey(apiName, detail)] = stableEventID
	p.registerLookupLocked(apiName, detail, logicalIdentity, stableEventID)
	p.revision++
	if physicalCollision {
		p.rewriteRevision++
	}
	result.StableEventID = stableEventID
	result.CanonicalEventIdentity = canonicalIdentity
	result.Version = 1
	result.Added = true
	result.RewriteRevision = p.rewriteRevision
	return result
}

// ExistingLogicalIdentity returns the immutable identity for an exact logical
// request identity. It does not use the mutable coordinate fallback, so an
// unseeded import cannot rekey an unrelated event that shares its coordinate.
func (p *UsageProjection) ExistingLogicalIdentity(apiName string, detail RequestDetail) (ProjectionIdentity, bool) {
	if p == nil {
		return ProjectionIdentity{}, false
	}
	detail = normalizeRequestDetailPreserveTimestamp(detail, detail.Provider)
	apiName = safeImportedAPIName(strings.TrimSpace(apiName), detail)
	p.mu.RLock()
	defer p.mu.RUnlock()
	logicalIdentity := detailIdentityKey(apiName, detail.Model, detail)
	stableEventID := p.lookup[logicalLookupIndexKey(logicalIdentity)]
	if stableEventID == "" {
		stableEventID = p.lookup[requestLookupIndexKey(apiName, detail)]
	}
	return p.identityForStableEventIDLocked(stableEventID)
}

// existingExactLogicalIdentity excludes coordinate and request-id fallbacks so
// restored legacy admission cannot collapse distinct API buckets or rekeys.
func (p *UsageProjection) existingExactLogicalIdentity(apiName string, detail RequestDetail) (ProjectionIdentity, bool) {
	if p == nil {
		return ProjectionIdentity{}, false
	}
	detail = normalizeRequestDetailPreserveTimestamp(detail, detail.Provider)
	apiName = safeImportedAPIName(strings.TrimSpace(apiName), detail)
	p.mu.RLock()
	defer p.mu.RUnlock()
	logicalIdentity := detailIdentityKey(apiName, detail.Model, detail)
	return p.identityForStableEventIDLocked(p.lookup[logicalLookupIndexKey(logicalIdentity)])
}

// ExistingIdentity returns the immutable identity currently associated with a
// logical request coordinate. It is used by admission to carry the same event
// identity across an enrichment record instead of allocating a new event.
func (p *UsageProjection) ExistingIdentity(apiName string, detail RequestDetail) (ProjectionIdentity, bool) {
	if p == nil {
		return ProjectionIdentity{}, false
	}
	detail = normalizeRequestDetailPreserveTimestamp(detail, detail.Provider)
	apiName = safeImportedAPIName(strings.TrimSpace(apiName), detail)
	p.mu.RLock()
	defer p.mu.RUnlock()
	logicalIdentity := detailIdentityKey(apiName, detail.Model, detail)
	stableEventID := p.lookup[logicalLookupIndexKey(logicalIdentity)]
	if stableEventID == "" {
		stableEventID = p.coordinate[coordinateIndexKey(apiName, detail)]
	}
	if stableEventID == "" {
		stableEventID = p.lookup[requestLookupIndexKey(apiName, detail)]
	}
	return p.identityForStableEventIDLocked(stableEventID)
}

// lookupExportDetails resolves rows from the projection-owned canonical detail
// arena. The arena belongs to one immutable generation and is never shared
// with live RequestStatistics detail locations.
func (p *UsageProjection) lookupExportDetails(events []EventRef) (map[string]RequestDetail, error) {
	if p == nil {
		return nil, ErrProjectionUnavailable
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	result := make(map[string]RequestDetail, len(events))
	for _, event := range events {
		stableEventID := strings.TrimSpace(event.StableEventID)
		if stableEventID == "" {
			return nil, ErrUsageExportGenerationChanged
		}
		current, ok := p.events[stableEventID]
		if !ok || current.CanonicalEventIdentity != event.CanonicalEventIdentity || current.Version != event.Version {
			return nil, ErrUsageExportGenerationChanged
		}
		detail, ok := p.exportDetails[stableEventID]
		if !ok || canonicalDetailHash(detail) != current.CanonicalDetailHash || UsageSourceIDV1(detail) != event.SourceID {
			return nil, ErrUsageExportGenerationChanged
		}
		result[stableEventID] = cloneRequestDetail(detail)
	}
	return result, nil
}

func (p *UsageProjection) identityForStableEventIDLocked(stableEventID string) (ProjectionIdentity, bool) {
	event, ok := p.events[stableEventID]
	if !ok {
		return ProjectionIdentity{}, false
	}
	return ProjectionIdentity{
		CanonicalIdentitySeed:  event.CanonicalIdentitySeed,
		CanonicalEventIdentity: event.CanonicalEventIdentity,
		StableEventID:          event.StableEventID,
		Sequence:               p.factSequenceLocked(event.FactRowID),
		BatchOrdinal:           p.factBatchOrdinalLocked(event.FactRowID),
		SourceGroupKey:         event.SourceGroupKey,
		SourceGroupOrdinal:     event.SourceGroupOrdinal,
	}, true
}

func (p *UsageProjection) factSequenceLocked(rowID projectionFactRowID) uint64 {
	return p.facts.sequence(rowID)
}

func (p *UsageProjection) factBatchOrdinalLocked(rowID projectionFactRowID) uint64 {
	index, ok := p.facts.index(rowID)
	if !ok {
		return 0
	}
	return p.facts.batchOrdinals[index]
}

// RemoveEvent revokes the current contribution for a stable event id. It is a
// projection primitive for the later mutation/rekey coordinator.
func (p *UsageProjection) RemoveEvent(stableEventID string) bool {
	if p == nil {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	event, ok := p.events[strings.TrimSpace(stableEventID)]
	if !ok {
		return false
	}
	contribution, ok := p.facts.contribution(event.FactRowID, p.stringRegistry)
	if !ok {
		return false
	}
	p.revokeContributionLocked(event.FactRowID, contribution)
	delete(p.events, event.StableEventID)
	delete(p.exportDetails, event.StableEventID)
	canonicalKey := newProjectionIndexKey(projectionIndexCanonical, event.CanonicalEventIdentity)
	if p.canonical[canonicalKey] == event.StableEventID {
		delete(p.canonical, canonicalKey)
	}
	for key, stableEventID := range p.lookup {
		if stableEventID == event.StableEventID {
			delete(p.lookup, key)
		}
	}
	for key, stableEventID := range p.coordinate {
		if stableEventID == event.StableEventID {
			delete(p.coordinate, key)
		}
	}
	p.revision++
	p.rewriteRevision++
	return true
}

// Snapshot returns a deep value copy of the current projection generation.
func (p *UsageProjection) Snapshot() ProjectionSnapshot {
	if p == nil {
		return ProjectionSnapshot{SchemaVersion: projectionSchemaVersion}
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.snapshotLocked()
}

// SnapshotWithBudget validates query scratch and scan limits before
// materializing public pricing DTOs. Projection-backed HTTP paths must use this
// method; Snapshot remains the compatibility surface for existing callers.
func (p *UsageProjection) SnapshotWithBudget() (ProjectionSnapshot, error) {
	if p == nil {
		return ProjectionSnapshot{SchemaVersion: projectionSchemaVersion}, ErrProjectionUnavailable
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	if err := p.validateStorageBudgetLocked(p.storageMetricsLocked()); err != nil {
		return ProjectionSnapshot{}, err
	}
	if err := p.validateQueryBudgetLocked(); err != nil {
		return ProjectionSnapshot{}, err
	}
	return p.snapshotLocked(), nil
}

// Clone returns a private mutable generation retaining immutable event
// identities and all resolver indexes. Rebuilds use it outside the live stats
// lock and publish it only after the coordinator CAS succeeds.
func (p *UsageProjection) Clone() *UsageProjection {
	if p == nil {
		return NewUsageProjection()
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.cloneLocked()
}

// cloneLocked returns a deep copy assuming p.mu is held for reading.
func (p *UsageProjection) cloneLocked() *UsageProjection {
	if p == nil {
		return NewUsageProjection()
	}
	clone := NewUsageProjection()
	clone.datasetEpoch = p.datasetEpoch
	clone.revision = p.revision
	clone.rewriteRevision = p.rewriteRevision
	clone.nextSequence = p.nextSequence
	clone.budget = p.budget
	clone.rollupCells = p.rollupCells
	clone.stringRegistry = p.stringRegistry.clone()
	clone.facts = p.facts.clone()
	clone.totals = cloneCompactAggregate(p.totals)
	clone.facets = cloneProjectionFacets(p.facets)
	clone.catalog = cloneCatalog(p.catalog)
	for key, value := range p.hours {
		clone.hours[key] = cloneCompactHourBucket(value)
	}
	for key, value := range p.days {
		clone.days[key] = cloneCompactHourBucket(value)
	}
	for key, value := range p.weeks {
		clone.weeks[key] = cloneCompactHourBucket(value)
	}
	for key, value := range p.months {
		clone.months[key] = cloneCompactHourBucket(value)
	}
	for key, value := range p.years {
		clone.years[key] = cloneCompactHourBucket(value)
	}
	for key, value := range p.health {
		clone.health[key] = value
	}
	clone.postings = p.postings.clone()
	clone.timeIndex = p.timeIndex.clone()
	for key, value := range p.canonical {
		clone.canonical[key] = value
	}
	for key, value := range p.coordinate {
		clone.coordinate[key] = value
	}
	for key, value := range p.lookup {
		clone.lookup[key] = value
	}
	for key, value := range p.ordinals {
		clone.ordinals[key] = value
	}
	for key, value := range p.events {
		clone.events[key] = value
	}
	for key, value := range p.exportDetails {
		clone.exportDetails[key] = cloneRequestDetail(value)
	}
	return clone
}

func (p *UsageProjection) snapshotLocked() ProjectionSnapshot {
	result := ProjectionSnapshot{
		SchemaVersion:   projectionSchemaVersion,
		DatasetEpoch:    p.datasetEpoch,
		Revision:        p.revision,
		RewriteRevision: p.rewriteRevision,
		Totals:          p.materializeAggregate(p.totals, p.facets),
		Hours:           make(map[string]HourBucket, len(p.hours)),
		Days:            make(map[string]HourBucket, len(p.days)),
		Weeks:           make(map[string]HourBucket, len(p.weeks)),
		Months:          make(map[string]HourBucket, len(p.months)),
		Years:           make(map[string]HourBucket, len(p.years)),
		Health:          make(map[string]HealthBucket, len(p.health)),
		Postings:        make(map[string][]EventRef, len(p.postings.rows)),
		Catalog:         cloneCatalog(p.catalog),
		Events:          make([]EventRef, 0, len(p.events)),
	}
	for key, bucket := range p.hours {
		result.Hours[key] = p.materializeHourBucket(bucket)
	}
	for key, bucket := range p.days {
		result.Days[key] = p.materializeHourBucket(bucket)
	}
	for key, bucket := range p.weeks {
		result.Weeks[key] = p.materializeHourBucket(bucket)
	}
	for key, bucket := range p.months {
		result.Months[key] = p.materializeHourBucket(bucket)
	}
	for key, bucket := range p.years {
		result.Years[key] = p.materializeHourBucket(bucket)
	}
	for key, bucket := range p.health {
		result.Health[key] = bucket
	}
	p.populateSnapshotPricing(&result)
	for compactKey, rowIDs := range p.postings.rows {
		key := p.postingKeyString(compactKey)
		if key == "" {
			continue
		}
		refs := make([]EventRef, 0, len(rowIDs))
		for _, rowID := range rowIDs {
			if !p.facts.isActive(rowID) {
				continue
			}
			if contribution, ok := p.facts.contribution(rowID, p.stringRegistry); ok {
				refs = append(refs, contribution.EventRef)
			}
		}
		result.Postings[key] = refs
		sort.SliceStable(result.Postings[key], func(i, j int) bool {
			return eventRefBefore(result.Postings[key][i], result.Postings[key][j])
		})
	}
	p.facts.eachActive(func(rowID projectionFactRowID) {
		if contribution, ok := p.facts.contribution(rowID, p.stringRegistry); ok {
			result.Events = append(result.Events, contribution.EventRef)
		}
	})
	sort.SliceStable(result.Events, func(i, j int) bool {
		return eventRefBefore(result.Events[i], result.Events[j])
	})
	return result
}

func (p *UsageProjection) buildContribution(detail RequestDetail, apiName string, ref EventRef, version uint64) projectionContribution {
	tokens := normaliseRequestTokens(detail.Tokens, detail.Provider)
	billable := GetBillableTokenComponents(detail)
	authIndex := strings.TrimSpace(detail.AuthIndex)
	sourceKey := UsageSourceKeyV1(detail)
	sourceID := UsageSourceIDV1(detail)
	priceKey := BuildPriceKey(detail.Provider, detail.Model)
	providerState := ProviderRawPresenceV1(detail.Provider)
	apiID := p.stringRegistry.intern(apiName)
	modelID := p.stringRegistry.intern(detail.Model)
	providerID := p.stringRegistry.intern(detail.Provider)
	authID := p.stringRegistry.intern(authIndex)
	sourceIDValue := p.stringRegistry.intern(sourceID)
	priceKeyID := p.stringRegistry.intern(priceKey)
	providerStateID := p.stringRegistry.intern(providerState)
	facetIDs := [5]projectionStringID{
		p.stringRegistry.intern("api:" + apiName),
		p.stringRegistry.intern("model:" + detail.Model),
		p.stringRegistry.intern("provider:" + detail.Provider),
		p.stringRegistry.intern("auth:" + authIndex),
		p.stringRegistry.intern("source:" + sourceID),
	}
	ref.Version = version
	ref.Timestamp = detail.Timestamp.UTC()
	ref.API = apiName
	ref.Model = detail.Model
	ref.Provider = detail.Provider
	ref.AuthIndex = authIndex
	ref.SourceID = sourceID
	ref.Failed = detail.Failed
	return projectionContribution{
		RequestID:       detail.RequestID,
		DetailRole:      detail.DetailRole,
		DetailSequence:  detail.DetailSequence,
		API:             apiName,
		Endpoint:        detail.Endpoint,
		Model:           detail.Model,
		Provider:        detail.Provider,
		AuthIndex:       authIndex,
		SourceID:        sourceID,
		SourceKey:       sourceKey,
		PriceKey:        priceKey,
		ProviderState:   providerState,
		Timestamp:       detail.Timestamp.UTC(),
		Failed:          detail.Failed,
		LatencyMs:       detail.LatencyMs,
		Tokens:          tokens,
		Billable:        billable,
		Classification:  ClassifyDetailV1(detail),
		ComponentMask:   billable.Mask(),
		APIID:           apiID,
		ModelID:         modelID,
		ProviderID:      providerID,
		AuthID:          authID,
		SourceIDValue:   sourceIDValue,
		PriceKeyID:      priceKeyID,
		ProviderStateID: providerStateID,
		FacetIDs:        facetIDs,
		EventRef:        ref,
	}
}

func (p *UsageProjection) applyContributionLocked(contribution projectionContribution) projectionFactRowID {
	rowID := p.facts.append(contribution)
	p.timeIndex.append(contribution.Timestamp, rowID)
	p.applyScalarContributionLocked(contribution, 1)
	for _, key := range contributionPostingKeys(contribution) {
		p.postings.append(key, rowID)
	}
	p.registerCatalogLocked(contribution)
	return rowID
}

func (p *UsageProjection) applyScalarContributionLocked(contribution projectionContribution, delta int64) {
	applyAggregateDelta(&p.totals, contribution, delta)
	applyFacetDelta(&p.facets, contribution, delta)
	p.addRollupCellDelta(updateBucketMap(p.hours, hourStart(contribution.Timestamp), hourStart(contribution.Timestamp).Add(time.Hour), contribution, delta))
	day := dayStart(contribution.Timestamp)
	p.addRollupCellDelta(updateBucketMap(p.days, day, day.AddDate(0, 0, 1), contribution, delta))
	week := weekStart(contribution.Timestamp)
	p.addRollupCellDelta(updateBucketMap(p.weeks, week, week.AddDate(0, 0, 7), contribution, delta))
	month := monthStart(contribution.Timestamp)
	p.addRollupCellDelta(updateBucketMap(p.months, month, month.AddDate(0, 1, 0), contribution, delta))
	year := yearStart(contribution.Timestamp)
	p.addRollupCellDelta(updateBucketMap(p.years, year, year.AddDate(1, 0, 0), contribution, delta))
	updateHealthBucket(p.health, contribution.Timestamp, contribution, delta)
}

func (p *UsageProjection) addRollupCellDelta(delta int64) {
	if delta >= 0 {
		p.rollupCells = saturatingAddUint64(p.rollupCells, uint64(delta))
		return
	}
	reduction := uint64(-delta)
	if reduction >= p.rollupCells {
		p.rollupCells = 1
		return
	}
	p.rollupCells -= reduction
}

func (p *UsageProjection) revokeContributionLocked(rowID projectionFactRowID, contribution projectionContribution) {
	if !p.facts.tombstone(rowID) {
		return
	}
	p.applyScalarContributionLocked(contribution, -1)
}

func (p *UsageProjection) registerCatalogLocked(contribution projectionContribution) {
	if contribution.Model != "" {
		p.catalog.Models[contribution.Model] = CatalogEntry{ID: contribution.Model, PriceKey: contribution.PriceKey, Provider: contribution.Provider, ProviderState: contribution.ProviderState}
	}
	if contribution.PriceKey != "" {
		p.catalog.PriceKeys[contribution.PriceKey] = CatalogEntry{ID: contribution.PriceKey, PriceKey: contribution.PriceKey, Provider: contribution.Provider, ProviderState: contribution.ProviderState}
	}
	if contribution.SourceID != "" {
		p.catalog.Sources[contribution.SourceID] = CatalogEntry{ID: contribution.SourceID, Label: contribution.SourceKey}
	}
}

func (p *UsageProjection) registerLookupLocked(apiName string, detail RequestDetail, logicalIdentity, stableEventID string) {
	logicalKey := logicalLookupIndexKey(logicalIdentity)
	if existing := p.lookup[logicalKey]; existing == "" || existing == stableEventID {
		p.lookup[logicalKey] = stableEventID
	}
	coordinateKey := coordinateIndexKey(apiName, detail)
	if existing := p.coordinate[coordinateKey]; existing == "" || existing == stableEventID {
		p.coordinate[coordinateKey] = stableEventID
	}
	if detail.RequestID != "" {
		key := requestLookupIndexKey(apiName, detail)
		if existing := p.lookup[key]; existing == "" || existing == stableEventID {
			p.lookup[key] = stableEventID
		}
	}
}

func logicalLookupIndexKey(identity string) projectionIndexKey {
	return newProjectionIndexKey(projectionIndexLogicalLookup, identity)
}

func coordinateIndexKey(apiName string, detail RequestDetail) projectionIndexKey {
	detail = normalizeRequestDetailPreserveTimestamp(detail, detail.Provider)
	coordinateAPI := canonicalCoordinateAPIName(apiName, detail)
	timestamp := "zero-time"
	if !detail.Timestamp.IsZero() {
		timestamp = detail.Timestamp.UTC().Format(time.RFC3339Nano)
	}
	stableAuthType := normalizedIdentityPart(detail.AuthType)
	stableAuthIndex := normalizedIdentityPart(detail.AuthIndex)
	stableSource := UsageSourceKeyV1(detail)
	stableExecutor := normalizedIdentityPart(detail.ExecutorType)
	role := normalizedIdentityPart(detail.DetailRole)
	sequence := normalizedIdentityPart(detail.DetailSequence)
	if detail.RequestID != "" {
		return newProjectionIndexKeyFromFields(
			projectionIndexCoordinate,
			"request-coordinate-index-v1",
			coordinateAPI,
			detail.RequestID,
			timestamp,
			stableAuthType,
			stableAuthIndex,
			stableSource,
			stableExecutor,
			role,
			sequence,
		)
	}
	return newProjectionIndexKeyFromFields(
		projectionIndexCoordinate,
		"fallback-coordinate-index-v1",
		coordinateAPI,
		timestamp,
		detail.Endpoint,
		stableAuthType,
		stableAuthIndex,
		stableSource,
		stableExecutor,
		detail.ClientIP,
		role,
		sequence,
	)
}

func requestLookupIndexKey(_ string, detail RequestDetail) projectionIndexKey {
	detail = normalizeRequestDetailPreserveTimestamp(detail, detail.Provider)
	timestamp := "zero-time"
	if !detail.Timestamp.IsZero() {
		timestamp = detail.Timestamp.UTC().Format(time.RFC3339Nano)
	}
	return newProjectionIndexKeyFromFields(
		projectionIndexRequestLookup,
		"request-lookup-index-v1",
		detail.RequestID,
		timestamp,
		detail.Endpoint,
		detail.Provider,
		detail.ExecutorType,
		detail.Model,
		normalizedIdentityPart(detail.AuthIndex),
		UsageSourceKeyV1(detail),
		detail.DetailRole,
		detail.DetailSequence,
	)
}

func requestLookupKey(_ string, detail RequestDetail) string {
	detail = normalizeRequestDetailPreserveTimestamp(detail, detail.Provider)
	timestamp := "zero-time"
	if !detail.Timestamp.IsZero() {
		timestamp = detail.Timestamp.UTC().Format(time.RFC3339Nano)
	}
	return "request:" + hex.EncodeToString(encodeLengthPrefixed(
		detail.RequestID,
		timestamp,
		detail.Endpoint,
		detail.Provider,
		detail.ExecutorType,
		detail.Model,
		normalizedIdentityPart(detail.AuthIndex),
		UsageSourceKeyV1(detail),
		detail.DetailRole,
		detail.DetailSequence,
	))
}

func contributionPostingKeys(contribution projectionContribution) [6]projectionPostingKey {
	return [6]projectionPostingKey{
		{Dimension: postingDimensionAll},
		{Dimension: postingDimensionAPI, Value: contribution.APIID},
		{Dimension: postingDimensionModel, Value: contribution.ModelID},
		{Dimension: postingDimensionProvider, Value: contribution.ProviderID},
		{Dimension: postingDimensionAuth, Value: contribution.AuthID},
		{Dimension: postingDimensionSource, Value: contribution.SourceIDValue},
	}
}

func removePostingEventID(stableEventIDs []string, stableEventID string) []string {
	filtered := stableEventIDs[:0]
	for _, candidate := range stableEventIDs {
		if candidate != stableEventID {
			filtered = append(filtered, candidate)
		}
	}
	return filtered
}

func canonicalDetailHash(detail RequestDetail) string {
	hash := sha256.Sum256(canonicalDetailBytes(detail))
	return hex.EncodeToString(hash[:])
}

func collisionQualifiedStableEventID(base, canonicalIdentity string) string {
	base = strings.TrimSpace(base)
	if base == "" {
		base = "event:unknown"
	}
	hash := sha256.Sum256([]byte(base + "\x00" + strings.TrimSpace(canonicalIdentity)))
	return base + ":collision:" + hex.EncodeToString(hash[:])[:16]
}

func eventRefBefore(left, right EventRef) bool {
	if !left.Timestamp.Equal(right.Timestamp) {
		return left.Timestamp.After(right.Timestamp)
	}
	if left.Sequence != right.Sequence {
		return left.Sequence > right.Sequence
	}
	if left.BatchOrdinal != right.BatchOrdinal {
		return left.BatchOrdinal > right.BatchOrdinal
	}
	return left.StableEventID > right.StableEventID
}

func cloneCatalog(catalog ProjectionCatalog) ProjectionCatalog {
	result := ProjectionCatalog{
		Models:    make(map[string]CatalogEntry, len(catalog.Models)),
		PriceKeys: make(map[string]CatalogEntry, len(catalog.PriceKeys)),
		Sources:   make(map[string]CatalogEntry, len(catalog.Sources)),
	}
	for key, value := range catalog.Models {
		result.Models[key] = value
	}
	for key, value := range catalog.PriceKeys {
		result.PriceKeys[key] = value
	}
	for key, value := range catalog.Sources {
		result.Sources[key] = value
	}
	return result
}

func cloneAggregate(aggregate Aggregate) Aggregate {
	result := aggregate
	result.PricingGroups = make(map[string]PricingAggregate, len(aggregate.PricingGroups))
	for key, value := range aggregate.PricingGroups {
		result.PricingGroups[key] = clonePricingAggregate(value)
	}
	result.Facets = make(map[string]int64, len(aggregate.Facets))
	for key, value := range aggregate.Facets {
		result.Facets[key] = value
	}
	return result
}

func clonePricingAggregate(pricing PricingAggregate) PricingAggregate {
	result := pricing
	result.ComponentMaskCounts = make(map[ComponentMask]int64, len(pricing.ComponentMaskCounts))
	for key, value := range pricing.ComponentMaskCounts {
		result.ComponentMaskCounts[key] = value
	}
	return result
}

func cloneHourBucket(bucket HourBucket) HourBucket {
	result := bucket
	result.Totals = cloneAggregate(bucket.Totals)
	result.APIs = cloneAggregateMap(bucket.APIs)
	result.Models = cloneAggregateMap(bucket.Models)
	result.Providers = cloneAggregateMap(bucket.Providers)
	result.Auths = cloneAggregateMap(bucket.Auths)
	result.Sources = cloneAggregateMap(bucket.Sources)
	return result
}

func cloneAggregateMap(values map[string]Aggregate) map[string]Aggregate {
	result := make(map[string]Aggregate, len(values))
	for key, value := range values {
		result[key] = cloneAggregate(value)
	}
	return result
}
