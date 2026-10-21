---
artifact: docs/IMPLEMENTATION_TRACE.md
stage: dev
workflow_id: prod-dev
workflow_version: 2
author: dev-director
date: "2026-10-21"
source_task_id: t_851f221c
design_ref: docs/DESIGN.md
prd_ref: docs/PRD.md
test_plan_ref: docs/TEST_PLAN.md
root_task_id: t_a8f34caa
---

# 实现 trace — cpa-plugin @ CPA v7.3.5

> 本文件是 dev 阶段的自测证据与 P0 对账结论，**不是**验收结论。
> 权威层级遵循 `docs/TEST_PLAN.md` §0.1：CI 是编译/单测/多平台的唯一权威，运行时是 AC-3/4/5/8 的唯一权威。
> 本文件不含任何 token / secret / 凭据；夹具全部为合成 fixture / fake host / `httptest`。

---

## 1. P0 — 工作树 WIP 对账（DESIGN §3–§7 逐条）

对账口径：**语义是否满足约束**，而不是文件是否存在。下表每个新增/改动文件都映射到一条约束；无法映射的已删除或不入库。

### 1.1 workbuddy（契约冻结侧，D7 先行）

| 文件 | 状态 | 映射约束 | 对账结论 |
|---|---|---|---|
| `workbuddy/quota.go` | 新增 | DESIGN §4.1–4.5、§14 ADR-001 | 符合：`Identifier()` 复用 `providerName`；`Describe` 声明 `supports_reset:false`；`FetchQuota` 走 `cachedAccountDetailsWithCallback`（同一 45s TTL 缓存 + 单飞 + limiter，`force=false`）；`ResetQuota` 恒 `success:false` 且零上游写；bucket 除法有 `size<=0` / NaN / Inf / 越界钳制 |
| `workbuddy/completion.go` | 新增 | DESIGN §5.2 | 符合：`accept()` 在单锁内查+插（单飞）；空/纯空白键拒绝且不记账；容量 4096 FIFO + 30m TTL 懒清理；去重只读 `RequestID`，从不读 `Metadata`；仅 `canceled`（执行器观测不到的终态）补记一次 |
| `workbuddy/quiesce.go` | 新增 | DESIGN §6.2–6.4 | 符合：`sync.Once` 幂等；停 checkin/keepalive 调度器 + loginStates janitor；原子落盘 model-catalog；`draining` 后 `plugin_draining`/`retryable:true`/`503`；`message` 本身是合法 JSON 对象以便宿主透传 `retryable` |
| `workbuddy/affinity.go` | 新增 | DESIGN §7、PRD 7.5 | 符合：无 session 不发 lookup；失败/不支持静默降级；探针 HTTP 200 只读；字段不全不发请求；面板路径无写回 |
| `workbuddy/main.go` | 改动 | DESIGN §6.2–6.3、§7 | 符合：`handleMethod` 新增 6 个 case（quota ×4 / request.complete / plugin.quiesce）；执行器与管理写入口 `guardDraining()`；janitor `for range ticker.C` 改为双分支 `select`；`cliproxyPluginShutdown` 保持 no-op（红线） |
| `workbuddy/checkin.go` | 改动 | DESIGN §6.3、TEST_PLAN A1-39 | 符合：新增 `stopCheckinScheduler()`，旧「deliberately no stopCheckinScheduler」注释改写并指明停止入口迁移至 quiesce |
| `workbuddy/management.go` | 改动 | DESIGN §7 | 符合：新增只读 `/affinity` 路由登记（无写方法） |
| `workbuddy/panel.html` | 改动 | DESIGN §7、VC-3 | 符合：affinity 为卡片内惰性字段；未绑定/不支持时不出错误块 |
| `workbuddy/panel.test.js` | 改动 | TEST_PLAN A3-01/03/04 | 符合：25 用例（既有 16 + affinity 4 + 其他）全绿 |
| `workbuddy/register_contract_test.go` | 新增 | DESIGN §9、TEST_PLAN A2-02 / A2-06 | **P0 补强项**：新增 `schema_version == pluginabi.SchemaVersion == 6` 断言；新增「新增 capability 键集合恰好等于 `{quota_provider, request_lifecycle_plugin}`」上界断言（多一个键即 FAIL） |
| `workbuddy/testdata/register.golden.json` | 新增 | DESIGN §9 | v7.2.30 侧 before 基线（`schema_version:1`、不含两新键）；**不重新采集**，加强的是断言而非基线 |
| `workbuddy/{quota,completion,quiesce,affinity,version}_test.go`、`test_helpers_test.go` | 新增 | TEST_PLAN A1-10…A1-25 / A1-30…A1-44 / A1-50 | 符合：每条断言都有对应用例（见 §2） |
| `workbuddy/go.mod` / `go.sum` | 改动 | DESIGN §3.3 | 符合：仅 `CLIProxyAPI/v7` + `golang.org/x/net` + `golang.org/x/sys` 变动（A0-3 输出为空） |

### 1.2 qoderwork（P6 跟随，复用 workbuddy 语义）

| 文件 | 状态 | 映射约束 | 对账结论 |
|---|---|---|---|
| `qoderwork/quota.go` | 新增 | §4 + §14 | 符合：与 workbuddy 同构；`providerName` = `qoderwork`；`cycleEnd` 由 `ExpiresAt`（epoch ms）戳入 Packages |
| `qoderwork/completion.go` | 新增 | §5.2 | 符合：同构闸门；`canceled` 记录进同一 usage sink |
| `qoderwork/quiesce.go` | 新增 | §6 | 符合：同构；落盘义务为**空集**（`TestQoderworkQuiesceHasNoOnDiskState` 如实断言，不新造状态文件） |
| `qoderwork/affinity.go` | 新增 | §7 | 符合：同构只读 |
| `qoderwork/billing.go` / `cache.go` / `host_bridge.go` | 改动 | §4.5、§14 ADR-001 | 符合：`*WithCallback` 变体把 RPC 信封的 `host_callback_id` 透传到 `host.http.do`；面板调用仍走原路径（行为不变） |
| `qoderwork/main.go` / `checkin.go` / `management.go` / `panel.html` | 改动 | §6、§7 | 符合：与 workbuddy 同构 |
| `qoderwork/version_test.go` | 新增 | TEST_PLAN A1-50/51/52 | 符合：默认值 / registry / README 三项全覆盖（本轮首次补齐） |
| `qoderwork/{quota,completion,quiesce,affinity}_test.go`、`register_contract_test.go`、`test_helpers_test.go`、`testdata/register.golden.json` | 新增 | 同上 | 符合：与 workbuddy 同构，golden 断言同样含 A2-02 / A2-06 |
| `qoderwork/go.mod` / `go.sum` | 改动 | §3.3 | 符合（A0-3 输出为空） |

### 1.3 仓库级

| 文件 | 状态 | 映射约束 | 对账结论 |
|---|---|---|---|
| `scripts/check-version-single-source.py` | 新增 | DESIGN §12 P1、TEST_PLAN A0-2 | 符合：三项检查（代码默认值 / registry / README 无版本字面量），`CHANGELOG*` 与 `docs/` 豁免 |
| `scripts/go-docker.sh` | 新增 | DESIGN §10.2 P-ENV | 符合：本机无 Go 工具链时的本地复跑通道（固定 `golang:1.26` 容器）；**CI 仍是唯一权威**，不作为验收证据替代 |
| `.github/workflows/build.yml` | 改动 | DESIGN §12 P1 | 符合：test job 增加 `check-version-single-source` 与 `node --test panel.test.js`（仅当文件存在） |
| `docs/{PRD,DESIGN,API_DESIGN,VISUAL_SPEC,TEST_PLAN}.md` | 新增（随 PR 入库） | 本卡输入 | 非本卡产出，但契约需要可追溯，故随实现一并入库 |
| `.baseline-capture/` | **不入库** | — | 历史临时快照目录（与 §3–§7 无映射），保持未跟踪状态 |

**结论**：所有新增/改动文件均可映射到 §3–§7（或 §9/§12 的版本治理与契约回归）；无「映射不上」的遗留实现。

---

## 2. 自测 trace（断言 ID → 命令 → 退出码 → 关键输出）

执行环境：本机无 Go 工具链（`command -v go` 空），L1/L2 经 `scripts/go-docker.sh` 在 `golang:1.26`（实测 `go version go1.26.10 linux/arm64`）容器内执行；CI 复跑同一批命令。

| 断言 | 命令 | 退出码 | 关键输出（脱敏） | 结论 |
|---|---|---|---|---|
| A0-1 | `python3 scripts/validate-registry.py registry.json` | 0 | `OK registry.json: 2 plugin(s), schema_version=1` | PASS |
| A0-2 | `python3 scripts/check-version-single-source.py` | 0 | `OK version single-source: qoderwork=0.4.1, workbuddy=0.9.3 (VERSION == code default == registry; READMEs clean)` | PASS |
| A0-3 / A0-4 | `git diff --unified=0 -- '*/go.mod' \| grep -E '^[+-][^+-]' \| grep -vE 'CLIProxyAPI/v7\|golang\.org/x/(net\|sys) '` | 1（无输出） | 空 | PASS（边界内） |
| A0-5 | `git status --porcelain` + 凭据模式扫描（`sk-`/`ghp_`/`AKIA`/`PRIVATE KEY`） | 0 | 无命中；无 `config.yaml*`/`.env*`/`auths/`/`*.pem` | PASS |
| A1-01…A1-03 | `go test -run 'TestRegister' ./...`（workbuddy, qoderwork） | 0 | `--- PASS: TestRegisterGoldenMatchesContract`、`TestRegisterAdvertisesSchemaSixAndNewCapabilities`、`TestRegisterAdvertisesOnlyImplementedCapabilities` | PASS |
| A1-10…A1-19 | `go test -run 'TestQuota' ./...`（两插件） | 0 | workbuddy 15 条 / qoderwork 16 条 `--- PASS`（含 `TestQuotaResetIssuesNoHostCalls`、`TestQuotaFetchForwardsHostCallbackID`、`TestQuotaFetchReusesCreditsCache`、`TestQuotaBucketFromCreditsZeroSizeIsNotNaN`、`TestQuotaFetchFailureIsSanitized`） | PASS |
| A1-20…A1-25 | `go test -run 'TestCompletion\|TestRequestComplete' ./...` | 0 | `TestCompletionGateIsSingleFlight`、`TestCompletionGateRejectsEmptyKeyWithoutRecording`、`TestCompletionGateExpiresByTTL`、`TestCompletionGateEvictsByCapacity`、`TestRequestCompleteNeverReadsMetadataKeys`、`TestRequestCompleteIgnoresObservedOutcomes`、`TestRequestCompleteRecordsCanceledOnce` 全 PASS | PASS |
| A1-30…A1-38 | `go test -run 'TestPluginQuiesce\|TestDraining\|TestQuiesceMethod\|TestShutdownExport' ./...` | 0 | `TestPluginQuiesceIsIdempotent`、`TestPluginQuiesceStopsBackgroundTimers`、`TestPluginQuiesceStopsLoginStatesJanitor`、`TestPluginQuiesceFlushesState`、`TestPluginQuiesceReportsFlushFailureWithoutPanicking`、`TestDrainingRejectsNewExecutorWork`、`TestDrainingRejectsManagementWrites`、`TestDrainingKeepsPanelReachable`、`TestQuiesceMethodIsRegisteredAsHandled`、`TestShutdownExportStaysNoop` 全 PASS | PASS |
| A1-39 | `grep -rn 'no stopCheckinScheduler' workbuddy qoderwork` | 1（无输出） | 空 | PASS |
| A1-40…A1-44 | `go test -run 'TestAffinity\|TestPanelHasNoAffinityWritePath' ./...` | 0 | 6 条 `--- PASS`（两插件各 6 条） | PASS |
| A1-50…A1-52 | `go test -run 'TestDefaultVersion\|TestRegistryVersion\|TestReadme' ./...` | 0 | `TestDefaultVersionMatchesVersionFile`（两插件）、`TestRegistryVersionMatchesVersionFile`、`TestReadmeHasNoVersionLiteral` PASS | PASS |
| A2-01…A2-06 | 同 A1-01 命令 | 0 | golden 比对通过；`schema_version` 断言走 `pluginabi.SchemaVersion` 常量且 == 6；新增键集合上界命中 | PASS |
| A3-01…A3-04 | `cd workbuddy && node --test panel.test.js` | 0 | `ℹ tests 25 / pass 25 / fail 0` | PASS |
| L1 全量 | `scripts/go-docker.sh workbuddy test ./... -count=1` | 0 | `ok github.com/sliverkiss/workbuddy-plus 5.094s` | PASS |
| L1 全量 | `scripts/go-docker.sh qoderwork test ./... -count=1` | 0 | `ok github.com/Sliverkiss/cpa-plugin/qoderwork 0.013s` | PASS |
| L1 vet | `scripts/go-docker.sh <plugin> vet ./...` | 0 | 无输出 | PASS |
| L1 race | `scripts/go-docker.sh <plugin> test -race ./... -count=1` | 0 | workbuddy `ok ... 6.380s`；qoderwork `ok ... 1.041s` | PASS |
| A8-01…A8-05 | CI `Build` workflow on PR #1（run `39536628694`，重跑后） | 见 §2.1 | 14 success / 2 failure / 1 skipped | **2 个 freebsd-amd64 交叉编译 job 红**，原因见 §5（外部 action 死链，与本次改动无关） |
| AC-2c / AC-3 / AC-4 运行时 / AC-5 / AC-8 运行时 / VC-1…VC-4 | 需宿主 v7.3.5 | — | — | **BLOCKED(P-HOST)** |
| AC-6（A5-01…A5-07） | 需两新仓 + 商店登记 | — | — | **BLOCKED(P-REPO / P-STORE)** |

断言总数：L0 5 项、L1 44 条命名断言、L2 6 条、L3 4 条 —— 除 L4/L5 外全部 PASS。

### 2.1 本轮（dev-director 复验）独立重跑的证据

上一轮 trace 的 CI 行只写「待 CI 收口」，**未核对实际结论**；本轮由 dev-director 亲自复跑并抓取 CI 结论，口径如下（全部为本机实跑，非引用）：

- 本地 Go 通道：`scripts/go-docker.sh` → `go version go1.26.10 linux/arm64`（`golang:1.26` 镜像）
- `scripts/go-docker.sh workbuddy test ./... -count=1` → exit 0，`ok github.com/sliverkiss/workbuddy-plus 4.953s`，`--- PASS` 355 行
- `scripts/go-docker.sh qoderwork test ./... -count=1` → exit 0，`ok github.com/Sliverkiss/cpa-plugin/qoderwork 0.011s`，`--- PASS` 48 行
- `scripts/go-docker.sh {workbuddy,qoderwork} vet ./...` → 两者 exit 0，无输出
- `scripts/go-docker.sh {workbuddy,qoderwork} test -race ./... -count=1` → workbuddy `ok 6.343s`、qoderwork `ok 1.043s`，均 exit 0
- `python3 scripts/validate-registry.py registry.json` → exit 0，`OK registry.json: 2 plugin(s), schema_version=1`
- `python3 scripts/check-version-single-source.py` → exit 0，`OK version single-source: qoderwork=0.4.1, workbuddy=0.9.3 (...)`
- `cd workbuddy && node --test panel.test.js` → `tests 25 / pass 25 / fail 0`
- `grep -rn 'no stopCheckinScheduler' workbuddy qoderwork` → 零命中（exit 1）
- 红线 5：`cliproxyPluginShutdown` 两侧均为注释化 no-op；红线 6：`ResetQuota` 两侧均 `Success: false`
- 契约对称性：两侧 `main.go` 的 `MethodQuota{Identifier,Describe,Fetch,Reset}` / `MethodRequestComplete` / `MethodPluginQuiesce` 六个 case 与 `quota_provider` / `request_lifecycle_plugin` 两个 capability 字段一一对应
- 凭据扫描：`git diff main...HEAD` 全部改动文件对 `sk-` / `ghp_` / `AKIA` / `PRIVATE KEY` / `Bearer <token>` 零命中；无 `config.yaml*` / `.env*` / `auths/` / `*.pem` 入库

**qoderwork 无 `panel.test.js`** 属 TEST_PLAN A3-01 的既定条件口径（「仅当 `panel.test.js` 存在时触发」），不构成缺口；其面板侧 A1-44 静态断言（`TestPanelHasNoAffinityWritePath`）已存在并通过。


---

## 3. 阶段状态（DESIGN §12）

| 阶段 | 状态 | 依据 |
|---|---|---|
| P0 WIP 对账 | 完成 | §1 映射表；两处 golden 断言强度缺口已补（A2-02 / A2-06）；qoderwork P3 已从零补齐 |
| P1 版本治理 | 完成 | A0-2 退出 0；A1-50…A1-52 全绿 |
| P2 SDK 升级 | 完成（CI 通道 1 项外部阻塞） | A0-3 输出为空；CI 14 success / 2 failure / 1 skipped —— 2 项 failure 为 freebsd-amd64 交叉编译的外部 action 死链，见 §5 |
| P3 能力补齐 | 完成 | A1-10…A1-44 全绿；A1-39 零命中 |
| P4 拆仓与发布单元 | **BLOCKED(P-REPO)** | `fanchcho/workbuddy-cpa-plugin` / `fanchcho/qoderwork-cpa-plugin` 在 GitHub 侧不存在；按卡内约束不得自行改造现有单仓结构 |
| P5 发布端到端 | **BLOCKED(P-REPO / P-STORE)** | 依赖 P4 产物 + 管理端 store source 登记 |
| P6 qoderwork 跟随 | 完成 | 与 P3 同判据，两插件同构 |
| P7 CodeBuddy Intl | 非目标（D6） | 不在本卡 |

---

## 4. 残余风险

| ID | 风险 | 现状 |
|---|---|---|
| R3 | `request.complete` 的 `Metadata` 键集未实证 | 已规避：去重只依赖 `RequestID`，并有反例用例锁定（A1-23 / A4-08c 待运行时复核） |
| R4 | schema 6 不再转义的反向假设 | 只能由 AC-3 运行时判据确认 → BLOCKED(P-HOST) |
| R8 | draining 错误体形态 | 单测锁定了 envelope 形状与合法 JSON `message`；下游 503 透传需运行时（A4-05e）确认 |
| R9 | 宿主版本漂移被代跑 | 本机宿主 8.0.35，未用于任何验收取证 |
| R10 | 未提交 WIP 被误当已验证 | 已按 §1 逐文件对账，语义不足处已改/补 |
| — | ADR-001 残余差异（`FetchQuota` 不使用 per-auth 代理） | 设计已知差异，本轮不承诺出口 IP 一致性 |

---

## 5. CI 外部阻塞项：freebsd-amd64 交叉编译（P-CI，需决策）

### 5.1 现象

CI run `39536628694`（PR #1，`feat/cpa-7.3.5`，重跑后）job 结论：

- `success` 14：`Test (workbuddy)`、`Test (qoderwork)`、两插件 × {linux/amd64, linux/arm64, darwin/amd64, darwin/arm64, windows/amd64} 共 10 个原生构建、两插件 `windows-arm64 (go-cross)`
- `failure` 2：`workbuddy freebsd-amd64 (go-cross)`、`qoderwork freebsd-amd64 (go-cross)`
- `skipped` 1：`Release`（非 tag 触发，符合预期）

两个 failure 的报错完全一致，且发生在编译**之前**：

```
##[error]Command failed with exit code 8: wget -q https://download.freebsd.org/releases/amd64/14.3-RELEASE/base.txz
```

### 5.2 根因（已实证）

1. `https://download.freebsd.org/releases/amd64/14.3-RELEASE/base.txz` 现返回 **HTTP 404**；`/releases/amd64/14.3-RELEASE/` 目录同样 404。现存版本为 `14.4 / 14.5 / 15.0 / 15.1`。
2. 该 URL 由 `go-cross/cgo-actions@v1` 内部**硬编码**（其 `dist/index.js`：`wget -q https://download.freebsd.org/releases/${os_arch}/14.3-RELEASE/base.txz`），`action.yml` **没有任何输入可以覆盖** FreeBSD 版本或镜像地址；仓库仅 `v1.0.0 / v1.0.1 / v1.1.0` 三个 tag，默认分支同样是 14.3。
3. 14.3 的原始比特在所有可达镜像上均已下架（`ftp.freebsd.org` 301→404、清华镜像 404），因此**无法取得与既有产物同源的 sysroot**。
4. 该 failure 与本次改动**无关**：freebsd-amd64 目标在 `main` 的 `build.yml` 中即已存在（本轮未增删目标）；失败点在 sysroot 下载，尚未进入任何本项目代码的编译；两个插件表现完全相同。

### 5.3 为什么本轮不自行修复

可选修法都要改动发布产物的构建基线，属于需要拍板的决定，超出 dev 阶段可自行推断的范围：

| 方案 | 影响 | 为何未执行 |
|---|---|---|
| 把 sysroot 从 14.3 提到 14.4/14.5 | 抬高 freebsd 产物的最低运行版本（ABI 下限） | 改变发布产物兼容性，需批准 |
| 从交叉矩阵移除 freebsd-amd64 | 产物从 7 个 zip 降到 5 个 | 直接违反 DESIGN §8「7 zip + checksums.txt 不变」与 D5 |
| 用 `wget` shim 重写到 14.4 | 同第一行（仍是 14.4 sysroot） | 只是把同样的兼容性变更藏进 CI 脚本，不解决决策问题 |

**请求决策（P-CI）**：freebsd-amd64 采用 (a) 升 sysroot 至 14.4 并接受 ABI 下限抬高、(b) 从发布矩阵移除该目标（需同步改 DESIGN §8 的 7-zip 契约）、还是 (c) 暂时接受该 leg 红并继续推进 L1/L2/L3 与 L4 验收。

### 5.4 对验收的影响

- 不受影响：AC-1 的 L0/L1/L3 部分（`Test` job 两插件全绿）、AC-2、AC-4 的 L1 部分、AC-5 的 L1 部分、AC-7、AC-8 的 L1 部分。
- 受影响：AC-1 的 L8（多平台产物）目前**不完整** —— 5 个原生平台 + windows-arm64 交叉已产出，freebsd-amd64 缺失。

