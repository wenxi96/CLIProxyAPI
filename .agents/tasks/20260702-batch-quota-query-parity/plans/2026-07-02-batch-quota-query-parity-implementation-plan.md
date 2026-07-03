# Batch Quota Query Parity Implementation Plan

## Input Mode

clear-requirements

## 需求来源

用户明确要求：B 路 batch-check 应保持和 A 路单文件刷新一样的 provider 调用方式，只是批量执行；正式查询额度时需要保持一致返回结果，并落地治理文档记录。

## 期望结果

批量检查不再维护独立简化 provider 查询逻辑。同步和异步 batch-check 通过同一个后端 quota query service 获取正式额度结果，返回 details 足以支撑前端与单文件刷新展示对齐。

## 范围边界

- Modify: `sdk/cliproxy/auth/quota_check.go`
- Modify: `internal/authquota/service.go`
- Modify: `internal/authquota/service_test.go`
- Modify: `internal/api/handlers/management/auth_files_batch_check.go`
- Modify: `internal/api/handlers/management/auth_files_batch_check_test.go`
- Modify: `.agents/README.md`
- Create/Modify: `.agents/tasks/20260702-batch-quota-query-parity/*`

## 非目标

- 不新增公开单文件 quota HTTP API。
- 不改变前端 API contract 名称。
- 不改变 batch-check job 生命周期和并发模型。
- 不做 provider 行为瘦身。

## 实施任务

### 1. 扩展共享 quota result contract

- 目标: 让 `coreauth.QuotaCheckResult` 能承载 provider details，并提供可 JSON 序列化的共享 window / reset credit 类型。
- 文件: `sdk/cliproxy/auth/quota_check.go`
- 依赖: none
- 验证: `go test ./sdk/cliproxy/auth` 或被更高层测试覆盖
- 停止条件: details 字段不影响现有 auto-disable 逻辑。

### 2. 扩展 `internal/authquota.Service` details 产出

- 目标: 将 Codex/Claude/Gemini/Kimi/Antigravity 的 windows、plan、subscription、reset credits、project/tier/credit 等展示字段统一由 service 返回。
- 文件: `internal/authquota/service.go`; `internal/authquota/service_test.go`
- 依赖: 任务 1
- 验证: `go test ./internal/authquota`
- 停止条件: Codex 会执行 usage + reset credits；provider API 不返回的数据不伪造。

### 3. batch-check 改为调用 quota service

- 目标: `checkSingleAuthFile` 汇合点使用 canonical quota service，batch-check 只保留批量外壳、聚合和结果包装。
- 文件: `internal/api/handlers/management/auth_files_batch_check.go`; `internal/api/handlers/management/auth_files_batch_check_test.go`
- 依赖: 任务 2
- 验证: `go test ./internal/api/handlers/management`
- 停止条件: 同步和异步 batch-check 测试保持通过，Codex details parity 测试覆盖两次 provider 调用。

### 4. 验证与治理收口

- 目标: 运行格式化、聚焦测试、管理包测试、server 构建，并更新任务治理记录。
- 文件: `.agents/tasks/20260702-batch-quota-query-parity/progress.md`; optional `closeout.md`
- 依赖: 任务 3
- 验证: `gofmt`; `go test ./internal/authquota`; `go test ./internal/api/handlers/management`; `go build -buildvcs=false -o test-output ./cmd/server`
- 停止条件: 验证结果写入治理记录，临时 `test-output` 清理。

## 风险

- `coreauth.QuotaCheckResult` 属于 SDK 包，新增字段应保持向后兼容，不改变现有字段语义。
- authquota service 需要保留 batch-check 目前已有 provider details，否则会造成管理面板回归。
- Codex reset credits 请求失败应体现在 details error 字段，不应让主 usage 成功结果整体失败。

