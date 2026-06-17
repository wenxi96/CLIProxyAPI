# Handoff

## Current State

本任务处于 `task_6_verified_v7.2.9_checkpoint_waiting_user_confirmation`。用户确认进入前端任务 7 后，任务开始前 FRESHNESS 发现后端 `upstream/main` 已从 `907e3493` 推进到 `2884a67e` / `v7.2.9`，触发防漂移停止条件；用户随后同意同步后端 `origin/main` 并继续吸收新增 4 个提交。当前后端已刷新吸收到 `2884a67e` / `v7.2.9` 并重新通过任务 4/5/6 验证，前端任务 7 尚未开始。

2026-06-12 第一至第三轮评审的"通过"结论已被 2026-06-15 第四轮独立评审撤回并 superseded。第四轮之后 plan 刷新到 v7.2.5/v1.16.6；第五轮刷新到 v7.2.7/v1.16.7；2026-06-16 任务 6 checkpoint 后又刷新到后端 `2884a67e` / `v7.2.9`、前端 `b0db1df` / `v1.16.7`。

当前有效计划目标（已按本次漂移刷新）：

- 后端 `upstream/main@2884a67e` / `v7.2.9`
- 前端 `upstream/main@b0db1df` / `v1.16.7`

当前 main 镜像状态：

- 后端 `origin/main...upstream/main = 0 0`，远端 `main` 已按用户授权同步到 `upstream/main@2884a67e`。
- 后端本地 `main...origin/main = 0 0`，本地 `main` 已与 `origin/main@2884a67e` 对齐。
- 前端 `origin/main...upstream/main = 0 0`，远端 `main` 已同步上游。
- 前端本地 `main...origin/main = 0 0`，已按用户确认直接移除 4 个本地治理文档提交并对齐 `origin/main@b0db1df`；未创建备份。

任务 1 freshness checkpoint（2026-06-16 17:22 HKT）已通过：

- 后端 freshness: `upstream/main=907e3493ee39`、`origin/main=907e3493ee39`、`dev=f52451d8ac42`、`origin/main...upstream/main=0 0`、`dev...upstream/main=90 194`、merge-tree unique conflicts `17`。
- 前端 freshness: `upstream/main=b0db1dfd5da5`、`origin/main=b0db1dfd5da5`、`dev=a02ebbcbf695`、`origin/main...upstream/main=0 0`、`dev...upstream/main=58 153`、merge-tree unique conflicts `60`。
- 结论：未发现上游漂移。任务 1 checkpoint 后用户已确认继续，后端任务 4/5/6 已执行完成。

后端任务 4/5/6 checkpoint（2026-06-16 20:53 HKT）：

- 创建 backup anchor：`backup/pre-merge-2026-06-16-f52451d8 = f52451d8ac42`。
- 已执行 `git merge --no-edit upstream/main` 到后端 `dev`，后端 17 个冲突文件均已解决并 staged；当前未提交。
- AMP/Ampcode 按用户确认跟随上游移除，仅保留 `internal/config/config.go: removeMapKey(root, "ampcode")` 迁移清理。
- Fork 默认面板源仍为 `https://github.com/wenxi96/Cli-Proxy-API-Management-Center`。
- `.goreleaser.yml` 已删除，`.github/workflows/release.yaml` 不再引用 `goreleaser`。
- 合并中修复了 `internal/watcher/watcher_test.go` 的上游签名变化导致的测试替身编译错误。
- 本地 PATH 无 `go` / `gofmt`，验证使用 Docker builder `go1.26.4 linux/amd64` 完成。
- 通过：container `gofmt`、`go test ./internal/managementasset ./cmd/server ./internal/watcher/diff`、`go test ./sdk/cliproxy/... ./internal/api/...`、`go test ./internal/watcher`、`go test ./...`、`go build -o test-output ./cmd/server && rm test-output`、`git diff --cached --check`。
- 未执行 push、commit、master merge、release、asset upload、credential/token 写入。
- `stash@{0}: pre-upstream-merge-local-governance` 仍保留；其 `.gitignore` / `.agents/README.md` 内容已恢复到工作区。

进入前端任务 7 前漂移停止（2026-06-16 HKT）：

- 后端漂移前预期：`upstream/main=907e3493ee39`、`origin/main=907e3493ee39`、`dev=f52451d8ac42`、`origin/main...upstream/main=0 0`、`dev...upstream --cherry-pick=90 194`、merge-tree unique conflicts `17`。
- 后端漂移后实测：`upstream/main=2884a67ed02a`、`origin/main=907e3493ee39`、`dev=f52451d8ac42`、`origin/main...upstream/main=0 4`、`dev...upstream --cherry-pick=90 198`、merge-tree unique conflicts `17`、`git tag --points-at upstream/main = v7.2.9`。
- 新增后端提交：`9f940f16 fix(pluginhost): keep stream callbacks alive until stream close`、`87132e54 feat(plugin): add ModelRouter before auth with single-slot routing targets (#3865)`、`f63cf982 docs: add CatAPI sponsorship details to README files`、`2884a67e feat(videos): add support for video_url extraction and validation in handlers`。
- 前端仍匹配预期：`upstream/main=origin/main=b0db1dfd5da5`、`dev=a02ebbcbf695`、`origin/main...upstream=0 0`、`dev...upstream --cherry-pick=58 153`、merge-tree unique conflicts `60`。
- 按防漂移规则，已停止所有写 / merge 动作，仅刷新 plan 目标行与 findings；未进入前端 merge。

后端 v7.2.9 漂移处置（2026-06-16 HKT）：

- 用户同意后端 `origin/main` 再次同步与继续吸收新增 4 个提交。
- 第一次 `git push origin upstream/main:main` 在写入前因连接关闭失败；第二次成功：`907e3493..2884a67e upstream/main -> main`。
- 本地 `main` 已 fast-forward 到 `origin/main@2884a67e`。
- 在当前 staged 后端候选上应用 `907e3493..2884a67e` 增量补丁，新增 ModelRouter、pluginhost stream callback 生命周期、CatAPI 文档与 `video_url` 处理相关改动。
- `.git/MERGE_HEAD` 已从 `907e3493ee391138ce31c045df2ecfc9b8311c6d` 校正为 `2884a67ed02a9c0989b3b3db42a0d07684fd466f`，避免未来 merge commit 使用旧上游父提交。
- 重新验证通过：container `gofmt`、`go test ./internal/managementasset ./cmd/server ./internal/watcher/diff`、`go test ./sdk/cliproxy/... ./internal/api/...`、`go test ./...`、`go build -o test-output ./cmd/server && rm test-output`、`docker build --progress=plain -t cliproxyapi-upstream-merge-verify .`。
- 最终 FRESHNESS：后端 `upstream/main=origin/main=2884a67ed02a`，`dev=f52451d8ac42`，`origin/main...upstream=0 0`，`dev...upstream --cherry-pick=90 198`，merge-tree unique conflicts `17`；前端仍为 `upstream/main=origin/main=b0db1dfd5da5`，`dev=a02ebbcbf695`，`origin/main...upstream=0 0`，`dev...upstream --cherry-pick=58 153`，merge-tree unique conflicts `60`。
- 当前仍 staged 未提交；未 push dev、未合入 master、未 release、未写凭证。

AMP/Ampcode 决策：用户已确认跟随上游移除，不另起兼容保留设计。实施任务 4/8 时应同步删除后端 Amp 模块/API/配置和前端 Ampcode 类型/API client/Provider Workbench 入口/表单/i18n/README，避免残余 UI 调用已删除后端 API。

## Completed Scope

- 已确认后端与前端分支模型：`main` 镜像上游，`dev` 吸收验证，`master` 稳定线。
- 已创建本任务目录，并记录事实与计划入口。
- 已写入并修订唯一 canonical implementation plan：`plans/2026-06-12-sync-upstream-v7-fork-customizations-implementation-plan.md`。
- 已刷新后端事实：`origin/main...upstream/main = 0 0`、`main...origin/main = 0 0`、`dev...upstream/main = 90 194`、`master...upstream/main = 89 194`、merge-tree 冲突 17 个（17 lines / 17 unique，sort -u 计数口径已固定）。
- 已刷新前端事实：`origin/main...upstream/main = 0 0`、`dev...upstream/main = 58 153`、`master...upstream/main = 57 153`、merge-tree 冲突 60 个（60 lines / 60 unique）；`dev@a02ebbc` 仍需随 `dev -> master` 流动。
- 已读取后端 `v7.1.69..v7.2.7` 与前端 `v1.16.0..v1.16.7` release notes，识别 v7.2.0 与 v1.16.5 为唯一明确破坏性变更（AMP/Ampcode 移除）；v7.2.6/v7.2.7 与 v1.16.7 涉及日志 cursor、插件源、tool_result 标准化与 logs 分页，无新增 feat! 级破坏性变更。
- 已补充后端 `v7.2.7..upstream/main` 两个新增提交：Claude cloak 全局开关与 credential fallback 改进需要进入后端配置 / executor 吸收范围；VisionCoder README 更新可直接吸收。
- 已新增前端 `main` 镜像同步 / 确认任务（plan 任务 3）。
- 已保留前端 `bun.lock` 官方 registry、`npmmirror` 检查和 `bun install --frozen-lockfile` 三道硬门禁。
- 已保留后端发布体系决策：保留 `.github/workflows/release.yaml`、删除 `.goreleaser.yml`。
- 已规范化合并前 backup 分支 / tag 命名：`backup/pre-merge-<YYYY-MM-DD>-<short-sha>`（不再依赖易漂移的 vX.Y.Z）。
- 已固定冲突计数口径：plan 任务 1 同时记录原始行数与 `sort -u` 唯一路径数。
- 已明确 plan 任务 2/3 验证段目标分支（后端/前端），防止误用证据。
- 已记录第四轮与第五轮独立评审处置：`evidence/third-party-review-round4-disposition-2026-06-15.md` 与本轮新 evidence（待创建）。
- 已完成后端任务 4/5/6 合并验证 evidence：`evidence/backend-release-config-resolution.md`、`evidence/backend-runtime-resolution.md`、`evidence/backend-verification.md`。

## Verification

- 两仓已执行 `git fetch upstream --tags --prune` 和 `git fetch origin --tags --prune`（本轮 2026-06-16）。
- 后端 `git tag --points-at upstream/main` 当前返回 `v7.2.9`；前端返回 `v1.16.7`。
- 后端 merge-tree 冲突数 17（17 lines / 17 unique）；前端 merge-tree 冲突数 60（60 lines / 60 unique）。
- 计划结构检查发现旧计划 11 个任务但仅 10 个 `停止条件`，任务 1 缺少停止条件；本轮已将其作为必须修复项纳入 canonical plan。
- 当前 authority 文件中的历史基线只允许作为评审历史出现，不得作为执行目标；执行目标以后端 `2884a67e` / 前端 `b0db1df` 为准。
- 后端工作区处于 merge resolved/staged 状态；后端任务 4/5/6 验证已通过，但未提交。`.gitignore` 的既有 `.codegraph/` 忽略规则已恢复，未回退用户意图。
- 前端工作区仍干净。

## Remaining Work

- 等待用户确认进入前端任务 7；每个任务开始前以及任何 merge 到 dev 之前仍必须再次 fetch 并核对 SHA/tag，若上游继续推进，先刷新 findings / plan。
- 推送 `main`、合入 `master`、触发 release、上传 `management.html` 或任何真实发布动作都需要用户单独明确授权。
