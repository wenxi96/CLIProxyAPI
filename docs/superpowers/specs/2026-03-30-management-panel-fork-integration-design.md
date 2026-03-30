# 管理面板前后端 Fork 接入设计

## 1. 背景

当前 `CLIProxyAPI` 后端仓库通过运行时下载 `management.html` 的方式托管管理面板页面。  
默认下载来源仍指向上游前端仓库 `router-for-me/Cli-Proxy-API-Management-Center`。

用户已经同时维护：

- 后端 fork：`https://github.com/920293630/CLIProxyAPI`
- 前端 fork：`https://github.com/920293630/Cli-Proxy-API-Management-Center`

目标不是只在本地临时覆盖下载地址，而是让后端 fork 默认引用用户自己的前端 fork，并将前端仓库纳入与后端一致的 fork 管理体系。

## 2. 目标

本设计要实现以下目标：

1. 前端 fork 本地纳管，采用与后端一致的 fork 同步与开发模式。
2. 后端 fork 默认从用户自己的前端 fork 拉取管理面板 Release 产物。
3. 当使用自定义前端仓库时，运行时严格绑定来源，不允许回退到官方 fallback 页面。
4. 前端 UI 的发布与后端托管解耦，后端只消费前端 Release，不参与前端构建。
5. 后续上游同步、前端开发、后端发布三条链路相互独立，避免耦合混乱。

## 3. 非目标

本设计不包含以下内容：

- 本次不直接实现认证文件批量检查功能本身。
- 本次不直接修改前端业务页面或后端认证聚合接口。
- 本次不把前端源码并入后端仓库。
- 本次不改变当前“运行时下载 `management.html`”这一基础机制。

## 4. 当前事实

### 4.1 后端现状

- 后端通过 `remote-management.panel-github-repository` 指定管理面板下载来源。
- 默认值位于 `config.example.yaml`，当前仍指向上游前端仓库。
- 运行时下载逻辑位于 `internal/managementasset/updater.go`。
- 面板访问路由为 `/management.html`，缺少本地文件时会现场下载。
- 当前下载逻辑在拉不到 Release 时，会尝试回退到官方 fallback 页面 `https://cpamc.router-for.me/`。

### 4.2 前端现状

- 前端仓库是独立 React + TypeScript 单页应用仓库。
- 构建产物为单文件 `dist/index.html`。
- 打 `v*` 标签会触发 `.github/workflows/release.yml`。
- Release 工作流会将 `dist/index.html` 重命名为 `management.html` 并作为 Release 产物发布。

### 4.3 本地仓库现状

- 后端本地目录：`/home/cheng/git-project/CLIProxyAPI`
- 前端本地目录：`/home/cheng/git-project/Cli-Proxy-API-Management-Center`
- 前端本地已配置：
  - `origin = https://github.com/920293630/Cli-Proxy-API-Management-Center.git`
  - `upstream = https://github.com/router-for-me/Cli-Proxy-API-Management-Center.git`

## 5. 目标架构

### 5.1 仓库职责

后端仓库负责：

- 管理 API
- 管理面板静态文件下载与托管
- 管理面板来源配置
- 下载失败后的本地缓存复用策略

前端仓库负责：

- 管理面板源码开发
- `management.html` 构建与发布
- 前端版本节奏控制

### 5.2 前后端关系

数据流固定为：

1. 前端仓库发布 `management.html` 到 GitHub Release。
2. 后端运行时读取 `panel-github-repository`。
3. 后端解析目标仓库 latest release。
4. 后端下载 `management.html` 到本地静态目录。
5. 用户访问 `/management.html` 时由后端直接返回本地静态文件。

这意味着：

- 前端是否更新，由前端 Release 决定。
- 后端是否拉到新版本，由后端下载检查决定。
- 前端上游同步不会自动影响后端正在运行的版本。

## 6. 分支与治理方案

### 6.1 后端仓库

保持当前策略：

- `main`：上游镜像分支，跟踪 `upstream/main`
- `master`：fork 稳定分支，GitHub 默认分支
- `dev`：集成分支
- `feature/*`：功能开发分支

### 6.2 前端仓库

前端仓库对齐后端分支模型：

- `main`：上游镜像分支，跟踪 `upstream/main`
- `master`：fork 稳定分支，GitHub 默认分支
- `dev`：集成分支
- `feature/*`：功能开发分支

### 6.3 前端同步策略

建议前端仓库也增加与后端一致的自动同步工作流：

- 定时从 `upstream/main` 同步到 `origin/main`
- `main` 只做镜像，不直接承载用户功能改动

### 6.4 前端发布策略

建议前端发布只从 `master` 产生：

1. 前端功能在 `feature/*` 开发
2. 合并到 `dev` 做集成验证
3. 合并到 `master` 形成稳定版本
4. 在 `master` 打 `vX.Y.Z` 标签
5. 由前端 Release 工作流发布 `management.html`

这样可以确保后端默认下载到的是用户 fork 的稳定面板，而不是开发中页面。

## 7. 后端默认化方案

### 7.1 默认下载源调整

后端 fork 需要把默认前端来源从上游改为用户 fork：

- 原默认值：
  - `https://github.com/router-for-me/Cli-Proxy-API-Management-Center`
- 新默认值：
  - `https://github.com/920293630/Cli-Proxy-API-Management-Center`

涉及范围：

- `config.example.yaml`
- README 或维护文档中关于管理面板下载来源的说明
- 任何展示默认面板来源的示例文本

### 7.2 行为语义

后端 fork 的默认语义应变为：

- 不显式配置时，默认下载用户 fork 的前端 Release
- 若用户显式修改 `panel-github-repository`，则运行时以用户配置为准

## 8. 严格来源绑定策略

### 8.1 设计原因

当前后端下载器在 Release 获取失败时，会回退到官方 fallback 页面。  
这会导致以下问题：

- 用户以为自己使用的是 fork 面板，实际却可能变成官方页面
- 前端 fork 的发布与验证边界被破坏
- 问题排查时无法确认页面真实来源

因此必须加上严格来源绑定。

### 8.2 规则

当 `panel-github-repository` 使用自定义仓库时：

1. 优先下载该仓库 latest release 的 `management.html`
2. 若下载失败但本地已有旧版静态文件，则继续使用旧版
3. 若下载失败且本地没有静态文件，则返回 404
4. 不再回退到官方 fallback 页面

### 8.3 推荐判断方式

可采用以下判定策略：

- 若 `panel-github-repository` 为空或解析后仍等于官方默认仓库，则允许官方 fallback
- 若 `panel-github-repository` 为显式自定义仓库，则禁用 fallback

该策略兼容上游默认行为，同时满足 fork 场景的来源一致性。

## 9. 失败处理设计

### 9.1 前端 Release 不存在

现象：

- 前端 fork 未打标签
- latest release 不存在

后端处理：

- 有本地旧版则继续提供旧版
- 无本地文件则 404

### 9.2 前端 Release 存在但下载失败

可能原因：

- 网络失败
- GitHub API 限流
- Release 资产缺失
- 哈希校验失败

后端处理：

- 有本地旧版则继续提供旧版
- 无本地文件则返回 404
- 不回退官方 fallback 页面

### 9.3 前端 fork 误发布错误文件

后端行为：

- 后端只负责拉取 release 资产，不判断业务正确性
- 通过前端 `master -> tag -> release` 节奏降低错误发布风险

## 10. 文档与配置变更范围

后端仓库需要变更：

- 示例配置中的默认前端仓库地址
- fork 维护文档，增加“前端 fork 一并维护”的说明
- 管理面板来源与严格来源绑定说明

前端仓库需要变更：

- fork 维护文档
- 分支模型与发布约定文档
- 自动同步 upstream 的工作流

## 11. 验证方案

### 11.1 前端仓库验证

需要验证：

1. `origin/upstream` 远端正确
2. 分支模型已落地
3. `v*` 标签可正确生成 Release
4. Release 中包含 `management.html`

### 11.2 后端仓库验证

需要验证：

1. 默认配置已指向用户前端 fork
2. 清空本地静态文件后首次访问 `/management.html` 可正常下载
3. 下载来源是用户前端 fork 的 latest release

### 11.3 严格来源验证

需要验证：

1. 将 `panel-github-repository` 指向用户 fork
2. 人为制造 Release 下载失败
3. 若本地存在旧版，仍返回旧版
4. 若本地不存在文件，返回 404
5. 不访问官方 fallback 页面

## 12. 实施顺序建议

建议按以下顺序实施：

1. 整理前端仓库分支模型
2. 为前端仓库补自动同步 upstream 工作流
3. 确认前端 fork 可从 `master` 正常打标签发 Release
4. 修改后端默认配置与相关文档
5. 修改后端下载器的 fallback 策略，加入严格来源绑定
6. 做端到端验证

## 13. 验收标准

本设计完成后，应满足以下验收条件：

1. 前端 fork 已纳入与后端一致的 fork 管理体系
2. 后端 fork 默认指向用户前端 fork，而不是上游前端仓库
3. 后端在使用自定义前端仓库时，不再回退到官方 fallback 页面
4. 前端通过 Release 向后端提供 `management.html`
5. 前后端更新、同步、开发、发布链路边界清晰且可重复执行

## 14. 后续衔接

完成本设计后，下一阶段应拆分为两个实施子任务：

1. fork 治理与默认来源切换
2. 认证文件批量检查能力设计与实现

其中第一个子任务是第二个子任务的基础，因为后续认证管理页面改造需要以用户自己的前端 fork 为默认交付面板。
