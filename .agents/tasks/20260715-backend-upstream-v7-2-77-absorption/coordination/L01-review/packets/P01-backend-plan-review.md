# P01 Backend Plan Review

- objective: 独立审查后端 `v7.2.77` 吸收清单、11 个冲突策略、fork 保护点和验证方案，发现遗漏、错误假设或不可执行建议。
- expected-output: 按 severity 排序的 findings；每项包含 ID、Evidence、Impact、Recommendation、Disposition 建议；最后给 `ready | ready_with_updates | changes_requested`。
- tool-guidance: 只读使用 `git log/show/diff/merge-tree`、`rg` 和任务 evidence；不得 merge、checkout、install、test、commit 或 push。
- read-scope: 当前任务 `task-charter.md`、`evidence/*.md`、L01 loop；冲突文件的 base/dev/upstream diff；相关历史吸收任务。
- write-scope: read-only。
- budget: 聚焦方案完整性和冲突策略，不重做 110 行标题抄录；最多 12 个高信号 findings。
- stop-condition: 上游目标不是 `c8803713`、需要写代码、证据不足以判断或发现敏感信息。
- workspace-contract: canonical `.agents` 为 `/home/cheng/git-project/CLIProxyAPI/.agents`；当前执行面为主工作树；不得写任务 authority。
