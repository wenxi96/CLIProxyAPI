# Codex 批量额度窗口分类修复

Status: complete

## 目标

修复 Codex 认证文件在批量检查中将月度额度窗口误标为 5 小时额度、并额外返回空周额度窗口的问题，使后端 `/v0/management/auth-files/batch-check` 与异步 job 路径输出的 Codex window 语义与单文件刷新展示使用的分类逻辑一致。

## 范围

- 调整后端 Codex quota details 的 window 分类逻辑，按 `limit_window_seconds` 识别 5 小时、周、月度窗口。
- 保持批量检查继续复用 canonical quota query service，不恢复 management handler 内的 provider-specific 查询逻辑。
- 增加后端回归测试，覆盖 primary window 为月度、secondary window 为空周窗口的场景。
- 记录验证结果与剩余风险。

## 非目标

- 不改 provider API 调用地址和认证头。
- 不改批量检查选择、并发、汇总、aggregate 逻辑。
- 修复实现阶段不自行提交、推送或发版；后续已按用户授权完成提交、推送、合入 `master` 和发版。

## 验收

- 月度 Codex primary window 返回 `id=monthly` / `label=monthly`，不再被标成 `five-hour`。
- secondary 仅有时长、无展示数值时，不再额外形成空周额度行。
- Codex reset credits、订阅到期、plan 等既有 details 字段不回退。
- 聚焦后端测试与 server 编译通过。
- 修复提交已进入 `dev@61d34dfd` 与 `master@766ec81c`，并随 `v7.2.49-wx-2.9` 发布。
