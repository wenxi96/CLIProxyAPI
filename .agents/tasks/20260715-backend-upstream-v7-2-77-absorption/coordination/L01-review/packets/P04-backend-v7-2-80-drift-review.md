# P04 Backend v7.2.80 Drift Review

- objective: 独立审查从已评审 `c8803713/v7.2.77` 漂移到 `09da52ad/v7.2.80` 的 8 个新增提交、46-path 新预检、generate/plugin/gitstore/incomplete 契约及验证方案。
- expected-output: 按 severity 输出新增 finding；核验 118/118 矩阵、11 conflict/35 auto ledger、旧方案契约是否仍成立；最后给 ready、ready_with_updates 或 changes_requested。
- tool-guidance: 只读使用 git log/show/diff/merge-tree、rg 和任务 evidence；不得 merge、checkout、install、test、commit、push 或写入。
- read-scope: `c8803713..09da52ad`、`evidence/repository-analysis.md`、`governance-plan.md`、`upstream-update-inventory.md`、`conflict-precheck.md`、`plan-review-report.md`。
- fixed-target: `09da52ad509e2c18e7b9540db3b98c2214c280aa` / `v7.2.80`。
- stop-condition: 目标再次漂移、需要代码候选才能判断、证据不足或发现敏感信息。
- write-scope: read-only；结果由 coordinator 持久化到 `workers/backend-v7-2-80-drift-reviewer/submissions/P04-backend-v7-2-80-drift-review/S01.md`。
