# Backend Runtime / API Resolution Evidence - 2026-06-16 HKT

## Scope

Tasks covered: backend task 5 runtime/API merge resolution.

Freshness before task 5 matched the expected baseline:

- backend `upstream/main = 907e3493ee39`
- backend `origin/main = 907e3493ee39`
- backend `dev = f52451d8ac42`
- backend `origin/main...upstream/main = 0 0`
- backend `dev...upstream/main --cherry-pick = 90 194`
- backend merge-tree unique conflicts = `17`

## Resolution Summary

- `internal/api/handlers/management/handler.go`: merged fork usage statistics and auth-file batch-check state with upstream plugin host / plugin store management fields and reload hooks.
- `internal/api/server_test.go`: kept fork management panel GET/HEAD and usage queue tests, added upstream plugin support header and plugin host injection tests.
- `sdk/cliproxy/auth/conductor.go`: preserved fork scoped-pool availability filtering while integrating upstream plugin scheduler fallback and refresh unschedule behavior.
- `sdk/cliproxy/auth/scheduler.go`: preserved scoped-pool predicates in single-provider and mixed-provider scheduler paths while keeping upstream scheduler strategy behavior.
- `sdk/cliproxy/auth/persist_policy_test.go`: kept fork async persistence / skip-persist tests and added upstream config API key persistence skip test.
- `sdk/cliproxy/builder.go`: integrated upstream plugin host wiring and config reload hook while preserving fork quota checker and post-auth sync behavior.
- `sdk/cliproxy/service.go`: kept fork usage persistence config / shutdown persistence, integrated upstream plugin runtime sync, config API key auth registration, and plugin host shutdown cleanup.
- `sdk/cliproxy/service_stale_state_test.go`: updated delete -> re-add stale state regression to match upstream removal semantics.
- `internal/watcher/watcher_test.go`: fixed one post-merge test compile error by updating `snapshotCoreAuthsFunc` test double to the upstream signature with `synthesizer.PluginAuthParser`.

## Verification

- `git diff --name-only --diff-filter=U`: empty.
- `rg -n '^<<<<<<<|^=======|^>>>>>>>'`: empty.
- `git diff --cached --check`: exit `0`.
- `docker run ... --entrypoint gofmt cliproxyapi-upstream-merge-builder -w <resolved go files>`: exit `0`.
- `docker run ... --entrypoint go cliproxyapi-upstream-merge-builder test ./sdk/cliproxy/... ./internal/api/...`: exit `0`.
  - covered `sdk/cliproxy`, `sdk/cliproxy/auth`, `internal/api`, `internal/api/handlers/management`, and middleware packages.
- First `docker run ... go test ./...`: exit `1`.
  - failing package: `internal/watcher`
  - error: test double for `snapshotCoreAuthsFunc` used old two-argument signature.
- `docker run ... --entrypoint go cliproxyapi-upstream-merge-builder test ./internal/watcher`: exit `0` after the signature fix.

## Fork Regression Coverage

The task 5 test run includes the fork regression test locations required by the plan:

- `sdk/cliproxy/auth/scoped_pool_test.go`
- `sdk/cliproxy/auth/quota_check_async_test.go`
- `internal/usage/persistence_test.go`
- `sdk/cliproxy/service_usage_persistence_test.go`
- `internal/api/handlers/management/routing_scoped_pool_test.go`

These files exist in the merged tree and are exercised by full `go test ./...` in task 6 evidence.

## Result

Task 5 runtime/API conflict resolution is verified after the watcher test signature fix. No push, commit, master merge, release, or credential write was performed.
