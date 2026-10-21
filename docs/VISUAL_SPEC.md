---
artifact: docs/VISUAL_SPEC.md
stage: architect
workflow_id: prod-dev
workflow_version: 2
author: architect-director
date: 2026-09-18
prd_ref: docs/PRD.md
design_ref: docs/DESIGN.md
root_task_id: t_a8f34caa
source_task_id: t_a8f34caa
trigger: PRD 6.2 的 VC-1…VC-4 需要可执行的视觉判据
approval_required: false
---

# 视觉规格 — cpa-plugin @ CPA v7.3.5

> 上游：`docs/PRD.md` 6.2（VC-1…VC-4）。本文件把 VC 判据转成**可执行的文本证据口径**。
> PRD 明确：视觉验收由 `test-director` 以文本证据（DOM 文本 / 响应原文）落地，**不要求像素级比对**。
> 本文件不引入任何设计系统、字体、组件库、前端构建链（PRD N2 / N3 非目标）。
> 视觉基线参照 `docs/cpamp-workbuddy-panel.png`（既有截图）。

---

## 1. 范围声明

| 项 | 本轮 |
|---|---|
| 面板 UI 重写 / 引入构建链 | **不做**（N2） |
| 新增前端依赖 | **不做**（N3） |
| `esc()` 等**生成端**转义函数 | **保留不动**（PRD W7） |
| 唯一修复点 | 宿主在 `schemaVersion < 6` 时对管理响应 JSON body 做的逐字符串 HTML 转义（`managementResponseEscapesHTML`，`internal/pluginhost/management.go:357-359`） |
| 唯一新增可视元素 | 账号卡片内的 affinity 只读惰性字段（VC-3 允许，可选项） |

---

## 2. VC-1：实体串零命中（D-A 的核心视觉判据）

### 2.1 判据

面板任意渲染态下，**页面可见文本**中不出现以下实体串：
`&amp;`  `&lt;`  `&gt;`  `&quot;`  `&#39;`

含特殊字符的昵称 / plan / 错误信息按**原文**展示。

### 2.2 可执行取证（两级）

**一级：响应原文（不依赖浏览器）**

1. 构造含特殊字符的账号：昵称 `A&B<C>D'E"D`，plan `<Pro>`（测试夹具，**不含真实凭据**）。
2. 请求 `GET /v0/management/plugins/<id>/accounts`，取**原始响应 body**（不经任何 JS 处理）。
3. 断言：body 中该字段为**原文**（含 `&`、`<`、`>`、`'`、`"` 字面字符），
   且 body 中**零命中** `&amp;` / `&lt;` / `&gt;` / `&quot;` / `&#39;`。
4. 反例判据：**若**命中实体串 → `managementResponseEscapesHTML(schemaVersion)` 仍为 true → 插件上报的 `schema_version` 不是 6。

**二级：渲染文本（可选，VM 内）**

`workbuddy/panel.test.js`（`node:test` + `node:vm`，现有 17 个用例）中新增断言：
把一级的响应原文喂给面板的解析入口，断言进入渲染路径的数据中该字段仍为原文（证明前端未做二次转义或二次解码）。

### 2.3 证据边界

`schema_version = 6` 无独立宿主端点暴露，AC-2(c) 只能靠 AC-3 的通过**间接证明**（PRD 已声明该边界）。因此 **VC-1 同时承担 AC-2 的运行时证据**，其取证必须记录：请求时间、插件版本、宿主版本（`CLIProxyAPI Version: 7.3.5`）、响应原文片段（原始字符，不截图）。

### 2.4 反向风险（R4）核验口径

| 假设 | 实读结论 | 断言 |
|---|---|---|
| 前端存在「依赖实体解码」的隐式逻辑 | **不存在**：`workbuddy/panel.html:661` 为 `JSON.parse(text)`（裸解析）；`qoderwork/panel.html:591` 为 `r.json()`（裸解析）；两处 `esc()` 均为**生成端**转义 | 回归断言：渲染路径输入 = 响应原文，输出可见文本 = 同一原文（无双解码） |

---

## 3. VC-2：无视觉回归

### 3.1 判据（结构级，非像素级）

对照 `docs/cpamp-workbuddy-panel.png`，以下结构不变：

| 区域 | 不变内容 | 锚点（workbuddy/panel.html） |
|---|---|---|
| 左侧导航 | 存在且项名不变 | — |
| 页头 | 「账号面板」 | — |
| 工具栏 | 「刷新数据 / 全部签到 / 自动签到」三个按钮存在且文案不变 | — |
| 账号卡片 | 昵称 + plan 徽章 + 用量进度 + 已用/总量 + 签到状态 | `card(a)` 生成，第 387-416 行；容器 `<div class="card" data-region="…" data-auth="…">` |

### 3.2 可执行取证

对渲染后的 DOM 取文本（`document.body.innerText` 等）或对页面源码做结构断言，逐项断言上述文案/容器存在。
**不需要**像素比对，**不允许**以截图相似度作为通过条件。

### 3.3 允许的变化

- 新增 affinity 只读字段（VC-3 约束下）。
- 卡片内文案不变；不得改变现有按钮的 `data-action` / `data-auth-index` 属性（事件委托依赖它们，panel.html:805,829,851,924）。

---

## 4. VC-3：新增只读信息不破坏卡片

### 4.1 判据

若展示 affinity 只读信息：

| # | 约束 | 违反检测 |
|---|---|---|
| V3-1 | 以卡片内**惰性字段**呈现（卡片渲染后异步补齐），**不得**新增阻塞式请求 | 主渲染流程不 `await` affinity；`load()` 的时序不因 affinity 改变 |
| V3-2 | 查询失败时**静默降级**：不出现红色错误块、不出现 toast、不出现占位错误文案 | 注入失败（mock 返回错误 / 非 bound）后，DOM 中无错误块节点 |
| V3-3 | 非 `bound` 状态不渲染该字段（`unbound` / `ambiguous` / `unsupported` 均不展示） | 三态各断言 DOM 无 affinity 节点 |
| V3-4 | 未提供会话标识时整段跳过（不发请求） | 请求计数为 0 |
| V3-5 | 只读：面板不得提供任何改写亲和性的控件或请求 | DOM 无相关按钮；请求集中无写方法 |

### 4.2 可执行取证

在 `panel.test.js`（或 DOM 取证脚本）中：mock affinity 调用返回「成功 bound / 成功 unbound / 失败」三种，分别断言 V3-1…V3-5。文本证据即可（DOM 文本 + 请求记录），无需截图。

---

## 5. VC-4：状态语义颜色不反转

### 5.1 判据

`已禁用` / `耗尽` / `使用中` 三个徽章的既有色语义保持不变。

| 徽章 | 既有语义 | 约束 |
|---|---|---|
| `已禁用` | 非活跃态 | 色值与类名不变 |
| `耗尽` | 警示态 | 色值与类名不变 |
| `使用中` | 活跃态 | 色值与类名不变 |

### 5.2 可执行取证

对卡片渲染产出的 HTML 取徽章节点，断言其 class / 内联色值与升级前基线**逐字符一致**。
基线来源：升级前同一夹具渲染结果（先取基线再取新版，两者对比）。文本/源码级对比，不截图。

### 5.3 与 D-A 修复的边界

D-A 修复**只**改变管理响应是否被 HTML 转义，**不得**影响：
- 徽章色语义（VC-4）；
- 面板脱敏规则（PRD 7.3：错误分类保持脱敏）；
- `esc()` 生成端转义行为（W7）。

因此 `&` 等字符从「被宿主转义」变为「原文抵达前端」，前端 `esc()` 仍会在写入 HTML 时按需转义 —— 这是**正确的**双重职责分工：宿主不再越权改写 JSON，前端负责 DOM 安全。

---

## 6. 视觉证据清单（交 test-director 落 `docs/TEST_REPORT.md`）

| 编号 | 证据形态 | 内容 |
|---|---|---|
| E-V1 | 响应原文片段 | `/accounts` 原始 body 中特殊字符字段（原文，零实体串） |
| E-V2 | 渲染路径数据断言 | 响应原文 → 渲染路径输入，字段不变（panel.test.js 断言输出） |
| E-V3 | DOM 文本 | 左侧导航 / 页头 / 工具栏 / 卡片五项结构存在 |
| E-V4 | DOM 文本 | affinity 三态 + 失败态：无错误块、无非 bound 渲染、无写控件 |
| E-V5 | 源码/HTML 对比 | 三个徽章 class 与色值基线一致 |

**禁止**作为视觉证据：
- 「截图看起来对」；
- 「手工把 .so 塞进 plugins 目录后页面能打开」（只能证明 dlopen，不等于行为正确，PRD 6.3 反例条款）。

---

*End of VISUAL_SPEC*