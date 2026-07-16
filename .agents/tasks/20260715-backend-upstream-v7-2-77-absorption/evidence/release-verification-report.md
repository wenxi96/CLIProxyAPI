# 后端发布核验报告

## Release Candidate

- master：`273fbba0679b8f522bdd55cbf79695ef0a782e19`。
- tag：`v7.2.80-wx-2.14`，远端精确指向 master。
- `scripts/version.sh auto-release`：`BASE_TAG=v7.2.80`、`EFFECTIVE_CUSTOM_VERSION=2.14`。
- master `.agents`：空。

## Actions

- release：run `29498942117`，`success`。
- docker-image：run `29498942179`，`success`。
- Release：https://github.com/wenxi96/CLIProxyAPI/releases/tag/v7.2.80-wx-2.14

## Release 资产

- 10 个归档：Darwin amd64/arm64、Linux amd64/arm64 普通与 no-plugin、Windows amd64/arm64、FreeBSD amd64 与 arm64 no-plugin。
- `checksums.txt` 已生成，包含全部 10 个归档 SHA-256。
- Linux amd64 checksum：`2c9c1584abf84161da4ece48783ecfc69b2809b56a41ca8e6fac5a47b7373a2e`，与 GitHub 资产 digest 一致。

## GHCR

- 镜像：`ghcr.io/wenxi96/cli-proxy-api:7.2.80-wx-2.14`。
- 同 digest tags：`latest`、`sha-273fbba0`。
- OCI index digest：`sha256:0a83d4a87dd592dae6a0a9850e0572d17dd363c25bc439388dbb252ec0d43f59`。
- 平台：`linux/amd64`、`linux/arm64`，并包含对应 attestation manifests。

## 结论

后端 tag、Actions、Release、归档校验和与 GHCR 多架构镜像全部核验通过。
