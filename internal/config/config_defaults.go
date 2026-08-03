package config

const (
	DefaultPanelGitHubRepository        = "https://github.com/wenxi96/Cli-Proxy-API-Management-Center"
	DefaultPprofAddr                    = "127.0.0.1:8316"
	DefaultScopedPoolLimit              = 5
	DefaultScopedPoolErrorLimit         = 3
	DefaultScopedPoolPenaltySec         = 300
	DefaultScopedPoolQuotaTTLSec        = 300
	DefaultScopedPoolIdleLogSec         = 60
	MaxScopedPoolQuotaPercent           = 50
	MaxAutoDisableQuotaThresholdPercent = 50
	DefaultActiveQuotaRefreshScanSec    = 30
	DefaultActiveQuotaRefreshTTLSec     = 600
	DefaultActiveQuotaRefreshWorkers    = 1
	MinActiveQuotaRefreshScanSec        = 5
	MinActiveQuotaRefreshTTLSec         = 60
	DefaultAuthDir                      = "~/.cli-proxy-api"
)
