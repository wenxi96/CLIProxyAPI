package usage

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"
)

type managerTestPlugin struct {
	mu        sync.Mutex
	records   []Record
	started   chan struct{}
	release   chan struct{}
	startOnce sync.Once
	block     bool
}

func (p *managerTestPlugin) HandleUsage(ctx context.Context, record Record) {
	p.startOnce.Do(func() {
		if p.started != nil {
			close(p.started)
		}
	})
	if p.block && p.release != nil {
		<-p.release
	}
	p.mu.Lock()
	p.records = append(p.records, record)
	p.mu.Unlock()
	if ctx == nil {
		panic("manager did not provide a drain context")
	}
}

func (p *managerTestPlugin) snapshot() []Record {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]Record(nil), p.records...)
}

type managerOutcomePlugin struct {
	authoritative bool
	err           error
	panicValue    any
}

func (p managerOutcomePlugin) HandleUsage(context.Context, Record) {}

func (p managerOutcomePlugin) HandleUsageOutcome(context.Context, Record) error {
	if p.panicValue != nil {
		panic(p.panicValue)
	}
	return p.err
}

func (p managerOutcomePlugin) IsAuthoritativeUsageSink() bool { return p.authoritative }

type managerOutcomeRecorder struct {
	mu       sync.Mutex
	outcomes []UsageDeliveryOutcome
}

func (r *managerOutcomeRecorder) RecordUsageDeliveryOutcome(outcome UsageDeliveryOutcome) {
	r.mu.Lock()
	r.outcomes = append(r.outcomes, outcome)
	r.mu.Unlock()
}

func (r *managerOutcomeRecorder) snapshot() []UsageDeliveryOutcome {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]UsageDeliveryOutcome(nil), r.outcomes...)
}

func TestManagerHardCapacityAndPreAdmissionCancellation(t *testing.T) {
	manager := NewManager(1)
	plugin := &managerTestPlugin{started: make(chan struct{}), release: make(chan struct{}), block: true}
	manager.Register(plugin)

	if err := manager.Publish(context.Background(), Record{Model: "first"}); err != nil {
		t.Fatalf("publish first: %v", err)
	}
	select {
	case <-plugin.started:
	case <-time.After(time.Second):
		t.Fatal("first item was not dispatched")
	}
	if err := manager.Publish(context.Background(), Record{Model: "second"}); err != nil {
		t.Fatalf("publish second: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := manager.Publish(ctx, Record{Model: "third"}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("third publish = %v, want context deadline while queue is full", err)
	}

	close(plugin.release)
	manager.Stop()
	if err := manager.Wait(context.Background()); err != nil {
		t.Fatalf("wait for drain: %v", err)
	}
	records := plugin.snapshot()
	if len(records) != 2 || records[0].Model != "first" || records[1].Model != "second" {
		t.Fatalf("drained records = %#v, want FIFO first/second", records)
	}
}

func TestManagerNormalizesNonPositiveCapacity(t *testing.T) {
	manager := NewManager(0)
	if manager.PublishQueueLimit() != 1 {
		t.Fatalf("publish queue limit = %d, want minimum capacity 1", manager.PublishQueueLimit())
	}
	manager.Stop()
	if err := manager.Wait(context.Background()); err != nil {
		t.Fatalf("wait after stopping empty manager: %v", err)
	}
}

func TestManagerPreCancelledPublishDoesNotAcquireSlot(t *testing.T) {
	manager := NewManager(1)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := manager.Publish(ctx, Record{Model: "cancelled"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-cancelled publish = %v, want context.Canceled", err)
	}
	if manager.PublishQueueLimit() != 1 {
		t.Fatalf("publish queue limit changed after rejected item: %d", manager.PublishQueueLimit())
	}
	manager.Stop()
	if err := manager.Wait(context.Background()); err != nil {
		t.Fatalf("wait after rejected publish: %v", err)
	}
}

func TestManagerAcceptedItemIgnoresCallerCancellation(t *testing.T) {
	manager := NewManager(1)
	plugin := &managerTestPlugin{}
	manager.Register(plugin)
	ctx, cancel := context.WithCancel(context.Background())
	if err := manager.Publish(ctx, Record{Model: "accepted"}); err != nil {
		t.Fatalf("publish accepted record: %v", err)
	}
	cancel()
	manager.Stop()
	if err := manager.Wait(context.Background()); err != nil {
		t.Fatalf("wait for accepted record: %v", err)
	}
	if records := plugin.snapshot(); len(records) != 1 || records[0].Model != "accepted" {
		t.Fatalf("records after caller cancellation = %#v", records)
	}
}

func TestManagerCopiesAcceptedRecordAndCarrier(t *testing.T) {
	manager := NewManager(1)
	plugin := &managerTestPlugin{started: make(chan struct{}), release: make(chan struct{}), block: true}
	manager.Register(plugin)
	if err := manager.Publish(context.Background(), Record{Model: "blocking"}); err != nil {
		t.Fatalf("publish blocking record: %v", err)
	}
	select {
	case <-plugin.started:
	case <-time.After(time.Second):
		t.Fatal("blocking item was not dispatched")
	}

	carrier := RecordContextCarrier{RequestID: "before", Generate: false}
	headers := http.Header{"X-Usage": []string{"before"}}
	record := Record{
		Model:           "queued",
		Generate:        nil,
		ResponseHeaders: headers,
		ContextCarrier:  &carrier,
	}
	if err := manager.Publish(context.Background(), record); err != nil {
		t.Fatalf("publish queued record: %v", err)
	}
	carrier.RequestID = "after"
	carrier.Generate = true
	headers.Set("X-Usage", "after")
	record.Model = "mutated"
	close(plugin.release)
	manager.Stop()
	if err := manager.Wait(context.Background()); err != nil {
		t.Fatalf("wait for copied records: %v", err)
	}
	records := plugin.snapshot()
	if len(records) != 2 {
		t.Fatalf("records = %#v, want two", records)
	}
	queued := records[1]
	if queued.Model != "queued" {
		t.Fatalf("queued record model = %q, want queued", queued.Model)
	}
	if queued.Generate == nil || *queued.Generate {
		t.Fatalf("queued carrier generate = %v, want explicit false", queued.Generate)
	}
	if queued.ContextCarrier == nil || queued.ContextCarrier.RequestID != "before" || queued.ContextCarrier.Generate {
		t.Fatalf("queued carrier mutated: %+v", queued.ContextCarrier)
	}
	if got := queued.ResponseHeaders.Get("X-Usage"); got != "before" {
		t.Fatalf("queued response header = %q, want before", got)
	}
}

func TestManagerWaitTimeoutDoesNotCancelDrain(t *testing.T) {
	manager := NewManager(1)
	plugin := &managerTestPlugin{started: make(chan struct{}), release: make(chan struct{}), block: true}
	manager.Register(plugin)
	if err := manager.Publish(context.Background(), Record{Model: "blocked"}); err != nil {
		t.Fatalf("publish blocked record: %v", err)
	}
	select {
	case <-plugin.started:
	case <-time.After(time.Second):
		t.Fatal("blocked item was not dispatched")
	}
	manager.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := manager.Wait(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("wait timeout = %v, want context deadline", err)
	}
	close(plugin.release)
	if err := manager.Wait(context.Background()); err != nil {
		t.Fatalf("wait after release: %v", err)
	}
}

func TestManagerAuthoritativePluginOutcomes(t *testing.T) {
	tests := []struct {
		name string
		plug managerOutcomePlugin
		want UsageDeliveryOutcomeKind
	}{
		{name: "error", plug: managerOutcomePlugin{authoritative: true, err: errors.New("sink failed")}, want: UsageOutcomePluginError},
		{name: "panic", plug: managerOutcomePlugin{authoritative: true, panicValue: "sink panic"}, want: UsageOutcomePluginPanic},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manager := NewManager(2)
			recorder := &managerOutcomeRecorder{}
			manager.SetOutcomeRecorder(recorder)
			manager.Register(test.plug)
			if err := manager.Publish(context.Background(), Record{Model: test.name}); err != nil {
				t.Fatalf("publish: %v", err)
			}
			manager.Stop()
			if err := manager.Wait(context.Background()); err != nil {
				t.Fatalf("wait: %v", err)
			}
			var itemOutcome *UsageDeliveryOutcome
			for _, outcome := range recorder.snapshot() {
				if outcome.Item {
					copy := outcome
					itemOutcome = &copy
				}
			}
			if itemOutcome == nil || itemOutcome.Kind != test.want {
				t.Fatalf("item outcome = %+v, want %s", itemOutcome, test.want)
			}
		})
	}
}

func TestManagerRejectsSecondAuthoritativeSink(t *testing.T) {
	manager := NewManager(2)
	recorder := &managerOutcomeRecorder{}
	manager.SetOutcomeRecorder(recorder)
	manager.RegisterNamed("first", managerOutcomePlugin{authoritative: true})
	manager.RegisterNamed("second", managerOutcomePlugin{authoritative: true})
	if err := manager.Publish(context.Background(), Record{Model: "only-first"}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	manager.Stop()
	if err := manager.Wait(context.Background()); err != nil {
		t.Fatalf("wait: %v", err)
	}
	outcomes := recorder.snapshot()
	pluginOutcomes := 0
	for _, outcome := range outcomes {
		if !outcome.Item && outcome.Kind != UsageOutcomeMultipleAuthoritativeSinks {
			pluginOutcomes++
		}
	}
	if pluginOutcomes != 1 {
		t.Fatalf("registered authoritative plugins invoked = %d, want 1", pluginOutcomes)
	}
	if manager.OutcomeCount(UsageOutcomeMultipleAuthoritativeSinks) != 1 {
		t.Fatalf("multiple authoritative sink diagnostics = %d, want 1", manager.OutcomeCount(UsageOutcomeMultipleAuthoritativeSinks))
	}
}

func TestManagerRecordsTerminalOutcomeWithoutExplicitRecorder(t *testing.T) {
	manager := NewManager(1)
	if err := manager.Publish(context.Background(), Record{Model: "no-plugin"}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	manager.Stop()
	if err := manager.Wait(context.Background()); err != nil {
		t.Fatalf("wait: %v", err)
	}
	if got := manager.OutcomeCount(UsageOutcomeNoPluginDiagnostic); got != 1 {
		t.Fatalf("no-plugin diagnostics = %d, want 1", got)
	}
}
