package cliproxy

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"time"

	internalusage "github.com/router-for-me/CLIProxyAPI/v7/internal/usage"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/watcher/synthesizer"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/config"
	log "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
)

func (s *Service) applyConfigUpdate(newCfg *config.Config) {
	s.applyConfigUpdateWithAuthSynthesis(context.Background(), newCfg, true)
}

func (s *Service) applyWatcherConfigUpdate(newCfg *config.Config) {
	s.applyConfigUpdateWithAuthSynthesis(context.Background(), newCfg, false)
}

type configCommit struct {
	cfg                   *config.Config
	sequence              uint64
	previousCfg           *config.Config
	previousOldConfigYaml []byte
	prePublished          bool
	published             bool
	// Home commits publish the config before the lifetime can be replaced. If
	// replacement cancels the subsequent runtime work, keep that completed
	// config commit while still compensating any staged usage state.
	preservePublishedOnCancellation  bool
	previousUsageEnabled             bool
	previousUsageReady               bool
	previousUsageRestoreState        string
	previousUsagePersistenceInterval time.Duration
	usagePublication                 func()
	usageRollback                    func()
	// runtimeCompensations are reverse-order undo closures for every reversible
	// runtime hook that applied successfully during applyConfigRuntimeUnsafe.
	// rollbackConfigCommitWithOptions runs them in reverse order when the
	// transaction fails so a later hook failure cannot leave the manager, pprof,
	// server, plugin or executor in a partially applied split-brain state.
	runtimeCompensations []func() error
}

// RuntimeConfigTxn provides an explicit prepare/apply/commit/rollback boundary
// for configuration side effects. The legacy service entry point remains a
// boolean API while this transaction keeps usage restore and loop changes
// compensatable when a runtime hook fails.
type RuntimeConfigTxn struct {
	service                           *Service
	commit                            configCommit
	prepared                          bool
	applied                           bool
	closed                            bool
	preservePublishedConfigOnRollback bool
}

func (tx *RuntimeConfigTxn) Prepare(ctx context.Context) bool {
	if tx == nil || tx.service == nil || tx.commit.cfg == nil || !tx.service.configCommitCurrent(tx.commit) {
		return false
	}
	if ctx != nil && ctx.Err() != nil {
		return false
	}
	tx.prepared = true
	return true
}

func (tx *RuntimeConfigTxn) Apply(ctx context.Context, synthesizeConfigAuths bool) bool {
	if tx == nil || !tx.prepared || tx.closed || tx.service == nil {
		return false
	}
	if !tx.service.applyConfigRuntimeUnsafe(ctx, &tx.commit, synthesizeConfigAuths) {
		return false
	}
	tx.applied = true
	return true
}

func (tx *RuntimeConfigTxn) Commit() {
	if tx != nil && tx.applied {
		tx.closed = true
	}
}

func (tx *RuntimeConfigTxn) Rollback() {
	if tx == nil || tx.closed || tx.service == nil {
		return
	}
	tx.service.rollbackConfigCommitWithOptions(tx.commit, tx.preservePublishedConfigOnRollback)
	tx.closed = true
}

type routingRuntimeState struct {
	strategy           string
	sessionAffinity    bool
	sessionAffinityTTL time.Duration
}

func normalizedRoutingRuntimeState(cfg *config.Config) routingRuntimeState {
	state := routingRuntimeState{
		strategy:           "round-robin",
		sessionAffinityTTL: time.Hour,
	}
	if cfg == nil {
		return state
	}

	switch strings.ToLower(strings.TrimSpace(cfg.Routing.Strategy)) {
	case "weighted-round-robin", "weightedroundrobin", "wrr":
		state.strategy = "weighted-round-robin"
	case "fill-first", "fillfirst", "ff":
		state.strategy = "fill-first"
	}
	state.sessionAffinity = cfg.Routing.SessionAffinity
	if ttl := strings.TrimSpace(cfg.Routing.SessionAffinityTTL); ttl != "" {
		if parsed, errParse := time.ParseDuration(ttl); errParse == nil && parsed > 0 {
			state.sessionAffinityTTL = parsed
		}
	}
	return state
}

func newRoutingSelector(state routingRuntimeState) coreauth.Selector {
	var selector coreauth.Selector
	switch state.strategy {
	case "weighted-round-robin":
		selector = &coreauth.WeightedRoundRobinSelector{}
	case "fill-first":
		selector = &coreauth.FillFirstSelector{}
	default:
		selector = &coreauth.RoundRobinSelector{}
	}
	if state.sessionAffinity {
		selector = coreauth.NewSessionAffinitySelectorWithConfig(coreauth.SessionAffinityConfig{
			Fallback: selector,
			TTL:      state.sessionAffinityTTL,
		})
	}
	return selector
}

func (s *Service) applyConfigUpdateWithAuthSynthesis(ctx context.Context, newCfg *config.Config, synthesizeConfigAuths bool) bool {
	commit := s.stageConfigUpdate(newCfg)
	if commit.cfg == nil {
		return false
	}
	return s.applyConfigRuntime(ctx, commit, synthesizeConfigAuths)
}

// commitConfigUpdate is the compatibility entry point used by the Home runtime
// path and tests that explicitly stage then apply a config. Synchronous service
// paths use stageConfigUpdate so publication occurs only after runtime Apply.
func (s *Service) commitConfigUpdate(newCfg *config.Config) configCommit {
	commit := s.stageConfigUpdate(newCfg)
	if commit.cfg == nil {
		return configCommit{}
	}
	s.cfgMu.Lock()
	s.cfg = commit.cfg
	s.oldConfigYaml, _ = yaml.Marshal(commit.cfg)
	s.cfgMu.Unlock()
	commit.published = true
	commit.prePublished = true
	return commit
}

func (s *Service) stageConfigUpdate(newCfg *config.Config) configCommit {
	if s == nil {
		return configCommit{}
	}

	s.configUpdateMu.Lock()
	defer s.configUpdateMu.Unlock()

	if newCfg == nil {
		s.cfgMu.RLock()
		newCfg = s.cfg
		s.cfgMu.RUnlock()
	}
	if newCfg == nil {
		return configCommit{}
	}
	if errValidate := newCfg.ValidateCredentialWeights(); errValidate != nil {
		log.WithError(errValidate).Warn("rejected config update with invalid credential weights")
		return configCommit{}
	}

	s.cfgMu.RLock()
	previousCfg := s.cfg
	s.cfgMu.RUnlock()
	if len(s.oldConfigYaml) > 0 {
		var snapshot config.Config
		if errUnmarshal := yaml.Unmarshal(s.oldConfigYaml, &snapshot); errUnmarshal == nil {
			previousCfg = &snapshot
		}
	}
	previousUsageEnabled := previousCfg != nil && previousCfg.UsageStatisticsEnabled
	previousUsageReady := internalusage.StatisticsReady()
	previousUsageRestoreState := s.usageRestoreStatus()
	previousUsagePersistenceInterval := usagePersistenceIntervalForConfig(previousCfg)
	previousCfgClone := previousCfg.CloneForRuntime()
	previousOldConfigYaml := append([]byte(nil), s.oldConfigYaml...)

	// Keep the candidate detached from callers and unpublished until Apply has
	// completed. This is the RuntimeConfigTxn publication barrier.
	candidate := newCfg.CloneForRuntime()
	s.configSequence++
	return configCommit{
		cfg:                              candidate,
		sequence:                         s.configSequence,
		previousCfg:                      previousCfgClone,
		previousOldConfigYaml:            previousOldConfigYaml,
		previousUsageEnabled:             previousUsageEnabled,
		previousUsageReady:               previousUsageReady,
		previousUsageRestoreState:        previousUsageRestoreState,
		previousUsagePersistenceInterval: previousUsagePersistenceInterval,
	}
}

func (s *Service) publishConfigCommit(commit *configCommit) bool {
	if s == nil || commit == nil || commit.cfg == nil || commit.sequence == 0 {
		return false
	}
	s.configUpdateMu.Lock()
	defer s.configUpdateMu.Unlock()
	if s.configSequence != commit.sequence {
		return false
	}
	s.cfgMu.Lock()
	s.cfg = commit.cfg
	s.oldConfigYaml, _ = yaml.Marshal(commit.cfg)
	s.cfgMu.Unlock()
	commit.published = true
	return true
}

func (s *Service) configCommitCurrent(commit configCommit) bool {
	if s == nil || commit.sequence == 0 {
		return false
	}
	s.configUpdateMu.Lock()
	current := s.configSequence == commit.sequence
	s.configUpdateMu.Unlock()
	return current
}

func (s *Service) applyConfigRuntime(ctx context.Context, commit configCommit, synthesizeConfigAuths bool) bool {
	if s == nil || commit.cfg == nil {
		return false
	}
	// Keep forward hooks, publication, and rollback compensation in one runtime
	// transaction. A newer candidate may be staged concurrently, but it cannot
	// apply runtime side effects until this transaction commits or fully undoes
	// its own changes.
	s.configRuntimeMu.Lock()
	defer s.configRuntimeMu.Unlock()

	txn := &RuntimeConfigTxn{service: s, commit: commit}
	rollback := func() {
		if commit.preservePublishedOnCancellation && ctx != nil && ctx.Err() != nil {
			txn.preservePublishedConfigOnRollback = true
		}
		txn.Rollback()
	}
	if !txn.Prepare(ctx) {
		rollback()
		return false
	}
	if !txn.Apply(ctx, synthesizeConfigAuths) {
		rollback()
		return false
	}
	if !commit.prePublished && !s.publishConfigCommit(&txn.commit) {
		rollback()
		return false
	}
	if txn.commit.usagePublication != nil {
		txn.commit.usagePublication()
	}
	txn.Commit()
	return true
}

func (s *Service) applyConfigRuntimeUnsafe(ctx context.Context, commit *configCommit, synthesizeConfigAuths bool) bool {
	if s == nil || commit == nil || commit.cfg == nil {
		return false
	}
	s.runtimeConfigCandidate.Store(commit.cfg)
	defer s.runtimeConfigCandidate.Store(nil)
	if !s.configCommitCurrent(*commit) {
		return false
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if errContext := ctx.Err(); errContext != nil {
		return false
	}
	usageCommit, usageRollback, errUsage := s.prepareUsagePersistenceConfigChange(commit.previousUsageEnabled, commit.previousUsagePersistenceInterval, commit.cfg)
	if errUsage != nil {
		restoreUsageRuntimeState(s, *commit)
		return false
	}
	commit.usagePublication = usageCommit
	commit.usageRollback = usageRollback

	// The management usage toggle changes only usage state. Avoid touching
	// unrelated runtime resources before the publication barrier.
	if runtimeConfigChangesOnlyUsage(*commit) {
		return true
	}

	// Reversible runtime hooks apply in declaration order. Each hook records a
	// compensation closure BEFORE it runs, so a hook that returns false after
	// mutating partial state (e.g. applyManagerConfig sets selector/retry/cooldown
	// before ApplyConfigWithCooldownStateStore can fail) is still compensated.
	// rollbackConfigCommitWithOptions runs the closures in reverse order on
	// failure, so a later hook failure cannot leave the manager, pprof, server,
	// plugin or executor in a partially applied split-brain state. Compensations
	// use a non-cancellable context so a canceled apply still restores prior state.
	addCompensation := func(undo func() error) {
		if undo != nil {
			commit.runtimeCompensations = append(commit.runtimeCompensations, undo)
		}
	}

	addCompensation(s.managerConfigCompensation())
	if !s.applyManagerConfig(ctx, *commit) {
		return false
	}
	if errContext := ctx.Err(); errContext != nil {
		return false
	}
	addCompensation(s.pprofConfigCompensation(commit.previousCfg))
	if !s.applyPprofConfigContext(ctx, commit.cfg) {
		return false
	}
	if errContext := ctx.Err(); errContext != nil {
		return false
	}
	addCompensation(s.serverClientsCompensation(commit.previousCfg))
	if !s.updateServerClientsContext(ctx, commit.cfg) {
		return false
	}
	if errContext := ctx.Err(); errContext != nil {
		return false
	}
	registrationCtx := coreauth.WithSkipPersist(ctx)
	if s.pluginHost != nil {
		addCompensation(s.pluginRuntimeConfigCompensation(commit.previousCfg))
		if !s.syncPluginRuntimeConfigForConfig(registrationCtx, commit.cfg) {
			return false
		}
	}
	if errContext := ctx.Err(); errContext != nil {
		return false
	}
	// Executor registration is additive: re-applying the previous config cannot
	// remove baseline/auth-derived executors added by the failed apply. Capture
	// the exact prior executor set and restore it on rollback instead.
	executorSnapshot := s.snapshotExecutors()
	var auths []*coreauth.Auth
	if s.coreManager != nil {
		auths = s.coreManager.List()
	}
	s.registerAvailableExecutors(registrationCtx, executorRegistrationOptions{
		includeBaseline:   commit.cfg.Home.Enabled,
		forceReplaceAuths: true,
		auths:             auths,
		cfg:               commit.cfg,
	})
	addCompensation(s.executorSnapshotCompensation(executorSnapshot))
	if errContext := ctx.Err(); errContext != nil {
		return false
	}
	// Test seam: abort right after executor registration so rollback compensates
	// every prior hook including plugin and executor state.
	if s.runtimeHookAfterExecutorsFn != nil && !s.runtimeHookAfterExecutorsFn() {
		return false
	}
	if synthesizeConfigAuths {
		s.registerConfigAPIKeyAuths(registrationCtx, commit.cfg)
		addCompensation(s.configAPIKeyAuthsCompensation(commit.previousCfg))
	}
	if errContext := ctx.Err(); errContext != nil {
		return false
	}
	if s.coreManager != nil && !commit.cfg.Home.Enabled && commit.cfg.SaveCooldownStatus {
		if errRestoreCooldown := s.coreManager.RestoreCooldownStates(registrationCtx); errRestoreCooldown != nil && ctx.Err() == nil {
			log.Warnf("failed to restore cooldown state after config update: %v", errRestoreCooldown)
		}
	}
	if errContext := ctx.Err(); errContext != nil {
		return false
	}
	s.syncPluginModelRuntimeForConfig(registrationCtx, commit.cfg)
	addCompensation(s.pluginModelRuntimeCompensation(commit.previousCfg))
	return ctx.Err() == nil
}

func (s *Service) rollbackConfigCommit(commit configCommit) {
	s.rollbackConfigCommitWithOptions(commit, false)
}

func (s *Service) rollbackConfigCommitWithOptions(commit configCommit, preservePublishedConfig bool) {
	if s == nil || commit.sequence == 0 {
		return
	}
	s.configUpdateMu.Lock()
	ownsPublishedGeneration := s.configSequence == commit.sequence
	if ownsPublishedGeneration && !preservePublishedConfig {
		s.cfgMu.Lock()
		s.cfg = commit.previousCfg.CloneForRuntime()
		s.oldConfigYaml = append([]byte(nil), commit.previousOldConfigYaml...)
		s.cfgMu.Unlock()
	}
	s.configUpdateMu.Unlock()
	// Runtime hook compensations only run for a real failure rollback. The Home
	// cancellation path (preservePublishedConfig) keeps the published config
	// and its partially applied runtime state for the replacement lifetime to
	// complete; it must not undo hooks that the new lifetime will re-apply.
	// A superseded transaction still compensates its own runtime changes while
	// configRuntimeMu excludes the newer runtime apply; it only skips writing an
	// older config generation back over the newer candidate.
	if !preservePublishedConfig {
		s.rollbackRuntimeCompensations(commit.runtimeCompensations)
	}
	if commit.usageRollback != nil {
		commit.usageRollback()
	} else {
		restoreUsageRuntimeState(s, commit)
	}
}

// rollbackRuntimeCompensations runs collected hook undo closures in reverse
// declaration order. A compensation failure is logged rather than silently
// dropped so a partial rollback is observable; remaining compensations still
// run so one failed hook cannot leave every prior hook uncompensated.
func (s *Service) rollbackRuntimeCompensations(compensations []func() error) {
	for index := len(compensations) - 1; index >= 0; index-- {
		undo := compensations[index]
		if undo == nil {
			continue
		}
		if err := undo(); err != nil {
			log.Errorf("runtime config rollback compensation failed: %v", err)
		}
	}
}

// managerConfigCompensation restores the exact in-memory manager state captured
// before applyManagerConfig. It intentionally does not re-run the forward hook,
// because doing so can repeat a blocked cooldown-store persistence side effect.
func (s *Service) managerConfigCompensation() func() error {
	if s == nil || s.coreManager == nil {
		return nil
	}
	managerState := s.coreManager.SnapshotRuntimeConfigState()
	previousTransientCooldown := coreauth.TransientErrorCooldownSeconds()
	var previousRoutingState *routingRuntimeState
	if s.appliedRoutingState != nil {
		state := *s.appliedRoutingState
		previousRoutingState = &state
	}
	return func() error {
		s.coreManager.RestoreRuntimeConfigState(managerState)
		coreauth.SetTransientErrorCooldownSeconds(previousTransientCooldown)
		s.appliedRoutingState = previousRoutingState
		return nil
	}
}

// pprofConfigCompensation re-applies the previous pprof config so a server
// started by the failed transaction is stopped or restored to the prior address.
func (s *Service) pprofConfigCompensation(previousCfg *config.Config) func() error {
	if s == nil || previousCfg == nil {
		return nil
	}
	previous := previousCfg.CloneForRuntime()
	return func() error {
		if !s.applyPprofConfigContext(context.Background(), previous) {
			return errors.New("runtime config rollback: restore pprof server")
		}
		return nil
	}
}

// serverClientsCompensation re-applies the previous server clients context.
func (s *Service) serverClientsCompensation(previousCfg *config.Config) func() error {
	if s == nil || previousCfg == nil {
		return nil
	}
	previous := previousCfg.CloneForRuntime()
	return func() error {
		if !s.updateServerClientsContext(context.Background(), previous) {
			return errors.New("runtime config rollback: restore server clients context")
		}
		return nil
	}
}

// pluginRuntimeConfigCompensation re-applies the previous plugin runtime config
// so plugin auth parsers, frontend auth providers and management routes match
// the prior configuration.
func (s *Service) pluginRuntimeConfigCompensation(previousCfg *config.Config) func() error {
	if s == nil || s.pluginHost == nil || previousCfg == nil {
		return nil
	}
	previous := previousCfg.CloneForRuntime()
	return func() error {
		if !s.syncPluginRuntimeConfigForConfig(coreauth.WithSkipPersist(context.Background()), previous) {
			return errors.New("runtime config rollback: restore plugin runtime config")
		}
		return nil
	}
}

// executorSnapshotCompensation restores the exact executor set captured before
// registerAvailableExecutors ran. Executor registration is additive, so
// re-applying the previous config cannot remove executors added by a failed
// apply; the snapshot/restore pair removes them and re-installs the prior set.
func (s *Service) executorSnapshotCompensation(snapshot map[string]coreauth.ProviderExecutor) func() error {
	if s == nil || s.coreManager == nil {
		return nil
	}
	return func() error {
		s.coreManager.RestoreExecutors(snapshot)
		return nil
	}
}

func (s *Service) snapshotExecutors() map[string]coreauth.ProviderExecutor {
	if s == nil || s.coreManager == nil {
		return nil
	}
	return s.coreManager.SnapshotExecutors()
}

// configAPIKeyAuthsCompensation re-synthesizes the previous config API key auths
// so the API key model alias table reflects the prior configuration.
func (s *Service) configAPIKeyAuthsCompensation(previousCfg *config.Config) func() error {
	if s == nil || s.coreManager == nil || previousCfg == nil {
		return nil
	}
	previous := previousCfg.CloneForRuntime()
	return func() error {
		s.registerConfigAPIKeyAuths(coreauth.WithSkipPersist(context.Background()), previous)
		return nil
	}
}

// pluginModelRuntimeCompensation re-registers plugin models for the previous
// config so the model registry reflects the prior baseline.
func (s *Service) pluginModelRuntimeCompensation(previousCfg *config.Config) func() error {
	if s == nil || s.pluginHost == nil || s.coreManager == nil || previousCfg == nil {
		return nil
	}
	previous := previousCfg.CloneForRuntime()
	return func() error {
		s.syncPluginModelRuntimeForConfig(coreauth.WithSkipPersist(context.Background()), previous)
		return nil
	}
}

func runtimeConfigChangesOnlyUsage(commit configCommit) bool {
	if commit.previousCfg == nil || commit.cfg == nil {
		return false
	}
	previous := commit.previousCfg.CloneForRuntime()
	candidate := commit.cfg.CloneForRuntime()
	if previous == nil || candidate == nil {
		return false
	}
	candidate.UsageStatisticsEnabled = previous.UsageStatisticsEnabled
	return reflect.DeepEqual(previous, candidate)
}

func (s *Service) applyManagerConfig(ctx context.Context, commit configCommit) bool {
	if s == nil || s.coreManager == nil || commit.cfg == nil {
		return s != nil && commit.cfg != nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if errContext := ctx.Err(); errContext != nil {
		return false
	}
	routingState := normalizedRoutingRuntimeState(commit.cfg)
	if s.appliedRoutingState == nil || *s.appliedRoutingState != routingState {
		s.coreManager.SetSelector(newRoutingSelector(routingState))
		s.appliedRoutingState = &routingState
	}
	s.applyRetryConfig(commit.cfg)
	store := s.resolveCooldownStateStore(commit.cfg)
	if !s.coreManager.ApplyConfigWithCooldownStateStore(ctx, commit.cfg, store) {
		return false
	}
	s.coreManager.SetOAuthModelAlias(commit.cfg.OAuthModelAlias)
	return true
}

func (s *Service) updateServerClientsContext(ctx context.Context, cfg *config.Config) bool {
	if s == nil || cfg == nil || (ctx != nil && ctx.Err() != nil) {
		return false
	}
	if s.updateServerClientsContextFn != nil {
		return s.updateServerClientsContextFn(ctx, cfg)
	}
	if s.server == nil {
		return true
	}
	return s.server.UpdateClientsContext(ctx, cfg)
}

func (s *Service) reloadConfigFromWatcher() bool {
	if s == nil || s.watcher == nil {
		return false
	}
	return s.watcher.ReloadConfigIfChanged()
}

func (s *Service) registerConfigAPIKeyAuths(ctx context.Context, cfg *config.Config) {
	if s == nil || s.coreManager == nil || cfg == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	configSynth := synthesizer.NewConfigSynthesizer()
	auths, errSynthesize := configSynth.Synthesize(&synthesizer.SynthesisContext{
		Config:      cfg,
		Now:         time.Now(),
		IDGenerator: synthesizer.NewStableIDGenerator(),
	})
	if errSynthesize != nil {
		log.Warnf("failed to synthesize config API key auths: %v", errSynthesize)
		return
	}

	registrationCtx := withSkipAuthLifecycleSync(coreauth.WithDeferredAPIKeyModelAliasRebuild(ctx))
	tasks := make([]modelRegistrationTask, 0, len(auths))
	needsAliasRebuild := false
	for _, auth := range auths {
		if !coreauth.IsConfigAPIKeyAuth(auth) {
			continue
		}
		prepared := s.prepareCoreAuthForModelRegistration(registrationCtx, auth)
		if prepared == nil {
			continue
		}
		needsAliasRebuild = true
		authForRegistration := prepared
		tasks = append(tasks, modelRegistrationTask{
			phase:    modelRegistrationPhaseConfigAPIKey,
			category: modelRegistrationCategory(authForRegistration),
			run: func(compatCache *openAICompatibilityRegistrationCache) {
				s.completeModelRegistrationForAuthWithCache(registrationCtx, authForRegistration, compatCache)
			},
		})
	}
	if needsAliasRebuild {
		s.coreManager.RefreshAPIKeyModelAlias()
	}
	s.runModelRegistrationTasks(registrationCtx, tasks)
}

func forceHomeRuntimeConfig(cfg *config.Config) {
	if cfg == nil {
		return
	}
	cfg.APIKeys = nil
	cfg.UsageStatisticsEnabled = true
	cfg.DisableCooling = true
	cfg.SaveCooldownStatus = false
	cfg.WebsocketAuth = false
	cfg.RemoteManagement.AllowRemote = false
	cfg.RemoteManagement.DisableControlPanel = true
	cfg.Plugins.StoreAuth = nil
}
