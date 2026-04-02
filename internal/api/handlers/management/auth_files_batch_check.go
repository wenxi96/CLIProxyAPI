package management

import (
	"context"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	log "github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
)

const (
	antigravityDefaultProjectID = "bamboo-precept-lgxtn"
	antigravityQuotaURLPrimary  = "https://daily-cloudcode-pa.googleapis.com/v1internal:fetchAvailableModels"
	antigravityQuotaURLSandbox  = "https://daily-cloudcode-pa.sandbox.googleapis.com/v1internal:fetchAvailableModels"
	antigravityQuotaURLDefault  = "https://cloudcode-pa.googleapis.com/v1internal:fetchAvailableModels"
	geminiCLIQuotaURL           = "https://cloudcode-pa.googleapis.com/v1internal:retrieveUserQuota"
	geminiCLICodeAssistURL      = "https://cloudcode-pa.googleapis.com/v1internal:loadCodeAssist"
	claudeProfileURL            = "https://api.anthropic.com/api/oauth/profile"
	claudeUsageURL              = "https://api.anthropic.com/api/oauth/usage"
	codexUsageURL               = "https://chatgpt.com/backend-api/wham/usage"
	kimiUsageURL                = "https://api.kimi.com/coding/v1/usages"
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
	authFileBatchCheckReenableThresholdBucket       = "danger"
)

var antigravityQuotaURLs = []string{
	antigravityQuotaURLPrimary,
	antigravityQuotaURLSandbox,
	antigravityQuotaURLDefault,
}

var claudeBatchCheckWindows = []struct {
	Key   string
	ID    string
	Label string
}{
	{Key: "five_hour", ID: "five-hour", Label: "five_hour"},
	{Key: "seven_day", ID: "seven-day", Label: "seven_day"},
	{Key: "seven_day_oauth_apps", ID: "seven-day-oauth-apps", Label: "seven_day_oauth_apps"},
	{Key: "seven_day_opus", ID: "seven-day-opus", Label: "seven_day_opus"},
	{Key: "seven_day_sonnet", ID: "seven-day-sonnet", Label: "seven_day_sonnet"},
	{Key: "seven_day_cowork", ID: "seven-day-cowork", Label: "seven_day_cowork"},
	{Key: "iguana_necktie", ID: "iguana-necktie", Label: "iguana_necktie"},
}

var antigravityBatchCheckGroups = []struct {
	ID          string
	Label       string
	Identifiers []string
}{
	{ID: "claude-gpt", Label: "Claude/GPT", Identifiers: []string{"claude-sonnet-4-6", "claude-opus-4-6-thinking", "gpt-oss-120b-medium"}},
	{ID: "gemini-3-pro", Label: "Gemini 3 Pro", Identifiers: []string{"gemini-3-pro-high", "gemini-3-pro-low"}},
	{ID: "gemini-3-1-pro-series", Label: "Gemini 3.1 Pro Series", Identifiers: []string{"gemini-3.1-pro-high", "gemini-3.1-pro-low"}},
	{ID: "gemini-2-5-flash", Label: "Gemini 2.5 Flash", Identifiers: []string{"gemini-2.5-flash", "gemini-2.5-flash-thinking"}},
	{ID: "gemini-2-5-flash-lite", Label: "Gemini 2.5 Flash Lite", Identifiers: []string{"gemini-2.5-flash-lite"}},
	{ID: "gemini-2-5-cu", Label: "Gemini 2.5 CU", Identifiers: []string{"rev19-uic3-1p"}},
	{ID: "gemini-3-flash", Label: "Gemini 3 Flash", Identifiers: []string{"gemini-3-flash"}},
	{ID: "gemini-image", Label: "gemini-3.1-flash-image", Identifiers: []string{"gemini-3.1-flash-image"}},
}

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

type authFileBatchCheckWindow struct {
	ID               string   `json:"id"`
	Label            string   `json:"label,omitempty"`
	UsedPercent      *int     `json:"used_percent,omitempty"`
	RemainingPercent *int     `json:"remaining_percent,omitempty"`
	ResetAt          *int64   `json:"reset_at,omitempty"`
	ResetAfter       *int     `json:"reset_after_seconds,omitempty"`
	ResetTime        string   `json:"reset_time,omitempty"`
	RemainingAmount  *int     `json:"remaining_amount,omitempty"`
	Limit            *int     `json:"limit,omitempty"`
	Used             *int     `json:"used,omitempty"`
	ResetHint        string   `json:"reset_hint,omitempty"`
	TokenType        string   `json:"token_type,omitempty"`
	ModelIDs         []string `json:"model_ids,omitempty"`
}

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

	switch result.Provider {
	case "codex":
		return h.checkCodexAuthFile(ctx, auth, result)
	case "claude":
		return h.checkClaudeAuthFile(ctx, auth, result)
	case "gemini-cli":
		return h.checkGeminiCLIAuthFile(ctx, auth, result)
	case "kimi":
		return h.checkKimiAuthFile(ctx, auth, result)
	case "antigravity":
		return h.checkAntigravityAuthFile(ctx, auth, result)
	default:
		result.Classification = authFileBatchCheckClassificationUnsupported
		return result
	}
}

func (h *Handler) checkCodexAuthFile(ctx context.Context, auth *coreauth.Auth, result authFileBatchCheckResult) authFileBatchCheckResult {
	accountID := resolveCodexBatchCheckAccountID(auth)
	if accountID == "" {
		return finalizeBatchCheckResult(result, authFileBatchCheckClassificationRequestFailed, nil, "missing chatgpt account id", 0, nil)
	}

	resp, err := h.executeBatchCheckAPICall(ctx, auth, apiCallRequest{
		Method: "GET",
		URL:    codexUsageURL,
		Header: map[string]string{
			"Authorization":      "Bearer $TOKEN$",
			"Content-Type":       "application/json",
			"User-Agent":         "codex_cli_rs/0.76.0 (Debian 13.0.0; x86_64) WindowsTerminal",
			"Chatgpt-Account-Id": accountID,
		},
	})

	classification, errorMessage, statusCode := classifyBatchCheckAPIResponse(resp, err)
	payload := gjson.Parse(resp.Body)
	windows := extractCodexBatchCheckWindows(payload)
	remaining := minRemainingFromWindows(windows)
	details := map[string]any{"windows": windows}
	if planType := strings.TrimSpace(payload.Get("plan_type").String()); planType != "" {
		details["plan_type"] = planType
	}
	if classification == "" && len(windows) == 0 {
		classification = authFileBatchCheckClassificationAPIError
		errorMessage = "empty codex quota payload"
	}
	if classification == "" {
		classification = classificationFromRemainingPercent(remaining)
	}
	return finalizeBatchCheckResult(result, classification, remaining, errorMessage, statusCode, details)
}

func (h *Handler) checkClaudeAuthFile(ctx context.Context, auth *coreauth.Auth, result authFileBatchCheckResult) authFileBatchCheckResult {
	resp, err := h.executeBatchCheckAPICall(ctx, auth, apiCallRequest{
		Method: "GET",
		URL:    claudeUsageURL,
		Header: map[string]string{
			"Authorization":  "Bearer $TOKEN$",
			"Content-Type":   "application/json",
			"anthropic-beta": "oauth-2025-04-20",
		},
	})

	classification, errorMessage, statusCode := classifyBatchCheckAPIResponse(resp, err)
	payload := gjson.Parse(resp.Body)
	windows := extractClaudeBatchCheckWindows(payload)
	remaining := minRemainingFromWindows(windows)
	details := map[string]any{"windows": windows}
	if classification == "" && len(windows) == 0 {
		classification = authFileBatchCheckClassificationAPIError
		errorMessage = "empty claude quota payload"
	}
	if classification == "" {
		profileResp, profileErr := h.executeBatchCheckAPICall(ctx, auth, apiCallRequest{
			Method: "GET",
			URL:    claudeProfileURL,
			Header: map[string]string{
				"Authorization":  "Bearer $TOKEN$",
				"Content-Type":   "application/json",
				"anthropic-beta": "oauth-2025-04-20",
			},
		})
		if profileErr == nil && profileResp.StatusCode >= http.StatusOK && profileResp.StatusCode < http.StatusMultipleChoices {
			if planType := resolveClaudeBatchCheckPlanType(gjson.Parse(profileResp.Body)); planType != "" {
				details["plan_type"] = planType
			}
		}
		classification = classificationFromRemainingPercent(remaining)
	}
	return finalizeBatchCheckResult(result, classification, remaining, errorMessage, statusCode, details)
}

func (h *Handler) checkGeminiCLIAuthFile(ctx context.Context, auth *coreauth.Auth, result authFileBatchCheckResult) authFileBatchCheckResult {
	projectID := resolveGeminiCLIBatchCheckProjectID(auth)
	if projectID == "" {
		return finalizeBatchCheckResult(result, authFileBatchCheckClassificationRequestFailed, nil, "missing project id", 0, nil)
	}

	resp, err := h.executeBatchCheckAPICall(ctx, auth, apiCallRequest{
		Method: "POST",
		URL:    geminiCLIQuotaURL,
		Header: map[string]string{
			"Authorization": "Bearer $TOKEN$",
			"Content-Type":  "application/json",
		},
		Data: fmt.Sprintf(`{"project":%q}`, projectID),
	})

	classification, errorMessage, statusCode := classifyBatchCheckAPIResponse(resp, err)
	payload := gjson.Parse(resp.Body)
	buckets := extractGeminiCLIBatchCheckBuckets(payload)
	remaining := minRemainingFromWindows(buckets)
	details := map[string]any{
		"project_id": projectID,
		"buckets":    buckets,
	}
	if classification == "" && len(buckets) == 0 {
		classification = authFileBatchCheckClassificationAPIError
		errorMessage = "empty gemini cli quota payload"
	}
	if classification == "" {
		suppResp, suppErr := h.executeBatchCheckAPICall(ctx, auth, apiCallRequest{
			Method: "POST",
			URL:    geminiCLICodeAssistURL,
			Header: map[string]string{
				"Authorization": "Bearer $TOKEN$",
				"Content-Type":  "application/json",
			},
			Data: fmt.Sprintf(`{"cloudaicompanionProject":%q,"metadata":{"ideType":"IDE_UNSPECIFIED","platform":"PLATFORM_UNSPECIFIED","pluginType":"GEMINI","duetProject":%q}}`, projectID, projectID),
		})
		if suppErr == nil && suppResp.StatusCode >= http.StatusOK && suppResp.StatusCode < http.StatusMultipleChoices {
			supplementary := gjson.Parse(suppResp.Body)
			if tierID := strings.TrimSpace(firstNonEmptyGJSON(supplementary, "paidTier.id", "paid_tier.id", "currentTier.id", "current_tier.id")); tierID != "" {
				details["tier_id"] = strings.ToLower(tierID)
			}
			if creditBalance := extractGeminiCLICreditBalance(supplementary); creditBalance != nil {
				details["credit_balance"] = *creditBalance
			}
		}
		classification = classificationFromRemainingPercent(remaining)
	}
	return finalizeBatchCheckResult(result, classification, remaining, errorMessage, statusCode, details)
}

func (h *Handler) checkKimiAuthFile(ctx context.Context, auth *coreauth.Auth, result authFileBatchCheckResult) authFileBatchCheckResult {
	resp, err := h.executeBatchCheckAPICall(ctx, auth, apiCallRequest{
		Method: "GET",
		URL:    kimiUsageURL,
		Header: map[string]string{
			"Authorization": "Bearer $TOKEN$",
		},
	})

	classification, errorMessage, statusCode := classifyBatchCheckAPIResponse(resp, err)
	payload := gjson.Parse(resp.Body)
	rows := extractKimiBatchCheckRows(payload)
	remaining := minRemainingFromWindows(rows)
	details := map[string]any{"rows": rows}
	if classification == "" && len(rows) == 0 {
		classification = authFileBatchCheckClassificationAPIError
		errorMessage = "empty kimi quota payload"
	}
	if classification == "" {
		classification = classificationFromRemainingPercent(remaining)
	}
	return finalizeBatchCheckResult(result, classification, remaining, errorMessage, statusCode, details)
}

func (h *Handler) checkAntigravityAuthFile(ctx context.Context, auth *coreauth.Auth, result authFileBatchCheckResult) authFileBatchCheckResult {
	projectID := resolveAntigravityBatchCheckProjectID(auth)
	requestBody := fmt.Sprintf(`{"project":%q}`, projectID)

	var lastResp apiCallResponse
	var lastErr error
	for _, quotaURL := range antigravityQuotaURLs {
		resp, err := h.executeBatchCheckAPICall(ctx, auth, apiCallRequest{
			Method: "POST",
			URL:    quotaURL,
			Header: map[string]string{
				"Authorization": "Bearer $TOKEN$",
				"Content-Type":  "application/json",
				"User-Agent":    "antigravity/1.11.5 windows/amd64",
			},
			Data: requestBody,
		})
		lastResp = resp
		lastErr = err

		classification, errorMessage, statusCode := classifyBatchCheckAPIResponse(resp, err)
		if classification == authFileBatchCheckClassificationInvalidated401 || classification == authFileBatchCheckClassificationRequestFailed {
			return finalizeBatchCheckResult(result, classification, nil, errorMessage, statusCode, nil)
		}
		if err != nil || resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
			continue
		}

		payload := gjson.Parse(resp.Body)
		groups := extractAntigravityBatchCheckGroups(payload)
		if len(groups) == 0 {
			continue
		}
		remaining := minRemainingFromWindows(groups)
		return finalizeBatchCheckResult(result, classificationFromRemainingPercent(remaining), remaining, "", resp.StatusCode, map[string]any{
			"project_id": projectID,
			"groups":     groups,
		})
	}

	classification, errorMessage, statusCode := classifyBatchCheckAPIResponse(lastResp, lastErr)
	if classification == "" {
		classification = authFileBatchCheckClassificationAPIError
		if errorMessage == "" {
			errorMessage = "failed to fetch antigravity quota"
		}
	}
	return finalizeBatchCheckResult(result, classification, nil, errorMessage, statusCode, nil)
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

func classificationFromRemainingPercent(remaining *int) string {
	if remaining != nil && *remaining <= 0 {
		return authFileBatchCheckClassificationNoQuota
	}
	return authFileBatchCheckClassificationOK
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

func classifyBatchCheckAPIResponse(resp apiCallResponse, err error) (string, string, int) {
	if err != nil {
		return authFileBatchCheckClassificationRequestFailed, strings.TrimSpace(err.Error()), 0
	}

	body := gjson.Parse(resp.Body)
	statusCode := resp.StatusCode
	if statusCode >= http.StatusOK && statusCode < http.StatusMultipleChoices {
		return "", "", statusCode
	}
	errorMessage := extractBatchCheckAPIErrorMessage(body, resp.Body)
	switch {
	case statusCode == http.StatusUnauthorized:
		return authFileBatchCheckClassificationInvalidated401, errorMessage, statusCode
	case looksLikeNoQuotaError(body, errorMessage, statusCode):
		return authFileBatchCheckClassificationNoQuota, errorMessage, statusCode
	case statusCode >= http.StatusBadRequest:
		return authFileBatchCheckClassificationAPIError, errorMessage, statusCode
	default:
		return "", errorMessage, statusCode
	}
}

func extractBatchCheckAPIErrorMessage(body gjson.Result, raw string) string {
	for _, candidate := range []string{
		body.Get("error.message").String(),
		body.Get("message").String(),
		body.Get("error").String(),
		raw,
	} {
		if trimmed := strings.TrimSpace(candidate); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func looksLikeNoQuotaError(body gjson.Result, errorMessage string, statusCode int) bool {
	if statusCode == http.StatusTooManyRequests {
		return true
	}
	joined := strings.ToLower(strings.Join([]string{
		body.Get("error.code").String(),
		body.Get("error.type").String(),
		errorMessage,
	}, " "))
	return strings.Contains(joined, "usage_limit_reached") ||
		strings.Contains(joined, "usage limit has been reached") ||
		(statusCode >= http.StatusBadRequest && strings.Contains(joined, "quota"))
}

func resolveCodexBatchCheckAccountID(auth *coreauth.Auth) string {
	if auth == nil {
		return ""
	}
	if auth.Metadata != nil {
		for _, key := range []string{"chatgpt_account_id", "chatgptAccountId"} {
			if value := strings.TrimSpace(stringValueAny(auth.Metadata[key])); value != "" {
				return value
			}
		}
	}
	if auth.Attributes != nil {
		for _, key := range []string{"chatgpt_account_id", "chatgptAccountId"} {
			if value := strings.TrimSpace(auth.Attributes[key]); value != "" {
				return value
			}
		}
	}
	if claims := extractCodexIDTokenClaims(auth); claims != nil {
		if value := strings.TrimSpace(stringValueAny(claims["chatgpt_account_id"])); value != "" {
			return value
		}
	}
	return ""
}

func resolveGeminiCLIBatchCheckProjectID(auth *coreauth.Auth) string {
	if auth == nil {
		return ""
	}
	if auth.Metadata != nil {
		for _, key := range []string{"project_id", "projectId"} {
			if value := strings.TrimSpace(stringValueAny(auth.Metadata[key])); value != "" {
				return value
			}
		}
	}
	_, account := auth.AccountInfo()
	if account == "" {
		return ""
	}
	start := strings.LastIndex(account, "(")
	end := strings.LastIndex(account, ")")
	if start >= 0 && end > start {
		return strings.TrimSpace(account[start+1 : end])
	}
	return ""
}

func resolveAntigravityBatchCheckProjectID(auth *coreauth.Auth) string {
	if auth == nil {
		return antigravityDefaultProjectID
	}
	if auth.Metadata != nil {
		for _, key := range []string{"project_id", "projectId"} {
			if value := strings.TrimSpace(stringValueAny(auth.Metadata[key])); value != "" {
				return value
			}
		}
	}
	path := strings.TrimSpace(authAttribute(auth, "path"))
	if path != "" {
		if content, err := os.ReadFile(path); err == nil {
			raw := gjson.ParseBytes(content)
			for _, key := range []string{"project_id", "projectId", "installed.project_id", "installed.projectId", "web.project_id", "web.projectId"} {
				if value := strings.TrimSpace(raw.Get(key).String()); value != "" {
					return value
				}
			}
		}
	}
	return antigravityDefaultProjectID
}

func resolveClaudeBatchCheckPlanType(payload gjson.Result) string {
	switch {
	case payload.Get("account.has_claude_max").Bool():
		return "plan_max"
	case payload.Get("account.has_claude_pro").Bool():
		return "plan_pro"
	case payload.Get("account.has_claude_max").Exists() || payload.Get("account.has_claude_pro").Exists():
		return "plan_free"
	default:
		return ""
	}
}

func extractCodexBatchCheckWindows(payload gjson.Result) []authFileBatchCheckWindow {
	definitions := []struct {
		ID    string
		Label string
		Path  string
	}{
		{ID: "five-hour", Label: "five_hour", Path: "rate_limit.primary_window"},
		{ID: "weekly", Label: "weekly", Path: "rate_limit.secondary_window"},
		{ID: "code-review-five-hour", Label: "code_review_five_hour", Path: "code_review_rate_limit.primary_window"},
		{ID: "code-review-weekly", Label: "code_review_weekly", Path: "code_review_rate_limit.secondary_window"},
	}

	windows := make([]authFileBatchCheckWindow, 0, len(definitions))
	for _, def := range definitions {
		window := payload.Get(def.Path)
		if !window.Exists() {
			continue
		}
		usedPercent := intPtrFromGJSON(window, "used_percent", "usedPercent")
		remainingPercent := remainingPercentFromUsedPercent(usedPercent)
		windows = append(windows, authFileBatchCheckWindow{
			ID:               def.ID,
			Label:            def.Label,
			UsedPercent:      usedPercent,
			RemainingPercent: remainingPercent,
			ResetAfter:       intPtrFromGJSON(window, "reset_after_seconds", "resetAfterSeconds"),
			ResetAt:          int64PtrFromGJSON(window, "reset_at", "resetAt"),
		})
	}
	return windows
}

func extractClaudeBatchCheckWindows(payload gjson.Result) []authFileBatchCheckWindow {
	windows := make([]authFileBatchCheckWindow, 0, len(claudeBatchCheckWindows))
	for _, def := range claudeBatchCheckWindows {
		window := payload.Get(def.Key)
		if !window.Exists() {
			continue
		}
		usedPercent := intPtrFromGJSON(window, "utilization")
		windows = append(windows, authFileBatchCheckWindow{
			ID:               def.ID,
			Label:            def.Label,
			UsedPercent:      usedPercent,
			RemainingPercent: remainingPercentFromUsedPercent(usedPercent),
			ResetTime:        strings.TrimSpace(window.Get("resets_at").String()),
		})
	}
	return windows
}

func extractGeminiCLIBatchCheckBuckets(payload gjson.Result) []authFileBatchCheckWindow {
	buckets := make([]authFileBatchCheckWindow, 0)
	for _, bucket := range payload.Get("buckets").Array() {
		modelID := strings.TrimSpace(firstNonEmptyGJSON(bucket, "modelId", "model_id"))
		if modelID == "" {
			continue
		}
		remainingPercent := percentageFromFraction(float64PtrFromGJSON(bucket, "remainingFraction", "remaining_fraction"))
		remainingAmount := intPtrFromGJSON(bucket, "remainingAmount", "remaining_amount")
		if remainingPercent == nil && remainingAmount != nil && *remainingAmount <= 0 {
			zero := 0
			remainingPercent = &zero
		}
		buckets = append(buckets, authFileBatchCheckWindow{
			ID:               modelID,
			Label:            modelID,
			RemainingPercent: remainingPercent,
			RemainingAmount:  remainingAmount,
			ResetTime:        strings.TrimSpace(firstNonEmptyGJSON(bucket, "resetTime", "reset_time")),
			TokenType:        strings.TrimSpace(firstNonEmptyGJSON(bucket, "tokenType", "token_type")),
			ModelIDs:         []string{modelID},
		})
	}
	return buckets
}

func extractGeminiCLICreditBalance(payload gjson.Result) *int {
	total := 0
	found := false
	for _, path := range []string{"paidTier.availableCredits", "paid_tier.available_credits", "currentTier.availableCredits", "current_tier.available_credits"} {
		for _, credit := range payload.Get(path).Array() {
			if strings.TrimSpace(firstNonEmptyGJSON(credit, "creditType", "credit_type")) != "GOOGLE_ONE_AI" {
				continue
			}
			amount := intPtrFromGJSON(credit, "creditAmount", "credit_amount")
			if amount == nil {
				continue
			}
			total += *amount
			found = true
		}
	}
	if !found {
		return nil
	}
	return &total
}

func extractKimiBatchCheckRows(payload gjson.Result) []authFileBatchCheckWindow {
	rows := make([]authFileBatchCheckWindow, 0)
	if usage := payload.Get("usage"); usage.Exists() {
		if row := buildKimiBatchCheckRow("summary", "weekly_limit", usage); row != nil {
			rows = append(rows, *row)
		}
	}
	for index, item := range payload.Get("limits").Array() {
		detail := item.Get("detail")
		if !detail.Exists() {
			detail = item
		}
		label := strings.TrimSpace(firstNonEmptyGJSON(detail, "name", "title"))
		if label == "" {
			label = fmt.Sprintf("limit_%d", index+1)
		}
		row := buildKimiBatchCheckRow(fmt.Sprintf("limit-%d", index), label, detail)
		if row == nil {
			continue
		}
		if row.ResetHint == "" {
			row.ResetHint = kimiBatchCheckResetHint(item.Get("window"))
		}
		rows = append(rows, *row)
	}
	return rows
}

func buildKimiBatchCheckRow(id, label string, payload gjson.Result) *authFileBatchCheckWindow {
	limit := intPtrFromGJSON(payload, "limit")
	used := intPtrFromGJSON(payload, "used")
	if used == nil {
		remaining := intPtrFromGJSON(payload, "remaining")
		if limit != nil && remaining != nil {
			value := *limit - *remaining
			used = &value
		}
	}
	if limit == nil && used == nil {
		return nil
	}

	var remainingPercent *int
	if limit != nil && *limit > 0 {
		usedValue := 0
		if used != nil {
			usedValue = *used
		}
		value := maxInt(0, minInt(100, int(float64((*limit-usedValue)*100)/float64(*limit)+0.5)))
		remainingPercent = &value
	} else if used != nil && *used > 0 {
		zero := 0
		remainingPercent = &zero
	}

	return &authFileBatchCheckWindow{
		ID:               id,
		Label:            label,
		Limit:            limit,
		Used:             used,
		RemainingPercent: remainingPercent,
		ResetHint:        kimiBatchCheckResetHint(payload),
	}
}

func kimiBatchCheckResetHint(payload gjson.Result) string {
	if !payload.Exists() {
		return ""
	}
	for _, key := range []string{"reset_at", "resetAt", "reset_time", "resetTime"} {
		value := strings.TrimSpace(payload.Get(key).String())
		if value == "" {
			continue
		}
		if ts, err := time.Parse(time.RFC3339Nano, value); err == nil {
			return durationHint(time.Until(ts))
		}
		if ts, err := time.Parse(time.RFC3339, value); err == nil {
			return durationHint(time.Until(ts))
		}
	}
	for _, key := range []string{"reset_in", "resetIn", "ttl"} {
		value := payload.Get(key).Int()
		if value > 0 {
			return durationHint(time.Duration(value) * time.Second)
		}
	}
	return ""
}

func extractAntigravityBatchCheckGroups(payload gjson.Result) []authFileBatchCheckWindow {
	models := payload.Get("models")
	if !models.Exists() {
		return nil
	}

	findModel := func(identifier string) *authFileBatchCheckWindow {
		direct := models.Get(identifier)
		if direct.Exists() {
			return antigravityWindowFromResult(identifier, direct)
		}
		var found *authFileBatchCheckWindow
		models.ForEach(func(key, value gjson.Result) bool {
			displayName := strings.TrimSpace(firstNonEmptyGJSON(value, "displayName", "display_name"))
			if strings.EqualFold(displayName, identifier) {
				found = antigravityWindowFromResult(key.String(), value)
				return false
			}
			return true
		})
		return found
	}

	groups := make([]authFileBatchCheckWindow, 0, len(antigravityBatchCheckGroups))
	for _, group := range antigravityBatchCheckGroups {
		matches := make([]authFileBatchCheckWindow, 0, len(group.Identifiers))
		for _, identifier := range group.Identifiers {
			if match := findModel(identifier); match != nil {
				matches = append(matches, *match)
			}
		}
		if len(matches) == 0 {
			continue
		}

		modelIDs := make([]string, 0, len(matches))
		var remaining *int
		resetTime := ""
		for _, match := range matches {
			modelIDs = append(modelIDs, match.ID)
			if match.RemainingPercent != nil {
				if remaining == nil || *match.RemainingPercent < *remaining {
					value := *match.RemainingPercent
					remaining = &value
				}
			}
			if resetTime == "" && match.ResetTime != "" {
				resetTime = match.ResetTime
			}
		}

		groups = append(groups, authFileBatchCheckWindow{
			ID:               group.ID,
			Label:            group.Label,
			RemainingPercent: remaining,
			ResetTime:        resetTime,
			ModelIDs:         modelIDs,
		})
	}
	return groups
}

func antigravityWindowFromResult(modelID string, payload gjson.Result) *authFileBatchCheckWindow {
	quotaInfo := payload.Get("quotaInfo")
	if !quotaInfo.Exists() {
		quotaInfo = payload.Get("quota_info")
	}
	remainingPercent := percentageFromFraction(float64PtrFromGJSON(quotaInfo, "remainingFraction", "remaining_fraction", "remaining"))
	resetTime := strings.TrimSpace(firstNonEmptyGJSON(quotaInfo, "resetTime", "reset_time"))
	if remainingPercent == nil && resetTime != "" {
		zero := 0
		remainingPercent = &zero
	}
	if remainingPercent == nil {
		return nil
	}
	return &authFileBatchCheckWindow{
		ID:               modelID,
		Label:            modelID,
		RemainingPercent: remainingPercent,
		ResetTime:        resetTime,
	}
}

func (h *Handler) executeBatchCheckAPICall(ctx context.Context, auth *coreauth.Auth, body apiCallRequest) (apiCallResponse, error) {
	if h != nil && h.apiCallExecutor != nil {
		return h.apiCallExecutor(ctx, auth, body)
	}
	if ctx == nil {
		ctx = context.Background()
	}

	method := strings.ToUpper(strings.TrimSpace(body.Method))
	if method == "" {
		return apiCallResponse{}, fmt.Errorf("missing method")
	}
	urlStr := strings.TrimSpace(body.URL)
	if urlStr == "" {
		return apiCallResponse{}, fmt.Errorf("missing url")
	}
	parsedURL, err := url.Parse(urlStr)
	if err != nil || parsedURL.Scheme == "" || parsedURL.Host == "" {
		return apiCallResponse{}, fmt.Errorf("invalid url")
	}

	headers := make(map[string]string, len(body.Header))
	for key, value := range body.Header {
		headers[key] = value
	}
	var token string
	var tokenResolved bool
	for key, value := range headers {
		if !strings.Contains(value, "$TOKEN$") {
			continue
		}
		if !tokenResolved {
			token, err = h.resolveTokenForAuth(ctx, auth)
			if err != nil {
				return apiCallResponse{}, fmt.Errorf("auth token refresh failed: %w", err)
			}
			tokenResolved = true
		}
		if token == "" {
			return apiCallResponse{}, fmt.Errorf("auth token not found")
		}
		headers[key] = strings.ReplaceAll(value, "$TOKEN$", token)
	}

	var requestBody io.Reader
	if body.Data != "" {
		requestBody = strings.NewReader(body.Data)
	}
	req, err := http.NewRequestWithContext(ctx, method, urlStr, requestBody)
	if err != nil {
		return apiCallResponse{}, fmt.Errorf("failed to build request: %w", err)
	}
	for key, value := range headers {
		if strings.EqualFold(key, "host") {
			req.Host = strings.TrimSpace(value)
			continue
		}
		req.Header.Set(key, value)
	}

	httpClient := &http.Client{
		Timeout:   defaultAPICallTimeout,
		Transport: h.apiCallTransport(auth),
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return apiCallResponse{}, fmt.Errorf("request failed: %w", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			log.WithError(closeErr).Warn("failed to close batch check response body")
		}
	}()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return apiCallResponse{}, fmt.Errorf("failed to read response: %w", err)
	}
	return apiCallResponse{
		StatusCode: resp.StatusCode,
		Header:     resp.Header,
		Body:       string(bodyBytes),
	}, nil
}

func minRemainingFromWindows(windows []authFileBatchCheckWindow) *int {
	var remaining *int
	for _, window := range windows {
		if window.RemainingPercent == nil {
			continue
		}
		if remaining == nil || *window.RemainingPercent < *remaining {
			value := *window.RemainingPercent
			remaining = &value
		}
	}
	return remaining
}

func remainingPercentFromUsedPercent(usedPercent *int) *int {
	if usedPercent == nil {
		return nil
	}
	value := maxInt(0, minInt(100, 100-*usedPercent))
	return &value
}

func percentageFromFraction(value *float64) *int {
	if value == nil {
		return nil
	}
	normalized := *value
	if normalized < 0 {
		normalized = 0
	}
	if normalized > 1 {
		normalized = 1
	}
	percentage := int(normalized*100 + 0.5)
	return &percentage
}

func durationHint(duration time.Duration) string {
	if duration <= 0 {
		return ""
	}
	totalMinutes := int(duration / time.Minute)
	hours := totalMinutes / 60
	minutes := totalMinutes % 60
	switch {
	case hours > 0 && minutes > 0:
		return fmt.Sprintf("%dh %dm", hours, minutes)
	case hours > 0:
		return fmt.Sprintf("%dh", hours)
	case minutes > 0:
		return fmt.Sprintf("%dm", minutes)
	default:
		return "<1m"
	}
}

func firstNonEmptyGJSON(result gjson.Result, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(result.Get(key).String()); value != "" {
			return value
		}
	}
	return ""
}

func intPtrFromGJSON(result gjson.Result, keys ...string) *int {
	for _, key := range keys {
		value := result.Get(key)
		if value.Exists() {
			converted := int(value.Int())
			return &converted
		}
	}
	return nil
}

func int64PtrFromGJSON(result gjson.Result, keys ...string) *int64 {
	for _, key := range keys {
		value := result.Get(key)
		if value.Exists() {
			converted := value.Int()
			return &converted
		}
	}
	return nil
}

func float64PtrFromGJSON(result gjson.Result, keys ...string) *float64 {
	for _, key := range keys {
		value := result.Get(key)
		if value.Exists() {
			converted := value.Float()
			return &converted
		}
	}
	return nil
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

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}
