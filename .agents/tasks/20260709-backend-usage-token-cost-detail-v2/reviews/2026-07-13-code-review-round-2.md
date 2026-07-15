# 后端代码评审 Round 2

## 评审结论

- Reviewer: `019f59f7-8ebf-7342-ba88-28dbfafde1c1`
- Verdict: `changes_requested`
- Scope: 当前 `dev` 工作区相对 `HEAD` 的非 `.agents` 后端改动。

## Round 1 Closure

- `BACKEND-USAGE-HIGH-001`: 已闭环。legacy total-only 导入已有回归测试。
- `BACKEND-USAGE-MED-002`: 已闭环。`UsageReporter` 不再在 helper 层合成 total。
- `BACKEND-USAGE-MED-003`: 已闭环。enrich 后顶层 success/failure 计数同步。

## Findings

### BACKEND-USAGE-HIGH-004

- Severity: High
- Summary: v1 legacy detail 同时有组件 token 和旧 `total_tokens` 时，旧 total 会被 computed total 覆盖。
- Impact: restore/import 会改写历史总 token，导致 auth totals、day/hour buckets 和成本明细漂移。
- Disposition: 已修复。`normaliseRequestTokens` 在缺少 v2 `reported_total_tokens` / `computed_total_tokens` 且存在 legacy `total_tokens` 时，优先把 legacy total 映射为 reported total。
- Regression: `TestRestoreRequestStatisticsPreservesLegacyTotalWhenComponentsExist`

### BACKEND-USAGE-HIGH-005

- Severity: High
- Summary: legacy snapshot 的 raw API key map key 可能原样进入 API map key 和 endpoint。
- Impact: 管理 API、export 和持久 snapshot 可能继续暴露历史 raw downstream API key。
- Disposition: 已修复。导入时只有真实 endpoint 才写入 `detail.Endpoint`；非 endpoint 的 legacy `APIs` map key 强制转换为稳定 `redacted:<hash>`，已脱敏标识不会二次 hash。
- Regression: `TestRestoreRequestStatisticsRedactsLegacyAPIMapKey`; 更新历史 merge 测试使用脱敏 key。

## Verification

- `docker run --rm -v "$PWD":/src -v cpa-go-mod-cache:/go/pkg/mod -v cpa-go-build-cache:/root/.cache/go-build -w /src golang:1.26 sh -lc 'export PATH=/usr/local/go/bin:$PATH GOPROXY=https://goproxy.cn,https://proxy.golang.org,direct; git config --global --add safe.directory /src; go test ./internal/usage ./internal/runtime/executor/helps'`
- `docker run --rm -v "$PWD":/src -v cpa-go-mod-cache:/go/pkg/mod -v cpa-go-build-cache:/root/.cache/go-build -w /src golang:1.26 sh -lc 'export PATH=/usr/local/go/bin:$PATH GOPROXY=https://goproxy.cn,https://proxy.golang.org,direct; git config --global --add safe.directory /src; go test ./internal/logging ./internal/usage ./internal/runtime/executor/helps ./internal/redisqueue ./internal/api/handlers/management'`
- `docker run --rm -v "$PWD":/src -v cpa-go-mod-cache:/go/pkg/mod -v cpa-go-build-cache:/root/.cache/go-build -w /src golang:1.26 sh -lc 'export PATH=/usr/local/go/bin:$PATH GOPROXY=https://goproxy.cn,https://proxy.golang.org,direct; git config --global --add safe.directory /src; go build -o test-output ./cmd/server && rm test-output'`
- `git diff --check`

## Next

派发后端代码 Round 3 独立评审，确认 Round 2 修复无新问题。
