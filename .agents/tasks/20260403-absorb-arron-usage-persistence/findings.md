# 已确认事实

- 当前仓库 `internal/usage` 只有 `logger_plugin.go` 和测试，没有快照文件持久化能力。
- 参考仓库新增了 `internal/usage/persistence.go` 与 `internal/usage/persistence_test.go`。
- 参考仓库的持久化能力依赖 `RequestStatistics` 新增以下状态接口：
  - `SnapshotWithState`
  - `HasPendingPersistence`
  - `MarkPersisted`
  - `MarkAllPersisted`
- 参考仓库将 usage 持久化挂在 `sdk/cliproxy/service.go` 生命周期上，而不是管理接口层。
- 当前仓库 `Service` 已有 `cfgMu` 和配置热重载回调，适合作为接入 usage 持久化循环的最小位置。
- 当前仓库 `internal/config/config.go` 尚无 `usage-statistics-persist-interval-seconds` 字段。
- 当前管理接口已支持 usage 导入/导出，本轮不需要改返回结构即可接入自动快照。
