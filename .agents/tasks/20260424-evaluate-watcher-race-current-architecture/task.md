# 任务：评估当前 watcher 架构下的竞态风险与等价修复路径

## 目标

基于当前主仓库 `internal/watcher` 的真实实现，重新评估参考提交 `e509adc9` 所修复的问题在当前架构下是否仍然存在，并给出是否需要修复、如果需要则应如何以当前架构方式修复的结论。

## 背景

- 参考仓库 `CLIProxyAPI2` 中的 `e509adc9` 依赖 `suppressedAuth`、`pendingAuthWrites`、`scheduleAuthWrite`、`flushPendingAuthWrite` 等机制。
- 当前主仓库 watcher 已不是该实现路径，而是以：
  - `handleEvent`
  - `addOrUpdateClient`
  - `removeClient`
  - `dispatchAuthUpdates`
  - `refreshAuthState`
  为主的增量更新模型。
- 因此不能直接将 `e509adc9` 作为小补丁照搬，必须先做当前架构下的等价性判断。

## 范围

- 核对 `internal/watcher/events.go`
- 核对 `internal/watcher/clients.go`
- 核对 `internal/watcher/dispatcher.go`
- 核对相关 watcher 测试覆盖面
- 明确当前架构下是否存在与参考提交同类的 race / duplicate processing 风险
- 若存在，提出当前架构版修复方案与最小测试集

## 非目标

- 本任务不直接吸收 `CLIProxyAPI2` 的 watcher 代码
- 本任务不直接修改 watcher 实现，除非评估完成后再进入单独实施任务
- 本任务不处理其他 Tier B / Claude 指纹 / 功能扩展项

## 验收

- 给出“当前是否存在等价竞态”的结论
- 给出“无需修复 / 需要修复 / 需要更多证据”三选一判断
- 若需要修复，明确当前架构下的最小改动面
- 若无需修复，明确为什么参考提交前提在主仓库不成立
- 给出建议的测试补强点

## 初步问题清单

- `handleEvent` 当前对 `Remove|Rename` 与 `Create|Write` 的处理是否可能在原子替换场景下重复触发增删？
- `dispatchRuntimeAuthUpdate` 与文件事件增量更新是否可能造成重复更新或错误删除？
- `shouldDebounceRemove` 的 remove debounce 是否足以覆盖当前架构下的 rename/remove 抖动？
- 当前测试是否覆盖“删除后快速重建”“原子替换”“runtime auth 与文件 auth 交错更新”等场景？

## 预期输出

1. 风险判断结论
2. 证据点列表
3. 若需修复的代码落点
4. 若不需修复的关闭理由
5. 下一步实施建议
