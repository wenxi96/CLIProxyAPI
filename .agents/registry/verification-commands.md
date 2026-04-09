# Verification Commands

## Smoke

- `go test ./internal/api/handlers/management -run 'TestAuthFile'`
  - 适用范围：认证文件管理相关 handler 回归
  - 来源：`internal/api/handlers/management/*_test.go`
  - 状态：静态推导

## Standard

- `go test ./internal/api/handlers/management`
  - 适用范围：管理 handler 相关测试
  - 来源：`internal/api/handlers/management/*_test.go`
  - 状态：静态推导
- `(cd /home/cheng/git-project/Cli-Proxy-API-Management-Center && npm run type-check)`
  - 适用范围：前端类型检查
  - 来源：前端仓库 `package.json`
  - 状态：静态推导
- `(cd /home/cheng/git-project/Cli-Proxy-API-Management-Center && npm run build)`
  - 适用范围：前端构建验证
  - 来源：前端仓库 `package.json`
  - 状态：静态推导

## Exhaustive

- 启动本地开发实例后手工验证 `/management.html` 中认证文件页面批量检查、汇总卡片与详情弹窗。
  - 适用范围：端到端联调
  - 来源：当前任务需求
  - 状态：人工验证入口

## Derivation Order

1. 先跑后端 handler 测试
2. 再跑前端类型检查与构建
3. 最后切换开发实例资源来源并做手工联调

## Command Sources

- `internal/api/handlers/management/*_test.go`
- `/home/cheng/git-project/Cli-Proxy-API-Management-Center/package.json`

## Known Gaps

- 前端仓库当前没有现成测试运行器，本次前端行为验证主要依赖类型检查、构建与开发实例联调。
