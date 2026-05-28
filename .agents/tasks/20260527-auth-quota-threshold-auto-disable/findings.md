# Findings

## 已确认事实

- 当前自动禁用入口位于 `sdk/cliproxy/auth/quota_check_async.go`，由 `quota-exceeded.auto-disable-auth-file-on-zero-quota` 控制。
- 当前自动禁用在异步 quota check 返回 `result.Exhausted` 后触发，并会持久化 `disabled=true`。
- 当前 quota check 投递来自运行时失败结果收口，不是主动定时扫描。
- scoped-pool 的低额度剔除位于 `sdk/cliproxy/auth/scoped_pool.go`，只在 `round-robin + scoped-pool enabled + provider enabled` 下生效。
- scoped-pool 阈值剔除只是临时路由状态，不持久化认证文件禁用状态。

## 设计结论

- 本次任务应独立于历史零额度自动禁用任务建档。
- 旧开关保留为总开关，新阈值作为新增全局配置。
- 自动禁用阈值属于认证管理层能力，应对 `fill-first` 和 `round-robin` 都生效。
- `disabled` 优先级高于 scoped-pool 的 `low_quota ejected`。
