# 任务说明

Status: complete

## 目标

将认证文件批量检查的正式额度查询路径调整为复用后端 canonical quota query service，使同步 `/v0/management/auth-files/batch-check` 与异步 `/v0/management/auth-files/batch-check-jobs` 在 provider 调用序列、字段语义和返回 details 上与单文件刷新展示保持一致。

## 范围

- 后端抽齐或扩展 `internal/authquota.Service` 的 provider details 返回能力。
- 批量检查入口改为调用 canonical quota query service，不再在 management batch-check 内维护独立 provider 查询逻辑。
- 保持 batch-check 现有批量选择、并发、job progress、summary、aggregate 行为。
- 记录本次架构判断、改动细节和验证证据。

## 非目标

- 不改前端展示交互。
- 不新增新的公开单文件额度查询 HTTP API，除非后续用户另行要求。
- 不删除当前已有 provider 支持能力，不以减少字段或减少 provider 为代价完成复用。
- 不提交、不推送、不部署。

## 验收

- Codex batch-check 正式查询会执行与单文件刷新等价的 `usage + rate-limit-reset-credits` 数据获取。
- batch-check details 保留 windows、plan、subscription、reset credits 等展示所需字段。
- 同步和异步 batch-check 均通过同一 `checkSingleAuthFile` / quota service 路径获得结果。
- 相关单测覆盖 provider details parity，管理包测试与 server 构建通过。
