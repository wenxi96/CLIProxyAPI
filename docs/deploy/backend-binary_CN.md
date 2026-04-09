# 后端 Binary 部署

本文档说明当前 fork 的 Binary 部署方式，适合 Linux 服务器、WSL 或本地长期运行环境。

## 目录结构

默认安装目录：

```text
~/cliproxyapi
├── cliproxyapi-installer.sh
├── cli-proxy-api
├── config.yaml
├── config.example.yaml
├── version.txt
├── update-cliproxyapi-safe.sh
├── setup-autostart-systemd.sh
├── start-cliproxyapi-system.sh
├── start-cliproxyapi-temporary.sh
└── stop-cliproxyapi.sh
```

## 首次部署

推荐直接使用一键安装脚本：

```bash
curl -fsSL https://raw.githubusercontent.com/wenxi96/CLIProxyAPI/refs/heads/master/install/linux/cliproxyapi-installer.sh | bash
```

安装完成后编辑配置：

```bash
cd ~/cliproxyapi
nano config.yaml
```

重点配置项：

- `port`
- `api-keys`
- `remote-management.secret-key`
- `remote-management.allow-remote`
- `auth-dir`

## 启动方式

### 系统级 service

```bash
cd ~/cliproxyapi
bash ./start-cliproxyapi-system.sh
```

### 临时运行

```bash
cd ~/cliproxyapi
bash ./start-cliproxyapi-temporary.sh
```

停止：

```bash
cd ~/cliproxyapi
bash ./stop-cliproxyapi.sh
```

## 升级方式

本地安全更新：

```bash
cd ~/cliproxyapi
bash ./update-cliproxyapi-safe.sh
```

或重新调用在线安装器：

```bash
curl -fsSL https://raw.githubusercontent.com/wenxi96/CLIProxyAPI/refs/heads/master/install/linux/cliproxyapi-installer.sh | bash -s -- update
```

## 管理面板来源

当前 fork 的 Binary 部署默认前端来源是：

```text
https://github.com/wenxi96/Cli-Proxy-API-Management-Center
```

后端会从该前端仓库的 latest release 下载 `management.html` 资产，而不是读取原始 `master` 文件。

## 验证方法

```bash
cd ~/cliproxyapi
cat version.txt
./cli-proxy-api 2>&1 | sed -n '1p'
systemctl status cliproxyapi --no-pager
```

## 适合场景

- 当前 fork 的正式稳定运行
- 本机或 WSL 长期常驻
- 不希望依赖 Docker
- 希望通过 systemd 管理生命周期
