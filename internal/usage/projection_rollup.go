package usage

import (
	"fmt"
	"strings"
	"time"
)

const healthBucketDuration = 15 * time.Minute

func hourStart(timestamp time.Time) time.Time {
	timestamp = timestamp.UTC()
	return time.Date(timestamp.Year(), timestamp.Month(), timestamp.Day(), timestamp.Hour(), 0, 0, 0, time.UTC)
}

func dayStart(timestamp time.Time) time.Time {
	timestamp = timestamp.UTC()
	return time.Date(timestamp.Year(), timestamp.Month(), timestamp.Day(), 0, 0, 0, 0, time.UTC)
}

func weekStart(timestamp time.Time) time.Time {
	start := dayStart(timestamp)
	weekday := int(start.Weekday())
	if weekday == 0 {
		weekday = 7
	}
	return start.AddDate(0, 0, -(weekday - 1))
}

func monthStart(timestamp time.Time) time.Time {
	timestamp = timestamp.UTC()
	return time.Date(timestamp.Year(), timestamp.Month(), 1, 0, 0, 0, 0, time.UTC)
}

func yearStart(timestamp time.Time) time.Time {
	timestamp = timestamp.UTC()
	return time.Date(timestamp.Year(), time.January, 1, 0, 0, 0, 0, time.UTC)
}

func bucketKey(timestamp time.Time) string {
	return timestamp.UTC().Format(time.RFC3339Nano)
}

func newCompactHourBucket(start, end time.Time) compactHourBucket {
	return compactHourBucket{
		IntervalStart: start.UTC(),
		IntervalEnd:   end.UTC(),
	}
}

func (bucket *compactHourBucket) applyContribution(contribution projectionContribution, delta int64) {
	if bucket == nil || delta == 0 {
		return
	}
	applyAggregateDelta(&bucket.Totals, contribution, delta)
	applyFacetDelta(&bucket.Facets, contribution, delta)
	applyDimensionDelta(&bucket.APIs, contribution.APIID, contribution, delta)
	applyDimensionDelta(&bucket.Models, contribution.ModelID, contribution, delta)
	applyDimensionDelta(&bucket.Providers, contribution.ProviderID, contribution, delta)
	applyDimensionDelta(&bucket.Auths, contribution.AuthID, contribution, delta)
	applyDimensionDelta(&bucket.Sources, contribution.SourceIDValue, contribution, delta)
}

func applyDimensionDelta(target *map[projectionStringID]compactAggregate, key projectionStringID, contribution projectionContribution, delta int64) {
	if target == nil || key == 0 {
		return
	}
	if *target == nil {
		*target = make(map[projectionStringID]compactAggregate)
	}
	aggregate := (*target)[key]
	applyAggregateDelta(&aggregate, contribution, delta)
	if compactAggregateIsZero(aggregate) {
		delete(*target, key)
		return
	}
	(*target)[key] = aggregate
}

func applyAggregateDelta(aggregate *compactAggregate, contribution projectionContribution, delta int64) {
	if aggregate == nil || delta == 0 {
		return
	}
	aggregate.counters.add(compactAggregateTotalRequests, delta)
	if contribution.Failed {
		aggregate.counters.add(compactAggregateFailureCount, delta)
	} else {
		aggregate.counters.add(compactAggregateSuccessCount, delta)
	}
	aggregate.counters.add(compactAggregateLatencyMsSum, contribution.LatencyMs*delta)
	aggregate.counters.add(compactAggregateInputTokens, contribution.Tokens.InputTokens*delta)
	aggregate.counters.add(compactAggregateOutputTokens, contribution.Tokens.OutputTokens*delta)
	aggregate.counters.add(compactAggregateReasoningTokens, contribution.Tokens.ReasoningTokens*delta)
	aggregate.counters.add(compactAggregateCachedTokens, contribution.Tokens.CachedTokens*delta)
	aggregate.counters.add(compactAggregateCacheReadTokens, contribution.Tokens.CacheReadTokens*delta)
	aggregate.counters.add(compactAggregateCacheCreationTokens, contribution.Tokens.CacheCreationTokens*delta)
	aggregate.counters.add(compactAggregateTotalTokens, contribution.Tokens.TotalTokens*delta)
	aggregate.counters.add(compactAggregateDetailCount, delta)
	if contribution.Tokens.TokenUsageSource == TokenUsageSourceMissing {
		aggregate.counters.add(compactAggregateMissingUsageCount, delta)
	} else {
		aggregate.counters.add(compactAggregateKnownUsageCount, delta)
		if contribution.Tokens.TokenUsageSource == TokenUsageSourceProvider {
			aggregate.counters.add(compactAggregateProviderUsageCount, delta)
		} else {
			aggregate.counters.add(compactAggregateComputedUsageCount, delta)
		}
	}
}

func applyFacetDelta(target *map[projectionStringID]int64, contribution projectionContribution, delta int64) {
	if target == nil || delta == 0 {
		return
	}
	if *target == nil {
		if delta < 0 {
			return
		}
		*target = make(map[projectionStringID]int64)
	}
	for _, facet := range contribution.FacetIDs {
		(*target)[facet] += delta
		if (*target)[facet] == 0 {
			delete(*target, facet)
		}
	}
}

func updateBucketMap(target map[string]compactHourBucket, start, end time.Time, contribution projectionContribution, delta int64) int64 {
	if target == nil {
		return 0
	}
	key := bucketKey(start)
	bucket, ok := target[key]
	beforeCells := compactHourBucketCellCount(bucket, ok)
	if !ok {
		bucket = newCompactHourBucket(start, end)
	}
	bucket.applyContribution(contribution, delta)
	if compactHourBucketIsZero(bucket) {
		delete(target, key)
		return -beforeCells
	}
	target[key] = bucket
	return compactHourBucketCellCount(bucket, true) - beforeCells
}

func compactHourBucketCellCount(bucket compactHourBucket, exists bool) int64 {
	if !exists {
		return 0
	}
	return int64(1 + len(bucket.APIs) + len(bucket.Models) + len(bucket.Providers) + len(bucket.Auths) + len(bucket.Sources))
}

func updateHealthBucket(target map[string]HealthBucket, timestamp time.Time, contribution projectionContribution, delta int64) {
	if target == nil {
		return
	}
	timestamp = timestamp.UTC()
	minute := (timestamp.Minute() / 15) * 15
	start := time.Date(timestamp.Year(), timestamp.Month(), timestamp.Day(), timestamp.Hour(), minute, 0, 0, time.UTC)
	key := bucketKey(start)
	bucket := target[key]
	if bucket.IntervalStart.IsZero() {
		bucket.IntervalStart = start
		bucket.IntervalEnd = start.Add(healthBucketDuration)
	}
	bucket.TotalRequests += delta
	if contribution.Failed {
		bucket.FailureCount += delta
	} else {
		bucket.SuccessCount += delta
	}
	bucket.LatencyMsSum += contribution.LatencyMs * delta
	if bucket.TotalRequests == 0 {
		delete(target, key)
	} else {
		target[key] = bucket
	}
}

func pricingGroupKeyV1(dimensionType, dimensionID, priceKey, providerPresence string) string {
	return strings.TrimSpace(dimensionType) + ":" + percentEncodeRFC3986(dimensionID) + ":" +
		percentEncodeRFC3986(priceKey) + ":" + strings.TrimSpace(providerPresence)
}

func percentEncodeRFC3986(value string) string {
	const hexDigits = "0123456789ABCDEF"
	bytes := []byte(value)
	encoded := make([]byte, 0, len(bytes))
	for _, char := range bytes {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') || char == '-' || char == '.' || char == '_' || char == '~' {
			encoded = append(encoded, char)
			continue
		}
		encoded = append(encoded, '%', hexDigits[char>>4], hexDigits[char&0x0f])
	}
	return string(encoded)
}

func compactAggregateIsZero(aggregate compactAggregate) bool {
	return aggregate.counters.value(compactAggregateTotalRequests) == 0
}

func compactPricingIsZero(pricing compactPricingAggregate) bool {
	for index := 0; index < compactPricingCounterCount; index++ {
		if pricing.counters.value(index) != 0 {
			return false
		}
	}
	return true
}

func compactHourBucketIsZero(bucket compactHourBucket) bool {
	return bucket.Totals.counters.value(compactAggregateTotalRequests) == 0 && len(bucket.Facets) == 0 && len(bucket.APIs) == 0 && len(bucket.Models) == 0 &&
		len(bucket.Providers) == 0 && len(bucket.Auths) == 0 && len(bucket.Sources) == 0
}

func rollupWindow(start time.Time, kind string) (time.Time, time.Time) {
	start = start.UTC()
	switch kind {
	case "hour":
		start = hourStart(start)
		return start, start.Add(time.Hour)
	case "day":
		start = dayStart(start)
		return start, start.AddDate(0, 0, 1)
	case "week":
		start = weekStart(start)
		return start, start.AddDate(0, 0, 7)
	case "month":
		start = monthStart(start)
		return start, start.AddDate(0, 1, 0)
	case "year":
		start = yearStart(start)
		return start, start.AddDate(1, 0, 0)
	default:
		return start, start
	}
}

func formatBucketLabel(start time.Time, kind string) string {
	start = start.UTC()
	switch kind {
	case "hour":
		return start.Format(time.RFC3339Nano)
	case "day":
		return start.Format("2006-01-02")
	case "week":
		year, week := start.ISOWeek()
		return fmt.Sprintf("%04d-W%02d", year, week)
	case "month":
		return start.Format("2006-01")
	case "year":
		return start.Format("2006")
	default:
		return start.Format(time.RFC3339Nano)
	}
}
