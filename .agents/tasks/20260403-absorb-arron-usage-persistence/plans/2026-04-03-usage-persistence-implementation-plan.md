# Usage 持久化吸收实现计划

- Goal: 吸收参考仓库 usage 快照恢复与周期持久化能力，在当前仓库中补齐启动恢复、周期落盘和关闭保存主链路。
- Input Mode: clear-requirements
- Requirements Source: session-confirmed
- Canonical Spec Path: None
- Scope Boundary: 稳定。本轮仅覆盖 `internal/usage`、`sdk/cliproxy/service.go`、`internal/config/config.go`、`config.example.yaml` 与直接相关测试，不扩展到管理配置编辑端点和 TUI。
- Non-Goals:
  - 不增加管理面板配置编辑入口
  - 不增加 TUI 字段
  - 不改 usage 管理接口返回结构
  - 不吸收其他参考仓库运行时差异
- Constraints:
  - 必须保持现有 usage 接口兼容
  - 周期持久化关闭时，启动恢复与关闭保存仍保留
  - 关闭流程不能引入明显阻塞或 goroutine 泄漏
- Detail Level: contract-first
- Execution Route: direct-inline
- Why This Route: 本轮写集集中、依赖顺序明确，且核心风险都在当前主线程已掌握的少量文件中，直接串行实现和验证最稳妥。
- Escalation Trigger:
  - 如果 `Service` 生命周期接入需要同步重构 watcher 或 server 架构
  - 如果配置热更新与现有行为冲突，无法在小范围内稳定收敛
  - 如果参考实现依赖本仓库尚不存在的大块基础设施

## File Structure
- Create:
  - `internal/usage/persistence.go`
  - `internal/usage/persistence_test.go`
- Modify:
  - `internal/usage/logger_plugin.go`
  - `internal/usage/logger_plugin_test.go`
  - `sdk/cliproxy/service.go`
  - `internal/config/config.go`
  - `config.example.yaml`
- Read:
  - `/tmp/arron-cli-proxyapi/internal/usage/persistence.go`
  - `/tmp/arron-cli-proxyapi/internal/usage/logger_plugin.go`
  - `/tmp/arron-cli-proxyapi/sdk/cliproxy/service.go`
- Test:
  - `./internal/usage`
  - `./sdk/cliproxy`

## Task Breakdown

### Task 1: 补齐 usage 快照状态与文件持久化能力

- Objective: 为 `RequestStatistics` 增加持久化状态跟踪，并引入 usage 快照文件的保存、加载、恢复能力。
- Files:
  - Create:
    - `internal/usage/persistence.go`
    - `internal/usage/persistence_test.go`
  - Modify:
    - `internal/usage/logger_plugin.go`
    - `internal/usage/logger_plugin_test.go`
  - Read:
    - `/tmp/arron-cli-proxyapi/internal/usage/persistence.go`
    - `/tmp/arron-cli-proxyapi/internal/usage/logger_plugin.go`
  - Test:
    - `./internal/usage`
- Dependencies: None
- Verification:
  - `go test ./internal/usage -count=1`
- Stop Conditions:
  - 如果发现去重或状态计数会破坏现有 usage 聚合语义，先停下重新收敛最小兼容方案

### Task 2: 在 Service 生命周期中接入恢复与周期保存

- Objective: 在服务启动、配置热更新和关闭路径中接入 usage 快照恢复与保存逻辑。
- Files:
  - Modify:
    - `sdk/cliproxy/service.go`
  - Read:
    - `/tmp/arron-cli-proxyapi/sdk/cliproxy/service.go`
  - Test:
    - `./sdk/cliproxy`
- Dependencies: Task 1
- Verification:
  - `go test ./sdk/cliproxy -count=1`
- Stop Conditions:
  - 如果接入后需要连带重构 watcher/server 生命周期，先停下确认是否扩大范围

### Task 3: 补齐配置项与示例配置

- Objective: 为周期持久化补齐配置字段和示例配置文档，确保默认行为明确。
- Files:
  - Modify:
    - `internal/config/config.go`
    - `config.example.yaml`
  - Test:
    - `./sdk/cliproxy`
    - 需要时补跑配置相关包测试
- Dependencies: Task 2
- Verification:
  - `go test ./sdk/cliproxy -count=1`
  - 如新增配置断言，则运行对应包测试
- Stop Conditions:
  - 如果配置项需要同步暴露到管理配置编辑端点才能保持兼容，先停下确认是否纳入本轮

## Execution Handoff
- Execution Route: direct-inline
- Why This Route: 改动集中于单仓库少量文件，主线程已掌握参考实现与当前差异，不需要拆多 agent。
- Escalate To:
  - `ulw-governed`：如果这一轮吸收被拆成多轮继续推进
  - `multi-agent`：如果后续决定把 usage 持久化与配置编辑入口拆成独立并行子任务
- Handoff Notes:
  - 先完成 `internal/usage` 的纯能力吸收，再接 `Service`，最后补配置项，避免生命周期改动和数据结构改动同时排错

## Notes
- 本计划仅覆盖用户已确认的方案 2，不自动扩展到参考仓库的其他差异点。
