# Review Fixes Evidence - 2026-06-17 HKT

## Scope

处理本次独立评审提出的两个后端阻断点：

1. `.github/workflows/rebuild-release-history.yml` 在 `.goreleaser.yml` 已删除后仍只支持 GoReleaser 构建。
2. 最新 `.agents` master 验证记录只存在于 `master`，未同步到 `dev`。

## Disposition

- RFX-1: accepted. `rebuild-release-history.yml` 保留 GoReleaser 作为 legacy rebuild entries 的路径；当 rebuild worktree 中不存在 `.goreleaser.yml` 时，改用直接 `go build` fallback，生成 `dist/CLIProxyAPI_<version>_linux_amd64.tar.gz`、`dist/CLIProxyAPI_<version>_linux_amd64_no-plugin.tar.gz` 和 `dist/checksums.txt`。
- RFX-2: accepted. 将 `master` 上的最新 master 验证记录 cherry-pick 回 `dev`，并从 `dev` 合回本地 `master`，避免 `.agents` 记录只存在于 `master`。

## Branch Actions

- `git switch dev`
- `git cherry-pick b94dd9cf8625`
  - result: `cf2a23f1 docs: record upstream sync master verification`
- `git commit -m "fix(ci): support release history rebuild without goreleaser config"`
  - result: `7a0fd347 fix(ci): support release history rebuild without goreleaser config`
- `git switch master`
- `git merge --no-ff dev -m "merge dev review fixes"`
  - result: `a854bde5 merge dev review fixes`
- `git diff --name-status dev..master`
  - result: empty at the code/doc sync checkpoint before this evidence entry

## Verification

- `python3` + `yaml.safe_load(.github/workflows/rebuild-release-history.yml)` exit 0.
- Extracted `Rebuild release history` run block via YAML parser and ran `bash -n /tmp/rebuild-release-history-run.sh` exit 0.
- `git diff --check` exit 0.
- `git diff --name-status dev..master` empty after merging the workflow fix and prior master verification docs.

## Boundaries

- No `git push`.
- No tag creation or push.
- No GitHub release trigger.
- No `management.html` upload.
- No credentials or private configuration written.
