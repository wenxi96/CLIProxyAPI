# 后端 Docker 部署

本文档说明当前 fork 的 Docker 部署方式。

## 当前支持状态

当前 fork 支持两条 Docker 路径：

### 1. 预构建镜像

默认镜像：

```text
ghcr.io/wenxi96/cli-proxy-api:latest
```

GitHub Actions 工作流会在以下场景发布镜像：

- `master` 分支推送
- `v*` 标签推送
- 手动触发 `docker-image` workflow

默认镜像标签包括：

- `latest`
- `master`
- `${VERSION}`
- `sha-${COMMIT}`

### 2. 源码构建运行

如果镜像还未首次发布，或 GHCR 包可见性尚未公开，可直接使用源码构建：

```bash
docker compose build
docker compose up -d
```

## 快速启动

使用预构建镜像：

```bash
docker compose up -d --no-build
```

使用源码构建：

```bash
docker compose build
docker compose up -d --pull never
```

## 默认镜像与覆盖方式

当前 `docker-compose.yml` 默认使用：

```text
ghcr.io/wenxi96/cli-proxy-api:latest
```

如需覆盖：

```bash
CLI_PROXY_IMAGE=ghcr.io/wenxi96/cli-proxy-api:master docker compose up -d --no-build
```

## 挂载目录

默认挂载如下：

- `./config.yaml -> /CLIProxyAPI/config.yaml`
- `./auths -> /root/.cli-proxy-api`
- `./logs -> /CLIProxyAPI/logs`

推荐在部署目录下准备：

```text
config.yaml
auths/
logs/
```

## 版本信息

Docker 构建会注入以下元数据：

- `VERSION`
- `COMMIT`
- `BUILD_DATE`
- `SOURCE_REPOSITORY`

这些字段会写入二进制构建信息，便于后续排查镜像来源。

## GHCR 注意事项

如果第一次发布后仍然无法匿名拉取镜像，请检查：

1. GitHub Packages 中对应镜像包是否已切换为 Public
2. 当前 workflow 是否成功完成了 `docker-image`
3. 当前仓库是否允许 `GITHUB_TOKEN` 写入 packages

## 验证方法

```bash
docker compose config
docker images | rg 'wenxi96/cli-proxy-api|ghcr.io/wenxi96/cli-proxy-api'
docker compose ps
```
