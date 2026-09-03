package cliproxy

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	internalconfig "github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/pluginhost"
	internalusage "github.com/router-for-me/CLIProxyAPI/v7/internal/usage"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	sdkconfig "github.com/router-for-me/CLIProxyAPI/v7/sdk/config"
	"gopkg.in/yaml.v3"
)

func TestRuntimeConfigTxnCancelledBeforeApplyRollsBackCommittedConfig(t *testing.T) {
	previousEnabled := internalusage.StatisticsEnabled()
	internalusage.SetStatisticsEnabled(false)
	t.Cleanup(func() { internalusage.SetStatisticsEnabled(previousEnabled) })

	previous := &sdkconfig.Config{UsageStatisticsEnabled: false, UsageStatisticsPersistIntervalSeconds: 30}
	service := &Service{cfg: previous}
	commit := service.commitConfigUpdate(&sdkconfig.Config{UsageStatisticsEnabled: true, UsageStatisticsPersistIntervalSeconds: 1})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if service.applyConfigRuntime(ctx, commit, false) {
		t.Fatal("cancelled RuntimeConfigTxn unexpectedly committed")
	}
	if got := service.currentConfig(); got == nil || got.UsageStatisticsEnabled {
		t.Fatalf("config after rollback = %#v, want usage disabled", got)
	}
	if internalusage.StatisticsEnabled() {
		t.Fatal("usage statistics flag changed after cancelled transaction")
	}
}

// mockCooldownStateStore is an in-memory CooldownStateStore used to observe
// cooldown store transitions during RuntimeConfigTxn rollback without touching
// disk. It records Save invocations so tests can assert the store was exercised.
type mockCooldownStateStore struct {
	saves int
}

func (m *mockCooldownStateStore) Load(context.Context) ([]coreauth.CooldownStateRecord, error) {
	return nil, nil
}

func (m *mockCooldownStateStore) Save(ctx context.Context, records []coreauth.CooldownStateRecord) error {
	m.saves++
	return nil
}

// runtimeConfigTxnFixture builds a Service backed by a real core manager and a
// pair of configs whose routing, retry, transient cooldown, OAuth model alias
// and cooldown store all differ. The previous config is staged as the published
// baseline; applying newCfg drifts every observable runtime side effect.
func runtimeConfigTxnFixture(t *testing.T) (service *Service, previousCfg, newCfg *sdkconfig.Config, cooldownStore *mockCooldownStateStore) {
	t.Helper()

	previousCfg = &sdkconfig.Config{
		Routing:                               internalconfig.RoutingConfig{Strategy: "round-robin"},
		RequestRetry:                          3,
		MaxRetryInterval:                      30,
		MaxRetryCredentials:                   2,
		TransientErrorCooldownSeconds:         5,
		SaveCooldownStatus:                    false,
		UsageStatisticsEnabled:                false,
		UsageStatisticsPersistIntervalSeconds: 30,
		OAuthModelAlias: map[string][]sdkconfig.OAuthModelAlias{
			"codex": {{Name: "gpt-5", Alias: "codex-pro"}},
		},
	}
	newCfg = &sdkconfig.Config{
		Routing:                               internalconfig.RoutingConfig{Strategy: "fill-first"},
		RequestRetry:                          7,
		MaxRetryInterval:                      90,
		MaxRetryCredentials:                   9,
		TransientErrorCooldownSeconds:         17,
		SaveCooldownStatus:                    true,
		UsageStatisticsEnabled:                false,
		UsageStatisticsPersistIntervalSeconds: 30,
		OAuthModelAlias: map[string][]sdkconfig.OAuthModelAlias{
			"codex": {{Name: "gpt-5", Alias: "codex-mini"}},
		},
	}

	manager := coreauth.NewManager(nil, &coreauth.RoundRobinSelector{}, nil)
	manager.SetRetryConfig(3, 30*time.Second, 2)
	manager.SetConfigSnapshot(previousCfg)
	coreauth.SetTransientErrorCooldownSeconds(5)
	manager.SetOAuthModelAlias(previousCfg.OAuthModelAlias)

	cooldownStore = &mockCooldownStateStore{}
	service = &Service{
		cfg:                 previousCfg,
		coreManager:         manager,
		cooldownStateStore:  cooldownStore,
		appliedRoutingState: &routingRuntimeState{strategy: "round-robin", sessionAffinityTTL: time.Hour},
	}
	service.oldConfigYaml, _ = yaml.Marshal(previousCfg)
	return service, previousCfg, newCfg, cooldownStore
}

// assertRuntimeStateRestored asserts the manager and published config match
// previousCfg after a RuntimeConfigTxn rollback.
func assertRuntimeStateRestored(t *testing.T, service *Service, previousCfg *sdkconfig.Config, expectedCooldownStore coreauth.CooldownStateStore) {
	t.Helper()

	if got := service.currentConfig(); got == nil || got.Routing.Strategy != "round-robin" {
		t.Fatalf("config after rollback = %#v, want round-robin strategy restored", got)
	}
	var restoredYaml sdkconfig.Config
	if errYaml := yaml.Unmarshal(service.oldConfigYaml, &restoredYaml); errYaml != nil || restoredYaml.Routing.Strategy != "round-robin" {
		t.Fatalf("oldConfigYaml after rollback = %s, want round-robin strategy restored", string(service.oldConfigYaml))
	}

	selector := service.coreManager.Selector()
	if _, ok := selector.(*coreauth.RoundRobinSelector); !ok {
		t.Fatalf("selector after rollback = %T, want *RoundRobinSelector restored", selector)
	}

	retry, creds, interval := service.coreManager.RetrySettings()
	if retry != 3 || creds != 2 || interval != 30*time.Second {
		t.Fatalf("retry after rollback = (%d, %d, %s), want (3, 2, 30s)", retry, creds, interval)
	}

	if got := coreauth.TransientErrorCooldownSeconds(); got != 5 {
		t.Fatalf("transient cooldown after rollback = %d, want 5", got)
	}
	if managerCfg := service.coreManager.CurrentConfig(); managerCfg == nil || managerCfg.Routing.Strategy != previousCfg.Routing.Strategy {
		t.Fatalf("manager runtime config after rollback = %#v, want %q routing restored", managerCfg, previousCfg.Routing.Strategy)
	}

	if store := service.coreManager.CooldownStateStore(); store != expectedCooldownStore {
		t.Fatalf("cooldown store after rollback = %T, want %T restored", store, expectedCooldownStore)
	}

	if upstream, _, ok := service.coreManager.OAuthModelAliasUpstream("codex", "codex-pro"); !ok || upstream != "gpt-5" {
		t.Fatalf("oauth alias codex-pro after rollback = (%q, ok=%v), want upstream gpt-5", upstream, ok)
	}
	if _, _, ok := service.coreManager.OAuthModelAliasUpstream("codex", "codex-mini"); ok {
		t.Fatal("oauth alias codex-mini still present after rollback, want previous table restored")
	}
}

// TestRuntimeConfigTxnManagerSuccessPprofFailureRollsBackAllSideEffects is
// the permanent failure-injection regression for L04B-TXN-01. The manager hook
// succeeds (selector/retry/cooldown/OAuth alias drift to the new config), then
// the pprof hook fails. Rollback must restore config/YAML, selector, retry,
// transient cooldown, cooldown store, OAuth alias and plugin/executor state.
func TestRuntimeConfigTxnManagerSuccessPprofFailureRollsBackAllSideEffects(t *testing.T) {
	previousEnabled := internalusage.StatisticsEnabled()
	internalusage.SetStatisticsEnabled(false)
	t.Cleanup(func() { internalusage.SetStatisticsEnabled(previousEnabled) })

	service, previousCfg, newCfg, _ := runtimeConfigTxnFixture(t)

	// Inject a deterministic pprof hook failure after the manager hook succeeds.
	service.applyPprofConfigContextFn = func(_ context.Context, cfg *sdkconfig.Config) bool {
		return cfg.Routing.Strategy != newCfg.Routing.Strategy
	}

	commit := service.stageConfigUpdate(newCfg)
	if commit.cfg == nil {
		t.Fatal("stageConfigUpdate returned empty commit")
	}
	if service.applyConfigRuntime(context.Background(), commit, false) {
		t.Fatal("RuntimeConfigTxn with pprof failure unexpectedly committed")
	}

	assertRuntimeStateRestored(t, service, previousCfg, nil)
}

// TestRuntimeConfigTxnLaterHookFailureCompensatesPriorHooks proves that a
// failure at a later hook (server clients) triggers reverse-order compensation
// of every previously successful hook (manager, pprof). This exercises the
// compensation slice with more than one entry.
func TestRuntimeConfigTxnLaterHookFailureCompensatesPriorHooks(t *testing.T) {
	previousEnabled := internalusage.StatisticsEnabled()
	internalusage.SetStatisticsEnabled(false)
	t.Cleanup(func() { internalusage.SetStatisticsEnabled(previousEnabled) })

	service, previousCfg, newCfg, _ := runtimeConfigTxnFixture(t)

	// Let the pprof hook succeed, then fail the server clients hook so the
	// manager and pprof compensations must both run in reverse order.
	service.applyPprofConfigContextFn = func(context.Context, *sdkconfig.Config) bool { return true }
	service.updateServerClientsContextFn = func(_ context.Context, cfg *sdkconfig.Config) bool {
		return cfg.Routing.Strategy != newCfg.Routing.Strategy
	}

	commit := service.stageConfigUpdate(newCfg)
	if commit.cfg == nil {
		t.Fatal("stageConfigUpdate returned empty commit")
	}
	if service.applyConfigRuntime(context.Background(), commit, false) {
		t.Fatal("RuntimeConfigTxn with server clients failure unexpectedly committed")
	}

	assertRuntimeStateRestored(t, service, previousCfg, nil)
}

// flakyCooldownStateStore fails its first Save to force
// ApplyConfigWithCooldownStateStore to return false after applyManagerConfig
// has already mutated selector/retry/transient cooldown. Any second Save would
// prove rollback repeated an external persistence side effect.
type flakyCooldownStateStore struct {
	mu    sync.Mutex
	calls int
}

func (f *flakyCooldownStateStore) Load(context.Context) ([]coreauth.CooldownStateRecord, error) {
	return nil, nil
}

func (f *flakyCooldownStateStore) Save(ctx context.Context, records []coreauth.CooldownStateRecord) error {
	f.mu.Lock()
	f.calls++
	fail := f.calls == 1
	f.mu.Unlock()
	if fail {
		return fmt.Errorf("flaky cooldown store: simulated persist failure")
	}
	return nil
}

// TestRuntimeConfigTxnManagerInternalFailureAfterMutationRollsBack proves
// that when applyManagerConfig mutates selector/retry/transient cooldown and
// then returns false (e.g. cooldown persist fails), the compensation still
// runs and restores the prior manager state. The compensation is registered
// before the hook so an internal partial failure is not left uncompensated.
func TestRuntimeConfigTxnManagerInternalFailureAfterMutationRollsBack(t *testing.T) {
	previousEnabled := internalusage.StatisticsEnabled()
	internalusage.SetStatisticsEnabled(false)
	t.Cleanup(func() { internalusage.SetStatisticsEnabled(previousEnabled) })

	service, previousCfg, newCfg, _ := runtimeConfigTxnFixture(t)
	// No cooldown store override and newCfg keeps cooldown save disabled, so
	// resolveCooldownStateStore returns nil for both configs while the manager
	// still holds a pre-set flaky store whose first persist fails.
	service.cooldownStateStore = nil
	newCfg.SaveCooldownStatus = false
	flaky := &flakyCooldownStateStore{}
	service.coreManager.ApplyConfigWithCooldownStateStore(context.Background(), previousCfg, flaky)

	commit := service.stageConfigUpdate(newCfg)
	if commit.cfg == nil {
		t.Fatal("stageConfigUpdate returned empty commit")
	}
	if service.applyConfigRuntime(context.Background(), commit, false) {
		t.Fatal("RuntimeConfigTxn with manager internal failure unexpectedly committed")
	}

	assertRuntimeStateRestored(t, service, previousCfg, flaky)
	if flaky.calls != 1 {
		t.Fatalf("flaky cooldown store Save called %d times, want 1 forward attempt and no rollback persistence", flaky.calls)
	}
}

// TestRuntimeConfigTxnPostExecutorFailureRollsBackPluginAndExecutorState is
// the permanent regression for L04B-TXN-01 later-hook rollback. The plugin and
// executor hooks run and mutate state (plugin host applied config, baseline
// executors registered), then the post-executor seam fails. Rollback must
// restore the plugin host config, remove the added executors (additive
// registration cannot be removed by re-applying the previous config, so the
// executor snapshot/restore path is exercised) and restore the manager.
func TestRuntimeConfigTxnPostExecutorFailureRollsBackPluginAndExecutorState(t *testing.T) {
	previousEnabled := internalusage.StatisticsEnabled()
	internalusage.SetStatisticsEnabled(false)
	t.Cleanup(func() { internalusage.SetStatisticsEnabled(previousEnabled) })

	service, previousCfg, newCfg, _ := runtimeConfigTxnFixture(t)
	// newCfg enables Home so registerAvailableExecutors registers observable
	// baseline executors (codex/claude/gemini ...). previousCfg keeps Home
	// disabled so the pre-apply executor snapshot is empty and the compensation
	// must remove the added baseline executors.
	newCfg.Home.Enabled = true
	previousCfg.Home.Enabled = false
	service.cooldownStateStore = nil
	newCfg.SaveCooldownStatus = false
	previousCfg.SaveCooldownStatus = false

	service.pluginHost = pluginhost.New()
	// Bypass real pprof/server/plugin side-effect chains while still observing
	// plugin host config transitions, then fail right after executor registration.
	service.applyPprofConfigContextFn = func(context.Context, *sdkconfig.Config) bool { return true }
	service.updateServerClientsContextFn = func(context.Context, *sdkconfig.Config) bool { return true }
	service.syncPluginRuntimeConfigForConfigFn = func(ctx context.Context, cfg *sdkconfig.Config) bool {
		service.pluginHost.ApplyConfig(ctx, cfg)
		return true
	}
	service.runtimeHookAfterExecutorsFn = func() bool { return false }

	commit := service.stageConfigUpdate(newCfg)
	if commit.cfg == nil {
		t.Fatal("stageConfigUpdate returned empty commit")
	}
	if service.applyConfigRuntime(context.Background(), commit, false) {
		t.Fatal("RuntimeConfigTxn with post-executor failure unexpectedly committed")
	}

	// Manager side effects restored.
	assertRuntimeStateRestored(t, service, previousCfg, nil)
	// Plugin host applied config rolled back to the previous config.
	if applied := service.pluginHost.AppliedRuntimeConfig(); applied == nil || applied.Routing.Strategy != "round-robin" {
		t.Fatalf("plugin host applied config after rollback = %#v, want round-robin strategy restored", applied)
	}
	// Executor registration is additive: the baseline executors added by the
	// failed apply must be removed by the snapshot/restore compensation.
	if executor, ok := service.coreManager.Executor("codex"); ok || executor != nil {
		t.Fatalf("codex executor after rollback = %T (ok=%v), want removed by executor snapshot/restore compensation", executor, ok)
	}
}

// TestRuntimeConfigTxnRollbackSerializesAgainstNewerRuntimeApply reproduces
// the rollback/apply interleaving that can leave the published config on the
// newest candidate while an older transaction's manager compensation restores
// stale selector and retry state afterward.
func TestRuntimeConfigTxnRollbackSerializesAgainstNewerRuntimeApply(t *testing.T) {
	previousEnabled := internalusage.StatisticsEnabled()
	internalusage.SetStatisticsEnabled(false)
	t.Cleanup(func() { internalusage.SetStatisticsEnabled(previousEnabled) })

	previousCfg := &sdkconfig.Config{
		Routing:                internalconfig.RoutingConfig{Strategy: "round-robin"},
		RequestRetry:           3,
		MaxRetryInterval:       30,
		MaxRetryCredentials:    2,
		SaveCooldownStatus:     false,
		UsageStatisticsEnabled: false,
	}
	failingCfg := previousCfg.CloneForRuntime()
	failingCfg.Routing.Strategy = "fill-first"
	failingCfg.RequestRetry = 5
	newerCfg := previousCfg.CloneForRuntime()
	newerCfg.Routing.Strategy = "weighted-round-robin"
	newerCfg.RequestRetry = 7

	manager := coreauth.NewManager(nil, &coreauth.RoundRobinSelector{}, nil)
	manager.SetRetryConfig(previousCfg.RequestRetry, time.Duration(previousCfg.MaxRetryInterval)*time.Second, previousCfg.MaxRetryCredentials)
	service := &Service{
		cfg:                 previousCfg,
		coreManager:         manager,
		appliedRoutingState: &routingRuntimeState{strategy: "round-robin", sessionAffinityTTL: time.Hour},
	}
	service.oldConfigYaml, _ = yaml.Marshal(previousCfg)

	rollbackEntered := make(chan struct{})
	releaseRollback := make(chan struct{})
	var rollbackEnteredOnce sync.Once
	var releaseRollbackOnce sync.Once
	release := func() { releaseRollbackOnce.Do(func() { close(releaseRollback) }) }
	t.Cleanup(release)

	service.applyPprofConfigContextFn = func(_ context.Context, cfg *sdkconfig.Config) bool {
		switch cfg.Routing.Strategy {
		case "fill-first":
			return false
		case "round-robin":
			rollbackEnteredOnce.Do(func() { close(rollbackEntered) })
			<-releaseRollback
			return true
		default:
			return true
		}
	}

	failingCommit := service.stageConfigUpdate(failingCfg)
	failingDone := make(chan bool, 1)
	go func() {
		failingDone <- service.applyConfigRuntime(context.Background(), failingCommit, false)
	}()

	select {
	case <-rollbackEntered:
	case <-time.After(time.Second):
		t.Fatal("older RuntimeConfigTxn rollback did not reach the compensation barrier")
	}

	newerCommit := service.stageConfigUpdate(newerCfg)
	newerDone := make(chan bool, 1)
	go func() {
		newerDone <- service.applyConfigRuntime(context.Background(), newerCommit, false)
	}()

	var newerCompletedEarly bool
	select {
	case newerCompletedEarly = <-newerDone:
	case <-time.After(100 * time.Millisecond):
	}

	release()
	if applied := <-failingDone; applied {
		t.Fatal("older RuntimeConfigTxn with injected pprof failure unexpectedly committed")
	}
	if newerCompletedEarly {
		t.Fatal("newer RuntimeConfigTxn completed while the older rollback compensation was still blocked")
	}
	if applied := <-newerDone; !applied {
		t.Fatal("newer RuntimeConfigTxn failed after the older rollback completed")
	}

	if got := service.currentConfig(); got == nil || got.Routing.Strategy != "weighted-round-robin" {
		t.Fatalf("published config after serialized rollback = %#v, want weighted-round-robin", got)
	}
	if selector := service.coreManager.Selector(); func() bool {
		_, ok := selector.(*coreauth.WeightedRoundRobinSelector)
		return ok
	}() == false {
		t.Fatalf("selector after serialized rollback = %T, want *WeightedRoundRobinSelector", selector)
	}
	retry, _, _ := service.coreManager.RetrySettings()
	if retry != newerCfg.RequestRetry {
		t.Fatalf("retry after serialized rollback = %d, want %d", retry, newerCfg.RequestRetry)
	}
}

// TestRuntimeConfigTxnSupersededFailureCompensatesOwnRuntimeEffects proves
// that a transaction superseded while a forward hook is running still undoes
// its own manager mutations. Supersession prevents stale config publication;
// it must not leave partial runtime state for a newer candidate to inherit.
func TestRuntimeConfigTxnSupersededFailureCompensatesOwnRuntimeEffects(t *testing.T) {
	previousEnabled := internalusage.StatisticsEnabled()
	internalusage.SetStatisticsEnabled(false)
	t.Cleanup(func() { internalusage.SetStatisticsEnabled(previousEnabled) })

	service, previousCfg, failingCfg, _ := runtimeConfigTxnFixture(t)
	newerCfg := previousCfg.CloneForRuntime()
	newerCfg.Routing.Strategy = "weighted-round-robin"
	newerCfg.RequestRetry = 11

	forwardPprofEntered := make(chan struct{})
	releaseForwardPprof := make(chan struct{})
	var forwardPprofEnteredOnce sync.Once
	var releaseForwardPprofOnce sync.Once
	release := func() { releaseForwardPprofOnce.Do(func() { close(releaseForwardPprof) }) }
	t.Cleanup(release)

	service.applyPprofConfigContextFn = func(_ context.Context, cfg *sdkconfig.Config) bool {
		if cfg.Routing.Strategy == "fill-first" {
			forwardPprofEnteredOnce.Do(func() { close(forwardPprofEntered) })
			<-releaseForwardPprof
			return false
		}
		return true
	}

	failingCommit := service.stageConfigUpdate(failingCfg)
	failingDone := make(chan bool, 1)
	go func() {
		failingDone <- service.applyConfigRuntime(context.Background(), failingCommit, false)
	}()

	select {
	case <-forwardPprofEntered:
	case <-time.After(time.Second):
		t.Fatal("superseded RuntimeConfigTxn did not reach the forward pprof barrier")
	}

	newerCommit := service.stageConfigUpdate(newerCfg)
	release()
	if applied := <-failingDone; applied {
		t.Fatal("superseded RuntimeConfigTxn with injected pprof failure unexpectedly committed")
	}

	assertRuntimeStateRestored(t, service, previousCfg, nil)
	if !service.applyConfigRuntime(context.Background(), newerCommit, false) {
		t.Fatal("newer RuntimeConfigTxn failed after superseded rollback completed")
	}
	if selector := service.coreManager.Selector(); func() bool {
		_, ok := selector.(*coreauth.WeightedRoundRobinSelector)
		return ok
	}() == false {
		t.Fatalf("selector after newer RuntimeConfigTxn = %T, want *WeightedRoundRobinSelector", selector)
	}
}
