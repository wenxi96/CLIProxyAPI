# 后端代码评审 Round 1

## 评审结论

- Reviewer: `019f59e9-8bb4-7573-8570-0a8f469a20e6`
- Verdict: `changes_requested`
- Scope: 当前 `dev` 工作区相对 `HEAD` 的非 `.agents` 后端改动，包含 `internal/usage/detail.go`。

## Findings

### BACKEND-USAGE-HIGH-001

- Severity: High
- Summary: v1 snapshot 中只有 `tokens.total_tokens` 的旧 detail 导入后会被归零。
- Impact: 老 snapshot restore/import 会丢失历史 token totals，影响 auth totals、day/hour buckets 和成本明细。
- Disposition: 已修复。`normaliseRequestTokens` 在没有 reported/computed total 时保留 legacy `total_tokens`，并将其归入 reported total 兼容路径。
- Regression: `TestRestoreRequestStatisticsPreservesLegacyTotalOnlyDetails`

### BACKEND-USAGE-MED-002

- Severity: Medium
- Summary: `UsageReporter` 在 publish 前合成 `input + output + reasoning` 到 `TotalTokens`，绕过 v2 reasoning 去重逻辑。
- Impact: 无 provider-reported total 但有 reasoning tokens 的 usage 可能被双算。
- Disposition: 已修复。移除 helper 层 total 合成，保持 Reporter 只传 provider facts，由 `internal/usage` 统一归一化。
- Regression: `TestUsageReporterDoesNotSynthesizeTotalTokens`

### BACKEND-USAGE-MED-003

- Severity: Medium
- Summary: enrich 替换 detail 的 `Failed` 状态时，只更新 token delta，不同步顶层 success/failure aggregates。
- Impact: failure-first / facts-later 场景中顶层成功失败计数可能与 detail/auth 派生统计不一致。
- Disposition: 已修复。enrich 后调用 outcome delta，同步 `successCount` / `failureCount`。
- Regression: `TestRequestStatisticsRecordEnrichUpdatesOutcomeCounts`

## Verification

- `docker run --rm -v "$PWD":/src -v cpa-go-mod-cache:/go/pkg/mod -v cpa-go-build-cache:/root/.cache/go-build -w /src golang:1.26 sh -lc 'export PATH=/usr/local/go/bin:$PATH GOPROXY=https://goproxy.cn,https://proxy.golang.org,direct; git config --global --add safe.directory /src; go test ./internal/usage ./internal/runtime/executor/helps'`
- `docker run --rm -v "$PWD":/src -v cpa-go-mod-cache:/go/pkg/mod -v cpa-go-build-cache:/root/.cache/go-build -w /src golang:1.26 sh -lc 'export PATH=/usr/local/go/bin:$PATH GOPROXY=https://goproxy.cn,https://proxy.golang.org,direct; git config --global --add safe.directory /src; go test ./internal/logging ./internal/usage ./internal/runtime/executor/helps ./internal/redisqueue ./internal/api/handlers/management'`
- `docker run --rm -v "$PWD":/src -v cpa-go-mod-cache:/go/pkg/mod -v cpa-go-build-cache:/root/.cache/go-build -w /src golang:1.26 sh -lc 'export PATH=/usr/local/go/bin:$PATH GOPROXY=https://goproxy.cn,https://proxy.golang.org,direct; git config --global --add safe.directory /src; go build -o test-output ./cmd/server && rm test-output'`
- `git diff --check`

## Next

派发后端代码 Round 2 独立评审，确认上述修复无新问题。
