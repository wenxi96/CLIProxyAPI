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

func (s *Service) usageStatisticsEnabled() bool {
	cfg := s.currentConfig()
	return cfg != nil && cfg.UsageStatisticsEnabled
}

func applyUsageStatisticsEnabled(enabled bool) {
	internalusage.SetStatisticsEnabled(enabled)
	redisqueue.SetUsageStatisticsEnabled(enabled)
}

func (s *Service) usagePersistenceInterval() time.Duration {
	return usagePersistenceIntervalForConfig(s.currentConfig())
}

func (s *Service) usageStatisticsFilePath() string {
	cfg := s.currentConfig()
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
	if s == nil || !s.usageStatisticsEnabled() {
		return
	}
	path := s.usageStatisticsFilePath()
	if strings.TrimSpace(path) == "" {
		return
	}
	loaded, result, errRestore := internalusage.RestoreRequestStatistics(path, s.usageStatisticsStore())
	if errRestore != nil {
		log.WithError(errRestore).Warnf("failed to restore usage statistics from %s", path)
		return
	}
	if loaded {
		log.Infof("usage statistics restored from %s (added=%d skipped=%d)", path, result.Added, result.Skipped)
	}
}

func (s *Service) persistUsageStatistics(reason string) {
	if s == nil {
		return
	}
	path := s.usageStatisticsFilePath()
	if strings.TrimSpace(path) == "" {
		return
	}
	saved, errPersist := internalusage.PersistRequestStatistics(path, s.usageStatisticsStore())
	if errPersist != nil {
		log.WithError(errPersist).Warnf("failed to persist usage statistics during %s", reason)
		return
	}
	if !saved {
		return
	}
	switch reason {
	case "shutdown":
		log.Infof("usage statistics persisted to %s during shutdown", path)
	default:
		log.Debugf("usage statistics persisted to %s (%s)", path, reason)
	}
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
	if s == nil || newCfg == nil {
		return
	}

	currentEnabled := newCfg.UsageStatisticsEnabled
	currentInterval := usagePersistenceIntervalForConfig(newCfg)

	if previousEnabled && !currentEnabled {
		s.persistUsageStatistics("disable")
	}
	if !previousEnabled && currentEnabled {
		s.restoreUsageStatistics()
	}
	if previousEnabled != currentEnabled || previousInterval != currentInterval {
		s.restartUsagePersistenceLoop()
	}
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
