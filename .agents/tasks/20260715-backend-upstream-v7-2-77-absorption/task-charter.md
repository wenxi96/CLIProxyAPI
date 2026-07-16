# T01 后端吸收上游 v7.2.80

## 任务摘要

检测、评审并安全吸收后端 `upstream/main@09da52ad509e2c18e7b9540db3b98c2214c280aa` / `v7.2.80`，保留 fork 自定义能力，并按 `dev -> master -> tag/release` 规则分阶段推进。任务初始目标为 `v7.2.77`，因合并前 fetch 漂移按 skill 门禁重定向到当前目标。

## 成功定义

- 仓库分析、118 个上游提交的分组清单、冲突预检和治理方案已落地并通过独立评审。
- 用户确认吸收清单后，在隔离执行面完成候选合并、冲突解决、多轮代码评审和全量验证。
- 获得对应外部副作用授权后，代码提交推送到 `dev`，仅代码进入 `master`；如执行发版，则 tag、Actions、Release 与 GHCR 均完成核验。

## 非目标

- 不在本任务维护前端 authority；前端使用独立任务 `20260715-frontend-upstream-v1-18-3-absorption`。
- 不顺带吸收评审目标 SHA 之后的新提交。
- 不在 L01 修改 Go 业务代码、合并上游、提交、推送、打 tag 或发版。
- 不删除 fork 定制来规避冲突。

## 约束

- `upstream_branch=main`、`integration_branch=dev`、`release_branch=master`。
- `origin/main` 是上游镜像分支；当前可 fast-forward 41 个提交到固定目标，候选合并前需单独授权推送并核验 `origin/main == upstream/main == upstream_target_sha`。
- `.agents` 只允许进入 `dev`；`master` 当前树必须保持无 `.agents`。
- 候选合并前必须输出完整清单、冲突与建议，并获得用户确认。
- 合并前必须重新 fetch 并确认目标仍为 `09da52ad509e2c18e7b9540db3b98c2214c280aa`。
- 持续代码写入必须在通过治理门禁的隔离 worktree 中执行。

## 执行模式

- Execution Mode: supervised
- Auto-Continue Between Loops: no
- Auto-Continue Between Tasks: no
- User Authorization Confirmed: yes
- Authorization Scope: 上游检测、治理落盘和方案评审；候选合并及后续外部副作用仍按 checkpoint 单独确认。

## Task Success Criteria

- Criterion: 上游更新清单和冲突预检完整且绑定固定 SHA。
  - Verification: 检查 `evidence/upstream-update-inventory.md`、`evidence/conflict-precheck.md` 与 `git rev-parse upstream/main`。
  - Pass Criterion: 清单覆盖 118 个上游提交的可审查分组，所有冲突和 fork 保护点有处理建议，目标 SHA 一致。
- Criterion: 候选合并通过评审和验证。
  - Verification: `evidence/post-merge-review-loop.md`、`evidence/verification-report.md`、risk-to-proof 聚焦矩阵、race 子集、Docker Go 1.26 全量测试与 server build。
  - Pass Criterion: 最后一轮评审无新增 finding，无未处理 medium 及以上问题；关键测试函数未因冲突解决丢失；聚焦、race、全量测试和构建退出码为 0。
- Criterion: 分支与发布边界正确。
  - Verification: 远端 refs、dev/master candidate SHA、非 `.agents` 树等价 diff、`git ls-tree -r master -- .agents`、Actions/Release/GHCR 证据。
  - Pass Criterion: `dev` 保留治理记录，`master` 无 `.agents` 且业务树等价；版本与发布门禁在实际 master candidate 上通过。

## 风险与未知

- 上游跨度为 `v7.2.52..v7.2.80`，包含 118 个提交；fork 自 `v7.2.52` 后有 135 个独有提交。
- 最近 usage v2 改动广泛触达 executor、usage、logging 和 redis queue，可能与上游 provider/runtime 改动形成行为冲突。
- release、插件、安装脚本、批量额度、阈值禁用和 scoped routing 等 fork 定制必须逐项保护。

## 全局停止条件

- `upstream/main` SHA 漂移。
- 出现未处理 high/critical，或 medium accepted risk 未获用户确认。
- fork 定制保护策略无法证明，或 merge-tree/验证环境不可用。
- 需要超出当前授权的 push、master 合入、tag、release、部署或破坏性操作。

## Loop 策略

- L01：仓库分析、更新清单、冲突预检、治理方案与独立方案评审，结束于用户确认 checkpoint。
- L02：用户确认后在隔离 worktree 执行候选合并和冲突解决。
- 后续 loop 仅在 L02 闭环后创建，用于合并后评审验证、提交推送和可选发版，避免提前伪造状态。

## 状态权威源

- live 状态以 `ulw-board.md` 为准。
- 机器可读状态以 `ulw-state.json` 为准。
- 任务静态边界以本文件为准。
- 治理方案以 `evidence/governance-plan.md` 为准。

## 状态指针

- 当前 loop 与 phase：见 `ulw-board.md`。
