package usage

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"
)

const (
	BillablePolicyVersionV1     = "v1"
	ProjectionIdentityVersionV1 = "canonical_identity_seed_v1"
	ProjectionSortVersionV1     = "canonical_sort_v1"
	UsageSourceIDVersionV1      = "usage_source_id_v1"
	UsageSourceKeyVersionV1     = "usage_source_key_v1"
)

// BuildPriceKey mirrors the management center price-key normalization. It is
// intentionally limited to trimming and case folding; provider aliases belong
// to the client-side price catalog, not the usage projection.
func BuildPriceKey(provider, model string) string {
	provider = strings.ToLower(strings.TrimSpace(provider))
	model = strings.ToLower(strings.TrimSpace(model))
	if provider == "" {
		provider = "legacy"
	}
	if model == "" {
		model = "-"
	}
	return provider + ":" + model
}

// ProviderRawPresenceV1 infers the v1 provenance state from the persisted
// Provider sentinel. A literal unknown provider is intentionally treated as
// indeterminate because the legacy detail format cannot distinguish it from a
// missing provider.
func ProviderRawPresenceV1(provider string) string {
	if strings.TrimSpace(provider) == "" || strings.EqualFold(strings.TrimSpace(provider), "unknown") {
		return "indeterminate_legacy"
	}
	return "present"
}

// UsageSourceKeyV1 returns the stable, redacted source coordinate used by the
// projection. It never exposes the raw credential value.
func UsageSourceKeyV1(detail RequestDetail) string {
	return strings.TrimSpace(safeSourceIdentifier(detail.Source, detail.AuthIndex))
}

// UsageSourceIDV1 returns the public-safe source coordinate used by filters
// and posting lists. The tuple order is fixed and every value is normalized
// before RFC3986 encoding, so source keys remain display-safe but do not
// collapse distinct auth/executor coordinates.
func UsageSourceIDV1(detail RequestDetail) string {
	return "source:v1:" + strings.Join([]string{
		percentEncodeRFC3986(normalizedIdentityPart(detail.AuthType)),
		percentEncodeRFC3986(normalizedIdentityPart(detail.AuthIndex)),
		percentEncodeRFC3986(normalizedIdentityPart(detail.ExecutorType)),
		percentEncodeRFC3986(normalizedIdentityPart(UsageSourceKeyV1(detail))),
	}, ":")
}

func stableScopeV1(detail RequestDetail) string {
	return hex.EncodeToString(encodeLengthPrefixed(
		normalizedIdentityPart(detail.AuthType),
		normalizedIdentityPart(detail.AuthIndex),
		normalizedIdentityPart(UsageSourceKeyV1(detail)),
		normalizedIdentityPart(detail.ExecutorType),
	))
}

// SourceGroupKeyV1 identifies the ordinal allocator scope for a detail.
func SourceGroupKeyV1(detail RequestDetail) string {
	detail = normalizeRequestDetail(detail, detail.Provider)
	return hex.EncodeToString(encodeLengthPrefixed(
		stableScopeV1(detail),
		normalizedIdentityPart(detail.DetailRole),
		normalizedIdentityPart(detail.Endpoint),
	))
}

// canonicalEventCoordinateKey is the mutable-detail-independent lookup used
// to carry an existing immutable event identity across enrichment. A request
// id plus role/sequence is the strongest coordinate; legacy records without a
// request id use timestamp, endpoint and stable source scope instead.
func canonicalEventCoordinateKey(apiName string, detail RequestDetail) string {
	detail = normalizeRequestDetailPreserveTimestamp(detail, detail.Provider)
	coordinateAPI := canonicalCoordinateAPIName(apiName, detail)
	role := normalizedIdentityPart(detail.DetailRole)
	sequence := normalizedIdentityPart(detail.DetailSequence)
	timestamp := "zero-time"
	if !detail.Timestamp.IsZero() {
		timestamp = detail.Timestamp.UTC().Format(time.RFC3339Nano)
	}
	if detail.RequestID != "" {
		return "request-coordinate:" + hex.EncodeToString(encodeLengthPrefixed(
			coordinateAPI,
			detail.RequestID,
			timestamp,
			stableScopeV1(detail),
			role,
			sequence,
		))
	}
	return "fallback-coordinate:" + hex.EncodeToString(encodeLengthPrefixed(
		coordinateAPI,
		timestamp,
		detail.Endpoint,
		stableScopeV1(detail),
		detail.ClientIP,
		role,
		sequence,
	))
}

func admissionCoordinateKey(apiName string, detail RequestDetail, discriminator string) string {
	base := canonicalEventCoordinateKey(apiName, detail)
	if discriminator == "" {
		return base
	}
	return base + "\x00admission:" + strings.TrimSpace(discriminator)
}

// admissionIdentityReuseKey scopes identity reuse to the strongest available
// identity. Batch entries include their batch id and complete logical identity
// so only retries within one batch can reuse an identity; two different
// provider/model events sharing a mutable coordinate cannot be collapsed.
// Live entries with an explicit discriminator retain the coordinate-based
// enrichment key.
func admissionIdentityReuseKey(apiName string, detail RequestDetail, batchID, discriminator string) string {
	if strings.TrimSpace(batchID) != "" {
		return "batch:" + strings.TrimSpace(batchID) + ":" + detailIdentityKey(apiName, detail.Model, detail)
	}
	return admissionCoordinateKey(apiName, detail, discriminator)
}

// canonicalCoordinateAPIName keeps a real endpoint or redacted API bucket in
// the enrichment coordinate, but ignores the provider fallback used when a
// record has no endpoint or downstream API key. Provider/model changes in that
// fallback form are enrichment, while distinct API buckets remain distinct.
func canonicalCoordinateAPIName(apiName string, detail RequestDetail) string {
	apiName = strings.TrimSpace(apiName)
	if strings.TrimSpace(detail.Endpoint) == "" {
		provider := strings.TrimSpace(detail.Provider)
		if provider != "" && strings.EqualFold(apiName, provider) {
			return "provider-fallback"
		}
	}
	return apiName
}

// CanonicalIdentitySeedV1 returns the deterministic seed for an immutable event
// identity. The ordinal is allocated by the mutation coordinator in the live
// path; projection-only callers may use the in-memory allocator.
func CanonicalIdentitySeedV1(detail RequestDetail, sourceGroupOrdinal uint64) string {
	detail = normalizeRequestDetailPreserveTimestamp(detail, detail.Provider)
	zeroTime := "zero-time"
	timestamp := zeroTime
	if !detail.Timestamp.IsZero() {
		timestamp = detail.Timestamp.UTC().Format(time.RFC3339Nano)
	}
	encoded := encodeLengthPrefixed(
		detail.RequestID,
		detail.DetailRole,
		detail.DetailSequence,
		timestamp,
		detail.Endpoint,
		stableScopeV1(detail),
		detail.ExecutorType,
		detail.Model,
		detail.Provider,
	)
	encoded = appendLengthPrefixedBytes(encoded, []byte(formatUint(sourceGroupOrdinal)))
	hash := sha256.Sum256(append([]byte(ProjectionIdentityVersionV1+"\x00"), encoded...))
	return hex.EncodeToString(hash[:])
}

// CanonicalEventIdentityV1 derives an immutable event identity from its seed.
func CanonicalEventIdentityV1(seed string) string {
	hash := sha256.Sum256(append([]byte("canonical_event_identity_v1\x00"), []byte(strings.TrimSpace(seed))...))
	return hex.EncodeToString(hash[:])
}

// StableEventIDV1 is the public-safe locator used by posting lists and cursors.
func StableEventIDV1(canonicalEventIdentity string) string {
	identity := strings.TrimSpace(canonicalEventIdentity)
	if identity == "" {
		return ""
	}
	return "event:" + identity
}

// CanonicalSortKeyV1 encodes the fixed sort tuple used when assigning
// deterministic sequence values to legacy payloads.
func CanonicalSortKeyV1(apiName string, detail RequestDetail, sourcePayloadOrdinal uint64) []byte {
	detail = normalizeRequestDetailPreserveTimestamp(detail, detail.Provider)
	timestamp := "zero-time"
	if !detail.Timestamp.IsZero() {
		timestamp = detail.Timestamp.UTC().Format(time.RFC3339Nano)
	}
	canonicalBytes, err := json.Marshal(detail)
	if err != nil {
		canonicalBytes = []byte{}
	}
	fields := []string{
		ProjectionSortVersionV1,
		timestamp,
		detail.Endpoint,
		detail.Model,
		detail.Provider,
		detail.AuthIndex,
		UsageSourceKeyV1(detail),
		detail.DetailRole,
		detail.DetailSequence,
		detail.RequestID,
	}
	encoded := encodeLengthPrefixed(fields...)
	encoded = appendLengthPrefixedBytes(encoded, canonicalBytes)
	return appendLengthPrefixedBytes(encoded, []byte(formatUint(sourcePayloadOrdinal)))
}

func canonicalDetailBytes(detail RequestDetail) []byte {
	detail = normalizeRequestDetailPreserveTimestamp(detail, detail.Provider)
	data, err := json.Marshal(detail)
	if err != nil {
		return nil
	}
	return data
}

func normalizedIdentityPart(value string) string {
	value = asciiLower(strings.TrimSpace(value))
	if value == "" {
		return "-"
	}
	return value
}

func asciiLower(value string) string {
	bytes := []byte(value)
	for index, char := range bytes {
		if char >= 'A' && char <= 'Z' {
			bytes[index] = char + ('a' - 'A')
		}
	}
	return string(bytes)
}

func encodeLengthPrefixed(fields ...string) []byte {
	encoded := make([]byte, 0, len(fields)*8)
	for _, field := range fields {
		encoded = appendLengthPrefixedBytes(encoded, []byte(field))
	}
	return encoded
}

func appendLengthPrefixedBytes(dst, value []byte) []byte {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(value)))
	dst = append(dst, length[:]...)
	return append(dst, value...)
}

// GetBillableTokenComponents mirrors the v1 client-side token decomposition.
// In particular, unsplit cached_tokens are billable cache_read tokens, while a
// split cache remainder is tracked separately as unclassified cache.
func GetBillableTokenComponents(detail RequestDetail) BillableTokenComponents {
	tokens := normaliseRequestTokens(detail.Tokens, detail.Provider)
	splitCacheTokens := tokens.CacheReadTokens + tokens.CacheCreationTokens
	hasSplitCache := splitCacheTokens > 0
	cacheReadTokens := tokens.CachedTokens
	if hasSplitCache {
		cacheReadTokens = tokens.CacheReadTokens
	}
	unclassifiedCacheTokens := int64(0)
	if hasSplitCache && tokens.CachedTokens > splitCacheTokens {
		unclassifiedCacheTokens = tokens.CachedTokens - splitCacheTokens
	}
	cachedTokensForInput := tokens.CachedTokens
	inputTokens := tokens.InputTokens
	if !isCacheAdditiveProviderV1(detail.Provider) {
		inputTokens -= cachedTokensForInput
		if inputTokens < 0 {
			inputTokens = 0
		}
	}
	reasoningSeparate := tokens.ReasoningCostMode == ReasoningCostSeparate
	outputTokens := tokens.OutputTokens
	if outputTokens <= 0 && !reasoningSeparate {
		outputTokens = tokens.ReasoningTokens
	}
	reasoningTokens := int64(0)
	if reasoningSeparate {
		reasoningTokens = tokens.ReasoningTokens
	}
	return BillableTokenComponents{
		InputTokens:             inputTokens,
		OutputTokens:            outputTokens,
		ReasoningTokens:         reasoningTokens,
		CacheReadTokens:         cacheReadTokens,
		CacheCreationTokens:     tokens.CacheCreationTokens,
		UnclassifiedCacheTokens: unclassifiedCacheTokens,
	}
}

func isCacheAdditiveProviderV1(provider string) bool {
	provider = strings.ToLower(strings.TrimSpace(provider))
	return provider == "claude" || provider == "anthropic" ||
		strings.HasPrefix(provider, "claude-") || strings.HasPrefix(provider, "anthropic-")
}

func ClassifyDetailV1(detail RequestDetail) DetailClassification {
	tokens := normaliseRequestTokens(detail.Tokens, detail.Provider)
	if tokens.TokenUsageSource == TokenUsageSourceMissing {
		return DetailClassUnknownUsage
	}
	components := GetBillableTokenComponents(detail)
	if components.Total() == 0 {
		if tokens.TotalTokens > 0 {
			return DetailClassKnownTotalOnly
		}
		return DetailClassZeroBillableComplete
	}
	return DetailClassPriceable
}
