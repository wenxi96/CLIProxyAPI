package usage

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// UsageExportServerFrame contains the projection-owned portion of the final
// usage-events-v2 JSON/CSV artifact. Client-owned source and cost cells are
// deliberately excluded and remain incomplete until the client profile is
// applied to immutable local snapshots.
type UsageExportServerFrame struct {
	JSONBytes        uint64
	JSONComplete     bool
	CSVBytes         uint64
	CSVComplete      bool
	EventCount       uint64
	MatchedSourceIDs map[string]uint64
	MatchedModels    map[string]uint64
}

const (
	usageExportJSONProfileMaxIntegerBytes = uint64(19)
	usageExportJSONProfileMaxRatioBytes   = uint64(10)
	usageExportCSVHeaderV1                = "timestamp,model,source,source_raw,auth_index,result,latency_ms,thinking_intensity,thinking_mode,thinking_level,thinking_budget,input_tokens,output_tokens,reasoning_tokens,cache_read_tokens,cache_creation_tokens,cached_tokens,cache_ratio,total_tokens,reported_total_tokens,computed_total_tokens,input_cost_usd,output_cost_usd,cache_cost_usd,total_cost_usd,cost_status,missing_price_models,missing_price_components\r\n"
)

// usageExportServerRowBytesV1 computes bounded server-owned bytes from compact
// projection facts and its string registry. It never reads canonical details.
func (p *UsageProjection) usageExportServerRowBytesV1(event EventRef) (uint64, uint64, bool) {
	if p == nil {
		return 0, 0, false
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.usageExportServerRowBytesV1Locked(event)
}

func (p *UsageProjection) usageExportServerRowBytesV1Locked(event EventRef) (uint64, uint64, bool) {
	projectionEvent, ok := p.events[event.StableEventID]
	if !ok || projectionEvent.CanonicalEventIdentity != event.CanonicalEventIdentity || projectionEvent.Version != event.Version {
		return 0, 0, false
	}
	contribution, ok := p.facts.contribution(projectionEvent.FactRowID, p.stringRegistry)
	if !ok {
		return 0, 0, false
	}
	sourceRaw := ""
	if event.SourceID != "" {
		entry, exists := p.catalog.Sources[event.SourceID]
		if !exists {
			return 0, 0, false
		}
		sourceRaw = entry.Label
	}

	jsonBytes, jsonOK := usageExportJSONServerRowBytesV1(contribution, sourceRaw)
	csvBytes, csvOK := usageExportCSVServerRowBytesV1(contribution, sourceRaw)
	return jsonBytes, csvBytes, jsonOK && csvOK
}

func usageExportJSONServerRowBytesV1(contribution projectionContribution, sourceRaw string) (uint64, bool) {
	serverFields := []uint64{
		jsonFieldBytes("timestamp", jsonTimeBytes(contribution.Timestamp)),
		jsonFieldBytes("model", jsonStringBytes(contribution.Model)),
		jsonFieldBytes("source_raw", jsonStringBytes(sourceRaw)),
		jsonFieldBytes("auth_index", jsonStringBytes(contribution.AuthIndex)),
		jsonFieldBytes("failed", jsonBoolBytes(contribution.Failed)),
		jsonFieldBytes("latency_ms", usageExportJSONIntegerBytes()),
		jsonFieldBytes("thinking", 4),
		jsonFieldBytes("thinking_coverage", jsonStringBytes("unavailable_legacy")),
		jsonFieldBytes("tokens", usageExportJSONTokensBytes()),
	}
	var total uint64 = 2 // object braces
	for _, value := range serverFields {
		if !addUsageExportBound(&total, value) {
			return 0, false
		}
	}
	// The profile row has eleven fixed members. The server owns all member
	// separators, including the two separators adjacent to client-owned source
	// and cost slots.
	if !addUsageExportBound(&total, 10) {
		return 0, false
	}
	return total, true
}

func usageExportJSONTokensBytes() uint64 {
	keys := [...]string{
		"input_tokens", "output_tokens", "reasoning_tokens", "cache_read_tokens",
		"cache_creation_tokens", "cached_tokens", "total_tokens", "reported_total_tokens",
		"computed_total_tokens", "cache_ratio",
	}
	var total uint64 = 2
	for index, key := range keys {
		if index > 0 {
			total++
		}
		valueBytes := usageExportJSONProfileMaxIntegerBytes
		if key == "cache_ratio" {
			valueBytes = usageExportJSONProfileMaxRatioBytes
		}
		total += jsonFieldBytes(key, valueBytes)
	}
	return total
}

func usageExportJSONIntegerBytes() uint64 { return usageExportJSONProfileMaxIntegerBytes }

func jsonFieldBytes(key string, valueBytes uint64) uint64 {
	return uint64(len(key)+4 /* quotes around key and colon */) + valueBytes
}

func jsonStringBytes(value string) uint64 {
	encoded, err := json.Marshal(value)
	if err != nil {
		return 0
	}
	return uint64(len(encoded))
}

func jsonTimeBytes(value time.Time) uint64 {
	encoded, err := json.Marshal(value.UTC())
	if err != nil {
		return 0
	}
	return uint64(len(encoded))
}

func jsonBoolBytes(value bool) uint64 {
	if value {
		return 4
	}
	return 5
}

func usageExportCSVServerRowBytesV1(contribution projectionContribution, sourceRaw string) (uint64, bool) {
	serverCells := []string{
		contribution.Timestamp.UTC().Format(time.RFC3339Nano),
		contribution.Model,
		"", // source is client-owned
		sourceRaw,
		contribution.AuthIndex,
		map[bool]string{true: "failed", false: "success"}[contribution.Failed],
		strconv.FormatInt(contribution.LatencyMs, 10),
		"", "", "", "", // thinking columns are fixed empty values
		strconv.FormatInt(contribution.Tokens.InputTokens, 10),
		strconv.FormatInt(contribution.Tokens.OutputTokens, 10),
		strconv.FormatInt(contribution.Tokens.ReasoningTokens, 10),
		strconv.FormatInt(contribution.Tokens.CacheReadTokens, 10),
		strconv.FormatInt(contribution.Tokens.CacheCreationTokens, 10),
		strconv.FormatInt(contribution.Tokens.CachedTokens, 10),
		"1.00000000", // canonical_decimal_v1 ratio upper bound
		strconv.FormatInt(contribution.Tokens.TotalTokens, 10),
		strconv.FormatInt(contribution.Tokens.ReportedTotalTokens, 10),
		strconv.FormatInt(contribution.Tokens.ComputedTotalTokens, 10),
		"", "", "", "", "", "", "", // cost columns are client-owned
	}
	var total uint64
	for index, value := range serverCells {
		if index == 2 || index >= 21 {
			continue
		}
		cellBytes, ok := usageExportCSVCellBytes(value)
		if !ok || !addUsageExportBound(&total, cellBytes) {
			return 0, false
		}
	}
	// The server owns all inter-column separators. The client profile owns the
	// final row CRLF together with its derived cells, matching the v1 framing
	// formula and avoiding double counting during bound composition.
	if !addUsageExportBound(&total, 27) {
		return 0, false
	}
	return total, true
}

func usageExportCSVCellBytes(value string) (uint64, bool) {
	trimmed := strings.TrimLeftFunc(value, unicode.IsSpace)
	if trimmed != "" && strings.ContainsRune("=+-@", []rune(trimmed)[0]) {
		value = "'" + value
	}
	escaped := strings.ReplaceAll(value, `"`, `""`)
	return uint64(len(escaped) + 2), true // RFC4180 quotes
}

func addUsageExportBound(total *uint64, value uint64) bool {
	if total == nil || ^uint64(0)-*total < value {
		return false
	}
	*total += value
	return true
}
