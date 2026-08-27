# 图谱语义相似边 + 永动调参 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 图谱页加语义相似边（向量 top-3 近邻）让同域条目成簇；用调参消除小球永动微抖（不引入冻结-唤醒状态机）。

**Architecture:** 后端 `buildGraph` 增加 sem 边通道（读项目索引库 vectors 表，`embed.Cosine` 两两相似度，每条目 top-3 且 ≥0.75，去重）；前端加 `.edge.sem` 样式与弹簧原长，物理三处调参（alpha 地板归零、速度死区）。

**Tech Stack:** Go（internal/index + internal/gui）、原生 JS/CSS。

## Global Constraints

- 用户已定参数：sem 边 top-3、余弦阈值 0.75、灰绿点线样式、弹簧原长 150。
- fail-open：未配置 embedding / 无向量 / 索引库打不开 → 静默无 sem 边，图退化为现状，不得报错。
- 永动走 B 方案（用户选定）：只调参，不引入冻结-唤醒状态机、不改 rAF 循环结构。
- 图谱节点 id = file 名的既有契约不变；sem 边计入 deg（骨架判定 deg>=6 随之更准确——有意为之）。
- 既有行为不回归：user-select:none、悬停 fade、分层模式、R4 跳转。
- 提交纪律：工作区有他人未提交改动，git add 只收本任务文件/hunk（diff→awk→git apply --cached）。

---

### Task 1: 后端 sem 边

**Files:**
- Modify: `internal/index/db.go`（或 query.go，按现有向量读取代码就近）——新增全量向量读取
- Modify: `internal/gui/graph.go`——buildGraph 加 sem 通道
- Modify: `internal/gui/api.go`（apiGraph 打开索引库传入）
- Test: `internal/gui/api_graph_test.go`（追加 sem 用例）

**Interfaces:**
- Consumes: `embed.Cosine(a, b []float32) float64`（internal/embed/embed.go:130）；vectors 表 schema（internal/index/db.go:35，`filename TEXT PK, dim INTEGER, blob BLOB`）；现有向量 blob 解码代码在 internal/index/query.go:260 附近（语义检索扫表现状，复用其解码方式）。
- Produces:
  - `func (db *DB) AllVectors() (map[string][]float32, error)`（index 包；条目无向量则不在 map 里）
  - buildGraph 签名改 `buildGraph(entries []*entry.Entry, vecs map[string][]float32) graphData`（vecs nil 时跳过 sem）
  - 新边 kind `"sem"`，计入 deg，计入既有 edges 排序

**实现口径（钉死）：**
- 对每条有向量的条目 A：与其余所有有向量条目算 Cosine，取相似度 ≥0.75 的前 3 名（按相似度降序，不足 3 条有几条算几条）。
- 配对去重：A→B 与 B→A 只留一条（按文件名升序定向 source<target）。
- n>2000 时跳过 sem 计算（O(n²×dim) 护栏，静默）。
- apiGraph 打开索引库的方式照仓内既有做法（先找 retrieve/其他端点怎么 index.Open；索引库路径在 store 项目目录下，implementer 自查 store 包）。打不开/无 vectors 表 → vecs=nil 继续。

- [ ] **Step 1: 失败测试**

`api_graph_test.go` 追加 `TestGraphSemanticEdges`：夹具在 mkProject 后往项目索引库直接写 vectors（参 propose_test.go:53 的 raw 开库做法）——A/B 向量同向（cosine=1）、C 与 A/B 正交、D 无向量；断言：边集含 A—B 的 sem 边且只一条、不含 A—C（0<0.75）、D 无 sem 边、deg 计入 sem。另跑 `TestGraphNoVectors`：不写 vectors 时响应与 Task 1 现状完全一致（无 sem 边，不报错）。

- [ ] **Step 2: 跑红**

Run: `go test ./internal/gui/ -run TestGraphSemantic -v` → FAIL（无 sem 边）

- [ ] **Step 3: 实现**

AllVectors + buildGraph sem 通道 + apiGraph 接线（含 n>2000 护栏与全部 fail-open 分支）。

- [ ] **Step 4: 跑绿 + 全包回归**

Run: `go test ./internal/gui/ -count=1` → 全绿

- [ ] **Step 5: Commit**

```bash
git add internal/gui/graph.go internal/gui/api.go internal/gui/api_graph_test.go internal/index/db.go
git commit -m "feat(gui): 图谱语义相似边——向量 top-3 近邻（阈值 0.75）"
```

---

### Task 2: 前端 sem 边样式 + 永动调参

**Files:**
- Modify: `web/app.js`（图谱页区段）
- Modify: `web/style.css`

**Interfaces:**
- Consumes: Task 1 的 kind=="sem" 边（已在 gV.edges/adj 里，hotNode 邻居淡化与 R4 面板关联列表自动覆盖，无需改）。
- Produces: 无新符号（只改样式与物理参数）。

- [ ] **Step 1: sem 边样式与原长**

- style.css 加：`.g-stage .edge.sem { stroke:#86c5a5; stroke-dasharray:2 3; stroke-width:1; opacity:.8; }`
- app.js gTick 弹簧 LEN 表加 `sem:150`（与 struct:110/ref:170 并列）。
- 图例不变（图例是类目维度，不管边型）。

- [ ] **Step 2: 永动调参（B 方案，三处）**

- alpha 地板归零：gTick 里 `Math.max(alpha, 0.02)` → `Math.max(alpha, 0)`（注释更新：语义从"永动地板"改为"衰减至静止"）。
- 速度死区：积分处 `n.vx *= 0.82` 之后加 `if (Math.abs(n.vx) < 0.02) n.vx = 0;`（vy 同理）——亚像素速度直接清零，消除平衡态微抖。
- 拖拽/展开唤醒的 alpha 回热值（.25/.15/.35）不动。

- [ ] **Step 3: 验证**

- `node --check web/app.js` 过。
- 隔离 okd + headless Edge CDP（隔离纪律照旧：全套 OK_*_HOME export、直写文件播种、严禁 ok init/setup）：①90 条目库（无向量环境也行）——加载后 5 秒，采样连续 120 帧的最大节点位移，断言收敛后位移为 0 或亚像素（修复前恒有 1-12px 抖动）；②语义边：给隔离库索引写两组同向向量（可手写 blob 或直接 SQL），断言出现 sem 类边元素且样式类名正确；③回归：拖拽/悬停 fade/分层展开收起/R4 双击各抽一遍。
- 真机无法验证 sem 效果时如实标注（真实库的向量需 embedding 配置）。

- [ ] **Step 4: Commit**

```bash
git add web/app.js web/style.css
git commit -m "feat(gui): 图谱 sem 边样式 + 物理调参消永动（alpha 地板归零+速度死区）"
```

---

## Self-Review 记录

- 覆盖：用户两项需求各有任务；参数与 Global Constraints 一致（top-3/0.75/B 方案）。
- 命名一致性：AllVectors/buildGraph 新签名只影响 Task 1 内部与 apiGraph 单点；Task 2 无新符号。
- 已知取舍：sem 边计入 deg 会改变分层骨架构成（有意）；无向量环境图退化现状（fail-open）；n>2000 护栏触发时无 sem 边（记录即可）。
