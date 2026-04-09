# Linux 一键安装与更新

本文档说明当前 `wenxi96/CLIProxyAPI` fork 在 Linux 环境下的自带安装脚本、更新脚本和 systemd 管理方式。

## 适用范围

- 适合希望直接从当前 fork 安装正式版本的用户
- 默认安装目录：`~/cliproxyapi`
- 默认 systemd 服务名：`cliproxyapi`
- 默认前端管理面板来源：`https://github.com/wenxi96/Cli-Proxy-API-Management-Center`

## 一键安装

直接执行：

```bash
curl -fsSL https://raw.githubusercontent.com/wenxi96/CLIProxyAPI/refs/heads/master/install/linux/cliproxyapi-installer.sh | bash
```

脚本会自动完成：

- 识别当前 Linux 架构
- 从当前 fork 的 latest release 下载匹配的 Binary 压缩包
- 安装到 `~/cliproxyapi`
- 保留已有 `config.yaml`
- 在首次安装时从 `config.example.yaml` 生成默认配置
- 下载本地运行脚本：
  - `cliproxyapi-installer.sh`
  - `update-cliproxyapi-safe.sh`
  - `setup-autostart-systemd.sh`
  - `start-cliproxyapi-system.sh`
  - `start-cliproxyapi-temporary.sh`
  - `stop-cliproxyapi.sh`

## 查看状态

```bash
bash install/linux/cliproxyapi-installer.sh status
```

如果是安装后的本地目录，也可以直接执行：

```bash
cd ~/cliproxyapi
bash ./cliproxyapi-installer.sh status
```

## 更新到最新稳定版本

在线更新：

```bash
curl -fsSL https://raw.githubusercontent.com/wenxi96/CLIProxyAPI/refs/heads/master/install/linux/cliproxyapi-installer.sh | bash -s -- update
```

安装完成后的本地安全更新：

```bash
cd ~/cliproxyapi
bash ./update-cliproxyapi-safe.sh
```

`update-cliproxyapi-safe.sh` 的行为是：

- 如系统级服务正在运行，则先停止
- 等待旧进程退出
- 调用当前 fork 安装器执行 `update`
- 若原本服务处于运行状态，则自动恢复服务

## 启用系统级自启动

安装完成后执行：

```bash
cd ~/cliproxyapi
bash ./setup-autostart-systemd.sh
sudo systemctl start cliproxyapi.service
```

常用命令：

```bash
sudo systemctl status cliproxyapi.service
sudo journalctl -u cliproxyapi.service -n 200 --no-pager
```

## 启动方式

当前 fork 默认提供两种启动方式：

### 1. 系统级服务

```bash
cd ~/cliproxyapi
bash ./start-cliproxyapi-system.sh
```

### 2. 临时运行

```bash
cd ~/cliproxyapi
bash ./start-cliproxyapi-temporary.sh
```

停止：

```bash
cd ~/cliproxyapi
bash ./stop-cliproxyapi.sh
```

## 常用环境变量

安装脚本支持以下环境变量覆盖：

- `REPO_OWNER`
- `REPO_NAME`
- `REPO_BRANCH`
- `RELEASE_API_URL`
- `PANEL_GITHUB_REPOSITORY`
- `INSTALL_DIR`
- `SERVICE_NAME`

示例：

```bash
INSTALL_DIR="$HOME/cliproxyapi-test" \
SERVICE_NAME="cliproxyapi-test" \
curl -fsSL https://raw.githubusercontent.com/wenxi96/CLIProxyAPI/refs/heads/master/install/linux/cliproxyapi-installer.sh | bash
```

## 注意事项

- 安装脚本默认面向 `master` 分支对应的正式分发入口，而不是 `dev`
- 如你从旧的官方安装脚本迁移过来，建议先检查 `config.yaml` 中的 `remote-management.panel-github-repository`
- 如果当前机器没有 `sudo` 或不允许安装 systemd 服务，可以先使用临时运行脚本
