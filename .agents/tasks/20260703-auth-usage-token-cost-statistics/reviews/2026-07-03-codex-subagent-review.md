# Codex 子代理评审

Review Status
- workflow.operation.name: independent_governance_scheme_review
- workflow.operation.status: completed
- workflow.review_scope.status: complete
- workflow.scope_check.status: clean
- workflow.findings.status: findings_reported
- verdict: changes_requested

Dispatch Receipt

- Mode: Codex 内部子代理 / 同工具子会话
- Command: `codex --ask-for-approval never exec -C /home/cheng/git-project/CLIProxyAPI --add-dir /home/cheng/git-project/Cli-Proxy-API-Management-Center -s read-only --ephemeral -`
- Model: `gpt-5.5`
- Sandbox: `read-only`
- Approval: `never`
- Session ID: `019f26d7-b8d2-73c2-8c18-c52462c11ea4`
- Writes: none by subagent

Review Scope

Codex 以只读方式评审后端任务目录、前端任务目录和指定源码事实：

- `internal/usage/logger_plugin.go`
- `sdk/cliproxy/auth/types.go`
- `src/utils/usage.ts`

本轮只做文档 / 契约评审。子代理没有修改文件、提交、推送或启动服务。

Scope Check

范围 clean。后端文档覆盖 `usage.auths` 聚合、单认证文件分页明细 API、`auth-files` usage 摘要和导入兼容。前端文档覆盖凭证 token / 估算金额列、详情弹窗、旧后端降级、i18n 和构建验证。

历史修复已确认：

- `total_tokens` 归一化口径已在前后端文档中对齐。
- `auth_index` 已按 opaque string 处理，并要求 URL encoding 或 query fallback。
- `usage.auths` 已定义为由 details 派生的聚合，不在导入时把派生聚合反向创建为事实明细。
- `estimated_cost_usd` 已明确为估算金额；后端第一阶段无价格表时返回 `null`，前端使用本地价格表。

Findings

### M-1

- Severity: medium
- Summary: 前端估算金额缺少部分价格覆盖契约，导致同一凭证同时使用已配置价格模型和未配置价格模型时，可能静默低估金额。
- Evidence:
  - 前端设计当时只区分“有模型价格”和“没有模型价格”。
  - 前端计划当时只覆盖全有价 / 全无价状态，没有覆盖 mixed priced/unpriced usage。
  - 当前 `calculateCost()` 在单个模型没有配置价格时返回 0：`src/utils/usage.ts`。
- Impact: 如果一个凭证混合使用已配置价格和缺失价格的模型，聚合金额只包含已配置模型，但 UI 可能把它展示成完整估算。用户会看到低估的单凭证金额，且 `0` 无法区分“免费”与“价格缺失”。
- Recommendation: 增加 `costStatus: complete | partial | unconfigured` 或等价字段，并记录 `missingPriceModels`。混合覆盖时应展示“已覆盖部分的估算金额 + 部分价格未配置”提示，并补充 mixed priced/unpriced 模型验证用例。
- Confidence: high

Scorecard

| Dimension | Score |
|---|---:|
| Scope Control | 5 |
| Evidence Quality | 4 |
| Correctness | 3 |
| Safety | 5 |
| Testability | 4 |
| Maintainability | 4 |

Verification Evidence

- 后端源码确认 `RequestDetail` 已有 `AuthIndex` 和 `TokenStats`，后端 total fallback 不重复计入 cached tokens，导入逻辑当前以 request details 作为事实来源。
- Auth 源码确认 `EnsureIndex()` 保留已有 `Auth.Index`，因此固定 16 位十六进制假设不安全，文档已处理该点。
- 前端源码确认当前 `extractTotalTokens()` 旧 fallback 会叠加 cached tokens，而文档已要求凭证统计使用后端一致 normalizer。
- 前端源码确认 `calculateCost()` 在模型缺少价格时返回 0，这正是 M-1 风险来源。

Open Questions / Limitations

- 业务代码尚未实现；运行时行为和测试仍属于后续工作。
- 明细接口仍是 path-based，文档已记录如果 `auth_index` 无法安全放入 Gin path params，需要切换为 query 参数方案。
- 子代理聚焦指定任务目录和源码事实，没有做全仓库实现审计。

Recommended Next Step

先修复 M-1：在前端计划文档中定义部分价格覆盖状态、展示行为和 mixed priced/unpriced 验证覆盖。修复后重新运行聚焦复审。
