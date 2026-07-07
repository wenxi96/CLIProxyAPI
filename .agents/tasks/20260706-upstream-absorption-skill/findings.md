# 发现记录

## 2026-07-06 仓库和治理状态

- 当前后端仓库 canonical `.agents` 位于仓库根目录，当前执行面是主工作树，不是 linked worktree。
- `.agents/README.md` 声明 `Persistence Mode: git-visible`，本次治理文档可写入 `.agents/tasks/` 并纳入后续提交候选。
- 当前仓库存在未提交的上一轮发版收口治理记录，不能使用 `git add .` 或混入本任务提交。
- 当前分支为 `dev`，远端包含 fork `origin` 和上游 `upstream`。
- 后端分支模型在既有吸收任务中体现为：`upstream/main` 为上游目标，`dev` 为集成分支，`master` 为稳定发版分支。
- 后端 release 号由 `scripts/version.sh` / `scripts/release-lib.sh` 根据当前 HEAD 可达的上游基线 tag 和 fork tag 计算；运行位置不同可能导致 `dev` 与 `master` 上计算结果不同，发版前必须在实际发版提交上核验。

## 2026-07-06 可复用历史经验

- `20260703-backend-upstream-v7-2-49-absorption` 已沉淀后端上游吸收流程：刷新远端、提交级清单、`merge-tree` 预检、候选合并、聚焦验证、全量测试、自评审、提交推送、合入 master、tag 发版、Actions 和资产核验。
- `20260626-backend-upstream-v7-2-42` 已沉淀更严格的长任务模式：先生成计划和提交级吸收清单，再进行独立审核修复，审核通过后执行代码合并和验证。
- 后续项目级 skill 应复用这些稳定步骤，但不能写死某一次版本号、提交号或本机绝对路径。

## 2026-07-06 项目级 skill 位置判断

- Codex 和 Gemini CLI 均可使用项目级 `.agents/skills/<skill-name>/SKILL.md` 作为跨 agent 项目 skill 入口。
- Claude Code 的项目级 skill 入口通常是 `.claude/skills/<skill-name>/SKILL.md`；因此本项目采用 `.agents/skills/` 作为 canonical 位置，并在 `.claude/skills/` 放兼容 wrapper。
- 不采用 `.skills/` 或顶层 `skills/`，因为它们不是当前主流 agent 的稳定项目级自动发现约定。
