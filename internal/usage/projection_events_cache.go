package usage

import "sync"

const (
	projectionEventPageCacheMaxEntries = 8
	projectionEventPageCacheMaxBytes   = uint64(16 << 20)
	projectionEventPageCacheRowBytes   = uint64(4)
)

// projectionEventPageCache keeps immutable, query-local fact row order for
// cursor continuation. It is bounded and never owns canonical request data.
type projectionEventPageCache struct {
	mu      sync.Mutex
	entries map[string]projectionEventPageCacheEntry
	order   []string
	bytes   uint64
}

type projectionEventPageCacheEntry struct {
	rows          []projectionFactRowID
	postingID     string
	candidateRows uint64
	scratchBytes  uint64
	bytes         uint64
}

func (cache *projectionEventPageCache) get(key string) (projectionEventPageCacheEntry, bool) {
	if cache == nil || key == "" {
		return projectionEventPageCacheEntry{}, false
	}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	entry, ok := cache.entries[key]
	if !ok {
		return projectionEventPageCacheEntry{}, false
	}
	cache.touchLocked(key)
	return entry, true
}

func (cache *projectionEventPageCache) put(key string, rows []projectionFactRowID, postingID string, candidateRows, scratchBytes uint64) {
	if cache == nil || key == "" || len(rows) == 0 || postingID == "" {
		return
	}
	bytes := saturatingMulUint64(uint64(len(rows)), projectionEventPageCacheRowBytes)
	if bytes > projectionEventPageCacheMaxBytes {
		return
	}
	entry := projectionEventPageCacheEntry{
		rows:          append([]projectionFactRowID(nil), rows...),
		postingID:     postingID,
		candidateRows: candidateRows,
		scratchBytes:  scratchBytes,
		bytes:         bytes,
	}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if cache.entries == nil {
		cache.entries = make(map[string]projectionEventPageCacheEntry)
	}
	if existing, ok := cache.entries[key]; ok {
		cache.bytes -= existing.bytes
		cache.removeLocked(key)
	}
	for len(cache.order) >= projectionEventPageCacheMaxEntries ||
		(cache.bytes > 0 && saturatingAddUint64(cache.bytes, bytes) > projectionEventPageCacheMaxBytes) {
		if len(cache.order) == 0 {
			break
		}
		cache.evictOldestLocked()
	}
	cache.entries[key] = entry
	cache.order = append(cache.order, key)
	cache.bytes = saturatingAddUint64(cache.bytes, bytes)
}

func (cache *projectionEventPageCache) touchLocked(key string) {
	for index, candidate := range cache.order {
		if candidate != key {
			continue
		}
		copy(cache.order[index:], cache.order[index+1:])
		cache.order[len(cache.order)-1] = key
		return
	}
}

func (cache *projectionEventPageCache) removeLocked(key string) {
	delete(cache.entries, key)
	for index, candidate := range cache.order {
		if candidate != key {
			continue
		}
		cache.order = append(cache.order[:index], cache.order[index+1:]...)
		return
	}
}

func (cache *projectionEventPageCache) evictOldestLocked() {
	if len(cache.order) == 0 {
		return
	}
	key := cache.order[0]
	cache.order = cache.order[1:]
	if entry, ok := cache.entries[key]; ok {
		if entry.bytes >= cache.bytes {
			cache.bytes = 0
		} else {
			cache.bytes -= entry.bytes
		}
		delete(cache.entries, key)
	}
}
