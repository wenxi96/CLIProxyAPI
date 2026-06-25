# Task 4 Evidence: Auto-disable and Scoped-pool Regression Coverage

## Scope

- Task: 自动禁用与 scoped-pool 集成回归。
- Date: 2026-06-24
- Verification status: not_run_deferred
- Reason: 用户明确要求“测试放到最后，代码文档先行”。

## Code Coverage Added

- `sdk/cliproxy/auth/active_quota_refresh_pool_test.go`
  - 覆盖 threshold=40 下 remaining=41/55/70/71 的分层间隔。
  - 覆盖 unsupported、nil remaining、query failure 出池。
  - 覆盖 TTL 过期出池和 in-flight 去重。
- `sdk/cliproxy/auth/quota_check_async_test.go`
  - 覆盖 active refresh 查询结果达到 auto-disable threshold 后禁用 auth 并从 active pool 出池。
  - 覆盖 active refresh 查询结果高于 auto-disable threshold、但低于 scoped-pool threshold 时，更新 scoped-pool quota snapshot 并 eject scoped-pool auth。
  - 覆盖 auto-disable 配置关闭时，`ApplyQuotaCheckResult` 不误报 disabled。

## Contract Checks

- Active pool 不直接实现第二套禁用判断；禁用仍由 `ApplyQuotaCheckResult` 和 `applyAutoDisableFromQuotaCheck` 统一处理。
- Scoped-pool quota snapshot 仍由 `scheduler.applyScopedPoolQuotaCheck` 接收同一个 quota result。
- Disabled auth 会从 active pool 移除，避免继续轮询已禁用认证文件。

## Deferred Verification

Run at final verification stage:

```bash
docker run --rm -v "$PWD":/workspace -w /workspace golang:1.26 bash -lc '/usr/local/go/bin/gofmt -w sdk/cliproxy/auth/active_quota_refresh_pool.go sdk/cliproxy/auth/active_quota_refresh_pool_test.go sdk/cliproxy/auth/quota_check_async.go sdk/cliproxy/auth/quota_check_async_test.go && /usr/local/go/bin/go test ./sdk/cliproxy/auth -run "Test.*ActiveQuota|Test.*ScopedPool.*Quota|TestMarkResult_.*Quota|TestApplyQuotaCheckResult" -count=1'
```

## Result

Regression coverage is in place for final-stage formatting and focused verification. No test was run in this step by request.
