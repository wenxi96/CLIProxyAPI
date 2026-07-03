# Round 1 Review And Disposition

## Review Status

- workflow.operation.name: scheme_document_review_round_1
- workflow.operation.status: completed_with_degraded_independent_input
- workflow.review_scope.status: partial_independent_plus_main_thread
- workflow.scope_check.status: requirements_missing
- workflow.findings.status: findings_reported
- verdict: changes_requested_then_fixed

## Reviewer Dispatch

- Strict same-model same-tool child session: failed. `codex exec -m gpt-5` returned provider error that the current API does not support `gpt-5`.
- Focused same-tool child session using CLI default model `gpt-5.5`: started in read-only mode and reviewed candidate documents/source excerpts, but timed out before writing a complete report.
- The focused reviewer produced a material in-flight finding about token total normalization before timeout. Main thread independently verified the finding against current backend and frontend source.

## Findings

### R1-F1

- Severity: medium
- Summary: Backend and frontend fallback token total semantics were not aligned in the plan documents.
- Evidence:
  - Backend current `normaliseDetail()` uses provider `TotalTokens` if present; otherwise it first computes `input + output + reasoning`, and only includes cached tokens if that sum is zero.
  - Frontend current `extractTotalTokens()` uses provider `total_tokens` if present; otherwise it computes `input + output + reasoning + cached`.
  - The backend spec previously said missing total is filled by `input/output/reasoning/cache`, and the detail API sample used `input=1000`, `output=300`, `cached=120`, `total=1420`.
- Impact: If implemented as written, backend `usage.auths` and frontend local fallback could show different `total_tokens` for the same credential. Cached tokens could be double-counted in the frontend fallback path.
- Recommendation: Define one canonical token normalization rule and require both backend auth aggregation and frontend credential fallback to use it.
- Confidence: high

## Disposition

- R1-F1: accepted.
- Backend fix:
  - Updated backend design spec to define provider total as source of truth; fallback total is `input + output + reasoning`; cached tokens are only used as total fallback when primary counters are all zero.
  - Corrected API sample `total_tokens` from `1420` to `1300`.
  - Added explicit requirement that detail API item token totals match auth aggregation.
  - Updated backend implementation plan tests to cover cached-token normalization.
- Frontend fix:
  - Updated frontend design spec to require backend-aligned token normalization for local fallback.
  - Updated frontend plan to add `normalizeCredentialTokenStats()` or equivalent helper in `credentialUsage.ts`.
  - Added verification coverage for details that have input/output/cached but no total.

## Remaining Limitations

- This round did not produce a complete independent review report because the same-tool child session timed out.
- Main thread performed the final disposition and repair, so this record should not be treated as a clean independent reviewer verdict.

## Next Step

Run a second review pass against the revised documents. If no new findings are found, finish with validation and a multi-round review/fix summary.
