---
artifact: docs/PRD.md
stage: prd
workflow_id: prod-dev
workflow_version: 2
author: product-director
date: 2026-09-18
root_task_id: t_a8f34caa
source_task_id: null
prd_ref: self
scope_target: cpa-plugin @ 3a039f9（workbuddy 0.9.3 / qoderwork 待定）适配 CLIProxyAPI v7.3.5
downstream: architect-director
approval_required: false
approval_note: >
  本 PRD 的范围与目标版本来自 admin 已授权的 commander 路由结论（D1–D7），不新增需拍板的产品决策。
  仅遗留两项**执行前置**（P-ENV 本地 Go 工具链授权、P-REPO 两个新仓库的 GitHub 侧创建），
  已在第 8 节列为显式前置条件，不改变本 PRD 的功能范围。
---

# cpa-plugin 适配 CLIProxyAPI v7.3.5 — 产品需求文档（PRD）

> 本文所有事实均来自**本轮本地实读 + 上游 v7.3.5 源码（`/tmp/cpasdk/CLIProxyAPI-7.3.5`、`/tmp/cpachk`）直读**，
> 关键结论标注证据位置。文档不含任何 secret、token、cookie、私钥或账号凭据。
> 本文取代此前以「commander 转发的范围说明」为输入的临时状态，为 `docs/DESIGN.md` 提供正式上游依据（`prd_ref: docs/PRD.md`）。

---

## 1. 背景与问题

`cpa-plugin` 是 CLIProxyAPI（下称 CPA）的插件集合，当前含两个 OAuth Provider 插件：`workbuddy`、`qoderwork`。
两者 `go.mod` 均钉在 CPA SDK `v7.2.30`（`workbuddy/go.mod:6`、`qoderwork/go.mod:7`），而宿主已升到 **v7.3.5**
（本机 `/opt/homebrew/bin/cliproxyapi` 实测 `CLIProxyAPI Version: 7.3.5`）。
「能加载」不等于「契约正确」：v7.3.0 起插件契约引入 3 个版本门控与 4 项新能力，`v7.2.30` 编译出的插件
在 v7.3.5 宿主上以 **legacy schema 语义**运行，其中一项门控会造成**用户可见的功能缺陷**。

因此本项目的目标不是「让它还能加载」，而是：**升到 v7.3.5 契约、修掉 3 个真实缺陷、补齐宿主已提供而插件未声明的能力、并把发布链路打通**。

### 1.1 三个真实缺陷（D2 的「修缺陷」主体）

| # | 缺陷 | 机制（证据） | 用户可见后果 |
|---|---|---|---|
| D-A | **面板 JSON 实体转义乱码** | 宿主在 `schemaVersion < 6` 时对管理响应的 JSON body 逐字符串做 `html.EscapeString`（`internal/pluginhost/management.go:270,348,356` → `internal/htmlsanitize/htmlsanitize.go:12`）；而两个面板前端对响应**只做裸 JSON 解析、无任何实体解码**（`workbuddy/panel.html:661` `JSON.parse(text)`、`qoderwork/panel.html:591` `r.json()`；两处 `esc()` 均为**生成端**转义） | 账号昵称 / plan / 错误信息中的 `& < > ' "` 显示为 `&amp;` `&lt;` 等实体 |
| D-B | **发布 tag 校验失败，商店安装链路断裂** | 宿主 `ReleaseVersion()` 只剥离前导 `v/V`，随后必须匹配 `^[0-9][0-9A-Za-z.+-]*$`（`internal/pluginstore/github.go:114-122`、`version.go:26-32`）。现有 tag 形如 `workbuddy-v0.8.5` → 校验**失败**；上游 `releases/latest` 返回的正是该 tag（实测 GitHub API） | 商店「安装/更新」全部失败；即便手工落盘，`ManifestFromRelease` 仍报错 |
| D-C | **qoderwork 版本号三处冲突** | `registry.json:27`=0.2.6、`qoderwork/VERSION`=0.4.1、`qoderwork/main.go:337`=0.8.2；`qoderwork/README.md:24,32` 另写 0.2.6 | 面板显示版本、商店声明版本、release 版本三者互不一致，无法判断「装的是哪一版」 |

D-A 由 **升 SDK 自动修复**（详见 2.1）。D-B、D-C 需显式工程动作。

---

## 2. 已定约束（D1–D7）

| # | 决策点 | 已定值 | 机制级依据 |
|---|---|---|---|
| D1 | 目标版本 | 钉 **v7.3.5** | v7.3.5 与 v7.3.6 的 `sdk/pluginabi`、`sdk/pluginapi` 字节完全相同；7.3.5→7.3.6 变更全在 `internal/*` 与 `sdk/cliproxy/*`，**插件契约零变化** |
| D2 | 兼容策略 | **升 SDK + 补齐能力**，重心是修缺陷 | 升 SDK 自动带来 schema 1→6 与 D-A 修复；`quota.*` / `request.complete` / `plugin.quiesce` 各自对应一个真实缺失面 |
| D3 | `SchemaVersion` 目标值 | **6**，不得硬编码 1 | 插件注册处写的是**常量引用** `pluginabi.SchemaVersion`（`workbuddy/main.go:344`、`qoderwork/main.go:341`）；该常量 v7.2.30=1、v7.3.0~7.3.6=6（`sdk/pluginabi/types.go:20`）。停在 1 必须显式写死字面量，属主动降级 |
| D4 | 发布 tag 命名 | 改用**裸 `vX.Y.Z`** | 见 D-B；`pluginStoreReleaseTagCandidates("0.9.4")` 只试 `["0.9.4","v0.9.4"]` |
| D5 | 仓库结构 | **拆为两个仓库**（workbuddy / qoderwork 各一），各自裸 `vX.Y.Z` | 裸 tag 在单仓内产生跨插件版本命名空间冲突；单仓下 `/releases/latest` 无法按插件解析版本 |
| D6 | CodeBuddy Intl | **本轮不纳入**，单独立项 | 引入新 realm / 额度池 / 登录平台，属功能扩张 |
| D7 | 实施顺序 | **workbuddy 先行，qoderwork 跟随** | qoderwork 版本治理更乱（D-C），且当前**零 Go 测试** |

### 2.1 升 SDK 后插件契约如何变化（实读结论）

- 注册请求新增 `schema_version` 字段（宿主 `internal/pluginhost/rpc_client.go:68-71`）；插件 `configure()` 只解 `config_yaml`，
  `encoding/json` 忽略未知字段 → **安全**。
- 宿主只拒绝 `resp.SchemaVersion > 6`（`rpc_client.go:75`）；`0` 视为 legacy 1。插件上报 6 **在允许范围内**。
- 两条未来约束（本次无影响，须写入 ADR 并转给架构/开发）：
  - schema ≥ 3：payload chunk 不再携带 `OriginalRequest` / `RequestBody`；
  - schema ≥ 5：payload chunk 不再携带 `HistoryChunks`。
  - 两个插件当前**零注册** `StreamChunkInterceptor`，故本次不受影响；**未来新增流式分片拦截器时必须自行从 header-init 分片缓存**（`StreamChunkHeaderInitIndex = -1`）。
- schema ≥ 6：管理响应不再 HTML 转义 → **D-A 修复**。

---

## 3. 范围

### 3.1 范围内（本次交付）

**W1 — SDK 契约升级（两插件）**
- `go.mod`：`github.com/router-for-me/CLIProxyAPI/v7 v7.2.30 → v7.3.5`；`go 1.26.0` 保持不变。
- `go mod tidy` 后确认传递依赖（logrus / x/net / x/sys）无意外跳动；`go.sum` 同步。
- 接受 `SchemaVersion` 由常量自然变为 6，**不写死字面量、不做降级开关**。

**W2 — 能力补齐（4 项，两插件同构）**

| 能力 | 采用 | 需求要点 |
|---|---|---|
| `quota.*` | **是** | 新增 QuotaProvider：`Identifier()` 返回**既有** `providerName`（`workbuddy` / `qoderwork`，不得新造字符串）；`DescribeQuota` 返回 `{DisplayName, SupportedProviders:[providerName], SupportsReset:false}`；`FetchQuota` 复用现有 credits 抓取路径并归一化；`ResetQuota` 恒返回 `{Success:false, Message:"provider-managed"}`，**绝不触发上游写操作**。**保留**现有 `/v0/management/<plugin>/credits` 接口（并行新增，非替换）。 |
| `request.complete` | **是** | 注册 `request_lifecycle_plugin`，在 `HandleRequestComplete` 中补齐现有 usage 上报**观测不到的终态**（尤其「客户端主动断开」的 `canceled`）。**以 `RequestID` 为唯一键做幂等去重**，避免与既有 usage 上报双报；**不得依赖 `Metadata` 键名**（键集未逐键实证）。 |
| `plugin.quiesce` | **是** | 实现 `plugin.quiesce`：停 checkin / keepalive / 登录态清理定时器；状态（模型目录缓存等）**原子落盘**；置 draining 标志，使新请求快速失败且带可重试语义。**清理逻辑只能放 quiesce，绝不能搬到 `cliproxyPluginShutdown`**（该导出是刻意的 no-op，宿主随后 `dlclose`，碰 Go runtime 会 SIGSEGV）。 |
| `host.affinity.lookup` | **是，只读** | 面板展示「该会话当前粘在哪个账号」，用于定位「为何路由到非选中账号」。**红线：只读，不得改写亲和性**（那是宿主状态，越权会与 Scheduler 能力冲突）。 |

**W3 — 发布链路修复（D-B / D4）**
- CI tag 触发条件由 `workbuddy-v*` / `qoderwork-v*` 改为裸 `v*`；「发布哪个插件」的判定改为 dispatch 输入或产物目录，不再从 tag 名解析。
- CI 内部 `tag=${P}-v${V}` → `v${V}`；`git tag -a` 同步。
- registry 的 `version` 字段必须与 `ReleaseVersion(tag)` 一致（宿主 `Manifest.Validate` 会比对）。
- 归档名与 zip 根条目**无需改动**（已实测匹配，见 5.1）。

**W4 — 版本单一真源（D-C / D7）**
- 规定唯一真源为各插件 `VERSION` 文件；代码内 `var version` 仅为占位默认值，发布一律 `-ldflags -X main.version=${VERSION}` 注入。
- `registry.json` 的 `version` 由 CI 在发布时同步写入并由校验脚本守住。
- README 不得内嵌具体版本号（改为占位/引用 VERSION），消除文档漂移。
- workbuddy 已有 `version_test.go` 断言「代码默认值 == VERSION」；**qoderwork 必须补同等断言**。

**W5 — 仓库拆分（D5）与 registry 指向**
- 拆为 `workbuddy`、`qoderwork` 两个仓库，各自持有自包含的 CI 与单插件 registry。
- 本仓库内的交付形态：为每个插件产出**自包含的发布单元**（`go.mod` / `VERSION` / `Makefile` / CI workflow / 单插件 registry 片段 / README），使拆分成为机械操作。
- registry 的 `repository` / `homepage` 必须指向**真正发布 release 的那个仓库**（当前指向 `Sliverkiss/cpa-plugin`，实测该仓库确有 `workbuddy-v0.8.5` / `qoderwork-v0.2.6` 两个 release；本 fork `fanchcho/cpa-plugin` 实测 **0 tag / 0 release**）。

**W6 — 测试基线补齐**
- workbuddy：48 个 Go 测试文件（已有）+ `panel.test.js`（Node `node:test`）。**CI 当前只跑 `go test ./...` 与 `go vet ./...`，未跑 `node --test`** → 必须纳入 CI。
- qoderwork：**当前零 Go 测试、无 `panel.test.js`**。本次至少补齐：版本单一真源断言、注册响应契约断言（capabilities / schema_version）、QuotaProvider 字段映射（含除零）、quiesce 状态机、RequestID 去重的单元测试。

**W7 — 非目标项的最小防护**
- 面板 `esc()`（生成端）保留不动；D-A 的修复点是**宿主不再转义**，而不是前端加解码。

### 3.2 非目标（明确不做）

| # | 排除项 | 理由 |
|---|---|---|
| D6 | **CodeBuddy Intl / 新 realm / 新额度池 / 新登录平台** | 显式排除，属功能扩张；如启用需另开 PRD，不得在本轮以「顺带」形式混入 |
| N1 | `websocket.response_event` | 两插件均为 HTTP SSE 上游，注册只会给每个请求挂一个永不触发的观察器；留作 CodeBuddy 若上线 WS 端点时再评估 |
| N2 | 重写面板 UI / 引入前端构建链 | 本次只做契约与行为修复，视觉以不回归为准（见 6.2） |
| N3 | 新增第三方 Go / 前端依赖、`package.json` / lockfile | 复用现有 `gopkg.in/yaml.v3`、stdlib、Node 内置 `node:test` |
| N4 | 账号自动删除策略变更、凭据重试 / failover、session routing 重写 | 与本次契约适配无关 |
| N5 | 修改 `docs/DESIGN.md`（由 architect-director 在本 PRD 下游负责） | 职责边界 |
| N6 | 任何形式的 secret / token / cookie / 私钥 / 账号凭据落盘到仓内文档、测试夹具或 CI 日志 | 安全边界 |

> **D6 说明（须在评审中显式确认）**：本轮**不**引入 CodeBuddy Intl。任何以 Intl realm 为前置的验收项均不在本 PRD 中。

---

## 4. 用户故事

| ID | 角色 | 故事 | 关联验收 |
|---|---|---|---|
| US-1 | 插件使用者（运维/自建 CPA 用户） | 作为使用 v7.3.5 宿主的用户，我希望从商店点「安装/更新」就能装上插件，而不是遇到版本解析失败 | AC-6 |
| US-2 | 插件使用者 | 作为多账号用户，我希望面板上含 `&`、`<`、`>` 的账号昵称与 plan 名按**原文**显示，而不是 `&amp;` 这类实体串 | AC-3 |
| US-3 | CPA 管理端使用者 | 作为管理端用户，我希望 `GET /v0/management/quota/providers` 能列出 workbuddy / qoderwork，并且配额视图的余额与插件面板展示的余额**一致** | AC-4 |
| US-4 | 插件使用者 | 作为热更新插件的用户，我希望升级插件时旧实例能确定性地停表、落盘、拒新请求，而不是留下后台定时器继续打上游 | AC-5 |
| US-5 | 维护者 | 作为维护者，我希望「当前版本号」只有一个真源，四处（VERSION / 代码默认值 / registry / README）永远一致，不再需要人工比对 | AC-7 |
| US-6 | 维护者 | 作为维护者，我希望 CI 是兼容性的权威判据：编译、单测、多平台产物、面板 JS 测试全部在其中执行 | AC-1 |
| US-7 | 计费/观测使用者 | 作为依赖用量数据的用户，我希望被客户端中断（canceled）的请求也能被计入，且不产生重复记录 | AC-5 附属 / AC-8 |
| US-8 | 维护者 | 作为需要在本地复现运行时行为的维护者，我希望有一条可重复的本地运行时验证路径（宿主 + 真实插件），而不是只能靠读码 | AC-2 / AC-3 / AC-5 |

---

## 5. 功能清单

### 5.1 已验证通过、本次**无需改动**（避免过度施工）

| 项 | 实测结论 | 证据 |
|---|---|---|
| 归档命名 | `ArchiveName(id, version, goos, goarch)` = `<id>_<version>_<goos>_<goarch>.zip`，与本地 CI `${PLUGIN}_${CLEAN_VERSION}_${goos}_${goarch}.zip` 完全匹配 | `internal/pluginstore/github.go:347-355`、`.github/workflows/build.yml:151` |
| zip 根条目 | 宿主期望根条目名 = `<id>.<ext>`；CI 用 `filepath.Base(libraryPath)` 作 entry name → 匹配 | `internal/pluginstore/install.go:318-320`、`.github/scripts/package-release.go:60` |
| `checksums.txt` | 宿主 `SelectReleaseAssets` 按资产名精确取 `checksums.txt`；`ParseChecksums` 取前两列且不校验第二列文件名 | `internal/pluginstore/github.go:330-336`、`install.go:129` |
| 落盘命名 | 安装时重命名为 `<id>-v<version><ext>`（`versionedPluginFileName`），插件 ID 反解依赖 `-v` 后缀 | `internal/pluginstore/install.go:371-373` |
| registry 结构 | 本地 `schema_version: 1` + `github-release`，与官方商店一致；`scripts/validate-registry.py` 规则与宿主 `ValidateRegistry` 一致 | 本地实读 + `internal/pluginstore/registry.go:18-19,167-173` |
| 能力匹配顺序 | `QuotaProvider.Identifier()` → 插件 ID → `AuthProvider.Identifier()`，再以 `SupportedProviders` 兜底 | `internal/pluginhost/quota_provider.go:157-190` |

### 5.2 `QuotaProvider` 字段映射需求（两插件同构）

`FetchQuota` 必须经宿主为本次调用开启的回调上下文发出（`hostHTTPDoWithCallback(req, req.HostCallbackID)`，**只回传、不自铸 callback id**）。原稿「使用宿主注入的 `req.HTTPClient`」（`internal/pluginhost/quota_provider.go:374-376` 按 `AuthIndex` 回填 `AuthID`/`Provider`/`StorageJSON`/`Metadata`/`Attributes`/`Host` 并注入 HTTP 桥）**已作废**：该字段带 `json:"-"` 不在 wire 上，且 `sanitizePluginRequest` 对 `pluginapi.QuotaFetchRequest` / `rpcQuotaFetchRequest` 双 case 均置 nil（`internal/pluginhost/rpc_client.go:185, 274-281`），在 c-shared 出进程插件模型下物理不可达。依据见 `docs/DESIGN.md` §14 **ADR-001**。
**不得**自建带 `host_callback_id` 的嵌套 `host.http.do`——宿主对回调嵌套有刻意的规避设计（`CompleteRequestExcept` / `skipPluginID`）。

| 现有字段 | 目标字段 |
|---|---|
| `TotalRemain` / `TotalSize` | `Groups[0].Buckets[0].RemainingFraction = remain/size`（`size <= 0` 时取 `1.0` 或省略 bucket，**必须防除零**） |
| `Plan` | `Subscription.Plan` |
| `Packages[].CycleEnd` | `Buckets[].ResetTime`（RFC3339 字符串，宿主不解析） |
| `TotalRemain` / `TotalUsed` | `Summary[].Value`（各一条 `QuotaMetric`，`Unit:"credits"`） |

三条硬约束：
1. `FetchQuota` 必须经宿主回调上下文发出（只回传、不自铸 callback id），依据 `docs/DESIGN.md` §14 ADR-001。
2. **保留** `/v0/management/<plugin>/credits`——面板依赖 `/accounts`、`/credits` 的 packages 明细、checkin、region 等富字段，`QuotaFetchResponse` 承载不了。
3. 复用现有 credits 缓存与失效路径（`invalidateAccountCredits`），避免配额查询绕开限流刷爆上游。

### 5.3 `request.complete` 去重需求

- 幂等键：`RequestID`（`pluginapi.RequestCompletion.RequestID`）。
- 终态枚举：`succeeded` / `failed` / `rejected` / `canceled`（`sdk/pluginapi/types.go:1091-1100`）。
- 目标：补齐插件内部**观测不到**的终态（客户端断开 → `canceled`）的计费，且**不产生第二条 usage 记录**。

### 5.4 `plugin.quiesce` 行为需求

| 动作 | 要求 |
|---|---|
| 停定时器 | checkin（09:00/21:00）、keepalive（22:00）、登录态清理 ticker 全部停止；停止后不得再发起任何上游请求 |
| 原子落盘 | 模型目录等过程内状态以「临时文件 + 同目录 rename」方式落盘（沿用既有 `writeModelCacheAtomic` 语义），不得产生半截文件 |
| draining | 置 draining 标志；此后到达的 executor / 管理写请求**快速失败**，响应 error 体显式携带 `retryable: true` 与 `http_status: 503`（宿主 `pluginabi.Error` 支持该两字段） |
| 禁止项 | **不得**把清理逻辑放进 `cliproxyPluginShutdown`（该导出保持 no-op） |

---

## 6. 可执行验收矩阵

**判据权威层级**：**CI = 编译/单测/多平台产物的唯一权威源**；**运行时验收 = 契约协商行为的唯一权威源**（schema / quota / complete / quiesce / affinity 全是宿主运行时协商，单测覆盖不到）。

### 6.1 功能验收矩阵

| ID | 对应 | 判据（可执行） | 方法 / 证据 | 类型 | 门 |
|---|---|---|---|---|---|
| **AC-1** | 1 | 两插件在 v7.3.5 下**编译 + 单测 + 多平台产物**全部通过 | CI `test` job：`go test ./...`、`go vet ./...`、`python3 scripts/validate-registry.py registry.json`，**并新增 `node --test panel.test.js`（workbuddy）**；`build` job 产出 6 平台 c-shared（linux amd64/arm64、darwin amd64/arm64、windows amd64、windows arm64）+ `build-cross`（windows-arm64、freebsd-amd64）；任一 job 红即失败 | CI | P0 |
| **AC-2** | 2 | `plugin.register` 响应的 `schema_version` = **6** | (a) `go.mod` 钉 `v7.3.5` 且 `go.sum` 同步；(b) 单测断言注册载荷 JSON 的 `schema_version == pluginabi.SchemaVersion`（值 = 6，取自常量而非字面量）；(c) 运行时以 AC-3 的通过**间接证明** schema ≥ 6 已生效（宿主无独立端点暴露该值，此为已知证据边界） | CI + 单测 + 运行时 | P0 |
| **AC-3** | 3 | 面板显示的账号昵称 / plan / 错误信息中，`& < > ' "` 呈现**原文** | 运行时：构造昵称含 `A&B<C>D'E"D`、plan 含 `<Pro>` 的账号，打开 `/v0/resource/plugins/<id>/panel`；管理响应 `/v0/management/plugins/<id>/accounts` 的原始 body 中该字段为**原文**，页面渲染**无** `&amp;`/`&lt;`/`&quot;`/`&#39;` 实体串 | 运行时 + 视觉 | P0 |
| **AC-4** | 4 | `GET /v0/management/quota/providers` 列出 `workbuddy` 与 `qoderwork`；`POST /v0/management/quota/fetch` 的余额与既有 `/v0/management/<plugin>/credits` **一致** | 运行时：`GET .../quota/providers` 返回 `providers[]` 含两项（`provider` 字段值为小写 `workbuddy` / `qoderwork`）；`POST .../quota/fetch {"auth_index":"<idx>","provider":"workbuddy"}` 返回 `Groups[0].Buckets[0].remainingFraction`，换算 `fraction × size` 与 `/credits` 的 `total_remain` 一致（同一账号、同一时刻，允许上游延迟窗口 ≤ 缓存 TTL）；`Summary` 含 `unit: "credits"` 的指标；`size <= 0` 的账号不得出现除零 / NaN | 运行时 | P0 |
| **AC-5** | 5 | `plugin.quiesce` 被调用时定时器停止、状态原子落盘、新请求快速失败 | 运行时：触发宿主热替换 → 宿主日志**不出现** `pluginhost: plugin quiesce unsupported` 的 Debug 回退，也不出现 Warn；此后一个调度窗口内**无**checkin / keepalive 上游请求；状态文件以 rename 更新（无 `.tmp-` 残留）；一个新 executor 请求得到显式失败响应且 error 体带 `retryable: true` + `http_status: 503` | 运行时 | P0 |
| **AC-6** | 6 | 发布链路端到端可用：打 tag → 生成 release（含 `checksums.txt` 与全平台归档）→ 从商店安装 → 面板版本号非空 | 端到端：以**裸** `vX.Y.Z` 打 tag（不再用 `<id>-v*`）；release 资产含 `<id>_<version>_<goos>_<goarch>.zip` × 7 平台 + 精确名 `checksums.txt`；在 CPA 管理端把该仓库的 `registry.json` 加为 store source → 安装成功；落盘文件名为 `<id>-v<version>.<ext>`；插件面板显示版本号非空且 == registry 声明版本 | 端到端 | P0 |
| **AC-7** | 7 | 版本号单一真源：`registry.json` / `VERSION` / 代码默认值 / README 四处一致 | 单测 + 脚本：每插件一个「`var version` == `VERSION` 内容」断言（workbuddy 已有，qoderwork 必须新增）；registry `version` == `VERSION` 断言；README **不得**出现形如 `0.9.3` / `0.2.6` 的具体版本字面量（脚本扫描，零命中）；`scripts/validate-registry.py` 通过 | CI | P0 |
| **AC-8** | US-7 | `canceled` 终态被记录，且无重复 usage 记录 | 运行时 + 单测：客户端主动断开一次流式请求 → usage 侧出现该次记录且带 canceled 语义；同一 `RequestID` 重放 `request.complete` **不产生第二条记录**（以 `RequestID` 幂等，不依赖 `Metadata` 键名） | 运行时 + 单测 | P1 |

### 6.2 视觉验收标准

| ID | 标准 | 判据 |
|---|---|---|
| VC-1 | **实体串零命中**（D-A 的核心视觉判据） | 面板任意渲染态下，页面可见文本中**不出现** `&amp;`、`&lt;`、`&gt;`、`&quot;`、`&#39;`；含特殊字符的昵称/plan/错误按原文展示 |
| VC-2 | 无视觉回归 | 面板布局与既有基线一致（对照 `docs/cpamp-workbuddy-panel.png`）：左侧导航、页头「账号面板」、工具栏「刷新数据 / 全部签到 / 自动签到」、账号卡片（昵称 + plan 徽章 + 用量进度 + 已用/总量 + 签到状态）结构不变 |
| VC-3 | 新增只读信息不破坏卡片 | 若展示 affinity 只读信息，必须以卡片内的惰性字段呈现，不得新增阻塞式请求；查询失败时静默降级（不出现红色错误块） |
| VC-4 | 状态语义颜色不反转 | `已禁用` / `耗尽` / `使用中` 徽章的既有色语义保持不变 |

> 视觉验收由 `test-director` 在 `docs/TEST_REPORT.md` 中以文本证据（DOM 文本 / 响应原文）落地，**不要求**像素级比对。

### 6.3 测试要求

| 层 | 要求 |
|---|---|
| 单元测试（Go） | 必须新增/覆盖：注册载荷契约（`schema_version`、`quota_provider`、`request_lifecycle_plugin`）；`QuotaProvider` 四方法字段映射 + `size<=0` 除零保护；`RequestCompletion` 幂等去重；quiesce 状态机（停止/落盘/draining）；版本单一真源断言（qoderwork 首次补齐）。**沿用合成 fixture / fake host / `httptest`，禁止访问真实上游或使用真实 token** |
| 前端测试（Node） | `workbuddy/panel.test.js`（`node:test` + `node:vm`，既有 16 个用例）必须被 CI 执行；D-A 相关的新增断言以「响应原文进入渲染路径」为准 |
| 契约回归 | SDK 升级前后各存一份 `plugin.register` 响应快照并 diff，确认**只有** `schema_version` 1→6 与 `capabilities` 新增字段两类变化 |
| 多平台 | 六平台 native + 两个 cross 目标全绿；`c-shared` 产物存在性由 CI 断言（非仅 `dlopen` 成功） |
| 运行时 | 见 6.1 AC-2/3/4/5/8；宿主取 **v7.3.5**，两插件分别加载（拆分后按各自仓库产物） |
| 反例 | 「手工把 `.so` 塞进 plugins 目录后宣称兼容」不算验收证据；**只能证明 dlopen 成功，≠ 行为正确** |

---

## 7. 安全边界

1. 任务 body、comment、metadata、文档、测试夹具、CI 日志中**不得出现**任何真实 API key、secret、token、cookie、私钥或账号凭据；本机 `config.yaml` 内的凭据字段**严禁**复制进仓内文件。
2. 面板 `management_key` / `usage_report_key` 的既有处理语义不变：空值依赖宿主中间件；非空时保持常量时间比较 + per-IP 限流；**不得**把 key 回显进任何响应。
3. 面板错误分类保持脱敏（不暴露上游响应体与原生错误文本）；D-A 修复只影响**转义**，不得放宽脱敏规则。
4. `ResetQuota` **禁止**触发上游写操作（恒 `Success:false`）。
5. `host.affinity.lookup` 只读；不得以任何形式改写宿主亲和性状态。
6. 发布资产仅由 CI 产出；不得在 release 中附带本地环境文件（`config.yaml*`、`.env*`、`auths/`、日志）。

---

## 8. 前置条件与已知风险

### 8.1 执行前置（需在开工前明确，不改变功能范围）

| ID | 前置 | 说明 | 归属 |
|---|---|---|---|
| **P-ENV** | 本地 Go 工具链 | 本机实测**无 `go`**、无 module cache、无 `vendor/`；`brew` 可提供 `go 1.27.1`（满足 `go 1.26.0`）；Node 已装。AC-2/3/4/5/8 的运行时验证需要本地编译 c-shared 产物 | 需 admin 授权 `brew install go`；否则运行时验收仅能由 CI + 宿主二进制读码完成，AC-3/4/5/8 需按「证据受限」重述 |
| **P-REPO** | 两个新仓库的 GitHub 侧创建 | D5 拆仓需要在 GitHub 创建/迁移仓库并推送；`AC-6` 端到端安装依赖真实的 `repository` 指向与 release 存在。本 fork 实测 **0 tag / 0 release** | 需 admin 执行仓库侧动作；开发侧交付「自包含发布单元」使之可机械执行 |
| **P-STORE** | 商店源登记 | 官方商店（74 插件）中**不存在** workbuddy / qoderwork；AC-6 走自定义 store source | 执行时在管理端登记仓库 `registry.json` URL |

### 8.2 风险

| # | 风险 | 应对 |
|---|---|---|
| R1 | 传递依赖在 `go mod tidy` 后意外跳动（x/net、x/sys） | 以 CI 全绿为门；`go.mod` diff 超出「SDK 行」即复审 |
| R2 | 单仓裸 tag 的命名空间冲突（D5 的备选方案 B） | 严格按 D5 拆分；若拆分未获授权，**必须**回到 admin 重新决策，不得自行退化为方案 B 并宣称 AC-6 通过 |
| R3 | `request.complete` 的 `Metadata` 键集未逐键实证 | 去重闸门以 `RequestID` 为唯一键，不依赖 `Metadata` |
| R4 | 面板/管理响应在 schema 6 下不再转义 → 若前端存在「依赖实体解码」的隐式假设则会反向出错 | 实读确认两个前端均为裸 JSON 解析、无解码逻辑（`workbuddy/panel.html:661`、`qoderwork/panel.html:591`），并新增回归断言 |
| R5 | qoderwork 零测试基线，补齐成本高于 workbuddy | 按 D7 顺序：workbuddy 先行交付并冻结契约，qoderwork 跟随复用 |
| R6 | affinity 只读接入若被误用为写路径 | 架构/评审阶段显式标注「只读」红线，测试覆盖「查询失败静默降级」 |

### 8.3 未验证项（不在本轮结论内）

1. 官方 `docs/plugin-development` 对 schema 6 的**文档口径**（本文全部结论来自源码直读；若官方文档与源码冲突，以文档复核）。
2. `request.complete` 的 `Metadata` 具体键集。
3. 社区情报中关于 CodeBuddy Intl 与 CN 同后端/同额度池的说法——**不影响本次 7.3.5 适配**，仅影响 D6 的未来立项。

---

## 9. 下游交接

- 下游：**exactly one** `architect-director` 子任务（board: `prod`，skill: `hermes-dev-workflow`）。
- 架构侧需在本 PRD 基础上完成：`docs/DESIGN.md` 的 `prd_ref` 回填与方案收口；按 5.2/5.3/5.4 落地接口与数据映射；判定并声明 `approval_required`。
- 架构侧**必须**为以下决策点给出可验证结论：SDK 升级后传递依赖范围、quiesce 的落盘文件清单与 draining 错误码、affinity 只读接入点、拆仓后的 CI/registry 文件布局与 P-REPO 的机械执行步骤。
- 本 PRD 不产出代码；`AC-*` 为下游 dev / test 的**唯一验收依据**。

---

*End of PRD — cpa-plugin @ CPA v7.3.5 适配（workbuddy / qoderwork）*
