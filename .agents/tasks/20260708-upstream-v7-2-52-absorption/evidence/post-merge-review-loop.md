# 合并后评审循环报告

## 候选范围

- 合并候选：`dev@181aa28a` + `upstream/main@14b139661d98acbbd7ac19eb827754e78118736f`
- 变更文件：25 个文件，覆盖配置、executor、translator、sdk 鉴权、Codex model metadata 和测试。
- 重点风险：fork token/usage 统计、OAuth 凭证暂停行为、流式 SSE 输出边界、Codex WebSocket 错误映射、translator thinking/signature 兼容。

## Review Loop

### Round 1

- 验证：
  - `git diff --check`
  - `git ls-files -u`
  - `rg -n "^(<<<<<<<|=======|>>>>>>>)" .`
  - `go test ./internal/translator/...`
  - `go test ./internal/runtime/executor/...`
  - `go test ./sdk/cliproxy/auth/... ./sdk/api/handlers/openai/... ./sdk/cliproxy/...`
- 评审：主线程手工评审 executor usage、auth invalid_grant、Codex model metadata、translator 变更和新增测试。
- 新发现：stream usage 已观察到 usage 后可能被后续 failure 抢占。
- 修复：调整 OpenAI compatibility、Kimi、Codex OpenAI image stream 的错误路径，先发布 buffered usage，再按需发布 failure；新增 usage reporter 防回归测试。
- 复验：
  - `go test ./internal/runtime/executor/helps -run 'TestStreamUsageBuffer|TestUsageReporterUsagePublishPreventsLaterFailure'`
  - `go test ./internal/runtime/executor/...`
- 结论：Round 1 finding 已修复。

### Round 2

- 验证：
  - `gofmt -w` 本轮 Go 文件。
  - `git diff --check`
  - `git ls-files -u`
  - `rg -n "^(<<<<<<<|=======|>>>>>>>)" .`
  - `go test ./internal/managementasset -run TestDownloadAssetAllowsSlowBodyAfterHeaders -count=3 -v`
  - `go test ./internal/managementasset -count=1`
  - `go test ./...`
  - `go build -buildvcs=false -o test-output ./cmd/server`
- 评审：复查 Round 1 修复后的完整候选；检查新增 helper 与已有 `UsageReporter` 发布语义一致，检查上游新增测试覆盖主要行为。
- 新发现：无。
- 修复：无。
- 复验：第二轮全量测试和构建通过；构建产物无残留。
- 结论：最后一轮无新增 finding。

### Round 3

- 验证：
  - `go test ./internal/runtime/executor -run 'TestOpenAICompatExecutorStreamJSONErrorPreservesObservedUsage|TestOpenAICompatExecutorStreamRejectsPlainJSONAfterBlankLines' -count=1 -v`
  - `go test ./internal/runtime/executor/...`
  - `go test ./internal/runtime/executor/helps -run 'TestStreamUsageBuffer|TestUsageReporterUsagePublishPreventsLaterFailure' -count=1`
  - `go test ./...`
  - `go build -buildvcs=false -o test-output ./cmd/server`
  - `git diff --check`
  - `rg -n "^(<<<<<<<|=======|>>>>>>>)" .`
- 评审：复查 OpenAI-compatible stream JSON error 分支、scanner/read error 分支、Codex image stream 和 Kimi stream 的 usage/failure 发布顺序。
- 新发现：无。
- 修复：Round 2 后发现的 plain JSON error 分支 usage 抢占问题已修复，并新增回归测试。
- 复验：聚焦测试、executor 包、全量测试和构建均通过。
- 结论：最后一轮无新增 finding。

## 退出条件核对

- 最后一轮是否无新增 finding：是。
- 是否存在未处理 high/critical：否。
- 是否存在未处理 medium：否。
- low/nit 是否已修复、标记不适用或记录为用户认可的剩余风险：是。
- 与 claim 匹配的验证是否通过：是。
- 是否可进入提交/推送：代码候选可提交；仍需用户明确授权后才能提交和推送。
