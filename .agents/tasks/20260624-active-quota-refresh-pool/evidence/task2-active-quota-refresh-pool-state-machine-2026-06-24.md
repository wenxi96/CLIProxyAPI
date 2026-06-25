# Task 2 Evidence: Active Quota Refresh Pool State Machine

## Scope

Implemented the standalone in-memory active quota refresh pool state machine without Manager lifecycle integration.

## Files

- `sdk/cliproxy/auth/active_quota_refresh_pool.go`
- `sdk/cliproxy/auth/active_quota_refresh_pool_test.go`

## Implemented Behavior

- `touch(authID, now)` adds an auth to the pool and updates `lastUsedAt`.
- First touch sets `nextCheckAt` to `now`, so the first background scan can check it without request-path quota work.
- `due(now, limit)` returns due auth IDs and marks them in-flight.
- In-flight entries are not returned again until completion/failure clears or removes them.
- Entries expire when `now - lastUsedAt > ttl`.
- `markComplete` clears in-flight state and schedules the next check from `remaining_percent - threshold_percent`.
- `markComplete` removes unsupported results and nil-remaining non-exhausted results.
- `markFailed` removes the auth from the pool.

## Interval Mapping

For threshold `40`:

- remaining `41`: delta `1`, next interval `120s`
- remaining `55`: delta `15`, next interval `120s`
- remaining `70`: delta `30`, next interval `180s`
- remaining `71`: delta `31`, next interval `300s`

## Verification

Command:

```bash
docker run --rm -v "$PWD":/workspace -w /workspace golang:1.26 bash -lc '/usr/local/go/bin/gofmt -w sdk/cliproxy/auth/active_quota_refresh_pool.go sdk/cliproxy/auth/active_quota_refresh_pool_test.go && /usr/local/go/bin/go test ./sdk/cliproxy/auth -run "TestActiveQuotaRefreshPool" -count=1'
```

Result:

```text
ok  	github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth	0.012s
```

Additional check:

```bash
git diff --check
```

Result: clean.

## Notes

This task intentionally does not integrate the pool with `Manager.MarkResult` or the real quota checker. That is Task 3. The exhausted-result behavior remains safe for Task 2 because final disable/removal coordination belongs to the Manager integration path.
