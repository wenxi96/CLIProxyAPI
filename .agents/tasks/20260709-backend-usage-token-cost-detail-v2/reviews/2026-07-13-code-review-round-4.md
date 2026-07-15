# 后端代码评审 Round 4

## 评审结论

- Reviewer: `019f5a24-383d-7b33-91f6-7e60df82d4ea`
- Verdict: `changes_requested`
- Scope: 当前 `dev` 工作区相对 `HEAD` 的非 `.agents` 后端改动，重点复核 Round 1 / Round 2 finding 闭环后的最终安全边界。

## Round Closure

- `BACKEND-USAGE-HIGH-001`: 已闭环。legacy total-only 导入已有回归测试。
- `BACKEND-USAGE-MED-002`: 已闭环。`UsageReporter` 不再在 helper 层合成 total。
- `BACKEND-USAGE-MED-003`: request_id 路径已闭环；无 request_id 边界见 `R4-002`，本轮已修复。
- `BACKEND-USAGE-HIGH-004`: 已闭环。legacy 组件 token 与旧 `total_tokens` 同时存在时保留旧 total。
- `BACKEND-USAGE-HIGH-005`: 已闭环。legacy 任意格式 `APIs` map key 已脱敏。

## Findings

### R4-001

- Severity: High
- Summary: `fail.body` 和错误摘要可能直接保留 upstream error body / `err.Error()`，未统一脱敏。
- Impact: 上游错误体中的 API key、Bearer token、cookie 等可能进入 redis queue payload、管理 API 或调试日志摘要。
- Disposition: 已修复。新增 `usage.SanitizeSensitiveText`，复用已有敏感值判断并对常见凭证模式做文本级替换；`redisqueue.resolveFail`、`UsageReporter.failFromErrors`、`SummarizeErrorBody` 均调用该 helper。
- Regression: `TestUsageQueuePluginRedactsFailureBody`; `TestSummarizeErrorBodyRedactsSensitiveValues`; `TestUsageReporterFailureRedactsSensitiveValues`

### R4-002

- Severity: Medium
- Summary: 无 `request_id` 的 fallback identity 包含 `latency_ms` 和 `failed`，missing usage 与后续 provider facts 可能无法 enrich，导致双计。
- Impact: 对未生成 request id 的执行路由，`EnsurePublished()` 先发 missing 后，后续 facts 可能新增第二条 detail，影响 total requests、success/failure 和 token 聚合。
- Disposition: 已修复。fallback identity 去除易变的 `latency_ms` 和 `failed`；`/v1beta/interactions` 加入 AI API request id 前缀。
- Regression: `TestRequestStatisticsRecordEnrichesWithoutRequestIDDifferentLatencyAndOutcome`; `TestIsAIAPIPathIncludesInteractions`

## Verification

- `docker run --rm -v "$PWD":/src -v cpa-go-mod-cache:/go/pkg/mod -v cpa-go-build-cache:/root/.cache/go-build -w /src golang:1.26 sh -lc 'export PATH=/usr/local/go/bin:$PATH GOPROXY=https://goproxy.cn,https://proxy.golang.org,direct; git config --global --add safe.directory /src; go test ./internal/logging ./internal/usage ./internal/runtime/executor/helps ./internal/redisqueue'` 通过。
- `docker run --rm -v "$PWD":/src -v cpa-go-mod-cache:/go/pkg/mod -v cpa-go-build-cache:/root/.cache/go-build -w /src golang:1.26 sh -lc 'export PATH=/usr/local/go/bin:$PATH GOPROXY=https://goproxy.cn,https://proxy.golang.org,direct; git config --global --add safe.directory /src; go test ./internal/logging ./internal/usage ./internal/runtime/executor/helps ./internal/redisqueue ./internal/api/handlers/management'` 通过。
- `docker run --rm -v "$PWD":/src -v cpa-go-mod-cache:/go/pkg/mod -v cpa-go-build-cache:/root/.cache/go-build -w /src golang:1.26 sh -lc 'export PATH=/usr/local/go/bin:$PATH GOPROXY=https://goproxy.cn,https://proxy.golang.org,direct; git config --global --add safe.directory /src; go build -o test-output ./cmd/server && rm test-output'` 通过。
- `git diff --check` 通过。

## Next

派发后端代码 Round 5 独立复审，确认 `R4-001`、`R4-002` 已闭环且没有新增问题。
