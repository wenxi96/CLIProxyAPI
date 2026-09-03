package usage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	coreusage "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
)

const legacyRecordAdmissionPrefix = "legacy-record:"

// legacyRecordAdmissionDiscriminator preserves the historical Record API's
// enrichment behavior without weakening strict manager/coordinator admission.
// The full detail identity includes model/provider, so a legacy retry cannot
// accidentally rekey a different provider or model into the same event.
func legacyRecordAdmissionDiscriminator(ctx context.Context, record coreusage.Record) string {
	detail := CanonicalRequestDetail(ctx, record)
	apiName := safeAPIIdentifier(ctx, record, detail)
	identity := detailIdentityKey(apiName, detail.Model, detail)
	digest := sha256.Sum256([]byte(identity))
	return legacyRecordAdmissionPrefix + hex.EncodeToString(digest[:])
}

type MutationCoordinatorConfig struct {
	JournalEntryLimit           int
	JournalBudgetBytes          int
	MaxSerializedIntentBytes    int
	GateQueueLimit              int
	MaxFinalizeReplayIntents    int
	MaxFinalizeRetries          int
	AdmissionIdentityCacheLimit int
	MaxBulkChunkIntents         int
	MaxBulkChunkBytes           int
	ProjectionBudget            ProjectionBudgetV2
}

func DefaultMutationCoordinatorConfig() MutationCoordinatorConfig {
	return MutationCoordinatorConfig{
		JournalEntryLimit:           4096,
		JournalBudgetBytes:          4096 * 4096,
		MaxSerializedIntentBytes:    4096,
		GateQueueLimit:              256,
		MaxFinalizeReplayIntents:    256,
		MaxFinalizeRetries:          2,
		AdmissionIdentityCacheLimit: 4096,
		MaxBulkChunkIntents:         256,
		MaxBulkChunkBytes:           2 * 1024 * 1024,
		ProjectionBudget:            DefaultProjectionBudgetV2(),
	}
}

// MutationCoordinator serializes mutation admission and terminal journal
// outcomes while leaving the existing RequestStatistics live lock short.
type MutationCoordinator struct {
	stats   *RequestStatistics
	journal *MutationJournal
	config  MutationCoordinatorConfig
	gate    *rebuildFinalizeGate

	mu                   sync.Mutex
	admissionMu          sync.Mutex
	applyMu              sync.Mutex
	applyOrderMu         sync.Mutex
	applyOrderCond       *sync.Cond
	nextApplySeq         uint64
	completedApply       map[uint64]struct{}
	sourceOrdinals       map[string]uint64
	pendingIdentity      map[string]pendingAdmission
	identityByAdmission  map[string]ProjectionIdentity
	identityCacheOrder   []identityCacheOrderEntry
	identityCacheHead    int
	identityCacheVersion map[string]uint64
	inFlight             int
	inFlightDone         chan struct{}
	activeCandidate      *RebuildCandidate
	activeCapture        *projectionCaptureSession
	activeBatch          bool
	batchDone            chan struct{}
	batchSequence        atomic.Uint64
	bulkChunkDoneHook    func(MutationBatchMarker)
}

type pendingAdmission struct {
	sequence uint64
	identity ProjectionIdentity
}

type identityCacheOrderEntry struct {
	key      string
	sequence uint64
}

type projectionCaptureSession struct {
	coordinator        *MutationCoordinator
	batchDone          chan struct{}
	baseJournalHead    uint64
	baseJournalHeadSet bool
	finished           bool
}

func NewMutationCoordinator(stats *RequestStatistics) *MutationCoordinator {
	return NewMutationCoordinatorWithConfig(stats, DefaultMutationCoordinatorConfig())
}

func NewMutationCoordinatorWithConfig(stats *RequestStatistics, config MutationCoordinatorConfig) *MutationCoordinator {
	defaults := DefaultMutationCoordinatorConfig()
	if config.JournalEntryLimit <= 0 {
		config.JournalEntryLimit = defaults.JournalEntryLimit
	}
	if config.MaxSerializedIntentBytes <= 0 {
		config.MaxSerializedIntentBytes = defaults.MaxSerializedIntentBytes
	}
	if config.JournalBudgetBytes <= 0 {
		config.JournalBudgetBytes = config.JournalEntryLimit * config.MaxSerializedIntentBytes
	}
	if config.JournalBudgetBytes < config.MaxSerializedIntentBytes {
		config.JournalBudgetBytes = config.MaxSerializedIntentBytes
	}
	if config.GateQueueLimit <= 0 {
		config.GateQueueLimit = defaults.GateQueueLimit
	}
	if config.MaxFinalizeReplayIntents <= 0 {
		config.MaxFinalizeReplayIntents = defaults.MaxFinalizeReplayIntents
	}
	if config.MaxFinalizeRetries <= 0 {
		config.MaxFinalizeRetries = defaults.MaxFinalizeRetries
	}
	if config.AdmissionIdentityCacheLimit <= 0 {
		config.AdmissionIdentityCacheLimit = config.JournalEntryLimit
	}
	if config.MaxBulkChunkIntents <= 0 {
		config.MaxBulkChunkIntents = defaults.MaxBulkChunkIntents
	}
	if config.MaxBulkChunkBytes <= 0 {
		config.MaxBulkChunkBytes = defaults.MaxBulkChunkBytes
	}
	config.ProjectionBudget = config.ProjectionBudget.normalized()
	done := make(chan struct{})
	close(done)
	batchDone := make(chan struct{})
	close(batchDone)
	journal := NewMutationJournalWithBudget(config.JournalEntryLimit, config.JournalBudgetBytes, config.MaxSerializedIntentBytes)
	gateLimit := config.GateQueueLimit
	if journalSlots := journal.QueueSlots(); gateLimit > journalSlots {
		gateLimit = journalSlots
	}
	if gateLimit <= 0 {
		gateLimit = 1
	}
	coordinator := &MutationCoordinator{
		stats:                stats,
		journal:              journal,
		config:               config,
		gate:                 newRebuildFinalizeGate(gateLimit),
		nextApplySeq:         1,
		completedApply:       make(map[uint64]struct{}),
		sourceOrdinals:       make(map[string]uint64),
		pendingIdentity:      make(map[string]pendingAdmission),
		identityByAdmission:  make(map[string]ProjectionIdentity),
		identityCacheVersion: make(map[string]uint64),
		inFlightDone:         done,
		batchDone:            batchDone,
	}
	coordinator.applyOrderCond = sync.NewCond(&coordinator.applyOrderMu)
	if stats != nil {
		stats.mu.Lock()
		stats.ensureProjectionLocked()
		stats.projection.SetBudget(config.ProjectionBudget)
		stats.mu.Unlock()
	}
	return coordinator
}

func (c *MutationCoordinator) Journal() *MutationJournal {
	if c == nil {
		return nil
	}
	return c.journal
}

// applyPersistedMetadata resumes the coordinator allocators after a process
// restart. Previous terminal intents are represented by the committed
// generation; only the monotonic sequence and source-group allocators need to
// continue in the new in-memory journal.
func (c *MutationCoordinator) applyPersistedMetadata(sidecar IdentitySidecar) {
	if c == nil {
		return
	}
	c.mu.Lock()
	for key, ordinal := range sidecar.SourceOrdinals {
		if ordinal > c.sourceOrdinals[key] {
			c.sourceOrdinals[key] = ordinal
		}
	}
	c.mu.Unlock()
	c.journal.AdvanceNextSequence(sidecar.NextSequence)
	if sidecar.BulkTransaction != nil {
		c.advanceBatchSequence(sidecar.BulkTransaction.BatchSequence)
		if _, ok := c.journal.TerminalBulkTransactionSnapshot(); !ok {
			_ = c.journal.RestoreBulkTransaction(*sidecar.BulkTransaction)
		}
	}
	c.applyOrderMu.Lock()
	if sidecar.NextSequence > c.nextApplySeq {
		c.nextApplySeq = sidecar.NextSequence
	}
	c.applyOrderCond.Broadcast()
	c.applyOrderMu.Unlock()
}

// recordRestoredGeneration resumes the coordinator allocators (next sequence,
// next apply sequence, source ordinals, batch sequence) from a directly
// hydrated generation and records a deterministic Restore bulk transaction
// chained after the persisted marker. It mirrors the replay path's bulk
// transaction chain semantics without per-detail contribution replay. Errors
// must propagate so RestoreRequestStatistics never claims success on a partial
// restore. The single synthetic chunk uses a domain-separated checksum derived
// from the persisted payload digest and generation, not an arbitrary value.
func (c *MutationCoordinator) recordRestoredGeneration(sidecar IdentitySidecar, totalIntents uint64, generation uint64) error {
	if c == nil || c.journal == nil {
		return nil
	}
	c.mu.Lock()
	for key, ordinal := range sidecar.SourceOrdinals {
		if ordinal > c.sourceOrdinals[key] {
			c.sourceOrdinals[key] = ordinal
		}
	}
	c.mu.Unlock()
	c.journal.AdvanceNextSequence(sidecar.NextSequence)
	c.applyOrderMu.Lock()
	if sidecar.NextSequence > c.nextApplySeq {
		c.nextApplySeq = sidecar.NextSequence
	}
	c.applyOrderCond.Broadcast()
	c.applyOrderMu.Unlock()

	if sidecar.BulkTransaction != nil {
		c.advanceBatchSequence(sidecar.BulkTransaction.BatchSequence)
		if _, ok := c.journal.TerminalBulkTransactionSnapshot(); !ok {
			if err := c.journal.RestoreBulkTransaction(*sidecar.BulkTransaction); err != nil {
				return fmt.Errorf("restore persisted bulk transaction: %w", err)
			}
		}
	}
	if totalIntents == 0 {
		return nil
	}
	batchSequence := c.batchSequence.Add(1)
	batchID := fmt.Sprintf("%s:%d", MutationOperationRestore, batchSequence)
	beginMarker := MutationBatchMarker{
		BatchID:       batchID,
		BatchSequence: batchSequence,
		Operation:     MutationOperationRestore,
		BaseRevision:  sidecar.CanonicalDetailGeneration,
		DatasetEpoch:  sidecar.DatasetEpoch,
		TotalIntents:  totalIntents,
		PayloadSHA256: sidecar.PayloadSHA256,
	}
	if err := c.journal.BeginBulkTransaction(beginMarker); err != nil {
		return fmt.Errorf("begin restore bulk transaction: %w", err)
	}
	chunkSHA := mutationRestoreChunkChecksum(sidecar, generation)
	if _, err := c.journal.AdvanceBulkChunk(batchID, 0, totalIntents, chunkSHA); err != nil {
		_, _ = c.journal.CompleteBulkTransaction(batchID, MutationTerminalTombstone, mutationBatchFailureCode(err), "")
		return fmt.Errorf("advance restore bulk frontier: %w", err)
	}
	marker, ok := c.journal.BulkTransactionSnapshot()
	if !ok {
		return fmt.Errorf("restore bulk transaction missing after advance: %w", ErrMutationBatchNotFound)
	}
	manifestSHA256 := bulkTransactionManifestChecksum(marker, MergeResult{Added: int64(totalIntents)})
	if _, err := c.journal.CompleteBulkTransaction(batchID, MutationTerminalCommitted, "", manifestSHA256); err != nil {
		return fmt.Errorf("complete restore bulk transaction: %w", err)
	}
	return nil
}

// mutationRestoreChunkChecksum is a domain-separated, deterministic checksum for
// the synthetic restore chunk. It binds the chunk to the persisted payload
// digest and generation so the bulk-transaction chain is reproducible without
// replaying canonical details.
func mutationRestoreChunkChecksum(sidecar IdentitySidecar, generation uint64) string {
	hasher := sha256.New()
	writeDigestPart(hasher, "usage:restore:chunk:v1")
	writeDigestPart(hasher, sidecar.PayloadSHA256)
	writeDigestPart(hasher, formatUint(generation))
	writeDigestPart(hasher, formatUint(sidecar.NextSequence))
	return hex.EncodeToString(hasher.Sum(nil))
}

// enter reserves one journal slot before sequence allocation and atomically
// joins the coordinator's in-flight set with the rebuild gate check.
func (c *MutationCoordinator) enter(ctx context.Context) (func(), int, error) {
	return c.enterBatch(ctx, 1)
}

// enterBatch reserves the entire journal footprint for one logical batch
// before admission. A batch consumes one rebuild-gate slot per intent so a
// large import cannot bypass the configured pending-intent bound. Exclusive
// bulk transactions use the internal allowActiveBatch variant after they have
// acquired the batch owner slot.
func (c *MutationCoordinator) enterBatch(ctx context.Context, count int) (func(), int, error) {
	return c.enterBatchMode(ctx, count, false)
}

func (c *MutationCoordinator) enterBatchForExclusive(ctx context.Context, count int) (func(), int, error) {
	return c.enterBatchMode(ctx, count, true)
}

func (c *MutationCoordinator) enterBatchMode(ctx context.Context, count int, allowActiveBatch bool) (func(), int, error) {
	if c == nil || c.journal == nil {
		return nil, 0, ErrProjectionUnavailable
	}
	if count <= 0 {
		return func() {}, 0, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if c.config.MaxSerializedIntentBytes <= 0 || count > int(^uint(0)>>1)/c.config.MaxSerializedIntentBytes {
		return nil, 0, ErrMutationJournalBudget
	}
	reservationBytes := c.config.MaxSerializedIntentBytes
	reservationTotal := count * reservationBytes
	if count > c.config.JournalEntryLimit {
		return nil, 0, ErrMutationJournalFull
	}
	if reservationTotal > c.config.JournalBudgetBytes {
		return nil, 0, ErrMutationJournalBudget
	}
	for {
		if err := ctx.Err(); err != nil {
			return nil, 0, err
		}
		if !allowActiveBatch {
			if err := c.waitForExclusiveBatch(ctx); err != nil {
				return nil, 0, err
			}
		}
		if err := c.journal.Reserve(reservationTotal); err != nil {
			if errors.Is(err, ErrMutationJournalFull) || errors.Is(err, ErrMutationJournalBudget) {
				c.mu.Lock()
				safeSequence := ^uint64(0)
				inFlight := c.inFlight
				inFlightDone := c.inFlightDone
				if c.activeCandidate != nil {
					safeSequence = c.activeCandidate.BaseJournalHead
				}
				if c.activeCapture != nil {
					captureSafeSequence := uint64(0)
					if c.activeCapture.baseJournalHeadSet {
						captureSafeSequence = c.activeCapture.baseJournalHead
					}
					if captureSafeSequence < safeSequence {
						safeSequence = captureSafeSequence
					}
				}
				c.mu.Unlock()
				if c.journal.CompactTerminalBefore(safeSequence) > 0 {
					continue
				}
				// A full journal can be a temporary admission backpressure
				// condition while an earlier intent is still applying. Wait for
				// that in-flight work to reach terminal state, then retry so a
				// caller does not create a duplicate canonical fallback record.
				if inFlight > 0 && inFlightDone != nil {
					select {
					case <-inFlightDone:
						continue
					case <-ctx.Done():
						return nil, 0, ctx.Err()
					}
				}
			}
			return nil, 0, err
		}
		releaseGate, err := c.gate.waitN(ctx, count)
		if err != nil {
			c.journal.ReleaseReservation(reservationTotal)
			return nil, 0, err
		}
		c.mu.Lock()
		if c.activeCandidate != nil || (!allowActiveBatch && c.activeBatch) {
			c.mu.Unlock()
			if releaseGate != nil {
				releaseGate()
			}
			c.journal.ReleaseReservation(reservationTotal)
			continue
		}
		if err := ctx.Err(); err != nil {
			c.mu.Unlock()
			if releaseGate != nil {
				releaseGate()
			}
			c.journal.ReleaseReservation(reservationTotal)
			return nil, 0, err
		}
		if c.inFlight == 0 {
			c.inFlightDone = make(chan struct{})
		}
		c.inFlight++
		c.mu.Unlock()
		return func() {
			if releaseGate != nil {
				releaseGate()
			}
			c.endInFlight()
		}, reservationBytes, nil
	}
}

func (c *MutationCoordinator) ApplyRecord(ctx context.Context, record coreusage.Record) error {
	if c == nil || c.stats == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	detail := CanonicalRequestDetail(ctx, record)
	apiName := safeAPIIdentifier(ctx, record, detail)
	release, reservationBytes, err := c.enter(ctx)
	if err != nil {
		return err
	}
	defer release()
	c.admissionMu.Lock()
	existingIdentity, _ := c.existingRestoredLegacyIdentity(apiName, detail, record)
	intent, identity, err := c.admitWithExistingIdentity(detail, apiName, MutationOperationRecord, "", 0, 0, record, record.Generate != nil, reservationBytes, existingIdentity)
	c.admissionMu.Unlock()
	if err != nil {
		return err
	}
	if _, ok := c.journal.Outcome(intent.Sequence); ok {
		return nil
	}
	if _, err := c.applyIntent(apiName, intent, identity); err != nil {
		if terminalErr := c.markTerminal(intent, MutationTerminalTombstone, err); terminalErr != nil {
			return fmt.Errorf("%w (terminal outcome: %v)", err, terminalErr)
		}
		return err
	}
	if err := c.markTerminal(intent, MutationTerminalCommitted, nil); err != nil {
		return err
	}
	return nil
}

func (c *MutationCoordinator) admit(detail RequestDetail, apiName string, operation MutationOperation, batchID string, batchSequence, batchOrdinal uint64, record coreusage.Record, incomingGenerateExplicit bool, reservationBytes int) (MutationIntent, ProjectionIdentity, error) {
	return c.admitWithExistingIdentity(detail, apiName, operation, batchID, batchSequence, batchOrdinal, record, incomingGenerateExplicit, reservationBytes, ProjectionIdentity{})
}

// admitWithExistingIdentity lets live enrichment and compatibility snapshot
// paths carry an exact identity from the current projection into admission.
// Strict runtime imports leave this empty so an unseeded detail always receives
// a fresh admission as required by the batch contract.
func (c *MutationCoordinator) admitWithExistingIdentity(detail RequestDetail, apiName string, operation MutationOperation, batchID string, batchSequence, batchOrdinal uint64, record coreusage.Record, incomingGenerateExplicit bool, reservationBytes int, existingIdentity ProjectionIdentity) (MutationIntent, ProjectionIdentity, error) {
	if c == nil || c.journal == nil {
		return MutationIntent{}, ProjectionIdentity{}, ErrProjectionUnavailable
	}
	detail = normalizeRequestDetailPreserveTimestamp(detail, detail.Provider)
	apiName = safeImportedAPIName(apiName, detail)
	coordinateKey := canonicalEventCoordinateKey(apiName, detail)
	c.mu.Lock()
	groupKey := record.SourceGroupKey
	if groupKey == "" {
		groupKey = SourceGroupKeyV1(detail)
	}
	ordinal := record.SourceGroupOrdinal
	seed := strings.TrimSpace(record.CanonicalIdentitySeed)
	discriminator := strings.TrimSpace(record.AdmissionDiscriminator)
	canReuseIdentity := batchID != "" || discriminator != ""
	reuseKey := admissionIdentityReuseKey(apiName, detail, batchID, discriminator)
	if discriminator != "" {
		coordinateKey = admissionCoordinateKey(apiName, detail, discriminator)
	}
	canonicalIdentity := ""
	stableEventID := ""
	projectionSequence := uint64(0)
	if seed == "" && existingIdentity.CanonicalIdentitySeed != "" {
		groupKey = existingIdentity.SourceGroupKey
		ordinal = existingIdentity.SourceGroupOrdinal
		seed = existingIdentity.CanonicalIdentitySeed
		canonicalIdentity = existingIdentity.CanonicalEventIdentity
		stableEventID = existingIdentity.StableEventID
		projectionSequence = existingIdentity.Sequence
	} else if seed == "" && canReuseIdentity {
		if existing, ok := c.identityByAdmission[reuseKey]; ok {
			groupKey = existing.SourceGroupKey
			ordinal = existing.SourceGroupOrdinal
			seed = existing.CanonicalIdentitySeed
			canonicalIdentity = existing.CanonicalEventIdentity
			stableEventID = existing.StableEventID
			projectionSequence = existing.Sequence
		} else if pending, ok := c.pendingIdentity[reuseKey]; ok {
			groupKey = pending.identity.SourceGroupKey
			ordinal = pending.identity.SourceGroupOrdinal
			seed = pending.identity.CanonicalIdentitySeed
			canonicalIdentity = pending.identity.CanonicalEventIdentity
			stableEventID = pending.identity.StableEventID
			projectionSequence = pending.identity.Sequence
		}
	}
	if seed == "" {
		if ordinal == 0 {
			ordinal = c.allocateSourceOrdinalLocked(groupKey)
		} else if next := ordinal + 1; c.sourceOrdinals[groupKey] < next {
			c.sourceOrdinals[groupKey] = next
		}
		seed = CanonicalIdentitySeedV1(detail, ordinal)
	} else if next := ordinal + 1; c.sourceOrdinals[groupKey] < next {
		c.sourceOrdinals[groupKey] = next
	}
	if canonicalIdentity == "" {
		canonicalIdentity = CanonicalEventIdentityV1(seed)
	}
	if stableEventID == "" {
		stableEventID = StableEventIDV1(canonicalIdentity)
	}
	if operation == MutationOperationRecord && projectionSequence != 0 {
		operation = MutationOperationEnrichment
	}
	logicalIdentity := detailIdentityKey(apiName, detail.Model, detail)
	intent := MutationIntent{
		BatchID:                  batchID,
		BatchSequence:            batchSequence,
		BatchOrdinal:             batchOrdinal,
		Operation:                operation,
		APIName:                  apiName,
		CoordinateKey:            coordinateKey,
		StableEventID:            stableEventID,
		ProjectionSequence:       projectionSequence,
		LogicalIdentity:          logicalIdentity,
		CanonicalEventIdentity:   canonicalIdentity,
		CanonicalIdentitySeed:    seed,
		SourceGroupKey:           groupKey,
		SourceGroupOrdinal:       ordinal,
		AdmissionDiscriminator:   discriminator,
		Detail:                   normalizeRequestDetail(detail, detail.Provider),
		IncomingGenerateExplicit: incomingGenerateExplicit,
	}
	if batchID != "" {
		intent.IdempotencyKey = fmt.Sprintf("batch:%s:%d", batchID, batchOrdinal)
	}
	if reservationBytes <= 0 {
		reservationBytes = c.config.MaxSerializedIntentBytes
		if err := c.journal.Reserve(reservationBytes); err != nil {
			c.mu.Unlock()
			return MutationIntent{}, ProjectionIdentity{}, err
		}
	}
	if serializedMutationIntentBytes(intent) > reservationBytes {
		c.journal.ReleaseReservation(reservationBytes)
		c.mu.Unlock()
		return MutationIntent{}, ProjectionIdentity{}, ErrMutationJournalBudget
	}
	appended, err := c.journal.AppendReserved(intent, reservationBytes)
	if err == nil {
		if projectionSequence == 0 {
			projectionSequence = appended.Sequence
		}
		if canReuseIdentity {
			c.pendingIdentity[reuseKey] = pendingAdmission{
				sequence: appended.Sequence,
				identity: ProjectionIdentity{
					CanonicalIdentitySeed:  appended.CanonicalIdentitySeed,
					CanonicalEventIdentity: appended.CanonicalEventIdentity,
					StableEventID:          stableEventID,
					Sequence:               projectionSequence,
					BatchOrdinal:           appended.BatchOrdinal,
					SourceGroupKey:         appended.SourceGroupKey,
					SourceGroupOrdinal:     appended.SourceGroupOrdinal,
				},
			}
			c.identityByAdmission[reuseKey] = c.pendingIdentity[reuseKey].identity
			c.identityCacheVersion[reuseKey] = appended.Sequence
			c.identityCacheOrder = append(c.identityCacheOrder, identityCacheOrderEntry{key: reuseKey, sequence: appended.Sequence})
			c.trimAdmissionIdentityCacheLocked()
		}
	}
	c.mu.Unlock()
	if err != nil {
		return MutationIntent{}, ProjectionIdentity{}, err
	}
	identity := ProjectionIdentity{
		CanonicalIdentitySeed:  appended.CanonicalIdentitySeed,
		CanonicalEventIdentity: appended.CanonicalEventIdentity,
		StableEventID:          stableEventID,
		Sequence:               projectionSequence,
		BatchOrdinal:           appended.BatchOrdinal,
		SourceGroupKey:         appended.SourceGroupKey,
		SourceGroupOrdinal:     appended.SourceGroupOrdinal,
	}
	return appended, identity, nil
}

func (c *MutationCoordinator) trimAdmissionIdentityCacheLocked() {
	limit := c.config.AdmissionIdentityCacheLimit
	if limit <= 0 || len(c.identityByAdmission) <= limit {
		return
	}
	candidates := len(c.identityCacheOrder) - c.identityCacheHead
	for len(c.identityByAdmission) > limit && candidates > 0 {
		entry := c.identityCacheOrder[c.identityCacheHead]
		c.identityCacheHead++
		candidates--
		if c.identityCacheVersion[entry.key] != entry.sequence {
			continue
		}
		if _, pending := c.pendingIdentity[entry.key]; pending {
			c.identityCacheOrder = append(c.identityCacheOrder, entry)
			continue
		}
		delete(c.identityByAdmission, entry.key)
		delete(c.identityCacheVersion, entry.key)
	}
	if c.identityCacheHead >= 1024 && c.identityCacheHead*2 >= len(c.identityCacheOrder) {
		order := append([]identityCacheOrderEntry(nil), c.identityCacheOrder[c.identityCacheHead:]...)
		c.identityCacheOrder = order
		c.identityCacheHead = 0
	}
}

func (c *MutationCoordinator) markTerminal(intent MutationIntent, state MutationTerminalState, err error) error {
	if c == nil || c.journal == nil {
		return ErrProjectionUnavailable
	}
	markErr := c.journal.MarkTerminal(intent.Sequence, state, err)
	if markErr != nil {
		return markErr
	}
	c.mu.Lock()
	reuseKey := admissionIdentityReuseKey(intent.APIName, intent.Detail, intent.BatchID, intent.AdmissionDiscriminator)
	if pending, ok := c.pendingIdentity[reuseKey]; ok && pending.sequence == intent.Sequence {
		delete(c.pendingIdentity, reuseKey)
	}
	c.mu.Unlock()
	c.completeApplySequence(intent.Sequence)
	return nil
}

func (c *MutationCoordinator) allocateSourceOrdinalLocked(groupKey string) uint64 {
	ordinal := c.sourceOrdinals[groupKey]
	c.sourceOrdinals[groupKey] = ordinal + 1
	return ordinal
}

func (c *MutationCoordinator) existingIdentity(apiName string, detail RequestDetail) (ProjectionIdentity, bool) {
	if c == nil || c.stats == nil {
		return ProjectionIdentity{}, false
	}
	c.stats.mu.RLock()
	projection := c.stats.projection
	if projection == nil {
		c.stats.mu.RUnlock()
		return ProjectionIdentity{}, false
	}
	identity, ok := projection.ExistingIdentity(apiName, detail)
	c.stats.mu.RUnlock()
	return identity, ok
}

// existingRestoredLegacyIdentity restores the deterministic enrichment
// behavior of the compatibility Record API after a generation hydrate. It is
// intentionally limited to exact logical matches and never applies to strict
// manager admissions or explicit reporter discriminators.
func (c *MutationCoordinator) existingRestoredLegacyIdentity(apiName string, detail RequestDetail, record coreusage.Record) (ProjectionIdentity, bool) {
	if c == nil || c.stats == nil || strings.TrimSpace(record.CanonicalIdentitySeed) != "" {
		return ProjectionIdentity{}, false
	}
	discriminator := strings.TrimSpace(record.AdmissionDiscriminator)
	if !strings.HasPrefix(discriminator, legacyRecordAdmissionPrefix) {
		return ProjectionIdentity{}, false
	}
	reuseKey := admissionIdentityReuseKey(apiName, detail, "", discriminator)
	c.mu.Lock()
	_, cached := c.identityByAdmission[reuseKey]
	if !cached {
		_, cached = c.pendingIdentity[reuseKey]
	}
	c.mu.Unlock()
	if cached {
		return ProjectionIdentity{}, false
	}
	c.stats.mu.RLock()
	projection := c.stats.projection
	metadataReady := c.stats.identityMetadataReady
	if projection == nil || !metadataReady {
		c.stats.mu.RUnlock()
		return ProjectionIdentity{}, false
	}
	identity, ok := projection.existingExactLogicalIdentity(apiName, detail)
	c.stats.mu.RUnlock()
	return identity, ok
}

func (c *MutationCoordinator) existingLogicalIdentity(apiName string, detail RequestDetail) (ProjectionIdentity, bool) {
	if c == nil || c.stats == nil {
		return ProjectionIdentity{}, false
	}
	c.stats.mu.RLock()
	projection := c.stats.projection
	if projection == nil {
		c.stats.mu.RUnlock()
		return ProjectionIdentity{}, false
	}
	identity, ok := projection.ExistingLogicalIdentity(apiName, detail)
	c.stats.mu.RUnlock()
	return identity, ok
}

func (c *MutationCoordinator) applyIntent(apiName string, intent MutationIntent, identity ProjectionIdentity) (detailUpsertStatus, error) {
	c.waitForApplyTurn(intent.Sequence)
	c.applyMu.Lock()
	status, err := c.applyIntentLocked(apiName, intent, identity)
	c.applyMu.Unlock()
	return status, err
}

func (c *MutationCoordinator) waitForApplyTurn(sequence uint64) {
	if c == nil || sequence == 0 {
		return
	}
	c.applyOrderMu.Lock()
	defer c.applyOrderMu.Unlock()
	for {
		c.advanceApplySequenceLocked()
		if sequence < c.nextApplySeq || sequence == c.nextApplySeq {
			return
		}
		c.applyOrderCond.Wait()
	}
}

func (c *MutationCoordinator) completeApplySequence(sequence uint64) {
	if c == nil || sequence == 0 {
		return
	}
	c.applyOrderMu.Lock()
	if sequence >= c.nextApplySeq {
		c.completedApply[sequence] = struct{}{}
		c.advanceApplySequenceLocked()
	}
	c.applyOrderCond.Broadcast()
	c.applyOrderMu.Unlock()
}

func (c *MutationCoordinator) advanceApplySequenceLocked() {
	if c.nextApplySeq == 0 {
		c.nextApplySeq = 1
	}
	for {
		if _, ok := c.completedApply[c.nextApplySeq]; !ok {
			return
		}
		delete(c.completedApply, c.nextApplySeq)
		c.nextApplySeq++
	}
}

func (c *MutationCoordinator) applyIntentLocked(apiName string, intent MutationIntent, identity ProjectionIdentity) (detailUpsertStatus, error) {
	if c.stats == nil {
		return detailUpsertSkipped, ErrProjectionUnavailable
	}
	c.stats.mu.Lock()
	defer c.stats.mu.Unlock()
	return c.stats.upsertDetailLockedWithIdentityBudget(apiName, intent.Detail.Model, intent.Detail, intent.IncomingGenerateExplicit, identity)
}

func (c *MutationCoordinator) ApplySnapshot(ctx context.Context, snapshot StatisticsSnapshot) (MergeResult, error) {
	return c.applySnapshot(ctx, snapshot, false, nil)
}

// ApplySnapshotWithIdentitySidecar restores a generation while preserving the
// immutable event identities allocated by the previous process.
func (c *MutationCoordinator) ApplySnapshotWithIdentitySidecar(ctx context.Context, snapshot StatisticsSnapshot, sidecar IdentitySidecar) (MergeResult, error) {
	return c.applySnapshot(ctx, snapshot, false, &sidecar)
}

// ApplySnapshotLegacy preserves the historical MergeSnapshot/restore
// behavior for unseeded snapshots. It still uses the coordinator and journal,
// but exact logical identities already present in the projection are reused
// across batches for deduplication and enrichment.
func (c *MutationCoordinator) ApplySnapshotLegacy(ctx context.Context, snapshot StatisticsSnapshot) (MergeResult, error) {
	return c.applySnapshot(ctx, snapshot, true, nil)
}

func (c *MutationCoordinator) applySnapshot(ctx context.Context, snapshot StatisticsSnapshot, reuseExistingLogical bool, sidecar *IdentitySidecar) (MergeResult, error) {
	items := sortedSnapshotDetails(snapshot)
	if len(items) > 0 && c.shouldUseBulkSnapshot(items) {
		return c.applySnapshotBulk(ctx, items, reuseExistingLogical, sidecar)
	}
	return c.applySnapshotStrict(ctx, items, reuseExistingLogical, sidecar)
}

func (c *MutationCoordinator) shouldUseBulkSnapshot(items []snapshotMutationDetail) bool {
	if c == nil || len(items) == 0 {
		return false
	}
	if c.config.MaxBulkChunkIntents > 0 && len(items) > c.config.MaxBulkChunkIntents {
		return true
	}
	if c.config.MaxBulkChunkBytes <= 0 {
		return false
	}
	var bytes int
	for _, item := range items {
		bytes += len(canonicalDetailBytes(item.detail)) + len(item.apiName) + len(item.modelName)
		if bytes > c.config.MaxBulkChunkBytes {
			return true
		}
	}
	return false
}

func (c *MutationCoordinator) applySnapshotStrict(ctx context.Context, items []snapshotMutationDetail, reuseExistingLogical bool, sidecar *IdentitySidecar) (MergeResult, error) {
	result := MergeResult{}
	if c == nil || c.stats == nil {
		return result, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if len(items) == 0 {
		return result, nil
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	releaseBatch, err := c.beginExclusiveBatch()
	if err != nil {
		return result, err
	}
	defer releaseBatch()
	batchSequence := c.batchSequence.Add(1)
	batchID := fmt.Sprintf("import:%d", batchSequence)
	release, reservationBytes, err := c.enterBatchForExclusive(ctx, len(items))
	if err != nil {
		return result, err
	}
	defer release()

	type admittedMutation struct {
		apiName  string
		intent   MutationIntent
		identity ProjectionIdentity
	}
	admitted := make([]admittedMutation, 0, len(items))
	sidecarOccurrences := make(map[string]uint64)
	var sidecarIndex map[string][]ProjectionIdentity
	if sidecar != nil {
		sidecarIndex = sidecar.identityIndex()
	}
	c.admissionMu.Lock()
	for batchOrdinal, item := range items {
		apiName := item.apiName
		modelName := item.modelName
		detail := item.detail
		detail.Endpoint = safeImportedEndpoint(apiName, detail.Endpoint)
		incomingGenerateExplicit := detail.Generate != nil
		if detail.Model == "" {
			detail.Model = modelName
		}
		detail = normalizeRequestDetail(detail, detail.Provider)
		targetAPIName := safeImportedAPIName(apiName, detail)
		var existingIdentity ProjectionIdentity
		if reuseExistingLogical {
			if candidate, ok := c.existingLogicalIdentity(targetAPIName, detail); ok {
				existingIdentity = candidate
			}
		}
		if sidecar != nil {
			key := sidecarDetailKey(targetAPIName, modelName, detail)
			occurrence := sidecarOccurrences[key]
			sidecarOccurrences[key] = occurrence + 1
			if candidates := sidecarIndex[key]; occurrence < uint64(len(candidates)) {
				candidate := candidates[occurrence]
				existingIdentity = candidate
			}
		}
		intent, identity, admitErr := c.admitWithExistingIdentity(detail, targetAPIName, MutationOperationImportMerge, batchID, batchSequence, uint64(batchOrdinal), coreusage.Record{}, incomingGenerateExplicit, reservationBytes, existingIdentity)
		if admitErr != nil {
			c.admissionMu.Unlock()
			remaining := len(items) - len(admitted) - 1
			if remaining > 0 {
				c.journal.ReleaseReservation(remaining * reservationBytes)
			}
			for _, previous := range admitted {
				_ = c.markTerminal(previous.intent, MutationTerminalTombstone, admitErr)
			}
			return result, admitErr
		}
		admitted = append(admitted, admittedMutation{apiName: targetAPIName, intent: intent, identity: identity})
	}
	c.admissionMu.Unlock()

	// Once the whole batch owns journal capacity and has been admitted, caller
	// cancellation cannot turn it into a partially committed import. Wait for
	// the first live sequence before taking applyMu so an earlier Record cannot
	// be blocked behind this batch while it owns the write lock.
	for _, mutation := range admitted {
		if _, terminal := c.journal.Outcome(mutation.intent.Sequence); !terminal {
			c.waitForApplyTurn(mutation.intent.Sequence)
			break
		}
	}
	c.applyMu.Lock()
	defer c.applyMu.Unlock()
	c.stats.mu.Lock()
	before := c.stats.cloneStateLocked()
	c.stats.mu.Unlock()
	statuses := make([]detailUpsertStatus, 0, len(admitted))
	for _, mutation := range admitted {
		intent := mutation.intent
		if _, terminal := c.journal.Outcome(intent.Sequence); terminal {
			c.completeApplySequence(intent.Sequence)
			statuses = append(statuses, detailUpsertSkipped)
			continue
		}
		status, applyErr := c.applyIntentLocked(mutation.apiName, intent, mutation.identity)
		if applyErr != nil {
			c.stats.mu.Lock()
			c.stats.restoreStateLocked(before)
			c.stats.mu.Unlock()
			for _, pending := range admitted {
				_ = c.markTerminal(pending.intent, MutationTerminalTombstone, applyErr)
			}
			return MergeResult{}, applyErr
		}
		statuses = append(statuses, status)
	}
	for index, mutation := range admitted {
		if _, terminal := c.journal.Outcome(mutation.intent.Sequence); terminal {
			result.Skipped++
			continue
		}
		if terminalErr := c.markTerminal(mutation.intent, MutationTerminalCommitted, nil); terminalErr != nil {
			return MergeResult{}, terminalErr
		}
		switch statuses[index] {
		case detailUpsertAdded:
			result.Added++
		case detailUpsertEnriched:
			result.Enriched++
		default:
			result.Skipped++
		}
	}
	return result, nil
}

type bulkAdmissionCheckpoint struct {
	sourceOrdinals       map[string]uint64
	pendingIdentity      map[string]pendingAdmission
	identityByAdmission  map[string]ProjectionIdentity
	identityCacheOrder   []identityCacheOrderEntry
	identityCacheHead    int
	identityCacheVersion map[string]uint64
}

func (c *MutationCoordinator) captureBulkAdmissionCheckpoint() bulkAdmissionCheckpoint {
	checkpoint := bulkAdmissionCheckpoint{
		sourceOrdinals:       make(map[string]uint64),
		pendingIdentity:      make(map[string]pendingAdmission),
		identityByAdmission:  make(map[string]ProjectionIdentity),
		identityCacheVersion: make(map[string]uint64),
	}
	if c == nil {
		return checkpoint
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for key, value := range c.sourceOrdinals {
		checkpoint.sourceOrdinals[key] = value
	}
	for key, value := range c.pendingIdentity {
		checkpoint.pendingIdentity[key] = value
	}
	for key, value := range c.identityByAdmission {
		checkpoint.identityByAdmission[key] = value
	}
	checkpoint.identityCacheOrder = append(checkpoint.identityCacheOrder, c.identityCacheOrder...)
	checkpoint.identityCacheHead = c.identityCacheHead
	for key, value := range c.identityCacheVersion {
		checkpoint.identityCacheVersion[key] = value
	}
	return checkpoint
}

func (c *MutationCoordinator) restoreBulkAdmissionCheckpoint(checkpoint bulkAdmissionCheckpoint) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.sourceOrdinals = checkpoint.sourceOrdinals
	c.pendingIdentity = checkpoint.pendingIdentity
	c.identityByAdmission = checkpoint.identityByAdmission
	c.identityCacheOrder = checkpoint.identityCacheOrder
	c.identityCacheHead = checkpoint.identityCacheHead
	c.identityCacheVersion = checkpoint.identityCacheVersion
	c.mu.Unlock()
}

func (c *MutationCoordinator) applySnapshotBulk(ctx context.Context, items []snapshotMutationDetail, reuseExistingLogical bool, sidecar *IdentitySidecar) (MergeResult, error) {
	result := MergeResult{}
	if c == nil || c.stats == nil {
		return result, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if len(items) == 0 {
		return result, nil
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	releaseBatch, err := c.beginExclusiveBatch()
	if err != nil {
		return result, err
	}
	defer releaseBatch()
	// Do not clone live state while an already admitted Record is still
	// applying. New admissions are blocked by activeBatch, while this bounded
	// wait lets in-flight intents terminalize without taking a long write lock.
	if err := c.waitForInFlightIdle(ctx); err != nil {
		return result, err
	}

	staged := &RequestStatistics{}
	c.stats.mu.RLock()
	stagedState := c.stats.cloneStateLocked()
	c.stats.mu.RUnlock()
	staged.mu.Lock()
	staged.adoptStateLocked(stagedState)
	staged.mu.Unlock()
	baseDatasetEpoch, baseRevision, _ := stagedState.projection.metadata()
	operation := MutationOperationImportMerge
	if sidecar != nil {
		operation = MutationOperationRestore
		if sidecar.BulkTransaction != nil {
			c.advanceBatchSequence(sidecar.BulkTransaction.BatchSequence)
		}
	}
	checkpoint := c.captureBulkAdmissionCheckpoint()
	batchSequence := c.batchSequence.Add(1)
	batchID := fmt.Sprintf("%s:%d", operation, batchSequence)
	previousMarkerSHA256 := ""
	if sidecar != nil && sidecar.BulkTransaction != nil {
		previousMarkerSHA256 = sidecar.BulkTransaction.MarkerSHA256
	}
	if err := c.journal.BeginBulkTransaction(MutationBatchMarker{
		BatchID:              batchID,
		BatchSequence:        batchSequence,
		Operation:            operation,
		BaseRevision:         baseRevision,
		DatasetEpoch:         baseDatasetEpoch,
		TotalIntents:         uint64(len(items)),
		PayloadSHA256:        bulkSnapshotPayloadChecksum(items),
		PreviousMarkerSHA256: previousMarkerSHA256,
	}); err != nil {
		return result, err
	}
	var sidecarIndex map[string][]ProjectionIdentity
	if sidecar != nil {
		sidecarIndex = sidecar.identityIndex()
	}
	sidecarOccurrences := make(map[string]uint64)

	failBulk := func(failure error) (MergeResult, error) {
		if _, terminalErr := c.journal.CompleteBulkTransaction(batchID, MutationTerminalTombstone, mutationBatchFailureCode(failure), ""); terminalErr != nil && !errors.Is(terminalErr, ErrMutationBatchNotFound) {
			failure = fmt.Errorf("%w (bulk terminal outcome: %v)", failure, terminalErr)
		}
		c.restoreBulkAdmissionCheckpoint(checkpoint)
		return MergeResult{}, failure
	}

	for start := 0; start < len(items); {
		if err := ctx.Err(); err != nil {
			return failBulk(err)
		}
		end := c.bulkChunkEnd(items, start)
		if end <= start {
			end = start + 1
		}
		chunkCount := end - start
		release, reservationBytes, reserveErr := c.enterBatchForExclusive(ctx, chunkCount)
		if reserveErr != nil {
			return failBulk(reserveErr)
		}

		type admittedMutation struct {
			apiName  string
			intent   MutationIntent
			identity ProjectionIdentity
		}
		admitted := make([]admittedMutation, 0, chunkCount)
		chunkSHA256 := bulkSnapshotChunkChecksum(items[start:end], uint64(start), uint64(end))
		admissionErr := error(nil)
		c.admissionMu.Lock()
		for offset := start; offset < end; offset++ {
			item := items[offset]
			apiName := item.apiName
			modelName := item.modelName
			detail := item.detail
			detail.Endpoint = safeImportedEndpoint(apiName, detail.Endpoint)
			incomingGenerateExplicit := detail.Generate != nil
			if detail.Model == "" {
				detail.Model = modelName
			}
			detail = normalizeRequestDetail(detail, detail.Provider)
			targetAPIName := safeImportedAPIName(apiName, detail)
			var existingIdentity ProjectionIdentity
			if reuseExistingLogical {
				if candidate, ok := c.existingLogicalIdentity(targetAPIName, detail); ok {
					existingIdentity = candidate
				}
			}
			if sidecar != nil {
				key := sidecarDetailKey(targetAPIName, modelName, detail)
				occurrence := sidecarOccurrences[key]
				sidecarOccurrences[key] = occurrence + 1
				if candidates := sidecarIndex[key]; occurrence < uint64(len(candidates)) {
					existingIdentity = candidates[occurrence]
				}
			}
			intent, identity, admitErr := c.admitWithExistingIdentity(
				detail,
				targetAPIName,
				MutationOperationImportMerge,
				batchID,
				batchSequence,
				uint64(offset),
				coreusage.Record{},
				incomingGenerateExplicit,
				reservationBytes,
				existingIdentity,
			)
			if admitErr != nil {
				admissionErr = admitErr
				break
			}
			admitted = append(admitted, admittedMutation{apiName: targetAPIName, intent: intent, identity: identity})
		}
		c.admissionMu.Unlock()
		if admissionErr != nil {
			remaining := chunkCount - len(admitted) - 1
			if remaining > 0 {
				c.journal.ReleaseReservation(remaining * reservationBytes)
			}
			for _, mutation := range admitted {
				if _, terminal := c.journal.Outcome(mutation.intent.Sequence); !terminal {
					_ = c.markTerminal(mutation.intent, MutationTerminalTombstone, admissionErr)
				}
			}
			if len(admitted) > 0 {
				c.journal.CompactTerminalBefore(admitted[len(admitted)-1].intent.Sequence)
			}
			release()
			return failBulk(admissionErr)
		}

		if len(admitted) > 0 {
			c.waitForApplyTurn(admitted[0].intent.Sequence)
		}
		statuses := make([]detailUpsertStatus, 0, len(admitted))
		var applyErr error
		for _, mutation := range admitted {
			if _, terminal := c.journal.Outcome(mutation.intent.Sequence); terminal {
				statuses = append(statuses, detailUpsertSkipped)
				continue
			}
			staged.mu.Lock()
			status, budgetErr := staged.upsertDetailLockedWithIdentityBudget(mutation.apiName, mutation.intent.Detail.Model, mutation.intent.Detail, mutation.intent.IncomingGenerateExplicit, mutation.identity)
			staged.mu.Unlock()
			if budgetErr != nil {
				applyErr = budgetErr
				break
			}
			statuses = append(statuses, status)
		}
		if applyErr != nil {
			for _, mutation := range admitted {
				if _, terminal := c.journal.Outcome(mutation.intent.Sequence); !terminal {
					_ = c.markTerminal(mutation.intent, MutationTerminalTombstone, applyErr)
				}
			}
			if len(admitted) > 0 {
				c.journal.CompactTerminalBefore(admitted[len(admitted)-1].intent.Sequence)
			}
			release()
			return failBulk(applyErr)
		}
		for index, mutation := range admitted {
			if _, terminal := c.journal.Outcome(mutation.intent.Sequence); terminal {
				result.Skipped++
				continue
			}
			if terminalErr := c.markTerminal(mutation.intent, MutationTerminalCommitted, nil); terminalErr != nil {
				for _, pending := range admitted[index+1:] {
					if _, terminal := c.journal.Outcome(pending.intent.Sequence); !terminal {
						_ = c.markTerminal(pending.intent, MutationTerminalTombstone, terminalErr)
					}
				}
				if len(admitted) > 0 {
					c.journal.CompactTerminalBefore(admitted[len(admitted)-1].intent.Sequence)
				}
				release()
				return failBulk(terminalErr)
			}
			switch statuses[index] {
			case detailUpsertAdded:
				result.Added++
			case detailUpsertEnriched:
				result.Enriched++
			default:
				result.Skipped++
			}
		}
		marker, markerErr := c.journal.AdvanceBulkChunk(batchID, uint64(start), uint64(end), chunkSHA256)
		if markerErr != nil {
			if len(admitted) > 0 {
				c.journal.CompactTerminalBefore(admitted[len(admitted)-1].intent.Sequence)
			}
			release()
			return failBulk(markerErr)
		}
		if len(admitted) > 0 {
			c.journal.CompactTerminalBefore(admitted[len(admitted)-1].intent.Sequence)
		}
		release()
		if c.bulkChunkDoneHook != nil {
			c.bulkChunkDoneHook(marker)
		}
		start = end
	}

	marker, ok := c.journal.BulkTransactionSnapshot()
	if !ok || marker.BatchID != batchID || marker.TerminalState != "" || marker.FrontierOrdinal != uint64(len(items)) {
		return failBulk(ErrMutationBatchIncomplete)
	}
	c.stats.mu.RLock()
	liveProjection := c.stats.projection
	c.stats.mu.RUnlock()
	liveDatasetEpoch, liveRevision, _ := liveProjection.metadata()
	if liveDatasetEpoch != baseDatasetEpoch || liveRevision != baseRevision {
		return failBulk(ErrMutationBatchFrontier)
	}
	staged.mu.Lock()
	budgetErr := staged.projection.ValidateStorageBudget()
	staged.mu.Unlock()
	if budgetErr != nil {
		return failBulk(budgetErr)
	}
	manifestSHA256 := bulkTransactionManifestChecksum(marker, result)
	if _, terminalErr := c.journal.CompleteBulkTransaction(batchID, MutationTerminalCommitted, "", manifestSHA256); terminalErr != nil {
		return failBulk(terminalErr)
	}

	// Publish the completed candidate exactly once. The batch owner prevents
	// new live admissions, so this short critical section cannot expose a half
	// applied chunk and does not require another full state clone.
	c.applyMu.Lock()
	c.stats.mu.Lock()
	staged.mu.Lock()
	finalState := staged.detachStateLocked()
	staged.mu.Unlock()
	c.stats.adoptStateLocked(finalState)
	c.stats.projectionUnavailable = false
	c.stats.projectionUnavailableCode = ""
	c.stats.mu.Unlock()
	c.applyMu.Unlock()
	return result, nil
}

func (c *MutationCoordinator) bulkChunkEnd(items []snapshotMutationDetail, start int) int {
	if c == nil || start >= len(items) {
		return start
	}
	maxIntents := c.config.MaxBulkChunkIntents
	if maxIntents <= 0 || maxIntents > c.config.JournalEntryLimit {
		maxIntents = c.config.JournalEntryLimit
	}
	if maxIntents <= 0 {
		maxIntents = 1
	}
	maxBytes := c.config.MaxBulkChunkBytes
	usedBytes := 0
	end := start
	for end < len(items) && end-start < maxIntents {
		item := items[end]
		itemBytes := len(canonicalDetailBytes(item.detail)) + len(item.apiName) + len(item.modelName) + c.config.MaxSerializedIntentBytes
		if end > start && maxBytes > 0 && usedBytes+itemBytes > maxBytes {
			break
		}
		usedBytes += itemBytes
		end++
	}
	return end
}

func bulkSnapshotPayloadChecksum(items []snapshotMutationDetail) string {
	hasher := sha256.New()
	writeDigestPart(hasher, mutationBulkTransactionVersionV1)
	writeDigestPart(hasher, formatUint(uint64(len(items))))
	for _, item := range items {
		writeDigestPart(hasher, item.apiName)
		writeDigestPart(hasher, item.modelName)
		writeDigestPart(hasher, string(canonicalDetailBytes(item.detail)))
	}
	return hex.EncodeToString(hasher.Sum(nil))
}

func bulkSnapshotChunkChecksum(items []snapshotMutationDetail, startOrdinal, endOrdinal uint64) string {
	hasher := sha256.New()
	writeDigestPart(hasher, mutationBulkTransactionVersionV1+":chunk")
	writeDigestPart(hasher, formatUint(startOrdinal))
	writeDigestPart(hasher, formatUint(endOrdinal))
	for _, item := range items {
		writeDigestPart(hasher, item.apiName)
		writeDigestPart(hasher, item.modelName)
		writeDigestPart(hasher, string(canonicalDetailBytes(item.detail)))
	}
	return hex.EncodeToString(hasher.Sum(nil))
}

func bulkTransactionManifestChecksum(marker MutationBatchMarker, result MergeResult) string {
	return mutationSHA256Parts(
		mutationBulkTransactionVersionV1+":manifest",
		marker.MarkerSHA256,
		marker.PayloadSHA256,
		marker.ChunkChecksumChain,
		formatUint(marker.BaseRevision),
		formatUint(marker.DatasetEpoch),
		formatUint(marker.TotalIntents),
		formatUint(uint64(result.Added)),
		formatUint(uint64(result.Enriched)),
		formatUint(uint64(result.Skipped)),
	)
}

func mutationBatchFailureCode(err error) string {
	switch {
	case errors.Is(err, context.Canceled):
		return "context_canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline_exceeded"
	case errors.Is(err, ErrMutationJournalFull):
		return "journal_full"
	case errors.Is(err, ErrMutationJournalBudget):
		return "journal_budget"
	case errors.Is(err, ErrProjectionBudgetExceeded):
		return "projection_budget"
	case errors.Is(err, ErrMutationBatchChecksum):
		return "checksum_mismatch"
	case errors.Is(err, ErrMutationBatchFrontier), errors.Is(err, ErrMutationBatchIncomplete):
		return "frontier_mismatch"
	default:
		return "batch_failed"
	}
}

func (c *MutationCoordinator) advanceBatchSequence(sequence uint64) {
	if c == nil || sequence == 0 {
		return
	}
	for {
		current := c.batchSequence.Load()
		if current >= sequence || c.batchSequence.CompareAndSwap(current, sequence) {
			return
		}
	}
}

type snapshotMutationDetail struct {
	apiName       string
	modelName     string
	detail        RequestDetail
	sourceOrdinal uint64
	sortKey       []byte
}

func sortedSnapshotDetails(snapshot StatisticsSnapshot) []snapshotMutationDetail {
	apiNames := make([]string, 0, len(snapshot.APIs))
	for apiName := range snapshot.APIs {
		apiNames = append(apiNames, apiName)
	}
	sort.Strings(apiNames)
	items := make([]snapshotMutationDetail, 0)
	var sourceOrdinal uint64
	for _, apiName := range apiNames {
		apiSnapshot := snapshot.APIs[apiName]
		modelNames := make([]string, 0, len(apiSnapshot.Models))
		for modelName := range apiSnapshot.Models {
			modelNames = append(modelNames, modelName)
		}
		sort.Strings(modelNames)
		for _, modelName := range modelNames {
			for _, detail := range apiSnapshot.Models[modelName].Details {
				sortKey := CanonicalSortKeyV1(apiName, detail, 0)
				items = append(items, snapshotMutationDetail{
					apiName:       apiName,
					modelName:     modelName,
					detail:        detail,
					sourceOrdinal: sourceOrdinal,
					sortKey:       sortKey,
				})
				sourceOrdinal++
			}
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		if comparison := bytes.Compare(items[i].sortKey, items[j].sortKey); comparison != 0 {
			return comparison < 0
		}
		if items[i].modelName != items[j].modelName {
			return items[i].modelName < items[j].modelName
		}
		return items[i].sourceOrdinal < items[j].sourceOrdinal
	})
	for index := range items {
		items[index].sortKey = nil
	}
	return items
}

func (c *MutationCoordinator) beginInFlight() {
	c.mu.Lock()
	if c.inFlight == 0 {
		c.inFlightDone = make(chan struct{})
	}
	c.inFlight++
	c.mu.Unlock()
}

func (c *MutationCoordinator) endInFlight() {
	c.mu.Lock()
	c.inFlight--
	if c.inFlight <= 0 {
		c.inFlight = 0
		select {
		case <-c.inFlightDone:
		default:
			close(c.inFlightDone)
		}
	}
	c.mu.Unlock()
}

func (c *MutationCoordinator) BeginRebuild(ctx context.Context) (*RebuildCandidate, error) {
	if c == nil {
		return nil, ErrProjectionUnavailable
	}
	if err := ctxErr(ctx); err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.activeCandidate != nil || c.activeCapture != nil || c.activeBatch {
		return nil, ErrProjectionRebuildInProgress
	}
	if err := c.gate.begin(); err != nil {
		return nil, err
	}
	candidate := &RebuildCandidate{
		BaseJournalHead:   c.journal.Head(),
		StartedAt:         time.Now().UTC(),
		MaxSuffixIntents:  c.config.MaxFinalizeReplayIntents,
		replayedSequences: make(map[uint64]struct{}),
	}
	if c.stats != nil {
		c.stats.mu.RLock()
		projection := c.stats.projection
		if projection != nil {
			candidate.Projection = projection.Clone()
			candidate.DatasetEpoch, candidate.BaseRevision, _ = candidate.Projection.metadata()
		}
		c.stats.mu.RUnlock()
	}
	if candidate.Projection == nil {
		candidate.Projection = NewUsageProjection()
		candidate.Projection.SetBudget(c.config.ProjectionBudget)
	}
	c.activeCandidate = candidate
	return candidate, nil
}

func (c *MutationCoordinator) FinalizeRebuild(ctx context.Context, candidate *RebuildCandidate) error {
	if c == nil || candidate == nil {
		return ErrProjectionUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctxErr(ctx); err != nil {
		return err
	}
	c.mu.Lock()
	if c.activeCandidate != candidate {
		c.mu.Unlock()
		return ErrProjectionCASConflict
	}
	waitDone := c.inFlightDone
	c.mu.Unlock()
	select {
	case <-waitDone:
	case <-ctx.Done():
		c.abortRebuild(candidate)
		return ctx.Err()
	}
	if candidate.Projection == nil {
		candidate.Projection = NewUsageProjection()
	}
	if candidate.replayedSequences == nil {
		candidate.replayedSequences = make(map[uint64]struct{})
	}
	for {
		if err := ctx.Err(); err != nil {
			c.abortRebuild(candidate)
			return err
		}
		suffix := c.journal.EntriesAfter(candidate.BaseJournalHead, false)
		if len(suffix) > candidate.MaxSuffixIntents {
			c.abortRebuild(candidate)
			return ErrProjectionUnavailable
		}
		replayed := 0
		for _, intent := range suffix {
			outcome, terminal := c.journal.Outcome(intent.Sequence)
			if !terminal {
				c.abortRebuild(candidate)
				return ErrProjectionUnavailable
			}
			if _, alreadyReplayed := candidate.replayedSequences[intent.Sequence]; alreadyReplayed {
				continue
			}
			if outcome.State == MutationTerminalCommitted {
				identity := ProjectionIdentity{
					CanonicalIdentitySeed:  intent.CanonicalIdentitySeed,
					CanonicalEventIdentity: intent.CanonicalEventIdentity,
					StableEventID:          intent.StableEventID,
					SourceGroupKey:         intent.SourceGroupKey,
					SourceGroupOrdinal:     intent.SourceGroupOrdinal,
					Sequence:               intent.ProjectionSequence,
					BatchOrdinal:           intent.BatchOrdinal,
				}
				if identity.Sequence == 0 {
					identity.Sequence = intent.Sequence
				}
				if err := candidate.Projection.CheckDetailBudget(intent.APIName, intent.Detail, identity); err != nil {
					c.abortRebuild(candidate)
					return err
				}
				candidate.Projection.ApplyDetailWithIdentity(intent.APIName, intent.Detail, identity)
			}
			candidate.replayedSequences[intent.Sequence] = struct{}{}
			replayed++
		}
		candidate.SuffixReplayCount += replayed
		if err := candidate.Projection.ValidateStorageBudget(); err != nil {
			c.abortRebuild(candidate)
			return err
		}

		currentProjection, currentSnapshot := c.currentProjectionClone()
		currentHead := c.journal.Head()
		candidateSnapshot := candidate.Projection.Snapshot()
		if projectionSnapshotsEquivalent(candidateSnapshot, currentSnapshot) &&
			currentHead == candidate.BaseJournalHead &&
			currentSnapshot.Revision == candidate.BaseRevision &&
			currentSnapshot.DatasetEpoch == candidate.DatasetEpoch {
			if c.tryCommitRebuild(candidate, candidateSnapshot) {
				return nil
			}
			if err := c.rebaseCandidate(candidate, ctx); err != nil {
				c.abortRebuild(candidate)
				return err
			}
			continue
		}

		candidate.CASRetryCount++
		candidate.Rebased = true
		if candidate.CASRetryCount > c.config.MaxFinalizeRetries {
			c.abortRebuild(candidate)
			return ErrProjectionCASConflict
		}
		if !projectionSnapshotsEquivalent(candidateSnapshot, currentSnapshot) {
			candidate.Projection = currentProjection
			candidate.replayedSequences = make(map[uint64]struct{})
		}
		candidate.BaseRevision = currentSnapshot.Revision
		candidate.BaseJournalHead = currentHead
		candidate.DatasetEpoch = currentSnapshot.DatasetEpoch
	}
}

func (c *MutationCoordinator) currentProjectionClone() (*UsageProjection, ProjectionSnapshot) {
	if c == nil || c.stats == nil {
		projection := NewUsageProjection()
		return projection, projection.Snapshot()
	}
	c.stats.mu.RLock()
	projection := c.stats.projection
	if projection == nil {
		projection = NewUsageProjection()
	}
	clone := projection.Clone()
	c.stats.mu.RUnlock()
	return clone, clone.Snapshot()
}

func (c *MutationCoordinator) tryCommitRebuild(candidate *RebuildCandidate, candidateSnapshot ProjectionSnapshot) bool {
	if c == nil || c.stats == nil {
		return false
	}
	c.mu.Lock()
	if c.activeCandidate != candidate {
		c.mu.Unlock()
		return false
	}
	c.stats.mu.Lock()
	live := c.stats.projection
	if live == nil {
		live = NewUsageProjection()
	}
	liveSnapshot := live.Snapshot()
	currentHead := c.journal.Head()
	if !projectionSnapshotsEquivalent(candidateSnapshot, liveSnapshot) ||
		currentHead != candidate.BaseJournalHead ||
		liveSnapshot.Revision != candidate.BaseRevision ||
		liveSnapshot.DatasetEpoch != candidate.DatasetEpoch {
		c.stats.mu.Unlock()
		c.mu.Unlock()
		return false
	}
	c.stats.projection = candidate.Projection
	c.stats.mu.Unlock()
	c.mu.Unlock()
	c.gate.end()
	c.mu.Lock()
	if c.activeCandidate == candidate {
		c.activeCandidate = nil
	}
	c.mu.Unlock()
	return true
}

func (c *MutationCoordinator) rebaseCandidate(candidate *RebuildCandidate, ctx context.Context) error {
	if err := ctxErr(ctx); err != nil {
		return err
	}
	candidate.CASRetryCount++
	candidate.Rebased = true
	if candidate.CASRetryCount > c.config.MaxFinalizeRetries {
		return ErrProjectionCASConflict
	}
	currentProjection, currentSnapshot := c.currentProjectionClone()
	candidate.Projection = currentProjection
	candidate.replayedSequences = make(map[uint64]struct{})
	candidate.BaseRevision = currentSnapshot.Revision
	candidate.BaseJournalHead = c.journal.Head()
	candidate.DatasetEpoch = currentSnapshot.DatasetEpoch
	return nil
}

func projectionSnapshotsEquivalent(left, right ProjectionSnapshot) bool {
	// CAS must cover every derived view, not only totals and EventRef. A
	// reverse enrichment order can leave those fields equal while pricing
	// groups, facets, rollups, postings, or catalog entries differ.
	return reflect.DeepEqual(left, right)
}

func (c *MutationCoordinator) abortRebuild(candidate *RebuildCandidate) {
	c.mu.Lock()
	active := c.activeCandidate == candidate
	c.mu.Unlock()
	if !active {
		return
	}
	c.gate.end()
	c.mu.Lock()
	if c.activeCandidate == candidate {
		c.activeCandidate = nil
	}
	c.mu.Unlock()
}

// beginExclusiveBatch reserves the shared import/restore slot. Record and
// enrichment admission remain independent so they can continue to terminalize
// while an exclusive batch is applying.
func (c *MutationCoordinator) beginExclusiveBatch() (func(), error) {
	if c == nil {
		return nil, ErrProjectionUnavailable
	}
	c.mu.Lock()
	if c.activeCandidate != nil || c.activeCapture != nil || c.activeBatch {
		c.mu.Unlock()
		return nil, ErrProjectionRebuildInProgress
	}
	batchDone := c.activateExclusiveBatchLocked()
	c.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			c.releaseExclusiveBatch(batchDone)
		})
	}, nil
}

func (c *MutationCoordinator) beginProjectionCapture(ctx context.Context) (*projectionCaptureSession, error) {
	if c == nil {
		return nil, ErrProjectionUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	c.mu.Lock()
	if c.activeCandidate != nil || c.activeCapture != nil || c.activeBatch {
		c.mu.Unlock()
		return nil, ErrProjectionRebuildInProgress
	}
	session := &projectionCaptureSession{coordinator: c}
	c.activeCapture = session
	session.batchDone = c.activateExclusiveBatchLocked()
	c.mu.Unlock()
	if err := c.waitForInFlightIdle(ctx); err != nil {
		session.finish()
		return nil, err
	}
	return session, nil
}

func (session *projectionCaptureSession) releaseFence() {
	if session == nil || session.coordinator == nil || session.batchDone == nil {
		return
	}
	session.coordinator.releaseExclusiveBatch(session.batchDone)
	session.batchDone = nil
}

func (session *projectionCaptureSession) setBaseJournalHead(head uint64) error {
	if session == nil || session.coordinator == nil {
		return ErrProjectionUnavailable
	}
	c := session.coordinator
	c.mu.Lock()
	defer c.mu.Unlock()
	if session.finished || c.activeCapture != session {
		return ErrProjectionRebuildInProgress
	}
	session.baseJournalHead = head
	session.baseJournalHeadSet = true
	return nil
}

func (session *projectionCaptureSession) beginFinalFence(ctx context.Context) error {
	if session == nil || session.coordinator == nil {
		return ErrProjectionUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	c := session.coordinator
	c.mu.Lock()
	if session.finished || c.activeCapture != session || c.activeCandidate != nil || c.activeBatch {
		c.mu.Unlock()
		return ErrProjectionRebuildInProgress
	}
	session.batchDone = c.activateExclusiveBatchLocked()
	c.mu.Unlock()
	if err := c.waitForInFlightIdle(ctx); err != nil {
		session.releaseFence()
		return err
	}
	return nil
}

func (session *projectionCaptureSession) finish() {
	if session == nil || session.coordinator == nil || session.finished {
		return
	}
	session.releaseFence()
	c := session.coordinator
	c.mu.Lock()
	if c.activeCapture == session {
		c.activeCapture = nil
	}
	session.finished = true
	c.mu.Unlock()
}

func (c *MutationCoordinator) activateExclusiveBatchLocked() chan struct{} {
	c.activeBatch = true
	batchDone := make(chan struct{})
	c.batchDone = batchDone
	return batchDone
}

func (c *MutationCoordinator) releaseExclusiveBatch(batchDone chan struct{}) {
	if c == nil || batchDone == nil {
		return
	}
	c.mu.Lock()
	if c.activeBatch && c.batchDone == batchDone {
		c.activeBatch = false
		close(batchDone)
		closed := make(chan struct{})
		close(closed)
		c.batchDone = closed
	}
	c.mu.Unlock()
}

func (c *MutationCoordinator) waitForExclusiveBatch(ctx context.Context) error {
	if c == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		c.mu.Lock()
		active := c.activeBatch
		done := c.batchDone
		c.mu.Unlock()
		if !active {
			return nil
		}
		if done == nil {
			done = make(chan struct{})
		}
		select {
		case <-done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (c *MutationCoordinator) waitForInFlightIdle(ctx context.Context) error {
	if c == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		c.mu.Lock()
		inFlight := c.inFlight
		done := c.inFlightDone
		c.mu.Unlock()
		if inFlight == 0 {
			return nil
		}
		if done == nil {
			done = make(chan struct{})
		}
		select {
		case <-done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (c *MutationCoordinator) RebuildActive() bool {
	if c == nil {
		return false
	}
	return c.gate.activeState()
}

func (c *MutationCoordinator) InFlight() int {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.inFlight
}

// WaitForIdle waits until no accepted mutation or exclusive rebuild remains in
// flight, so a persisted generation cannot capture an admitted-but-nonterminal
// journal suffix.
func (c *MutationCoordinator) WaitForIdle(ctx context.Context) error {
	if c == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		c.mu.Lock()
		inFlight := c.inFlight
		active := c.activeCandidate != nil || c.activeCapture != nil || c.activeBatch
		done := c.inFlightDone
		c.mu.Unlock()
		if inFlight == 0 && !active {
			return nil
		}
		if inFlight == 0 && active {
			return ErrProjectionRebuildInProgress
		}
		if done == nil {
			done = make(chan struct{})
		}
		select {
		case <-done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func ctxErr(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}
