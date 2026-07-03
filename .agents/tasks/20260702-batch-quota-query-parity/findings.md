# Findings

## 已确认事实

- 旧 B 路 batch-check 在 `internal/api/handlers/management/auth_files_batch_check.go` 内自行实现 provider 调用与字段提取，和前端 A 路单文件刷新使用的 provider 调用序列发生漂移。
- `internal/authquota.Service` 已作为后端运行时 quota checker 存在，并被 `sdk/cliproxy` 注册到 core auth manager，但当前返回的 `coreauth.QuotaCheckResult` 只承载分类、剩余百分比、错误码等自动禁用所需最小字段。
- 因 `QuotaCheckResult` 缺少 details，batch-check 无法直接复用 `internal/authquota.Service` 来满足管理面板展示需求。
- 同步 `/batch-check` 和异步 `/batch-check-jobs` 都会经由 `checkSingleAuthFile`，因此调整该汇合点即可覆盖两个 B 路入口。

## 架构判断

B 路正式额度查询应复用 canonical quota query service。batch-check 不应维护简化版 provider 查询逻辑；它只应负责批量选择、并发执行、job progress 和聚合统计。

## 约束

- 不能以删除 provider、减少 details 字段或降低 Codex reset credits 能力为代价完成复用。
- provider API 未返回的数据应保留为 `null` 或缺省字段语义，而不是伪造。
- 不记录 token、cookie、auth JSON 原文或其他敏感凭证。

