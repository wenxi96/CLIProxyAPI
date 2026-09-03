package usage

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	coreusage "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
)

func projectionTestDetail(requestID string, timestamp time.Time, provider string, model string, tokens RequestTokenStats) RequestDetail {
	return normalizeRequestDetail(RequestDetail{
		RequestID: requestID,
		Timestamp: timestamp,
		Endpoint:  "POST /v1/responses",
		Model:     model,
		Provider:  provider,
		AuthType:  "api_key",
		AuthIndex: "auth-projection",
		Source:    "auth-projection",
		Tokens:    tokens,
	}, provider)
}

func TestProjectionIdentityAndPriceKeysAreStable(t *testing.T) {
	detail := projectionTestDetail("req-identity", time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC), " OpenAI ", " GPT-5 ", RequestTokenStats{InputTokens: 1, TotalTokens: 1})
	if got := BuildPriceKey(detail.Provider, detail.Model); got != "openai:gpt-5" {
		t.Fatalf("price key = %q, want openai:gpt-5", got)
	}
	if got := ProviderRawPresenceV1(" unknown "); got != "indeterminate_legacy" {
		t.Fatalf("provider presence = %q", got)
	}
	seedA := CanonicalIdentitySeedV1(detail, 0)
	seedB := CanonicalIdentitySeedV1(detail, 0)
	if seedA == "" || seedA != seedB || CanonicalIdentitySeedV1(detail, 1) == seedA {
		t.Fatalf("identity seed is not deterministic/ordinal-scoped: %q", seedA)
	}
	if CanonicalEventIdentityV1(seedA) == CanonicalEventIdentityV1(seedB) &&
		StableEventIDV1(CanonicalEventIdentityV1(seedA)) == "" {
		t.Fatal("stable event id must be derived from canonical identity")
	}
	if SourceGroupKeyV1(detail) != SourceGroupKeyV1(detail) {
		t.Fatal("source group key is not stable")
	}
	if !strings.Contains(pricingGroupKeyV1("model", "gpt-5:mini", "openai:gpt-5", "present"), "%3A") {
		t.Fatal("pricing group key must RFC3986-escape separators")
	}
}

func TestCanonicalSortKeyUsesStableBytes(t *testing.T) {
	detail := projectionTestDetail("req-sort", time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC), "openai", "gpt-5", RequestTokenStats{InputTokens: 1, TotalTokens: 1})
	left := CanonicalSortKeyV1("POST /v1/responses", detail, 0)
	right := CanonicalSortKeyV1("POST /v1/responses", detail, 0)
	if string(left) != string(right) || len(left) == 0 {
		t.Fatal("canonical sort key is not deterministic")
	}
	if string(left) == string(CanonicalSortKeyV1("POST /v1/responses", detail, 1)) {
		t.Fatal("source payload ordinal must be the final tie breaker")
	}
}

func TestCanonicalIdentityAndSortPreserveZeroTimestamp(t *testing.T) {
	detail := RequestDetail{RequestID: "legacy-zero", Provider: "openai", Model: "gpt-5", Endpoint: "POST /v1/responses"}
	seed := CanonicalIdentitySeedV1(detail, 0)
	if seed == "" || seed != CanonicalIdentitySeedV1(detail, 0) {
		t.Fatalf("zero timestamp seed is not stable: %q", seed)
	}
	key := CanonicalSortKeyV1(detail.Endpoint, detail, 0)
	if len(key) == 0 || string(key) != string(CanonicalSortKeyV1(detail.Endpoint, detail, 0)) {
		t.Fatal("zero timestamp sort key is not stable")
	}
}

func TestBillableComponentsV1CoversSplitCacheAndSeparateReasoning(t *testing.T) {
	unsplit := projectionTestDetail("req-unsplit", time.Now(), "openai", "gpt-5", RequestTokenStats{
		InputTokens: 100, CachedTokens: 30, OutputTokens: 20, TotalTokens: 120,
	})
	components := GetBillableTokenComponents(unsplit)
	if components.InputTokens != 70 || components.CacheReadTokens != 30 || components.OutputTokens != 20 {
		t.Fatalf("unsplit components = %+v", components)
	}

	split := projectionTestDetail("req-split", time.Now(), "openai", "gpt-5", RequestTokenStats{
		InputTokens: 100, CachedTokens: 50, CacheReadTokens: 30, OutputTokens: 20, TotalTokens: 120,
	})
	components = GetBillableTokenComponents(split)
	if components.CacheReadTokens != 30 || components.CacheCreationTokens != 0 || components.UnclassifiedCacheTokens != 20 {
		t.Fatalf("split components = %+v", components)
	}

	reasoning := projectionTestDetail("req-reasoning", time.Now(), "gemini", "gemini-2", RequestTokenStats{
		InputTokens: 10, OutputTokens: 20, ReasoningTokens: 5, TotalTokens: 35,
	})
	components = GetBillableTokenComponents(reasoning)
	if components.OutputTokens != 20 || components.ReasoningTokens != 5 {
		t.Fatalf("separate reasoning components = %+v", components)
	}
}

func TestProjectionEnrichmentRevokesOldContribution(t *testing.T) {
	projection := NewUsageProjection()
	timestamp := time.Date(2026, 8, 1, 12, 30, 0, 0, time.UTC)
	first := projectionTestDetail("req-enrich", timestamp, "openai", "gpt-5", RequestTokenStats{InputTokens: 10, OutputTokens: 5, TotalTokens: 15})
	second := first
	second.Tokens = RequestTokenStats{InputTokens: 20, OutputTokens: 10, TotalTokens: 30}
	added := projection.ApplyDetail("POST /v1/responses", first)
	enriched := projection.ApplyDetail("POST /v1/responses", second)
	if !added.Added || !enriched.Enriched || enriched.StableEventID != added.StableEventID || enriched.Version != 2 {
		t.Fatalf("mutation results = added=%+v enriched=%+v", added, enriched)
	}
	snapshot := projection.Snapshot()
	if snapshot.Totals.TotalRequests != 1 || snapshot.Totals.Tokens.TotalTokens != 30 || snapshot.Revision != 2 || snapshot.RewriteRevision != 1 {
		t.Fatalf("projection totals = %+v revision=%d rewrite=%d", snapshot.Totals, snapshot.Revision, snapshot.RewriteRevision)
	}
	if len(snapshot.Events) != 1 || len(snapshot.Postings["all"]) != 1 || snapshot.Events[0].Version != 2 {
		t.Fatalf("events/postings = %d/%d event=%+v", len(snapshot.Events), len(snapshot.Postings["all"]), snapshot.Events)
	}
}

func TestProjectionCompactStorageMaterializesPublicAggregates(t *testing.T) {
	projection := NewUsageProjection()
	timestamp := time.Date(2026, 8, 1, 12, 30, 0, 0, time.UTC)
	openAI := projectionTestDetail("req-materialize-openai", timestamp, "openai", "gpt-5", RequestTokenStats{
		InputTokens: 10, OutputTokens: 5, TotalTokens: 15,
	})
	openAI.AuthIndex = "auth-openai"
	openAI.Source = "auth-openai"
	gemini := projectionTestDetail("req-materialize-gemini", timestamp.Add(time.Minute), "gemini", "gemini-2", RequestTokenStats{
		InputTokens: 8, OutputTokens: 4, ReasoningTokens: 2, TotalTokens: 14,
	})
	gemini.AuthIndex = "auth-gemini"
	gemini.Source = "auth-gemini"

	projection.ApplyDetail("POST /v1/responses", openAI)
	projection.ApplyDetail("POST /v1/responses", gemini)
	snapshot := projection.Snapshot()
	if snapshot.Totals.TotalRequests != 2 || snapshot.Totals.Tokens.TotalTokens != 29 {
		t.Fatalf("materialized totals = %+v", snapshot.Totals)
	}
	if snapshot.Totals.Facets["model:gpt-5"] != 1 || snapshot.Totals.Facets["auth:auth-gemini"] != 1 {
		t.Fatalf("materialized facets = %#v", snapshot.Totals.Facets)
	}
	openAIGroupKey := pricingGroupKeyV1("global", "", BuildPriceKey(openAI.Provider, openAI.Model), ProviderRawPresenceV1(openAI.Provider))
	openAIPricing, ok := snapshot.Totals.PricingGroups[openAIGroupKey]
	if !ok || openAIPricing.BillableTokens.InputTokens != 10 || openAIPricing.BillableTokens.OutputTokens != 5 {
		t.Fatalf("materialized pricing = %+v, exists=%v", openAIPricing, ok)
	}
	openAIMask := ComponentInput | ComponentOutput
	if openAIPricing.ComponentMaskCounts[openAIMask] != 1 {
		t.Fatalf("materialized component masks = %#v", openAIPricing.ComponentMaskCounts)
	}
	hour := snapshot.Hours[bucketKey(hourStart(timestamp))]
	if hour.Models["gpt-5"].TotalRequests != 1 || hour.Auths["auth-gemini"].TotalRequests != 1 {
		t.Fatalf("materialized dimensions = models:%#v auths:%#v", hour.Models, hour.Auths)
	}
	if len(hour.Models["gpt-5"].Facets) != 0 || len(hour.Auths["auth-gemini"].Facets) != 0 {
		t.Fatalf("dimension aggregates duplicated global facets: models=%#v auths=%#v", hour.Models["gpt-5"].Facets, hour.Auths["auth-gemini"].Facets)
	}
	if len(snapshot.Postings["auth:auth-openai"]) != 1 || len(snapshot.Postings["source:"+UsageSourceIDV1(gemini)]) != 1 {
		t.Fatalf("materialized postings = %#v", snapshot.Postings)
	}

	openAI.Tokens = normaliseRequestTokens(RequestTokenStats{InputTokens: 20, OutputTokens: 10, TotalTokens: 30}, openAI.Provider)
	result := projection.ApplyDetail("POST /v1/responses", openAI)
	if !result.Enriched {
		t.Fatalf("enrichment result = %+v", result)
	}
	snapshot = projection.Snapshot()
	openAIPricing = snapshot.Totals.PricingGroups[openAIGroupKey]
	if snapshot.Totals.TotalRequests != 2 || snapshot.Totals.Tokens.TotalTokens != 44 ||
		openAIPricing.BillableTokens.InputTokens != 20 || openAIPricing.ComponentMaskCounts[openAIMask] != 1 {
		t.Fatalf("materialized enrichment totals=%+v pricing=%+v", snapshot.Totals, openAIPricing)
	}
}

func TestCompactComponentMaskCountsSupportsMultipleMasks(t *testing.T) {
	var counts compactComponentMaskCounts
	first := ComponentInput | ComponentOutput
	second := ComponentInput | ComponentCacheRead
	third := ComponentOutput | ComponentReasoning
	counts.add(first, 2)
	counts.add(second, 3)
	counts.add(third, 4)
	counts.add(first, -2)
	counts.add(second, -1)
	materialized := counts.materialize()
	if len(materialized) != 2 || materialized[second] != 2 || materialized[third] != 4 {
		t.Fatalf("component mask counts = %#v, want second=2 third=4", materialized)
	}
	clone := counts.clone()
	clone.add(second, -2)
	if clone.materialize()[second] != 0 || counts.materialize()[second] != 2 {
		t.Fatalf("component mask clone shared storage: original=%#v clone=%#v", counts.materialize(), clone.materialize())
	}
}

func TestProjectionKeepsDistinctEventsOnPhysicalIdentityCollision(t *testing.T) {
	projection := NewUsageProjection()
	timestamp := time.Date(2026, 8, 1, 12, 30, 0, 0, time.UTC)
	first := projectionTestDetail("req-collision-a", timestamp, "openai", "gpt-5", RequestTokenStats{InputTokens: 10, TotalTokens: 10})
	second := projectionTestDetail("req-collision-b", timestamp, "openai", "gpt-5", RequestTokenStats{InputTokens: 20, TotalTokens: 20})
	forcedID := "event:forced-physical-id"
	firstResult := projection.ApplyDetailWithIdentity("POST /v1/responses", first, ProjectionIdentity{
		CanonicalIdentitySeed:  "seed-a",
		CanonicalEventIdentity: "canonical-a",
		StableEventID:          forcedID,
		Sequence:               1,
	})
	secondResult := projection.ApplyDetailWithIdentity("POST /v1/responses", second, ProjectionIdentity{
		CanonicalIdentitySeed:  "seed-b",
		CanonicalEventIdentity: "canonical-b",
		StableEventID:          forcedID,
		Sequence:               2,
	})
	if !firstResult.Added || !secondResult.Added || secondResult.Enriched || secondResult.StableEventID == forcedID {
		t.Fatalf("physical collision was merged or overwrote an event: first=%+v second=%+v", firstResult, secondResult)
	}
	if secondResult.RewriteRevision != 1 || projection.Snapshot().RewriteRevision != 1 {
		t.Fatalf("collision rewrite revision = result:%d snapshot:%d, want 1", secondResult.RewriteRevision, projection.Snapshot().RewriteRevision)
	}
	snapshot := projection.Snapshot()
	if snapshot.Totals.TotalRequests != 2 || snapshot.Totals.Tokens.TotalTokens != 30 || len(snapshot.Events) != 2 || len(snapshot.Postings["all"]) != 2 {
		t.Fatalf("collision projection lost an event: totals=%+v events=%d postings=%d", snapshot.Totals, len(snapshot.Events), len(snapshot.Postings["all"]))
	}
}

func TestProjectionRemoveEventClearsIdentityLookups(t *testing.T) {
	projection := NewUsageProjection()
	detail := projectionTestDetail("req-remove", time.Date(2026, 8, 1, 12, 30, 0, 0, time.UTC), "openai", "gpt-5", RequestTokenStats{InputTokens: 1, TotalTokens: 1})
	first := projection.ApplyDetail("POST /v1/responses", detail)
	if !projection.RemoveEvent(first.StableEventID) {
		t.Fatal("RemoveEvent returned false for existing event")
	}
	second := projection.ApplyDetail("POST /v1/responses", detail)
	if !second.Added || second.StableEventID == "" {
		t.Fatalf("re-added event = %+v, want a fresh event after lookup cleanup", second)
	}
	if snapshot := projection.Snapshot(); snapshot.Totals.TotalRequests != 1 || len(snapshot.Events) != 1 {
		t.Fatalf("re-added projection = totals:%+v events:%d", snapshot.Totals, len(snapshot.Events))
	}
}

func TestProjectionDoesNotMergeSameRequestCoordinatesAcrossEndpoints(t *testing.T) {
	projection := NewUsageProjection()
	timestamp := time.Date(2026, 8, 1, 12, 30, 0, 0, time.UTC)
	first := projectionTestDetail("req-shared", timestamp, "openai", "gpt-5", RequestTokenStats{InputTokens: 10, TotalTokens: 10})
	second := first
	second.Endpoint = "POST /v1/chat/completions"
	firstResult := projection.ApplyDetail(first.Endpoint, first)
	secondResult := projection.ApplyDetail(second.Endpoint, second)
	if !firstResult.Added || !secondResult.Added || secondResult.Enriched {
		t.Fatalf("same request id across endpoints was merged: first=%+v second=%+v", firstResult, secondResult)
	}
	if snapshot := projection.Snapshot(); snapshot.Totals.TotalRequests != 2 || len(snapshot.Events) != 2 {
		t.Fatalf("projection merged distinct endpoint events: totals=%+v events=%d", snapshot.Totals, len(snapshot.Events))
	}
}

func TestProjectionIndexDigestsPreserveLegacyKeyEquivalence(t *testing.T) {
	timestamp := time.Date(2026, 8, 1, 12, 0, 0, 123, time.UTC)
	base := projectionTestDetail("req-index", timestamp, "openai", "gpt-5", RequestTokenStats{InputTokens: 1, TotalTokens: 1})
	cases := []struct {
		apiName string
		detail  RequestDetail
	}{
		{apiName: "POST /v1/responses", detail: base},
		{apiName: "POST /v1/responses", detail: base},
		{apiName: "redacted:0123456789abcdef", detail: base},
		{apiName: "POST /v1/responses", detail: func() RequestDetail {
			detail := base
			detail.Provider = "azure-openai"
			detail.Model = "gpt-5-deployed"
			return detail
		}()},
		{apiName: "POST /v1/responses", detail: func() RequestDetail {
			detail := base
			detail.AuthIndex = "other-auth"
			detail.Source = "other-auth"
			return detail
		}()},
		{apiName: "POST /v1/responses", detail: func() RequestDetail {
			detail := base
			detail.RequestID = ""
			detail.ClientIP = "192.0.2.1"
			return detail
		}()},
		{apiName: "POST /v1/responses", detail: func() RequestDetail {
			detail := base
			detail.RequestID = ""
			detail.ClientIP = "192.0.2.2"
			return detail
		}()},
	}
	for leftIndex, left := range cases {
		for rightIndex, right := range cases {
			oldCoordinateEqual := canonicalEventCoordinateKey(left.apiName, left.detail) == canonicalEventCoordinateKey(right.apiName, right.detail)
			newCoordinateEqual := coordinateIndexKey(left.apiName, left.detail) == coordinateIndexKey(right.apiName, right.detail)
			if oldCoordinateEqual != newCoordinateEqual {
				t.Fatalf("coordinate equivalence drifted for cases %d/%d: old=%t new=%t", leftIndex, rightIndex, oldCoordinateEqual, newCoordinateEqual)
			}
			oldRequestEqual := requestLookupKey(left.apiName, left.detail) == requestLookupKey(right.apiName, right.detail)
			newRequestEqual := requestLookupIndexKey(left.apiName, left.detail) == requestLookupIndexKey(right.apiName, right.detail)
			if oldRequestEqual != newRequestEqual {
				t.Fatalf("request lookup equivalence drifted for cases %d/%d: old=%t new=%t", leftIndex, rightIndex, oldRequestEqual, newRequestEqual)
			}
		}
	}
}

func TestProjectionRollupsUseUTCHalfOpenBoundaries(t *testing.T) {
	projection := NewUsageProjection()
	first := projectionTestDetail("req-boundary-1", time.Date(2026, 8, 2, 23, 59, 59, 0, time.UTC), "openai", "gpt-5", RequestTokenStats{InputTokens: 1, TotalTokens: 1})
	second := projectionTestDetail("req-boundary-2", time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC), "openai", "gpt-5", RequestTokenStats{InputTokens: 2, TotalTokens: 2})
	projection.ApplyDetail(first.Endpoint, first)
	projection.ApplyDetail(second.Endpoint, second)
	snapshot := projection.Snapshot()
	if len(snapshot.Hours) != 2 || len(snapshot.Days) != 2 || len(snapshot.Weeks) != 2 || len(snapshot.Months) != 1 || len(snapshot.Years) != 1 {
		t.Fatalf("bucket counts hours=%d days=%d weeks=%d months=%d years=%d", len(snapshot.Hours), len(snapshot.Days), len(snapshot.Weeks), len(snapshot.Months), len(snapshot.Years))
	}
	if snapshot.Totals.TotalRequests != 2 || snapshot.Totals.Tokens.TotalTokens != 3 {
		t.Fatalf("totals = %+v", snapshot.Totals)
	}
}

func TestRequestStatisticsProjectionDoesNotChangeLegacyDetailJSON(t *testing.T) {
	stats := NewRequestStatistics()
	stats.Record(context.Background(), coreusage.Record{
		Provider:    "openai",
		Model:       "gpt-5",
		AuthIndex:   "auth-projection-json",
		RequestedAt: time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC),
		Detail:      coreusage.Detail{InputTokens: 10, TotalTokens: 10},
	})
	detail := stats.Snapshot().APIs["openai"].Models["gpt-5"].Details[0]
	data, err := json.Marshal(detail)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "stable_event_id") || strings.Contains(string(data), "schema_version") {
		t.Fatalf("projection fields leaked into legacy detail JSON: %s", data)
	}
	projection := stats.ProjectionSnapshot()
	if projection.Totals.TotalRequests != 1 || len(projection.Events) != 1 {
		t.Fatalf("projection snapshot = %+v", projection.Totals)
	}
}
