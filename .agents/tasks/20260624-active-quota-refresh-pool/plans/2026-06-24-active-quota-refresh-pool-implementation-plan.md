# 活跃额度刷新池实施计划

- Goal: 新增后端活跃额度刷新池，让最近被真实请求使用的认证文件按节流规则主动刷新 quota，并复用现有低额度自动禁用逻辑。
- Input Mode: clear-requirements
- Requirements Source: 用户在 2026-06-24 会话中确认的设计：基于 `remaining_percent - threshold_percent` 分层，`<=15` 为 120 秒，`>15 && <=30` 为 180 秒，`>30` 为 300 秒，10 分钟无活动出池；不接受每次请求成功后同步查询额度。
- Canonical Spec Path: `.agents/tasks/20260624-active-quota-refresh-pool/specs/2026-06-24-active-quota-refresh-pool-design.md`
- Execution Route: direct_inline
- Detail Level: contract-first
- 为什么使用该路由: 变更主要集中在后端 auth manager、配置模型和测试，依赖现有 quota checker 与 `ApplyQuotaCheckResult`。串行实现可以更好控制状态机、并发边界和现有未提交改动的取舍。
- 升级触发条件: 如果实现必须同时改前端配置页、引入新持久化状态、改 provider quota 解析范围，或发现活跃池与 scoped-pool/自动禁用存在状态分叉，则停止并升级为独立设计或多阶段任务。

## 文件结构

### 新建

- `sdk/cliproxy/auth/active_quota_refresh_pool.go`
  - 活跃池状态、扫描 loop、worker、调度、in-flight 去重与出池逻辑。
- `sdk/cliproxy/auth/active_quota_refresh_pool_test.go`
  - 池入池、出池、分层间隔、去重、worker 调用、异常出池测试。
- `.agents/tasks/20260624-active-quota-refresh-pool/evidence/*.md`
  - 实现与验证证据。

### 修改

- `internal/config/config.go`
  - 新增 `ActiveQuotaRefreshConfig`。
  - 在 `QuotaExceeded` 下增加 `ActiveQuotaRefresh ActiveQuotaRefreshConfig`。
  - 增加 sanitize/default 逻辑。
- `config.example.yaml`
  - 增加 `quota-exceeded.active-quota-refresh` 示例。
- `sdk/cliproxy/auth/conductor.go`
  - 在 `Manager` 上增加 active quota refresh pool 字段。
  - 在真实运行时 `MarkResult` 后 `Touch` auth。
  - 接入 pool 生命周期。
- `sdk/cliproxy/auth/quota_check_async.go`
  - 保留并复用 `ApplyQuotaCheckResult` 作为唯一 quota 结果状态入口。
  - 如实现需要，抽出快照/执行 quota check 的内部能力，避免复制已有异步检查逻辑。
- `sdk/cliproxy/builder.go`
  - 确认 quota checker 注册后 active refresh pool 可用；如生命周期需要，接入 Start/Stop。
- `sdk/cliproxy/service.go`
  - 如 pool 需要随服务启动/停止显式生命周期，在服务 Start/Stop 中接入。
- `internal/watcher/diff/config_diff.go`
  - 增加 active quota refresh 配置 diff 文案。
- `.agents/tasks/20260624-active-quota-refresh-pool/progress.md`
  - 记录实现、验证、阻塞和结论。
- `.agents/tasks/20260624-active-quota-refresh-pool/handoff.md`
  - 更新接手入口。

### 读取

- `internal/authquota/service.go`
  - 复用现有 quota checker 行为；本任务不扩大 provider quota 解析范围。
- `sdk/cliproxy/auth/quota_check.go`
  - 确认现有失败触发异步 quota check 逻辑不被破坏。
- `sdk/cliproxy/auth/scoped_pool.go`
- `sdk/cliproxy/auth/scheduler.go`
  - 确认 quota 结果通过 `ApplyQuotaCheckResult` 后仍更新 scoped-pool 快照。
- `internal/api/handlers/management/api_tools.go`
- `internal/api/handlers/management/auth_files_batch_check.go`
  - 仅用于回退/剥离前一轮“管理动作复用 quota 结果”未提交改动。

### 测试

- `sdk/cliproxy/auth/active_quota_refresh_pool_test.go`
- `sdk/cliproxy/auth/quota_check_async_test.go`
- `internal/config/quota_exceeded_test.go`
- `internal/watcher/diff/config_diff_test.go`
- `cmd/server` build

## 任务拆分

### Task 0: 收敛前置未提交 quota 改动

- 目标: 区分本次活跃池必须保留的共享入口与应移除的前一轮管理动作复用改动，避免两套方案叠加导致 quota 查询或禁用触发路径过多。
- 文件:
  - 修改 `sdk/cliproxy/auth/quota_check_async.go`
  - 修改 `sdk/cliproxy/auth/quota_check_async_test.go`
  - 修改 `internal/api/handlers/management/api_tools.go`
  - 修改 `internal/api/handlers/management/api_tools_test.go`
  - 修改 `internal/api/handlers/management/auth_files_batch_check.go`
  - 修改 `internal/api/handlers/management/auth_files_batch_check_test.go`
  - 修改 `internal/authquota/service.go`
  - 修改 `internal/authquota/service_test.go`
- 接口 / 契约:
  - 保留 `Manager.ApplyQuotaCheckResult(authID, result)`，因为活跃池 worker 需要用它复用自动禁用、持久化和 scoped-pool 更新逻辑。
  - 移除 `/api-call` 响应自动调用 `ApplyQuotaCheckResult` 的未提交改动。
  - 移除认证文件批量检查自动调用 `ApplyQuotaCheckResult` 的未提交改动。
  - 移除仅为上述管理动作复用服务的 `authquota.ResultFromAPICallResponse` 及相关测试，除非后续另起独立任务重新设计。
- 依赖: None
- 验证:
  - `go test ./sdk/cliproxy/auth ./internal/authquota ./internal/api/handlers/management -run 'Test.*Quota|Test.*Batch|Test.*APICall' -count=1`
- 停止条件:
  - 如果发现现有代码中已有已提交功能依赖 `/api-call` 或批量检查自动触发禁用，停止并重新评估，不直接删除。

### Task 1: 配置模型与默认值

- 目标: 增加 active quota refresh 配置，默认关闭并归一化危险值。
- 文件:
  - 修改 `internal/config/config.go`
  - 修改 `config.example.yaml`
  - 测试 `internal/config/quota_exceeded_test.go`
- 接口 / 契约:
  - YAML:
    ```yaml
    quota-exceeded:
      active-quota-refresh:
        enabled: false
        scan-interval-seconds: 30
        active-ttl-seconds: 600
        workers: 1
    ```
  - 默认值:
    - `enabled=false`
    - `scan-interval-seconds=30`
    - `active-ttl-seconds=600`
    - `workers=1`
  - 合法化:
    - scan interval 小于安全下限时归一到默认值或最低安全值。
    - active TTL 小于安全下限时归一到默认值或最低安全值。
    - workers 小于 1 时归一到 1。
- 依赖: Task 0
- 验证:
  - `go test ./internal/config -run 'Test.*Quota|Test.*ActiveQuota' -count=1`
- 停止条件:
  - 若现有配置保存/热重载路径无法稳定保留 nested map，停止并先补配置解析设计。

### Task 2: 活跃池核心状态机

- 目标: 实现内存态 active quota refresh pool，不接入真实 Manager 生命周期。
- 文件:
  - 新建 `sdk/cliproxy/auth/active_quota_refresh_pool.go`
  - 新建 `sdk/cliproxy/auth/active_quota_refresh_pool_test.go`
- 接口 / 契约:
  - `Touch(authID string, now time.Time)`
  - `Remove(authID string)`
  - `Start(ctx context.Context)` 或由 Manager 启动。
  - `Stop()` 如需要。
  - `nextIntervalForDelta(remaining, threshold int) time.Duration`
- 步骤:
  - 实现池条目与互斥锁。
  - 实现 `Touch` 入池/更新 `lastUsedAt`。
  - 实现 TTL 出池。
  - 实现 in-flight 去重。
  - 实现 delta 分层：
    - `0 < delta <= 15` -> 120 秒
    - `15 < delta <= 30` -> 180 秒
    - `delta > 30` -> 300 秒
  - `delta <= 0` 不由池直接禁用，只保证结果会进入 `ApplyQuotaCheckResult`。
- 依赖: Task 1
- 验证:
  - `go test ./sdk/cliproxy/auth -run 'TestActiveQuotaRefreshPool' -count=1`
- 停止条件:
  - 若需要复制大量 `Manager` 内部逻辑才能测试，停止并重构为小接口注入，不直接扩大 Manager 锁范围。

### Task 3: Manager 接入 Touch 与后台查询

- 目标: 将真实请求使用过的 auth 加入池，并由后台 worker 调用 quota checker。
- 文件:
  - 修改 `sdk/cliproxy/auth/conductor.go`
  - 修改 `sdk/cliproxy/auth/quota_check_async.go`
  - 测试 `sdk/cliproxy/auth/active_quota_refresh_pool_test.go`
  - 测试 `sdk/cliproxy/auth/quota_check_async_test.go`
- 接口 / 契约:
  - `MarkResult` 在已有状态更新、scheduler 更新之后 touch active pool。
  - Touch 不阻塞请求路径，不调用 `QuotaChecker.Check`。
  - worker 查询成功后调用 `ApplyQuotaCheckResult(authID, result)`。
  - worker 查询错误、unsupported、disabled、deleted、runtime-only 均出池。
  - `/api-call` 和批量检查等管理动作不激活 active pool。
- 步骤:
  - Manager 增加 active pool 字段。
  - 配置启用时启动 pool；关闭时停止或不调度。
  - `SetQuotaChecker` 后 pool 可读取 checker。
  - 真实 `MarkResult` 后触发 `Touch`。
  - 避免管理链路反向 touch；管理链路不是活跃池输入源。
- 依赖: Task 2
- 验证:
  - `go test ./sdk/cliproxy/auth -run 'TestMarkResult_.*ActiveQuota|TestActiveQuotaRefreshPool' -count=1`
- 停止条件:
  - 若发现无法区分真实运行时请求与管理动作，停止并增加明确的 context 标记或专用调用入口，不允许管理动作误入池。

### Task 4: 自动禁用与 scoped-pool 集成回归

- 目标: 确认 active pool 查询结果完整复用现有自动禁用与 scoped-pool 逻辑。
- 文件:
  - 修改 `sdk/cliproxy/auth/active_quota_refresh_pool_test.go`
  - 修改 `sdk/cliproxy/auth/quota_check_async_test.go`
  - 读取 `sdk/cliproxy/auth/scoped_pool.go`
  - 读取 `sdk/cliproxy/auth/scheduler.go`
- 验证场景:
  - 阈值 40，检查结果 remaining 41，下一次间隔 120 秒。
  - 阈值 40，检查结果 remaining 55，下一次间隔 120 秒。
  - 阈值 40，检查结果 remaining 70，下一次间隔 180 秒。
  - 阈值 40，检查结果 remaining 71，下一次间隔 300 秒。
  - 阈值 40，检查结果 remaining 40，触发 disabled。
  - `RemainingPercent=nil` 且非 exhausted 不禁用，并按设计出池。
  - 查询错误出池，下一次真实请求可重新入池。
  - disabled 后出池。
  - scoped-pool quota 快照收到更新。
- 依赖: Task 3
- 验证:
  - `go test ./sdk/cliproxy/auth -run 'Test.*ActiveQuota|Test.*ScopedPool.*Quota|TestMarkResult_.*Quota' -count=1`
- 停止条件:
  - 如果 active pool 与 scoped-pool 对同一结果产生不同阈值解释，必须以 `ApplyQuotaCheckResult` 为唯一写状态入口，不能新增第二套禁用判断。

### Task 5: 配置变更展示与可选管理入口

- 目标: 保持第一版后端 YAML 配置可用，并在 watcher diff 中可见；除非实现中发现已有管理 API/TUI 模式可以低风险复用，否则不强行做前端配置 UI。
- 文件:
  - 修改 `internal/watcher/diff/config_diff.go`
  - 测试 `internal/watcher/diff/config_diff_test.go`
  - 可选修改 `internal/api/handlers/management/quota.go`
  - 可选修改 `internal/api/server.go`
  - 可选修改 `internal/tui/config_tab.go`
- 依赖: Task 1
- 验证:
  - `go test ./internal/watcher/diff -run 'Test.*Quota|Test.*ConfigDiff' -count=1`
  - 如改管理 API：`go test ./internal/api/handlers/management -run 'Test.*Quota' -count=1`
- 停止条件:
  - 如果前端配置页改动会触及复杂布局、i18n 或另一个仓库，停止并拆成独立前端任务。

### Task 6: 全量验证与证据沉淀

- 目标: 完成代码格式化、定点测试、构建验证和任务证据记录。
- 文件:
  - 修改 `.agents/tasks/20260624-active-quota-refresh-pool/progress.md`
  - 新增或修改 `.agents/tasks/20260624-active-quota-refresh-pool/evidence/*.md`
  - 修改 `.agents/tasks/20260624-active-quota-refresh-pool/handoff.md`
- 依赖: Task 0-5
- 验证:
  - `gofmt -w <modified-go-files>`
  - `go test ./sdk/cliproxy/auth ./internal/config ./internal/api/handlers/management ./internal/authquota ./internal/watcher/diff -count=1`
  - `go build -o test-output ./cmd/server && rm test-output`
- 停止条件:
  - 任一测试失败且无法明确归因，停止并进入系统化调试，不继续扩大改动。

## 执行交接

- 按 Task 0 -> Task 6 顺序执行。
- 实现前先处理当前工作区已有 quota 相关未提交改动；必须移除 `/api-call` 或批量检查结果自动触发门禁，不把它们作为活跃池的一部分或并行机制保留。
- `ApplyQuotaCheckResult` 是本任务需要保留的共享入口；它不应发起 quota 查询，只应用已经得到的结果。
- 默认不改前端仓库；如需要前端配置 UI，另起前端任务。
- 未经用户明确授权，不提交、不推送、不发布。
- Go 改动后必须 `gofmt`。
- 如果本机无 Go，可继续使用 Docker Go 镜像执行格式化和测试。

## 备注

- 前一轮“监测额度变化触发门禁”的管理动作复用方案在本任务中视为 superseded：活跃池会主动刷新真实运行时活跃认证文件额度，因此不需要依赖用户打开配额页或执行批量检查来触发低额度门禁。保留它会增加额外触发路径和测试矩阵，本任务明确移除该路径，只保留 `ApplyQuotaCheckResult` 这一共享状态入口。
