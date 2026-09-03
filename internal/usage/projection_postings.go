package usage

type postingRegistryV1 struct {
	rows map[projectionPostingKey][]projectionFactRowID
	refs uint64
}

func newPostingRegistryV1() postingRegistryV1 {
	return postingRegistryV1{rows: make(map[projectionPostingKey][]projectionFactRowID)}
}

func (registry *postingRegistryV1) append(key projectionPostingKey, rowID projectionFactRowID) {
	if registry == nil || rowID == 0 {
		return
	}
	if registry.rows == nil {
		registry.rows = make(map[projectionPostingKey][]projectionFactRowID)
	}
	registry.rows[key] = append(registry.rows[key], rowID)
	registry.refs++
}

func (registry postingRegistryV1) clone() postingRegistryV1 {
	result := newPostingRegistryV1()
	result.refs = registry.refs
	for key, rows := range registry.rows {
		result.rows[key] = append([]projectionFactRowID(nil), rows...)
	}
	return result
}
