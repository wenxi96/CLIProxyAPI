Release Closeout Status
- workflow.operation.name: release_closeout
- workflow.operation.status: complete
- workflow.rollout.status: completed
- workflow.verification.status: pass
- workflow.runtime_health.status: not_applicable
- workflow.rollback.status: limited
- workflow.followup.status: disclosed

## 发布摘要

- 摘要: 发布后端认证文件 token 与估算金额统计能力。
- 发布类型: package_publish

## 发布范围

- 已包含: 后端 usage 认证文件维度聚合、单认证文件请求明细 API、auth-files usage 摘要合并、相关测试与治理记录。
- 未包含: 前端 UI 发布由前端仓库 `v1.17.8-wx-2.10` 单独承载；真实 provider 账单金额不在本次后端范围内。
- 范围边界: 发布到 fork `wenxi96/CLIProxyAPI` 的 GitHub Release 和 GHCR 镜像，不包含运行实例热更新。

## 制品与目标

- 制品引用: GitHub Release `https://github.com/wenxi96/CLIProxyAPI/releases/tag/v7.2.49-wx-2.10`; GHCR `ghcr.io/wenxi96/cli-proxy-api:7.2.49-wx-2.10`
- Commit / Tag / Version: `master@07be8ef6fde08e27eacd069801dee4689efbcdc9`; `v7.2.49-wx-2.10`; version `7.2.49-wx-2.10`
- 目标: GitHub Release 资产与 GHCR Docker 镜像。
- 渠道: Git tag push 触发 GitHub Actions `release` 与 `docker-image`。
- 发布依赖: GitHub Actions、GitHub Release、GHCR。

## Rollout 记录

- 触发方式: `git push origin v7.2.49-wx-2.10`
- 阶段: tag 创建、release workflow、docker-image workflow、资产与镜像核验均完成。
- 最终发布结果: completed
- Rollout Ref: build: `release` run `28651471567`; build: `docker-image` run `28651471614`; release: `v7.2.49-wx-2.10`

## 验证

- 验证项: 远端 tag 与 master 目标提交一致
  - 结果: pass
  - 验证引用: command: `git ls-remote --tags origin v7.2.49-wx-2.10`
- 验证项: GitHub Actions 发布流程
  - 结果: pass
  - 验证引用: build: `release` run `28651471567` completed/success; build: `docker-image` run `28651471614` completed/success
- 验证项: Release 资产
  - 结果: pass
  - 验证引用: release: GitHub Release API 返回 11 个 uploaded assets；direct download for Linux/Windows/checksums assets returned HTTP 200
- 验证项: Docker 镜像
  - 结果: pass
  - 验证引用: command: `docker manifest inspect ghcr.io/wenxi96/cli-proxy-api:7.2.49-wx-2.10`

## 运行健康与监控

- 观察窗口: 发布后制品可用性核验窗口。
- 健康摘要: not_applicable；本次是 Release/GHCR 制品发布，没有切换运行中服务或生产流量。
- 监控信号: Release 资产 HTTP 200、GitHub Actions success、GHCR manifest 可解析。
- 证据引用: manual: release asset checks; build: GitHub Actions runs; command: docker manifest inspect

## 回滚与恢复

- 回滚路径: 如发现制品问题，可删除/下架错误 release/tag 与 GHCR tag，并基于修复后的 `master` 重新发布递增 tag。
- 当前姿态: limited
- 限制: 已发布制品可能已被用户下载；删除 tag/release/GHCR tag 是外部副作用，需要再次确认。
- 恢复说明: 无数据库迁移和不可逆数据变更；代码级恢复可通过后续修复 tag 发布完成。

## 文档与沟通

- 已更新文档: 本任务 `closeout.md`、`progress.md`、`handoff.md`。
- 已发送沟通: 本会话内同步发布与核验状态。
- 支持交接: `handoff.md` 已更新为 released 状态。
- 剩余文档 / 沟通工作: 无；本文件记录已完成的发布收口事实，是否已持久化入库以包含该文件的 Git 提交历史为准。

## 已知问题与后续项

- 已知问题: 无。

## 需要用户提供

- 需要: 无。
- 说明: 后端发布收口已完成；提交、推送或清理本地 stash 属于后续仓库操作，不属于发布收口剩余工作。
