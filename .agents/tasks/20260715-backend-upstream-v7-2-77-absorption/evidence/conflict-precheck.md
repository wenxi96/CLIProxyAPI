# 后端冲突预检报告（漂移后目标 v7.2.80）

## 预检命令

- 命令: `git merge-tree --write-tree --name-only dev 09da52ad509e2c18e7b9540db3b98c2214c280aa`
- 目标分支: `dev@1c36ebc5`
- 上游目标: `09da52ad` / `v7.2.80`
- 退出码: `1`（存在内容冲突）
- merge-tree object: `c87eda197a6866db8ed902c4a74305b3ee1da9fe`

## 机械冲突

- 结论: 11 个内容冲突，另有 35 个双方修改但可自动合并的重叠文件。相对 v7.2.77，`codex_executor.go` 成为机械冲突，`sdk/cliproxy/usage/manager.go` 转为自动热点，并新增 pluginhost/gitstore/builder 热点。

| 文件 | 冲突来源 | 建议解决 |
|---|---|---|
| `.github/workflows/docker-image.yml` | fork tag-only/GHCR 元数据链；上游 remote model refresh | 保留 fork 触发、标签和 GHCR 语义，吸收上游模型目录刷新步骤；不得恢复 master 自动正式发布 |
| `.github/workflows/release.yaml` | fork 多平台/GLIBC/FreeBSD/checksum 发布链；上游模型刷新与 FreeBSD `TARGET_GOARCH` 修复 | 以 fork workflow 为骨架，逐步吸收两个上游修复并验证资产矩阵 |
| `cmd/server/main_test.go` | fork Home/plugin runtime defaults；上游 Codex model catalog remote refresh | 保留 fork 默认值断言，增加上游 updater 生命周期/关闭语义断言 |
| `internal/api/handlers/management/oauth_callback_test.go` | 旧兼容夹具；上游 cancelable/device OAuth session | 按上游新状态机重写测试夹具，同时保留 fork 管理端兼容断言 |
| `internal/api/server_test.go` | fork 自定义管理路由；上游 Alpha Search、model updater 和 plugin auth 路由测试 | 组合路由集合，不能删除 fork endpoint；按新依赖注入调整测试初始化 |
| `internal/logging/request_logger.go` | fork request metadata/client IP 生命周期；上游错误时 deferred request body capture | 采用上游 deferred capture，但继续使用 fork request ID/client IP helper，禁止日志泄密 |
| `internal/redisqueue/plugin.go` | fork canonical RequestDetailV2/脱敏 queue；上游 request/response service tier | 在 canonical detail/payload 上新增 tier 字段，保持脱敏、identity 与 cost/token 字段 |
| `internal/runtime/executor/helps/usage_helpers.go` | fork唯一终态、token/cache/reasoning facts；上游 cache alias、service tier、快速过滤 | 语义合并：保留唯一终态与 canonical facts，吸收 tier/cache alias/性能优化；禁止回退到重复 publish |
| `internal/runtime/executor/helps/usage_helpers_test.go` | 双方均大幅扩充 usage 测试 | 合并两套测试矩阵并去除仅结构重复；必须覆盖 tier、cache aliases、乱序、missing/failure 唯一终态 |
| `internal/runtime/executor/codex_executor.go` | fork usage/replay/identity；上游 explicit incomplete terminal、缺失 terminal/断流错误、response conversion 与 stream terminal 处理 | explicit `response.incomplete` 作为成功终态转换但不缓存 completed replay；缺失 terminal/非客户端断流使用 request-scoped error；保留 fork UsageReporter、reasoning replay/identity |
| `internal/runtime/executor/xai_executor.go` | fork usage 采集；上游 xAI reasoning replay、namespace、x_search、API key 大改 | 以上游 executor 行为为主体，重新接入 fork UsageReporter 观察/终态；逐项验证 tool routing 与 usage |

## 机械冲突的预期 resolved shape

| 文件 | 必须保留的 fork 语义 | 必须吸收的上游语义 | 禁止结果 |
|---|---|---|---|
| .github/workflows/docker-image.yml | v* tag-only、GHCR 多架构和 fork 元数据 | model catalog refresh script、setup-go 与 catalog 校验 | 引入 DockerHub 发布链或恢复 main/master 自动正式发布 |
| .github/workflows/release.yaml | 多平台、GLIBC、checksums、SourceRepository ldflag 和版本后缀 | model refresh、FreeBSD TARGET_GOARCH 修复 | 丢失任一资产或让 GOARCH/ldflag 只保留单侧 |
| cmd/server/main_test.go | Home/plugin runtime defaults | updater 启动、local-model 禁用和关闭生命周期 | 删除 fork defaults 断言或只验证构造不验证关闭 |
| internal/api/handlers/management/oauth_callback_test.go | 管理端旧客户端兼容 | cancelable/device session、completed 幂等和未知状态拒绝 | 恢复不可取消状态机或删除兼容夹具 |
| internal/api/server_test.go | fork management、batch quota 和 usage 路由 | Alpha Search、model catalog、plugin auth 路由 | 任一已有 endpoint 消失 |
| internal/logging/request_logger.go | request ID、client IP、脱敏 regex 和 metadata 生命周期 | DeferredAPIRequest 延迟抓取请求体 | 提前消费 body、记录密钥或丢失 request metadata |
| internal/redisqueue/plugin.go | canonical RequestDetailV2、identity、token/cost 字段与脱敏 | request/response service tier | 绕过 canonical payload、恢复敏感原文或丢失 tier |
| internal/runtime/executor/helps/usage_helpers.go | 唯一终态、numeric presence、reported/computed total 分层 | tier、cache aliases 和无关 chunk 快速过滤 | parser 合成 reported total、tier-only 变 observed usage、重复 publish |
| internal/runtime/executor/helps/usage_helpers_test.go | missing/explicit-zero、乱序、failure 唯一终态矩阵 | tier、cache_write/cache_creation aliases 和快速过滤矩阵 | 为解决冲突删除任一行为测试 |
| internal/runtime/executor/codex_executor.go | fork UsageReporter、identity、reasoning replay 与 stream 终态 | explicit incomplete 成功终态、缺失 terminal/断流 request-scoped error、terminal failure status 和跨协议转换 | 把 incomplete 当 completed 缓存 replay、重复终态或把缺失 terminal 错误归因凭证 |
| internal/runtime/executor/xai_executor.go | UsageReporter/PublishParsed、fork routing 与日志钩子 | reasoning replay、namespace、X Search、API key、compaction cleanup | 整文件选侧、丢失 replay cleanup 或重复 usage 终态 |

## Usage v2 合并契约

1. Provider parser 只把上游明确报告的 total 写入 Detail.TotalTokens；provider 未给 total 时保持 0，不在 parser 或 UsageReporter 中合成 reported total。
2. internal/usage 是 computed total 的唯一归一化层；ReportedTotalTokens 与 ComputedTotalTokens 分开保存，TotalTokens 按既有 provider-aware 优先级选择，避免 reasoning/cache 重复计数。
3. UsageObserved 只由 numeric token 字段的存在性决定，显式 0 与字段缺失必须可区分；仅有 response_service_tier 时保留 tier，但 UsageObserved=false。
4. cached_tokens 映射 CacheReadTokens/CachedTokens；cache_write_tokens 与 cache_creation_tokens 统一映射 CacheCreationTokens；同一事实不得重复累加。
5. response tier 是 metadata，不得单独触发成功 usage 终态；失败、missing usage、乱序流式事件仍只能发布一个终态。
6. generate 使用三态兼容：core Record 中 nil 与 true 均视为生成启用，只有显式 false 禁用；该事实从 handler metadata、conductor context、UsageReporter、internal usage detail、plugin ABI 和 redis queue 全链路保留。fork 持久化旧记录缺少该字段时也必须归一化为 true，不能因 Go bool 零值误判为 false；对外可暴露归一化后的 effective boolean。
7. xAI images/generations 路径必须发布 usage，但仍遵守 once-only；显式 generate=false 不得在中途被默认值覆盖。

## Auth 选择与 401 刷新契约

- SelectAuthByKind 必须先按 auth kind 过滤 eligible candidates，再进入 scoped-pool/scheduler 选择；只有最终选中的 eligible credential 才能更新 LastSelectedAt 或池成员状态。
- Alpha Search 的 OAuth-only 路径继续参与 scoped-pool，但 API-key 不得被试选或产生 MarkSelected 副作用。
- 401 自动刷新必须保持单飞：并发请求只触发一次 refresh；成功后保留 auth index/source、quota 与 scoped-pool identity，失败后只走一次 fallback/失败计数，不得刷新风暴或错误 eject。
- Home force mapping、plugin scheduler、normal/stream/credits 路径都要维持上述顺序。

## Codex terminal 三路契约

1. 显式 `response.incomplete`：是成功 terminal。保留 status、incomplete_details/finish reason 和 usage，完成跨协议转换并返回成功；不得缓存 completed reasoning replay。
2. 在 `response.completed` / `response.incomplete` 前断流，或 terminal 前发生非客户端 transport failure：返回 request-scoped 408；停止 fallback，不惩罚、不 cooldown、不禁用凭证，usage 只发布一次失败终态。
3. 显式 `response.failed` / `error`：保留 upstream status/error body，默认不是 request-scoped；按真实错误类型进入既有 auth/result 处理，不得被降级为 generic incomplete。

## 46 个重叠路径处置账本

下面的 46 个路径包含 11 个机械冲突和 35 个 Git 自动合并热点。自动合并只代表文本可合并，不能自动接受行为。

| 路径 | 结果 | 处置 | 保护点与验证 |
|---|---|---|---|
| .github/workflows/docker-image.yml | conflict | explicit-fix | tag-only/GHCR + catalog refresh，workflow 静态断言 |
| .github/workflows/release.yaml | conflict | explicit-fix | 资产矩阵/ldflag + GOARCH/catalog，release dry-run |
| README.md | auto | semantic-review | fork 安装/发布说明与上游 provider 文档并存 |
| README_CN.md | auto | semantic-review | 中文 fork 文档与上游功能说明并存 |
| README_JA.md | auto | semantic-review | 多语言链接和资产不破坏 |
| cmd/server/main.go | auto | semantic-review | plugin/Home defaults + model updater 生命周期 |
| cmd/server/main_test.go | conflict | explicit-fix | defaults、updater start/disable/close 测试 |
| config.example.yaml | auto | semantic-review | fork scoped/quota 配置与 xAI/model header 新配置并存 |
| internal/api/handlers/management/auth_files.go | auto | semantic-review | batch quota/禁用逻辑 + upstream auth 行为 |
| internal/api/handlers/management/oauth_callback_test.go | conflict | explicit-fix | 旧兼容 + cancelable/device session |
| internal/api/server.go | auto | semantic-review | fork 管理路由 + Alpha Search/catalog/plugin routes |
| internal/api/server_test.go | conflict | explicit-fix | 完整路由集合与依赖初始化 |
| internal/config/config.go | auto | semantic-review | scoped/quota + xAI key/display/header 字段 |
| internal/config/parse.go | auto | semantic-review | 新旧字段 parse/round-trip |
| internal/config/vertex_compat.go | auto | semantic-review | Vertex 兼容不被通用配置重构破坏 |
| internal/logging/request_logger.go | conflict | explicit-fix | deferred body + metadata/脱敏 |
| internal/pluginhost/adapters.go | auto | semantic-review | generate flag、plugin ABI 默认值与 fork adapter 兼容 |
| internal/redisqueue/plugin.go | conflict | explicit-fix | RequestDetailV2 + tier + 脱敏 |
| internal/redisqueue/plugin_test.go | auto | semantic-review | wire contract、tier、identity、secret redaction |
| internal/runtime/executor/antigravity_executor.go | auto | semantic-review | upstream header/tool 行为 + 唯一 usage 终态 |
| internal/runtime/executor/claude_executor.go | auto | semantic-review | cache split/tier + canonical usage |
| internal/runtime/executor/codex_executor.go | conflict | explicit-fix | search/tool/image + usage/routing hooks + explicit incomplete/缺失 terminal 分流 |
| internal/runtime/executor/codex_openai_images.go | auto | semantic-review | image auth manager + usage/error 行为 |
| internal/runtime/executor/codex_websockets_executor.go | auto | semantic-review | compaction/search + liveness/usage |
| internal/runtime/executor/codex_websockets_executor_test.go | auto | semantic-review | compaction、stream、usage 测试均保留 |
| internal/runtime/executor/helps/logging_helpers.go | auto | semantic-review | deferred logging 与凭证脱敏 |
| internal/runtime/executor/helps/usage_helpers.go | conflict | explicit-fix | Usage v2 合并契约 |
| internal/runtime/executor/helps/usage_helpers_test.go | conflict | explicit-fix | 双方测试矩阵并集 |
| internal/runtime/executor/kimi_executor.go | auto | semantic-review | provider 行为 + canonical usage |
| internal/runtime/executor/openai_compat_executor.go | auto | semantic-review | tier/cache alias + fork usage |
| internal/runtime/executor/xai_executor.go | conflict | explicit-fix | replay/tools/API key + UsageReporter |
| internal/runtime/executor/xai_websockets_executor.go | auto | semantic-review | replay/namespace + usage/日志 |
| internal/runtime/executor/xai_websockets_executor_test.go | auto | semantic-review | API key、replay、stream 回归 |
| internal/store/gitstore.go | auto | semantic-review | 新 client options/禁用签名 + fork store lifecycle |
| internal/tui/client.go | auto | semantic-review | xAI key/config API + fork TUI 行为 |
| internal/tui/i18n.go | auto | semantic-review | 新配置文案和现有 locale |
| internal/watcher/diff/config_diff.go | auto | semantic-review | xAI/header 字段 + scoped/quota 热更 |
| internal/watcher/diff/config_diff_test.go | auto | semantic-review | 新旧字段 diff 断言 |
| internal/watcher/watcher_test.go | auto | semantic-review | reload/synthesizer 状态保持 |
| sdk/api/handlers/handlers.go | auto | semantic-review | request metadata 与新 auth/model handler |
| sdk/auth/filestore.go | auto | semantic-review | auth identity/hash 与 fork 存储 |
| sdk/cliproxy/auth/conductor.go | auto | explicit-fix | kind-before-pool、401 singleflight、force mapping |
| sdk/cliproxy/auth/types.go | auto | semantic-review | xAI/API-key kind 与 scoped identity |
| sdk/cliproxy/builder.go | auto | semantic-review | resolved plugin path + fork service builder defaults |
| sdk/cliproxy/service.go | auto | semantic-review | updater/provider registration + fork lifecycle |
| sdk/cliproxy/usage/manager.go | auto | explicit-fix | UsageObserved + service tier + generate tri-state |

## 非重叠但必须联动审查的新增面

| 能力 | 主要路径 | 原因 |
|---|---|---|
| Codex model catalog refresh | .github/scripts/refresh-model-catalogs.sh、cmd/fetch_codex_models、cmd/validate_codex_models、internal/registry | 新增 updater/validator 和 release/PR workflow，不能只审冲突 workflow |
| xAI API key 全链路 | config、management API、watcher、TUI、sdk/cliproxy/providers.go | 423f3d5f 跨配置、热更、管理 API、SDK 和 executor |
| Alpha Search / SelectAuthByKind | sdk/cliproxy/auth/conductor.go 及 Codex handlers/executors | 自动合并仍存在 kind/pool 选择副作用风险 |
| Usage generate tri-state | sdk/api handlers、sdk/pluginapi/types.go、executor/types、usage manager、plugin/queue adapters | 新增事实必须进入 fork canonical usage v2，缺省兼容和 explicit false 不能丢 |
| Plugin path resolution | internal/config/plugin_path.go、plugin handlers、homeplugins、pluginhost、builder | 相对/绝对/tilde 路径要在所有入口一致，避免商店安装与 runtime 加载指向不同目录 |
| xAI image usage | xAI executor/tests | images/generations 新 usage 路径与 fork once-only reporter 交叉 |
| Gitstore client migration | go.mod/go.sum、internal/store/gitstore.go | 依赖和 client option 行为变化，需验证存储生命周期与凭证处理 |
| Codex incomplete conversion | codex translator 四协议、auth errors/conductor、executor stream tests | explicit incomplete 是可转换成功终态；只有缺失 terminal/非客户端断流是 request-scoped，且所有输出协议要一致 |

## 行为冲突风险

### Usage 与计费事实

- 风险说明: 上游新增 `cache_write_tokens`、cache alias、request/response tier、generate tri-state、xAI image usage 和流式性能优化，直接触达刚发布的 usage v2。
- 证据: 5 个 usage 相关机械冲突，另有多 provider executor 自动合并热点。
- 建议解决: 独立 usage compatibility slice；先合并结构，再按 provider 逐条检查 publish/observe/finalize 顺序，运行乱序重复测试。

### xAI / Codex tool 与 reasoning

- 风险说明: 上游 xAI executor 覆盖 encrypted replay、namespace tool、X Search、API key、image usage 和 schema rejection；Codex 新增 explicit incomplete 成功终态、缺失 terminal/断流 request-scoped error，translator 还新增 custom tool name 去重和 incomplete conversion。
- 证据: `xai_executor.go` 与 `codex_executor.go` 冲突；executor/translator 为上游最大变更区域之一。
- 建议解决: 以目标上游的工具路由为行为基线，只移植 fork usage、routing pool 和日志钩子；禁止整文件 ours。

### Auth/OAuth 与 scoped routing

- 风险说明: 上游新增 unauthorized 自动刷新、cancelable device flow、Home force mapping 和 `SelectAuthByKind`，可能改变 fork 选择池与别名。
- 证据: OAuth/server tests 冲突，`sdk/cliproxy/auth/conductor.go` 为自动合并热点。
- 建议解决: 候选合并后对 auth selection 进行专项静态评审和并发测试。

### Plugin path 与 Gitstore

- 风险说明: plugin dir 现在会默认和展开 tilde，并被 store/home/host/builder 共同使用；gitstore 同时升级 client options 并禁用 commit signing。
- 建议解决: plugin resolver 作为唯一入口，验证商店安装路径等于 runtime 加载路径；gitstore 测试必须预设 `commit.gpgsign=true`，执行 `EnsureRepository` 后断言变为 false，并完成后续 commit/push；同时运行 `go mod verify` 与 `go mod tidy -diff`。

### Codex incomplete 的凭证归因

- 风险说明: explicit `response.incomplete` 应作为成功终态转换并保留 usage；只有缺失 terminal 或非客户端取消的断流应被视为 request-scoped。若两者混淆，会错误返回失败或错误 eject/disable 有效凭证。
- 建议解决: 覆盖 explicit incomplete、missing completion、transport failure、terminal failure 的 normal/stream/mixed pool；验证 incomplete 不缓存 completed replay，request-scoped 失败终态只发布一次且凭证状态不改变。

### Release 链路

- 风险说明: 直接选 theirs 会丢失 fork tag-only、GHCR、多资产与版本后缀；直接选 ours 会丢失上游 FreeBSD 修复和 model refresh。
- 建议解决: 手工语义合并，随后对 workflow trigger、matrix、asset naming、checksums 和 version script 做静态断言。

## 合并建议

- 建议是否进入候选合并: 有条件允许。必须先完成完整 inventory、独立方案评审和用户确认，再在隔离 worktree 推进。
- 需要用户确认的点: 接受 11 个机械冲突和 35 个自动热点的语义合并策略；确认不分批发布中间版本，而以 `v7.2.80` 单一目标形成候选。
