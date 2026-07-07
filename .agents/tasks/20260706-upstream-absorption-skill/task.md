---
Status: completed
Created: 2026-07-06
Owner: Codex
---

# 上游吸收流程 Skill 治理任务

## 目标

将当前项目中“检测上游更新、同步上游更新、拉取合并上游内容、梳理合并更新清单、解决冲突、多轮评审修复、生成报告、提交推送、合并、最终验证链路、申请或执行发版”的完整流程沉淀为项目级 skill，后续吸收上游时可直接调用。

## 范围

- 设计并创建一个项目级 skill，命名为 `upstream-absorption`。
- skill 覆盖后端仓库和前端仓库的上游吸收通用流程。
- skill 要求所有吸收清单、冲突报告、评审报告、验证报告和发版核验证据落入对应仓库 `.agents/tasks/<task-id>/` 任务目录。
- 在当前仓库生成本次 skill 设计治理文档，留存决策和证据。

## 非目标

- 本任务不实际吸收新的上游代码。
- 本任务不提交、不推送、不发版。
- 本任务不改写既有上游吸收任务历史记录。
- 本任务不处理前后端现存业务代码缺陷。

## 授权边界

- 当前已允许生成仓库内治理文档。
- 项目级 canonical skill 位于 `.agents/skills/upstream-absorption/SKILL.md`。
- Claude Code 兼容 wrapper 位于 `.claude/skills/upstream-absorption/SKILL.md`，但 canonical 内容仍以 `.agents/skills/` 为准。
- 后续运行该 skill 时，任何提交、推送、合并 `master`、创建 tag、触发 release 或部署类外部副作用都必须单独获得用户明确授权。
- 不在治理文档、skill 或报告中写入密钥、Token、Cookie 或私密配置。

## 验收条件

- 已形成拟创建 skill 的名称、位置、触发描述、文件结构和核心流程。
- 已把设计报告写入本任务 `evidence/` 目录。
- 用户确认后，已创建 skill 并通过 skill 校验。
- 创建完成后，已更新本任务进度和交接记录，明确如何调用该 skill。
