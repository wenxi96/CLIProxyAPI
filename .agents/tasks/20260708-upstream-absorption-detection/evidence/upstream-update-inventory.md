# 上游更新清单

上游目标：`upstream/main@14b139661d98acbbd7ac19eb827754e78118736f`  
上游标签：`v7.2.52`

## 新增提交

| 提交 | 更新内容 | 影响模块 | 作用 | 风险与建议 |
|---|---|---|---|---|
| `3aa42a6f` | 处理 OAuth `invalid_grant` 错误并增加 retry suspension 逻辑 | `sdk/cliproxy/auth/conductor.go`; `sdk/cliproxy/auth/conductor_overrides_test.go` | 避免无效授权持续重试，改善认证生命周期处理 | 触碰 auth 调度逻辑；吸收后需跑 auth conductor 相关测试 |
| `ab6ed392` | 增加 Claude executor 完整 SSE event passthrough 单测，并调整 executor | `internal/runtime/executor/claude_executor.go`; `internal/runtime/executor/claude_executor_test.go` | 提升 Claude SSE 透传正确性覆盖 | 需关注 fork 是否有 Claude executor 定制行为 |
| `dc77bf4d` | 增强 Claude tool response 结构化 content 解析 | `internal/translator/claude/openai/responses/*` | 改善 Claude OpenAI Responses 转换能力 | 触碰 translator；按仓库规则需作为更大吸收的一部分评审，不能孤立处理 |
| `078ed178` | 为 Codex client models 增加 input/output modalities 支持 | `config.example.yaml`; `internal/config/config.go`; `sdk/api/handlers/openai/codex_client_models.go`; `sdk/cliproxy/service.go`; tests | 增强 OpenAI/Codex 兼容模型配置表达 | 触碰配置和服务模型生成；需复核 fork 配置示例与默认模型定制 |
| `4f157fbd` | 将 Codex WebSocket `message_too_big` 映射为结构化 API 响应 | `internal/runtime/executor/codex_websockets_executor.go`; tests | 改善错误返回可读性和协议一致性 | 需跑 Codex WebSocket executor 测试 |
| `dea47879` | 集中 OpenAI stream usage 处理到 `StreamUsageBuffer` | `internal/runtime/executor/helps/usage_helpers.go`; Codex/Kimi/OpenAI compat executor; tests | 降低流式 usage 处理重复逻辑 | 与 fork 新增 usage/token 统计相关，需重点评审不丢 token 统计 |
| `14b13966` | 简化 translator response 逻辑并增强 thinking 兼容处理 | Antigravity Claude/OpenAI translator 和 tests | 改善 Antigravity thinking 与响应转换兼容性 | 触碰 translator 和 thinking 相关链路；需跑相关 translator 单测并做行为评审 |

## 建议

- 建议吸收，但应作为后端代码吸收任务推进，而不是只提交治理文档。
- 候选合并后至少执行：
  - `go test ./internal/runtime/executor/...`
  - `go test ./internal/translator/...`
  - `go test ./sdk/cliproxy/...`
  - `go test ./...`
  - `go build -o test-output ./cmd/server && rm test-output`
- 若本机 Go 不可用，使用仓库既有 Docker Go 验证路径。
