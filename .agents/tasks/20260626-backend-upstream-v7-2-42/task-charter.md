# T01 后端吸收上游 v7.2.42

## 任务摘要

将后端 `CLIProxyAPI` 的 `dev` 分支从当前 fork 集成状态吸收到最新上游镜像 `origin/main == upstream/main == 4c0c6029` / `v7.2.42`，在保留 fork 定制的前提下完成计划、独立审核修复、代码合并和验证。

## 成功定义

- 新任务计划和提交级吸收清单已落地在本任务目录。
- 后端方案已经过独立审核修复流程，评审结论无阻断问题。
- `dev` 成功吸收 `v7.2.42`，冲突解决符合 fork 定制保留策略。
- 后端验证通过，至少覆盖 `go test ./...` 与 `go build -buildvcs=false -o /tmp/cli-proxy-api-check ./cmd/server` 的等价验证路径。
- 未经用户授权不推送 `dev` / `master`，不创建 tag，不触发 release。

## 非目标

- 不处理前端仓库；前端使用独立任务 `20260626-frontend-upstream-v1-17-7`。
- 不把本任务与旧的跨仓库 `20260612-sync-upstream-v7-fork-customizations` 混写。
- 不在计划和审核阶段修改业务代码。
- 不执行 push、tag、release、部署或运行实例更新，除非用户后续明确授权。

## 约束

- 分支模型：`main` 为上游镜像，`dev` 为集成分支，`master` 为稳定发版分支。
- 当前任务先在 `dev` 上推进；`master` 仅在 `dev` 验证通过后再评估。
- 代码写入前必须完成方案审核修复流程。
- 多 agent 审核仅允许 read-only 或 evidence-only，业务代码写入只能由主执行者在审核通过后开始。
- 不写入密钥、token、Cookie 或私密配置。

## 执行模式

- Execution Mode: supervised
- Auto-Continue Between Loops: no
- Auto-Continue Between Tasks: no
- User Authorization Confirmed: no

## Task Success Criteria

- Criterion: 计划和提交级吸收清单完整落地。
  - Verification: 检查 `task-charter.md`、`plans/2026-06-26-backend-upstream-v7-2-42-implementation-plan.md`、`findings.md`、`ulw-board.md`、`loops/`。
  - Pass Criterion: 文件存在，路径归属本仓库，且不引用前端作为当前执行 authority。
- Criterion: 审核修复流程完成且无阻断问题。
  - Verification: 检查 `coordination/` 或 `progress.md` 中的 reviewer/verifier 结论和主线程裁决。
  - Pass Criterion: 所有阻断项已修正或明确降级为非阻断，并记录证据。
- Criterion: 后端 `dev` 吸收 `v7.2.42` 后验证通过。
  - Verification: Docker Go 或本地 Go 执行 `go test ./...` 和 `go build -buildvcs=false -o /tmp/cli-proxy-api-check ./cmd/server`。
  - Pass Criterion: 命令 exit 0，且无未解决冲突标记。

## 风险与未知

- `sdk/cliproxy/auth/conductor.go` 同时承载 fork scoped-pool 与上游 OAuth model alias force-mapping，冲突解决必须保持两条逻辑链。
- `cmd/server/main.go` 的 Home 插件同步需要同时保留 fork runtime defaults 和上游 sync report。
- 主机无 Go 工具链时需使用 Docker Go 验证。

## 全局停止条件

- `origin/main` / `upstream/main` 再次漂移。
- 发现 fork 定制被上游重构覆盖且无法通过小范围整合保留。
- 验证环境不可用且无法用 Docker 等价替代。
- 需要 push、tag、release、部署或外部凭证。

## Loop 策略

- L01：任务文档和实施计划落地，建立提交级吸收清单和初始治理状态。
- L02：独立方案审核修复，先由 reviewer/verifier 检查方案和冲突策略，主线程修正文档到无阻断。
- L03：审核通过后执行代码合并和冲突解决。
- L04：验证、收口、交接，并等待用户授权是否推进 `master` / push。

## 状态权威源

- live 状态以 `ulw-board.md` 为准。
- 机器可读状态以 `ulw-state.json` 为准。
- 计划权威以 `plans/2026-06-26-backend-upstream-v7-2-42-implementation-plan.md` 为准。

## 状态指针

- Loop 状态：以 `ulw-board.md` 为准。
- 当前阶段：以 `ulw-board.md` 为准。
