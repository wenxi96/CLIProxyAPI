package usage

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"
)

type exportProfileTokensFixtureV1 struct {
	InputTokens         int64  `json:"input_tokens"`
	OutputTokens        int64  `json:"output_tokens"`
	ReasoningTokens     int64  `json:"reasoning_tokens"`
	CacheReadTokens     int64  `json:"cache_read_tokens"`
	CacheCreationTokens int64  `json:"cache_creation_tokens"`
	CachedTokens        int64  `json:"cached_tokens"`
	TotalTokens         int64  `json:"total_tokens"`
	ReportedTotalTokens int64  `json:"reported_total_tokens"`
	ComputedTotalTokens int64  `json:"computed_total_tokens"`
	CacheRatio          string `json:"cache_ratio"`
}

type exportProfileCostFixtureV1 struct {
	InputCostUSD           string   `json:"input_cost_usd"`
	OutputCostUSD          string   `json:"output_cost_usd"`
	CacheCostUSD           string   `json:"cache_cost_usd"`
	TotalCostUSD           string   `json:"total_cost_usd"`
	CostStatus             string   `json:"cost_status"`
	MissingPriceModels     []string `json:"missing_price_models"`
	MissingPriceComponents []string `json:"missing_price_components"`
}

type exportProfileRowFixtureV1 struct {
	Timestamp        time.Time                    `json:"timestamp"`
	Model            string                       `json:"model"`
	Source           string                       `json:"source"`
	SourceRaw        string                       `json:"source_raw"`
	AuthIndex        string                       `json:"auth_index"`
	Failed           bool                         `json:"failed"`
	LatencyMs        int64                        `json:"latency_ms"`
	Thinking         any                          `json:"thinking"`
	ThinkingCoverage string                       `json:"thinking_coverage"`
	Tokens           exportProfileTokensFixtureV1 `json:"tokens"`
	Cost             exportProfileCostFixtureV1   `json:"cost"`
}

func marshalExportProfileRowFixtureV1(contribution projectionContribution, sourceRaw string) ([]byte, error) {
	return json.Marshal(exportProfileRowFixtureV1{
		Timestamp:        contribution.Timestamp.UTC(),
		Model:            contribution.Model,
		Source:           "Display + label",
		SourceRaw:        sourceRaw,
		AuthIndex:        contribution.AuthIndex,
		Failed:           contribution.Failed,
		LatencyMs:        contribution.LatencyMs,
		Thinking:         nil,
		ThinkingCoverage: "unavailable_legacy",
		Tokens: exportProfileTokensFixtureV1{
			InputTokens: contribution.Tokens.InputTokens, OutputTokens: contribution.Tokens.OutputTokens,
			ReasoningTokens: contribution.Tokens.ReasoningTokens, CacheReadTokens: contribution.Tokens.CacheReadTokens,
			CacheCreationTokens: contribution.Tokens.CacheCreationTokens, CachedTokens: contribution.Tokens.CachedTokens,
			TotalTokens: contribution.Tokens.TotalTokens, ReportedTotalTokens: contribution.Tokens.ReportedTotalTokens,
			ComputedTotalTokens: contribution.Tokens.ComputedTotalTokens, CacheRatio: "1.00000000",
		},
		Cost: exportProfileCostFixtureV1{
			InputCostUSD: "1.25000000", OutputCostUSD: "2.50000000", CacheCostUSD: "0.12500000",
			TotalCostUSD: "3.87500000", CostStatus: "complete",
			MissingPriceModels: []string{}, MissingPriceComponents: []string{},
		},
	})
}

func exportProfileJSONClientMemberBytesV1(source string, cost exportProfileCostFixtureV1) (uint64, error) {
	encoded, err := json.Marshal(struct {
		Source string                     `json:"source"`
		Cost   exportProfileCostFixtureV1 `json:"cost"`
	}{Source: source, Cost: cost})
	if err != nil {
		return 0, err
	}
	if len(encoded) < 3 {
		return 0, nil
	}
	// Remove object braces and the separator owned by the server row bound.
	return uint64(len(encoded) - 3), nil
}

func exportProfileCSVCellFixtureV1(value string) string {
	trimmed := strings.TrimLeft(value, " \t\r\n")
	if trimmed != "" && strings.ContainsRune("=+-@", []rune(trimmed)[0]) {
		value = "'" + value
	}
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

func exportProfileCSVRowFixtureV1(contribution projectionContribution, sourceRaw string) []string {
	return []string{
		contribution.Timestamp.UTC().Format(time.RFC3339Nano), contribution.Model, "Display + label", sourceRaw,
		contribution.AuthIndex, map[bool]string{true: "failed", false: "success"}[contribution.Failed],
		strconv.FormatInt(contribution.LatencyMs, 10), "", "", "", "",
		strconv.FormatInt(contribution.Tokens.InputTokens, 10), strconv.FormatInt(contribution.Tokens.OutputTokens, 10),
		strconv.FormatInt(contribution.Tokens.ReasoningTokens, 10), strconv.FormatInt(contribution.Tokens.CacheReadTokens, 10),
		strconv.FormatInt(contribution.Tokens.CacheCreationTokens, 10), strconv.FormatInt(contribution.Tokens.CachedTokens, 10),
		"1.00000000", strconv.FormatInt(contribution.Tokens.TotalTokens, 10),
		strconv.FormatInt(contribution.Tokens.ReportedTotalTokens, 10), strconv.FormatInt(contribution.Tokens.ComputedTotalTokens, 10),
		"1.25000000", "2.50000000", "0.12500000", "3.87500000", "complete", "", "",
	}
}

func TestUsageExportFinalProfileDifferentialBoundsV1(t *testing.T) {
	contribution := projectionContribution{
		Model:     "=model/\"unicode-模型",
		AuthIndex: "auth,\"index",
		SourceKey: "redacted:+source/\"ключ",
		Timestamp: time.Date(2026, 8, 10, 12, 34, 56, 123456789, time.UTC),
		Failed:    true,
		LatencyMs: 987654321,
		Tokens:    RequestTokenStats{InputTokens: 123, OutputTokens: 456, ReasoningTokens: 7, CacheReadTokens: 8, CacheCreationTokens: 9, CachedTokens: 17, TotalTokens: 602, ReportedTotalTokens: 602, ComputedTotalTokens: 602},
	}

	jsonBound, jsonComplete := usageExportJSONServerRowBytesV1(contribution, contribution.SourceKey)
	if !jsonComplete {
		t.Fatal("JSON server bound unexpectedly incomplete")
	}
	jsonArtifact, err := marshalExportProfileRowFixtureV1(contribution, contribution.SourceKey)
	if err != nil {
		t.Fatalf("marshal JSON artifact: %v", err)
	}
	jsonClientBytes, err := exportProfileJSONClientMemberBytesV1("Display + label", exportProfileCostFixtureV1{
		InputCostUSD: "1.25000000", OutputCostUSD: "2.50000000", CacheCostUSD: "0.12500000", TotalCostUSD: "3.87500000", CostStatus: "complete",
		MissingPriceModels: []string{}, MissingPriceComponents: []string{},
	})
	if err != nil {
		t.Fatalf("marshal JSON client fields: %v", err)
	}
	if uint64(len(jsonArtifact)) > saturatingAddUint64(jsonBound, jsonClientBytes) {
		t.Fatalf("JSON artifact exceeds owned bounds: actual=%d server=%d client=%d", len(jsonArtifact), jsonBound, jsonClientBytes)
	}

	csvBound, csvComplete := usageExportCSVServerRowBytesV1(contribution, contribution.SourceKey)
	if !csvComplete {
		t.Fatal("CSV server bound unexpectedly incomplete")
	}
	cells := exportProfileCSVRowFixtureV1(contribution, contribution.SourceKey)
	encodedCells := make([]string, len(cells))
	for index, cell := range cells {
		encodedCells[index] = exportProfileCSVCellFixtureV1(cell)
	}
	csvArtifact := strings.Join(encodedCells, ",") + "\r\n"
	var csvClientBytes uint64
	for index, cell := range encodedCells {
		if index == 2 || index >= 21 {
			csvClientBytes = saturatingAddUint64(csvClientBytes, uint64(len(cell)))
		}
	}
	csvClientBytes = saturatingAddUint64(csvClientBytes, 2) // client-owned row CRLF
	if uint64(len(csvArtifact)) > saturatingAddUint64(csvBound, csvClientBytes) {
		t.Fatalf("CSV artifact exceeds owned bounds: actual=%d server=%d client=%d", len(csvArtifact), csvBound, csvClientBytes)
	}
}

func TestUsageExportEmptyFinalFramingBoundsV1(t *testing.T) {
	// Empty JSON has only the final array brackets; CSV keeps its fixed header.
	if got := uint64(len("[]")); got != 2 {
		t.Fatalf("empty JSON framing bytes=%d", got)
	}
	if got := uint64(len(usageExportCSVHeaderV1)); got == 0 || !strings.HasSuffix(usageExportCSVHeaderV1, "\r\n") {
		t.Fatalf("empty CSV framing header bytes=%d header=%q", got, usageExportCSVHeaderV1)
	}
}
