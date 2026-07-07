---
name: upstream-absorption
description: "Project-level workflow for CLIProxyAPI upstream absorption. Use when analyzing the repository, planning a new absorption governance round, detecting upstream updates, reviewing the absorption plan, merging upstream into dev, resolving conflicts, running repeated review and verification loops until no new findings remain, pushing, merging master, and preparing or executing releases for this project."
---

# Upstream Absorption

## 定位

这是本项目级上游吸收流程卡，主入口位于 `.agents/skills/upstream-absorption/SKILL.md`。它只服务当前 CLIProxyAPI fork 及用户明确纳入同一吸收批次的配套前端仓库，不作为全局 skill 使用。

使用本 skill 时，所有任务状态、清单、冲突说明、评审、验证和发版核验证据必须落入对应仓库的 `.agents/tasks/<task-id>/`。不要把任务报告写入临时 `docs/`，也不要把前端任务 authority 混写到后端仓库。

## 入口门禁

开始前必须完成：

- 读取当前仓库 `AGENTS.md`、`CLAUDE.md` 或等价本地规则。
- 按 `.agents` 工作区治理规则确认 canonical `.agents`、`Persistence Mode`、当前是否 linked worktree。
- 检查 `git status --short`，识别无关脏改；不得覆盖、回退或混入用户未授权改动。
- 判断当前请求是新建吸收任务还是恢复已有任务。已 closeout 的历史吸收任务默认不复用。
- 新建或恢复 `.agents/tasks/<task-id>/`，至少维护 `task.md`、`findings.md`、`progress.md`、`handoff.md`、`evidence/`。

提交、推送、合并 `master`、创建 tag、触发 release、部署或任何外部副作用，必须先获得用户明确授权。用户只要求“检查、梳理、建议”时，不执行这些动作。

## 分支变量

默认分支变量：

- `upstream_branch`: 上游主分支，默认按远端实际情况解析，当前后端通常为 `main`。
- `integration_branch`: 集成分支，默认 `dev`，若仓库本地规则不同则以本地规则为准。
- `release_branch`: 发布分支，默认 `master`，若仓库本地规则不同则以本地规则为准。

治理方案、更新清单、冲突预检、候选合并、提交推送、发布分支合入、release candidate gate 和远端核验必须使用同一组分支变量。若使用非默认分支，必须在治理方案和对应报告中记录理由。

## 标准流程

1. 仓库分析
   - 梳理仓库本地规则、分支模型、远端关系、release 脚本、CI workflow、验证命令、`.agents` 持久化状态和当前脏改。
   - 梳理 fork 自定义能力保护点，至少覆盖最近吸收任务、近期业务改动、release/install/CI 定制、前后端联动点。
   - 报告写入 `evidence/repository-analysis.md`。

2. 新一轮治理方案
   - 为本次吸收创建清晰的治理方案，说明目标、范围、非目标、分支/发版策略、授权边界、任务拆分、停止条件、验证策略和评审策略。
   - 普通吸收可使用标准任务模式；跨多阶段、多仓库或高风险冲突时，升级为 ULW 或等价长任务治理。
   - 方案写入 `plans/<date>-upstream-absorption-plan.md` 或 `evidence/governance-plan.md`，并在 `task.md` / `task-charter.md` 中引用。

3. 检测上游状态
   - 执行 `git fetch --all --tags --prune`。
   - 记录当前分支、`origin`、`upstream`、`integration_branch`、`release_branch`、`upstream_branch` 和最新 tag。
   - 记录上游目标 SHA：`upstream_target_sha="$(git rev-parse upstream/${upstream_branch})"`，后续清单、预检、评审和合并都必须引用该 SHA。
   - 计算 `${integration_branch}...upstream/${upstream_branch}`、`${release_branch}...upstream/${upstream_branch}`、最近 fork release tag 到上游目标的增量。
   - 将稳定事实写入 `findings.md`，动作写入 `progress.md`。

4. 生成上游更新清单
   - 使用 `git log --reverse`、`git show --stat`、`git diff --name-status` 梳理每个上游提交。
   - 每项说明：更新内容、影响模块、功能作用、潜在风险、是否触碰 fork 自定义区域、建议吸收策略。
   - 报告写入 `evidence/upstream-update-inventory.md`。

5. 冲突预检
   - 优先执行 `git merge-tree --write-tree ${integration_branch} ${upstream_target_sha}` 做无写入预检。
   - 必要时使用隔离 worktree 执行候选 merge，不在不清楚的脏工作区里直接合并。
   - 区分机械冲突、行为冲突、验证风险和发版风险。
   - 写入 `evidence/conflict-precheck.md`。

6. 吸收方案多轮评审
   - 实际合并前，必须对仓库分析、更新清单、冲突预检、治理方案和验证策略做至少一轮方案评审。
   - 评审可以由主线程自评审完成；若存在高风险冲突、跨仓库吸收、fork 自定义能力保护、或用户要求多轮评审，必须使用只读 reviewer / subagent 或等价独立检查。
   - 对每个发现写明 disposition：`fixed`、`accepted_risk`、`not_applicable` 或 `blocked`。
   - 若发现 high/critical 或会改变合并策略的问题，先修复治理方案和清单，再复评；不得直接进入候选合并。
   - 退出条件：最后一轮方案评审无新增 finding；无未处理 high/critical；所有 finding 均为 `fixed`、`not_applicable` 或经用户认可的 `accepted_risk`。
   - 若存在 medium 及以上 `accepted_risk`，必须在发送确认清单时显式披露，并等待用户确认后再进入候选合并。
   - 报告写入 `evidence/plan-review-report.md`。

7. 发送确认清单
   - 候选合并前默认必须输出完整吸收清单、冲突点和建议解决方案，等待确认。
   - 每项必须说明“更新了什么、影响什么模块、起什么作用、冲突是什么、建议如何处理”。
   - 只有用户明确要求直接合并、且不存在未处理 finding 或未披露 `accepted_risk` 时，才允许记录确认清单豁免；豁免原因必须写入 `progress.md` 和 `evidence/plan-review-report.md`。

8. 候选合并
   - 在授权后，将上游目标合入 `${integration_branch}`。
   - 合并前必须重新执行 `git fetch --all --tags --prune` 并核验 `git rev-parse upstream/${upstream_branch}` 是否仍等于已评审的 `upstream_target_sha`。
   - 若上游目标 SHA 发生变化，停止合并，回到“生成上游更新清单 -> 冲突预检 -> 吸收方案多轮评审 -> 发送确认清单”。
   - 推荐使用 `git merge --no-commit --no-ff <upstream_target_sha>` 形成可审查候选；若使用分支名，必须在命令前后记录分支名解析到的 SHA。
   - 逐文件解决冲突，并记录处理原则和实际选择。
   - 写入 `evidence/conflict-resolution-report.md`；无冲突也要明确记录。

9. 验证和合并后评审循环
   - 先跑聚焦验证，再按风险扩大到全量验证。
   - 必跑 `git diff --check` 和 `rg -n "^(<<<<<<<|=======|>>>>>>>)" .`。
   - 后端默认验证：`go test ./...` 与 `go build -o test-output ./cmd/server && rm test-output`；本机无 Go 时可用 Docker Go 等价命令并说明。
   - 前端默认验证：读取前端仓库规则后执行其 install、lint、typecheck、build、测试或页面验证命令。
   - 至少执行主线程自评审；复杂冲突、高风险模块或用户要求时，进行多轮只读评审并闭环修复。
   - 评审循环规则：每轮评审发现问题后，先修复或明确降级，再重新运行与修复匹配的验证和复评。
   - 退出条件：最后一轮复评无新增 finding；无未处理 high/critical；无未处理 medium；low/nit 必须修复、标记不适用，或记录为用户认可的剩余风险。
   - 最后一次修复后必须至少再跑一轮复评；不能用“已修复上一轮问题”替代最终无新增问题的证明。
   - 不得把“测试通过”当作“评审无问题”；两者都需要记录。
   - 写入 `evidence/review-report.md` 和 `evidence/verification-report.md`。
   - 多轮评审的轮次和退出结论写入 `evidence/post-merge-review-loop.md`。

10. 提交和推送
   - 仅在用户明确授权后执行。
   - 提交前必须确认 `evidence/post-merge-review-loop.md` 的退出结论为可提交，且最新验证报告仍匹配当前候选。
   - 精确暂存本任务相关文件和合并候选文件，不使用会混入无关改动的 `git add .`。
   - 提交到 `${integration_branch}`，推送 `origin/${integration_branch}`。
   - 用 `git ls-remote --heads origin ${integration_branch}` 或等价方式核验远端分支指向。

11. 合入发布分支
   - 仅在 `${integration_branch}` 验证通过且用户明确授权后执行。
   - 将 `${integration_branch}` 合入 `${release_branch}`，推送 `origin/${release_branch}`。
   - 核验 `origin/${release_branch}` 指向预期提交，并确认 `${release_branch}` 包含本次吸收提交。
   - 写入 release candidate gate：记录 `master_release_candidate_sha`，并在该 SHA 上完成发版前复验或等价性证明。
   - 发版前复验至少包含版本脚本输出、`git diff --check`、冲突标记扫描，以及仓库要求的构建/测试；若 `master_release_candidate_sha` 与已完成全量验证的 `${integration_branch}` SHA 完全一致，可记录同一 SHA 证据作为测试等价性证明。

12. 发版申请或执行
   - 若用户要求“申请发版”，只输出 release candidate、目标 tag、变更摘要、验证证据和剩余风险，等待确认。
   - 若用户明确授权发版，按仓库 release 规则在实际发版提交上计算 tag。
   - 后端 CLIProxyAPI 特别注意：`scripts/version.sh auto-release` 依赖当前 HEAD 可达 tag，必须在实际发版提交或 detached `${release_branch}` 提交上核验，不能只看 `${integration_branch}` 上的输出。
   - 创建 tag 前必须确认 `master_release_candidate_sha` 已通过 release candidate gate；tag 必须指向该 SHA。
   - 创建并推送 tag 后，核验 GitHub Actions、Release 资产、校验和、Docker/GHCR manifest 或前端发布资产。
   - 写入 `evidence/release-verification-report.md`。

13. 收口
   - 更新 `task.md` 状态、`progress.md`、`handoff.md`，必要时创建 `closeout.md`。
   - 最终答复必须区分：代码改动、治理记录、提交状态、推送状态、`master` 合并状态、发版状态、验证证据和剩余风险。

## 前后端协同规则

- 后端仓库和前端仓库各自维护自己的 `.agents/tasks/<task-id>/`。
- 每个参与仓库都必须独立读取本地规则，确认 canonical `.agents`、`Persistence Mode`、linked worktree 状态、分支变量和 `git status --short`；任一仓库无法确认时，停止写入该仓库治理记录并等待用户确认。
- 同一轮用户要求同时处理前后端时，先分别梳理清单，再汇总给用户确认。
- 前端仓库没有本 skill 时，仍必须读取前端仓库本地规则，并将前端报告落入前端仓库 `.agents/tasks/`。
- 不把后端 `AGENTS.md` 的 Go 验证命令套用到前端；不把前端构建命令套用到后端。

## 报告模板

需要写报告时，读取 `references/report-templates.md`。只读取需要的模板，不要把所有模板原文复制进最终回复。

## 完成前检查

声称完成、可提交、已推送、已合并、可发版或已发版前，必须有新的验证证据。最低检查包括：

- `git status --short`
- `git diff --check`
- 冲突标记扫描
- 仓库规则要求的测试或构建
- 远端分支、tag、Actions、Release 或资产核验，按本轮实际动作选择

如果无法执行某项验证，必须说明原因和剩余风险。
