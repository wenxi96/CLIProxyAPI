# 项目级上游吸收 Skill 任务收口

## 当前状态

项目级 `upstream-absorption` skill 已创建，并已根据子代理独立评审意见完成补强和复验；当前处于待提交状态。

## 已完成范围

- 创建 canonical 项目级 skill：`.agents/skills/upstream-absorption/SKILL.md`。
- 创建报告模板：`.agents/skills/upstream-absorption/references/report-templates.md`。
- 补强完整上游吸收链路：仓库分析、新一轮治理方案、方案多轮评审、合并后多轮复核评审循环。
- 收紧评审退出标准：最后一轮必须无新增 finding；所有问题必须修复、标记不适用，或作为用户认可剩余风险记录。
- 修复整体评审发现：补充上游目标 SHA pin / 漂移检查、master release candidate gate、13 阶段治理方案模板和统一评审严重级别模板。
- 完成修复后复审，前一轮 4 个 findings 均已关闭，未发现新的阻断问题。
- 调用子代理完成只读独立评审，结论为 `ready_with_updates`，未发现 high/critical 阻断。
- 修复子代理发现的 4 个问题：治理方案模板字段缺失、前后端逐仓库 workspace authority gate 不明确、候选合并前确认清单门禁偏弱、分支变量未贯穿提交/推送/发布分支合入阶段。
- 清理工作区：上一轮 `20260703-auth-usage-token-cost-statistics` 发布收口治理遗留改动已放入命名 stash，当前工作区只保留本次 skill 相关改动。
- 已补充工作区改动梳理报告，明确上一轮发布收口治理记录和本轮 skill 改动均可提交，但应拆成两个独立提交。
- 创建 skill UI 元数据：`.agents/skills/upstream-absorption/agents/openai.yaml`。
- 创建 Claude Code 兼容 wrapper：`.claude/skills/upstream-absorption/SKILL.md`。
- 更新项目入口说明：`AGENTS.md` 和 `.agents/README.md`。
- 更新本任务治理记录与设计报告。
- 新增覆盖复核报告：`evidence/20260706-skill-coverage-review.md`。

## 验证

已执行并通过：

```bash
python3 ~/.codex/skills/.system/skill-creator/scripts/quick_validate.py .agents/skills/upstream-absorption
python3 ~/.codex/skills/.system/skill-creator/scripts/quick_validate.py .claude/skills/upstream-absorption
```

```bash
python3 ~/.agent-workstation/bootstrap/bootstrap.py standard-doc-audit --task .agents/tasks/20260706-upstream-absorption-skill --json
```

```bash
git diff --check -- .gitignore AGENTS.md .agents/README.md .agents/skills .claude/skills .agents/tasks/20260706-upstream-absorption-skill
rg -n "^(<<<<<<<|=======|>>>>>>>)" .gitignore AGENTS.md .agents/README.md .agents/skills .claude/skills .agents/tasks/20260706-upstream-absorption-skill
```

补充检查：

- `agents/openai.yaml` 可被 YAML 解析。
- `short_description` 长度为 35，满足 25-64 字符建议。
- `default_prompt` 包含 `$upstream-absorption`。
- `.claude/settings.local.json` 仍被忽略，`.claude/skills/upstream-absorption/SKILL.md` 已被 `.gitignore` 窄范围放行。
- 复核补强后再次执行 skill 校验、任务文档审计、空白检查和冲突标记扫描，均通过。
- 清理后复核：`git status --short --ignored -- .gitignore AGENTS.md .agents/README.md .agents/skills .claude .agents/tasks/20260706-upstream-absorption-skill .agents/tasks/20260703-auth-usage-token-cost-statistics docs` 显示旧任务改动不在工作区；当前可见改动均属本次 skill 任务。
- 子代理独立评审修复后再次执行 independent-review audit、skill 校验、任务文档审计、空白检查、冲突标记扫描、本机路径扫描和定点文本检查，均通过或无匹配。

## 剩余工作

- 是否提交本次项目级 skill、Claude wrapper、入口说明和治理记录，等待用户后续授权。

## 剩余风险

- 项目级 `.agents/skills` 可被 Codex/Gemini 作为项目 skill 入口；Claude Code 通过 `.claude/skills` wrapper 读取 canonical 文件。其他 agent 若不支持这两个目录，需要按 `AGENTS.md` 显式读取。
- `20260703-auth-usage-token-cost-statistics` 发布收口治理记录保存在本地 stash，尚未提交入库；需要时可单独恢复和处理。
