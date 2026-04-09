# Fork 维护工作流

这个 fork 使用分层分支模型，以便把上游同步和本地开发彻底分开。

## 分支职责

- `main`：上游镜像分支，始终对齐 `upstream/main`
- `master`：fork 的稳定分支，同时也是当前 GitHub 默认分支
- `dev`：集成分支，用来吸收上游更新和已完成的功能开发
- `feature/*`：实际开发分支，从 `dev` 拉出，短期存在

## 为什么要这样设计

这套模型把四件事拆开了：

1. 上游发布了什么
2. fork 当前认定的稳定版本是什么
3. 当前正在集成什么
4. 当前还在开发中的内容是什么

这样可以保证 `main` 保持干净，不会把未完成工作混入稳定历史，也能把上游冲突集中在固定通道里解决。

## 每日上游同步

默认分支 `master` 中包含工作流文件 `.github/workflows/sync-upstream.yml`。

这个工作流会：

- 每天北京时间 09:00 运行一次
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

### 3. 从 `dev` 拉出新功能分支

```bash
git checkout dev
git pull origin dev
git checkout -b feature/my-change
```

### 4. 功能完成后先回到 `dev`

```bash
git checkout dev
git merge feature/my-change
git push origin dev
```

### 5. 验证通过后再推进到 `master`

```bash
git checkout master
git pull origin master
git merge dev
git push origin master
```

## 本地手动同步命令

如果你想手动同步本地上游镜像分支，可以执行：

```bash
git checkout main
git pull
git push
```

当前仓库已经配置成在 `main` 分支上：

- `git pull` 从 `upstream/main` 拉取
- `git push` 推送到 `origin/main`

## 维护规则

- 不要直接在 `main` 上开发
- 不要把未完成工作直接放进 `master`
- 除非冲突只和某个功能有关，否则不要在 `feature/*` 分支处理上游冲突
- `feature/*` 分支尽量保持短生命周期
- 把 `master` 理解为“已验证的 fork 稳定状态”，而不是“最新上游状态”

## 前端管理面板 Fork

如果你同时维护自己的 `Cli-Proxy-API-Management-Center` fork，建议让前端仓库也采用同样的 `main/master/dev/feature/*` 模型，并把 `remote-management.panel-github-repository` 指向你的前端 fork。

对当前这个 fork，默认值指向 `https://github.com/wenxi96/Cli-Proxy-API-Management-Center`；如果你维护的是其他 fork，请改成你自己的前端仓库。

推荐默认值：

```yaml
remote-management:
  panel-github-repository: "https://github.com/wenxi96/Cli-Proxy-API-Management-Center"
```

这样 `/management.html` 的真实来源就会是你自己的前端发布流水线，而不是上游面板仓库。

## Docker 镜像与安装脚本

当前 fork 额外维护以下分发入口：

- GitHub Container Registry 镜像：`ghcr.io/wenxi96/cli-proxy-api`
- Linux 一键安装脚本：`install/linux/cliproxyapi-installer.sh`
- Linux 本地安全更新脚本：`install/linux/update-cliproxyapi-safe.sh`

推荐做法：

- `master` 作为稳定分支，对外提供 Binary release、Docker 镜像与安装脚本
- `main` 只保留上游镜像，不放 fork 专属分发入口
- 若 Docker 镜像首次发布后默认仍不可拉取，请在 GitHub Packages 页面把镜像改为 Public
