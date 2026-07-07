# Findings

## 已确认事实

- 本任务是后端真实上游吸收执行任务，不复用已完成的检测干跑 任务。
- 检测干跑 已确认后端 `upstream/main` 目标为 `8b9c4da2452b42aaa917a80daadf72aadc843a13`，最新 tag 为 `v7.2.51`。
- 检测干跑 已确认 `dev` / `master` 对上游目标均会在 `internal/api/server.go` 出现内容冲突。
- 当前主工作树检出 `master`，且有本轮治理记录未提交；真实合并必须在隔离 worktree 中进行。
- 后端 CodeGraph 可用，已用于读取 `internal/api/server.go` 的当前 fork 侧关键逻辑。

## 待确认 / 待关闭

- 候选合并后 `internal/api/server.go` 的 safe mode、interactions、management routes、usage、batch-check、scoped-pool、quota-threshold 是否全部保留。
- 验证通过后实际提交、推送和发版核验是否能一次通过。
