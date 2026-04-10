# Findings

- 旧逻辑只使用 `release-metadata.env` 中的 `CUSTOM_VERSION` 生成快照版本，导致在已有正式版 `v6.9.16-wx.1.1` 之后，新的 master 快照仍然生成 `v6.9.16-wx.1.0-build.<sha>`。
- GitHub Releases 中 snapshot 仍作为普通 release 发布，因此一旦版号回退，就会直接干扰“latest”认知和默认资产消费。
- 旧版 release notes 的自定义提交范围按 `upstream/main..HEAD` 或 `BASE_TAG..HEAD` 计算，会把此前已经发版过的 fork 提交重复带入新版本说明。
- 前端当前只有 `v1.7.30-wx.1.0-build.*` 快照标签，没有正式 `v1.7.30-wx.1.0` 或更高正式标签，因此修复后的前端快照仍会停留在 `1.0-build`，直到后续产生正式 tag。
