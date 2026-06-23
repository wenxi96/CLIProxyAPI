# 任务说明

## 目标

在现有“零额度自动禁用认证文件”能力基础上，新增全局额度阈值禁用能力。默认行为必须兼容旧配置；当用户配置阈值后，支持真实额度查询的认证文件在剩余额度达到阈值时自动禁用并持久化。

## 范围

- 自动禁用总开关以新命名 `quota-exceeded.auto-disable-auth-file-on-low-quota` 表达低额度语义，同时兼容读取旧配置键 `quota-exceeded.auto-disable-auth-file-on-zero-quota`
- 新增全局阈值配置 `quota-exceeded.auto-disable-auth-file-quota-threshold-percent`
- 扩展异步 quota check 后的自动禁用判断
- 明确该能力对 `fill-first` 和 `round-robin` 都生效
- 明确 scoped-pool 阈值移出与自动禁用阈值之间的优先级和互不替代关系
- 增加管理 API、示例配置、TUI 配置项、配置 diff 与相关测试

## 非目标

- 不实现 provider 级阈值配置
- 不实现主动定时额度扫描
- 不改变 scoped-pool 现有阈值移出语义
- 不修改不支持真实额度查询 provider 的判断策略
- 不自动提交、推送或触发部署

## 验收

- 未配置新阈值时，旧零额度自动禁用行为不变
- 设置阈值为 `10` 后，`RemainingPercent <= 10` 会自动禁用
- `RemainingPercent` 高于阈值时不会禁用，除非 `Exhausted=true`
- `RemainingPercent=nil` 且非耗尽分类时不会按阈值禁用
- 明确零额度或等价耗尽写入 `auto_disabled_quota_exhausted`
- 非零阈值命中写入 `auto_disabled_quota_threshold`
- `fill-first` 和 `round-robin` 下自动禁用阈值都生效
- scoped-pool 阈值只影响 round-robin scoped-pool 池内剔除，不持久禁用
- 同时命中 scoped-pool 阈值和自动禁用阈值时，最终状态为 `disabled`
- 旧配置键 `auto-disable-auth-file-on-zero-quota` 可读取为新总开关值，保存时收敛到 `auto-disable-auth-file-on-low-quota`
- 管理 API 新端点使用 `auto-disable-auth-file-on-low-quota`，旧 `auto-disable-auth-file-on-zero-quota` 端点继续兼容并委托到同一配置
- 管理 API、示例配置、TUI 配置项、配置 diff 与测试同步更新
