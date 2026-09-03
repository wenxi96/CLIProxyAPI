package usage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	projectionGenerationManifestVersionV1 = "usage_projection_generation_manifest_v1"
	projectionGenerationManifestVersionV2 = "usage_projection_generation_manifest_v2"
	snapshotDigestVersionV1               = "snapshot_digest_v1"
)

var ErrProjectionRestoreUnavailable = errors.New("usage projection restore unavailable")

type ProjectionGenerationManifest struct {
	SchemaVersion         string    `json:"schema_version"`
	Generation            uint64    `json:"generation"`
	ParentGeneration      uint64    `json:"parent_generation,omitempty"`
	DatasetEpoch          uint64    `json:"dataset_epoch"`
	Revision              uint64    `json:"revision"`
	RewriteRevision       uint64    `json:"rewrite_revision"`
	CanonicalDetailDigest string    `json:"canonical_detail_digest"`
	TerminalFrontier      uint64    `json:"terminal_frontier"`
	JournalSHA256         string    `json:"journal_sha256"`
	DetailsFile           string    `json:"details_file"`
	ProjectionFile        string    `json:"projection_file"`
	SidecarFile           string    `json:"sidecar_file"`
	DetailsSHA256         string    `json:"details_sha256"`
	ProjectionSHA256      string    `json:"projection_sha256"`
	ProjectionSchema      string    `json:"projection_schema,omitempty"`
	FactsSHA256           string    `json:"facts_sha256,omitempty"`
	PostingsSHA256        string    `json:"postings_sha256,omitempty"`
	ScalarRollupSHA256    string    `json:"scalar_rollup_sha256,omitempty"`
	SidecarSHA256         string    `json:"sidecar_sha256"`
	CommittedAt           time.Time `json:"committed_at"`
}

type projectionGeneration struct {
	Manifest   ProjectionGenerationManifest
	Snapshot   StatisticsSnapshot
	Projection projectionGenerationState
	Sidecar    IdentitySidecar
	// Sections retains the validated v2 facts/postings/scalar-rollup section
	// bytes so a normal v2 restart can directly hydrate UsageProjection without
	// replaying canonical details. Legacy v1 generations leave Sections empty.
	Sections projectionV2Sections
}

// projectionV2Sections holds the raw, checksum-validated v2 projection section
// payloads. decodeProjectionGenerationStateV2 populates it; direct hydrate
// unmarshals each section into UsageProjection stores.
type projectionV2Sections struct {
	Facts         []byte
	Postings      []byte
	ScalarRollups []byte
}

func SnapshotDigestV1(snapshot StatisticsSnapshot) string {
	items := snapshotDigestDetails(snapshot)
	hasher := sha256.New()
	_, _ = hasher.Write([]byte(snapshotDigestVersionV1))
	for _, item := range items {
		writeDigestPart(hasher, item.apiName)
		writeDigestPart(hasher, item.modelName)
		writeDigestBytes(hasher, item.canonicalBytes)
	}
	return hex.EncodeToString(hasher.Sum(nil))
}

type snapshotDigestDetail struct {
	apiName        string
	modelName      string
	sourceOrdinal  uint64
	sortFields     [9]string
	canonicalBytes []byte
}

func snapshotDigestDetails(snapshot StatisticsSnapshot) []snapshotDigestDetail {
	apiNames := make([]string, 0, len(snapshot.APIs))
	for apiName := range snapshot.APIs {
		apiNames = append(apiNames, apiName)
	}
	sort.Strings(apiNames)
	items := make([]snapshotDigestDetail, 0)
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
				detail = normalizeRequestDetailPreserveTimestamp(detail, detail.Provider)
				timestamp := "zero-time"
				if !detail.Timestamp.IsZero() {
					timestamp = detail.Timestamp.UTC().Format(time.RFC3339Nano)
				}
				items = append(items, snapshotDigestDetail{
					apiName:       apiName,
					modelName:     modelName,
					sourceOrdinal: sourceOrdinal,
					sortFields: [9]string{
						timestamp,
						detail.Endpoint,
						detail.Model,
						detail.Provider,
						detail.AuthIndex,
						UsageSourceKeyV1(detail),
						detail.DetailRole,
						detail.DetailSequence,
						detail.RequestID,
					},
					canonicalBytes: canonicalDetailBytes(detail),
				})
				sourceOrdinal++
			}
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		for fieldIndex := range items[i].sortFields {
			if comparison := compareLengthPrefixedString(items[i].sortFields[fieldIndex], items[j].sortFields[fieldIndex]); comparison != 0 {
				return comparison < 0
			}
		}
		if comparison := compareLengthPrefixedBytes(items[i].canonicalBytes, items[j].canonicalBytes); comparison != 0 {
			return comparison < 0
		}
		if items[i].modelName != items[j].modelName {
			return items[i].modelName < items[j].modelName
		}
		return items[i].sourceOrdinal < items[j].sourceOrdinal
	})
	return items
}

func compareLengthPrefixedString(left, right string) int {
	if len(left) < len(right) {
		return -1
	}
	if len(left) > len(right) {
		return 1
	}
	return strings.Compare(left, right)
}

func compareLengthPrefixedBytes(left, right []byte) int {
	if len(left) < len(right) {
		return -1
	}
	if len(left) > len(right) {
		return 1
	}
	return bytes.Compare(left, right)
}

func writeDigestPart(hasher interface{ Write([]byte) (int, error) }, value string) {
	writeDigestBytes(hasher, []byte(value))
}

func writeDigestBytes(hasher interface{ Write([]byte) (int, error) }, value []byte) {
	var length [8]byte
	putBigEndianUint64(length[:], uint64(len(value)))
	_, _ = hasher.Write(length[:])
	_, _ = hasher.Write(value)
}

func putBigEndianUint64(dst []byte, value uint64) {
	for index := 7; index >= 0; index-- {
		dst[index] = byte(value)
		value >>= 8
	}
}

func projectionGenerationManifestPath(path string) string {
	return strings.TrimSpace(path) + ".generation.manifest"
}

func projectionGenerationFileBase(path string, generation uint64, suffix string) string {
	base := filepath.Base(strings.TrimSpace(path))
	return fmt.Sprintf("%s.generation-%d.%s", base, generation, suffix)
}

func projectionGenerationManifestFilePath(path string, generation uint64) string {
	dir := filepath.Dir(filepath.Clean(strings.TrimSpace(path)))
	return filepath.Join(dir, projectionGenerationFileBase(path, generation, "manifest"))
}

func SaveProjectionGeneration(path string, stats *RequestStatistics) error {
	if stats == nil {
		return nil
	}
	stats.persistenceMu.Lock()
	defer stats.persistenceMu.Unlock()
	return saveProjectionGenerationWithCaptureHook(path, stats, nil)
}

func saveProjectionGenerationWithCaptureHook(path string, stats *RequestStatistics, afterSnapshot func()) error {
	return saveProjectionGenerationWithCaptureResult(path, stats, afterSnapshot, nil)
}

func saveProjectionGenerationWithCaptureResult(path string, stats *RequestStatistics, afterSnapshot func(), captureResult *capturedProjectionGeneration) error {
	if stats == nil {
		return nil
	}
	cleanupProjectionGenerationTemps(path)
	captureCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	captured, err := captureProjectionGenerationState(captureCtx, stats)
	if err != nil {
		return err
	}
	if captureResult != nil {
		*captureResult = captured
	}
	if afterSnapshot != nil {
		afterSnapshot()
	}
	snapshot := captured.snapshot
	capturedProjection := captured.projection
	sidecar := captured.sidecar
	projection, projectionData, err := capturedProjection.marshalGenerationStateV2()
	if err != nil {
		return fmt.Errorf("usage: marshal generation projection: %w", err)
	}
	previousGeneration, previousErr := latestValidProjectionGeneration(path)
	generation := projection.Revision
	if previousErr == nil && generation <= previousGeneration.Manifest.Generation {
		generation = previousGeneration.Manifest.Generation + 1
	}
	if generation == 0 {
		generation = uint64(time.Now().UTC().UnixNano())
	}
	detailDigest := SnapshotDigestV1(snapshot)
	sidecar.Generation = generation
	sidecar.CanonicalDetailGeneration = projection.Revision
	sidecar.PayloadSHA256 = detailDigest
	if previousErr == nil && previousGeneration.Manifest.Generation != generation {
		sidecar.ParentGeneration = previousGeneration.Manifest.Generation
	}
	detailsData, err := json.Marshal(StatisticsFilePayload{Version: StatisticsFileVersion, ExportedAt: time.Now().UTC(), Usage: snapshot})
	if err != nil {
		return fmt.Errorf("usage: marshal generation details: %w", err)
	}
	sidecarData, err := json.Marshal(sidecar)
	if err != nil {
		return fmt.Errorf("usage: marshal generation sidecar: %w", err)
	}
	sidecar.Checksum = sha256Hex(sidecarData)
	sidecarData, err = json.Marshal(sidecar)
	if err != nil {
		return fmt.Errorf("usage: marshal generation sidecar checksum: %w", err)
	}
	journalData, err := marshalTerminalJournal(sidecar.TerminalFrontier, sidecar.TerminalIntents, sidecar.BulkTransaction)
	if err != nil {
		return fmt.Errorf("usage: marshal generation journal: %w", err)
	}
	dir := filepath.Dir(filepath.Clean(strings.TrimSpace(path)))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("usage: create generation directory: %w", err)
	}
	detailsName := projectionGenerationFileBase(path, generation, "details.json")
	projectionName := projectionGenerationFileBase(path, generation, "projection.bin")
	sidecarName := projectionGenerationFileBase(path, generation, "identity.json")
	for _, file := range []struct {
		name string
		data []byte
	}{
		{detailsName, detailsData}, {projectionName, projectionData}, {sidecarName, sidecarData},
	} {
		if err := writeFileAtomic(filepath.Join(dir, file.name), file.data); err != nil {
			return err
		}
	}
	manifest := ProjectionGenerationManifest{
		SchemaVersion:         projectionGenerationManifestVersionV2,
		Generation:            generation,
		ParentGeneration:      sidecar.ParentGeneration,
		DatasetEpoch:          projection.DatasetEpoch,
		Revision:              projection.Revision,
		RewriteRevision:       projection.RewriteRevision,
		CanonicalDetailDigest: detailDigest,
		TerminalFrontier:      sidecar.TerminalFrontier,
		JournalSHA256:         sha256Hex(journalData),
		DetailsFile:           detailsName,
		ProjectionFile:        projectionName,
		SidecarFile:           sidecarName,
		DetailsSHA256:         sha256Hex(detailsData),
		ProjectionSHA256:      sha256Hex(projectionData),
		ProjectionSchema:      projection.SchemaVersion,
		FactsSHA256:           projection.FactsSHA256,
		PostingsSHA256:        projection.PostingsSHA256,
		ScalarRollupSHA256:    projection.ScalarRollupSHA256,
		SidecarSHA256:         sha256Hex(sidecarData),
		CommittedAt:           time.Now().UTC(),
	}
	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("usage: marshal generation manifest: %w", err)
	}
	manifestData = append(manifestData, '\n')
	// Keep an immutable manifest for recovery. The pointer manifest below is
	// replaced last; a crash or partial pointer write can therefore be repaired
	// by scanning these committed generation manifests.
	if err := writeFileAtomic(projectionGenerationManifestFilePath(path, generation), manifestData); err != nil {
		return fmt.Errorf("usage: commit immutable generation manifest: %w", err)
	}
	if err := writeFileAtomic(projectionGenerationManifestPath(path), manifestData); err != nil {
		return fmt.Errorf("usage: commit generation manifest: %w", err)
	}
	gcProjectionGenerations(path, generation, sidecar.ParentGeneration)
	return nil
}

func loadProjectionGenerationManifest(path, manifestPath string) (projectionGeneration, error) {
	var result projectionGeneration
	manifestData, err := os.ReadFile(manifestPath)
	if err != nil {
		return result, err
	}
	if err := json.Unmarshal(manifestData, &result.Manifest); err != nil {
		return result, fmt.Errorf("%w: decode generation manifest: %v", ErrProjectionRestoreUnavailable, err)
	}
	if result.Manifest.SchemaVersion != projectionGenerationManifestVersionV1 && result.Manifest.SchemaVersion != projectionGenerationManifestVersionV2 {
		return result, fmt.Errorf("%w: unsupported generation schema %q", ErrProjectionRestoreUnavailable, result.Manifest.SchemaVersion)
	}
	if result.Manifest.Generation == 0 || result.Manifest.CanonicalDetailDigest == "" {
		return result, fmt.Errorf("%w: incomplete generation manifest", ErrProjectionRestoreUnavailable)
	}
	dir := filepath.Dir(filepath.Clean(strings.TrimSpace(path)))
	readGenerationFile := func(name, expectedHash string) ([]byte, error) {
		if name == "" || filepath.Base(name) != name {
			return nil, fmt.Errorf("%w: invalid generation file name", ErrProjectionRestoreUnavailable)
		}
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return nil, fmt.Errorf("%w: read generation file %s: %v", ErrProjectionRestoreUnavailable, name, err)
		}
		if expectedHash != "" && !strings.EqualFold(expectedHash, sha256Hex(data)) {
			return nil, fmt.Errorf("%w: generation file hash mismatch %s", ErrProjectionRestoreUnavailable, name)
		}
		return data, nil
	}
	detailsData, err := readGenerationFile(result.Manifest.DetailsFile, result.Manifest.DetailsSHA256)
	if err != nil {
		return result, err
	}
	projectionData, err := readGenerationFile(result.Manifest.ProjectionFile, result.Manifest.ProjectionSHA256)
	if err != nil {
		return result, err
	}
	sidecarData, err := readGenerationFile(result.Manifest.SidecarFile, result.Manifest.SidecarSHA256)
	if err != nil {
		return result, err
	}
	var payload StatisticsFilePayload
	if err := json.Unmarshal(detailsData, &payload); err != nil {
		return result, fmt.Errorf("%w: decode generation details: %v", ErrProjectionRestoreUnavailable, err)
	}
	switch result.Manifest.SchemaVersion {
	case projectionGenerationManifestVersionV1:
		var legacyProjection ProjectionSnapshot
		if err := json.Unmarshal(projectionData, &legacyProjection); err != nil {
			return result, fmt.Errorf("%w: decode generation projection: %v", ErrProjectionRestoreUnavailable, err)
		}
		result.Projection = projectionGenerationState{
			SchemaVersion:   legacyProjection.SchemaVersion,
			DatasetEpoch:    legacyProjection.DatasetEpoch,
			Revision:        legacyProjection.Revision,
			RewriteRevision: legacyProjection.RewriteRevision,
		}
	case projectionGenerationManifestVersionV2:
		result.Projection, result.Sections, err = decodeProjectionGenerationStateV2(projectionData)
		if err != nil {
			return result, err
		}
		if result.Manifest.ProjectionSchema != result.Projection.SchemaVersion ||
			!strings.EqualFold(result.Manifest.FactsSHA256, result.Projection.FactsSHA256) ||
			!strings.EqualFold(result.Manifest.PostingsSHA256, result.Projection.PostingsSHA256) ||
			!strings.EqualFold(result.Manifest.ScalarRollupSHA256, result.Projection.ScalarRollupSHA256) {
			return result, fmt.Errorf("%w: projection section metadata mismatch", ErrProjectionRestoreUnavailable)
		}
	}
	if err := json.Unmarshal(sidecarData, &result.Sidecar); err != nil {
		return result, fmt.Errorf("%w: decode generation sidecar: %v", ErrProjectionRestoreUnavailable, err)
	}
	if !result.Sidecar.valid() {
		return result, fmt.Errorf("%w: unsupported identity sidecar schema", ErrProjectionRestoreUnavailable)
	}
	if !verifyIdentitySidecarChecksum(sidecarData, result.Sidecar) {
		return result, fmt.Errorf("%w: identity sidecar checksum mismatch", ErrProjectionRestoreUnavailable)
	}
	result.Snapshot = payload.Usage
	if result.Manifest.Generation != result.Sidecar.Generation ||
		result.Manifest.DatasetEpoch != result.Projection.DatasetEpoch ||
		result.Manifest.Revision != result.Projection.Revision ||
		result.Manifest.RewriteRevision != result.Projection.RewriteRevision ||
		result.Sidecar.CanonicalDetailGeneration != result.Manifest.Revision ||
		result.Sidecar.DatasetEpoch != result.Manifest.DatasetEpoch ||
		result.Sidecar.TerminalFrontier != result.Manifest.TerminalFrontier {
		return result, fmt.Errorf("%w: generation metadata mismatch", ErrProjectionRestoreUnavailable)
	}
	journalData, err := marshalTerminalJournal(result.Sidecar.TerminalFrontier, result.Sidecar.TerminalIntents, result.Sidecar.BulkTransaction)
	if err != nil || result.Manifest.JournalSHA256 == "" || !strings.EqualFold(result.Manifest.JournalSHA256, sha256Hex(journalData)) {
		return result, fmt.Errorf("%w: journal metadata checksum mismatch", ErrProjectionRestoreUnavailable)
	}
	seenTerminalSequences := make(map[uint64]struct{}, len(result.Sidecar.TerminalIntents))
	for _, intent := range result.Sidecar.TerminalIntents {
		if intent.Sequence == 0 || intent.Sequence > result.Sidecar.TerminalFrontier {
			return result, fmt.Errorf("%w: invalid terminal frontier", ErrProjectionRestoreUnavailable)
		}
		if _, exists := seenTerminalSequences[intent.Sequence]; exists {
			return result, fmt.Errorf("%w: duplicate terminal sequence", ErrProjectionRestoreUnavailable)
		}
		seenTerminalSequences[intent.Sequence] = struct{}{}
	}
	if result.Sidecar.BulkTransaction != nil {
		if err := validateMutationBatchMarker(*result.Sidecar.BulkTransaction, true); err != nil {
			return result, fmt.Errorf("%w: invalid bulk transaction marker: %v", ErrProjectionRestoreUnavailable, err)
		}
	}
	if got := SnapshotDigestV1(result.Snapshot); !strings.EqualFold(got, result.Manifest.CanonicalDetailDigest) {
		return result, fmt.Errorf("%w: canonical detail digest mismatch", ErrProjectionRestoreUnavailable)
	}
	if result.Sidecar.PayloadSHA256 == "" || !strings.EqualFold(result.Sidecar.PayloadSHA256, result.Manifest.CanonicalDetailDigest) {
		return result, fmt.Errorf("%w: sidecar payload digest mismatch", ErrProjectionRestoreUnavailable)
	}
	return result, nil
}

// LoadProjectionGeneration validates the current pointer first and then falls
// back to the newest immutable committed generation. A corrupt current
// manifest, a half-written pointer, or a checksum mismatch therefore cannot
// erase the last known-good generation. If no committed generation is valid,
// the original restore-unavailable error is retained for fail-closed callers.
func LoadProjectionGeneration(path string) (projectionGeneration, error) {
	cleanupProjectionGenerationTemps(path)
	currentPath := projectionGenerationManifestPath(path)
	current, currentErr := loadProjectionGenerationManifest(path, currentPath)
	if currentErr == nil {
		return current, nil
	}

	failedGeneration := uint64(0)
	if manifestData, readErr := os.ReadFile(currentPath); readErr == nil {
		var manifest ProjectionGenerationManifest
		if json.Unmarshal(manifestData, &manifest) == nil {
			failedGeneration = manifest.Generation
		}
	}
	if failedGeneration > 0 {
		// A torn pointer may leave the immutable manifest and all payload files
		// intact. Prefer that same generation before falling back to its parent.
		if immutable, immutableErr := loadProjectionGenerationManifest(path, projectionGenerationManifestFilePath(path, failedGeneration)); immutableErr == nil {
			return immutable, nil
		}
	}
	if fallback, fallbackErr := latestValidProjectionGenerationExcluding(path, failedGeneration); fallbackErr == nil {
		return fallback, nil
	}
	if os.IsNotExist(currentErr) {
		return projectionGeneration{}, currentErr
	}
	return projectionGeneration{}, currentErr
}

func latestValidProjectionGeneration(path string) (projectionGeneration, error) {
	return latestValidProjectionGenerationExcluding(path, 0)
}

func latestValidProjectionGenerationExcluding(path string, excludedGeneration uint64) (projectionGeneration, error) {
	excluded := make(map[uint64]struct{})
	if excludedGeneration != 0 {
		excluded[excludedGeneration] = struct{}{}
	}
	return latestValidProjectionGenerationExcludingSet(path, excluded)
}

func latestValidProjectionGenerationExcludingSet(path string, excluded map[uint64]struct{}) (projectionGeneration, error) {
	var best projectionGeneration
	bestGeneration := uint64(0)
	dir := filepath.Dir(filepath.Clean(strings.TrimSpace(path)))
	base := filepath.Base(strings.TrimSpace(path))
	paths, err := filepath.Glob(filepath.Join(dir, base+".generation-*.manifest"))
	if err != nil {
		return best, fmt.Errorf("%w: scan generation manifests: %v", ErrProjectionRestoreUnavailable, err)
	}
	for _, manifestPath := range paths {
		candidate, candidateErr := loadProjectionGenerationManifest(path, manifestPath)
		if candidateErr != nil || candidate.Manifest.Generation == 0 {
			continue
		}
		if _, skip := excluded[candidate.Manifest.Generation]; skip {
			continue
		}
		if candidate.Manifest.Generation > bestGeneration {
			best = candidate
			bestGeneration = candidate.Manifest.Generation
		}
	}
	if bestGeneration == 0 {
		return best, fmt.Errorf("%w: no valid committed generation", ErrProjectionRestoreUnavailable)
	}
	return best, nil
}

func generationFromFileName(path, name string) (uint64, bool) {
	prefix := filepath.Base(strings.TrimSpace(path)) + ".generation-"
	if !strings.HasPrefix(name, prefix) {
		return 0, false
	}
	remainder := strings.TrimPrefix(name, prefix)
	separator := strings.IndexByte(remainder, '.')
	if separator <= 0 {
		return 0, false
	}
	generation, err := strconv.ParseUint(remainder[:separator], 10, 64)
	return generation, err == nil && generation > 0
}

// gcProjectionGenerations runs only after the new pointer manifest is
// committed. It keeps current and parent generations and removes older data
// files/manifests plus known generation temp files. GC is best effort: a
// cleanup failure must not invalidate an already committed generation.
func gcProjectionGenerations(path string, currentGeneration, parentGeneration uint64) {
	cleanupProjectionGenerationTemps(path)
	dir := filepath.Dir(filepath.Clean(strings.TrimSpace(path)))
	base := filepath.Base(strings.TrimSpace(path))
	patterns := []string{
		filepath.Join(dir, base+".generation-*.details.json"),
		filepath.Join(dir, base+".generation-*.projection.json"),
		filepath.Join(dir, base+".generation-*.projection.bin"),
		filepath.Join(dir, base+".generation-*.identity.json"),
		filepath.Join(dir, base+".generation-*.manifest"),
	}
	for _, pattern := range patterns {
		paths, err := filepath.Glob(pattern)
		if err != nil {
			continue
		}
		for _, candidate := range paths {
			name := filepath.Base(candidate)
			generation, ok := generationFromFileName(path, name)
			if !ok || generation == currentGeneration || generation == parentGeneration {
				continue
			}
			_ = os.Remove(candidate)
		}
	}
}

func cleanupProjectionGenerationTemps(path string) {
	dir := filepath.Dir(filepath.Clean(strings.TrimSpace(path)))
	base := filepath.Base(strings.TrimSpace(path))
	patterns := []string{
		filepath.Join(dir, base+".generation-*.tmp"),
		filepath.Join(dir, base+".generation-*.tmp-*"),
		filepath.Join(dir, base+".generation.manifest.tmp-*"),
	}
	for _, pattern := range patterns {
		paths, err := filepath.Glob(pattern)
		if err != nil {
			continue
		}
		for _, candidate := range paths {
			_ = os.Remove(candidate)
		}
	}
}

func marshalTerminalJournal(frontier uint64, intents []IdentitySidecarTerminalIntent, bulkTransaction *MutationBatchMarker) ([]byte, error) {
	ordered := append([]IdentitySidecarTerminalIntent(nil), intents...)
	sort.SliceStable(ordered, func(i, j int) bool {
		return ordered[i].Sequence < ordered[j].Sequence
	})
	return json.Marshal(struct {
		TerminalFrontier uint64                          `json:"terminal_frontier"`
		TerminalIntents  []IdentitySidecarTerminalIntent `json:"terminal_intents"`
		BulkTransaction  *MutationBatchMarker            `json:"bulk_transaction,omitempty"`
	}{TerminalFrontier: frontier, TerminalIntents: ordered, BulkTransaction: cloneMutationBatchMarker(bulkTransaction)})
}

func sha256Hex(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

func verifyIdentitySidecarChecksum(sidecarData []byte, sidecar IdentitySidecar) bool {
	checksum := sidecar.Checksum
	if checksum == "" || checksum != strings.TrimSpace(checksum) || !isMutationSHA256(checksum) {
		return false
	}
	prefix := []byte(`"checksum":"`)
	marker := make([]byte, 0, len(prefix)+len(checksum)+1)
	marker = append(marker, prefix...)
	marker = append(marker, checksum...)
	marker = append(marker, '"')
	if markerIndex := bytes.Index(sidecarData, marker); markerIndex >= 0 && bytes.Index(sidecarData[markerIndex+len(marker):], marker) < 0 {
		valueStart := markerIndex + len(prefix)
		valueEnd := valueStart + len(checksum)
		hasher := sha256.New()
		_, _ = hasher.Write(sidecarData[:valueStart])
		_, _ = hasher.Write(sidecarData[valueEnd:])
		return strings.EqualFold(checksum, hex.EncodeToString(hasher.Sum(nil)))
	}
	sidecar.Checksum = ""
	checksumData, err := json.Marshal(sidecar)
	return err == nil && strings.EqualFold(checksum, sha256Hex(checksumData))
}

func (p *UsageProjection) applyPersistedMetadata(sidecar IdentitySidecar, snapshot projectionGenerationState) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if snapshot.DatasetEpoch > 0 {
		p.datasetEpoch = snapshot.DatasetEpoch
	}
	if snapshot.Revision > p.revision {
		p.revision = snapshot.Revision
	}
	if snapshot.RewriteRevision > p.rewriteRevision {
		p.rewriteRevision = snapshot.RewriteRevision
	}
	if sidecar.NextSequence > p.nextSequence {
		p.nextSequence = sidecar.NextSequence
	}
	for key, ordinal := range sidecar.SourceOrdinals {
		if ordinal > p.ordinals[key] {
			p.ordinals[key] = ordinal
		}
	}
}

func sortedSidecarEntries(sidecar IdentitySidecar) []IdentitySidecarEntry {
	entries := append([]IdentitySidecarEntry(nil), sidecar.Entries...)
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].Sequence != entries[j].Sequence {
			return entries[i].Sequence < entries[j].Sequence
		}
		return entries[i].StableEventID < entries[j].StableEventID
	})
	return entries
}
