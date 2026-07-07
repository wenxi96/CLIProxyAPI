---
Status: complete
Created: 2026-07-07
Owner: Codex
---

# 后端上游 v7.2.51 吸收执行

## 目标

在隔离 worktree 中将后端上游 `upstream/main@8b9c4da2452b42aaa917a80daadf72aadc843a13` 吸收到 fork 的 `dev` 集成链路，解决冲突，完成评审和验证，并在通过后按仓库规则推进提交、推送、合入 `master` 与发版核验。

## 输入

- 检测任务：`.agents/tasks/20260707-upstream-absorption-detection/`
- 后端上游目标：`8b9c4da2452b42aaa917a80daadf72aadc843a13`
- 后端上游 tag：`v7.2.51`
- 已知冲突：`internal/api/server.go`

## 范围

- 创建或复用隔离 linked worktree。
- 合并固定上游目标到从 `dev` 切出的候选分支。
- 解决 `internal/api/server.go` 冲突，并保留 fork 管理端路由、usage、batch-check、scoped-pool、quota-threshold 能力。
- 吸收上游 safe mode、WebsocketAuth 默认配置、quota backoff guard、Google Interactions 等能力。
- 运行聚焦验证、全量 Go 验证、diff 检查、冲突标记扫描和多轮评审。
- 验证通过后按授权推进提交、推送、`master` 合入和发版核验。

## 非目标

- 不处理前端代码；前端吸收由前端仓库自己的任务承载。
- 不覆盖当前主工作树中既有未提交治理记录。
- 不保留密钥、token、cookie 或私密配置。

## 分支变量

- `upstream_branch`: `main`
- `integration_branch`: `dev`
- `release_branch`: `master`
- `candidate_branch`: `codex/upstream-v7-2-51-absorption`
- `candidate_worktree`: `~/.agents/worktrees/wenxi96/CLIProxyAPI/upstream-v7-2-51-absorption`

## 授权边界

- 用户已确认进入真实吸收阶段。
- 若上游目标 SHA 变化、验证出现无法自动收口的问题、或需要强推/破坏性操作，必须停止并重新确认。

## 验收条件

- 后端候选分支完成上游合并且无冲突标记。
- `internal/api/server.go` 同时保留 fork 定制和上游新增能力。
- 聚焦验证和全量 Go 验证完成，失败项已修复或明确阻塞。
- 多轮评审最后一轮无新增未处理问题。
- 按仓库规则完成提交、推送、`master` 合入和发版核验，或明确停在用户需要确认的边界。
