# .agents 工作区

本目录用于保存当前仓库的持久化任务上下文与最小索引。

Persistence Mode: git-visible

## 目录职责

- `registry/`：仓库级稳定索引与验证入口。
- `tasks/`：活跃任务上下文、计划、发现与进度。
- `agents/`：多 agent 共享或私有工作区。
- `scratch/`：临时输出，不作为长期事实来源。
- `archive/`：已完成或中止任务的归档。

## 当前活跃任务

- `20260331-auth-file-batch-check`：认证文件批量检查与汇总展示。
- `20260403-absorb-arron-usage-persistence`：吸收参考仓库 usage 快照恢复与周期持久化能力。
- `20260408-auth-zero-quota-auto-disable`：额度查询型认证文件在额度真实耗尽后自动禁用。
- `20260527-auth-quota-threshold-auto-disable`：在零额度自动禁用基础上新增全局额度阈值禁用。
- `20260624-active-quota-refresh-pool`：为低额度自动禁用新增后端活跃额度刷新池设计与实施计划。
- `20260702-batch-quota-query-parity`：将批量检查正式额度查询调整为复用 canonical quota query service，并对齐单文件刷新展示字段。
- `20260409-fork-install-docker-self-hosting`：补齐 fork 自有 Docker 发布链路与仓库内 Linux 安装更新脚本。
- `20260409-provider-scoped-routing-pool`：按供应商类别独立启用的范围轮询设计与后续实现。
- `20260424-absorb-cliproxyapi2-fixes`：制定 CLIProxyAPI2 改动吸收计划并沉淀分批执行方案。
- `20260424-evaluate-watcher-race-current-architecture`：评估当前 watcher 架构下是否存在与参考仓库同类竞态，并决定是否另起实施任务。
- `20260612-sync-upstream-v7-fork-customizations`：规划前后端吸收最新上游版本，同时保留 fork 自定义功能。
- `20260626-backend-upstream-v7-2-42`：后端独立吸收 `upstream/main@b05a27e4` / `v7.2.43`，已完成验证、`dev`/`master` 推送与 release tag `v7.2.43-wx-2.6`。

## 说明

- 默认使用中文记录正文。
- 不在此目录存放敏感信息、令牌、Cookie 或私密配置。
