package usage

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"sort"
)

const projectionStorageSchemaVersionV2 = "usage_projection_storage_v2"

var projectionStorageMagicV2 = []byte("USAGE-PROJECTION-STORAGE-V2\x00")

type projectionGenerationState struct {
	SchemaVersion      string
	DatasetEpoch       uint64
	Revision           uint64
	RewriteRevision    uint64
	StorageMetrics     ProjectionStorageMetrics
	FactsSHA256        string
	PostingsSHA256     string
	ScalarRollupSHA256 string
}

func (stats *RequestStatistics) marshalProjectionGenerationStateV2() (projectionGenerationState, []byte, error) {
	if stats == nil {
		return NewUsageProjection().marshalGenerationStateV2()
	}
	stats.mu.RLock()
	projection := stats.projection
	stats.mu.RUnlock()
	return projection.marshalGenerationStateV2()
}

func (projection *UsageProjection) marshalGenerationStateV2() (projectionGenerationState, []byte, error) {
	if projection == nil {
		projection = NewUsageProjection()
	}
	projection.mu.RLock()
	defer projection.mu.RUnlock()

	facts, err := projection.marshalFactsV2Locked()
	if err != nil {
		return projectionGenerationState{}, nil, err
	}
	postings, err := projection.marshalPostingsV2Locked()
	if err != nil {
		return projectionGenerationState{}, nil, err
	}
	rollups, err := projection.marshalScalarRollupsV2Locked()
	if err != nil {
		return projectionGenerationState{}, nil, err
	}
	state := projectionGenerationState{
		SchemaVersion:      projectionStorageSchemaVersionV2,
		DatasetEpoch:       projection.datasetEpoch,
		Revision:           projection.revision,
		RewriteRevision:    projection.rewriteRevision,
		StorageMetrics:     projection.storageMetricsLocked(),
		FactsSHA256:        projectionSectionSHA256(facts),
		PostingsSHA256:     projectionSectionSHA256(postings),
		ScalarRollupSHA256: projectionSectionSHA256(rollups),
	}

	var payload bytes.Buffer
	payload.Grow(len(projectionStorageMagicV2) + len(facts) + len(postings) + len(rollups) + 512)
	payload.Write(projectionStorageMagicV2)
	writeProjectionString(&payload, state.SchemaVersion)
	writeProjectionUint64(&payload, state.DatasetEpoch)
	writeProjectionUint64(&payload, state.Revision)
	writeProjectionUint64(&payload, state.RewriteRevision)
	writeProjectionStorageMetrics(&payload, state.StorageMetrics)
	for _, section := range []struct {
		name string
		hash string
		data []byte
	}{
		{name: "facts", hash: state.FactsSHA256, data: facts},
		{name: "postings", hash: state.PostingsSHA256, data: postings},
		{name: "scalar_rollups", hash: state.ScalarRollupSHA256, data: rollups},
	} {
		writeProjectionString(&payload, section.name)
		writeProjectionString(&payload, section.hash)
		writeProjectionBytes(&payload, section.data)
	}
	return state, payload.Bytes(), nil
}

func decodeProjectionGenerationStateV2(data []byte) (projectionGenerationState, projectionV2Sections, error) {
	reader := bytes.NewReader(data)
	magic := make([]byte, len(projectionStorageMagicV2))
	if _, err := io.ReadFull(reader, magic); err != nil || !bytes.Equal(magic, projectionStorageMagicV2) {
		return projectionGenerationState{}, projectionV2Sections{}, fmt.Errorf("%w: invalid projection storage magic", ErrProjectionRestoreUnavailable)
	}
	schemaVersion, err := readProjectionString(reader)
	if err != nil || schemaVersion != projectionStorageSchemaVersionV2 {
		return projectionGenerationState{}, projectionV2Sections{}, fmt.Errorf("%w: unsupported projection storage schema", ErrProjectionRestoreUnavailable)
	}
	state := projectionGenerationState{SchemaVersion: schemaVersion}
	if state.DatasetEpoch, err = readProjectionUint64(reader); err != nil {
		return projectionGenerationState{}, projectionV2Sections{}, fmt.Errorf("%w: decode projection dataset epoch: %v", ErrProjectionRestoreUnavailable, err)
	}
	if state.Revision, err = readProjectionUint64(reader); err != nil {
		return projectionGenerationState{}, projectionV2Sections{}, fmt.Errorf("%w: decode projection revision: %v", ErrProjectionRestoreUnavailable, err)
	}
	if state.RewriteRevision, err = readProjectionUint64(reader); err != nil {
		return projectionGenerationState{}, projectionV2Sections{}, fmt.Errorf("%w: decode projection rewrite revision: %v", ErrProjectionRestoreUnavailable, err)
	}
	if state.StorageMetrics, err = readProjectionStorageMetrics(reader); err != nil {
		return projectionGenerationState{}, projectionV2Sections{}, fmt.Errorf("%w: decode projection metrics: %v", ErrProjectionRestoreUnavailable, err)
	}
	var sections projectionV2Sections
	expectedSections := []string{"facts", "postings", "scalar_rollups"}
	for _, expectedName := range expectedSections {
		name, readErr := readProjectionString(reader)
		if readErr != nil || name != expectedName {
			return projectionGenerationState{}, projectionV2Sections{}, fmt.Errorf("%w: invalid projection section %q", ErrProjectionRestoreUnavailable, name)
		}
		expectedHash, readErr := readProjectionString(reader)
		if readErr != nil {
			return projectionGenerationState{}, projectionV2Sections{}, fmt.Errorf("%w: decode projection section hash: %v", ErrProjectionRestoreUnavailable, readErr)
		}
		section, readErr := readProjectionBytesView(reader, data)
		if readErr != nil || !stringsEqualFoldASCII(expectedHash, projectionSectionSHA256(section)) {
			return projectionGenerationState{}, projectionV2Sections{}, fmt.Errorf("%w: projection section checksum mismatch %s", ErrProjectionRestoreUnavailable, expectedName)
		}
		switch expectedName {
		case "facts":
			state.FactsSHA256 = expectedHash
			sections.Facts = section
		case "postings":
			state.PostingsSHA256 = expectedHash
			sections.Postings = section
		case "scalar_rollups":
			state.ScalarRollupSHA256 = expectedHash
			sections.ScalarRollups = section
		}
	}
	if reader.Len() != 0 {
		return projectionGenerationState{}, projectionV2Sections{}, fmt.Errorf("%w: trailing projection storage bytes", ErrProjectionRestoreUnavailable)
	}
	return state, sections, nil
}

func (projection *UsageProjection) marshalFactsV2Locked() ([]byte, error) {
	var buffer bytes.Buffer
	writeProjectionUint64(&buffer, uint64(len(projection.stringRegistry.values)))
	for _, value := range projection.stringRegistry.values {
		writeProjectionString(&buffer, value)
	}
	rowCount := len(projection.facts.timestampSeconds)
	writeProjectionUint64(&buffer, uint64(rowCount))
	for index := 0; index < rowCount; index++ {
		buffer.WriteByte(projection.facts.timestampPresent[index])
		writeProjectionInt64(&buffer, projection.facts.timestampSeconds[index])
		writeProjectionUint32(&buffer, uint32(projection.facts.timestampNanoseconds[index]))
		writeProjectionUint64(&buffer, projection.facts.sequences[index])
		writeProjectionUint64(&buffer, projection.facts.batchOrdinals[index])
		writeProjectionUint64(&buffer, projection.facts.versions[index])
		writeProjectionInt64(&buffer, projection.facts.latencyMs[index])
		for _, id := range []projectionStringID{
			projection.facts.apiIDs[index], projection.facts.modelIDs[index], projection.facts.providerIDs[index],
			projection.facts.authIDs[index], projection.facts.sourceIDs[index], projection.facts.priceKeyIDs[index], projection.facts.providerStateIDs[index],
		} {
			writeProjectionUint32(&buffer, uint32(id))
		}
		for column := range projection.facts.facetIDs {
			writeProjectionUint32(&buffer, uint32(projection.facts.facetIDs[column][index]))
		}
		for column := range projection.facts.tokenColumns {
			writeProjectionInt64(&buffer, projection.facts.tokenColumns[column][index])
		}
		for column := range projection.facts.billableColumns {
			writeProjectionInt64(&buffer, projection.facts.billableColumns[column][index])
		}
		buffer.WriteByte(projection.facts.failed[index])
		buffer.WriteByte(projection.facts.usageSources[index])
		buffer.WriteByte(projection.facts.classifications[index])
		buffer.WriteByte(byte(projection.facts.componentMasks[index]))
		buffer.WriteByte(boolByte(projection.facts.isActive(projectionFactRowID(index + 1))))
		writeProjectionString(&buffer, projection.facts.stableEventIDs[index])
		writeProjectionString(&buffer, projection.facts.canonicalEventIdentities[index])
	}
	return buffer.Bytes(), nil
}

func (projection *UsageProjection) marshalPostingsV2Locked() ([]byte, error) {
	keys := make([]projectionPostingKey, 0, len(projection.postings.rows))
	for key := range projection.postings.rows {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].Dimension != keys[j].Dimension {
			return keys[i].Dimension < keys[j].Dimension
		}
		return keys[i].Value < keys[j].Value
	})
	var buffer bytes.Buffer
	writeProjectionUint64(&buffer, uint64(len(keys)))
	for _, key := range keys {
		buffer.WriteByte(byte(key.Dimension))
		writeProjectionUint32(&buffer, uint32(key.Value))
		rows := projection.postings.rows[key]
		writeProjectionUint64(&buffer, uint64(len(rows)))
		for _, rowID := range rows {
			writeProjectionUint32(&buffer, uint32(rowID))
		}
	}
	return buffer.Bytes(), nil
}

func (projection *UsageProjection) marshalScalarRollupsV2Locked() ([]byte, error) {
	var buffer bytes.Buffer
	writeCompactAggregateV2(&buffer, projection.totals)
	writeProjectionFacetMapV2(&buffer, projection.facets)
	for _, buckets := range []map[string]compactHourBucket{projection.hours, projection.days, projection.weeks, projection.months, projection.years} {
		writeProjectionBucketMapV2(&buffer, buckets)
	}
	healthKeys := make([]string, 0, len(projection.health))
	for key := range projection.health {
		healthKeys = append(healthKeys, key)
	}
	sort.Strings(healthKeys)
	writeProjectionUint64(&buffer, uint64(len(healthKeys)))
	for _, key := range healthKeys {
		bucket := projection.health[key]
		writeProjectionString(&buffer, key)
		writeProjectionInt64(&buffer, bucket.IntervalStart.UnixNano())
		writeProjectionInt64(&buffer, bucket.IntervalEnd.UnixNano())
		writeProjectionInt64(&buffer, bucket.TotalRequests)
		writeProjectionInt64(&buffer, bucket.SuccessCount)
		writeProjectionInt64(&buffer, bucket.FailureCount)
		writeProjectionInt64(&buffer, bucket.LatencyMsSum)
	}
	writeProjectionCatalogV2(&buffer, projection.catalog)
	return buffer.Bytes(), nil
}

func writeProjectionBucketMapV2(buffer *bytes.Buffer, buckets map[string]compactHourBucket) {
	keys := make([]string, 0, len(buckets))
	for key := range buckets {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	writeProjectionUint64(buffer, uint64(len(keys)))
	for _, key := range keys {
		bucket := buckets[key]
		writeProjectionString(buffer, key)
		writeProjectionInt64(buffer, bucket.IntervalStart.UnixNano())
		writeProjectionInt64(buffer, bucket.IntervalEnd.UnixNano())
		writeCompactAggregateV2(buffer, bucket.Totals)
		writeProjectionFacetMapV2(buffer, bucket.Facets)
		for _, values := range []map[projectionStringID]compactAggregate{bucket.APIs, bucket.Models, bucket.Providers, bucket.Auths, bucket.Sources} {
			writeProjectionAggregateMapV2(buffer, values)
		}
	}
}

func writeProjectionAggregateMapV2(buffer *bytes.Buffer, values map[projectionStringID]compactAggregate) {
	keys := make([]projectionStringID, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	writeProjectionUint64(buffer, uint64(len(keys)))
	for _, key := range keys {
		writeProjectionUint32(buffer, uint32(key))
		writeCompactAggregateV2(buffer, values[key])
	}
}

func writeProjectionFacetMapV2(buffer *bytes.Buffer, values map[projectionStringID]int64) {
	keys := make([]projectionStringID, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	writeProjectionUint64(buffer, uint64(len(keys)))
	for _, key := range keys {
		writeProjectionUint32(buffer, uint32(key))
		writeProjectionInt64(buffer, values[key])
	}
}

func writeCompactAggregateV2(buffer *bytes.Buffer, aggregate compactAggregate) {
	for index := 0; index < compactAggregateCounterCount; index++ {
		writeProjectionInt64(buffer, aggregate.counters.value(index))
	}
}

func writeProjectionCatalogV2(buffer *bytes.Buffer, catalog ProjectionCatalog) {
	for _, values := range []map[string]CatalogEntry{catalog.Models, catalog.PriceKeys, catalog.Sources} {
		keys := make([]string, 0, len(values))
		for key := range values {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		writeProjectionUint64(buffer, uint64(len(keys)))
		for _, key := range keys {
			entry := values[key]
			writeProjectionString(buffer, key)
			writeProjectionString(buffer, entry.ID)
			writeProjectionString(buffer, entry.Label)
			writeProjectionString(buffer, entry.PriceKey)
			writeProjectionString(buffer, entry.Provider)
			writeProjectionString(buffer, entry.ProviderState)
		}
	}
}

func writeProjectionStorageMetrics(buffer *bytes.Buffer, metrics ProjectionStorageMetrics) {
	for _, value := range []uint64{
		metrics.FactRows, metrics.ActiveFactRows, metrics.TombstonedFactRows, metrics.PostingRefs,
		metrics.RegistryEntries, metrics.RollupCells, metrics.RollupPricingGroups, metrics.EstimatedProjectionBytes,
	} {
		writeProjectionUint64(buffer, value)
	}
}

func readProjectionStorageMetrics(reader *bytes.Reader) (ProjectionStorageMetrics, error) {
	values := make([]uint64, 8)
	for index := range values {
		value, err := readProjectionUint64(reader)
		if err != nil {
			return ProjectionStorageMetrics{}, err
		}
		values[index] = value
	}
	return ProjectionStorageMetrics{
		FactRows: values[0], ActiveFactRows: values[1], TombstonedFactRows: values[2], PostingRefs: values[3],
		RegistryEntries: values[4], RollupCells: values[5], RollupPricingGroups: values[6], EstimatedProjectionBytes: values[7],
	}, nil
}

func writeProjectionUint64(buffer *bytes.Buffer, value uint64) {
	var data [8]byte
	binary.BigEndian.PutUint64(data[:], value)
	buffer.Write(data[:])
}

func writeProjectionInt64(buffer *bytes.Buffer, value int64) {
	writeProjectionUint64(buffer, uint64(value))
}

func writeProjectionUint32(buffer *bytes.Buffer, value uint32) {
	var data [4]byte
	binary.BigEndian.PutUint32(data[:], value)
	buffer.Write(data[:])
}

func writeProjectionString(buffer *bytes.Buffer, value string) {
	writeProjectionUint64(buffer, uint64(len(value)))
	buffer.WriteString(value)
}

func writeProjectionBytes(buffer *bytes.Buffer, value []byte) {
	writeProjectionUint64(buffer, uint64(len(value)))
	buffer.Write(value)
}

func readProjectionUint64(reader *bytes.Reader) (uint64, error) {
	if reader.Len() < 8 {
		return 0, io.ErrUnexpectedEOF
	}
	var data [8]byte
	_, _ = reader.Read(data[:])
	return binary.BigEndian.Uint64(data[:]), nil
}

func readProjectionUint32(reader *bytes.Reader) (uint32, error) {
	if reader.Len() < 4 {
		return 0, io.ErrUnexpectedEOF
	}
	var data [4]byte
	_, _ = reader.Read(data[:])
	return binary.BigEndian.Uint32(data[:]), nil
}

func readProjectionByte(reader *bytes.Reader) (byte, error) {
	value, err := reader.ReadByte()
	if err != nil {
		return 0, io.ErrUnexpectedEOF
	}
	return value, nil
}

func readProjectionInt64(reader *bytes.Reader) (int64, error) {
	value, err := readProjectionUint64(reader)
	return int64(value), err
}

func readProjectionString(reader *bytes.Reader) (string, error) {
	data, err := readProjectionBytes(reader)
	return string(data), err
}

func readProjectionBytes(reader *bytes.Reader) ([]byte, error) {
	length, err := readProjectionUint64(reader)
	if err != nil {
		return nil, err
	}
	if length > uint64(reader.Len()) || length > uint64(^uint(0)>>1) {
		return nil, io.ErrUnexpectedEOF
	}
	data := make([]byte, int(length))
	_, _ = reader.Read(data)
	return data, nil
}

func readProjectionBytesView(reader *bytes.Reader, source []byte) ([]byte, error) {
	length, err := readProjectionUint64(reader)
	if err != nil {
		return nil, err
	}
	if length > uint64(reader.Len()) || length > uint64(^uint(0)>>1) {
		return nil, io.ErrUnexpectedEOF
	}
	start := len(source) - reader.Len()
	end := start + int(length)
	if start < 0 || end < start || end > len(source) {
		return nil, io.ErrUnexpectedEOF
	}
	if _, err := reader.Seek(int64(length), io.SeekCurrent); err != nil {
		return nil, err
	}
	return source[start:end], nil
}

func projectionSectionSHA256(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

func stringsEqualFoldASCII(left, right string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		leftByte := left[index]
		rightByte := right[index]
		if leftByte >= 'A' && leftByte <= 'Z' {
			leftByte += 'a' - 'A'
		}
		if rightByte >= 'A' && rightByte <= 'Z' {
			rightByte += 'a' - 'A'
		}
		if leftByte != rightByte {
			return false
		}
	}
	return true
}
