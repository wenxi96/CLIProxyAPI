# Upstream Absorption Report Templates

本文件只在需要生成报告时读取。报告正文默认写入当前任务目录的 `evidence/`，并使用中文。

## repository-analysis.md

```md
# 仓库分析报告

## 本地规则

- 入口规则：
- 验证命令：
- 禁止/限制项：

## 分支与远端

- 当前分支：
- origin：
- upstream：
- 集成分支（integration_branch）：
- 发布分支（release_branch）：
- 上游主分支（upstream_branch）：
- 上游目标 SHA：

## Release 链路

- 版本脚本：
- GitHub Actions：
- Release 资产：
- 镜像或静态产物：
- 发版前必须核验：

## Fork 定制保护点

| 能力 | 文件/符号 | 风险 | 验证 |
|---|---|---|---|

## 当前工作区

- 脏改：
- 无关改动处理：
- 是否需要隔离 worktree：
```

## governance-plan.md

```md
# 上游吸收治理方案

## 目标

## 范围

## 非目标

## 分支/发版策略

- upstream_branch：
- integration_branch：
- release_branch：
- release candidate gate：
- tag / release 触发条件：
- 分支策略例外及理由：

## 授权边界

- 允许：
- 需要再次确认：
- 禁止：

## 任务拆分

- 后端仓库任务：
- 前端仓库任务：
- 共享确认点：
- 不纳入本轮的改动：
- 跨仓库证据落点：

## 阶段拆分

1. 仓库分析：
2. 新一轮治理方案：
3. 检测上游状态：
4. 生成上游更新清单：
5. 冲突预检：
6. 吸收方案多轮评审：
7. 发送确认清单：
8. 候选合并：
9. 验证和合并后评审循环：
10. 提交推送：
11. 发布分支合入：
12. 发版申请或执行：
13. 收口：

## 评审策略

- 方案评审触发条件：
- 独立评审 / 子代理触发条件：
- 合并后评审轮次：
- finding disposition 规则：
- 退出门禁：

## 停止条件

- 上游目标漂移：
- fork 定制保护点不清：
- 验证环境不可用：
- 评审发现阻断问题：
- 需要外部副作用但未授权：

## 验证策略

- 聚焦验证：
- 全量验证：
- 发布后验证：
```

## upstream-update-inventory.md

```md
# 上游更新吸收清单

## 基线

- 当前仓库：
- 当前分支：
- 当前 integration_branch：
- 当前 release_branch：
- 当前 fork release tag：
- 上游目标：
- 上游目标 SHA：
- 上游最新 tag：
- 增量范围：

## 汇总

- 上游新增提交数：
- 触达模块：
- 是否存在机械冲突：
- 是否存在行为冲突风险：
- 建议结论：

## 逐项清单

### 1. <commit> <title>

- 更新内容：
- 影响模块：
- 功能作用：
- 风险：
- 与 fork 自定义能力关系：
- 建议处理：
```

## conflict-precheck.md

```md
# 冲突预检报告

## 预检命令

- 命令：
- 目标分支：
- 上游目标：
- 退出码：

## 机械冲突

- 结论：
- 文件：
- 建议：

## 行为冲突风险

### <module or file>

- 风险说明：
- 证据：
- 建议解决：

## 合并建议

- 建议是否进入候选合并：
- 需要用户确认的点：
```

## conflict-resolution-report.md

```md
# 冲突解决报告

## 合并命令

- 命令：
- 评审时上游目标 SHA：
- 合并前上游目标 SHA：
- MERGE_HEAD：
- 当前分支：
- 漂移检查结果：

## 冲突处理

### <file>

- 冲突类型：
- 解决原则：
- 实际处理：
- 验证：

## 无冲突说明

若无冲突，写明预检和实际 merge 均无冲突，并说明仍检查了哪些 fork 保护点。
```

## plan-review-report.md

```md
# 吸收方案评审报告

## 评审输入

- 仓库分析：
- 上游清单：
- 冲突预检：
- 治理方案：
- 验证策略：

## 评审轮次

### Round 1

- Reviewer：
- 范围：
- 发现：
- 结论：

## Findings Disposition

| ID | 严重级别 | 问题 | 处理 | 复评 |
|---|---|---|---|---|

## 退出门禁

- 最后一轮是否无新增 finding：
- 是否存在未处理 high/critical：
- 是否存在未处理 medium：
- medium 及以上 accepted risk 是否已披露并获得用户确认：
- 是否允许进入候选合并：

## 退出结论

- 是否允许进入候选合并：
- 剩余风险：
- 需要用户确认：
```

## review-report.md

```md
# 评审报告

## 评审范围

- Diff 范围：
- 重点模块：
- 排除范围：

## 发现

### critical/high/medium/low/nit <title>

- 位置：
- 问题：
- 影响：
- 建议：
- 处理状态：
- Disposition: `fixed | accepted_risk | not_applicable | blocked`

## 修复复核

- 修复项：
- 复核命令或检查：
- 结论：

## 结论

- 是否存在阻断问题：
- 最后一轮是否无新增 finding：
- 是否存在未处理 high/medium：
- 剩余风险：
```

## post-merge-review-loop.md

```md
# 合并后评审循环报告

## 候选范围

- 合并提交/候选：
- 变更文件：
- 重点风险：

## Review Loop

### Round 1

- 验证：
- 评审：
- 新发现：
- 修复：
- 复验：
- 结论：

### Round N

- 验证：
- 评审：
- 新发现：
- 修复：
- 复验：
- 结论：

## 退出条件核对

- 最后一轮是否无新增 finding：
- 是否存在未处理 high/critical：
- 是否存在未处理 medium：
- low/nit 是否已修复、标记不适用或记录为用户认可的剩余风险：
- 与 claim 匹配的验证是否通过：
- 是否可进入提交/推送：
```

## verification-report.md

```md
# 验证报告

## 命令

| 命令 | 结果 | 说明 |
|---|---:|---|
| `git diff --check` | pass/fail | |
| `rg -n "^(<<<<<<<|=======|>>>>>>>)" .` | pass/fail | |

## 聚焦验证

- 命令：
- 结果：
- 覆盖：

## 全量验证

- 命令：
- 结果：
- 说明：

## 未执行项

- 项目：
- 原因：
- 风险：
```

## release-verification-report.md

```md
# 发布核验报告

## Release Candidate

- integration_branch：
- release_branch：
- master release candidate SHA：
- 版本脚本输出：
- 发版前复验或等价性证明：
- tag：
- release workflow：
- docker 或前端 workflow：

## 远端核验

- `origin/<integration_branch>`：
- `origin/<release_branch>`：
- tag：

## Actions 核验

- Workflow：
- Run：
- Status：
- Conclusion：

## 资产核验

- Release 页面：
- 资产列表：
- 关键资产 HTTP：
- checksums：
- 镜像或静态产物：

## 结论

- 发布状态：
- 剩余风险：
```
