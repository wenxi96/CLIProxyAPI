# 仓库分析报告

## 本地规则

- 入口规则：用户明确要求调用项目级 `upstream-absorption` skill；已读取 `.agents/skills/upstream-absorption/SKILL.md` 与 `AGENTS.md`。
- 验证命令：后端默认要求 `go test ./...` 与 `go build -o test-output ./cmd/server && rm test-output`；本轮为检测干跑，未进入真实合并候选验证阶段。
- 禁止/限制项：不真实合并、不提交、不推送、不合入 `${release_branch}`、不创建 tag、不触发发布、不部署。

## 分支与远端

- 当前分支：`master`
- origin：`https://github.com/wenxi96/CLIProxyAPI.git`
- upstream：`https://github.com/router-for-me/CLIProxyAPI.git`
- 集成分支（integration_branch）：`dev`
- 发布分支（release_branch）：`master`
- 上游主分支（upstream_branch）：`main`；`git remote show upstream` 显示 HEAD branch 为 `main`
- 上游目标 SHA：`8b9c4da2452b42aaa917a80daadf72aadc843a13`

## 发布链路

- 版本脚本：`scripts/version.sh auto-release`，按项目级 skill 要求必须在实际发版提交或 detached `${release_branch}` 提交上核验。
- GitHub Actions：仓库存在 release / docker image 工作流，真实发版时需核验对应 run。
- Release 资产：真实发版时需核验 GitHub Release 资产、校验和、Docker/GHCR manifest。
- 发版前必须核验：`master_release_candidate_sha`、版本脚本输出、`git diff --check`、冲突标记扫描、构建/测试。

## Fork 定制保护点

| 能力 | 文件/符号 | 风险 | 验证 |
|---|---|---|---|
| 管理端批量认证文件检查 | `internal/api/handlers/management/auth_files*`; `internal/api/server.go` 路由 | 上游路由改动与 fork 管理路由集中在 `internal/api/server.go`，真实合并需保留 fork 路由 | merge-tree 预检已定位 `internal/api/server.go` 冲突 |
| 凭证 token / 金额统计 | `internal/usage/*`; `internal/api/handlers/management/usage.go`; `internal/api/server.go` | 上游 interactions API 与 safe mode 也修改 server 路由和配置更新，需合并路由与统计开关逻辑 | 真实合并后跑 usage 聚焦测试与全量 Go 验证 |
| auth quota / 自动禁用 | `internal/authquota/*`; `sdk/cliproxy/auth/*` | 上游新增 quota backoff guard，可能与 fork quota refresh / auto-disable 策略相互影响 | 真实合并后跑 auth/quota 相关测试 |
| scoped routing pool | `internal/api/handlers/management/routing_scoped_pool*`; `internal/api/server.go` | server 路由冲突解决时不能丢失 scoped pool 管理端点 | 冲突解决报告中逐项核对 |
| 项目级治理 skill | `.agents/skills/upstream-absorption/*`; `.claude/skills/upstream-absorption/SKILL.md` | 上游不包含 fork `.agents`，真实合并不得误删治理记录 | 只按 merge 结果保留 fork 本地治理目录 |
| fork install / release 自定义 | `install/`; `.github/workflows/*`; `scripts/version.sh` | 上游无 fork 发布规则，真实合并需保护 fork 发版链路 | 发布分支 gate 与 发布核验 |

## 当前工作区

- 脏改：本轮任务新增 `.agents/tasks/20260707-upstream-absorption-detection/`，并更新 `.agents/README.md` 活跃任务入口。
- 无关改动处理：`.claude/settings.local.json`、`.codegraph/`、`.codex`、`.tmp-dev/`、`auths/test-batch-check-50/`、`config.yaml` 为 ignored 本机文件，不纳入本轮。
- 是否需要隔离 worktree：若用户授权进入真实候选合并，建议使用隔离 worktree 或先确保当前治理记录已提交/暂存隔离；检测干跑 阶段不需要。

## 额外观察

- 当前 `master` 本地领先 `origin/master` 1 个提交：`d304d60b docs(skill): 验证上游吸收流程卡`。
- `upstream/dev` 更新到 `3aa42a6f`，但本轮按上游 HEAD branch `main` 固定目标，不把 `upstream/dev` 混入本轮。
