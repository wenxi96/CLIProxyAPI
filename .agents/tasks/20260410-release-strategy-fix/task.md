# 任务：修复版本发布策略

## 目标

- 修复 fork 发布链路中快照版本号回退的问题。
- 统一后端与前端的版本生成规则，避免构建版和正式版混淆。
- 修正 release notes 的变更范围口径，使其优先基于上一正式 fork 版本。

## 范围

- 后端 `scripts/release-lib.sh`、`scripts/version.sh`、`scripts/version.ps1`、`scripts/release-notes.sh`
- 前端 `scripts/release-lib.sh`、`scripts/version.sh`、`scripts/release-notes.sh`

## 非目标

- 本轮不创建新的正式 tag。
- 本轮不直接发布新的 GitHub Release。
- 本轮不调整现有正式版历史记录。

## 验收

- 后端当前快照版号从 `6.9.16-wx.1.1` 正确推进到 `6.9.16-wx.1.2-build.<sha>`。
- 正式 release 模式仍能解析既有 tag 为正式版本号。
- release notes 的“自定义提交”不再从上游基线全量累计，而是优先从上一正式 fork 版本起算。
