# 验证报告

## 命令

| 命令 | 结果 | 说明 |
|---|---:|---|
| `git diff --check` | pass | 无空白错误。 |
| `git ls-files -u` | pass | 无 unmerged index。 |
| `rg -n "^(<<<<<<<|=======|>>>>>>>)" .` | pass | 无冲突标记。 |
| `docker run --rm -v "$PWD":/workspace -w /workspace golang:1.26 gofmt -w ...` | pass | 对本轮 Go 文件执行格式化。 |
| `docker run --rm -e GOPROXY=https://goproxy.cn,direct ... go build -buildvcs=false -o test-output ./cmd/server` | pass | 构建通过；已确认 `test-output` 无残留。 |
| `python3 /home/cheng/.agent-workstation/bootstrap/bootstrap.py standard-doc-audit --task .agents/tasks/20260708-upstream-v7-2-52-absorption --json` | pass | 任务文档审计 clean，`issue_count: 0`。 |
| `docker run --rm -e GOPROXY=https://goproxy.cn,direct ... go test ./internal/runtime/executor -run 'TestOpenAICompatExecutorStreamJSONErrorPreservesObservedUsage\|TestOpenAICompatExecutorStreamRejectsPlainJSONAfterBlankLines' -count=1 -v` | pass | 覆盖 plain JSON error 分支保留已观察到 usage。 |

## 聚焦验证

- 命令：`docker run --rm -e GOPROXY=https://goproxy.cn,direct -v "$PWD":/workspace -v cli-go-mod:/go/pkg/mod -v cli-go-cache:/root/.cache/go-build -w /workspace golang:1.26 go test ./internal/runtime/executor/helps -run 'TestStreamUsageBuffer|TestUsageReporterUsagePublishPreventsLaterFailure'`
- 结果：pass。
- 覆盖：stream usage buffer 保留最终 usage；usage 发布后后续 failure 不会覆盖 token 记录。

- 命令：`docker run --rm -e GOPROXY=https://goproxy.cn,direct -v "$PWD":/workspace -v cli-go-mod:/go/pkg/mod -v cli-go-cache:/root/.cache/go-build -w /workspace golang:1.26 go test ./internal/runtime/executor -run 'TestOpenAICompatExecutorStreamJSONErrorPreservesObservedUsage|TestOpenAICompatExecutorStreamRejectsPlainJSONAfterBlankLines' -count=1 -v`
- 结果：pass。
- 覆盖：OpenAI-compatible stream 在已观察到 usage 后遇到 plain JSON error line 时，保留 usage record 且不记录为 failure。

- 命令：`docker run --rm -e GOPROXY=https://goproxy.cn,direct ... go test ./internal/runtime/executor/...`
- 结果：pass。
- 覆盖：Claude executor passthrough、Codex WebSocket、OpenAI/Kimi/Codex stream usage 路径。

- 命令：`docker run --rm -e GOPROXY=https://goproxy.cn,direct ... go test ./internal/translator/...`
- 结果：pass。
- 覆盖：Antigravity / Claude / OpenAI translator 变更。

- 命令：`docker run --rm -e GOPROXY=https://goproxy.cn,direct ... go test ./sdk/cliproxy/auth/... ./sdk/api/handlers/openai/... ./sdk/cliproxy/...`
- 结果：pass。
- 覆盖：invalid_grant fallback、Codex client model modalities、OpenAI compatibility config model metadata。

## 全量验证

- 命令：`docker run --rm -e GOPROXY=https://goproxy.cn,direct -v "$PWD":/workspace -v cli-go-mod:/go/pkg/mod -v cli-go-cache:/root/.cache/go-build -w /workspace golang:1.26 go test ./...`
- 首轮结果：fail，一次性失败在 `internal/managementasset` 的 `TestDownloadAssetAllowsSlowBodyAfterHeaders`，错误为 `timeout awaiting response headers`。
- 复核：
  - `go test ./internal/managementasset -run TestDownloadAssetAllowsSlowBodyAfterHeaders -count=3 -v` 通过。
  - `go test ./internal/managementasset -count=1` 通过。
  - 第二轮 `go test ./...` 通过。
- 说明：首轮失败未复现，且失败包不在本轮合并 diff 内；按一次并行/时序敏感测试抖动记录。

## 未执行项

- 项目：真实 provider 联调。
- 原因：本轮是上游代码吸收，不持有生产 provider 凭证，也不应在治理文档中写入凭证。
- 风险：provider 真实响应格式仍依赖现有单测覆盖和后续运行观察。
