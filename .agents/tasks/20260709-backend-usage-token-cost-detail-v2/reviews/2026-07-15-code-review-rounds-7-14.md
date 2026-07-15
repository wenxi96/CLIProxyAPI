# 后端代码评审 Round 7-14

## 评审范围

- Scope: `dev` 工作区相对 `8926f2ec22d6f8916dab0c91d3bbde65730816cd` 的全部非 `.agents` 候选改动。
- Method: 每轮均由独立 reviewer 只读复审；发现问题后由主会话核验、修复、补回归测试，再进入下一轮。
- Verdict: Round 7-14 均为 `ready_with_updates`，所有 finding 已在后续轮次闭环。

## Round 7

- `B-R7-001`: missing usage 后再到 facts 会形成 plugin/Redis 双事件；改为 missing 是唯一终态，后到 facts 不再形成 revision。
- `B-R7-002`: `x-codex-*` response header 通配可能带出非预期字段；收紧为精确白名单。
- `B-R7-003`: stream 已读到 usage 后 scanner 失败仍可能记成功；失败终态保留已观测 facts。
- `B-R7-004`: v1 snapshot 的 endpoint 迁移规则不完整；补旧快照导入兼容。
- `B-R7-005`: 同时间戳分页缺少稳定 tie-break；加入 identity/facts 排序键。

## Round 8

- `B-R8-001`: Codex image helper 过早 `EnsurePublished`；调整为工具 usage 与主 usage 按唯一终态顺序发布。
- `B-R8-002`: context cancellation 会被流式 defer 记为成功；`Finalize` 读取 `ctx.Err()` 并发布失败。
- `B-R8-003`: provider 明确返回全零 usage 被误判为 missing；引入内部 `UsageObserved` presence。
- `B-R8-004`: rate-limit header 前缀白名单仍过宽；改为精确字段集合。
- `B-R8-005`: 跨 API bucket 同时间戳分页仍可能漂移；排序加入原始 API bucket。

## Round 9

- `B-R9-001`: provider parser 与 caller 对 presence 的使用未统一；non-stream parser 统一返回 `(Detail, observed)`。
- `B-R9-002`: Codex/xAI WebSocket 未进入统一终态 buffer；改为 terminal payload 后由 buffer 发布。

## Round 10

- `B-R10-001`: `null` / `{}` usage 被误判为显式零；只有含受支持 JSON number token 字段的对象才算 observed。
- `B-R10-002`: 曾把 `UsageObserved` 扩入 `sdk/pluginapi.UsageRecord`；标记为兼容风险，后续 Round 14 撤回。

## Round 11

- `B-R11-001`: 3 个旧 parse-success caller 未传 observed；全部迁移到 `PublishParsed`。
- `B-R11-002`: Interactions 前置无效 usage 节点遮蔽后续有效 fallback；改为继续搜索有效节点。
- `B-R11-003`: endpoint 形状的 legacy API map key 可能泄漏；map key 不再推断 endpoint，默认哈希。
- `B-R11-004`: token 字段 `null` / 非数字会误判 presence；严格要求 JSON number。
- `B-R11-005`: 固化 plugin API 兼容边界：presence 仅存在于内部 usage record。

## Round 12

- `B-R12-001`: 混合类型 usage 会吸收数字字符串；parser 只读取 JSON number。
- `B-R12-002`: `EstimatedCostUSD` 指针浅拷贝且 nil enrichment 会清除旧值；改为值复制并保留已有非 nil 值。

## Round 13

- `B-R13-001`: Codex/xAI 三个 non-stream 成功路径在 usage 缺失时完全漏记；统一发布 missing usage 终态。

## Round 14

- `B-R14-001`: cost-only import 不触发 enrichment；把 cost 变化纳入 enrichment 判定。
- `B-R14-002`: 撤回 `sdk/pluginapi.UsageRecord.UsageObserved` 扩展，保持外部插件 ABI 字段面不变；presence 仅保留在 `sdk/cliproxy/usage.Record`。

## Verification

- 每轮修复均补充或更新聚焦回归测试。
- Round 14 后相关 8 包 `go test` 通过，关键用例 `-shuffle=on -count=3` 通过，非 `.agents` `git diff --check` 通过。
