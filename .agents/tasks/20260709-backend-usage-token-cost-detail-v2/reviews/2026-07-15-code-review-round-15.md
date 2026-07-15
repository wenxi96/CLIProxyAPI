# 后端代码评审 Round 15

## 评审结论

- Reviewer: independent Claude CLI (`safe-mode`, read-only)
- Verdict: `ready_with_updates`
- Scope: 当前完整非 `.agents` 后端候选，重点复核 Round 14 处置和流式 usage 终态。

## Finding

- `B-R15-001` (`high`): Antigravity 的“上游流式、对下游非流式”路径先过滤 usage metadata 再解析，且成功结束时没有 finalize 已观测的 `StreamUsageBuffer`。当 usage 只存在于被过滤或最终转换不保留的 chunk 时，会错误发布 explicit zero/missing，丢失真实 token facts。

## Disposition

- 两条 Antigravity 流路径都改为先从原始 SSE line 观察 usage，再过滤面向下游的 payload。
- `executeClaudeNonStream` 的 stream buffer 增加 `defer Finalize(...)`，成功、取消和 scanner error 都进入唯一终态。
- 新增 `TestAntigravityClaudeNonStreamPreservesFilteredStreamUsage`，覆盖非终止 chunk 含 usage、终止 chunk 不含 usage 的场景，并断言无重复终态记录。

## Verification

- Reviewer 原始长报告在外部通道返回时被截断，但保留了 finding 标题；主会话按调用链复现并确认 finding 为真。
- 聚焦执行器测试通过，随后相关 8 包测试通过。
