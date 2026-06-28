# Findings

## Branch Baseline

- Repository: `CLIProxyAPI`
- Work branch: `dev@3359d754a390`
- Remote development branch: `origin/dev@3359d754a390`
- Upstream mirror: `origin/main == upstream/main == b05a27e4d708`
- Target tag: `v7.2.43`
- Divergence before merge: `origin/dev...origin/main = 110 30`
- Divergence after merge: `dev...origin/main = 112 0`
- Merge rehearsal conflicts:
  - `cmd/server/main.go`
  - `internal/runtime/executor/xai_executor.go`
  - `sdk/cliproxy/auth/conductor.go`

## Commit Absorption Matrix

| Commit | 功能作用 | 建议 | 冲突 | 解决建议 |
|---|---|---|---|---|
| `7c390a7a` | 增加 Claude Code session handling、缓存与测试。 | 吸收 | `xai_executor.go` 间接冲突 | 与 xAI reasoning 相关清洗逻辑一起验证。 |
| `f1ed8912` | 将 message-level system roles 包装为用户可见提醒。 | 吸收 | 无 | 保持上游 translator 行为。 |
| `53a21dfb` | xAI Grok 上游前丢弃外部 `encrypted_content`。 | 吸收 | `xai_executor.go` | 与 fork 现有 xAI 请求清洗合并。 |
| `05d1792d` | 为 Claude 消息回放 Grok reasoning。 | 吸收 | `xai_executor.go` | 使用上游 reasoning replay 路径，同时保留 fork 请求兼容逻辑。 |
| `e9a11db7` | Home 插件管理和同步增强。 | 吸收 | `cmd/server/main.go` | 保留 fork `applyHomeRuntimeDefaults`，改用上游 `SyncWithReport`。 |
| `b89c594a` | Home PR merge。 | 随父提交吸收 | 无 | 无单独处理。 |
| `70053bea` | 认证类型检测重构，增加动态来源分类。 | 吸收 | `conductor.go` | 与 scoped-pool 候选过滤顺序核对。 |
| `3a13865d` | Home plugin status reporting 迁移/重命名。 | 吸收 | `cmd/server/main.go` | 同步调用 `home.ReportPluginStatus`。 |
| `38ed7aef` | 清洗 Codex 直连图片下游 UA。 | 吸收 | 无 | 保持上游修复。 |
| `fd93ee03` | OAuth model alias force-mapping。 | 吸收 | `conductor.go` | 保留上游 alias result helper，并让 scoped-pool filter 继续作用于候选集。 |
| `c20ac28a` | OAuth force-mapping PR merge。 | 随父提交吸收 | 无 | 无单独处理。 |
| `87e6d9cf` | video auth management 增加 model binding 和传播。 | 吸收 | 无 | 补视频 API 聚焦验证。 |
| `a183e729` | 增加 `ParseAuths` 展开 credential payload。 | 吸收 | 无 | 核对 auth store / SDK 调用兼容。 |
| `a7250275` | 强制将响应模型映射回 config alias。 | 吸收 | 无 | 与 `fd93ee03` 一起验证。 |
| `df10a5b1` | shadow plugin 管理与清理。 | 吸收 | 无 | 保持 pluginhost 新能力。 |
| `7712ffed` | SDK PR merge。 | 随父提交吸收 | 无 | 无单独处理。 |
| `b53d1e95` | pluginhost 用 `activeRecords` 替代 `Snapshot().records`。 | 吸收 | 无 | 注意 fork plugin callback 非递归测试。 |
| `810abe5e` | plugin metadata 增加 `OAuthProvider`。 | 吸收 | 无 | 与前端 plugin OAuth 能力对齐。 |
| `29b53434` | plugin OAuth PR merge。 | 随父提交吸收 | 无 | 无单独处理。 |
| `192888f9` | plugin 日志增加 plugin name/path 字段。 | 吸收 | 无 | 保持结构化日志不泄密。 |
| `c4cf0fd3` | plugin PR merge。 | 随父提交吸收 | 无 | 无单独处理。 |
| `eb2e1e33` | 重写 API key alias response models。 | 吸收 | `conductor.go` | 与 fork API key alias / scoped-pool 行为共同验证。 |
| `7d1d2512` | README 增加 Universal Chat Provider。 | 吸收 | 无 | 文档变更。 |
| `abe68cc1` | 文档 PR merge。 | 随父提交吸收 | 无 | 无单独处理。 |
| `cb6992ef` | README 增加 Universal Chat Provider section。 | 吸收 | 无 | 文档变更。 |
| `65f2288a` | Gemini 3.5 Flash variants 和 Medium tier。 | 吸收 | 无 | 核对 model registry。 |
| `6a59d645` | plugin version management / hot reload logging。 | 吸收 | 无 | 与 pluginhost 热加载验证一起看。 |
| `4c0c6029` | plugin PR merge，目标 tag `v7.2.42`。 | 随父提交吸收 | 无 | 作为目标基线。 |
| `2fa4dabe` | 改进 downstream response ID rewrite，并补重复 response 场景测试。 | 吸收 | 无 | 已随 `v7.2.43` 增量吸收；由 `go test ./internal/runtime/executor` 和全量 `go test ./...` 覆盖。 |
| `b05a27e4` | README 多语言 partners 增加 CyberPay，并新增 `assets/cyberpay.jpg`，目标 tag `v7.2.43`。 | 吸收 | 无 | 文档/资产变更；已随最新 `origin/main` 合入。 |

## Conflict Strategy

- `cmd/server/main.go`: 用 fork 的 `cfg = applyHomeRuntimeDefaults(parsed, homeCfg)` 作为 Home 配置进入运行时前的唯一输入；吸收上游 `homeplugins.SyncWithReport(ctxHomePlugins, cfg, pluginHost)` 并向 `home.ReportPluginStatus(...)` 上报同步结果；同时保留上游第二阶段 load-result 上报，即 `pluginHost.ApplyConfig` / runtime apply 后调用 `homeplugins.MarkLoadResults(...)` 并再次 `home.ReportPluginStatus(...)`。
- `internal/runtime/executor/xai_executor.go`: 合并顺序必须覆盖完整上游链路：先执行 `applyXAIReasoningReplayCacheRequired(...)` 建立 replay scope，再执行 `normalizeXAIInputReasoningItems(...)`、`sanitizeXAIInputEncryptedContent(...)`、`sanitizeXAIResponsesBody(...)`；`sanitizeXAIResponsesBody` 使用 `reasoning.Exists() && reasoning.IsObject() && len(reasoning.Map()) == 0` 删除空 reasoning；流式和非流式完成事件都必须保留 reasoning replay cache 写入。
- `sdk/cliproxy/auth/conductor.go`: scoped-pool 不应在 alias candidate 生成后才过滤 auth；正确不变式是：fork scoped-pool 的 `filterScopedPoolAvailable` / provider 分组过滤先作用于 auth candidate set，再进入 scheduler / default auth selection；上游 `preparedExecutionModelsWithAlias`、`executionModelCandidatesWithAlias`、alias response rewrite helper 仍作为 selected auth 的执行模型与响应模型处理，并覆盖 normal、stream、count/credits 路径。

## Fork Preservation Checklist

这些文件/符号是 fork 定制保留清单，即使不是文本冲突，也必须在 L03 合并后做文件/符号检查和聚焦验证：

- scoped-pool: `sdk/cliproxy/auth/scoped_pool.go`, `sdk/cliproxy/auth/scoped_pool_test.go`, `sdk/cliproxy/auth/types.go` 中的 `PoolState` / `PoolReason` / `PoolSnapshot` 相关类型，以及 `sdk/cliproxy/auth/scheduler.go` scoped-pool hooks。
- quota auto-disable / active quota refresh: `sdk/cliproxy/auth/quota_check.go`, `sdk/cliproxy/auth/quota_check_async.go`, `sdk/cliproxy/auth/active_quota_refresh_pool.go`, `internal/authquota/service.go` 及对应测试。
- usage persistence: `internal/usage/persistence.go`, `internal/usage/logger_plugin.go`, `sdk/cliproxy/service_usage_persistence_test.go`, `internal/tui/usage_tab.go`。
- external auth lifecycle: `sdk/cliproxy/service.go` 中 `authMaintenanceHook` / lifecycle sync 包装，`sdk/cliproxy/service_external_auth_registration_test.go`。
- management/config surfaces: `internal/api/handlers/management/routing_scoped_pool.go`, `internal/api/handlers/management/config_basic.go`, `internal/config/routing_scoped_pool_test.go`, `internal/config/quota_exceeded_test.go`。

## Semantic Risk Files

`git merge-tree` 的文本冲突目前集中在 3 个文件，但以下双方都改过或承载 fork 定制的文件必须在 L03/L04 额外检查，不能只以“无 conflict marker”作为安全依据：

- `sdk/cliproxy/service.go`
- `sdk/cliproxy/auth/types.go`
- `sdk/cliproxy/auth/scheduler.go`
- `internal/config/config.go`
- `config.example.yaml`
- `internal/store/gitstore.go`
- `internal/store/objectstore.go`
- `internal/store/postgresstore.go`

## Verification Notes

- Host may not have Go installed; use Docker Go 1.26:
  - `docker run --rm -v /home/cheng/git-project/CLIProxyAPI:/workspace -w /workspace -e GOCACHE=/workspace/.tmp/go-build -e GOMODCACHE=/workspace/.tmp/go-mod golang:1.26 go test ./...`
  - `docker run --rm -v /home/cheng/git-project/CLIProxyAPI:/workspace -w /workspace -e GOCACHE=/workspace/.tmp/go-build -e GOMODCACHE=/workspace/.tmp/go-mod golang:1.26 go build -buildvcs=false -o /tmp/cli-proxy-api-check ./cmd/server`
- Before L03 code merge, run a writable-surface merge rehearsal command and record conflicts:
  - `git merge-tree --write-tree --name-only dev origin/main`
- Before L03 code merge, verify at least one Go runner is available:
  - `go version` or `docker image inspect golang:1.26`
- Focused post-merge tests must include:
  - `go test ./internal/runtime/executor -run 'Test.*XAI|Test.*Grok'`
  - `go test ./sdk/cliproxy/auth -run 'Test.*(OAuthAlias|APIKeyAlias|ScopedPool|OpenAICompat|Quota|ActiveQuota)'`
  - `go test ./sdk/cliproxy -run 'Test.*(ExternalAuthRegistration|UsagePersistence|Plugin)'`
  - `go test ./internal/homeplugins ./internal/home ./cmd/server -run 'Test.*(Home|Plugin|Sync|Report|RuntimeDefaults)'`
- Add or preserve a combined regression test for OAuth alias + scoped-pool ordering: route model maps through alias to upstream model, scoped-pool filters auth candidates before selection, executor receives upstream model, and usage / response model still preserve requested alias where required.
