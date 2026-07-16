# 后端上游吸收治理方案

## 目标

将漂移后固定上游目标 `09da52ad` / `v7.2.80` 形成可审查、可验证、可回滚的吸收候选，并保护 fork 定制。

## 范围

- 仓库分析、118 个上游提交的更新清单、冲突预检、方案评审。
- 用户确认后的隔离候选 merge、冲突解决、验证与多轮评审。
- 经授权的 dev/master 分支推进和可选发布。

## 非目标

- 不吸收目标 SHA 之后的新提交。
- 不在 L01 修改业务代码或产生外部副作用。

## 分支/发版策略

- upstream_branch: `main`
- integration_branch: `dev`
- release_branch: `master`
- upstream mirror: 当前 `origin/main@5b7f2361` 可 fast-forward 到固定目标 `09da52ad`，落后 41 个提交。候选合并前在单独授权点执行 `git push origin 09da52ad:main`，并核验 `origin/main == upstream_target_sha`；若不能 fast-forward 或目标漂移则停止。
- release candidate gate: master 当前树无 `.agents`，且 master candidate 通过测试、构建、diff 与冲突扫描。
- tag / release 触发条件: 用户明确授权、版本脚本在实际 master candidate 上计算、tag 指向已验证 SHA。
- 分支策略例外及理由: 无。

## 授权边界

- 允许: fetch、分析、治理文档、只读预检与方案评审。
- 需要再次确认: 候选 merge；任何 commit/push/master/tag/release/deploy。
- 禁止: 强推、历史改写、删除 fork 定制或把 `.agents` 带入 master。

## 任务拆分

- 后端仓库任务: 当前任务。
- 前端仓库任务: `20260715-frontend-upstream-v1-18-3-absorption`。
- 共享确认点: 两仓库清单与冲突评审均闭环后统一确认。
- 不纳入本轮的改动: 目标 SHA 之后的新上游提交、无关功能开发。
- 跨仓库证据落点: 各仓库独立 `evidence/`，最终仅汇总结论。

## 阶段拆分

1. 仓库分析与目标固定。
2. 更新清单和冲突预检。
3. 独立方案评审与复评。
4. 用户确认 checkpoint。
5. 重新 fetch、核验目标未漂移，并经授权 fast-forward `origin/main` 镜像。
6. 隔离候选合并和冲突解决。
7. 聚焦/全量验证与多轮代码评审。
8. 经授权的提交、master 与发版。

## 评审策略

- 跨仓库且高跨度，L01 必须独立只读评审。
- finding 使用 `fixed | accepted_risk | not_applicable | blocked`。
- 最后一轮无新增 finding、无未处理 medium 及以上问题才允许进入确认 checkpoint。

## 停止条件

- 上游目标漂移、fork 定制保护点不清、验证环境不可用、评审发现阻断问题或外部副作用未授权。

## 验证策略

- 冲突测试保全: 解决测试文件冲突前记录 base/dev/upstream 的测试函数集合；resolved candidate 不得静默删除任一仍有效的 fork 或上游行为断言。
- 聚焦验证: 按下方 risk-to-proof 矩阵执行，修复某一切片后重跑对应测试。
- 并发验证: auth/OAuth、usage 终态和 watcher 相关包执行有界 `go test -race`；若环境不支持 race，记录原因和剩余风险。
- 全量验证: Docker Go 1.26 `go test ./...`、server build、`git diff --check` 与冲突标记扫描。
- 发布后验证: refs、Actions、Release assets、checksums 与 GHCR manifest。

## Risk-to-proof 矩阵

| 风险切片 | 必保行为 | 聚焦验证与通过标准 |
|---|---|---|
| Usage/tier/cache/generate | parser 不合成 reported total；explicit-zero 与 missing 可区分；tier-only 不触发 observed usage；cache alias 不双算；generate 缺省/true 向后兼容、旧持久化记录缺字段归一化 true、explicit false 全链路保留；终态唯一 | `go test ./internal/runtime/executor/helps ./internal/usage ./internal/redisqueue ./internal/pluginhost ./sdk/api/handlers ./sdk/cliproxy/usage`；usage、queue、plugin adapter、metadata、legacy persistence 测试全部通过，测试函数集合不减少 |
| Auth/OAuth/scoped pool | kind-before-pool、401 singleflight、device cancel/idempotent、Home/alias/scoped identity 保持 | `go test -race ./sdk/cliproxy/auth ./internal/api/handlers/management ./internal/authquota`；mixed API-key/OAuth、刷新成功/失败/并发、quota/pool 状态测试通过 |
| xAI/Codex tools 与 incomplete response | replay、namespace、X Search、custom tool batching/name dedupe、compaction cleanup、xAI image usage 与 fork usage hook 同时存在；explicit response.incomplete 成功转换，缺失 terminal/非客户端断流才转 request-scoped 错误且不禁用凭证 | `go test ./internal/runtime/executor ./internal/translator/... ./sdk/cliproxy/auth`；xAI image/generation、Codex explicit incomplete、missing completion、stream、tool event、malformed history 和 auth error compatibility 测试通过 |
| Logging | DeferredAPIRequest 不提前消费 body，request ID/client IP 保留，secret redaction 不回退 | `go test ./internal/logging ./sdk/api/handlers`；请求体回放、metadata、失败日志脱敏断言通过 |
| Registry/catalog | updater start/disable/close、local-model 模式、fetch/validate 工具和 catalog schema | `go test ./internal/registry ./cmd/fetch_codex_models ./cmd/validate_codex_models ./cmd/server`；validator/build 退出码 0，local-model 不启动远端刷新 |
| Config/watcher/xAI key | config parse/round-trip、management API、热更新和 model hash 保持 fork scoped/quota 字段 | `go test -race ./internal/config ./internal/watcher ./internal/api/handlers/management ./sdk/cliproxy`；新增 xAI/header 字段与 fork 字段均可 round-trip |
| Plugin path/runtime | 空值默认 plugins、leading tilde 展开、store/home/pluginhost/builder 使用同一 resolved path，fork plugin source/runtime 不回退 | `go test ./internal/config ./internal/api/handlers/management ./internal/homeplugins ./internal/pluginhost ./sdk/cliproxy`；相对/绝对/tilde 路径和安装/加载测试通过 |
| Gitstore/dependencies | 新 go-git client options 与禁用 commit signing 不破坏 fork store 初始化、pull/push 和凭证处理 | 新增 direct test：预设 `commit.gpgsign=true`，`EnsureRepository` 后断言 false 并完成 commit/push；运行 `go test ./internal/store`、`go mod verify`、`go mod tidy -diff` |
| Release workflows | tag-only、GHCR、多资产、SourceRepository、checksums、TARGET_GOARCH、catalog refresh | `bash -n .github/scripts/refresh-model-catalogs.sh`；对 workflow trigger、matrix、ldflag、asset naming 做静态断言；不得出现 DockerHub 发布 job |

## Dev 到 Master 候选构造

1. 固定并记录通过全量验证的 `dev_candidate_sha`。
2. 在独立 master worktree 中执行 `master <- dev_candidate_sha` 的 no-ff 候选合并。
3. 在提交 master candidate 前从候选 index 删除 `.agents`，执行 `git ls-files --stage -- .agents`，输出必须为空；该检查针对待提交 index，而不是旧 `HEAD`。
4. 提交后记录 `master_candidate_sha`，执行 `git ls-tree -r --name-only "$master_candidate_sha" -- .agents`，输出必须为空。
5. 执行 `git diff --exit-code dev_candidate_sha..master_candidate_sha -- . ':(exclude).agents'`，证明非 `.agents` 业务树等价。
6. 因 SHA 不同，在实际 master candidate 上重跑版本脚本、diff/conflict scan、server build 和仓库要求的发布门禁；只有该 SHA 可作为 tag 目标。
