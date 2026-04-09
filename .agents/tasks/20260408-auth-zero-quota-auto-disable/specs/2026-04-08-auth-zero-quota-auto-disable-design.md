# 认证文件额度耗尽自动禁用设计

## Goal

在不影响现有请求失败重试链路的前提下，为“支持真实额度查询”的认证文件增加异步额度确认与自动禁用能力，减少额度已耗尽认证文件持续参与调度造成的无效请求。

## Background

当前运行时在遇到 `429`、`402/403` 等错误时，只会为认证文件或模型打上冷却与 quota 状态，不会把认证文件切换到 `disabled`。管理端的批量检查能力已经具备对部分 provider 的真实额度查询逻辑，但这条能力只用于管理统计，尚未接入运行时链路。

用户已明确约束：

- 只有可进行真实额度查询的认证文件才参与自动禁用
- 不允许根据“疑似耗尽”直接禁用
- 失败后触发的额度确认必须异步执行，不能阻塞当前请求链路
- 同一认证文件同一时间只能有一个额度确认任务
- 其他无法真实查询额度的认证文件保持现状

## In Scope

- 抽取共享额度查询服务，复用现有批量检查中的核心 provider 逻辑
- 在 `auth.Manager` 中新增异步额度确认队列
- 在运行时失败结果收口处挂异步额度确认投递
- 新增配置项 `quota-exceeded.auto-disable-auth-file-on-zero-quota`
- 新增管理接口读取与修改该配置
- 修复 file / git / object / postgres store 的禁用状态落盘一致性
- 增加单元测试和回归保护

## Non-Goals

- 不实现自动恢复
- 不对 `runtime_only` auth 生效
- 不为不支持真实额度查询的 provider 增加猜测性判断
- 不在本轮增加管理前端配置入口
- 不调整现有 quota 冷却窗口策略

## Constraints

- 当前请求失败后的重试链路不能被额度确认阻塞
- 额度确认任务必须使用独立超时上下文，不能复用原请求上下文
- 同一 auth 的并发去重必须在 `Manager` 内完成，避免多次重复查询
- 自动禁用后必须同步刷新调度器与持久化，避免仅内存禁用
- 注释遵循仓库约定，代码注释保持英文

## Supported Providers

第一版仅覆盖当前已有真实额度查询实现的 provider：

- `codex`
- `claude`
- `gemini-cli`
- `kimi`
- `antigravity`

以下 provider 不参与自动禁用，保持现状：

- `qwen`
- 普通 `gemini`
- `vertex`
- `openai-compatibility`
- 其他无真实额度查询链路的 provider

## Recommended Design

### 1. 抽取共享额度查询服务

新增内部服务层，例如 `internal/authquota`，承载与管理 handler 解耦后的共享能力：

- `Supports(auth *coreauth.Auth) bool`
- `Check(ctx context.Context, auth *coreauth.Auth) (Result, error)`

该服务复用当前批量检查里对不同 provider 的配额请求与解析逻辑，但返回结构聚焦于运行时所需的最小字段：

- 是否支持
- 是否成功完成真实查询
- 是否明确额度耗尽
- 剩余额度百分比或剩余窗口摘要
- 原始分类/错误说明

### 2. 在 Manager 中新增异步额度确认队列

参照 `persist_async.go` 的模式，在 `sdk/cliproxy/auth` 中新增一个单 worker 队列：

- `quotaCheckPending map[string]struct{}`
- `quotaCheckRunning map[string]struct{}`
- `quotaCheckWake chan struct{}`

提供 `tryEnqueueQuotaCheck(authID string)`：

- 若配置关闭，直接返回
- 若 auth 不存在、已禁用、为 `runtime_only`、或 provider 不支持真实查询，直接返回
- 若 auth 已在 `pending/running` 中，直接返回
- 否则加入队列并唤醒 worker

worker 对每个 auth 使用独立 `context.WithTimeout(context.Background(), 45*time.Second)` 执行真实额度查询。

### 3. 在运行时失败后异步投递

在 `Manager.MarkResult` 中，当结果为失败时仅做投递尝试，不做同步查询：

- 不改变当前请求的错误返回
- 不改变当前重试策略
- 不等待额度检查完成

这样请求链路与额度确认链路彻底解耦。

### 4. 仅在“明确耗尽”时自动禁用

异步任务得到真实查询结果后，仅当满足以下条件之一时执行自动禁用：

- 查询成功且剩余额度明确为 `0`
- 查询分类明确为 `quota_exhausted` 等等价耗尽状态

自动禁用动作应直接修改 auth 运行时状态并持久化：

- `Disabled = true`
- `Status = disabled`
- `Unavailable = false`
- `StatusMessage = auto_disabled_quota_exhausted`
- 清理或重置短期冷却状态，避免与“已禁用”状态冲突

### 5. 配置中心接入

在 `QuotaExceeded` 配置块中新增：

- `auto-disable-auth-file-on-zero-quota`

同步提供管理接口：

- `GET /quota-exceeded/auto-disable-auth-file-on-zero-quota`
- `PUT /quota-exceeded/auto-disable-auth-file-on-zero-quota`
- `PATCH /quota-exceeded/auto-disable-auth-file-on-zero-quota`

### 6. 修复持久化一致性

当前 file store 在 `auth.Storage != nil` 时会注入 metadata，而 git/object/postgres store 没有同等处理。需要统一：

- 在保存前向支持 metadata 注入的 storage 写回 `auth.Metadata`
- 确保 `disabled` 与相关状态能稳定落盘

否则自动禁用只会停留在内存态，重启后会回弹。

## Risks

- 若共享额度查询服务抽取不干净，可能把管理 handler 的 HTTP 细节硬耦合到运行时
- 若自动禁用后未正确刷新 scheduler，仍可能短时间选中已禁用 auth
- 若持久化修补不完整，会出现“运行时已禁用、重启后恢复”的一致性缺陷
- 若 worker 去重不严，会在失败风暴下造成重复额度查询

## Verification Strategy

- 为共享额度查询服务补 provider 支持判断与耗尽识别测试
- 为 `Manager` 增加“失败后异步投递但不阻塞”的测试
- 覆盖“同一 auth 多次失败只触发一次查询任务”的去重测试
- 覆盖“查询明确耗尽才禁用，其他情况不禁用”的行为测试
- 覆盖“配置关闭时不投递”的测试
- 覆盖 git/object/postgres/file store 的 metadata 注入与 `disabled` 落盘测试
- 覆盖管理配置接口的读写测试

## Open Questions / User Decisions

- None

## Need From User

- None
