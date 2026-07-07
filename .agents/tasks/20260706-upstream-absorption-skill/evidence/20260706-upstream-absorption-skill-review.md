# Upstream Absorption Skill 整体评审报告

Review Status
- workflow.operation.name: pre_landing_review
- workflow.operation.status: completed
- workflow.review_scope.status: complete
- workflow.scope_check.status: requirements_missing
- workflow.findings.status: findings_reported

## Review Scope

- Base Ref: 当前 `dev` 工作区中已创建的项目级 skill 候选。
- Head Ref: 工作区未提交 B 类改动。
- Candidate: `.agents/skills/upstream-absorption/`、`.claude/skills/upstream-absorption/`、`AGENTS.md`、`.agents/README.md`、`.gitignore` 以及 `.agents/tasks/20260706-upstream-absorption-skill/` 治理记录。
- Review Goal: 判断该 skill 是否满足“分析仓库、梳理新一轮治理方案、吸收方案多轮评审修复、落地本地治理文档、开始吸收、完成后评审、最终多轮复核直到没有新的问题、提交合并、最后发版”的完整流程。

## Scope Check

- Intent: 创建当前项目专用上游吸收流程 skill，后续吸收上游时直接调用。
- Delivered: 已创建项目级 skill、报告模板、Claude wrapper、入口说明和治理任务记录。
- Scope Check: requirements_missing。
- Missing Requirements: 当前流程主体完整，但上游目标漂移防护和 master/release commit 复验门禁仍不够明确。

## Findings

### High: 合并前未强制重新 pin / 核验上游目标，可能吸收未经清单和评审覆盖的新提交

- Why It Matters: 上游吸收任务通常会经历仓库分析、清单、冲突预检、方案评审和用户确认。若这期间 `upstream/<branch>` 继续前进，后续直接 merge `upstream/<branch>` 可能吸收未进入清单、未冲突预检、未评审的新提交。
- Evidence Ref: `.agents/skills/upstream-absorption/SKILL.md:38-42` 只要求初始 fetch 与增量计算；`.agents/skills/upstream-absorption/SKILL.md:68-72` 在候选合并阶段直接合入 `upstream/<branch>`，未要求记录目标 SHA、合并前 re-fetch/rev-parse、漂移则重做清单/预检/评审。
- Confidence: high。
- Recommendation: 在上游状态检测阶段记录 `upstream_target_sha`；在候选合并前执行 `git fetch --all --tags --prune` 和 `git rev-parse upstream/<branch>`，若 SHA 变化，必须回到更新清单、冲突预检和方案评审阶段。

### Medium: master 合入后发版前缺少“实际发版提交”复验门禁

- Why It Matters: `dev` 验证通过不等于 `master` 上实际发版提交必然等价。master 合并方式、已有 master 差异、版本脚本可达 tag 关系都可能影响最终 release candidate。当前 skill 提到版本号要在实际发版提交上核验，但没有把 master 合入后的测试/构建/冲突扫描或等价证明写成发版前硬门禁。
- Evidence Ref: `.agents/skills/upstream-absorption/SKILL.md:94-104` 只要求合入 master、核验远端、计算 tag 和发布后核验；`.agents/skills/upstream-absorption/SKILL.md:121-131` 是泛化完成前检查，但没有明确 release candidate 必须基于 `master` 实际提交复验。
- Confidence: high。
- Recommendation: 在 `master` 合入后、tag 前新增 release candidate gate：确认 `master` 目标 SHA、在该提交上执行版本脚本、`git diff --check`、冲突标记扫描，以及仓库要求的构建/测试；若因 fast-forward 且已验证同一 SHA 而跳过测试，必须记录等价性证据。

### Low: 方案模板阶段与 skill 的 13 阶段流程不完全一致

- Why It Matters: skill 主体已扩展为 13 阶段，但 `governance-plan.md` 模板仍只有 9 个阶段，缺少“发送确认清单”“冲突解决报告”“收口”等显式项。执行者使用模板时可能漏写部分治理节点。
- Evidence Ref: `.agents/skills/upstream-absorption/SKILL.md:28-108` 定义 13 阶段；`.agents/skills/upstream-absorption/references/report-templates.md:62-72` 的治理方案模板只列出 9 个阶段。
- Confidence: medium。
- Recommendation: 将 `governance-plan.md` 模板阶段拆分为与 skill 主体一致的 13 项，或明确 9 项是聚合阶段并列出包含关系。

### Low: 评审模板的严重级别命名与 skill 主体不一致

- Why It Matters: skill 主体使用 `high/critical/medium/low/nit` 和 disposition 语义；`review-report.md` 模板使用 `P0/P1/P2`，可能导致后续报告和退出门禁难以直接对应。
- Evidence Ref: `.agents/skills/upstream-absorption/SKILL.md:58-61`、`.agents/skills/upstream-absorption/SKILL.md:80-85` 使用 finding/disposition/critical/high/medium/low/nit 语义；`.agents/skills/upstream-absorption/references/report-templates.md:235-243` 使用 `P0/P1/P2`。
- Confidence: medium。
- Recommendation: 统一模板严重级别为 `critical/high/medium/low/nit`，并在模板中要求每个 finding 写 disposition。

## Open Questions / Limitations

- 本次是主线程本地评审，不是独立 reviewer 评审。
- 未实际执行一次上游吸收演练；结论基于 skill 文档、模板和当前仓库既有吸收任务经验。
- 未验证 Codex/Gemini/Claude 实际运行时是否会自动加载 `.agents/skills` 和 `.claude/skills`，仅验证了文件结构与 skill frontmatter。

## Verification Gaps

- 尚未对修复后的 skill 重新执行完整 review，因为本报告只输出发现，未修改 skill。
- 尚未通过一次模拟上游吸收任务验证模板是否足够驱动执行。

## Recommended Next Step

先修复以上 4 项后再提交 B 类 skill。优先级建议：

1. 补上游目标 SHA pin 与合并前漂移检查。
2. 补 master/release candidate 发版前复验门禁。
3. 对齐治理方案模板为 13 阶段。
4. 统一评审模板严重级别和 disposition 字段。
