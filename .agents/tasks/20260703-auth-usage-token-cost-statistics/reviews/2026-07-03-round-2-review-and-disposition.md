# Round 2 Focused Review and Disposition

Review Status
- workflow.operation.name: scheme_review_round_2
- workflow.operation.status: completed_with_fallback
- workflow.review_scope.status: partial
- workflow.scope_check.status: clean
- workflow.findings.status: findings_reported
- verdict: changes_requested_then_fixed

## Review Scope

- Review Type: design / plan / governance docs
- Candidate Scope:
  - Backend task: `.agents/tasks/20260703-auth-usage-token-cost-statistics/`
  - Frontend task: `/home/cheng/git-project/Cli-Proxy-API-Management-Center/.agents/tasks/20260703-frontend-auth-usage-token-cost-statistics/`
- Objective: Confirm Round 1 token total normalization fix and look for new material contract issues before declaring the scheme docs ready for implementation.
- External reviewer attempt: `gemini --approval-mode plan` fallback was attempted after same-tool child review failed in Round 1, but the session failed with provider errors and quota/rate-limit errors (`502 unknown provider...`, then `429 rate_limit_exceeded`). No external report was produced.
- Fallback: main-thread focused review using source inspection and document consistency checks. This is not an independent reviewer verdict.

## Scope Check

- Intent: Document a backend and frontend implementation plan for per-auth credential token, estimated-cost, and request-detail statistics.
- Delivered: Backend and frontend specs/plans exist, include token aggregation, detail API, auth-files usage summary, frontend credential stats columns, detail modal, local fallback, i18n, and verification tasks.
- Scope Result: clean. The docs do not propose installing plugins, storing prompts/responses, changing quota display, or treating estimated cost as true provider billing.

## Findings

### R2-F1

- Severity: medium
- Summary: The backend findings previously stated stable `auth_index` was a 16-character hex string and suitable for path parameters. Source shows generated indexes are 16-hex hashes, but existing `Auth.Index` values are preserved as arbitrary strings.
- Evidence:
  - `sdk/cliproxy/auth/types.go`: `EnsureIndex()` returns an existing trimmed `Auth.Index` before generating a hash.
  - `sdk/cliproxy/auth/types.go`: generated `stableAuthIndex()` returns a 16-hex hash only for newly derived local indexes.
- Impact: Implementers could assume `auth_index` is always hex and concatenate it into `/usage/auths/:auth_index/requests` without encoding or testing non-hex values, causing detail lookup failures for externally supplied or runtime-preserved indexes.
- Recommendation: Treat `auth_index` as an opaque string, require URL encoding for the path-based API, test non-hex values, and switch to query parameters if actual values can contain path separators.
- Confidence: high
- Disposition: accepted
- Fixes:
  - Backend `task.md`, `findings.md`, spec, and plan now state that `auth_index` is an opaque string and must not be treated as fixed hex.
  - Backend plan now requires a non-hex URL escape test and keeps a query-parameter fallback stop condition.
  - Frontend `task.md`, `findings.md`, spec, and plan now require service-layer URL encoding and no fixed-format assumptions.

### R2-F2

- Severity: medium
- Summary: The backend compatibility section had an ambiguous rule for importing snapshots when `auths` exists but details are missing, while also saying details are the fact source.
- Evidence:
  - Current `RequestStatistics.MergeSnapshot()` imports request details from `snapshot.APIs.*.Models.*.Details` and uses dedup keys based on those details.
  - `auths` is planned as a derived aggregation over those details, not an independent event log.
- Impact: Implementers could either double-count imported data by merging both details and auth aggregates, or try to synthesize request details from aggregate `auths`, losing event-level fidelity and pagination correctness.
- Recommendation: Define imported `auths` as a derived snapshot only; rebuild auth aggregation from details and do not reverse-create details from aggregate auths.
- Confidence: high
- Disposition: accepted
- Fixes:
  - Backend spec now states imported `auths` is a derived snapshot and details remain the fact source.
  - Backend plan now requires a new-format fixture containing `auths` to verify no duplicate aggregation is introduced.
  - Backend findings now records auth aggregation as a derived query view.

## Scorecard

| Dimension | Score | Notes |
|---|---:|---|
| Scope Control | 5 | Changes stay inside planning/governance docs. |
| Evidence Quality | 4 | Findings are backed by source inspection; external reviewer failed. |
| Correctness | 4 | Round 1 total-token issue remains fixed; Round 2 fixed two additional contract ambiguities. |
| Safety | 5 | No business code, secrets, plugin install, commit, push, or deploy changes. |
| Testability | 4 | Plans now include token normalization, import compatibility, and URL encoding tests. |
| Maintainability | 4 | Frontend and backend docs use the same `auth_index` and token normalization contract. |

## Verification Evidence

- Source inspected:
  - `internal/usage/logger_plugin.go`
  - `sdk/cliproxy/auth/types.go`
  - `/home/cheng/git-project/Cli-Proxy-API-Management-Center/src/utils/usage.ts`
- Document consistency search:
  - `rg -n "16 位|十六进制|hex|details 缺失|以 details 为事实源|auths.*details|auth_index.*固定|fixed|encodeURIComponent|URL escape|path 分隔符" ...`
  - `rg -n "1420|input.*output.*reasoning.*cache|cached.*叠加|cached.*total|extractTotalTokens|total_tokens" ...`

## Open Questions / Limitations

- This round is not an independent reviewer verdict because both same-tool child review and external Gemini review were unavailable or failed before report output.
- Business code was not implemented or executed in this task; validation is limited to design/plan consistency and governance-document checks.

## Recommended Next Step

- Run one final main-thread focused review after the Round 2 fixes.
- Then run `.agents` doc audits, `git diff --check`, and conflict-marker scans before declaring the planning docs ready for user review.
