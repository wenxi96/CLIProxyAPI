# Findings

## 已确认事实

- 当前参考仓库：`/home/cheng/git-project/CLIProxyAPI2`
- 当前目标仓库：`/home/cheng/git-project/CLIProxyAPI`
- 参考清单中存在一批真正值得吸收的安全与运行时修复，但不适合按原顺序整包拿
- 主仓库已经等价吸收了若干项，继续重复吸收会制造无意义冲突

## 核对结论

### 已等价吸收

- `8210c76d`
  - 当前 [internal/api/handlers/management/auth_files.go](/home/cheng/git-project/CLIProxyAPI/internal/api/handlers/management/auth_files.go) 已有 `isUnsafeAuthFileName()`
  - 已覆盖 `TrimSpace`、`/\\`、`filepath.VolumeName()` 校验
- `02a486c6`
  - 当前 [internal/managementasset/updater.go](/home/cheng/git-project/CLIProxyAPI/internal/managementasset/updater.go) 已使用 `io.LimitReader(resp.Body, maxAssetDownloadSize+1)`
  - 已在超限时返回 `download exceeds maximum allowed size`，行为等价
- `3904e319`
  - 当前 [internal/managementasset/updater.go](/home/cheng/git-project/CLIProxyAPI/internal/managementasset/updater.go) 已在 digest mismatch 时中止更新
  - 当前 fallback 路径也已对“无 digest 校验”输出显式告警
  - 该提交的安全核心行为已等价覆盖；其配置语义部分不适合迁入
- `bf06e433`
  - 当前 [sdk/cliproxy/auth/conductor.go](/home/cheng/git-project/CLIProxyAPI/sdk/cliproxy/auth/conductor.go) 与 [sdk/cliproxy/service.go](/home/cheng/git-project/CLIProxyAPI/sdk/cliproxy/service.go) 已按 disabled/active 状态控制 `ModelStates` 继承
- `81ce6550`
  - 当前 [internal/runtime/executor/codex_executor.go](/home/cheng/git-project/CLIProxyAPI/internal/runtime/executor/codex_executor.go) 已有 `isCodexModelCapacityError`
  - 当前 [internal/runtime/executor/codex_executor_retry_test.go](/home/cheng/git-project/CLIProxyAPI/internal/runtime/executor/codex_executor_retry_test.go) 已覆盖 capacity -> retryable 429 行为
- `f9259505`
  - 当前 [internal/runtime/executor/codex_websockets_executor.go](/home/cheng/git-project/CLIProxyAPI/internal/runtime/executor/codex_websockets_executor.go) 已有 `body, _ = sjson.SetBytes(body, "model", baseModel)`
  - 行为等价
- `47d1d127` + `4c0d7c76` + `b4efc3cd`
  - 当前 [internal/translator/claude/openai/chat-completions/claude_openai_request.go](/home/cheng/git-project/CLIProxyAPI/internal/translator/claude/openai/chat-completions/claude_openai_request.go) 已有 top-level system 映射与 system-only fallback turn
- `2d897a79`
  - 当前 [sdk/cliproxy/service.go](/home/cheng/git-project/CLIProxyAPI/sdk/cliproxy/service.go) 已有 usage persistence interval、热重载切换和恢复逻辑
- `5c23abf3`
  - 当前主仓库不存在 `internal/runtime/executor/codex_request_plan.go`
  - 已在 [internal/runtime/executor/codex_executor.go](/home/cheng/git-project/CLIProxyAPI/internal/runtime/executor/codex_executor.go) 多处删除 `stream_options`
  - 当前无需再补 request-plan 快路径
- `398b0f5b`
  - 当前 [sdk/auth/filestore.go](/home/cheng/git-project/CLIProxyAPI/sdk/auth/filestore.go) 已内联实现 `project_id` hydration
  - 已包含 `FetchAntigravityProjectID`、Gemini token refresh 和回写文件逻辑，行为等价
- `823332cf`
  - 当前管理接口已支持多文件上传、批量删除、压缩下载，产品能力基本等价

### 建议吸收

- `5833fb3a`
  - 当前常规 add/update 会注册模型，但 external auth 更新链还缺 lifecycle 同步补丁
  - 当前已开始按主仓库现有 `Service.emitAuthUpdate` / `coreauth.Hook` 架构落地

### 当前架构下另立评估

- `e509adc9`
  - 参考补丁依赖 `suppressedAuth`、`pendingAuthWrites`、`scheduleAuthWrite`、`flushPendingAuthWrite`
  - 当前主仓库 watcher 不存在这套子系统
  - 不能按参考仓库直接吸收，需先重新判断当前 `handleEvent / addOrUpdateClient / removeClient / dispatchAuthUpdates` 架构下是否存在等价竞态

### 功能扩展

- `8ebb93d5`
  - 当前主仓库默认行为已等价：`isRequestInvalidError(err)` 后直接返回 `false`，不跨 credential 重试
  - 该提交真正新增的是可配置参数 `max-invalid-request-retries`
  - 应视为功能扩展，不应再按缺陷修复归类

### 建议暂缓

- `bb99f7b4`
  - 需要先确认 fallback 路径是否仍真实可达
- Tier B 全部
  - 需要性能基线和压测支撑
- Claude 指纹系列
  - 需要单独专题评估

### 不建议吸收

- `2420ffe0`
- `e60452f2`
- `c248504a`
- `6705f691`

## 当前推荐吸收清单

建议作为下一批候选的最小集合：

- `5833fb3a`

## 当前不建议直接整批吸收的原因

- 参考仓库部分提交依赖不同配置语义
- 当前主仓库已经有 fork 自有改造，直接 cherry-pick 易制造语义回退
- Tier B / Claude 系列会引入热路径和长期行为变化，需要独立测试闭环

## 校验方法修正

本轮首次文档曾出现 6 处误判，根因是对“是否已吸收”的校验存在函数名/关键字搜索偏差：

- `02a486c6`
- `f9259505`
- `398b0f5b`
- `8ebb93d5`
- `3904e319`
- `5c23abf3`
- `81ce6550`

后续统一按以下方法核对：

1. 先 `git show` 参考提交，提取核心行为差异
2. 再到主仓库核对行为级逻辑是否已经存在
3. 不再以“函数名不存在”直接推出“逻辑未吸收”

典型高误判场景：

- 补丁被内联实现，而不是抽成独立 helper
- 主仓库已有等价修复，但函数名、注释或结构与参考仓库不同
- 主仓库已默认实现参考仓库的“安全默认行为”，但未暴露同名配置项
