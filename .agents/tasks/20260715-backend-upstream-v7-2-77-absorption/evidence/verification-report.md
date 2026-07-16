# 验证报告

## 环境

- 本机未安装 Go，使用 `golang:1.26-bookworm` Docker 工具链。
- Go module/build cache 使用 `cliproxy-go-mod` 与 `cliproxy-go-build` volumes。
- 验证对象：当前 staged merge candidate，MERGE_HEAD 为 `09da52ad509e2c18e7b9540db3b98c2214c280aa`。

## 命令

| 命令 | 结果 | 说明 |
|---|---:|---|
| `test -z "$(gofmt -l .)"` | pass | Go 文件格式正确。 |
| `go test ./...` | pass | 全仓库测试通过。 |
| `go build -o test-output ./cmd/server && rm test-output` | pass | server 编译通过并清理产物。 |
| `git diff --cached --check` | pass | 无空白错误。 |
| `rg -n "^(<<<<<<<\|=======\|>>>>>>>)" . --glob '!.git/**'` | pass | 无冲突标记；`rg` 无匹配返回 1。 |
| `git diff --name-only --diff-filter=U` | pass | 无未解决索引。 |
| `git ls-remote --heads origin main` | pass | `origin/main` 指向 `09da52ad...`。 |

## 重点覆盖

- Codex/XAI 流式成功、失败和 missing terminal usage 终态。
- usage presence、cache aliases、tier metadata 与 Generate enrichment。
- Redis queue canonical request detail schema。
- Gitstore 在全局 commit signing 开启时的 direct commit/push。
- OAuth callback、plugin store/host、auth conductor 与 release helper。

## 未执行项

- GitHub Actions、真实容器镜像发布、GitHub Release 与外部 provider 端到端调用未执行。
- 原因：候选尚未提交、推送、合入 master 或获得发版授权。
- 风险：workflow 和真实发布资产需在后续授权阶段按同一候选 SHA 继续核验。
