# Handoff

## Current State

实现已完成并已合入当前分支。当前任务用于追踪“全局额度阈值自动禁用”能力，业务代码、配置入口、管理 API、TUI、配置 diff、示例配置和测试均已落地。

## Completed Scope

- 保留 `quota-exceeded.auto-disable-auth-file-on-zero-quota` 作为自动禁用总开关。
- 新增 `quota-exceeded.auto-disable-auth-file-quota-threshold-percent` 全局阈值配置。
- 扩展异步 quota check 自动禁用逻辑，支持 `remaining_percent <= threshold`。
- 区分 `auto_disabled_quota_exhausted` 与 `auto_disabled_quota_threshold`。
- 管理 API、TUI 配置页、示例配置和 watcher diff 已同步。
- fill-first、round-robin、scoped-pool 关系已有回归测试覆盖。

## Verification

- `git diff --check` passed.
- `go test ./sdk/cliproxy/auth -run 'TestMarkResult_AutoDisablesAuthOnThresholdHit|TestMarkResult_DoesNotDisableOnThresholdWhenAboveThreshold|TestMarkResult_DoesNotDisableOnThresholdWhenRemainingPercentNil|TestMarkResult_DisablesOnZeroThresholdOnlyWhenExhausted|TestShouldAutoDisable|TestEffectiveAutoDisableThresholdClampsRuntimeConfig|TestMarkResult_UsesUpdatedThresholdConfigForNextQuotaCheck|TestMarkResult_DeduplicatesConcurrentThresholdQuotaChecks|TestMarkResult_AutoDisableThresholdAppliesWhenFillFirstDisablesScopedPool|TestMarkResult_AutoDisableThresholdTakesPriorityOverScopedPoolLowQuota' -count=1` passed via Go container.
- `go test ./internal/config ./internal/watcher/diff ./internal/api/handlers/management -run 'Test.*Quota|TestBuildConfigChangeDetails' -count=1` passed via Go container.
- `go build -o test-output ./cmd/server && rm test-output` passed via Go container.

## Remaining Work

无当前任务内剩余实现项。后续 provider 级阈值配置、主动定时扫描不在本任务范围内，如需推进应新建独立任务。

## Key Constraints

- 不复用或改写历史任务 `20260408-auth-zero-quota-auto-disable` 的规划文档。
- 保持旧配置默认行为兼容。
- 不引入 provider 级阈值。
- 不引入主动定时扫描。
