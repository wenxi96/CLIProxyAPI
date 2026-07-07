# 独立评审请求包：项目级 upstream-absorption skill

Request Mode: direct_subagent

Reviewer Selection: 当前运行面提供一等 subagent 调度工具，使用只读 reviewer 子代理。

Reviewer Capability Probe: 已探测到 `multi_agent_v1.spawn_agent` / `wait_agent`，可派发同工具子代理；本次不使用外部 CLI。

Reviewer Model Policy: 默认继承主会话模型。

Dispatch Receipt: completed；结果见 `reviews/20260707-independent-skill-review.md`，处置见 `reviews/20260707-independent-skill-review-disposition.md`。

Review Objective: 对项目级 `upstream-absorption` skill 的整体设计和细节做独立评审，判断是否满足完整上游吸收治理流程，以及是否仍存在阻断性缺口。

Candidate Scope:

- `AGENTS.md`
- `.agents/README.md`
- `.gitignore`
- `.agents/skills/upstream-absorption/SKILL.md`
- `.agents/skills/upstream-absorption/references/report-templates.md`
- `.agents/skills/upstream-absorption/agents/openai.yaml`
- `.claude/skills/upstream-absorption/SKILL.md`
- `.agents/tasks/20260706-upstream-absorption-skill/evidence/20260706-upstream-absorption-skill-review.md`
- `.agents/tasks/20260706-upstream-absorption-skill/evidence/20260706-upstream-absorption-skill-rereview.md`
- `.agents/tasks/20260706-upstream-absorption-skill/evidence/20260706-skill-coverage-review.md`
- `.agents/tasks/20260706-upstream-absorption-skill/evidence/20260706-worktree-change-triage.md`

Author Claims:

- 项目级 skill 已覆盖仓库分析、新一轮治理方案、上游检测、更新清单、冲突预检、方案多轮评审、确认清单、候选合并、合并后验证和评审循环、提交推送、master 合入、发版申请/执行和收口。
- 已修复上一轮本地主线程评审发现：上游目标 SHA pin / 漂移检查、master release candidate gate、13 阶段模板对齐、评审严重级别模板统一。
- 当前 skill 可作为 B 类独立提交候选。

Required Evidence:

- 阅读上述 Candidate Scope 文件。
- 对照完整流程要求：分析仓库、梳理新一轮治理方案、针对吸收方案多轮评审修复、落地本地治理文档、开始吸收、完成后评审、最终复核评审多轮直到没有新的问题、提交合并、最后发版。
- 检查授权边界、工作区混杂防护、上游漂移防护、release candidate gate、报告模板完整性、前后端协同边界、Claude wrapper 兼容策略和 `.gitignore` 放行策略。

Review Type: mixed

Allowed Skills: `aw-review`, `aw-verification-before-completion`, `aw-plan-review` if available.

Forbidden Actions:

- 不修改文件。
- 不提交、不推送、不合并、不创建 tag、不触发 release。
- 不删除 stash 或 ignored 本机文件。
- 不访问或输出敏感信息。

Known Risks:

- `20260703-auth-usage-token-cost-statistics` 发布收口治理记录已保存在 stash，不属于本次 B 类 candidate。
- 本次评审对象是 workflow/skill 文档，不是业务代码。
- 本地 `.claude/settings.local.json` 应继续 ignored，不应纳入提交。

Report Schema:

```text
Review Status
- workflow.operation.name:
- workflow.operation.status:
- workflow.review_scope.status:
- workflow.scope_check.status:
- workflow.findings.status:
- verdict:

Review Scope

Scope Check

Findings

Scorecard

Verification Evidence

Open Questions / Limitations

Recommended Next Step
```

Scorecard 必须包含 6 个 0-5 整数分值：

- Scope Control
- Evidence Quality
- Correctness
- Safety
- Testability
- Maintainability

Verdict 必须是：

- `ready`
- `ready_with_updates`
- `changes_requested`
- `blocked`
- `rejected`

请做对抗式证伪：假设该 skill 会在真实上游吸收中失败，找出最可能失败的路径，并用文件证据验证。
