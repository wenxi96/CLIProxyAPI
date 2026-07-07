# 上游更新吸收清单

## 基线

- 当前仓库：CLIProxyAPI 后端 fork
- 当前分支：`master`
- 当前 integration_branch：`dev@32d6be097045e2cd7abbcb94cee0fdbb6fcee8b4`
- 当前 release_branch：`master@d304d60b9550c3642a36c0517f8da2077c08bf88`
- 当前 fork 发布标签：`v7.2.49-wx-2.10`
- 上游目标：`upstream/main`
- 上游目标 SHA：`8b9c4da2452b42aaa917a80daadf72aadc843a13`
- 上游最新 tag：`v7.2.51`
- 增量范围：`f8334be82755113acce3f4a9fb03adc6c1313529..8b9c4da2452b42aaa917a80daadf72aadc843a13`

## 汇总

- 上游新增提交数：14
- `dev...upstream/main`：fork 侧 123 个提交；上游侧 14 个提交
- `master...upstream/main`：fork 侧 141 个提交；上游侧 14 个提交
- 触达模块：README/assets、safe mode、WebsocketAuth 默认配置、quota backoff、Google Interactions、translator、runtime executor、SDK handlers、config watcher、management config API、`internal/api/server.go`
- 是否存在机械冲突：是，`internal/api/server.go`
- 是否存在行为冲突风险：是，主要集中在 server 路由注册、safe mode middleware、fork 管理端点、usage 统计开关、interactions API 路由并存
- 建议结论：可以进入候选合并前确认阶段；真实合并需用户授权，并建议隔离 worktree 处理。

## 逐项清单

### 1. `c1b952da` feat(docs): add Claude API sponsorship information to README files

- 更新内容：新增 Claude API 赞助说明和 `assets/claudeapi.png`。
- 影响模块：README 多语言文档与 assets。
- 功能作用：展示赞助商信息。
- 风险：与 fork README 自定义内容自动合并，低风险。
- 与 fork 自定义能力关系：无运行时影响。
- 建议处理：吸收，保留 fork README 现有内容。

### 2. `00787ef9` fix(docs): correct link formatting for Claude API sponsorship in README

- 更新内容：修正 README 链接格式。
- 影响模块：README。
- 功能作用：修正文档展示。
- 风险：低。
- 与 fork 自定义能力关系：无。
- 建议处理：吸收。

### 3. `87c091e2` fix(docs): correct formatting and wording for Claude API sponsorship in README files

- 更新内容：修正多语言 README 中 Claude API 赞助文案。
- 影响模块：README / README_CN / README_JA。
- 功能作用：文案一致性。
- 风险：低。
- 与 fork 自定义能力关系：无。
- 建议处理：吸收。

### 4. `ac21758e` feat(docs): add Code0 sponsorship information to README files in English, Chinese, and Japanese

- 更新内容：新增 Code0 赞助说明和 `assets/code0.png`。
- 影响模块：README 多语言文档与 assets。
- 功能作用：展示赞助商信息。
- 风险：低。
- 与 fork 自定义能力关系：无。
- 建议处理：吸收。

### 5. `9e9c2442` Merge pull request #4095 from router-for-me/readme-add

- 更新内容：合并赞助商 README 更新。
- 影响模块：文档。
- 功能作用：汇总文档变更。
- 风险：低。
- 与 fork 自定义能力关系：无。
- 建议处理：随对应文档提交吸收。

### 6. `5afc0f1d` fix(translator): remove temperature parameter handling in Claude request transformations

- 更新内容：调整 Claude 相关 translator 与 executor 测试，移除/修正 temperature 参数处理。
- 影响模块：`internal/runtime/executor/claude_executor*`、`internal/translator/claude/*`。
- 功能作用：对齐上游 Claude 请求转换语义。
- 风险：中；触碰 translator，需确认不会破坏 fork 兼容路径。
- 与 fork 自定义能力关系：间接影响 Claude/OpenAI/Gemini 转换。
- 建议处理：吸收后跑 Claude translator/executor 聚焦测试。

### 7. `df080389` fix: allow management access in example API key safe mode

- 更新内容：把示例 API key 安全模式从 warning-only server 改为 middleware / server option，允许管理端访问并阻断代理 API。
- 影响模块：`cmd/server/main.go`、`internal/api/server.go`、`internal/safemode/*`。
- 功能作用：模板 key 存在时仍可进入管理端修复配置。
- 风险：中高；与 fork 的管理端路由、usage 路由、batch-check 路由同处 `internal/api/server.go`，已经触发冲突。
- 与 fork 自定义能力关系：直接触碰管理端入口和 server option。
- 建议处理：真实合并时保留 safe mode middleware，同时保留 fork 管理 API 路由。

### 8. `49094932` feat(config): default enable WebsocketAuth in LoadConfigOptional and ParseConfigBytes

- 更新内容：默认启用 WebsocketAuth。
- 影响模块：`internal/config/config.go`、`internal/config/parse.go`。
- 功能作用：配置默认值安全性调整。
- 风险：中；可能影响默认配置行为。
- 与 fork 自定义能力关系：间接影响 websocket / wsrelay 认证默认值。
- 建议处理：吸收后跑配置解析测试和 websocket 相关验证。

### 9. `22bb89a4` Merge pull request #4107 from router-for-me/safemode

- 更新内容：合并 safe mode 变更。
- 影响模块：server、cmd、safemode。
- 功能作用：汇总 safe mode。
- 风险：同第 7 项。
- 与 fork 自定义能力关系：server 路由冲突。
- 建议处理：随第 7 项处理。

### 10. `3ef74dce` Merge pull request #4109 from router-for-me/websocket

- 更新内容：合并 WebsocketAuth 默认配置。
- 影响模块：config。
- 功能作用：汇总 websocket 默认值。
- 风险：同第 8 项。
- 与 fork 自定义能力关系：配置默认值。
- 建议处理：随第 8 项处理。

### 11. `270869dd` fix(auth): escalate quota backoff once per cooldown window and jitter cooldown waits

- 更新内容：新增 quota cooldown backoff 测试和 conductor 逻辑调整。
- 影响模块：`sdk/cliproxy/auth/conductor.go`、`sdk/cliproxy/auth/cooldown_backoff_test.go`。
- 功能作用：防止 backoff 频繁升级并抖动等待。
- 风险：中；可能与 fork quota refresh / auto-disable 策略产生行为叠加。
- 与 fork 自定义能力关系：涉及认证调度与额度耗尽路径。
- 建议处理：吸收后跑 auth/quota 相关测试。

### 12. `0d23f791` fix(auth): keep jittered cooldown waits within max-retry-interval

- 更新内容：限制抖动后的 cooldown 不超过最大 retry interval。
- 影响模块：`sdk/cliproxy/auth/conductor.go`、cooldown 测试。
- 功能作用：修正 cooldown 上限。
- 风险：中；同 quota backoff。
- 与 fork 自定义能力关系：认证调度。
- 建议处理：与第 11 项一起验证。

### 13. `4a2a3b29` Merge pull request #4117 from router-for-me/quota-backoff-guard

- 更新内容：合并 quota backoff guard。
- 影响模块：auth scheduler/conductor。
- 功能作用：汇总 quota backoff 修复。
- 风险：同第 11-12 项。
- 与 fork 自定义能力关系：认证调度。
- 建议处理：随第 11-12 项处理。

### 14. `8b9c4da2` feat(interactions): add support for Google Interactions

- 更新内容：新增 Google Interactions 支持，覆盖 config、management API、server routes、runtime executors、thinking provider、translator、SDK handlers 与测试。
- 影响模块：`internal/translator/*/interactions`、`internal/runtime/executor/*`、`internal/api/server.go`、`internal/config/*`、`sdk/api/handlers/*`、`sdk/cliproxy/*`。
- 功能作用：支持 `/v1beta/interactions` 与相关 provider 转换链路。
- 风险：高；新增大量 translator/runtime 路径，且与 fork server 路由和配置管理扩展同处关键模块。
- 与 fork 自定义能力关系：间接影响 Gemini/Claude/Codex/OpenAI/Antigravity 转换路径，真实合并需特别保护 fork 管理端接口和 usage/quota 扩展。
- 建议处理：吸收但需独立评审；真实合并后跑 interactions 相关测试、translator 测试和全量 Go 验证。
