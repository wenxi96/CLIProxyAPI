# 冲突解决报告

## 合并命令

- 命令：`git merge --no-commit --no-ff 14b139661d98acbbd7ac19eb827754e78118736f`
- 评审时上游目标 SHA：`14b139661d98acbbd7ac19eb827754e78118736f`
- 合并前上游目标 SHA：`14b139661d98acbbd7ac19eb827754e78118736f`
- MERGE_HEAD：`14b139661d98acbbd7ac19eb827754e78118736f`
- 合并前 HEAD：`181aa28a151ef95424ac9b59b4f346fcf128a31f`
- 当前分支：`dev`
- 漂移检查结果：合并前 `upstream` HTTPS fetch 多次失败，错误为 `gnutls_handshake() failed: The TLS connection was non-properly terminated.`；随后用 `origin/main` SSH 查询和本地 `upstream/main` 交叉核验，二者均为已评审 SHA `14b139661d98acbbd7ac19eb827754e78118736f`，未发现目标漂移。

## 冲突处理

### 无机械冲突

- 冲突类型：无。
- 解决原则：保留上游 `v7.2.52` 变更，同时保护 fork 新增 token/usage 统计能力。
- 实际处理：Git 自动合并成功，未出现 unmerged index。
- 验证：
  - `git ls-files -u` 无输出。
  - `git diff --check` 通过。
  - `rg -n "^(<<<<<<<|=======|>>>>>>>)" .` 无匹配。

## 合并后行为修复

### stream usage failure 抢占

- 冲突类型：行为冲突风险，不是机械冲突。
- 位置：`internal/runtime/executor/openai_compat_executor.go`、`internal/runtime/executor/kimi_executor.go`、`internal/runtime/executor/codex_openai_images.go`、`internal/runtime/executor/helps/usage_helpers_test.go`
- 解决原则：流式响应中如果已观察到 usage，后续 scanner/read error 不能用失败空记录抢先占用 `UsageReporter` 的 `sync.Once`。
- 实际处理：在错误路径先尝试 `streamUsage.Publish(ctx, reporter)`；仅当没有 usage 可发布时才调用 `reporter.PublishFailure(ctx, err)`；新增 `TestUsageReporterUsagePublishPreventsLaterFailure` 覆盖后续 failure 不产生第二条失败记录。
- 验证：`go test ./internal/runtime/executor/helps -run 'TestStreamUsageBuffer|TestUsageReporterUsagePublishPreventsLaterFailure'` 通过。

## 结论

- 预检和实际 merge 均无机械冲突。
- 合并后识别到 1 个与 fork 定制 usage 统计相关的行为风险，已修复并补充测试。
- 当前候选可以进入提交前最终复核；提交、推送和后续 `master` 合入仍需用户授权。
