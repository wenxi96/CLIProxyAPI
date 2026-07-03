package management

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/authquota"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

const (
	authFileBatchCheckClassificationOK              = "ok"
	authFileBatchCheckClassificationNoQuota         = "no_quota"
	authFileBatchCheckClassificationInvalidated401  = "invalidated_401"
	authFileBatchCheckClassificationAPIError        = "api_error"
	authFileBatchCheckClassificationRequestFailed   = "request_failed"
	authFileBatchCheckClassificationUnsupported     = "unsupported_provider"
	authFileBatchCheckClassificationSkippedDisabled = "disabled"
	authFileBatchCheckClassificationRuntimeOnly     = "runtime_only"
	authFileBatchCheckClassificationNotFound        = "auth_not_found"
	authFileBatchCheckClassificationUnknown         = "unknown"
	authFileBatchCheckReenableThresholdBucket       = "alert"
)

type authFileBatchCheckRequest struct {
	Names           []string `json:"names"`
	IncludeDisabled bool     `json:"include_disabled"`
	Concurrency     int      `json:"concurrency"`
}

type authFileBatchCheckSummary struct {
	CheckedCount           int            `json:"checked_count"`
	AvailableCount         int            `json:"available_count"`
	AvailableProviderCount int            `json:"available_provider_count"`
	SkippedCount           int            `json:"skipped_count"`
	AverageRemaining       *int           `json:"average_remaining_percent,omitempty"`
	ClassificationCounts   map[string]int `json:"classification_counts"`
	BucketCounts           map[string]int `json:"bucket_counts"`
}

type authFileBatchCheckCapacityOverview struct {
	RemainingTotal         int      `json:"remaining_total"`
	TotalCapacity          int      `json:"total_capacity"`
	RemainingPercent       float64  `json:"remaining_percent"`
	UsedTotal              int      `json:"used_total"`
	UsedPercent            float64  `json:"used_percent"`
	EquivalentFullAccounts float64  `json:"equivalent_full_accounts"`
	AverageRemaining       *float64 `json:"average_remaining,omitempty"`
	MedianRemaining        *float64 `json:"median_remaining,omitempty"`
	UnknownRemainingCount  int      `json:"unknown_remaining_count"`
}

type authFileBatchCheckRiskOverview struct {
	Invalidated401Count   int `json:"invalidated_401_count"`
	NoQuotaCount          int `json:"no_quota_count"`
	APIErrorCount         int `json:"api_error_count"`
	RequestFailedCount    int `json:"request_failed_count"`
	ExhaustedCount        int `json:"exhausted_count"`
	LowRemaining129Count  int `json:"low_remaining_1_29_count"`
	MidLowRemaining149Cnt int `json:"mid_low_remaining_1_49_count"`
}

type authFileBatchCheckScopeOverview struct {
	TotalCount     int `json:"total_count"`
	EnabledCount   int `json:"enabled_count"`
	DisabledCount  int `json:"disabled_count"`
	ProcessedCount int `json:"processed_count"`
	SkippedCount   int `json:"skipped_count"`
}

type authFileBatchCheckHighlightWindow struct {
	Label string `json:"label"`
	Count int    `json:"count"`
}

type authFileBatchCheckRefreshOverview struct {
	NextRefreshAt       *time.Time                          `json:"next_refresh_at,omitempty"`
	HighlightWindows    []authFileBatchCheckHighlightWindow `json:"highlight_windows"`
	RefreshWindowCounts map[string]int                      `json:"refresh_window_counts"`
}

type authFileBatchCheckPlanDistribution struct {
	PlanTypeCounts       map[string]int `json:"plan_type_counts"`
	PrimaryCycleCounts   map[string]int `json:"primary_cycle_counts"`
	SecondaryCycleCounts map[string]int `json:"secondary_cycle_counts"`
}

type authFileBatchCheckDiagnosis struct {
	Label    string   `json:"label"`
	Count    int      `json:"count"`
	Note     string   `json:"note"`
	Examples []string `json:"examples"`
}

type authFileBatchCheckActionCandidates struct {
	Invalidated401Names     []string `json:"invalidated_401_names"`
	DisableExhaustedNames   []string `json:"disable_exhausted_names"`
	ReenableNames           []string `json:"reenable_names"`
	ReenableThresholdBucket string   `json:"reenable_threshold_bucket"`
}

type authFileBatchCheckAggregate struct {
	CapacityOverview authFileBatchCheckCapacityOverview `json:"capacity_overview"`
	RiskOverview     authFileBatchCheckRiskOverview     `json:"risk_overview"`
	HealthBuckets    map[string]int                     `json:"health_buckets"`
	ScopeOverview    authFileBatchCheckScopeOverview    `json:"scope_overview"`
	RefreshOverview  authFileBatchCheckRefreshOverview  `json:"refresh_overview"`
	PlanDistribution authFileBatchCheckPlanDistribution `json:"plan_distribution"`
	Diagnosis        []authFileBatchCheckDiagnosis      `json:"diagnosis"`
	ActionCandidates authFileBatchCheckActionCandidates `json:"action_candidates"`
}

var authFileBatchCheckHealthBucketOrder = []string{
	"full",
	"very_high",
	"high",
	"usable",
	"fair",
	"alert",
	"danger",
	"exhausted",
	"unknown",
}

var authFileBatchCheckRefreshWindowOrder = []string{
	"已到刷新时间",
	"1小时内",
	"1-3小时",
	"3-6小时",
	"6-12小时",
	"12-24小时",
	"1-3天",
	"3-7天",
	"下周及以后",
	"未知",
}

var authFileBatchCheckReenableBucketRanks = map[string]int{
	"danger":    1,
	"alert":     2,
	"fair":      3,
	"usable":    4,
	"high":      5,
	"very_high": 6,
	"full":      7,
}

type authFileBatchCheckSkipped struct {
	Name     string `json:"name"`
	Provider string `json:"provider,omitempty"`
	Reason   string `json:"reason"`
}

type authFileBatchCheckWindow = coreauth.QuotaWindow

type authFileBatchCheckResult struct {
	Name             string         `json:"name"`
	Provider         string         `json:"provider"`
	AuthIndex        string         `json:"auth_index,omitempty"`
	Status           string         `json:"status,omitempty"`
	StatusMessage    string         `json:"status_message,omitempty"`
	Disabled         bool           `json:"disabled"`
	Unavailable      bool           `json:"unavailable"`
	Available        bool           `json:"available"`
	Classification   string         `json:"classification"`
	RemainingPercent *int           `json:"remaining_percent,omitempty"`
	Bucket           string         `json:"bucket"`
	StatusCode       int            `json:"status_code,omitempty"`
	ErrorMessage     string         `json:"error_message,omitempty"`
	CheckedAt        time.Time      `json:"checked_at"`
	Details          map[string]any `json:"details,omitempty"`
}

func (h *Handler) BatchCheckAuthFiles(c *gin.Context) {
	if h == nil || h.authManager == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "core auth manager unavailable"})
		return
	}

	var req authFileBatchCheckRequest
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
			return
		}
	}

	concurrency, err := resolveBatchCheckConcurrency(req.Concurrency)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	requestedNames := normalizeBatchCheckNames(req.Names)
	auths := h.authManager.List()
	selectedAuths, skipped := selectAuthsForBatchCheck(auths, requestedNames, req.IncludeDisabled)

	results := h.runBatchCheckConcurrently(c.Request.Context(), selectedAuths, concurrency, nil, nil)

	sort.Slice(results, func(i, j int) bool {
		return strings.ToLower(results[i].Name) < strings.ToLower(results[j].Name)
	})
	sort.Slice(skipped, func(i, j int) bool {
		return strings.ToLower(skipped[i].Name) < strings.ToLower(skipped[j].Name)
	})

	summary := buildBatchCheckSummary(results, skipped)
	aggregate := buildBatchCheckAggregate(results, skipped)

	c.JSON(http.StatusOK, gin.H{
		"checked_at": time.Now().UTC(),
		"summary":    summary,
		"aggregate":  aggregate,
		"results":    results,
		"skipped":    skipped,
	})
}

func normalizeBatchCheckNames(names []string) []string {
	if len(names) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(names))
	result := make([]string, 0, len(names))
	for _, name := range names {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	return result
}

func selectAuthsForBatchCheck(auths []*coreauth.Auth, requestedNames []string, includeDisabled bool) ([]*coreauth.Auth, []authFileBatchCheckSkipped) {
	filters := make(map[string]struct{}, len(requestedNames))
	for _, name := range requestedNames {
		filters[name] = struct{}{}
	}

	selected := make([]*coreauth.Auth, 0, len(auths))
	skipped := make([]authFileBatchCheckSkipped, 0)
	matched := make(map[string]struct{}, len(requestedNames))

	for _, auth := range auths {
		if auth == nil {
			continue
		}
		name := authFileBatchCheckName(auth)
		if len(filters) > 0 {
			if _, ok := filters[name]; !ok {
				continue
			}
			matched[name] = struct{}{}
		}

		provider := normalizeBatchCheckProvider(auth.Provider)
		switch {
		case isRuntimeOnlyAuth(auth):
			skipped = append(skipped, authFileBatchCheckSkipped{Name: name, Provider: provider, Reason: authFileBatchCheckClassificationRuntimeOnly})
		case auth.Disabled && !includeDisabled:
			skipped = append(skipped, authFileBatchCheckSkipped{Name: name, Provider: provider, Reason: authFileBatchCheckClassificationSkippedDisabled})
		case !isSupportedBatchCheckProvider(provider):
			skipped = append(skipped, authFileBatchCheckSkipped{Name: name, Provider: provider, Reason: authFileBatchCheckClassificationUnsupported})
		default:
			selected = append(selected, auth)
		}
	}

	if len(filters) > 0 {
		for _, name := range requestedNames {
			if _, ok := matched[name]; ok {
				continue
			}
			skipped = append(skipped, authFileBatchCheckSkipped{Name: name, Reason: authFileBatchCheckClassificationNotFound})
		}
	}

	return selected, skipped
}

func buildBatchCheckSummary(results []authFileBatchCheckResult, skipped []authFileBatchCheckSkipped) authFileBatchCheckSummary {
	summary := authFileBatchCheckSummary{
		CheckedCount:         len(results),
		SkippedCount:         len(skipped),
		ClassificationCounts: map[string]int{},
		BucketCounts:         map[string]int{},
	}

	availableProviders := make(map[string]struct{})
	remainingValues := make([]int, 0, len(results))
	for _, result := range results {
		summary.ClassificationCounts[result.Classification]++
		summary.BucketCounts[result.Bucket]++
		if result.Available {
			summary.AvailableCount++
			availableProviders[result.Provider] = struct{}{}
		}
		if result.RemainingPercent != nil {
			remainingValues = append(remainingValues, *result.RemainingPercent)
		}
	}
	summary.AvailableProviderCount = len(availableProviders)
	if len(remainingValues) > 0 {
		total := 0
		for _, value := range remainingValues {
			total += value
		}
		average := total / len(remainingValues)
		summary.AverageRemaining = &average
	}
	return summary
}

func buildBatchCheckAggregate(results []authFileBatchCheckResult, skipped []authFileBatchCheckSkipped) authFileBatchCheckAggregate {
	aggregate := authFileBatchCheckAggregate{
		HealthBuckets: newBatchCheckCountMap(authFileBatchCheckHealthBucketOrder),
		ScopeOverview: authFileBatchCheckScopeOverview{
			TotalCount:     len(results) + len(skipped),
			ProcessedCount: len(results),
			SkippedCount:   len(skipped),
		},
		RefreshOverview: authFileBatchCheckRefreshOverview{
			HighlightWindows:    make([]authFileBatchCheckHighlightWindow, 0, 3),
			RefreshWindowCounts: newBatchCheckCountMap(authFileBatchCheckRefreshWindowOrder),
		},
		PlanDistribution: authFileBatchCheckPlanDistribution{
			PlanTypeCounts:       map[string]int{},
			PrimaryCycleCounts:   map[string]int{},
			SecondaryCycleCounts: map[string]int{},
		},
		Diagnosis: make([]authFileBatchCheckDiagnosis, 0, 4),
		ActionCandidates: authFileBatchCheckActionCandidates{
			Invalidated401Names:     make([]string, 0),
			DisableExhaustedNames:   make([]string, 0),
			ReenableNames:           make([]string, 0),
			ReenableThresholdBucket: authFileBatchCheckReenableThresholdBucket,
		},
	}

	now := time.Now().UTC()
	remainingValues := make([]int, 0, len(results))
	diagnosisIndex := map[string]int{}

	for _, item := range skipped {
		if item.Reason == authFileBatchCheckClassificationSkippedDisabled {
			aggregate.ScopeOverview.DisabledCount++
		}
	}

	for _, result := range results {
		if result.Disabled {
			aggregate.ScopeOverview.DisabledCount++
		} else {
			aggregate.ScopeOverview.EnabledCount++
		}

		bucketKey := result.Bucket
		if _, ok := aggregate.HealthBuckets[bucketKey]; !ok {
			bucketKey = "unknown"
		}
		aggregate.HealthBuckets[bucketKey]++

		switch result.Classification {
		case authFileBatchCheckClassificationInvalidated401:
			aggregate.RiskOverview.Invalidated401Count++
			aggregate.ActionCandidates.Invalidated401Names = append(aggregate.ActionCandidates.Invalidated401Names, result.Name)
			appendBatchCheckDiagnosis(diagnosisIndex, &aggregate.Diagnosis, "认证失效", "请重新登录或更换认证文件。", result.Name)
		case authFileBatchCheckClassificationNoQuota:
			aggregate.RiskOverview.NoQuotaCount++
			appendBatchCheckDiagnosis(diagnosisIndex, &aggregate.Diagnosis, "额度耗尽", "建议禁用已耗尽文件，等待额度恢复后再启用。", result.Name)
		case authFileBatchCheckClassificationAPIError:
			aggregate.RiskOverview.APIErrorCount++
			appendBatchCheckDiagnosis(diagnosisIndex, &aggregate.Diagnosis, "接口错误", "请检查上游接口状态、代理链路或返回格式。", result.Name)
		case authFileBatchCheckClassificationRequestFailed:
			aggregate.RiskOverview.RequestFailedCount++
			appendBatchCheckDiagnosis(diagnosisIndex, &aggregate.Diagnosis, "请求失败", "请检查网络、代理配置或本地运行环境。", result.Name)
		}

		if result.RemainingPercent != nil {
			remaining := *result.RemainingPercent
			aggregate.CapacityOverview.RemainingTotal += remaining
			remainingValues = append(remainingValues, remaining)
			if remaining <= 0 {
				aggregate.RiskOverview.ExhaustedCount++
			}
			if remaining >= 1 && remaining <= 29 {
				aggregate.RiskOverview.LowRemaining129Count++
			}
			if remaining >= 1 && remaining <= 49 {
				aggregate.RiskOverview.MidLowRemaining149Cnt++
			}
		} else {
			aggregate.CapacityOverview.UnknownRemainingCount++
		}

		if !result.Disabled && result.Bucket == "exhausted" {
			aggregate.ActionCandidates.DisableExhaustedNames = append(aggregate.ActionCandidates.DisableExhaustedNames, result.Name)
		}
		if result.Disabled && batchCheckBucketMeetsThreshold(result.Bucket, authFileBatchCheckReenableThresholdBucket) && result.Classification == authFileBatchCheckClassificationOK {
			aggregate.ActionCandidates.ReenableNames = append(aggregate.ActionCandidates.ReenableNames, result.Name)
		}

		if planType := strings.TrimSpace(batchCheckDetailsString(result.Details, "plan_type")); planType != "" {
			aggregate.PlanDistribution.PlanTypeCounts[planType]++
		}
		for _, cycle := range batchCheckPrimaryCycleLabels(result.Details, now) {
			aggregate.PlanDistribution.PrimaryCycleCounts[cycle]++
		}
		for _, cycle := range batchCheckSecondaryCycleLabels(result.Details, now) {
			aggregate.PlanDistribution.SecondaryCycleCounts[cycle]++
		}

		refreshAt := batchCheckResultRefreshAt(result, now)
		refreshLabel := batchCheckRefreshWindowLabel(refreshAt, now)
		aggregate.RefreshOverview.RefreshWindowCounts[refreshLabel]++
		if refreshAt != nil && (aggregate.RefreshOverview.NextRefreshAt == nil || refreshAt.Before(*aggregate.RefreshOverview.NextRefreshAt)) {
			value := refreshAt.UTC()
			aggregate.RefreshOverview.NextRefreshAt = &value
		}
	}

	knownCount := len(remainingValues)
	aggregate.CapacityOverview.TotalCapacity = knownCount * 100
	aggregate.CapacityOverview.UsedTotal = maxInt(0, aggregate.CapacityOverview.TotalCapacity-aggregate.CapacityOverview.RemainingTotal)
	if aggregate.CapacityOverview.TotalCapacity > 0 {
		aggregate.CapacityOverview.RemainingPercent = batchCheckRound2(float64(aggregate.CapacityOverview.RemainingTotal) * 100 / float64(aggregate.CapacityOverview.TotalCapacity))
		aggregate.CapacityOverview.UsedPercent = batchCheckRound2(float64(aggregate.CapacityOverview.UsedTotal) * 100 / float64(aggregate.CapacityOverview.TotalCapacity))
	}
	if knownCount > 0 {
		aggregate.CapacityOverview.EquivalentFullAccounts = batchCheckRound2(float64(aggregate.CapacityOverview.RemainingTotal) / 100)
		average := batchCheckRound2(float64(aggregate.CapacityOverview.RemainingTotal) / float64(knownCount))
		aggregate.CapacityOverview.AverageRemaining = &average
		aggregate.CapacityOverview.MedianRemaining = batchCheckMedian(remainingValues)
	}

	for _, label := range authFileBatchCheckRefreshWindowOrder {
		if label == "未知" {
			continue
		}
		count := aggregate.RefreshOverview.RefreshWindowCounts[label]
		if count <= 0 {
			continue
		}
		aggregate.RefreshOverview.HighlightWindows = append(aggregate.RefreshOverview.HighlightWindows, authFileBatchCheckHighlightWindow{
			Label: label,
			Count: count,
		})
		if len(aggregate.RefreshOverview.HighlightWindows) >= 3 {
			break
		}
	}

	sort.Strings(aggregate.ActionCandidates.Invalidated401Names)
	sort.Strings(aggregate.ActionCandidates.DisableExhaustedNames)
	sort.Strings(aggregate.ActionCandidates.ReenableNames)
	for index := range aggregate.Diagnosis {
		sort.Strings(aggregate.Diagnosis[index].Examples)
	}

	return aggregate
}

func newBatchCheckCountMap(keys []string) map[string]int {
	counts := make(map[string]int, len(keys))
	for _, key := range keys {
		counts[key] = 0
	}
	return counts
}

func batchCheckRound2(value float64) float64 {
	return math.Round(value*100) / 100
}

func batchCheckMedian(values []int) *float64 {
	if len(values) == 0 {
		return nil
	}
	clone := append([]int(nil), values...)
	sort.Ints(clone)
	middle := len(clone) / 2
	if len(clone)%2 == 1 {
		value := float64(clone[middle])
		return &value
	}
	value := batchCheckRound2(float64(clone[middle-1]+clone[middle]) / 2)
	return &value
}

func appendBatchCheckDiagnosis(index map[string]int, collection *[]authFileBatchCheckDiagnosis, label, note, example string) {
	if label == "" {
		return
	}
	if idx, ok := index[label]; ok {
		(*collection)[idx].Count++
		if example != "" && len((*collection)[idx].Examples) < 3 && !containsString((*collection)[idx].Examples, example) {
			(*collection)[idx].Examples = append((*collection)[idx].Examples, example)
		}
		return
	}
	item := authFileBatchCheckDiagnosis{
		Label:    label,
		Count:    1,
		Note:     note,
		Examples: make([]string, 0, 3),
	}
	if example != "" {
		item.Examples = append(item.Examples, example)
	}
	index[label] = len(*collection)
	*collection = append(*collection, item)
}

func batchCheckBucketMeetsThreshold(bucket, threshold string) bool {
	bucketRank := authFileBatchCheckReenableBucketRanks[bucket]
	thresholdRank := authFileBatchCheckReenableBucketRanks[threshold]
	return bucketRank > 0 && thresholdRank > 0 && bucketRank >= thresholdRank
}

func batchCheckDetailsString(details map[string]any, key string) string {
	if len(details) == 0 {
		return ""
	}
	return strings.TrimSpace(stringValueAny(details[key]))
}

func batchCheckPrimaryCycleLabels(details map[string]any, now time.Time) []string {
	labels := make([]string, 0)
	for _, window := range batchCheckDetailsWindows(details, "windows") {
		label := batchCheckCycleLabel(window, now)
		if label != "" {
			labels = append(labels, label)
		}
	}
	return uniqueStrings(labels)
}

func batchCheckSecondaryCycleLabels(details map[string]any, now time.Time) []string {
	labels := make([]string, 0)
	for _, window := range batchCheckDetailsWindows(details, "windows") {
		if !batchCheckWindowLooksSecondary(window) {
			continue
		}
		label := batchCheckCycleLabel(window, now)
		if label != "" {
			labels = append(labels, label)
		}
	}
	return uniqueStrings(labels)
}

func batchCheckWindowLooksSecondary(window authFileBatchCheckWindow) bool {
	joined := strings.ToLower(strings.Join([]string{window.ID, window.Label}, " "))
	return strings.Contains(joined, "secondary") || strings.Contains(joined, "weekly") || strings.Contains(joined, "seven_day") || strings.Contains(joined, "seven-day")
}

func batchCheckCycleLabel(window authFileBatchCheckWindow, now time.Time) string {
	if window.ResetAfter != nil && *window.ResetAfter > 0 {
		return batchCheckDurationCycleLabel(time.Duration(*window.ResetAfter) * time.Second)
	}
	if resetAt := batchCheckWindowResetAt(window, now); resetAt != nil {
		return batchCheckDurationCycleLabel(resetAt.Sub(now))
	}
	joined := strings.ToLower(strings.Join([]string{window.ID, window.Label}, " "))
	switch {
	case strings.Contains(joined, "five_hour") || strings.Contains(joined, "five-hour") || strings.Contains(joined, "5h"):
		return "5h"
	case strings.Contains(joined, "seven_day") || strings.Contains(joined, "seven-day") || strings.Contains(joined, "weekly") || strings.Contains(joined, "7d"):
		return "7d"
	case strings.Contains(joined, "day"):
		return "1d"
	default:
		return ""
	}
}

func batchCheckDurationCycleLabel(duration time.Duration) string {
	if duration <= 0 {
		return ""
	}
	switch {
	case duration <= 6*time.Hour:
		return "5h"
	case duration <= 36*time.Hour:
		return "1d"
	case duration <= 10*24*time.Hour:
		return "7d"
	default:
		return "长期"
	}
}

func batchCheckDetailsWindows(details map[string]any, key string) []authFileBatchCheckWindow {
	if len(details) == 0 {
		return nil
	}
	raw, ok := details[key]
	if !ok || raw == nil {
		return nil
	}
	if windows, ok := raw.([]authFileBatchCheckWindow); ok {
		return windows
	}
	return nil
}

func batchCheckResultRefreshAt(result authFileBatchCheckResult, now time.Time) *time.Time {
	var earliest *time.Time
	for _, key := range []string{"windows", "buckets", "rows", "groups"} {
		for _, window := range batchCheckDetailsWindows(result.Details, key) {
			resetAt := batchCheckWindowResetAt(window, now)
			if resetAt == nil {
				continue
			}
			if earliest == nil || resetAt.Before(*earliest) {
				value := resetAt.UTC()
				earliest = &value
			}
		}
	}
	return earliest
}

func batchCheckWindowResetAt(window authFileBatchCheckWindow, now time.Time) *time.Time {
	if window.ResetAt != nil && *window.ResetAt > 0 {
		value := *window.ResetAt
		timeValue := time.Unix(value, 0).UTC()
		if value > 1_000_000_000_000 {
			timeValue = time.UnixMilli(value).UTC()
		}
		return &timeValue
	}
	if window.ResetAfter != nil && *window.ResetAfter > 0 {
		value := now.Add(time.Duration(*window.ResetAfter) * time.Second).UTC()
		return &value
	}
	if window.ResetTime != "" {
		for _, layout := range []string{time.RFC3339Nano, time.RFC3339, time.DateTime} {
			if parsed, err := time.Parse(layout, window.ResetTime); err == nil {
				value := parsed.UTC()
				return &value
			}
		}
	}
	return batchCheckResetHintTime(window.ResetHint, now)
}

func batchCheckResetHintTime(hint string, now time.Time) *time.Time {
	trimmed := strings.TrimSpace(strings.ToLower(hint))
	if trimmed == "" {
		return nil
	}
	trimmed = strings.ReplaceAll(trimmed, " ", "")
	duration, err := time.ParseDuration(trimmed)
	if err != nil || duration <= 0 {
		return nil
	}
	value := now.Add(duration).UTC()
	return &value
}

func batchCheckRefreshWindowLabel(refreshAt *time.Time, now time.Time) string {
	if refreshAt == nil {
		return "未知"
	}
	delta := refreshAt.Sub(now)
	switch {
	case delta <= 0:
		return "已到刷新时间"
	case delta <= time.Hour:
		return "1小时内"
	case delta <= 3*time.Hour:
		return "1-3小时"
	case delta <= 6*time.Hour:
		return "3-6小时"
	case delta <= 12*time.Hour:
		return "6-12小时"
	case delta <= 24*time.Hour:
		return "12-24小时"
	case delta <= 3*24*time.Hour:
		return "1-3天"
	case delta <= 7*24*time.Hour:
		return "3-7天"
	default:
		return "下周及以后"
	}
}

func containsString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

func uniqueStrings(items []string) []string {
	seen := make(map[string]struct{}, len(items))
	result := make([]string, 0, len(items))
	for _, item := range items {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	sort.Strings(result)
	return result
}

func (h *Handler) checkSingleAuthFile(ctx context.Context, auth *coreauth.Auth) authFileBatchCheckResult {
	result := authFileBatchCheckResult{
		Name:           authFileBatchCheckName(auth),
		Provider:       normalizeBatchCheckProvider(auth.Provider),
		AuthIndex:      auth.EnsureIndex(),
		Status:         string(auth.Status),
		StatusMessage:  strings.TrimSpace(auth.StatusMessage),
		Disabled:       auth.Disabled,
		Unavailable:    auth.Unavailable,
		Classification: authFileBatchCheckClassificationUnknown,
		Bucket:         "unknown",
		CheckedAt:      time.Now().UTC(),
	}

	service := h.newBatchCheckQuotaService()
	if service == nil || !service.Supports(auth) {
		result.Classification = authFileBatchCheckClassificationUnsupported
		return result
	}
	quotaResult, err := service.Check(ctx, auth)
	if err != nil {
		return finalizeBatchCheckResult(result, authFileBatchCheckClassificationRequestFailed, nil, err.Error(), 0, nil)
	}
	return finalizeBatchCheckResult(
		result,
		quotaResult.Classification,
		quotaResult.RemainingPercent,
		quotaResult.ErrorMessage,
		quotaResult.StatusCode,
		quotaResult.Details,
	)
}

func (h *Handler) newBatchCheckQuotaService() *authquota.Service {
	if h == nil {
		return nil
	}
	return authquota.NewService(authquota.Options{
		ConfigProvider: func() *config.Config {
			return h.cfg
		},
		TransportProvider: func(auth *coreauth.Auth, _ *config.Config) http.RoundTripper {
			return h.apiCallTransport(auth)
		},
		APICallExecutor: h.batchCheckQuotaAPICallExecutor(),
	})
}

func (h *Handler) batchCheckQuotaAPICallExecutor() func(context.Context, *coreauth.Auth, authquota.APICallRequest) (authquota.APICallResponse, error) {
	if h == nil || h.apiCallExecutor == nil {
		return nil
	}
	return func(ctx context.Context, auth *coreauth.Auth, req authquota.APICallRequest) (authquota.APICallResponse, error) {
		resp, err := h.apiCallExecutor(ctx, auth, apiCallRequest{
			Method: req.Method,
			URL:    req.URL,
			Header: req.Header,
			Data:   req.Data,
		})
		return authquota.APICallResponse{
			StatusCode: resp.StatusCode,
			Header:     resp.Header,
			Body:       resp.Body,
		}, err
	}
}

func finalizeBatchCheckResult(result authFileBatchCheckResult, classification string, remaining *int, errorMessage string, statusCode int, details map[string]any) authFileBatchCheckResult {
	if classification == "" {
		classification = authFileBatchCheckClassificationUnknown
	}
	result.Classification = classification
	result.RemainingPercent = remaining
	result.ErrorMessage = strings.TrimSpace(errorMessage)
	result.StatusCode = statusCode
	result.Bucket = quotaBucketFromRemainingPercent(remaining)
	result.Available = classification == authFileBatchCheckClassificationOK
	if details != nil && len(details) > 0 {
		result.Details = details
	}
	return result
}

func quotaBucketFromRemainingPercent(remaining *int) string {
	if remaining == nil {
		return "unknown"
	}
	value := *remaining
	switch {
	case value <= 0:
		return "exhausted"
	case value >= 98:
		return "full"
	case value >= 90:
		return "very_high"
	case value >= 75:
		return "high"
	case value >= 50:
		return "usable"
	case value >= 30:
		return "fair"
	case value >= 10:
		return "alert"
	default:
		return "danger"
	}
}

func normalizeBatchCheckProvider(provider string) string {
	return strings.ToLower(strings.TrimSpace(provider))
}

func isSupportedBatchCheckProvider(provider string) bool {
	switch provider {
	case "antigravity", "claude", "codex", "gemini-cli", "kimi":
		return true
	default:
		return false
	}
}

func authFileBatchCheckName(auth *coreauth.Auth) string {
	if auth == nil {
		return ""
	}
	if name := strings.TrimSpace(auth.FileName); name != "" {
		return name
	}
	return strings.TrimSpace(auth.ID)
}

func stringValueAny(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case fmt.Stringer:
		return typed.String()
	case int:
		return strconv.Itoa(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	default:
		return ""
	}
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}
