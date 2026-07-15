# 后端 Usage Token 与金额明细契约升级实施计划

- 目标: 升级后端请求级 usage detail，使凭证统计能稳定获取输入、输出、缓存、推理和总 token facts，并为前端估算金额明细提供一致数据契约。
- 输入模式: approved-spec
- 需求来源: spec:.agents/tasks/20260709-backend-usage-token-cost-detail-v2/specs/2026-07-09-backend-usage-token-cost-detail-v2-design.md
- Canonical Spec 路径: `.agents/tasks/20260709-backend-usage-token-cost-detail-v2/specs/2026-07-09-backend-usage-token-cost-detail-v2-design.md`
- 范围边界: `internal/usage` persistent detail、import/export/merge/auth pagination、management usage API、usage reporter publish 边界、redisqueue payload、client_ip helper 与测试；不新增后端官方价格表，不扩插件 API。
- 非目标: 不存储请求/响应正文；不输出原始密钥；不迁移价格配置；不改变 provider 路由、认证文件选择或额度逻辑。
- 约束: 旧 snapshot 兼容；request detail 与 aggregate token stats 隔离；`detail_role` 必须持久化并参与 import/export identity；reasoning/cache 不双算；`source`/queue/API map key/header 不持久化 raw key/token/cookie；同 request 多模型 usage 不被 dedup 吞掉；empty facts enrich 必须同步校正 aggregate totals；client_ip 必须在请求期快照；`.agents` 只在 dev；Go 代码必须 gofmt。
- 细化层级: contract-first
- 执行路由: multi_agent
- 为什么使用该路由: 本任务跨 usage 持久化、executor helper、redisqueue 与 management API，需要先以计划驱动方式实施，再由独立 reviewer 多轮评审。实际写代码阶段同一仓库只允许一个主写者，前后端可并行但后端内部按任务顺序推进。
- 升级触发条件: 如果实现发现需要改动 `sdk/pluginapi` 外部插件契约、引入后端价格表、或无法保证旧 snapshot 兼容，暂停并回到设计确认。

## 文件结构

- 新建:
  - `internal/usage/request_detail_v2_test.go` 或等价聚焦测试文件
- 修改:
  - `internal/logging/client_ip.go`
  - `internal/logging/client_ip_test.go`
  - `internal/usage/logger_plugin.go`
  - `internal/usage/logger_plugin_test.go`
  - `internal/usage/persistence.go`
  - `internal/usage/persistence_test.go`
  - `internal/api/handlers/management/usage.go`
  - `internal/api/handlers/management/usage_test.go`
  - `internal/runtime/executor/helps/usage_helpers.go`
  - `internal/runtime/executor/helps/usage_helpers_test.go`
  - `internal/redisqueue/plugin.go`
  - `internal/redisqueue/plugin_test.go`
  - `internal/logging/requestmeta.go` 或新增等价 helper 文件
- 读取:
  - `sdk/cliproxy/usage/manager.go`
  - `sdk/pluginapi/types.go`
  - `internal/pluginhost/adapters.go`
  - `internal/runtime/executor/*`
  - `internal/api/server.go`
- 测试:
  - `go test ./internal/usage`
  - `go test ./internal/runtime/executor/helps`
  - `go test ./internal/redisqueue`
  - `go test ./internal/api/handlers/management`
  - `go build -o test-output ./cmd/server && rm test-output`

## 任务拆分

### 任务 1：定义 persistent canonical detail 与 token facts

- 目标: 在 `internal/usage` 中建立 request-level canonical detail，持久化 request context、token facts、total 归一化字段和 cost placeholder。
- 文件:
  - 新建: `internal/usage/request_detail_v2_test.go` 或等价测试文件
  - 修改: `internal/usage/logger_plugin.go`; `internal/usage/logger_plugin_test.go`
  - 读取: `sdk/cliproxy/usage/manager.go`
  - 测试: `go test ./internal/usage`
- 依赖: None
- 验证: 测试 detail JSON 包含 `request_id/client_ip/endpoint/model/provider/executor_type/auth_type/model_alias/source/auth_index/detail_role/failed/latency_ms/estimated_cost_usd/tokens`；`detail_role` 默认 `primary` 并可区分 additional/tool/image/video 等 role；token fields 包含 reported/computed total、cache split、reasoning cost mode；aggregate `TokenStats` 不暴露 request-only 字段；raw API key/access token/cookie 不出现在 `source`、snapshot JSON、API detail 或 `APIs` map key。
- 停止条件: 如果只能通过修改外部 SDK/plugin API 才能传递必要字段，停止并提交设计变更。
- 接口 / 契约: `estimated_cost_usd` 缺价格时为 `null`；`reasoning_cost_mode` 默认 `included_in_output` 或 `unknown`，不得默认单独叠加。

### 任务 2：重做 merge/import identity 与 facts enrich

- 目标: 将 dedup 从 token-inclusive key 改为 request identity + facts hash，支持旧 detail 后续被 enriched facts 补齐且不重复加总。
- 文件:
  - 新建: None
  - 修改: `internal/usage/logger_plugin.go`; `internal/usage/persistence.go`; `internal/usage/persistence_test.go`
  - 读取: `.agents/tasks/20260703-auth-usage-token-cost-statistics/plans/2026-07-03-auth-usage-token-cost-statistics-implementation-plan.md`
  - 测试: `go test ./internal/usage`
- 依赖: 任务 1
- 验证: 旧 v1 snapshot 可导入；同一 identity 的空 token detail 被后续 enriched detail 补齐；重复 enriched import 不增加请求数；enrich 空 detail 后会更新 `TotalTokens`、model/auth `TokenStats`、`tokensByDay` / `tokensByHour`；不同 request_id 但相同 tokens 不被误去重；同一 request_id 下同 model 但不同 `detail_role` 均保留，additional/tool/image/video role 不被 primary empty usage 吞掉。
- 接口 / 契约: `identityKey` 必须包含 usage record scope，推荐 `request_id + provider + executor_type + model + auth_index/source + detail_role`；`request_id` 只作为 scope 的一部分，不能单独作为唯一 key；`detail_role` 必须作为 persistent detail 字段参与 snapshot import/export。
- 停止条件: 如果旧 snapshot 缺少足够 identity 字段导致无法无损 enrich，记录降级规则并先复审。

### 任务 3：修复 UsageReporter publish 边界

- 目标: 防止空 usage `Publish` 抢占 enriched usage，保证正式 token facts 能进入 canonical detail。
- 文件:
  - 新建: None
  - 修改: `internal/runtime/executor/helps/usage_helpers.go`; `internal/runtime/executor/helps/usage_helpers_test.go`
  - 读取: `internal/runtime/executor`
  - 测试: `go test ./internal/runtime/executor/helps`
- 依赖: 任务 1
- 验证: 测试先 publish missing usage、后 publish provider usage 时最终使用 enriched facts；请求完成且无 facts 时才发布 missing usage；覆盖 stream/non-stream helper。
- 停止条件: 如果需要逐个 executor 改调用点，先列出调用清单并分批实现，避免只修 helper 不生效。
- 交接说明: 实现记录必须附 executor publish 覆盖清单。

### 任务 4：统一 client_ip helper 并接入 usage/queue

- 目标: 新增或复用 `logging.ClientIPFromContext(ctx)`，并在请求期快照 `client_ip`，让 usage logger 和 redisqueue 使用同一不可变值。
- 文件:
  - 新建: None
  - 修改: `internal/logging/client_ip.go`; `internal/logging/client_ip_test.go`; `internal/logging/requestmeta.go` 或新增 helper 文件; `sdk/api/handlers/handlers.go` 或 `internal/runtime/executor/helps/usage_helpers.go`; `internal/usage/logger_plugin.go`; `internal/redisqueue/plugin.go`
  - 读取: `internal/logging/gin_logger.go`; `internal/logging/requestid.go`
  - 测试: `go test ./internal/logging ./internal/usage ./internal/redisqueue`
- 依赖: 任务 1
- 验证: 同一请求在 usage 和 queue 中得到相同 `client_ip`；无 gin context 返回空字符串；异步 usage dispatch / recycled Gin context 不改变已快照的 `client_ip`；不泄露 token 或 header 私密值。
- 停止条件: 如果 context 中 gin key 用法不稳定，先收敛 context helper，而不是复制解析逻辑。

### 任务 5：让 management API 与 redis queue 同源派生

- 目标: `/usage` details、auth pagination、export/import 和 redis queue payload 都从 persistent canonical detail 派生，且 queue 不输出 raw credential。
- 文件:
  - 新建: None
  - 修改: `internal/api/handlers/management/usage.go`; `internal/api/handlers/management/usage_test.go`; `internal/usage/logger_plugin.go`; `internal/redisqueue/plugin.go`; `internal/redisqueue/plugin_test.go`
  - 读取: `internal/api/server.go`
  - 测试: `go test ./internal/api/handlers/management ./internal/redisqueue ./internal/usage`
- 依赖: 任务 1, 任务 2, 任务 4
- 验证: `/usage/auths/:auth_index/requests` 返回 v2 顶层字段和 tokens 子字段；redis queue payload 中 provider/model/request_id/client_ip 等字段与 usage detail 一致；queue JSON 不包含 raw `api_key`、access token、cookie、Authorization header、Cookie header、token/key 类 header；usage snapshot / management API JSON 不包含 raw downstream API key 作为 `APIs` map key；export/import 后字段不丢失。
- 停止条件: 如果需要 API versioning 才能保持兼容，停止并设计响应兼容层。

### 任务 6：后端最终验证与治理记录

- 目标: 完成格式化、聚焦测试、构建和治理进度记录，为代码独立评审做准备。
- 文件:
  - 新建: None
  - 修改: `.agents/tasks/20260709-backend-usage-token-cost-detail-v2/progress.md`; `.agents/tasks/20260709-backend-usage-token-cost-detail-v2/findings.md`
  - 读取: None
  - 测试: `gofmt -w ...`; `go test ./internal/usage ./internal/runtime/executor/helps ./internal/redisqueue ./internal/api/handlers/management`; `go build -o test-output ./cmd/server && rm test-output`; `git diff --check`
- 依赖: 任务 1, 任务 2, 任务 3, 任务 4, 任务 5
- 验证: 命令通过；若本机 Go 不可用，使用仓库记忆中的 Docker Go 1.26 验证路径并记录原因；准备代码独立评审 packet。
- 停止条件: 如果聚焦测试失败且无法判断是否本任务引入，停止并保留失败输出。

## 执行交接

- 执行路由: multi_agent
- 为什么使用该路由: 主会话统筹前后端，后端实现可由一个 bounded implementer 执行，完成后交由独立 reviewer 评审；不允许后端多个写者并发改同一工作树。
- 升级到: plan-driven-serial
- 交接说明: 后端实现者必须按任务 1-6 顺序推进；任何发现会改变 API 契约、插件 API 或价格表边界的情况，都必须交回主会话。

## 备注

- 本计划是 `20260703-auth-usage-token-cost-statistics` 的后续增强，不修改其发布历史。
- 后端只保证 facts 与 API 契约；输入/输出/缓存金额拆分由前端价格表计算。
