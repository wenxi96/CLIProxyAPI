# Fork Custom Feature Inventory - Backend - 2026-06-23

## Scope

- Repository: `/home/cheng/git-project/CLIProxyAPI`
- Pre-merge fork baseline: `backup/pre-merge-2026-06-16-f52451d8 = f52451d8ac42`
- Current branch at inspection: `dev@b8ee828c6e0b`
- Current merge-candidate state: `git merge --no-commit --no-ff upstream/main` is in progress with `MERGE_HEAD=bd646819ed95`; conflicts in `cmd/server/main.go` and `sdk/cliproxy/service.go` have been resolved and staged, but no merge commit has been created.
- Backup anchor before the local merge candidate: `backup/pre-merge-2026-06-23-b8ee828c = b8ee828c6e0b`
- Fresh fetch result on 2026-06-23 CST:
  - `upstream/main = bd646819ed95`, tag `v7.2.29`
  - `origin/main = 1f2504ebcc30`
  - `origin/main...upstream/main = 0 6`
  - `dev...upstream/main --cherry-pick = 107 9`
  - `git merge-base --is-ancestor upstream/main HEAD` exit `1`
  - current merge-tree conflict count against latest upstream: `2` (`cmd/server/main.go`, `sdk/cliproxy/service.go`)

Conclusion: current backend `dev` preserves the fork custom features listed below. The latest fetched upstream through `bd646819ed95` / `v7.2.29` has been applied to the local uncommitted merge candidate and the two conflicts are resolved. Because the merge is not committed, `HEAD` still remains `b8ee828c6e0b` and graph checks such as `git merge-base --is-ancestor upstream/main HEAD` still fail; that is expected for this uncommitted merge state. Compile/build/test verification is intentionally deferred per user instruction on 2026-06-23, so the current proof level is static code review plus non-compile conflict checks, not final release readiness.

## Baseline Reference Method

This inventory separates fork custom features from upstream features by comparing three sources:

- Fork baseline: `backup/pre-merge-2026-06-16-f52451d8`, the local fork state before the v7 upstream absorption work.
- Current candidate: the current working tree on `dev`, including the uncommitted merge candidate with `MERGE_HEAD=bd646819ed95`.
- Current upstream: `upstream/main@bd646819ed95`, used to distinguish newly absorbed upstream capabilities from fork-authored customizations.

The baseline checks used targeted `git grep <baseline>` and current `rg`/`git grep` over the feature anchors below. A feature is recorded as a fork customization only when it existed in the fork baseline and remains in current code, or when it is an intentional fork adaptation required to keep a baseline customization working after upstream changed the surrounding implementation. Upstream-only features are recorded separately in the static checklist below and are not counted as fork customizations.

### Baseline Extraction Evidence

Mechanical baseline extraction on 2026-06-23 used these checks:

- Baseline governance files present: `.agents`, `AGENTS.md`, `CLAUDE.md`.
- Baseline feature files present:
  - `internal/api/modules/amp`
  - `internal/api/handlers/management/auth_files_batch_check.go`
  - `internal/api/handlers/management/auth_files_download_test.go`
  - `internal/api/handlers/management/routing_scoped_pool.go`
  - `internal/usage/persistence.go`
  - `sdk/cliproxy/service_usage_persistence_test.go`
  - `.goreleaser.yml`
  - `.github/workflows/release.yaml`
  - `install/linux/cliproxyapi-installer.sh`
- Baseline symbol scan anchors included:
  - `wenxi96`, `display_name`, `displayName`
  - `auth-files/batch-check`, `auth-files/download-archive`
  - `scoped-pool`
  - `auto-disable-auth-file-on-zero-quota`
  - `UsageStatisticsEnabled`, `/usage/export`, `/usage/import`
  - `.goreleaser.yml`, `goreleaser`
  - `ampcode`, `internal/api/modules/amp`
- Current quick symbol counts from repository `rg`:
  - `wenxi96/Cli-Proxy-API-Management-Center`: 16
  - `display_name`: 127
  - `auth-files/batch-check`: 17
  - `auth-files/download-archive`: 4
  - `scoped-pool`: 46
  - `auto-disable-auth-file-on-low-quota`: 20
  - `auto-disable-auth-file-on-zero-quota`: 14
  - `UsageStatisticsEnabled`: 64
  - `internal/api/modules/amp`: 0
  - `ampcode`: 1, expected config migration cleanup only

The matrix below is the normalized interpretation of that extraction. It intentionally excludes generic upstream architecture paths and includes only fork-specific or fork-adapted behavior.

## Latest Upstream Delta Applied In Local Merge Candidate

Before the local merge candidate, `git log --oneline --left-right --cherry-pick dev...upstream/main` showed these upstream-side commits. They are now present in the staged merge result, but not yet committed:

- `bd646819` test(translator, runtime): ensure empty text parts are skipped without null values
- `5d9ea166` Merge pull request #3963 from router-for-me/home
- `c58da381` feat(plugins): sync home plugin manifests
- `290f421f` Merge pull request #3959 from fdreamsu/codex/fix-codex-ws-prefix
- `36ed0e5c` fix(codex): strip model prefix for websocket payloads
- `079ec51f` feat(cliproxy): optimize API key alias rebuild with deferred execution and caching
- `1f2504eb` fix(claude): bypass signature sanitizer for non-Claude models (#3946)
- `369e560f` feat(api): refactor provider key logic for API key usage and add test for compatibility grouping
- `babef2a1` feat(cliproxy): add `unregisterOpenAICompatExecutor` and sync runtime configuration

Conflict resolution notes:

- `cmd/server/main.go`: retained fork `applyHomeRuntimeDefaults(parsed, homeCfg)` and added upstream `homeplugins.Sync(ctxHomePlugins, cfg, pluginHost)` after config normalization so home plugin manifests are fetched without dropping fork home runtime defaults.
- `sdk/cliproxy/service.go`: retained fork `s.applyUsagePersistenceConfigChange(...)` on config updates and added upstream `s.syncPluginRuntimeConfig(ctx)` using the runtime config sync path, so usage persistence and plugin runtime reconfiguration both remain active.

## Upstream Absorption Static Checklist

These items are upstream capabilities or latest upstream deltas that should be present after the absorption. They are listed separately from fork custom features to avoid treating upstream additions as fork requirements.

| Upstream capability | Baseline signal | Current static evidence | Status |
|---|---|---|---|
| Dynamic plugin host, plugin store and public SDK wrappers | `backup/pre-merge-2026-06-16-f52451d8` did not contain `internal/pluginhost`, `internal/pluginstore`, `sdk/pluginhost`, `sdk/pluginstore` or `internal/homeplugins` directories | current tree contains those directories; `cmd/server/main.go:153-156,571-573,651,700`, `internal/config/config.go:180-189`, `sdk/cliproxy/builder.go:152-301`, `sdk/pluginhost/host.go`, `sdk/pluginstore/pluginstore.go` | Absorbed |
| Home plugin manifest sync from latest upstream | absent from fork baseline | `cmd/server/main.go:313-317` calls `homeplugins.Sync`; `sdk/cliproxy/home_plugins.go:14` exposes service-level sync | Absorbed in local merge candidate |
| Runtime plugin scheduler/config synchronization | absent from fork baseline | `sdk/cliproxy/service.go:186-192,1621-1622,1962`; `sdk/cliproxy/service_plugin_scheduler_test.go` anchors scheduler injection/clearing behavior | Absorbed while retaining usage persistence |
| API key usage refactor / compatibility grouping | absent from fork baseline | management route `internal/api/server.go:674`; handler/test files `internal/api/handlers/management/api_key_usage.go` and `api_key_usage_test.go` are present | Absorbed |
| OAuth excluded models management | absent from older fork scope | config example `config.example.yaml:427-428`; routes `internal/api/server.go:736-739`; service exclusion handling `sdk/cliproxy/service.go:2220-2223` | Absorbed |
| OpenAI/xAI video endpoints and auth binding TTL | absent from fork baseline | config `config.example.yaml:154-156`; xAI routes `internal/api/server.go:443-447`; OpenAI-compatible routes `internal/api/server.go:458-460`; route tests `internal/api/server_test.go:423-458` | Absorbed |
| WebSocket runtime/provider support | absent from older fork scope | config `config.example.yaml:213`; server websocket route attachment `internal/api/server.go:555`; scheduler websocket buckets `sdk/cliproxy/auth/scheduler.go:57,889,1074-1080`; service websocket lifecycle `sdk/cliproxy/service.go:907-926,1985-1991` | Absorbed |
| Request error logs and retention management | absent from older fork scope | config `config.example.yaml:91`; request logger setup `internal/api/server.go:87`; management routes `internal/api/server.go:633-635,687`; runtime retention update `internal/api/server.go:1647-1649` | Absorbed |
| Claude cloak global toggle and per-credential cloak documentation | absent from older fork scope | config `config.example.yaml:137-142,292-299`; current runtime files include Claude executor updates under `internal/runtime/executor/claude_executor.go` | Absorbed |
| Latest translator/runtime empty-text and Codex websocket prefix fixes | upstream-side commits `bd646819`, `36ed0e5c` were pending before the local merge candidate | current staged changes include `internal/translator/antigravity/openai/chat-completions/antigravity_openai_request.go`, `internal/translator/gemini/openai/chat-completions/gemini_openai_request.go`, `internal/runtime/executor/codex_websockets_executor.go` and related tests | Absorbed in local merge candidate |

Static absorption conclusion: frontend-compatible backend upstream capabilities through `bd646819ed95` are present in the local merge candidate. Because compile/build/test execution is deferred by user instruction, this checklist proves code-path presence and conflict resolution only; it does not prove runtime correctness.

## Feature Preservation Matrix

| Fork custom feature | Baseline evidence | Current evidence | Status |
|---|---|---|---|
| Fork management panel source | `internal/config/config.go:23` and `config.example.yaml:36` point to `wenxi96/Cli-Proxy-API-Management-Center`; installer default `install/linux/cliproxyapi-installer.sh:18` | same defaults remain at `internal/config/config.go:23`, `config.example.yaml:36`, `install/linux/cliproxyapi-installer.sh:18`; `internal/config/parse.go:34,53` still backfills the fork default | Preserved |
| Auth Files display name passthrough | `internal/api/handlers/management/auth_files.go:301`; `internal/api/server.go:914,973,1173` emit/read `display_name` | `internal/api/handlers/management/auth_files.go:381`; `internal/api/server.go:1041,1156,1383`; batch-check extracts `displayName/display_name` at `auth_files_batch_check.go:1437` | Preserved |
| Auth Files ZIP download | baseline archive writer at `internal/api/handlers/management/auth_files.go:752` | current archive writer at `internal/api/handlers/management/auth_files.go:829`; tests include `TestDownloadAuthFilesArchive_ReturnsZip` and traversal tests | Preserved |
| Auth Files batch check and async jobs | baseline routes at `internal/api/server.go:713-715`; implementation under `auth_files_batch_check*.go` | current routes at `internal/api/server.go:748-750`; implementation and job retention/concurrency remain in `auth_files_batch_check*.go`; tests include summary, concurrency, job progress, completed results | Preserved |
| Provider-local scoped pool routing | baseline scheduler/conductor hooks: `scheduler.go:202,228,235`; `conductor.go:660,671,2462,3102,3127,3191,3265,3296,3387,4007` | current hooks: `scheduler.go:160,186,193`; `conductor.go:1225,1236,3534,4230,4261,4325,4401,4437,4528,5202`; management auth-file list emits scoped-pool fields from `auth_files.go:332` | Preserved |
| Low-quota auto-disable and threshold | baseline primary key was `auto-disable-auth-file-on-zero-quota`: `config.example.yaml:122`, `config.go:232`, routes `server.go:603-609` | current primary key is `auto-disable-auth-file-on-low-quota`: `config.example.yaml:166`, `config.go:336`, routes `server.go:657-668`; legacy `zero-quota` remains only as config/API compatibility in `config.go:353,384`, `quota.go:38-43`, `server.go:662-664`; config save removes old key at `config.go:2277` | Preserved with intentional renamed primary key |
| Usage statistics persistence and management API | baseline `UsageStatisticsEnabled` in `config.go:77`, management routes `server.go:584-586`, persistence at `internal/usage/persistence.go:52,68,130`, service persist at `sdk/cliproxy/service.go:259` | current `UsageStatisticsEnabled` in `config.go:80`, management routes `server.go:637-639`, persistence at `internal/usage/persistence.go:52,68,130`, service persist at `sdk/cliproxy/service.go:562`; usage import/export handlers in `internal/api/handlers/management/usage.go` | Preserved |
| Fork release/install packaging | baseline docker image `ghcr.io/wenxi96/cli-proxy-api`, GoReleaser workflow/assets, checksums | current docker image remains `ghcr.io/wenxi96/cli-proxy-api`; main release workflow builds direct Go archives, no-plugin Linux/FreeBSD assets and checksums (`release.yaml:376-497`, `517-656`, `683-684`); release-history fallback generates 10 archives (`rebuild-release-history.yml:150-205`) while keeping GoReleaser only for historical worktrees with `.goreleaser.yml` | Preserved and adapted to upstream release rewrite |
| AMP/Ampcode removal | baseline had Amp integration before user decision | current `internal/api/modules/amp` directory is absent; source search for `ampcode` only finds config migration cleanup `internal/config/config.go:2244` | Removed intentionally, not a missing fork feature |

## Detailed Backend Feature Notes

### Fork Management Panel Source

- Purpose: keep the backend updater and installer pointed at the fork management panel repository, so the server downloads `management.html` from `wenxi96/Cli-Proxy-API-Management-Center` rather than the upstream panel.
- Baseline logic: `internal/config/config.go` defined `DefaultPanelGitHubRepository` as the fork URL; `internal/config/parse.go` backfilled missing config values; the Linux installer injected or preserved `panel-github-repository`.
- Current logic: `internal/config/config.go:23` still defines the fork default, `internal/config/parse.go:34,53` still backfills it, `config.example.yaml:36` documents it, and `install/linux/cliproxyapi-installer.sh:173-185` still updates the config file.
- Runtime path: `internal/managementasset/updater.go` calls `resolveManagementReleaseSource(panelRepository)` and therefore uses the configured fork repo when `RemoteManagement.PanelGitHubRepository` is populated.
- Verification anchor: `internal/config/panel_defaults_test.go` checks the default panel repository.

### Auth Files Display Name

- Purpose: preserve human-friendly credential / model labels across management APIs, provider workbench, model registry output and usage views.
- Baseline logic: auth-file model listing emitted `display_name`; server model API read and returned display names; batch-check extracted `displayName` / `display_name`.
- Current logic: `internal/api/handlers/management/auth_files.go:381` emits `display_name`, `internal/api/server.go:1041,1156,1383` preserves display-name fields in model management flows, and `internal/api/handlers/management/auth_files_batch_check.go:1437` still extracts display-name aliases for batch-check results.
- Runtime path: display-name metadata flows from config/provider model data into management JSON responses and is consumed by the frontend for cards, modals and usage attribution.
- Verification anchor: `internal/api/server_test.go` includes display-name assertions; frontend inventory separately verifies the display-name consumer paths.

### Auth Files ZIP Download

- Purpose: allow users to select multiple auth files and download them as one zip archive instead of one file at a time.
- Baseline logic: `DownloadAuthFilesArchive` accepted requested file names, rejected traversal, read matching auth files and emitted a zip response.
- Current logic: route registration remains `internal/api/server.go:753`; implementation remains `internal/api/handlers/management/auth_files.go:813-860` with `zip.NewWriter` at line 829.
- Runtime path: frontend posts selected names to `/v0/management/auth-files/download-archive`; backend validates names, reads from the auth material directory, writes archive entries and returns `application/zip`.
- Verification anchor: `internal/api/handlers/management/auth_files_download_test.go` and `auth_files_download_windows_test.go` cover zip output and path traversal rejection.

### Auth Files Batch Check And Async Jobs

- Purpose: inspect credential quota / health in bulk, summarize capacity and risk, and drive recovery actions such as disabling exhausted credentials or re-enabling recovered credentials.
- Baseline logic: synchronous `/auth-files/batch-check` plus result aggregation and provider-specific quota inspection existed before this upstream absorption.
- Current logic: routes remain `internal/api/server.go:748-750`; `internal/api/handlers/management/auth_files_batch_check.go` performs quota checks, classification, bucket aggregation, refresh-window labels and action-candidate construction; `auth_files_batch_check_jobs.go` stores async job state in the handler's `batchCheckJobs` map.
- Runtime path: the frontend creates a job through `/auth-files/batch-check-jobs`, polls `/auth-files/batch-check-jobs/:id`, and receives summary, aggregate, result, skipped and progress payloads.
- Verification anchor: `auth_files_batch_check_test.go` and `auth_files_batch_check_jobs_test.go` cover summary, concurrency, disabled-file scope, job progress and completed job reads.

### Provider-Local Scoped Pool Routing

- Purpose: enable provider-specific pool selection so one provider category can route through a bounded healthy credential subset without changing unrelated providers.
- Baseline logic: `routing.scoped-pool` config was normalized in `internal/config`, candidate selection lived in `sdk/cliproxy/auth`, and management endpoints exposed both config and runtime status.
- Current logic: `internal/config/config.go:422-453` defines config, `internal/config/config.go:1005-1013` reports/normalizes enablement, `sdk/cliproxy/auth/scoped_pool.go` and scheduler/conductor logic select pool members, and `sdk/cliproxy/auth/conductor.go:5213` exposes `ScopedPoolSnapshot`.
- Runtime path: routing remains normal round-robin unless scoped-pool is enabled globally/provider-locally; quota/errors update pool state, and management status exposes in-pool, standby, penalized and disabled reasons.
- Verification anchor: `sdk/cliproxy/auth/scoped_pool_test.go`, `internal/config/routing_scoped_pool_test.go` and `internal/api/handlers/management/routing_scoped_pool_test.go`.

### Low-Quota Auto-Disable With Legacy Zero-Quota Compatibility

- Purpose: automatically disable supported file-backed auth entries when real quota inspection shows low or exhausted quota, while retaining compatibility with the older zero-quota config/API naming.
- Baseline logic: the main config key and endpoint used `auto-disable-auth-file-on-zero-quota`.
- Current logic: the primary key is now `quota-exceeded.auto-disable-auth-file-on-low-quota` (`config.example.yaml:166`, `internal/config/config.go:336`); legacy YAML/JSON fields are read in `internal/config/config.go:352-408`; old API endpoints are still registered at `internal/api/server.go:662-664`; config save removes the old YAML key in `internal/config/config.go:2277`.
- Runtime path: `sdk/cliproxy/auth/quota_check_async.go:22` gates auto-disable from the low-quota setting; management can read/write through both new and old routes, but persisted config converges on the new low-quota key.
- Verification anchor: `internal/config/quota_exceeded_test.go`, `internal/api/handlers/management/quota_test.go`, `internal/api/server_test.go` and `sdk/cliproxy/auth/quota_check_async_test.go`.

### Usage Statistics Persistence And Management API

- Purpose: keep usage accounting across process restarts and expose management endpoints for frontend Usage page, export/import and auth-file usage hints.
- Baseline logic: `UsageStatisticsEnabled`, internal usage snapshot persistence, management `/usage` APIs and service-level persistence reconfiguration existed before merge.
- Current logic: `internal/config/config.go:80` keeps the config field; routes are registered at `internal/api/server.go:675-678`; handlers live in `internal/api/handlers/management/usage.go`; service config changes call `applyUsagePersistenceConfigChange` in `sdk/cliproxy/service.go:650`; runtime toggle updates `usage` and `redisqueue` at `internal/api/server.go:1638-1640`.
- Runtime path: usage records accumulate in memory, can flush to snapshot, can be exported/imported through management API and can gate redisqueue usage payload publishing.
- Verification anchor: `internal/usage/persistence_test.go`, `sdk/cliproxy/service_usage_persistence_test.go`, `internal/api/handlers/management/usage_test.go` and `internal/api/server_test.go`.

### Release, Install And History-Rebuild Assets

- Purpose: preserve fork release names, Docker image target, no-plugin artifacts, checksums and historical release rebuilds after upstream removed GoReleaser from the main release flow.
- Baseline logic: fork release packaging depended on GoReleaser and fork-specific asset naming / image names.
- Current logic: `.goreleaser.yml` is removed in the upstream-aligned path; `.github/workflows/release.yaml` directly builds platform archives and `checksums.txt`; `.github/workflows/rebuild-release-history.yml` uses GoReleaser only for historical worktrees that still contain `.goreleaser.yml`, otherwise builds the current 10 archive fallback set.
- Runtime path: new tag releases should use the direct workflow; history rebuild can still service older tags without requiring current `.goreleaser.yml`.
- Verification anchor: `evidence/release-history-fallback-assets-2026-06-17.md` records the 10-archive fallback validation already performed before the latest 2026-06-23 upstream merge candidate.

### AMP/Ampcode Removal

- Purpose: follow the user's explicit decision to accept upstream removal of the Amp integration and avoid a frontend/backend contract split.
- Baseline logic: fork had `internal/api/modules/amp`, config examples and management routes for Ampcode.
- Current logic: `internal/api/modules/amp` is absent; management routes are removed; config cleanup only removes stale `ampcode` keys via `removeMapKey(root, "ampcode")`.
- Runtime path: Amp/Ampcode provider management is no longer available and should not be presented by the frontend.
- Verification anchor: source search for `ampcode` in backend source should only find config migration cleanup or unrelated strings.

### Upstream Plugin/Home Sync Integration In The Latest Merge Candidate

- Purpose: absorb latest upstream plugin/home changes without dropping fork usage persistence and runtime config behavior.
- Current merge-candidate logic: `cmd/server/main.go` imports `internal/homeplugins` and calls `homeplugins.Sync` after `applyHomeRuntimeDefaults`; `sdk/cliproxy/service.go` keeps fork usage persistence reconfiguration and adds upstream plugin runtime synchronization.
- Status: non-compile checks show no unresolved conflict files, no conflict markers in the two resolved files and clean `git diff --check`. Compile verification is deferred by user instruction.

## Current Regression Anchors

Relevant tests currently present in the worktree:

- `sdk/cliproxy/auth/scoped_pool_test.go`
- `sdk/cliproxy/auth/quota_check_async_test.go`
- `internal/config/quota_exceeded_test.go`
- `internal/api/handlers/management/quota_test.go`
- `internal/api/handlers/management/routing_scoped_pool_test.go`
- `internal/api/handlers/management/auth_files_batch_check_test.go`
- `internal/api/handlers/management/auth_files_batch_check_jobs_test.go`
- `internal/api/handlers/management/auth_files_download_test.go`
- `internal/usage/persistence.go` plus `sdk/cliproxy/service_usage_persistence_test.go`
- `internal/config/panel_defaults_test.go`

## Verification Notes

Commands read during this inventory:

- `git fetch upstream --tags --prune && git fetch origin --tags --prune`
- `git rev-parse --short=12 upstream/main origin/main dev HEAD`
- `git rev-list --left-right --count origin/main...upstream/main`
- `git rev-list --left-right --count --cherry-pick dev...upstream/main`
- `git merge-base --is-ancestor upstream/main HEAD`
- targeted `rg` and `git grep` over baseline/current feature symbols listed above
- 2026-06-23 post-conflict non-compile checks: `git diff --name-only --diff-filter=U` returned empty, conflict-marker search in `cmd/server/main.go` and `sdk/cliproxy/service.go` returned no matches, and `git diff --check` returned no output.

This inventory is static code evidence. It does not replace the required post-merge backend build/tests after absorbing `bd646819ed95`; those checks are deferred because the user explicitly requested no compile verification for now.
