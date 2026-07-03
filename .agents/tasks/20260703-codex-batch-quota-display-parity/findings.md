# Findings

## 已确认事实

- 单文件刷新 A 路径在前端 `buildCodexQuotaWindows` 中按 `limit_window_seconds` 分类窗口：`18000` 为 5 小时，`604800` 为周，28-31 天区间为月度。
- 后端 batch-check B 路径虽然已经复用 canonical quota query service，但 `extractCodexWindows` 仍保留了旧的槽位语义：`rate_limit.primary_window` 固定映射为 `five-hour`，`secondary_window` 固定映射为 `weekly`。
- 当 provider 返回 `primary_window.limit_window_seconds=2592000` 时，A 路径展示为月度；旧 B 路径会展示为 5 小时。
- 当 `secondary_window` 只有 `limit_window_seconds=604800` 且无 `used_percent` / `remaining_percent` / reset 数值时，旧 B 路径仍会生成空 weekly 行，导致前端批量卡片出现空周额度。

## 根因

后端 Codex window 映射仍按 primary/secondary 位置硬编码语义，没有复用单文件刷新侧已经验证过的“按窗口时长分类”规则。

## 已排除

- 不是前端 i18n label 映射问题：前端已有 id 到 labelKey 的映射，但后端传入的 id 本身错误。
- 不是 reset credits 或订阅到期字段缺失导致：本次异常发生在 window 分类和空窗口展示层。
