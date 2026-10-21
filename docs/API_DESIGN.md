---
artifact: docs/API_DESIGN.md
stage: architect
workflow_id: prod-dev
workflow_version: 2
author: architect-director
date: 2026-09-18
amended: "2026-09-18 — ADR-001（docs/DESIGN.md §14）：Q1 的 HTTPClient 措辞作废，出站通道改为 req.HostCallbackID 的宿主回调上下文"
prd_ref: docs/PRD.md
design_ref: docs/DESIGN.md
root_task_id: t_a8f34caa
source_task_id: t_a8f34caa
trigger: 新增管理端可见接口面（quota.* 4 方法 + 宿主 3 条路由）与发布/registry 契约
approval_required: false
---

# 接口设计 — cpa-plugin @ CPA v7.3.5

> 上游：`docs/PRD.md`；机制依据：`docs/DESIGN.md` 第 4/6/7 节。
> 本文件只描述**接口面**（方法名、请求/响应形状、错误码、状态码）。数据映射的字段来源见 DESIGN 4.4。
> 不含任何 token / secret / 凭据。

---

## 1. 插件侧 RPC 方法（宿主 → 插件，`cliproxyPluginCall` 分派）

所有方法共用同一 envelope：`{"ok": bool, "result": <raw>, "error": {"code","message","retryable","http_status"}}`（`workbuddy/main.go:299-309`）。

### 1.1 新增：`quota.identifier` → `MethodQuotaIdentifier`

```jsonc
// request: {}  （宿主传 rpcEmptyResponse）
// result:
{"identifier": "workbuddy"}
```

- `identifier` **必须**等于既有 `providerName` 常量（`workbuddy` / `qoderwork`）。
- 宿主注册期回调一次用于建立路由键（`internal/pluginhost/rpc_client.go:167-177`）。
- 失败即视为未注册配额能力（`callPluginIdentifier` 返回空字符串）。

### 1.2 新增：`quota.describe` → `MethodQuotaDescribe`

```jsonc
// request:
{"plugin": { /* pluginapi.Metadata（宿主回填） */ }}

// result:
{
  "display_name": "WorkBuddy",
  "supported_providers": ["workbuddy"],
  "supports_reset": false
}
```

- `supports_reset` **恒 false**，与 1.4 的恒失败语义一致。
- 宿主在 `QuotaProviders()` 中优先用 `supported_providers`，为空时回落到 `identifier` / 插件 ID（`quota_provider.go:74-101`）。

### 1.3 新增：`quota.fetch` → `MethodQuotaFetch`

```jsonc
// request（宿主注入，插件只读）:
{
  "auth_index": "<host auth index>",
  "auth_id":    "<credential id>",
  "provider":   "workbuddy",
  "storage_json": "<base64 / provider-owned persisted auth>",
  "metadata":   { /* host-managed */ },
  "attributes": { /* routing */ },
  "host":       { /* HostConfigSummary */ },
  // 宿主为本次调用开启的回调上下文 id：插件只回传、不自铸（ADR-001）
  "host_callback_id": "<host callback context id>"
  // HTTPClient 为 json:"-" 的宿主注入接口，不在 wire 上，且被 sanitizePluginRequest 置 nil
  // → 物理不可达，不得依赖（原 Q1 措辞已作废，见 ADR-001）
}

// result:
{
  "subscription": {"plan": "Pro"},
  "summary": [
    {"key": "credits_remaining", "label": "可用积分", "value": 1234, "unit": "credits"},
    {"key": "credits_used",      "label": "已用积分", "value": 266,  "unit": "credits"}
  ],
  "groups": [
    {
      "displayName": "CN 资源包",
      "buckets": [
        {"window": "cycle", "remainingFraction": 0.8227, "resetTime": "2026-10-01T00:00:00+08:00",
         "description": "主资源包"}
      ]
    }
  ],
  "serverTimeOffsetMs": 0
}
```

硬约束（DESIGN 4.5）：

| # | 约束 | 违反后果 |
|---|---|---|
| Q1 | 出站请求**必须**经宿主为本次调用开启的回调上下文：`hostHTTPDoWithCallback(req, req.HostCallbackID)`（**只回传、不自铸** callback id） | 自铸 callback id 会与宿主 `CompleteRequestExcept`/`skipPluginID` 的嵌套规避设计对抗，产生嵌套回调歧义。**原措辞作废**：`req.HTTPClient` 带 `json:"-"` 且被 `sanitizePluginRequest` 置 nil，出进程插件下不可达 —— 依据见 `docs/DESIGN.md` **ADR-001** |
| Q2 | **必须**复用 credits 缓存与 `invalidateAccountCredits` 失效路径 | 配额查询绕开 TTL 会把上游刷爆 |
| Q3 | `total_size <= 0` 时 `remainingFraction` 取 `1.0`（或省略 bucket），**禁止** NaN/Inf | 管理端渲染出现 `NaN` |
| Q4 | `ResetTime` 为 RFC3339 字符串（宿主不解析） | — |
| Q5 | 不返回错误信息中含上游原始响应体（脱敏规则不变） | 违反 PRD 7.3 |

错误形状（envelope）：
```jsonc
{"ok": false, "error": {"code": "quota_upstream_unavailable", "message": "upstream billing unavailable", "http_status": 502}}
```

### 1.4 新增：`quota.reset` → `MethodQuotaReset`

```jsonc
// result（恒为成功 envelope，体内声明不支持）:
{"success": false, "message": "provider-managed"}
```

- **绝不触发上游写操作**（PRD 7.4）。宿主对 `success:false` 不做重试。
- **不发出任何 host 调用**：即宿主已为本次调用开启回调上下文（`req.HostCallbackID` 恒非空），也不使用；恒返回上列常量体，绝不触上游。

### 1.5 新增：`request.complete` → `MethodRequestComplete`

```jsonc
// request（宿主构造，随信封带 host_callback_id 供嵌套规避）:
{
  "RequestID": "9f2c...",
  "TraceID": "…",
  "SourceFormat": "chat-completions",
  "Model": "…",
  "RequestedModel": "…",
  "Stream": true,
  "Outcome": "canceled",        // succeeded | failed | rejected | canceled
  "StatusCode": 0,
  "Error": "",
  "StartedAt": "2026-09-18T01:02:03Z",
  "CompletedAt": "2026-09-18T01:02:09Z",
  "Metadata": { /* 宿主上下文快照，键集不承诺 */ }
}

// result: {} （宿主只用 ok/err，不解析结果体）
```

行为规约：

| # | 规约 |
|---|---|
| R1 | 幂等键**只**取 `RequestID`（`DESIGN.md` 5.2） |
| R2 | `TrimSpace(RequestID) == ""` → 不记账、不入表、返回 ok |
| R3 | 重复 `RequestID` → 直接丢弃并返回 ok（**不是**错误） |
| R4 | 不得读取/匹配 `Metadata` 的键名做去重判断 |
| R5 | 回调运行在 `context.WithoutCancel` 的独立 goroutine 上（`adapters_interceptors.go:190-224`）；实现不得假设请求上下文存活，也不得阻塞超过宿主侧调用超时 |
| R6 | 记账内容仅限：`Outcome`/`StatusCode`/`Error`/时间戳 + 从 `RequestID` 派生的关联信息 |

### 1.6 新增：`plugin.quiesce` → `MethodPluginQuiesce`

```jsonc
// request: {}   // result: {}
```

| # | 规约 |
|---|---|
| QS1 | **幂等**：重复调用不得 panic、不得重复 close 通道 |
| QS2 | 停表：checkin/keepalive 调度循环 + `loginStatesPruneInterval` janitor（DESIGN 6.2） |
| QS3 | 原子落盘：仅限**已有落盘路径**的状态（workbuddy model-catalog；qoderwork 为空集） |
| QS4 | 置 `draining` 标志后，新 executor / 管理写请求快速失败 |
| QS5 | **清理逻辑不得放进 `cliproxyPluginShutdown`**（保持 no-op） |

draining 错误形状（`http_status` 由宿主 `rpcError` 传导，`message` 须为合法 JSON 才能让下游 body 带字面 `retryable`）：

```jsonc
{"ok": false, "error": {
   "code": "plugin_draining",
   "message": "{\"error\":{\"message\":\"plugin is draining after hot-reload\",\"type\":\"server_error\",\"code\":\"plugin_unavailable\",\"retryable\":true}}",
   "retryable": true,
   "http_status": 503}}
```

> 实现必读：`envelopeError`（`workbuddy/main.go:305-309`）当前**缺 `Retryable` 字段**，须补齐；`errorEnvelopeWithStatus`（main.go:838-841）已支持 `http_status`。

### 1.7 新增：`host.affinity.lookup` → 插件 → 宿主（`hostCall`）

```jsonc
// request（三字段必填，宿主侧校验）:
{"provider": "workbuddy", "model": "<requested model>", "session_id": "<下游会话标识>"}

// result:
{"status": "bound", "auth_index": "3", "observed_at": "2026-09-18T01:02:03Z",
 "disabled": false, "unavailable": false}
// 或 {"status": "unbound" | "ambiguous" | "unsupported", "observed_at": "…"}
```

只读红线（DESIGN 7.2）：**不得**改写宿主亲和性；任何失败只在面板内静默降级。

### 1.8 变更汇总（注册响应）

| 字段 | 升级前 | 升级后 |
|---|---|---|
| `schema_version` | 1（`pluginabi.SchemaVersion` @ v7.2.30） | 6（同常量 @ v7.3.5） |
| `capabilities.quota_provider` | 不存在 | `true` |
| `capabilities.request_lifecycle_plugin` | 不存在 | `true` |
| `capabilities.websocket_response_observer` | 不存在 | **仍不存在**（PRD N1 非目标） |
| 其余 capabilities / metadata | — | 逐键不变（DESIGN 第 9 节 golden 回归） |

---

## 2. 宿主侧 HTTP 接口（管理端，可观测验收锚点）

| 方法 | 路径 | 用途 | 响应要点 | 代码锚点 |
|---|---|---|---|---|
| GET | `/v0/management/quota/providers` | 列出已注册配额提供者 | `{"providers":[{"plugin_id","provider","display_name","supported_providers","supports_reset"}]}`；AC-4 要求含 `workbuddy` 与 `qoderwork` | `server_management.go:83` → `plugin_quota.go:43-56` |
| POST | `/v0/management/quota/fetch` | 按凭据取配额 | body 需 `auth_index`；返回 `QuotaFetchResponse`（含 `groups[0].buckets[0].remainingFraction`） | `server_management.go:84` → `plugin_quota.go:59+` |
| POST | `/v0/management/quota/reset` | 重置配额 | 插件恒 `success:false` | `server_management.go:85` |
| GET | `/v0/management/plugins/:id/quota` | 按插件粒度读取 | 同 fetch 语义 | `server_management.go:42` |
| POST | `/v0/management/plugins/:id/quota` | 按插件粒度触发抓取 | — | `server_management.go:43` |
| DELETE | `/v0/management/plugins/:id/quota` | 按插件粒度重置 | 插件恒失败 | `server_management.go:44` |
| GET | `/v0/management/plugins/<id>/accounts` | **保留**（面板依赖） | 富字段：nickname / plan / region / packages 明细 / checkin | 插件 `managementRegistration()`(workbuddy/management.go:170) |
| GET | `/v0/management/plugins/<id>/credits` | **保留**（面板依赖） | `total_remain` / `total_used` / `total_size` / packages | 同处 175 行；AC-4 一致性对照基准 |
| GET | `/v0/resource/plugins/<id>/panel` | 面板页面 | 静态 HTML | 插件 resource 路由 |

**保留条款**（PRD 5.2 硬约束 2）：`/credits` 与 `/accounts` 属**并行保留**，不因引入 `QuotaProvider` 而删除或裁剪字段。

---

## 3. 发布与 registry 契约（D4 / D5）

### 3.1 宿主消费契约（不得偏离）

| 项 | 规则 | 锚点 |
|---|---|---|
| 版本来源 | `ReleaseVersion(release) = validPluginVersion(normalizeVersion(tag))`；`normalizeVersion` 仅剥离**前导一个** `v`/`V`；`validPluginVersion` = 非空 且 不以 `v` 开头 且 `^[0-9][0-9A-Za-z.+-]*$` | `github.go:112-122`、`version.go:26-32`、`registry.go:407-409` |
| 归档名 | `<id>_<version>_<goos>_<goarch>.zip` | `github.go:347-355` |
| zip 根条目 | `<id>.<so\|dylib\|dll>` | `install.go:318-320` |
| 校验和资产名 | **精确** `checksums.txt`；解析取每行前两列，不校验第二列文件名 | `github.go:330-336`、`checksum.go` |
| 落盘名 | `<id>-v<version><ext>`（宿主持有 `-v` 后缀反解插件 ID） | `install.go:371-373` |
| registry `schema_version` | `1`（`github-release`）| `registry.go:18-19,167-173` |
| registry `version` | 必须与 `ReleaseVersion(tag)` 相等 | `manifest.go:128-143` |
| install-by-version 的 tag 候选 | `[version, "v"+version]` | `plugin_store.go:471-480` |

### 3.2 本设计采用值

| 项 | 值 |
|---|---|
| tag | **裸 `vX.Y.Z`**（如 `v0.9.4`） |
| 归档名 | `workbuddy_0.9.4_linux_amd64.zip` 等（与现 CI 完全一致，**不改**） |
| 每仓 assets | 7 平台 zip（linux amd64/arm64, darwin amd64/arm64, windows amd64/arm64, freebsd amd64）+ `checksums.txt` |
| registry | 每仓单插件条目，`repository`/`homepage` 指向该仓 |
| module | `github.com/fanchcho/workbuddy-cpa-plugin` / `github.com/fanchcho/qoderwork-cpa-plugin`（DESIGN 8.3） |

### 3.3 registry.json 单插件模板（workbuddy 仓）

```jsonc
{
  "schema_version": 1,
  "plugins": [
    {
      "id": "workbuddy",
      "name": "WorkBuddy",
      "description": "<原描述，去掉 qoderwork 相关表述>",
      "author": "Sliverkiss",
      "version": "0.9.4",                       // 由 CI 在发布时写入，等于 VERSION
      "repository": "https://github.com/fanchcho/workbuddy-cpa-plugin",
      "logo": "<保持原 URL>",
      "homepage": "https://github.com/fanchcho/workbuddy-cpa-plugin",
      "license": "MIT",
      "tags": ["Provider", "OAuth", "CodeBuddy", "WorkBuddy", "Management"]
    }
  ]
}
```

`scripts/validate-registry.py` 必须在 CI 中继续执行（AC-1 / AC-7）。

---

## 4. 安全边界（接口层）

1. 任何响应体不得回显 `management_key` / `usage_report_key`（PRD 7.2）。
2. 错误分类保持脱敏：不上抛上游原始响应体与原生错误文本（PRD 7.3）；`quota.fetch` 的错误 `message` 用固定文案。
3. `ResetQuota` 无上游写操作（PRD 7.4）。
4. `host.affinity.lookup` 只读（PRD 7.5）。
5. 文档、测试夹具、CI 日志不得含真实凭据（PRD 7.1）。

---

*End of API_DESIGN*