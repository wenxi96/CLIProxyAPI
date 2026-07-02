# Verification Evidence

## 2026-07-02

### Formatting

Command:

```bash
docker run --rm -v "$PWD":/workspace -w /workspace golang:1.26 gofmt -w sdk/cliproxy/auth/quota_check.go internal/authquota/service.go internal/authquota/service_test.go internal/api/handlers/management/auth_files_batch_check.go internal/api/handlers/management/auth_files_batch_check_test.go
```

Result: exit code 0.

### Focused Tests

Command:

```bash
docker run --rm -v "$PWD":/workspace -w /workspace golang:1.26 go test ./internal/authquota ./internal/api/handlers/management ./sdk/cliproxy/auth
```

Result:

```text
ok  	github.com/router-for-me/CLIProxyAPI/v7/internal/authquota	0.100s
ok  	github.com/router-for-me/CLIProxyAPI/v7/internal/api/handlers/management	1.321s
ok  	github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth	2.485s
```

### Server Build

Command:

```bash
docker run --rm -v "$PWD":/workspace -w /workspace golang:1.26 go build -buildvcs=false -o test-output ./cmd/server && rm -f test-output
```

Result: exit code 0.

### Diff Check

Command:

```bash
git diff --check
```

Result: exit code 0.

### Dead Code Check

Command:

```bash
rg -n "check(Codex|Claude|GeminiCLI|Kimi|Antigravity)AuthFile|extract.*BatchCheck|resolve.*BatchCheck|executeBatchCheckAPICall|classifyBatchCheckAPIResponse|codexBatchCheck|antigravityQuotaURLs|claudeBatchCheckWindows|antigravityBatchCheckGroups" internal/api/handlers/management/auth_files_batch_check.go
```

Result: only `resolveBatchCheckConcurrency` matched as a false positive for `resolve.*BatchCheck`; no old provider query function matched.
