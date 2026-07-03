# 前后端吸收最新上游并保留 fork 定制实施计划

- 目标: 在保留 fork 自定义功能的前提下，将后端吸收到当前 `upstream/main@2884a67e` / `v7.2.9`，将前端 fork 主线吸收到 `upstream/main@b0db1df` / `v1.16.7`，并重新打通后端管理面板更新链路。
- 输入模式: clear-requirements
- 需求来源: session-confirmed: 用户确认分支模型、冲突点说明与处置建议；2026-06-15 独立评审指出 2026-06-12 基线过期并要求修订计划。
- Canonical Spec 路径: None
- 范围边界: 后端 `/home/cheng/git-project/CLIProxyAPI` 与前端 `/home/cheng/git-project/Cli-Proxy-API-Management-Center` 的上游同步、冲突处理、验证与发布前门禁设计；本计划不直接执行 dev 代码合并、远端推送或发布。
- 非目标: 不自动提交、推送、触发 release、部署或修改全局配置；不回退当前后端 `.gitignore` 改动；不改变 `main/master/dev` 分支职责；AMP/Ampcode 已由用户确认跟随上游移除，不另行保留兼容。
- 约束: `main` 只同步上游；`dev` 先吸收并验证；`master` 仅接收稳定结果；发现未确认冲突需暂停；前端发布产物必须更新到 fork release；不写入凭证或私密配置。
- 细化层级: contract-first
- 执行路由: direct_inline
- 为什么使用该路由: 该任务跨两个仓库、多个分支、多个发布链路，包含高冲突文件、跨 minor 破坏性变更和需要分阶段 checkpoint 的验证门禁，适合按 ULW loop 分段推进并保留恢复点。
- 升级触发条件: 单个 loop 内若出现可隔离的前后端并行实现包，可在该 loop 内启用 multi_agent；若出现未确认冲突、发布凭证需求、测试需要外部账号或需推送 / release，必须停止并请求用户确认。

## 文件结构

- 新建:
  - `.agents/tasks/20260612-sync-upstream-v7-fork-customizations/evidence/backend-branch-snapshot.md`
  - `.agents/tasks/20260612-sync-upstream-v7-fork-customizations/evidence/frontend-branch-snapshot.md`
  - `.agents/tasks/20260612-sync-upstream-v7-fork-customizations/evidence/upstream-release-notes-review.md`
  - `.agents/tasks/20260612-sync-upstream-v7-fork-customizations/evidence/backend-main-sync.md`
  - `.agents/tasks/20260612-sync-upstream-v7-fork-customizations/evidence/frontend-main-sync.md`
  - `.agents/tasks/20260612-sync-upstream-v7-fork-customizations/evidence/backend-release-config-resolution.md`
  - `.agents/tasks/20260612-sync-upstream-v7-fork-customizations/evidence/backend-runtime-resolution.md`
  - `.agents/tasks/20260612-sync-upstream-v7-fork-customizations/evidence/backend-verification.md`
  - `.agents/tasks/20260612-sync-upstream-v7-fork-customizations/evidence/frontend-scope-review.md`
  - `.agents/tasks/20260612-sync-upstream-v7-fork-customizations/evidence/frontend-release-config-resolution.md`
  - `.agents/tasks/20260612-sync-upstream-v7-fork-customizations/evidence/frontend-management-asset.md`
  - `.agents/tasks/20260612-sync-upstream-v7-fork-customizations/evidence/integration-verification.md`
- 修改:
  - `.agents/tasks/20260612-sync-upstream-v7-fork-customizations/findings.md`
  - `.agents/tasks/20260612-sync-upstream-v7-fork-customizations/progress.md`
  - `.agents/tasks/20260612-sync-upstream-v7-fork-customizations/handoff.md`
  - `/home/cheng/git-project/CLIProxyAPI/.github/workflows/release.yaml`
  - `/home/cheng/git-project/CLIProxyAPI/.goreleaser.yml`（删除）
  - `/home/cheng/git-project/CLIProxyAPI/Dockerfile`
  - `/home/cheng/git-project/CLIProxyAPI/cmd/server/main_test.go`
  - `/home/cheng/git-project/CLIProxyAPI/internal/api/handlers/management/handler.go`
  - `/home/cheng/git-project/CLIProxyAPI/internal/api/server_test.go`
  - `/home/cheng/git-project/CLIProxyAPI/internal/config/config.go`
  - `/home/cheng/git-project/CLIProxyAPI/internal/managementasset/updater.go`
  - `/home/cheng/git-project/CLIProxyAPI/internal/managementasset/updater_test.go`
  - `/home/cheng/git-project/CLIProxyAPI/internal/tui/config_tab.go`
  - `/home/cheng/git-project/CLIProxyAPI/internal/watcher/diff/config_diff_test.go`
  - `/home/cheng/git-project/CLIProxyAPI/sdk/cliproxy/auth/conductor.go`
  - `/home/cheng/git-project/CLIProxyAPI/sdk/cliproxy/auth/persist_policy_test.go`
  - `/home/cheng/git-project/CLIProxyAPI/sdk/cliproxy/auth/scheduler.go`
  - `/home/cheng/git-project/CLIProxyAPI/sdk/cliproxy/builder.go`
  - `/home/cheng/git-project/CLIProxyAPI/sdk/cliproxy/service.go`
  - `/home/cheng/git-project/CLIProxyAPI/sdk/cliproxy/service_stale_state_test.go`
  - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/.github/workflows/release.yml`
  - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/README.md`
  - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/README_CN.md`
  - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/bun.lock`
  - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/package.json`
  - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/components/config/VisualConfigEditor.*`
  - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/components/layout/MainLayout.tsx`
  - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/components/providers/`
  - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/components/quota/quotaConfigs.ts`
  - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/components/ui/`
  - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/features/authFiles/`
  - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/features/providers/`
  - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/hooks/useVisualConfig.ts`
  - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/i18n/locales/`
  - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/pages/AuthFilesPage.*`
  - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/pages/LogsPage.module.scss`
  - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/services/api/`
  - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/stores/index.ts`
  - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/styles/layout.scss`
  - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/types/`
  - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/utils/`
- 读取:
  - `/home/cheng/git-project/CLIProxyAPI/AGENTS.md`
  - `/home/cheng/git-project/CLIProxyAPI/config.example.yaml`
  - `/home/cheng/git-project/CLIProxyAPI/internal/api/modules/amp/`
  - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/CLAUDE.md`
  - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/.agents/tasks/20260527-sync-upstream/`
  - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/types/ampcode.ts`
  - 两仓 GitHub release notes / tags / compare logs
- 测试:
  - `/home/cheng/git-project/CLIProxyAPI/`
  - `/home/cheng/git-project/CLIProxyAPI/internal/api/...`
  - `/home/cheng/git-project/CLIProxyAPI/internal/managementasset/...`
  - `/home/cheng/git-project/CLIProxyAPI/sdk/cliproxy/...`
  - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/`
  - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/dist/index.html`

## 任务拆分

### 任务 1：刷新执行面、上游基线与 release notes 门禁

- 目标: 在不改动业务代码的前提下确认两个仓库的工作区、远端基线、divergence、冲突全集和跨 minor release notes 风险，建立当前执行入口。
- 文件:
  - 新建:
    - `.agents/tasks/20260612-sync-upstream-v7-fork-customizations/evidence/backend-branch-snapshot.md`
    - `.agents/tasks/20260612-sync-upstream-v7-fork-customizations/evidence/frontend-branch-snapshot.md`
    - `.agents/tasks/20260612-sync-upstream-v7-fork-customizations/evidence/upstream-release-notes-review.md`
  - 修改:
    - `.agents/tasks/20260612-sync-upstream-v7-fork-customizations/findings.md`
    - `.agents/tasks/20260612-sync-upstream-v7-fork-customizations/progress.md`
  - 读取:
    - `/home/cheng/git-project/CLIProxyAPI/.git`
    - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/.git`
    - 两仓 GitHub release notes / local tags
  - 测试:
    - None
- 依赖: None
- 验证: 两仓分别运行 `git fetch upstream --tags --prune`、`git fetch origin --tags --prune`、`git status --short --branch`、`git tag --points-at upstream/main`、`git rev-list --left-right --count origin/main...upstream/main`、`git rev-list --left-right --count main...origin/main`、`git rev-list --left-right --count --cherry-pick dev...upstream/main`、`git rev-list --left-right --count --cherry-pick master...upstream/main`、`git merge-tree --name-only --no-messages dev upstream/main | sed '1d'` (原始行数) 与 `git merge-tree --name-only --no-messages dev upstream/main | sed '1d' | sort -u` (唯一路径数,任务 1 同时记录两者,固定冲突计数口径);前端额外记录 `git log --oneline dev ^master`、`git log --oneline main ^origin/main`、`cat ~/.npmrc`、`rg -n 'npmmirror' bun.lock || true` 与 `package.json` scripts;release notes 覆盖后端 `v7.1.69..v7.2.7`、后端 `v7.2.7..upstream/main` 新增提交和前端 `v1.16.0..v1.16.7`。
- 停止条件: 任一仓库 `upstream/main` 与本计划记录不一致且未刷新 findings / plan；后端 `upstream/main` 无 tag 但未记录 tag 缺口处置；merge-tree 原始行数或唯一路径数与 findings 不一致且未更新；前端本地 `main` 存在本地独有提交但未明确处置；release notes / compare log 未覆盖新增提交；发现任务字段完整性异常。
### 任务 2：同步后端 main 作为上游镜像

- 目标: 将后端 `origin/main` 对齐到 `upstream/main@907e3493`，保持 `main` 不包含 fork 定制；该操作涉及推送 fork 远端，必须先获得用户明确授权。
- 文件:
  - 新建:
    - `.agents/tasks/20260612-sync-upstream-v7-fork-customizations/evidence/backend-main-sync.md`
  - 修改:
    - `/home/cheng/git-project/CLIProxyAPI` 的 `main` 分支引用
  - 读取:
    - `/home/cheng/git-project/CLIProxyAPI/.github/workflows/`
    - `/home/cheng/git-project/CLIProxyAPI/README.md`
  - 测试:
    - None
- 依赖: 任务 1
- 验证: 后端 `git rev-list --left-right --count origin/main...upstream/main` 变为 `0 0`；`git rev-parse origin/main` 完全等于 `git rev-parse upstream/main`；本地 `main` 在远端同步后 fast-forward 到 `origin/main`，并确认 `git rev-list --left-right --count main...origin/main` 为 `0 0`。
- 停止条件: 后端 `origin/main` 左侧计数非 0、同步需要强推、远端权限不足、`upstream/main` 已再次推进且未回到任务 1、用户未授权推送，或本地 `main` 有不可丢弃的本地提交。
- 交接说明: 若只允许本地准备，不推送远端，则记录本地状态并停止在推送前。

### 任务 3：同步前端 main 作为上游镜像

- 目标: 确认前端 `origin/main` 已对齐 `upstream/main@b0db1df` / `v1.16.7`，并在用户确认后处理本地 `main` 的 4 个治理文档本地提交，使本地 `main` 与远端镜像策略一致。
- 文件:
  - 新建:
    - `.agents/tasks/20260612-sync-upstream-v7-fork-customizations/evidence/frontend-main-sync.md`
  - 修改:
    - `/home/cheng/git-project/Cli-Proxy-API-Management-Center` 的 `main` 分支引用
  - 读取:
    - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/.github/workflows/`
    - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/README.md`
  - 测试:
    - None
- 依赖: 任务 1
- 验证: 前端 `git rev-list --left-right --count origin/main...upstream/main` 为 `0 0`；`git rev-parse origin/main` 完全等于 `git rev-parse upstream/main`；本地 `main` 已按用户确认直接对齐 `origin/main`，并确认 `git rev-list --left-right --count main...origin/main` 为 `0 0`。
- 停止条件: 前端 `origin/main` 左侧计数非 0、同步需要强推、远端权限不足、`upstream/main` 已再次推进且未回到任务 1、本地 `main` 存在业务 fork-only 提交，或本地 `main...origin/main` 不为 `0 0`。
- 交接说明: 第六轮评审声称 `87702bb` 是本地 `main` 独有业务提交，但本轮核验 `87702bb` 已存在于 `origin/main` / `upstream/main`；不得为该提交做重复 cherry-pick。

### 任务 4：后端 dev 吸收发布、配置、management asset 与 AMP 处置

- 目标: 在后端 `dev` 上吸收当前 `upstream/main@907e3493` 的发布链路、Docker、配置、management asset、plugin store 配置、API key exclusion、video auth、Claude cloak 全局开关、credential fallback 改进和 TUI / watcher diff 更新，同时保留 fork release 后缀、安装脚本、默认前端仓库和历史发布策略；发布体系以当前上游非 GoReleaser workflow 为基底，收敛为保留 `.github/workflows/release.yaml`、删除 `.goreleaser.yml`。
- 文件:
  - 新建:
    - `.agents/tasks/20260612-sync-upstream-v7-fork-customizations/evidence/backend-release-config-resolution.md`
  - 修改:
    - `/home/cheng/git-project/CLIProxyAPI/.github/workflows/release.yaml`
    - `/home/cheng/git-project/CLIProxyAPI/Dockerfile`
    - `/home/cheng/git-project/CLIProxyAPI/.goreleaser.yml`（删除）
    - `/home/cheng/git-project/CLIProxyAPI/internal/config/config.go`
    - `/home/cheng/git-project/CLIProxyAPI/internal/managementasset/updater.go`
    - `/home/cheng/git-project/CLIProxyAPI/internal/tui/config_tab.go`
    - `/home/cheng/git-project/CLIProxyAPI/internal/watcher/diff/config_diff_test.go`
    - `/home/cheng/git-project/CLIProxyAPI/config.example.yaml`
  - 读取:
    - `/home/cheng/git-project/CLIProxyAPI/internal/api/modules/amp/`
    - `/home/cheng/git-project/CLIProxyAPI/internal/managementasset/updater_test.go`
    - `/home/cheng/git-project/CLIProxyAPI/cmd/server/main_test.go`
  - 测试:
    - `/home/cheng/git-project/CLIProxyAPI/internal/managementasset/updater_test.go`
    - `/home/cheng/git-project/CLIProxyAPI/cmd/server/main_test.go`
- 依赖: 任务 1；任务 2 可并行完成但不作为代码合并前置
- 验证: 合并前创建 `backup/pre-merge-$(date +%F)-<short-sha>` 分支或 tag 并记录 sha；`gofmt -w` 后运行 `go test ./internal/managementasset ./cmd/server ./internal/watcher/diff`；确认默认管理面板仓库仍是 `wenxi96/Cli-Proxy-API-Management-Center`，并执行 `rg 'wenxi96/Cli-Proxy-API-Management-Center' internal/config/config.go`；确认 `.github/workflows/release.yaml` 不再引用 `goreleaser`，覆盖 fork 后缀、tag release、Docker / no-plugin 构建需求，且 `.goreleaser.yml` 不再作为发布入口存在；Dockerfile 冲突解决后运行可行的 Docker build 或记录无法运行原因；确认后端 AMP/Ampcode 模块、配置项、管理 API 和测试已按上游移除，且没有残余路由注册。
- 停止条件: 删除 `.goreleaser.yml` 会导致 fork release 后缀、安装脚本、历史 release 或 no-plugin 包任一能力缺失；需要改变用户可见版本语义；无法以单一 workflow 覆盖上游和 fork 发布需求；AMP/Ampcode 移除后仍残留不可编译引用。

### 任务 5：后端 dev 吸收 pluginhost、runtime、video 与 websocket 能力

- 目标: 合并上游 pluginhost、plugin store、plugin scheduler/executor/auth provider、host model callback、递归保护、interceptor skip、home credential forwarding、OpenAI video、XAI / Codex websocket 和 transcript compaction，同时保留 fork 范围轮询、quota auto-disable、usage persistence 与认证文件管理增强。
- 文件:
  - 新建:
    - `.agents/tasks/20260612-sync-upstream-v7-fork-customizations/evidence/backend-runtime-resolution.md`
  - 修改:
    - `/home/cheng/git-project/CLIProxyAPI/internal/api/handlers/management/handler.go`
    - `/home/cheng/git-project/CLIProxyAPI/internal/api/server_test.go`
    - `/home/cheng/git-project/CLIProxyAPI/sdk/cliproxy/auth/conductor.go`
    - `/home/cheng/git-project/CLIProxyAPI/sdk/cliproxy/auth/persist_policy_test.go`
    - `/home/cheng/git-project/CLIProxyAPI/sdk/cliproxy/auth/scheduler.go`
    - `/home/cheng/git-project/CLIProxyAPI/sdk/cliproxy/builder.go`
    - `/home/cheng/git-project/CLIProxyAPI/sdk/cliproxy/service.go`
    - `/home/cheng/git-project/CLIProxyAPI/sdk/cliproxy/service_stale_state_test.go`
  - 读取:
    - `/home/cheng/git-project/CLIProxyAPI/internal/runtime/executor/`
    - `/home/cheng/git-project/CLIProxyAPI/internal/translator/`
  - 测试:
    - `/home/cheng/git-project/CLIProxyAPI/sdk/cliproxy/auth/`
    - `/home/cheng/git-project/CLIProxyAPI/sdk/cliproxy/`
    - `/home/cheng/git-project/CLIProxyAPI/internal/api/server_test.go`
- 依赖: 任务 4
- 验证: 运行 `go test ./sdk/cliproxy/... ./internal/api/...`；新增或保留可执行定点测试覆盖 plugin callback 不递归、范围轮询仍选中正确 pool、quota auto-disable 仍触发、usage persistence 不丢状态、API key exclusion 不误伤 fork credential selection；若上游重构相关测试文件，必须确认 fork 断言未被覆盖丢失。
- 停止条件: 上游 plugin/runtime 调度模型与 fork scheduler 契约无法同时满足，或需要重写 `internal/translator/` 作为唯一改动范围。
- 接口 / 契约: `sdk/cliproxy/auth` 的 credential selection、disabled state、scoped pool selection 与 usage persistence 必须保持向后兼容。

### 任务 6：后端集成验证与稳定线合入准备

- 目标: 对后端 `dev` 的完整吸收结果做构建、单测、关键行为验证，并准备合入 `master` 的候选状态。
- 文件:
  - 新建:
    - `.agents/tasks/20260612-sync-upstream-v7-fork-customizations/evidence/backend-verification.md`
  - 修改:
    - `.agents/tasks/20260612-sync-upstream-v7-fork-customizations/progress.md`
  - 读取:
    - `/home/cheng/git-project/CLIProxyAPI/go.mod`
    - `/home/cheng/git-project/CLIProxyAPI/go.sum`
    - `/home/cheng/git-project/CLIProxyAPI/config.example.yaml`
  - 测试:
    - `/home/cheng/git-project/CLIProxyAPI/`
- 依赖: 任务 4；任务 5
- 验证: `go test ./...`；`go build -o test-output ./cmd/server && rm test-output`；必要时补充 `go test ./internal/api/handlers/management ./internal/managementasset ./sdk/cliproxy/...`。
- 停止条件: 全量测试失败且根因不明确、构建失败、管理面板默认源被改回上游、AMP/Ampcode 处置与用户确认不一致、或 fork 定制回归。
- 交接说明: 合入 `master` 和推送远端必须单独获得用户授权。

### 任务 7：前端同步范围复核并选择集成基底

- 目标: 在前端仓库确认 `main` 已对齐 `upstream/main@b0db1df`，复核旧任务 `20260527-sync-upstream` 的结果，刷新 `dev@a02ebbc` 领先 `master` 的真实基线，并确定本次以 `v1.16.7` 上游新架构为基底重移植 fork 定制。
- 文件:
  - 新建:
    - `.agents/tasks/20260612-sync-upstream-v7-fork-customizations/evidence/frontend-scope-review.md`
  - 修改:
    - `.agents/tasks/20260612-sync-upstream-v7-fork-customizations/findings.md`
    - `.agents/tasks/20260612-sync-upstream-v7-fork-customizations/progress.md`
  - 读取:
    - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/.agents/tasks/20260527-sync-upstream/`
    - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/CLAUDE.md`
    - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/package.json`
  - 测试:
    - None
- 依赖: 任务 1；任务 3
- 验证: `git rev-list --left-right --count origin/main...upstream/main` 为 `0 0`；`git log --oneline dev ^master` 记录 `a02ebbc`；`git rev-list --left-right --count --cherry-pick dev...upstream/main` 记录当前 `58 153`；`git merge-tree --name-only --no-messages dev upstream/main | sed '1d'` 记录 60 个冲突文件；合并前创建 `backup/pre-merge-$(date +%F)-<short-sha>` 分支或 tag 并记录 sha；确认旧任务只作为 predecessor，不作为当前 canonical plan。
- 停止条件: 前端 `main` 与 `upstream/main` 不一致、`a02ebbc` 未随 `dev -> master` 流动、冲突文件数与 findings 不一致且未修订、旧任务处于仍在执行且竞争同一分支、前端 Ampcode 移除未按用户已确认的上游路径完成，或用户要求继续旧任务而不是新建联合任务。

### 任务 8：前端 dev 吸收 Provider、Auth Files、Plugin、Quota、Logs 与 Ampcode 处置

- 目标: 在前端 `dev` 上吸收 `v1.16.7` Provider Workbench、Plugin Store、plugin management、quota reset / subscription expiry、连接测试、模型发现、UI state persistence、fullscreen / error logs、logs pagination、auth-files 修复，同时重建 DisplayName、Scoped Poll、批量检查和多选 zip 下载能力。
	- 文件:
	  - 新建:
	    - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/components/usage/ApiDetailsCard.tsx`
	    - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/components/usage/ChartLineSelector.tsx`
	    - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/components/usage/CostTrendChart.tsx`
	    - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/components/usage/CredentialStatsCard.tsx`
	    - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/components/usage/ModelStatsCard.tsx`
	    - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/components/usage/PriceSettingsCard.tsx`
	    - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/components/usage/RequestEventsDetailsCard.tsx`
	    - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/components/usage/ServiceHealthCard.tsx`
	    - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/components/usage/StatCards.tsx`
	    - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/components/usage/TokenBreakdownChart.tsx`
	    - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/components/usage/UsageChart.tsx`
	    - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/components/usage/hooks/index.ts`
	    - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/components/usage/hooks/useChartData.ts`
	    - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/components/usage/hooks/useSparklines.ts`
	    - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/components/usage/hooks/useUsageData.ts`
	    - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/components/usage/index.ts`
	    - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/features/authFiles/hooks/useAuthFilesStats.ts`
	    - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/pages/UsagePage.module.scss`
	    - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/pages/UsagePage.tsx`
	    - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/services/api/usage.ts`
	    - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/stores/useUsageStatsStore.ts`
	    - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/types/sourceInfo.ts`
	    - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/types/usage.ts`
	    - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/utils/sourceResolver.ts`
	    - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/utils/usage.ts`
	    - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/utils/usage/chartConfig.ts`
	    - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/utils/usage/index.ts`
	    - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/utils/usage/latency.ts`
	    - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/utils/usageIndex.ts`
  - 修改:
    - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/bun.lock`
    - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/components/providers/`
    - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/components/quota/quotaConfigs.ts`
    - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/components/ui/`
    - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/features/providers/`
    - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/features/authFiles/`
    - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/components/config/VisualConfigEditor.*`
    - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/components/layout/MainLayout.tsx`
    - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/pages/AuthFilesPage.*`
    - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/pages/LogsPage.module.scss`
    - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/services/api/`
    - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/hooks/useVisualConfig.ts`
    - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/stores/index.ts`
    - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/styles/layout.scss`
    - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/types/`
    - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/utils/`
  - 读取:
    - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/types/ampcode.ts`
    - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/i18n/locales/`
    - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/pages/`
  - 测试:
    - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/`
- 依赖: 任务 7
- 验证: 以任务 7 的 60 个 merge-tree 冲突文件全集为权威输入逐项解决，并分三批 checkpoint：8a 核心架构层（`bun.lock`、`package.json`、stores、types、utils、styles、hooks）、8b 功能域（providers、authFiles、quota、logs、plugins、usage）、8c 文案与配置（i18n、README、VisualConfigEditor、MainLayout、release 相关）；每批后至少运行 `bun run type-check`，可行时同步运行 `bun run build`；人工检查 DisplayName 输入和展示、Auth Files 批量检查跨页状态、Scoped Poll 总开关、多选 zip 下载入口、Plugin Store、plugin delete / install warning、quota reset / subscription expiry、logs pagination、Usage 统计卡片、Usage 图表时间范围与图表线持久化、Usage 模型/API 统计、Usage 导出/导入和移动端布局；在 evidence 中记录 Ampcode 最终处置依据。
- 停止条件: 需要保留被上游删除的旧 provider 编辑页作为主路径、i18n key 大面积缺失、批量检查 / Scoped Poll 任一 fork 定制无法迁移到新架构、AMP/Ampcode 移除后仍残留前端入口或 API client 调用已删除后端接口，或发现新的冲突文件未补入 evidence / findings。
- 接口 / 契约: provider payload 仍需序列化 fork 的 `display-name` 和 scoped-pool 相关字段；Auth Files API 仍需兼容后端批量检查与 zip 下载接口。

### 任务 9：前端构建链路、lockfile、i18n 与 release 策略收敛

- 目标: 吸收上游 Bun / Vite / Node 24 构建链路和 release workflow 改进，同时保留 fork tag-only 发布、版本后缀、release notes 规则和完整 locale 文案。
- 文件:
  - 新建:
    - `.agents/tasks/20260612-sync-upstream-v7-fork-customizations/evidence/frontend-release-config-resolution.md`
  - 修改:
    - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/.github/workflows/release.yml`
    - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/bun.lock`
    - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/package.json`
    - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/i18n/locales/en.json`
    - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/i18n/locales/ru.json`
    - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/i18n/locales/zh-CN.json`
    - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/i18n/locales/zh-TW.json`
    - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/README.md`
    - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/README_CN.md`
  - 读取:
    - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/.agents/tasks/20260527-sync-upstream/evidence/commit-scope-review-2026-05-29.md`
  - 测试:
    - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/dist/index.html`
- 依赖: 任务 8
- 验证: 解决 `bun.lock` add/add 冲突后，推荐接受上游 lockfile 为基底并追加 fork 依赖，再使用 `npm_config_registry=https://registry.npmjs.org bun install` 或等价方式屏蔽本机 `~/.npmrc` 镜像影响；`rg -n 'npmmirror' bun.lock` 必须为空；`bun install --frozen-lockfile` 必须退出码 0；`bun run build` 生成单文件 `dist/index.html`；检查 release workflow 仍仅 tag 触发正式发布；执行 locale key 完整性检查或记录等价人工核对。
- 停止条件: `bun.lock` 含 `npmmirror`、`bun install --frozen-lockfile` 失败、构建工具迁移要求删除 fork release 后缀、workflow 会在非 tag 上正式发布、或 locale 缺失导致关键 fork UI 无文案。

### 任务 10：前端 management.html 发布候选与后端更新链路验证

- 目标: 生成并验证新的前端 `management.html`，确认后端管理面板更新器能从 fork 前端仓库获取正确 release 产物。
- 文件:
  - 新建:
    - `.agents/tasks/20260612-sync-upstream-v7-fork-customizations/evidence/frontend-management-asset.md`
  - 修改:
    - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/dist/index.html`
    - `/home/cheng/git-project/CLIProxyAPI/internal/managementasset/updater.go`
  - 读取:
    - `/home/cheng/git-project/CLIProxyAPI/internal/managementasset/updater_test.go`
    - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/.github/workflows/release.yml`
  - 测试:
    - `/home/cheng/git-project/CLIProxyAPI/internal/managementasset/updater_test.go`
- 依赖: 任务 6；任务 9
- 验证: 前端 `bun run build` 通过；后端 `go test ./internal/managementasset` 通过；在 evidence 记录本轮目标 release tag 命名和 fork `-wx-` 后缀递增依据；人工核对 latest release tag 与 asset 命名符合后端下载逻辑，并说明新 release 发布前后端仍拉取旧面板的过渡窗口。
- 停止条件: 需要真实 GitHub release 发布、需要 token / 凭证、或 latest release 仍指向旧 `management.html` 且用户未授权发布。
- 交接说明: 实际发布前端 release、上传 asset、后端拉取线上 release 都需要用户另行授权。

### 任务 11：前后端联合回归与 master 合入门禁

- 目标: 在后端与前端都完成 `dev` 吸收后，执行联合验证，确认 fork 自定义功能和上游新增功能均可用，再准备合入 `master`。
- 文件:
  - 新建:
    - `.agents/tasks/20260612-sync-upstream-v7-fork-customizations/evidence/integration-verification.md`
  - 修改:
    - `.agents/tasks/20260612-sync-upstream-v7-fork-customizations/progress.md`
    - `.agents/tasks/20260612-sync-upstream-v7-fork-customizations/handoff.md`
  - 读取:
    - `/home/cheng/git-project/CLIProxyAPI/config.example.yaml`
    - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/dist/index.html`
    - `/home/cheng/git-project/CLIProxyAPI/.github/workflows/release.yaml`
    - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/.github/workflows/release.yml`
  - 测试:
    - `/home/cheng/git-project/CLIProxyAPI/`
    - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/dist/index.html`
- 依赖: 任务 6；任务 10
- 验证: 后端 `go test ./...` 与 `go build -o test-output ./cmd/server && rm test-output` 通过；前端 `bun install --frozen-lockfile` 与 `bun run build` 通过；后端必须有可执行测试覆盖 scoped pool selection、quota auto-disable、usage persistence、plugin callback 非递归；人工回归 DisplayName、Auth Files 批量检查、Scoped Poll、zip 下载、plugin management / store、fullscreen / error logs、logs pagination、xAI / Grok quota、video、websocket、API key exclusion；合入 master 后运行 `git merge-base --is-ancestor a02ebbc master` 确认 `a02ebbc` 随 `dev -> master` 流动。
- 停止条件: 任一 fork 定制回归、任一上游新增关键功能不可用、AMP/Ampcode 处置与用户确认不一致、存在无法解释的测试失败、或合入 `master` / 推送 / 发布未获授权。
- 交接说明: 合入 `master` 后应立即更新两仓库 `.agents` 进度与 handoff，并记录最终验证证据。

## 执行交接

- 执行路由: direct_inline
- 为什么使用该路由: 前后端同步跨仓库、跨分支、跨发布链路，并且需要在每个 checkpoint 后保留可恢复状态；单轮 direct inline 容易丢失冲突决策和验证证据。
- 升级到: 在单个 ULW loop 内，如果后端任务 4-6 与前端任务 7-9 可以隔离写入，可启用 `multi_agent`；涉及 release、push、真实账号验证时不得自动升级，必须先获得用户授权。
- 交接说明:
  - 先执行任务 1，确认 execution surface、上游 SHA/tag、release notes、未提交改动边界和冲突全集。
  - 如果任务 1 发现 `upstream/main` 已不同于后端 `907e3493` / 前端 `b0db1df`，必须先刷新 findings 与本计划，再继续。
  - 当前后端 `origin/main` 与本地 `main` 已同步到 `upstream/main@907e3493`；当前前端 `origin/main` 与本地 `main` 已同步到 `upstream/main@b0db1df`。
  - 后端任务 4 与前端任务 7 进入实际 merge 前必须先创建本地 backup 分支或 tag，并把 sha 记录到 evidence。
  - 后端任务 2 与前端任务 3 是 `main` 镜像同步任务；涉及远端推送必须先获得用户授权。
  - AMP/Ampcode 是新增破坏性变更：用户已确认接受上游移除；任务 4/8 按移除路径执行，不保留兼容。
  - 后端任务 4-6 与前端任务 7-9 可分阶段推进，但任务 10 依赖两边结果。
  - 任务 11 是 master 合入前的共同门禁，不通过不得合入稳定分支。
  - 每个阶段都要更新本任务 `progress.md`，稳定事实补入 `findings.md`，命令输出或人工核对摘要放入 `evidence/`。

## 备注

- 后端旧分支 `sync/v7-preserve-fork@e0331af9` 只作为历史参考，不是当前 `upstream/main@907e3493` 吸收结果。
- 前端旧任务 `/home/cheng/git-project/Cli-Proxy-API-Management-Center/.agents/tasks/20260527-sync-upstream/` 是 predecessor/reference；当前任务的 canonical plan 以本文件为准。
- 第一至第三轮 2026-06-12 评审结论已被第四轮独立评审撤回；本计划是基于 2026-06-15 刷新后的修订版。
- 本计划已按用户确认的冲突处置原则编写；出现未列出的实质功能冲突时，应暂停执行并回到用户确认。
