# 管理页树类目分组 + wiki 结构化治理 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 管理页知识树按类目分组（需求 5）落地真实 GUI；openknowledge-wiki 技能落地"总览瘦身+演进历程滚动归档"写作纪律并完成本库历史数据迁移。

**Architecture:** 前端纯 JS 归组（数据已在 `MGMT.list` 全量缓存，`entrySummaryJSON.mtime` 已有，零后端改动）；交互行为以已定稿原型 `docs/prototypes/prototype-manage-terminal.html` 为实现参照（函数可移植，代码风格按 web/app.js 约定重写）。wiki 治理为技能文档修改 + 一次性数据迁移（走 `ok add --force` 正式写路径）。

**Tech Stack:** 原生 JS/CSS（web/app.js、web/style.css）、Go（仅 SKILL.md go:embed 重新构建验证）、ok CLI（迁移执行）。

## Global Constraints

- 规格来源：`docs/2026-08-23-manage-search-terminal-requirements.md` 需求 5 节；`docs/2026-08-23-wiki-structure-governance.md` 全文。冲突时以规格文档为准。
- 归组优先级：**归档 > 草稿 > 注入 > 类型**，每条目只出现一个目录。
- 类目顺序：**草稿固定置顶、归档固定沉底**；中间类目（注入/参考/笔记/踩坑/规则）按类内最新 `mtime` 降序。
- 默认展开：项目首次展开时——有草稿只展开草稿目录；无草稿展开类内最新 mtime 最大类目；用户手动状态优先。
- 参考目录子结构：架构总览（目录+文件双重身份，文件夹图标，子节点=章节条目，无"章节"中间层）→ 演进历程（文件节点，新设计历史图标）→ 其他（真目录收普通 reference）。章节归集 = tags 含 `wiki` ∩ type=reference ∩ 标题非架构总览/演进历程；`branch:` 差异条目按当前分支过滤排后。
- i18n：所有新文案中英双份，两字典键集合必须相等。
- 不回归：树拖拽分隔条、4s 轮询（pollManage 的 .search 焦点与 edBusy 守卫）、内联编辑三态、需求 2 搜索命令、需求 3/4 终端与布局。
- git commit 步骤需用户确认后执行（本会话纪律），执行时统一在计划末尾批量提交。
- 前端无 JS 测试框架，验证手段：`node --check` + Node 抽取式逻辑断言 + 无头 Edge 截图冒烟（`--headless=new --screenshot`）。

---

### Task 1: 树类目分组核心（归组 + 类目行 + 参考子结构）

**Files:**
- Modify: `web/app.js`（`renderTree`/`fillTree` 区域、I18N 字典、ICON 对象）
- Modify: `web/style.css`（类目行/双重身份节点样式）
- Reference: `docs/prototypes/prototype-manage-terminal.html`（`CATEGORIES`/`catRow`/`dualLeaf`/`specialLeaf`/`renderRefGroup` 可移植逻辑）

**Interfaces:**
- Produces: `CATEGORY_ORDER`（归组优先级数组）、`groupEntries(entries)` 返回 `{archived:[],draft:[],mandatory:[],rule:[],pitfall:[],note:[],reference:[]}`、`renderCatGroup(proj, key, entries)`、`renderRefGroup(proj, entries)`（含架构总览双热区节点、演进历程节点、其他子目录）、i18n 键 `catDraft/catMandatory/catReference/catNote/catPitfall/catRule/catArchived/catOther`、ICON 新增 `history`（16px stroke 时钟：圆圈+两指针，自绘，不用 emoji）。

- [ ] **Step 1: 抽取式失败断言**

写 `scripts/` 外临时断言脚本 `/tmp/test-tree-cat.js`（stub DOM/localStorage，从 web/app.js 抽取 `groupEntries`）：

```js
// 多维重叠只入归档；draft 只入草稿；mandatory 只入注入；reference 入参考
const g = groupEntries([
  {type:"rule",archived:true,draft:true}, {type:"rule",draft:true},
  {type:"note",mandatory:true}, {type:"reference",tags:["wiki"]},
]);
assert(g.archived.length===1 && g.draft.length===1 && g.mandatory.length===1 && g.reference.length===1);
assert(g.rule.length===0); // 重叠条目不再出现在 rule
```

Run: `node /tmp/test-tree-cat.js` → Expected: FAIL（groupEntries 未定义）。

- [ ] **Step 2: 实现归组与类目渲染**

按原型逻辑移植到 app.js：`fillTree` 项目展开后改按 `groupEntries` 结果渲染类目行（文件夹 SVG 图标 + 类名 + 计数徽标，空类目不渲染，类目间不互斥，`state.catOpen` 存展开态）；`renderRefGroup` 实现参考子结构（架构总览双热区：箭头/图标=展开收起、点名=`state.sel` 出详情；子节点=章节条目；演进历程文件节点；其他子目录收无 wiki 标签 reference；固定顺序 架构总览→演进历程→其他）。条目行徽标渲染（`badges()`/draft 琥珀/archived 淡化）复用现有函数不动。style.css 加 `.tn-cat`/`.leaf.dual` 样式（参照原型同名类）。

- [ ] **Step 3: 断言转绿 + 语法检查**

Run: `node /tmp/test-tree-cat.js` → PASS；`node --check web/app.js` → 无输出。

- [ ] **Step 4: 无头截图冒烟**

```bash
"/c/Program Files (x86)/Microsoft/Edge/Application/msedge.exe" --headless=new --disable-gpu --user-data-dir="$TEMP/edge-ok-smoke" --screenshot=/tmp/shot-task1.png --window-size=1500,950 "http://127.0.0.1:<port>/?token=<tok>"   # 需 okd 起真实 GUI；不可用则退化为对原型截图
```

预期：类目分组出现、空类目隐藏、参考子结构正确。（真机 okd 冒烟由用户确认后做；本步允许用原型截图代替并记录偏差。）

---

### Task 2: 动态排序 + 默认展开规则 + 兼容回归

**Files:**
- Modify: `web/app.js`（`fillTree` 排序与首展开逻辑、`pollManage` 守卫区）

**Interfaces:**
- Consumes: Task 1 的 `groupEntries`/类目渲染。
- Produces: `catLatest(entries)`（类内 max mtime）、`sortCategories(groups)`（草稿 pin 顶、归档 pin 底、中间按 catLatest 降序）、`applyDefaultCatOpen(proj, groups)`（`state.catDefaulted` 每项目只应用一次）。

- [ ] **Step 1: 失败断言**（并入 /tmp/test-tree-cat.js）

```js
// 排序：草稿置顶、归档沉底、中间按最新 mtime 降序
const order = sortCategories({mandatory:[{mtime:100}],reference:[{mtime:300}],note:[{mtime:200}],draft:[{mtime:50}],archived:[{mtime:999}]});
assert(deepEqual(order, ["draft","reference","note","mandatory","archived"]));
// 默认展开：有草稿→draft；无草稿→最新类目；归档不优先
assert(defaultCat({draft:[{mtime:1}],reference:[{mtime:9}]})==="draft");
assert(defaultCat({reference:[{mtime:9}],note:[{mtime:3}],archived:[{mtime:99}]})==="reference");
```

Run → FAIL（函数未定义）。

- [ ] **Step 2: 实现**

`fillTree` 归组后 `sortCategories` 定渲染顺序；项目首次非过滤态展开时 `applyDefaultCatOpen` 写 `state.catOpen`（有草稿→只开草稿；否则开 catLatest 最大组；其余收）；过滤态不触发、不标记 defaulted。确认 `pollManage`/`refreshManage` 的 `.search` 焦点守卫与 `edBusy()` 守卫仍先于 render 返回（读现有代码逐行核对，不改语义）。

- [ ] **Step 3: 断言转绿** + `node --check web/app.js`。

- [ ] **Step 4: i18n 键集合一致性脚本**（zh/en 两字典 keys diff 为空）。

---

### Task 3: 需求 5 全链路验收 + dist 同步

**Files:**
- Modify: `dist/web/app.js`、`dist/web/style.css`（cp 同步）

- [ ] **Step 1: 按规格文档需求 5 验收节逐条过**：多维重叠归组、mtime 排序变化（改 mock/真实条目 mtime 观察类目移动）、架构总览双热区、演进历程图标、无 wiki 项目只有"其他"、`/type rule` 过滤空类目隐藏、树拖拽/轮询/内联编辑回归。
- [ ] **Step 2:** `go build ./...`（SKILL.md 未改前应为绿，确认前端改动不影响 Go 构建）。
- [ ] **Step 3:** `cp web/app.js web/style.css dist/web/` 并记录。

---

### Task 4: openknowledge-wiki SKILL.md 写作纪律落地

**Files:**
- Modify: `internal/setupx/skills/openknowledge-wiki/SKILL.md`（分发源，go:embed）
- Modify: `C:/Users/Administrator/.agents/skills/openknowledge-wiki/SKILL.md`（用户级副本，与分发源保持同文）

**Interfaces:**
- Consumes: `docs/2026-08-23-wiki-structure-governance.md` 的"落地面（SKILL.md 修改点）"四款。

- [ ] **Step 1: 分发源增补三节**（按治理文档逐字落地）：
  - 全量流程：按顶层目录/主题域出条目；架构总览只写目录地图（一行一域+指针）。
  - 增量流程：按 `git diff --stat` 路径路由到主题域条目重写；演进历程追加前查阈值（正文>6000 字或里程碑>25 个）先归档（`演进历程-归档（<版本段>）`，tags `wiki,历史`）再重写。
  - 条目规范增补：主题域条目含"沿革"小节；迁移纪律（备份先行、add --force 正式写路径、幂等可重入）。
- [ ] **Step 2: 同步用户级副本**（diff 两文件确认一致）。
- [ ] **Step 3: `go build ./internal/setupx/`**（go:embed 重新打包验证）。

---

### Task 5: 本库 wiki 历史数据迁移

**Files:**
- 数据面：`C:/Users/Administrator/.openknowledge/projects/OpenKnowledge/knowledge/`（经 ok CLI 写入，不手改）

**Interfaces:**
- Consumes: 治理文档"历史数据迁移"章（含 20 条目主题域映射表、切点 v2.15、例外规则）。

- [ ] **Step 1: 备份**

```bash
"D:/software/OpenKnowledge/okd.exe" wiki status   # 确认在基准分支、无 diverged
# GUI 其他页导出 zip 或：
"D:/software/OpenKnowledge/okd.exe" list > /tmp/wiki-pre-migrate.txt
```

- [ ] **Step 2: 演进历程切分**：读现有 `演进历程.md` 正文 → v2.15 及之前段写临时文件 → `okd add --title "演进历程-归档（v1.0~v2.15）" --type reference --tags wiki,历史 --summary <老里程碑检索线索> --file <归档正文>` → 主条目重写（近期段+一行早期简述+指针，`--force`）。
- [ ] **Step 3: 架构总览重写**：逐段核对旧正文独有细节（缺失先搬进主题条目）→ `--force` 重写为目录地图。
- [ ] **Step 4: 校验**：`okd search 托盘` 等老里程碑关键词命中归档条目；`okd list` 条目数 = 20+1（归档新增）；GUI 树参考目录下架构总览子节点=主题域条目（真机确认）。
- [ ] **Step 5: `okd wiki mark`** 不动游标（迁移不改代码）；记录迁移摘要。

---

### Task 6: 收尾提交（需用户确认）

- [ ] **Step 1:** `git status` 列出全部改动面（web/、internal/gui 已提交与否、docs/、setupx）。
- [ ] **Step 2:** 向用户确认后按主题分批 commit（feat: 管理页树类目分组 / docs: 需求与原型 / feat: wiki 技能治理 + 迁移记录）。
- [ ] **Step 3:** 提醒沉淀检查：本批结构型变更已进 wiki（Task 5 顺带覆盖）。

## Self-Review 记录

- 规格覆盖：需求 5 结构/交互/验收 → Task 1-3；治理文档落地面/迁移/验收 → Task 4-5；两处文档交叉引用已建。
- 占位扫描：无 TBD；验证命令均具体。
- 类型一致：`groupEntries`/`sortCategories`/`catLatest`/`applyDefaultCatOpen` 命名全文一致；i18n 键清单完整。
- 已知取舍：真机 okd 冒烟需用户在场确认（okd 正在运行安装目录副本），Task 1 Step 4 允许原型截图替代。
