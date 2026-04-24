# 任务：制定 CLIProxyAPI2 改动吸收计划

## 目标

基于 `/home/cheng/git-project/CLIProxyAPI2` 的对比结果，为当前主仓库制定一套完整、分批、可执行的吸收方案，优先纳入安全与运行时正确性修复，避免重复吸收已等价存在的改动，并明确每一批的验证与落地顺序。

## 范围

- 核对 `CLIProxyAPI2` 提供的 Tier S / Tier A / Tier B / Tier C 建议项
- 判断每个提交在当前主仓库中的状态：
  - 已等价吸收
  - 建议直接吸收
  - 建议部分移植
  - 建议暂缓
  - 不建议吸收
- 形成后续实施批次、风险说明、验证方案和提交切分建议

## 非目标

- 本任务不直接吸收代码
- 本任务不修改 release 历史
- 本任务不调整当前分支模型
- 本任务不触发新的发布

## 验收

- 形成一份可直接执行的吸收计划
- 对 Tier S / Tier A 至少给出逐项处置建议
- 明确哪些提交已在主仓库等价存在，避免重复劳动
- 明确哪些提交不能直接 cherry-pick，而应手工移植
- 明确下一步建议的批次顺序、验证命令和风险边界

## 当前状态

- 本轮直接执行范围已闭环：
  - `5833fb3a` 已按当前主仓库架构落地、验证并提交推送
  - `81ce6550` 已确认主仓库原本等价吸收，无需执行
- 本任务卡对应的整体吸收计划尚未全部完成
- 后续仍待处理的内容包括：
  - `e509adc9` 的当前架构下等价竞态评估
  - `bb99f7b4` 的补核
  - Tier B / Claude 指纹系列 / 功能扩展线的后续专项任务

## 计划分批

### 批次 0：已吸收项归档，不重复处理

以下提交经核对，当前主仓库已存在等价实现或覆盖，不建议再次吸收：

- `8210c76d` `fix(security): validate auth file names`
- `02a486c6` `fix: reject oversized downloads instead of truncating`
- `3904e319` `fix(security): harden management panel asset updater`
- `bf06e433` `fix(auth): prevent stale ModelStates inheritance from disabled auth entries`
- `f9259505` `fix(codex): strip websocket model prefixes upstream`
- `47d1d127` `fix: keep a fallback turn for system-only Claude inputs`
- `4c0d7c76` `test: verify remaining user message after system merge`
- `b4efc3cd` `fix: map OpenAI system messages to Claude top-level system`
- `5c23abf3` `fix(codex): strip stream_options from Responses API requests`
- `81ce6550` `Handle Codex capacity errors as retryable`
- `2d897a79` `fix(usage): persist stats across config changes`
- `398b0f5b` `fix(auth): restore filestore project id hydration`
- `823332cf` `feat(api): support batch auth file upload and delete`

处理方式：

- 仅在实施文档中记录“已覆盖”
- 不做 cherry-pick
- 后续若出现行为差异，再回到具体测试层补核

### 批次 1：安全与正确性第一波

这是当前最值得优先落地的一批，目标是先补齐真正会留下安全或错误恢复风险的修复。

建议纳入：

- `5833fb3a`
  - 目标：外部 auth 更新同步回 model registry
  - 处理：按当前 service/watcher 结构手工移植

### 批次 2：运行时正确性补强

这批收益较高，但优先级略低于批次 1，需要以“当前主仓库是否已有等价处理”为前提再决定是否纳入：

- `bb99f7b4`
  - 核对 `sdk/translator/registry.go` fallback 路径是否仍可能泄漏前缀
  - 若命中路径存在，再吸收

### 批次 2.5：当前架构下另立评估

以下改动不再按参考补丁直接执行，而是需要先判断当前主仓库架构下是否存在等价问题：

- `e509adc9`
  - 当前主仓库 watcher 不存在参考补丁依赖的 `suppressedAuth`、`pendingAuthWrites`、`scheduleAuthWrite`、`flushPendingAuthWrite` 子系统
  - 应先基于当前 `handleEvent / addOrUpdateClient / removeClient / dispatchAuthUpdates` 架构重新评估
  - 只有确认存在同类竞态后，才另做当前架构版修复

### 批次 3：性能与稳定性专项评估

以下改动不建议与批次 1 混做，需独立验证：

- `31026cdb`
- `03f62814`
- `2bd7c74a`
- `d34cdd0c`

处理原则：

- 单独建任务卡
- 必须附性能基线、压测场景和回滚策略
- 不与安全修复混成一个提交

### 批次 4：Claude 指纹稳定性专项

以下提交归为一个专题，不与当前主链路混合推进：

- `6f02529d`
- `e8aaca7e`
- `74730aac`
- `8ca4c202`
- `ae1d7e91`
- `c7b5907e`
- `f36a579b`
- `6aa81ac5`
- `ed0e297b`

处理原则：

- 若当前主要风险集中在 OpenAI / Gemini / Codex，则可暂缓
- 若 Claude 是长期主链路，再单独开展专题吸收和真实账号回归

### 批次 5：功能扩展线

以下改动归入“产品路线评估”而非立即吸收：

- `8ebb93d5`
  - 当前主仓库默认行为已等价实现：`invalid_request_error` 已直接停止重试
  - 该提交真正新增的是可配置参数 `max-invalid-request-retries`
  - 是否暴露该配置项，按产品路线决定
- `4ba0f758`
- `aeae7478`
- `f5f70a6e`
- `ea217b0b`
- `4e35f965`

## 不建议吸收

- `2420ffe0`：当前发布矩阵不需要
- `e60452f2`：面向旧 backport 收尾，已过时
- `c248504a`：与旧 backport 背景强相关，需谨慎，默认不纳入当前计划
- `6705f691`：配置语义翻转，对现有用户有 breaking change 风险

## 实施建议

### 推荐顺序

1. 先做批次 1
2. 先收口 `5833fb3a` 的测试与提交
3. 再评估批次 2 与批次 2.5
4. 批次 3 / 4 / 5 单独立项，不与当前主线混合

### 推荐提交切分

当前建议收缩后，更适合拆成 2 个提交：

1. `fix(runtime): sync external auth lifecycle updates into model registry`
2. `docs(agents): rescope watcher race item into architecture-specific evaluation`

## 校验方法约束

后续对“建议吸收 / 未吸收”的判断，不再使用“grep 函数名或提交说明关键字 0 hits”作为主要依据。

统一改为以下顺序：

1. 先 `git show <commit>` 提取参考仓库提交的核心行为改动
2. 再到主仓库搜索实际逻辑是否已经以内联或不同函数名方式存在
3. 只有在行为级证据缺失时，才判定为“未吸收”

特别说明：

- 对 `project_id hydration`、`model prefix stripping`、`download size rejection` 这类逻辑，必须按行为核对，不以函数名是否一致为准
- 对“默认行为已等价，但参考仓库新增了可配置开关”的提交，应归入功能扩展而非缺陷修复

### 验证建议

- `go test ./internal/managementasset ./internal/api/handlers/management ./internal/watcher ./sdk/...`
- 最少补以下定点回归：
  - watcher 在 auth 文件删除/重建时不误吞写事件
  - 外部 auth 更新后 model registry 能同步

### 风险提示

- `8ebb93d5` 当前已归入功能扩展，暂不吸收；若未来决定暴露可配置 invalid-request 重试开关，再单独评估行为影响
- `5833fb3a` 涉及 watcher / lifecycle，需要避免 external auth 事件与内部 apply 路径形成自循环
- `e509adc9` 当前不应按参考实现直接落地，需先确认现有 watcher 架构下是否真的存在同类竞态
