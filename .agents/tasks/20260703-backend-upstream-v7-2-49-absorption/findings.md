# 发现记录

## 2026-07-03 初始上游状态

- `git fetch --all --tags --prune` 后，`origin/main` 已更新到 `f8334be8`，与 `upstream/main` 一致。
- `dev...upstream/main` 计数为 `117 10`，表示 fork 侧有 117 个非上游提交，上游侧有 10 个提交需要按 merge 语义吸收。
- `master...upstream/main` 计数为 `131 10`。
- `v7.2.46..upstream/main` 包含 6 个实际变更提交与 4 个上游 PR merge commit。
- `git merge-tree --write-tree dev upstream/main` 退出码为 `0`，后端未发现机械内容冲突。

## 上游吸收项摘要

1. `150e7f0d` 修复 force-mapped Responses SSE 在 WebSocket forwarder 中的帧粘连和尾部 flush 问题。
2. `8f686345` 修复 WS-to-SSE Codex 路径的完整 transcript replay 与 pinned auth 释放逻辑。
3. `611d65ea` 为 OpenAI Responses 转换逻辑补充 `delta.reasoning` fallback。
4. `956ce7cf` 增加 Claude Sonnet 5 模型元数据，并调整 Claude thinking 模式下的采样参数规整。
5. `e1302645` 扩展 public plugin host SDK 的 auth provider 与注册插件元数据方法。
6. `f8334be8` 更新多语言 README 中 VisionCoder 链接。

## 需要保护的 fork 能力

- 近期 `20260702-batch-quota-query-parity` 的批量额度查询与单文件刷新对齐逻辑。
- 额度自动禁用、活跃额度刷新池和 provider scoped routing 相关能力。
- fork 自有 CI / release / installer / `.agents` 治理文件。
- upstream direct diff 中显示的大量 fork-only 文件删除不应按覆盖式 diff 处理，应以 merge 语义保留。

## 2026-07-03 后端合并验证结论

- 已实际执行 `git merge --no-commit --no-ff upstream/main`，未产生提交。
- 合并候选包含上游 `v7.2.49` 的运行时修复、registry 更新、pluginhost SDK 扩展与 README 链接更新。
- 本机缺少 `go` 命令，后端验证使用 Docker `golang:1.26`。
- 聚焦测试覆盖上游触达模块：`sdk/cliproxy/auth`、`sdk/api/handlers/openai`、`internal/runtime/executor`、`internal/registry`，结果通过。
- 仓库要求的构建验证 `go build -o test-output ./cmd/server && rm test-output` 已用 Docker 等价执行并通过。
- 自评审前补充执行 `go test -buildvcs=false ./...`，结果通过。
