# 后端仓库分析报告（目标漂移后更新至 v7.2.80）

## 本地规则

- 入口规则: `AGENTS.md` 与项目级 `.agents/skills/upstream-absorption/SKILL.md`。
- 验证命令: changed Go files `gofmt`；聚焦测试；Docker Go 1.26 `go test ./...`；server build；diff/conflict scan。
- 禁止/限制项: `.agents` 只进入 `dev`；`master` 当前树必须无 `.agents`；不得单独重构 translator；不得泄露凭证；不得用整侧 ours/theirs 覆盖语义冲突。

## 分支与远端

- 当前分支: `dev@1c36ebc54f939b15cd3765fee233a75a6f5aeb6d`
- origin: `wenxi96/CLIProxyAPI`
- upstream: `router-for-me/CLIProxyAPI`
- origin main mirror: `origin/main@5b7f2361`，是固定目标祖先，落后 `09da52ad` 41 个提交，可 fast-forward；推送前需用户授权。
- 集成分支: `dev`
- 发布分支: `master@5f1c36461513bc555e93823112992f3cb876c938`
- 上游主分支: `main`
- 上游目标 SHA: `09da52ad509e2c18e7b9540db3b98c2214c280aa` / `v7.2.80`
- merge base: `14b139661d98acbbd7ac19eb827754e78118736f` / `v7.2.52`
- 分叉: fork 独有 135 commits，上游新增 118 commits；上游变更 216 files、+18,825/-2,363。
- 漂移增量: `c8803713..09da52ad` 新增 8 commits、48 files、+1,891/-212，覆盖 plugin path、generate usage flag、xAI image usage、gitstore client、xAI schema 与 Codex incomplete response。
- release topology: `master...dev = 22/5`；当前两分支非 `.agents` 业务树等价。后续必须分别固定 dev/master candidate SHA，并重新证明非 `.agents` 树等价。

## Release 链路

- 版本脚本: `scripts/version.sh auto-release`，必须在实际 master candidate 上运行。
- GitHub Actions: `.github/workflows/release.yaml` 与 `docker-image.yml` 均由 `v*` tag 触发。
- Release 资产: 多平台归档与 `checksums.txt`。
- 镜像: `ghcr.io/wenxi96/cli-proxy-api` amd64/arm64 manifest。
- 发版前必须核验: master 无 `.agents`、版本脚本、测试/构建、远端 refs、Actions、assets/checksums、GHCR aliases。

## Fork 定制保护点

| 能力 | 文件/符号 | 风险 | 验证 |
|---|---|---|---|
| usage v2 请求事实与唯一终态 | `internal/usage/**`; `internal/runtime/executor/helps/usage_helpers.go`; provider executors | 上游 service tier/cache/xAI 改动与本地 token facts 冲突 | usage/helper/executor/redisqueue 全套测试及全量测试 |
| 凭证 usage queue 与脱敏 | `internal/redisqueue/plugin.go`; management usage API | 上游 tier 字段可能绕过 canonical detail 或恢复敏感字段 | queue payload、导入导出和脱敏回归测试 |
| request ID/client IP 与 deferred logging | `internal/logging/**`; SDK handlers | 上游 deferred body capture 可能破坏本地 metadata 生命周期 | logging/request context 测试 |
| 批量额度、阈值禁用和刷新池 | management auth-files、`internal/authquota/**` | 上游 auth/OAuth/config 重构可能改变启停和额度入口 | authquota、management handler 聚焦测试 |
| scoped routing / alias / home mapping | `sdk/cliproxy/auth/conductor.go`; config | 上游 Home force mapping、Alpha Search auth selection 与本地 pool 选择叠加 | auth selection、alias/scoped pool 测试 |
| 插件、Home、安装与发布定制 | pluginhost/store、`cmd/server`、`.github/workflows/**`、`scripts/**` | 上游模型刷新和 release 修复可能覆盖 fork tag-only/GHCR/GLIBC 资产链 | workflow 静态检查、server/plugin tests、发布 dry-run |

## 当前工作区

- 脏改: 仅当前新任务 `.agents` 治理文件；业务代码无修改。
- 无关改动处理: 无无关脏改，不覆盖历史任务。
- 是否需要隔离 worktree: 是。L02 持续代码写入和真实 merge 必须在 linked worktree 中完成，并绑定 canonical `.agents`。
