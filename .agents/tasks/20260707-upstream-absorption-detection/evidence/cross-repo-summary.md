# 前后端上游吸收检测汇总

## 范围

本轮按项目级 `upstream-absorption` skill 执行双仓库检测干跑：

- 后端仓库：CLIProxyAPI
- 前端仓库：Cli-Proxy-API-Management-Center

本轮只检测、梳理和预检冲突，不执行真实 merge，不解决冲突，不提交，不推送，不合并发布分支，不创建 tag，不触发发布。

## 后端检测结果

- 当前分支：`master`
- integration_branch：`dev`
- release_branch：`master`
- 上游分支：`upstream/main`
- 上游目标 SHA：`8b9c4da2452b42aaa917a80daadf72aadc843a13`
- 上游最新 tag：`v7.2.51`
- 增量基线：`f8334be82755113acce3f4a9fb03adc6c1313529`
- 上游新增提交数：14
- `dev...upstream/main`：fork 侧 123 个提交；上游侧 14 个提交
- `master...upstream/main`：fork 侧 141 个提交；上游侧 14 个提交

主要更新内容：

- README / assets 增加 Claude API 与 Code0 赞助信息。
- Claude translator / executor 调整 temperature 处理。
- example API key safe mode 改为 server option / middleware，允许管理端修复配置。
- 默认启用 WebsocketAuth。
- quota cooldown backoff 调整，避免重复升级与超出 max retry interval。
- 新增 Google Interactions 支持，触达 config、management API、server routes、runtime executors、thinking provider、translator、SDK handlers 与测试。

冲突与影响：

- `git merge-tree --write-tree dev upstream/main`：退出码 `1`，冲突文件 `internal/api/server.go`。
- `git merge-tree --write-tree master upstream/main`：退出码 `1`，冲突文件同为 `internal/api/server.go`。
- 冲突核心是 fork 管理端路由、usage、batch-check、scoped-pool、quota-threshold 与上游 safe mode / Google Interactions 路由和 server option 同文件叠加。

建议：

- 真实吸收前重新 fetch 并核验上游目标 SHA 是否仍为 `8b9c4da2452b42aaa917a80daadf72aadc843a13`。
- 建议使用隔离 worktree 处理候选合并。
- 冲突解决时同时保留 fork 管理路由与上游 safe mode / interactions 能力。
- 合并后必须运行 server、management、usage、auth/quota、interactions、translator 聚焦验证，再运行全量 Go 验证。

## 前端检测结果

- 当前分支：`dev`
- integration_branch：`dev`
- release_branch：`master`
- 上游分支：`upstream/main`
- 上游目标 SHA：`4064b01ac3a67be825495a1da8adf7534790d755`
- 上游最新 tag：`v1.17.10`
- 增量基线：`e9817a8ce1a4cde785bccc63df378e355075e6a7`
- 上游新增提交数：8
- `dev...upstream/main`：fork 侧 72 个提交；上游侧 8 个提交
- `master...upstream/main`：fork 侧 79 个提交；上游侧 8 个提交

主要更新内容：

- quota config 调整无有效 limit 时的 amount 显示。
- 新增 ClaudeAPI provider 相关配置、icon、descriptor、表单默认 base URL 和 i18n。
- Sponsor provider 增加 Gemini 通道。
- provider workbench 调整隐藏品牌与过滤逻辑。
- 新增 Code0 provider 顺序和资源定义，并保持 Code0 / ClaudeAPI 分组可见。
- 新增 xAI pay-as-you-go quota progress。

冲突与影响：

- `git merge-tree --write-tree dev upstream/main`：退出码 `1`，冲突文件 `src/features/providers/adapters.ts` 与 `src/features/providers/sheets/forms/BaseProviderForm.tsx`。
- `git merge-tree --write-tree master upstream/main`：退出码 `1`，冲突文件相同。
- 冲突核心是 fork DisplayName 定制与上游 ClaudeAPI / Code0 / Gemini sponsor provider 逻辑同文件叠加。

建议：

- 真实吸收前重新 fetch 并核验上游目标 SHA 是否仍为 `4064b01ac3a67be825495a1da8adf7534790d755`。
- 建议使用隔离 worktree 处理候选合并，避免混入前端仓库已有历史 `.agents` 治理改动。
- 冲突解决时保留 fork `displayName` / `fallbackIdentifier` 行为，同时叠加上游 ClaudeAPI、Code0 和 sponsor provider 泛化逻辑。
- 合并后必须回归 provider workbench、provider form、DisplayName 展示、认证文件额度卡片、xAI/Grok quota progress，并运行前端 lint/typecheck/build。

## 总体结论

- 两个仓库均存在新的上游更新需要吸收。
- 两个仓库的 `dev` 与 `master` 对上游目标均存在 merge-tree 内容冲突。
- 当前不建议直接在现有工作区进入真实合并；建议用户确认后，分别使用隔离 worktree 做候选合并和冲突解决。
- 本轮检测支持“可进入候选合并前确认阶段”的结论，不支持“已吸收上游”的结论。

