# Repository Overview

## Repository Summary

CLIProxyAPI 是一个 Go 后端服务，提供 OpenAI / Claude / Gemini / Codex 等兼容接口，并包含管理 API 与远程管理面板入口。

## Repository Type

- 类型：单仓库后端服务
- 置信度：confirmed
- 证据：`README_CN.md`、`go.mod`、`internal/api/server.go`

## Primary Languages

- Go
- 少量 YAML / Markdown

## Package and Build Systems

- Go Modules
- 前端管理面板位于独立仓库，不在本仓库内构建

## Workspace Roots

- `.`：CLIProxyAPI 后端主仓库

## Main Apps and Services

- `internal/api/server.go`
  - 角色：HTTP API 服务入口与路由注册
  - 置信度：confirmed
- `internal/api/handlers/management/`
  - 角色：管理 API 处理器
  - 置信度：confirmed
- `internal/managementasset/`
  - 角色：管理面板静态资源拉取与更新
  - 置信度：confirmed

## Key Entry Files

- `main.go`
- `internal/api/server.go`
- `internal/api/handlers/management/auth_files.go`
- `internal/api/handlers/management/api_tools.go`

## Important Operational Files

- `config.example.yaml`
- `README_CN.md`
- `docs/fork-maintainer-workflow_CN.md`

## Unknowns

- 当前开发机是否长期具备前端构建依赖，需要在联调阶段再确认。
