# Findings

## 已确认分支模型

- 后端与前端均采用同一分支职责：
  - `main` 只保持与上游同步，不承载 fork 定制。
  - `master` 是自定义 fork 的稳定主分支。
  - `dev` 是自定义 fork 的开发分支。
- 上游吸收应先在 `dev` 完成冲突处理与验证，再合入 `master`。
- `main` 的同步与 fork 定制线的吸收要分开处理，避免污染上游镜像。
- 2026-06-15 独立评审发现 2026-06-12 基线已过期；2026-06-16 HKT 再次 `fetch upstream --tags --prune` / `fetch origin --tags --prune` 后，后端上游继续推进到 `upstream/main@2884a67e` / `v7.2.9`，前端保持 `v1.16.7`。

## 后端仓库事实

- 仓库：`/home/cheng/git-project/CLIProxyAPI`
- 当前工作区：`dev...origin/dev`
- 当前已有未提交改动：`.gitignore`、`.agents/README.md`、本任务目录；实施时不得覆盖或回退既有 `.gitignore` 改动。
- `dev == origin/dev == f52451d8`。
- `master == origin/master == c9fa502d`，tag `v7.1.23-wx-2.4`。
- 本地 `main == 907e3493`，已对齐 `origin/main`；`main...origin/main = 0 0`。
- `origin/main == 907e3493`，当前落后最新 `upstream/main`；`origin/main...upstream/main = 0 4`。
- `upstream/main == 2884a67e`，tag `v7.2.9`。
- `dev...upstream/main` 计数为 `90 198`，`master...upstream/main` 计数为 `89 198`，说明 fork 主线未完整吸收当前上游。
- 本地存在 `sync/v7-preserve-fork@e0331af9`，只可作为旧同步参考；该分支停在 `v7.0.6-wx-2.3`，不能视为已完成当前 `v7.2.7` 吸收。

## 后端 release notes / 跨 minor 风险

- 已读取 GitHub release notes：`v7.1.69` 至 `v7.2.9`；`v7.2.4` API 请求短暂失败，已用本地 tag message 与 `git log v7.2.3..v7.2.4` 补齐；v7.2.6 至 v7.2.9 主要涉及日志 cursor、插件源、插件删除 reload、tool_result 标准化、ModelRouter、stream callback 生命周期、文档赞助信息和 `video_url` 提取 / 校验，未发现新增 feat! 级破坏性变更。
- `v7.2.7..upstream/main` 目前新增 6 个提交：`8fad0d03 feat(config+executor): add global Claude cloak mode toggle and improve credential fallback logic`、`907e3493 docs: update VisionCoder details in README files`、`9f940f16 fix(pluginhost): keep stream callbacks alive until stream close`、`87132e54 feat(plugin): add ModelRouter before auth with single-slot routing targets (#3865)`、`f63cf982 docs: add CatAPI sponsorship details to README files`、`2884a67e feat(videos): add support for video_url extraction and validation in handlers`。需要在后端配置 / executor / plugin runtime / video handler 合并时吸收这些行为；README 文档更新可直接吸收。
- `v7.2.0` 包含 `feat!: remove amp integration support`、移除 legacy migration code、移除 deprecated route module interfaces，是本轮最高风险破坏性变更。
- 当前 fork 仍存在 `internal/api/modules/amp/`、`config.example.yaml` 中 `ampcode` 配置示例、`internal/api/server.go` 的 Amp 管理路由和 config update 钩子；该差异必须在实施前确认处置。
- 用户决策：跟随上游移除 AMP/Ampcode，不另行保留兼容。实施时同步删除后端 Amp 模块、配置项、管理 API、测试与前端入口；不得保留残余前端入口调用已删除的后端 API。
- 其他后端新增范围：plugin store / plugin management、plugin support header、HTML/JSON sanitize、Antigravity WebSearch bridge、plugin delete、`disable-image-generation: passthrough`、home credential forwarding、config API key exclusion、OpenAI video support、XAI / Codex websocket、websocket transcript compaction、video auth binding。

## 后端已确认冲突处置

| 冲突域 | 上游功能与作用 | Fork 功能与作用 | 已确认处置 |
|---|---|---|---|
| 发布链路 | GLIBC 2.17、`no-plugin` 包、Docker / workflow 改进，上游删除 `.goreleaser.yml` | fork 版本后缀、tag 发布、安装脚本、历史发布迁移 | 保留 fork 版本语义与历史发布能力，发布体系收敛到 `.github/workflows/release.yaml`，删除 `.goreleaser.yml`，避免双体系长期并存 |
| 配置 | 新增 `plugins`、API key exclusion、video auth、`disable-image-generation: passthrough` 等配置 | 默认管理面板仓库指向 `https://github.com/wenxi96/Cli-Proxy-API-Management-Center`，fork quota / usage 配置 | 保留 fork 面板源与 fork 配置语义，追加上游新配置；AMP/Ampcode 按用户确认跟随上游移除 |
| 管理 API | plugin management API、plugin store、pluginhost 资源管理、API key exclusion | 认证文件批量检查、范围轮询、quota、usage 管理增强、Ampcode 管理入口 | 在 fork 管理接口上追加 plugin/API key exclusion，不回退既有 fork 管理能力；Ampcode 管理入口按用户确认跟随上游移除 |
| runtime service/auth/scheduler | pluginhost、plugin scheduler/executor/auth provider、host model callback、递归保护、interceptor skip、video / websocket 能力 | 范围轮询、quota auto-disable、usage persistence、自定义 credential 行为 | 以上游 plugin/runtime 架构为主线吸收，接入 fork 调度、额度与 usage 逻辑，重点防止递归和状态漂移 |
| management asset | Home / cluster skip 等自动更新修复 | fork 面板源与重试增强 | 保留 fork 面板源，吸收上游自动更新修复 |
| TUI / watcher diff | 上游新增 config tab 与 config diff 测试调整 | fork 配置展示与热重载行为 | 吸收上游测试与 UI 行为，同时确保 fork 新配置不从 TUI / watcher diff 中丢失 |

## 后端 merge-tree 冲突全集

- 实测命令：`git merge-tree --name-only --no-messages dev upstream/main | sed '1d'`
- 当前冲突文件数：17
- `.github/workflows/release.yaml`
- `.goreleaser.yml`
- `Dockerfile`
- `cmd/server/main_test.go`
- `internal/api/handlers/management/handler.go`
- `internal/api/server_test.go`
- `internal/config/config.go`
- `internal/managementasset/updater.go`
- `internal/managementasset/updater_test.go`
- `internal/tui/config_tab.go`
- `internal/watcher/diff/config_diff_test.go`
- `sdk/cliproxy/auth/conductor.go`
- `sdk/cliproxy/auth/persist_policy_test.go`
- `sdk/cliproxy/auth/scheduler.go`
- `sdk/cliproxy/builder.go`
- `sdk/cliproxy/service.go`
- `sdk/cliproxy/service_stale_state_test.go`

## 前端仓库事实

- 仓库：`/home/cheng/git-project/Cli-Proxy-API-Management-Center`
- 当前工作区：`dev...origin/dev`，工作区干净。
- `origin/main == upstream/main == b0db1df`，tag `v1.16.7`；本轮 fetch 后 `origin/main...upstream/main = 0 0`，远端 `main` 已同步上游。
- 本地 `main == b0db1df`，已按用户确认直接移除 4 个本地治理文档提交并对齐 `origin/main`；`main...origin/main = 0 0`。
- `master == origin/master == c54efc0`。
- `dev == origin/dev == a02ebbc`，tag `v1.14.0-wx-2.6`，提交为 `fix(deps): add chart.js and react-chartjs-2 to bun.lock`，当前 `dev` 领先 `master` 一个 bun lock 修复提交。
- `dev...upstream/main` 计数为 `58 153`，`master...upstream/main` 计数为 `57 153`，说明 fork 主线未把 `v1.16.7` 作为祖先完整包含进来，且后续 `dev -> master` 必须保留 `a02ebbc`。
- 前端已有历史任务 `/home/cheng/git-project/Cli-Proxy-API-Management-Center/.agents/tasks/20260527-sync-upstream/`，其基线为旧 `upstream/main@87702bb`，本次作为 predecessor/reference，不直接覆盖其历史计划。
- 本机 `~/.npmrc` 当前配置 `registry=https://registry.npmmirror.com`；当前 `bun.lock` 未检出 `npmmirror` 字符串，但实施中若重生成 lockfile，必须强制使用官方 registry 并执行 frozen lockfile 验证。
- `package.json` 当前存在 `build`、`lint`、`type-check` scripts。

## 前端 release notes / 跨 minor 风险

- 已读取 GitHub release notes：`v1.16.0` 至 `v1.16.7`;v1.16.7 优化 `fetchCompleteHomeLogs` 分页与性能,未发现新增 feat! 级破坏性变更。
- `v1.16.0` 引入 Plugin Store、plugin management、quota reset / subscription expiry、error log viewer、provider workbench 大量重构与 quota monthly limits。
- `v1.16.1` 引入 plugin feature detection、plugin system config、disable cooling provider 配置、plugin mutation loading / refresh。
- `v1.16.2` 至 `v1.16.4` 主要增强 logs 与 Plugin Store 描述 / 删除能力。
- `v1.16.5` 包含 Codex connectivity test、third-party plugin install security warning、plugin source、quota weekly/monthly window 调整，并包含 `Refactor: Remove Ampcode integration and related configurations`。
- `v1.16.7` 优化 `fetchCompleteHomeLogs` 分页与性能。
- 当前 fork 前端仍存在 `src/types/ampcode.ts`、provider `ampcode` 类型、locale 中 Ampcode 文案、README Ampcode 描述和 provider category Ampcode 展示；该差异必须与后端 `v7.2.0` AMP 移除一起确认。

## 前端不可丢失定制

- DisplayName：凭证自定义展示名，provider 编辑页输入、卡片标题展示。
- Auth Files Batch Check 增强：tiered 重启选择模态、结果跨页持久化、mobile 可达性。
- Scoped Poll：VisualConfigEditor 总开关、AuthFileCard 徽章展示。
- 认证文件多选压缩下载。
- CI / Release：仅 tag 触发正式发布、保留 fork 版本后缀。

## 前端已确认冲突处置

| 冲突域 | 上游功能与作用 | Fork 功能与作用 | 已确认处置 |
|---|---|---|---|
| Provider Workbench | 新 provider workbench、资源表格、连接测试、模型发现、UI state persistence、plugin feature detection、disable cooling、Codex connectivity test | DisplayName、Scoped Poll 展示、usage 入口 | 以 `v1.16.7` 上游新架构为基底，重新移植 fork 定制，不保留旧 fork 文件硬合 |
| Auth Files | HTML challenge 展示、invalid content copy、websocket 文案等修复、本地排序优化 | 批量检查、tiered reenable、批量 zip、跨页状态 | 吸收上游修复，同时保留 fork 批量与跨页能力 |
| Usage / logs | fullscreen logs、error log viewer、logs pagination、新布局导航 | usage 持久化 UI | 接受上游日志能力，保留 fork usage 页面并适配新布局 |
| Plugin Store / plugins | Plugin Store、plugin management、third-party install security warning、plugin delete | fork 既有管理面板导航与 release 产物 | 吸收上游插件管理 UI，与后端 plugin API 对齐 |
| Quota | quota reset、subscription expiry、monthly / weekly window 规则 | fork quota auto-disable 与展示增强 | 吸收上游 quota UI，保留 fork 自动禁用与批量检查相关展示 |
| Ampcode | 上游移除 Ampcode 集成与相关配置 | fork 当前仍有 Ampcode 类型、文案、provider 分类与 README 描述 | 用户已确认跟随上游移除；同步删除前端类型、API client、provider 入口、表单、文案与 README 描述 |
| 发布链路 | Bun / Vite / Node 24 构建链路 | fork release suffix 与 tag-only 发布 | 保留 fork 版本策略，吸收上游构建链路，最终重新发布 `management.html` |
| i18n | 新 provider/quota/logs/plugin 文案 | fork 自定义功能文案 | 合并 locale，补齐 fork 定制 key，避免 UI 缺文案 |

## 前端 merge-tree 冲突全集

- 实测命令：`git merge-tree --name-only --no-messages dev upstream/main | sed '1d'`
- 当前冲突文件数：60
- `.github/workflows/release.yml`
- `README.md`
- `README_CN.md`
- `bun.lock`
- `package.json`
- `src/components/config/VisualConfigEditor.module.scss`
- `src/components/config/VisualConfigEditor.tsx`
- `src/components/layout/MainLayout.tsx`
- `src/components/providers/index.ts`
- `src/components/providers/utils.ts`
- `src/components/quota/quotaConfigs.ts`
- `src/components/ui/Collapsible/Collapsible.tsx`
- `src/components/ui/icons.tsx`
- `src/features/authFiles/components/AuthFileCard.tsx`
- `src/features/authFiles/components/AuthFilesPrefixProxyEditorModal.tsx`
- `src/features/authFiles/constants.ts`
- `src/features/authFiles/hooks/useAuthFilesPrefixProxyEditor.ts`
- `src/features/providers/ProvidersWorkbenchPage.tsx`
- `src/features/providers/adapters.ts`
- `src/features/providers/components/ProviderCategoryList.module.scss`
- `src/features/providers/components/ProviderCategoryList.tsx`
- `src/features/providers/components/ProviderHeaderCard.module.scss`
- `src/features/providers/components/ProviderHeaderCard.tsx`
- `src/features/providers/components/ProviderResourcePanel.module.scss`
- `src/features/providers/components/ProviderResourcePanel.tsx`
- `src/features/providers/components/ProviderResourceTable.module.scss`
- `src/features/providers/components/ProviderResourceTable.tsx`
- `src/features/providers/components/providerStatusBar.module.scss`
- `src/features/providers/descriptors.ts`
- `src/features/providers/sheets/ProviderSheet.tsx`
- `src/features/providers/sheets/ResourceDetailView.tsx`
- `src/features/providers/sheets/forms/BaseProviderForm.tsx`
- `src/features/providers/sheets/forms/sharedForm.module.scss`
- `src/features/providers/sheets/forms/useConnectivityTest.ts`
- `src/features/providers/sheets/forms/useModelDiscovery.ts`
- `src/features/providers/types.ts`
- `src/features/providers/useProviderWorkbench.ts`
- `src/hooks/useVisualConfig.ts`
- `src/i18n/locales/en.json`
- `src/i18n/locales/ru.json`
- `src/i18n/locales/zh-CN.json`
- `src/i18n/locales/zh-TW.json`
- `src/pages/AuthFilesPage.module.scss`
- `src/pages/AuthFilesPage.tsx`
- `src/pages/LogsPage.module.scss`
- `src/services/api/authFiles.ts`
- `src/services/api/config.ts`
- `src/services/api/index.ts`
- `src/services/api/models.ts`
- `src/services/api/providers.ts`
- `src/services/api/transformers.ts`
- `src/stores/index.ts`
- `src/styles/layout.scss`
- `src/types/authFile.ts`
- `src/types/visualConfig.ts`
- `src/utils/constants.ts`
- `src/utils/format.ts`
- `src/utils/helpers.ts`
- `src/utils/quota/parsers.ts`
- `src/utils/sourceResolver.ts`

## 可直接吸收的上游功能

- 后端：pluginhost / plugin management / plugin store、host model callback、interceptor skip、auto updater 修复、release / Docker 构建改进、home credential forwarding、API key exclusion、OpenAI video、XAI / Codex websocket、websocket transcript compaction、video auth binding。
- 前端：Plugin Store / plugin management、xAI / Grok quota、quota reset / subscription expiry、连接测试、模型发现、UI state persistence、fullscreen / error logs、logs pagination、auth-files 小修、Bun / Vite 构建链路。
- 条件：AMP/Ampcode 已确认跟随上游移除，可作为吸收范围的一部分执行。

## 执行风险

- 普通 merge 可能把 fork 曾经选择性跳过的前端历史提交一并纳入；本次用户已确认按当前建议吸收上游功能，但执行前仍需列出差异并确保不会回退 fork 功能。
- 上游已跨 minor 推进到后端 `upstream/main@907e3493`（`v7.2.7` 后 2 个提交，当前无 tag）/ 前端 `v1.16.7`，计划执行前必须刷新 SHA/tag；若再次漂移，不得继续使用过期计划。
- 后端远端 `origin/main` 已按用户授权同步上游（`0 0`），本地 `main` 也已 fast-forward 到 `origin/main@907e3493`。前端远端 `origin/main` 已同步上游（`0 0`），本地 `main` 已按用户确认直接对齐 `origin/main@b0db1df`。
- 后端 `v7.2.0` 与前端 `v1.16.5` 均涉及 AMP/Ampcode 移除；用户已确认跟随上游移除。v7.2.6/v7.2.7 与 v1.16.7 未引入新的 feat! 级破坏性变更。
- 后端 runtime 冲突集中在 `sdk/cliproxy/auth` 与 `sdk/cliproxy/service.go`，需要先保留行为契约再改代码。
- 前端 `management.html` 若未重新发布，后端管理面板更新器即使指向 fork 仓库，也可能下载到旧构建产物。
- 前端 `bun.lock` 为 add/add 冲突；本机 registry 指向 npmmirror，重生成 lockfile 时若不显式指定官方 registry，会污染 lockfile 并影响 GitHub runner 的 frozen install。
- 前后端执行真正 merge 前必须建立本地 backup 分支或 tag，并把 sha 写入 evidence，避免高冲突同步中失去回滚点。

## 第三方评审处理结论

- 第一至第三轮评审的 2026-06-12 通过结论已被 2026-06-15 独立评审撤回；原因是上游在三天窗口内推进到后端 `v7.2.5` / 前端 `v1.16.6`，原计划基线过期。第五轮于 2026-06-16 fetch 后漂移到 v7.2.7 / v1.16.7；本轮同日再次 fetch 后后端继续漂移到 `907e3493`，按相同刷新流程处理。
- 第四轮阻断意见已采纳：
  - 刷新后端与前端 upstream SHA/tag。
  - 重跑 merge-tree 并将冲突数更新为后端 17、前端 60。
  - 刷新 divergence 计数。
  - 补充前端 `main` 镜像同步 / 确认任务。
  - 读取后端 `v7.1.69..v7.2.5` 与前端 `v1.16.0..v1.16.6` release notes（第四轮历史口径；最新执行口径已扩展至后端 `v7.1.69..v7.2.7` 加 `v7.2.7..upstream/main`，前端 `v1.16.0..v1.16.7`）。
  - 将“upstream SHA/tag 不一致必须先刷新 findings / plan”加入任务 1 停止条件。
  - 更新实施前检查清单为当前 SHA/tag。
- 仍保留的硬门禁：
  - 前端 `bun.lock` 必须使用官方 registry 重生成，不得含 `npmmirror`，且 `bun install --frozen-lockfile` 退出码为 0。
  - 后端发布体系明确为保留 `.github/workflows/release.yaml`、删除 `.goreleaser.yml`。
  - 前后端合并前建立 backup 分支或 tag。
  - fork 回归断言必须是可执行测试或明确的人工回归 evidence。
  - 计划任务 1 必须包含停止条件；若发现任务数量与停止条件数量不一致，不得进入实施。

## 2026-06-22 审核补充

- 本任务目录缺失 `task.md`，但 `progress.md` 曾把 `task.md` 列为文件；已补建 `task.md` 作为当前任务的静态权威入口，live 状态继续以 `progress.md` / `handoff.md` 为准。
- 前端旧任务 `/home/cheng/git-project/Cli-Proxy-API-Management-Center/.agents/tasks/20260527-sync-upstream/` 仍是 predecessor/reference；当前 canonical plan 继续以本任务为准。
- 旧任务 `20260527-sync-upstream` 中 `b25f722` 与 `632be0b` 的 `continue_skip` 决策基于旧基线 `upstream/main@87702bb` 和旧 provider / usage 架构。
- 本任务以 `v1.16.7` 新布局为基底重新保留 fork Usage 页面；因此旧 skip 决策不再禁止当前 Usage 功能落地，但仍可作为“不要普通 merge 间接吸收旧大范围变更”的历史审计证据。
- 当前前端 Usage 新增文件应作为本任务 8/9 的显式范围跟踪：
  - `src/components/usage/`
  - `src/features/authFiles/hooks/useAuthFilesStats.ts`
  - `src/pages/UsagePage.tsx`
  - `src/pages/UsagePage.module.scss`
  - `src/services/api/usage.ts`
  - `src/stores/useUsageStatsStore.ts`
  - `src/types/sourceInfo.ts`
  - `src/types/usage.ts`
  - `src/utils/sourceResolver.ts`
  - `src/utils/usage.ts`
  - `src/utils/usage/`
  - `src/utils/usageIndex.ts`
- Usage 验收需要覆盖：统计卡片、时间范围切换、图表线选择持久化、模型/API 统计、导出/导入、4 语言 i18n、移动端布局，以及后端 `/usage` / `/usage/export` / `/usage/import` 契约一致性。
