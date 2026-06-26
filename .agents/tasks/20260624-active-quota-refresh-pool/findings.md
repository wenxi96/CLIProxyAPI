# Findings

## Current Runtime Behavior

- 普通模型请求成功不会触发 quota 查询。`sdk/cliproxy/auth/quota_check.go` 的 `shouldEnqueueQuotaCheck` 在 `result.Success == true` 时直接返回 `false`。
- 当前自动禁用链路只在 quota check 结果进入 `Manager.ApplyQuotaCheckResult` 后判断是否禁用。
- 现有 quota checker 注册点在 `sdk/cliproxy/builder.go`，通过 `authquota.NewService(...)` 注入 `coreManager`。
- 现有 quota 结果应用入口为 `sdk/cliproxy/auth/quota_check_async.go` 的 `ApplyQuotaCheckResult`，同时更新 scoped-pool quota 快照和自动禁用状态。

## Existing Refresh Mechanisms

- 前端配额管理页没有几秒级自动 provider quota 刷新。
- 前端认证文件页有 `/auth-files` 列表自动刷新，间隔为 240 秒，不等同于 provider quota 查询。
- 批量检查任务进行中有 1.5 秒 job 轮询，但只在批量检查 active job 存在时运行。
- 后端已有 auth auto-refresh loop，但它刷新认证凭证，不是 provider quota。

## Design Constraints

- 用户不接受每次请求成功后额外 quota 查询。
- 用户希望低额度门禁更及时，不能用固定 15 分钟间隔导致 `threshold=40`、`remaining=41` 后长时间不再检查。
- 用户确认分层策略基于 `remaining_percent - threshold_percent`，而不是固定剩余额度区间。
- 用户确认活跃池 10 分钟无活动出池。

## Prior Uncommitted Quota Reuse Changes

- 当前工作区存在一组未提交改动，把 `/api-call` quota 响应和认证文件批量检查结果复用到 `ApplyQuotaCheckResult`。
- `ApplyQuotaCheckResult` 本身仍是活跃池需要的共享入口，应保留。
- `/api-call` 和批量检查自动触发门禁在实现活跃额度刷新池后不再需要，应在实现前移除，避免管理动作与运行时活跃池形成两套触发路径。
- `internal/authquota.ResultFromAPICallResponse` 主要服务于上述管理动作复用；Task 0 移除管理动作复用时，该 helper 和对应测试也应移除。

## Active Quota Refresh Implementation Facts

- `sdk/cliproxy/auth/conductor.go` 的 `Manager.MarkResult` 只在真实运行时结果记录后触发 active quota refresh touch；管理 API 链路不作为入池入口。
- `sdk/cliproxy/auth/quota_check_async.go` 的 active refresh worker 通过 `QuotaChecker.Check` 获取结果后，复用 `Manager.ApplyQuotaCheckResult` 应用 scoped-pool quota 快照和低额度自动禁用状态。
- `ApplyQuotaCheckResult` 的返回值语义是“实际禁用了 auth”，不是“结果达到禁用阈值”。这避免 auto-disable 配置关闭时 active pool 误认为 auth 已禁用并出池。
- Active refresh pool lifecycle 会在配置关闭、checker 缺失、TTL/scan interval/workers 变化时停止或重建；成功请求路径只 touch，不同步查询 quota。
- `internal/watcher/diff/config_diff.go` 已展示 `quota-exceeded.active-quota-refresh` 的 enabled、scan interval、active TTL 和 workers 变更。
- 自动禁用成功后，`Manager.applyAutoDisableFromQuotaCheck` 会调用 `removeActiveQuotaRefresh` 将 auth 从 active quota refresh pool 移除，满足 disabled auth 出池契约。
