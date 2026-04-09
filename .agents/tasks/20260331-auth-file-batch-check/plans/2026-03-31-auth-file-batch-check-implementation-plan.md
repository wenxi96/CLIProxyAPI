# 认证文件批量检查实现计划

- Goal: 在现有异步批量检查能力基础上，补齐面向管理页的“汇总看板 + 汇总详情 + 直接处置”实现计划，并将后端统计口径对齐到 `cliproxyapi-tool` 的批量检查脚本。
- Input Mode: approved-spec
- Requirements Source:
  - spec:docs/superpowers/specs/2026-04-01-auth-file-batch-check-aggregate-dashboard-design.md
- Canonical Spec Path: `docs/superpowers/specs/2026-04-01-auth-file-batch-check-aggregate-dashboard-design.md`
- Scope Boundary: 稳定。本次仅覆盖认证文件批量检查结果区、汇总详情弹窗、汇总动作入口及其后端聚合结构，不改动底层检查链路的整体架构，不引入跨批次历史能力。
- Non-Goals:
  - 不恢复单文件批量详情弹窗
  - 不新增 WebSocket 或 SSE
  - 不做跨批次历史结果存档与历史清理
  - 不扩展到底层 provider quota 逻辑重写
- Constraints:
  - 必须复用当前异步批量检查任务与轮询模型
  - 后端新增 `aggregate` 作为稳定汇总对象，前端不长期承担复杂统计拼装
  - 动作必须只作用于“本次检查结果”中的候选项
  - `恢复已恢复` 阈值口径需对齐 `cliproxyapi-tool`，默认 `danger`
  - 保持中文文案、中文注释与中文提交信息约定
- Detail Level: contract-first
- Execution Route: direct-inline
- Why This Route: 变更虽然同时涉及后端仓库和配套前端仓库，但边界清晰、依赖顺序明确，且当前主线程已掌握完整上下文；直接串行推进比重新拆多 agent 更稳妥。
- Escalation Trigger:
  - 如果 `aggregate` 设计需要反向改动异步任务基础契约或存储模型
  - 如果动作执行需要引入新的后端批量处置接口而无法复用现有能力
  - 如果前端展示结构需要超出当前页面范围的大规模状态管理重构

## File Structure
- Create:
  - None
- Modify:
  - `internal/api/handlers/management/auth_files_batch_check.go`
  - `internal/api/handlers/management/auth_files_batch_check_jobs.go`
  - `internal/api/handlers/management/auth_files_batch_check_jobs_test.go`
  - `internal/api/handlers/management/auth_files_batch_check_test.go`
  - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/types/authFile.ts`
  - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/services/api/authFiles.ts`
  - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/features/authFiles/hooks/useAuthFilesBatchCheck.ts`
  - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/features/authFiles/components/AuthFilesBatchCheckModal.tsx`
  - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/features/authFiles/components/AuthFileCard.tsx`
  - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/pages/AuthFilesPage.tsx`
  - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/pages/AuthFilesPage.module.scss`
  - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/i18n/locales/zh-CN.json`
  - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/i18n/locales/en.json`
  - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/i18n/locales/ru.json`
- Read:
  - `docs/superpowers/specs/2026-04-01-auth-file-batch-check-aggregate-dashboard-design.md`
  - `/home/cheng/git-project/cliproxyapi-tool/batch_check_auth/batch_check_auth_quota.py`
  - `/home/cheng/git-project/CLIProxyAPI/.agents/tasks/20260331-auth-file-batch-check/findings.md`
- Test:
  - `internal/api/handlers/management/auth_files_batch_check_jobs_test.go`
  - `internal/api/handlers/management/auth_files_batch_check_test.go`
  - `/home/cheng/git-project/Cli-Proxy-API-Management-Center` type-check 与 build 验证

## Task Breakdown

### Task 1: 收敛后端汇总契约

- Objective: 在现有批量检查结果上新增稳定 `aggregate` 结构，明确容量总览、风险总览、健康分层、范围总览、刷新总览、套餐分布、诊断列表与动作候选字段，并把 `恢复已恢复` 阈值口径固定为 `danger` 默认值。
- Files:
  - Modify: `internal/api/handlers/management/auth_files_batch_check.go`
  - Modify: `internal/api/handlers/management/auth_files_batch_check_jobs.go`
  - Read: `docs/superpowers/specs/2026-04-01-auth-file-batch-check-aggregate-dashboard-design.md`
  - Read: `/home/cheng/git-project/cliproxyapi-tool/batch_check_auth/batch_check_auth_quota.py`
  - Test: `internal/api/handlers/management/auth_files_batch_check_test.go`
  - Test: `internal/api/handlers/management/auth_files_batch_check_jobs_test.go`
- Dependencies: None
- Interfaces / Contracts:
  - 结果对象新增 `aggregate`
  - `aggregate.action_candidates` 至少包含：
    - `invalidated_401_names`
    - `disable_exhausted_names`
    - `reenable_names`
    - `reenable_threshold_bucket`
  - `aggregate.risk_overview` 需显式区分：
    - `invalidated_401_count`
    - `no_quota_count`
    - `api_error_count`
    - `request_failed_count`
    - `exhausted_count`
    - `low_remaining_1_29_count`
    - `mid_low_remaining_1_49_count`
- Verification:
  - `go test ./internal/api/handlers/management -run 'Test.*BatchCheck.*' -count=1`
  - 针对空数组、阈值字段、计数字段补充断言，确保 JSON 序列化结构稳定
- Stop Conditions:
  - 如果发现现有结果对象无法无破坏扩展 `aggregate`，必须停下先重新确认兼容策略

### Task 2: 对齐脚本统计口径与动作候选生成

- Objective: 将后端汇总逻辑对齐 `cliproxyapi-tool` 的关键统计口径，生成首屏 12 项指标所需数据和汇总详情弹窗所需分布数据，并基于本次检查结果生成三类动作候选名单。
- Files:
  - Modify: `internal/api/handlers/management/auth_files_batch_check.go`
  - Modify: `internal/api/handlers/management/auth_files_batch_check_jobs.go`
  - Read: `/home/cheng/git-project/cliproxyapi-tool/batch_check_auth/batch_check_auth_quota.py`
  - Test: `internal/api/handlers/management/auth_files_batch_check_test.go`
  - Test: `internal/api/handlers/management/auth_files_batch_check_jobs_test.go`
- Dependencies: Task 1
- Interfaces / Contracts:
  - 统计口径需覆盖：
    - 保守总剩余 / 总容量 / 剩余占比 / 已使用总量 / 已使用占比
    - 等效满血账号数 / 平均剩余 / 中位数剩余
    - 已启用 / 已禁用 / 已处理 / 已跳过
    - 401失效 / 已耗尽 / 低额度(1-29) / 中低额度(1-49) / 剩余额度未知 / 接口异常
    - 健康分层 / 下次刷新 / 近期恢复批次 / 刷新时间节点分布 / 套餐类型与周期分布 / 诊断建议
  - 动作候选约束：
    - `清理401失效` 只来自本次结果中的 `invalidated_401`
    - `禁用已耗尽` 只包含本次结果中已耗尽且当前未禁用项
    - `恢复已恢复` 只包含本次结果中当前已禁用且分桶达到 `danger` 及以上项
- Verification:
  - `go test ./internal/api/handlers/management -count=1`
  - 新增针对分类、分桶、刷新窗口与动作候选的表格驱动测试
- Stop Conditions:
  - 如果需要把工具脚本全部一比一搬入后端才能实现，停下收敛为“首屏与弹窗必需统计”的最小子集后再继续

### Task 3: 改造前端数据契约与状态消费

- Objective: 扩展前端类型、API 和 hook，使页面统一消费后端 `aggregate` 结果，不再由前端长期自行拼装复杂汇总口径。
- Files:
  - Modify: `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/types/authFile.ts`
  - Modify: `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/services/api/authFiles.ts`
  - Modify: `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/features/authFiles/hooks/useAuthFilesBatchCheck.ts`
  - Read: `docs/superpowers/specs/2026-04-01-auth-file-batch-check-aggregate-dashboard-design.md`
  - Test: `/home/cheng/git-project/Cli-Proxy-API-Management-Center` type-check
- Dependencies: Task 2
- Verification:
  - `(cd /home/cheng/git-project/Cli-Proxy-API-Management-Center && npm run type-check)`
  - 确认 hook 对 `results/skipped/aggregate` 的空值、空数组和任务完成态均有容错
- Stop Conditions:
  - 如果需要引入新的全局状态容器才能消费 `aggregate`，先停下评估是否能维持页面局部状态

### Task 4: 重构页面首屏看板与汇总详情弹窗

- Objective: 将认证文件页批量检查结果区改造成两排 12 项汇总看板，删除单文件卡片上的“查看详情”入口，并把现有弹窗重构为“汇总详情弹窗”。
- Files:
  - Modify: `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/pages/AuthFilesPage.tsx`
  - Modify: `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/pages/AuthFilesPage.module.scss`
  - Modify: `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/features/authFiles/components/AuthFileCard.tsx`
  - Modify: `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/features/authFiles/components/AuthFilesBatchCheckModal.tsx`
  - Modify: `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/i18n/locales/zh-CN.json`
  - Modify: `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/i18n/locales/en.json`
  - Modify: `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/i18n/locales/ru.json`
  - Test: `/home/cheng/git-project/Cli-Proxy-API-Management-Center` type-check 与 build
- Dependencies: Task 3
- Interfaces / Contracts:
  - 首屏两排 12 项必须对应 spec 约定的两组指标
  - 汇总详情弹窗固定 4 个区块：
    - 总量概览
    - 健康分层
    - 风险与处置
    - 恢复时间与分布
  - 文件列表区仅保留轻量状态标签，不再承载批量详情入口
- Verification:
  - `(cd /home/cheng/git-project/Cli-Proxy-API-Management-Center && npm run type-check)`
  - `(cd /home/cheng/git-project/Cli-Proxy-API-Management-Center && npm run build)`
  - 人工检查页面：首屏能直接看到两排指标，弹窗不再出现逐文件详情卡片
- Stop Conditions:
  - 如果现有弹窗组件结构无法承载汇总详情而不引入明显重复，先停下收敛组件职责再继续

### Task 5: 接入直接处置动作并完成联调验证

- Objective: 在结果区右上角和汇总详情弹窗中接入四个动作入口，复用现有批量删除与启停能力实现“查看汇总详情 / 清理401失效 / 禁用已耗尽 / 恢复已恢复”，并完成本地联调验证。
- Files:
  - Modify: `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/pages/AuthFilesPage.tsx`
  - Modify: `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/features/authFiles/components/AuthFilesBatchCheckModal.tsx`
  - Modify: `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/features/authFiles/hooks/useAuthFilesBatchCheck.ts`
  - Modify: `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/i18n/locales/zh-CN.json`
  - Modify: `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/i18n/locales/en.json`
  - Modify: `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/i18n/locales/ru.json`
  - Read: `/home/cheng/git-project/CLIProxyAPI/.agents/tasks/20260331-auth-file-batch-check/findings.md`
  - Test: `/home/cheng/git-project/Cli-Proxy-API-Management-Center` type-check 与 build
- Dependencies: Task 4
- Steps:
  - 结果区右上角放置 4 个动作按钮，并展示数量徽标
  - 当候选数为 0 时按钮置灰
  - 所有动作执行前二次确认，执行中防重复点击
  - 动作完成后刷新认证文件列表，并保留“建议重新批量检查”的提示
- Verification:
  - `(cd /home/cheng/git-project/Cli-Proxy-API-Management-Center && npm run type-check)`
  - `(cd /home/cheng/git-project/Cli-Proxy-API-Management-Center && npm run build)`
  - 本地开发实例手工验证：
    - `清理401失效` 仅针对本次结果中的 401 失效文件
    - `禁用已耗尽` 与 `恢复已恢复` 数量和候选项与 `aggregate.action_candidates` 一致
    - 动作后页面提示“建议重新批量检查以更新汇总统计”
- Stop Conditions:
  - 如果现有批量删除或启停接口无法满足“只处理本次候选集”的要求，先停下确认是否要新增后端处置接口

## Execution Handoff
- Execution Route: direct-inline
- Why This Route: 当前实现链路具备明确顺序依赖，先收敛后端统计，再切前端消费与联调，比并发拆分更容易控制兼容性与验收口径。
- Escalate To:
  - `ulw-governed`：如果实现跨多个会话继续推进，或需要引入显式阶段检查点
  - `multi-agent`：如果后续决定把后端聚合和前端页面重构拆成两个互不重叠写集
- Handoff Notes:
  - 执行时优先保护当前已存在的异步任务能力与测试通过状态
  - 若要声明“完成”，必须重新运行后端相关 `go test` 与前端 `type-check/build`，并做一次本地页面联调

## Notes
- 该计划是对既有 `20260331-auth-file-batch-check` 任务的 canonical implementation plan 更新，不新建平行 plan。
- 旧计划中的“单文件详情弹窗”目标已被新 spec 废弃，后续实现应以“汇总详情弹窗”取代。
