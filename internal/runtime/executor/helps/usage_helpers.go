package helps

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	internallogging "github.com/router-for-me/CLIProxyAPI/v7/internal/logging"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/thinking"
	internalusage "github.com/router-for-me/CLIProxyAPI/v7/internal/usage"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

type UsageReporter struct {
	provider      string
	executorType  string
	model         string
	alias         string
	authID        string
	authIndex     string
	authType      string
	apiKey        string
	source        string
	reasoning     string
	serviceTier   string
	generate      bool
	requestedAt   time.Time
	ttftMu        sync.RWMutex
	ttft          time.Duration
	ttftStart     time.Time
	ttftSet       bool
	publishMu     sync.Mutex
	missingSent   bool
	factsSent     bool
	failureSent   bool
	additionalSeq atomic.Uint64
}

type usageExecutor interface {
	Identifier() string
}

func NewExecutorUsageReporter(ctx context.Context, executor usageExecutor, model string, auth *cliproxyauth.Auth) *UsageReporter {
	provider := ""
	if executor != nil {
		provider = executor.Identifier()
	}
	reporter := NewUsageReporter(ctx, provider, model, auth)
	reporter.executorType = ExecutorTypeName(executor)
	return reporter
}

func NewUsageReporter(ctx context.Context, provider, model string, auth *cliproxyauth.Auth) *UsageReporter {
	apiKey := APIKeyFromContext(ctx)
	alias := usage.RequestedModelAliasFromContext(ctx)
	if alias == "" {
		alias = model
	}
	reporter := &UsageReporter{
		provider:    provider,
		model:       model,
		alias:       strings.TrimSpace(alias),
		requestedAt: time.Now(),
		apiKey:      apiKey,
		source:      resolveUsageSource(auth, apiKey),
		authType:    resolveUsageAuthType(auth),
		reasoning:   usage.ReasoningEffortFromContext(ctx),
		serviceTier: usage.ServiceTierFromContext(ctx),
		generate:    usage.GenerateFromContext(ctx),
	}
	if auth != nil {
		reporter.authID = auth.ID
		reporter.authIndex = auth.EnsureIndex()
	}
	return reporter
}

func ExecutorTypeName(executor any) string {
	if executor == nil {
		return ""
	}
	executorType := reflect.TypeOf(executor)
	for executorType.Kind() == reflect.Pointer {
		executorType = executorType.Elem()
	}
	return strings.TrimSpace(executorType.Name())
}

func (r *UsageReporter) Publish(ctx context.Context, detail usage.Detail) {
	r.publishWithOutcome(ctx, detail, false, false, usage.Failure{})
}

// PublishObserved emits usage that was explicitly present in a provider response.
func (r *UsageReporter) PublishObserved(ctx context.Context, detail usage.Detail) {
	r.publishWithOutcome(ctx, detail, true, false, usage.Failure{})
}

// PublishParsed emits observed provider usage or a missing-usage terminal record.
func (r *UsageReporter) PublishParsed(ctx context.Context, detail usage.Detail, observed bool) {
	if observed {
		r.PublishObserved(ctx, detail)
		return
	}
	if hasUsageDetailMetadata(detail) {
		r.publishWithOutcome(ctx, detail, false, false, usage.Failure{})
		return
	}
	r.EnsurePublished(ctx)
}

func (r *UsageReporter) PublishAdditionalModel(ctx context.Context, model string, detail usage.Detail) {
	record, ok := r.buildAdditionalModelRecord(model, detail)
	if !ok {
		return
	}
	sequence := r.additionalSeq.Add(1)
	ctx = internallogging.WithUsageDetailRole(ctx, "additional")
	ctx = internallogging.WithUsageDetailSequence(ctx, strconv.FormatUint(sequence, 10))
	record.UsageObserved = true
	r.publishRecord(ctx, record)
}

func (r *UsageReporter) SetTranslatedReasoningEffort(payload []byte, format string) {
	if r == nil {
		return
	}
	r.reasoning = thinking.ExtractTranslatedReasoningEffort(payload, format)
}

func (r *UsageReporter) TrackHTTPClient(client *http.Client) *http.Client {
	if r == nil || client == nil {
		return client
	}
	tracked := *client
	transport := tracked.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	tracked.Transport = usageTTFTRoundTripper{
		base:     transport,
		reporter: r,
	}
	return &tracked
}

func (r *UsageReporter) ObserveResponse(resp *http.Response) {
	if r == nil || resp == nil || resp.Body == nil {
		return
	}
	r.StartResponseTTFT()
	resp.Body = &usageTTFTReadCloser{
		ReadCloser: resp.Body,
		mark: func() {
			r.MarkFirstResponseByte()
		},
	}
}

func (r *UsageReporter) StartResponseTTFT() {
	if r == nil {
		return
	}
	r.ttftMu.Lock()
	if !r.ttftSet && r.ttftStart.IsZero() {
		r.ttftStart = time.Now()
	}
	r.ttftMu.Unlock()
}

func (r *UsageReporter) MarkFirstResponseByte() {
	if r == nil {
		return
	}
	r.ttftMu.Lock()
	if r.ttftSet {
		r.ttftMu.Unlock()
		return
	}
	start := r.ttftStart
	r.ttftStart = time.Time{}
	r.ttftMu.Unlock()
	if start.IsZero() {
		return
	}
	r.setTTFT(time.Since(start))
}

func (r *UsageReporter) buildAdditionalModelRecord(model string, detail usage.Detail) (usage.Record, bool) {
	if r == nil {
		return usage.Record{}, false
	}
	model = strings.TrimSpace(model)
	if model == "" {
		return usage.Record{}, false
	}
	if !hasNonZeroTokenUsage(detail) {
		return usage.Record{}, false
	}
	return r.buildRecordForModel(model, detail, false, usage.Failure{}), true
}

func (r *UsageReporter) PublishFailure(ctx context.Context, errs ...error) {
	r.publishWithOutcome(ctx, usage.Detail{}, false, true, failFromErrors(errs...))
}

// PublishFailureWithUsage emits one final failed record while preserving partial usage facts.
func (r *UsageReporter) PublishFailureWithUsage(ctx context.Context, detail usage.Detail, errs ...error) {
	r.publishWithOutcome(ctx, detail, true, true, failFromErrors(errs...))
}

func (r *UsageReporter) TrackFailure(ctx context.Context, errPtr *error) {
	if r == nil || errPtr == nil {
		return
	}
	if *errPtr != nil {
		r.PublishFailure(ctx, *errPtr)
	}
}

func (r *UsageReporter) publishWithOutcome(ctx context.Context, detail usage.Detail, observed bool, failed bool, fail usage.Failure) {
	if r == nil {
		return
	}
	detail, failed, fail, ok := r.claimPublish(detail, observed, failed, fail)
	if !ok {
		return
	}
	record := r.buildRecord(detail, failed, fail)
	record.UsageObserved = observed
	r.publishRecord(ctx, record)
}

func hasNonZeroTokenUsage(detail usage.Detail) bool {
	return detail.InputTokens != 0 ||
		detail.OutputTokens != 0 ||
		detail.ReasoningTokens != 0 ||
		detail.CachedTokens != 0 ||
		detail.CacheReadTokens != 0 ||
		detail.CacheCreationTokens != 0 ||
		detail.TotalTokens != 0
}

func hasUsageDetailMetadata(detail usage.Detail) bool {
	return strings.TrimSpace(detail.ResponseServiceTier) != ""
}

// EnsurePublished emits a missing-usage record only when no terminal record won.
func (r *UsageReporter) EnsurePublished(ctx context.Context) {
	if r == nil {
		return
	}
	detail, failed, fail, ok := r.claimPublish(usage.Detail{}, false, false, usage.Failure{})
	if !ok {
		return
	}
	r.publishRecord(ctx, r.buildRecord(detail, failed, fail))
}

func (r *UsageReporter) claimPublish(detail usage.Detail, observed bool, failed bool, fail usage.Failure) (usage.Detail, bool, usage.Failure, bool) {
	hasFacts := observed || hasNonZeroTokenUsage(detail) || hasUsageDetailMetadata(detail)
	r.publishMu.Lock()
	defer r.publishMu.Unlock()
	switch {
	case hasFacts:
		if r.factsSent || r.missingSent || r.failureSent {
			return usage.Detail{}, false, usage.Failure{}, false
		}
		r.factsSent = true
		if failed {
			r.failureSent = true
		}
		return detail, failed, fail, true
	case failed:
		if r.factsSent || r.missingSent || r.failureSent {
			return usage.Detail{}, false, usage.Failure{}, false
		}
		r.failureSent = true
		return usage.Detail{}, true, fail, true
	default:
		if r.factsSent || r.missingSent || r.failureSent {
			return usage.Detail{}, false, usage.Failure{}, false
		}
		r.missingSent = true
		return usage.Detail{}, false, usage.Failure{}, true
	}
}

func (r *UsageReporter) publishRecord(ctx context.Context, record usage.Record) {
	record.ResponseHeaders = internallogging.GetResponseHeaders(ctx)
	usage.PublishRecord(ctx, record)
}

func (r *UsageReporter) buildRecord(detail usage.Detail, failed bool, failures ...usage.Failure) usage.Record {
	var fail usage.Failure
	if len(failures) > 0 {
		fail = failures[0]
	}
	if r == nil {
		return usage.Record{Detail: detail, Failed: failed, Fail: fail, Generate: usage.GenerateFlag(true)}
	}
	return r.buildRecordForModel(r.model, detail, failed, fail)
}

func (r *UsageReporter) buildRecordForModel(model string, detail usage.Detail, failed bool, fail usage.Failure) usage.Record {
	if r == nil {
		return usage.Record{Model: model, Detail: detail, Failed: failed, Fail: fail, Generate: usage.GenerateFlag(true)}
	}
	return usage.Record{
		Provider:            r.provider,
		ExecutorType:        r.executorType,
		Model:               model,
		Alias:               r.alias,
		Source:              r.source,
		APIKey:              r.apiKey,
		AuthID:              r.authID,
		AuthIndex:           r.authIndex,
		AuthType:            r.authType,
		ReasoningEffort:     r.reasoning,
		ServiceTier:         r.serviceTier,
		ResponseServiceTier: strings.TrimSpace(detail.ResponseServiceTier),
		Generate:            usage.GenerateFlag(r.generate),
		RequestedAt:         r.requestedAt,
		Latency:             r.latency(),
		TTFT:                r.ttftDuration(),
		Failed:              failed,
		Fail:                fail,
		Detail:              detail,
	}
}

func failFromErrors(errs ...error) usage.Failure {
	for _, err := range errs {
		if err == nil {
			continue
		}
		fail := usage.Failure{
			Body: internalusage.SanitizeSensitiveText(err.Error()),
		}
		var se interface{ StatusCode() int }
		if errors.As(err, &se) && se != nil {
			fail.StatusCode = se.StatusCode()
		}
		return fail
	}
	return usage.Failure{}
}

func (r *UsageReporter) latency() time.Duration {
	if r == nil || r.requestedAt.IsZero() {
		return 0
	}
	latency := time.Since(r.requestedAt)
	if latency < 0 {
		return 0
	}
	return latency
}

func (r *UsageReporter) setTTFT(ttft time.Duration) {
	if r == nil {
		return
	}
	if ttft < 0 {
		ttft = 0
	}
	r.ttftMu.Lock()
	if r.ttftSet {
		r.ttftMu.Unlock()
		return
	}
	r.ttft = ttft
	r.ttftSet = true
	r.ttftStart = time.Time{}
	r.ttftMu.Unlock()
}

func (r *UsageReporter) ttftDuration() time.Duration {
	if r == nil {
		return 0
	}
	r.ttftMu.RLock()
	defer r.ttftMu.RUnlock()
	return r.ttft
}

type usageTTFTRoundTripper struct {
	base     http.RoundTripper
	reporter *UsageReporter
}

func (t usageTTFTRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	t.reporter.StartResponseTTFT()
	resp, errRoundTrip := t.base.RoundTrip(req)
	if errRoundTrip != nil {
		return resp, errRoundTrip
	}
	t.reporter.ObserveResponse(resp)
	return resp, nil
}

type usageTTFTReadCloser struct {
	io.ReadCloser
	once sync.Once
	mark func()
}

func (r *usageTTFTReadCloser) Read(p []byte) (int, error) {
	if r == nil || r.ReadCloser == nil {
		return 0, io.ErrClosedPipe
	}
	n, errRead := r.ReadCloser.Read(p)
	if n > 0 && r.mark != nil {
		r.once.Do(r.mark)
	}
	return n, errRead
}

func APIKeyFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	ginCtx, ok := ctx.Value("gin").(*gin.Context)
	if !ok || ginCtx == nil {
		return ""
	}
	if v, exists := ginCtx.Get("userApiKey"); exists {
		switch value := v.(type) {
		case string:
			return value
		case fmt.Stringer:
			return value.String()
		default:
			return fmt.Sprintf("%v", value)
		}
	}
	return ""
}

func resolveUsageSource(auth *cliproxyauth.Auth, ctxAPIKey string) string {
	if auth != nil {
		provider := strings.TrimSpace(auth.Provider)
		if strings.EqualFold(provider, "vertex") {
			if auth.Metadata != nil {
				if projectID, ok := auth.Metadata["project_id"].(string); ok {
					if trimmed := strings.TrimSpace(projectID); trimmed != "" {
						return trimmed
					}
				}
				if project, ok := auth.Metadata["project"].(string); ok {
					if trimmed := strings.TrimSpace(project); trimmed != "" {
						return trimmed
					}
				}
			}
		}
		if kind, value := auth.AccountInfo(); value != "" && !strings.EqualFold(kind, "api_key") {
			return strings.TrimSpace(value)
		}
		if auth.Metadata != nil {
			if email, ok := auth.Metadata["email"].(string); ok {
				if trimmed := strings.TrimSpace(email); trimmed != "" {
					return trimmed
				}
			}
		}
		if id := strings.TrimSpace(auth.ID); id != "" {
			return id
		}
		if index := strings.TrimSpace(auth.EnsureIndex()); index != "" {
			return index
		}
	}
	return ""
}

func resolveUsageAuthType(auth *cliproxyauth.Auth) string {
	if auth == nil {
		return ""
	}
	return auth.AuthKind()
}

// StreamUsageBuffer keeps the latest usage detail observed in a stream.
type StreamUsageBuffer struct {
	detail   usage.Detail
	ok       bool
	observed bool
}

var (
	openAIStreamUsageMarker       = []byte(`"usage"`)
	openAIStreamServiceTierMarker = []byte(`"service_tier"`)
)

// Observe records provider usage or response metadata, allowing the final stream usage to win.
func (b *StreamUsageBuffer) Observe(detail usage.Detail, observed bool) {
	if b == nil || (!observed && !hasUsageDetailMetadata(detail)) {
		return
	}
	responseServiceTier := strings.TrimSpace(detail.ResponseServiceTier)
	if responseServiceTier == "" || hasNonZeroTokenUsage(detail) {
		preservedTier := b.detail.ResponseServiceTier
		b.detail = detail
		if b.detail.ResponseServiceTier == "" {
			b.detail.ResponseServiceTier = preservedTier
		}
	} else {
		b.detail.ResponseServiceTier = responseServiceTier
	}
	b.ok = true
	b.observed = b.observed || observed
}

// ObserveOpenAIStream records response-tier state and the latest usage from an
// OpenAI-style stream while avoiding JSON parsing for irrelevant chunks.
func (b *StreamUsageBuffer) ObserveOpenAIStream(line []byte) {
	if b == nil {
		return
	}
	payload := jsonPayload(line)
	if len(payload) == 0 {
		return
	}

	hasUsageCandidate := bytes.Contains(payload, openAIStreamUsageMarker)
	needTier := b.detail.ResponseServiceTier == "" || hasUsageCandidate
	hasTierCandidate := needTier && bytes.Contains(payload, openAIStreamServiceTierMarker)
	if !hasUsageCandidate && !hasTierCandidate {
		return
	}
	if !gjson.ValidBytes(payload) {
		return
	}

	detail := usage.Detail{}
	usageOK := false
	if hasUsageCandidate {
		usageNode := gjson.GetBytes(payload, "usage")
		if hasOpenAIStyleUsageTokenFields(usageNode) {
			detail = parseOpenAIStyleUsageNode(usageNode)
			usageOK = true
		}
	}
	if hasTierCandidate {
		detail.ResponseServiceTier = extractResponseServiceTierFromValidJSON(payload)
	}
	b.Observe(detail, usageOK)
}

// Publish emits the latest observed usage detail, if any.
func (b *StreamUsageBuffer) Publish(ctx context.Context, reporter *UsageReporter) bool {
	if b == nil || !b.ok || reporter == nil {
		return false
	}
	reporter.PublishParsed(ctx, b.detail, b.observed)
	return true
}

// Finalize emits exactly one terminal stream outcome, preserving observed usage on failure.
func (b *StreamUsageBuffer) Finalize(ctx context.Context, reporter *UsageReporter, terminalErr error) {
	if reporter == nil {
		return
	}
	if terminalErr == nil && ctx != nil {
		terminalErr = ctx.Err()
	}
	if terminalErr != nil {
		if !b.PublishFailure(ctx, reporter, terminalErr) {
			reporter.PublishFailure(ctx, terminalErr)
		}
		return
	}
	if !b.Publish(ctx, reporter) {
		reporter.EnsurePublished(ctx)
	}
}

// PublishFailure emits the latest observed usage detail with a failed outcome.
func (b *StreamUsageBuffer) PublishFailure(ctx context.Context, reporter *UsageReporter, errs ...error) bool {
	if b == nil || !b.ok || reporter == nil {
		return false
	}
	reporter.publishWithOutcome(ctx, b.detail, b.observed, true, failFromErrors(errs...))
	return true
}

// Detail returns the latest observed usage detail.
func (b *StreamUsageBuffer) Detail() (usage.Detail, bool) {
	if b == nil || !b.ok {
		return usage.Detail{}, false
	}
	return b.detail, true
}

// GeminiStreamUsageAccumulator reconstructs Gemini SSE data lines across
// arbitrary transport chunk boundaries before observing usage metadata.
type GeminiStreamUsageAccumulator struct {
	pending         []byte
	discardingLine  bool
	maxPendingBytes int
}

const defaultGeminiStreamUsageMaxPendingBytes = 64 << 20

// Observe appends a transport chunk and observes every complete SSE data line.
func (a *GeminiStreamUsageAccumulator) Observe(chunk []byte, buffer *StreamUsageBuffer) {
	if a == nil || buffer == nil || len(chunk) == 0 {
		return
	}
	for len(chunk) > 0 {
		if a.discardingLine {
			lineEnd := bytes.IndexByte(chunk, '\n')
			if lineEnd < 0 {
				return
			}
			a.discardingLine = false
			chunk = chunk[lineEnd+1:]
			continue
		}

		lineEnd := bytes.IndexByte(chunk, '\n')
		if lineEnd >= 0 {
			if a.appendFragment(chunk[:lineEnd]) {
				a.observeLine(a.pending, buffer)
				a.pending = a.pending[:0]
			} else {
				a.pending = nil
			}
			chunk = chunk[lineEnd+1:]
			continue
		}

		if !a.appendFragment(chunk) {
			a.pending = nil
			a.discardingLine = true
			return
		}
		if geminiStreamUsageLineComplete(a.pending) {
			a.observeLine(a.pending, buffer)
			a.pending = a.pending[:0]
		}
		return
	}
}

// Flush observes any final unterminated SSE data line.
func (a *GeminiStreamUsageAccumulator) Flush(buffer *StreamUsageBuffer) {
	if a == nil || buffer == nil {
		return
	}
	if !a.discardingLine && len(a.pending) > 0 {
		a.observeLine(a.pending, buffer)
	}
	a.pending = nil
	a.discardingLine = false
}

func (a *GeminiStreamUsageAccumulator) appendFragment(fragment []byte) bool {
	limit := a.maxPendingBytes
	if limit <= 0 {
		limit = defaultGeminiStreamUsageMaxPendingBytes
	}
	if len(fragment) > limit-len(a.pending) {
		return false
	}
	a.pending = append(a.pending, fragment...)
	return true
}

func (a *GeminiStreamUsageAccumulator) observeLine(line []byte, buffer *StreamUsageBuffer) {
	line = bytes.TrimSuffix(line, []byte{'\r'})
	if detail, ok := ParseGeminiStreamUsage(line); ok {
		buffer.Observe(detail, true)
	}
}

func geminiStreamUsageLineComplete(line []byte) bool {
	trimmed := bytes.TrimSpace(line)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("[DONE]")) {
		return len(trimmed) > 0
	}
	if bytes.HasPrefix(trimmed, []byte("event:")) || bytes.HasPrefix(trimmed, []byte(":")) {
		return true
	}
	payload := jsonPayload(trimmed)
	return len(payload) > 0 && gjson.ValidBytes(payload)
}

func ParseCodexUsage(data []byte) (usage.Detail, bool) {
	responseServiceTier := extractResponseServiceTier(data)
	usageNode := gjson.ParseBytes(data).Get("response.usage")
	if !hasOpenAIStyleUsageTokenFields(usageNode) {
		if responseServiceTier == "" {
			return usage.Detail{}, false
		}
		return usage.Detail{ResponseServiceTier: responseServiceTier}, false
	}
	detail := parseOpenAIStyleUsageNode(usageNode)
	detail.ResponseServiceTier = responseServiceTier
	return detail, true
}

func ParseCodexImageToolUsage(data []byte) (usage.Detail, bool) {
	usageNode := gjson.ParseBytes(data).Get("response.tool_usage.image_gen")
	if !hasOpenAIStyleUsageTokenFields(usageNode) {
		return usage.Detail{}, false
	}
	return parseOpenAIStyleUsageNode(usageNode), true
}

func ParseOpenAIUsage(data []byte) (usage.Detail, bool) {
	responseServiceTier := extractResponseServiceTier(data)
	usageNode := gjson.ParseBytes(data).Get("usage")
	if !hasOpenAIStyleUsageTokenFields(usageNode) {
		return usage.Detail{ResponseServiceTier: responseServiceTier}, false
	}
	detail := parseOpenAIStyleUsageNode(usageNode)
	detail.ResponseServiceTier = responseServiceTier
	return detail, true
}

func hasOpenAIStyleUsageTokenFields(usageNode gjson.Result) bool {
	if !usageNode.Exists() || !usageNode.IsObject() {
		return false
	}
	return hasNumericUsageField(usageNode, "prompt_tokens") ||
		hasNumericUsageField(usageNode, "input_tokens") ||
		hasNumericUsageField(usageNode, "completion_tokens") ||
		hasNumericUsageField(usageNode, "output_tokens") ||
		hasNumericUsageField(usageNode, "total_tokens") ||
		hasNumericUsageField(usageNode, "prompt_tokens_details.cached_tokens") ||
		hasNumericUsageField(usageNode, "input_tokens_details.cached_tokens") ||
		hasNumericUsageField(usageNode, "prompt_tokens_details.cache_write_tokens") ||
		hasNumericUsageField(usageNode, "prompt_tokens_details.cache_creation_tokens") ||
		hasNumericUsageField(usageNode, "input_tokens_details.cache_write_tokens") ||
		hasNumericUsageField(usageNode, "input_tokens_details.cache_creation_tokens") ||
		hasNumericUsageField(usageNode, "completion_tokens_details.reasoning_tokens") ||
		hasNumericUsageField(usageNode, "output_tokens_details.reasoning_tokens")
}

func parseOpenAIStyleUsageNode(usageNode gjson.Result) usage.Detail {
	inputNode := usageNode.Get("prompt_tokens")
	if inputNode.Type != gjson.Number {
		inputNode = usageNode.Get("input_tokens")
	}
	outputNode := usageNode.Get("completion_tokens")
	if outputNode.Type != gjson.Number {
		outputNode = usageNode.Get("output_tokens")
	}
	detail := usage.Detail{
		InputTokens:  numericUsageValue(inputNode),
		OutputTokens: numericUsageValue(outputNode),
		TotalTokens:  numericUsageField(usageNode, "total_tokens"),
	}
	cached := usageNode.Get("prompt_tokens_details.cached_tokens")
	if cached.Type != gjson.Number {
		cached = usageNode.Get("input_tokens_details.cached_tokens")
	}
	if cached.Type == gjson.Number {
		detail.CachedTokens = numericUsageValue(cached)
		detail.CacheReadTokens = detail.CachedTokens
	}
	cacheCreation := firstNumericUsageNode(
		usageNode,
		"input_tokens_details.cache_creation_tokens",
		"input_tokens_details.cache_write_tokens",
		"prompt_tokens_details.cache_creation_tokens",
		"prompt_tokens_details.cache_write_tokens",
	)
	if cacheCreation.Type == gjson.Number {
		detail.CacheCreationTokens = numericUsageValue(cacheCreation)
	}
	reasoning := usageNode.Get("completion_tokens_details.reasoning_tokens")
	if reasoning.Type != gjson.Number {
		reasoning = usageNode.Get("output_tokens_details.reasoning_tokens")
	}
	if reasoning.Type == gjson.Number {
		detail.ReasoningTokens = numericUsageValue(reasoning)
	}
	return detail
}

func ParseOpenAIStreamUsage(line []byte) (usage.Detail, bool) {
	payload := jsonPayload(line)
	if len(payload) == 0 || !gjson.ValidBytes(payload) {
		return usage.Detail{}, false
	}
	responseServiceTier := extractResponseServiceTier(payload)
	usageNode := gjson.GetBytes(payload, "usage")
	if !hasOpenAIStyleUsageTokenFields(usageNode) {
		if responseServiceTier == "" {
			return usage.Detail{}, false
		}
		return usage.Detail{ResponseServiceTier: responseServiceTier}, false
	}
	detail := parseOpenAIStyleUsageNode(usageNode)
	detail.ResponseServiceTier = responseServiceTier
	return detail, true
}

func ParseClaudeUsage(data []byte) (usage.Detail, bool) {
	usageNode := gjson.ParseBytes(data).Get("usage")
	if !hasClaudeUsageTokenFields(usageNode) {
		return usage.Detail{}, false
	}
	return parseClaudeUsageNode(usageNode), true
}

func ParseClaudeStreamUsage(line []byte) (usage.Detail, bool) {
	payload := jsonPayload(line)
	if len(payload) == 0 || !gjson.ValidBytes(payload) {
		return usage.Detail{}, false
	}
	var detail usage.Detail
	found := false
	for _, path := range []string{"message.usage", "usage"} {
		usageNode := gjson.GetBytes(payload, path)
		if !hasClaudeUsageTokenFields(usageNode) {
			continue
		}
		detail = mergeClaudeStreamUsage(detail, parseClaudeUsageNode(usageNode))
		found = true
	}
	if !found {
		return usage.Detail{}, false
	}
	return detail, true
}

func hasClaudeUsageTokenFields(node gjson.Result) bool {
	if !node.Exists() || !node.IsObject() {
		return false
	}
	return hasNumericUsageField(node, "input_tokens") ||
		hasNumericUsageField(node, "output_tokens") ||
		hasNumericUsageField(node, "cache_read_input_tokens") ||
		hasNumericUsageField(node, "cache_creation_input_tokens")
}

// ClaudeStreamUsageBuffer merges cumulative usage facts spread across Claude SSE events.
type ClaudeStreamUsageBuffer struct {
	detail usage.Detail
	ok     bool
}

// Observe records usage from a Claude SSE line.
func (b *ClaudeStreamUsageBuffer) Observe(line []byte) {
	if b == nil {
		return
	}
	detail, ok := ParseClaudeStreamUsage(line)
	if !ok {
		return
	}
	b.detail = mergeClaudeStreamUsage(b.detail, detail)
	b.ok = true
}

// Publish emits the merged Claude stream usage once.
func (b *ClaudeStreamUsageBuffer) Publish(ctx context.Context, reporter *UsageReporter) bool {
	if b == nil || !b.ok || reporter == nil {
		return false
	}
	reporter.PublishObserved(ctx, b.detail)
	return true
}

// PublishFailure emits the merged Claude stream usage with a failed outcome.
func (b *ClaudeStreamUsageBuffer) PublishFailure(ctx context.Context, reporter *UsageReporter, errs ...error) bool {
	if b == nil || !b.ok || reporter == nil {
		return false
	}
	reporter.PublishFailureWithUsage(ctx, b.detail, errs...)
	return true
}

// Detail returns the merged Claude stream usage.
func (b *ClaudeStreamUsageBuffer) Detail() (usage.Detail, bool) {
	if b == nil || !b.ok {
		return usage.Detail{}, false
	}
	return b.detail, true
}

func mergeClaudeStreamUsage(current, incoming usage.Detail) usage.Detail {
	current.InputTokens = maxInt64(current.InputTokens, incoming.InputTokens)
	current.OutputTokens = maxInt64(current.OutputTokens, incoming.OutputTokens)
	current.ReasoningTokens = maxInt64(current.ReasoningTokens, incoming.ReasoningTokens)
	current.CachedTokens = maxInt64(current.CachedTokens, incoming.CachedTokens)
	current.CacheReadTokens = maxInt64(current.CacheReadTokens, incoming.CacheReadTokens)
	current.CacheCreationTokens = maxInt64(current.CacheCreationTokens, incoming.CacheCreationTokens)
	current.TotalTokens = maxInt64(current.TotalTokens, incoming.TotalTokens)
	return current
}

func maxInt64(left, right int64) int64 {
	if right > left {
		return right
	}
	return left
}

func parseClaudeUsageNode(usageNode gjson.Result) usage.Detail {
	cacheReadTokens := numericUsageField(usageNode, "cache_read_input_tokens")
	cacheCreationTokens := numericUsageField(usageNode, "cache_creation_input_tokens")
	detail := usage.Detail{
		InputTokens:         numericUsageField(usageNode, "input_tokens"),
		OutputTokens:        numericUsageField(usageNode, "output_tokens"),
		CachedTokens:        cacheReadTokens,
		CacheReadTokens:     cacheReadTokens,
		CacheCreationTokens: cacheCreationTokens,
	}
	if detail.CachedTokens == 0 {
		detail.CachedTokens = detail.CacheCreationTokens
	}
	return detail
}

func parseGeminiFamilyUsageDetail(node gjson.Result) usage.Detail {
	cachedTokens := numericUsageField(node, "cachedContentTokenCount")
	detail := usage.Detail{
		InputTokens:     numericUsageField(node, "promptTokenCount"),
		OutputTokens:    numericUsageField(node, "candidatesTokenCount"),
		ReasoningTokens: numericUsageField(node, "thoughtsTokenCount"),
		TotalTokens:     numericUsageField(node, "totalTokenCount"),
		CachedTokens:    cachedTokens,
		CacheReadTokens: cachedTokens,
	}
	return detail
}

func parseInteractionsUsageDetail(node gjson.Result) usage.Detail {
	cacheRead := firstNumericUsageNode(node, "cache_read_tokens", "cacheReadTokens")
	detail := usage.Detail{
		InputTokens:         firstNumericUsageNode(node, "input_tokens", "prompt_tokens", "total_input_tokens").Int(),
		OutputTokens:        firstNumericUsageNode(node, "output_tokens", "completion_tokens", "total_output_tokens").Int(),
		ReasoningTokens:     firstNumericUsageNode(node, "reasoning_tokens", "thoughtsTokenCount", "total_thought_tokens").Int(),
		TotalTokens:         firstNumericUsageNode(node, "total_tokens", "totalTokenCount").Int(),
		CachedTokens:        firstNumericUsageNode(node, "cached_tokens", "cachedContentTokenCount", "total_cached_tokens").Int(),
		CacheReadTokens:     cacheRead.Int(),
		CacheCreationTokens: firstNumericUsageNode(node, "cache_creation_tokens", "cacheCreationTokens", "cache_write_tokens", "cacheWriteTokens").Int(),
	}
	if cacheRead.Type != gjson.Number && detail.CachedTokens > 0 {
		detail.CacheReadTokens = detail.CachedTokens
	}
	return detail
}

func ParseInteractionsUsage(data []byte) (usage.Detail, bool) {
	root := gjson.ParseBytes(data)
	responseServiceTier := extractResponseServiceTier(data)
	var node gjson.Result
	for _, path := range []string{"usage", "total_usage", "metadata.total_usage", "metadata.usage", "usageMetadata", "usage_metadata", "interaction.usage", "interaction.total_usage", "interaction.metadata.total_usage"} {
		candidate := root.Get(path)
		if hasInteractionsUsageTokenFields(candidate) {
			node = candidate
			break
		}
	}
	if !node.Exists() {
		return usage.Detail{ResponseServiceTier: responseServiceTier}, false
	}
	var detail usage.Detail
	if hasNumericUsageField(node, "promptTokenCount") || hasNumericUsageField(node, "candidatesTokenCount") {
		detail = parseGeminiFamilyUsageDetail(node)
	} else {
		detail = parseInteractionsUsageDetail(node)
	}
	detail.ResponseServiceTier = responseServiceTier
	return detail, true
}

func hasInteractionsUsageTokenFields(node gjson.Result) bool {
	if !node.Exists() || !node.IsObject() {
		return false
	}
	for _, path := range []string{
		"input_tokens", "prompt_tokens", "total_input_tokens",
		"output_tokens", "completion_tokens", "total_output_tokens",
		"reasoning_tokens", "thoughtsTokenCount", "total_thought_tokens",
		"total_tokens", "totalTokenCount",
		"cached_tokens", "cachedContentTokenCount", "total_cached_tokens",
		"cache_read_tokens", "cacheReadTokens",
		"cache_creation_tokens", "cacheCreationTokens", "cache_write_tokens", "cacheWriteTokens",
		"promptTokenCount", "candidatesTokenCount",
	} {
		if hasNumericUsageField(node, path) {
			return true
		}
	}
	return false
}

func extractResponseServiceTier(payload []byte) string {
	if len(payload) == 0 || !gjson.ValidBytes(payload) {
		return ""
	}
	return extractResponseServiceTierFromValidJSON(payload)
}

func extractResponseServiceTierFromValidJSON(payload []byte) string {
	for _, path := range []string{"response.service_tier", "service_tier", "interaction.service_tier"} {
		if tier := strings.TrimSpace(gjson.GetBytes(payload, path).String()); tier != "" {
			return tier
		}
	}
	return ""
}

func ParseInteractionsStreamUsage(line []byte) (usage.Detail, bool) {
	payload := jsonPayload(line)
	if len(payload) == 0 {
		payload = line
	}
	if len(payload) == 0 || !gjson.ValidBytes(payload) {
		return usage.Detail{}, false
	}
	return ParseInteractionsUsage(payload)
}

func ParseGeminiUsage(data []byte) (usage.Detail, bool) {
	usageNode := gjson.ParseBytes(data)
	node := usageNode.Get("usageMetadata")
	if !hasGeminiFamilyUsageTokenFields(node) {
		node = usageNode.Get("usage_metadata")
	}
	if !hasGeminiFamilyUsageTokenFields(node) {
		return usage.Detail{}, false
	}
	return parseGeminiFamilyUsageDetail(node), true
}

func hasGeminiFamilyUsageTokenFields(node gjson.Result) bool {
	if !node.Exists() || !node.IsObject() {
		return false
	}
	return hasNumericUsageField(node, "promptTokenCount") ||
		hasNumericUsageField(node, "candidatesTokenCount") ||
		hasNumericUsageField(node, "thoughtsTokenCount") ||
		hasNumericUsageField(node, "totalTokenCount") ||
		hasNumericUsageField(node, "cachedContentTokenCount")
}

func hasNumericUsageField(node gjson.Result, path string) bool {
	return node.Get(path).Type == gjson.Number
}

func numericUsageField(node gjson.Result, path string) int64 {
	return numericUsageValue(node.Get(path))
}

func numericUsageValue(value gjson.Result) int64 {
	if value.Type != gjson.Number {
		return 0
	}
	return value.Int()
}

func ParseGeminiStreamUsage(line []byte) (usage.Detail, bool) {
	payload := jsonPayload(line)
	if len(payload) == 0 || !gjson.ValidBytes(payload) {
		return usage.Detail{}, false
	}
	node := gjson.GetBytes(payload, "usageMetadata")
	if !hasGeminiFamilyUsageTokenFields(node) {
		node = gjson.GetBytes(payload, "usage_metadata")
	}
	if !hasGeminiFamilyUsageTokenFields(node) {
		return usage.Detail{}, false
	}
	return parseGeminiFamilyUsageDetail(node), true
}

func firstNumericUsageNode(root gjson.Result, paths ...string) gjson.Result {
	for _, path := range paths {
		node := root.Get(path)
		if node.Type == gjson.Number {
			return node
		}
	}
	return gjson.Result{}
}

func ParseAntigravityUsage(data []byte) (usage.Detail, bool) {
	usageNode := gjson.ParseBytes(data)
	node := usageNode.Get("response.usageMetadata")
	if !hasGeminiFamilyUsageTokenFields(node) {
		node = usageNode.Get("usageMetadata")
	}
	if !hasGeminiFamilyUsageTokenFields(node) {
		node = usageNode.Get("usage_metadata")
	}
	if !hasGeminiFamilyUsageTokenFields(node) {
		return usage.Detail{}, false
	}
	return parseGeminiFamilyUsageDetail(node), true
}

func ParseAntigravityStreamUsage(line []byte) (usage.Detail, bool) {
	payload := jsonPayload(line)
	if len(payload) == 0 || !gjson.ValidBytes(payload) {
		return usage.Detail{}, false
	}
	node := gjson.GetBytes(payload, "response.usageMetadata")
	if !hasGeminiFamilyUsageTokenFields(node) {
		node = gjson.GetBytes(payload, "usageMetadata")
	}
	if !hasGeminiFamilyUsageTokenFields(node) {
		node = gjson.GetBytes(payload, "usage_metadata")
	}
	if !hasGeminiFamilyUsageTokenFields(node) {
		return usage.Detail{}, false
	}
	return parseGeminiFamilyUsageDetail(node), true
}

var stopChunkWithoutUsage sync.Map

func rememberStopWithoutUsage(traceID string) {
	stopChunkWithoutUsage.Store(traceID, struct{}{})
	time.AfterFunc(10*time.Minute, func() { stopChunkWithoutUsage.Delete(traceID) })
}

// FilterSSEUsageMetadata removes usageMetadata from SSE events that are not
// terminal (finishReason != "stop"). Stop chunks are left untouched. This
// function is shared between aistudio and antigravity executors.
func FilterSSEUsageMetadata(payload []byte) []byte {
	if len(payload) == 0 {
		return payload
	}

	lines := bytes.Split(payload, []byte("\n"))
	modified := false
	foundData := false
	for idx, line := range lines {
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) == 0 || !bytes.HasPrefix(trimmed, []byte("data:")) {
			continue
		}
		foundData = true
		dataIdx := bytes.Index(line, []byte("data:"))
		if dataIdx < 0 {
			continue
		}
		rawJSON := bytes.TrimSpace(line[dataIdx+5:])
		traceID := gjson.GetBytes(rawJSON, "traceId").String()
		if isStopChunkWithoutUsage(rawJSON) && traceID != "" {
			rememberStopWithoutUsage(traceID)
			continue
		}
		if traceID != "" {
			if _, ok := stopChunkWithoutUsage.Load(traceID); ok && hasUsageMetadata(rawJSON) {
				stopChunkWithoutUsage.Delete(traceID)
				continue
			}
		}

		cleaned, changed := StripUsageMetadataFromJSON(rawJSON)
		if !changed {
			continue
		}
		var rebuilt []byte
		rebuilt = append(rebuilt, line[:dataIdx]...)
		rebuilt = append(rebuilt, []byte("data:")...)
		if len(cleaned) > 0 {
			rebuilt = append(rebuilt, ' ')
			rebuilt = append(rebuilt, cleaned...)
		}
		lines[idx] = rebuilt
		modified = true
	}
	if !modified {
		if !foundData {
			// Handle payloads that are raw JSON without SSE data: prefix.
			trimmed := bytes.TrimSpace(payload)
			cleaned, changed := StripUsageMetadataFromJSON(trimmed)
			if !changed {
				return payload
			}
			return cleaned
		}
		return payload
	}
	return bytes.Join(lines, []byte("\n"))
}

// StripUsageMetadataFromJSON drops usageMetadata unless finishReason is present (terminal).
// It handles both formats:
// - Aistudio: candidates.0.finishReason
// - Antigravity: response.candidates.0.finishReason
func StripUsageMetadataFromJSON(rawJSON []byte) ([]byte, bool) {
	jsonBytes := bytes.TrimSpace(rawJSON)
	if len(jsonBytes) == 0 || !gjson.ValidBytes(jsonBytes) {
		return rawJSON, false
	}

	// Check for finishReason in both aistudio and antigravity formats
	finishReason := gjson.GetBytes(jsonBytes, "candidates.0.finishReason")
	if !finishReason.Exists() {
		finishReason = gjson.GetBytes(jsonBytes, "response.candidates.0.finishReason")
	}
	terminalReason := finishReason.Exists() && strings.TrimSpace(finishReason.String()) != ""

	usageMetadata := gjson.GetBytes(jsonBytes, "usageMetadata")
	if !usageMetadata.Exists() {
		usageMetadata = gjson.GetBytes(jsonBytes, "response.usageMetadata")
	}

	// Terminal chunk: keep as-is.
	if terminalReason {
		return rawJSON, false
	}

	// Nothing to strip
	if !usageMetadata.Exists() {
		return rawJSON, false
	}

	// Remove usageMetadata from both possible locations
	cleaned := jsonBytes
	var changed bool

	if usageMetadata = gjson.GetBytes(cleaned, "usageMetadata"); usageMetadata.Exists() {
		// Rename usageMetadata to cpaUsageMetadata in the message_start event of Claude
		cleaned, _ = sjson.SetRawBytes(cleaned, "cpaUsageMetadata", []byte(usageMetadata.Raw))
		cleaned, _ = sjson.DeleteBytes(cleaned, "usageMetadata")
		changed = true
	}

	if usageMetadata = gjson.GetBytes(cleaned, "response.usageMetadata"); usageMetadata.Exists() {
		// Rename usageMetadata to cpaUsageMetadata in the message_start event of Claude
		cleaned, _ = sjson.SetRawBytes(cleaned, "response.cpaUsageMetadata", []byte(usageMetadata.Raw))
		cleaned, _ = sjson.DeleteBytes(cleaned, "response.usageMetadata")
		changed = true
	}

	return cleaned, changed
}

func hasUsageMetadata(jsonBytes []byte) bool {
	if len(jsonBytes) == 0 || !gjson.ValidBytes(jsonBytes) {
		return false
	}
	if gjson.GetBytes(jsonBytes, "usageMetadata").Exists() {
		return true
	}
	if gjson.GetBytes(jsonBytes, "response.usageMetadata").Exists() {
		return true
	}
	return false
}

func isStopChunkWithoutUsage(jsonBytes []byte) bool {
	if len(jsonBytes) == 0 || !gjson.ValidBytes(jsonBytes) {
		return false
	}
	finishReason := gjson.GetBytes(jsonBytes, "candidates.0.finishReason")
	if !finishReason.Exists() {
		finishReason = gjson.GetBytes(jsonBytes, "response.candidates.0.finishReason")
	}
	trimmed := strings.TrimSpace(finishReason.String())
	if !finishReason.Exists() || trimmed == "" {
		return false
	}
	return !hasUsageMetadata(jsonBytes)
}

func JSONPayload(line []byte) []byte {
	return jsonPayload(line)
}

func jsonPayload(line []byte) []byte {
	trimmed := bytes.TrimSpace(line)
	if len(trimmed) == 0 {
		return nil
	}
	if bytes.Equal(trimmed, []byte("[DONE]")) {
		return nil
	}
	if bytes.HasPrefix(trimmed, []byte("event:")) {
		return nil
	}
	if bytes.HasPrefix(trimmed, []byte("data:")) {
		trimmed = bytes.TrimSpace(trimmed[len("data:"):])
	}
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return nil
	}
	return trimmed
}
