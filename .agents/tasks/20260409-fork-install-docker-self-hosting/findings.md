# 已确认事实

- 当前仓库内没有自带的 Linux 一键安装 / 更新脚本。
- 当前 `docker-compose.yml` 默认镜像仍为 `eceasy/cli-proxy-api:latest`。
- 当前 `.github/workflows/docker-image.yml` 默认发布目标仍为 `eceasy/cli-proxy-api`，且只在 `v*` tag 上触发。
- 当前 fork 的 GitHub release 已正常产出二进制压缩包，命名仍兼容 `CLIProxyAPI_<version>_linux_amd64.tar.gz` 形式。
- 当前后端默认 `remote-management.panel-github-repository` 已指向 `https://github.com/wenxi96/Cli-Proxy-API-Management-Center`。
- 当前前端 fork 已具备 `master` 分支构建并上传 `management.html` release 资产的工作流。
- 上游常用 Linux 安装器不是后端仓库内文件，而是独立仓库 `brokechubb/cliproxyapi-installer`。
- 该安装器核心逻辑可复用，但默认仓库地址、文档地址、服务方式与当前 fork 需求不完全一致，需要做 fork 化改造。

## 关键约束

- 仓库内文档正文默认使用中文。
- 代码注释保持英文。
- `main` 继续作为上游同步分支，fork 专属脚本与文档仅进入 `dev/master`。
- 计划阶段不假设外部 Docker Hub 凭证已经配置完成。

## 实施后补充事实

- 当前仓库已新增自带 Linux 安装器、更新脚本和 systemd 辅助脚本，不再依赖 `cliproxyapi-tool` 才能完成基础安装/更新。
- 当前 `docker-compose.yml` 默认镜像已改为 `ghcr.io/wenxi96/cli-proxy-api:latest`。
- 当前 `.github/workflows/docker-image.yml` 已改为向 GitHub Container Registry 发布 fork 镜像，并支持 `master` push、`v*` tag 与手动触发。
- 当前安装器已经通过一次临时目录真实安装验证，确认 latest release 下载、配置生成、脚本落盘和状态查看可以工作。
- 当前仓库已通过 Docker 容器完成 `go build`、ShellCheck 与 actionlint 验证，本地缺少原生命令不再构成阻塞。
