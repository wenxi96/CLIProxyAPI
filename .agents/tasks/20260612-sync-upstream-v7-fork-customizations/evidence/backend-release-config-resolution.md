# Backend Release / Config Resolution Evidence - 2026-06-16 HKT

## Scope

Tasks covered: backend task 4 release/config/management asset merge resolution.

Backup anchor:

- `backup/pre-merge-2026-06-16-f52451d8 = f52451d8ac42`

Freshness before merge matched the expected baseline:

- backend `upstream/main = 907e3493ee39`
- backend `origin/main = 907e3493ee39`
- backend `dev = f52451d8ac42`
- backend `origin/main...upstream/main = 0 0`
- backend `dev...upstream/main --cherry-pick = 90 194`
- backend merge-tree unique conflicts = `17`

## Resolution Summary

- `.github/workflows/release.yaml`: adopted upstream non-GoReleaser release workflow as the base, kept fork `v*` tag-only trigger, and preserved fork source repository ldflag wiring with `SOURCE_REPOSITORY=https://github.com/${GITHUB_REPOSITORY}`.
- `.goreleaser.yml`: deleted, matching upstream single-workflow release model.
- `Dockerfile`: adopted Go `1.26-bookworm` / Debian bookworm path and preserved `SourceRepository` ldflag.
- `cmd/server/main_test.go`: kept upstream example API key warning coverage and restored fork home runtime defaults coverage.
- `internal/config/config.go`: preserved fork `DefaultPanelGitHubRepository = https://github.com/wenxi96/Cli-Proxy-API-Management-Center`, scoped-pool config, quota auto-disable threshold, usage persistence interval; added upstream plugin / Claude cloak config; AMP follows upstream removal.
- `internal/managementasset/updater.go` and `_test.go`: preserved fork retry/custom repo behavior and added upstream auto-update skip reason coverage.
- `internal/tui/config_tab.go`: removed AMP config section and preserved fork helper logic.
- `internal/watcher/diff/config_diff_test.go`: removed AmpCode expectations and preserved quota auto-disable threshold assertions.

## Static Checks

- `git diff --name-only --diff-filter=U`: empty.
- `rg -n '^<<<<<<<|^=======|^>>>>>>>'`: empty.
- `git diff --cached --check`: exit `0`.
- `rg -n 'wenxi96/Cli-Proxy-API-Management-Center' internal/config/config.go internal/managementasset/updater.go internal/managementasset/updater_test.go`: found fork repository in `internal/config/config.go` and management asset tests.
- `rg -n 'goreleaser' .github/workflows/release.yaml .goreleaser.yml`: no matches; `.goreleaser.yml` is deleted.
- `rg -n 'AmpCode|ampcode|Ampcode|AMP' internal config.example.yaml cmd sdk test`: only unrelated `API_RESPONSE_TIMESTAMP` plus `internal/config/config.go:2151 removeMapKey(root, "ampcode")` migration cleanup.

## Verification

Local host did not have `go` / `gofmt` in PATH:

- `go version`: exit `127`, `/bin/sh: 1: go: not found`
- `gofmt ...`: exit `127`, `/bin/sh: 1: gofmt: not found`

Containerized Go toolchain was used instead:

- `docker build --progress=plain -t cliproxyapi-upstream-merge-verify .`
  - first attempt: exit `100`, Debian apt mirror returned `502 Bad Gateway` for `libdpkg-perl`.
  - retry: exit `0`; Docker image built successfully.
- `docker build --progress=plain --target builder -t cliproxyapi-upstream-merge-builder .`: exit `0`.
- `docker run --rm --entrypoint go cliproxyapi-upstream-merge-builder version`: `go version go1.26.4 linux/amd64`.
- `docker run ... --entrypoint gofmt cliproxyapi-upstream-merge-builder -w <resolved go files>`: exit `0`.
- `docker run ... --entrypoint go cliproxyapi-upstream-merge-builder test ./internal/managementasset ./cmd/server ./internal/watcher/diff`: exit `0`.

## Result

Task 4 release/config/asset conflict resolution is verified. No push, commit, master merge, release, or credential write was performed.
