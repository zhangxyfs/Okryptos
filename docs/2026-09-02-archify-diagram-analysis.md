# Archify「项目架构动态图」调研：对 OpenKnowledge wiki 可视化的借鉴价值

日期：2026-09-02
调研对象：archify 本地副本 `D:\develop\archify`，版本 `2.17.0-dev.1`（`archify/package.json`），上游为 [tt-a1i/archify](https://github.com/tt-a1i/archify)，MIT 许可（`LICENSE`：Copyright tt-a1i + Cocoon AI，基于 Cocoon-AI/architecture-diagram-generator MIT v1.0，见 `archify/SKILL.md` frontmatter `based_on`）
调研目的：评估能否把 archify 的"生成项目架构动态图"能力借鉴进 OpenKnowledge，作为项目 wiki 的可视化呈现

> 独立第三方调研。文中路径均相对 `D:\develop\archify\`；`file:line` 引用基于上述本地版本，上游更新后行号可能漂移。

---

## 0. 结论先行

**archify 不是代码分析工具，而是一台"确定性图形编译器"**：AI agent（Cursor / Claude Code / Codex CLI / OpenCode）在聊天中阅读仓库、手工产出一份 typed JSON IR，archify（纯 Node.js ESM，零运行时依赖）把这份 JSON 校验、布局、渲染成一个自包含的交互式 HTML/SVG 文件。所谓"动态图"是**阅读侧的交互与有限动画**（搜索、聚焦、上下游追踪、路径探测、引导故事、trace 动画、PNG/SVG/WebM 导出），不是实时更新，也不是从源码自动推导拓扑——**拓扑全部由 LLM 作者负责，archify 只做校验与渲染**。

对 OpenKnowledge 的借鉴结论：**渲染产物形态（自包含 HTML + vanilla JS viewer）和"LLM 产 IR、确定性渲染、git 证据校验"的分层模式高度契合 OK 的 agent 工作流，可低成本接入；但不要指望它解决"自动从代码生成图"的问题，OK 的 wiki 生成流程正好补这一环。**

---

## 1. archify 是什么

一句话：**archify 是一个以 Agent Skill 形式分发的 Node.js 图表渲染与校验系统——agent 产出 typed JSON IR，它把 IR 确定性地编译成自包含的交互式 HTML/SVG，支持 architecture / workflow / sequence / dataflow / lifecycle 五种图。**

关键事实：

- **形态**：不是服务、不是库，而是一个 npm private 包 + CLI（`archify/package.json`：`"bin": {"archify": "./bin/archify.mjs"}`，`"type": "module"`，`engines.node >= 18`），通过 `npx skills add tt-a1i/archify` 安装为 AI 编码助手的 Skill（`README.md`）。
- **技术栈**：纯 Node.js ESM `.mjs`，**无运行时依赖**——`package.json` 只有 devDependencies（ajv、parse5、saxes、simple-icons），渲染主路径只用 Node 标准库。核心代码约 1.2 万行：CLI 1990 行（`archify/bin/archify.mjs`）、共享几何/布线 1423 行（`renderers/shared/geometry.mjs`）、architecture 渲染器 1078 行（`renderers/architecture/render-architecture.mjs`）、viewer 模板 14787 行（`assets/template.html`）、workflow 编译器 4400 行（`renderers/workflow/workflow-compiler.mjs`）。
- **核心分工**（`README.md` 开头）："Agents produce typed JSON IR; Archify deterministically compiles it into HTML/SVG." LLM 负责语义与事实，archify 负责 schema 校验、几何布局、渲染、交付验收。
- **许可**：MIT（`LICENSE`、`archify/package.json` `license` 字段），另有 `THIRD_PARTY_NOTICES.md`。对 OK 无法律障碍，复用时保留版权声明即可。

## 2. 动态图功能机制拆解

### 2.1 入口与触发方式

- 唯一入口是 CLI `archify/bin/archify.mjs`，命令集（`bin/archify.mjs:15-34` usage，`1943-1989` dispatch）：`render / compare / deliver / preview / validate / migrate / inspect / check / visual-check / guide / brands / examples / doctor / demo`。
- 核心命令：`archify render <type> <input.json> [output.html]`（`bin/archify.mjs:18`）；`validate` 做带诊断码的校验，`deliver` 是"冻结 JSON 快照 → 渲染 → 原子写入 HTML → 输出 SHA-256 回执"的最终验收（`archify/SKILL.md` "Delivery" 节）。
- **触发方是 AI agent 而非最终用户**：`archify/SKILL.md` 是给 agent 读的操作手册（"Fast authoring path"：选图型 → 读 schema 和示例 → 写候选 JSON → validate → deliver）。没有 GUI、没有 watch 模式、没有 API 服务。

### 2.2 输入：LLM 手工产出的 JSON IR，无 AST/静态分析

这是最需要澄清的一点：**archify 自身完全不解析源码**。没有 tree-sitter、没有 AST、没有依赖扫描。

- 输入是按 JSON Schema 定义的 IR，五种图各一份 schema（`archify/schemas/architecture.schema.json` 等 + `common.schema.json`）。
- architecture 图的顶层模型（`schemas/architecture.schema.json` properties）：`meta / layout / components / boundaries / connections / cards`。
  - **节点** `components[]`：`id, type, label, sublabel, tag, brand, sources[], row/col 或 pos/size`。`type` 是七类语义枚举：`frontend / backend / database / cloud / security / messagebus / external`（`schemas/common.schema.json` `$defs.componentType`）。
  - **边** `connections[]`：`from, to, label, variant, fromSide/toSide, route(auto|straight|orthogonal-h|orthogonal-v), via[], labelAt`（`schemas/architecture.schema.json` connections）。
  - **布局** `layout`：只有 `mode: "grid"` 一种，节点给 row/col 或显式 pos（同上 layout 定义；`renderers/architecture/grid.mjs`）。
- **仓库证据（repository evidence）是"校验"不是"提取"**（`archify/references/authoring-contract.md:179-187`）：agent 读仓库后把事实写进 `meta.repository`（GitHub URL + 40 位 commit SHA）和 `component.sources[]`（repo 相对路径 + 起止行号，每组件至多 3 条，schema 中 `sources` 定义）；archify 用 `git` 命令核对 revision 存在、路径合法（防逃逸，禁 `.git`）、文件在指定 revision 存在、行号不超界（`renderers/shared/repository-evidence.mjs:84` `verifyRepositoryEvidence`，全文 235 行）。合约明令 "Record only evidence you actually verified. Never infer runtime causality from file proximity or naming alone"（authoring-contract.md:186-187）。README 中"traced mco-org/mco at 9f1a1cf"的案例，tracing 动作是 agent 做的，archify 只验证钉住的证据。

### 2.3 "动态图"具体指什么

不是动画图表库，也不是实时数据。三层含义，全部在**生成的自包含 HTML 内部**实现：

1. **交互式阅读器（viewer runtime）**：`assets/template.html`（14787 行，内联 CSS + vanilla JS，无 D3/Cytoscape/Mermaid，无外部请求）。能力见 `archify/references/viewer-runtime.md` "Exploration"：暗/亮主题切换（localStorage + URL override，`template.html:5559` `Archify.theme`）、pan/zoom、Node Finder 搜索、聚焦节点的 Semantic Passport（作者声明的上下游事实 + 可复制深链）、Route Probe（两个端点间在**作者声明的有向边**上的最短路径，"never infers a route from geometry"）、Semantic Lens（按节点/边类型过滤）、Semantic Radar（小地图）。
2. **有限动画**：`meta.animation: "trace"` 为可选项（`schemas/common.schema.json` animation 枚举 `["trace","none"]`；静态是默认）。实现极轻：渲染器只给元素打 `data-animate="..." style="--step:N"`（`renderers/shared/cli.mjs:169-176` `animateAttr`），动画本体是模板里的 CSS，且被 WebM 录制的 6 秒上限约束。
3. **引导故事（guided views）**：`meta.views` 至多 5 个章节，每章 = `{id, label, focus: [nodeId...], note}`（`schemas/common.schema.json` `$defs.guidedViews`）；viewer 派生出章节轨道、跟随镜头、逐拍导航（viewer-runtime.md "Guided views and story"）。约束同样是"不得从几何/顺序推断拓扑"。

### 2.4 数据流与产物

```
agent 读仓库/需求 → 手写 diagram.json（IR）
   → archify validate（JSON Schema + 跨集合校验，如关系 id 唯一 shared/cli.mjs:87
      validateRelationshipIds、guided views 引用的节点必须存在 :112 validateGuidedViews、
      几何诊断：边穿节点/标签碰撞/走廊歧义 geometry.mjs clean*Problems 系列）
   → 渲染器生成 inline SVG（render-architecture.mjs；自动布线、端口散布
      automaticPortSpread、标签避障均在 geometry.mjs）
   → writeDiagram 把 SVG/cards/meta 注入 template.html（shared/cli.mjs:53）
   → 单文件自包含 HTML（示例 ~725KB，docs/gallery/artifacts/production-deployment.architecture.html）
   → viewer 内可导出 PNG/JPEG/WebP/SVG/WebM 及 1200×630 share card
      （viewer-runtime.md "Canonical exports"，导出时剥离全部 viewer 状态）
```

另有 `compare` 命令：对两份 architecture JSON 做结构化 diff，产出 Before/Delta/After 三联图（`delta/architecture-delta.mjs`，canonical 序列化后逐字段比对 added/removed/changed/moved/rerouted），用于"合并前评审架构变更"。

## 3. 对 OpenKnowledge 的可借鉴性评估

技术栈差异摆明：archify = Node.js ESM + LLM 作者 IR + 自包含 HTML viewer；OK = Go 单二进制后端 + `web/` 下 vanilla JS 前端，wiki 条目由 openknowledge-wiki 技能驱动 agent 生成、经 `ok add` 录入（`internal/wiki/wiki.go` 管 git 游标/分支，`internal/wiki/diff.go` 管结构变化摘要）。两边都是"agent 生产内容、系统负责校验与呈现"的模式，这是契合点。

### 3.1 可直接复用

1. **整个 archify 作为可选外部渲染器**（最低成本路径）。OK 的 wiki 流程里 agent 反正要读代码、写条目；让它额外产出一份 archify architecture JSON（带 `meta.repository` + `sources`），OK 侧检测本机 `node >= 18` 即调 `node bin/archify.mjs deliver architecture wiki.arch.json out.html`。产物是**零依赖自包含 HTML**，OK 前端只需一个链接/新标签页即可呈现，连 iframe 适配都不用做。MIT 许可无障碍（保留 `LICENSE` / `THIRD_PARTY_NOTICES.md`）。
2. **节点/边 IR 模型本身**。`components/connections` 的 schema 非常薄（七类语义节点 + 有向带标签边 + grid 布局），即便不走 Node 路径，这个模型定义也值得照抄为 OK 自己的"架构图条目"格式。
3. **仓库证据校验思路**（`repository-evidence.mjs`，235 行）：条目事实绑定 `commit SHA + 文件路径 + 行号`，渲染/交付时用 git 核对。OK 已有 per-branch 游标与落后计数（`internal/wiki/wiki.go` State/BranchCursor、`status.go` CheckStatus），把 wiki 条目的 sources 与 commit 绑定做"条目是否过期"判定，几乎是现成机制的延伸。

### 3.2 需重写（思路可借鉴，代码不可搬）

1. **若要去掉 Node 依赖**，需用 Go 重写：JSON Schema 校验（archify 用 ajv，dev 依赖；其 zero-install 路径有自写的共享校验，见 `shared/cli.mjs:87/112`）、grid 布局 + 自动正交布线 + 标签避障（`geometry.mjs` 1423 行是全部渲染器共享的核心资产）、SVG 生成（`render-architecture.mjs` 1078 行）。只移植 architecture 单图型、砍到"grid + 直线/正交布线 + inline SVG"，工作量可控在两千行 Go 以内；五图型全搬（尤其 workflow 编译器 4400 行约束求解）不现实也不必。
2. **viewer 交互若内嵌进 `web/` 现有页面**（而非独立 HTML），需要把 template.html 里 theme/search/focus/route 等模块拆出来改造——它们是按"独占了整个文档"写的（`template.html:8` 起整页 IIFE），直接嵌入 OK 的 SPA 会有全局状态冲突，宜只取交互思路、按 OK `web/app.js` 风格重写。
3. **compare/delta 三联图**：`architecture-delta.mjs` 的 canonical-diff 思路与 OK `internal/wiki/diff.go` DiffSummary 互补（一个 diff 图语义，一个 diff 文件树），若做"wiki 架构图随分支演进"功能可借鉴其 canonical 序列化比对，代码同样是 JS 需重写。

### 3.3 不建议借鉴

1. **workflow-compiler（4400 行）、品牌图标体系**（`generated-brand-marks.mjs` 2003 行 + simple-icons + `brands capture` 远程抓取流程）：体量与 OK 的需求完全不成比例。
2. **其重提示词工程**（`SKILL.md` + `references/authoring-contract.md` 数百条 authoring invariant）：那是为它自己的"agent 盲画几何"问题服务的；OK 有自己的技能体系，只需借"IR schema + validate 诊断码"这一层。
3. **90+ 测试文件、49 轮视觉演进 research 文档**：证明其成熟度，但都不是可移植资产。

### 3.4 障碍清单

- **Node ≥18 运行时**：OK 目前是单 Go 二进制分发（`dist/ok.exe` 等），引入 archify 意味着要么检测+提示装 Node，要么内嵌重写（3.2 节）。这是最大的集成障碍。
- **不解决"自动从代码生成图"**：archify 的拓扑 100% 来自 LLM 作者（2.2 节）。OK 若想要"扫描代码结构自动生成"，这一环仍由 wiki 技能/agent 承担（OK 现状本就如此），archify 只接管渲染与校验。
- **npm private 包**：不在 npm registry，需 vendored 或 git 引用；`npx skills add` 是它的分发方式。
- viewer 无移动端适配（viewer-runtime.md 明示 "This is not a mobile product feature"），OK 若在意移动浏览需注意。

## 4. 建议的集成方案

**方案 A（推荐，一期，1~2 天工作量）**：archify 作为可选外部渲染器接入 wiki 流程。

1. openknowledge-wiki 技能在生成/更新 wiki 后追加一步"架构图条目"：agent 依据刚扫描的代码结构 + git 历史，产出 archify architecture JSON（`meta.repository` 钉当前 HEAD SHA，`components[].sources` 指向关键文件——正好复用 wiki 流程已收集的事实）。
2. `ok` CLI 增加（或 wiki 技能脚本化）一步：探测 `node --version >= 18` → 调 `archify deliver architecture wiki.arch.json <state>/wiki-arch.html`；无 Node 则跳过并提示，fail-open（与 `DiffSummary` 的 fail-open 风格一致，`internal/wiki/diff.go:14`）。
3. `web/` 前端在 wiki/项目页加一个"架构图"入口，新标签页打开该自包含 HTML——viewer 的主题/搜索/追踪/导出全部免费获得，OK 侧零 viewer 代码。
4. 顺手落地 3.1.3：把该 JSON 的 commit SHA 与 sources 记入 wiki 条目，`internal/wiki` 的落后计数天然给出"图已过期 N commits"的信号。

**方案 B（二期，若 Node 依赖被证明不可接受）**：Go 端只移植 architecture 图型的最小渲染——IR schema（照抄 3.1.2）+ grid 布局 + 直线/正交布线 + inline SVG + 简化交互（pan/zoom/搜索），直接嵌进 `web/` 页面。范围严格限定单图型，预估 ~2k 行 Go；workflow/sequence/dataflow/lifecycle 不做。

**不建议**：试图把 archify 的 IR 生成环节替换成 OK 的自动静态分析。archify 的全部价值在"确定性渲染 + 证据校验 + 成熟 viewer"，拓扑 authored-by-LLM 是其设计前提（authoring-contract.md:186），逆着用会把最重的部分（语义建模）留给自己、把最成熟的部分（渲染）丢掉。
