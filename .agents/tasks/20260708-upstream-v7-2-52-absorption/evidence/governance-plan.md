# 治理方案

## 目标

将上游 `v7.2.52` 的 7 个新增提交吸收到 fork `dev`，保持 fork 定制和发布分支治理边界。

## 阶段

1. 合并前确认
   - 固定 SHA、更新清单、冲突预检、方案评审。
   - 输出确认清单等待用户确认。

2. 候选合并
   - 重新 fetch。
   - 核验 `upstream/main` 仍为 `14b139661d98acbbd7ac19eb827754e78118736f`。
   - 执行 `git merge --no-commit --no-ff 14b139661d98acbbd7ac19eb827754e78118736f`。

3. 冲突处理
   - 若出现冲突，按 fork 定制优先、上游新增能力叠加原则处理。
   - 记录冲突解决报告；无冲突也记录。

4. 验证与评审
   - 聚焦验证：
     - `go test ./sdk/cliproxy/auth/...`
     - `go test ./internal/runtime/executor/...`
     - `go test ./internal/translator/...`
     - `go test ./sdk/api/handlers/openai/...`
     - `go test ./sdk/cliproxy/...`
   - 全量验证：
     - `go test ./...`
     - `go build -o test-output ./cmd/server && rm test-output`
   - 基础检查：
     - `git diff --check`
     - `rg -n "^(<<<<<<<|=======|>>>>>>>)" .`
   - 若本机 Go 不可用，使用 Docker Go 等价命令。

5. 提交推送
   - 用户授权后提交并推送 `dev`。

6. 合入 `master` 与发版
   - 用户授权后合入 `master`。
   - 合入 `master` 前删除 `.agents`，并核验 `git ls-tree -r HEAD -- .agents` 为空。
   - 发版前核验版本脚本、构建、测试和 release 资产。

## 停止条件

- 合并前上游目标 SHA 变化。
- 出现未解决 high/critical 评审发现。
- 测试/构建失败且无法在本轮修复。
- `master` 候选树出现 `.agents`。

## 当前授权

- 已授权推送检测治理记录。
- 已授权启动吸收任务。
- 尚未授权候选合并、代码提交、推送、合入 `master` 或发版。
