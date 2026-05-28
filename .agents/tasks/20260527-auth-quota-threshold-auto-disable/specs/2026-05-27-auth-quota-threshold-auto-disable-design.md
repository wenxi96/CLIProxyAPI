# Auth Quota Threshold Auto Disable Design

## Goal

在保留既有零额度自动禁用能力的前提下，将自动禁用扩展为“全局额度阈值禁用”。第一阶段仅提供全局阈值，不引入 per-provider 覆盖；实现验证稳定后，再评估 provider 级配置。

## Configuration Semantics

保留现有开关作为兼容入口：

- `quota-exceeded.auto-disable-auth-file-on-zero-quota`

新增全局阈值：

- `quota-exceeded.auto-disable-auth-file-quota-threshold-percent`

语义：

- 开关为 `false`：不执行自动禁用。
- 开关为 `true` 且阈值为 `0`：保持旧行为，只在明确零额度或等价耗尽时禁用。
- 开关为 `true` 且阈值大于 `0`：当真实额度查询返回 `remaining_percent <= threshold` 时禁用。

阈值必须归一化到安全范围，建议第一阶段使用 `0..50`，避免误配置成过高比例导致大量认证文件被持久禁用。

## Status Messages

自动禁用必须区分触发原因，避免管理端、日志和测试无法判断禁用来源：

- 明确零额度或等价耗尽：`auto_disabled_quota_exhausted`
- 非零阈值命中：`auto_disabled_quota_threshold`

当 `result.Exhausted=true` 时，即使同时存在 `RemainingPercent <= threshold`，也按耗尽原因处理。

## Runtime Semantics

阈值禁用继续复用现有异步 quota check 链路，不引入主动定时扫描：

1. 请求失败并带有 quota 信号后，`Manager.MarkResult` 尝试投递异步 quota check。
2. quota checker 对支持真实额度查询的 auth 执行确认。
3. 如果结果明确耗尽，或返回的剩余额度百分比小于等于全局禁用阈值，则禁用认证文件并持久化。
4. 如果结果没有 `RemainingPercent` 且也不是明确耗尽，不按阈值禁用。

该能力属于认证管理层，必须对 `fill-first` 和 `round-robin` 都生效。

## Relationship With Scoped Pool

scoped-pool 的 `quota-threshold-percent` 与自动禁用阈值保持独立：

- scoped-pool 阈值：临时路由层行为，仅把 auth 从 provider-local active pool 中移出，不持久化，不禁用文件；只在 `round-robin + scoped-pool enabled + provider enabled` 下生效。
- 自动禁用阈值：认证文件状态行为，会持久化 `disabled=true`，影响所有路由策略。

优先级：

```text
disabled > scoped-pool low_quota ejected > normal routing
```

如果两个阈值同时触发，自动禁用优先。禁用后该 auth 必须从普通调度和 scoped-pool 中都移除。

## Compatibility

旧配置无需迁移。未配置新阈值时，默认 `0`，行为与原零额度禁用一致。

管理 API 和示例配置新增阈值入口；旧开关 API 保留。

## Non-Goals

- 不实现 provider 级阈值配置
- 不实现主动定时额度扫描
- 不改变 scoped-pool 现有阈值移出语义
- 不改变 quota checker 支持范围

## Verification Strategy

- 覆盖默认阈值 `0` 的旧行为兼容
- 覆盖阈值低于、等于、高于边界和 `RemainingPercent=nil` 行为
- 覆盖阈值从 `0` 动态修改为非 `0` 后的新行为
- 覆盖并发/重复 quota check 下，同一 auth 只禁用一次且状态消息与触发原因一致
- 覆盖 `fill-first` 与 `round-robin` 路由模式
- 覆盖 scoped-pool 阈值和自动禁用阈值同时存在时的 disabled 优先级
- 覆盖管理 API、配置 diff、示例配置和 TUI 配置入口
