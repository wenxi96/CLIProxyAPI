package usage

import (
	"fmt"
	"testing"
	"time"
)

func compactPricingAggregateForTest(mask ComponentMask) compactPricingAggregate {
	var counts compactComponentMaskCounts
	counts.add(mask, 1)
	pricing := compactPricingAggregate{ComponentMaskCounts: counts}
	pricing.counters.add(compactPricingInputTokens, 1)
	pricing.counters.add(compactPricingPriceableDetailCount, 1)
	return pricing
}

func TestCompactPricingGroupsUsesInlineStorageBeforePromotion(t *testing.T) {
	var groups compactPricingGroups
	for id := projectionPricingGroupID(1); id <= 5; id++ {
		groups.set(id, compactPricingAggregateForTest(ComponentInput))
	}

	if got := groups.len(); got != 5 {
		t.Fatalf("inline group count = %d, want 5", got)
	}
	if groups.overflow == nil || groups.overflow.additionalByID != nil || len(groups.overflow.additional) != 4 {
		t.Fatalf("groups promoted too early: additional=%d map=%v", len(groups.overflow.additional), groups.overflow.additionalByID != nil)
	}

	for id := projectionPricingGroupID(6); id <= compactPricingGroupInlineLimit+1; id++ {
		groups.set(id, compactPricingAggregateForTest(ComponentOutput))
	}
	if got := groups.len(); got != compactPricingGroupInlineLimit+1 {
		t.Fatalf("slice group count = %d, want %d", got, compactPricingGroupInlineLimit+1)
	}
	if groups.overflow == nil || groups.overflow.additionalByID != nil || len(groups.overflow.additional) != compactPricingGroupInlineLimit {
		t.Fatalf("groups promoted too early at slice threshold")
	}

	groups.set(compactPricingGroupInlineLimit+2, compactPricingAggregateForTest(ComponentOutput))
	if got := groups.len(); got != compactPricingGroupInlineLimit+2 {
		t.Fatalf("promoted group count = %d, want %d", got, compactPricingGroupInlineLimit+2)
	}
	if groups.overflow == nil || groups.overflow.additionalByID == nil || len(groups.overflow.additional) != 0 {
		t.Fatalf("groups did not promote at threshold")
	}
}

func TestCompactPricingGroupsDeleteCompactsPromotedMap(t *testing.T) {
	var groups compactPricingGroups
	for id := projectionPricingGroupID(1); id <= compactPricingGroupInlineLimit+2; id++ {
		groups.set(id, compactPricingAggregateForTest(ComponentInput))
	}

	groups.delete(2)
	if got := groups.len(); got != compactPricingGroupInlineLimit+1 {
		t.Fatalf("after map deletion group count = %d, want %d", got, compactPricingGroupInlineLimit+1)
	}
	if groups.overflow == nil || groups.overflow.additionalByID != nil || len(groups.overflow.additional) != compactPricingGroupInlineLimit {
		t.Fatalf("promoted map was not compacted")
	}
	if _, ok := groups.get(2); ok {
		t.Fatal("deleted pricing group is still present")
	}

	groups.delete(1)
	if got := groups.len(); got != compactPricingGroupInlineLimit {
		t.Fatalf("after primary deletion group count = %d, want %d", got, compactPricingGroupInlineLimit)
	}
	if _, ok := groups.get(1); ok {
		t.Fatal("deleted primary pricing group is still present")
	}
}

func TestCompactPricingGroupsCloneDoesNotShareBackingStorage(t *testing.T) {
	var groups compactPricingGroups
	for id := projectionPricingGroupID(1); id <= compactPricingGroupInlineLimit+2; id++ {
		pricing := compactPricingAggregateForTest(ComponentInput)
		pricing.ComponentMaskCounts.add(ComponentOutput, 1)
		groups.set(id, pricing)
	}
	clone := groups.clone()

	pricing, ok := clone.get(1)
	if !ok {
		t.Fatal("cloned primary pricing group is missing")
	}
	pricing.ComponentMaskCounts.add(ComponentCacheRead, 1)
	clone.set(1, pricing)
	original, ok := groups.get(1)
	if !ok {
		t.Fatal("original primary pricing group is missing")
	}
	if _, exists := original.ComponentMaskCounts.materialize()[ComponentCacheRead]; exists {
		t.Fatal("clone mutation changed original component-mask storage")
	}

	clone.delete(6)
	if _, exists := groups.get(6); !exists {
		t.Fatal("clone mutation changed original pricing-group map")
	}
}

func TestCompactCountersPromoteAndDemoteWithoutSharingOverflow(t *testing.T) {
	maxCompact := int64(^uint32(0))
	var aggregate compactAggregateCounters
	aggregate.add(compactAggregateTotalTokens, maxCompact+10)
	if got := aggregate.value(compactAggregateTotalTokens); got != maxCompact+10 {
		t.Fatalf("aggregate overflow value = %d, want %d", got, maxCompact+10)
	}
	aggregateClone := aggregate.clone()
	aggregateClone.add(compactAggregateTotalTokens, -20)
	if got := aggregateClone.value(compactAggregateTotalTokens); got != maxCompact-10 {
		t.Fatalf("aggregate demoted value = %d, want %d", got, maxCompact-10)
	}
	if got := aggregate.value(compactAggregateTotalTokens); got != maxCompact+10 {
		t.Fatalf("aggregate clone changed original overflow = %d", got)
	}

	var pricing compactPricingCounters
	pricing.add(compactPricingInputTokens, maxCompact+20)
	if got := pricing.value(compactPricingInputTokens); got != maxCompact+20 {
		t.Fatalf("pricing overflow value = %d, want %d", got, maxCompact+20)
	}
	pricingClone := pricing.clone()
	pricingClone.add(compactPricingInputTokens, -40)
	if got := pricingClone.value(compactPricingInputTokens); got != maxCompact-20 {
		t.Fatalf("pricing demoted value = %d, want %d", got, maxCompact-20)
	}
	if got := pricing.value(compactPricingInputTokens); got != maxCompact+20 {
		t.Fatalf("pricing clone changed original overflow = %d", got)
	}
}

func TestCompactPricingGroupsMaterializePublicAggregate(t *testing.T) {
	projection := NewUsageProjection()
	timestamp := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	for index := 0; index < 6; index++ {
		provider := fmt.Sprintf("provider-%d", index)
		model := fmt.Sprintf("model-%d", index)
		detail := projectionTestDetail(
			fmt.Sprintf("compact-materialize-%d", index),
			timestamp.Add(time.Duration(index)*time.Minute),
			provider,
			model,
			RequestTokenStats{InputTokens: 1, OutputTokens: 1, TotalTokens: 2},
		)
		result := projection.ApplyDetail("POST /v1/responses", detail)
		if !result.Added {
			t.Fatalf("detail %d was not added: %+v", index, result)
		}
	}

	snapshot := projection.Snapshot()
	if got := len(snapshot.Totals.PricingGroups); got != 6 {
		t.Fatalf("materialized pricing-group count = %d, want 6", got)
	}
	for index := 0; index < 6; index++ {
		provider := fmt.Sprintf("provider-%d", index)
		model := fmt.Sprintf("model-%d", index)
		key := pricingGroupKeyV1("global", "", BuildPriceKey(provider, model), ProviderRawPresenceV1(provider))
		pricing, ok := snapshot.Totals.PricingGroups[key]
		if !ok || pricing.BillableTokens.InputTokens != 1 || pricing.BillableTokens.OutputTokens != 1 {
			t.Fatalf("materialized pricing[%q] = %+v, exists=%v", key, pricing, ok)
		}
	}
}
