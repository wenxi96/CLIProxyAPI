# Task 0 Evidence: Quota Reuse Convergence

- Date: 2026-06-24 CST
- Scope: 收敛前置未提交 quota 改动

## Changes

- 保留 `sdk/cliproxy/auth/quota_check_async.go::ApplyQuotaCheckResult`。
- `runQuotaCheck` 改为调用 `ApplyQuotaCheckResult`，使后续 active quota refresh worker 可以复用同一个状态入口。
- 移除未提交候选方案中的管理动作自动触发路径：
  - `internal/api/handlers/management/api_tools.go::applyQuotaResultFromAPICall`
  - `internal/api/handlers/management/auth_files_batch_check.go::applyBatchCheckQuotaResult`
  - `internal/api/handlers/management/auth_files_batch_check.go::batchCheckQuotaResult`
  - `internal/authquota/service.go::ResultFromAPICallResponse`
- 移除上述暂缓方案对应测试。
- 保留并新增保护性测试：成功请求即使开启阈值门禁，也不会同步或异步立即调用 quota checker。

## Verification

```bash
docker run --rm -v "$PWD":/workspace -w /workspace golang:1.26 bash -lc '/usr/local/go/bin/gofmt -w sdk/cliproxy/auth/quota_check_async.go sdk/cliproxy/auth/quota_check_async_test.go internal/api/handlers/management/api_tools.go internal/api/handlers/management/api_tools_test.go internal/api/handlers/management/auth_files_batch_check.go internal/api/handlers/management/auth_files_batch_check_test.go internal/authquota/service.go internal/authquota/service_test.go && /usr/local/go/bin/go test ./sdk/cliproxy/auth ./internal/authquota ./internal/api/handlers/management -run "Test.*Quota|Test.*Batch|Test.*APICall" -count=1'
```

Result:

```text
ok  	github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth	0.893s
ok  	github.com/router-for-me/CLIProxyAPI/v7/internal/authquota	0.035s
ok  	github.com/router-for-me/CLIProxyAPI/v7/internal/api/handlers/management	0.359s
```

Additional checks:

```bash
git diff --check
rg -n "ResultFromAPICallResponse|applyQuotaResultFromAPICall|applyBatchCheckQuotaResult|batchCheckQuotaResult|ApplyQuotaCheckResult" internal/api/handlers/management internal/authquota sdk/cliproxy/auth -g '*.go'
```

Result:

- `git diff --check`: clean
- residual symbols:
  - `sdk/cliproxy/auth/quota_check_async.go::ApplyQuotaCheckResult`
  - `sdk/cliproxy/auth/quota_check_async.go::runQuotaCheck` caller

## Conclusion

Task 0 complete. 管理动作 quota 结果复用已按计划暂缓/移除，`ApplyQuotaCheckResult` 作为后续 active quota refresh pool 的共享状态入口保留。
