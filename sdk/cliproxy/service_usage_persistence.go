package cliproxy

import (
	"context"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/redisqueue"
	internalusage "github.com/router-for-me/CLIProxyAPI/v7/internal/usage"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/config"
	log "github.com/sirupsen/logrus"
)

const usagePersistenceDisabledPollInterval = 5 * time.Second

const (
	usageRestoreStateReady       = "ready"
	usageRestoreStateInProgress  = "restore_in_progress"
	usageRestoreStateUnavailable = "restore_unavailable"
)

func usagePersistenceIntervalForConfig(cfg *config.Config) time.Duration {
	if cfg == nil || cfg.UsageStatisticsPersistIntervalSeconds <= 0 {
		return 0
	}
	return time.Duration(cfg.UsageStatisticsPersistIntervalSeconds) * time.Second
}

func (s *Service) currentConfig() *config.Config {
	if s == nil {
		return nil
	}
	s.cfgMu.RLock()
	defer s.cfgMu.RUnlock()
	return s.cfg
}

// runtimeConfig returns the candidate config only to internal staging-aware
// helpers. Public readers continue to observe currentConfig until publication.
func (s *Service) runtimeConfig() *config.Config {
	if s == nil {
		return nil
	}
	if candidate := s.runtimeConfigCandidate.Load(); candidate != nil {
		return candidate
	}
	return s.currentConfig()
}

func (s *Service) usageStatisticsEnabled() bool {
	cfg := s.runtimeConfig()
	return cfg != nil && cfg.UsageStatisticsEnabled
}

func applyUsageStatisticsEnabled(enabled bool) {
	internalusage.SetStatisticsEnabled(enabled)
	redisqueue.SetUsageStatisticsEnabled(enabled)
}

func (s *Service) usagePersistenceInterval() time.Duration {
	return usagePersistenceIntervalForConfig(s.runtimeConfig())
}

func (s *Service) usageStatisticsFilePath() string {
	cfg := s.runtimeConfig()
	if cfg == nil {
		return ""
	}
	return internalusage.StatisticsFilePath(cfg)
}

func (s *Service) usageStatisticsStore() *internalusage.RequestStatistics {
	if s != nil && s.usageStats != nil {
		return s.usageStats
	}
	return internalusage.GetRequestStatistics()
}

func (s *Service) restoreUsageStatistics() {
	s.setUsageRestoreState(usageRestoreStateInProgress)
	if err := s.restoreUsageStatisticsWithError(); err != nil {
		applyUsageStatisticsEnabled(false)
		internalusage.SetStatisticsReady(false)
		s.setUsageRestoreState(usageRestoreStateUnavailable)
		log.WithError(err).Warn("usage statistics restore unavailable")
		return
	}
	applyUsageStatisticsEnabled(s.usageStatisticsEnabled())
	internalusage.SetStatisticsReady(true)
	s.setUsageRestoreState(usageRestoreStateReady)
}

func (s *Service) usageRestoreStatus() string {
	if s == nil {
		return usageRestoreStateUnavailable
	}
	s.usageRestoreMu.RLock()
	state := s.usageRestoreState
	s.usageRestoreMu.RUnlock()
	if state == "" {
		return usageRestoreStateReady
	}
	return state
}

func (s *Service) setUsageRestoreState(state string) {
	if s == nil {
		return
	}
	s.usageRestoreMu.Lock()
	s.usageRestoreState = state
	s.usageRestoreMu.Unlock()
}

func (s *Service) restoreUsageStatisticsWithError() error {
	if s == nil || !s.usageStatisticsEnabled() {
		return nil
	}
	path := s.usageStatisticsFilePath()
	if strings.TrimSpace(path) == "" {
		return nil
	}
	loaded, result, errRestore := internalusage.RestoreRequestStatistics(path, s.usageStatisticsStore())
	if errRestore != nil {
		return errRestore
	}
	if loaded {
		log.Infof("usage statistics restored from %s (added=%d skipped=%d)", path, result.Added, result.Skipped)
	}
	return nil
}

func (s *Service) persistUsageStatistics(reason string) {
	if err := s.persistUsageStatisticsWithError(reason); err != nil {
		log.WithError(err).Warnf("failed to persist usage statistics during %s", reason)
	}
}

func (s *Service) persistUsageStatisticsWithError(reason string) error {
	if s == nil {
		return nil
	}
	path := s.usageStatisticsFilePath()
	if strings.TrimSpace(path) == "" {
		return nil
	}
	saved, errPersist := internalusage.PersistRequestStatistics(path, s.usageStatisticsStore())
	if errPersist != nil {
		return errPersist
	}
	if !saved {
		return nil
	}
	switch reason {
	case "shutdown":
		log.Infof("usage statistics persisted to %s during shutdown", path)
	default:
		log.Debugf("usage statistics persisted to %s (%s)", path, reason)
	}
	return nil
}

func (s *Service) nextUsagePersistenceWait() time.Duration {
	if !s.usageStatisticsEnabled() {
		return usagePersistenceDisabledPollInterval
	}
	interval := s.usagePersistenceInterval()
	if interval <= 0 {
		return usagePersistenceDisabledPollInterval
	}
	return interval
}

func (s *Service) startUsagePersistenceLoop() {
	if s == nil {
		return
	}

	s.usagePersistenceMu.Lock()
	defer s.usagePersistenceMu.Unlock()
	if s.usagePersistenceCancel != nil {
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	s.usagePersistenceCancel = cancel
	s.usagePersistenceDone = done

	go func() {
		defer close(done)
		for {
			wait := s.nextUsagePersistenceWait()
			timer := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				return
			case <-timer.C:
			}

			if s.usageStatisticsEnabled() && s.usagePersistenceInterval() > 0 {
				s.persistUsageStatistics("periodic")
			}
		}
	}()
}

func (s *Service) restartUsagePersistenceLoop() {
	if s == nil {
		return
	}
	s.stopUsagePersistenceLoop()
	s.startUsagePersistenceLoop()
}

func (s *Service) applyUsagePersistenceConfigChange(previousEnabled bool, previousInterval time.Duration, newCfg *config.Config) {
	if err := s.applyUsagePersistenceConfigChangeWithError(previousEnabled, previousInterval, newCfg); err != nil {
		log.WithError(err).Warn("usage persistence config transaction failed")
	}
}

func (s *Service) applyUsagePersistenceConfigChangeWithError(previousEnabled bool, previousInterval time.Duration, newCfg *config.Config) error {
	commit, rollback, errPrepare := s.prepareUsagePersistenceConfigChange(previousEnabled, previousInterval, newCfg)
	if errPrepare != nil {
		if rollback != nil {
			rollback()
		}
		return errPrepare
	}
	if commit != nil {
		commit()
	}
	return nil
}

// prepareUsagePersistenceConfigChange performs durable restore/persist work
// without publishing usage flags or restarting the persistence loop. Those
// visible transitions are returned as a commit closure for the config
// publication barrier; rollback restores the exact pre-transaction state.
func (s *Service) prepareUsagePersistenceConfigChange(previousEnabled bool, previousInterval time.Duration, newCfg *config.Config) (func(), func(), error) {
	if s == nil || newCfg == nil {
		return func() {}, func() {}, nil
	}
	currentEnabled := newCfg.UsageStatisticsEnabled
	currentInterval := usagePersistenceIntervalForConfig(newCfg)
	previousReady := internalusage.StatisticsReady()
	previousRestoreState := s.usageRestoreStatus()

	if previousEnabled && !currentEnabled {
		if errPersist := s.persistUsageStatisticsWithError("disable"); errPersist != nil {
			return nil, func() {
				applyUsageStatisticsEnabled(previousEnabled)
				internalusage.SetStatisticsReady(previousReady)
				s.setUsageRestoreState(previousRestoreState)
			}, errPersist
		}
	}
	if !previousEnabled && currentEnabled {
		s.setUsageRestoreState(usageRestoreStateInProgress)
		if errRestore := s.restoreUsageStatisticsWithError(); errRestore != nil {
			s.setUsageRestoreState(usageRestoreStateUnavailable)
			return nil, func() {
				applyUsageStatisticsEnabled(previousEnabled)
				internalusage.SetStatisticsReady(previousReady)
				s.setUsageRestoreState(previousRestoreState)
			}, errRestore
		}
	}

	var commitOnce bool
	var rollbackOnce bool
	commit := func() {
		if commitOnce {
			return
		}
		commitOnce = true
		if previousEnabled != currentEnabled {
			applyUsageStatisticsEnabled(currentEnabled)
		}
		if !previousEnabled && currentEnabled {
			internalusage.SetStatisticsReady(true)
			s.setUsageRestoreState(usageRestoreStateReady)
		}
		if previousEnabled != currentEnabled || previousInterval != currentInterval {
			s.restartUsagePersistenceLoop()
		}
	}
	rollback := func() {
		if rollbackOnce {
			return
		}
		rollbackOnce = true
		applyUsageStatisticsEnabled(previousEnabled)
		internalusage.SetStatisticsReady(previousReady)
		s.setUsageRestoreState(previousRestoreState)
	}
	return commit, rollback, nil
}

func restoreUsageRuntimeState(s *Service, commit configCommit) {
	if s == nil {
		return
	}
	applyUsageStatisticsEnabled(commit.previousUsageEnabled)
	internalusage.SetStatisticsReady(commit.previousUsageReady)
	s.setUsageRestoreState(commit.previousUsageRestoreState)
}

func (s *Service) stopUsagePersistenceLoop() {
	if s == nil {
		return
	}

	s.usagePersistenceMu.Lock()
	cancel := s.usagePersistenceCancel
	done := s.usagePersistenceDone
	s.usagePersistenceCancel = nil
	s.usagePersistenceDone = nil
	s.usagePersistenceMu.Unlock()

	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
}
