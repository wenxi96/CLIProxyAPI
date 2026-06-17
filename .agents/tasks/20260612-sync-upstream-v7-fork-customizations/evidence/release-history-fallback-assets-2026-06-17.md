# Release History Fallback Assets Evidence - 2026-06-17 HKT

## Scope

处理独立评审发现项：后端 `.github/workflows/rebuild-release-history.yml` 的无 `.goreleaser.yml` fallback 只生成 `linux_amd64` 和 `linux_amd64_no-plugin` 两个资产，未来用于新提交 release history rebuild 时会发布不完整 release。

## Change

- 将 fallback 从 `build_linux_archive` 改为 `build_fallback_archive` 表驱动构建。
- fallback 现在生成与主 release workflow 同名的 10 个 archive 资产：
  - `CLIProxyAPI_<version>_darwin_amd64.tar.gz`
  - `CLIProxyAPI_<version>_darwin_aarch64.tar.gz`
  - `CLIProxyAPI_<version>_windows_amd64.zip`
  - `CLIProxyAPI_<version>_windows_aarch64.zip`
  - `CLIProxyAPI_<version>_linux_amd64.tar.gz`
  - `CLIProxyAPI_<version>_linux_aarch64.tar.gz`
  - `CLIProxyAPI_<version>_linux_amd64_no-plugin.tar.gz`
  - `CLIProxyAPI_<version>_linux_aarch64_no-plugin.tar.gz`
  - `CLIProxyAPI_<version>_freebsd_amd64.tar.gz`
  - `CLIProxyAPI_<version>_freebsd_aarch64_no-plugin.tar.gz`
- fallback 增加资产数量检查：archive 数量必须为 `10`，否则停止。
- fallback 继续生成 `dist/checksums.txt` 并随 archive 一起发布。

## Important Constraint

该 fallback 运行在单个 Ubuntu job 中。对主 release workflow 需要 hosted runner 或专用 cross-build tooling 的非原生目标，fallback 使用 `CGO_ENABLED=0` 构建。这样补齐历史重建资产集合，但不等价替代主 release workflow 的平台原生构建语义。

## Verification

- `python3` + `yaml.safe_load(.github/workflows/rebuild-release-history.yml)`: exit `0`.
- Extracted `Rebuild release history` run block and ran `bash -n /tmp/rebuild-release-history-run.sh`: exit `0`.
- `git diff --check`: exit `0`.
- Actual fallback build in `cliproxyapi-upstream-merge-builder` container with Go `1.26.4`: exit `0`.
- The actual fallback build produced these archives plus `checksums.txt`, then removed `dist/`:
  - `CLIProxyAPI_review-fallback_darwin_aarch64.tar.gz`
  - `CLIProxyAPI_review-fallback_darwin_amd64.tar.gz`
  - `CLIProxyAPI_review-fallback_freebsd_aarch64_no-plugin.tar.gz`
  - `CLIProxyAPI_review-fallback_freebsd_amd64.tar.gz`
  - `CLIProxyAPI_review-fallback_linux_aarch64.tar.gz`
  - `CLIProxyAPI_review-fallback_linux_aarch64_no-plugin.tar.gz`
  - `CLIProxyAPI_review-fallback_linux_amd64.tar.gz`
  - `CLIProxyAPI_review-fallback_linux_amd64_no-plugin.tar.gz`
  - `CLIProxyAPI_review-fallback_windows_aarch64.zip`
  - `CLIProxyAPI_review-fallback_windows_amd64.zip`
  - `checksums.txt`

## Boundaries

- No `git push`.
- No tag creation or push.
- No GitHub release trigger.
- No `management.html` upload.
- No credentials or private configuration written.
