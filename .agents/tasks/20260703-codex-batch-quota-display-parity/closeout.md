Bugfix Status
- workflow.operation.name: bugfix_delivery
- workflow.operation.status: fixed
- workflow.root_cause.status: confirmed
- workflow.verification.status: pass
- workflow.regression_guard.status: added

## 问题摘要

- 问题: Codex 批量检查会把月度额度窗口误标为 5 小时，并额外返回空 weekly 窗口。
- 影响: `/v0/management/auth-files/batch-check` 与 `/auth-files/batch-check-jobs` 的 Codex details 会误导前端展示。
- 预期: B 路 window 分类与单文件刷新 A 路一致，月度窗口显示为月度，空 secondary weekly 不形成展示行。
- 实际: 后端旧逻辑按 primary/secondary 槽位硬编码 `five-hour` / `weekly`。

## 复现

- 前置条件: provider usage payload 中 `rate_limit.primary_window.limit_window_seconds=2592000`，`secondary_window` 仅有 `limit_window_seconds=604800`。
- 步骤: 执行新增测试 `TestBatchCheckAuthFiles_CodexMonthlyPrimaryWindowMatchesSingleRefresh`。
- 当前结果: 修复后返回一个 `monthly` window，remaining_percent 为 75，不再返回空 weekly。

## 最小复现边界

- 最小边界: mock Codex usage API + batch-check handler。
- 已排除项: 不依赖真实 token、reset credits 列表内容或前端 i18n。
- 剩余限制: 未做真实 provider live API 验证。

## 根因

- 根因: `extractCodexWindows` 按字段位置硬编码窗口语义，没有按 `limit_window_seconds` 分类。
- 证据: 单文件刷新 A 路按时长分类；后端旧 B 路固定 `rate_limit.primary_window -> five-hour`、`secondary_window -> weekly`。
- 证据引用: path:internal/authquota/service.go; path:src/components/quota/quotaConfigs.ts
- 为什么这是根因: 前端收到错误 id 后只能按错误窗口类型展示，空 weekly 也来自后端额外生成的窗口。

## 修复策略

- 选定修复: 后端 Codex window 提取改为按窗口时长识别 5 小时、周、月度窗口，并让普通 quota 与 code-review quota 复用同一 helper。
- 为什么这样修: 与 A 路已验证分类规则对齐，直接修正错误数据契约。
- 根因关联: 消除 primary/secondary 槽位硬编码。
- 放弃的候选方案: 不只在前端改 label，因为后端契约仍会给异步 job 和后续调用方传错语义。

## 改动摘要

| 文件 | 改动类型 | 改动内容 | 必要性 | 根因关联 |
|------|----------|----------|--------|----------|
| `internal/authquota/service.go` | code | 新增 Codex window 时长分类 helper，输出 `monthly` / `code-review-monthly` | 修正后端 details 契约 | 直接替换槽位硬编码 |
| `internal/authquota/service_test.go` | test | 增加 service 层月度 primary 回归用例 | 防止分类回退 | 覆盖根因输入 |
| `internal/api/handlers/management/auth_files_batch_check_test.go` | test | 增加 batch-check handler 月度 primary 回归用例 | 覆盖 HTTP 响应契约 | 验证前端入口数据 |
| `.agents/tasks/20260703-codex-batch-quota-display-parity/*` | docs | 中文记录任务、根因、验证和收口 | 满足治理记录要求 | 保留决策证据 |

## 验证

| 检查项 | 命令或步骤 | 最新结果 | 证据 | 范围 |
|--------|--------------|----------|------|------|
| Go 格式化 | `docker run --rm -v "$PWD":/workspace -w /workspace golang:1.26 gofmt -w internal/authquota/service.go internal/authquota/service_test.go internal/api/handlers/management/auth_files_batch_check_test.go` | pass | command:gofmt | 后端改动文件 |
| 后端聚焦测试 | `docker run --rm -v "$PWD":/workspace -w /workspace golang:1.26 go test -buildvcs=false ./internal/authquota ./internal/api/handlers/management` | pass | command:go test | service + management handler |
| 后端编译 | `docker run --rm -v "$PWD":/workspace -w /workspace golang:1.26 go build -buildvcs=false -o test-output ./cmd/server && rm -f test-output` | pass | command:go build | server 编译 |

## 回归防护

- 防护类型: new-test
- 位置: `internal/authquota/service_test.go`; `internal/api/handlers/management/auth_files_batch_check_test.go`
- 防止什么: 防止 Codex 月度 primary window 再被标成 5 小时，或空 weekly 再进入 batch details。
- 为什么这样足够: 两个测试分别覆盖共享 service 输出和管理接口响应契约。

## 风险与未知

- 剩余风险: 未用真实 Codex 认证文件跑 live provider API。
- 未覆盖场景: additional_rate_limits 的月度窗口仍沿用现有前端 A 路规则，后端本次只处理主 quota 与 code-review quota。
- 剩余未知: provider 是否会在其它账户类型返回新的窗口时长枚举。

## 需要用户提供

None
