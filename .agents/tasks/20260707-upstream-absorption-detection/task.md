---
Status: completed
Created: 2026-07-07
Owner: Codex
---

# 前后端上游吸收检测 Dry-Run

## 目标

调用项目级 `upstream-absorption` skill，对后端仓库和配套前端仓库执行一轮上游同步吸收检测干跑，确认当前上游是否存在新内容、需要吸收哪些提交、是否存在预期冲突，以及下一步建议。

## 范围

- 读取本地规则与项目级 skill。
- 确认 `.agents` workspace authority 与当前工作区状态。
- 执行 `git fetch --all --tags --prune` 更新远端引用。
- 固定 `upstream_target_sha`。
- 生成仓库分析、治理方案、上游更新清单和冲突预检报告。
- 按前后端协同规则读取前端本地规则，并在前端仓库独立生成对应检测任务记录。
- 输出前后端检测结论和候选合并前确认清单。

## 非目标

- 不执行真实 `git merge`。
- 不解决冲突。
- 不提交、不推送、不合并 `${release_branch}`。
- 不创建 tag、不触发发布、不部署。
- 不把前端任务 authority 混写到后端仓库。

## 分支变量

- `upstream_branch`: `main`
- `integration_branch`: `dev`
- `release_branch`: `master`

## 授权边界

- 已授权执行检测干跑 和写入本任务治理记录。
- 候选合并、提交、推送、发布分支合入、标签、发布 和部署均需要再次获得用户明确授权。

## 验收条件

- 已记录 fetch 后的上游目标 SHA。
- 已生成上游更新清单。
- 已完成无写入冲突预检。
- 已说明前后端是否建议进入候选合并，以及需要用户确认的点。
