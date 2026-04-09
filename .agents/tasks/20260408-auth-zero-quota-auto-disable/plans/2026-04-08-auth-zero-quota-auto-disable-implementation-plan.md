# Auth Zero Quota Auto Disable Implementation Plan

- Goal: 为支持真实额度查询的认证文件补齐“运行时失败后异步额度确认，并在额度真实耗尽时自动禁用”的完整链路。
- Input Mode: approved-spec
- Requirements Source: spec:.agents/tasks/20260408-auth-zero-quota-auto-disable/specs/2026-04-08-auth-zero-quota-auto-disable-design.md
- Canonical Spec Path: .agents/tasks/20260408-auth-zero-quota-auto-disable/specs/2026-04-08-auth-zero-quota-auto-disable-design.md
- Scope Boundary: 稳定。本轮只覆盖共享额度查询服务、`auth.Manager` 异步确认与自动禁用、配置项/管理接口、存储持久化一致性修复和直接相关测试，不扩展前端与自动恢复。
- Non-Goals:
  - 不为不支持真实额度查询的 provider 新增猜测逻辑
  - 不调整现有 quota 冷却窗口算法
  - 不实现自动恢复或前端配置页面
- Constraints:
  - 失败请求不能被额度确认阻塞
  - 同一 auth 同时只允许一个额度确认任务
  - 只有真实额度查询明确耗尽时才允许禁用
  - 自动禁用必须正确落盘，避免重启回弹
- Detail Level: contract-first
- Execution Route: direct-inline
- Why This Route: 改动虽然跨配置、运行时和存储，但依赖链清晰且核心写面集中在后端少量文件中，由主线程串行推进更容易保持状态一致和测试顺序。
- Escalation Trigger:
  - 如果共享额度查询服务抽取后需要大范围迁移管理接口依赖
  - 如果 `Manager` 现有并发模型不足以安全容纳新增 worker
  - 如果存储一致性修复暴露出更大范围的 metadata 持久化缺陷

## File Structure
- Create:
  - `internal/authquota/service.go`
  - `internal/authquota/service_test.go`
  - `sdk/cliproxy/auth/quota_check_async.go`
- Modify:
  - `sdk/cliproxy/auth/conductor.go`
  - `internal/config/config.go`
  - `config.example.yaml`
  - `internal/api/handlers/management/quota.go`
  - `internal/api/server.go`
  - `internal/store/gitstore.go`
  - `internal/store/objectstore.go`
  - `internal/store/postgresstore.go`
  - `sdk/auth/filestore.go`
  - 直接相关测试文件
- Read:
  - `internal/api/handlers/management/auth_files_batch_check.go`
  - `sdk/cliproxy/auth/persist_async.go`
  - `sdk/cliproxy/auth/types.go`
  - `sdk/cliproxy/auth/conductor.go`
- Test:
  - `./internal/authquota`
  - `./sdk/cliproxy/auth`
  - `./internal/api/handlers/management`
  - `./internal/store`
  - 编译验证 `./cmd/server`

## Task Breakdown

### Task 1: 抽取共享额度查询服务

- Objective: 从管理批量检查逻辑中提炼运行时可复用的额度查询与耗尽识别能力。
- Files:
  - Create:
    - `internal/authquota/service.go`
    - `internal/authquota/service_test.go`
  - Modify:
    - 如有必要，小范围调整 `internal/api/handlers/management/auth_files_batch_check.go` 以复用公共逻辑
  - Read:
    - `internal/api/handlers/management/auth_files_batch_check.go`
  - Test:
    - `./internal/authquota`
- Dependencies: None
- Verification:
  - `go test ./internal/authquota -count=1`
- Stop Conditions:
  - 如果抽取后需要把大段 management handler 反向依赖运行时包，先停下收敛公共边界
- Interfaces / Contracts:
  - `Supports(auth *auth.Auth) bool`
  - `Check(ctx context.Context, auth *auth.Auth) (Result, error)`

### Task 2: 为 Manager 接入异步额度确认与自动禁用

- Objective: 在运行时失败后异步投递额度确认，并在明确耗尽时自动禁用认证文件。
- Files:
  - Create:
    - `sdk/cliproxy/auth/quota_check_async.go`
  - Modify:
    - `sdk/cliproxy/auth/conductor.go`
    - `sdk/cliproxy/auth/types.go`
    - 相关测试文件
  - Read:
    - `sdk/cliproxy/auth/persist_async.go`
    - `sdk/cliproxy/auth/conductor.go`
  - Test:
    - `./sdk/cliproxy/auth`
- Dependencies: Task 1
- Verification:
  - `go test ./sdk/cliproxy/auth -count=1`
- Stop Conditions:
  - 如果自动禁用动作无法在不破坏当前调度语义的前提下安全插入，需要先确认是否重构 `Manager` 更新路径
- Interfaces / Contracts:
  - `tryEnqueueQuotaCheck(authID string)`
  - `applyAutoDisableFromQuotaCheck(authID string, result Result)`

### Task 3: 补齐配置入口与管理接口

- Objective: 为自动禁用能力接入配置结构、示例配置和管理读写接口。
- Files:
  - Modify:
    - `internal/config/config.go`
    - `config.example.yaml`
    - `internal/api/handlers/management/quota.go`
    - `internal/api/server.go`
    - 相关 handler 测试
  - Test:
    - `./internal/api/handlers/management`
- Dependencies: Task 2
- Verification:
  - `go test ./internal/api/handlers/management -count=1`
- Stop Conditions:
  - 如果管理配置保存链路要求额外联动 watcher 或前端协议，先停下确认是否纳入本轮

### Task 4: 修复各存储后端的禁用状态落盘一致性

- Objective: 统一 file/git/object/postgres store 在 `auth.Storage != nil` 场景下的 metadata 注入，确保自动禁用状态可持久化。
- Files:
  - Modify:
    - `sdk/auth/filestore.go`
    - `internal/store/gitstore.go`
    - `internal/store/objectstore.go`
    - `internal/store/postgresstore.go`
    - 相关测试文件
  - Test:
    - `./internal/store`
    - 需要时补跑 `./sdk/auth`
- Dependencies: Task 2
- Verification:
  - `go test ./internal/store ./sdk/auth -count=1`
- Stop Conditions:
  - 如果发现现有 `TokenStorage` 实现对 metadata 注入契约不一致，需要先统一接口边界再继续

## Execution Handoff
- Execution Route: direct-inline
- Why This Route: 任务虽然多步骤，但各任务之间强依赖明显，严格按“公共服务 -> 运行时 -> 配置 -> 持久化 -> 验证”推进最稳妥。
- Escalate To:
  - `ulw-governed`：如果实现或验证需要拆成多轮提交
  - `multi-agent`：如果后续决定把公共额度服务与存储一致性修复拆成独立写面并行推进
- Handoff Notes:
  - 严格按 TDD 顺序推进，先为公共服务和 Manager 行为补失败测试，再落最小实现
  - 自动禁用状态文案统一使用内部常量，避免后续测试与管理端判断漂移

## Notes
- 本计划基于已确认设计，不再重新讨论是否应让不支持真实额度查询的 provider 参与自动禁用。
