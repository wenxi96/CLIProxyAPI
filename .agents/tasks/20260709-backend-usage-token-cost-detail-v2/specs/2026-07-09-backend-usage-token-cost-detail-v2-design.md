# 后端 Usage Token 与金额明细契约升级设计

## 方案来源

- 用户目标: 在原有统计信息基础上新增每个凭证的请求 token 明细和估算金额明细，覆盖输入、输出、缓存、推理、总 token 和各项估算金额。
- 方案复审结论: 后端 v6 独立复审 verdict 为 `ready`，上一轮 low finding 已关闭。
- 历史前置: `20260703-auth-usage-token-cost-statistics` 已提供认证文件 usage 聚合和单认证文件明细 API，本任务只做第二阶段契约升级。

## 关键设计

### 1. Persistent Canonical Detail

请求级 detail 必须落到持久结构，而不是临时 API DTO。推荐新增 `RequestDetailV2` 或扩展 `RequestDetail`，至少持久化并序列化：

- 顶层字段: `request_id`、`client_ip`、`timestamp`、`endpoint`、`model`、`provider`、`executor_type`、`auth_type`、`model_alias`、`source`、`auth_index`、`detail_role`、`failed`、`latency_ms`、`estimated_cost_usd`
- token 字段: `input_tokens`、`output_tokens`、`reasoning_tokens`、`cached_tokens`、`cache_read_tokens`、`cache_creation_tokens`、`total_tokens`、`reported_total_tokens`、`computed_total_tokens`、`token_usage_source`、`cache_split_status`、`reasoning_cost_mode`

派生规则：

- `/usage` model details 直接返回 persistent detail。
- `/usage/auths/:auth_index/requests` 从 persistent detail filter / paginate，不单独补字段。
- `/usage/export` / `/usage/import` 序列化同一 persistent detail。
- redis queue payload 从 persistent detail 派生 provider/model/auth/request context，但不得输出 raw `api_key`、access token、cookie 或其他认证密钥。
- request facts dedup 与 merge 使用 persistent detail 字段。

安全来源规则：

- `source` 只能持久化安全标识，例如 `auth_index`、auth ID、认证文件路径、provider/account 的脱敏摘要或显式非敏感 label。
- 禁止将 raw API key、access token、refresh token、cookie、Authorization header 或请求下游 API key 写入 `source`、snapshot、管理 API 或 queue payload。
- 若当前 `resolveUsageSource` 只能拿到 raw key，必须返回空 `source` 或脱敏摘要，并优先依赖 `auth_index` 做关联。
- redis queue 不再序列化 `api_key` 字段；若兼容需要保留字段名，只能输出空值或脱敏值，并测试证明 raw key 不泄漏。
- usage snapshot / management API 的 `APIs` map key 不能使用 raw downstream API key；若旧逻辑只能拿到 raw key，必须改用 endpoint、provider/model、安全 source 或脱敏摘要。
- redis queue 的 header 字段必须过滤或脱敏 `Authorization`、`Cookie`、`Set-Cookie`、token/key 类 header；安全测试需要覆盖 `response_headers` 泄漏面。

### 2. Request Identity 与 Facts Hash 分离

需要区分“同一个请求”和“同一个请求 facts 是否已 enrich”：

- `identityKey`: 不能只使用 `request_id`。必须包含 usage record scope，推荐使用 `request_id + provider + executor_type + model + auth_index/source + detail_role`；缺失 `request_id` 时回退到 timestamp、endpoint、model、source/auth_index、client_ip、latency、failed 等稳定 request identity 字段。
- `detail_role`: 必须作为 persistent detail 字段持久化和 import/export 序列化，用于区分 primary request usage、additional model usage、tool/image/video 等同一用户请求内的多模型 facts；没有显式 role 时按 `primary` 处理。
- `factsHash`: 使用 token facts、usage source、cache split、reasoning mode 等 facts 字段。
- import/merge 时同 identity 不重复加总；如果旧 detail token 为空、新 detail facts 更完整，允许 enrich 旧记录或按明确规则保留更完整 facts。
- enrich 空 facts 时不能只替换 detail；必须同步校正 API/model/auth aggregate totals、`TokenStats`、按天/小时 token bucket，确保汇总与 detail 一致且 request count 不重复增加。
- 同一个 `request_id` 下不同 model 或不同 detail role 必须保留为多条 facts，不能被 enrich 合并吞掉。

### 3. Publish 边界

当前 `UsageReporter` 的 `sync.Once` 语义存在“空 usage 先 Publish，后续 `EnsurePublished` 或 enriched usage 无法发布”的风险。新设计需要：

- 将 terminal/success/failure publish 与 token facts publish 分离，或使用状态机保证 enriched facts 有机会覆盖空 facts。
- 明确 `missing usage` 只在请求完成且没有任何 usage facts 时发布。
- 覆盖 non-stream、stream、Codex WS、Gemini、Claude、OpenAI-compatible、Kimi、AIStudio、Vertex、xAI 等成功路径清单。

### 4. Token 与 Cost 语义

后端本阶段不维护官方价格表，但必须提供可计算且不双算的 token facts。

- `reported_total_tokens`: provider 原始 total，可为空。
- `computed_total_tokens`: 后端按 non-overlap 规则计算的 total。
- `total_tokens`: 展示兼容字段，优先 provider reported total，否则 computed total。
- `reasoning_cost_mode`: `included_in_output`、`separate`、`unknown`。
- `cache_split_status`: `none`、`read_only`、`creation_only`、`read_and_creation`、`unknown`。
- `estimated_cost_usd`: 保持可空；如果没有后端价格表则为 `null`，不写 0。

### 5. Client IP Shared Helper

新增或复用 `internal/logging.ClientIPFromContext(ctx)`：

- request context 构造阶段或 `UsageReporter.buildRecord` 阶段必须把 `client_ip` 快照成不可变 context value / record field / canonical detail field。
- `ClientIPFromContext(ctx)` 优先读取不可变快照值；仅在没有快照时 fallback 到 `logging.ResolveClientIP(ginCtx)`。
- `internal/usage` 与 `internal/redisqueue` 都调用该 helper。
- 无 gin context 时返回空字符串。
- 测试证明 usage logger 与 queue sink 对同一个 context 得到相同 `client_ip`。
- 测试必须覆盖异步 usage dispatch / recycled Gin context 场景，证明不会在请求结束后读取复用后的 `gin.Context`。

## API 契约

管理 API request detail item 顶层字段固定为：

```json
{
  "request_id": "req_x",
  "client_ip": "127.0.0.1",
  "timestamp": "2026-07-09T12:00:00Z",
  "endpoint": "POST /v1/chat/completions",
  "model": "gpt-5",
  "provider": "openai",
  "executor_type": "openai",
  "auth_type": "api_key",
  "model_alias": "gpt-5",
  "source": "auths/custom.json",
  "auth_index": "custom-auth-index",
  "detail_role": "primary",
  "failed": false,
  "latency_ms": 1234,
  "estimated_cost_usd": null,
  "tokens": {
    "input_tokens": 100,
    "output_tokens": 50,
    "reasoning_tokens": 10,
    "cached_tokens": 30,
    "cache_read_tokens": 30,
    "cache_creation_tokens": 0,
    "total_tokens": 150,
    "reported_total_tokens": 150,
    "computed_total_tokens": 150,
    "token_usage_source": "provider_usage",
    "cache_split_status": "read_only",
    "reasoning_cost_mode": "included_in_output"
  }
}
```

## 风险与处理

- 外部 SDK/plugin API 兼容风险: 不在 `sdk/pluginapi.UsageDetail` 扩字段作为第一步，只在内部 usage detail 与 management API 扩展。
- 历史快照兼容风险: import 对缺失字段设置 empty/null/unknown，并从旧 detail 派生可用 identity。
- 双算风险: total 归一化必须集中在后端 helper，测试覆盖 reasoning/cache 已包含于 input/output 的常见 provider 语义。
- 覆盖缺口风险: executor publish 清单作为验收证据，不只依赖少数 OpenAI path。
- 敏感信息风险: `source` 与 queue payload 必须使用安全标识，测试断言 raw API key / access token / cookie 不出现在 snapshot、management API 和 queue JSON。
- API map key 与 header 泄漏风险: usage `APIs` map key 和 redis queue `response_headers` 也属于安全输出面，必须覆盖 raw downstream API key、Authorization、Cookie、token/key 类 header。
- 多模型请求风险: identity 必须包含 model/detail role，测试覆盖同一 `request_id` 下主模型与 additional model 都被保留。
- 汇总漂移风险: 空 facts 被 enriched facts 补齐时，必须校正 aggregate token totals、auth/model totals、按天/小时 buckets，不能只更新 detail。
- 异步上下文风险: `client_ip` 必须在请求期快照，不能在异步 usage plugin 中直接读取可复用的 Gin context。

## 验证策略

- 后端单元测试: usage detail schema、merge/import、token normalization、client_ip helper。
- API 测试: `/usage` 与 auth pagination 返回新字段。
- Queue 测试: redis queue payload 从同源 detail 派生。
- Executor helper 测试: `Publish` / `EnsurePublished` enriched facts 优先级。
- 安全测试: raw `api_key` / token / cookie 不进入 persistent detail、API response 或 queue JSON。
- 安全测试补充: raw downstream API key 不作为 `APIs` map key；queue `response_headers` 不包含 Authorization、Cookie、Set-Cookie、token/key 类原文。
- Identity 测试: 同 `request_id` 不同 model/detail role 保留多条 usage；同 identity 空 facts 可被 enriched facts 补齐。
- Enrich 汇总测试: 同 identity 空 facts 被 enriched facts 补齐后，request count 不增加，但 `TotalTokens`、model/auth `TokenStats`、`tokensByDay` / `tokensByHour` 同步更新。
- Async context 测试: recycled Gin context 不影响 persisted `client_ip`。
- 构建验证: `go test` 聚焦包和 `go build`。
