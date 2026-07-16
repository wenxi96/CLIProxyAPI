# 后端吸收方案评审报告

## Round 1

- reviewer: Darwin / 019f6536-5788-7271-820d-cc022110bee2
- raw submission: `coordination/L01-review/workers/backend-plan-reviewer/submissions/P01-backend-plan-review/S01.md`
- verdict: `changes_requested`
- findings: Critical 0、High 5、Medium 3。

| Finding | Disposition | 修订证据 |
|---|---|---|
| H-01 Usage v2 事实语义未裁决 | fixed | `conflict-precheck.md` 新增 Usage v2 合并契约，明确 reported/computed total、UsageObserved、tier-only 和 cache alias |
| H-02 32 个自动热点无清单 | fixed | `conflict-precheck.md` 新增完整 43-path 处置账本 |
| H-03 SelectAuthByKind 与 scoped pool 副作用 | fixed | `conflict-precheck.md` 新增 kind-before-pool、eligible-only MarkSelected 契约 |
| H-04 缺少 risk-to-proof | fixed | `governance-plan.md` 新增 7 个风险切片、聚焦/race/全量验证和测试保全 |
| H-05 dev/master 发布等价性缺失 | fixed | repository analysis 记录 22/5 拓扑；governance plan 固定双 candidate SHA、非 `.agents` 树等价和 master 复验 |
| M-01 冲突策略不精确到 hunk | fixed | `conflict-precheck.md` 为 11 个冲突新增预期 resolved shape |
| M-02 跨域提交欠分类 | fixed | `upstream-update-inventory.md` 新增 xAI key、catalog、401、tier/cache 等 capability 子项 |
| M-03 401 refresh 与 quota/pool 交叉验证不足 | fixed | Auth 契约和 risk-to-proof 增加成功/失败/并发、identity/quota/pool 状态验证 |

## 主线程追加修订

- `origin/main@5b7f2361` 是固定目标祖先、落后 33 个提交；在用户授权后仅 fast-forward 到固定 SHA，目标漂移则回到清单评审。
- DockerHub 发布链明确排除，仅保留 fork tag-only/GHCR/资产矩阵并吸收 catalog refresh。
- 当前状态: 等待 Round 2 独立复评；复评通过前不得进入 L02。

## Round 2

- verdict: `changes_requested`。
- Round 1 的 H-01、H-02、H-03、H-04、M-01、M-02、M-03 均确认 fixed。
- H-05 reopen 为 R2-H-01：master 提交前错误使用旧 `HEAD` 检查 `.agents`。
- disposition: fixed。`governance-plan.md` 已改为 pre-commit index 检查 `git ls-files --stage -- .agents`，post-commit candidate SHA 检查 `git ls-tree -r --name-only "$master_candidate_sha" -- .agents`。
- 当前状态: 等待聚焦 Round 3；通过前不得进入 L02。

## Round 3

- scope: 聚焦复核 R2-H-01。
- disposition: fixed。
- new findings: none。
- verdict: `ready`。
- 退出结论: 后端 L01 方案评审无未处理 high/critical/medium，可进入用户确认 checkpoint；尚未授权或执行 L02 merge。

## Round 4 目标漂移重开

- 2026-07-16 合并前 fetch 发现 `upstream/main` 从 `c8803713` / `v7.2.77` 前进到 `09da52ad` / `v7.2.80`。
- 新增 8 commits，涉及 plugin path、usage generate、xAI image usage/schema、gitstore 和 Codex incomplete/error conversion。
- merge-tree 更新为 `c87eda197a6866db8ed902c4a74305b3ee1da9fe`；仍有 11 个机械冲突，但 `codex_executor.go` 替换 `sdk/cliproxy/usage/manager.go` 成为冲突，重叠路径增至 46。
- Round 3 的 `ready` 仅对旧目标有效；当前状态重开为 pending independent drift review，复评通过前不得发送合并确认或进入 L02。

## Round 4 结果

- verified: 8-commit drift、118/118 matrix、46/46 ledger、11 conflict/35 auto。
- P04-H-01: Codex explicit incomplete 与 missing terminal 语义需要三路区分。Disposition `fixed`，已新增成功 incomplete、request-scoped missing terminal、upstream failed/error 三路契约。
- P04-M-01: Gitstore signing 缺少 direct proof。Disposition `fixed`，已要求 `commit.gpgsign=true -> EnsureRepository -> false -> commit/push` 测试及 `go mod tidy -diff`。
- verdict: `changes_requested`；等待 P05 聚焦复评。

## Round 5

- P04-H-01 Codex terminal 三路契约: fixed。
- P04-M-01 Gitstore signing direct proof: fixed。
- new findings: none。
- verdict: `ready`。
- 退出结论: 漂移后固定目标 `09da52ad` / `v7.2.80` 的方案评审无未处理 high/critical/medium，可进入用户确认 checkpoint；尚未执行 L02 merge。
