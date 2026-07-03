# 认证文件 Token 与金额统计实施计划

- 目标: 后端按认证文件 `auth_index` 聚合请求 token 数据，提供单认证文件调用明细 API，并为前端凭证统计展示提供稳定数据契约。
- 输入模式: approved-spec
- 需求来源: spec:.agents/tasks/20260703-auth-usage-token-cost-statistics/specs/2026-07-03-auth-usage-token-cost-statistics-design.md
- Canonical Spec 路径: `.agents/tasks/20260703-auth-usage-token-cost-statistics/specs/2026-07-03-auth-usage-token-cost-statistics-design.md`
- 范围边界: `internal/usage` 聚合与持久化兼容、management usage API、auth-files usage 摘要、后端测试；不修改 provider 路由、不引入插件、不落地共享价格表。
- 非目标: 不存储请求/响应正文；不输出原始密钥；不把估算金额称为真实账单；不删除现有 usage 或 auth-files 字段。
- 约束: Go 代码必须 gofmt；旧 usage snapshot 必须兼容；明细接口必须分页；无 token usage 的请求 token 计为 0；`estimated_cost_usd` 无价格时为 `null`；`total_tokens` 归一化必须与现有后端 `normaliseDetail` 口径一致，不能把 cached tokens 重复叠加到 total；`auth_index` 按普通字符串处理，不假设固定长度或十六进制格式。
- 细化层级: contract-first
- 执行路由: direct_inline
- 为什么使用该路由: 后端改动集中在 usage 聚合、management handler 和对应测试，跨模块但依赖顺序清晰，不需要多 agent 或 ULW 状态机。
- 升级触发条件: 若实现中发现多个 provider 未设置 `AuthIndex`、需要迁移价格配置持久化，或 usage snapshot 格式需要版本升级，则暂停并升级为新的设计确认任务。

## 文件结构

- 新建:
  - `internal/api/handlers/management/usage_auth_requests_test.go`
- 修改:
  - `internal/usage/logger_plugin.go`
  - `internal/usage/logger_plugin_test.go`
  - `internal/usage/persistence_test.go`
  - `internal/api/handlers/management/usage.go`
  - `internal/api/handlers/management/usage_test.go`
  - `internal/api/handlers/management/auth_files.go`
  - `internal/api/handlers/management/auth_files_recent_requests_test.go`
  - `internal/api/server.go`
- 读取:
  - `sdk/cliproxy/usage`
  - `sdk/cliproxy/auth/types.go`
  - `internal/redisqueue/plugin.go`
- 测试:
  - `go test ./internal/usage`
  - `go test ./internal/api/handlers/management`
  - `go build -o test-output ./cmd/server && rm test-output`

## 任务拆分

### 任务 1：新增 usage auth 聚合模型

- 目标: 在 `internal/usage` 中建立认证文件维度聚合结构，并在实时记录和 snapshot 中返回 `auths`。
- 文件:
  - 新建: None
  - 修改: `internal/usage/logger_plugin.go`; `internal/usage/logger_plugin_test.go`
  - 读取: `sdk/cliproxy/usage`
  - 测试: `go test ./internal/usage`
- 依赖: None
- 验证: 新增测试构造包含 `AuthIndex` 的 usage record，断言 `Snapshot().Auths[authIndex]` 的请求数、成功/失败数、token breakdown、模型 breakdown、首末请求时间正确；补充 input/output/reasoning 与 cached 同时存在、仅 cached 存在两类 total 归一化用例，防止 cached tokens 被重复计入 total。
- 停止条件: 如果 `coreusage.Record` 无法稳定提供 `AuthIndex`，停止并先定位 provider publish 路径。
- 接口 / 契约: 新增 `StatisticsSnapshot.Auths map[string]AuthUsageSnapshot`，`estimated_cost_usd` 无价格时编码为 `null`。

### 任务 2：保证导入、恢复和历史快照兼容

- 目标: 旧 snapshot 导入恢复后可以从 details 重建 auth 聚合，新 snapshot 缺少或包含 `auths` 都不破坏导入。
- 文件:
  - 新建: None
  - 修改: `internal/usage/logger_plugin.go`; `internal/usage/persistence_test.go`
  - 读取: `internal/usage/persistence.go`
  - 测试: `go test ./internal/usage`
- 依赖: 任务 1
- 验证: 使用不含 `auths` 的历史格式 fixture 恢复，断言 `Auths` 被重建；导入包含 `auths` 的新格式 fixture 时仍从 details 重建且不把导入文件中的派生 `auths` 再叠加一次；导入重复明细仍按现有 dedup 规则跳过。
- 停止条件: 如果需要改变 `StatisticsFileVersion` 或旧文件解析行为，停止并单独确认迁移策略。

### 任务 3：新增单认证文件调用明细 API

- 目标: 提供 `GET /v0/management/usage/auths/:auth_index/requests`，支持分页和基础筛选。
- 文件:
  - 新建: `internal/api/handlers/management/usage_auth_requests_test.go`
  - 修改: `internal/api/handlers/management/usage.go`; `internal/api/handlers/management/usage_test.go`; `internal/api/server.go`
  - 读取: `internal/api/handlers/management/logs.go`
  - 测试: `go test ./internal/api/handlers/management`
- 依赖: 任务 1
- 验证: 测试不同 `auth_index`、`limit`、`offset`、`model`、`failed`、`from/to` 查询，断言返回倒序、total、分页和参数错误处理符合契约；至少覆盖一个非十六进制 `auth_index` 的 URL escape/读取路径；断言明细 item 的 `tokens.total_tokens` 与 auth 聚合使用同一归一化口径。
- 停止条件: 如果 Gin path 参数无法安全承载实际 auth index，尤其是包含 path 分隔符的已有 Index，改为 query 参数方案并更新前后端 spec 后再继续。
- 接口 / 契约: 响应字段为 `auth_index`、`total`、`limit`、`offset`、`items`；item 包含 `timestamp`、`endpoint`、`model`、`source`、`auth_index`、`failed`、`latency_ms`、`tokens`、`estimated_cost_usd`。

### 任务 4：在 auth-files 响应中追加 usage 摘要

- 目标: `/v0/management/auth-files` 对有 `auth_index` 的认证文件追加 `usage` 摘要，供认证文件列表或前端缓存直接使用。
- 文件:
  - 新建: None
  - 修改: `internal/api/handlers/management/auth_files.go`; `internal/api/handlers/management/auth_files_recent_requests_test.go`
  - 读取: `internal/api/handlers/management/config_auth_index.go`
  - 测试: `go test ./internal/api/handlers/management`
- 依赖: 任务 1
- 验证: 构造 handler usageStats 和 auth manager，断言 auth file JSON 中 `usage.tokens.total_tokens`、`usage.total_requests`、`usage.last_request_at` 与 snapshot 匹配；无 usage 数据时不影响原字段。
- 停止条件: 如果现有 auth-files 响应由多个路径构造，停止并先统一定位所有出口，避免只补一个入口。

### 任务 5：后端集成验证

- 目标: 完成格式化、聚焦测试和构建验证。
- 文件:
  - 新建: None
  - 修改: None
  - 读取: None
  - 测试: `gofmt -w internal/usage internal/api/handlers/management internal/api/server.go`; `go test ./internal/usage ./internal/api/handlers/management`; `go build -o test-output ./cmd/server && rm test-output`
- 依赖: 任务 1, 任务 2, 任务 3, 任务 4
- 验证: 命令全部通过，且 `git diff --check` 无空白错误。
- 停止条件: 如果全量测试出现与本任务无关的历史失败，记录失败证据并至少保证聚焦测试和构建通过。

## 执行交接

- 执行路由: direct_inline
- 为什么使用该路由: 改动范围明确，先后端数据契约再前端展示，单 agent 可以按任务顺序推进。
- 升级到: multi_agent
- 交接说明: 如果用户要求前后端并行实现，可将后端任务 1-4 与前端接入任务拆给不同执行面，但必须以后端 API 契约为准。

## 备注

- 金额字段第一阶段只建立契约，不新增后端共享价格表。
- 若用户明确要求“金额也必须由后端统一计算”，应先新增价格表 spec，再扩展本计划，不直接把前端 localStorage 价格隐式迁入后端。
