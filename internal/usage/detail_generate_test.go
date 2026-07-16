package usage

import (
	"context"
	"testing"
	"time"

	coreusage "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
)

func TestCanonicalRequestDetailGenerateDefaultsToTrue(t *testing.T) {
	detail := CanonicalRequestDetail(context.Background(), coreusage.Record{})
	if detail.Generate == nil || !*detail.Generate {
		t.Fatalf("Generate = %v, want true", detail.Generate)
	}
}

func TestCanonicalRequestDetailPreservesExplicitGenerateFalse(t *testing.T) {
	detail := CanonicalRequestDetail(context.Background(), coreusage.Record{
		Generate: coreusage.GenerateFlag(false),
	})
	if detail.Generate == nil || *detail.Generate {
		t.Fatalf("Generate = %v, want false", detail.Generate)
	}
}

func TestNormalizeRequestDetailLegacyGenerateDefaultsToTrue(t *testing.T) {
	detail := normalizeRequestDetail(RequestDetail{}, "openai")
	if detail.Generate == nil || !*detail.Generate {
		t.Fatalf("Generate = %v, want true for legacy detail", detail.Generate)
	}
}

func TestExplicitGenerateFalseEnrichesLegacyDefault(t *testing.T) {
	stats := NewRequestStatistics()
	legacy := generateMergeTestDetail(nil)
	stats.MergeSnapshot(generateMergeTestSnapshot(legacy))

	explicitFalse := generateMergeTestDetail(coreusage.GenerateFlag(false))
	stats.MergeSnapshot(generateMergeTestSnapshot(explicitFalse))

	detail := stats.Snapshot().APIs[legacy.Endpoint].Models[legacy.Model].Details[0]
	if detail.Generate == nil || *detail.Generate {
		t.Fatalf("Generate = %v, want explicit false to replace legacy default", detail.Generate)
	}
}

func TestLegacyMissingGenerateDoesNotOverwriteExplicitFalse(t *testing.T) {
	stats := NewRequestStatistics()
	explicitFalse := generateMergeTestDetail(coreusage.GenerateFlag(false))
	stats.MergeSnapshot(generateMergeTestSnapshot(explicitFalse))

	legacy := generateMergeTestDetail(nil)
	stats.MergeSnapshot(generateMergeTestSnapshot(legacy))

	detail := stats.Snapshot().APIs[legacy.Endpoint].Models[legacy.Model].Details[0]
	if detail.Generate == nil || *detail.Generate {
		t.Fatalf("Generate = %v, want explicit false preserved over legacy missing value", detail.Generate)
	}
}

func generateMergeTestDetail(generate *bool) RequestDetail {
	return RequestDetail{
		RequestID: "req-generate-merge",
		Timestamp: time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC),
		Endpoint:  "POST /v1/responses",
		Model:     "gpt-5.4",
		Provider:  "openai",
		AuthIndex: "auth-generate-merge",
		Generate:  generate,
		Tokens: RequestTokenStats{
			InputTokens:  1,
			OutputTokens: 1,
			TotalTokens:  2,
		},
	}
}

func generateMergeTestSnapshot(detail RequestDetail) StatisticsSnapshot {
	return StatisticsSnapshot{APIs: map[string]APISnapshot{
		detail.Endpoint: {
			Models: map[string]ModelSnapshot{
				detail.Model: {Details: []RequestDetail{detail}},
			},
		},
	}}
}
