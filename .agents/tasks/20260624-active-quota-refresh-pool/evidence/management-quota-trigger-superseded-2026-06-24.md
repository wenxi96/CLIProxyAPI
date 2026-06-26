# Management Quota Trigger Superseded Evidence

## Scope

核对前一轮“监测额度变化触发门禁”候选改动与本次 active quota refresh pool 方案的关系。

## Decision

`/api-call` quota 响应和认证文件批量检查结果自动触发低额度禁用的路径不再保留。

原因：

- active quota refresh pool 会基于真实运行时请求把认证文件入池，并由后台 worker 主动刷新 quota。
- 管理动作触发路径依赖用户打开配额管理页或执行批量检查，不能作为运行时门禁的稳定采样来源。
- 同时保留管理动作触发与 active pool 会形成两套主动门禁路径，增加额外查询、误触发风险和测试矩阵。

## Required Code Boundary

- 保留 `Manager.ApplyQuotaCheckResult(authID, result)`。
- 移除 `/api-call` 响应自动调用 `ApplyQuotaCheckResult` 的候选改动。
- 移除认证文件批量检查自动调用 `ApplyQuotaCheckResult` 的候选改动。
- 移除仅服务于上述管理动作复用的 `authquota.ResultFromAPICallResponse` 及相关测试。
- 不把 `/api-call` 或批量检查作为 active pool 入池或门禁触发源。

## Documentation Updated

- `.agents/tasks/20260624-active-quota-refresh-pool/task.md`
- `.agents/tasks/20260624-active-quota-refresh-pool/findings.md`
- `.agents/tasks/20260624-active-quota-refresh-pool/specs/2026-06-24-active-quota-refresh-pool-design.md`
- `.agents/tasks/20260624-active-quota-refresh-pool/plans/2026-06-24-active-quota-refresh-pool-implementation-plan.md`
- `.agents/tasks/20260624-active-quota-refresh-pool/handoff.md`
- `.agents/tasks/20260624-active-quota-refresh-pool/progress.md`

## Result

当前权威文档已明确：active quota refresh pool 是第一版唯一的主动 quota 采样方案；管理动作 quota 结果复用路径被视为 superseded，并由 Task 0 移除。
