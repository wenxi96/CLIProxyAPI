package usage

import "time"

const (
	projectionTimeIndexRowBudgetBytes    = uint64(8)
	projectionTimeIndexBucketBudgetBytes = uint64(96)
)

// projectionTimeIndexV1 maps UTC hour segments to compact fact row ids. It is
// generation-local and rebuilt from facts after direct hydrate, so it never
// changes the persisted projection wire format.
type projectionTimeIndexV1 struct {
	hourly   map[int64][]projectionFactRowID
	zeroRows []projectionFactRowID
}

type projectionTimeRange struct {
	from time.Time
	to   time.Time
}

func newProjectionTimeIndexV1() projectionTimeIndexV1 {
	return projectionTimeIndexV1{hourly: make(map[int64][]projectionFactRowID)}
}

func (index *projectionTimeIndexV1) append(timestamp time.Time, rowID projectionFactRowID) {
	if index == nil || rowID == 0 {
		return
	}
	if timestamp.IsZero() {
		index.zeroRows = append(index.zeroRows, rowID)
		return
	}
	if index.hourly == nil {
		index.hourly = make(map[int64][]projectionFactRowID)
	}
	key := hourStart(timestamp).Unix()
	index.hourly[key] = append(index.hourly[key], rowID)
}

func (index projectionTimeIndexV1) clone() projectionTimeIndexV1 {
	result := newProjectionTimeIndexV1()
	result.zeroRows = append(result.zeroRows, index.zeroRows...)
	for key, rows := range index.hourly {
		result.hourly[key] = append([]projectionFactRowID(nil), rows...)
	}
	return result
}

func (index projectionTimeIndexV1) estimatedBytes() uint64 {
	bytes := saturatingMulUint64(uint64(len(index.zeroRows)), projectionTimeIndexRowBudgetBytes)
	for _, rows := range index.hourly {
		bytes = saturatingAddUint64(bytes, projectionTimeIndexBucketBudgetBytes)
		bytes = saturatingAddUint64(bytes, saturatingMulUint64(uint64(len(rows)), projectionTimeIndexRowBudgetBytes))
	}
	return bytes
}

func (index projectionTimeIndexV1) prospectiveBytes(timestamp time.Time) uint64 {
	bytes := projectionTimeIndexRowBudgetBytes
	if timestamp.IsZero() {
		return bytes
	}
	if _, exists := index.hourly[hourStart(timestamp).Unix()]; !exists {
		bytes = saturatingAddUint64(bytes, projectionTimeIndexBucketBudgetBytes)
	}
	return bytes
}

func (projection *UsageProjection) rebuildTimeIndexLocked() {
	if projection == nil {
		return
	}
	index := newProjectionTimeIndexV1()
	for row := range projection.facts.timestampSeconds {
		rowID := projectionFactRowID(row + 1)
		timestamp, ok := projection.facts.timestamp(rowID)
		if !ok {
			continue
		}
		index.append(timestamp, rowID)
	}
	projection.timeIndex = index
}

func (projection *UsageProjection) selectTimeRangeCandidateRowsLocked(ranges ...projectionTimeRange) []projectionFactRowID {
	if projection == nil || len(ranges) == 0 {
		return nil
	}
	rows := make([]projectionFactRowID, 0)
	for hour, candidates := range projection.timeIndex.hourly {
		start := time.Unix(hour, 0).UTC()
		end := start.Add(time.Hour)
		if !projectionTimeRangesMayIntersectHour(ranges, start, end) {
			continue
		}
		for _, rowID := range candidates {
			timestamp, ok := projection.facts.timestamp(rowID)
			if ok && projectionTimeRangesMatch(ranges, timestamp) {
				rows = append(rows, rowID)
			}
		}
	}
	for _, rowID := range projection.timeIndex.zeroRows {
		timestamp, ok := projection.facts.timestamp(rowID)
		if ok && projectionTimeRangesMatch(ranges, timestamp) {
			rows = append(rows, rowID)
		}
	}
	return rows
}

func projectionTimeRangesMayIntersectHour(ranges []projectionTimeRange, start, end time.Time) bool {
	for _, interval := range ranges {
		if !interval.from.IsZero() && !end.After(interval.from) {
			continue
		}
		if !interval.to.IsZero() && !start.Before(interval.to) {
			continue
		}
		return true
	}
	return false
}

func projectionTimeRangesMatch(ranges []projectionTimeRange, timestamp time.Time) bool {
	for _, interval := range ranges {
		if projectionPricingTimestampMatches(timestamp, interval.from, interval.to) {
			return true
		}
	}
	return false
}
