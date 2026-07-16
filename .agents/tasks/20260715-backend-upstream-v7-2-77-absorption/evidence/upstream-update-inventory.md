# 后端上游更新吸收清单（漂移后目标 v7.2.80）

## 基线

- 当前仓库: `wenxi96/CLIProxyAPI`
- 当前分支: `dev@1c36ebc5`
- integration_branch: `dev`
- release_branch: `master`
- 当前 fork release tag: `v7.2.52-wx-2.13`
- 上游目标: `router-for-me/CLIProxyAPI` `main`
- 上游目标 SHA: `09da52ad509e2c18e7b9540db3b98c2214c280aa`
- 上游最新 tag: `v7.2.80`
- 增量范围: `v7.2.52@14b13966..v7.2.80@09da52ad`
- 漂移说明: 初始评审目标 `v7.2.77@c8803713`；2026-07-16 合并前 fetch 发现新增 8 commits，按 skill 门禁扩展到当前目标。

## 汇总

- 上游新增提交数: 118
- 变更规模: 216 files，+18,825/-2,363。
- 触达模块: models/registry、auth/OAuth、Codex/XAI executors、tool translators、usage/tier/cache/generate、plugins/path、gitstore、logging、management API、release workflows 和文档资产。
- 机械冲突: 11 files；相对旧目标冲突集合以 `codex_executor.go` 替换 `sdk/cliproxy/usage/manager.go`，重叠路径总数从 43 增至 46。
- 行为冲突风险: 高，集中在 usage v2、xAI executor、auth selection、tool translation 和 fork release 链。
- 建议结论: 以 `v7.2.80` 为单一目标形成候选；冲突分 release、usage、xAI/Codex、auth/plugin/store 切片语义解决。

## 版本边界

| Tag | 端点 | 主要作用 | 风险/建议 |
|---|---|---|---|
| `v7.2.53` | `4f2e1904` | auth hash、tool terminal、Antigravity UA | medium，吸收并测 auth/tool |
| `v7.2.54` | `3fd18926` | XAI reasoning registry | high，进入 xAI 切片 |
| `v7.2.55` | `f21beb05` | GPT-5.6 registry | medium，模型表验证 |
| `v7.2.56` | `b4c59405` | client version/usage helper | high，usage 冲突 |
| `v7.2.57` | `15f30371` | Codex modalities | medium，模型契约验证 |
| `v7.2.58` | `26d45fd4` | model header override | medium，config/API 验证 |
| `v7.2.59` | `35dba9b4` | GPT-5.6 Sol 配置 | medium，模型表验证 |
| `v7.2.60` | `20e61f28` | ultra reasoning 修复合入 | medium，translator/executor 验证 |
| `v7.2.61` | `ca67caf0` | xAI namespace/tool schema | high，xAI 切片 |
| `v7.2.62` | `3554b637` | Responses Lite image tool | medium，image/tool 测试 |
| `v7.2.63` | `cc2095f3` | docs/sponsor 清理 | low，按 fork README 裁决 |
| `v7.2.64` | `6e819ab6` | cancelable/device OAuth | high，OAuth 状态机验证 |
| `v7.2.65` | `8c2bf2c2` | service tier usage | high，usage v2 语义合并 |
| `v7.2.66` | `e99a2056` | XAI using_api/path toggle | high，xAI 配置验证 |
| `v7.2.67` | `2075f77c` | cache token alias | high，token facts 合并 |
| `v7.2.68` | `042f1fea` | image handler auth manager | medium，handler 测试 |
| `v7.2.69` | `f4a8aee6` | namespace/custom tools | high，translator/tool 回放 |
| `v7.2.70` | `9c3f7207` | Codex Alpha Search affinity | high，auth selection/session |
| `v7.2.71` | `5b7f2361` | xAI reasoning replay | high，xAI 大改 |
| `v7.2.72` | `6279bb8a` | provider logos/descriptions | low，资产/README 合并 |
| `v7.2.73` | `2a63b271` | FreeBSD release GOARCH 修复 | high，fork workflow 语义合并 |
| `v7.2.74` | `411d7d41` | Home mapping/SelectAuthByKind | high，scoped routing 联调 |
| `v7.2.75` | `e5741673` | deferred request body logging | high，日志隐私与 metadata |
| `v7.2.76` | `9f62c8df` | custom tool batching | high，translator 行为验证 |
| `v7.2.77` | `c8803713` | tool name 去重/消歧 | high，request/response tool 映射 |
| `v7.2.78` | `768b4c49` | plugin 路径解析、usage generate flag | high，plugin/runtime 与 usage v2 |
| `v7.2.79` | `b6ce0bee` | xAI image usage 与 function schema 简化 | high，xAI executor 冲突 |
| `v7.2.80` | `09da52ad` | gitstore client 更新、Codex incomplete response/error conversion | high，依赖、executor、translator、auth |

## 功能分组

| 分组 | 更新了什么 | 影响模块与作用 | 与 fork 定制关系 | 建议 |
|---|---|---|---|---|
| Usage/tier/cache | service tier、cache_write/cache aliases、stream parse 优化 | usage manager、helpers、queue、executors | 直接冲突刚发布的 RequestDetailV2/唯一终态 | 单独切片语义合并并扩大乱序测试 |
| XAI reasoning/tools | encrypted replay、namespace、X Search、API key、tool schema | xai executor/translator/auth | 与 fork usage hook、routing 和日志交叉 | 上游行为优先，重接 fork hooks |
| Codex tools/search | image tool、custom/additional tools、Alpha Search、compaction | codex executor/translator/server/auth | 影响 alias/scoped pool 与 usage | 按工具事件链和 auth selection 验证 |
| Auth/OAuth | unauthorized refresh、device/cancelable session、Home mapping | auth、management OAuth、conductor | 影响 fork pool、额度禁用和管理端 | 合并状态机并做并发/路由测试 |
| Models/registry | GPT-5.6/Grok、reasoning levels、remote catalog | registry/model updater/config | fork release 会刷新 catalog | 吸收，验证本地/远端模型模式 |
| Release/plugin | model refresh、FreeBSD fix、plugin source/session fixes | workflows、plugin store、server | 与 fork tag-only/GHCR/资产矩阵冲突 | 以 fork workflow 为骨架吸收补丁 |
| Logging/config | deferred body、headers/base URL、display name | logging/config/executor | 隐私和 fork display/routing 配置 | 最小语义合并与脱敏验证 |
| Usage generate | 请求 `generate` flag 以 nil/true 向后兼容、false 显式传播到 plugin/queue | handler metadata、conductor、usage manager、plugin adapters、RequestDetailV2 | 触达 fork usage v2 事实模型 | 增加 canonical generate 字段并验证全链路默认值 |
| Plugin path | plugins dir 默认值、tilde 展开和 store/home/host/builder 统一路径 | config、management plugin store、homeplugins、pluginhost、builder | fork 插件商店和 runtime 定制 | 以上游 resolver 为单一入口，验证相对/绝对/tilde |
| xAI image usage/schema | images/generations usage 发布、function schema rejection 简化 | xAI executor/tests | 与 fork 唯一终态和 cost 统计冲突 | 上游行为主体，重接 UsageReporter 并验证失败/成功唯一终态 |
| Gitstore | go-git client options API、禁用 commit signing | go.mod/go.sum、internal/store | 存储后端与依赖升级 | 吸收并运行 store tests/go mod verify |
| Codex incomplete status | explicit response.incomplete 成功终态、缺失 terminal/断流 request-scoped error、跨协议 conversion | codex executor、translator、auth error mapping | 可能影响凭证禁用、重试、usage 终态 | 保留 fork usage/routing，吸收上游成功/错误分类和协议转换 |

## 跨域提交能力拆分

部分提交不能只按主标签理解，L02 必须按以下 capability 子项吸收和验证。

| Commit | Capability 子项 | 实际影响路径 | 作用与验证 |
|---|---|---|---|
| `423f3d5f` | xAI API key 配置与管理 | `config.example.yaml`、`internal/config`、management config handlers、TUI | 新增配置、CRUD、disable、列表与文案；验证 parse/round-trip、密钥脱敏和管理 API |
| `423f3d5f` | xAI API key 热更新与模型路由 | `internal/watcher`、`sdk/cliproxy/providers.go`、`sdk/cliproxy/service.go`、auth conductor/types | 运行时增删 key、model alias/force mapping；验证 watcher diff/hash、provider registration、auth kind |
| `423f3d5f` | xAI executor 认证 | xAI websocket/executor tests | API key 与 OAuth 路径共存；验证 header、base URL、日志不泄密 |
| `4fe2c60c` | catalog fetch/validate | `cmd/fetch_codex_models`、`cmd/validate_codex_models`、`internal/registry` | 远端目录下载、校验、内置 JSON 更新；验证 schema、失败回退和 local-model 模式 |
| `4fe2c60c` | server/handler updater 生命周期 | `cmd/server`、`internal/api/server.go`、Codex model handler | 启动、禁用、关闭 updater 并向 API 暴露目录；验证启动/关闭和 handler 响应 |
| `4fe2c60c` | CI/release refresh | `.github/scripts`、PR build、docker/release workflows | 构建前刷新目录；保留 fork tag-only、GHCR 和资产矩阵 |
| `ec3aba23` | 401 自动刷新 | `sdk/cliproxy/auth/conductor.go` | unauthorized 时 singleflight refresh；验证成功、失败、并发与 fallback |
| `ec3aba23` | fork pool/quota identity 保持 | conductor 与 fork scoped/quota hooks | refresh 前后保持 auth index/source、quota、pool membership 与 LastSelectedAt 语义 |
| `411d7d41` | SelectAuthByKind | auth conductor、Alpha Search | OAuth-only 选择必须先按 kind 过滤，再进入 scoped pool/scheduler |
| `e5741673` | 延迟请求日志 | request logger、SDK handler context | 错误时抓取 request body，同时保留 request ID/client IP 和 secret redaction |
| `dc4be167` / `03d58c44` / `2075f77c` | service tier 与 cache aliases | usage helpers/manager、executors、redis queue | metadata 与 token facts扩展；遵守 reported/computed total、UsageObserved 和唯一终态契约 |
| `35a5f066` | plugin path resolver | config、management plugin handlers、homeplugins、pluginhost、builder | 空值默认、tilde 展开和所有插件入口使用一致路径 |
| `768b4c49` | generate metadata | API handlers、conductor、usage manager/helpers、plugin API、redis queue | missing/true 默认启用，explicit false 贯穿 canonical detail 和下游 sink |
| `9f500aef` / `466cee6e` | xAI image usage | xAI executor | images/generations 路径也发布一次且仅一次 usage 终态 |
| `71b5beb8` | gitstore client migration | go.mod/go.sum、internal/store | 新 options API 与禁用签名，验证 store lifecycle 和依赖 |
| `b6ce0bee` / `3cb2d27d` | xAI schema/rejection | xAI executor | 简化 function schema，保留 namespace/replay 与 fork usage hooks |
| `09da52ad` | Codex incomplete/error conversion | Codex executor、四组 translator、auth errors/conductor | explicit incomplete 成功转换；missing terminal/断流 request-scoped，避免错误 credential ban，并统一跨协议响应 |

## 完整提交矩阵

| Commit | 更新内容 | 模块 | 风险 | 建议处理 |
|---|---|---|---|---|
| `505c59d8` | team plan credential hash 防覆盖 | auth | high | 吸收并验证 account/team identity |
| `cdccc72d` | terminal response 解决 pending Codex tool calls | translator | high | 吸收，工具事件链测试 |
| `4f2e1904` | Antigravity hub user agent | executor | low | 自动合并 + header test |
| `7c47edb1` | Grok 4.5 registry | models | medium | 吸收并核验 capabilities/context |
| `186c87ba` | xhigh confidence targeting | models | medium | 与后续 level 变更按最终态吸收 |
| `ec3aba23` | unauthorized 自动刷新凭证 | auth | high | 语义合并，防刷新风暴 |
| `c6121086` | 调整 thinking levels | models | medium | 以最终 registry 为准 |
| `3fd18926` | XAI reasoning effort 使用 registry | xai/models | high | 上游优先 + fork usage hook |
| `bea95670` | responses/messages cache control | translator | medium | 吸收并测协议转换 |
| `d899c962` | OpenAI max_tokens 到 Gemini | translator | medium | 吸收 + translator tests |
| `53ebde03` | Fenno.ai sponsor | docs | low | 按 fork README 策略吸收 |
| `bc279c61` | Qiniu sponsor | docs | low | 按 fork README 策略吸收 |
| `1204101f` | thinking zero_allowed=false | models | medium | 以最终 registry 为准 |
| `ee71dc52` | Claude model prefix/plugin auth disabled | auth/models | high | 吸收并测 plugin auth/routes |
| `db4f1cef` | cross-family level clamping | validation | medium | 吸收 + model mismatch tests |
| `0df267ad` | XAI tool schema hang/Retry-After | xai | high | 上游优先，桌面客户端回归 |
| `445de6c0` | GPT-5.6 Sol/Terra/Luna | models | medium | 按最终模型表吸收 |
| `f21beb05` | 补注册 GPT-5.6 models | models | medium | 按最终模型表吸收 |
| `b4c59405` | client UA/GPT-5.5/usage helper | models/usage | high | usage 冲突语义合并 |
| `ef0a4a56` | Codex response websocket logging | logging/ws | medium | 吸收并保持 request metadata |
| `ed293344` | 暴露 ultra reasoning | models | medium | 与后续删除按最终态吸收 |
| `15f30371` | Codex modalities 限制 | models | medium | 吸收并测 model response |
| `5f8899b7` | 移除 GPT-5.6 Sol | models | medium | 按最终模型表吸收 |
| `26d45fd4` | model header overrides | config/executor | medium | 吸收并验证配置热更 |
| `f9162d39` | image generation function tool checks | executor | medium | 吸收 + image tests |
| `cfa90f9f` | image PR merge | integration | low | 随父提交吸收 |
| `35dba9b4` | Sol UA/config 更新 | models | medium | 按最终模型表吸收 |
| `20e61f28` | ultra reasoning PR merge | integration | low | 随父提交吸收 |
| `bf25331c` | XAI desktop fix PR merge | integration | low | 随父提交吸收 |
| `ca67caf0` | namespace tool 参数/Codex schema | xai/tools | high | 上游行为优先，工具路由测试 |
| `f084eefa` | 移除 ultra effort | models | medium | 按最终 registry 吸收 |
| `1af23344` | reject unknown OAuth completed state | oauth | high | 吸收新状态机 |
| `f081b91e` | plugin store source identity | plugin | medium | 吸收并保留 fork store source |
| `d1ef06cb` | legacy getter 隐藏 completed session | oauth | high | 吸收 + legacy compatibility test |
| `7115e7e0` | OAuth completion idempotent | oauth | high | 吸收 + concurrent completion test |
| `04109920` | OAuth/plugin source PR merge | integration | low | 随父提交吸收 |
| `631f7a65` | Responses Lite image tool | executor | medium | 吸收 + lite request tests |
| `3554b637` | Sol Lite image PR merge | integration | low | 随父提交吸收 |
| `abb52248` | Codex cache_write_tokens | usage | high | 映射到 canonical cache creation/write facts |
| `cc2095f3` | 移除 sponsor docs | docs | low | 按 fork README 裁决 |
| `045a9642` | Grok image-only models | handlers | medium | 吸收并测 image routing |
| `dc4be167` | request/response service tiers | usage | high | 扩展 canonical detail/queue |
| `ea20742e` | 无 usage 时保留 response tier | usage | high | 保持唯一终态且保留 metadata |
| `3533484a` | chat-proxy headers/base URL | executor | medium | 吸收并测 header/base URL |
| `dc162b93` | XAI header refactor | xai | high | 上游优先 + fork logging/usage |
| `bc812e5f` | 跳过无关 stream chunks | usage/perf | high | 吸收但证明不漏 usage |
| `6e819ab6` | device flow/cancelable OAuth | oauth | high | 状态机语义合并 |
| `8c2bf2c2` | service tier PR merge | integration | low | 随 usage 提交吸收 |
| `aa05fb27` | websocket output restore/compact | executor | high | 吸收并测 compact/stream |
| `e99a2056` | XAI using_api path toggle | xai/config | high | 吸收并验证配置默认值 |
| `759b30ee` | CPA Tray docs | docs | low | 文档吸收 |
| `2075f77c` | cache_tokens aliases | usage | high | canonical cache split 语义合并 |
| `9418054a` | tray docs PR merge | integration | low | 随父提交吸收 |
| `f35539c2` | Cubence sponsor | docs | low | 按 fork README 裁决 |
| `6fc4f0c4` | FastAIToken sponsor | docs | low | 按 fork README 裁决 |
| `46e2894a` | GPT-5.6 standalone search | codex | high | 吸收并测 search routing |
| `1a0bbe09` | Grok CLI OAuth 识别 | xai/auth | high | 吸收并测 credential kind |
| `6c70996e` | XAI resolved base URL logging | xai/logging | medium | 吸收但脱敏 URL/credentials |
| `042f1fea` | image handler auth manager API | handlers | medium | 吸收并调整调用测试 |
| `0ba5fab5` | XAI CLI UA PR merge | integration | low | 随父提交吸收 |
| `07455ecb` | 合成缺失 tool call id | translator | high | 吸收 + event chain tests |
| `bd7cc647` | Codex additional/custom tools | translator | high | 吸收并测 history replay |
| `bd2aafb8` | custom_tool_call item replay | translator | high | 吸收 + client protocol tests |
| `e9d3dfbc` | tool output/custom input 健壮性 | translator | medium | 吸收 + malformed fixtures |
| `dc39f445` | Responses Lite tool events | translator | high | 吸收 + stream events |
| `f4a8aee6` | namespace/custom tool support | translator | high | 与 xAI/Codex 联合验证 |
| `3586d3e7` | configurable model display names | config/models | medium | 吸收并保持 API 兼容 |
| `041816c2` | XAI encrypted reasoning replay | xai | high | 上游主体 + fork usage hook |
| `dc551b7d` | 删除 unused xAI helpers | xai | medium | 跟随上游最终结构 |
| `dee653cd` | standalone search PR merge | integration | low | 随父提交吸收 |
| `9c3f7207` | Alpha Search affinity/logging | codex/auth | high | 与 scoped routing 合并 |
| `55f4d6ed` | Gin context 传递到 auth selection | codex/auth | high | 保留 request metadata 与 selection |
| `3f875ecd` | 避免 ambiguous reasoning injection | xai | high | 吸收 + replay tests |
| `0e3a3e61` | namespace routing | xai | high | 吸收 + tool choice tests |
| `f1e9347f` | tool-call-only replay batches | xai | high | 吸收 + replay tests |
| `c4eda81b` | namespace review feedback | xai | high | 跟随上游最终行为 |
| `487f8afc` | 禁用未隔离 replay | xai/security | high | 吸收并验证 auth isolation |
| `eb6d1694` | namespaced tool choices | xai | high | 吸收 + routing tests |
| `18d239d5` | compaction 后清 replay | xai | high | 吸收 + compaction tests |
| `5b7f2361` | xAI replay PR merge | integration | low | 随父提交吸收 |
| `e0a0b5a4` | websocket local compaction summary | codex/ws | high | 吸收并测 summary/usage |
| `4123e275` | additional namespace tools | xai | high | 跟随最终路由 |
| `19fb0f07` | namespace PR merge | integration | low | 随父提交吸收 |
| `e674f191` | allowed tool namespace choices | xai | high | 吸收 + allowed tools tests |
| `4fe2c60c` | remote-refresh Codex model catalog | registry/release | high | 吸收 updater；手工合并 fork workflows |
| `6279bb8a` | provider SVG logos/descriptions | assets/docs | low | 吸收资产和 README |
| `ceaeb75d` | tool search 按 provider gate | codex | high | 吸收 + provider tests |
| `e73aad2e` | tool search 要求 model template | codex | high | 吸收 + fallback tests |
| `7efe8b39` | disable custom tool search PR merge | integration | low | 随父提交吸收 |
| `cf10f25e` | Grok Search MCP docs | docs | low | 文档吸收 |
| `a9813dcc` | MCP docs translations | docs | low | 文档吸收 |
| `7bb81328` | normalize custom tool history | xai | high | 吸收 + history fixtures |
| `7f6d491e` | reasoning encrypted_content 性能 | executor/perf | medium | 吸收 + correctness/perf sanity |
| `caa93a7f` | filter internal X search calls | xai | high | 吸收 + filtering tests |
| `194fbce4` | strip orphan reasoning ids | executor | high | 吸收 + store-disabled tests |
| `4651c370` | reuse parsed input array | xai/perf | medium | 吸收 + behavior tests |
| `4fd81f90` | X search history PR merge | integration | low | 随父提交吸收 |
| `a5577cc6` | client-declared X Search filtering | xai | high | 吸收 + internal/external tool tests |
| `423f3d5f` | XAI API key support | xai/auth | high | 吸收并验证凭证/日志脱敏 |
| `a9831c84` | sync allowed_tools/prune choices | xai | high | 跟随最终工具路由 |
| `2a63b271` | FreeBSD `TARGET_GOARCH` 修复 | release | high | 移植到 fork release workflow |
| `06a4e46f` | MCP docs PR merge | integration | low | 随父提交吸收 |
| `6c6b16f0` | Kimi logo 更新 | assets | low | 吸收资产 |
| `03d58c44` | collapse service tier metadata | usage | high | 按最终 tier schema 合并 |
| `160a5561` | Home force mapping | auth | high | 与 fork scoped routing 合并 |
| `411d7d41` | SelectAuthByKind/Alpha Search | auth | high | 专项 auth selection tests |
| `3d46ede4` | reasoning content serialization | translator | high | 吸收 + protocol tests |
| `e5741673` | deferred request body capture | logging | high | 合并 request metadata 并防泄密 |
| `9f62c8df` | custom tool batching | translator | high | 吸收 + batching/event tests |
| `c8803713` | tool name 消歧/去重 | translator | high | 吸收最终映射并做端到端 translator tests |
| `35a5f066` | plugin dir 默认/tilde 路径解析并统一 store/home/host/builder 使用 | plugins/config | high | 吸收 resolver，验证相对/绝对/tilde 与 fork plugin runtime |
| `768b4c49` | usage generate flag 向后兼容传播 | usage/plugin/queue/handlers/auth | high | 扩展 canonical usage detail，nil/true 默认启用、false 显式保留 |
| `9f500aef` | xAI images/generations 发布 usage | xai/usage | high | 与 fork UsageReporter 唯一终态语义合并 |
| `71b5beb8` | gitstore 新 client options、禁用 commit signing | store/dependencies | medium | 吸收并运行 store tests、go mod verify |
| `b6ce0bee` | 简化 xAI function schema 与 rejection | xai/tools | high | 上游行为主体，保留 replay/namespace/usage hooks |
| `3cb2d27d` | xAI schema PR merge | integration | low | 随最终 xAI 行为吸收 |
| `466cee6e` | 补全 xAI image usage reporting | xai/usage | high | 以最终提交为准，覆盖成功/失败/重复事件 |
| `09da52ad` | Codex explicit incomplete、缺失 terminal/断流错误和跨协议响应转换 | codex/translator/auth | high | 吸收成功/错误分流，保留 fork usage/routing，防错误禁用凭证 |

## 吸收结论

- 不建议分历史 tag 逐次 merge；这些 tag 在同一线性主线上，单一固定目标更易保证最终语义，但冲突处理必须按 release、usage、xAI/Codex、auth/plugin/store 风险切片分阶段评审。
- L02 的核心原则是“上游 provider/tool 行为为主体，fork usage/routing/release 能力显式重接”，而非整文件保留 fork 旧实现。
- 进入 L02 前必须完成 `v7.2.80` 漂移增量复评，并获得用户对 11 个机械冲突、35 个自动热点和单目标吸收方式的确认。
