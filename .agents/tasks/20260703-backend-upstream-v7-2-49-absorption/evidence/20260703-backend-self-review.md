# 2026-07-03 后端合并候选自评审

## 评审状态

- workflow.operation.name: pre_landing_review
- workflow.operation.status: completed
- workflow.review_scope.status: complete
- workflow.findings.status: none
- workflow.verification.status: pass

## 评审边界

- 基线： 合并前 `dev@9ebd268c`
- 候选： 当前工作区中 `git merge --no-commit --no-ff upstream/main@f8334be8` 产生的后端 staged merge 候选
- 评审目标： `pre_merge_absorption_review`

## 范围检查

本次后端候选仅吸收上游 `v7.2.49` 相关改动：

- Responses SSE / WebSocket forwarder 修复
- Codex WS-to-SSE transcript replay 修复
- OpenAI Responses reasoning fallback
- Claude Sonnet 5 registry 与 Claude thinking sampling 修正
- public pluginhost SDK 方法扩展
- README VisionCoder 链接更新

未发现后端候选包含 fork 自定义额度查询、自动禁用、scoped routing、release workflow 等范围外覆盖。

## 评审检查

- 检查 `sdk/cliproxy/auth/response_model_rewriter.go`：上游补丁围绕 SSE glue normalization、pending buffer flush 和 line-wise fallback，新增测试覆盖 Codex 与 Antigravity/Gemini 模拟场景。
- 检查 `sdk/api/handlers/openai/openai_responses_websocket.go`：上游补丁限定在 WS-to-SSE transcript replay、pinned auth release 与 successful SSE credential pinning。
- 检查 `internal/runtime/executor/claude_executor.go`：上游补丁将 thinking 模式下的采样参数规整从 temperature 扩展到 `top_p` / `top_k`，并有 executor test 覆盖。
- 检查 `sdk/pluginhost/host.go`：上游补丁只扩展 public wrapper 方法，未改变现有方法语义。

## 验证证据

- `docker run --rm -v "$PWD":/workspace -w /workspace golang:1.26 go test -buildvcs=false ./sdk/cliproxy/auth ./sdk/api/handlers/openai ./internal/runtime/executor ./internal/registry`：通过。
- `docker run --rm -v "$PWD":/workspace -w /workspace golang:1.26 go build -buildvcs=false -o test-output ./cmd/server && rm test-output`：通过。
- `docker run --rm -v "$PWD":/workspace -w /workspace golang:1.26 go test -buildvcs=false ./...`：通过。
- `git diff --check`：通过。
- `rg -n "^(<<<<<<<|=======|>>>>>>>)" .`：无输出。

## 发现问题

未发现需要修复的实质问题。

## 剩余风险

- 本次验证没有连接真实上游 provider 做在线流式请求，只能证明本地测试覆盖和构建通过。
- 自评审发生时合并仍处于候选阶段；后续已按用户授权把 `.agents` 治理记录一并纳入提交与发布收口。
