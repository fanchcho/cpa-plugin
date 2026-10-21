---
artifact: docs/TEST_PLAN.md
stage: architect
workflow_id: prod-dev
workflow_version: 2
author: architect-director
date: 2026-09-18
revised: "2026-10-21 — 补产出（DESIGN §10.3 已登记本文件为「触发且必须产出」）；补登环境漂移 P-HOST（宿主二进制由 7.3.5 漂移到 8.0.35）与本地能力实测"
prd_ref: docs/PRD.md
design_ref: docs/DESIGN.md
api_ref: docs/API_DESIGN.md
visual_ref: docs/VISUAL_SPEC.md
root_task_id: t_a8f34caa
source_task_id: t_a8f34caa
trigger: PRD 6.3 测试要求需要分层落地映射（Go 单测 / 前端 node:test / 契约回归 / 多平台 / 运行时）
approval_required: false
---

# 测试计划 — cpa-plugin @ CPA v7.3.5

> 上游：`docs/PRD.md` §6.1（AC-1…AC-8）、§6.2（VC-1…VC-4）、§6.3（测试要求）、§7（安全边界）、§8.1（前置）。
> 机制依据：`docs/DESIGN.md` §2（事实基线）、§3.3（`go.mod` diff 边界）、§5.2（闸门并发语义）、§6（quiesce/draining）、§9（golden 回归）、§10.2（P-ENV / P-REPO / P-STORE）。
> 接口依据：`docs/API_DESIGN.md` §1（RPC 方法）、§2（宿主 HTTP 端点）、§3（发布与 registry 契约）。
> 视觉依据：`docs/VISUAL_SPEC.md` §2–§5（VC-1…VC-4 的文本证据口径）。
> 本文件把 PRD 的测试要求落成**可执行的分层断言**：每条断言都有命令、判据、失败语义。不含任何 token / secret / 凭据。

---

## 0. 权威层级与执行前提

### 0.1 判据权威层级（PRD 6 引言，不得改写）

| 层级 | 权威范围 | 说明 |
|---|---|---|
| **CI** | 编译、`go vet`、Go 单测、前端测试、多平台 `c-shared` 产物 | 唯一权威源。本地复跑是便利，不是替代 |
| **运行时** | `schema` / `quota` / `request.complete` / `quiesce` / `affinity` 的契约协商行为 | 唯一权威源。这些全是宿主运行时协商，**单测覆盖不到** |
| **端到端** | 发布链路（tag → release → 商店安装 → 面板版本） | AC-6 专用；需 P-REPO + P-STORE |

任何一层缺失，对应该层的 AC 判为**未验证**，不得以其他层的间接证据「补齐」成通过。

### 0.2 执行环境能力实测（2026-10-21，本机）

| 能力 | 实测 | 影响 |
|---|---|---|
| `go` 工具链 | **不存在**（`command -v go` 空） | L1 / L2 / L4-build 的本地执行不可行 → **P-ENV**（PRD 8.1，归属 admin） |
| `node` | 存在（v26.12.0） | L3 `node --test panel.test.js` **本地可执行** |
| `python3` | 存在 | L0 的 `validate-registry.py` / `check-version-single-source.py` **本地可执行** |
| 宿主机二进制 | `/opt/homebrew/bin/cliproxyapi` → **8.0.35**（`brew list --versions` 同值） | **与 D1 钉版 7.3.5 不符** → 见 §0.3 P-HOST |
| 上游源码副本 | `/tmp/cpasdk`、`/tmp/cpachk` **已不存在**（临时目录已清理） | DESIGN 引用的行号锚点需按 tag 重新取源（`go mod download` 或 `raw.githubusercontent.com/router-for-me/CLIProxyAPI/v7.3.5/...`）复核，不得凭记忆断言 |

### 0.3 环境漂移登记：P-HOST（本轮新增前置，非设计变更）

**事实**：D1 钉定目标宿主为 v7.3.5；本机日常宿主已被 brew 升级到 **8.0.35**。二者不同代：8.0.35 的宿主机内字符串检出 `/v8/management/` 前缀，而 7.3.5 使用 `/v0/management/`（`docs/API_DESIGN.md` §2 的端点表即按 v7.3.5 编写）。

**处置**（不改 D1，不放宽 AC）：

1. **L4 运行时验收必须在 v7.3.5 宿主上执行**。获取方式（机械步骤，不涉及凭据）：
   - 优先：官方 release 下载对应平台的 v7.3.5 二进制到临时目录，`<path>/cliproxyapi --version` 必须打印 `CLIProxyAPI Version: 7.3.5`（**版本断言是 L4 的第 0 步**，不通过则不得开始 L4）；
   - 或：brew 版本钉定/切换后同样以 `--version` 断言。**不得**直接以 8.0.35 代跑并声称 AC 通过。
2. **8.0.35 上的安装/加载冒烟属非目标**（D1 未覆盖 8.x）。若执行方顺手做了，只能记为观察项，**不得**写入验收证据链。
3. 若 v7.3.5 二进制无法取得 → L4 全部 AC（AC-2c / AC-3 / AC-4 / AC-5 / AC-8 运行时部分）标记 **blocked(P-HOST)**，由 dev/test 卡以 `kind=capability` 上报，**不得**自行降级判据。

> P-HOST 与 P-ENV / P-REPO / P-STORE 同属**执行前置**，不是待拍板的设计选项；本文件把它转成显式阻断点。

### 0.4 反例：不可接受的证据（PRD 6.3 末行 + DESIGN §11）

| 不可接受 | 原因 |
|---|---|
| 手工把 `.so` / `.dylib` 塞进 plugins 目录后宣称兼容 | 只能证明 `dlopen` 成功，**≠ 行为正确** |
| 用 8.0.35（或任何非 7.3.5）宿主代跑 AC-2c/3/4/5/8 | 跨代协商语义不同，判据失效（§0.3） |
| 仅靠单测断言 `schema_version == 6` 就宣称「宿主已按 schema 6 处理」 | 单测不覆盖宿主协商；只能由 AC-3 的运行时表现间接证明 |
| 访问真实上游 / 使用真实 token 得到的响应作为夹具 | 违反 PRD 7.1；且不可复现 |
| 「我看了代码，逻辑没问题」 | 不是证据。证据 = 命令 + 退出码 + 关键输出 |
| 用 `panel.html` 源码 grep 代替渲染后 DOM 文本 | VC-1 判据是**页面可见文本**，不是源码 |

### 0.5 证据格式（写入 `docs/TEST_REPORT.md` 的统一口径）

每条断言一行，五元组齐全：

```
[断言ID] 层 | 命令（可复制复跑） | 退出码 / HTTP 状态 | 关键输出片段（脱敏、≤20 行） | 结论 PASS/FAIL/BLOCKED
```

- 输出片段**必须脱敏**：管理 key、access/refresh token、cookie、auth 文件名中的账号标识一律替换为 `<REDACTED>`；不得整段粘贴宿主日志中出现凭据的部分。
- FAIL 必须附**最小复现**（命令 + 前置状态），否则 test-director 无法判定退回。
- BLOCKED 必须写明阻断前置代号（P-ENV / P-HOST / P-REPO / P-STORE）与证据（如 `command -v go` 空输出）。

---

## 1. 分层模型

| 层 | 名称 | 执行位置 | 覆盖 AC/VC | 执行者 |
|---|---|---|---|---|
| **L0** | 静态与版本治理 | 本地 + CI | AC-7、AC-1（registry 校验） | dev（实现时）/ test（复核） |
| **L1** | Go 单元 + vet + race | CI（权威）/ 本地需 P-ENV | AC-2b、AC-4（映射）、AC-7（单测态）、AC-8（单测）、R6/R7 | dev（自测）/ test（复核） |
| **L2** | 契约回归（golden diff） | CI（权威） | 结论 7（DESIGN §9）、AC-2b 的强形式 | dev（采集）/ test（复核 diff 形状） |
| **L3** | 前端 `node:test` | 本地 + CI | AC-1（frontend job）、D-A 渲染路径 | dev / test |
| **L4** | 运行时验收（宿主 v7.3.5） | 仅有 7.3.5 宿主时 | AC-2c、AC-3、AC-4（端到端）、AC-5、AC-8（运行时）、VC-1…VC-4 | test-director |
| **L5** | 发布端到端 | 需 P-REPO + P-STORE | AC-6 | dev（发布）/ test（验证） |

**通过语义**：P0 断言（AC-1…AC-7）在**其所属权威层**全绿才算 PASS；P1（AC-8）允许在其权威层部分 BLOCKED，但必须显式登记，不得沉默。

---

## 2. L0 — 静态与版本治理（AC-7 主体）

### 2.1 命令与判据

```bash
# A0-1 registry 结构合法（AC-1 test job 既有步骤）
python3 scripts/validate-registry.py registry.json          # 退出码 0

# A0-2 版本单一真源（AC-7；DESIGN §12 P1）
python3 scripts/check-version-single-source.py              # 退出码 0，输出零违例
```

`check-version-single-source.py` 必须检查三件事（缺一即 FAIL）：

| # | 检查 | 判据 |
|---|---|---|
| 1 | `<plugin>/main.go` 的 `var version = "..."` 默认值 == `<plugin>/VERSION` | 逐插件比对，两插件都查 |
| 2 | `registry.json` 中该插件条目的 `version` == `<plugin>/VERSION` | 两插件都查 |
| 3 | README（根 + 各插件）**无具体版本字面量** | 不得出现：`<plugin>_<semver>_<goos>_<goarch>` 归档名、`-X main.version=<semver>`、以及等于 VERSION 内容的裸串 |

**豁免**：`CHANGELOG*` 与 `docs/` 下的历史记录**必须**能写过往版本号，脚本不得对其报错（否则会把历史记录逼成违例）。

### 2.2 人工复核项（脚本覆盖不到的等价性）

| ID | 判据 | 方法 |
|---|---|---|
| A0-3 | `docs/DESIGN.md` 的 `go.mod` diff 边界被遵守 | `git diff --unified=0 -- '*/go.mod' \| grep -E '^[+-][^+-]' \| grep -vE 'CLIProxyAPI/v7\|golang\.org/x/(net\|sys) '` → **必须为空**（输出任意行即超界，按 R1 复审） |
| A0-4 | `go` 指令不变 | 两 `go.mod` 的 `go 1.26.0` 行未出现在上述 diff 中（已被 A0-3 覆盖，单列便于阅读报告） |
| A0-5 | 无凭据入库 | `git status --porcelain` 与 `git diff` 中不得出现 `config.yaml*`、`.env*`、`auths/`、`*.log`、`*.pem` |

---

## 3. L1 — Go 单元 / vet / race

### 3.1 执行

```bash
cd workbuddy && go test ./... -count=1 && go vet ./...
cd qoderwork && go test ./... -count=1 && go vet ./...
# 竞态（新增并发结构必须有覆盖：闸门、draining 标志、quiesce Once）
cd workbuddy && go test -race ./... -count=1
cd qoderwork && go test -race ./... -count=1
```

- 权威位置：CI `test` job（`ubuntu-24.04`，matrix `workbuddy` / `qoderwork`），`fail-fast: false`，任一红即整体红。
- 本地执行需 **P-ENV**；无 Go 时本节一律 `BLOCKED(P-ENV)`，不得用「读代码」替代。

### 3.2 必须覆盖的断言清单

> 命名不构成约束（实现方可重命名），**语义必须覆盖**。「当前工作树锚点」是便于复核的参考位置，不是判据。

#### 3.2.1 注册载荷契约（对应 DESIGN §9 / API_DESIGN §1.8）

| ID | 断言 | 当前工作树锚点 |
|---|---|---|
| A1-01 | 注册载荷的 `schema_version` == `pluginabi.SchemaVersion` 的运行值，且该值 == 6（常量比较，禁止字面量魔法数） | `workbuddy/register_contract_test.go`（`TestRegisterAdvertisesSchemaSixAndNewCapabilities`） |
| A1-02 | `capabilities.quota_provider == true`、`capabilities.request_lifecycle_plugin == true` | 同上 |
| A1-03 | `capabilities.websocket_response_observer` **不出现或为 false**（PRD N1 非目标） | `TestRegisterAdvertisesOnlyImplementedCapabilities` |

#### 3.2.2 `QuotaProvider` 四方法（对应 PRD 5.2 / DESIGN §4 / API_DESIGN §1.1–1.4）

| ID | 断言 | 当前工作树锚点 |
|---|---|---|
| A1-10 | `Identifier()` 返回值 == 既有 `providerName` 常量（`workbuddy` / `qoderwork`），不得另起字面量 | `quota_test.go`（`TestQuotaIdentifierMatchesProviderName`） |
| A1-11 | `Describe()` 的 supported providers / supports_reset 形态符合契约（AC-4 的 `providers[]` 数据来源） | `TestQuotaDescribeContracts` |
| A1-12 | `ResetQuota()` **恒** `Success:false`，且**无任何上游写调用**（用 fake host 断言零 `host.http.*` 写） | `TestQuotaResetNeverSucceeds` |
| A1-13 | `remainingFraction = total_remain / total_size`；`size <= 0` 时**不得**产生除零 / NaN / Inf（必须给出确定性取值并单测锁定） | `TestQuotaBucketFromCreditsZeroSizeIsNotNaN` |
| A1-14 | fraction 越界（<0 或 >1）被钳制到合法区间 | `TestQuotaBucketFractionClampsOutOfRange` |
| A1-15 | 空额度池不产出空 bucket（省略而非填零，避免下游误读） | `TestQuotaBucketFromCreditsEmptyPoolOmitsBucket` |
| A1-16 | `FetchQuota()` **复用** `invalidateAccountCredits` 既有 credits 缓存与失效路径（不得另建第二套缓存） | `TestQuotaFetchReusesCreditsCache` |
| A1-17 | 上游失败时错误**脱敏**：`message` 为固定文案，不含上游原始响应体 / 原生错误文本 | `TestQuotaFetchFailureIsSanitized` |
| A1-18 | 无凭据时不 panic，返回确定性失败 envelope | `TestQuotaFetchWithoutCredentialFails` |
| A1-19 | 出站通道符合 **ADR-001**：带本次 RPC 信封的 `host_callback_id`，**禁止自铸** callback id（用 fake host 断言传出的 id == 入参 id） | 见 DESIGN §14 验证口径；实现锚点 `quota.go` 的 `hostHTTPDoWithCallback` 调用 |

#### 3.2.3 `request.complete` 闸门（对应 PRD 5.3 / DESIGN §5.2）

| ID | 断言 | 当前工作树锚点 |
|---|---|---|
| A1-20 | 同一 `RequestID` 的 N 个**并发**调用，`accept` **恰好一个**返回 true（单飞） | `completion_test.go`（`TestCompletionGateIsSingleFlight`） |
| A1-21 | 空键 / 纯空白 `RequestID` → false，且**不入表也不记账** | `TestCompletionGateRejectsEmptyKeyWithoutRecording` |
| A1-22 | TTL 过期条目被懒清理；容量上界（4096）触发 FIFO 淘汰（**有界内存**硬要求） | `TestCompletionGateExpiresByTTL` / `TestCompletionGateEvictsByCapacity` |
| A1-23 | 去重路径**只读** `RequestID`：构造 `Metadata` 含形似键名（如 `request_id` / `id`）但 `RequestID` 不同的记录，**必须都记账**；`RequestID` 相同而 `Metadata` 不同则**只记一次** | `TestRequestCompleteNeverReadsMetadataKeys` |
| A1-24 | 执行器侧已记账的请求 → 终态回调**不产生第二条 usage 记录**（共用同一键空间，DESIGN §5.2 第 5 条） | `TestRequestCompleteIgnoresObservedOutcomes` |
| A1-25 | `canceled` 终态被记录（执行器观测不到的终态由本路径补记） | `TestRequestCompleteRecordsCanceledOnce` |

#### 3.2.4 `plugin.quiesce` / draining（对应 PRD 5.4 / DESIGN §6）

| ID | 断言 | 当前工作树锚点 |
|---|---|---|
| A1-30 | `pluginQuiesce()` **幂等**：重复调用不 panic、不重复 close 通道（`sync.Once` 或判空置 nil） | `quiesce_test.go`（`TestPluginQuiesceIsIdempotent`） |
| A1-31 | checkin 09:00/21:00 与 keepalive 22:00 共用的调度循环被停止（close 停止通道后 `schedulerLoop` 退出，timer 被 `Stop()`） | `TestPluginQuiesceStopsBackgroundTimers` |
| A1-32 | `loginStatesPruneInterval` janitor（1 分钟 ticker）能退出（`for range ticker.C` 已改造为双分支 `select`） | `TestPluginQuiesceStopsLoginStatesJanitor` |
| A1-33 | 已存在落盘路径的状态被落盘（workbuddy：model-catalog 元数据 + 单模型缓存，原子 rename）；qoderwork 落盘义务为**空集**（如实断言，不新造状态文件） | `TestPluginQuiesceFlushesState` |
| A1-34 | 落盘失败时**不 panic**、返回可观测失败（不得因 quiesce 失败而拖死宿主） | `TestPluginQuiesceReportsFlushFailureWithoutPanicking` |
| A1-35 | draining 置位后新 executor 请求快速失败，error 体含 `code=plugin_draining`、`retryable: true`、`http_status: 503` | `TestDrainingRejectsNewExecutorWork` |
| A1-36 | draining 置位后管理写路由快速失败，但**面板/只读路由仍可达**（否则热替换期间面板 500） | `TestDrainingRejectsManagementWrites` / `TestDrainingKeepsPanelReachable` |
| A1-37 | **红线**：`cliproxyPluginShutdown` 保持 **no-op**（碰 Go runtime 会 SIGSEGV；DESIGN §6.3 红线） | `TestShutdownExportStaysNoop` |
| A1-38 | `MethodPluginQuiesce` 已进入 `handleMethod` 分派（否则宿主回落为 `unsupported`） | `TestQuiesceMethodIsRegisteredAsHandled` |
| A1-39 | 两处 `checkin.go` 中「deliberately no stopCheckinScheduler」的旧注释已改写，指明停止入口迁移至 quiesce（代码可读性即 AC-5 的静态侧证据） | `grep -rn 'no stopCheckinScheduler' workbuddy qoderwork` → **零命中** |

#### 3.2.5 `host.affinity.lookup` 只读（对应 DESIGN §7 / PRD 7.5）

| ID | 断言 | 当前工作树锚点 |
|---|---|---|
| A1-40 | 无 session 时不发起 lookup（惰性，不产生额外 host 调用） | `affinity_test.go`（`TestAffinityQueryWithoutSessionSkipsLookup`） |
| A1-41 | 宿主不支持 / 调用失败 → **静默降级**（失败即空结果，不阻塞面板、不产生错误块 → VC-3） | `TestAffinityLookupFailsClosedWithoutHost` |
| A1-42 | 探针只读：判别用 HTTP 200，**无任何写方法** | `TestAffinityProbeAlwaysUsesHTTP200` |
| A1-43 | 请求字段完整（缺字段即不发请求，避免宿主侧拒绝） | `TestAffinityLookupRequestRequiresAllFields` |
| A1-44 | **R6 红线**：面板代码路径中**不存在**把 affinity 结果写回宿主的调用（静态扫描断言） | `TestPanelHasNoAffinityWritePath` |

#### 3.2.6 版本单一真源（单测态，与 L0 脚本互补）

| ID | 断言 | 当前工作树锚点 |
|---|---|---|
| A1-50 | `var version` 默认值 == `VERSION` 文件（workbuddy 既有；**qoderwork 本轮首次补齐**） | `workbuddy/version_test.go`、`qoderwork/version_test.go` |
| A1-51 | `registry.json` 条目 version == `VERSION` | `qoderwork/version_test.go`（`TestRegistryVersionMatchesVersionFile`） |
| A1-52 | README 无版本字面量（脚本的测试化镜像，双保险） | `TestReadmeHasNoVersionLiteral` |

### 3.3 测试替身约束（PRD 6.3）

- 合成 fixture / fake host / `httptest`；**禁止**访问真实上游、**禁止**使用真实 token。
- 时间相关断言用可注入时钟（`now` 参数或 clock 接口），禁止 `time.Sleep` 等真实等待；否则 TTL/容量用例在 CI 上抖动。
- 涉及宿主回调的用例通过 fake host 断言「传出的 callback id == 入参 id」，即在单测层锁住 ADR-001。

---

## 4. L2 — 契约回归（golden diff，DESIGN §9）

### 4.1 流程（离线、确定性、CI 可跑）

```bash
# 1) 基线采集（go.mod 仍为 v7.2.30 时；本仓库 HEAD 已是待升状态，故基线取自 v7.2.30 的
#    临时 worktree，或在升级前的一次提交上采集）
cd workbuddy && go test -run TestRegisterGolden -update-register-golden ./...
# 2) 升级到 v7.3.5 后，只做断言（不带 -update）
cd workbuddy && go test -run TestRegisterGolden ./...
# 3) diff 形状检查（人工 + 断言脚本双轨）
git diff -- workbuddy/testdata/register.golden.json
```

**注意（本仓库实际状态，2026-10-21 实测）**：`workbuddy/testdata/register.golden.json` 当前内容为 **`schema_version: 1`、且不含 `quota_provider` / `request_lifecycle_plugin`** —— 即它**就是 v7.2.30 侧的「before」基线**，不是「after」。这符合 §9 的用法（基线冻结、live payload 作为 after），但**现行 WIP 的断言强度不足**，必须补两条：

| 缺口 | 现状 | 必须补成 |
|---|---|---|
| `schema_version` 未参与 golden 比较 | 现行只比对顶层键集 / metadata / 既有 capability 布尔值，**不比较** `schema_version` | 断言 live 的 `schema_version` == `pluginabi.SchemaVersion`（常量比较）且 == 6（A2-02） |
| 新增 capability 键**未设上界** | 现行只要求「既有键不消失、布尔值不翻转」，因此**任意第三个新键**都会通过 | 断言「新出现的键集合」**恰好等于** `{quota_provider, request_lifecycle_plugin}`，多一个即 FAIL（A2-03 / A2-06） |

> 这两条不是新增需求，而是 DESIGN §9「任何其他变化 → 回归失败」的可执行化。dev 的 P0 对账必须把 golden 测试补到这个强度；否则「只有两类变化」只是一个未被机器守住的说法。

### 4.2 断言（缺一即 FAIL）

| ID | 断言 |
|---|---|
| A2-01 | 顶层键集不变：`schema_version` / `metadata` / `capabilities`（不新增、不删除顶层键） |
| A2-02 | `schema_version`：1 → 6，且断言用常量 `pluginabi.SchemaVersion` 比较 |
| A2-03 | `capabilities` **真值集合**变化仅限：`quota_provider` 由不存在→true、`request_lifecycle_plugin` 由不存在→true；**既有键的布尔值逐一不变** |
| A2-04 | `capabilities.websocket_response_observer` 仍不存在（PRD N1） |
| A2-05 | `metadata` 除 `version`（随 VERSION 漂移，测试内冻结或忽略）外全等 |
| A2-06 | 「live 新增键集合」**恰好等于** `{quota_provider, request_lifecycle_plugin}`（`set(got.keys) - set(golden.keys)` 的等值断言；多一个键即 FAIL，堵住「任意第三能力混入」） |

### 4.3 预期 diff 与失败处置

- **预期**：恰好 `schema_version: 1 → 6` + `capabilities` 两个新增键。
- 出现**任何第三类变化**（字段消失、既有布尔翻转、metadata 漂移）→ FAIL，按 R1「传递依赖/契约意外变动」复审，不得以「看起来无害」放行。
- 该层**不需要宿主参与**（注册响应完全由插件侧构造），是老化的最佳护栏。

---

## 5. L3 — 前端 `node:test`

```bash
cd workbuddy && node --test panel.test.js     # 既有 16 个用例，必须全绿
```

| ID | 断言 |
|---|---|
| A3-01 | 既有 16 个用例全绿（AC-1 的 frontend 要求；CI `test` job 已加 `node --test`，仅当 `panel.test.js` 存在时触发） |
| A3-02 | **D-A 渲染路径**：以「响应原文进入渲染路径」为准 —— 给定含 `A&B<C>D'E"D` 的昵称与含 `<Pro>` 的 plan 的**管理响应原文**，渲染路径**不得**二次解码或转义：输出文本等于原文，且不含 `&amp;` / `&lt;` / `&gt;` / `&quot;` / `&#39;` |
| A3-03 | `esc()` 等**生成端**转义函数保持存在且行为不变（PRD W7：只修宿主侧转义，不动生成端） |
| A3-04 | 面板无 affinity 写路径（与 A1-44 同源的前端侧静态断言） |

> L3 的 `node --test` 是**本地可执行**的（node 存在），故 dev/test 不得以「无 Go 环境」为由跳过 L3。

---

## 6. L4 — 运行时验收（宿主 v7.3.5）

### 6.1 前置（第 0 步，不通过则整层 BLOCKED）

| # | 前置 | 判据 |
|---|---|---|
| R0-1 | 宿主版本 | `<host> --version` 打印 `CLIProxyAPI Version: 7.3.5`（**P-HOST**，见 §0.3） |
| R0-2 | 插件产物 | 两个平台匹配的 `c-shared` 产物由 **CI 构建**得到（不得用本地手搓 `.so` 冒充；`dlopen` 成功不是证据） |
| R0-3 | 管理路由 | 管理端可访问；鉴权材料**只经环境变量注入**（下例 `$CPA_MANAGEMENT_KEY`），任何命令/日志/文档不得出现其值 |
| R0-4 | 账号夹具 | 使用合成/沙箱账号；昵称/plan 由夹具显式构造（AC-3 需要特殊字符） |
| R0-5 | 网络 | 允许从商店源拉 registry 与资产（AC-6）；不需要真实上游额度接口即可完成 AC-3/AC-5 |

下例统一用 `X-Management-Key` 头（宿主内 header 名实测存在）；路径按 v7.3.5 的 `/v0/management/`。

```bash
HOST=http://127.0.0.1:8317
AUTH=(-H "X-Management-Key: $CPA_MANAGEMENT_KEY")
```

### 6.2 AC-3 — 实体串零命中（D-A 的运行时判据）

```bash
# (a) 管理响应原文：昵称/plan 必须是原文，未转义
curl -s "${AUTH[@]}" "$HOST/v0/management/plugins/workbuddy/accounts" > /tmp/acc.json
python3 - <<'PY'
import json,sys
raw=open('/tmp/acc.json',encoding='utf-8').read()
bad=[e for e in ('&amp;','&lt;','&gt;','&quot;','&#39;') if e in raw]
print("HTML_ENTITY_IN_RAW_RESPONSE:", bad or "NONE")
d=json.loads(raw)
# 打印昵称/plan 字段以确认原文形态（不含凭据）
PY
# (b) 面板页面：渲染后可见文本
curl -s "${AUTH[@]}" "$HOST/v0/resource/plugins/workbuddy/panel" > /tmp/panel.html
```

| ID | 判据 |
|---|---|
| A4-03a | `/accounts` **原始 body** 中不含上述 5 个实体串，且含特殊字符字段为原文 |
| A4-03b | 浏览器渲染后 `document.body.innerText` 中不含上述 5 个实体串（取证方式见 `docs/VISUAL_SPEC.md` §2.2 两级） |
| A4-03c | **反向风险 R4**：若前端存在隐式实体解码，会在原文进入后反向出错 → 以 A4-03b 的「原文可见」为最终判据（`VISUAL_SPEC.md` §2.4） |

### 6.3 AC-4 — `QuotaProvider` 端到端一致性

```bash
curl -s "${AUTH[@]}" "$HOST/v0/management/quota/providers"
curl -s "${AUTH[@]}" -H 'Content-Type: application/json' \
  -d '{"auth_index":"<idx>","provider":"workbuddy"}' \
  "$HOST/v0/management/quota/fetch"
curl -s "${AUTH[@]}" "$HOST/v0/management/plugins/workbuddy/credits"
```

| ID | 判据 |
|---|---|
| A4-04a | `providers[]` 含 `provider` 值分别为小写 `workbuddy` 与 `qoderwork` 的两项（**两插件都装**才满足） |
| A4-04b | `quota/fetch` 的 `groups[0].buckets[0].remainingFraction × size` 与 `/credits` 的 `total_remain` **一致**（同一账号、同一时刻；允许上游延迟窗口 ≤ 缓存 TTL，超出即 FAIL） |
| A4-04c | `summary` 含 `unit: "credits"` 的指标 |
| A4-04d | `size <= 0` 的账号：响应中**无** `NaN` / `Infinity` / 除零 panic，且 fetch 不 5xx |
| A4-04e | `/v0/management/plugins/<id>/credits` 与 `/accounts` **仍存在且字段未被裁剪**（PRD 5.2 硬约束 2「保留」） |
| A4-04f | `quota/reset` → 插件恒 `success:false`，且上游**无写操作**（以 fake/沙箱上游的请求计数取证） |

### 6.4 AC-5 — quiesce / draining（两级取证，`DESIGN §6.4` 证据边界）

```bash
# 触发宿主热替换（按管理端既有方式重载插件），随后：
grep -c 'plugin quiesce unsupported' <host-log>      # 必须 0（且不得有 'plugin quiesce failed' Warn）
ls -a "<state-dir>/workbuddy/model-catalog" | grep '\.tmp-'   # 必须无输出
curl -s -o /tmp/drain.json -w '%{http_code}\n' "${AUTH[@]}" \
  -H 'Content-Type: application/json' -d '{"model":"<m>","messages":[]}' \
  "$HOST/v1/chat/completions"                        # 期望 503
```

| ID | 判据 | 层级 |
|---|---|---|
| A4-05a | 宿主日志**不出现** `pluginhost: plugin quiesce unsupported`（Debug 回退），也**不出现** `plugin quiesce failed`（Warn） | 宿主日志 |
| A4-05b | 热替换后**一个调度窗口内**无 checkin / keepalive 上游请求（以沙箱上游请求日志计数，窗口 = 覆盖 09:00/21:00/22:00 中最近一个触发点，或直接观测 ticker 周期内零请求） | 上游日志 |
| A4-05c | 状态文件以 rename 更新：状态目录**无** `.tmp-` 残留 | 文件系统 |
| A4-05d | **第一级**：插件 envelope 含 `code=plugin_draining`、`retryable: true`、`http_status: 503` | 插件侧 |
| A4-05e | **第二级**：下游 HTTP 状态码 = **503**，响应体含字面 `"retryable": true`（宿主丢弃 `Retryable` 字段，故 `message` 必须是合法 JSON 对象才能透传 —— `DESIGN §6.4`） | 下游响应 |
| A4-05f | 面板在 draining 状态下仍可访问（不得 500；新实例接管面板属正常） | 运行时 |

> A4-05d/e 必须**两级都取证**。只给下游 503 而无 envelope 证据，无法区分「插件主动 draining」与「别的 5xx」。

### 6.5 AC-8 — `canceled` 终态与不重复记账

| ID | 判据 |
|---|---|
| A4-08a | 客户端主动断开一次**流式**请求 → usage 侧出现该次记录且带 `canceled` 语义（`outcome=canceled`） |
| A4-08b | 以同一 `RequestID` 重放 `request.complete` → **不产生第二条** usage 记录（幂等以 `RequestID` 为键，不依赖 `Metadata` 键名） |
| A4-08c | 反例核对：构造 `Metadata` 含形似键名但 `RequestID` 不同 → 两条**都**记账（证明去重不读 Metadata） |

### 6.6 VC-1…VC-4 — 文本证据（不要求像素比对）

| ID | 文本证据 | 口径 |
|---|---|---|
| VC-1 | `document.body.innerText` 全文不含 5 个实体串；含特殊字符的昵称/plan/错误按原文出现 | A4-03b 同一份采集 |
| VC-2 | 结构级对照基线 `docs/cpamp-workbuddy-panel.png`：左侧导航、页头「账号面板」、工具栏「刷新数据 / 全部签到 / 自动签到」、账号卡片（昵称 + plan 徽章 + 用量进度 + 已用/总量 + 签到状态）**结构不变** | 以 DOM 节点存在性/文本断言，不比对像素 |
| VC-3 | affinity 只读信息（若展示）为卡片内**惰性**字段：无新增阻塞式请求；查询失败**静默降级**（无红色错误块） | 断网/宿主不支持两种条件下各采一次 |
| VC-4 | `已禁用` / `耗尽` / `使用中` 徽章的既有色语义不反转 | 取徽章 class/内联色值文本证据，不做像素差 |

---

## 7. L5 — 发布端到端（AC-6）

```bash
# 前置：P-REPO（两个新仓库已建）+ 自包含发布单元已推送
git tag v<X.Y.Z>            # 裸 tag（D4）；不得再用 <id>-v*
git push origin v<X.Y.Z>
gh release view v<X.Y.Z> --json tagName,assets
```

| ID | 判据 |
|---|---|
| A5-01 | tag 为**裸** `vX.Y.Z`；`ReleaseVersion(tag)` 解析成功（不得退化为 `<id>-v*`，R2） |
| A5-02 | release 资产 = **7 个** `<id>_<version>_<goos>_<goarch>.zip` + 精确名 `checksums.txt` |
| A5-03 | `checksums.txt` 可解析且覆盖全部 7 个归档（宿主取每行前两列，不校验第二列文件名） |
| A5-04 | 管理端把该仓 `registry.json` 的 raw URL 登记为 store source（**P-STORE**）→ 安装成功 |
| A5-05 | 落盘文件名为 `<id>-v<version>.<ext>`（宿主持有 `-v` 后缀反解插件 ID） |
| A5-06 | 插件面板显示版本号**非空**且 == registry 声明的 version（`<id>-v...` 落盘名 + 版本反解一致） |
| A5-07 | 资产中不含本地环境文件（`config.yaml*`、`.env*`、`auths/`、日志） |

> A5-04 依赖人工在管理端登记（`DESIGN §10.2` P-STORE）。在此之前 A5-01…A5-03 / A5-07 可独立验证；A5-04…A5-06 标记 `BLOCKED(P-STORE)`。

---

## 8. 多平台产物断言（AC-1 的 build 侧）

**口径澄清（不降低覆盖）**：PRD AC-1 文字为「6 平台 native + `build-cross`」，CI 实际矩阵为**每插件 5 个 native**（`linux/amd64`、`linux/arm64`、`darwin/amd64`、`darwin/arm64`、`windows/amd64`）+ **2 个 cross**（`windows/arm64`、`freebsd/amd64`）= **7 个 (goos,goarch) 目标**。PRD 列举的 7 个目标**全部被覆盖**，差异仅在 windows/arm64 由 cross 作业产出而非 native runner。判据按「7 目标全绿」执行。

| ID | 判据 |
|---|---|
| A8-01 | `test` job 两插件全绿（`validate-registry` → `check-version-single-source` → `go test` → `go vet` → `node --test`） |
| A8-02 | `build` job 产出 5 个 native `c-shared`，`file` 类型断言为对应平台共享库（`.so` / `.dylib` / `.dll`） |
| A8-03 | `build-cross` 产出 2 个目标（`windows-arm64`、`freebsd-amd64`） |
| A8-04 | 每个归档的 **zip 根条目**为 `<id>.<so|dylib|dll>`（workflow 内的 zip 断言步骤，非仅「文件存在」） |
| A8-05 | 归档命名严格 `<id>_<version>_<goos>_<goarch>.zip`，version 无 `v` 前缀、无 `+ci` 残留 |

---

## 9. 覆盖矩阵（AC/VC → 层 → 断言）

| AC/VC | 权威层 | 断言 ID | 备注 |
|---|---|---|---|
| AC-1 | L0 + L1 + L3 + L8 | A8-01…A8-05、A1-50…A1-52、A3-01 | CI 唯一权威 |
| AC-2 | L1 + L2 +（L4 间接） | A1-01、A2-01…A2-06、A4-03b | AC-2c 为**间接证明**（宿主无独立端点，已知证据边界） |
| AC-3 | L4 | A4-03a/b/c、VC-1 | 需 P-HOST |
| AC-4 | L1 + L4 | A1-10…A1-19、A4-04a…A4-04f | |
| AC-5 | L1 + L4 | A1-30…A1-39、A4-05a…A4-05f | 两级取证 |
| AC-6 | L0 + L5 | A5-01…A5-07、A0-1、A0-2 | 需 P-REPO / P-STORE |
| AC-7 | L0 + L1 | A0-1、A0-2、A1-50…A1-52 | |
| AC-8 | L1 + L4 | A1-20…A1-25、A4-08a…A4-08c | P1 |
| VC-1…VC-4 | L4 | §6.6 四项 | 文本证据 |

---

## 10. 失败分类与退回约定（test-director 执行口径）

| 现象 | 分类 | 动作 |
|---|---|---|
| 断言 FAIL，且可复现、属实现缺陷 | 缺陷 | 在 **dev 源卡** comment 缺陷（断言 ID + 复现 + 期望/实际），block review 卡并给下一步建议；**不创建修复任务**（`hermes-dev-workflow` test-director 约定） |
| 断言 BLOCKED(P-ENV/P-HOST/P-REPO/P-STORE) | 前置缺失 | 在 review 卡 `block(kind=capability)`，写明缺失前置与证据；**不判 FAIL**，也不判 PASS |
| 证据不足（只有代码阅读 / 只有 `dlopen` / 用手工产物） | 证据无效 | 视为未验证，退回补证据 |
| `go.mod` diff 超界（A0-3 非空） | R1 风险 | 退回并附 diff，要求收窄到边界内或走复审 |
| golden diff 出现第三类变化（A2-03/04/05/06） | 契约意外变动 | 退回，禁止放行 |

---

## 11. 安全与凭据（PRD §7）

1. 文档、夹具、CI 日志、Kanban comment/metadata 中**不得**出现真实 API key / secret / token / cookie / 私钥 / 账号凭据。
2. 本机 `~/.cli-proxy-api/config.yaml` 含真实凭据字段：**禁止读取复制**进仓库任何文件（含测试夹具）。运行时验收的管理鉴权经**环境变量**注入（§6.1 `$CPA_MANAGEMENT_KEY`），命令与日志中一律以 `<REDACTED>` 呈现。
3. 面板 `management_key` / `usage_report_key`：空值依赖宿主中间件；非空时保持常量时间比较 + per-IP 限流；**任何响应不得回显**。
4. 错误分类保持脱敏：不上抛上游原始响应体与原生错误文本（A1-17 / A1-18 单测锁定）。
5. `ResetQuota` 无上游写操作（A1-12 / A4-04f）；`host.affinity.lookup` 只读（A1-42 / A1-44）。
6. 发布资产仅由 CI 产出（A5-07）。

---

## 12. 已知证据边界（诚实登记，不进入结论）

1. **AC-2c 是间接证明**：宿主无独立端点暴露 `schema_version`，只能由 AC-3 的「不再转义」表现反推 schema ≥ 6。
2. **ADR-001 残余差异**：`FetchQuota` 走回调上下文后不使用 per-auth 代理（`DESIGN §14` 残余差异 1）。其运行时后果（出口 IP / 上游 403）**本轮不验证、不承诺**；取证口径见 ADR-001「验证口径」，且以「与面板 `/credits` 出口一致」为符合判据。
3. **P-ENV 状态下**：L1/L2 只能由 CI 支撑；本地无法复跑不等于未验证，但报告必须写明证据来自 CI run（附 run id / job 名）。
4. **P-HOST 状态下**：L4 全部不可执行；不得以 8.0.35 代跑。
5. 官方 `docs/plugin-development` 对 schema 6 的文档口径未核对（本轮结论全部来自源码直读）。
6. `request.complete` 的 `Metadata` 具体键集未逐键实证 → 因此去重**只**依赖 `RequestID`（A1-23 / A4-08c 反向证明此约束）。

---

## 13. 执行顺序与最小通过集

1. **第一波（无需任何前置）**：L0（A0-1…A0-5，本地 python3 可跑）+ L3（本地 node 可跑）→ 立即可得 AC-7 的静态侧与 D-A 的前端侧信号。
2. **第二波（需 CI）**：L1 + L2 + A8-01…A8-05 → 覆盖 AC-1 / AC-2b / AC-4 映射 / AC-8 单测 / AC-7。**CI 全绿是本轮的主干证据。**
3. **第三波（需 P-HOST）**：L4 → 覆盖 AC-3 / AC-4 端到端 / AC-5 / AC-8 运行时 / VC-1…VC-4。
4. **第四波（需 P-REPO + P-STORE）**：L5 → AC-6。

**最小通过集（P0 全绿）** = 第一波 + 第二波 + 第三波；缺第三波则 AC-3/AC-5 未验证，**不得**判本轮通过。

---

*End of TEST_PLAN — cpa-plugin @ CPA v7.3.5 适配*
