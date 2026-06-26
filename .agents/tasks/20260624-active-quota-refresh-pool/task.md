# Active Quota Refresh Pool Task

- Task ID: `20260624-active-quota-refresh-pool`
- Status: implementation_verified_pending_commit
- Input Mode: clear-requirements
- Scope: backend runtime quota refresh pool, optional frontend config exposure
- Related Prior Work:
  - `.agents/tasks/20260408-auth-zero-quota-auto-disable/`
  - `.agents/tasks/20260527-auth-quota-threshold-auto-disable/`

## Goal

为低额度自动禁用认证文件新增一个后端运行时“活跃额度刷新池”。当认证文件参与真实请求后入池，后台按配置节流刷新其真实额度，并复用现有 `ApplyQuotaCheckResult` 链路触发 scoped-pool 状态更新与低额度自动禁用。

## Scope

本任务做：

- 新增运行时活跃额度刷新池。
- 请求成功/失败后只 `touch` 活跃认证文件，不在请求路径同步查额度。
- 后台 worker 对池内认证文件按 `remaining_percent - threshold_percent` 差值计算下一次刷新时间。
- 10 分钟无活动自动出池。
- 认证文件禁用、删除、不支持 quota、查询异常时出池，下一次真实调用可重新入池。
- 复用现有 quota checker、`ApplyQuotaCheckResult`、自动禁用与 scoped-pool 状态更新逻辑。
- 收敛前一轮未提交的管理动作 quota 结果复用改动：保留 `ApplyQuotaCheckResult`，移除 `/api-call` 与批量检查自动触发门禁，避免与 active pool 形成并行门禁路径。

本任务不做：

- 不把普通请求成功路径改成同步 quota 查询。
- 不新增前端轮询 quota provider API。
- 不改变现有批量检查和配额管理页手动刷新行为。
- 不依赖配额管理页 `/api-call` 或认证文件批量检查来触发低额度门禁。
- 不改变 provider quota endpoint 解析范围。
- 不实现 per-provider 活跃刷新策略。

## Acceptance Criteria

- 启用后，真实请求会将支持 quota check 的认证文件加入活跃池。
- 同一个认证文件不会在请求路径立刻查额度。
- 后台扫描只处理活跃池内、未禁用、未删除、非 runtime-only、支持 quota check 的认证文件。
- `delta = remaining_percent - threshold_percent`：
  - `delta <= 0`：现有自动禁用链路禁用认证文件。
  - `0 < delta <= 15`：下一次检查间隔 120 秒。
  - `15 < delta <= 30`：下一次检查间隔 180 秒。
  - `delta > 30`：下一次检查间隔 300 秒。
- 10 分钟没有真实请求活动的认证文件自动出池。
- 查询异常出池，直到下一次真实调用再次入池。
- `workers` 默认 1，避免并发打爆 provider quota API。
- 默认配置应保守，不对升级用户产生不可预期高频 provider quota 查询。
- `/api-call` 和认证文件批量检查不会作为 active pool 的入池或门禁触发源。
