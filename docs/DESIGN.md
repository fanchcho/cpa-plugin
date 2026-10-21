---
artifact: docs/DESIGN.md
stage: architect
workflow_id: prod-dev
workflow_version: 2
author: architect-director
date: 2026-09-18
amended: "2026-09-18 — ADR-001：修正 §4.3 / §4.5 的 QuotaProvider 传输通道措辞（req.HTTPClient 物理不可达，改以 req.HostCallbackID 的宿主回调上下文为准）"
amended_2: "2026-10-21 — 环境漂移补记：本机宿主二进制已由 7.3.5 升级到 8.0.35（brew `cliproxyapi 8.0.35`），D1 钉版不变；§2.1 与 §10.2 增列 P-HOST 前置，L4 运行时验收须另取 v7.3.5 二进制。同时补产出 docs/TEST_PLAN.md（§10.3 早已登记为「触发且必须产出」）"
prd_ref: docs/PRD.md
root_task_id: t_a8f34caa
source_task_id: t_a8f34caa
supersedes: docs/DESIGN.md @ 2026-09-17（草案，prd_ref: null，approval_required: true）
scope_target: cpa-plugin @ 3a039f9（workbuddy 0.9.3 / qoderwork 0.4.1）适配 CLIProxyAPI v7.3.5
approval_required: false
approval_reason: >
  前稿的 approval_required: true 建立在两个已消解的前提上——「docs/PRD.md 不存在」与「prod board 不存在」。
  两项均已消解：PRD 已落盘并作为本设计的正式上游（prd_ref: docs/PRD.md），prod board 已存在并在调度本卡。
  候选裁决矩阵 D1–D7 已由 admin 在 commander 路由阶段授权采纳，本设计不再新开任何产品级选项，
  只把它们固化为「已定约束」并落到机制层实现。
  本设计对既有代码的改动经契约符号集差实证为**纯增补**（pluginabi/pluginapi 的 type/func/const/结构体字段删除数均为 0），
  不改变数据库、不改变认证模型、不放宽脱敏规则、不新增第三方依赖。
  仅有的非代码前置（P-ENV 本地 Go 工具链、P-REPO GitHub 建仓，以及 2026-10-21 新登记的 P-HOST v7.3.5 宿主二进制、P-STORE 商店源登记）
  是 PRD 8.1 已显式归属 admin 的**执行前置**，不是待拍板的设计决策；
  本设计已把它们转成下游 dev/test 卡上的显式阻断点（见第 10.2 节与 docs/TEST_PLAN.md §0.3），不会被执行方自行假定通过。
---

# cpa-plugin 适配 CLIProxyAPI v7.3.5 — 技术架构设计（收口版）

> 上游依据：`docs/PRD.md`（product-director，2026-09-18，286 行）。范围与验收一律以 PRD 第 3 / 5 / 6 节为唯一判据，本设计不放宽、不替换。
> 本文全部结论来自**本轮实读**：CPA v7.3.5 全量源码 `/tmp/cpasdk/CLIProxyAPI-7.3.5`、v7.2.30 契约源码（`raw.githubusercontent.com` 直取）、
> 本地仓库 `/Users/fanchunchun/hermes_plugin/cpa-plugin @ 3a039f9`、宿主二进制 `/opt/homebrew/bin/cliproxyapi`（本轮实测时为 7.3.5；**2026-10-21 复核已漂移至 8.0.35，见 §10.2 P-HOST**）、
> GitHub API（`Sliverkiss/cpa-plugin` / `fanchcho/cpa-plugin` / 官方商店 registry）。
> 文中不含任何 secret、token、cookie、私钥或账号凭据；`~/.cli-proxy-api/config.yaml` 从未被读取或复制。

---

## 0. 收口说明（相对 2026-09-17 草案的变更）

| # | 草案状态 | 本次收口 |
|---|---|---|
| F1 | `prd_ref: null`（无 PRD） | 回填 `prd_ref: docs/PRD.md`，PRD 为唯一范围与验收依据 |
| F2 | `approval_required: true`，理由含「prod board 不存在」 | 重判为 **false**；理由见 frontmatter 与第 9 节 |
| F3 | 第 0 节「裁决矩阵（需拍板项）」 | 改为 **第 1 节「已定约束（D1–D7，不再重开）」** |
| F4 | 第 5.4 节 tag 命名方案 A/B/C 并列 | 收敛为 **方案 A（拆仓）已定**；B/C 仅作否决记录 |
| F5 | 第 6 节「无 Go 工具链下的验证路径（建议交 test-director）」 | 升级为 **第 8 节契约回归方法 + 第 9 节验收通道与证据边界**（可执行） |
| F6 | 缺「拆仓后 CI / registry 文件布局」与「P-REPO 机械步骤」 | 新增 **第 7 节**（本设计必须给出的可验证结论 6） |
| F7 | 三个新能力的实现约束散落在第 3/4 节 | 拆为 **第 3/4/5/6 节**，逐项给出接口签名、数据结构、并发语义、红线 |
| F8 | 未声明 CodeBuddy Intl 排除 | 新增 **第 10.4 节**：本轮显式排除（PRD D6 / 3.2） |
| F9 | 初稿把 `req.HTTPClient` 写成 `QuotaProvider` 的必用通道（§4.3 / §4.5） | 实测该字段**物理不可达**；由 **ADR-001**（第 14 节）修正 §4.3 / §4.5 与 `docs/API_DESIGN.md` §1.3，使文档与 B3 实现对齐 |

**路径裁决（草案「只升 SDK vs 补齐能力」之争）已随 D2 结束**：PRD D2 = 升 SDK + 补齐能力，重心修 3 个真实缺陷。

---

## 1. 已定约束（D1–D7，本设计不得重开）

| # | 约束 | 机制级依据（本轮实证） |
|---|---|---|
| D1 | 目标版本钉 **v7.3.5** | `sdk/pluginabi/types.go` 与 `sdk/pluginapi/types.go` 在 v7.3.5 / v7.3.6 **字节相同**（6375 B / 65760 B，`diff` 无输出）；7.3.5→7.3.6 变更全在 `internal/*` |
| D2 | **升 SDK + 补齐能力** | 升 SDK 自动带来 schema 1→6 与缺陷 D-A 修复；`quota.*` / `request.complete` / `plugin.quiesce` / `host.affinity.lookup` 各对应一个真实缺失面 |
| D3 | `SchemaVersion` 目标值 **6**，引用常量不得硬编码 | `workbuddy/main.go:344`、`qoderwork/main.go:341` 写的是 `pluginabi.SchemaVersion`；该常量 v7.2.30 = 1、v7.3.0–7.3.6 = 6（`sdk/pluginabi/types.go:20`）。停在 1 需改成字面量 → 主动降级 |
| D4 | 发布 tag 改用**裸 `vX.Y.Z`** | 宿主 `ReleaseVersion()` = `validPluginVersion(normalizeVersion(tag))`，正则 `^[0-9][0-9A-Za-z.+-]*$`（`internal/pluginstore/github.go:112-122`、`registry.go:407-409`）。模拟实跑见第 1.3 节：`workbuddy-v0.9.4` → 失败 |
| D5 | **拆为两个仓库**（workbuddy / qoderwork 各一），各自裸 tag | 裸 tag 在单仓内跨插件命名空间冲突；且 `/releases/latest` 在单仓下无法按插件解析版本。生态惯例一致（官方 registry 74 插件中 `jshandler` tag `v1.0.1`、`credential-tier-router` tag `v0.1.2`） |
| D6 | CodeBuddy Intl **本轮不纳入** | 见第 10.4 节，显式排除 |
| D7 | **workbuddy 先行、qoderwork 跟随** | qoderwork 版本治理更乱（D-C 三处冲突）且当前**零 Go 测试**（实测 `ls qoderwork/*_test.go` = 0；workbuddy = 48） |

---

## 2. 已核验事实基线

### 2.1 仓库 / 环境

| 项 | 实测结果 | 证据 |
|---|---|---|
| HEAD | `3a039f9ddf9a7cc248f231fc5964a9f6fab3805b`，branch `main`，remote `git@github.com:fanchcho/cpa-plugin.git` | `git rev-parse` / `git remote -v` |
| 工作区 | 仅 `docs/DESIGN.md`、`docs/PRD.md` 未跟踪 | `git status --short` |
| SDK 钉版 | 两插件 `go.mod` 均 `CLIProxyAPI/v7 v7.2.30`，`go 1.26.0` | `workbuddy/go.mod`、`qoderwork/go.mod` |
| 插件实际引用的 SDK 包 | `sdk/pluginabi`(6) `sdk/pluginapi`(18) `sdk/cliproxy/usage`(3) `sdk/proxyutil`(1)（workbuddy）；qoderwork 同构但无 `proxyutil` | 非测试源码 import 统计 |
| 宿主二进制 | 实测于 2026-09-18：`CLIProxyAPI Version: 7.3.5, Commit: Homebrew`。**2026-10-21 复核：同路径二进制已升级为 `8.0.35`（`brew list --versions cliproxyapi` = `cliproxyapi 8.0.35`），D1 钉版不变 → 运行时验收需另取 v7.3.5（§10.2 P-HOST）** | `/opt/homebrew/bin/cliproxyapi --version`（2026-09-18）；`brew list --versions cliproxyapi`（2026-10-21） |
| 上游源码副本 | `/tmp/cpasdk/CLIProxyAPI-7.3.5`、`/tmp/cpachk` 已于 2026-10-21 不存在（临时目录被清理）→ 第 2.2 / 4.1 节的行号锚点需按 tag 重新取源复核（`raw.githubusercontent.com/router-for-me/CLIProxyAPI/v7.3.5/...` 或 `go mod download` 后的模块缓存） | 目录探测实测 |
| **工作区漂移（2026-10-21）** | 设计基线 `3a039f9` 之上，工作树现存 **未提交 WIP**：两 `go.mod` 已升至 `v7.3.5`；workbuddy 新增 `quota.go` / `quiesce.go` / `completion.go` / `affinity.go` 及对应测试与 `testdata/register.golden.json`；qoderwork 仅 `version` 常量修正 + `go.mod`。**本设计不背书该 WIP 的正确性**：dev 卡须按第 12 节路线图逐项核对、补 qoderwork（P6）、跑 CI，再进入 review | `git status --porcelain`（31 项）、`git diff --stat` |
| 本地 Go | **不存在**（`go: command not found`；`/opt/homebrew/Cellar/go` 不存在）；Node v26.8.2 存在；python3 存在 | 环境实测 |
| GitHub 凭据 | `gh auth status` → token **invalid**；`ssh -T git@github.com` → “Hi fanchcho! You've successfully authenticated” | 实测 |
| 发布历史 | `Sliverkiss/cpa-plugin` 仅 2 个 release：`workbuddy-v0.8.5`、`qoderwork-v0.2.6`（2026-07-26）；`/releases/latest` = `workbuddy-v0.8.5`；`fanchcho/cpa-plugin` **0 tag / 0 release** | GitHub API |
| 官方商店 | `router-for-me/CLIProxyAPI-Plugins-Store`：`schema_version: 1`，74 插件，**无** workbuddy / qoderwork / codebuddy；全部 `github-release`（0 个 `direct`） | 拉取 registry.json 统计 |
| 转义缺陷 | `managementResponseEscapesHTML(v) = v < 6`（`internal/pluginhost/management.go:357-359`）；schema<6 时对 JSON body 逐字符串 `html.EscapeString`（`internal/htmlsanitize/htmlsanitize.go:12`，判别器 `JSONBodyIfLikely`） | 源码实读 |

### 2.2 契约符号集差（v7.2.30 → v7.3.5）——「会不会编译不过」的完整答案

方法：分别取两版 `sdk/pluginabi/types.go`、`sdk/pluginapi/types.go`，抽取 `type` / `func` / `const` 名称集合与结构体字段集合后做差集。
脚本：`/tmp/cpa_sim/symbol_diff.py`（v7.2.30 源取自 `raw.githubusercontent.com/router-for-me/CLIProxyAPI/v7.2.30/...`）。

| 包 | type 删除 | func 删除 | const 删除 | 共享结构体字段删除 |
|---|---|---|---|---|
| `sdk/pluginabi` | **0** | **0** | **0** | **0** |
| `sdk/pluginapi` | **0** | **0** | **0** | **0** |

纯增补清单：

- `pluginabi` +13 const（`SchemaVersion` 由 1→6，新增 4 个 `SchemaVersion*` 门控常量、`MethodPluginQuiesce`、`MethodRequestComplete`、`MethodWebSocketResponseEvent`、`MethodQuota*`×4、`MethodHostAffinityLookup`），+4 func（`Error`/`NewError`/`NewErrorEnvelope`/`StatusCode`）。
- `pluginapi` +19 type（`QuotaProvider`/`QuotaDescribeRequest`/`QuotaDescribeResponse`/`QuotaFetchRequest`/`QuotaFetchResponse`/`QuotaGroup`/`QuotaBucket`/`QuotaMetric`/`QuotaSubscription`/`QuotaResetRequest`/`QuotaResetResponse`/`RequestCompletion`/`RequestCompletionOutcome`/`RequestLifecyclePlugin`/`WebSocketResponseEvent`/`WebSocketResponseObserver`/`HTTPWireProfile`/`HostAffinityLookupRequest`/`HostAffinityLookupResponse`），+8 const，+`UnmarshalJSON` 若干。
- 共享结构体新增字段（对**具名字段字面量**构造者安全）：`Capabilities` += `QuotaProvider` / `RequestLifecyclePlugin` / `WebSocketResponseObserver`；`Plugin` += `SchemaVersion`；`RequestInterceptRequest` += `RequestID`/`TraceID`；`ResponseInterceptRequest` += `RequestID`；`StreamChunkInterceptRequest` += `RequestID`；`UsageRecord` += `BaseURL`/`Generate`/`SessionID`/`ParentSessionID`；`HostAuthFileEntry` += `BaseURL`；`HTTPRequest` += `WireProfile`。

**结论：编译层面零破坏。** 两插件源码对 SDK 结构体一律用具名字段字面量（实读 `registration{...}`、`pluginapi.Metadata{...}`、`usage.Detail{...}` 等），不存位置字面量风险。风险全部在**运行时协商**与**发布链路**。

### 2.3 三个版本门控对本品的实际影响

| 门控 | 条件 | 本品影响 |
|---|---|---|
| payload chunk 省略 `OriginalRequest`/`RequestBody` | schema ≥ 3 | **本次无影响**（两插件零注册 `StreamChunkInterceptor`，实测 grep 全量非测试源码）。**构成未来约束** |
| payload chunk 省略 `HistoryChunks` | schema ≥ 5 | **本次无影响**，同理构成未来约束（`StreamChunkHeaderInitIndex = -1`） |
| 管理响应不再 HTML 转义 | schema ≥ 6 | **修复用户可见缺陷 D-A**（面板出现 `&amp;` 等实体串） |

### 2.4 纯函数模拟（无 Go 工具链下对宿主判定的证伪）

脚本 `/tmp/cpa_sim/sim_host.py` 逐字复刻宿主判据并对真实取值实跑：

```
ReleaseVersion(tag)  [version.go:26-32 + github.go:112-122 + registry.go:407-409]
  workbuddy-v0.9.4  -> INVALID     qoderwork-v0.4.1 -> INVALID
  workbuddy-v0.8.5  -> INVALID  ← 上游 /releases/latest 实际返回的 tag
  qoderwork-v0.2.6  -> INVALID
  v0.9.4            -> 0.9.4 ✓    0.9.4 -> 0.9.4 ✓
  v0.4.1            -> 0.4.1 ✓    workbuddy-0.9.3 -> INVALID

pluginStoreReleaseTagCandidates("0.9.4")  [plugin_store.go:471-480] -> ["0.9.4", "v0.9.4"]
Manifest.Validate release-tag vs version   [manifest.go:128-143]
  tag=v0.9.4 / version=0.9.4 -> valid       tag=workbuddy-v0.9.4 -> INVALID
ArchiveName("workbuddy","0.9.4","linux","amd64") == CI 产物名 -> MATCH
managementResponseEscapesHTML: schema 1 -> true, 5 -> true, 6 -> false
UpdateAvailable("0.9.3","0.9.4") -> true；("0.9.4","0.9.3") -> false
```

**这证明 D4 是 P0 阻塞而非命名口味**：三条版本解析路径（fetch-latest、install-by-version、manifest 校验）在 `workbuddy-v*` 约定下**同时失败**，且都停在解析步，`AC-6` 的端到端安装在此之前无法执行。

---

## 3. SDK 升级影响面与 diff 边界（结论 1）

### 3.1 改动点（两插件同构）

```
workbuddy/go.mod : github.com/router-for-me/CLIProxyAPI/v7 v7.2.30 -> v7.3.5
qoderwork/go.mod : github.com/router-for-me/CLIProxyAPI/v7 v7.2.30 -> v7.3.5
（go 1.26.0 两处均保持不变；v7.3.5 自身 declare go 1.26.0）
```

### 3.2 传递依赖范围（实测 v7.2.30/v7.3.5 的 `go.mod` 对照）

| 依赖 | v7.2.30 | v7.3.5 | 是否进入插件模块图 |
|---|---|---|---|
| `github.com/sirupsen/logrus` | v1.9.3 | **v1.9.3（不变）** | 是（`sdk/cliproxy/usage` 直接 import） |
| `golang.org/x/net` | v0.47.0 | **v0.57.0** | 是（`sdk/proxyutil` import `x/net/proxy`；workbuddy 引用 proxyutil） |
| `golang.org/x/sys` | v0.38.0 | **v0.47.0** | 是（x/net 传递） |
| `golang.org/x/crypto` | v0.45.0 | v0.54.0 | 仅模块图（x/net 声明），插件不使用 |
| `golang.org/x/text` | v0.31.0 | v0.40.0 | 同上 |
| `golang.org/x/sync` | v0.18.0 | v0.22.0 | 同上 |
| `golang.org/x/oauth2` | v0.30.0 | v0.30.0（不变） | 插件不使用 |
| `gopkg.in/yaml.v3` | v3.0.1 | v3.0.1（不变） | 插件直接依赖（workbuddy） |
| `github.com/google/uuid` | v1.6.0 | v1.6.0（不变） | 插件直接依赖（qoderwork） |

**API 面变化核实**：`x/net v0.57.0` 的 `go.mod` 要求 `go 1.25.0`（低于插件 `go 1.26.0`，安全）；`golang.org/x/net/proxy` 的 `Dialer`/`Auth` 导出形状在 v0.47.0 与 v0.57.0 一致（两版 `proxy.go` 直读比对），`sdk/proxyutil` 对插件的可用面不变。

### 3.3 允许的 `go.mod` / `go.sum` diff 边界（CI 复审判据）

- `go.mod`：**只允许** `require` 中 `CLIProxyAPI/v7` 行变更为 `v7.3.5`，以及 indirect 块中 `golang.org/x/net` → `v0.57.0`、`golang.org/x/sys` → `v0.47.0`。出现其他模块版本变动、或 `go` 指令变动 → 视为超界，按 R1 复审。
- `go.sum`：允许新增/替换上表所列模块的 `h1:`/`go.mod` 行（预期新增 `x/net v0.57.0`、`x/sys v0.47.0`、`x/crypto v0.54.0`、`x/text v0.40.0`、`x/term v0.45.0`、`x/sync v0.22.0` 及各自 `/go.mod` 行）。
- `qoderwork/go.sum` 不含 `x/net`（当前无该行，实测），升级后若因 `proxyutil` 不在其 import 集而不引入 x/net，则其 diff 仅 x/sys 一行 —— **以 CI 实跑为准**，不得凭预期改写。

### 3.4 编译期兼容性（已实证）

见第 2.2 节：删除数全 0、具名字段字面量 → 升版后可直接编译。
`usage.Detail` 新增 `TokenBreakdown`/`ResponseServiceTier` 等字段同理安全。

---

## 4. `QuotaProvider` 接口与数据映射（结论 2）

### 4.1 接口签名（逐字取自 `sdk/pluginapi/types.go:1474-1484`）

```go
type QuotaProvider interface {
	Identifier() string
	DescribeQuota(context.Context, QuotaDescribeRequest) (QuotaDescribeResponse, error)
	FetchQuota(context.Context, QuotaFetchRequest) (QuotaFetchResponse, error)
	ResetQuota(context.Context, QuotaResetRequest) (QuotaResetResponse, error)
}
```

### 4.2 注册侧改动（两插件同构，模式与既有 capability 一致）

1. `registrationCapability` 增加字段 `QuotaProvider bool \`json:"quota_provider"\``（workbuddy/main.go:326-337、qoderwork/main.go:323-334）。
2. `wbRegistration()` 中置 `QuotaProvider: true`。
3. `handleMethod` 增加 4 个 case：`pluginabi.MethodQuotaIdentifier` / `MethodQuotaDescribe` / `MethodQuotaFetch` / `MethodQuotaReset`（宿主按方法名直调，**不是** capability 协商后的间接调用，见 `internal/pluginhost/rpc_client.go:650-672`）。
4. 宿主注册期仅校验 `capabilities.quota_provider == true` 并回调 `quota.identifier` 取标识（`rpc_client.go:167-177`），随后按标识路由。

### 4.3 方法实现规约

| 方法 | 实现 | 硬约束 |
|---|---|---|
| `Identifier()` | 返回既有常量 `providerName`（`workbuddy` / `qoderwork`） | **不得新造字符串**。宿主匹配顺序为 `QuotaProvider.Identifier()` → 插件 ID → `AuthProvider.Identifier()`，再以 `SupportedProviders` 兜底（`internal/pluginhost/quota_provider.go:157-190`）；新造标识会导致 auth 对不上 |
| `DescribeQuota()` | `{DisplayName: <面板名>, SupportedProviders: [providerName], SupportsReset: false}` | `SupportsReset: false` 与 `ResetQuota` 恒失败一致，避免管理端展示可用的重置按钮 |
| `FetchQuota()` | 复用既有 credits 抓取路径，归一化为 `QuotaFetchResponse` | 必须走**宿主为本次调用开启的回调上下文**：`hostHTTPDoWithCallback(req, req.HostCallbackID)`；**不得自铸 callback id**。原「必须使用宿主注入的 `req.HTTPClient`」措辞已由 **ADR-001** 作废（该字段 `json:"-"` 且被 `sanitizePluginRequest` 置 nil，出进程插件模型下不可达） |
| `ResetQuota()` | 恒返回 `{Success: false, Message: "provider-managed"}` | **绝不触发上游写操作** |

### 4.4 字段映射（PRD 5.2 的机制化落地）

目标类型逐字取自 `sdk/pluginapi/types.go:1551-1625`：`QuotaFetchResponse{Subscription *QuotaSubscription; Summary []QuotaMetric; ServerTimeOffsetMs int64; Groups []QuotaGroup}`，`QuotaGroup{DisplayName, Buckets []QuotaBucket}`，`QuotaBucket{Window, RemainingFraction float64, ResetTime, Description}`，`QuotaMetric{Key, Label, Value float64, Unit, Format, Currency}`。

| 现有（`creditsSummary`，workbuddy/management.go:37-51；qoderwork/management.go:29+） | 目标字段 |
|---|---|
| `TotalRemain` / `TotalSize` | `Groups[0].Buckets[0].RemainingFraction = TotalRemain / TotalSize` |
| `size <= 0` | **除零保护**：取 `1.0`，或整条 bucket 省略（二者择一后写入单测断言，不得留 NaN/Inf） |
| `Plan` | `Subscription.Plan` |
| `Packages[].CycleEnd` | `Buckets[].ResetTime`（RFC3339 字符串，宿主不解析） |
| `TotalRemain` / `TotalUsed` | `Summary` 两条 `QuotaMetric`，`Unit: "credits"`（`Key`/`Label` 稳定不随语言变化） |

`QuotaBucket`/`QuotaGroup`/`QuotaSubscription`/`QuotaFetchResponse` 均带 `UnmarshalJSON` 兼容 camelCase 与 snake_case，插件按 camelCase 输出即可（`types.go:1529-1643`）。

### 4.5 三条硬约束（PRD 5.2，逐条落到机制）

1. **走宿主回调上下文（唯一可传输的出站通道）**。宿主在 `callQuotaFetch` 中按 `AuthIndex` 反查 auth，回填 `AuthID`/`Provider`/`StorageJSON`/`Metadata`/`Attributes`（`internal/pluginhost/quota_provider.go:349-378`）；同一调用链上的 RPC 适配器 `rpcQuotaProvider.FetchQuota` 会**开启回调上下文并把 `HostCallbackID` 放进本次 wire 结构**（`internal/pluginhost/rpc_client.go:658-664`、`rpc_schema.go:135-138`）。插件回传该 id 后，宿主 `callHostHTTPDo` 用 `resolveCallbackContext(callbackID, ctx)` 还原上下文再发请求（`internal/pluginhost/host_callbacks.go:146-153`）。自铸 callback id 会与宿主对回调嵌套的刻意规避（`CompleteRequestExcept` / `skipPluginID`）对抗。
   - **实现注记（修正）**：`req.HTTPClient` **物理不可达** —— 该字段带 `json:"-"`（`sdk/pluginapi/types.go:1518-1519`），且 `sanitizePluginRequest` 对 `pluginapi.QuotaFetchRequest` 与 `rpcQuotaFetchRequest` 两个 case 都执行 `req.HTTPClient = nil`（`internal/pluginhost/rpc_client.go:274-281`）。因此 `FetchQuota` 分支**必须**走 `callbackID` 路径（`req.HostCallbackID` 在宿主侧恒非空），与现有 credits 抓取 `billingCallWithCallback(sa, path, body, callbackID)` **完全同构**，两条路径收敛到同一缓存（45s TTL + singleflight）与 `userResourceSlots` 限流语义。完整依据与残余差异见 **ADR-001**（第 14 节）。
2. **保留** `/v0/management/<plugin>/credits`（两插件 `managementRegistration()` 中已注册，workbuddy/management.go:175）。`QuotaProvider` 是**并行新增通道**：面板依赖 `/accounts`、`/credits` 的 packages 明细、checkin、region 等富字段，`QuotaFetchResponse` 承载不了。
3. **复用 credits 缓存与失效路径**（`invalidateAccountCredits`，workbuddy/lifecycle.go:485）。配额查询不得绕开 TTL 直接刷上游。

### 4.6 宿主侧可观测入口（验收锚点）

`GET /v0/management/quota/providers`（`internal/api/server_management.go:83` → `plugin_quota.go:43-56`，返回 `{"providers":[RegisteredQuotaProviderInfo]}`，字段 `plugin_id`/`provider`/`display_name`/`supported_providers`/`supports_reset`）；
`POST /v0/management/quota/fetch`（`server_management.go:84`，body 需 `auth_index`）。

---

## 5. `request.complete` 幂等去重闸门（结论 3）

### 5.1 宿主行为（实读，决定闸门语义）

- 接口：`RequestLifecyclePlugin{ HandleRequestComplete(context.Context, RequestCompletion) error }`（`sdk/pluginapi/types.go:991-993`）。
- 触发：`requestLifecycleTracker` 在**每个进入执行链的请求**上创建，`once.Do` 保证**每请求恰好一次**终态回调（`sdk/api/handlers/handlers_interceptors.go:117-186`）。`RequestID` 由宿主 `uuid.NewString()` 生成（同处 119 行），与 `TraceID`（`logging.GetRequestID(ctx)`）不同。
- 终态枚举：`succeeded` / `failed` / `rejected` / `canceled`（`types.go:1088-1099`）；`canceled` 即「请求 context 被取消或下游客户端断开」，这正是插件执行器**观测不到**的终态。
- 投递语义：`CompleteRequestExcept` 用 `context.WithoutCancel(ctx)` 起新 goroutine 异步调用，`Metadata` 被深拷贝（`internal/pluginhost/adapters_interceptors.go:190-224`）。**故插件侧回调不得假设自己运行在请求上下文里**；同时 `Record.Metadata` 的键集属宿主内部实现（含 `canonical_session_id`、`parent_session_id`、`requested_model` 等，`sdk/cliproxy/executor/types.go:11-60`），**不逐键承诺**。

### 5.2 闸门数据结构与并发语义（设计）

```go
// 每插件一份（workbuddy / qoderwork 各自实例）
type completionGate struct {
	mu    sync.Mutex
	seen  map[string]time.Time // RequestID -> 首次受理时刻
	order []string             // FIFO 淘汰次序（有界）
	size  int                  // 容量上界，= 4096
	ttl   time.Duration        // = 30 * time.Minute
}

// 返回 true = 首次受理（调用方应记账）；false = 重复，直接丢弃
func (g *completionGate) accept(requestID string, now time.Time) bool
```

并发语义（写进单测断言）：

1. **单飞**：`accept` 在 `mu` 内完成「查表 → 命中即 false → 未命中则插入」。同一 `RequestID` 的 N 个并发调用**恰好一个返回 true**。
2. **键的合法性**：`strings.TrimSpace(RequestID) == ""` → 返回 false（不记账也不入表）。空键不构成幂等键，宁可漏记不可双记。
3. **TTL 与容量**：`accept` 时懒清理（`now.Sub(t) > ttl` 的条目删除）；`seen` 超过 `size` 时按 `order` FIFO 淘汰最旧条目。**有界内存**是硬要求——宿主每个请求都会回调，无界 map 是内存泄漏。
4. **不依赖 `Metadata` 键名**：闸门只读 `RequestCompletion.RequestID`；`Outcome`、`StatusCode`、`Error`、`StartedAt`/`CompletedAt` 可用于记账内容，但**任何键名匹配逻辑都不允许进入去重路径**（键集未逐键实证，见 PRD R3）。
5. **与既有 usage 上报的关系**：现有 `publishUsage`（workbuddy/usage.go:68，12 处调用点）在插件执行器内发；`HandleRequestComplete` 只补**执行器观测不到的终态**（尤其 `canceled`），并由闸门防止二次记账。执行器已记账的请求，其 `RequestID` 不在闸门表内 → 需在**执行器侧记账时同步登记闸门**（同一 `RequestID` 双写一条 `accept` 结果），使两条路径共用同一键空间。这是本设计对 AC-8「不产生第二条记录」的实现方式。

### 5.3 注册侧改动

`registrationCapability` 增 `RequestLifecyclePlugin bool \`json:"request_lifecycle_plugin"\``；`wbRegistration()` 置 true；`handleMethod` 增 `case pluginabi.MethodRequestComplete: return handleRequestComplete(request)`（返回空结果 envelope，宿主不解析结果体，`rpcEmptyResponse`）。

---

## 6. `plugin.quiesce` 行为设计（结论 4）

### 6.1 宿主调用时机与错误分类（实读）

- `internal/pluginhost/host.go:285`：检测到同 ID 插件文件被**替换**时，先 `h.callQuiesce(ctx, replaced)`，再加载新版本。
- `quiesceUnsupported(err)`（host.go:877-900）把 `unknown_method` / `method_not_found` / `unsupported_method` 归类为「不支持」→ **仅 Debug 日志**；其他错误 → **Warn**。当前两插件未实现该方法，落在 Debug 分支。
- 因此 AC-5 的判据「宿主日志不出现 `pluginhost: plugin quiesce unsupported`」等价于：**插件必须实现 `plugin.quiesce` 并返回 OK envelope**。

### 6.2 需停止的定时器清单（逐项对源码）

| 定时器 | 位置 | 停止动作 |
|---|---|---|
| checkin 调度循环（09:00 / 21:00） | `workbuddy/checkin.go:16-31`（`schedulerStop chan struct{}` + `schedulerLoop`），`checkinHours = []int{9, 21}`（`usage_config.go:22`） | **close `schedulerStop`**，使 `schedulerLoop` 的 `select` 命中 `<-stop` 并 `timer.Stop()` 后 return（checkin.go:66-79）。同构于 qoderwork/checkin.go |
| keepalive（22:00） | 与上同一调度循环（`keepaliveHours = []int{22}`，`keepalive.go:35`；`nextCheckinTime` 合并两组小时） | 同一次 close 覆盖；**无需独立 timer** |
| `loginStatesPruneInterval` janitor（每 1 分钟） | `workbuddy/main.go:122-140`（`init()` 内 `time.NewTicker` + `for range ticker.C`），`qoderwork/main.go:125-138` | 需要一个 stop 通道：`select { case <-ticker.C: ...; case <-janitorStop: ticker.Stop(); return }`。当前实现是 `for range ticker.C` 无出口，**必须改造** |
| 生命周期 reconcile 的 detached goroutine | `workbuddy/lifecycle.go:410`（`go func()`） | 属请求驱动的短任务，不新增停止机制；由 draining 标志阻止**新**任务启动 |

实现要求：`pluginQuiesce()` 必须**幂等**（重复调用不 panic），且在返回前确保三个 stop 通道只被 close 一次（`sync.Once` 或 `schedulerMu` 下判空置 nil）。

### 6.3 需原子落盘的文件清单

| 状态 | 路径（实读） | 落盘方式 |
|---|---|---|
| 模型目录元数据 | `filepath.Join(os.UserConfigDir(), "CLIProxyAPI", "workbuddy", "model-catalog", "metadata.json")`（model_store.go:87-93,100,111） | 复用既有 `writeModelCacheAtomic`（model_store.go:266-313）：同目录 `os.CreateTemp(dir, "."+base+".tmp-")` + fsync + rename，并在 rename 后 `syncModelCacheDirectory` |
| 单模型缓存 | 同目录 `models/<identitySHA256>.json`（model_store.go:121,135） | 同上 |
| keepalive 状态 | 现存于内存（`lastKeepalive`，keepalive.go:230-243）—— **无落盘路径** | 本设计**不新增**落盘文件：`plugin.quiesce` 的落盘义务限定为「已存在落盘路径的状态」，keepalive 摘要属易失观测数据，落盘会是本轮无依据的范围扩张 |
| qoderwork 侧 | 实测**无** `writeModelCacheAtomic` / model store 落盘路径（`grep` 零命中） | qoderwork 的 quiesce 落盘义务为**空集**，如实声明；不得为对称而新造状态文件 |

**红线（PRD 5.4 / 任务 4）**：清理逻辑**只能**放 `plugin.quiesce`；`cliproxyPluginShutdown` 保持 **no-op**（workbuddy/main.go:191-199、qoderwork/main.go:194-202 的既有注释已说明原因：宿主随后 `dlclose`，碰 Go runtime 会 SIGSEGV）。同时须删除/改写两处 `checkin.go` 中「there is deliberately no stopCheckinScheduler」的注释，指明停止入口已迁移至 quiesce。

### 6.4 draining 语义与错误码

- 插件内新增原子标志 `draining atomic.Bool`（`pluginQuiesce` 中置 true）。
- 置位后：`executor.execute` / `executor.execute_stream` / `sdk_plan` 的管理写路由快速失败，返回错误 envelope：

```json
{"ok": false, "error": {"code": "plugin_draining",
  "message": "{\"error\":{\"message\":\"plugin is draining after hot-reload\",\"type\":\"server_error\",\"code\":\"plugin_unavailable\",\"retryable\":true}}",
  "retryable": true, "http_status": 503}}
```

- **两条必须同时满足**（这是 AC-5「error 体带 retryable: true + http_status: 503」的可验证落地）：
  1. `envelopeError` 结构体当前**缺 `Retryable` 字段**（workbuddy/main.go:305-309），必须补齐 `Retryable bool \`json:"retryable,omitempty"\``（对齐 `pluginabi.Error`）。
  2. `http_status: 503` 由宿主 `rpcError.statusCode` → `clienterror.HTTPStatusFromError` → `interfaces.ErrorMessage.StatusCode` → `WriteErrorResponse` 传导为下游 HTTP 503（`internal/pluginhost/rpc_client.go:44-56,355-372`、`internal/clienterror/client_error.go:HTTPStatusFromError`、`sdk/api/handlers/handlers_errors.go:116-155`）。
- **证据边界（本轮实证，必须写入验收口径）**：宿主 `rpcError` 只保留 `Code`/`message`/`statusCode`，**丢弃 `Retryable`**；下游 JSON 体的 `retryable` 字段仅在 `BuildErrorResponseBodyWithError` 的 terminal-auth 分支出现（`handlers.go:71-110`）。因此要拿到字面 `"retryable": true`，`message` 本身必须是合法 JSON 对象（`handlers.go:113-115`：`json.Valid(trimmed) → 原样返回`）。上例即按此设计。该结论由 `test-director` 按「先断言插件 envelope，再断言下游响应体」两级取证。

### 6.5 验收锚点（运行时）

条件：宿主与插件的管理路由开启、`plugin.quiesce` 已实现。
判据：① 宿主日志无 `plugin quiesce unsupported`（Debug）且无 `plugin quiesce failed`（Warn）；② 热替换后一个调度窗口内无 checkin/keepalive 上游请求；③ 状态目录无 `.tmp-` 残留；④ 新 executor 请求得到 HTTP 503 且 body 含 `retryable: true`。

---

## 7. `host.affinity.lookup` 只读接入点（结论 5）

### 7.1 契约（逐字取自 `sdk/pluginapi/types.go:786-815`）

```go
HostAffinityLookupRequest{ Provider, Model, SessionID string }   // 三者必填（host 侧校验，空则报错）
HostAffinityLookupResponse{ Status string; AuthIndex string; ObservedAt time.Time; Disabled, Unavailable bool }
// Status ∈ {bound, unbound, ambiguous, unsupported}
```

调用方式：`hostCall(pluginabi.MethodHostAffinityLookup, mustJSON(req))`，与既有 `hostCall(pluginabi.MethodHostAuthList, ...)`（workbuddy/host_auth.go:34）同机制。
宿主实现：`internal/pluginhost/affinity_callbacks.go:14-61` → `manager.LookupSessionAffinity(provider, model, sessionID)`；`manager == nil` 时返回 `unsupported`。`bound` 时 `EnsureIndex()` 后返回 `AuthIndex` 及 `Disabled`/`Unavailable`。

### 7.2 面板展示点

- 位置：账号卡片内的**惰性字段**（workbuddy/panel.html `card(a)`，第 387-416 行；`data-auth="${a.auth_index}"` 已存在，可直接用作行标识）。
- 取值来源：面板从 `/accounts` 拿到 `auth_index` 列表后，为**当前会话**请求一次 affinity 查询；会话标识取管理请求可携带的值（无则整段跳过）。
- 展示内容：`Status`（bound → 显示绑定账号昵称/索引；unbound/ambiguous → 文案提示；unsupported → 不展示）。
- **红线**：只读。不得以任何形式改写宿主亲和性状态（那是宿主 `AuthManager`/`Scheduler` 的状态，越权会与 Scheduler 能力冲突）。

### 7.3 失败静默降级（VC-3 的机制化）

1. 任何非 `bound` 结果、任何 hostCall 错误、`manager == nil` → **不渲染该字段**，不出现红色错误块、不出现 toast。
2. 不得因 affinity 查询失败影响 `load()` 主流程（错误在 affinity 分支内被吞掉并仅在 debug 日志留痕）。
3. 不得为 affinity 新增**阻塞式**请求：查询在卡片渲染后异步补齐（与既有 `refreshCardCredits` 的卡片内刷新模式一致，panel.html:437-460）。
4. 未提供会话标识时整段跳过（不发请求）。

---

## 8. 拆仓后的 CI / registry 文件布局与 P-REPO 机械步骤（结论 6）

### 8.1 目标仓库布局（每仓库自包含）

```text
<plugin-repo>/                      # workbuddy 仓 / qoderwork 仓，各一
├── go.mod                          # module 名改为各自仓库路径（见 8.3 的 module 决定）
├── go.sum
├── VERSION                         # 版本单一真源（D-C / AC-7）
├── Makefile                        # 沿用现 Makefile（build/test/lint/clean/release/tag）
├── README.md / README_CN.md / LICENSE / CHANGELOG.md
├── .github/workflows/build.yml     # 单插件版（见 8.2）
├── .github/scripts/package-release.go   # 原样平移（无插件耦合）
├── registry.json                   # 单插件 registry（本仓发布单元的一部分）
└── <plugin 源码 + 测试>            # 现 workbuddy/ 或 qoderwork/ 内容整体上移为仓库根
```

- **直接搬移而非复制**：`workbuddy/` 内容整体成为 workbuddy 仓根；`qoderwork/` 同理。这样 `VERSION`、`Makefile`、源码相对路径不变，`version_test.go` 的 `os.ReadFile("VERSION")` 继续成立。
- 现根目录的共享资产分流：**保留在 workbuddy 仓**的是 `.github/scripts/package-release.go`（被两仓各复制一份）；**不带走**的是 `models_list.json`、`test_global_*.py`、`analysis/`、`docs/`（历史分析文档）。
- `registry.json`：每仓只保留自己的那一个插件条目（去掉另一插件），`repository` / `homepage` 改指本仓 URL（`registry.json:10,28` 当前指向 `Sliverkiss/cpa-plugin` 是 P0-2 缺陷）。

### 8.2 workflow 改动（相对现 `.github/workflows/build.yml`）

| 现文件 | 拆仓后 |
|---|---|
| `on.push.tags: ["workbuddy-v*", "qoderwork-v*"]`（第 17 行） | `on.push.tags: ["v*"]` |
| `test` job 的 `matrix.dir: [workbuddy, qoderwork]`（第 35-45 行） | 单 dir（`.`），并**新增** `node --test panel.test.js`（AC-1 要求；workbuddy 仓；qoderwork 仓待 AC-7 补齐 panel.test.js 后再开） |
| `build` / `build-cross` 的 `matrix.id` 双插件（第 64-119、199-212 行） | 单插件（`id` 固定为本仓插件） |
| 版本解析 `if [[ "${GITHUB_REF_NAME}" == ${PLUGIN}-v* ]]`（第 137-138、228-229 行） | `VERSION="${GITHUB_REF_NAME#v}"`；dispatch 输入取消 `plugin` 项，仅保留 `version` |
| `release.tag=${P}-v${V}`（第 315 行） | `tag=v${V}` |
| release 触发条件 `startsWith(refs/tags/workbuddy-v)`（第 288-291 行） | `startsWith(github.ref, 'refs/tags/v')` |
| release notes 中 `raw.githubusercontent.com/Sliverkiss/cpa-plugin/main/registry.json`（第 403 行） | 改指本仓 raw URL |
| registry 同步脚本（第 343-359 行） | 保留（单插件 registry 下仍需写 `version`） |

**保持不变（PRD 5.1 已实测匹配，不得改动）**：归档名 `<id>_<version>_<goos>_<goarch>.zip`、zip 根条目 `<id>.<ext>`、`checksums.txt` 精确名与两列格式、六平台 + 两 cross 目标、`scripts/validate-registry.py` 校验规则（已核对其正则与宿主 `ValidateRegistry` 一致：`ID_RE ^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`、`VER_RE ^[0-9][0-9A-Za-z.+-]*$`）。

### 8.3 module 路径决定（本设计拍定，避免两个仓各选一种）

- `workbuddy` 仓 module：`github.com/fanchcho/workbuddy-cpa-plugin`。
- `qoderwork` 仓 module：`github.com/fanchcho/qoderwork-cpa-plugin`。

理由：现 `workbuddy/go.mod` 的 module 是 `github.com/sliverkiss/workbuddy-plus`、`qoderwork/go.mod` 是 `github.com/Sliverkiss/cpa-plugin/qoderwork` —— 两者都指向**并不发布本插件的**上游命名空间（P0-2 同源缺陷）。拆仓后 module 必须随仓库实际归属走。`release` / `fanchcho` 由本设计指定为唯一发布方（fork 实测 0 tag / 0 release，admin 需在 P-REPO 确认此归属）。

### 8.4 P-REPO 机械执行步骤（admin 侧；dev 不得代做）

当前实测约束：`gh` token **invalid**；`ssh -T git@github.com` 以 `fanchcho` **成功**。故走 SSH 路径：

```bash
# 1) 建仓（二选一：Web UI 或已授权的 API token）
#    https://github.com/new  →  fanchcho/workbuddy-cpa-plugin   (Public, 不初始化 README)
#    https://github.com/new  →  fanchcho/qoderwork-cpa-plugin   (Public, 不初始化 README)

# 2) 在两个插件的自包含发布单元就绪后（dev 交付物），各自推送：
cd <workbuddy 发布单元> && git init -b main
git remote add origin git@github.com:fanchcho/workbuddy-cpa-plugin.git
git add -A && git commit -m "chore: extract workbuddy as standalone CPA plugin repo"
git push -u origin main

# 3) 打裸 tag 并推送（D4）
git tag -a v0.9.4 -m "workbuddy v0.9.4" && git push origin v0.9.4

# 4) 验证 release 与资产名（AC-6 前置）
gh release view v0.9.4 --repo fanchcho/workbuddy-cpa-plugin
#    期望：7 个 <id>_<version>_<goos>_<goarch>.zip + checksums.txt

# 5) 验证宿主可解析该 tag（第 2.4 节模拟的等价运行时检查）
curl -s "https://api.github.com/repos/fanchcho/workbuddy-cpa-plugin/releases/latest" | grep '"tag_name"'
#    期望：tag_name = "v0.9.4"（不得为 workbuddy-v0.9.4）

# 6) 商店源登记（P-STORE）：管理端登记本仓 registry.json 的 raw URL
#    https://raw.githubusercontent.com/fanchcho/workbuddy-cpa-plugin/main/registry.json
```

**阻断约定**：dev-director 到达步骤 1 时，若仓库尚未创建（`git ls-remote git@github.com:fanchcho/workbuddy-cpa-plugin.git` 返回空/报错），**必须** `kanban_block(kind="capability", reason="P-REPO 未就绪：需要 admin 在 GitHub 侧创建两个仓库")`，不得自行使用其他命名空间、不得把发布退化为单仓（R2）。

---

## 9. 契约回归方法（结论 7）

**目标**：证明 SDK 升级造成的 `plugin.register` 响应变化**只有两类**（`schema_version` 1→6、`capabilities` 新增字段）。

**采集方式**（在 CI 或本地，依赖 P-ENV）：

1. 在 `go.mod` 仍为 `v7.2.30` 时，运行新增的 golden 采集测试（每插件一份），把 `wbRegistration()` 的 JSON 序列化写入 `testdata/register.golden.json`：
   `go test -run TestRegisterGoldenWrite -update ./...`（默认不带 `-update` 时只做断言）。**测试名与 `-update` 旗标名属实现细节**，现行工作树 WIP 用的是 `TestRegisterGoldenMatchesContract` + `-update-register-golden`，两者等价，不构成约束。
2. 升级 `go.mod` 到 `v7.3.5`，重跑同一测试，逐键 diff `testdata/register.golden.json`。
3. 断言脚本（测试内实现，避免外部工具）：
   - 顶层键集不变（`schema_version` / `metadata` / `capabilities`）；
   - `schema_version` 由 `1` 变为 `pluginabi.SchemaVersion` 的运行值，且该值 == `6`（用常量比较，不写字面量断言值之外的魔法数）；
   - `capabilities` 的**真值集合**只允许「新增的键为 true、既有键的布尔值不变」；
   - `metadata` 全等（除 `version` 字段随 VERSION 文件，测试中冻结为常量或忽略该键）。
4. **不需要**宿主参与：`plugin.register` 的响应体完全由插件构造（`wbRegistration()`），因此这是可离线执行、可 CI 化的确定性回归。宿主侧协商（schema ≥ 6 的实际生效）由第 2.3 节门控 + AC-3 运行时证据间接覆盖。

**预期 diff（基线冻结）**：`schema_version: 1 → 6`；`capabilities` 新增 `quota_provider: true`、`request_lifecycle_plugin: true`（本设计新增项）。`websocket_response_observer` 保持不出现（PRD N1 非目标）。任何其他变化 → 回归失败。

---

## 10. 审批门判定与下游落地

### 10.1 判定：`approval_required: false`

**正面回应「为何不再需要董事长拍板」**：

1. **不存在待决定的产品选项**。D1–D7 已由 admin 在 commander 路由阶段授权采纳（PRD 第 2 节逐条引用）；本设计把它们固化为约束，新增的裁决文书中没有一条属于「产品取舍」。
2. **技术风险经实证收敛**。契约符号集差为「零删除」（第 2.2 节），编译不破坏；`go.mod` diff 边界已列明（第 3.3 节）；tag 解析失败已用宿主自身判据证伪并给出修复（第 2.4 节）。
3. **不触碰需要董事会级知情的高风险面**：无数据库、无 schema 迁移、无认证模型变更、不放宽脱敏、不新增第三方依赖、不引入新 realm（D6 排除）。
4. **原 `approval_reason` 的两个前提均已消解**：`docs/PRD.md` 已落盘；`prod` board 已存在并在调度（本卡 `status: running`，`board: prod`）。

**若有分歧**：本文件即设计终稿的完整内容，admin 可原样进入评审；我不假定通过任何**新增**决策。

### 10.2 P-ENV / P-REPO 在下游的落地（PRD 8.1）

| 前置 | 归属 | 下游落地方式 |
|---|---|---|
| **P-ENV** 本地 Go 工具链 | admin（`brew install go`） | dev 卡显式步骤：开工第一步检测 `command -v go`；缺失则 `kanban_block(kind="capability", reason="P-ENV 未就绪：需 admin 授权 brew install go（本机实测无 go，brew 可用）")`。**不阻塞 AC-1**：CI 是编译/单测/多平台产物的权威通道，dev 可先推分支让 CI 跑绿；仅 AC-2(b)/AC-3/AC-5/AC-8 的本地运行时验证需要 P-ENV |
| **P-REPO** 两个新仓库 | admin（GitHub 建仓） | 见第 8.4 节机械步骤 + 阻断约定。dev 在完成「自包含发布单元」后即到该步，未就绪则 block，不自行改造仓库结构 |
| **P-STORE** 商店源登记 | 执行时人工（管理端） | AC-6 的后半段；dev 交付 registry.json raw URL 与登记步骤，由 admin/操作者在管理端登记后执行端到端 |
| **P-HOST** v7.3.5 宿主二进制（2026-10-21 新增） | admin / 执行方取官方 release | 本机宿主已漂移至 8.0.35，与 D1 钉版不同代（8.x 用 `/v8/management/` 前缀）。L4 运行时验收第一步断言 `<host> --version` = `7.3.5`；取不到则 AC-2c/3/4/5/8 的运行时部分 `block(kind=capability)`，**不得**用 8.0.35 代跑并宣告通过（详见 `docs/TEST_PLAN.md` §0.3） |

### 10.3 条件文件触发判定（DATABASE 不触发；API / VISUAL / TEST_PLAN 已产出）

| 契约条件文件 | 是否触发 | 理由 |
|---|---|---|
| `docs/DATABASE_DESIGN.md` | **否** | 无数据库、无持久化服务；插件状态为宿主 auth store + 本地缓存文件（第 6.3 节） |
| `docs/DATABASE_DDL.sql` | **否** | 同上，无 DDL |
| `docs/API_DESIGN.md` | **是** | 新增管理端可见接口面（`quota.*` 4 方法 + 宿主 3 条路由）与发布/registry 契约 → 已产出 |
| `docs/VISUAL_SPEC.md` | **是** | PRD 6.2 的 VC-1…VC-4 需要可执行的视觉判据 → 已产出 |
| `docs/TEST_PLAN.md` | **是** | PRD 6.3 测试要求需要分层落地映射 → 已产出 |

### 10.4 非目标同步声明（PRD 3.2 / D6）

本轮**不**引入 CodeBuddy Intl、新 realm、新额度池、新登录平台；**不**注册 `websocket.response_event`；**不**重写面板 UI 或引入前端构建链；**不**新增第三方 Go / 前端依赖；**不**改动账号自动删除策略、凭据重试/failover、session routing；**不**修改 `docs/DESIGN.md` 以外的契约文档（本文件即其产物）。

---

## 11. 风险与未验证项

### 11.1 风险（含应对，与 PRD 8.2 对齐）

| # | 风险 | 本设计的应对 |
|---|---|---|
| R1 | `go mod tidy` 后传递依赖意外跳动 | `go.mod` diff 边界已量化（第 3.3 节）；CI 全绿为门；越界即复审 |
| R2 | 单仓裸 tag 命名空间冲突 → 退化为方案 B | D5 拆仓为唯一路径；第 8.4 节设阻断点，dev 不得自选命名空间 |
| R3 | `request.complete` 的 `Metadata` 键集未实证 | 闸门只读 `RequestID`（第 5.2 节第 4 条） |
| R4 | schema 6 不再转义的反向假设（前端若有隐式实体解码会反向出错） | 实读两前端均为裸解析（`workbuddy/panel.html:661` `JSON.parse`；`qoderwork/panel.html:591` `r.json()`），`esc()` 均为生成端 → 无双解码路径；回归断言见 TEST_PLAN |
| R5 | qoderwork 零测试基线 | D7 顺序：workbuddy 先行冻结契约，qoderwork 跟随复用（第 1 节 D7） |
| R6 | affinity 只读被误用为写路径 | 第 7.2 节红线 + TEST_PLAN 覆盖「失败静默降级」 |
| R7 | quiesce 的 janitor 改造破坏既有行为 | 改造为 `select` 双分支（第 6.2 节），保留原 tick 语义；单测断言 stop 后不再吞过期项 |
| R8 | draining 状态下错误体形态与 AC-5 字面判据不符 | 已给出可满足字面的实现（第 6.4 节 message 为合法 JSON）+ 两级取证口径 |
| R9 | **宿主版本漂移**：本机日常宿主已到 8.0.35，若执行方顺手用 8.x 代跑运行时验收，会得到跨代无效结论 | §10.2 P-HOST 前置 + `docs/TEST_PLAN.md` §0.3 第 0 步版本断言；8.x 上的任何冒烟只记为观察项，不进证据链 |
| R10 | **工作区存在未提交 WIP**（非本设计产出），易被误当作「已验证实现」 | §2.1「工作区漂移」行；dev 卡明确要求逐项核对 + 补 qoderwork + CI 全绿；本设计不为该 WIP 背书 |

### 11.2 未验证项（不进入结论）

1. 官方 `docs/plugin-development` 对 schema 6 的文档口径（本轮结论全部来自源码直读）。
2. `request.complete` 的 `Metadata` **具体键集**（已确认注入点与若干已知键，未逐键展开）。
3. `x/net v0.57.0` 相对 v0.47.0 的完整变更集（仅核实了 `proxy` 包导出面与 `go` 指令；其余包插件未使用）。
4. `qoderwork` 升级后是否引入 `golang.org/x/net`（取决于 `go mod tidy` 实际结果，见第 3.3 节末）。
5. `P-ENV`：本设计的运行时判据（AC-3/4/5/8）在无 Go 工具链时**只能由 CI + 源码读证支撑**，属证据受限，不得宣称为已验证。
6. **ADR-001 残余差异 1 的运行时后果**：`QuotaProvider` 走回调上下文后不再使用 per-auth 代理（源码读证已确认该退化；是否在真实部署中造成上游错误出口 IP **未验证**）。取证方式见 ADR-001 验证口径。

---

## 12. 实施路线图（供下游 dev-director 直接执行）

| 阶段 | 内容 | 完成判据 |
|---|---|---|
| **P0 WIP 对账**（2026-10-21 增补） | 工作树已存在未提交实现（workbuddy 的 quota/quiesce/completion/affinity + 测试 + golden；qoderwork 仅 version 常量）。逐项对照本设计 §3–§7 核对语义（**不是**照抄现状），缺项补齐、错项改正 | `git status` 中每个新增文件都能映射到 §3–§7 的某条约束；无法映射者删除或说明理由 |
| **P1 版本治理**（先于发布） | D-C 单一真源：`VERSION` 为准，代码内 `var version` 仅占位并在发布时 `-ldflags -X main.version=$VERSION` 注入；registry `version` 由 CI 写入；README 去版本字面量（`qoderwork/README.md:24,32` 现存 `0.2.6` 必须清除） | 单测断言 `var version == VERSION`（workbuddy 已有 `version_test.go`，qoderwork 新增）；README 无版本字面量（脚本扫描零命中） |
| **P2 SDK 升级**（两插件） | 第 3.1 节改动 + `go mod tidy` | CI 全绿；`go.mod` diff 在第 3.3 节边界内；第 9 节 golden 回归仅两类变化 |
| **P3 能力补齐**（workbuddy 先行） | 第 4 节 QuotaProvider → 第 5 节 request.complete → 第 6 节 plugin.quiesce → 第 7 节 affinity 只读接入 | 单测（含 `size<=0` 除零、闸门并发/空键/TTL/容量、quiesce 幂等与 stop、affinity 降级）+ CI 绿 |
| **P4 拆仓与发布** | 第 8 节布局 + 8.4 步骤（含 P-REPO 阻断点） | 自包含发布单元落盘；裸 tag release 含 7 归档 + `checksums.txt`；`gh release view` 通过 |
| **P5 端到端验收** | P-STORE 登记 → 商店安装 → 面板核对 | AC-6 通过（落盘名 `<id>-v<version>.<ext>`、面板版本非空且 == registry 声明版本） |
| **P6 qoderwork 跟随** | 复用 P1–P3 的 workbuddy 实现（含新增版本断言、注册契约断言、QuotaProvider 映射、quiesce、闸门） | 同 P1–P3 判据 + 补齐 panel.test.js（如需） |
| **P7（独立立项）** | CodeBuddy Intl | 另开 PRD，不在本轮 |

---

## 13. 下游

- 通过后创建 **exactly one** `dev-director` 子任务（board: `prod`，skill: `hermes-dev-workflow`，稳定 idempotency key 由 source task 派生）。
- dev 卡的验收依据为 PRD 第 6 节 `AC-1…AC-8` / `VC-1…VC-4`，本设计提供其落地机制与证据口径（第 3–9 节）。
- 本文件为**设计交付物，不含代码改动**；工作区新增仅 `docs/DESIGN.md` 与三份条件文件。

---

## 14. 架构决策记录（ADR）

### ADR-001：`QuotaProvider` 的上游传输通道 — 以宿主回调上下文（`req.HostCallbackID`）替代 `req.HTTPClient`

- **状态**：已采纳（2026-09-18，架构总监裁定）。**修正范围**：本节 §4.3 表格 `FetchQuota()` 行、§4.5 第 1 条及其实现注记、`docs/API_DESIGN.md` §1.3 的 Q1 与 §1.4 注记。
- **触发**：dev-director 在 B3 实施回报中提出 —— 原 §4.5 Q1「必须使用宿主注入的 `req.HTTPClient`」在物理上无法执行。

#### 决策

`FetchQuota` 的上游请求**必须**经宿主为本次调用开启的回调上下文发出（即 `hostHTTPDoWithCallback(req, req.HostCallbackID)`；等价地：任何 `host.http.*` 调用都带上本次 RPC 信封里的 `host_callback_id`），**禁止自铸 callback id**。原表述中「使用 `req.HTTPClient`」作废。

#### 依据（本轮实读，逐条可在 v7.3.5 源码复核）

| # | 事实 | 出处 |
|---|---|---|
| E1 | `QuotaFetchRequest.HTTPClient` 带 `json:"-"`，**不在 wire 上** | `sdk/pluginapi/types.go:1518-1519` |
| E2 | 宿主 `callQuotaFetch` 确实为该字段注入了 client（`h.newHTTPClient(authRecord, req.Provider)`），但那是**进程内接口值** | `internal/pluginhost/quota_provider.go:374-376` |
| E3 | 插件侧实际持有的是 RPC 适配器 `rpcQuotaProvider`；请求在 `callPlugin` 中先过 `sanitizePluginRequest`，`case pluginapi.QuotaFetchRequest` 与 `case rpcQuotaFetchRequest` **均执行 `req.HTTPClient = nil`** | `internal/pluginhost/rpc_client.go:185, 274-281` |
| E4 | 同一适配器在调用前已开启回调上下文，并把 **`HostCallbackID` 放进与请求同一个 wire 结构** | `internal/pluginhost/rpc_client.go:658-664`、`internal/pluginhost/rpc_schema.go:135-138` |
| E5 | 插件回传该 id 后，宿主 `callHostHTTPDo` 用 `resolveCallbackContext(callbackID, ctx)` 还原调用上下文再发请求 | `internal/pluginhost/host_callbacks.go:146-153` |

结论：E2 的注入在 E1（不可序列化）与 E3（显式置 nil）两道边界上**必然被销毁**，故「用 `req.HTTPClient`」在 c-shared 出进程插件模型下不可达；E4/E5 是宿主为这类调用提供的**唯一可传输通道**，且由宿主自己接线（非插件绕过）。

#### 约束意图未被放宽

原 Q1 要防的是「插件**自铸** callback id，绕开宿主回调上下文，与 `CompleteRequestExcept` / `skipPluginID` 的嵌套规避设计对抗」。改用 `req.HostCallbackID` 后该风险为零：插件回传的正是宿主刚为本次配额调用开启的上下文，既未自铸、也未新建嵌套，宿主侧的规避设计保持原样生效。**故这是等价实现，不是变通绕过。**

#### 残余差异（如实登记，不宣称为与 `HTTPClient` 逐项等价）

1. 宿主注入的 client 携带 per-auth 信息（`authRecord` 决定 `auth.ProxyURL`，并为请求日志提供 `provider`）；回调上下文路径下 `callHostHTTPDo` 用 `newHTTPClient(nil)`（auth 为 nil），因此**不使用 per-auth 代理**，退化为「全局 `cfg.ProxyURL` / 上下文 RoundTripper」（`internal/pluginhost/http_bridge.go:31-35, 199-206`、`internal/runtime/executor/helps/proxy_helpers.go:29-63`）。
2. 该退化与面板 `/credits` 路径**逐字相同**（两者都走 `hostHTTPDoWithCallback` + `rpcManagementRequest.HostCallbackID`），因此 PRD 5.2 约束 3「复用既有 credits 缓存与失效路径」被强化：配额查询与面板查询的传输语义完全一致。
3. 若未来出现「配额抓取必须走 per-auth 代理」的真实需求，应作为**独立缺陷上报上游**（`rpcQuotaFetchRequest` 需要一个可传输的 auth 绑定通道），不得由插件侧绕开回调上下文自行解决。

#### 验证口径（本 ADR 结论的取证边界）

- **已验证**（源码直读）：E1–E5，以及残余差异 1 的代码路径。
- **未验证**（需运行时）：per-auth 代理缺省在真实部署中是否导致上游 403 或错误的出口 IP。取证方式：在配置了 per-auth 代理的 auth 上执行 `POST /v0/management/quota/fetch`，比对出口 IP 与同一 auth 的面板 `/credits` 出口 IP —— **两者一致即符合本 ADR**；若两者一致但仍与上游预期不符，属上游能力缺口，不构成本轮回归。

#### 文档同步清单（本 ADR 的落地项）

| 文档 | 位置 | 处理 |
|---|---|---|
| `docs/DESIGN.md` | §4.3 `FetchQuota()` 行、§4.5 第 1 条、§0 表 F9、§11.2 第 6 项 | 本次已修正 |
| `docs/API_DESIGN.md` | §1.3 Q1 与请求样例注释、§1.4 注记 | 本次已修正 |
| `docs/PRD.md` | §5.2 第 1 段（L163）与「三条硬约束」第 1 条（L174）含同一措辞 | **已修正**（2026-09-18，经 commander 正式路由 product-director 落盘）。架构侧实测确认：L163 / L174 已改为「必须经宿主回调上下文发出（只回传、不自铸 callback id）」并引用本 ADR；L164 原「**不得**自建带 `host_callback_id` 的嵌套 `host.http.do`」条**刻意保留**（该禁止项与本 ADR 同向，不属作废范围）；全文残留 `req\.HTTPClient` 字样仅存于 L163 的「已作废」更正记录内。PRD 行数 285 不变（原稿行数即 285；commander 路由函中的「L164 / 第 163-164 行」为笔误，硬约束第 1 条实际在 L174，L164 是「不得」条，product-director 按实际行号处理正确） |

---

*End of DESIGN — cpa-plugin @ CPA v7.3.5 适配（收口版 + ADR-001）*
