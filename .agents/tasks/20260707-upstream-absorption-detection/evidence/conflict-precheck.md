# 冲突预检报告

## 预检命令

- 命令：`git merge-tree --write-tree dev upstream/main`
- 目标分支：`dev@32d6be097045e2cd7abbcb94cee0fdbb6fcee8b4`
- 上游目标：`upstream/main@8b9c4da2452b42aaa917a80daadf72aadc843a13`
- 退出码：`1`

补充检查：

- 命令：`git merge-tree --write-tree master upstream/main`
- 目标分支：`master@d304d60b9550c3642a36c0517f8da2077c08bf88`
- 退出码：`1`
- 结论：release_branch 也会遇到同一个核心冲突。

## 机械冲突

- 结论：存在 1 个明确内容冲突。
- 文件：`internal/api/server.go`
- merge-tree 输出摘要：
  - `CONFLICT (content): Merge conflict in internal/api/server.go`
  - README / config / cmd / SDK / translator 等多处自动合并。
- 建议：
  - 真实合并时在隔离 worktree 中执行候选 merge。
  - 在 `internal/api/server.go` 同时保留 fork 侧管理路由、usage 统计开关、batch-check / scoped-pool / quota-threshold 路由，以及上游 safe mode / interactions 路由和 server option。
  - 冲突解决后必须跑 server 路由、management、usage、interactions、auth/quota 聚焦测试，再跑全量验证。

## 行为冲突风险

### `internal/api/server.go`

- 风险说明：fork 侧新增 usage 统计开关、management usage 路由、batch-check、quota threshold、scoped-pool 等管理端路由；上游新增 example API key safe mode middleware、`/v1beta/interactions`、interactions-api-key 管理路由、safe mode server option、access config update 行为调整。
- 证据：
  - fork 侧 diff 新增 `usage.SetStatisticsEnabled`、`mgmt.GET("/usage"...`、`mgmt.POST("/auth-files/batch-check"...`、`mgmt.GET("/routing/scoped-pool"...` 等。
  - 上游侧 diff 新增 `safemode` import、`WithExampleAPIKeySafeMode`、`exampleAPIKeySafeModeMiddleware`、`v1beta.POST("/interactions"...`、`mgmt.GET("/interactions-api-key"...` 等。
- 建议解决：
  - 合并 import：同时保留 `internal/usage` 与 `internal/safemode`。
  - 合并 server option state：保留 fork 现有 option 字段并加入上游 safe mode 字段。
  - 合并 middleware：保持 home heartbeat 与 safe mode middleware 顺序，避免管理端修复路径被阻断。
  - 合并 routes：不要覆盖 fork 管理端路由；新增上游 interactions 路由。
  - 合并 `UpdateClients`：同时保留 usage/redisqueue 统计开关更新、safe mode active 状态更新和 interactions key count。

### `internal/translator/*` 与 runtime executor

- 风险说明：上游新增大量 interactions translator/runtime 代码，同时修改 Claude/Gemini/OpenAI/Codex 既有转换逻辑。
- 证据：上游增量中 `internal/translator/*/interactions`、`internal/runtime/executor/gemini_executor.go`、`sdk/api/handlers/gemini/interactions_handlers.go` 等大量新增。
- 建议解决：真实合并后跑 translator/interactions 聚焦测试；由于项目本地规则限制 standalone translator 改动，本次属于 broader upstream absorption，可在整体合并中处理，但需记录原因。

### `sdk/cliproxy/auth/conductor.go`

- 风险说明：上游 quota backoff guard 与 fork auth quota 自动禁用、active quota refresh 逻辑可能交互。
- 证据：上游新增 `sdk/cliproxy/auth/cooldown_backoff_test.go` 并修改 conductor。
- 建议解决：真实合并后跑 auth/quota 相关测试，重点检查 cooldown、禁用、恢复和 refresh 行为。

### `cmd/server/main.go` 与 safe mode

- 风险说明：上游从 warning-only server 切换为 safe mode server option，fork 当前启动参数和 plugin host / management 行为需要保留。
- 证据：上游新增 `api.WithExampleAPIKeySafeMode()` 调用链。
- 建议解决：合并时保持 plugin host 启动路径和 fork 参数，同时传递上游 serverOptions。

## 合并建议

- 建议是否进入候选合并：可以，但必须先由用户确认，并建议在隔离 worktree 中处理。
- 需要用户确认的点：
  - 是否先推送当前 `master` 本地领先的 skill 验证提交，或在真实吸收前改从 `dev` 创建隔离 worktree。
  - 是否接受本轮吸收同时引入 `v7.2.50` / `v7.2.51`，尤其是 Google Interactions 大范围 translator/runtime 新增。
  - 是否同意将 `internal/api/server.go` 冲突解决作为重点评审对象。
