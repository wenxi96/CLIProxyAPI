# 按供应商类别独立启用的范围轮询实现计划

- Goal: 为认证文件与配置型 AI 供应商凭证补齐“按供应商类别独立启用的范围轮询”能力，并保证只有显式开启时才生效，未开启时完全保持当前调度与管理逻辑不变。
- Input Mode: approved-spec
- Requirements Source: spec:.agents/tasks/20260409-provider-scoped-routing-pool/specs/2026-04-09-provider-scoped-routing-pool-design.md
- Canonical Spec Path: .agents/tasks/20260409-provider-scoped-routing-pool/specs/2026-04-09-provider-scoped-routing-pool-design.md
- Scope Boundary: 稳定。本轮覆盖后端配置结构、运行时池管理、管理状态接口与认证文件列表扩展，以及前端认证文件页、AI Providers 页、配置中心和直接相关类型/翻译/验证入口；不扩展到全局单池、隐式默认启用、配置型 API key 持久化禁用和与本需求无关的 dashboard 改造。
- Non-Goals:
  - 不实现全局共享池
  - 不在未开启配置时改变任何默认调度路径
  - 不把配置型 API key 凭证直接切换为持久化 disabled
  - 不把所有 OpenAI compatibility 源混成一个池
  - 不在本轮扩大到非直接相关的页面重构
- Constraints:
  - 必须显式 `enabled=true` 才生效
  - 未开启时必须完全走旧逻辑
  - 范围轮询必须按 provider category 独立建池
  - 文件型 auth 与配置型 provider 凭证统一走 `coreauth.Auth`
  - 前端池状态展示必须基于后端运行时状态，而不是前端猜测
- Detail Level: contract-first
- Execution Route: multi-agent
- Why This Route: 后端运行时与前端管理中心位于两个独立仓库，写面天然分离；先冻结后端契约与接口后，可将后端运行时实现与前端接入分阶段并行推进，最后由主线程统一完成集成验证。
- Escalation Trigger:
  - 如果范围轮询层无法作为“可旁路的 provider-local 过滤层”接入，而必须深度改写默认 scheduler 语义
  - 如果 OpenAI compatibility 的分组键在现有运行时结构中不稳定，导致前后端无法对齐
  - 如果前端需要的运行时池状态无法通过现有管理接口扩展稳定表达

## File Structure

- Create:
  - `sdk/cliproxy/auth/scoped_pool.go`
  - `sdk/cliproxy/auth/scoped_pool_test.go`
  - `internal/api/handlers/management/routing_scoped_pool.go`
  - `internal/api/handlers/management/routing_scoped_pool_test.go`
- Modify:
  - `internal/config/config.go`
  - `config.example.yaml`
  - `internal/api/handlers/management/config_basic.go`
  - `internal/api/server.go`
  - `internal/api/handlers/management/auth_files.go`
  - `sdk/cliproxy/auth/conductor.go`
  - `sdk/cliproxy/auth/types.go`
  - `sdk/cliproxy/auth/scheduler.go`
  - `sdk/cliproxy/auth/selector.go`
  - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/types/authFile.ts`
  - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/types/config.ts`
  - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/types/visualConfig.ts`
  - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/types/provider.ts`
  - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/services/api/transformers.ts`
  - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/stores/useConfigStore.ts`
  - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/hooks/useVisualConfig.ts`
  - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/components/config/VisualConfigEditor.tsx`
  - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/pages/AuthFilesPage.tsx`
  - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/features/authFiles/components/AuthFileCard.tsx`
  - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/pages/AiProvidersPage.tsx`
  - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/components/providers/GeminiSection/GeminiSection.tsx`
  - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/components/providers/ClaudeSection/ClaudeSection.tsx`
  - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/components/providers/CodexSection/CodexSection.tsx`
  - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/components/providers/VertexSection/VertexSection.tsx`
  - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/components/providers/OpenAISection/OpenAISection.tsx`
  - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/i18n/locales/*.json`
- Read:
  - `sdk/cliproxy/service.go`
  - `internal/watcher/synthesizer/config.go`
  - `sdk/cliproxy/auth/conductor.go`
  - `sdk/cliproxy/auth/scheduler.go`
  - `sdk/cliproxy/auth/selector.go`
  - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/pages/AuthFilesPage.tsx`
  - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/pages/AiProvidersPage.tsx`
  - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/components/config/VisualConfigEditor.tsx`
- Test:
  - `go test ./sdk/cliproxy/auth -count=1`
  - `go test ./internal/api/handlers/management -count=1`
  - `go build -o /tmp/cli-proxy-api-test ./cmd/server`
  - `npm run type-check`
  - `npm run build`

## Task Breakdown

### Task 1: 冻结后端配置与运行时状态契约

- Objective: 为范围轮询定义稳定的配置结构、provider 分组键规则和运行时状态字段，确保关闭状态下完全旁路。
- Files:
  - Create:
    - `sdk/cliproxy/auth/scoped_pool.go`
  - Modify:
    - `internal/config/config.go`
    - `config.example.yaml`
    - `sdk/cliproxy/auth/types.go`
  - Read:
    - `internal/watcher/synthesizer/config.go`
    - `sdk/cliproxy/auth/types.go`
  - Test:
    - `go test ./sdk/cliproxy/auth -count=1`
- Dependencies: None
- Verification:
  - 为配置解析、默认值和 provider 分组键补充单元测试
  - 证明 `enabled=false` / 缺失配置时不会生成生效态池配置
- Stop Conditions:
  - 如果 `provider_key` / `provider` 不能稳定表达 OpenAI compatibility 的池分组，需要先补充分组契约再继续
- Interfaces / Contracts:
  - `Routing.ScopedPool.Defaults`
  - `Routing.ScopedPool.Providers[providerKey]`
  - `PoolState` / `PoolReason` / `PoolSnapshot`

### Task 2: 实现后端 provider-local 池管理与可旁路调度接入

- Objective: 在不改写默认语义的前提下，为已开启 provider 接入范围池过滤、出池、补位、惩罚和异步额度快照判定。
- Files:
  - Create:
    - `sdk/cliproxy/auth/scoped_pool.go`
    - `sdk/cliproxy/auth/scoped_pool_test.go`
  - Modify:
    - `sdk/cliproxy/auth/conductor.go`
    - `sdk/cliproxy/auth/scheduler.go`
    - `sdk/cliproxy/auth/selector.go`
    - `sdk/cliproxy/auth/types.go`
  - Read:
    - `sdk/cliproxy/auth/conductor.go`
    - `sdk/cliproxy/auth/scheduler.go`
    - `sdk/cliproxy/auth/selector.go`
  - Test:
    - `go test ./sdk/cliproxy/auth -count=1`
    - `go build -o /tmp/cli-proxy-api-test ./cmd/server`
- Dependencies: Task 1
- Verification:
  - 关闭配置时选择行为与旧逻辑等价
  - `codex` 池变化不影响 `claude`
  - 连续错误、超时、额度低于阈值会触发出池与补位
  - mixed-provider 请求在未开启 provider 上仍走旧逻辑
- Stop Conditions:
  - 如果必须深度改写 scheduler 主模型才能接入，先停下回到主线程重新收敛最小可兼容方案
- Interfaces / Contracts:
  - `ScopedPoolManager.FilterCandidates(...)`
  - `ScopedPoolManager.MarkResult(...)`
  - `ScopedPoolManager.Snapshot()`

### Task 3: 暴露管理接口与认证文件列表池状态

- Objective: 为前端提供稳定的运行时池状态读取入口，并在认证文件列表中补轻量池字段。
- Files:
  - Create:
    - `internal/api/handlers/management/routing_scoped_pool.go`
    - `internal/api/handlers/management/routing_scoped_pool_test.go`
  - Modify:
    - `internal/api/server.go`
    - `internal/api/handlers/management/config_basic.go`
    - `internal/api/handlers/management/auth_files.go`
  - Read:
    - `internal/api/handlers/management/auth_files.go`
    - `internal/api/handlers/management/config_basic.go`
  - Test:
    - `go test ./internal/api/handlers/management -count=1`
    - `go build -o /tmp/cli-proxy-api-test ./cmd/server`
- Dependencies: Task 2
- Verification:
  - `GET /v0/management/routing/scoped-pool/status` 返回 provider 摘要与 auth 池状态
  - 认证文件列表在已开启场景下返回轻量池字段
  - 未开启场景不返回误导性生效状态
- Stop Conditions:
  - 如果前端所需字段与后端运行时状态之间出现一对多不稳定映射，先补契约说明再继续
- Interfaces / Contracts:
  - `GET /v0/management/routing/scoped-pool/status`
  - `AuthFileItem.pool_enabled`
  - `AuthFileItem.in_pool`
  - `AuthFileItem.pool_state`
  - `AuthFileItem.pool_reason`

### Task 4: 接入认证文件页过滤与池状态展示

- Objective: 在认证文件页新增“仅显示未禁用”过滤，并为卡片补充范围池状态展示。
- Files:
  - Modify:
    - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/types/authFile.ts`
    - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/pages/AuthFilesPage.tsx`
    - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/features/authFiles/components/AuthFileCard.tsx`
    - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/i18n/locales/*.json`
  - Read:
    - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/pages/AuthFilesPage.tsx`
    - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/features/authFiles/components/AuthFileCard.tsx`
  - Test:
    - `npm run type-check`
    - `npm run build`
- Dependencies: Task 3
- Verification:
  - “仅显示未禁用”只做前端过滤，不影响后端接口
  - 已开启 provider 的认证文件卡片正确显示 `池内运行 / 候补 / 降权 / 已踢出`
  - 未开启 provider 不出现误导性池 badge
- Stop Conditions:
  - 如果认证文件列表轻量字段不足以稳定驱动卡片展示，先回补后端状态接口再继续

### Task 5: 接入 AI Providers 页与配置中心

- Objective: 在 AI Providers 页展示 provider-local 池状态，并在配置中心提供 provider 级范围轮询编辑入口。
- Files:
  - Modify:
    - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/types/config.ts`
    - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/types/visualConfig.ts`
    - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/types/provider.ts`
    - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/services/api/transformers.ts`
    - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/stores/useConfigStore.ts`
    - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/hooks/useVisualConfig.ts`
    - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/components/config/VisualConfigEditor.tsx`
    - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/pages/AiProvidersPage.tsx`
    - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/components/providers/GeminiSection/GeminiSection.tsx`
    - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/components/providers/ClaudeSection/ClaudeSection.tsx`
    - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/components/providers/CodexSection/CodexSection.tsx`
    - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/components/providers/VertexSection/VertexSection.tsx`
    - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/components/providers/OpenAISection/OpenAISection.tsx`
    - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/i18n/locales/*.json`
  - Read:
    - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/pages/AiProvidersPage.tsx`
    - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/components/config/VisualConfigEditor.tsx`
    - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/services/api/transformers.ts`
  - Test:
    - `npm run type-check`
    - `npm run build`
- Dependencies: Task 3
- Verification:
  - 配置中心可编辑 provider 级 `enabled/limit/quota-threshold/error-threshold`
  - 未开启 provider 不显示运营态 badge
  - OpenAI compatibility 按具体 provider 名称显示独立池状态
  - `fill-first + scoped-pool enabled` 不进入生效组合
- Stop Conditions:
  - 如果现有 visual config 单值结构不足以表达 provider 级表格配置，需要先收敛前端状态结构再继续

### Task 6: 完成跨栈回归验证与手工验收

- Objective: 对关闭兼容性、provider 隔离性、后端状态接口和前端展示完成跨栈回归验证。
- Files:
  - Read:
    - `.agents/tasks/20260409-provider-scoped-routing-pool/specs/2026-04-09-provider-scoped-routing-pool-design.md`
    - `.agents/tasks/20260409-provider-scoped-routing-pool/plans/2026-04-09-provider-scoped-routing-pool-implementation-plan.md`
  - Test:
    - `go test ./sdk/cliproxy/auth ./internal/api/handlers/management -count=1`
    - `go build -o /tmp/cli-proxy-api-test ./cmd/server`
    - `npm run type-check`
    - `npm run build`
    - 手工验证：关闭配置、只开 `codex`、同时开 `codex+claude`、OpenAI compatibility 独立分组
- Dependencies: Task 4, Task 5
- Verification:
  - 关闭配置时行为等价
  - 开启 `codex` 时只影响 `codex`
  - 开启 `claude` 时不影响 `codex`
  - 前端卡片与后端状态一致
- Stop Conditions:
  - 如果回归中发现关闭状态与旧逻辑不等价，必须停止提交并优先回到兼容性修复

## Execution Handoff

- Execution Route: multi-agent
- Why This Route: 后端与前端仓库写面独立，且在 Task 3 之后接口契约基本冻结，适合拆成“后端运行时/接口”和“前端接入/展示”两条并行子流，最后由主线程统一完成 Task 6 验证。
- Escalate To:
  - `direct-inline`：如果后端契约在实现过程中持续漂移，不适合并行
  - `ulw-governed`：如果本轮拆成多次提交或跨多会话推进
- Handoff Notes:
  - 严格先完成 Task 1-3，再允许前端并行推进
  - 任何实现都不得破坏“未开启时完全保持旧逻辑”这一最高门禁
  - OpenAI compatibility 的分组键以后端运行时契约为准，前端只消费，不自行推断

## Notes

- 该计划基于已确认 canonical spec，不再重新讨论是否应改为全局单池。
