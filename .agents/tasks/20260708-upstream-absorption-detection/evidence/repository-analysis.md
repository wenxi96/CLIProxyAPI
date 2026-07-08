# 仓库分析

## 仓库与分支

- 仓库：`CLIProxyAPI`
- 当前分支：`dev`
- 集成分支：`dev`
- 发布分支：`master`
- 上游分支：`upstream/main`
- `origin/main`：`14b139661d98acbbd7ac19eb827754e78118736f`
- `upstream/main`：`14b139661d98acbbd7ac19eb827754e78118736f`
- `origin/dev`：`4f57db691a20934c237f57e90c1d5d28a4533d02`
- `origin/master`：`b22de9c36378e75bd0a7c122b6332e232c25052e`

## 仓库规则

- 代码类改动：先提交并推送到 `dev`，再合并到 `master` 并推送 `master`。
- `.agents` 治理文档类改动：只提交并推送到 `dev`，不得合入或污染 `master`。
- `master` 稳定发布分支当前树必须保持不包含 `.agents`。

## 检测说明

- 首次 `git fetch --all --tags --prune` 时，`origin/main` 已更新，但 `upstream` 因 TLS 中断失败。
- 后续按 remote 重试后，`upstream/main` 和 tag 拉取成功。
- 本轮没有执行候选合并。
