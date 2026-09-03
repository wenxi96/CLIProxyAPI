package usage

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

var (
	ErrMutationJournalFull     = errors.New("usage mutation journal is full")
	ErrMutationJournalBudget   = errors.New("usage mutation journal budget exceeded")
	ErrMutationAlreadyTerminal = errors.New("usage mutation intent is already terminal")
	ErrMutationBatchActive     = errors.New("usage mutation bulk transaction is already active")
	ErrMutationBatchNotFound   = errors.New("usage mutation bulk transaction was not found")
	ErrMutationBatchFrontier   = errors.New("usage mutation bulk transaction frontier mismatch")
	ErrMutationBatchChecksum   = errors.New("usage mutation bulk transaction checksum mismatch")
	ErrMutationBatchIncomplete = errors.New("usage mutation bulk transaction is incomplete")
)

const (
	mutationBulkTransactionVersionV1 = "bulk_transaction_v1"
	maxMutationBulkMarkerBytes       = 4096
)

type MutationOperation string

const (
	MutationOperationRecord      MutationOperation = "record"
	MutationOperationEnrichment  MutationOperation = "enrichment"
	MutationOperationImportMerge MutationOperation = "import_merge"
	MutationOperationRestore     MutationOperation = "restore"
	MutationOperationTombstone   MutationOperation = "tombstone"
)

type MutationTerminalState string

const (
	MutationTerminalCommitted MutationTerminalState = "committed"
	MutationTerminalTombstone MutationTerminalState = "terminal_tombstone"
)

// MutationIntent is the canonical replay unit. It carries canonical detail
// bytes rather than projection deltas so a rebuild can run the same resolver.
type MutationIntent struct {
	Sequence                 uint64
	BatchID                  string
	BatchSequence            uint64
	BatchOrdinal             uint64
	Operation                MutationOperation
	APIName                  string
	CoordinateKey            string
	StableEventID            string
	ProjectionSequence       uint64
	LogicalIdentity          string
	CanonicalEventIdentity   string
	CanonicalIdentitySeed    string
	SourceGroupKey           string
	SourceGroupOrdinal       uint64
	AdmissionDiscriminator   string
	Detail                   RequestDetail
	IncomingGenerateExplicit bool
	IdempotencyKey           string
	ObservedLiveVersion      uint64
	AdmittedAt               time.Time
}

type MutationTerminalOutcome struct {
	Sequence uint64
	State    MutationTerminalState
	Err      error
}

// MutationBatchMarker is the bounded replay frontier for one bulk restore or
// import. Chunk intents may be compacted after they terminalize because this
// marker retains the canonical payload checksum, contiguous frontier, rolling
// chunk checksum chain, and batch-level terminal outcome.
type MutationBatchMarker struct {
	SchemaVersion        string                `json:"schema_version"`
	BatchID              string                `json:"batch_id"`
	BatchSequence        uint64                `json:"batch_sequence"`
	Operation            MutationOperation     `json:"operation"`
	BaseRevision         uint64                `json:"base_revision"`
	DatasetEpoch         uint64                `json:"dataset_epoch"`
	TotalIntents         uint64                `json:"total_intents"`
	FrontierOrdinal      uint64                `json:"frontier_ordinal"`
	ChunkCount           uint64                `json:"chunk_count"`
	PayloadSHA256        string                `json:"payload_sha256"`
	LastChunkSHA256      string                `json:"last_chunk_sha256,omitempty"`
	ChunkChecksumChain   string                `json:"chunk_checksum_chain"`
	ManifestSHA256       string                `json:"manifest_sha256,omitempty"`
	TerminalState        MutationTerminalState `json:"terminal_state,omitempty"`
	FailureCode          string                `json:"failure_code,omitempty"`
	PreviousMarkerSHA256 string                `json:"previous_marker_sha256,omitempty"`
	MarkerSHA256         string                `json:"marker_sha256"`
}

type mutationJournalEntry struct {
	Intent  MutationIntent
	Outcome *MutationTerminalOutcome
	Size    int
}

// MutationJournal is an in-memory bounded intent journal. Terminal prefixes
// can be compacted once no active rebuild candidate can replay them. Durable
// identity/journal persistence remains an L04 concern.
type MutationJournal struct {
	mu            sync.Mutex
	maxEntries    int
	maxBytes      int
	intentBytes   int
	usedBytes     int
	reservedBytes int
	highWatermark int
	overflowCount uint64
	nextSequence  uint64
	entries       []mutationJournalEntry
	byIdempotency map[string]int
	activeBulk    *MutationBatchMarker
	lastBulk      *MutationBatchMarker
}

func NewMutationJournal(maxEntries int) *MutationJournal {
	if maxEntries <= 0 {
		maxEntries = 1024
	}
	return NewMutationJournalWithBudget(maxEntries, maxEntries*4096, 4096)
}

func NewMutationJournalWithBudget(maxEntries, budgetBytes, maxIntentBytes int) *MutationJournal {
	if maxEntries <= 0 {
		maxEntries = 1024
	}
	if maxIntentBytes <= 0 {
		maxIntentBytes = 4096
	}
	if budgetBytes < maxIntentBytes {
		budgetBytes = maxIntentBytes
	}
	return &MutationJournal{
		maxEntries:    maxEntries,
		maxBytes:      budgetBytes,
		intentBytes:   maxIntentBytes,
		nextSequence:  1,
		byIdempotency: make(map[string]int),
	}
}

func (j *MutationJournal) Append(intent MutationIntent) (MutationIntent, error) {
	size := j.maxIntentBytes()
	if err := j.Reserve(size); err != nil {
		return MutationIntent{}, err
	}
	return j.AppendReserved(intent, size)
}

// Reserve reserves one fixed-size intent slot before sequence allocation.
// A zero size uses the journal's configured maximum intent size.
func (j *MutationJournal) Reserve(size int) error {
	if j == nil {
		return ErrMutationJournalFull
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if size <= 0 {
		size = j.maxIntentBytes()
	}
	projectedReservedBytes := j.reservedBytes + size
	projectedReservedSlots := (projectedReservedBytes + j.maxIntentBytes() - 1) / j.maxIntentBytes()
	if len(j.entries)+projectedReservedSlots > j.maxEntries {
		j.overflowCount++
		return ErrMutationJournalFull
	}
	if j.usedBytes+projectedReservedBytes > j.maxBytes {
		j.overflowCount++
		return ErrMutationJournalBudget
	}
	j.reservedBytes += size
	if total := j.usedBytes + j.reservedBytes; total > j.highWatermark {
		j.highWatermark = total
	}
	return nil
}

// ReleaseReservation returns a pre-sequence reservation after cancellation or
// a gate wait failure.
func (j *MutationJournal) ReleaseReservation(size int) {
	if j == nil || size <= 0 {
		return
	}
	j.mu.Lock()
	if size > j.reservedBytes {
		size = j.reservedBytes
	}
	j.reservedBytes -= size
	j.mu.Unlock()
}

// AppendReserved consumes a reservation and appends an intent atomically.
// A zero size creates an internal reservation for direct callers.
func (j *MutationJournal) AppendReserved(intent MutationIntent, size int) (MutationIntent, error) {
	if j == nil {
		return MutationIntent{}, ErrMutationJournalFull
	}
	if size <= 0 {
		size = j.maxIntentBytes()
		if err := j.Reserve(size); err != nil {
			return MutationIntent{}, err
		}
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if intent.IdempotencyKey != "" {
		if index, ok := j.byIdempotency[intent.IdempotencyKey]; ok && index >= 0 && index < len(j.entries) {
			j.releaseReservationLocked(size)
			return j.entries[index].Intent, nil
		}
	}
	if len(j.entries) >= j.maxEntries {
		j.releaseReservationLocked(size)
		j.overflowCount++
		return MutationIntent{}, ErrMutationJournalFull
	}
	if j.reservedBytes < size || j.usedBytes+size > j.maxBytes {
		j.releaseReservationLocked(size)
		j.overflowCount++
		return MutationIntent{}, ErrMutationJournalBudget
	}
	if intent.Sequence == 0 {
		intent.Sequence = j.nextSequence
	}
	if intent.Sequence >= j.nextSequence {
		j.nextSequence = intent.Sequence + 1
	}
	if intent.IdempotencyKey == "" {
		intent.IdempotencyKey = "intent:" + formatUint(intent.Sequence)
	}
	if intent.AdmittedAt.IsZero() {
		intent.AdmittedAt = time.Now().UTC()
	}
	entry := mutationJournalEntry{Intent: cloneMutationIntent(intent), Size: size}
	j.entries = append(j.entries, entry)
	j.byIdempotency[intent.IdempotencyKey] = len(j.entries) - 1
	j.reservedBytes -= size
	j.usedBytes += size
	return cloneMutationIntent(intent), nil
}

func (j *MutationJournal) maxIntentBytes() int {
	if j == nil {
		return 4096
	}
	if j.intentBytes > 0 {
		return j.intentBytes
	}
	return 4096
}

func (j *MutationJournal) reservedSlotsLocked() int {
	maxIntentBytes := j.maxIntentBytes()
	if maxIntentBytes <= 0 {
		return 0
	}
	return (j.reservedBytes + maxIntentBytes - 1) / maxIntentBytes
}

func (j *MutationJournal) releaseReservationLocked(size int) {
	if size > j.reservedBytes {
		size = j.reservedBytes
	}
	if size > 0 {
		j.reservedBytes -= size
	}
}

func (j *MutationJournal) QueueSlots() int {
	if j == nil {
		return 0
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	remaining := j.maxBytes - j.usedBytes - j.reservedBytes
	maxIntentBytes := j.maxIntentBytes()
	if remaining <= 0 || maxIntentBytes <= 0 {
		return 0
	}
	slots := remaining / maxIntentBytes
	if entrySlots := j.maxEntries - len(j.entries) - j.reservedSlotsLocked(); slots > entrySlots {
		slots = entrySlots
	}
	return slots
}

func (j *MutationJournal) BudgetStats() (usedBytes, reservedBytes, highWatermark int, overflowCount uint64) {
	if j == nil {
		return 0, 0, 0, 0
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.usedBytes, j.reservedBytes, j.highWatermark, j.overflowCount
}

func (j *MutationJournal) MarkTerminal(sequence uint64, state MutationTerminalState, err error) error {
	if j == nil {
		return ErrMutationAlreadyTerminal
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	for index := range j.entries {
		if j.entries[index].Intent.Sequence != sequence {
			continue
		}
		if j.entries[index].Outcome != nil {
			return ErrMutationAlreadyTerminal
		}
		outcome := &MutationTerminalOutcome{Sequence: sequence, State: state, Err: err}
		j.entries[index].Outcome = outcome
		return nil
	}
	return errors.New("usage mutation sequence not found")
}

// BeginBulkTransaction installs one bounded batch marker. The exclusive batch
// owner is enforced by MutationCoordinator; the journal independently rejects
// overlapping markers so chunk compaction cannot erase another transaction's
// replay frontier.
func (j *MutationJournal) BeginBulkTransaction(marker MutationBatchMarker) error {
	if j == nil {
		return ErrMutationBatchNotFound
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.activeBulk != nil {
		return ErrMutationBatchActive
	}
	marker.SchemaVersion = mutationBulkTransactionVersionV1
	marker.BatchID = strings.TrimSpace(marker.BatchID)
	marker.PayloadSHA256 = strings.ToLower(strings.TrimSpace(marker.PayloadSHA256))
	marker.ManifestSHA256 = ""
	marker.TerminalState = ""
	marker.FailureCode = ""
	marker.FrontierOrdinal = 0
	marker.ChunkCount = 0
	marker.LastChunkSHA256 = ""
	if j.lastBulk != nil {
		marker.PreviousMarkerSHA256 = j.lastBulk.MarkerSHA256
	} else {
		marker.PreviousMarkerSHA256 = strings.ToLower(strings.TrimSpace(marker.PreviousMarkerSHA256))
	}
	marker.ChunkChecksumChain = initialMutationBatchChecksumChain(marker)
	if err := validateMutationBatchMarker(marker, false); err != nil {
		return err
	}
	if err := refreshMutationBatchMarkerChecksum(&marker); err != nil {
		return err
	}
	j.activeBulk = cloneMutationBatchMarker(&marker)
	return nil
}

// AdvanceBulkChunk records one contiguous, fully terminalized chunk. The
// rolling checksum chain makes a reordered, omitted, or substituted chunk
// detectable while keeping marker storage constant regardless of batch size.
func (j *MutationJournal) AdvanceBulkChunk(batchID string, startOrdinal, endOrdinal uint64, chunkSHA256 string) (MutationBatchMarker, error) {
	if j == nil {
		return MutationBatchMarker{}, ErrMutationBatchNotFound
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.activeBulk == nil || j.activeBulk.BatchID != strings.TrimSpace(batchID) {
		return MutationBatchMarker{}, ErrMutationBatchNotFound
	}
	marker := *j.activeBulk
	chunkSHA256 = strings.ToLower(strings.TrimSpace(chunkSHA256))
	if startOrdinal != marker.FrontierOrdinal || endOrdinal <= startOrdinal || endOrdinal > marker.TotalIntents {
		return MutationBatchMarker{}, ErrMutationBatchFrontier
	}
	if !isMutationSHA256(chunkSHA256) {
		return MutationBatchMarker{}, ErrMutationBatchChecksum
	}
	marker.LastChunkSHA256 = chunkSHA256
	marker.ChunkChecksumChain = nextMutationBatchChecksumChain(marker.ChunkChecksumChain, marker.BatchID, startOrdinal, endOrdinal, chunkSHA256)
	marker.FrontierOrdinal = endOrdinal
	marker.ChunkCount++
	if err := refreshMutationBatchMarkerChecksum(&marker); err != nil {
		return MutationBatchMarker{}, err
	}
	j.activeBulk = cloneMutationBatchMarker(&marker)
	return marker, nil
}

// CompleteBulkTransaction replaces the active marker with one terminal
// outcome. Committed batches require a complete frontier and manifest
// checksum; tombstones preserve the last safe frontier and a stable failure
// code without persisting raw error text.
func (j *MutationJournal) CompleteBulkTransaction(batchID string, state MutationTerminalState, failureCode, manifestSHA256 string) (MutationBatchMarker, error) {
	if j == nil {
		return MutationBatchMarker{}, ErrMutationBatchNotFound
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.activeBulk == nil || j.activeBulk.BatchID != strings.TrimSpace(batchID) {
		return MutationBatchMarker{}, ErrMutationBatchNotFound
	}
	marker := *j.activeBulk
	marker.TerminalState = state
	marker.FailureCode = strings.TrimSpace(failureCode)
	marker.ManifestSHA256 = strings.ToLower(strings.TrimSpace(manifestSHA256))
	switch state {
	case MutationTerminalCommitted:
		if marker.FrontierOrdinal != marker.TotalIntents || marker.TotalIntents == 0 {
			return MutationBatchMarker{}, ErrMutationBatchIncomplete
		}
		if !isMutationSHA256(marker.ManifestSHA256) {
			return MutationBatchMarker{}, ErrMutationBatchChecksum
		}
		marker.FailureCode = ""
	case MutationTerminalTombstone:
		if marker.FailureCode == "" {
			marker.FailureCode = "batch_failed"
		}
		marker.ManifestSHA256 = ""
	default:
		return MutationBatchMarker{}, ErrMutationAlreadyTerminal
	}
	if err := refreshMutationBatchMarkerChecksum(&marker); err != nil {
		return MutationBatchMarker{}, err
	}
	j.activeBulk = nil
	j.lastBulk = cloneMutationBatchMarker(&marker)
	return marker, nil
}

// BulkTransactionSnapshot returns the active marker when a batch is in
// progress, otherwise the latest terminal marker.
func (j *MutationJournal) BulkTransactionSnapshot() (MutationBatchMarker, bool) {
	if j == nil {
		return MutationBatchMarker{}, false
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.activeBulk != nil {
		return *cloneMutationBatchMarker(j.activeBulk), true
	}
	if j.lastBulk != nil {
		return *cloneMutationBatchMarker(j.lastBulk), true
	}
	return MutationBatchMarker{}, false
}

// TerminalBulkTransactionSnapshot returns only a completed marker so a
// persisted generation can never serialize an in-progress staging candidate.
func (j *MutationJournal) TerminalBulkTransactionSnapshot() (MutationBatchMarker, bool) {
	if j == nil {
		return MutationBatchMarker{}, false
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.lastBulk == nil {
		return MutationBatchMarker{}, false
	}
	return *cloneMutationBatchMarker(j.lastBulk), true
}

// RestoreBulkTransaction restores the latest terminal marker from a validated
// identity sidecar. Active staging candidates are intentionally never
// restorable; a process restart keeps the previous committed generation.
func (j *MutationJournal) RestoreBulkTransaction(marker MutationBatchMarker) error {
	if j == nil {
		return ErrMutationBatchNotFound
	}
	if err := validateMutationBatchMarker(marker, true); err != nil {
		return err
	}
	j.mu.Lock()
	j.activeBulk = nil
	j.lastBulk = cloneMutationBatchMarker(&marker)
	j.mu.Unlock()
	return nil
}

// CompactTerminalBefore reclaims a terminal prefix whose sequence is at or
// below safeSequence. The caller must ensure no active rebuild candidate needs
// that prefix for replay. Sequence allocation remains monotonic after removal.
func (j *MutationJournal) CompactTerminalBefore(safeSequence uint64) int {
	if j == nil {
		return 0
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	compactCount := 0
	reclaimedBytes := 0
	for compactCount < len(j.entries) {
		entry := j.entries[compactCount]
		if entry.Intent.Sequence > safeSequence || entry.Outcome == nil {
			break
		}
		entrySize := entry.Size
		if entrySize <= 0 {
			entrySize = j.maxIntentBytes()
		}
		reclaimedBytes += entrySize
		compactCount++
	}
	if compactCount == 0 {
		return 0
	}
	copy(j.entries, j.entries[compactCount:])
	j.entries = j.entries[:len(j.entries)-compactCount]
	j.byIdempotency = make(map[string]int, len(j.entries))
	for index := range j.entries {
		j.byIdempotency[j.entries[index].Intent.IdempotencyKey] = index
	}
	j.usedBytes -= reclaimedBytes
	if j.usedBytes < 0 {
		j.usedBytes = 0
	}
	return compactCount
}

func (j *MutationJournal) Head() uint64 {
	if j == nil {
		return 0
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.nextSequence - 1
}

// AdvanceNextSequence restores the durable sequence allocator without
// manufacturing terminal journal entries for the previous generation.
func (j *MutationJournal) AdvanceNextSequence(next uint64) {
	if j == nil || next == 0 {
		return
	}
	j.mu.Lock()
	if j.nextSequence < next {
		j.nextSequence = next
	}
	j.mu.Unlock()
}

func (j *MutationJournal) PendingCount() int {
	if j == nil {
		return 0
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	pending := 0
	for _, entry := range j.entries {
		if entry.Outcome == nil {
			pending++
		}
	}
	return pending
}

func (j *MutationJournal) EntriesAfter(sequence uint64, terminalOnly bool) []MutationIntent {
	if j == nil {
		return nil
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	result := make([]MutationIntent, 0)
	for _, entry := range j.entries {
		if entry.Intent.Sequence <= sequence {
			continue
		}
		if terminalOnly && entry.Outcome == nil {
			continue
		}
		result = append(result, cloneMutationIntent(entry.Intent))
	}
	return result
}

func (j *MutationJournal) Outcome(sequence uint64) (MutationTerminalOutcome, bool) {
	if j == nil {
		return MutationTerminalOutcome{}, false
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	for _, entry := range j.entries {
		if entry.Intent.Sequence == sequence && entry.Outcome != nil {
			return *entry.Outcome, true
		}
	}
	return MutationTerminalOutcome{}, false
}

// TerminalSnapshot returns the terminal frontier and immutable terminal
// metadata needed to validate a persisted generation after restart.
func (j *MutationJournal) TerminalSnapshot() (uint64, []IdentitySidecarTerminalIntent) {
	if j == nil {
		return 0, nil
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	intents := make([]IdentitySidecarTerminalIntent, 0, len(j.entries))
	pending := uint64(0)
	for _, entry := range j.entries {
		if entry.Outcome == nil {
			if pending == 0 || entry.Intent.Sequence < pending {
				pending = entry.Intent.Sequence
			}
			continue
		}
		intents = append(intents, IdentitySidecarTerminalIntent{
			Sequence:       entry.Intent.Sequence,
			State:          entry.Outcome.State,
			IdempotencyKey: entry.Intent.IdempotencyKey,
			Tombstone:      entry.Outcome.State == MutationTerminalTombstone,
		})
	}
	frontier := j.nextSequence - 1
	if pending > 0 && pending <= frontier {
		frontier = pending - 1
	}
	return frontier, intents
}

func cloneMutationBatchMarker(marker *MutationBatchMarker) *MutationBatchMarker {
	if marker == nil {
		return nil
	}
	clone := *marker
	return &clone
}

func validateMutationBatchMarker(marker MutationBatchMarker, requireTerminal bool) error {
	if marker.SchemaVersion != mutationBulkTransactionVersionV1 || strings.TrimSpace(marker.BatchID) == "" || marker.BatchSequence == 0 || marker.TotalIntents == 0 {
		return ErrMutationBatchIncomplete
	}
	if marker.MarkerSHA256 != "" {
		expected, err := mutationBatchMarkerChecksum(marker)
		if err != nil || !strings.EqualFold(expected, marker.MarkerSHA256) {
			return ErrMutationBatchChecksum
		}
	}
	if marker.Operation != MutationOperationImportMerge && marker.Operation != MutationOperationRestore {
		return ErrMutationBatchIncomplete
	}
	if marker.FrontierOrdinal > marker.TotalIntents || !isMutationSHA256(marker.PayloadSHA256) || !isMutationSHA256(marker.ChunkChecksumChain) {
		return ErrMutationBatchChecksum
	}
	if marker.ChunkCount == 0 {
		if marker.FrontierOrdinal != 0 || marker.LastChunkSHA256 != "" {
			return ErrMutationBatchFrontier
		}
	} else if marker.FrontierOrdinal == 0 || !isMutationSHA256(marker.LastChunkSHA256) {
		return ErrMutationBatchChecksum
	}
	if marker.PreviousMarkerSHA256 != "" && !isMutationSHA256(marker.PreviousMarkerSHA256) {
		return ErrMutationBatchChecksum
	}
	if requireTerminal {
		switch marker.TerminalState {
		case MutationTerminalCommitted:
			if marker.FrontierOrdinal != marker.TotalIntents || !isMutationSHA256(marker.ManifestSHA256) || marker.FailureCode != "" {
				return ErrMutationBatchIncomplete
			}
		case MutationTerminalTombstone:
			if marker.FailureCode == "" || marker.ManifestSHA256 != "" {
				return ErrMutationBatchIncomplete
			}
		default:
			return ErrMutationBatchIncomplete
		}
	} else if marker.TerminalState != "" || marker.FailureCode != "" || marker.ManifestSHA256 != "" {
		return ErrMutationBatchIncomplete
	}
	return nil
}

func refreshMutationBatchMarkerChecksum(marker *MutationBatchMarker) error {
	if marker == nil {
		return ErrMutationBatchIncomplete
	}
	checksum, err := mutationBatchMarkerChecksum(*marker)
	if err != nil {
		return err
	}
	marker.MarkerSHA256 = checksum
	return nil
}

func mutationBatchMarkerChecksum(marker MutationBatchMarker) (string, error) {
	marker.MarkerSHA256 = ""
	data, err := json.Marshal(marker)
	if err != nil {
		return "", fmt.Errorf("usage: marshal bulk transaction marker: %w", err)
	}
	if len(data) > maxMutationBulkMarkerBytes {
		return "", ErrMutationJournalBudget
	}
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:]), nil
}

func initialMutationBatchChecksumChain(marker MutationBatchMarker) string {
	return mutationSHA256Parts(
		mutationBulkTransactionVersionV1,
		marker.BatchID,
		formatUint(marker.BatchSequence),
		string(marker.Operation),
		formatUint(marker.BaseRevision),
		formatUint(marker.DatasetEpoch),
		formatUint(marker.TotalIntents),
		marker.PayloadSHA256,
		marker.PreviousMarkerSHA256,
	)
}

func nextMutationBatchChecksumChain(previous, batchID string, startOrdinal, endOrdinal uint64, chunkSHA256 string) string {
	return mutationSHA256Parts(previous, batchID, formatUint(startOrdinal), formatUint(endOrdinal), chunkSHA256)
}

func mutationSHA256Parts(parts ...string) string {
	hasher := sha256.New()
	for _, part := range parts {
		var length [8]byte
		value := uint64(len(part))
		for index := 7; index >= 0; index-- {
			length[index] = byte(value)
			value >>= 8
		}
		_, _ = hasher.Write(length[:])
		_, _ = hasher.Write([]byte(part))
	}
	return hex.EncodeToString(hasher.Sum(nil))
}

func isMutationSHA256(value string) bool {
	decoded, err := hex.DecodeString(strings.TrimSpace(value))
	return err == nil && len(decoded) == sha256.Size
}

func cloneMutationIntent(intent MutationIntent) MutationIntent {
	intent.BatchID = strings.TrimSpace(intent.BatchID)
	intent.Operation = MutationOperation(strings.TrimSpace(string(intent.Operation)))
	intent.APIName = strings.TrimSpace(intent.APIName)
	intent.CoordinateKey = strings.TrimSpace(intent.CoordinateKey)
	intent.StableEventID = strings.TrimSpace(intent.StableEventID)
	intent.LogicalIdentity = strings.TrimSpace(intent.LogicalIdentity)
	intent.CanonicalEventIdentity = strings.TrimSpace(intent.CanonicalEventIdentity)
	intent.CanonicalIdentitySeed = strings.TrimSpace(intent.CanonicalIdentitySeed)
	intent.SourceGroupKey = strings.TrimSpace(intent.SourceGroupKey)
	intent.AdmissionDiscriminator = strings.TrimSpace(intent.AdmissionDiscriminator)
	intent.IdempotencyKey = strings.TrimSpace(intent.IdempotencyKey)
	intent.Detail = cloneRequestDetail(intent.Detail)
	return intent
}

// serializedMutationIntentBytes returns a deterministic upper-bound estimate
// for the journal payload. Sequence, idempotency and admission timestamp are
// filled with their maximum-width encodings so a later append cannot exceed
// the fixed reservation budget.
func serializedMutationIntentBytes(intent MutationIntent) int {
	intent.Sequence = ^uint64(0)
	if strings.TrimSpace(intent.IdempotencyKey) == "" {
		intent.IdempotencyKey = "intent:" + formatUint(^uint64(0))
	}
	intent.AdmittedAt = time.Date(9999, 12, 31, 23, 59, 59, 999999999, time.UTC)
	data, err := json.Marshal(intent)
	if err != nil {
		return int(^uint(0) >> 1)
	}
	return len(data)
}

func formatUint(value uint64) string {
	const digits = "0123456789"
	if value == 0 {
		return "0"
	}
	var buffer [20]byte
	index := len(buffer)
	for value > 0 {
		index--
		buffer[index] = digits[value%10]
		value /= 10
	}
	return string(buffer[index:])
}
