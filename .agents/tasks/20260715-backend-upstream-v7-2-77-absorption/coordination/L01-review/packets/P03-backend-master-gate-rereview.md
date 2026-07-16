# P03 Backend Master Gate Rereview

- objective: 聚焦复核 R2-H-01 是否关闭，并确认修订后无新的 high/medium 方案问题。
- expected-output: 核验 pre-commit index 与 post-commit candidate SHA 两级 `.agents` 门禁；给 ready 或 changes_requested。
- read-scope: `evidence/governance-plan.md` 的 Dev 到 Master 候选构造、`evidence/plan-review-report.md`、P02 submission。
- fixed-target: `c8803713c972af0076f55933fdeed4db81d72d24`。
- tool-guidance: read-only；不得写入、merge、test、commit 或 push。
- write-scope: 结果由 coordinator 持久化到 `workers/backend-master-gate-rereviewer/submissions/P03-backend-master-gate-rereview/S01.md`。
