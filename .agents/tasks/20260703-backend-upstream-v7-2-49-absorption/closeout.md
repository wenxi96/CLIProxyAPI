# 后端上游 v7.2.49 合并吸收任务收口

## 当前状态

后端合并吸收任务已完成到“已合并候选、已验证、已自评审、未提交”状态。

- 当前分支：`dev`
- 吸收目标：`upstream/main@f8334be8` / `v7.2.49`
- 当前方式：`git merge --no-commit --no-ff upstream/main`
- 提交状态：未提交
- 推送状态：未推送
- 发版状态：未发版

## 已完成范围

已吸收上游后端改动：

- Responses SSE / WebSocket forwarder 修复。
- Codex WS-to-SSE full transcript replay 修复。
- OpenAI Responses reasoning fallback。
- Claude Sonnet 5 registry 与 Claude thinking sampling 修正。
- public pluginhost SDK auth provider / plugin metadata 方法扩展。
- README 多语言 VisionCoder 链接更新。

治理记录已落地：

- `task.md`
- `findings.md`
- `progress.md`
- `handoff.md`
- `closeout.md`
- `evidence/20260703-backend-merge-verification.md`
- `evidence/20260703-backend-self-review.md`

## 冲突解决

后端实际合并无内容冲突。

## 验证

已执行并通过：

```bash
docker run --rm -v "$PWD":/workspace -w /workspace golang:1.26 go test -buildvcs=false ./sdk/cliproxy/auth ./sdk/api/handlers/openai ./internal/runtime/executor ./internal/registry
```

```bash
docker run --rm -v "$PWD":/workspace -w /workspace golang:1.26 go build -buildvcs=false -o test-output ./cmd/server && rm test-output
```

```bash
docker run --rm -v "$PWD":/workspace -w /workspace golang:1.26 go test -buildvcs=false ./...
```

```bash
git diff --check
rg -n "^(<<<<<<<|=======|>>>>>>>)" .
```

补充说明：当前环境没有本机 `go` 命令，因此按仓库既有方式使用 Docker `golang:1.26` 验证。

## 评审结果

已完成主线程自评审，未发现需要修复的实质问题。

重点确认：

- 候选 diff 只包含上游吸收项与治理记录。
- fork 近期额度查询、自动禁用、scoped routing、release workflow 未被覆盖。
- 后端测试覆盖从聚焦测试扩展到全量 `go test ./...`。

## 剩余工作

需要用户后续明确授权后才能执行：

- 提交当前后端合并候选。
- 推送 `dev` / 合入 `master`。
- 创建或推送 release tag。

## 剩余风险

- 未执行真实 provider 在线流式请求验证。
- 当前合并候选仍处于工作区，尚未形成 Git commit。
