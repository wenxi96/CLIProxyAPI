# 后端代码评审 Round 5

## 评审结论

- Reviewer: `019f5a31-32bc-7e02-8364-44a45b683116`
- Verdict: `changes_requested`
- Scope: 当前 `dev` 工作区相对 `HEAD` 的非 `.agents` 后端改动，重点复核 Round 4 脱敏与无 request_id enrich 修复。

## Round Closure

- `R4-001`: 部分闭环。API key、Bearer、cookie 已覆盖；generic token 与 Basic auth 仍有缺口，见 `R5-001`。
- `R4-002`: 已闭环。fallback identity 已去除 latency/failed；`/v1beta/interactions` 已纳入 request id 前缀。

## Findings

### R5-001

- Severity: High
- Summary: `SanitizeSensitiveText` 未覆盖 `"token":"..."`、`token=...`、`x-api-token` 和 Basic/Digest auth 等常见泄漏模式。
- Impact: generic token 或 Basic auth 仍可能进入 queue fail body、UsageReporter failure body 或 error summary。
- Disposition: 已修复。`SanitizeSensitiveText` 增加 JSON key 递归脱敏，敏感 key 覆盖 `token`、`*_token`、`authorization`、`cookie`、`secret`、`api_key` 等；free-text 兜底覆盖 Bearer/Basic/Digest 和 token key-value，同时保留 `total_tokens` / `input_tokens` 等非敏感计数字段。
- Regression: `TestSanitizeSensitiveTextRedactsGenericTokensAndPreservesTokenCounters`; `TestUsageQueuePluginRedactsFailureBody`; `TestSummarizeErrorBodyRedactsSensitiveValues`; `TestUsageReporterFailureRedactsSensitiveValues`

### R5-002

- Severity: Medium
- Summary: 同一 `request_id` 下的 `additional` detail identity 只有 model/provider/executor/scope/detail_role，多次上报同一 additional model 可能被当作同一 detail。
- Impact: Codex image tool 同一请求内多次 completed usage 可能少计 additional model token/cost。
- Disposition: 已修复。新增 `detail_sequence`，`UsageReporter.PublishAdditionalModel` 对 additional usage 递增注入 sequence；`detailIdentityKey` 纳入 sequence；旧无 sequence 的 additional facts 使用 facts hash 区分，避免不同 facts 被覆盖。
- Regression: `TestUsageReporterPublishAdditionalModelAddsSequence`; `TestRequestStatisticsKeepsAdditionalSameModelSequences`

## Verification

- `docker run --rm -v "$PWD":/src -v cpa-go-mod-cache:/go/pkg/mod -v cpa-go-build-cache:/root/.cache/go-build -w /src golang:1.26 sh -lc 'export PATH=/usr/local/go/bin:$PATH GOPROXY=https://goproxy.cn,https://proxy.golang.org,direct; git config --global --add safe.directory /src; go test ./internal/logging ./internal/usage ./internal/runtime/executor/helps ./internal/redisqueue'` 通过。
- `docker run --rm -v "$PWD":/src -v cpa-go-mod-cache:/go/pkg/mod -v cpa-go-build-cache:/root/.cache/go-build -w /src golang:1.26 sh -lc 'export PATH=/usr/local/go/bin:$PATH GOPROXY=https://goproxy.cn,https://proxy.golang.org,direct; git config --global --add safe.directory /src; go test ./internal/logging ./internal/usage ./internal/runtime/executor/helps ./internal/redisqueue ./internal/api/handlers/management'` 通过。
- `docker run --rm -v "$PWD":/src -v cpa-go-mod-cache:/go/pkg/mod -v cpa-go-build-cache:/root/.cache/go-build -w /src golang:1.26 sh -lc 'export PATH=/usr/local/go/bin:$PATH GOPROXY=https://goproxy.cn,https://proxy.golang.org,direct; git config --global --add safe.directory /src; go build -o test-output ./cmd/server && rm test-output'` 通过。
- `git diff --check` 通过。

## Next

派发后端代码 Round 6 独立复审，确认 `R5-001`、`R5-002` 已闭环且没有新增问题。
