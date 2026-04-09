# 任务说明

## 目标

吸收参考仓库中 usage 统计快照持久化能力，为当前仓库补齐启动恢复、周期落盘与关闭保存能力，降低服务重启或异常退出后的 usage 统计丢失风险。

## 范围

- 为 `internal/usage` 增加快照文件读写与恢复能力
- 为 `RequestStatistics` 增加持久化状态跟踪
- 在 `sdk/cliproxy/service.go` 中接入启动恢复、周期保存、关闭保存
- 增加配置项 `usage-statistics-persist-interval-seconds`
- 补齐对应单元测试

## 非目标

- 不在本轮新增管理面板的配置编辑入口
- 不扩展 TUI 配置界面
- 不重构现有 usage 管理接口返回格式
- 不顺手吸收参考仓库其他 executor/translator 改动

## 验收

- 服务启动时可从 usage 快照文件恢复统计数据
- usage 有变更时可按配置周期落盘到快照文件
- 服务关闭时会额外执行一次保存
- 配置项为 `0` 时禁用周期落盘，但保留启动恢复与关闭保存
- 相关测试通过
