---
Status: released
Created: 2026-07-09
Owner: backend
Execution Route: multi_agent
---

# 后端 Usage Token 与金额明细契约升级

## 任务身份判定

本任务为新建独立任务。历史任务 `20260703-auth-usage-token-cost-statistics` 已发布并完成第一阶段认证文件 usage 聚合、单认证文件明细 API 和 auth-files usage 摘要。本任务是在该能力之上继续升级“请求级 token facts 与估算金额契约”，包括 canonical persistent detail、缓存 token 明细、reasoning 计费语义、request facts 去重、redis queue 同源派生和 `client_ip` 统一解析，目标、范围和验收条件均已扩大，不复用已 released 的历史任务目录。

## 背景

前端需要在原有统计信息基础上新增每个凭证的请求 token 明细和估算金额明细，且要区分输入、输出、缓存、推理和总 token，以及输入金额、输出金额、缓存金额、总金额、缓存占比等信息。上一轮方案评审确认：后端应优先提供稳定、可持久化、可导入导出、可分页查询的请求事实；价格表与前端展示计算先由前端负责，后端保留 `estimated_cost_usd` 可空字段和 token/cost 语义契约。

## 目标

- 把请求级 usage detail 升级为持久 canonical detail，避免 provider/model/auth/request context 只存在于临时 DTO 或 queue payload。
- 让 `/v0/management/usage`、`/usage/export`、`/usage/import`、`/usage/auths/:auth_index/requests` 和 redis usage queue 从同一 persistent detail 派生。
- 修复 `UsageReporter.Publish` 与 `EnsurePublished` 互相抢占导致空 usage 覆盖正式 usage 的风险。
- 固定 token facts 与 request identity 的分离策略，避免 enriched usage merge 时因 token 变化导致重复计数。
- 补齐 request detail 顶层字段与 `tokens` 内字段，包括 cache split、reported/computed total、source 与 reasoning cost mode。
- 统一 `client_ip` 解析 helper，避免 usage logger 与 redisqueue 逻辑分叉。
- 保持 plugin API / SDK 外部契约兼容，新增字段只在内部 usage detail 和管理 API 中落地。

## 范围

- 修改 `internal/usage` 的 request detail、snapshot、merge/import/export、auth pagination 和测试。
- 修改 `internal/runtime/executor/helps` 的 usage publish 边界，确保成功路径不会被空 usage 抢占。
- 修改 `internal/logging` 增加或复用 `ClientIPFromContext(ctx)`，并让 usage logger 和 redisqueue 共用。
- 修改 `internal/redisqueue` usage queue payload，使 queue 从 canonical detail 派生。
- 修改 management usage handler 的响应 fixture / 测试。
- 补充 executor publish 覆盖清单与针对性测试。

## 非目标

- 不迁移或新增后端官方价格表。
- 不把估算金额声明为真实账单金额。
- 不存储 prompt、response body、原始 API key、access token、cookie 或私密配置。
- 不扩大插件 API 的字段面；插件 API 兼容旧 usage 字段。
- 不重构 provider executor 架构，不改变路由、额度查询或认证文件选择逻辑。

## 约束

- 代码改动必须在 `dev` 分支；`.agents` 治理记录只提交 `dev`，不得合入 `master`。
- Go 代码必须 `gofmt`。
- 旧 usage snapshot 必须可导入；新字段缺失时按 null/zero/unknown 兼容读取。
- request detail 与 aggregate token stats 必须隔离，不能为了 detail 展示把聚合 JSON 污染成 request-only schema。
- `reasoning_tokens` 默认视为 output 的明细子集；是否单独计费由 `reasoning_cost_mode` 表达，避免双算。
- cache token 字段必须区分 `cached_tokens`、`cache_read_tokens`、`cache_creation_tokens`，并保留 split 状态。
- `total_tokens` 不能把 output 已包含的 reasoning 或 input 已包含的 cache 重复叠加。
- `auth_index`、`request_id`、`client_ip` 均按普通字符串处理，不假设固定格式。

## 验收条件

- `internal/usage.RequestDetail` 或 versioned `RequestDetailV2` 持久化并序列化 canonical request context 与 token fields。
- `/v0/management/usage` details、auth request pagination、export/import 和 redis queue 都从 persistent canonical detail 派生。
- `UsageReporter.Publish` / `EnsurePublished` 能保证 enriched usage 不被早到的空 usage 抢占；覆盖主要 streaming/success executor 路径。
- merge/import 使用 request identity 与 facts hash 分离策略，支持 v1 detail 后续 enriched merge，不重复计数。
- `client_ip` 解析由 `internal/logging.ClientIPFromContext(ctx)` 或等价 shared helper 统一提供，并有一致性测试。
- 后端测试覆盖 usage detail schema、旧快照兼容、auth pagination、queue payload、publish once 语义和 client_ip 共享 helper。
- 完成后运行 `gofmt`、聚焦 `go test`、`go build -o test-output ./cmd/server && rm test-output` 和 `git diff --check`。

## Canonical 文档

- 需求与设计: `specs/2026-07-09-backend-usage-token-cost-detail-v2-design.md`
- 实施计划: `plans/2026-07-09-backend-usage-token-cost-detail-v2-implementation-plan.md`
