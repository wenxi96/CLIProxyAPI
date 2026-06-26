# Handoff

## Current State

本任务处于 `fork_custom_inventory_complete_backend_v7_2_29_merge_candidate_conflicts_resolved_compile_deferred`。

2026-06-23 fresh fetch 后的当前事实：

- 后端当前 `dev@b8ee828c6e0b` 静态保留 fork 自定义功能；最新 `upstream/main@bd646819ed95` / `v7.2.29` 已通过 `git merge --no-commit --no-ff upstream/main` 应用到本地未提交 merge 候选，`MERGE_HEAD=bd646819ed95`。
- 后端本地 merge 候选的两个冲突文件 `cmd/server/main.go`、`sdk/cliproxy/service.go` 已解决并 staged；无未合并文件、无冲突标记、`git diff --check` clean。
- 因 merge 尚未 commit，`HEAD` 仍是 `b8ee828c6e0b`，所以 `git merge-base --is-ancestor upstream/main HEAD` exit `1` 与 `HEAD...upstream/main --cherry-pick = 107 9` 仍反映提交图状态，不代表 staged merge 候选未包含上游。
- 后端 `origin/main@1f2504ebcc30` 也落后最新上游 6 个提交：`origin/main...upstream/main = 0 6`。
- 前端当前 `dev@b60462dc1d33` 已包含最新 fetched `upstream/main@ed4124ff3b24` / `v1.17.1`；`origin/main == upstream/main`，`dev...upstream/main --cherry-pick = 65 0`。
- 后端 / 前端自定义功能静态清单已写入：
  - 后端：`evidence/fork-custom-feature-inventory-2026-06-23.md`
  - 前端：`/home/cheng/git-project/Cli-Proxy-API-Management-Center/.agents/tasks/20260612-sync-upstream-v7-fork-customizations/evidence/fork-custom-feature-inventory-2026-06-23.md`

此前 `v7.2.12` / `v1.16.7` master 验证记录仍是历史 evidence，不再代表 2026-06-23 fetch 后的最新上游状态。

已完成过本地 `dev -> master` 合入与 master 后自动化验证，但该结论在最新后端上游漂移后需要重新收口：

- 后端本地 `dev` / `master` 曾包含本轮上游合并与 review-fix 变更；推送前必须重新执行 FRESHNESS，并先处理后端 `v7.2.29` 漂移。
- 前端当前 dev 已吸收到 `v1.17.1` 并保留 fork 自定义功能。
- 后端 / 前端 `main` 镜像状态需要在下一轮执行前再次确认；当前后端 `origin/main` 已落后 latest upstream。
- 后端 backup anchor：`backup/pre-merge-2026-06-17-c9fa502d = c9fa502d85b8`。
- 前端 backup anchor：`backup/pre-merge-2026-06-17-c54efc0e = c54efc0e1ffc`。

2026-06-17 独立评审发现项已本地处理：

- 后端 `.github/workflows/rebuild-release-history.yml` 已支持无 `.goreleaser.yml` 的 rebuild fallback；旧 rebuild entries 仍可继续使用 GoReleaser。
- release-history fallback 已补全为与主 release workflow 同名的 10 个 archive 资产，并增加 archive 数量检查；Evidence：`evidence/release-history-fallback-assets-2026-06-17.md`。
- `master` 上最新 `.agents` master 验证记录已同步回 `dev`，并从 `dev` 合回本地 `master`。
- Evidence：`evidence/review-fixes-2026-06-17.md`。

2026-06-22 审核修正已补入当前任务记录：

- 新增 `task.md` 作为静态任务权威入口，live 状态继续以 `progress.md` / `handoff.md` 为准。
- 前端项目内新增 canonical plan 引用任务，避免只看前端 `.agents` 时误用旧 `20260527-sync-upstream` 任务。
- `findings.md` 已补充 Usage 新文件范围、Usage 验收项，以及旧 `b25f722` / `632be0b` skip 决策与当前 `v1.16.7` Usage 保留方案之间的关系。
- 后端 quota auto-disable 已补回旧 `on-zero-quota` 管理 API 路由兼容，并记录新旧命名策略。
- quota auto-disable 主配置键已确认为 `quota-exceeded.auto-disable-auth-file-on-low-quota`；旧 `auto-disable-auth-file-on-zero-quota` 仅作为兼容配置输入和旧管理 API 路由保留，保存配置时收敛到新 key；前端 VisualConfigEditor / transformer 与该契约一致。

2026-06-23 自定义功能清单补充：

- 已按合并前基线 `backup/pre-merge-2026-06-16-f52451d8` / `backup/pre-merge-2026-06-16-a02ebbc` 对比当前代码，逐项记录后端与前端 fork 自定义功能保留状态。
- 清单覆盖默认面板源、DisplayName、Auth Files 批量检查、ZIP 下载、Auth Files 自定义筛选/简略模式、Scoped Pool、低额度自动禁用命名、Usage 页面/持久化、release 资产链路、AMP/Ampcode 移除。
- 两份清单已补齐 `Baseline Reference Method` 和 `Upstream Absorption Static Checklist`，明确区分 fork 自定义保留与上游新增能力吸收。后端上游吸收静态项包括 pluginhost/pluginstore/homeplugins、home plugin sync、plugin runtime sync、API key usage、OAuth excluded models、video routes、websocket support、error logs、Claude cloak 等；前端上游吸收静态项包括 plugin pages/store、Logs fullscreen/error logs、OAuth excluded UI、xAI/Grok OAuth/quota、Codex websocket controls、Bun/Node 24 release/rebuild workflow 等。
- 两份清单已补齐 `Baseline Extraction Evidence`，记录 baseline refs 中实际存在的功能文件、符号扫描 anchors 与当前 quick symbol counts。后端 baseline 证据来自 `backup/pre-merge-2026-06-16-f52451d8` 的 `AGENTS.md` / `CLAUDE.md`、Amp、batch-check、download-archive、scoped-pool、usage persistence、release / installer 等文件；前端 baseline 证据来自 `backup/pre-merge-2026-06-16-a02ebbc` 的 Usage、batch check、auth-file data hook、uiState、ScopedPool badge、VisualConfigEditor、Usage API/types、Ampcode、release workflows 等文件。
- 结论是当前代码静态保留自定义功能；前端最新上游已包含；后端最新上游已进入未提交 merge 候选且冲突已解决，但编译验证按用户要求暂缓，不能进入最终提交 / 推送 / 发布收口。

未执行：

- 未 push `dev`。
- 未 push `master`。
- 未创建或推送 tag。
- 未触发 GitHub release。
- 未上传 `management.html`。
- 未写入凭证、token 或私密配置。

## Completed Scope

- 后端任务 4/5/6 已完成并刷新验证到 `v7.2.15`。
- 前端任务 7/8/9 已完成并刷新验证到 `v1.16.10`（本地 `dev` / `master`），同时保留前端 `origin/main` 落后 upstream 的远端镜像差异待授权同步。
- 任务 10 management panel 本地链路验证已完成；下一前端 release 目标 tag 记录为 `v1.16.7-wx-2.7`，线上 latest release 仍是旧面板，真实发布仍需授权。
- 任务 11 自动化联合验证已完成。
- 用户确认 AMP/Ampcode 跟随上游移除，后端模块/API/测试与前端类型/API/provider/i18n/README 已按移除路径处理。
- 最新收口项：后端修复 `xai_executor` 对不支持 reasoning 的模型残留空 `reasoning:{}` 的回归；前端修复 `rebuild-release-history.yml` 以兼容 bun/npm 历史提交，并清理未使用的 `chart.js` / `react-chartjs-2` 依赖。
- Fork 定制保留：后端默认面板源、scoped pool、quota auto-disable、usage persistence、plugin callback 非递归相关测试；前端 DisplayName、Scoped Pool / Scoped Poll、Auth Files 批量检查、ZIP 下载、fork tag-only release、`a02ebbc` lockfile 修复。

## Verification

最新 master 后验证：

- 后端：在 `cliproxyapi-upstream-merge-builder` 容器中执行 `go test -run TestXAIExecutorOmitsUnsupportedReasoningEffort ./internal/runtime/executor` exit 0；随后执行 `go test ./...` exit 0；`go build -o test-output ./cmd/server && rm test-output` exit 0。
- 前端：`.github/workflows/rebuild-release-history.yml` 与 `.github/workflows/release.yml` YAML parse exit 0；提取 rebuild script 后 `bash -n` exit 0；`/home/cheng/.bun/bin/bun install --frozen-lockfile` exit 0；`/home/cheng/.bun/bin/bun run build` exit 0。
- 后端 / 前端 unmerged file 检查为空，conflict marker 检查为空。
- Evidence：`evidence/master-merge-verification.md`。

本轮 review-fix 验证：

- `python3` + `yaml.safe_load(.github/workflows/rebuild-release-history.yml)` exit 0。
- 从 YAML 解析出 `Rebuild release history` run block 后执行 `bash -n /tmp/rebuild-release-history-run.sh` exit 0。
- `git diff --check` exit 0。
- `git diff --name-status dev..master` 在 workflow fix 与既有 `.agents` 文档首次合并后为空。
- fallback 资产补全后，在 `cliproxyapi-upstream-merge-builder` 容器中用 Go `1.26.4` 实际执行 fallback 构建，产出 10 个 archive 资产与 `checksums.txt` 后清理 `dist/`，命令 exit 0。

2026-06-23 清单与非编译验证：

- 两仓执行 fresh fetch。
- 后端 `upstream/main=bd646819ed95`、`origin/main=1f2504ebcc30`、`dev=b8ee828c6e0b`、`origin/main...upstream/main=0 6`、`dev...upstream/main --cherry-pick=107 9`、`git merge-base --is-ancestor upstream/main HEAD` exit `1`。
- 前端 `upstream/main=origin/main=ed4124ff3b24`、`dev=b60462dc1d33`、`origin/main...upstream/main=0 0`、`dev...upstream/main --cherry-pick=65 0`、`git merge-base --is-ancestor upstream/main HEAD` exit `0`。
- 基于 targeted `rg` / `git grep <baseline>` 对比已写入两份 inventory evidence。
- 基于 targeted `rg` / `git grep` 的上游吸收路径静态核对已写入两份 inventory evidence。
- 后端最新上游 merge 候选非编译检查：`git diff --name-only --diff-filter=U` 返回空；`rg -n '^<<<<<<<|^=======|^>>>>>>>' cmd/server/main.go sdk/cliproxy/service.go` 无匹配；`git diff --check` 无输出。
- 编译 / 构建 / 测试验证：按用户“暂时不做编译验证”要求，本轮未继续运行 `go test`、`go build`、Docker build、`bun run build` 或 `bun run type-check`。

## Remaining Work

下一步只能在用户再次明确授权或恢复验证后执行：

- 后端：在用户恢复编译验证后，针对当前 merge 候选运行后端验证（至少 `go test ./...` 与 `go build -o test-output ./cmd/server && rm test-output`，或既定 Docker builder 等价命令）；通过后再创建 merge commit。
- 后端：同步 `origin/main` 到 `upstream/main@bd646819ed95` 仍是远端写操作，需要用户单独授权；当前本地 merge 候选不等于已推送远端 main。
- push 后端 `dev` / `master`。
- push 前端 `dev` / `master`。
- 同步前端 `origin/main` 到 `upstream/main@c74fa6d400de`。
- 创建 / 推送 release tag。
- 触发 GitHub release。
- 上传或发布 `management.html`。

继续前必须先再次执行 FRESHNESS。若上游再次漂移，立即停止写 / push / release，刷新 findings / plan 并等待用户决定。

## Notes

- 后端本地仍保留几个历史 stash，其中最新两个是本轮切换到 master 前用于保护 `.agents` 中间状态的本地 stash；当前权威状态以本文件、`progress.md` 和 `evidence/master-merge-verification.md` 为准。
- 第一至第三轮 2026-06-12 评审结论已 superseded；当前 latest fetched execution target 以后端 `bd646819ed95` / `v7.2.29` 与前端 `ed4124ff3b24` / `v1.17.1` 为准。此前 `8d2c00c107b2` / `b0db1dfd5da5` 仅是历史验证目标。
