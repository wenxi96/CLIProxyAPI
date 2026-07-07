# 独立评审报告：项目级 upstream-absorption skill

Review Status
- workflow.operation.name: independent_upstream_absorption_skill_review
- workflow.operation.status: completed
- workflow.review_scope.status: complete
- workflow.scope_check.status: requirements_missing
- workflow.findings.status: findings_reported
- verdict: ready_with_updates

Review Scope

- Base Ref: 当前工作区中的未提交 B 类候选。
- Candidate Scope: 已独立读取请求包列出的 11 个文件，包括 `AGENTS.md`、`.agents/README.md`、`.gitignore`、canonical skill、report templates、OpenAI metadata、Claude wrapper 和 4 个 evidence 报告。
- Review Goal: 对项目级 `upstream-absorption` skill 做对抗式独立评审，判断完整上游吸收治理流程是否闭环，以及是否仍有阻断性缺口。
- Read-only Boundary: 未修改、提交、推送、合并、打 tag、触发 release 或删除任何文件。

Scope Check

- 核心流程覆盖度较高：当前 skill 已包含仓库分析、治理方案、上游 SHA pin、更新清单、冲突预检、方案评审、候选合并、合并后验证/评审循环、提交推送、master 合入、release candidate gate、发版核验和收口。
- 前一轮高风险缺口已基本修复：`upstream_target_sha` 漂移检查、`master_release_candidate_sha` gate 已存在。
- 仍存在非阻断但应修复的治理/模板一致性问题，主要影响真实执行时的计划完整性、跨仓库 authority 和预合并确认可审计性。
- 结论：未发现 high/critical 阻断性缺口；建议提交前按 findings 更新。

Findings

ID: UA-SKILL-001
Severity: medium
Summary: `governance-plan.md` 模板缺少 skill 主体要求的关键治理字段。
Evidence: skill 主体要求治理方案说明“分支/发版策略、任务拆分、评审策略”；但模板此前只有目标、范围、非目标、授权边界、阶段、停止条件和验证策略。
Impact: 执行者按模板生成方案时，可能漏掉分支/发版决策、任务边界和评审策略，导致后续 plan review 输入不完整。
Recommendation: 在模板中新增 `分支/发版策略`、`任务拆分`、`评审策略` 三节，并要求写明独立评审触发条件和退出门禁。
Confidence: high

ID: UA-SKILL-002
Severity: medium
Summary: 前后端协同时没有明确要求对每个仓库单独完成 workspace authority gate。
Evidence: skill 声明可服务配套前端仓库，并要求前后端各自维护 `.agents/tasks`；入口门禁只泛化要求确认 canonical `.agents`、Persistence Mode、linked worktree，没有在前端规则中明确逐仓库执行。
Impact: 跨仓库吸收时，执行者可能只确认后端 `.agents`，随后在前端仓库写入任务 authority 或验证记录，造成持久化模式不明、任务目录混写或 linked worktree 状态误判。
Recommendation: 在“前后端协同规则”中明确：每个参与仓库都必须独立读取本地规则、确认 canonical `.agents` / Persistence Mode / linked worktree / `git status --short`，并在无法确认时停止等待用户确认。
Confidence: medium

ID: UA-SKILL-003
Severity: medium
Summary: 预合并确认清单被写成条件步骤，削弱了完整吸收清单的审计门禁。
Evidence: skill 此前写的是“若用户要求先确认”；候选合并只要求“在授权后”执行。
Impact: 如果用户给了泛化授权，执行者可能跳过完整清单、冲突点和建议方案的最终披露，降低真实上游吸收前的可审计性。
Recommendation: 将确认清单设为候选合并前默认门禁；只有用户明确要求直接合并且无未处理 accepted risk 时才允许记录豁免。
Confidence: medium

ID: UA-SKILL-004
Severity: low
Summary: 集成分支在合并阶段可配置，但提交/推送阶段又硬编码为 `dev`。
Evidence: 候选合并允许 `dev` 或仓库约定集成分支；提交推送和 master 合入阶段固定写 `dev` / `origin/dev`。
Impact: 当前后端仓库使用 `dev` 时风险较低；但配套前端或未来分支模型不同，会导致推送和核验证据指向错误分支。
Recommendation: 引入 `integration_branch` / `release_branch` 变量，默认 `dev` / `master`，后续所有提交、推送和核验引用同一变量。
Confidence: high

Scorecard

| Dimension | Score |
|---|---:|
| Scope Control | 4 |
| Evidence Quality | 4 |
| Correctness | 4 |
| Safety | 4 |
| Testability | 3 |
| Maintainability | 4 |

Verification Evidence

- `git diff --check`: exit 0，无输出。
- `rg -n "^(<<<<<<<|=======|>>>>>>>)" .`: exit 1，无冲突标记匹配。
- `git check-ignore -v`: `.claude/skills/upstream-absorption/SKILL.md` 被 `.gitignore` 放行；`.claude/settings.local.json` 仍被忽略。
- `git status --short --ignored -- .agents .claude AGENTS.md .gitignore`: 候选改动可见，`.claude/settings.local.json` 仍为 ignored。
- 固定词扫描未发现本机绝对路径写入 Candidate Scope 文件。

Open Questions / Limitations

- `independent-review-orchestration.md` 在预期路径未找到；本次按用户请求包和只读评审协议执行。
- 未执行真实上游吸收 dry-run；结论基于文档、模板和只读 git 检查。
- 未运行 Go 测试或构建，因为本次候选是 workflow/skill 文档，不是业务代码。

Recommended Next Step

按 4 条 findings 做小范围文档更新后，再做一次只读复审。当前没有发现阻断性 high/critical 缺口，但不建议在补齐模板字段、逐仓库 authority gate 和确认清单语义前标记为完全 ready。
