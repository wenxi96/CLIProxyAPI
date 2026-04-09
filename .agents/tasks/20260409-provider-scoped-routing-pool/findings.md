# 发现记录

## 已确认事实

- 当前运行时凭证调度核心位于 `sdk/cliproxy/auth`，入口主要在 `conductor.go`、`scheduler.go`、`selector.go`。
- 当前 `routing.strategy` 仅支持 `round-robin` 与 `fill-first`，相关配置与管理接口位于 `internal/config/config.go` 与 `internal/api/handlers/management/config_basic.go`。
- 配置型 provider 凭证不会单独走另一套调度链路，而是先由 watcher synthesizer 转换为统一的 `coreauth.Auth`：
  - `gemini-api-key`
  - `claude-api-key`
  - `codex-api-key`
  - `vertex-api-key`
  - `openai-compatibility`
- 认证文件列表接口已能返回 `auth_index`、`disabled`、`unavailable`、`status_message` 等运行时字段，适合扩展池状态字段。
- 前端认证文件页已具备多维过滤和卡片渲染能力，新增“仅显示未禁用”属于低风险页面层过滤。
- 前端 AI Providers 页按 provider section 分块展示，适合为每类 provider 卡片补充“池内运行 / 候补 / 降权”等状态标记。
- 当前仓库已存在“额度真实耗尽后异步确认并自动禁用认证文件”能力，可复用其异步检查去重思路，但不能直接把“配置型 provider 凭证”也做成持久化禁用。

## 关键设计结论

- 范围轮询必须是“按供应商类别独立建池”，不能是全局单池。
- 范围轮询必须显式开启才生效，未开启时不能影响现有逻辑。
- 推荐做成 `round-robin` 之上的附加池过滤层，而不是直接重写默认 selector 语义。
