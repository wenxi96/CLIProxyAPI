# 认证文件 Token 与金额统计设计方案

## 需求结论

可以新增该功能，但应区分两类数据：

- 实际数据：请求次数、成功/失败、token breakdown、模型、时间、延迟、endpoint、认证文件 `auth_index`。这些来自当前 usage pipeline，可由后端稳定记录和聚合。
- 估算数据：金额。金额依赖模型价格表，不等同 provider 真实账单。当前前端已有本地模型价格表，第一阶段建议按前端本地价格计算展示；后端 API 预留 `estimated_cost_usd` 字段，在没有后端价格表时返回 `null`。

## 当前实现分析

### 已具备能力

- `internal/usage/logger_plugin.go` 已记录 `RequestDetail.AuthIndex` 和 `RequestDetail.Tokens`。
- `TokenStats` 已包含 `input_tokens`、`output_tokens`、`reasoning_tokens`、`cached_tokens`、`total_tokens`。
- `StatisticsSnapshot` 已能导出和持久化 endpoint/model 层级请求明细。
- `/v0/management/usage` 已返回 usage snapshot，前端可读取所有请求明细。
- `/v0/management/auth-files` 已返回每个认证文件的 `auth_index`、`success`、`failed`、`recent_requests`。

### 当前缺口

- 后端没有 `auth_index` 维度的聚合结构，前端只能从全量 usage details 临时聚合。
- `/auth-files` 响应不包含 token/金额摘要，认证文件列表无法直接展示 token 统计。
- 没有单认证文件分页明细 API。全量 `/usage` 数据增长后，前端弹窗只靠本地过滤会有性能和传输问题。
- 后端没有共享模型价格表，无法在后端稳定计算金额。

## 数据模型设计

新增认证文件聚合字段：

```go
type StatisticsSnapshot struct {
    // existing fields
    Auths map[string]AuthUsageSnapshot `json:"auths,omitempty"`
}

type AuthUsageSnapshot struct {
    AuthIndex        string                        `json:"auth_index"`
    TotalRequests    int64                         `json:"total_requests"`
    SuccessCount     int64                         `json:"success_count"`
    FailureCount     int64                         `json:"failure_count"`
    Tokens           TokenStats                    `json:"tokens"`
    EstimatedCostUSD *float64                      `json:"estimated_cost_usd"`
    FirstRequestAt   *time.Time                    `json:"first_request_at,omitempty"`
    LastRequestAt    *time.Time                    `json:"last_request_at,omitempty"`
    Models           map[string]AuthModelSnapshot  `json:"models,omitempty"`
}

type AuthModelSnapshot struct {
    TotalRequests    int64     `json:"total_requests"`
    SuccessCount     int64     `json:"success_count"`
    FailureCount     int64     `json:"failure_count"`
    Tokens           TokenStats `json:"tokens"`
    EstimatedCostUSD *float64  `json:"estimated_cost_usd"`
}
```

说明：

- `estimated_cost_usd` 不加 `omitempty`，没有价格表时返回 `null`，前端可明确识别“未配置价格”。
- `auths` 使用 `auth_index` 作为 map key，snapshot 内部也保留 `auth_index` 便于前端直接消费。
- `FirstRequestAt` / `LastRequestAt` 只在存在明细时返回。
- `TokenStats.TotalTokens` 继续复用当前后端 normalise 规则：provider 已给 `total_tokens` 时原样保留；缺失时按 `input_tokens + output_tokens + reasoning_tokens` 补齐；只有当上述主计数均为 0 且仅存在 cached token 信号时，才把 `cached_tokens` 作为兜底 total。`cached_tokens` 默认视为输入 token 的子集或折扣维度，不自动叠加到 total，避免重复计数。
- `auth_index` 不限定为 16 位十六进制。后端本地生成值通常是稳定 hash，但已有运行态或外部提供的 Index 可能是非十六进制字符串；API 实现和前端调用都必须按普通字符串处理。

## API 设计

### 1. 获取总 usage 快照

现有接口：

```http
GET /v0/management/usage
```

新增响应片段：

```json
{
  "usage": {
    "total_requests": 42,
    "success_count": 40,
    "failure_count": 2,
    "total_tokens": 123456,
    "auths": {
      "a1b2c3d4e5f60708": {
        "auth_index": "a1b2c3d4e5f60708",
        "total_requests": 12,
        "success_count": 11,
        "failure_count": 1,
        "tokens": {
          "input_tokens": 100000,
          "output_tokens": 20000,
          "reasoning_tokens": 3000,
          "cached_tokens": 12000,
          "total_tokens": 123000
        },
        "estimated_cost_usd": null,
        "first_request_at": "2026-07-03T02:15:00Z",
        "last_request_at": "2026-07-03T06:40:00Z",
        "models": {
          "gpt-5-mini": {
            "total_requests": 12,
            "success_count": 11,
            "failure_count": 1,
            "tokens": {
              "input_tokens": 100000,
              "output_tokens": 20000,
              "reasoning_tokens": 3000,
              "cached_tokens": 12000,
              "total_tokens": 123000
            },
            "estimated_cost_usd": null
          }
        }
      }
    }
  },
  "failed_requests": 2
}
```

### 2. 查询单认证文件调用明细

新增接口：

```http
GET /v0/management/usage/auths/:auth_index/requests?limit=50&offset=0&model=&failed=&from=&to=
```

参数：

- `auth_index`: URL path 参数，必填；调用方必须 URL escape，后端不得假设固定长度或十六进制格式。如果实现复核发现实际 `auth_index` 可能包含 path 分隔符导致 Gin path 参数无法安全承载，应在落地前改为 query 参数方案并同步更新前后端 spec。
- `limit`: 默认 50，最大 500。
- `offset`: 默认 0。
- `model`: 可选，精确匹配模型名。
- `failed`: 可选，`true` 或 `false`。
- `from` / `to`: 可选，支持 RFC3339 或 unix 秒时间戳。

响应：

```json
{
  "auth_index": "a1b2c3d4e5f60708",
  "total": 2,
  "limit": 50,
  "offset": 0,
  "items": [
    {
      "timestamp": "2026-07-03T06:40:00Z",
      "endpoint": "POST /v1/chat/completions",
      "model": "gpt-5-mini",
      "source": "t:codex",
      "auth_index": "a1b2c3d4e5f60708",
      "failed": false,
      "latency_ms": 2300,
      "tokens": {
        "input_tokens": 1000,
        "output_tokens": 300,
        "reasoning_tokens": 0,
        "cached_tokens": 120,
        "total_tokens": 1300
      },
      "estimated_cost_usd": null
    }
  ]
}
```

实现建议：

- 在 `RequestStatistics` 中新增只读查询方法，从 endpoint/model 明细中筛选并排序，避免复制第二份完整明细。
- 默认按 `timestamp` 倒序返回。
- 返回的 `endpoint` 使用现有 `apis` map key。
- 返回的 `tokens.total_tokens` 必须使用与 `StatisticsSnapshot.Auths` 相同的后端归一化口径，不因同一条 detail 同时有 input 和 cached 而重复计入 cached。
- 不返回 client IP 给凭证明细弹窗，除非后续有明确审计需求；如保留该字段，需要在前端默认隐藏并评估隐私。

### 3. 认证文件列表摘要

现有接口：

```http
GET /v0/management/auth-files
```

对每个文件追加：

```json
{
  "name": "codex_xxx.json",
  "provider": "codex",
  "auth_index": "a1b2c3d4e5f60708",
  "success": 11,
  "failed": 1,
  "usage": {
    "total_requests": 12,
    "success_count": 11,
    "failure_count": 1,
    "tokens": {
      "input_tokens": 100000,
      "output_tokens": 20000,
      "reasoning_tokens": 3000,
      "cached_tokens": 12000,
      "total_tokens": 123000
    },
    "estimated_cost_usd": null,
    "last_request_at": "2026-07-03T06:40:00Z"
  }
}
```

## 金额计算策略

第一阶段：

- 后端记录和聚合 token 事实数据。
- 后端 `estimated_cost_usd` 返回 `null`。
- 前端凭证统计使用现有 `modelPrices` 本地价格表按模型计算估算金额。

后续增强：

- 如需要多用户共享价格配置，再新增后端价格表管理 API 和持久化。
- 后端价格表应按 model name 存储 prompt/completion/cache 单价，单位保持“每 1M tokens 美元”，与前端现有 `ModelPrice` 一致。
- 金额应查询时动态计算，不建议把派生金额作为不可变事实写入 usage detail，避免价格调整后历史显示无法重算。

## 兼容性

- `auths` 是新增字段，不影响旧前端读取 `usage.apis`。
- 导入旧 snapshot 时通过现有 details 重建 `auths`。
- 导入新 snapshot 时也应以 details 为事实源重建 `auths`；导入文件中的 `auths` 只视为派生快照，不直接叠加进内存聚合，避免 details 与 auths 双重计数或漂移。
- 如果导入文件缺少 details，则不应仅凭 `auths` 派生聚合反向制造请求明细；保持现有可恢复数据，记录兼容限制。
- `/auth-files` 新增 `usage` 字段，不改变已有字段语义。

## 风险与控制

- 数据量增长风险：单认证文件明细必须分页，前端弹窗默认只拉第一页。
- auth_index 缺失风险：实现前检查各 provider usage publish 路径；缺失时补齐 runtime record，不在统计层盲猜。
- 金额误解风险：字段、文案和文档均使用“estimated/估算”，不称为真实账单。
- 隐私风险：不新增 prompt/response body，不新增原始密钥输出，明细接口只返回统计元数据。

## 推荐落地顺序

1. 后端先补 usage auth 聚合和明细接口。
2. 后端再把 usage 摘要合并到 auth-files 响应。
3. 前端接入 `usage.auths` 和明细接口，凭证统计展示 token 与估算金额。
4. 如用户确认需要共享价格表，再另起后端价格配置任务。
