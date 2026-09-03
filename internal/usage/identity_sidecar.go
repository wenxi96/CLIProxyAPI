package usage

import (
	"sort"
	"strings"
)

const identitySidecarVersionV1 = "usage_identity_sidecar_v1"

// IdentitySidecar is the durable identity metadata kept beside, rather than
// inside, the legacy request-detail JSON payload.
type IdentitySidecar struct {
	SchemaVersion             string                          `json:"schema_version"`
	Generation                uint64                          `json:"generation"`
	ParentGeneration          uint64                          `json:"parent_generation"`
	CanonicalDetailGeneration uint64                          `json:"canonical_detail_generation"`
	DatasetEpoch              uint64                          `json:"dataset_epoch"`
	NextSequence              uint64                          `json:"next_sequence"`
	PayloadSHA256             string                          `json:"payload_sha256"`
	Checksum                  string                          `json:"checksum"`
	TerminalFrontier          uint64                          `json:"terminal_frontier"`
	TerminalIntents           []IdentitySidecarTerminalIntent `json:"terminal_intents"`
	BulkTransaction           *MutationBatchMarker            `json:"bulk_transaction,omitempty"`
	SourceOrdinals            map[string]uint64               `json:"source_ordinals"`
	Entries                   []IdentitySidecarEntry          `json:"entries"`
}

type IdentitySidecarTerminalIntent struct {
	Sequence       uint64                `json:"sequence"`
	State          MutationTerminalState `json:"state"`
	IdempotencyKey string                `json:"idempotency_key"`
	Tombstone      bool                  `json:"tombstone"`
}

type IdentitySidecarEntry struct {
	APIName                string `json:"api_name"`
	ModelName              string `json:"model_name"`
	CanonicalDetailHash    string `json:"canonical_detail_hash"`
	Occurrence             uint64 `json:"occurrence"`
	CanonicalIdentitySeed  string `json:"canonical_identity_seed"`
	CanonicalEventIdentity string `json:"canonical_event_identity"`
	StableEventID          string `json:"stable_event_id"`
	SourceGroupKey         string `json:"source_group_key"`
	SourceGroupOrdinal     uint64 `json:"source_group_ordinal"`
	Sequence               uint64 `json:"sequence"`
	BatchOrdinal           uint64 `json:"batch_ordinal"`
}

func newIdentitySidecar() IdentitySidecar {
	return IdentitySidecar{
		SchemaVersion:   identitySidecarVersionV1,
		TerminalIntents: make([]IdentitySidecarTerminalIntent, 0),
		SourceOrdinals:  make(map[string]uint64),
		Entries:         make([]IdentitySidecarEntry, 0),
	}
}

// IdentitySidecarSnapshot returns a stable, value-only copy of projection
// identities. It intentionally does not expose the mutable projection maps.
func (s *RequestStatistics) IdentitySidecarSnapshot() IdentitySidecar {
	if s == nil {
		return newIdentitySidecar()
	}
	s.mu.RLock()
	projection := s.projection
	coordinator := s.coordinator
	s.mu.RUnlock()
	if projection == nil {
		return newIdentitySidecar()
	}
	result := projection.identitySidecarSnapshot()
	if coordinator != nil {
		result.TerminalFrontier, result.TerminalIntents = coordinator.Journal().TerminalSnapshot()
		if marker, ok := coordinator.Journal().TerminalBulkTransactionSnapshot(); ok {
			result.BulkTransaction = &marker
		}
	}
	return result
}

func (p *UsageProjection) identitySidecarSnapshot() IdentitySidecar {
	result := newIdentitySidecar()
	if p == nil {
		return result
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	result.DatasetEpoch = p.datasetEpoch
	result.Generation = p.revision
	result.CanonicalDetailGeneration = p.revision
	result.NextSequence = p.nextSequence
	result.SourceOrdinals = make(map[string]uint64, len(p.ordinals))
	for key, ordinal := range p.ordinals {
		result.SourceOrdinals[key] = ordinal
	}
	keys := make([]string, 0, len(p.events))
	for stableEventID := range p.events {
		keys = append(keys, stableEventID)
	}
	sort.Slice(keys, func(i, j int) bool {
		left := p.facts.sequence(p.events[keys[i]].FactRowID)
		right := p.facts.sequence(p.events[keys[j]].FactRowID)
		if left != right {
			return left < right
		}
		return keys[i] < keys[j]
	})
	occur := make(map[string]uint64)
	for _, stableEventID := range keys {
		event := p.events[stableEventID]
		contribution, ok := p.facts.contribution(event.FactRowID, p.stringRegistry)
		if !ok {
			continue
		}
		key := sidecarDetailKeyFromHash(contribution.API, contribution.Model, event.CanonicalDetailHash)
		entry := IdentitySidecarEntry{
			APIName:                contribution.API,
			ModelName:              contribution.Model,
			CanonicalDetailHash:    event.CanonicalDetailHash,
			Occurrence:             occur[key],
			CanonicalIdentitySeed:  event.CanonicalIdentitySeed,
			CanonicalEventIdentity: event.CanonicalEventIdentity,
			StableEventID:          event.StableEventID,
			SourceGroupKey:         event.SourceGroupKey,
			SourceGroupOrdinal:     event.SourceGroupOrdinal,
			Sequence:               contribution.EventRef.Sequence,
			BatchOrdinal:           contribution.EventRef.BatchOrdinal,
		}
		occur[key]++
		result.Entries = append(result.Entries, entry)
	}
	return result
}

func sidecarDetailKey(apiName, modelName string, detail RequestDetail) string {
	return sidecarDetailKeyFromHash(apiName, modelName, canonicalDetailHash(detail))
}

func sidecarDetailKeyFromHash(apiName, modelName, detailHash string) string {
	return strings.TrimSpace(apiName) + "\x00" + strings.TrimSpace(modelName) + "\x00" + strings.TrimSpace(detailHash)
}

func (sidecar IdentitySidecar) valid() bool {
	return sidecar.SchemaVersion == identitySidecarVersionV1
}

// identityFor returns the sidecar identity for a detail in deterministic
// snapshot order. Missing entries deliberately return false so legacy payloads
// can be rebuilt with fresh canonical identities.
func (sidecar IdentitySidecar) identityFor(apiName, modelName string, detail RequestDetail, occurrence uint64) (ProjectionIdentity, bool) {
	if !sidecar.valid() {
		return ProjectionIdentity{}, false
	}
	hash := canonicalDetailHash(detail)
	key := sidecarDetailKeyFromHash(apiName, modelName, hash)
	for _, entry := range sidecar.Entries {
		if entry.Occurrence != occurrence || sidecarDetailKeyFromHash(entry.APIName, entry.ModelName, entry.CanonicalDetailHash) != key {
			continue
		}
		return ProjectionIdentity{
			CanonicalIdentitySeed:  entry.CanonicalIdentitySeed,
			CanonicalEventIdentity: entry.CanonicalEventIdentity,
			StableEventID:          entry.StableEventID,
			SourceGroupKey:         entry.SourceGroupKey,
			SourceGroupOrdinal:     entry.SourceGroupOrdinal,
			Sequence:               entry.Sequence,
			BatchOrdinal:           entry.BatchOrdinal,
		}, true
	}
	return ProjectionIdentity{}, false
}

func (sidecar IdentitySidecar) identityIndex() map[string][]ProjectionIdentity {
	index := make(map[string][]ProjectionIdentity, len(sidecar.Entries))
	if !sidecar.valid() {
		return index
	}
	entries := append([]IdentitySidecarEntry(nil), sidecar.Entries...)
	sort.SliceStable(entries, func(i, j int) bool {
		left := sidecarDetailKeyFromHash(entries[i].APIName, entries[i].ModelName, entries[i].CanonicalDetailHash)
		right := sidecarDetailKeyFromHash(entries[j].APIName, entries[j].ModelName, entries[j].CanonicalDetailHash)
		if left != right {
			return left < right
		}
		if entries[i].Occurrence != entries[j].Occurrence {
			return entries[i].Occurrence < entries[j].Occurrence
		}
		if entries[i].Sequence != entries[j].Sequence {
			return entries[i].Sequence < entries[j].Sequence
		}
		return entries[i].BatchOrdinal < entries[j].BatchOrdinal
	})
	for _, entry := range entries {
		key := sidecarDetailKeyFromHash(entry.APIName, entry.ModelName, entry.CanonicalDetailHash)
		identity := ProjectionIdentity{
			CanonicalIdentitySeed:  entry.CanonicalIdentitySeed,
			CanonicalEventIdentity: entry.CanonicalEventIdentity,
			StableEventID:          entry.StableEventID,
			SourceGroupKey:         entry.SourceGroupKey,
			SourceGroupOrdinal:     entry.SourceGroupOrdinal,
			Sequence:               entry.Sequence,
			BatchOrdinal:           entry.BatchOrdinal,
		}
		index[key] = append(index[key], identity)
	}
	return index
}
