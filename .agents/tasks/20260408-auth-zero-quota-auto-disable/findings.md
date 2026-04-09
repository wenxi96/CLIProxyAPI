# 已确认事实

- 当前运行时 `sdk/cliproxy/auth/conductor.go` 在 `MarkResult` 中只会记录 quota/cooldown，不会自动禁用认证文件。
- 管理批量检查 `internal/api/handlers/management/auth_files_batch_check.go` 已具备 `codex`、`claude`、`gemini-cli`、`kimi`、`antigravity` 的真实额度查询逻辑。
- `auth.Manager` 已有 `persist_async.go` 形式的异步 worker，可复用其并发模型实现额度确认队列。
- `QuotaExceeded` 配置块与管理接口已存在布尔开关模式，适合新增自动禁用配置项。
- `sdk/auth/filestore.go` 会在保存时向支持 metadata 注入的 storage 写回 metadata，但 `gitstore`、`objectstore`、`postgresstore` 暂未对齐该行为。
- 若不修复 store 一致性，自动禁用可能只停留在内存态，服务重启后回弹。
