# 仓库分析

## 本地规则

- 后端仓库规则入口：`AGENTS.md` 与 `.agents/README.md`。
- `.agents` Persistence Mode：`git-visible`。
- 代码类改动：先提交并推送到 `dev`，再合并到 `master` 并推送 `master`。
- `.agents` 治理文档类改动：只提交并推送到 `dev`，不得合入或污染 `master`。
- `master` 稳定发布分支当前树必须保持不包含 `.agents`。

## 分支与远端

- 当前分支：`dev`
- `origin/main`：`14b139661d98acbbd7ac19eb827754e78118736f`
- `upstream/main`：`14b139661d98acbbd7ac19eb827754e78118736f`
- `origin/dev`：`7e7bff89b1ba240bf6f2f75f4d577f1c86737e9d`
- `origin/master`：`b22de9c36378e75bd0a7c122b6332e232c25052e`
- `origin/master` 当前树 `.agents` 文件数：0

## Fork 保护点

- `.agents` 治理目录只在 `dev` 保留。
- fork release/install/CI 定制和版本后缀不能被上游差异误删。
- 近期新增的 batch quota、usage token/cost、auth 统计等 fork 能力不得因 stream usage 或配置吸收被破坏。
- `internal/translator/` 只能作为整体吸收的一部分修改，本任务满足该条件。
