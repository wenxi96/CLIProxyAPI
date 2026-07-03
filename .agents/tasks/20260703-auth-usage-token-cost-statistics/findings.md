# Findings

## 已确认事实

- `internal/usage/logger_plugin.go` 的 `RequestDetail` 已包含 `AuthIndex string` 和 `Tokens TokenStats`，因此 token 事实数据已经进入 usage pipeline。
- 当前 `StatisticsSnapshot` 只有总量、`apis`、按天/小时请求数和 token 数，没有 `auths` 维度聚合。
- `RequestStatistics.Record()` 当前以 `record.APIKey` 或 endpoint/provider 作为 `statsKey`，随后把明细写入 endpoint/model 层级；没有按 `record.AuthIndex` 维护独立聚合。
- `MergeSnapshot()` 从旧快照恢复时逐条明细导入，因此新增 auth 聚合可以在导入阶段从 detail 重新构建，保持旧文件兼容。
- `/v0/management/usage` 当前由 `internal/api/handlers/management/usage.go` 返回 `{"usage": snapshot, "failed_requests": snapshot.FailureCount}`。
- 管理路由在 `internal/api/server.go` 中注册，usage 相关现有路由包括 `/usage`、`/usage/export`、`/usage/import`、`/usage-queue`。
- `sdk/cliproxy/auth/types.go` 的本地生成 stable auth index 为 16 位十六进制字符串，但 `Auth.Index` 允许保留已有运行态或外部提供的非十六进制值；明细接口不得假设固定长度或固定字符集，path 方案必须测试 URL escape，若实际 index 可能包含 path 分隔符则改用 query 参数。
- `/v0/management/auth-files` 当前已有 `auth_index`、`success`、`failed` 和 `recent_requests` 等字段，适合追加可选 `usage` 摘要而不破坏旧前端。

## 设计判断

- `Auth.Success` / `Auth.Failed` 是认证文件运行态计数，不应改造成 token 账单统计源。token/金额统计应归属 `internal/usage`。
- 实际 token 数来自 provider/runtime 产生的 usage 记录；对于无 usage 的失败请求或 provider 未返回 token 的请求，只能统计请求结果，token 为 0。
- 金额不是 provider 真实账单，必须按“估算金额”处理。第一阶段后端可保留 `estimated_cost_usd: null`，前端使用现有本地模型价格表计算展示；后端共享价格表可作为后续独立增强。
- `total_tokens` 归一化需要以前后端一致为硬契约。后端当前 `normaliseDetail` 在缺失 total 时优先使用 `input + output + reasoning`，只有主计数均为 0 时才把 cached token 纳入 total；前端现有 `extractTotalTokens()` 在缺失 total 时会把 cached token 叠加进去。后续实现必须收敛该差异，避免凭证统计和后端快照 total 不一致。
- `auths` 聚合是由 request details 派生出的查询视图；导入和恢复时应从 details 重建，避免同时信任导入文件里的 `auths` 派生聚合而造成重复或漂移。

## 需在实现时复核

- `coreusage.Record` 是否在所有 provider runtime 都稳定设置 `AuthIndex`。若有 provider 缺失，需要按 provider 执行路径补齐，而不是只在统计层猜测。
- `/auth-files` list handler 的响应结构和 auth index 生成点需要在实现前再次定位，避免重复计算或造成 auth index 漂移。
- 如果新增后端价格表，需要单独设计持久化位置、管理 API、导入导出兼容和前端价格设置迁移路径。
