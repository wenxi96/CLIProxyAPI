package usage

// requestStatisticsState is an in-memory transaction snapshot for one import
// batch. It intentionally excludes the coordinator and journal, whose terminal
// outcomes are finalized separately by the caller.
type requestStatisticsState struct {
	totalRequests  int64
	successCount   int64
	failureCount   int64
	totalTokens    int64
	changeCount    uint64
	persistedCount uint64

	apis                       map[string]*apiStats
	requestsByDay              map[string]int64
	requestsByHour             map[int]int64
	tokensByDay                map[string]int64
	tokensByHour               map[int]int64
	projection                 *UsageProjection
	projectionUnavailable      bool
	projectionUnavailableCode  string
	identityMetadataReady      bool
	identityMetadataGeneration uint64
}

// adoptStateLocked installs an already detached state without cloning it.
// Bulk restore uses this to keep one private staging copy while chunks are
// admitted and applied; the live RequestStatistics remains untouched until the
// final publication step.
func (s *RequestStatistics) adoptStateLocked(state requestStatisticsState) {
	if s == nil {
		return
	}
	s.totalRequests = state.totalRequests
	s.successCount = state.successCount
	s.failureCount = state.failureCount
	s.totalTokens = state.totalTokens
	s.changeCount = state.changeCount
	s.persistedCount = state.persistedCount
	s.apis = state.apis
	s.requestsByDay = state.requestsByDay
	s.requestsByHour = state.requestsByHour
	s.tokensByDay = state.tokensByDay
	s.tokensByHour = state.tokensByHour
	s.projection = state.projection
	s.projectionUnavailable = state.projectionUnavailable
	s.projectionUnavailableCode = state.projectionUnavailableCode
	s.identityMetadataReady = state.identityMetadataReady
	s.identityMetadataGeneration = state.identityMetadataGeneration
	s.detailLocations = nil
	s.detailEventLocations = nil
	s.ensureDetailLocationsLocked()
}

// detachStateLocked transfers the mutable aggregate state to a caller. The
// caller must hold s.mu. It is intentionally not used for ordinary snapshots;
// its purpose is the one-time publication of a completed bulk candidate.
func (s *RequestStatistics) detachStateLocked() requestStatisticsState {
	if s == nil {
		return requestStatisticsState{}
	}
	state := requestStatisticsState{
		totalRequests:              s.totalRequests,
		successCount:               s.successCount,
		failureCount:               s.failureCount,
		totalTokens:                s.totalTokens,
		changeCount:                s.changeCount,
		persistedCount:             s.persistedCount,
		apis:                       s.apis,
		requestsByDay:              s.requestsByDay,
		requestsByHour:             s.requestsByHour,
		tokensByDay:                s.tokensByDay,
		tokensByHour:               s.tokensByHour,
		projection:                 s.projection,
		projectionUnavailable:      s.projectionUnavailable,
		projectionUnavailableCode:  s.projectionUnavailableCode,
		identityMetadataReady:      s.identityMetadataReady,
		identityMetadataGeneration: s.identityMetadataGeneration,
	}
	s.apis = nil
	s.requestsByDay = nil
	s.requestsByHour = nil
	s.tokensByDay = nil
	s.tokensByHour = nil
	s.projection = nil
	s.detailLocations = nil
	s.detailEventLocations = nil
	return state
}

func (s *RequestStatistics) cloneStateLocked() requestStatisticsState {
	state := requestStatisticsState{
		totalRequests:              s.totalRequests,
		successCount:               s.successCount,
		failureCount:               s.failureCount,
		totalTokens:                s.totalTokens,
		changeCount:                s.changeCount,
		persistedCount:             s.persistedCount,
		apis:                       cloneAPIStats(s.apis),
		requestsByDay:              cloneInt64Map(s.requestsByDay),
		requestsByHour:             cloneIntMap(s.requestsByHour),
		tokensByDay:                cloneInt64Map(s.tokensByDay),
		tokensByHour:               cloneIntMap(s.tokensByHour),
		projection:                 s.projection.Clone(),
		projectionUnavailable:      s.projectionUnavailable,
		projectionUnavailableCode:  s.projectionUnavailableCode,
		identityMetadataReady:      s.identityMetadataReady,
		identityMetadataGeneration: s.identityMetadataGeneration,
	}
	return state
}

func (s *RequestStatistics) restoreStateLocked(state requestStatisticsState) {
	s.totalRequests = state.totalRequests
	s.successCount = state.successCount
	s.failureCount = state.failureCount
	s.totalTokens = state.totalTokens
	s.changeCount = state.changeCount
	s.persistedCount = state.persistedCount
	s.apis = cloneAPIStats(state.apis)
	s.requestsByDay = cloneInt64Map(state.requestsByDay)
	s.requestsByHour = cloneIntMap(state.requestsByHour)
	s.tokensByDay = cloneInt64Map(state.tokensByDay)
	s.tokensByHour = cloneIntMap(state.tokensByHour)
	s.projection = state.projection.Clone()
	s.projectionUnavailable = state.projectionUnavailable
	s.projectionUnavailableCode = state.projectionUnavailableCode
	s.identityMetadataReady = state.identityMetadataReady
	s.identityMetadataGeneration = state.identityMetadataGeneration
	s.detailLocations = nil
	s.detailEventLocations = nil
	s.ensureDetailLocationsLocked()
}

func cloneAPIStats(source map[string]*apiStats) map[string]*apiStats {
	result := make(map[string]*apiStats, len(source))
	for apiName, stats := range source {
		if stats == nil {
			result[apiName] = nil
			continue
		}
		clone := &apiStats{TotalRequests: stats.TotalRequests, TotalTokens: stats.TotalTokens, Models: make(map[string]*modelStats, len(stats.Models))}
		for modelName, model := range stats.Models {
			if model == nil {
				clone.Models[modelName] = nil
				continue
			}
			details := make([]RequestDetail, len(model.Details))
			for index, detail := range model.Details {
				details[index] = cloneRequestDetail(detail)
			}
			clone.Models[modelName] = &modelStats{TotalRequests: model.TotalRequests, TotalTokens: model.TotalTokens, Details: details}
		}
		result[apiName] = clone
	}
	return result
}

func cloneInt64Map(source map[string]int64) map[string]int64 {
	result := make(map[string]int64, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func cloneIntMap(source map[int]int64) map[int]int64 {
	result := make(map[int]int64, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}
