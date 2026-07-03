# 后端上游 v7.2.49 合并吸收任务收口

## 当前状态

后端合并吸收任务已完成到“已提交、已推送、已合入 master、已发版并完成发布后复核”状态。

- 当前分支：`dev`
- 吸收目标：`upstream/main@f8334be8` / `v7.2.49`
- 吸收提交：`dev@7cd99f73`
- 发布合并：`master@766ec81c`
- 推送状态：`origin/dev@61d34dfd`；`origin/master@766ec81c`
- 发版状态：`v7.2.49-wx-2.9`

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

合并候选阶段已执行并通过：

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

发布后已复核：

- `git ls-remote --heads origin dev master`：远端 `dev` / `master` 指向预期提交。
- `git ls-remote --tags origin v7.2.49-wx-2.9`：tag 指向 `master@766ec81c`。
- GitHub release workflow：`completed/success`。
- GitHub docker-image workflow：`completed/success`。
- Release 页面与 Linux amd64 资产返回 HTTP 200。
- GHCR 镜像 `ghcr.io/wenxi96/cli-proxy-api:7.2.49-wx-2.9` 与 `latest` manifest 可读取。

## 评审结果

已完成主线程自评审，未发现需要修复的实质问题。

重点确认：

- 候选 diff 只包含上游吸收项与治理记录。
- fork 近期额度查询、自动禁用、scoped routing、release workflow 未被覆盖。
- 后端测试覆盖从聚焦测试扩展到全量 `go test ./...`。

## 剩余工作

无本任务剩余提交、推送或发版工作。

## 剩余风险

- 未执行真实 provider 在线流式请求验证。
- 任务完成后上游 `main` 已继续前进；后续上游增量应另建吸收任务处理。
