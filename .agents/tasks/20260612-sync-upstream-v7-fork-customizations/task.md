# 任务说明

## 目标

在保留 fork 自定义功能的前提下，将后端 CLIProxyAPI 与前端 Cli-Proxy-API-Management-Center 吸收到当前上游基线，并完成发布前的联合验证与交接记录。

## 当前权威入口

- Canonical Plan Path: `plans/2026-06-12-sync-upstream-v7-fork-customizations-implementation-plan.md`
- Live Status Authority: `progress.md`
- Handoff Authority: `handoff.md`
- Findings Authority: `findings.md`
- Evidence Directory: `evidence/`

## 范围

- 后端仓库：`/home/cheng/git-project/CLIProxyAPI`
- 前端仓库：`/home/cheng/git-project/Cli-Proxy-API-Management-Center`
- 后端目标基线以执行期 freshness 为准；2026-06-23 fresh fetch 发现最新 `upstream/main@bd646819ed95` / `v7.2.29`。该上游已应用到当前本地未提交 merge 候选（`MERGE_HEAD=bd646819ed95`），`cmd/server/main.go` 与 `sdk/cliproxy/service.go` 冲突已解决并 staged；因尚未创建 merge commit，`HEAD` 仍为 `dev@b8ee828c6e0b`，最终提交 / push / release 前必须先完成用户允许后的后端编译验证与提交收口。
- 前端目标基线以执行期 freshness 为准；2026-06-23 fresh fetch 显示 `origin/main == upstream/main == ed4124ff3b24` / `v1.17.1`，当前 `dev@b60462dc1d33` 已包含该上游。
- 保留并验证 fork 定制：
  - 默认管理面板源
  - scoped pool / scoped poll
  - quota auto-disable 与阈值自动禁用
  - usage persistence 与 Usage 页面
  - Auth Files 批量检查与 ZIP 下载
  - DisplayName
  - fork tag-only release 策略与版本后缀
- 前端旧任务 `20260527-sync-upstream` 仅作为 predecessor/reference，不作为当前 canonical plan。

## 非目标

- 不自动提交、推送、创建或推送 tag。
- 不触发 GitHub release、部署、上传或发布 `management.html`。
- 不写入凭证、token、Cookie 或私密配置。
- 不保留 AMP/Ampcode 兼容；该能力已按用户确认跟随上游移除。
- 不把迁移历史任务的旧 skip 决策直接当作当前任务约束；旧决策仅作为审计参考。

## 验收条件

- 后端 `go test ./...` 通过。
- 后端 `go build -o test-output ./cmd/server && rm test-output` 通过。
- 前端 `bun install --frozen-lockfile` 通过。
- 前端 `bun run build` 通过。
- 后端 scoped pool selection、quota auto-disable、usage persistence、plugin callback 非递归等 fork 关键断言未被上游吸收覆盖。
- 前端 DisplayName、Auth Files 批量检查、Scoped Pool / Scoped Poll、ZIP 下载、plugin management / store、fullscreen / error logs、logs pagination、xAI / Grok quota、video、websocket、API key exclusion 完成人工或自动验证。
- Usage 页面可展示总请求、总 token、总成本等统计。
- Usage 图表可切换 `7h` / `24h` / `7d` / `all` 时间范围，并持久化选择。
- Usage 图表线可选择并持久化。
- Usage 模型统计和 API 统计可正确展示。
- Usage 导出 / 导入快照功能可用。
- Usage 相关 4 语言文案齐全：`en`、`ru`、`zh-CN`、`zh-TW`。
- Usage 页面移动端响应式布局可用。
- 后端 `/v0/management/usage`、`/v0/management/usage/export`、`/v0/management/usage/import` 与前端类型和 API 调用一致。
- 后端 quota auto-disable 新旧 API 命名保持兼容：新端点使用 `on-low-quota`，旧 `on-zero-quota` 端点继续可用。
- 后端 quota auto-disable 配置命名保持当前低额度语义：主配置键为 `quota-exceeded.auto-disable-auth-file-on-low-quota`，旧 `auto-disable-auth-file-on-zero-quota` 仅作为兼容读取 / 旧 API 路由保留，保存配置时收敛到新 key。
- 前端 VisualConfigEditor / `/config` transformer / API 调用使用 low-quota 主命名，读取旧 zero-quota 字段兼容，保存 YAML 时移除旧 zero-quota key。
- 合入 master 后，前端 `a02ebbcbf69549b87e81054151eba02d1ade59cb` 随 `dev -> master` 流动。
- 推送、tag、release 与 management.html 上传继续停在用户授权门禁。

## 约束

- `main` 只同步上游；`dev` 吸收并验证；`master` 只接收稳定结果。
- 继续前必须执行 freshness 检查；如上游漂移，停止写入并刷新计划 / findings。
- 发现未确认冲突、凭证需求、真实外部发布需求或验证不可复现时，暂停并请求用户确认。
- `.agents` 为 `git-visible` 持久化工作区，任务记录应保留在本任务目录下。

## 当前自定义功能清单

- 后端清单：`evidence/fork-custom-feature-inventory-2026-06-23.md`
- 前端清单：`/home/cheng/git-project/Cli-Proxy-API-Management-Center/.agents/tasks/20260612-sync-upstream-v7-fork-customizations/evidence/fork-custom-feature-inventory-2026-06-23.md`
- 清单内容：两仓清单均包含 baseline reference method、upstream absorption static checklist、fork feature preservation matrix 和逐项功能说明。
- 清单结论：当前代码中 fork 自定义功能的静态保留核对通过；前端最新 fetched 上游已包含在 `dev`；后端最新 fetched 上游已应用到未提交 merge 候选且冲突已解决，但编译 / 构建 / 测试验证按用户要求暂缓，因此尚不能声明最终可提交 / 可发布。
