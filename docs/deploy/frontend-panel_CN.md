# 前端管理面板发布与接入

本文档说明当前 fork 的前端管理面板如何发布，以及后端如何接入它。

## 核心结论

当前 fork 的前端不是传统意义上的“常驻前端服务”。

生产形态是：

1. 前端仓库构建单文件 `management.html`
2. 将该文件作为 release 资产发布
3. 后端运行时根据 `remote-management.panel-github-repository` 下载最新 release 资产

因此，访问方式仍然是：

```text
http://<host>:<api_port>/management.html
```

而不是单独部署一个 Node / Vite 前端服务。

## 当前前端仓库

默认前端来源：

```text
https://github.com/wenxi96/Cli-Proxy-API-Management-Center
```

后端默认配置已经指向这个仓库。

## 前端 release 流程

前端仓库 `master` 分支会构建并发布：

- `dist/index.html`
- 重命名为 `management.html`
- 上传到 GitHub release

后端随后会拉取这个 `management.html` 资产。

## 后端如何接入

后端配置项：

```yaml
remote-management:
  panel-github-repository: "https://github.com/wenxi96/Cli-Proxy-API-Management-Center"
```

注意：

- 这里拉取的是 latest release 资产
- 不是前端仓库 `master` 的原始 HTML 文件
- 如果你切换到其他前端 fork，需要同步改这个地址

## 前端本地开发

如果需要本地调试前端仓库：

```bash
cd ~/git-project/Cli-Proxy-API-Management-Center
npm install
npm run dev
```

然后手动连接到本地或远端 CLIProxyAPI 后端实例。

## 生产验证

后端侧可检查：

```bash
grep -n "panel-github-repository" config.yaml
```

访问验证：

```text
http://127.0.0.1:8317/management.html
```

如果页面未更新，优先检查：

1. 前端仓库 latest release 是否已发布最新 `management.html`
2. 后端当前 `panel-github-repository` 是否仍指向旧仓库
3. 本地静态缓存或旧的 `management.html` 是否仍被使用
