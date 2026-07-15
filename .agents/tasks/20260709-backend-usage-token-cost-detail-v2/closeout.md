Release Closeout Status
- workflow.operation.name: release_closeout
- workflow.operation.status: complete
- workflow.rollout.status: completed
- workflow.verification.status: pass
- workflow.runtime_health.status: not_applicable
- workflow.rollback.status: limited
- workflow.followup.status: none

## 发布摘要

- 摘要: 发布后端请求级 token、缓存、推理与凭证用量明细契约升级。
- 发布类型: package_publish

## 发布范围

- 已包含: canonical request detail、凭证明细与持久化/导入导出、usage 去重与 enrichment、redis queue 同源派生、client IP/request ID 统一、主要 provider streaming/WebSocket usage 采集和回归测试。
- 未包含: 前端展示由前端仓库独立发布；不包含真实 provider 账单、数据库迁移或运行实例热更新。
- 范围边界: 发布到 fork `wenxi96/CLIProxyAPI` 的 GitHub Release 和 GHCR，不切换生产流量。

## 制品与目标

- 制品引用: release: `https://github.com/wenxi96/CLIProxyAPI/releases/tag/v7.2.52-wx-2.13`; package: `ghcr.io/wenxi96/cli-proxy-api:7.2.52-wx-2.13`
- Commit / Tag / Version: `master@5f1c36461513bc555e93823112992f3cb876c938`; `v7.2.52-wx-2.13`; version `7.2.52-wx-2.13`
- 目标: GitHub Release 多平台归档、checksums 与 GHCR 多架构镜像。
- 渠道: `v*` tag push 触发 GitHub Actions `release` 和 `docker-image`。
- 发布依赖: GitHub Actions、GitHub Release、GHCR。

## Rollout 记录

- 触发方式: command: `git push origin v7.2.52-wx-2.13`
- 发布阶段: dev 代码/治理分离提交、master 仅代码 cherry-pick、master 验证与推送、tag 发布、Release 与 GHCR 后验收均完成。
- 最终发布结果: completed
- Rollout Ref: build: `release#29403076268`; build: `docker-image#29403076015`; release: `v7.2.52-wx-2.13`

## 验证

- 验证项: master release candidate
  - 结果: pass
  - 验证引用: command: `go test -count=1 ./...`; command: `go build -o test-output ./cmd/server`
- 验证项: 远端 refs 与治理边界
  - 结果: pass
  - 验证引用: command: `git ls-remote --heads --tags origin dev master refs/tags/v7.2.52-wx-2.13`; command: `git ls-tree -r origin/master -- .agents` 无输出
- 验证项: GitHub Actions
  - 结果: pass
  - 验证引用: build: `release#29403076268` completed/success; build: `docker-image#29403076015` completed/success
- 验证项: Release 资产
  - 结果: pass
  - 验证引用: release: 11 个 assets 均为 uploaded；`checksums.txt` 覆盖 10 个归档；Linux amd64 range download 返回 HTTP 206
- 验证项: GHCR 镜像
  - 结果: pass
  - 验证引用: package: digest `sha256:7545bb4c2968f2789cb5fb7e5a9023e78a52e5a93c0766a0de17694ce39374ef`；linux/amd64 与 linux/arm64 manifest 可解析；version/latest/sha alias digest 一致

## 运行健康与监控

- 观察窗口: tag 推送至 Release 和 GHCR 制品完成后的可用性核验窗口。
- 健康摘要: not_applicable；本次只发布可下载制品和容器镜像，没有部署或切换运行中的 CPA 服务。
- 监控信号: Actions success、Release asset state、下载 HTTP 状态、GHCR manifest/digest。
- 证据引用: build: GitHub Actions runs; release: GitHub Release API; package: Docker buildx imagetools inspect

## 回滚与恢复

- 回滚路径: 使用上一正式版本 `v7.2.52-wx-2.12` 或 `ghcr.io/wenxi96/cli-proxy-api:7.2.52-wx-2.12`；代码问题通过后续修复 tag 发布。
- 当前姿态: limited
- 限制: 已下载制品不可召回；删除当前 tag、Release 或 GHCR tag 属于额外外部副作用，需要重新授权。
- 恢复说明: 无数据库迁移或不可逆数据变更。

## 文档与沟通

- 已更新文档: 本任务 `task.md`、`progress.md`、`handoff.md`、`closeout.md` 与 release closeout review。
- 已发送沟通: 本会话持续同步提交、master 验证、tag、Actions 与制品状态。
- 支持交接: `handoff.md` 已更新为 released 状态。
- 剩余文档 / 沟通工作: 无。

## 已知问题与后续项

- 已知问题: 无。

## 需要用户提供

None
