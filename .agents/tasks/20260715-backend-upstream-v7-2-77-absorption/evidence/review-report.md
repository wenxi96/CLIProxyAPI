# 评审报告

## 评审范围

- 候选范围：`dev@1c36ebc5 + MERGE_HEAD@09da52ad`，共 219 个业务文件。
- 重点模块：release/Docker、OAuth、plugin、usage v2、Redis queue、Codex/XAI executor、Gitstore、auth conductor、translator 与 watcher。
- 排除范围：未评审提交、推送、master 合入和发版动作；这些动作尚未授权和执行。

## Findings

### M-01 Generate 未参与同请求 enrichment

- 严重级别：Medium。
- 问题：legacy 默认化可能让 incoming `Generate` 的显式值无法修正已有请求明细，或被 `nil` 错误覆盖。
- 影响：单请求明细的 generate 事实可能不准确；若错误放入 identity，还可能导致请求与 token 重复计数。
- 处理：在 normalization 前记录 presence；显式值参与 enrichment；legacy `nil` 不覆盖显式值；不把 generate 纳入 identity。
- 覆盖：`internal/usage/detail_generate_test.go` 双向覆盖 legacy -> explicit false 与 explicit false -> legacy。
- Disposition：`fixed`。

## 独立复评

- Reviewer：Darwin（只读独立 reviewer）。
- 复评结论：`No findings`，`ready`。
- 复核重点：M-01 enrichment、identity/dedup、usage/tier、Codex/XAI 终态、Redis schema、Gitstore signing、plugin、auth conductor、release/Docker。

## 主线程复核

- 核对所有 conflict index 已清空，冲突解决与 fork 保护策略一致。
- 核对 `Generate` 只作为可修正事实，不改变 request identity。
- 核对 tier-only metadata 不误标 `UsageObserved`，reported total 不被人工合成。
- 未发现新的 high/medium/low/nit 问题。

## 结论

- 阻断问题：无。
- 最后一轮无新增 finding：是。
- 未处理 high/medium：无。
- 剩余风险：CI workflow、真实 release 和运行时外部 provider 行为只能在后续提交/发布阶段继续验证。
