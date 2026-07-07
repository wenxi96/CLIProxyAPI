# 后端发版核验报告

## 范围

- 仓库：`wenxi96/CLIProxyAPI`
- 上游基线：`router-for-me/CLIProxyAPI@v7.2.51`
- 集成分支：`dev`
- 发布分支：`master`
- 发版标签：`v7.2.51-wx-2.11`

## 分支与提交

- `origin/dev`: `148089b320f3667cd5ea246b933fe8c7b3add806`
- `origin/master`: `d02d8926de99d38a80f3dc5b7ee78c75a6f0ae06`
- `master` 已包含本次 dev 合并提交：`git branch --contains 148089b320f3667cd5ea246b933fe8c7b3add806 --all` 包含 `remotes/origin/master`。

## 发布候选门禁

- 发布 worktree：`~/.agents/worktrees/wenxi96/CLIProxyAPI/master-v7-2-51-absorption`
- master 发布候选提交：`d02d8926de99d38a80f3dc5b7ee78c75a6f0ae06`
- 复验命令：
  - `git diff --check -- ':!.agents'`
  - `rg -n '^(<<<<<<<|=======|>>>>>>>)' . --glob '!.agents/**'`
  - Docker Go 1.26 `go test ./...`
  - Docker Go 1.26 `go build -o test-output ./cmd/server && rm test-output`
- 结论：复验通过，未发现冲突标记、空白错误、测试失败或构建失败。

## 版本脚本

在实际 master 发布候选 上执行：

```text
bash ./scripts/version.sh auto-release
BASE_TAG=v7.2.51
RELEASE_TAG=v7.2.51-wx-2.11
VERSION=7.2.51-wx-2.11
FULL_COMMIT=d02d8926de99d38a80f3dc5b7ee78c75a6f0ae06
```

## Tag 核验

- 本地创建轻量 tag：`v7.2.51-wx-2.11`
- 已推送：`git push origin v7.2.51-wx-2.11`
- 远端核验：`git ls-remote --tags origin v7.2.51-wx-2.11` 返回 `d02d8926de99d38a80f3dc5b7ee78c75a6f0ae06`。

## GitHub 发布

- 发布地址：`https://github.com/wenxi96/CLIProxyAPI/releases/tag/v7.2.51-wx-2.11`
- 状态：`draft=false`，`prerelease=false`
- 发布者：`github-actions[bot]`
- 资产：包含 `checksums.txt` 与多平台构建包，资产状态为 `uploaded`。
- `checksums.txt` 下载核验：`curl -I -L` 返回 `HTTP/2 200`，`content-length: 1164`。

## GitHub Actions 核验

- `release` run `28850560622`: `completed/success`
- `docker-image` run `28850560592`: `completed/success`
- `rebuild-release-history` run `28850437707`: `completed/skipped`，符合该 workflow 的 master push 条件。

## Docker / GHCR

- 镜像：`ghcr.io/wenxi96/cli-proxy-api:7.2.51-wx-2.11`
- Manifest digest：`sha256:71b2306a2e639e2d8699e8bfe60c3c25546aec8cfe5f9bb54ff2e4f81854ef3b`
- 平台：`linux/amd64`、`linux/arm64`
- 命令：`docker buildx imagetools inspect ghcr.io/wenxi96/cli-proxy-api:7.2.51-wx-2.11`

## 结论

后端 `v7.2.51-wx-2.11` 发版链路完成，分支、tag、GitHub Release、发布资产、Actions 和 GHCR manifest 均已核验通过。
