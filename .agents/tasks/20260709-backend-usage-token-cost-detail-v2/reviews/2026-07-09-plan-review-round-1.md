# 后端计划 Round 1 独立评审与处置

## Review Summary

- Reviewer: `codex exec --ephemeral -s read-only`
- Verdict: `changes_requested`
- Scope: `.agents/tasks/20260709-backend-usage-token-cost-detail-v2/` 计划文档与相关 usage 源码抽查

## Findings Disposition

### PLAN-HIGH-001

- Disposition: accepted
- Summary: “不存储/输出原始密钥”没有落到 `source` 与 redis queue 的具体规则。
- Fix: 设计和计划补充安全来源规则，禁止 raw API key/access token/cookie 进入 `source`、snapshot、API 或 queue；queue 不再输出 raw `api_key`，并增加泄漏测试验收。

### PLAN-HIGH-002

- Disposition: accepted
- Summary: `identityKey` 优先 `request_id` 可能误合并同一请求下多模型 usage。
- Fix: 设计和计划改为 `request_id + provider + executor_type + model + auth_index/source + detail_role` scope；新增同 request 多 model / additional model 测试要求。

### PLAN-HIGH-003

- Disposition: accepted
- Summary: `ClientIPFromContext(ctx)` 直接读 Gin context 可能在异步 usage dispatch 中读到复用上下文。
- Fix: 设计和计划要求 request-time 快照 `client_ip`，helper 优先读不可变快照，再 fallback Gin；新增 recycled Gin context 回归测试。

### PLAN-LOW-001

- Disposition: accepted
- Summary: 计划把已存在的 `internal/logging/client_ip_test.go` 写成新建。
- Fix: 文件结构改为修改/扩展现有测试文件。
