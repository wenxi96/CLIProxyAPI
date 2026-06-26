# Multi-Round Review Fix Summary

## Summary

本文件汇总 active quota refresh pool 设计与实施计划在多轮评审修复中发现的问题、处置方式和当前状态。

## Round 1: 文档口径误把候选改动写成稳定现状

- Finding: 设计文档 `Problem` 段把 `/api-call` quota 响应复用和认证文件批量检查结果复用描述成当前低额度自动禁用依赖来源。
- Impact: 后续实现者可能误以为这些管理动作触发路径必须保留，从而和 active quota refresh pool 形成两套主动门禁路径。
- Resolution:
  - 修正设计文档，明确当前稳定行为主要依赖“请求失败且像 quota 错误时触发异步 quota check”。
  - 把 `/api-call` 与批量检查结果复用标记为前一轮未提交候选方案。
  - 在 Task 0 中要求收敛这组候选改动。
- Evidence:
  - `.agents/tasks/20260624-active-quota-refresh-pool/evidence/design-plan-review-round1-2026-06-24.md`
  - `.agents/tasks/20260624-active-quota-refresh-pool/evidence/task0-quota-reuse-convergence-2026-06-24.md`

## Round 2: 管理动作触发门禁边界需要从“暂缓/移除”收紧为“移除”

- Finding: 用户补充指出，如果实现 active quota refresh pool，前面“监测额度变化触发门禁”的相关改动应评估是否不再需要。
- Impact: 若文档继续保留“暂缓/移除”这种可选措辞，后续执行者可能保留 `/api-call` 或批量检查触发门禁，造成额外查询、重复状态入口和更复杂的测试矩阵。
- Resolution:
  - 当前权威文档已明确：旧管理动作触发门禁路径被 active quota refresh pool supersede。
  - 第一版实现不保留 `/api-call` quota 响应自动调用 `ApplyQuotaCheckResult`。
  - 第一版实现不保留认证文件批量检查结果自动调用 `ApplyQuotaCheckResult`。
  - 移除仅服务于该旧路径的 `authquota.ResultFromAPICallResponse` 及相关测试。
  - 保留 `Manager.ApplyQuotaCheckResult(authID, result)`，作为 active pool worker、既有异步 quota check、scoped-pool 与自动禁用共享的唯一状态应用入口。
- Evidence:
  - `.agents/tasks/20260624-active-quota-refresh-pool/evidence/management-quota-trigger-superseded-2026-06-24.md`
  - `.agents/tasks/20260624-active-quota-refresh-pool/evidence/design-plan-review-round2-2026-06-24.md`

## Task 0 Resolution: 旧候选路径代码收敛

- Result:
  - `ResultFromAPICallResponse` removed.
  - `applyQuotaResultFromAPICall` removed.
  - `applyBatchCheckQuotaResult` / `batchCheckQuotaResult` removed.
  - `ApplyQuotaCheckResult` retained and used as shared state application entry.
- Verification:
  - `go test ./sdk/cliproxy/auth ./internal/authquota ./internal/api/handlers/management -run "Test.*Quota|Test.*Batch|Test.*APICall" -count=1`
  - `rg -n "ResultFromAPICallResponse|applyQuotaResultFromAPICall|applyBatchCheckQuotaResult|batchCheckQuotaResult|ApplyQuotaCheckResult" internal/api/handlers/management internal/authquota sdk/cliproxy/auth -g '*.go'`
- Evidence:
  - `.agents/tasks/20260624-active-quota-refresh-pool/evidence/task0-quota-reuse-convergence-2026-06-24.md`

## Task 1 Resolution: 配置模型落地

- Result:
  - Added `quota-exceeded.active-quota-refresh`.
  - Default disabled.
  - Default scan interval: 30 seconds.
  - Default active TTL: 600 seconds.
  - Default workers: 1.
  - Unsafe values are normalized.
- Verification:
  - `go test ./internal/config -run "Test.*Quota|Test.*ActiveQuota" -count=1`
- Evidence:
  - `.agents/tasks/20260624-active-quota-refresh-pool/evidence/task1-config-model-2026-06-24.md`

## Task 2-5 Resolution: 实现代码文档先行

- Result:
  - Added active quota refresh pool state machine.
  - Integrated Manager runtime touch and background quota check worker.
  - Kept request path quota-query-free.
  - Kept management `/api-call` and batch check outside active pool activation and auto-disable trigger sources.
  - Added watcher diff display for active quota refresh config.
- Evidence:
  - `.agents/tasks/20260624-active-quota-refresh-pool/evidence/task2-active-quota-refresh-pool-state-machine-2026-06-24.md`
  - `.agents/tasks/20260624-active-quota-refresh-pool/evidence/task3-manager-active-quota-refresh-integration-2026-06-24.md`
  - `.agents/tasks/20260624-active-quota-refresh-pool/evidence/task4-auto-disable-scoped-pool-regression-2026-06-24.md`
  - `.agents/tasks/20260624-active-quota-refresh-pool/evidence/task5-config-diff-active-quota-refresh-2026-06-24.md`

## Task 6 Resolution: 最终验证中发现 disabled 出池缺口

- Finding: 最终 auth focused 测试发现，`ApplyQuotaCheckResult` 触发低额度自动禁用后，auth 可能仍留在 active quota refresh pool。
- Impact: 违反“禁用了也会出池”的任务契约。
- Resolution:
  - Added `Manager.removeActiveQuotaRefresh(authID)`.
  - `applyAutoDisableFromQuotaCheck` 在实际禁用成功后调用 `removeActiveQuotaRefresh`。
  - 保持请求路径只 touch，不新增同步 quota 查询。
- Verification:
  - `go test ./sdk/cliproxy/auth -run "Test.*ActiveQuota|Test.*ScopedPool.*Quota|TestMarkResult_.*Quota|TestApplyQuotaCheckResult" -count=1 -timeout 3m`
  - `go test ./sdk/cliproxy/auth ./internal/config ./internal/api/handlers/management ./internal/authquota ./internal/watcher/diff -count=1 -timeout 8m`
  - `go build -o test-output ./cmd/server && rm test-output`
- Evidence:
  - `.agents/tasks/20260624-active-quota-refresh-pool/evidence/task6-final-verification-2026-06-25.md`

## Current Review Verdict

No new design, implementation-plan, or focused verification findings remain. The current implementation is verified by the planned package test set and server build, and is pending user-authorized commit only.
