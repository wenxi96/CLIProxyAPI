# 冲突预检

## 命令

```bash
git merge-tree --write-tree dev upstream/main
```

## 结果

返回合成树：

```text
7c3fa7642c69cb326a256ddd43735c19465c2432
```

命令未输出冲突文件或冲突说明，当前判断为无机械冲突。

## 注意事项

- 原始 `git diff --name-status dev..upstream/main` 会显示大量 `.agents`、release/install、usage 和 fork 定制文件删除，这是因为上游仓库不包含 fork 治理和定制内容；这些不应被简单解释为本轮应删除内容。
- 真正新增范围以 `git log dev..upstream/main` 的 7 个提交为准。
- 行为风险集中在 auth、executor、translator、Codex/OpenAI 模型配置和 stream usage 处理。
