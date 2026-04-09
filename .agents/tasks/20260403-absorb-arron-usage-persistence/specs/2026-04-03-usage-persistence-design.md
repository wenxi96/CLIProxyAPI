# Usage 持久化吸收设计

## Goal

为当前仓库引入 usage 统计快照文件的自动恢复与自动保存能力，减少服务重启、热更新或异常退出导致的 usage 统计丢失。

## Background

当前仓库只在内存中维护 usage 统计。参考仓库已经补齐了一套基于快照文件的恢复与持久化能力，并通过 `Service` 生命周期进行托管。用户已确认本轮采用“分阶段完整吸收”，优先完成后端核心能力，不扩展管理 UI/TUI 配置编辑。

## In Scope

- `RequestStatistics` 增加持久化状态跟踪能力
- 新增 `internal/usage/persistence.go`
- `Service` 接入启动恢复、周期保存、关闭保存
- 配置项 `usage-statistics-persist-interval-seconds`
- 与上述能力直接相关的测试

## Non-Goals

- 不增加管理面板配置编辑端点
- 不增加 TUI 字段
- 不修改现有 usage 导入/导出接口结构
- 不吸收参考仓库其他大范围 runtime 改动

## Constraints

- 必须保持现有 usage 接口兼容
- 周期持久化关闭时，启动恢复与关闭保存仍应可用
- 变更范围控制在后端核心文件，不扩展到无关模块
- 保持中文注释与中文提交信息约定

## Options Considered

### 方案 1：只做快照文件读写与启动/关闭保存

- 优点：改动最小，风险最低
- 缺点：异常退出时仍可能丢最近一段统计，能力不完整

### 方案 2：分阶段完整吸收

- 优点：能补齐启动恢复、周期落盘、关闭保存三条主链路，风险仍可控制
- 缺点：需要改动 `Service` 生命周期和配置结构

### 方案 3：全量对齐参考仓库

- 优点：与参考仓库最接近
- 缺点：范围过大，会把本轮扩大成横向配置与界面改造

## Recommended Design

采用方案 2。

- 在 `internal/usage/logger_plugin.go` 中引入 change/persist 版本状态
- 新增 `internal/usage/persistence.go`，提供快照文件原子写入、加载、恢复与兼容旧文件名逻辑
- 在 `sdk/cliproxy/service.go` 中新增 usage 持久化生命周期方法
- 在 `internal/config/config.go` 与 `config.example.yaml` 中补齐周期落盘配置项
- 本轮不接入管理配置编辑端点与 TUI

## Risks

- `Service` 生命周期接入不当可能导致关闭阻塞或 goroutine 泄漏
- 持久化状态计数若处理错误，可能导致重复写盘或错误跳过写盘
- 配置热更新若没正确处理启停切换，可能出现周期任务失效

## Verification Strategy

- 为 `internal/usage` 补齐快照恢复/保存测试
- 为 `sdk/cliproxy` 补齐启动恢复、周期落盘、关闭保存相关测试
- 验证 `persist interval = 0` 与 `>0` 两种模式
- 验证重复恢复不会导致统计重复导入

## Open Questions / User Decisions

- None

## Need From User

- None
