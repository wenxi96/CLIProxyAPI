# Closeout

## 结论

B 路 batch-check 的正式额度查询已调整为复用 `internal/authquota.Service`。同步 `/v0/management/auth-files/batch-check` 与异步 `/v0/management/auth-files/batch-check-jobs` 都经由 `checkSingleAuthFile`，因此共享同一 provider 查询与 details 组装语义。

## 改动摘要

- `sdk/cliproxy/auth/quota_check.go`: 为 `QuotaCheckResult` 增加 `Details map[string]any`，新增共享 `QuotaWindow` 与 `CodexRateLimitResetCredit` 类型。
- `internal/authquota/service.go`: 扩展 canonical quota query service，让 provider details 由 service 统一产出。
- `internal/authquota/service.go`: Codex 成功 usage 查询后追加 `https://chatgpt.com/backend-api/wham/rate-limit-reset-credits`，返回 `rate_limit_reset_credits`、`rate_limit_reset_credits_available_count`、`rate_limit_reset_credits_error`。
- `internal/api/handlers/management/auth_files_batch_check.go`: `checkSingleAuthFile` 改为调用 `authquota.Service.Check`；删除旧的 provider-specific 查询与解析函数。
- `internal/api/handlers/management/auth_files_batch_check_test.go`: 增加 Codex details parity 覆盖，验证 usage 与 reset credits 请求序列、windows null 字段、subscription 与 reset credits details。

## 字段对齐

- Codex `details.windows`: 使用共享 `QuotaWindow`，包含 `used_percent`、`remaining_percent`、`reset_at`、`reset_after_seconds`、`limit_window_seconds` 等可空字段。provider 未返回的字段以 JSON `null` 表达。
- Codex `details.plan_type`: 来自 usage payload。
- Codex `details.subscription_active_until`: 优先来自 usage payload，缺失时从 auth metadata/attributes/id token claims 补充。
- Codex `details.rate_limit_reset_credits`: 来自 reset credits endpoint，仅保留可用于展示的 reset credit 记录。
- Claude/Gemini CLI/Kimi/Antigravity: details 也迁移到 `internal/authquota.Service` 产出，batch-check 不再复制 provider 解析器。

## 验证

- `docker run --rm -v "$PWD":/workspace -w /workspace golang:1.26 go test ./internal/authquota ./internal/api/handlers/management ./sdk/cliproxy/auth`
- `docker run --rm -v "$PWD":/workspace -w /workspace golang:1.26 go build -buildvcs=false -o test-output ./cmd/server && rm -f test-output`
- `git diff --check`
- `rg -n "check(Codex|Claude|GeminiCLI|Kimi|Antigravity)AuthFile|extract.*BatchCheck|resolve.*BatchCheck|executeBatchCheckAPICall|classifyBatchCheckAPIResponse|codexBatchCheck|antigravityQuotaURLs|claudeBatchCheckWindows|antigravityBatchCheckGroups" internal/api/handlers/management/auth_files_batch_check.go`

## 未验证范围

- 未使用真实 Codex/Claude/Gemini/Kimi/Antigravity 凭证执行 live provider API 请求。
- provider 未来新增字段不会自动进入 details，仍需按共享 service contract 显式映射。
