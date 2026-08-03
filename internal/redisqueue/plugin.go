package redisqueue

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	internallogging "github.com/router-for-me/CLIProxyAPI/v7/internal/logging"
	internalusage "github.com/router-for-me/CLIProxyAPI/v7/internal/usage"
	coreusage "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
)

func init() {
	coreusage.RegisterPlugin(&usageQueuePlugin{})
}

type usageQueuePlugin struct{}

func (p *usageQueuePlugin) HandleUsage(ctx context.Context, record coreusage.Record) {
	if p == nil {
		return
	}
	if !Enabled() || !UsageStatisticsEnabled() {
		return
	}

	timestamp := record.RequestedAt
	if timestamp.IsZero() {
		timestamp = time.Now()
	}

	detail := internalusage.CanonicalRequestDetail(ctx, record)
	reasoningEffort := strings.TrimSpace(record.ReasoningEffort)
	if reasoningEffort == "" {
		reasoningEffort = coreusage.ReasoningEffortFromContext(ctx)
	}
	serviceTier := strings.TrimSpace(record.ServiceTier)
	if serviceTier == "" {
		serviceTier = strings.TrimSpace(record.RequestServiceTier)
	}
	if serviceTier == "" {
		serviceTier = coreusage.ServiceTierFromContext(ctx)
	}
	responseServiceTier := strings.TrimSpace(record.ResponseServiceTier)
	clientRequestMetadata := internallogging.GetClientRequestMetadata(ctx)
	usageDetail := coreusage.EnsureTokenBreakdownForProvider(record.Detail, record.Provider, record.ExecutorType)
	if strings.TrimSpace(detail.ClientIP) == "" {
		detail.ClientIP = strings.TrimSpace(clientRequestMetadata.ClientIP)
	}
	failed := detail.Failed
	fail := resolveFail(ctx, record, failed)

	if detail.Timestamp.IsZero() {
		detail.Timestamp = timestamp
	}

	payload, err := json.Marshal(queuedUsageDetail{
		RequestDetail:       detail,
		Tokens:              queueTokenStats(detail.Tokens, usageDetail),
		APIKeyHash:          internalusage.APIKeyHash(record.APIKey),
		Alias:               detail.ModelAlias,
		TTFTMs:              normaliseDurationMillis(record.TTFT),
		Fail:                fail,
		ResponseHeaders:     sanitizeResponseHeaders(record.ResponseHeaders),
		AccountingVersion:   coreusage.TokenAccountingSchemaVersion,
		TokenBreakdown:      usageDetail.TokenBreakdown,
		XForwardedFor:       clientRequestMetadata.XForwardedFor,
		UserAgent:           clientRequestMetadata.UserAgent,
		ReasoningEffort:     reasoningEffort,
		ServiceTier:         serviceTier,
		ResponseServiceTier: responseServiceTier,
	})
	if err != nil {
		return
	}
	Enqueue(payload)
}

type queuedUsageDetail struct {
	internalusage.RequestDetail
	Tokens              tokenStats               `json:"tokens"`
	APIKeyHash          string                   `json:"api_key_hash,omitempty"`
	Alias               string                   `json:"alias,omitempty"`
	TTFTMs              int64                    `json:"ttft_ms"`
	Fail                failDetail               `json:"fail"`
	ResponseHeaders     http.Header              `json:"response_headers,omitempty"`
	ReasoningEffort     string                   `json:"reasoning_effort"`
	ServiceTier         string                   `json:"service_tier"`
	ResponseServiceTier string                   `json:"response_service_tier,omitempty"`
	AccountingVersion   int                      `json:"accounting_version"`
	TokenBreakdown      coreusage.TokenBreakdown `json:"token_breakdown"`
	XForwardedFor       string                   `json:"x_forwarded_for,omitempty"`
	UserAgent           string                   `json:"user_agent,omitempty"`
}

type tokenStats struct {
	InputTokens            int64  `json:"input_tokens"`
	OutputTokens           int64  `json:"output_tokens"`
	ReasoningTokens        int64  `json:"reasoning_tokens"`
	CachedTokens           int64  `json:"cached_tokens"`
	CacheReadTokens        int64  `json:"cache_read_tokens"`
	CacheReadTokensPresent bool   `json:"cache_read_tokens_present"`
	CacheCreationTokens    int64  `json:"cache_creation_tokens"`
	TotalTokens            int64  `json:"total_tokens"`
	ReportedTotalTokens    int64  `json:"reported_total_tokens"`
	ComputedTotalTokens    int64  `json:"computed_total_tokens"`
	TokenUsageSource       string `json:"token_usage_source"`
	CacheSplitStatus       string `json:"cache_split_status"`
	ReasoningCostMode      string `json:"reasoning_cost_mode"`
}

func queueTokenStats(tokens internalusage.RequestTokenStats, detail coreusage.Detail) tokenStats {
	return tokenStats{
		InputTokens:            detail.InputTokens,
		OutputTokens:           detail.OutputTokens,
		ReasoningTokens:        detail.ReasoningTokens,
		CachedTokens:           detail.CachedTokens,
		CacheReadTokens:        detail.CacheReadTokens,
		CacheReadTokensPresent: true,
		CacheCreationTokens:    detail.CacheCreationTokens,
		TotalTokens:            detail.TotalTokens,
		ReportedTotalTokens:    tokens.ReportedTotalTokens,
		ComputedTotalTokens:    tokens.ComputedTotalTokens,
		TokenUsageSource:       tokens.TokenUsageSource,
		CacheSplitStatus:       tokens.CacheSplitStatus,
		ReasoningCostMode:      tokens.ReasoningCostMode,
	}
}

type failDetail struct {
	StatusCode int    `json:"status_code"`
	Body       string `json:"body"`
}

func resolveFail(ctx context.Context, record coreusage.Record, failed bool) failDetail {
	fail := failDetail{
		StatusCode: record.Fail.StatusCode,
		Body:       internalusage.SanitizeSensitiveText(record.Fail.Body),
	}
	if !failed {
		return failDetail{StatusCode: 200}
	}
	if fail.StatusCode <= 0 {
		fail.StatusCode = internallogging.GetResponseStatus(ctx)
	}
	if fail.StatusCode <= 0 {
		fail.StatusCode = 500
	}
	return fail
}

func resolveSuccess(ctx context.Context) bool {
	status := internallogging.GetResponseStatus(ctx)
	if status == 0 {
		return true
	}
	return status < httpStatusBadRequest
}

func sanitizeResponseHeaders(headers http.Header) http.Header {
	if len(headers) == 0 {
		return nil
	}
	sanitized := make(http.Header)
	for key, values := range headers {
		if !isAllowedUsageHeaderName(key) {
			continue
		}
		cleanValues := make([]string, 0, len(values))
		for _, value := range values {
			cleanValues = append(cleanValues, internalusage.SanitizeSensitiveText(value))
		}
		sanitized[key] = cleanValues
	}
	if len(sanitized) == 0 {
		return nil
	}
	return sanitized
}

func isAllowedUsageHeaderName(name string) bool {
	normalized := strings.ToLower(strings.TrimSpace(name))
	if normalized == "" {
		return false
	}
	switch normalized {
	case "content-type", "date", "retry-after", "request-id", "x-request-id",
		"x-upstream-request-id", "openai-request-id", "anthropic-request-id",
		"traceparent",
		"ratelimit-limit", "ratelimit-remaining", "ratelimit-reset", "ratelimit-policy",
		"x-ratelimit-limit", "x-ratelimit-remaining", "x-ratelimit-reset",
		"x-ratelimit-limit-requests", "x-ratelimit-remaining-requests", "x-ratelimit-reset-requests",
		"x-ratelimit-limit-tokens", "x-ratelimit-remaining-tokens", "x-ratelimit-reset-tokens",
		"anthropic-ratelimit-requests-limit", "anthropic-ratelimit-requests-remaining", "anthropic-ratelimit-requests-reset",
		"anthropic-ratelimit-tokens-limit", "anthropic-ratelimit-tokens-remaining", "anthropic-ratelimit-tokens-reset",
		"anthropic-ratelimit-input-tokens-limit", "anthropic-ratelimit-input-tokens-remaining", "anthropic-ratelimit-input-tokens-reset",
		"anthropic-ratelimit-output-tokens-limit", "anthropic-ratelimit-output-tokens-remaining", "anthropic-ratelimit-output-tokens-reset":
		return true
	}
	return false
}

func normaliseDurationMillis(duration time.Duration) int64 {
	if duration <= 0 {
		return 0
	}
	return duration.Milliseconds()
}

const httpStatusBadRequest = 400
