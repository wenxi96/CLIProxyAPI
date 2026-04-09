# 按供应商类别独立启用的范围轮询设计

## Goal

在不破坏现有请求调度语义的前提下，为认证文件与配置型 AI 供应商凭证增加“按供应商类别独立启用的范围轮询”能力，使每一类 provider 可以维护自己的活跃轮询池，并支持基于额度阈值、连续错误和超时的成员剔除与补位。

## Background

当前系统已经具备两层关键基础：

- 认证文件与配置型 provider 凭证最终都会被统一转换为 `coreauth.Auth`
- 运行时调度已经按 provider / model 分片处理

这意味着“范围轮询”不需要额外引入第二套凭证模型，而应该在现有 `Auth -> scheduler -> selector` 链路之上增加一层可旁路的 provider-local 活跃池过滤逻辑。

用户已明确确认以下关键边界：

- 范围轮询不是全局池，而是“某一类 provider 自己的池”
- 例如 `codex` 可以有自己的活跃池 `5` 个，`claude` 也可以有自己的活跃池 `5` 个
- 只有在配置中显式开启时才生效
- 如果没有开启，则必须完全保持当前逻辑，不允许有任何隐式行为变化

## In Scope

- 设计 provider-local 范围轮询配置结构
- 设计运行时池状态、踢出、补位与降权逻辑
- 设计额度阈值与错误阈值的适用边界
- 设计管理接口与前端展示方式
- 设计日志与测试策略

## Non-Goals

- 不实现全局共享池
- 不在本轮设计中引入新的全局默认路由语义
- 不在关闭配置时改变任何现有调度链路
- 不把配置型 API key 凭证直接持久化切换为 disabled
- 不在本轮直接实现代码

## Hard Compatibility Gate

这是本需求的最高优先级约束：

- 只有当某个 provider 的范围轮询显式 `enabled=true` 时，该 provider 才进入范围轮询逻辑
- 如果某个 provider 没有开启范围轮询，则必须完全走现有逻辑
- 如果 `routing.scoped-pool` 整体不存在，或默认值与 provider 局部值都未开启，则必须完全走现有逻辑
- 不能因为配置里出现 `limit`、`threshold` 等字段就自动启用
- 不能在关闭状态下提前维护池状态并悄悄影响选择结果

## Options Considered

### Option 1: Global Single Pool

将所有 provider 的候选凭证统一放进一个全局活跃池，例如全局只保留 `5` 个活跃成员。

不采用原因：

- 与用户已确认语义不符
- 会导致一个 provider 的池状态影响另一个 provider 的可用性
- 与现有按 provider 分片的调度模型冲突最大

### Option 2: New Global Routing Strategy

新增新的 `routing.strategy`，例如 `scoped-round-robin`，并让所有 provider 一起使用这套新策略。

不推荐作为第一版实现方式：

- 会直接扩张原有策略语义
- 容易把“是否启用范围池”与“策略切换”耦合在一起
- 对关闭状态下的兼容保证更难做严

### Option 3: Round-Robin Plus Provider-Scoped Pool Layer

保留当前 `routing.strategy`，只在 `round-robin` 语义之上叠加一个“按 provider 独立启用的范围池层”；未开启的 provider 完全旁路。

这是推荐方案，原因：

- 兼容性最好
- 便于灰度开启与逐 provider 控制
- 与现有 `Auth` 和 `scheduler` 分片方式天然一致

## Recommended Design

### 1. 配置模型

推荐新增配置：

```yaml
routing:
  strategy: round-robin
  scoped-pool:
    defaults:
      enabled: false
      limit: 5
      quota-threshold-percent: 0
      consecutive-error-threshold: 3
      penalty-window-seconds: 300
      quota-snapshot-ttl-seconds: 300
      idle-log-throttle-seconds: 60
    providers:
      codex:
        enabled: true
        limit: 5
      claude:
        enabled: true
        limit: 5
```

字段解释：

- `enabled`: 显式开关，默认关闭
- `limit`: 当前 provider 类别允许进入活跃池的成员数
- `quota-threshold-percent`: 额度阈值，取值范围 `0%-50%`
- `consecutive-error-threshold`: 连续错误阈值，默认 `3`
- `penalty-window-seconds`: 短时间失败惩罚窗口
- `quota-snapshot-ttl-seconds`: 额度快照有效期
- `idle-log-throttle-seconds`: 无变化日志节流窗口

### 2. 池的粒度

池按“运行时 provider category”独立建立。

默认分组键：

- `codex`
- `claude`
- `gemini-cli`
- `vertex`
- `aistudio`
- `qwen`
- `iflow`
- `kimi`
- `antigravity`

对于 OpenAI compatibility：

- 不建议把所有兼容源合并为一个池
- 建议按具体 provider 名称独立建池，例如 `openrouter`、`siliconflow`
- 分组键可优先取 `attributes.provider_key`，否则退回 `provider`

### 3. 统一候选对象

运行时不引入新对象模型，仍统一使用 `coreauth.Auth`。

候选对象来源包括：

- 文件型认证文件
- `gemini-api-key`
- `claude-api-key`
- `codex-api-key`
- `vertex-api-key`
- `openai-compatibility.api-key-entries`
- 其他被 watcher synthesizer 转换为 `Auth` 的配置型凭证

### 4. 运行时池状态模型

每个 provider shard 下维护两类集合：

- `active members`
- `standby candidates`

每个成员建议维护的运行时状态：

- `in_pool`
- `pool_state`
- `pool_reason`
- `supports_quota_check`
- `remaining_percent`
- `last_quota_checked_at`
- `consecutive_errors`
- `recent_timeout_count`
- `penalty_score`
- `penalty_until`
- `last_selected_at`
- `last_pool_event_at`

### 5. 入池资格

某个候选成员可以进入其 provider 活跃池，当且仅当：

- 未禁用
- 未被标记为显式不可用
- 不在 cooldown 中
- 不属于已知坏凭证
- 若支持额度检查，则最近有效额度快照 `>= quota-threshold-percent`
- 若不支持额度检查，则跳过额度门槛，只按健康状态参与

### 6. 出池条件

某个候选成员应从其 provider 活跃池踢出，当满足以下任一条件：

- 支持额度检查，且异步额度确认结果低于阈值
- 连续错误次数达到阈值
- 请求超时
- 明确的 auth 级错误，例如 `401/403/429/408/5xx`

以下错误不应直接计入 auth 错误阈值：

- 请求体无效
- 模型不支持
- request-scoped 的局部错误

这些分类应复用现有 `conductor.go` 中对错误性质的判断，避免误伤健康凭证。

### 7. 补位规则

当某个 provider 的活跃池成员被踢出后，应立即从同 provider 的候补集合中补位。

候补排序建议按以下优先级：

1. 显式 `priority` 更高
2. 当前不在惩罚窗口
3. 惩罚分更低
4. 更久未被选中

如果某 provider 的合格候选总数小于 `limit`，则以实际合格数为准，不强行补满。

### 8. 调度接入方式

范围轮询不应直接替换现有 selector。

推荐接入方式：

- 在 `pickSingle` / `pickMixed` 进入 provider shard 前
- 先判断该 provider 是否开启范围池
- 如果未开启：
  - 直接使用当前完整候选集
- 如果已开启：
  - 先由池管理层从完整候选集中筛出当前 `active members`
  - 再把筛后的集合交给现有 selector / scheduler

这样可以确保：

- 关闭时零行为漂移
- 开启时仅影响对应 provider

### 9. 额度检查与异步化

额度检查必须异步，不能阻塞当前请求与后续重试链路。

推荐做法：

- 复用现有“失败后异步额度确认”的去重模式
- 同一个 auth 同一时间只允许一个额度检查任务
- 检查成功后更新额度快照与池状态
- 如果额度不足阈值，则踢出当前 provider 活跃池

说明：

- 文件型 auth 可继续与“额度真实耗尽后自动禁用认证文件”能力协同工作
- 配置型 API key auth 不建议直接自动禁用配置，只做出池和降权

### 10. 错误与超时惩罚

短时间多次请求不通的成员应降低重新入池优先级。

建议规则：

- 超时也计入错误
- 在 `penalty-window-seconds` 内，失败次数累加
- 达到阈值即出池
- 出池后进入 `penalty_until`
- 惩罚期结束后可重新回到 standby 排队，但不应立即抢回最高优先级

### 11. 管理接口

建议新增一个只读运行时状态接口：

- `GET /v0/management/routing/scoped-pool/status`

返回内容至少包括：

- 全局 `scoped-pool` 是否启用
- provider 级配置快照
- 每个 provider 的池摘要
- 每个 auth 的池状态

建议同时在认证文件列表接口扩展轻量字段：

- `pool_enabled`
- `pool_group`
- `in_pool`
- `pool_state`
- `pool_reason`

### 12. 前端设计

#### 认证文件页

在 [AuthFilesPage.tsx](/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/pages/AuthFilesPage.tsx) 新增：

- 显示选项：`仅显示未禁用`

该能力可先在页面层实现，不依赖后端新增接口。

在 [AuthFileCard.tsx](/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/features/authFiles/components/AuthFileCard.tsx) 新增池状态 badge：

- `池内运行`
- `候补中`
- `低额度未入池`
- `错误降权`
- `已踢出`

#### AI Providers 页

在各 provider section 卡片中补充池状态：

- [GeminiSection.tsx](/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/components/providers/GeminiSection/GeminiSection.tsx)
- [ClaudeSection.tsx](/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/components/providers/ClaudeSection/ClaudeSection.tsx)
- [CodexSection.tsx](/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/components/providers/CodexSection/CodexSection.tsx)
- [VertexSection.tsx](/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/components/providers/VertexSection/VertexSection.tsx)
- [OpenAISection.tsx](/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/components/providers/OpenAISection/OpenAISection.tsx)

展示原则：

- 未开启范围轮询的 provider，不显示运营态 badge
- 已开启的 provider，显示池摘要与卡片状态
- OpenAI compatibility 按具体 provider 名称显示，不做跨源混合统计

#### 配置中心

配置中心需要同时支持：

- 选择路由策略
- 配置 provider 级 `scoped-pool`

展示上可对用户呈现为“范围轮询”，但内部仍应保持：

- `routing.strategy = round-robin`
- `routing.scoped-pool.providers.<provider>.enabled = true`

不建议允许 `fill-first + scoped-pool enabled` 这种组合进入生效态。

### 13. 日志设计

必须提供结构化日志，方便排查池变更。

建议事件：

- `scoped_pool_init`
- `scoped_pool_rebalance`
- `scoped_pool_member_added`
- `scoped_pool_member_removed`
- `scoped_pool_member_skipped`
- `scoped_pool_quota_check_started`
- `scoped_pool_quota_check_finished`
- `scoped_pool_noop`

字段建议包含：

- `provider`
- `pool_group`
- `auth_id`
- `auth_index`
- `reason`
- `remaining_percent`
- `threshold_percent`
- `consecutive_errors`
- `pool_size`
- `active_count`

即使没有发生池更新，也应保留 `noop` 日志，但必须做节流，避免每次请求都打印。

## Backend Impact Assessment

主要影响面：

- `internal/config/config.go`
- `internal/api/handlers/management/config_basic.go`
- `sdk/cliproxy/auth/conductor.go`
- `sdk/cliproxy/auth/scheduler.go`
- `sdk/cliproxy/auth/selector.go`
- 可能新增 `sdk/cliproxy/auth/scoped_pool*.go`
- `internal/api/handlers/management/auth_files.go`

实现原则：

- 默认路径不动
- 新能力以“旁路层”方式叠加
- 所有 provider 开关和局部配置都以 provider 维度判定

## Frontend Impact Assessment

主要影响面：

- 认证文件页过滤与卡片状态
- AI Providers 页卡片状态
- 配置中心表单与类型系统
- Dashboard 如需增加池摘要，可作为后续增强项，不属于首轮必做

## Risks

- 若把范围池逻辑写进默认 selector 主路径过深，关闭状态下也可能产生行为漂移
- 若 OpenAI compatibility 分组规则不稳定，可能造成兼容源卡片与运行时分组不一致
- 若错误分类不准确，可能把请求参数错误误记到 auth 惩罚上
- 若无变化日志不做节流，会显著增加日志噪音

## Verification Strategy

- 配置关闭时行为等价测试：
  - 关闭 `scoped-pool` 前后选择结果一致
- provider 维度隔离测试：
  - `codex` 池变化不影响 `claude`
- 入池资格测试：
  - 支持额度检查且低于阈值时不入池
  - 不支持额度检查时跳过额度门槛
- 出池与补位测试：
  - 连续错误触发出池
  - 超时触发出池
  - 候补补位正确
- 惩罚窗口测试：
  - 短时间失败会降低重新入池优先级
- mixed-provider 请求回归测试：
  - 未开启 provider 仍按旧逻辑参与
- 管理接口与前端状态映射测试

## Open Questions / User Decisions

- None

## Need From User

- None
