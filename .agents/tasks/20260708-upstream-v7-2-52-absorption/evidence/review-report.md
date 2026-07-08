# 评审报告

## 评审范围

- Diff 范围：`dev@181aa28a` 合入上游 `14b139661d98acbbd7ac19eb827754e78118736f` 后的 25 个文件。
- 重点模块：`sdk/cliproxy/auth`、`internal/runtime/executor`、`internal/runtime/executor/helps`、`internal/translator`、`sdk/api/handlers/openai`、`sdk/cliproxy`、`config.example.yaml`。
- 排除范围：前端仓库、真实 provider 联调、发布分支合入、标签和发版。

## 发现

### medium stream usage 已观察到 usage 后可能被后续 failure 抢占

- 位置：`internal/runtime/executor/openai_compat_executor.go`、`internal/runtime/executor/kimi_executor.go`、`internal/runtime/executor/codex_openai_images.go`
- 问题：上游新增 `StreamUsageBuffer` 后，将流式 usage 延迟到 defer 发布；如果 scanner/read 在已观察到 usage 后返回错误，错误路径先调用 `reporter.PublishFailure`，会因为 `UsageReporter` 的 `sync.Once` 抢先发布失败空记录，导致已观察到 token usage 丢失。
- 影响：影响 fork 新增的 token/cost 使用统计，尤其是流式请求在尾部读失败但已收到 usage chunk 的场景。
- 建议：错误路径先发布已观察到的 stream usage；没有 usage 时才发布 failure。
- 处理状态：已修复并补测试。
- Disposition: `fixed`

## 修复复核

- 修复项：在 OpenAI compatibility、Kimi、Codex OpenAI image stream 的错误路径使用 `if !streamUsage.Publish(ctx, reporter) { reporter.PublishFailure(ctx, err) }`。
- 复核命令或检查：
  - `go test ./internal/runtime/executor/helps -run 'TestStreamUsageBuffer|TestUsageReporterUsagePublishPreventsLaterFailure'`
  - `go test ./internal/runtime/executor/...`
  - 手工检查 `UsageReporter.Publish` 与 `PublishFailure` 均走 `sync.Once`，新增测试确认后续 failure 不会产生第二条记录。
- 结论：修复有效，未发现重复发布或 usage 丢失的新问题。

## 第二轮复评

- 检查项：
  - `invalid_grant` 只对 400/401 且包含 `invalid_grant` 的错误进入 30 分钟暂停，仍允许 fallback 到可用凭证。
  - Claude direct passthrough 改为完整 SSE event 粒度输出，有测试覆盖事件边界。
  - Codex WebSocket `message_too_big` 映射为 413 结构化错误，有测试覆盖。
  - Codex client model modalities 与 OpenAI compatibility config 字段互相对齐，image endpoint model 不暴露 chat/responses input modalities。
  - Antigravity thinking / signature 兼容变更有非流式和流式测试覆盖。
  - Claude Responses tool result 支持 image/file 结构化 content，有测试覆盖 input image。
- 新发现：无。
- Disposition: `not_applicable`

## 第三轮复评

- 触发原因：提交前复审发现 OpenAI-compatible stream 的 plain JSON error 分支仍可能在已观察到 usage 后直接 `PublishFailure`，导致 usage 被 failure 抢占。
- 修复项：`internal/runtime/executor/openai_compat_executor.go` 的 JSON error 分支改为先 `streamUsage.Publish(ctx, reporter)`，没有 buffered usage 时才 `PublishFailure`。
- 回归测试：新增 `TestOpenAICompatExecutorStreamJSONErrorPreservesObservedUsage`，覆盖“先收到 usage SSE data line，随后收到 plain JSON error line”的场景，断言最终 usage record 非 failure 且保留 total tokens。
- 复核命令或检查：
  - `go test ./internal/runtime/executor -run 'TestOpenAICompatExecutorStreamJSONErrorPreservesObservedUsage|TestOpenAICompatExecutorStreamRejectsPlainJSONAfterBlankLines' -count=1 -v`
  - `go test ./internal/runtime/executor/...`
  - `go test ./...`
  - `go build -buildvcs=false -o test-output ./cmd/server`
  - `git diff --check`
  - `rg -n "^(<<<<<<<|=======|>>>>>>>)" .`
- 新发现：无。
- Disposition: `fixed`

## 结论

- 是否存在阻断问题：否。
- 最后一轮是否无新增 finding：是。
- 是否存在未处理 high/medium：否。
- 剩余风险：真实 provider 响应格式仍需后续运行观察；本轮未做真实凭证联调。
