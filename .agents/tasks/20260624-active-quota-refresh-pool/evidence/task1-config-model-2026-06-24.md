# Task 1 Evidence: Config Model

- Date: 2026-06-24 CST
- Scope: active quota refresh 配置模型、默认值和示例

## Changes

- Added `config.ActiveQuotaRefreshConfig`.
- Added `QuotaExceeded.ActiveQuotaRefresh`.
- Added defaults:
  - `DefaultActiveQuotaRefreshScanSec = 30`
  - `DefaultActiveQuotaRefreshTTLSec = 600`
  - `DefaultActiveQuotaRefreshWorkers = 1`
- Added minimum guards:
  - `MinActiveQuotaRefreshScanSec = 5`
  - `MinActiveQuotaRefreshTTLSec = 60`
- Added `NormalizeActiveQuotaRefreshConfig`.
- Added `config.example.yaml` sample under `quota-exceeded.active-quota-refresh`.
- Added config tests for default and sanitize behavior.

## Verification

```bash
docker run --rm -v "$PWD":/workspace -w /workspace golang:1.26 bash -lc '/usr/local/go/bin/gofmt -w internal/config/config.go internal/config/quota_exceeded_test.go && /usr/local/go/bin/go test ./internal/config -run "Test.*Quota|Test.*ActiveQuota" -count=1'
```

Result:

```text
ok  	github.com/router-for-me/CLIProxyAPI/v7/internal/config	0.011s
```

Additional checks:

```bash
git diff --check
rg -n "ActiveQuotaRefresh|active-quota-refresh|DefaultActiveQuotaRefresh|MinActiveQuotaRefresh" internal/config config.example.yaml
```

Result:

- `git diff --check`: clean
- Expected config symbols and example keys are present.

## Conclusion

Task 1 complete. 配置默认关闭，解析和 sanitize 行为已覆盖。
