# .agents 工作区

本目录用于保存当前仓库的持久化任务上下文与最小索引。

Persistence Mode: git-visible

## 目录职责

- `registry/`：仓库级稳定索引与验证入口。
- `skills/`：项目级 skill / 流程卡入口，供支持 `.agents/skills` 的 agent 自动发现或按规则读取。
- `tasks/`：活跃任务上下文、计划、发现与进度。
- `workers/`：worker 本地草稿，默认可丢弃。
- `reports/`：仓库级审计或评审报告。
- `scratch/`：临时输出，不作为长期事实来源。
- `archive/`：已完成或中止任务的归档。

## 当前活跃任务

- `20260331-auth-file-batch-check`：认证文件批量检查与汇总展示。
- `20260403-absorb-arron-usage-persistence`：吸收参考仓库 usage 快照恢复与周期持久化能力。
- `20260408-auth-zero-quota-auto-disable`：额度查询型认证文件在额度真实耗尽后自动禁用。
- `20260527-auth-quota-threshold-auto-disable`：在零额度自动禁用基础上新增全局额度阈值禁用。
- `20260624-active-quota-refresh-pool`：为低额度自动禁用新增后端活跃额度刷新池设计与实施计划。
- `20260702-batch-quota-query-parity`：将批量检查正式额度查询调整为复用 canonical quota query service，并对齐单文件刷新展示字段；已作为后续 Codex 展示对齐修复的前置基础。
- `20260703-codex-batch-quota-display-parity`：修复 Codex 批量检查把月度窗口误展示为 5 小时、并出现空周额度的问题；已提交到 `dev@61d34dfd`、合入 `master@766ec81c`，并随 `v7.2.49-wx-2.9` 发布。
- `20260703-auth-usage-token-cost-statistics`：规划按认证文件 `auth_index` 记录和聚合请求 token、估算金额，并提供单认证文件调用明细 API。
- `20260706-upstream-absorption-skill`：沉淀当前项目上游吸收、冲突处理、评审验证、提交推送、合并 master 与发版申请/核验流程为项目级 skill。
- `20260707-upstream-absorption-detection`：调用项目级 `upstream-absorption` skill 执行后端上游吸收检测干跑，固定上游目标、生成更新清单并完成无写入冲突预检。
- `20260707-upstream-v7-2-51-absorption`：后端独立吸收 `upstream/main@8b9c4da2` / `v7.2.51`；已提交到 `dev@148089b3`、合入 `master@d02d8926`，并随 `v7.2.51-wx-2.11` 发布。
- `20260708-upstream-absorption-detection`：再次检测后端上游状态，固定 `upstream/main@14b13966` / `v7.2.52`，确认存在 7 个新增上游提交且无机械冲突输出。
- `20260409-fork-install-docker-self-hosting`：补齐 fork 自有 Docker 发布链路与仓库内 Linux 安装更新脚本。
- `20260409-provider-scoped-routing-pool`：按供应商类别独立启用的范围轮询设计与后续实现。
- `20260424-absorb-cliproxyapi2-fixes`：制定 CLIProxyAPI2 改动吸收计划并沉淀分批执行方案。
- `20260424-evaluate-watcher-race-current-architecture`：评估当前 watcher 架构下是否存在与参考仓库同类竞态，并决定是否另起实施任务。
- `20260612-sync-upstream-v7-fork-customizations`：规划前后端吸收最新上游版本，同时保留 fork 自定义功能。
- `20260626-backend-upstream-v7-2-42`：后端独立吸收 `upstream/main@b05a27e4` / `v7.2.43`，已完成验证、`dev`/`master` 推送与发布标签 `v7.2.43-wx-2.6`。
- `20260703-backend-upstream-v7-2-49-absorption`：后端独立吸收 `upstream/main@f8334be8` / `v7.2.49`；已提交到 `dev@7cd99f73`、合入 `master@766ec81c`，并随 `v7.2.49-wx-2.9` 发布。

## 说明

- 默认使用中文记录正文。
- 项目级 skill 主入口位于 `.agents/skills/<skill-name>/SKILL.md`；Claude Code 兼容 wrapper 可放在 `.claude/skills/<skill-name>/SKILL.md`，但 canonical 内容仍以 `.agents/skills/` 为准。
- 代码类改动：先提交并推送到 `dev`，再合并到 `master` 并推送 `master`。
- `.agents` 治理文档类改动：只提交并推送到 `dev`，不得合入或污染 `master`。
- `.agents/` 治理记录只在 `dev` 集成分支维护；`master` 稳定发布分支当前树必须保持不包含 `.agents`。
- 不在此目录存放敏感信息、令牌、Cookie 或私密配置。
