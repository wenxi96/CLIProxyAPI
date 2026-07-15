package usage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/mail"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/logging"
	coreusage "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
)

const (
	DetailRolePrimary    = "primary"
	DetailRoleAdditional = "additional"

	TokenUsageSourceProvider = "provider_usage"
	TokenUsageSourceMissing  = "missing_usage"

	CacheSplitNone            = "none"
	CacheSplitReadOnly        = "read_only"
	CacheSplitCreationOnly    = "creation_only"
	CacheSplitReadAndCreation = "read_and_creation"
	CacheSplitUnknown         = "unknown"

	ReasoningCostIncludedInOutput = "included_in_output"
	ReasoningCostSeparate         = "separate"
	ReasoningCostUnknown          = "unknown"
)

// CanonicalRequestDetail derives the persistent usage detail from a usage record.
func CanonicalRequestDetail(ctx context.Context, record coreusage.Record) RequestDetail {
	timestamp := record.RequestedAt
	if timestamp.IsZero() {
		timestamp = time.Now()
	}
	modelName := strings.TrimSpace(record.Model)
	if modelName == "" {
		modelName = "unknown"
	}
	alias := strings.TrimSpace(record.Alias)
	if alias == "" {
		alias = modelName
	}
	failed := record.Failed
	if !failed {
		failed = !resolveSuccess(ctx)
	}
	detail := RequestDetail{
		RequestID:      strings.TrimSpace(logging.GetRequestID(ctx)),
		ClientIP:       logging.ClientIPFromContext(ctx),
		Timestamp:      timestamp,
		Endpoint:       resolveEndpoint(ctx, record),
		Model:          modelName,
		Provider:       defaultIfEmpty(record.Provider, "unknown"),
		ExecutorType:   defaultIfEmpty(record.ExecutorType, "unknown"),
		AuthType:       defaultIfEmpty(record.AuthType, "unknown"),
		ModelAlias:     alias,
		Source:         safeSourceIdentifier(record.Source, record.AuthIndex),
		AuthIndex:      strings.TrimSpace(record.AuthIndex),
		DetailRole:     normalizeDetailRole(logging.GetUsageDetailRole(ctx)),
		DetailSequence: strings.TrimSpace(logging.GetUsageDetailSequence(ctx)),
		Failed:         failed,
		LatencyMs:      normaliseLatency(record.Latency),
		Tokens:         normaliseDetail(record.Detail, record.Provider),
	}
	if record.UsageObserved {
		detail.Tokens.TokenUsageSource = TokenUsageSourceProvider
	}
	if detail.Source == "" && detail.AuthIndex != "" {
		detail.Source = detail.AuthIndex
	}
	return normalizeRequestDetail(detail, record.Provider)
}

func normalizeRequestDetail(detail RequestDetail, provider string) RequestDetail {
	detail.EstimatedCostUSD = cloneFloat64Ptr(detail.EstimatedCostUSD)
	detail.RequestID = strings.TrimSpace(detail.RequestID)
	detail.ClientIP = strings.TrimSpace(detail.ClientIP)
	detail.Endpoint = sanitizeOutputIdentifier(detail.Endpoint)
	detail.Model = defaultIfEmpty(detail.Model, "unknown")
	detail.Provider = defaultIfEmpty(detail.Provider, defaultIfEmpty(provider, "unknown"))
	detail.ExecutorType = defaultIfEmpty(detail.ExecutorType, "unknown")
	detail.AuthType = defaultIfEmpty(detail.AuthType, "unknown")
	detail.ModelAlias = strings.TrimSpace(detail.ModelAlias)
	if detail.ModelAlias == "" {
		detail.ModelAlias = detail.Model
	}
	detail.AuthIndex = strings.TrimSpace(detail.AuthIndex)
	detail.Source = safeSourceIdentifier(detail.Source, detail.AuthIndex)
	if detail.Source == "" && detail.AuthIndex != "" {
		detail.Source = detail.AuthIndex
	}
	detail.DetailRole = normalizeDetailRole(detail.DetailRole)
	detail.DetailSequence = strings.TrimSpace(detail.DetailSequence)
	if detail.LatencyMs < 0 {
		detail.LatencyMs = 0
	}
	if detail.Timestamp.IsZero() {
		detail.Timestamp = time.Now()
	}
	detail.Tokens = normaliseRequestTokens(detail.Tokens, detail.Provider)
	return detail
}

func normalizeDetailRole(role string) string {
	role = strings.TrimSpace(role)
	if role == "" {
		return DetailRolePrimary
	}
	return role
}

func normaliseDetail(detail coreusage.Detail, provider string) RequestTokenStats {
	tokens := RequestTokenStats{
		InputTokens:         detail.InputTokens,
		OutputTokens:        detail.OutputTokens,
		ReasoningTokens:     detail.ReasoningTokens,
		CachedTokens:        detail.CachedTokens,
		CacheReadTokens:     detail.CacheReadTokens,
		CacheCreationTokens: detail.CacheCreationTokens,
		ReportedTotalTokens: detail.TotalTokens,
	}
	return normaliseRequestTokens(tokens, provider)
}

func normaliseRequestTokens(tokens RequestTokenStats, provider string) RequestTokenStats {
	legacyTotalTokens := clampNonNegative(tokens.TotalTokens)
	tokens.InputTokens = clampNonNegative(tokens.InputTokens)
	tokens.OutputTokens = clampNonNegative(tokens.OutputTokens)
	tokens.ReasoningTokens = clampNonNegative(tokens.ReasoningTokens)
	tokens.CachedTokens = clampNonNegative(tokens.CachedTokens)
	tokens.CacheReadTokens = clampNonNegative(tokens.CacheReadTokens)
	tokens.CacheCreationTokens = clampNonNegative(tokens.CacheCreationTokens)
	if splitCachedTokens := tokens.CacheReadTokens + tokens.CacheCreationTokens; splitCachedTokens > 0 {
		if tokens.CacheReadTokens > 0 && tokens.CacheCreationTokens > 0 {
			tokens.CachedTokens = splitCachedTokens
		} else if splitCachedTokens > tokens.CachedTokens {
			tokens.CachedTokens = splitCachedTokens
		}
	}
	tokens.ReportedTotalTokens = clampNonNegative(tokens.ReportedTotalTokens)
	if tokens.ReportedTotalTokens == 0 && tokens.ComputedTotalTokens == 0 && legacyTotalTokens > 0 {
		tokens.ReportedTotalTokens = legacyTotalTokens
	}

	tokens.CacheSplitStatus = cacheSplitStatus(tokens)
	tokens.ReasoningCostMode = reasoningCostMode(provider, tokens)
	tokens.ComputedTotalTokens = computedTotalTokens(tokens, provider)
	if tokens.ReportedTotalTokens > 0 {
		tokens.TotalTokens = tokens.ReportedTotalTokens
	} else if tokens.ComputedTotalTokens > 0 {
		tokens.TotalTokens = tokens.ComputedTotalTokens
	} else if legacyTotalTokens > 0 {
		tokens.ReportedTotalTokens = legacyTotalTokens
		tokens.TotalTokens = legacyTotalTokens
	} else {
		tokens.TotalTokens = 0
	}
	if hasTokenFacts(tokens) || strings.TrimSpace(tokens.TokenUsageSource) == TokenUsageSourceProvider {
		tokens.TokenUsageSource = TokenUsageSourceProvider
	} else {
		tokens.TokenUsageSource = TokenUsageSourceMissing
	}
	if tokens.CacheSplitStatus == "" {
		tokens.CacheSplitStatus = CacheSplitNone
	}
	if tokens.ReasoningCostMode == "" {
		tokens.ReasoningCostMode = ReasoningCostUnknown
	}
	return tokens
}

func computedTotalTokens(tokens RequestTokenStats, provider string) int64 {
	total := tokens.InputTokens + tokens.OutputTokens
	if shouldAddCacheSplitToComputedTotal(provider, tokens) {
		total += tokens.CacheReadTokens + tokens.CacheCreationTokens
	}
	if tokens.ReasoningTokens > 0 {
		switch tokens.ReasoningCostMode {
		case ReasoningCostSeparate:
			total += tokens.ReasoningTokens
		case ReasoningCostUnknown:
			if tokens.InputTokens == 0 && tokens.OutputTokens == 0 {
				total += tokens.ReasoningTokens
			}
		}
	}
	if total == 0 && tokens.CachedTokens > 0 {
		total = tokens.CachedTokens
	}
	return total
}

func shouldAddCacheSplitToComputedTotal(provider string, tokens RequestTokenStats) bool {
	if tokens.CacheReadTokens == 0 && tokens.CacheCreationTokens == 0 {
		return false
	}
	if tokens.InputTokens == 0 && tokens.OutputTokens == 0 {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(provider), "claude")
}

func cacheSplitStatus(tokens RequestTokenStats) string {
	switch {
	case tokens.CacheReadTokens > 0 && tokens.CacheCreationTokens > 0:
		return CacheSplitReadAndCreation
	case tokens.CacheReadTokens > 0:
		return CacheSplitReadOnly
	case tokens.CacheCreationTokens > 0:
		return CacheSplitCreationOnly
	case tokens.CachedTokens > 0:
		return CacheSplitUnknown
	default:
		return CacheSplitNone
	}
}

func reasoningCostMode(provider string, tokens RequestTokenStats) string {
	if tokens.ReasoningTokens <= 0 {
		return ReasoningCostUnknown
	}
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "gemini", "gemini-interactions", "vertex", "aistudio", "antigravity", "interactions":
		return ReasoningCostSeparate
	default:
		return ReasoningCostIncludedInOutput
	}
}

func requestTokenSummary(tokens RequestTokenStats, provider string) TokenStats {
	tokens = normaliseRequestTokens(tokens, provider)
	return TokenStats{
		InputTokens:     tokens.InputTokens,
		OutputTokens:    tokens.OutputTokens,
		ReasoningTokens: tokens.ReasoningTokens,
		CachedTokens:    tokens.CachedTokens,
		TotalTokens:     tokens.TotalTokens,
	}
}

func addRequestTokens(left TokenStats, right RequestTokenStats, provider string) TokenStats {
	summary := requestTokenSummary(right, provider)
	return TokenStats{
		InputTokens:     left.InputTokens + summary.InputTokens,
		OutputTokens:    left.OutputTokens + summary.OutputTokens,
		ReasoningTokens: left.ReasoningTokens + summary.ReasoningTokens,
		CachedTokens:    left.CachedTokens + summary.CachedTokens,
		TotalTokens:     left.TotalTokens + summary.TotalTokens,
	}
}

func hasTokenFacts(tokens RequestTokenStats) bool {
	return tokens.InputTokens != 0 ||
		tokens.OutputTokens != 0 ||
		tokens.ReasoningTokens != 0 ||
		tokens.CachedTokens != 0 ||
		tokens.CacheReadTokens != 0 ||
		tokens.CacheCreationTokens != 0 ||
		tokens.ReportedTotalTokens != 0 ||
		tokens.ComputedTotalTokens != 0 ||
		tokens.TotalTokens != 0
}

func tokenFactScore(tokens RequestTokenStats) int {
	score := 0
	for _, value := range []int64{
		tokens.InputTokens,
		tokens.OutputTokens,
		tokens.ReasoningTokens,
		tokens.CachedTokens,
		tokens.CacheReadTokens,
		tokens.CacheCreationTokens,
		tokens.ReportedTotalTokens,
		tokens.ComputedTotalTokens,
		tokens.TotalTokens,
	} {
		if value > 0 {
			score++
		}
	}
	if tokens.TokenUsageSource == TokenUsageSourceProvider {
		score++
	}
	if tokens.CacheSplitStatus != "" && tokens.CacheSplitStatus != CacheSplitNone {
		score++
	}
	if tokens.ReasoningCostMode != "" && tokens.ReasoningCostMode != ReasoningCostUnknown {
		score++
	}
	return score
}

func detailFactsHash(detail RequestDetail) string {
	tokens := normaliseRequestTokens(detail.Tokens, detail.Provider)
	return fmt.Sprintf(
		"%d|%d|%d|%d|%d|%d|%d|%d|%d|%s|%s|%s|%s",
		tokens.InputTokens,
		tokens.OutputTokens,
		tokens.ReasoningTokens,
		tokens.CachedTokens,
		tokens.CacheReadTokens,
		tokens.CacheCreationTokens,
		tokens.TotalTokens,
		tokens.ReportedTotalTokens,
		tokens.ComputedTotalTokens,
		tokens.TokenUsageSource,
		tokens.CacheSplitStatus,
		tokens.ReasoningCostMode,
		safeSourceIdentifier(detail.Source, detail.AuthIndex),
	)
}

func detailIdentityKey(apiName, modelName string, detail RequestDetail) string {
	detail = normalizeRequestDetail(detail, detail.Provider)
	model := defaultIfEmpty(detail.Model, modelName)
	scope := strings.TrimSpace(detail.AuthIndex)
	if scope == "" {
		scope = detail.Source
	}
	if detail.RequestID != "" {
		sequence := detailIdentitySequence(detail)
		return strings.Join([]string{
			"request",
			detail.RequestID,
			detail.Timestamp.UTC().Format(time.RFC3339Nano),
			apiName,
			detail.Endpoint,
			detail.Provider,
			detail.ExecutorType,
			model,
			scope,
			detail.DetailRole,
			sequence,
		}, "|")
	}
	sequence := detailIdentitySequence(detail)
	return strings.Join([]string{
		"fallback",
		detail.Timestamp.UTC().Format(time.RFC3339Nano),
		defaultIfEmpty(detail.Endpoint, apiName),
		model,
		detail.Provider,
		detail.ExecutorType,
		scope,
		detail.ClientIP,
		detail.DetailRole,
		sequence,
	}, "|")
}

func detailIdentitySequence(detail RequestDetail) string {
	sequence := strings.TrimSpace(detail.DetailSequence)
	if sequence != "" {
		return sequence
	}
	if detail.DetailRole == DetailRoleAdditional && hasTokenFacts(detail.Tokens) {
		return "facts:" + detailFactsHash(detail)
	}
	return "-"
}

func shouldEnrichDetail(existing, incoming RequestDetail) bool {
	existing = normalizeRequestDetail(existing, existing.Provider)
	incoming = normalizeRequestDetail(incoming, incoming.Provider)
	if detailFactsHash(existing) == detailFactsHash(incoming) {
		return incoming.Failed && !existing.Failed || estimatedCostChanged(existing.EstimatedCostUSD, incoming.EstimatedCostUSD)
	}
	if !hasTokenFacts(existing.Tokens) && hasTokenFacts(incoming.Tokens) {
		return true
	}
	return tokenFactScore(incoming.Tokens) > tokenFactScore(existing.Tokens)
}

func estimatedCostChanged(existing, incoming *float64) bool {
	if incoming == nil {
		return false
	}
	return existing == nil || *existing != *incoming
}

func mergeEnrichedDetail(existing, incoming RequestDetail) RequestDetail {
	existing = normalizeRequestDetail(existing, existing.Provider)
	incoming = normalizeRequestDetail(incoming, incoming.Provider)
	merged := existing
	if incoming.RequestID != "" {
		merged.RequestID = incoming.RequestID
	}
	if incoming.ClientIP != "" {
		merged.ClientIP = incoming.ClientIP
	}
	if incoming.Endpoint != "" {
		merged.Endpoint = incoming.Endpoint
	}
	if incoming.Model != "" && incoming.Model != "unknown" {
		merged.Model = incoming.Model
	}
	if incoming.Provider != "" && incoming.Provider != "unknown" {
		merged.Provider = incoming.Provider
	}
	if incoming.ExecutorType != "" && incoming.ExecutorType != "unknown" {
		merged.ExecutorType = incoming.ExecutorType
	}
	if incoming.AuthType != "" && incoming.AuthType != "unknown" {
		merged.AuthType = incoming.AuthType
	}
	if incoming.ModelAlias != "" {
		merged.ModelAlias = incoming.ModelAlias
	}
	if incoming.Source != "" {
		merged.Source = incoming.Source
	}
	if incoming.AuthIndex != "" {
		merged.AuthIndex = incoming.AuthIndex
	}
	if incoming.DetailRole != "" {
		merged.DetailRole = incoming.DetailRole
	}
	if incoming.DetailSequence != "" {
		merged.DetailSequence = incoming.DetailSequence
	}
	if incoming.LatencyMs > 0 {
		merged.LatencyMs = incoming.LatencyMs
	}
	merged.Failed = existing.Failed || incoming.Failed
	if incoming.EstimatedCostUSD != nil {
		merged.EstimatedCostUSD = cloneFloat64Ptr(incoming.EstimatedCostUSD)
	}
	merged.Tokens = incoming.Tokens
	return normalizeRequestDetail(merged, merged.Provider)
}

func cloneFloat64Ptr(value *float64) *float64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func safeAPIIdentifier(ctx context.Context, record coreusage.Record, detail RequestDetail) string {
	if apiKeyHash := APIKeyHash(record.APIKey); apiKeyHash != "" {
		return apiKeyHash
	}
	if endpoint := strings.TrimSpace(detail.Endpoint); endpoint != "" {
		return sanitizeOutputIdentifier(endpoint)
	}
	if endpoint := strings.TrimSpace(resolveEndpoint(ctx, record)); endpoint != "" {
		return sanitizeOutputIdentifier(endpoint)
	}
	if provider := strings.TrimSpace(record.Provider); provider != "" {
		return sanitizeOutputIdentifier(provider)
	}
	return "unknown"
}

func safeImportedAPIName(apiName string, detail RequestDetail) string {
	apiName = strings.TrimSpace(apiName)
	if isRedactedHash(apiName) {
		return apiName
	}
	if endpoint := strings.TrimSpace(detail.Endpoint); endpoint != "" {
		if looksLikeEndpoint(endpoint) {
			return sanitizeOutputIdentifier(endpoint)
		}
		if provider := strings.TrimSpace(detail.Provider); provider != "" && strings.EqualFold(endpoint, provider) {
			return sanitizeOutputIdentifier(provider)
		}
		return redactedHash(endpoint)
	}
	if apiName == "" {
		return "unknown"
	}
	if provider := strings.TrimSpace(detail.Provider); provider != "" && strings.EqualFold(apiName, provider) {
		return sanitizeOutputIdentifier(provider)
	}
	if strings.EqualFold(apiName, "unknown") {
		return "unknown"
	}
	if isRedactedHash(apiName) {
		return apiName
	}
	if isSensitiveValue(apiName) {
		return redactedHash(apiName)
	}
	return redactedHash(apiName)
}

func safeImportedEndpoint(_ string, endpoint string) string {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint != "" {
		if looksLikeEndpoint(endpoint) {
			return sanitizeOutputIdentifier(endpoint)
		}
		return ""
	}
	return ""
}

func safeSourceIdentifier(source, authIndex string) string {
	source = strings.TrimSpace(source)
	authIndex = strings.TrimSpace(authIndex)
	if authIndex != "" {
		return authIndex
	}
	if source == "" {
		return ""
	}
	if isRedactedHash(source) || isEmailIdentifier(source) {
		return source
	}
	return redactedHash(source)
}

func isEmailIdentifier(value string) bool {
	address, err := mail.ParseAddress(value)
	return err == nil && strings.EqualFold(strings.TrimSpace(address.Address), strings.TrimSpace(value))
}

func sanitizeOutputIdentifier(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if isSensitiveValue(value) {
		return redactedHash(value)
	}
	return value
}

// SanitizeSensitiveText redacts common credential patterns from text intended
// for persisted diagnostics, queue payloads, or log summaries.
func SanitizeSensitiveText(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if sanitized, ok := sanitizeJSONSensitiveText(value); ok {
		return sanitized
	}
	for _, pattern := range sensitiveTextPatterns {
		value = pattern.ReplaceAllString(value, "[redacted]")
	}
	return value
}

func sanitizeJSONSensitiveText(value string) (string, bool) {
	var payload any
	if err := json.Unmarshal([]byte(value), &payload); err != nil {
		return "", false
	}
	sanitized := sanitizeJSONValue(payload)
	data, err := json.Marshal(sanitized)
	if err != nil {
		return "", false
	}
	return string(data), true
}

func sanitizeJSONValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, nested := range typed {
			if isSensitiveTextKey(key) {
				result[key] = "[redacted]"
				continue
			}
			result[key] = sanitizeJSONValue(nested)
		}
		return result
	case []any:
		result := make([]any, len(typed))
		for i, nested := range typed {
			result[i] = sanitizeJSONValue(nested)
		}
		return result
	case string:
		result := typed
		for _, pattern := range sensitiveTextPatterns {
			result = pattern.ReplaceAllString(result, "[redacted]")
		}
		return result
	default:
		return value
	}
}

func isSensitiveTextKey(key string) bool {
	normalized := strings.ToLower(strings.TrimSpace(key))
	normalized = strings.NewReplacer("-", "_", ".", "_", " ", "_").Replace(normalized)
	if normalized == "" {
		return false
	}
	switch normalized {
	case "authorization", "proxy_authorization", "cookie", "set_cookie",
		"api_key", "apikey", "secret", "token", "access_token",
		"refresh_token", "id_token", "password", "passphrase", "private_key",
		"client_secret", "credentials":
		return true
	}
	return strings.HasSuffix(normalized, "_token") ||
		strings.HasSuffix(normalized, "_secret") ||
		strings.HasSuffix(normalized, "_api_key") ||
		strings.HasSuffix(normalized, "_password") ||
		strings.HasSuffix(normalized, "_private_key")
}

var sensitiveTextPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(bearer|basic|digest)\s+[A-Za-z0-9._~+/=-]+`),
	regexp.MustCompile(`(?i)["']?(authorization|proxy-authorization|api[_-]?key|apikey|token|[A-Za-z0-9_-]+[-_]token|access[_-]?token|refresh[_-]?token|id[_-]?token|secret|cookie|signature|sig|credential|password|passphrase|private[_-]?key)["']?\s*[:=]\s*["']?[^"',\s}\]]+`),
	regexp.MustCompile(`sk-(?:proj-)?[A-Za-z0-9_-]+`),
	regexp.MustCompile(`sk_[A-Za-z0-9_-]+`),
	regexp.MustCompile(`sk-ant-[A-Za-z0-9_-]+`),
	regexp.MustCompile(`xai-[A-Za-z0-9_-]+`),
	regexp.MustCompile(`AIza[A-Za-z0-9_-]+`),
	regexp.MustCompile(`AKIA[0-9A-Z]+`),
}

func isSensitiveValue(value string) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return false
	}
	lower := strings.ToLower(trimmed)
	switch {
	case strings.HasPrefix(lower, "bearer "):
		return true
	case strings.HasPrefix(trimmed, "sk-"),
		strings.HasPrefix(trimmed, "sk_"),
		strings.HasPrefix(trimmed, "sk-proj-"),
		strings.HasPrefix(trimmed, "sk-ant-"),
		strings.HasPrefix(trimmed, "xai-"),
		strings.HasPrefix(trimmed, "AIza"),
		strings.HasPrefix(trimmed, "AKIA"):
		return true
	case strings.Contains(lower, "authorization"),
		strings.Contains(lower, "access_token"),
		strings.Contains(lower, "refresh_token"),
		strings.Contains(lower, "id_token"),
		strings.Contains(lower, "api_key"),
		strings.Contains(lower, "apikey"),
		strings.Contains(lower, "cookie"),
		strings.Contains(lower, "secret"),
		strings.Contains(lower, "password"),
		strings.Contains(lower, "passphrase"),
		strings.Contains(lower, "private_key"):
		return true
	case strings.Count(trimmed, ".") >= 2 && len(trimmed) > 80:
		return true
	case len(trimmed) > 96 && !strings.ContainsAny(trimmed, "/\\@ "):
		return true
	default:
		return false
	}
}

func redactedHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return "redacted:" + hex.EncodeToString(sum[:])[:16]
}

// APIKeyHash returns a stable non-reversible identifier for a downstream API key.
func APIKeyHash(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return redactedHash(value)
}

var redactedHashPattern = regexp.MustCompile(`^redacted:[0-9a-f]{16}$`)

func isRedactedHash(value string) bool {
	return redactedHashPattern.MatchString(strings.TrimSpace(value))
}

func resolveEndpoint(ctx context.Context, record coreusage.Record) string {
	if endpoint := strings.TrimSpace(logging.GetEndpoint(ctx)); endpoint != "" {
		return endpoint
	}
	if ctx != nil {
		if ginCtx, ok := ctx.Value("gin").(*gin.Context); ok && ginCtx != nil {
			path := ginCtx.FullPath()
			if path == "" && ginCtx.Request != nil && ginCtx.Request.URL != nil {
				path = ginCtx.Request.URL.Path
			}
			method := ""
			if ginCtx.Request != nil {
				method = ginCtx.Request.Method
			}
			path = strings.TrimSpace(path)
			method = strings.TrimSpace(method)
			if path != "" {
				if method != "" {
					return method + " " + path
				}
				return path
			}
		}
	}
	return ""
}

func looksLikeEndpoint(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	if strings.HasPrefix(value, "/") {
		return true
	}
	method, path, ok := strings.Cut(value, " ")
	if !ok || !strings.HasPrefix(strings.TrimSpace(path), "/") {
		return false
	}
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case "GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS":
		return true
	default:
		return false
	}
}

func defaultIfEmpty(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func clampNonNegative(value int64) int64 {
	if value < 0 {
		return 0
	}
	return value
}
