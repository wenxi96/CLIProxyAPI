# Fork Install And Docker Self-Hosting Implementation Plan

- Goal: 为 `wenxi96/CLIProxyAPI` 补齐仓库内自带的 Linux 一键安装 / 更新脚本，并完成 Docker 镜像发布链路自有化与对应部署文档收口。
- Input Mode: clear-requirements
- Requirements Source: session-confirmed
- Canonical Spec Path: None
- Scope Boundary: 仅覆盖当前后端仓库内的安装脚本、更新脚本、systemd 辅助脚本、Docker 发布工作流、默认 Docker 镜像引用、README 入口与本地部署文档；不把 `cliproxyapi-tool` 纳入仓库内依赖，不改上游 `main` 分支治理策略，不扩展为整站帮助中心迁移。
- Non-Goals: 不重写整套安装器为另一种架构；不在本次任务内实现完整 docs site；不自动创建或修改外部 Docker Hub 仓库与凭证；不改变当前前端 `management.html` 发布模式。
- Constraints: 安装脚本必须可独立于 `cliproxyapi-tool` 运行；配置保护与升级行为必须可解释且可验证；代码注释保持英文；文档正文使用中文；仓库默认行为必须优先指向 fork 资源，不能静默回退官方源。
- Detail Level: contract-first
- Execution Route: direct-inline
- Why This Route: 本任务涉及脚本、工作流、文档与默认值联动，文件边界清晰但强耦合，适合由单一主写者一次性收口，避免多 agent 并发导致分发入口和文档描述不一致。
- Escalation Trigger: 若实现过程中发现 Docker 镜像仓库命名、发布权限、Tag 策略或安装脚本服务模型需要变更当前 `master/tag` 发布规范，或需要新增外部 secrets / registry 资源，则暂停实现并先与用户确认。

## File Structure
- Create:
  - `install/linux/cliproxyapi-installer.sh`
  - `install/linux/update-cliproxyapi-safe.sh`
  - `install/linux/setup-autostart-systemd.sh`
  - `docs/deploy/install-script_CN.md`
  - `docs/deploy/backend-docker_CN.md`
  - `docs/deploy/backend-binary_CN.md`
- Modify:
  - `README.md`
  - `README_CN.md`
  - `docker-compose.yml`
  - `docker-build.sh`
  - `.github/workflows/docker-image.yml`
  - `docs/fork-maintainer-workflow.md`
  - `docs/fork-maintainer-workflow_CN.md`
- Read:
  - `config.example.yaml`
  - `internal/managementasset/updater.go`
  - `scripts/version.sh`
  - `scripts/release-lib.sh`
  - `scripts/release-notes.sh`
  - `Cli-Proxy-API-Management-Center/.github/workflows/release.yml`
  - 上游安装器：`brokechubb/cliproxyapi-installer`
- Test:
  - `go build -o test-output ./cmd/server && rm test-output`
  - `bash install/linux/cliproxyapi-installer.sh --help`
  - `bash install/linux/cliproxyapi-installer.sh status`
  - `bash install/linux/update-cliproxyapi-safe.sh --help`
  - `docker compose config`
  - 基于 shell 的脚本静态检查（若本地可用 `shellcheck`）

## Task Breakdown

### Task 1: 落 fork 自带 Linux 安装器与更新脚本

- Objective: 在仓库内新增可独立分发的 Linux 安装器、升级包装脚本和 systemd 辅助脚本，默认面向 `wenxi96/CLIProxyAPI` 最新 release 资产工作。
- Files:
  - Create:
    - `install/linux/cliproxyapi-installer.sh`
    - `install/linux/update-cliproxyapi-safe.sh`
    - `install/linux/setup-autostart-systemd.sh`
  - Modify:
    - `README.md`
    - `README_CN.md`
  - Read:
    - `config.example.yaml`
    - `scripts/version.sh`
    - 上游安装器脚本与 README
  - Test:
    - `bash install/linux/cliproxyapi-installer.sh --help`
    - `bash install/linux/cliproxyapi-installer.sh status`
    - 以 `DRY_RUN` 或安全模式验证下载地址解析与配置保护逻辑
- Dependencies: None
- Verification:
  - 安装器帮助输出完整，包含 install / update / status / uninstall 等入口
  - 脚本默认仓库来源为 `wenxi96/CLIProxyAPI`
  - fresh install 路径会生成或保留 `config.yaml`，并将 `remote-management.panel-github-repository` 指向 `wenxi96/Cli-Proxy-API-Management-Center`
  - 更新脚本与主安装器共用同一套版本来源解析逻辑，不存在双份下载逻辑
- Stop Conditions:
  - 如果现有 release 资产命名与脚本匹配规则不一致，先停下修正规则，不继续写文档
  - 如果需要引入 `sudo` 强耦合的系统级安装模型，先与用户确认，不默认扩大为全系统安装器
- Interfaces / Contracts:
  - 安装器必须支持非交互执行
  - 更新脚本只负责安全包装，不复制安装器核心逻辑
  - 安装器应允许通过环境变量覆盖 repo、release API 和 install 目录，便于后续测试
- Handoff Notes:
  - 若后续补测试，可优先为“版本解析”“配置保护”“面板仓库默认值写入”抽离可测试的小函数

### Task 2: 完成 Docker 镜像发布链路自有化

- Objective: 让仓库默认 Docker 入口从“官方镜像优先”改为“fork 镜像优先”，并让 Docker 发布工作流可向用户自己的镜像仓库发布。
- Files:
  - Create:
    - None
  - Modify:
    - `.github/workflows/docker-image.yml`
    - `docker-compose.yml`
    - `docker-build.sh`
  - Read:
    - `scripts/release-lib.sh`
    - `scripts/version.sh`
    - 当前 GitHub release / Docker 发布配置
  - Test:
    - `docker compose config`
    - YAML 结构检查
    - 如本地环境允许，验证 `docker compose build` 的默认 build args
- Dependencies: Task 1
- Verification:
  - `docker-compose.yml` 默认镜像地址改为 fork 自定义镜像名，且仍可通过环境变量覆盖
  - `docker-image.yml` 不再硬编码 `eceasy/cli-proxy-api`
  - 工作流支持将 tag 构建发布到 fork 指定镜像仓库
  - 文档中明确区分“源码构建运行”和“预构建镜像运行”两条路径
- Stop Conditions:
  - 如果尚未明确最终 Docker Hub 仓库名或命名空间，先把工作流改成可配置模式，不硬编码错误目标
  - 如果需要引入新的 registry 类型或多 registry 同步，先暂停并确认，不在本任务内扩展
- Interfaces / Contracts:
  - Docker 仓库名应优先来自显式环境变量 / workflow env
  - 默认镜像名与文档示例必须保持一致

### Task 3: 收口 README 与部署文档入口

- Objective: 将后端仓库从“链接到上游帮助站”改为“链接到本仓库自有部署文档”，并补齐 fork 语境下的 binary / Docker / 前端面板说明。
- Files:
  - Create:
    - `docs/deploy/install-script_CN.md`
    - `docs/deploy/backend-docker_CN.md`
    - `docs/deploy/backend-binary_CN.md`
  - Modify:
    - `README.md`
    - `README_CN.md`
    - `docs/fork-maintainer-workflow.md`
    - `docs/fork-maintainer-workflow_CN.md`
  - Read:
    - `config.example.yaml`
    - `internal/managementasset/updater.go`
    - 前端仓库 README 与 release workflow
  - Test:
    - 人工核对 README 链接
    - 确认文档中的脚本路径、镜像名、面板来源与代码默认值一致
- Dependencies: Task 1, Task 2
- Verification:
  - README 的“新手入门 / 使用手册”入口改为仓库内文档
  - 中文文档正文使用中文，并明确说明当前前端生产形态是 `management.html` release 资产
  - 文档不再把 `cliproxyapi-tool` 写成仓库内安装依赖
  - binary 与 Docker 两套路径的默认更新源都对齐 fork
- Stop Conditions:
  - 如果实现后的默认行为仍有一处回退到官方源，先修默认值，不继续润色文档
  - 如果文档需要依赖尚未落地的外部凭证或镜像仓库状态，显式标注前置条件，不伪造“已可用”
- Steps:
  - 先写最小部署文档，再回填 README 链接
  - README 仅做入口，不承载完整操作手册

### Task 4: 做实现后验证与交付收口

- Objective: 为后续真正实现建立最低可执行验证矩阵，确保脚本、工作流、文档三者没有相互漂移。
- Files:
  - Create:
    - None
  - Modify:
    - 视实现结果调整相关文档与脚本注释
  - Read:
    - 本计划涉及的全部改动文件
  - Test:
    - `go build -o test-output ./cmd/server && rm test-output`
    - `bash install/linux/cliproxyapi-installer.sh --help`
    - `bash install/linux/cliproxyapi-installer.sh status`
    - `bash install/linux/update-cliproxyapi-safe.sh --help`
    - `docker compose config`
    - 如本地可用，运行 `shellcheck` 和 `actionlint`
- Dependencies: Task 1, Task 2, Task 3
- Verification:
  - 所有默认下载源、镜像源、文档入口都已切换到 fork
  - 至少完成一次本地 dry-run / help 级脚本验证
  - 对无法在本地完成的外部验证项（如真实 Docker 推送）进行明确披露
- Stop Conditions:
  - 若本地验证暴露“脚本默认值 / 文档示例 / workflow env”任一不一致，必须先修复再提交
  - 若需真实推送镜像验证但缺少凭证，停止在“可提交待外部验证”状态，不宣称发布链路已完全可用

## Execution Handoff
- Execution Route: direct-inline
- Why This Route: 该任务本质是“分发入口统一化”，关键风险来自默认值和文档漂移，而不是大规模代码复杂度。由单一执行者顺序完成，能更稳定地保持脚本、workflow、README 和部署文档的口径一致。
- Escalate To:
  - 用户：当需要确认最终 Docker Hub 仓库名、镜像标签策略、是否保留 user-level systemd 兼容行为时
  - 后续发布验证：当代码完成但缺少外部 registry 凭证导致无法完成真实推送验证时
- Handoff Notes:
  - 实现顺序建议为“脚本 -> Docker 工作流 -> 文档 -> 本地验证”
  - 不要先改 README 再改脚本，否则文档会短暂领先实现
  - 若后续增加英文部署文档，应以中文文档为主源翻译，不要双写漂移

## Notes
- 当前仓库已具备二进制 release 发布能力，安装器可直接消费 latest release 资产，无需额外引入第三方更新工具。
- 当前前端生产接入模式已经是“后端按 `panel-github-repository` 拉取最新 `management.html` release 资产”，文档应明确这不是运行时拉 raw `master` 文件。
- Docker 自有化链路的真正外部阻塞点只有镜像仓库命名与凭证，不在脚本或代码结构本身。
