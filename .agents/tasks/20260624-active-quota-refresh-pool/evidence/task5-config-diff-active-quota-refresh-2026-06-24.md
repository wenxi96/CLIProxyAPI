# Task 5 Evidence: Active Quota Refresh Config Diff

## Scope

- Task: 配置变更展示与可选管理入口。
- Date: 2026-06-24
- Verification status: not_run_deferred
- Reason: 用户明确要求“测试放到最后，代码文档先行”。

## Code Changes

- `internal/watcher/diff/config_diff.go`
  - 增加 active quota refresh 配置变更展示：
    - `quota-exceeded.active-quota-refresh.enabled`
    - `quota-exceeded.active-quota-refresh.scan-interval-seconds`
    - `quota-exceeded.active-quota-refresh.active-ttl-seconds`
    - `quota-exceeded.active-quota-refresh.workers`
- `internal/watcher/diff/config_diff_test.go`
  - 在 flags/key 覆盖测试和 all-branches 覆盖测试中补充上述字段的断言。

## Management UI/API Decision

本任务第一版不改前端配置页，不新增管理 API。配置入口保持 YAML 后端配置，避免把后端 active pool 实现扩大到前端布局、i18n 和跨仓库发布链路。

## Deferred Verification

Run at final verification stage:

```bash
docker run --rm -v "$PWD":/workspace -w /workspace golang:1.26 bash -lc '/usr/local/go/bin/gofmt -w internal/watcher/diff/config_diff.go internal/watcher/diff/config_diff_test.go && /usr/local/go/bin/go test ./internal/watcher/diff -run "Test.*Quota|Test.*ConfigDiff|TestBuildConfigChangeDetails" -count=1'
```

## Result

Config diff code/docs are ready for final-stage formatting and focused verification. No test was run in this step by request.
