# Handoff

## Current State

本任务处于 `task_11_master_merged_review_fixes_applied_waiting_push_release_authorization`。

已完成本地 `dev -> master` 合入与 master 后自动化验证：

- 后端已吸收到 `upstream/main@8d2c00c107b2` / `v7.2.12`。
- 前端已吸收到 `upstream/main@b0db1dfd5da5` / `v1.16.7`。
- 后端 `origin/main` / 本地 `main` 已同步上游；前端 `origin/main` / 本地 `main` 已同步上游。
- 后端本地 `dev` / `master` 已包含本轮上游合并与 review-fix 变更；推送前必须重新执行 `git rev-parse --short=12 dev master` 与 FRESHNESS。
- 前端本地 `dev = b38985210ce8`，本地 `master = 4d46037b4dce`。
- 后端 backup anchor：`backup/pre-merge-2026-06-17-c9fa502d = c9fa502d85b8`。
- 前端 backup anchor：`backup/pre-merge-2026-06-17-c54efc0e = c54efc0e1ffc`。

2026-06-17 独立评审发现项已本地处理：

- 后端 `.github/workflows/rebuild-release-history.yml` 已支持无 `.goreleaser.yml` 的 rebuild fallback；旧 rebuild entries 仍可继续使用 GoReleaser。
- release-history fallback 已补全为与主 release workflow 同名的 10 个 archive 资产，并增加 archive 数量检查；Evidence：`evidence/release-history-fallback-assets-2026-06-17.md`。
- `master` 上最新 `.agents` master 验证记录已同步回 `dev`，并从 `dev` 合回本地 `master`。
- Evidence：`evidence/review-fixes-2026-06-17.md`。

未执行：

- 未 push `dev`。
- 未 push `master`。
- 未创建或推送 tag。
- 未触发 GitHub release。
- 未上传 `management.html`。
- 未写入凭证、token 或私密配置。

## Completed Scope

- 后端任务 4/5/6 已完成并重新验证到 `v7.2.12`。
- 前端任务 7/8/9 已完成并验证到 `v1.16.7`。
- 任务 10 management panel 本地链路验证已完成；下一前端 release 目标 tag 记录为 `v1.16.7-wx-2.7`，线上 latest release 仍是旧面板，真实发布仍需授权。
- 任务 11 自动化联合验证已完成。
- 用户确认 AMP/Ampcode 跟随上游移除，后端模块/API/测试与前端类型/API/provider/i18n/README 已按移除路径处理。
- Fork 定制保留：后端默认面板源、scoped pool、quota auto-disable、usage persistence、plugin callback 非递归相关测试；前端 DisplayName、Scoped Pool / Scoped Poll、Auth Files 批量检查、ZIP 下载、fork tag-only release、`a02ebbc` lockfile 修复。

## Verification

最新 master 后验证：

- 后端：`go test ./...` exit 0；`go build -o test-output ./cmd/server && rm test-output` exit 0。验证在 Docker builder 中执行，显式设置 `PATH=/usr/local/go/bin:$PATH`。
- 前端：`git merge-base --is-ancestor a02ebbcbf69549b87e81054151eba02d1ade59cb master` exit 0；`bun install --frozen-lockfile` exit 0；`bun run build` exit 0。
- 后端 / 前端 unmerged file 检查为空，conflict marker 检查为空。
- Evidence：`evidence/master-merge-verification.md`。

本轮 review-fix 验证：

- `python3` + `yaml.safe_load(.github/workflows/rebuild-release-history.yml)` exit 0。
- 从 YAML 解析出 `Rebuild release history` run block 后执行 `bash -n /tmp/rebuild-release-history-run.sh` exit 0。
- `git diff --check` exit 0。
- `git diff --name-status dev..master` 在 workflow fix 与既有 `.agents` 文档首次合并后为空。
- fallback 资产补全后，在 `cliproxyapi-upstream-merge-builder` 容器中用 Go `1.26.4` 实际执行 fallback 构建，产出 10 个 archive 资产与 `checksums.txt` 后清理 `dist/`，命令 exit 0。

## Remaining Work

下一步只能在用户再次明确授权后执行：

- push 后端 `dev` / `master`。
- push 前端 `dev` / `master`。
- 创建 / 推送 release tag。
- 触发 GitHub release。
- 上传或发布 `management.html`。

继续前必须先再次执行 FRESHNESS。若上游再次漂移，立即停止写 / push / release，刷新 findings / plan 并等待用户决定。

## Notes

- 后端本地仍保留几个历史 stash，其中最新两个是本轮切换到 master 前用于保护 `.agents` 中间状态的本地 stash；当前权威状态以本文件、`progress.md` 和 `evidence/master-merge-verification.md` 为准。
- 第一至第三轮 2026-06-12 评审结论已 superseded；当前执行目标以后端 `8d2c00c107b2` / 前端 `b0db1dfd5da5` 为准。
