# 项目级 upstream-absorption skill 覆盖复核

## 仓库梳理摘要

- 当前仓库本地规则要求 Go 1.26+，后端修改后至少执行 `go test ./...` 和 `go build -o test-output ./cmd/server && rm test-output`。
- 仓库分支模型在既有吸收任务中稳定表现为：`dev` 为集成分支，`master` 为稳定发版分支，`upstream/main` 为上游目标。
- Release 链路由 `scripts/version.sh`、`scripts/release-lib.sh`、`.github/workflows/release.yaml` 和 `.github/workflows/docker-image.yml` 共同承载；版本号依赖当前 HEAD 可达 tag，发版前必须在实际发版提交上核验。
- `.agents/README.md` 声明 `Persistence Mode: git-visible`；上游吸收清单、治理方案、评审报告、验证报告和发版核验证据应落入 `.agents/tasks/<task-id>/`。
- 既有 `20260626-backend-upstream-v7-2-42` 任务证明：复杂上游吸收需要先计划和方案评审，再进入实际合并；reviewer 曾发现 fork 定制保护清单、验证策略和冲突不变式不足，必须先修复方案再合并。

## 覆盖矩阵

| 用户要求流程 | 原 skill 覆盖 | 本轮补强后覆盖 | 说明 |
|---|---|---|---|
| 分析仓库 | 部分 | 已覆盖 | 新增 `仓库分析` 阶段和 `repository-analysis.md` 模板。 |
| 梳理新一轮治理方案 | 部分 | 已覆盖 | 新增 `新一轮治理方案` 阶段和 `governance-plan.md` 模板。 |
| 针对吸收方案多轮评审修复 | 不足 | 已覆盖 | 新增 `吸收方案多轮评审` 阶段和 `plan-review-report.md` 模板，要求 high/critical 先修复再复评。 |
| 落地本地治理文档 | 已覆盖 | 已覆盖 | 保持所有报告落入 `.agents/tasks/<task-id>/`。 |
| 开始吸收 | 已覆盖 | 已覆盖 | 候选合并阶段保留授权门禁。 |
| 完成后评审 | 部分 | 已覆盖 | 验证和评审阶段补强为合并后评审循环。 |
| 最终复核评审多轮直到没有新的问题 | 不足 | 已覆盖 | 新增 review loop 退出条件：最后一轮无新增 finding，所有 high/medium 已处理，low/nit 已修复、标记不适用或作为用户认可剩余风险记录。 |
| 提交合并 | 已覆盖 | 已覆盖 | 保留 dev 提交推送、master 合入和远端核验。 |
| 最后发版 | 已覆盖 | 已覆盖 | 保留申请发版/执行发版分支和发布后核验。 |

## 结论

原 skill 能覆盖主要执行链路，但对“合并前治理方案评审”和“合并后多轮评审直到无新问题”的强制性不足。本轮已补强为 13 阶段流程，并补充对应报告模板。补强后的退出标准要求最后一轮无新增 finding，且所有问题均已修复、标记不适用或作为用户认可的剩余风险记录。补强后满足用户描述的完整上游吸收治理链路。
