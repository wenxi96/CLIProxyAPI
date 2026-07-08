---
Status: merged_to_master
Created: 2026-07-08
Owner: Codex
Execution Route: upstream_absorption
---

# 后端上游吸收：v7.2.52

## 目标

吸收 `router-for-me/CLIProxyAPI` 上游 `upstream/main@14b139661d98acbbd7ac19eb827754e78118736f` / `v7.2.52` 到 fork 的 `dev` 分支，保留现有 fork 定制、治理目录和发布分支规则。

## 范围

- 仓库：`CLIProxyAPI`
- 集成分支：`dev`
- 发布分支：`master`
- 上游分支：`upstream/main`
- 上游目标：`14b139661d98acbbd7ac19eb827754e78118736f`
- 上游标签：`v7.2.52`

## 非目标

- 本阶段不处理前端吸收；前端本轮检测无新增上游提交。
- 未经用户确认前，不执行候选合并。
- 未经用户明确授权前，不提交代码、不推送代码、不合入 `master`、不创建标签、不发版。
- 不将 `.agents` 治理记录合入 `master`。

## 当前状态

- 候选合并已执行：`git merge --no-commit --no-ff 14b139661d98acbbd7ac19eb827754e78118736f`。
- 实际合并无机械冲突，冲突标记扫描为空。
- 已发现并修复 stream usage 在后续读流错误时可能被 failure 记录抢占的问题。
- 聚焦验证、全量验证、构建和合并后复评已完成。
- 代码合并提交已创建：`148a4425 merge(upstream): 吸收 v7.2.52`。
- `dev` 已推送：`origin/dev@a638e2ab2ecb972500e628d8382ae9c0afda0984`。
- `master` 已合入并推送：`origin/master@9c53e7472bf61b4a6e8f78fce4a29d49d1795afb`。
- `master` 当前树 `.agents` 文件数为 0。

## 验收条件

- 合并前：完整更新清单、冲突预检、治理方案和方案评审已完成并获得用户确认。
- 合并后：冲突解决报告、聚焦验证、全量验证和多轮评审闭环完成。
- 提交推送：仅在用户授权后提交并推送 `dev`。
- 发布分支：仅在用户授权后合入 `master`，且 `master` 当前树不包含 `.agents`。
- 发版：仅在用户授权后按仓库发布规则执行并核验。
