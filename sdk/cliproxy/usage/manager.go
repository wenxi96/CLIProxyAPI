package usage

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	log "github.com/sirupsen/logrus"
)

// DefaultServiceTier is retained for direct SDK and non-OpenAI usage callers.
const DefaultServiceTier = "default"

// AutoServiceTier is the OpenAI request semantics when service_tier is omitted.
// OpenAI HTTP handlers set it explicitly, without changing other providers'
// historical direct-SDK default.
const AutoServiceTier = "auto"

// Record contains the usage statistics captured for a single provider request.
type Record struct {
	Provider string
	// ExecutorType stores the concrete executor type that handled the request.
	ExecutorType string
	Model        string
	Alias        string
	APIKey       string
	AuthID       string
	AuthIndex    string
	AuthType     string
	Source       string
	// ReasoningEffort stores the translated upstream thinking level for request event logs.
	ReasoningEffort string
	// ServiceTier stores the client-requested service tier.
	ServiceTier string
	// RequestServiceTier is a deprecated input-only alias retained for existing
	// plugin callers. It is normalized into ServiceTier and never emitted.
	RequestServiceTier string
	// ResponseServiceTier stores the final tier reported by the upstream response.
	ResponseServiceTier string
	// Generate reports whether the client requested actual generation.
	// nil or true means generation is enabled; only an explicit false disables generation.
	// Use GenerateFlag to set the value and GenerateEnabled to read it with the default.
	Generate    *bool
	RequestedAt time.Time
	Latency     time.Duration
	TTFT        time.Duration
	Failed      bool
	Fail        Failure
	Detail      Detail
	// UsageObserved distinguishes an explicit provider-reported zero from missing usage.
	UsageObserved bool
	// ResponseHeaders stores a snapshot of upstream response headers for usage sinks.
	ResponseHeaders http.Header
	// ContextCarrier stores the immutable request metadata needed by asynchronous
	// usage sinks. It is populated by Publish when the caller does not provide one.
	ContextCarrier *RecordContextCarrier
	// CanonicalIdentitySeed is an optional seed carrier for mutation admission.
	CanonicalIdentitySeed string
	// AdmissionDiscriminator explicitly links retries/enrichment from one live
	// reporter without making an unseeded Record globally reusable by request ID.
	AdmissionDiscriminator string
	// SourceGroupKey and SourceGroupOrdinal carry the coordinator's immutable
	// identity allocation when a caller already owns one.
	SourceGroupKey     string
	SourceGroupOrdinal uint64
}

// Failure holds HTTP failure metadata for an upstream request attempt.
type Failure struct {
	StatusCode int
	Body       string
}

// Detail holds the token usage breakdown.
type Detail struct {
	InputTokens         int64
	OutputTokens        int64
	ReasoningTokens     int64
	CachedTokens        int64
	CacheReadTokens     int64
	CacheCreationTokens int64
	TotalTokens         int64
	TokenBreakdown      TokenBreakdown
	ResponseServiceTier string
}

// RecordContextCarrier is the immutable subset of request context required by
// asynchronous usage consumers. It deliberately contains no context.Context
// or request-owned mutable values.
type RecordContextCarrier struct {
	RequestID              string
	ClientIP               string
	XForwardedFor          string
	UserAgent              string
	Endpoint               string
	ModelAlias             string
	DetailRole             string
	DetailSequence         string
	ReasoningEffort        string
	ServiceTier            string
	Source                 string
	AuthIndex              string
	AuthType               string
	AdmissionDiscriminator string
	Generate               bool
	Success                bool
	HasSuccess             bool
}

type recordContextCarrierKey struct{}

// WithRecordContextCarrier attaches an immutable carrier to a context.
func WithRecordContextCarrier(ctx context.Context, carrier RecordContextCarrier) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, recordContextCarrierKey{}, cloneRecordContextCarrier(carrier))
}

// RecordContextCarrierFromContext returns the carrier attached to ctx.
func RecordContextCarrierFromContext(ctx context.Context) (RecordContextCarrier, bool) {
	if ctx == nil {
		return RecordContextCarrier{}, false
	}
	carrier, ok := ctx.Value(recordContextCarrierKey{}).(RecordContextCarrier)
	if !ok {
		return RecordContextCarrier{}, false
	}
	return cloneRecordContextCarrier(carrier), true
}

func cloneRecordContextCarrier(carrier RecordContextCarrier) RecordContextCarrier {
	carrier.RequestID = strings.TrimSpace(carrier.RequestID)
	carrier.ClientIP = strings.TrimSpace(carrier.ClientIP)
	carrier.XForwardedFor = strings.TrimSpace(carrier.XForwardedFor)
	carrier.UserAgent = strings.TrimSpace(carrier.UserAgent)
	carrier.Endpoint = strings.TrimSpace(carrier.Endpoint)
	carrier.ModelAlias = strings.TrimSpace(carrier.ModelAlias)
	carrier.DetailRole = strings.TrimSpace(carrier.DetailRole)
	carrier.DetailSequence = strings.TrimSpace(carrier.DetailSequence)
	carrier.ReasoningEffort = strings.TrimSpace(carrier.ReasoningEffort)
	carrier.ServiceTier = strings.TrimSpace(carrier.ServiceTier)
	carrier.Source = strings.TrimSpace(carrier.Source)
	carrier.AuthIndex = strings.TrimSpace(carrier.AuthIndex)
	carrier.AuthType = strings.TrimSpace(carrier.AuthType)
	carrier.AdmissionDiscriminator = strings.TrimSpace(carrier.AdmissionDiscriminator)
	return carrier
}

// CaptureRecordContext copies the SDK-owned values used by usage sinks. Code
// with access to richer request metadata can merge those values into the
// returned carrier before publishing.
func CaptureRecordContext(ctx context.Context, record Record) RecordContextCarrier {
	carrier, _ := RecordContextCarrierFromContext(ctx)
	if carrier.ModelAlias == "" {
		carrier.ModelAlias = RequestedModelAliasFromContext(ctx)
	}
	if carrier.ReasoningEffort == "" {
		carrier.ReasoningEffort = ReasoningEffortFromContext(ctx)
	}
	if carrier.ServiceTier == "" {
		carrier.ServiceTier = ServiceTierFromContext(ctx)
	}
	if !carrier.HasSuccess {
		failed := record.Failed || record.Fail.StatusCode >= http.StatusBadRequest
		carrier.Success = !failed
		carrier.HasSuccess = true
	}
	if record.Generate == nil {
		carrier.Generate = GenerateFromContext(ctx)
	} else {
		carrier.Generate = GenerateEnabled(record.Generate)
	}
	carrier.Source = firstNonEmpty(carrier.Source, record.Source)
	carrier.AuthIndex = firstNonEmpty(carrier.AuthIndex, record.AuthIndex)
	carrier.AuthType = firstNonEmpty(carrier.AuthType, record.AuthType)
	carrier.AdmissionDiscriminator = firstNonEmpty(carrier.AdmissionDiscriminator, record.AdmissionDiscriminator)
	return cloneRecordContextCarrier(carrier)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

type requestedModelAliasContextKey struct{}
type reasoningEffortContextKey struct{}
type serviceTierContextKey struct{}
type generateContextKey struct{}

// WithRequestedModelAlias stores the client-requested model name for usage sinks.
func WithRequestedModelAlias(ctx context.Context, alias string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	alias = strings.TrimSpace(alias)
	if alias == "" {
		return ctx
	}
	return context.WithValue(ctx, requestedModelAliasContextKey{}, alias)
}

// RequestedModelAliasFromContext returns the client-requested model name stored in ctx.
func RequestedModelAliasFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	raw := ctx.Value(requestedModelAliasContextKey{})
	switch value := raw.(type) {
	case string:
		return strings.TrimSpace(value)
	case []byte:
		return strings.TrimSpace(string(value))
	default:
		return ""
	}
}

// WithReasoningEffort stores the client-requested reasoning effort for usage sinks.
func WithReasoningEffort(ctx context.Context, effort string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	effort = strings.TrimSpace(effort)
	if effort == "" {
		return ctx
	}
	return context.WithValue(ctx, reasoningEffortContextKey{}, effort)
}

// ReasoningEffortFromContext returns the client-requested reasoning effort stored in ctx.
func ReasoningEffortFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	raw := ctx.Value(reasoningEffortContextKey{})
	switch value := raw.(type) {
	case string:
		return strings.TrimSpace(value)
	case []byte:
		return strings.TrimSpace(string(value))
	default:
		return ""
	}
}

// WithServiceTier stores the client-requested service tier for usage sinks.
func WithServiceTier(ctx context.Context, tier string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	tier = strings.TrimSpace(tier)
	if tier == "" {
		tier = DefaultServiceTier
	}
	return context.WithValue(ctx, serviceTierContextKey{}, tier)
}

// ServiceTierFromContext returns the client-requested service tier stored in ctx.
func ServiceTierFromContext(ctx context.Context) string {
	if ctx == nil {
		return DefaultServiceTier
	}
	raw := ctx.Value(serviceTierContextKey{})
	switch value := raw.(type) {
	case string:
		tier := strings.TrimSpace(value)
		if tier == "" {
			return DefaultServiceTier
		}
		return tier
	case []byte:
		tier := strings.TrimSpace(string(value))
		if tier == "" {
			return DefaultServiceTier
		}
		return tier
	default:
		return DefaultServiceTier
	}
}

// WithGenerate stores whether the client requested actual generation for usage sinks.
// Missing context values default to true; only an explicit false disables generation.
func WithGenerate(ctx context.Context, generate bool) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, generateContextKey{}, generate)
}

// GenerateFromContext returns whether the client requested actual generation.
// Missing values default to true.
func GenerateFromContext(ctx context.Context) bool {
	if ctx == nil {
		return true
	}
	raw := ctx.Value(generateContextKey{})
	switch value := raw.(type) {
	case bool:
		return value
	default:
		return true
	}
}

// GenerateFlag returns a pointer suitable for Record.Generate.
func GenerateFlag(generate bool) *bool {
	return &generate
}

// GenerateEnabled reports whether generation is enabled for the record field.
// A nil value defaults to true so legacy callers that omit Generate keep the historical behavior.
func GenerateEnabled(generate *bool) bool {
	if generate == nil {
		return true
	}
	return *generate
}

// ErrUsageManagerClosed means the manager has started draining and will not
// accept new records.
var ErrUsageManagerClosed = errors.New("usage manager is closed")

// Plugin consumes usage records emitted by the proxy runtime.
type Plugin interface {
	HandleUsage(ctx context.Context, record Record)
}

// UsageOutcomePlugin is an optional error-reporting adapter for a Plugin.
// Legacy void plugins remain valid and are treated as successful when they
// return normally.
type UsageOutcomePlugin interface {
	HandleUsageOutcome(ctx context.Context, record Record) error
}

// UsageSinkRole marks a plugin as the authoritative mutation sink. There can
// be at most one authoritative sink on a manager.
type UsageSinkRole interface {
	IsAuthoritativeUsageSink() bool
}

type UsageDeliveryOutcomeKind string

const (
	UsageOutcomeCommitted                  UsageDeliveryOutcomeKind = "committed"
	UsageOutcomePluginError                UsageDeliveryOutcomeKind = "plugin_error"
	UsageOutcomePluginPanic                UsageDeliveryOutcomeKind = "plugin_panic"
	UsageOutcomeObserverError              UsageDeliveryOutcomeKind = "observer_plugin_error"
	UsageOutcomeObserverPanic              UsageDeliveryOutcomeKind = "observer_plugin_panic"
	UsageOutcomeNoPlugin                   UsageDeliveryOutcomeKind = "committed"
	UsageOutcomeNoPluginDiagnostic         UsageDeliveryOutcomeKind = "usage_manager_no_plugin"
	UsageOutcomeDeliveryCancelled          UsageDeliveryOutcomeKind = "delivery_cancelled_before_dispatch"
	UsageOutcomeMultipleAuthoritativeSinks UsageDeliveryOutcomeKind = "multiple_authoritative_usage_sinks"
)

// UsageDeliveryOutcome is emitted for each plugin and then once at item level.
type UsageDeliveryOutcome struct {
	ItemID      uint64
	PluginID    string
	Kind        UsageDeliveryOutcomeKind
	PluginCount int
	Err         error
	Item        bool
}

// UsageOutcomeRecorder receives terminal delivery observations.
type UsageOutcomeRecorder interface {
	RecordUsageDeliveryOutcome(UsageDeliveryOutcome)
}

type queueItem struct {
	id      uint64
	record  Record
	carrier RecordContextCarrier
}

type pluginRegistration struct {
	id            string
	plugin        Plugin
	authoritative bool
}

// Manager maintains a hard-bounded FIFO queue and delivers records to plugins.
type Manager struct {
	once     sync.Once
	stopOnce sync.Once
	cancel   context.CancelFunc

	mu       sync.Mutex
	cond     *sync.Cond
	queue    []queueItem
	limit    int
	closed   bool
	started  bool
	inFlight int
	idle     chan struct{}
	done     chan struct{}
	stopCh   chan struct{}
	slots    chan struct{}

	pluginsMu sync.RWMutex
	plugins   []pluginRegistration
	named     map[string]int

	outcomeMu       sync.RWMutex
	outcomeRecorder UsageOutcomeRecorder
	outcomeCounts   map[UsageDeliveryOutcomeKind]uint64
	itemSequence    atomic.Uint64
}

// NewManager constructs a manager with a hard-bounded FIFO queue.
func NewManager(buffer int) *Manager {
	if buffer <= 0 {
		buffer = 1
		log.WithField("code", "invalid_publish_queue_limit").Warn("usage: invalid publish queue limit; using 1")
	}
	m := &Manager{
		limit:         buffer,
		idle:          make(chan struct{}),
		done:          make(chan struct{}),
		stopCh:        make(chan struct{}),
		slots:         make(chan struct{}, buffer),
		outcomeCounts: make(map[UsageDeliveryOutcomeKind]uint64),
	}
	close(m.idle)
	m.cond = sync.NewCond(&m.mu)
	return m
}

// PublishQueueLimit returns the immutable hard queue limit.
func (m *Manager) PublishQueueLimit() int {
	if m == nil {
		return 0
	}
	return m.limit
}

// SetOutcomeRecorder configures the manager's terminal outcome observer.
func (m *Manager) SetOutcomeRecorder(recorder UsageOutcomeRecorder) {
	if m == nil {
		return
	}
	m.outcomeMu.Lock()
	m.outcomeRecorder = recorder
	m.outcomeMu.Unlock()
}

// OutcomeCount returns the number of terminal delivery observations recorded
// by this manager for the given outcome kind.
func (m *Manager) OutcomeCount(kind UsageDeliveryOutcomeKind) uint64 {
	if m == nil {
		return 0
	}
	m.outcomeMu.RLock()
	defer m.outcomeMu.RUnlock()
	return m.outcomeCounts[kind]
}

// Start launches the background dispatcher. Calling Start multiple times is safe.
func (m *Manager) Start(ctx context.Context) {
	if m == nil {
		return
	}
	m.once.Do(func() {
		// The worker owns a drain context. It must not inherit cancellation from
		// an individual request or be cancelled until accepted items are drained.
		workerCtx, cancel := context.WithCancel(context.Background())
		m.mu.Lock()
		m.started = true
		m.cancel = cancel
		m.mu.Unlock()
		go m.run(workerCtx)
	})
}

// Stop closes admission and starts FIFO drain. It does not wait for plugins.
func (m *Manager) Stop() {
	if m == nil {
		return
	}
	m.Start(context.Background())
	m.stopOnce.Do(func() {
		m.mu.Lock()
		m.closed = true
		close(m.stopCh)
		m.cond.Broadcast()
		m.mu.Unlock()
	})
}

// Wait waits for accepted items and all in-flight plugin calls to reach a
// terminal outcome. A caller timeout never cancels the manager's drain.
func (m *Manager) Wait(ctx context.Context) error {
	if m == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	m.mu.Lock()
	if !m.started {
		m.mu.Unlock()
		return nil
	}
	idle := m.idle
	m.mu.Unlock()
	select {
	case <-idle:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Register appends a plugin to the delivery list.
func (m *Manager) Register(plugin Plugin) {
	if m == nil || plugin == nil {
		return
	}
	m.register("", plugin)
}

// RegisterNamed registers or replaces a plugin by name.
func (m *Manager) RegisterNamed(name string, plugin Plugin) {
	if m == nil || plugin == nil {
		return
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	m.register(name, plugin)
}

func (m *Manager) register(name string, plugin Plugin) {
	authoritative := isAuthoritative(plugin)
	rejected := false
	m.pluginsMu.Lock()
	if authoritative && m.hasOtherAuthoritativeLocked(name) {
		rejected = true
	} else {
		if m.named == nil {
			m.named = make(map[string]int)
		}
		if name != "" {
			if index, exists := m.named[name]; exists && index >= 0 && index < len(m.plugins) {
				m.plugins[index] = pluginRegistration{id: name, plugin: plugin, authoritative: authoritative}
				m.pluginsMu.Unlock()
				return
			}
		}
		id := name
		if id == "" {
			id = pluginIdentifier(plugin, len(m.plugins))
		}
		if name != "" {
			m.named[name] = len(m.plugins)
		}
		m.plugins = append(m.plugins, pluginRegistration{id: id, plugin: plugin, authoritative: authoritative})
	}
	m.pluginsMu.Unlock()
	if rejected {
		errRejected := errors.New("multiple authoritative usage sinks rejected")
		log.WithField("code", string(UsageOutcomeMultipleAuthoritativeSinks)).WithError(errRejected).Error("usage: authoritative sink registration rejected")
		m.recordOutcome(UsageDeliveryOutcome{PluginID: name, Kind: UsageOutcomeMultipleAuthoritativeSinks, Err: errRejected})
	}
}

func (m *Manager) hasOtherAuthoritativeLocked(replacing string) bool {
	replacingIndex, replacingExists := m.named[replacing]
	for index, registration := range m.plugins {
		if !registration.authoritative {
			continue
		}
		if replacingExists && replacingIndex == index {
			continue
		}
		return true
	}
	return false
}

func isAuthoritative(plugin Plugin) bool {
	role, ok := plugin.(UsageSinkRole)
	return ok && role.IsAuthoritativeUsageSink()
}

func pluginIdentifier(plugin Plugin, index int) string {
	typeName := "plugin"
	if plugin != nil {
		typeName = reflect.TypeOf(plugin).String()
	}
	return typeName + "#" + strconv.Itoa(index)
}

// Publish enqueues a usage record. A nil error means the record was accepted
// into the manager FIFO, not that it reached a mutation coordinator.
func (m *Manager) Publish(ctx context.Context, record Record) error {
	if m == nil {
		return ErrUsageManagerClosed
	}
	if ctx == nil {
		ctx = context.Background()
	}
	m.Start(context.Background())
	if err := ctx.Err(); err != nil {
		return err
	}
	carrier := CaptureRecordContext(ctx, record)
	if record.ContextCarrier != nil {
		carrier = mergeRecordContextCarrier(carrier, *record.ContextCarrier)
	}
	if record.Generate == nil {
		record.Generate = GenerateFlag(carrier.Generate)
	}
	record.ContextCarrier = &carrier
	if err := m.acquireSlot(ctx); err != nil {
		return err
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		<-m.slots
		return ErrUsageManagerClosed
	}
	if err := ctx.Err(); err != nil {
		m.mu.Unlock()
		<-m.slots
		return err
	}
	queuedRecord := cloneRecord(record)
	item := queueItem{id: m.itemSequence.Add(1), record: queuedRecord, carrier: cloneRecordContextCarrier(carrier)}
	if len(m.queue) == 0 && m.inFlight == 0 {
		m.idle = make(chan struct{})
	}
	m.queue = append(m.queue, item)
	m.mu.Unlock()
	m.cond.Signal()
	return nil
}

func cloneRecord(record Record) Record {
	if record.Generate != nil {
		generate := *record.Generate
		record.Generate = &generate
	}
	if record.ResponseHeaders != nil {
		record.ResponseHeaders = record.ResponseHeaders.Clone()
	}
	if record.ContextCarrier != nil {
		carrier := cloneRecordContextCarrier(*record.ContextCarrier)
		record.ContextCarrier = &carrier
	}
	return record
}

func (m *Manager) acquireSlot(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	closed := m.closed
	m.mu.Unlock()
	if closed {
		return ErrUsageManagerClosed
	}
	select {
	case m.slots <- struct{}{}:
		if err := ctx.Err(); err != nil {
			<-m.slots
			return err
		}
		m.mu.Lock()
		closed = m.closed
		m.mu.Unlock()
		if closed {
			<-m.slots
			return ErrUsageManagerClosed
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-m.stopCh:
		return ErrUsageManagerClosed
	}
}

func mergeRecordContextCarrier(base, override RecordContextCarrier) RecordContextCarrier {
	if override.RequestID != "" {
		base.RequestID = override.RequestID
	}
	if override.ClientIP != "" {
		base.ClientIP = override.ClientIP
	}
	if override.XForwardedFor != "" {
		base.XForwardedFor = override.XForwardedFor
	}
	if override.UserAgent != "" {
		base.UserAgent = override.UserAgent
	}
	if override.Endpoint != "" {
		base.Endpoint = override.Endpoint
	}
	if override.ModelAlias != "" {
		base.ModelAlias = override.ModelAlias
	}
	if override.DetailRole != "" {
		base.DetailRole = override.DetailRole
	}
	if override.DetailSequence != "" {
		base.DetailSequence = override.DetailSequence
	}
	if override.ReasoningEffort != "" {
		base.ReasoningEffort = override.ReasoningEffort
	}
	if override.ServiceTier != "" {
		base.ServiceTier = override.ServiceTier
	}
	if override.Source != "" {
		base.Source = override.Source
	}
	if override.AuthIndex != "" {
		base.AuthIndex = override.AuthIndex
	}
	if override.AuthType != "" {
		base.AuthType = override.AuthType
	}
	if override.AdmissionDiscriminator != "" {
		base.AdmissionDiscriminator = override.AdmissionDiscriminator
	}
	if override.HasSuccess {
		base.Success, base.HasSuccess = override.Success, true
	}
	base.Generate = override.Generate
	return cloneRecordContextCarrier(base)
}

func (m *Manager) run(workerCtx context.Context) {
	defer func() {
		if m.cancel != nil {
			m.cancel()
		}
		close(m.done)
	}()
	for {
		m.mu.Lock()
		for !m.closed && len(m.queue) == 0 {
			m.cond.Wait()
		}
		if len(m.queue) == 0 && m.closed {
			m.mu.Unlock()
			return
		}
		item := m.queue[0]
		m.queue = m.queue[1:]
		m.inFlight++
		m.mu.Unlock()
		<-m.slots
		m.dispatch(workerCtx, item)
		m.mu.Lock()
		m.inFlight--
		if len(m.queue) == 0 && m.inFlight == 0 {
			select {
			case <-m.idle:
			default:
				close(m.idle)
			}
			m.cond.Broadcast()
		}
		m.mu.Unlock()
	}
}

func (m *Manager) dispatch(workerCtx context.Context, item queueItem) {
	m.pluginsMu.RLock()
	plugins := append([]pluginRegistration(nil), m.plugins...)
	m.pluginsMu.RUnlock()
	ctx := WithRecordContextCarrier(workerCtx, item.carrier)
	if len(plugins) == 0 {
		m.recordOutcome(UsageDeliveryOutcome{ItemID: item.id, Kind: UsageOutcomeNoPluginDiagnostic, PluginCount: 0})
		m.recordOutcome(UsageDeliveryOutcome{ItemID: item.id, Kind: UsageOutcomeNoPlugin, PluginCount: 0, Item: true})
		return
	}
	itemKind := UsageOutcomeCommitted
	for _, registration := range plugins {
		if registration.plugin == nil {
			continue
		}
		kind, err := safeInvoke(registration.plugin, ctx, item.record)
		if kind != UsageOutcomeCommitted {
			if registration.authoritative {
				itemKind = kind
			} else if kind == UsageOutcomePluginError {
				kind = UsageOutcomeObserverError
			} else if kind == UsageOutcomePluginPanic {
				kind = UsageOutcomeObserverPanic
			}
		}
		m.recordOutcome(UsageDeliveryOutcome{ItemID: item.id, PluginID: registration.id, Kind: kind, PluginCount: len(plugins), Err: err})
	}
	m.recordOutcome(UsageDeliveryOutcome{ItemID: item.id, Kind: itemKind, PluginCount: len(plugins), Item: true})
}

func safeInvoke(plugin Plugin, ctx context.Context, record Record) (kind UsageDeliveryOutcomeKind, err error) {
	kind = UsageOutcomeCommitted
	defer func() {
		if recovered := recover(); recovered != nil {
			log.Errorf("usage: plugin panic recovered: %v", recovered)
			kind = UsageOutcomePluginPanic
			err = errors.New("usage plugin panic")
		}
	}()
	if outcomePlugin, ok := plugin.(UsageOutcomePlugin); ok {
		if invokeErr := outcomePlugin.HandleUsageOutcome(ctx, record); invokeErr != nil {
			return UsageOutcomePluginError, invokeErr
		}
		return UsageOutcomeCommitted, nil
	}
	plugin.HandleUsage(ctx, record)
	return UsageOutcomeCommitted, nil
}

func (m *Manager) recordOutcome(outcome UsageDeliveryOutcome) {
	m.outcomeMu.RLock()
	recorder := m.outcomeRecorder
	m.outcomeMu.RUnlock()
	m.outcomeMu.Lock()
	if m.outcomeCounts == nil {
		m.outcomeCounts = make(map[UsageDeliveryOutcomeKind]uint64)
	}
	m.outcomeCounts[outcome.Kind]++
	m.outcomeMu.Unlock()
	if recorder == nil {
		return
	}
	defer func() { _ = recover() }()
	recorder.RecordUsageDeliveryOutcome(outcome)
}

var defaultManager = NewManager(512)

// DefaultManager returns the global usage manager instance.
func DefaultManager() *Manager { return defaultManager }

// RegisterPlugin registers a plugin on the default manager.
func RegisterPlugin(plugin Plugin) { DefaultManager().Register(plugin) }

// RegisterNamedPlugin registers or replaces a named plugin on the default manager.
func RegisterNamedPlugin(name string, plugin Plugin) { DefaultManager().RegisterNamed(name, plugin) }

// PublishRecord publishes a record using the default manager.
func PublishRecord(ctx context.Context, record Record) error {
	return DefaultManager().Publish(ctx, record)
}

// StartDefault starts the default manager's dispatcher.
func StartDefault(ctx context.Context) { DefaultManager().Start(ctx) }

// StopDefault stops the default manager's dispatcher.
func StopDefault() { DefaultManager().Stop() }

// WaitDefault waits for all accepted records on the default manager to drain.
func WaitDefault(ctx context.Context) error { return DefaultManager().Wait(ctx) }
