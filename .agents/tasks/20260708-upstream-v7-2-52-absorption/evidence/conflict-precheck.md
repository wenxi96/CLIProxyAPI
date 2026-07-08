# 冲突预检

## 命令

```bash
git merge-tree --write-tree dev upstream/main
```

## 结果

```text
7c3fa7642c69cb326a256ddd43735c19465c2432
```

命令未输出冲突文件或冲突说明，当前判断为无机械冲突。

## 行为冲突风险

- `config.example.yaml` / `internal/config/config.go`：需保留 fork 配置模板定制。
- `internal/translator/**`：需确认 thinking 与 translator 既有架构不被破坏。
- `internal/runtime/executor/helps/usage_helpers.go`：需确认 stream usage 汇总与 fork 统计链路兼容。
- `sdk/cliproxy/auth/conductor.go`：需确认 invalid_grant 处理不影响 fork 自动禁用和刷新策略。
