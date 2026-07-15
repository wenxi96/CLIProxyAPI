# 后端计划 Round 2 独立评审与处置

## Review Status

- workflow.operation.name: `aw-plan-review / engineering / round-2`
- workflow.operation.status: `completed`
- workflow.review_scope.status: `required_docs_read`
- workflow.scope_check.status: `in_scope_with_updates`
- workflow.findings.status: `no_critical_or_high; medium_updates_only`
- verdict: `ready_with_updates`

## Review Summary

- Reviewer: `multi_agent_v1` independent subagent
- Scope: `.agents/tasks/20260709-backend-usage-token-cost-detail-v2/` 计划文档与 `internal/usage`、`internal/runtime/executor/helps`、`internal/redisqueue`、`internal/logging`、`internal/api/handlers/management/usage.go` 源码抽查。
- Result: Round 1 的 high/low finding 已关闭；本轮发现 3 个 medium update，需要写回计划后复审。

## Findings Disposition

### PLAN-MED-001

- Disposition: accepted
- Summary: `detail_role` 被要求进入 identityKey，但未明确作为 persistent detail 字段持久化。
- Fix: 设计和计划补充 `detail_role` 顶层字段、默认 `primary`、参与 import/export identity，并增加同 request 同 model 不同 role 保留测试。

### PLAN-MED-002

- Disposition: accepted
- Summary: empty facts enrich 的测试口径需要显式覆盖 aggregate totals 校正。
- Fix: 设计和计划补充 enrich 空 facts 后不增加 request count，但必须更新 `TotalTokens`、model/auth `TokenStats`、`tokensByDay` / `tokensByHour`。

### PLAN-MED-003

- Disposition: accepted
- Summary: 安全测试应明确覆盖 `APIs` map key 和 queue headers 这两个泄漏面。
- Fix: 设计和计划补充 raw downstream API key 不得作为 `APIs` map key，queue `response_headers` 必须过滤或脱敏 Authorization、Cookie、Set-Cookie、token/key 类 header，并加入完整 JSON 泄漏断言。

## Scorecard

- Scope Control: 4
- Evidence Quality: 4
- Correctness: 4
- Safety: 4
- Testability: 4
- Maintainability: 4

## Verification Evidence

- 子代理只读评审了指定计划/设计文档。
- 子代理源码抽查确认当前存在 raw source、queue `api_key`、dedup token facts、usage client_ip 直接读 Gin context 等实现风险。

## Recommended Next Step

- 已采纳并修复计划；进入 Round 3 独立复审。
