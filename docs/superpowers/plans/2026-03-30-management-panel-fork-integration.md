# 管理面板 Fork 默认化接入 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让 `CLIProxyAPI` fork 默认从 `920293630/Cli-Proxy-API-Management-Center` 的 GitHub Release 拉取 `management.html`，并把前端仓库纳入与后端一致的 fork 分支治理与自动同步体系。

**Architecture:** 前端仓库继续负责 React/Vite 单文件页面的构建与 Release 发布，后端仓库继续负责运行时下载和托管 `/management.html`。后端新增“自定义前端仓库时禁用官方 fallback”的严格来源绑定；前端仓库补齐 `main/master/dev/feature/*` 分支模型、每日上游同步工作流和维护文档，确保前后端 fork 的默认行为一致且可验证。

**Tech Stack:** Go 1.x、GitHub Actions、GitHub Releases、YAML、Markdown、React 19、Vite 7、Bash、GitHub CLI

---

## File Map

### 后端仓库 `/home/cheng/git-project/CLIProxyAPI`

- Modify: `/home/cheng/git-project/CLIProxyAPI/config.example.yaml`
  - 默认把 `remote-management.panel-github-repository` 指向用户自己的前端 fork。
- Modify: `/home/cheng/git-project/CLIProxyAPI/README.md`
  - 英文说明后端默认管理面板来源与 fork 场景。
- Modify: `/home/cheng/git-project/CLIProxyAPI/README_CN.md`
  - 中文说明后端默认管理面板来源与 fork 场景。
- Modify: `/home/cheng/git-project/CLIProxyAPI/docs/fork-maintainer-workflow.md`
  - 增加“前端面板 fork 联动维护”说明。
- Modify: `/home/cheng/git-project/CLIProxyAPI/docs/fork-maintainer-workflow_CN.md`
  - 增加“前端面板 fork 联动维护”说明。
- Modify: `/home/cheng/git-project/CLIProxyAPI/internal/managementasset/updater.go`
  - 将 Release 地址解析与 fallback 策略拆开；自定义仓库禁用官方 fallback。
- Create: `/home/cheng/git-project/CLIProxyAPI/internal/managementasset/updater_test.go`
  - 为自定义仓库 strict source binding、新旧默认仓库判定与 fallback 决策补单元测试。

### 前端仓库 `/home/cheng/git-project/Cli-Proxy-API-Management-Center`

- Create: `/home/cheng/git-project/Cli-Proxy-API-Management-Center/.github/workflows/sync-upstream.yml`
  - 每日把 `origin/main` 与 `upstream/main` 做 fast-forward 同步。
- Create: `/home/cheng/git-project/Cli-Proxy-API-Management-Center/docs/fork-maintainer-workflow.md`
  - 说明前端 repo 的 `main/master/dev/feature/*` 分支职责与 Release 约束。
- Create: `/home/cheng/git-project/Cli-Proxy-API-Management-Center/docs/fork-maintainer-workflow_CN.md`
  - 同上，中文版。
- Modify: `/home/cheng/git-project/Cli-Proxy-API-Management-Center/README.md`
  - 补 fork 维护文档入口。
- Modify: `/home/cheng/git-project/Cli-Proxy-API-Management-Center/README_CN.md`
  - 补 fork 维护文档入口。

### 临时验证产物

- Create during verification: `/tmp/cpa-panel-fork.yaml`
  - 临时本地验证用后端配置副本。
- Create during verification: `/tmp/cpa-panel-release/management.html`
  - 从前端 fork Release 下载的 `management.html` 基准文件。
- Create during verification: `/tmp/cpa-panel-static/management.html`
  - 运行时下载得到的管理面板静态文件。

---

### Task 1: 前端仓库建立 fork 分支治理与自动同步

**Files:**
- Create: `/home/cheng/git-project/Cli-Proxy-API-Management-Center/.github/workflows/sync-upstream.yml`
- Create: `/home/cheng/git-project/Cli-Proxy-API-Management-Center/docs/fork-maintainer-workflow.md`
- Create: `/home/cheng/git-project/Cli-Proxy-API-Management-Center/docs/fork-maintainer-workflow_CN.md`
- Modify: `/home/cheng/git-project/Cli-Proxy-API-Management-Center/README.md`
- Modify: `/home/cheng/git-project/Cli-Proxy-API-Management-Center/README_CN.md`

- [ ] **Step 1: 初始化前端仓库的 `main/master/dev/feature/*` 分支骨架**

```bash
cd /home/cheng/git-project/Cli-Proxy-API-Management-Center
git fetch origin
git fetch upstream main
git checkout main
git merge --ff-only upstream/main
git push origin main

git show-ref --verify --quiet refs/heads/master || git branch master main
git show-ref --verify --quiet refs/heads/dev || git branch dev master
git push -u origin master
git push -u origin dev

git checkout -B feature/fork-governance-bootstrap dev
```

Expected:
- `git merge --ff-only upstream/main` 成功结束
- `origin/main` 与 `upstream/main` 一致
- 本地已有 `master`、`dev`、`feature/fork-governance-bootstrap`

- [ ] **Step 2: 创建前端仓库的每日上游同步工作流**

在 `/home/cheng/git-project/Cli-Proxy-API-Management-Center/.github/workflows/sync-upstream.yml` 写入以下内容：

```yaml
name: sync-upstream

on:
  schedule:
    # GitHub Actions cron uses UTC. 01:17 UTC = 09:17 Asia/Shanghai / Asia/Hong_Kong.
    - cron: '17 1 * * *'
  workflow_dispatch:

permissions:
  contents: write

concurrency:
  group: sync-upstream-main
  cancel-in-progress: false

jobs:
  sync:
    runs-on: ubuntu-latest

    steps:
      - name: Checkout default branch
        uses: actions/checkout@v4
        with:
          fetch-depth: 0

      - name: Configure Git identity
        run: |
          git config user.name "github-actions[bot]"
          git config user.email "41898282+github-actions[bot]@users.noreply.github.com"

      - name: Sync fork from upstream
        env:
          UPSTREAM_REPO: router-for-me/Cli-Proxy-API-Management-Center
          TARGET_BRANCH: main
        run: |
          set -euo pipefail

          git remote add upstream "https://github.com/${UPSTREAM_REPO}.git" 2>/dev/null || \
            git remote set-url upstream "https://github.com/${UPSTREAM_REPO}.git"

          git fetch origin "${TARGET_BRANCH}"
          git fetch upstream main
          git checkout -B "${TARGET_BRANCH}" "origin/${TARGET_BRANCH}"

          if [ "$(git rev-parse HEAD)" = "$(git rev-parse "upstream/main")" ]; then
            echo "Already up to date with upstream/main."
            exit 0
          fi

          if git merge-base --is-ancestor HEAD "upstream/main"; then
            git merge --ff-only "upstream/main"
            git push origin "HEAD:${TARGET_BRANCH}"
            exit 0
          fi

          if git merge-base --is-ancestor "upstream/main" HEAD; then
            echo "Fork main is ahead of upstream/main; refusing to overwrite fork-only commits."
            exit 1
          fi

          echo "Fork main has diverged from upstream/main; manual resolution required."
          exit 1
```

- [ ] **Step 3: 添加前端 fork 维护文档**

在 `/home/cheng/git-project/Cli-Proxy-API-Management-Center/docs/fork-maintainer-workflow_CN.md` 写入以下内容：

````md
# Fork 维护工作流

这个前端 fork 使用与后端一致的分层分支模型，以便把上游同步、前端开发和管理面板发布彻底分开。

## 分支职责

- `main`：上游镜像分支，始终对齐 `upstream/main`
- `master`：fork 的稳定分支，同时也是当前 GitHub 默认分支
- `dev`：集成分支，用来吸收上游更新和已完成的前端功能开发
- `feature/*`：实际开发分支，从 `dev` 拉出，短期存在

## 为什么要这样设计

这套模型把四件事拆开了：

1. 上游前端发布了什么
2. fork 当前认定的稳定管理面板版本是什么
3. 当前正在集成什么
4. 当前还在开发中的内容是什么

这样可以保证 `main` 保持干净，也能把“可发布的 `management.html`”和“仍在开发中的前端页面”彻底分离。

## 每日上游同步

默认分支 `master` 中包含工作流文件 `.github/workflows/sync-upstream.yml`。

这个工作流会：

- 每天北京时间 09:17 运行一次
- 支持手动 `workflow_dispatch`
- 把 `origin/main` 与 `upstream/main` 对齐
- 只允许 fast-forward 更新
- 如果 `main` 上存在 fork 专属提交，则直接失败，不会强制覆盖

注意：工作流文件放在 `master` 上，但它真正更新的是 `main`。

## 推荐流程

### 1. 让自动化更新 `main`

正常情况下，GitHub Actions 会每天早上自动更新 `origin/main`。

如果需要，也可以在 GitHub Actions 页面里手动触发 `sync-upstream`。

### 2. 把上游更新合并到 `dev`

```bash
git checkout dev
git pull origin dev
git merge main
```

上游冲突统一在 `dev` 里解决，不要在 `master` 里处理。

### 3. 从 `dev` 拉出前端功能分支

```bash
git checkout dev
git pull origin dev
git checkout -b feature/my-ui-change
```

### 4. 功能完成后先回到 `dev`

```bash
git checkout dev
git merge feature/my-ui-change
git push origin dev
```

### 5. 验证通过后再推进到 `master`

```bash
git checkout master
git pull origin master
git merge dev
git push origin master
```

### 6. 仅从 `master` 打发布标签

```bash
git checkout master
git pull origin master
git tag v2026.03.30-fork.1
git push origin v2026.03.30-fork.1
```

只有 `master` 上的已验证提交才允许生成 `management.html` Release。

## 本地手动同步命令

如果你想手动同步本地上游镜像分支，可以执行：

```bash
git checkout main
git pull
git push
```

当前仓库应配置成在 `main` 分支上：

- `git pull` 从 `upstream/main` 拉取
- `git push` 推送到 `origin/main`

## 与后端仓库的关系

- 这个仓库负责生成 `management.html`
- 后端仓库负责通过 `remote-management.panel-github-repository` 下载并托管该文件
- 如果后端默认指向你的前端 fork，那么这里的 Release 就会成为 `/management.html` 的真实来源

## 维护规则

- 不要直接在 `main` 上开发
- 不要把未完成工作直接放进 `master`
- 只从 `master` 打面板发布标签
- `feature/*` 分支尽量保持短生命周期
- 把 `master` 理解为“已验证的前端稳定状态”，而不是“最新上游状态”
````

在 `/home/cheng/git-project/Cli-Proxy-API-Management-Center/docs/fork-maintainer-workflow.md` 写入以下内容：

````md
# Fork Maintainer Workflow

This frontend fork uses the same layered branch model as the backend so upstream sync, UI development, and `management.html` release publishing stay separate.

## Branch Roles

- `main`: upstream mirror branch. Keep this branch aligned with `upstream/main`.
- `master`: stable fork branch and the default branch on GitHub for this fork.
- `dev`: integration branch for upstream updates and completed UI work.
- `feature/*`: short-lived development branches created from `dev`.

## Why This Model Exists

This setup separates four concerns:

1. What upstream shipped
2. What the fork considers stable
3. What is currently being integrated
4. What is still under active development

That keeps `main` clean and ensures only validated UI builds become published `management.html` release assets.

## Daily Upstream Sync

The default branch `master` contains the workflow file `.github/workflows/sync-upstream.yml`.

That workflow:

- runs every day at 09:17 Asia/Shanghai / Asia/Hong_Kong time
- supports manual `workflow_dispatch`
- syncs `origin/main` from `upstream/main`
- only allows fast-forward updates
- fails instead of overwriting fork-only commits on `main`

Important: the workflow lives on `master`, but it updates `main`.

## Recommended Flow

### 1. Let automation update `main`

In normal operation, the GitHub Actions workflow updates `origin/main` every morning.

If needed, you can also trigger `sync-upstream` manually from the GitHub Actions page.

### 2. Bring upstream changes into `dev`

```bash
git checkout dev
git pull origin dev
git merge main
```

Resolve conflicts in `dev`, not in `master`.

### 3. Start new UI work from `dev`

```bash
git checkout dev
git pull origin dev
git checkout -b feature/my-ui-change
```

### 4. Merge feature work back into `dev`

```bash
git checkout dev
git merge feature/my-ui-change
git push origin dev
```

### 5. Promote validated work to `master`

```bash
git checkout master
git pull origin master
git merge dev
git push origin master
```

### 6. Publish releases only from `master`

```bash
git checkout master
git pull origin master
git tag v2026.03.30-fork.1
git push origin v2026.03.30-fork.1
```

Only validated `master` commits should produce release assets.

## Local Sync Commands

If you want to sync the local upstream mirror branch manually:

```bash
git checkout main
git pull
git push
```

This repository should be configured so that on `main`:

- `git pull` pulls from `upstream/main`
- `git push` pushes to `origin/main`

## Relationship To The Backend Repository

- This repository produces `management.html`.
- The backend repository downloads and serves that file through `remote-management.panel-github-repository`.
- If the backend fork defaults to your frontend fork, this repository's releases become the real source of `/management.html`.

## Rules Of Thumb

- Do not develop directly on `main`.
- Do not use `master` for unfinished work.
- Publish `management.html` releases only from `master`.
- Keep `feature/*` branches short-lived.
- Treat `master` as "validated frontend state", not "latest upstream state".
````

- [ ] **Step 4: 在前端 README 中补维护文档入口**

在 `/home/cheng/git-project/Cli-Proxy-API-Management-Center/README.md` 的 “Contributing” 段落后追加这一行：

```md
Fork maintainers who want to keep this UI fork aligned with upstream while publishing their own `management.html` releases can follow [docs/fork-maintainer-workflow.md](docs/fork-maintainer-workflow.md).
```

在 `/home/cheng/git-project/Cli-Proxy-API-Management-Center/README_CN.md` 的 “贡献” 段落后追加这一行：

```md
如果你维护的是自己的管理面板 fork，并希望同时保留上游镜像、稳定分支和开发分支，可参考 [docs/fork-maintainer-workflow_CN.md](docs/fork-maintainer-workflow_CN.md)。
```

- [ ] **Step 5: 检查工作流和文档变更**

Run:

```bash
cd /home/cheng/git-project/Cli-Proxy-API-Management-Center
git diff --check
rg -n "sync-upstream|fork-maintainer-workflow|management\\.html releases" .github/workflows README.md README_CN.md docs
```

Expected:
- `git diff --check` 无输出且退出码为 `0`
- `rg` 输出命中 `.github/workflows/sync-upstream.yml`、README 和两份维护文档

- [ ] **Step 6: 提交前端治理改动**

Run:

```bash
cd /home/cheng/git-project/Cli-Proxy-API-Management-Center
git add .github/workflows/sync-upstream.yml docs/fork-maintainer-workflow.md docs/fork-maintainer-workflow_CN.md README.md README_CN.md
git commit -m "治理: 补充前端 fork 同步工作流与维护文档"
```

Expected:
- 生成 1 个提交，提交信息为 `治理: 补充前端 fork 同步工作流与维护文档`

- [ ] **Step 7: 推进前端分支并将默认分支切到 `master`**

Run:

```bash
cd /home/cheng/git-project/Cli-Proxy-API-Management-Center
git checkout dev
git merge --ff-only feature/fork-governance-bootstrap
git push origin dev

git checkout master
git merge --ff-only dev
git push origin master

gh repo edit 920293630/Cli-Proxy-API-Management-Center --default-branch master
gh repo view 920293630/Cli-Proxy-API-Management-Center --json defaultBranchRef -q .defaultBranchRef.name
```

Expected:
- `dev` 与 `master` 都包含新的治理提交
- 最后一条命令输出 `master`

---

### Task 2: 后端默认切换到用户自己的前端 fork

**Files:**
- Modify: `/home/cheng/git-project/CLIProxyAPI/config.example.yaml`
- Modify: `/home/cheng/git-project/CLIProxyAPI/README.md`
- Modify: `/home/cheng/git-project/CLIProxyAPI/README_CN.md`
- Modify: `/home/cheng/git-project/CLIProxyAPI/docs/fork-maintainer-workflow.md`
- Modify: `/home/cheng/git-project/CLIProxyAPI/docs/fork-maintainer-workflow_CN.md`

- [ ] **Step 1: 从后端 `dev` 拉出默认来源切换分支**

Run:

```bash
cd /home/cheng/git-project/CLIProxyAPI
git checkout dev
git pull origin dev
git checkout -b feature/panel-fork-source-default
```

Expected:
- 当前分支为 `feature/panel-fork-source-default`

- [ ] **Step 2: 修改后端示例配置的默认前端仓库地址**

将 `/home/cheng/git-project/CLIProxyAPI/config.example.yaml` 中这一行：

```yaml
  panel-github-repository: "https://github.com/router-for-me/Cli-Proxy-API-Management-Center"
```

替换为：

```yaml
  panel-github-repository: "https://github.com/920293630/Cli-Proxy-API-Management-Center"
```

- [ ] **Step 3: 在后端 README 中补默认面板来源说明**

在 `/home/cheng/git-project/CLIProxyAPI/README.md` 的 “Management API” 小节之后追加：

```md
Fork maintainers who publish their own `Cli-Proxy-API-Management-Center` fork can point `remote-management.panel-github-repository` to that fork so `/management.html` downloads their own `management.html` release asset by default.
```

在 `/home/cheng/git-project/CLIProxyAPI/README_CN.md` 的 “管理 API 文档” 小节之后追加：

```md
如果你同时维护自己的 `Cli-Proxy-API-Management-Center` fork，可以把 `remote-management.panel-github-repository` 指向该 fork，这样 `/management.html` 默认会下载你自己的 `management.html` 发布产物。
```

- [ ] **Step 4: 在后端 fork 维护文档中增加前端联动说明**

在 `/home/cheng/git-project/CLIProxyAPI/docs/fork-maintainer-workflow.md` 末尾追加：

````md
## Frontend Panel Fork

If you also maintain your own `Cli-Proxy-API-Management-Center` fork, keep that repository on the same `main/master/dev/feature/*` model and point `remote-management.panel-github-repository` at your fork.

Recommended default:

```yaml
remote-management:
  panel-github-repository: "https://github.com/920293630/Cli-Proxy-API-Management-Center"
```

That keeps `/management.html` sourced from your own frontend release pipeline instead of the upstream panel repository.
````

在 `/home/cheng/git-project/CLIProxyAPI/docs/fork-maintainer-workflow_CN.md` 末尾追加：

````md
## 前端管理面板 Fork

如果你同时维护自己的 `Cli-Proxy-API-Management-Center` fork，建议让前端仓库也采用同样的 `main/master/dev/feature/*` 模型，并把 `remote-management.panel-github-repository` 指向你的前端 fork。

推荐默认值：

```yaml
remote-management:
  panel-github-repository: "https://github.com/920293630/Cli-Proxy-API-Management-Center"
```

这样 `/management.html` 的真实来源就会是你自己的前端发布流水线，而不是上游面板仓库。
````

- [ ] **Step 5: 验证后端配置与文档默认值**

Run:

```bash
cd /home/cheng/git-project/CLIProxyAPI
git diff --check
rg -n "920293630/Cli-Proxy-API-Management-Center" config.example.yaml README.md README_CN.md docs/fork-maintainer-workflow.md docs/fork-maintainer-workflow_CN.md
```

Expected:
- `git diff --check` 无输出且退出码为 `0`
- `rg` 输出 5 个文件都命中用户前端 fork 地址

- [ ] **Step 6: 提交后端默认来源文档与配置变更**

Run:

```bash
cd /home/cheng/git-project/CLIProxyAPI
git add config.example.yaml README.md README_CN.md docs/fork-maintainer-workflow.md docs/fork-maintainer-workflow_CN.md
git commit -m "配置: 默认切换管理面板前端来源"
```

Expected:
- 生成 1 个提交，提交信息为 `配置: 默认切换管理面板前端来源`

---

### Task 3: 后端实现自定义前端仓库的严格来源绑定

**Files:**
- Modify: `/home/cheng/git-project/CLIProxyAPI/internal/managementasset/updater.go`
- Test: `/home/cheng/git-project/CLIProxyAPI/internal/managementasset/updater_test.go`

- [ ] **Step 1: 先写失败测试，定义新行为边界**

在 `/home/cheng/git-project/CLIProxyAPI/internal/managementasset/updater_test.go` 写入以下内容：

```go
package managementasset

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestResolveManagementReleaseSource(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name          string
		repo          string
		wantURL       string
		wantFallback  bool
		wantErr       bool
	}{
		{
			name:         "empty uses default repo and fallback",
			repo:         "",
			wantURL:      defaultManagementReleaseURL,
			wantFallback: true,
		},
		{
			name:         "official github repo keeps fallback",
			repo:         "https://github.com/router-for-me/Cli-Proxy-API-Management-Center",
			wantURL:      defaultManagementReleaseURL,
			wantFallback: true,
		},
		{
			name:         "official api repo keeps fallback",
			repo:         "https://api.github.com/repos/router-for-me/Cli-Proxy-API-Management-Center/releases/latest",
			wantURL:      defaultManagementReleaseURL,
			wantFallback: true,
		},
		{
			name:         "custom github repo disables fallback",
			repo:         "https://github.com/920293630/Cli-Proxy-API-Management-Center",
			wantURL:      "https://api.github.com/repos/920293630/Cli-Proxy-API-Management-Center/releases/latest",
			wantFallback: false,
		},
		{
			name:         "custom api repo disables fallback",
			repo:         "https://api.github.com/repos/920293630/Cli-Proxy-API-Management-Center/releases/latest",
			wantURL:      "https://api.github.com/repos/920293630/Cli-Proxy-API-Management-Center/releases/latest",
			wantFallback: false,
		},
		{
			name:    "invalid custom repo returns error",
			repo:    "not-a-url",
			wantErr: true,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := resolveManagementReleaseSource(tc.repo)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.releaseURL != tc.wantURL {
				t.Fatalf("releaseURL = %q, want %q", got.releaseURL, tc.wantURL)
			}
			if got.allowFallback != tc.wantFallback {
				t.Fatalf("allowFallback = %v, want %v", got.allowFallback, tc.wantFallback)
			}
		})
	}
}

func TestEnsureLatestManagementHTML_CustomRepoSkipsFallbackWhenReleaseFetchFails(t *testing.T) {
	tempDir := t.TempDir()

	prevFetch := fetchLatestAssetFunc
	prevDownload := downloadAssetFunc
	t.Cleanup(func() {
		fetchLatestAssetFunc = prevFetch
		downloadAssetFunc = prevDownload
	})

	fetchLatestAssetFunc = func(context.Context, *http.Client, string) (*releaseAsset, string, error) {
		return nil, "", errors.New("release lookup failed")
	}

	fallbackCalls := 0
	downloadAssetFunc = func(context.Context, *http.Client, string) ([]byte, string, error) {
		fallbackCalls++
		return []byte("<html>fallback</html>"), "fallback-hash", nil
	}

	ok := EnsureLatestManagementHTML(context.Background(), tempDir, "", "https://github.com/920293630/Cli-Proxy-API-Management-Center")
	if ok {
		t.Fatal("expected sync to fail when custom repo has no release and no local file")
	}
	if fallbackCalls != 0 {
		t.Fatalf("expected no fallback download, got %d", fallbackCalls)
	}
	if _, err := os.Stat(filepath.Join(tempDir, ManagementFileName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected no local management file, got err=%v", err)
	}
}

func TestEnsureLatestManagementHTML_CustomRepoKeepsExistingLocalFileWhenReleaseFetchFails(t *testing.T) {
	tempDir := t.TempDir()
	localPath := filepath.Join(tempDir, ManagementFileName)
	if err := os.WriteFile(localPath, []byte("<html>existing</html>"), 0o644); err != nil {
		t.Fatalf("seed local management file: %v", err)
	}

	prevFetch := fetchLatestAssetFunc
	prevDownload := downloadAssetFunc
	t.Cleanup(func() {
		fetchLatestAssetFunc = prevFetch
		downloadAssetFunc = prevDownload
	})

	fetchLatestAssetFunc = func(context.Context, *http.Client, string) (*releaseAsset, string, error) {
		return nil, "", errors.New("release lookup failed")
	}

	fallbackCalls := 0
	downloadAssetFunc = func(context.Context, *http.Client, string) ([]byte, string, error) {
		fallbackCalls++
		return []byte("<html>fallback</html>"), "fallback-hash", nil
	}

	ok := EnsureLatestManagementHTML(context.Background(), tempDir, "", "https://github.com/920293630/Cli-Proxy-API-Management-Center")
	if !ok {
		t.Fatal("expected sync to keep existing local file when custom repo release lookup fails")
	}
	if fallbackCalls != 0 {
		t.Fatalf("expected no fallback download, got %d", fallbackCalls)
	}
	body, err := os.ReadFile(localPath)
	if err != nil {
		t.Fatalf("read local management file: %v", err)
	}
	if string(body) != "<html>existing</html>" {
		t.Fatalf("expected existing local file to remain unchanged, got %q", string(body))
	}
}

func TestEnsureLatestManagementHTML_DefaultRepoUsesFallbackWhenReleaseFetchFails(t *testing.T) {
	tempDir := t.TempDir()

	prevFetch := fetchLatestAssetFunc
	prevDownload := downloadAssetFunc
	t.Cleanup(func() {
		fetchLatestAssetFunc = prevFetch
		downloadAssetFunc = prevDownload
	})

	fetchLatestAssetFunc = func(context.Context, *http.Client, string) (*releaseAsset, string, error) {
		return nil, "", errors.New("release lookup failed")
	}

	fallbackCalls := 0
	downloadAssetFunc = func(context.Context, *http.Client, string) ([]byte, string, error) {
		fallbackCalls++
		return []byte("<html>fallback</html>"), "fallback-hash", nil
	}

	ok := EnsureLatestManagementHTML(context.Background(), tempDir, "", "")
	if !ok {
		t.Fatal("expected default repo to allow fallback download")
	}
	if fallbackCalls != 1 {
		t.Fatalf("expected one fallback download, got %d", fallbackCalls)
	}
	body, err := os.ReadFile(filepath.Join(tempDir, ManagementFileName))
	if err != nil {
		t.Fatalf("read local management file: %v", err)
	}
	if string(body) != "<html>fallback</html>" {
		t.Fatalf("unexpected local management file contents: %q", string(body))
	}
}
```

- [ ] **Step 2: 运行测试并确认当前实现失败**

Run:

```bash
cd /home/cheng/git-project/CLIProxyAPI
go test ./internal/managementasset -run 'TestResolveManagementReleaseSource|TestEnsureLatestManagementHTML_' -count=1
```

Expected:
- 测试失败
- 输出中至少包含 `undefined: resolveManagementReleaseSource`、`undefined: fetchLatestAssetFunc` 或等价的编译失败信息

- [ ] **Step 3: 在下载器中实现 release 来源解析与 fallback 开关**

将 `/home/cheng/git-project/CLIProxyAPI/internal/managementasset/updater.go` 按以下方式修改：

```go
var (
	lastUpdateCheckMu   sync.Mutex
	lastUpdateCheckTime time.Time
	currentConfigPtr    atomic.Pointer[config.Config]
	schedulerOnce       sync.Once
	schedulerConfigPath atomic.Value
	sfGroup             singleflight.Group

	fetchLatestAssetFunc = fetchLatestAsset
	downloadAssetFunc    = downloadAsset
)

type managementReleaseSource struct {
	releaseURL    string
	allowFallback bool
}

func resolveManagementReleaseSource(repo string) (managementReleaseSource, error) {
	repo = strings.TrimSpace(repo)
	if repo == "" {
		return managementReleaseSource{
			releaseURL:    defaultManagementReleaseURL,
			allowFallback: true,
		}, nil
	}

	parsed, err := url.Parse(repo)
	if err != nil || parsed.Host == "" {
		return managementReleaseSource{}, fmt.Errorf("invalid panel github repository: %q", repo)
	}

	host := strings.ToLower(parsed.Host)
	parsed.Path = strings.TrimSuffix(parsed.Path, "/")

	switch host {
	case "github.com":
		parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
		if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
			return managementReleaseSource{}, fmt.Errorf("invalid github repository path: %q", repo)
		}
		owner := parts[0]
		repoName := strings.TrimSuffix(parts[1], ".git")
		releaseURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", owner, repoName)
		return managementReleaseSource{
			releaseURL:    releaseURL,
			allowFallback: strings.EqualFold(owner, "router-for-me") && strings.EqualFold(repoName, "Cli-Proxy-API-Management-Center"),
		}, nil

	case "api.github.com":
		lowerPath := strings.ToLower(parsed.Path)
		if !strings.HasSuffix(lowerPath, "/releases/latest") {
			parsed.Path = parsed.Path + "/releases/latest"
			lowerPath = strings.ToLower(parsed.Path)
		}
		return managementReleaseSource{
			releaseURL:    parsed.String(),
			allowFallback: strings.HasPrefix(lowerPath, "/repos/router-for-me/cli-proxy-api-management-center/"),
		}, nil
	}

	return managementReleaseSource{}, fmt.Errorf("unsupported panel github repository host: %q", parsed.Host)
}

func EnsureLatestManagementHTML(ctx context.Context, staticDir string, proxyURL string, panelRepository string) bool {
	if ctx == nil {
		ctx = context.Background()
	}

	staticDir = strings.TrimSpace(staticDir)
	if staticDir == "" {
		log.Debug("management asset sync skipped: empty static directory")
		return false
	}
	localPath := filepath.Join(staticDir, managementAssetName)

	_, _, _ = sfGroup.Do(localPath, func() (interface{}, error) {
		lastUpdateCheckMu.Lock()
		now := time.Now()
		timeSinceLastAttempt := now.Sub(lastUpdateCheckTime)
		if !lastUpdateCheckTime.IsZero() && timeSinceLastAttempt < managementSyncMinInterval {
			lastUpdateCheckMu.Unlock()
			log.Debugf(
				"management asset sync skipped by throttle: last attempt %v ago (interval %v)",
				timeSinceLastAttempt.Round(time.Second),
				managementSyncMinInterval,
			)
			return nil, nil
		}
		lastUpdateCheckTime = now
		lastUpdateCheckMu.Unlock()

		localFileMissing := false
		if _, errStat := os.Stat(localPath); errStat != nil {
			if errors.Is(errStat, os.ErrNotExist) {
				localFileMissing = true
			} else {
				log.WithError(errStat).Debug("failed to stat local management asset")
			}
		}

		if errMkdirAll := os.MkdirAll(staticDir, 0o755); errMkdirAll != nil {
			log.WithError(errMkdirAll).Warn("failed to prepare static directory for management asset")
			return nil, nil
		}

		releaseSource, err := resolveManagementReleaseSource(panelRepository)
		if err != nil {
			log.WithError(err).Warn("failed to resolve management panel release source")
			return nil, nil
		}

		client := newHTTPClient(proxyURL)

		localHash, err := fileSHA256(localPath)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				log.WithError(err).Debug("failed to read local management asset hash")
			}
			localHash = ""
		}

		asset, remoteHash, err := fetchLatestAssetFunc(ctx, client, releaseSource.releaseURL)
		if err != nil {
			if localFileMissing && releaseSource.allowFallback {
				log.WithError(err).Warn("failed to fetch latest management release information, trying fallback page")
				if ensureFallbackManagementHTML(ctx, client, localPath) {
					return nil, nil
				}
				return nil, nil
			}
			log.WithError(err).Warn("failed to fetch latest management release information")
			return nil, nil
		}

		if remoteHash != "" && localHash != "" && strings.EqualFold(remoteHash, localHash) {
			log.Debug("management asset is already up to date")
			return nil, nil
		}

		data, downloadedHash, err := downloadAssetFunc(ctx, client, asset.BrowserDownloadURL)
		if err != nil {
			if localFileMissing && releaseSource.allowFallback {
				log.WithError(err).Warn("failed to download management asset, trying fallback page")
				if ensureFallbackManagementHTML(ctx, client, localPath) {
					return nil, nil
				}
				return nil, nil
			}
			log.WithError(err).Warn("failed to download management asset")
			return nil, nil
		}

		if remoteHash != "" && !strings.EqualFold(remoteHash, downloadedHash) {
			log.Errorf("management asset digest mismatch: expected %s got %s — aborting update for safety", remoteHash, downloadedHash)
			return nil, nil
		}

		if err = atomicWriteFile(localPath, data); err != nil {
			log.WithError(err).Warn("failed to update management asset on disk")
			return nil, nil
		}

		log.Infof("management asset updated successfully (hash=%s)", downloadedHash)
		return nil, nil
	})

	_, err := os.Stat(localPath)
	return err == nil
}

func ensureFallbackManagementHTML(ctx context.Context, client *http.Client, localPath string) bool {
	data, downloadedHash, err := downloadAssetFunc(ctx, client, defaultManagementFallbackURL)
	if err != nil {
		log.WithError(err).Warn("failed to download fallback management control panel page")
		return false
	}

	log.Warnf("management asset downloaded from fallback URL without digest verification (hash=%s) — "+
		"enable verified GitHub updates by keeping disable-auto-update-panel set to false", downloadedHash)

	if err = atomicWriteFile(localPath, data); err != nil {
		log.WithError(err).Warn("failed to persist fallback management control panel page")
		return false
	}

	log.Infof("management asset updated from fallback page successfully (hash=%s)", downloadedHash)
	return true
}
```

- [ ] **Step 4: 重新运行测试并确认严格来源绑定通过**

Run:

```bash
cd /home/cheng/git-project/CLIProxyAPI
go test ./internal/managementasset -run 'TestResolveManagementReleaseSource|TestEnsureLatestManagementHTML_' -count=1
```

Expected:
- 输出 `ok  	github.com/router-for-me/CLIProxyAPI/v6/internal/managementasset`

- [ ] **Step 5: 提交后端下载器的来源绑定实现**

Run:

```bash
cd /home/cheng/git-project/CLIProxyAPI
git add internal/managementasset/updater.go internal/managementasset/updater_test.go
git commit -m "修复: 自定义管理面板仓库禁用官方回退"
```

Expected:
- 生成 1 个提交，提交信息为 `修复: 自定义管理面板仓库禁用官方回退`

---

### Task 4: 生成前端 Release 并验证后端实际拉取的是用户 fork

**Files:**
- Create during verification: `/tmp/cpa-panel-fork.yaml`
- Create during verification: `/tmp/cpa-panel-release/management.html`
- Create during verification: `/tmp/cpa-panel-static/management.html`

- [ ] **Step 1: 从前端 `master` 打一个测试发布标签**

Run:

```bash
cd /home/cheng/git-project/Cli-Proxy-API-Management-Center
git checkout master
git pull origin master

test_tag="v2026.03.30-fork.1"
git rev-parse "$test_tag" >/dev/null 2>&1 || git tag "$test_tag"
git push origin "$test_tag"
```

Expected:
- 标签 `v2026.03.30-fork.1` 已存在于 `origin`

- [ ] **Step 2: 等待前端 Release 产出 `management.html`**

Run:

```bash
rm -rf /tmp/cpa-panel-release
mkdir -p /tmp/cpa-panel-release
gh release view v2026.03.30-fork.1 \
  --repo 920293630/Cli-Proxy-API-Management-Center \
  --json assets \
  -q '.assets[].name'
gh release download v2026.03.30-fork.1 \
  --repo 920293630/Cli-Proxy-API-Management-Center \
  --pattern management.html \
  --dir /tmp/cpa-panel-release
test -f /tmp/cpa-panel-release/management.html
```

Expected:
- 输出包含 `management.html`
- `/tmp/cpa-panel-release/management.html` 已下载完成

- [ ] **Step 3: 准备后端本地验证配置并清空静态缓存**

Run:

```bash
cd /home/cheng/git-project/CLIProxyAPI
cp config.example.yaml /tmp/cpa-panel-fork.yaml
python3 - <<'PY'
from pathlib import Path
p = Path("/tmp/cpa-panel-fork.yaml")
text = p.read_text()
text = text.replace('secret-key: ""', 'secret-key: "test-management-key"', 1)
if 'panel-github-repository: "https://github.com/920293630/Cli-Proxy-API-Management-Center"' not in text:
    raise SystemExit("expected config.example.yaml to default to the forked management panel repository")
p.write_text(text)
PY
rm -rf /tmp/cpa-panel-static
mkdir -p /tmp/cpa-panel-static
```

Expected:
- `/tmp/cpa-panel-fork.yaml` 存在
- `/tmp/cpa-panel-static` 是空目录

- [ ] **Step 4: 启动后端并请求 `/management.html`**

Run:

```bash
cd /home/cheng/git-project/CLIProxyAPI
MANAGEMENT_STATIC_PATH=/tmp/cpa-panel-static \
go run ./cmd/server -config /tmp/cpa-panel-fork.yaml >/tmp/cpa-panel-fork.log 2>&1 &
server_pid=$!
trap 'kill $server_pid >/dev/null 2>&1 || true' EXIT

for i in $(seq 1 60); do
  if curl -fsS http://127.0.0.1:8317/management.html -o /tmp/cpa-management.html; then
    break
  fi
  sleep 2
done

test -f /tmp/cpa-panel-static/management.html
cmp -s /tmp/cpa-panel-release/management.html /tmp/cpa-panel-static/management.html
cmp -s /tmp/cpa-panel-release/management.html /tmp/cpa-management.html
```

Expected:
- `curl` 成功返回 HTML
- `/tmp/cpa-panel-static/management.html` 已生成
- `/tmp/cpa-panel-static/management.html` 与 Release 中下载的 `management.html` 完全一致
- `/tmp/cpa-management.html` 与 Release 中下载的 `management.html` 完全一致

- [ ] **Step 5: 清理验证进程并检查后端日志**

Run:

```bash
kill $server_pid >/dev/null 2>&1 || true
wait $server_pid 2>/dev/null || true
tail -n 50 /tmp/cpa-panel-fork.log
```

Expected:
- 日志中没有回退到 `cpamc.router-for.me` 的记录
- 日志中应出现成功下载或更新管理面板的记录

---

### Task 5: 推进后端分支并完成双仓收口

**Files:**
- Modify via merge: `/home/cheng/git-project/CLIProxyAPI/*`
- Modify via merge: `/home/cheng/git-project/Cli-Proxy-API-Management-Center/*`

- [ ] **Step 1: 确认后端功能分支工作区干净**

Run:

```bash
cd /home/cheng/git-project/CLIProxyAPI
git status --short --branch
go test ./internal/managementasset -run 'TestResolveManagementReleaseSource|TestEnsureLatestManagementHTML_' -count=1
```

Expected:
- `git status` 只显示当前分支名，不再有未提交改动
- 单元测试仍然通过

- [ ] **Step 2: 将后端功能分支合并回 `dev`**

Run:

```bash
cd /home/cheng/git-project/CLIProxyAPI
git checkout dev
git pull origin dev
git merge --ff-only feature/panel-fork-source-default
git push origin dev
```

Expected:
- `origin/dev` 包含“默认切换管理面板前端来源”和“自定义管理面板仓库禁用官方回退”两次提交

- [ ] **Step 3: 将已验证的后端改动推进到 `master`**

Run:

```bash
cd /home/cheng/git-project/CLIProxyAPI
git checkout master
git pull origin master
git merge dev
git push origin master
```

Expected:
- `origin/master` 包含本次默认来源与严格来源绑定改动

- [ ] **Step 4: 再次核对前后端默认状态**

Run:

```bash
cd /home/cheng/git-project/CLIProxyAPI
git show origin/master:config.example.yaml | rg "920293630/Cli-Proxy-API-Management-Center"

cd /home/cheng/git-project/Cli-Proxy-API-Management-Center
git branch -r
gh repo view 920293630/Cli-Proxy-API-Management-Center --json defaultBranchRef -q .defaultBranchRef.name
```

Expected:
- 后端默认配置仍指向 `920293630/Cli-Proxy-API-Management-Center`
- 前端远端分支包含 `origin/main`、`origin/dev`、`origin/master`
- 前端默认分支仍为 `master`

---

## Self-Review

### Spec coverage

- “前端 fork 本地纳管，采用与后端一致的 fork 同步与开发模式”：
  - Task 1
- “后端 fork 默认从用户自己的前端 fork 拉取管理面板 Release 产物”：
  - Task 2
- “自定义前端仓库时禁用官方 fallback”：
  - Task 3
- “下载失败且本地已有旧面板时继续沿用旧文件”：
  - Task 3
- “前端发布与后端托管解耦，后端只消费 Release”：
  - Task 1、Task 4
- “验证真实拉取页面来自用户 fork”：
  - Task 4、Task 5

### Placeholder scan

- 本计划未保留未完成标记或待补充内容。
- 每个文件修改步骤都给出了明确路径和内容。
- 每个验证步骤都给出了明确命令和预期结果。

### Type consistency

- 测试和实现统一使用：
  - `resolveManagementReleaseSource`
  - `managementReleaseSource`
  - `fetchLatestAssetFunc`
  - `downloadAssetFunc`
- 后端严格来源绑定的行为入口统一收敛在 `EnsureLatestManagementHTML`。
