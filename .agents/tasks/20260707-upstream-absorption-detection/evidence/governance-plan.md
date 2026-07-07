# 上游吸收治理方案

## 目标

对后端仓库执行一轮上游吸收检测干跑，判断 `upstream/main` 是否存在新内容、增量范围是什么、是否存在冲突，以及真实吸收前需要用户确认的事项。

## 范围

- 执行 fetch、固定上游目标 SHA。
- 生成仓库分析、更新清单、冲突预检和方案自评审报告。
- 输出候选合并前确认清单。

## 非目标

- 不执行真实合并。
- 不改业务代码。
- 不跑完整测试矩阵。
- 不提交、不推送、不合入发布分支、不发版。
- 不处理前端仓库。

## 分支/发版策略

- upstream_branch：`main`
- integration_branch：`dev`
- release_branch：`master`
- 发布候选 gate：本轮不进入；真实吸收后必须记录 `master_release_candidate_sha`。
- 标签 / 发布 触发条件：本轮不触发；真实发版需用户单独授权。
- 分支策略例外及理由：无。本轮仅检测。

## 授权边界

- 允许：fetch、只读 Git 分析、无写入 merge-tree、写入本任务治理记录。
- 需要再次确认：真实候选合并、冲突解决、提交、推送、合入 `${release_branch}`、标签、发布、部署。
- 禁止：未授权外部副作用和混入无关 ignored 本机文件。

## 任务拆分

- 后端仓库任务：`.agents/tasks/20260707-upstream-absorption-detection/`
- 前端仓库任务：本轮不涉及。
- 共享确认点：检测清单、冲突预检、是否进入候选合并。
- 不纳入本轮的改动：前端仓库、真实 merge、发布链路。
- 跨仓库证据落点：无。

## 阶段拆分

1. 仓库分析：已执行并写入 `evidence/repository-analysis.md`。
2. 新一轮治理方案：当前文件。
3. 检测上游状态：已执行 fetch 并固定 `upstream_target_sha`。
4. 生成上游更新清单：写入 `evidence/upstream-update-inventory.md`。
5. 冲突预检：写入 `evidence/conflict-precheck.md`。
6. 吸收方案多轮评审：本轮 干跑 做主线程自评审，写入 `evidence/plan-review-report.md`。
7. 发送确认清单：最终回复输出。
8. 候选合并：不执行。
9. 验证和合并后评审循环：不执行。
10. 提交推送：不执行。
11. 发布分支合入：不执行。
12. 发版申请或执行：不执行。
13. 收口：更新 progress / handoff，保留证据。

## 评审策略

- 方案评审触发条件：发现冲突、上游目标漂移、触碰 fork 自定义保护点。
- 独立评审 / 子代理触发条件：用户授权进入真实合并，或冲突解决触碰 `internal/api/server.go`、`internal/translator/`、auth/quota 等高风险模块。
- 合并后评审轮次：本轮不进入；真实合并后至少一轮主线程自评审，复杂冲突建议独立只读复评。
- finding disposition 规则：`fixed`、`accepted_risk`、`not_applicable`、`blocked`。
- 退出门禁：检测报告完整；冲突和风险已列明；未执行外部副作用。

## 停止条件

- 上游目标漂移：真实合并前必须重新 fetch 并核对 `upstream/main` 仍为 `8b9c4da2452b42aaa917a80daadf72aadc843a13`。
- fork 定制保护点不清：停止真实合并并补充保护点清单。
- 验证环境不可用：不得声明可合并/可发版。
- 评审发现阻断问题：先修复方案或等待用户确认。
- 需要外部副作用但未授权：停止。

## 验证策略

- 聚焦验证：真实合并后优先跑 `internal/api/server.go` 路由相关测试、auth/quota、usage、interactions 相关测试。
- 全量验证：`go test ./...` 与 `go build -o test-output ./cmd/server && rm test-output`。
- 发布后验证：按发布工作流、发布资产、GHCR manifest 和版本脚本核验。
