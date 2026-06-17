# Backend Verification Evidence - 2026-06-16 HKT

## Scope

Task covered: backend task 6 integration verification.

2026-06-16 continuation note: after the task 6 checkpoint, a new FRESHNESS run detected backend upstream drift from `907e3493ee39` to `2884a67ed02a` / `v7.2.9`. User authorized syncing backend `origin/main` again and continuing the backend absorption. This file now records the refreshed `v7.2.9` verification.

Freshness before task 6 matched the expected baseline:

- backend `upstream/main = 2884a67ed02a`
- backend `origin/main = 2884a67ed02a`
- backend `dev = f52451d8ac42`
- backend `origin/main...upstream/main = 0 0`
- backend `dev...upstream/main --cherry-pick = 90 198`
- backend merge-tree unique conflicts = `17`
- frontend cross-check also matched baseline: `upstream/main = origin/main = b0db1dfd5da5`, `dev = a02ebbcbf695`, `dev...upstream/main --cherry-pick = 58 153`, merge-tree unique conflicts `60`.

## Toolchain

Local host did not expose Go in PATH:

- `go version`: exit `127`, `/bin/sh: 1: go: not found`
- local `go test` and `go build` commands therefore returned exit `127`.

Verification was completed with a Docker builder image:

- `docker build --progress=plain -t cliproxyapi-upstream-merge-verify .`
  - first attempt: exit `100` due to Debian apt mirror `502 Bad Gateway` while fetching `libdpkg-perl`.
  - retry: exit `0`; image built successfully, including `go build ./cmd/server`.
- `docker build --progress=plain --target builder -t cliproxyapi-upstream-merge-builder .`: exit `0`.
- `docker run --rm --entrypoint go cliproxyapi-upstream-merge-builder version`: `go version go1.26.4 linux/amd64`.

## Commands And Results

- `docker run ... --entrypoint gofmt cliproxyapi-upstream-merge-builder -w <resolved go files>`: exit `0`.
- `docker run ... --entrypoint go cliproxyapi-upstream-merge-builder test ./internal/managementasset ./cmd/server ./internal/watcher/diff`: exit `0`.
- `docker run ... --entrypoint go cliproxyapi-upstream-merge-builder test ./sdk/cliproxy/... ./internal/api/...`: exit `0`.
- `docker run ... --entrypoint go cliproxyapi-upstream-merge-builder test ./internal/watcher`: exit `0` after updating the watcher test double signature.
- `docker run ... --entrypoint go cliproxyapi-upstream-merge-builder test ./...`: exit `0`.
- `docker run ... --entrypoint sh cliproxyapi-upstream-merge-builder -c 'go build -o test-output ./cmd/server && rm test-output'`: exit `0`.
- `docker build --progress=plain -t cliproxyapi-upstream-merge-verify .`: exit `0` against the refreshed `v7.2.9` candidate.
- `git diff --cached --check`: exit `0`.
- `git diff --name-only --diff-filter=U`: empty.
- `rg -n '^<<<<<<<|^=======|^>>>>>>>'`: empty.

Additional `v7.2.9` rerun after applying `907e3493..2884a67e`:

- `git push origin upstream/main:main`: first attempt failed before writing with `Connection to github.com closed by remote host`; second attempt succeeded as `907e3493..2884a67e upstream/main -> main`.
- local `main` was fast-forwarded to `origin/main`; `origin/main = main = upstream/main = 2884a67ed02a`; `origin/main...upstream/main = 0 0`; `main...origin/main = 0 0`.
- `git diff --binary --full-index 907e3493ee391138ce31c045df2ecfc9b8311c6d..2884a67ed02a | git apply --check --index`: exit `0`.
- `git diff --binary --full-index 907e3493ee391138ce31c045df2ecfc9b8311c6d..2884a67ed02a | git apply --index`: exit `0`.
- `.git/MERGE_HEAD` was corrected from `907e3493ee391138ce31c045df2ecfc9b8311c6d` to `2884a67ed02a9c0989b3b3db42a0d07684fd466f` so a future merge commit has the current upstream parent.
- `docker run ... gofmt -w` over staged existing Go files: exit `0`.
- `docker run ... go test ./internal/managementasset ./cmd/server ./internal/watcher/diff`: exit `0`.
- `docker run ... go test ./sdk/cliproxy/... ./internal/api/...`: exit `0`.
- `docker run ... go test ./...`: exit `0`.
- `docker run ... sh -c 'go build -o test-output ./cmd/server && rm test-output'`: exit `0`.
- `docker build --progress=plain -t cliproxyapi-upstream-merge-verify .`: exit `0`.

## Behavioral Gates

- Fork panel repository preserved: `internal/config/config.go` still contains `https://github.com/wenxi96/Cli-Proxy-API-Management-Center`.
- GoReleaser removed from the active release workflow: `rg -n 'goreleaser' .github/workflows/release.yaml .goreleaser.yml` returned no matches; `.goreleaser.yml` is deleted.
- AMP/Ampcode follows upstream removal: repository search only found unrelated `API_RESPONSE_TIMESTAMP` strings and `removeMapKey(root, "ampcode")` migration cleanup in `internal/config/config.go`.
- Fork regression tests exist and are covered by `go test ./...`:
  - `sdk/cliproxy/auth/scoped_pool_test.go`
  - `sdk/cliproxy/auth/quota_check_async_test.go`
  - `internal/usage/persistence_test.go`
  - `sdk/cliproxy/service_usage_persistence_test.go`
  - `internal/api/handlers/management/routing_scoped_pool_test.go`

## Working Tree / Authorization

- Backend merge is resolved and staged on `dev`, but not committed.
- No push, force push, master merge, release, asset upload, credential write, or token write was performed.
- Stash `stash@{0}: pre-upstream-merge-local-governance` is still retained as a backup; its `.gitignore` / `.agents/README.md` content has been restored to the working tree.

## Result

Backend tasks 4, 5, and 6 are verified against refreshed backend upstream `2884a67ed02a` / `v7.2.9`. This is the checkpoint after refreshed task 6; execution must stop for user confirmation before starting frontend task 7.
