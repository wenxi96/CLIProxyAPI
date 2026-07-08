# 上游更新清单

上游目标：`14b139661d98acbbd7ac19eb827754e78118736f`  
上游标签：`v7.2.52`

| 提交 | 更新了什么 | 影响模块 | 作用 | 冲突与建议 |
|---|---|---|---|---|
| `3aa42a6f` | 增加 `invalid_grant` 错误的 retry suspension 逻辑 | `sdk/cliproxy/auth/conductor.go`; `sdk/cliproxy/auth/conductor_overrides_test.go` | 避免 OAuth invalid_grant 持续重试，改善认证失败恢复 | 无机械冲突预期；需关注 fork auth 自动禁用、刷新池和持久化策略是否受影响 |
| `ab6ed392` | 增加 Claude executor 完整 SSE passthrough 单测并调整 executor | `internal/runtime/executor/claude_executor.go`; tests | 保证 Claude executor 不吞完整 SSE 事件 | 无机械冲突预期；合并后跑 executor 测试 |
| `dc77bf4d` | 增强 Claude tool response 的结构化 content 解析 | `internal/translator/claude/openai/responses/*` | 改善 OpenAI Responses 到 Claude tool result 的兼容性 | 触碰 translator；需要重点评审协议转换行为 |
| `078ed178` | Codex client models 增加 input/output modalities 支持 | `config.example.yaml`; `internal/config/config.go`; `sdk/api/handlers/openai/codex_client_models.go`; `sdk/cliproxy/service.go`; tests | 让模型配置能声明输入/输出模态 | 可能与 fork 配置模板和 OpenAI compat 模型配置定制重叠；建议逐项保留 fork 配置 |
| `4f157fbd` | 将 Codex WebSocket `message_too_big` 映射为结构化 API 响应 | `internal/runtime/executor/codex_websockets_executor.go`; tests | 改善错误返回可观测性和客户端兼容性 | 无机械冲突预期；跑 Codex websocket 测试 |
| `dea47879` | 抽出 `StreamUsageBuffer` 统一 OpenAI stream usage 处理 | `internal/runtime/executor/helps/usage_helpers.go`; Codex/Kimi/OpenAI compat executor; tests | 减少 usage 处理重复逻辑 | 与 fork usage token/cost 统计高度相关；需确认不丢 usage chunk 和最终统计 |
| `14b13966` | 简化 response 逻辑并增强 thinking 兼容 | Antigravity Claude/OpenAI translator 和 tests | 改善 thinking 与 Antigravity 响应兼容 | 触碰 translator 和 thinking；需跑相关 translator 测试并评审兼容路径 |

## 总体建议

可以吸收。当前无机械冲突输出，但本轮涉及 auth、executor、translator、config 和 usage 多条关键链路，合并后必须执行聚焦验证和主线程复评。
