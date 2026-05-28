# Auth Quota Threshold Auto Disable Implementation Plan

- Goal: 在现有零额度自动禁用链路上增加全局额度阈值禁用能力，默认保持旧行为。
- Input Mode: approved-spec
- Requirements Source: user-approved design in current task
- Canonical Spec Path: `.agents/tasks/20260527-auth-quota-threshold-auto-disable/specs/2026-05-27-auth-quota-threshold-auto-disable-design.md`
- Scope Boundary: 稳定。本轮只增加全局阈值配置、管理接口、运行时判断、文档示例与测试；不实现 provider 级阈值，不实现主动定时扫描，不改变 scoped-pool 阈值语义。
- Execution Route: direct-inline
- Why This Route: 变更沿用已有 quota check 和自动禁用链路，写面集中在配置、管理 API、`auth.Manager` 与测试，串行推进可控。

## File Structure

- Modify:
  - `internal/config/config.go`
    - 在 `QuotaExceeded` 中新增 `AutoDisableAuthFileQuotaThresholdPercent int`
    - 增加归一化逻辑，阈值 clamp 到 `0..50`
  - `config.example.yaml`
    - 增加 `auto-disable-auth-file-quota-threshold-percent: 0` 示例和说明
  - `sdk/cliproxy/auth/quota_check_async.go`
    - 将 `result.Exhausted` 触发条件扩展为 `result.Exhausted || remainingPercent <= threshold`
    - 保持配置开关 `auto-disable-auth-file-on-zero-quota` 作为总开关
    - 明确状态消息：耗尽触发使用 `auto_disabled_quota_exhausted`，阈值触发使用 `auto_disabled_quota_threshold`
  - `sdk/cliproxy/auth/quota_check_async_test.go`
    - 增加阈值等于、低于、高于场景
    - 覆盖阈值默认 `0` 时旧行为不变
    - 覆盖缺失 `RemainingPercent` 且非 `Exhausted` 时不禁用
    - 覆盖阈值从 `0` 动态修改为非 `0` 后的新行为
    - 覆盖并发/重复 quota check 下只禁用一次且状态消息与触发原因一致
  - `internal/api/handlers/management/quota.go`
    - 增加阈值 GET/PUT/PATCH handler
  - `internal/api/server.go`
    - 注册阈值管理路由
  - `internal/api/handlers/management/quota_test.go`
    - 覆盖阈值读写与 clamp/保存行为
  - `internal/watcher/diff/config_diff.go`
    - 增加阈值配置变更摘要
  - `internal/watcher/diff/config_diff_test.go`
    - 覆盖阈值 diff 输出
  - `internal/tui/config_tab.go`
    - 在配置页暴露阈值字段，保持现有开关字段不变
- Read:
  - `sdk/cliproxy/auth/quota_check.go`
  - `sdk/cliproxy/auth/scoped_pool.go`
  - `internal/config/routing_scoped_pool_test.go`
  - `internal/api/handlers/management/config_basic.go`
- Test:
  - `./sdk/cliproxy/auth`
  - `./internal/api/handlers/management`
  - `./internal/watcher/diff`
  - `./internal/config`
  - `./cmd/server` build

## Task Breakdown

### Task 1: 配置模型与归一化

- Objective: 增加全局阈值字段并保证默认 `0` 完全兼容旧行为。
- Implementation:
  - 在 `QuotaExceeded` 增加 `AutoDisableAuthFileQuotaThresholdPercent int`
  - 新增或复用配置 sanitize 路径，将小于 `0` 的值归零，大于 `50` 的值压到 `50`
  - 补充配置 diff 与示例配置
- Verification:
  - `go test ./internal/config ./internal/watcher/diff -count=1`

### Task 2: 运行时阈值禁用判断

- Objective: 扩展现有异步 quota check 后的自动禁用条件。
- Implementation:
  - 新增 helper：读取有效阈值、判断 `QuotaCheckResult` 是否达到禁用条件并返回触发原因
  - 推荐 helper 形态：
    ```go
    func shouldAutoDisable(result QuotaCheckResult, threshold int) (bool, string) {
        if result.Exhausted {
            return true, "exhausted"
        }
        if threshold > 0 && result.RemainingPercent != nil && *result.RemainingPercent <= threshold {
            return true, "threshold"
        }
        return false, ""
    }
    ```
  - 修改 `runQuotaCheck`，不要只在 `result.Exhausted` 时调用禁用逻辑
  - 修改 `applyAutoDisableFromQuotaCheck` 的 guard，使其接受明确耗尽或阈值命中，并根据 reason 写入状态消息
  - 保持 `runtime_only`、已禁用、unsupported provider 的现有跳过逻辑
  - 禁用后继续 `scheduler.upsertAuth`，确保普通调度和 scoped-pool 都看到 `disabled`
- Verification:
  - `go test ./sdk/cliproxy/auth -run 'TestMarkResult_.*Quota|TestScopedPool' -count=1`

### Task 3: 管理 API 与 TUI 配置入口

- Objective: 允许通过管理接口和 TUI 读取/更新全局阈值。
- Implementation:
  - 新增路由：
    - `GET /v0/management/quota-exceeded/auto-disable-auth-file-quota-threshold-percent`
    - `PUT /v0/management/quota-exceeded/auto-disable-auth-file-quota-threshold-percent`
    - `PATCH /v0/management/quota-exceeded/auto-disable-auth-file-quota-threshold-percent`
  - handler 使用现有配置更新模式，写入后走同一保存链路
  - TUI 配置页新增整数配置项
- Verification:
  - `go test ./internal/api/handlers/management -run Test.*Quota -count=1`

### Task 4: scoped-pool 关系回归保护

- Objective: 明确自动禁用阈值和 scoped-pool 阈值互不替代，且 disabled 优先。
- Implementation:
  - 增加测试：round-robin scoped-pool 中同一 quota check 同时命中 pool 阈值和禁用阈值时，最终 auth 为 disabled
  - 增加测试：fill-first 下 scoped-pool 不生效，但自动禁用阈值仍生效
  - 保持 scoped-pool 现有 `< quota-threshold-percent` 语义不变，避免破坏兼容
- Verification:
  - `go test ./sdk/cliproxy/auth -run 'Test.*Quota.*Threshold|TestScopedPool' -count=1`

### Task 5: 全量验证

- Objective: 确认配置、运行时、管理接口和编译链路完整。
- Verification:
  - `gofmt -w` on modified Go files
  - `go test ./internal/config ./internal/watcher/diff ./internal/api/handlers/management ./sdk/cliproxy/auth -count=1`
  - `go build -o test-output ./cmd/server && rm test-output`

## Acceptance Criteria

- 未配置新阈值时，`auto-disable-auth-file-on-zero-quota: true` 仍只在明确零额度/耗尽时禁用。
- 设置阈值为 `10` 后，`RemainingPercent` 为 `10` 或更低会自动禁用。
- `RemainingPercent` 为 `11` 不会自动禁用，除非 `Exhausted=true`。
- `RemainingPercent=nil` 且非耗尽分类不会按阈值自动禁用。
- 耗尽触发写入 `auto_disabled_quota_exhausted`；阈值触发写入 `auto_disabled_quota_threshold`。
- 阈值从 `0` 动态修改为非 `0` 后，新 quota check 使用新阈值。
- 并发/重复 quota check 不会造成重复禁用或错误状态消息。
- `fill-first` 和 `round-robin` 下自动禁用阈值都生效。
- scoped-pool 阈值仍只影响 round-robin scoped-pool 池内剔除，不持久禁用。
- 同时命中 scoped-pool 阈值和自动禁用阈值时，最终状态为 `disabled`。
- 管理 API 可读取和更新阈值。
- 示例配置、TUI 配置项、配置 diff 与测试同步更新。
