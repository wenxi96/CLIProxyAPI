# 项目级 Skill 设计报告

## 创建位置

- Skill 名称：`upstream-absorption`
- Canonical 目录：`.agents/skills/upstream-absorption`
- Claude Code 兼容目录：`.claude/skills/upstream-absorption`
- 资源结构：`SKILL.md` + `agents/openai.yaml` + `references/report-templates.md`；Claude wrapper 只负责指向 canonical 文件。

说明：skill 正文和引用文档不写死当前机器绝对路径；只使用项目相对路径或 `<repo-root>`、`<task-dir>`、`<frontend-repo>`、`<backend-repo>` 等占位符。

## 触发描述

当用户要求检测上游更新、同步/拉取/合并上游内容、梳理上游更新清单、判断和解决冲突、多轮评审修复、生成治理报告、提交推送、合并 `master`、申请或执行发版、发布后核验时使用。适用于 fork 仓库从 upstream 吸收更新，尤其是 CLIProxyAPI 后端和管理中心前端的 dev -> master -> release 流程。

## 核心原则

- 每个仓库独立治理：前端和后端分别使用各自仓库的 `.agents/tasks/<task-id>/`，不混写任务 authority。
- 先清单后合并：先刷新远端、计算增量、提交级梳理影响，再进行合并预检。
- 先预检后写入：实际合并前先使用 `merge-tree` 或隔离 worktree 预检机械冲突和行为冲突。
- 保留 fork 定制：冲突解决优先保留 fork 自定义能力，同时吸收上游 bugfix、模型/依赖/构建更新。
- 评审必须闭环：至少执行自评审，复杂冲突或高风险模块需要多轮评审复核并记录修复。
- 报告必须入治理目录：吸收清单、冲突报告、评审报告、验证报告和发布核验证据写入 `<task-dir>/evidence/`。
- 外部副作用显式授权：提交、推送、合并 `master`、tag、release、部署必须在执行前获得用户明确授权。

## 标准流程

1. 入口治理
   - 读取仓库 `AGENTS.md` / `CLAUDE.md` / README 和 `.agents/README.md`。
   - 确认 canonical `.agents`、持久化模式、当前分支、远端、工作区脏改和是否 linked worktree。
   - 新建或恢复对应 `.agents/tasks/<task-id>/`，写入 `task.md`、`findings.md`、`progress.md`、`handoff.md` 和 `evidence/`。

2. 上游状态检测
   - 执行 `git fetch --all --tags --prune`。
   - 记录 `origin`、`upstream`、当前 `dev` / `master`、上游主分支和最新上游 tag。
   - 计算 `dev...upstream/<branch>`、`master...upstream/<branch>`、上次 fork release tag 到上游目标的增量。

3. 更新清单报告
   - 使用 `git log --reverse`、`git show --stat`、`git diff --name-status` 梳理每个上游提交。
   - 每项说明：更新内容、影响模块、功能作用、潜在风险、是否触碰 fork 自定义区域、建议吸收策略。
   - 报告落入 `<task-dir>/evidence/upstream-update-inventory.md`。

4. 冲突预检和解决建议
   - 使用 `git merge-tree --write-tree <target-branch> upstream/<branch>` 或临时 worktree 预检。
   - 区分机械冲突、行为冲突、验证风险和 release 风险。
   - 给出逐项解决建议，写入 `<task-dir>/evidence/conflict-precheck.md`。
   - 若用户要求先确认，清单发送给用户后等待确认再实际合并。

5. 候选合并
   - 确认工作区脏改归属；不覆盖用户未授权改动。
   - 在 `dev` 或约定集成分支执行 `git merge --no-commit --no-ff upstream/<branch>`，必要时使用隔离 worktree。
   - 解决冲突后记录每个冲突文件的处理原则和实际结果。

6. 验证和多轮评审
   - 执行仓库规定的格式、lint、测试、构建和冲突标记扫描。
   - 至少执行：`git diff --check`、`rg -n "^(<<<<<<<|=======|>>>>>>>)" .`、仓库核心测试、仓库构建命令。
   - 对高风险模块执行聚焦测试，再执行全量或等价验证。
   - 进行自评审；复杂场景可调用 reviewer / subagent 做只读评审，所有发现必须修复或明确降级。
   - 报告落入 `<task-dir>/evidence/review-report.md` 和 `<task-dir>/evidence/verification-report.md`。

7. 提交和推送
   - 获得明确授权后，只暂存本任务相关文件和合并候选文件，避免混入无关脏改。
   - 提交到 `dev`，推送 `origin/dev`。
   - 推送后核验远端 `dev` 指向预期提交。

8. 合入 master
   - 在 `dev` 验证通过且获得授权后，将 `dev` 合入 `master`。
   - 推送 `origin/master`。
   - 核验 `origin/master` 指向预期提交，且 `master` 包含本次 `dev` 提交。

9. 申请或执行发版
   - 若用户只要求“申请发版”，输出 release candidate、目标 tag、变更摘要、验证证据和剩余风险，等待确认。
   - 若用户明确授权执行发版，按仓库 release 规则计算 tag，在实际发版提交上核验版本号，创建并推送 tag。
   - 发布后核验 GitHub Actions、Release 资产、校验和、Docker/GHCR manifest 或前端静态资产，按仓库类型记录。

10. 收口
   - 更新 `task.md` 状态、`progress.md`、`handoff.md`、必要时创建 `closeout.md`。
   - 最终答复必须区分：代码改动、治理记录、提交/推送状态、合并状态、发版状态、验证证据和剩余风险。

## 必须生成的报告

- `evidence/upstream-update-inventory.md`：上游新增内容逐项清单。
- `evidence/conflict-precheck.md`：冲突预检和解决建议。
- `evidence/conflict-resolution-report.md`：实际冲突解决记录；无冲突也要说明。
- `evidence/review-report.md`：多轮评审发现、处理和结论。
- `evidence/verification-report.md`：测试、构建、冲突标记扫描和发布前验证。
- `evidence/release-verification-report.md`：发版后 Actions、资产、镜像或前端产物核验。
- `closeout.md`：任务最终收口摘要。

## 验证建议

后端 CLIProxyAPI：
- `gofmt -w <changed-go-files>`
- `go test ./...` 或 Docker Go 等价验证
- `go build -o test-output ./cmd/server && rm test-output`
- `git diff --check`
- `rg -n "^(<<<<<<<|=======|>>>>>>>)" .`

前端管理中心：
- 读取前端仓库本地规则后执行其规定的 install、lint、typecheck、build 或测试命令
- 使用浏览器或静态产物验证关键页面，尤其是上游吸收触达 UI 时
- 检查 `management.html` 或发布资产是否上传成功

## 需要用户确认

## 项目级位置决策

- Codex 和 Gemini CLI 可使用 `.agents/skills/<skill-name>/SKILL.md` 作为项目级 skill 入口。
- Claude Code 项目级入口使用 `.claude/skills/<skill-name>/SKILL.md`，因此本项目提供 wrapper。
- `.skills/` 和顶层 `skills/` 不作为本项目 canonical skill 入口。
