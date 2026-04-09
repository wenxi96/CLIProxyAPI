# .agents 工作区

本目录用于保存当前仓库的持久化任务上下文与最小索引。

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
- `20260409-fork-install-docker-self-hosting`：补齐 fork 自有 Docker 发布链路与仓库内 Linux 安装更新脚本。
- `20260409-provider-scoped-routing-pool`：按供应商类别独立启用的范围轮询设计与后续实现。

## 说明

- 默认使用中文记录正文。
- 不在此目录存放敏感信息、令牌、Cookie 或私密配置。
