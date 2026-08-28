# 图谱中心辐射改版 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 图谱改为"架构总览+演进历程"中心双锚辐射布局，语义边阈值动态校准（平均度≈1），节点慢速漂移可点击（撤掉冻结）。

**Architecture:** 后端 `buildGraph` 的 sem 阈值从固定 0.75 改为按候选分布反推（目标边数≈0.45n，钳 [0.55,0.75]）；前端删类目焦点环改中心双锚、位移钳 ±12→±1.5、回退 a3f928a 的冷却冻结（保留 b1718c4 错误分支停 rAF）。

**Tech Stack:** Go（internal/gui）、原生 JS/CSS（web/）。

**Spec:** `docs/2026-08-27-graph-center-radial-redesign.md`（用户已定稿：动态阈值、中心双锚、慢速漂移不冻结）

## Global Constraints

- 动态阈值：每次出图按 top-3 候选分布反推，目标平均度 ≈1（边数≈0.45×节点数）；钳制 [0.55, 0.75]；<10 条目直接用下限 0.55；n>2000 走既有护栏跳过；fail-open 语义不变。
- 中心双锚：架构总览/演进历程（is_dir）居中 ±120px 强锚定；删除类目焦点环——类目只管配色不管位置。
- 慢速漂移：展示期位移钳 ±1.5px/帧 + 积分速度 ×0.5 档（实施时实测微调）；预跑 900 tick 仍用原速度；**不冻结**——用户明确否决静止。
- 必须保留：b1718c4 的 refreshGraph 失败分支停 rAF/断 observer（错误分支资源回收，与冻结之争无关）；alpha 地板 0 与速度死区 0.02（a3f928a 已改，不回归）。
- 既有行为不回归：user-select:none、悬停 fade、分层模式（>400）、R4 双击跳转、sem 边样式（.edge.sem）。
- a3f928a（前端 sem 样式+冻结）尚未独立评审——Task B 评审时并入核对（sem:150 弹簧、边 class 挂法 kind!=="struct"、冻结回退点）。
- 提交纪律：工作区有他人 WIP，git add 只收本任务文件/hunk（diff→awk→git apply --cached）。

---

### Task A: 后端 sem 阈值动态校准

**Files:**
- Modify: `internal/gui/graph.go`（sem 通道，:115-150 附近）
- Test: `internal/gui/api_graph_test.go`（改写/追加用例，CRLF 行尾）

**Interfaces:**
- Consumes: 现有 sem 通道（d56a66b：top-3、配对去重、kind="sem"、n>2000 护栏）。
- Produces: `semThreshold(cands []float64, nodeCount int) float64`（纯函数，可单测）；
  buildGraph 签名不变。

**实现口径（钉死）：**
- 收集全部条目的 top-3 候选相似度（≤3n 个分值）。
- 目标：去重后边数 ≈ 0.45×nodeCount → 候选分值按降序取第 ceil(0.45×n×2) 名的分值作阈值
  （每个候选被两端各提名一次，×2 换算）；候选不足时取候选最小值。
- 钳制 [0.55, 0.75]；nodeCount<10 → 直接用 0.55。
- 既有 d56a66b 的 0.75 常量删除。

- [ ] **Step 1: 失败测试**

`api_graph_test.go` 追加（或改写 TestGraphSemanticEdges 的阈值依赖）：
- `TestSemThresholdCalibration`（可直接测纯函数或经 buildGraph）：构造 20 节点、候选分值已知分布（如均匀 0.5~0.9），断言产出阈值 ≈ 使边数≈9（0.45×20）的分位值且在 [0.55,0.75] 内；
- 全低分库（候选全 0.3）：断言零 sem 边（阈值被钳到 0.55 后无候选过线）；
- 全高分库（候选全 0.9）：断言阈值 ≤0.75 且 top-3 上限仍生效；
- <10 节点小库：阈值=0.55。

- [ ] **Step 2: 跑红** `go test ./internal/gui/ -run TestSemThreshold -v` → FAIL

- [ ] **Step 3: 实现** semThreshold 纯函数 + sem 通道改用动态阈值（候选收集在打分循环里顺手做，不新增第二趟 O(n²)）。

- [ ] **Step 4: 跑绿 + 全包** `go test ./internal/gui/ -count=1` 全绿

- [ ] **Step 5: Commit**
```bash
git add internal/gui/graph.go internal/gui/api_graph_test.go
git commit -m "feat(gui): 图谱 sem 阈值动态校准——目标平均度≈1 分位反推，钳制 [0.55,0.75]"
```

---

### Task B: 前端中心双锚布局 + 慢速漂移

**Files:**
- Modify: `web/app.js`（图谱页区段：gLayoutFoci :2784-2788、gTick :2799+、gFrame :2847 附近、gInitGraph :2892+、graphReset；均以锚点为准，行号可能漂移）
- Modify: `web/style.css`（如需中心锚点视觉强调，可选）

**Interfaces:**
- Consumes: Task A 的动态阈值边数据（前端零改动消费）；a3f928a 的 sem:150/.edge.sem/alpha 地板 0/速度死区。
- Produces: 无新对外符号（gLayoutFoci 语义改变、新增内部常量 CENTER_ANCHOR_GAP=120、MAX_STEP=1.5、SPEED_SCALE=0.5）。

**实现口径（钉死）：**
- **删类目焦点环**：gLayoutFoci 的 ring 逻辑删除；gTick 里 CLUSTER 聚拢力删除（含 CLUSTER 常量）。
  类目只剩 catColor 配色用途。
- **中心双锚**：两个 is_dir 节点（按 title ∈ {架构总览, 演进历程} 找）——初始布点在 (W/2-120, H/2) 与
  (W/2+120, H/2)；每帧施加回中力（k=0.01 档，目标即上述两点），用户可拖走但松手缓慢回中；
  无双锚（项目无 wiki 目录条目）时跳过锚定逻辑，纯向心+边弹簧。
- **慢速漂移**：位移钳 `Math.max(-12, Math.min(12, …))` → ±1.5（常量 MAX_STEP）；积分速度整体
  ×0.5（SPEED_SCALE，n.vx*=0.82 之后 n.vx*=0.5 或合并阻尼 0.41——实施者选可读性好的写法，
  报告中说明）；预跑 900 tick 用原速度（预跑阶段 MAX_STEP 仍为 12，预跑结束切展示档）。
- **回退冻结**：gFrame 的"alpha≤0.02 且无拖拽停帧"分支删除，恢复永动 rAF（页面守卫
  state.menu!=="graph" || !gV 保留）；pointerdown 的 rAF 重启分支如无必要一并回退
  （但 b1718c4 错误分支停 rAF 三行必须保留）。
- 分层模式：展开类目时新成员初始位置从"类目球心"改为"其可见邻居质心（无邻居则中心）"。

- [ ] **Step 1: 布局与限速实现**（按上三条口径改；常量集中在图谱区段顶部并注释语义）

- [ ] **Step 2: node --check** `node --check web/app.js` 过

- [ ] **Step 3: CDP 验证**（隔离 okd + headless Edge；隔离纪律照旧：全套 OK_*_HOME export、直写文件播种、严禁 ok init/setup、真实环境零接触）
  - 形态：90 条目库（含 wiki 双锚 + 手写向量让 sem 边存在）——截图，断言：双锚节点屏幕坐标距 stage 中心 <200px；类目焦点环常量已删（grep 无 CLUSTER）；
  - 漂移：CDP 采样连续 300 帧，断言最大节点位移 ∈ (0, 1.5]px/帧；rAF 存活（不冻结）；
  - 点击：对移动中的节点模拟 pointerdown/up 20 次，选中成功率 100%；
  - 回归：拖拽后松手回中（双锚）/悬停 fade/分层（>400 种子库）展开收起/R4 双击跳转/搜索；
  - 新旧截图对照（旧 5 坨 vs 新中心辐射）存 .superpowers/sdd/。

- [ ] **Step 4: Commit**
```bash
git add web/app.js web/style.css
git commit -m "feat(gui): 图谱中心双锚辐射布局——删类目焦点环，限速 ±1.5 慢速漂移（撤冻结）"
```

---

### Task C: 收口——a3f928a 复审、文档、知识库、测试包

**Files:**
- Modify: `docs/2026-08-27-gui-graph-page-requirements.md`（验收记录追加改版段）
- Modify: `docs/changelogs/2.22.3.md`（图谱条目补改版口径，只收自己 hunk）
- 知识库《GUI 图谱页》（ok add --force，控制器执行或指导）

- [ ] **Step 1: a3f928a 并入复审**——确认 sem:150/边 class 挂法/alpha 地板/死区在 Task B 回退冻结后仍正确（评审在 Task B 评审中已完成则只记录结论）。
- [ ] **Step 2: 全量验证** `go test ./internal/gui/ -count=1` 绿、`node --check` 过、`go build ./...` 过。
- [ ] **Step 3: 文档**：需求文档验收记录追加（改版三项 + CDP 数据）；changelog 图谱条目补"中心辐射布局/语义边动态阈值/慢速漂移"口径（他人 hunk 不碰）。
- [ ] **Step 4: Commit**
```bash
git add docs/2026-08-27-gui-graph-page-requirements.md docs/changelogs/2.22.3.md
git commit -m "docs: 图谱中心辐射改版验收记录 + changelog"
```
- [ ] **Step 5: 知识库 + 测试包**（控制器）：wiki《GUI 图谱页》add --force 改写改版口径 + wiki mark；`python scripts/build.py --test` 重打测试包（若输出被占用按先例处理）。

---

## Self-Review 记录

- Spec 覆盖：动态阈值→Task A；中心双锚+慢速漂移→Task B；收口（复审/文档/wiki/测试包）→Task C。
- 命名一致性：semThreshold/CENTER_ANCHOR_GAP/MAX_STEP/SPEED_SCALE 仅 Task A/B 内部，无跨任务依赖。
- 已知取舍：冻结回退是用户明确裁决（不要静止）；b1718c4 错误分支回收与冻结无关须保留；无双锚项目退化为向心+边弹簧（合理形态）；CLUSTER 删除后纯孤立节点靠斥力+向心平衡在外围（用户认可"没关系就靠边"）。
