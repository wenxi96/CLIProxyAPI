# Findings

## 已确认事实

- 当前自动禁用入口位于 `sdk/cliproxy/auth/quota_check_async.go`，由 `quota-exceeded.auto-disable-auth-file-on-low-quota` 控制；旧 `quota-exceeded.auto-disable-auth-file-on-zero-quota` 作为兼容输入保留。
- 当前自动禁用在异步 quota check 返回 `result.Exhausted` 后触发，并会持久化 `disabled=true`。
- 当前 quota check 投递来自运行时失败结果收口，不是主动定时扫描。
- scoped-pool 的低额度剔除位于 `sdk/cliproxy/auth/scoped_pool.go`，只在 `round-robin + scoped-pool enabled + provider enabled` 下生效。
- scoped-pool 阈值剔除只是临时路由状态，不持久化认证文件禁用状态。

## 设计结论

- 本次任务应独立于历史零额度自动禁用任务建档。
- 总开关重命名为 `auto-disable-auth-file-on-low-quota` 以匹配阈值语义，旧 `auto-disable-auth-file-on-zero-quota` 配置键和管理 API 端点作为兼容层保留。
- 自动禁用阈值属于认证管理层能力，应对 `fill-first` 和 `round-robin` 都生效。
- `disabled` 优先级高于 scoped-pool 的 `low_quota ejected`。

## 2026-06-22 兼容性补充

- YAML / JSON 读取兼容旧 `auto-disable-auth-file-on-zero-quota` 字段，保存配置时收敛为新 `auto-disable-auth-file-on-low-quota` 字段。
- 管理 API 同时注册新旧端点：
  - `/v0/management/quota-exceeded/auto-disable-auth-file-on-low-quota`
  - `/v0/management/quota-exceeded/auto-disable-auth-file-on-zero-quota`
- 旧 API handler 委托到新 handler；GET 响应同时返回新旧字段名，PUT / PATCH 更新同一个新配置字段。
