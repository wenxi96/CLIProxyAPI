# Task 3 Evidence: Manager Active Quota Refresh Integration

## Scope

- Task: Manager 接入 Touch 与后台查询。
- Date: 2026-06-24
- Verification status: not_run_deferred
- Reason: 用户明确要求“测试放到最后，代码文档先行”。

## Code Changes

- `sdk/cliproxy/auth/conductor.go`
  - `Manager` 增加 active quota refresh pool、cancel、scan interval、worker count 状态字段。
  - `SetConfig` 触发 active refresh 生命周期 reconcile。
  - `MarkResult` 在真实运行时结果记录、scheduler 更新和 quota enqueue 后 touch active pool。
  - `StopAutoRefresh` 同步停止 active quota refresh loop。
- `sdk/cliproxy/auth/quota_check.go`
  - `SetQuotaChecker` 触发 active refresh 生命周期 reconcile。
- `sdk/cliproxy/auth/quota_check_async.go`
  - 新增 active refresh 配置解析、启动/停止、touch、scan 和后台 check。
  - 后台 check 成功后复用 `ApplyQuotaCheckResult`，不复制禁用逻辑。
  - `ApplyQuotaCheckResult` 返回值收敛为“实际禁用了 auth”。
  - TTL、scan interval 或 worker count 变化时重建 active refresh loop。
- `sdk/cliproxy/auth/quota_check_async_test.go`
  - 补充成功请求只 touch、手动 scan 才触发 quota check 的测试。
  - 补充 active refresh disabled 时不建池、不查询的测试。
  - 补充低额度自动禁用后从 active pool 出池的测试。
  - 补充 scan interval 配置变化时重启 active pool 的测试。
  - 补充 auto-disable 关闭时 `ApplyQuotaCheckResult` 不误报 disabled 的测试。

## Contract Checks

- Request path does not call `QuotaChecker.Check`; it only touches the in-memory pool after successful runtime `MarkResult`.
- `/api-call` and auth-files batch check are not active pool inputs.
- Worker query errors, unsupported/deleted/disabled/runtime-only snapshots leave the pool through `markFailed` or snapshot rejection.
- Worker query success goes through the existing shared state entry `ApplyQuotaCheckResult`.

## Deferred Verification

Run at final verification stage:

```bash
docker run --rm -v "$PWD":/workspace -w /workspace golang:1.26 bash -lc '/usr/local/go/bin/gofmt -w sdk/cliproxy/auth/conductor.go sdk/cliproxy/auth/quota_check.go sdk/cliproxy/auth/quota_check_async.go sdk/cliproxy/auth/quota_check_async_test.go && /usr/local/go/bin/go test ./sdk/cliproxy/auth -run "TestMarkResult_.*ActiveQuota|TestActiveQuotaRefresh|TestApplyQuotaCheckResult" -count=1'
```

## Result

Code/docs are ready for final-stage formatting and focused verification. No test was run in this step by request.
