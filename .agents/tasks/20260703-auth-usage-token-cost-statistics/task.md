---
Status: released
Created: 2026-07-03
Owner: backend
Execution Route: direct_inline
---

# 认证文件 Token 与金额统计

## 任务身份判定

本任务为新建独立任务。历史任务 `20260403-absorb-arron-usage-persistence` 解决 usage 快照恢复与周期持久化，`20260702-batch-quota-query-parity` 和 `20260703-codex-batch-quota-display-parity` 解决额度查询展示一致性；本任务新增的是“按认证文件维度聚合实际请求 token、估算金额，并提供单认证文件调用明细查询”，目标、范围和验收条件不同，不复用旧任务目录。

## 背景

当前认证文件列表和运行态认证模型已经能显示每个认证文件的请求成功/失败次数，但该计数不包含 token 明细和金额统计。使用统计模块已经记录每次请求的 token breakdown，并在 `RequestDetail` 中保存 `auth_index`，但快照对外主要按 API endpoint/model 聚合，缺少认证文件维度的稳定聚合和分页明细接口。

## 目标

- 在后端 usage 统计中增加认证文件维度聚合，以 `auth_index` 作为主关联键。
- 在 `/v0/management/usage` 快照中暴露每个认证文件的请求数、成功/失败数、token breakdown、模型分布和时间范围。
- 新增单认证文件调用明细查询接口，支持分页和基础筛选，供前端凭证统计弹窗使用。
- 在 `/v0/management/auth-files` 响应中为每个认证文件补充可选 `usage` 摘要，保持现有 `success`、`failed`、`recent_requests` 字段不变。
- 为金额字段建立“估算金额”契约。没有后端价格表时后端返回 `estimated_cost_usd: null`，前端可继续按本地模型价格计算展示。

## 范围

- 修改 `internal/usage` 统计模型、聚合、快照、导入恢复和测试。
- 修改 management usage handler，新增认证文件 usage 明细 API。
- 修改 management auth files list handler，将 usage 摘要按 `auth_index` 合并到响应。
- 补充后端单元测试与接口测试。

## 非目标

- 不宣称金额为真实账单，只提供基于价格表的估算能力。
- 不存储 prompt、response body、原始 API key、access token 或其他敏感内容。
- 不引入插件依赖，不要求安装 `cpa-key-policy` 或 `codex-token-usage`。
- 不改变现有请求路由、认证文件选择、额度查询或自动禁用逻辑。
- 不删除或替换现有 `success`、`failed`、`recent_requests` 运行态计数字段。

## 约束

- 使用统计关闭时不得产生新的聚合数据；历史快照仍可读取展示。
- 只有上游响应或现有 usage pipeline 能取得 token 时，token 才能被统计；无 usage 的请求 token 计为 0。
- `auth_index` 缺失的历史明细不得强行归入某个认证文件，可归入 `unknown` 或仅保留在原 endpoint/model 明细中。
- `auth_index` 是稳定身份字符串，不得在 API 和前端实现中假设固定长度或十六进制格式。
- 现有 usage snapshot JSON 必须保持向后兼容，导入旧快照不能失败。
- 金额使用 `estimated_cost_usd` 命名，避免与真实账单混淆。

## 验收条件

- `internal/usage.StatisticsSnapshot` 新增认证文件维度字段，旧快照导入和恢复仍通过测试。
- `/v0/management/usage` 响应包含 `usage.auths`，每个 auth 聚合包含请求数、成功/失败数、token breakdown、模型 breakdown、首末请求时间和可空估算金额。
- 新增 `GET /v0/management/usage/auths/:auth_index/requests` 可返回指定认证文件的分页调用明细，明细包含时间、endpoint、model、source、auth_index、失败状态、延迟和 token breakdown。
- `/v0/management/auth-files` 对有 `auth_index` 的文件返回 `usage` 摘要；没有数据时字段可为空或为零值摘要，但不得影响原响应。
- 后端测试覆盖聚合、分页明细、旧快照兼容和 auth-files 合并。
- 完成后运行 `gofmt`、聚焦测试和 `go build -o test-output ./cmd/server && rm test-output`。

## Canonical 文档

- 需求与设计: `specs/2026-07-03-auth-usage-token-cost-statistics-design.md`
- 实施计划: `plans/2026-07-03-auth-usage-token-cost-statistics-implementation-plan.md`
