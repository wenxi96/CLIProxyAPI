# Findings

## 已确认事实

- 历史任务 `20260703-auth-usage-token-cost-statistics` 已 `released`，本轮需求是后续增强，必须新建任务目录。
- 当前任务在 `dev` 分支创建；`master` 分支不应包含 `.agents` 治理文档。
- 上一轮后端 v6 方案独立复审结果为 `ready`，可作为本任务设计契约来源。
- 当前后端已存在 usage 认证文件维度聚合与单认证文件请求明细 API，但 request detail schema 仍需要升级。

## 待实现中确认

- 所有 executor 成功/streaming 路径是否都经由可修复的 `UsageReporter` helper。
- `RequestDetail` 扩展与 versioned `RequestDetailV2` 哪个实现方式对旧 snapshot 兼容成本更低。
- redisqueue 当前 payload 与 usage detail 同源派生的最小改动面。

## Round 1 计划评审已采纳问题

- `PLAN-HIGH-001`: 已采纳。计划和设计补充 `source` 安全来源规则、queue 不输出 raw credential、raw key/token/cookie 泄漏测试。
- `PLAN-HIGH-002`: 已采纳。计划和设计补充 identity 必须包含 usage record scope，并覆盖同 request 多模型 / additional model 测试。
- `PLAN-HIGH-003`: 已采纳。计划和设计补充 request-time `client_ip` 快照和 recycled Gin context 回归测试。
- `PLAN-LOW-001`: 已采纳。计划改为修改现有 `internal/logging/client_ip_test.go`。

## Round 2 计划评审已采纳问题

- `PLAN-MED-001`: 已采纳。设计和计划补充 `detail_role` 必须作为 persistent detail 字段持久化、参与 import/export identity，并覆盖同 request 同 model 不同 role 保留测试。
- `PLAN-MED-002`: 已采纳。设计和计划补充 empty facts enrich 后必须同步校正 aggregate totals、model/auth `TokenStats`、按天/小时 token bucket。
- `PLAN-MED-003`: 已采纳。设计和计划补充安全测试覆盖 `APIs` map key 与 redis queue `response_headers`，禁止 raw downstream API key、Authorization、Cookie、token/key 类 header 泄漏。

## 代码评审 Round 7-16

- Round 7-14 共发现并修复 21 项 usage 终态、presence、流式/WebSocket、legacy import、脱敏、分页、estimated cost 和插件 ABI 问题；逐轮明细见 `reviews/2026-07-15-code-review-rounds-7-14.md`。
- Round 15 发现并修复 Antigravity 首条流路径丢失已观测 usage 的 high 问题，新增执行器级回归测试。
- Round 16 独立复审结论为 `Findings: None`、`Verdict: ready`。
- 最终兼容决策：`UsageObserved` 仅保留在内部 `sdk/cliproxy/usage.Record`，不扩展 `sdk/pluginapi.UsageRecord`。
- 最终终态决策：missing、facts、failure 三者只能有一个终态；流式失败可在同一失败终态中保留已观测 token facts，不产生 revision 双事件。

## 代码评审 Round 17-20

- 主线程复核发现 Gemini 与 AI Studio 在 `FilterSSEUsageMetadata` 之后解析 usage，非终止 chunk 的 usage 可能先被过滤；已改为从原始 payload 观察 usage，再把过滤结果交给翻译器。
- Round 17 发现 AI Studio `wsrelay.stream_chunk` 没有完整 SSE frame 保证；已新增跨 chunk 的 Gemini SSE usage 累积器，并覆盖合并帧与分片帧。
- Round 18 发现累积器缺少跨消息内存上限，以及 `HTTPResp` usage 观察晚于可取消下游发送；已增加 64 MiB 单行上限、超限丢弃与换行恢复，并将 usage 观察前移。
- Round 19 发现超限测试只覆盖单块超限；已改为两个单块均未超限、累计后超限的 `300 + 213` 字节场景。
- Round 20 最终聚焦复审结论为 `Findings: None`、`Verdict: ready_with_updates`；`ready_with_updates` 仅表示静态代码无 blocker，测试和编译门禁仍待执行。

## 提交前动态验证

- 2026-07-15 使用本机缓存的 `golang:1.26` 镜像完成 changed Go files `gofmt -l` 检查，结果无未格式化文件。
- 聚焦测试覆盖 `sdk/pluginapi`、`internal/pluginhost`、`internal/logging`、`internal/usage`、executor helper、redis queue、management handler 和 executor，全部通过。
- `go test -count=1 ./...`、`go build -o test-output ./cmd/server` 和 `git diff --check` 全部通过；临时构建产物已删除。
- Verification: 详见 `progress.md` 的 `2026-07-15 16:23 后端提交前动态验证` 与 `reviews/2026-07-15-edit-batch-review.md`。
