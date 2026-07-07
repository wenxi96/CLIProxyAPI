# 工作区改动梳理与提交建议

## 背景

本轮创建项目级 `upstream-absorption` skill 时，工作区同时存在上一轮 `20260703-auth-usage-token-cost-statistics` 发布收口治理记录。该混杂状态需要拆分判断，不能直接 `git add .` 或混成一个提交。

## 当前分类

### A. 上一轮发布收口治理记录

状态：已临时保存到 stash，未丢失。

- Stash: `wip release closeout docs 20260703-auth-usage-token-cost-statistics before skill cleanup`
- 范围：
  - `.agents/tasks/20260703-auth-usage-token-cost-statistics/task.md`
  - `.agents/tasks/20260703-auth-usage-token-cost-statistics/progress.md`
  - `.agents/tasks/20260703-auth-usage-token-cost-statistics/handoff.md`
  - `.agents/tasks/20260703-auth-usage-token-cost-statistics/closeout.md`
  - `.agents/tasks/20260703-auth-usage-token-cost-statistics/reviews/2026-07-03-release-closeout-edit-batch-review.md`
- 内容：
  - 将任务状态从 `implemented` 更新为 `released`。
  - 记录后端 release tag `v7.2.49-wx-2.10`。
  - 记录 GitHub Actions `release` / `docker-image` 成功、Release 资产、GHCR manifest 核验。
  - 新增 release closeout 和 edit-batch review。
- 验证：
  - `standard-doc-audit`：clean。
  - stashed diff `git diff --check`：pass。
  - stashed conflict marker scan：no matches。
  - stashed release closeout edit-batch review audit：clean。
- 判断：
  - 可以提交，但必须作为独立治理提交，不应与 `upstream-absorption` skill 改动混在一起。
- 建议提交信息：
  - `docs(agents): 记录凭证统计后端发布收口`
- 恢复方式：
  - 使用 `git stash apply stash@{0}` 或按文件从 stash 恢复后精确暂存上述 5 个文件。

### B. 本轮项目级 upstream-absorption skill

状态：当前工作区可见，未提交。

- 范围：
  - `AGENTS.md`
  - `.gitignore`
  - `.agents/README.md`
  - `.agents/skills/upstream-absorption/SKILL.md`
  - `.agents/skills/upstream-absorption/agents/openai.yaml`
  - `.agents/skills/upstream-absorption/references/report-templates.md`
  - `.claude/skills/upstream-absorption/SKILL.md`
  - `.agents/tasks/20260706-upstream-absorption-skill/**`
- 内容：
  - 建立项目级 skill canonical 入口 `.agents/skills/upstream-absorption/SKILL.md`。
  - 建立 Claude Code 兼容 wrapper。
  - 更新项目入口说明和 `.agents` 目录职责说明。
  - 窄范围放行 `.claude/skills/upstream-absorption/SKILL.md`，同时保持 `.claude/settings.local.json` ignored。
  - 将上游吸收流程补强为 13 阶段：仓库分析、治理方案、上游检测、更新清单、冲突预检、方案多轮评审、确认、候选合并、合并后评审循环、提交推送、master 合入、发版、收口。
  - 强化评审退出标准：最后一轮无新增 finding；high/medium 不得未处理；low/nit 必须修复、标记不适用或作为用户认可剩余风险记录。
- 验证：
  - `quick_validate.py .agents/skills/upstream-absorption`：pass。
  - `quick_validate.py .claude/skills/upstream-absorption`：pass。
  - `standard-doc-audit --task .agents/tasks/20260706-upstream-absorption-skill`：clean。
  - `git diff --check`：pass。
  - conflict marker scan：no matches。
  - `agents/openai.yaml` 可解析，`short_description` 长度满足 25-64，`default_prompt` 包含 `$upstream-absorption`。
- 判断：
  - 可以提交，但应作为独立项目级 skill 提交。
- 建议提交信息：
  - `docs(agents): 增加项目级上游吸收流程 skill`

### C. 本机忽略项

状态：不应提交。

- `.claude/settings.local.json`
- `.codegraph/`
- `.codex`
- `.tmp-dev/`
- `.tmp/`
- `auths/test-batch-check-50/`
- `config.yaml`

判断：这些是本机配置、缓存、临时输出或认证相关内容，保持 ignored，不能纳入提交。

## 推荐收口顺序

1. 先提交 B：项目级 `upstream-absorption` skill。
2. 如需保留上一轮发布收口记录，再恢复 A 并单独提交。
3. 不处理 C，保持 ignored。

## 当前结论

- 当前工作区已从混杂状态收敛为只包含 B 类可提交候选。
- A 类内容已保存到 stash，可恢复后单独提交。
- 不建议把 A 和 B 合并成一个提交，因为它们属于不同任务、不同验收口径。
