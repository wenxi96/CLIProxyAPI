# Active Quota Refresh Pool Design

## Goal

在不增加请求路径同步 quota 查询的前提下，让低额度自动禁用具备主动采样能力：最近真实使用过的认证文件进入活跃额度刷新池，由后台 worker 节流查询真实额度，并将结果交给现有自动禁用和 scoped-pool 逻辑处理。

## Problem

当前已提交的低额度自动禁用主要依赖一种运行时结果来源：

- 请求失败且像 quota 错误时触发异步 quota check。

这意味着普通成功请求不会更新 provider quota。若某个认证文件持续成功消耗额度，系统可能直到 provider 返回 quota 错误后才确认并禁用。对于 `auto-disable-auth-file-quota-threshold-percent` 设置为 10%、40% 等门禁值的场景，这会降低“提前禁用”的实际价值。

前一轮曾存在未提交候选方案：复用配额管理页 `/api-call` 响应和认证文件批量检查结果来触发门禁。该方案不属于当前稳定现状；在本设计落地后不再需要，应从本任务实现中移除。本任务改为通过真实请求活跃池主动刷新额度。

## Recommended Design

新增后端内存态 `ActiveQuotaRefreshPool`。它只维护运行时活跃认证文件，不持久化。

### Relationship With Prior Quota Result Reuse Changes

前一轮已有未提交改动尝试把管理动作中已经拿到的 quota 响应复用到自动禁用链路，包括：

- 配额管理页 `/api-call` 的 quota 响应自动调用 `ApplyQuotaCheckResult`。
- 认证文件批量检查结果自动调用 `ApplyQuotaCheckResult`。
- `authquota.ResultFromAPICallResponse` 将管理 API 响应转换为 quota result。

本设计认为这些改动在实现活跃额度刷新池后不再需要，应从本任务实现中移除：

- 活跃池的目标是基于真实运行时请求把认证文件入池，再由后台 worker 主动刷新额度；它不依赖用户打开配额管理页或手动执行批量检查。
- 管理动作复用结果会引入第二类触发源，增加门禁生效路径、测试矩阵和误触发风险；保留它会削弱 active pool 作为唯一主动采样来源的边界。
- 仍应保留 `Manager.ApplyQuotaCheckResult(authID, result)`，因为它是 active pool worker、既有异步 quota check、scoped-pool 与自动禁用共享的唯一状态应用入口。

因此第一版实现范围为：

- 保留/完善 `ApplyQuotaCheckResult`。
- 不让 `/api-call` 或批量检查自动触发低额度禁用。
- 不保留仅为管理动作复用服务的 `internal/authquota` quota 响应解析 helper，除非另起独立任务重新设计。

### Pool Entry

每个池条目以 `authID` 为 key，建议包含：

```text
authID
provider
lastUsedAt
lastCheckedAt
nextCheckAt
lastRemainingPercent
inFlight
lastErrorAt
```

最小实现可以只保留：

```text
authID
lastUsedAt
lastCheckedAt
nextCheckAt
inFlight
```

`lastRemainingPercent` 只用于观测或调试，不作为禁用权威；禁用权威仍是本次 quota check 结果。

### Activation

认证文件参与真实请求后调用 `Touch(authID)` 入池或更新时间。

触发点应在 `Manager.MarkResult` 后段，满足：

- `AuthID` 非空。
- 该认证文件存在。
- 该认证文件未 disabled。
- 非 runtime-only。
- 当前 quota checker 支持该 provider/auth。
- 只有真实运行时请求结果触发，不由 `/api-call`、批量检查等管理动作反向触发。

`Touch` 只更新时间，不执行 quota 查询。

### Scan And Workers

后台扫描器按 `scan-interval-seconds` 周期扫描池内条目，默认 30 秒。

某条目只有满足以下条件才进入查询队列：

- `now >= nextCheckAt`
- `now - lastUsedAt <= active-ttl-seconds`
- 未 disabled
- auth 仍存在
- 非 runtime-only
- provider/auth 仍支持 quota check
- 当前没有同 auth 的 in-flight 查询

workers 默认 1。实现时应避免同一认证文件重复 in-flight。

### Initial Check

认证文件首次入池后，不同步查额度。下一轮扫描时，如果满足条件即可查，最多等待一个 scan interval。

这样避免请求路径额外耗时，同时能尽快建立 quota 基线。

### Next Check Interval

每次 quota check 成功并得到 `RemainingPercent` 后，根据：

```text
delta = remaining_percent - auto_disable_threshold_percent
```

计算下一次检查时间：

```text
delta <= 0        -> ApplyQuotaCheckResult 后由现有链路禁用
0 < delta <= 15   -> 120 seconds
15 < delta <= 30  -> 180 seconds
delta > 30        -> 300 seconds
```

如果 `threshold_percent == 0`，仍允许 active refresh 更新 scoped-pool 快照；自动禁用只会在 `Exhausted=true` 或 `no_quota` 时发生。下一次检查间隔可按 `remaining_percent - 0` 计算。

如果 `RemainingPercent == nil` 且非 exhausted：

- 不因阈值禁用。
- 建议按 300 秒设置下一次检查，或在实现中选择出池。为减少调用，推荐第一版出池，等待下一次真实调用重新入池。

### Removal Conditions

以下情况移出池：

- `now - lastUsedAt > active-ttl-seconds`，默认 600 秒。
- auth 不存在。
- auth 已 disabled。
- runtime-only auth。
- quota checker 不支持该 auth。
- quota 查询返回错误。
- quota check 结果明确 `ClassificationUnsupported`。

查询错误出池的设计是为了避免 provider 或网络异常时后台持续重试。下一次真实请求仍会重新 `Touch` 入池。

### Relationship With Existing Auto Disable

活跃刷新池不直接改 auth 状态。它只负责调用 quota checker 并把结果交给：

```go
Manager.ApplyQuotaCheckResult(authID, result)
```

禁用判断仍由现有逻辑负责：

```text
Exhausted=true -> disabled
threshold > 0 && remaining_percent <= threshold -> disabled
```

这样可以复用：

- 自动禁用状态消息。
- 持久化。
- scoped-pool 快照更新。
- scheduler 同步。
- 现有测试和行为约束。

### Configuration

建议在 `quota-exceeded` 下新增：

```yaml
quota-exceeded:
  active-quota-refresh:
    enabled: false
    scan-interval-seconds: 30
    active-ttl-seconds: 600
    workers: 1
```

默认值语义：

- `enabled: false`：升级不默认新增 provider quota 调用。
- `scan-interval-seconds: 30`：扫描频率，不等于每个 auth 的查询频率。
- `active-ttl-seconds: 600`：10 分钟无真实请求活动即出池。
- `workers: 1`：全局同一时间只执行一个后台 quota 查询。

分层间隔第一版建议内置，不暴露配置：

```text
near threshold: 120 seconds
middle: 180 seconds
far: 300 seconds
```

后续若用户需要再开放配置项。

### Frontend

第一版前端不是必须项。若要提供可视化配置，建议只在配置编辑器暴露：

- 启用活跃额度刷新。
- 扫描间隔秒数。
- 活跃 TTL 秒数。
- worker 数。

不在认证文件页新增自动 quota 刷新轮询。

### Observability

建议后端 debug 日志记录：

- auth 入池。
- auth 出池原因。
- quota refresh scheduled / skipped。
- quota refresh error。
- quota refresh result with remaining percent and next interval。

日志不得输出 token、cookie 或 auth secret。

## Non-Goals

- 不在请求成功路径同步执行 quota check。
- 不新增 per-provider 策略。
- 不持久化活跃池状态。
- 不改变 quota checker 支持范围。
- 不改变手动配额管理和批量检查接口。
- 不把 `/api-call` 或批量检查结果作为第一版自动禁用触发源。
- 不新增前端 provider quota 自动轮询。
- 不改变 auth auto-refresh 凭证刷新逻辑。

## Risks And Mitigations

### Provider quota API 调用过多

缓解：

- 默认关闭。
- worker 默认 1。
- 活跃 TTL 10 分钟。
- 同 auth in-flight 去重。
- 查询异常出池。
- 下一次查询按 delta 分层，最多 120 秒一次。

### 阈值门禁仍存在滞后

缓解：

- 首次入池下一轮扫描尽快检查。
- 越接近门禁间隔越短。
- 失败 quota 错误链路仍保留兜底。

### 状态分叉

缓解：

- 活跃池不直接写 auth 状态。
- 所有结果统一通过 `ApplyQuotaCheckResult`。

### 热重载配置

缓解：

- worker 每轮读取 `CurrentConfig()`。
- 配置关闭后停止扫描或清空池。
- worker 数变更可在服务重启后生效；如要热调整，计划中单独处理。

## Acceptance

- 请求路径只 touch，不同步查 quota。
- 后台刷新池只处理最近 10 分钟活跃认证文件。
- 按 delta 分层设置下一次检查时间：120/180/300 秒。
- 查询结果复用现有 `ApplyQuotaCheckResult`。
- 默认关闭，不改变现有用户运行行为。
- 单元测试证明成功请求不会阻塞，也不会立即调用 quota checker。
- 单元测试证明后台 worker 到期后会调用 quota checker 并触发阈值禁用。
