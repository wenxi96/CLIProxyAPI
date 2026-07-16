# Findings

## 已确认事实

- 本任务为新建任务；既有 `20260708-upstream-v7-2-52-absorption` 已完成，不复用其 authority。
- 当前 `dev@1c36ebc5` 与 `origin/dev` 一致，工作区在任务创建前干净。
- 初始目标为 `c8803713` / `v7.2.77`；2026-07-16 漂移检查后固定目标更新为 `upstream/main@09da52ad509e2c18e7b9540db3b98c2214c280aa`，精确 tag 为 `v7.2.80`。
- merge base 为 `v7.2.52@14b13966`；`dev...upstream/main` 为 135/118。
- `origin/main@5b7f2361` 比固定目标少 41 个提交，不能代替 `upstream/main` 作为本轮权威目标。
- 最新 fork release 为 `v7.2.52-wx-2.13`。

## L01 当前结论

- 采用单一固定目标 `v7.2.80`，不逐 tag merge；冲突按 release、usage、xAI/Codex、auth/plugin/store 切片解决。
- 11 个机械冲突和 35 个自动热点均已纳入 46-path ledger。
- Usage parser 不合成 reported total；tier-only 保留 metadata 但 `UsageObserved=false`；cache alias 不双算。
- Auth 选择采用 kind-before-pool，401 refresh 保持 singleflight 和 fork quota/scoped identity。
- `origin/main` 在用户授权后仅 fast-forward 到固定目标；master candidate 必须通过 index/SHA 两级 `.agents` 门禁和非 `.agents` 业务树等价检查。
- v7.2.77 三轮方案评审作为历史证据；目标漂移后 P04/P05 已完成 8-commit 增量和新冲突集合复评，最终 `ready`，可进入用户确认。
