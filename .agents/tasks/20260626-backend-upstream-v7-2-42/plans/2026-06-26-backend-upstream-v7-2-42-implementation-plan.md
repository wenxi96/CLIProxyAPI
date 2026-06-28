# 后端吸收上游 v7.2.42 实施计划

- 目标: 将 `CLIProxyAPI` 后端 `dev` 吸收到 `origin/main == upstream/main == 4c0c6029` / `v7.2.42`，保留 fork scoped-pool、quota auto-disable、usage persistence、external auth lifecycle 等定制能力。
- 输入模式: clear-requirements
- 需求来源: request:用户要求前后端分别落地上游吸收计划、独立审核修复后再改代码
- Canonical Spec 路径: None
- 范围边界: 仅后端仓库；仅 `dev <- origin/main` 的上游吸收、冲突解决、验证与后续 `master` 候选评估；不覆盖前端。
- 非目标: 不 push、不 tag、不 release、不部署、不修改运行实例；不把前端任务写入本任务目录。
- 约束: 代码改动前必须完成独立审核修复；分支模型保持 `main` 镜像、`dev` 集成、`master` 稳定；敏感信息不得写入 `.agents`。
- 细化层级: contract-first
- 执行路由: ulw_governed
- 为什么使用该路由: 任务跨计划、审核、合并、验证、可能的 `master` 推进多个阶段，并且用户明确要求长任务和多 agent 处理。
- 升级触发条件: 冲突扩展到 translator/runtime 大面积改造；验证失败超过一次且根因不清；需要外部凭证、push、tag、release 或运行实例操作。

## 文件结构

- 新建:
  - `.agents/tasks/20260626-backend-upstream-v7-2-42/task-charter.md`
  - `.agents/tasks/20260626-backend-upstream-v7-2-42/ulw-board.md`
  - `.agents/tasks/20260626-backend-upstream-v7-2-42/ulw-state.json`
  - `.agents/tasks/20260626-backend-upstream-v7-2-42/loops/`
  - `.agents/tasks/20260626-backend-upstream-v7-2-42/plans/`
  - `.agents/tasks/20260626-backend-upstream-v7-2-42/evidence/`
- 修改:
  - `.agents/README.md`
  - `cmd/server/main.go`
  - `internal/runtime/executor/xai_executor.go`
  - `sdk/cliproxy/auth/conductor.go`
  - fork 定制保留清单相关文件：`sdk/cliproxy/service.go`; `sdk/cliproxy/auth/types.go`; `sdk/cliproxy/auth/scheduler.go`; `sdk/cliproxy/auth/scoped_pool.go`; `sdk/cliproxy/auth/quota_check_async.go`; `sdk/cliproxy/auth/active_quota_refresh_pool.go`; `internal/authquota/service.go`; `internal/usage/persistence.go`
  - 以及 merge 自动修改的上游文件
- 读取:
  - `AGENTS.md`
  - `CLAUDE.md`
  - `.agents/tasks/20260612-sync-upstream-v7-fork-customizations/`
  - `dev..origin/main`
- 测试:
  - L03 前环境预检：`go version` 或 `docker image inspect golang:1.26`
  - L03 前合并预演：`git merge-tree --write-tree --name-only dev origin/main`
  - L03 合并后冲突检查：`git diff --name-only --diff-filter=U`; `rg -n "^<<<<<<<|^=======|^>>>>>>>" <changed files>`
  - 聚焦测试：`go test ./internal/runtime/executor -run 'Test.*XAI|Test.*Grok'`
  - 聚焦测试：`go test ./sdk/cliproxy/auth -run 'Test.*(OAuthAlias|APIKeyAlias|ScopedPool|OpenAICompat|Quota|ActiveQuota)'`
  - 聚焦测试：`go test ./sdk/cliproxy -run 'Test.*(ExternalAuthRegistration|UsagePersistence|Plugin)'`
  - 聚焦测试：`go test ./internal/homeplugins ./internal/home ./cmd/server -run 'Test.*(Home|Plugin|Sync|Report|RuntimeDefaults)'`
  - `go test ./...`
  - `go build -buildvcs=false -o /tmp/cli-proxy-api-check ./cmd/server`

## 任务拆分

### 任务 1：计划和提交清单落地

- 目标: 建立后端独立任务目录、提交级吸收清单、冲突策略和 ULW 状态。
- 文件:
  - 新建: `.agents/tasks/20260626-backend-upstream-v7-2-42/**`
  - 修改: `.agents/README.md`
  - 读取: `.agents/tasks/20260612-sync-upstream-v7-fork-customizations/**`
  - 测试: 检查任务目录中不存在未完成占位语句。
- 依赖: None
- 验证: 文件存在、计划必填字段完整、findings 覆盖 28 个上游提交。
- 停止条件: 发现同目标新任务已存在；`.agents` 持久化模式冲突。

### 任务 2：独立审核修复

- 目标: 由 reviewer/verifier 检查提交吸收建议、冲突策略和验证路径，主线程修正阻断问题。
- 文件:
  - 新建: `.agents/tasks/20260626-backend-upstream-v7-2-42/coordination/L02-review/`
  - 修改: `.agents/tasks/20260626-backend-upstream-v7-2-42/findings.md`; `.agents/tasks/20260626-backend-upstream-v7-2-42/progress.md`
  - 读取: `cmd/server/main.go`; `internal/runtime/executor/xai_executor.go`; `sdk/cliproxy/auth/conductor.go`
  - 测试: read-only review; no code tests
- 依赖: 任务 1
- 验证: reviewer/verifier 结论均无阻断项，或阻断项已修正文档并重新审核。
- 停止条件: reviewer 发现需要改变核心合并策略；验证路径不可执行。
- 交接说明: 多 agent 默认 read-only，禁止直接写业务代码。
- 审核修复要求:
  - 若 reviewer 报告 high/critical，必须先更新 `findings.md` / 本计划并复审。
  - reviewer 报告结构若机器审计失败，主线程必须记录该限制，并用 disposition 文件逐条裁决 findings。

### 任务 3：执行 `dev <- origin/main` 合并

- 目标: 在审核通过后，将最新上游合入后端 `dev` 并解决冲突。
- 文件:
  - 新建: None
  - 修改: `cmd/server/main.go`; `internal/runtime/executor/xai_executor.go`; `sdk/cliproxy/auth/conductor.go`; merge 自动修改文件
  - 读取: `findings.md`
- 测试: `git diff --name-only --diff-filter=U`; `rg -n "^<<<<<<<|^=======|^>>>>>>>" <changed files>`; `gofmt`
- 依赖: 任务 2
- 验证: 无 unmerged 文件、无 conflict marker、`gofmt` 后无意外格式问题；fork preservation checklist 中的文件/符号仍存在；semantic risk files 已人工核对。
- 停止条件: 冲突超出 3 个已知文件；fork scoped-pool 或 external auth lifecycle 被覆盖。
- 冲突解决不变式:
  - `cmd/server/main.go`: Home config 使用 fork `applyHomeRuntimeDefaults` 后再走上游 report-aware plugin sync；插件 runtime apply 后保留上游 `MarkLoadResults` 和第二次 `ReportPluginStatus`。
  - `xai_executor.go`: 保留 reasoning replay cache、invalid `encrypted_content` sanitizer、empty reasoning object deletion 和完成事件 cache 写入完整链路。
  - `conductor.go`: scoped-pool 先过滤 auth candidate set；alias candidate / response rewrite 作为 selected auth 的执行模型处理，不得让 alias 绕过 scoped-pool。

### 任务 4：后端验证与修复

- 目标: 运行后端验证，按失败证据最小修复。
- 文件:
  - 新建: 必要测试或 evidence
  - 修改: 仅限失败根因相关文件
  - 读取: `go.mod`; 相关测试文件
- 测试:
  - `go test ./internal/runtime/executor -run 'Test.*XAI|Test.*Grok'`
  - `go test ./sdk/cliproxy/auth -run 'Test.*(OAuthAlias|APIKeyAlias|ScopedPool|OpenAICompat|Quota|ActiveQuota)'`
  - `go test ./sdk/cliproxy -run 'Test.*(ExternalAuthRegistration|UsagePersistence|Plugin)'`
  - `go test ./internal/homeplugins ./internal/home ./cmd/server -run 'Test.*(Home|Plugin|Sync|Report|RuntimeDefaults)'`
  - `go test ./...`
  - `go build -buildvcs=false -o /tmp/cli-proxy-api-check ./cmd/server`
- 依赖: 任务 3
- 验证: 命令 exit 0；OAuth alias + scoped-pool 组合行为有测试或等价证据；Home plugin sync/report 两阶段有测试或等价证据；失败修复后更新 `progress.md`。
- 停止条件: 同一错误族连续失败三次；需要外部服务凭证。

### 任务 5：收口和后续推进建议

- 目标: 更新 handoff、progress、必要 evidence，给出是否可进入 `master` 评估的结论。
- 文件:
  - 新建: `.agents/tasks/20260626-backend-upstream-v7-2-42/evidence/*`
  - 修改: `handoff.md`; `progress.md`; `ulw-board.md`; `ulw-state.json`
  - 读取: `git status`; validation outputs
  - 测试: 文档核查和工作区状态核对
- 依赖: 任务 4
- 验证: 文档状态与代码状态一致；无未记录验证缺口。
- 停止条件: 用户未授权 push、tag、release 时不得继续外部副作用。

## 执行交接

- 执行路由: ulw_governed
- 为什么使用该路由: 合并具有已知冲突和验证风险，且用户要求先审核、再代码改动。
- 升级到: `multi_agent` nested review for L02；必要时使用 isolated worktree for L03。
- 交接说明: 子 agent 只读审查；主线程负责最终冲突裁决和业务代码写入。

## 备注

- 后端本地 `main` 落后 `origin/main`，但本任务以 `origin/main` / `upstream/main` 为最新事实来源。
- 旧任务 `20260612-sync-upstream-v7-fork-customizations` 是历史 predecessor，不再作为本任务 authority。
