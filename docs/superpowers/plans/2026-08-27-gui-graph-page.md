# GUI 图谱页 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在 Web GUI 左侧导航"管理"下方新增"图谱"栏目，按项目渲染知识条目关系图（手写 SVG 力导向），支持项目切换、条目详情面板、关联条目双击跳回管理页定位，并在条目数 >400 时自动进入骨架/下钻分层模式。

**Architecture:** 后端 `internal/gui` 新增 `GET /api/graph?project=`（复用 resolveProject/withAuth/entry.Load，边提取新写在 `internal/gui/graph.go`）；前端把 `docs/prototypes/prototype-wiki-graph.html` 的引擎与交互移植进 `web/app.js` 新页面区段，模拟/布局状态放模块级对象以扛住 GUI 的整页 DOM 重渲。

**Tech Stack:** Go（net/http，无新依赖）、原生 JS + SVG（零依赖，照抄原型物理参数）、CSS（进 `web/style.css`）。

**Spec:** `docs/2026-08-27-gui-graph-page-requirements.md`（R1–R4、数据契约、验收标准以此为准）
**分层设计:** `docs/2026-08-27-wiki-graph-layered-design.md`（Task 5 依据）
**引擎规范源:** `docs/prototypes/prototype-wiki-graph.html`（物理参数与交互手感逐字沿用）

## Global Constraints

- 渲染路线锁定手写 SVG 原型；不得引入 Sigma/G6/WebGL 或任何 vendor 依赖（需求 §4 非目标）。
- 图谱节点 `id` = 条目 file 名（如 `GUI-图谱页.md`），不是标题——R4 的 `jumpToEntry(file, project)` 按 file 定位（需求 §2）。
- 类目（category）口径与管理页树一致：`web/app.js:1942-1946` `catKeyOf` 的优先级
  `archived > draft > mandatory > rule > pitfall > note > reference`，后端按同一优先级产出 category 键。
- 原型已修行为不得回归：画布 `user-select:none`（拖拽不出文本选中蓝框）、悬停时非邻居节点 `.fade` 淡化（opacity .15）。
- 双击判定必须走 `dblClick(h, key)` 时间戳检测（`web/app.js:1002-1007`），禁用原生 dblclick（重渲下不可靠，知识库沉淀坑）。
- 图谱页不做条目编辑、不做跨项目联合图、不做 4s 轮询（需求 §4 非目标）。
- 所有新增 HTTP 端点必须挂 `withAuth`（`internal/gui/api.go:140-148`），401 语义与现有端点一致。
- web/ 改完须同步 dist/web 才会进 exe 调试产物（`scripts/build.py:100-103` 自动拷贝；本地裸 `go build` 不同步，知识库沉淀坑——验收前跑 `python scripts/build.py` 或手动 `cp -r web dist/`）。

---

### Task 1: 后端 GET /api/graph 端点

**Files:**
- Create: `internal/gui/graph.go`
- Create: `internal/gui/api_graph_test.go`
- Modify: `internal/gui/api.go:73`（路由表加一行）

**Interfaces:**
- Consumes: `resolveProject(w, projectQuery)`（api.go:201-220）、`entry.Load(dir)`（internal/entry/entry.go，`[]*entry.Entry`，字段 Title/Type/Tags/Mandatory/Draft/Archived/Summary/Body/Path，`Path` 为绝对路径，file 名取 `filepath.Base(Path)`）、`writeJSON/writeErr`（api.go:172-180）、`withAuth`（api.go:140-148）。
- Produces:
  - `GET /api/graph?project=<名>` → 200 `{"nodes":[graphNode],"edges":[graphEdge],"categories":[string]}`
  - `graphNode`：`{id,title,type,tags,category,summary,is_dir,deg}`（JSON tag 用 snake_case 还是 camel 对照 entrySummaryJSON——该结构体用小写 `json:"file"` 风格，故用 `is_dir`/`deg`；前端 Task 3 按此消费）
  - `graphEdge`：`{source,target,kind}`，kind ∈ `struct|ref`
  - `buildGraph(entries []*entry.Entry) graphData`（graph.go，纯函数，可单测）

**边与字段口径（实现者照此写，勿自行发挥）：**
- `category`：按 catKeyOf 同优先级：`archived`→`"archived"`，`draft`→`"draft"`，`mandatory`→`"mandatory"`，type 为 `rule|pitfall|note` 时取 type，否则 `"reference"`。
- `isDir`：`type=="reference" && (title=="架构总览" || title=="演进历程")`（对齐前端 `refSubGroups` 标题约定，web/app.js:1575-1576）。
- `ref` 边：条目 A 正文 `Body` 中 markdown 链接 `](xxx.md)` 的文件名命中条目集里 B 的 file 名 → 边 A→B。正则：`]\(([^)\s]+\.md)\)`，对 `filepath.Base` 匹配。自环剔除。
- `struct` 边：tags 含 `wiki` 的 reference 条目挂目录节点——tags 同时含 `历史` 的挂 `"演进历程"`，其余挂 `"架构总览"`；仅当对应目录节点存在时生成。
- `deg`：每条边两端各 +1。
- `categories`：nodes 中出现的 category 去重，按固定序 `mandatory,rule,pitfall,note,reference,draft,archived` 过滤排列（未出现的跳过）。

- [ ] **Step 1: 写失败测试**

新建 `internal/gui/api_graph_test.go`（与 api_test.go 同包 `gui`；注意该目录测试文件是 CRLF 行尾）：

```go
package gui

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
)

type graphTestPayload struct {
	Nodes []struct {
		ID       string   `json:"id"`
		Title    string   `json:"title"`
		Type     string   `json:"type"`
		Tags     []string `json:"tags"`
		Category string   `json:"category"`
		IsDir    bool     `json:"is_dir"`
		Deg      int      `json:"deg"`
	} `json:"nodes"`
	Edges []struct {
		Source string `json:"source"`
		Target string `json:"target"`
		Kind   string `json:"kind"`
	} `json:"edges"`
	Categories []string `json:"categories"`
}

// writeGraphEntry 复用 api_readme_test.go:36 writeKnowledge 的同款文件格式
func writeGraphEntry(t *testing.T, dir, file, title, typ string, tags []string, body string) {
	t.Helper()
	fm := fmt.Sprintf("---\ntitle: %s\ntype: %s\ntags: [%s]\n---\n\n%s\n",
		title, typ, strings.Join(tags, ", "), body)
	writeKnowledge(t, dir, file, fm)
}

func TestGraphBasic(t *testing.T) {
	h, env := newEnv(t)   // 若 newEnv 签名不同以 api_test.go:29-59 为准
	_ = env
	mkProject(t, h, "p")  // 若签名不同以 api_test.go:62-74 为准
	dir := ""             // 取 mkProject 注册的 knowledge 目录（按 api_test.go 现成 helper 的实际返回调整）

	writeGraphEntry(t, dir, "架构总览.md", "架构总览", "reference", []string{"wiki"}, "总览")
	writeGraphEntry(t, dir, "演进历程.md", "演进历程", "reference", []string{"wiki"}, "历程")
	writeGraphEntry(t, dir, "模块甲.md", "模块甲", "reference", []string{"wiki"}, "参见 [乙](模块乙.md)")
	writeGraphEntry(t, dir, "演进历程（v2 段）.md", "演进历程（v2 段）", "reference", []string{"wiki", "历史"}, "段")
	writeGraphEntry(t, dir, "模块乙.md", "模块乙", "reference", nil, "正文")
	writeGraphEntry(t, dir, "坑一.md", "坑一", "pitfall", []string{"gui"}, "坑")

	srv := httptest.NewServer(h)
	defer srv.Close()
	status, body := do(t, "GET", srv.URL+"/api/graph?project=p", testToken, nil)
	if status != 200 {
		t.Fatalf("status=%d body=%s", status, body)
	}
	var g graphTestPayload
	if err := json.Unmarshal([]byte(body), &g); err != nil {
		t.Fatalf("json: %v", err)
	}
	if len(g.Nodes) != 6 {
		t.Fatalf("nodes=%d want 6", len(g.Nodes))
	}
	byID := map[string]int{}
	for _, n := range g.Nodes {
		byID[n.ID] = n.Deg
		if n.ID == "架构总览.md" && (!n.IsDir || n.Category != "reference") {
			t.Errorf("dir node: %+v", n)
		}
		if n.ID == "坑一.md" && n.Category != "pitfall" {
			t.Errorf("category: %+v", n)
		}
	}
	has := func(s, t2, k string) bool {
		for _, e := range g.Edges {
			if e.Source == s && e.Target == t2 && e.Kind == k {
				return true
			}
		}
		return false
	}
	if !has("模块甲.md", "模块乙.md", "ref") {
		t.Errorf("missing ref edge: %+v", g.Edges)
	}
	if !has("架构总览.md", "模块甲.md", "struct") {
		t.Errorf("missing struct edge 架构总览→模块甲")
	}
	if !has("演进历程.md", "演进历程（v2 段）.md", "struct") {
		t.Errorf("missing struct edge 演进历程→历史段")
	}
	if byID["模块乙.md"] != 1 || byID["模块甲.md"] != 2 {
		t.Errorf("deg: %v", byID)
	}
}

func TestGraphGuards(t *testing.T) {
	h, _ := newEnv(t)
	mkProject(t, h, "p")
	srv := httptest.NewServer(h)
	defer srv.Close()
	if s, _ := do(t, "GET", srv.URL+"/api/graph?project=p", "wrong-token", nil); s != 401 {
		t.Errorf("no token status=%d want 401", s)
	}
	if s, _ := do(t, "GET", srv.URL+"/api/graph", testToken, nil); s != 400 {
		t.Errorf("no project status=%d want 400", s)
	}
	if s, _ := do(t, "GET", srv.URL+"/api/graph?project=nope", testToken, nil); s != 404 {
		t.Errorf("bad project status=%d want 404", s)
	}
}
```

> 注：`newEnv`/`mkProject`/`do`/`testToken`/`writeKnowledge` 的确切签名以 `internal/gui/api_test.go:29-132` 与 `api_readme_test.go:36` 现状为准，照抄同文件里 `TestEntries` 类测试的调用方式适配；`mkProject` 若不返回 knowledge 目录路径，按其内部实现拼 `<OK_HOME>/projects/p/knowledge`。

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/gui/ -run TestGraph -v`
Expected: FAIL（`h.apiGraph` 未定义 / 404）

- [ ] **Step 3: 实现**

新建 `internal/gui/graph.go`：

```go
package gui

import (
	"path/filepath"
	"regexp"
	"sort"

	"github.com/<module>/internal/entry"   // module 名以 go.mod 为准
)

type graphNode struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	Type     string   `json:"type"`
	Tags     []string `json:"tags"`
	Category string   `json:"category"`
	Summary  string   `json:"summary"`
	IsDir    bool     `json:"is_dir"`
	Deg      int      `json:"deg"`
}

type graphEdge struct {
	Source string `json:"source"`
	Target string `json:"target"`
	Kind   string `json:"kind"`
}

type graphData struct {
	Nodes      []graphNode `json:"nodes"`
	Edges      []graphEdge `json:"edges"`
	Categories []string    `json:"categories"`
}

var mdLinkRe = regexp.MustCompile(`\]\(([^)\s]+\.md)\)`)

func graphCategory(e *entry.Entry) string {
	switch {
	case e.Archived:
		return "archived"
	case e.Draft:
		return "draft"
	case e.Mandatory:
		return "mandatory"
	case e.Type == "rule" || e.Type == "pitfall" || e.Type == "note":
		return e.Type
	default:
		return "reference"
	}
}

func hasTag(e *entry.Entry, tag string) bool {
	for _, t := range e.Tags {
		if t == tag {
			return true
		}
	}
	return false
}

func buildGraph(entries []*entry.Entry) graphData {
	nodes := make([]graphNode, 0, len(entries))
	idx := map[string]int{}
	files := map[string]bool{}
	for _, e := range entries {
		f := filepath.Base(e.Path)
		files[f] = true
		idx[f] = len(nodes)
		nodes = append(nodes, graphNode{
			ID: f, Title: e.Title, Type: e.Type, Tags: e.Tags,
			Category: graphCategory(e), Summary: e.Summary,
			IsDir: e.Type == "reference" && (e.Title == "架构总览" || e.Title == "演进历程"),
		})
	}
	var edges []graphEdge
	seen := map[string]bool{}
	add := func(s, t, k string) {
		if s == t || !files[s] || !files[t] {
			return
		}
		key := s + "→" + t + "→" + k
		if seen[key] {
			return
		}
		seen[key] = true
		edges = append(edges, graphEdge{Source: s, Target: t, Kind: k})
		nodes[idx[s]].Deg++
		nodes[idx[t]].Deg++
	}
	for _, e := range entries {
		f := filepath.Base(e.Path)
		for _, m := range mdLinkRe.FindAllStringSubmatch(e.Body, -1) {
			add(f, filepath.Base(m[1]), "ref")
		}
		if e.Type == "reference" && hasTag(e, "wiki") && !nodes[idx[f]].IsDir {
			if hasTag(e, "历史") {
				add("演进历程.md", f, "struct")
			} else {
				add("架构总览.md", f, "struct")
			}
		}
	}
	order := []string{"mandatory", "rule", "pitfall", "note", "reference", "draft", "archived"}
	used := map[string]bool{}
	for _, n := range nodes {
		used[n.Category] = true
	}
	var cats []string
	for _, c := range order {
		if used[c] {
			cats = append(cats, c)
		}
	}
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].Source != edges[j].Source {
			return edges[i].Source < edges[j].Source
		}
		return edges[i].Target < edges[j].Target
	})
	return graphData{Nodes: nodes, Edges: edges, Categories: cats}
}
```

`internal/gui/api.go` 两处改动：

路由表（api.go:73 附近）加一行：

```go
api("GET /api/graph", h.apiGraph)
```

handler（放 `apiEntries` 之后，api.go:712 附近）：

```go
// apiGraph GET /api/graph?project= —— 图谱页数据契约（docs/2026-08-27-gui-graph-page-requirements.md §2）
func (h *handler) apiGraph(w http.ResponseWriter, r *http.Request) {
	st, ok := h.resolveProject(w, r.URL.Query().Get("project"))
	if !ok {
		return
	}
	entries, err := entry.Load(st.KnowledgeDir())
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, buildGraph(entries))
}
```

> `handler` 类型名、`st.KnowledgeDir()` 以 api.go 现状为准（Handler/handler、resolveProject 返回值签名照抄 `apiEntries` 内部写法）。

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/gui/ -run TestGraph -v`
Expected: PASS（2 个测试函数全绿）；再跑 `go test ./internal/gui/` 全包确认无回归。

- [ ] **Step 5: Commit**

```bash
git add internal/gui/graph.go internal/gui/api_graph_test.go internal/gui/api.go
git commit -m "feat(gui): GET /api/graph 图谱页数据契约（nodes/edges/categories）"
```

---

### Task 2: 前端页面骨架——菜单、i18n、项目选择器

**Files:**
- Modify: `web/app.js`（I18N zh 块 :43-180、en 块 :182-319、ICON :328-341、MENUS :342-345、renderBody :4073-4089、侧栏缓存联动 :4051-4058；新增"图谱页"区段，放管理页区段之后，约 :2030 前）
- Modify: `web/style.css`（图谱页样式追加到文件尾）

**Interfaces:**
- Consumes: Task 1 的 `GET /api/graph`；`lazyPage`/`menuRender`（app.js:421-430）；`api()` helper。
- Produces（后续 Task 依赖的名字，必须一字不差）:
  - `let GRAPH = null` —— `{proj, data, err}` 页级缓存
  - `let graphProj = ""` —— 当前选中项目名
  - `const gV = {...}` —— 图谱视图全部可变状态（nodes 坐标/速度、view、drag、展开集合等，Task 3/5 填充）
  - `function loadGraph()` / `function refreshGraph()` / `function renderGraph()` —— 与 manage 页 `loadManage/refreshManage/renderManageLayout` 同构
  - `function graphReset(proj)` —— 切项目/重拉时重建 gV 与 DOM

- [ ] **Step 1: i18n / ICON / MENUS**

zh 块（:45 行附近）`misc:"其他",` 后加：

```js
    graph:"图谱", gPickProject:"项目", gSearch:"搜索条目标题 / tags…",
    gEmpty:"暂无条目", gBack:"返回总览", gFlat:"平铺", gFold:"分层",
```

en 块对应位置加：

```js
    graph:"Graph", gPickProject:"Project", gSearch:"Search title / tags…",
    gEmpty:"No entries", gBack:"Overview", gFlat:"Flat", gFold:"Layered",
```

ICON 表（:328-341）加（复用现有 stroke 风格，24×24 圆点+连线）：

```js
  graph:"<svg viewBox='0 0 24 24' fill='none' stroke='currentColor' stroke-width='2'><circle cx='6' cy='6' r='3'/><circle cx='18' cy='18' r='3'/><circle cx='18' cy='6' r='3'/><path d='M8.5 7.5 15 16M9 6h6'/></svg>",
```

> ICON 表项的实际存值形式（字符串/svg 对象）照抄相邻项现状调整。

MENUS（:342-345）manage 项之后插入：

```js
  { key:"graph", ico:ICON.graph },
```

- [ ] **Step 2: 页面区段骨架（web/app.js 管理页区段之后新增）**

```js
/* ================= 图谱页 ================= */
/* 需求 docs/2026-08-27-gui-graph-page-requirements.md；引擎移植自
   docs/prototypes/prototype-wiki-graph.html（物理参数逐字沿用）。
   关键集成约束：GUI render() 是整页 DOM 重建——一切模拟/布局/视图状态
   必须放模块级 gV，renderGraph 只按 gV 重建 DOM，坐标不丢。 */
let GRAPH = null;        // {proj, data, err}；loadGraph 惰性加载
let graphProj = "";      // 当前选中项目（页内独立，不同步管理页）
let gV = null;           // 视图状态（Task 3 填充：nodes/edges/byId/view/expanded/...）
let gProjects = null;    // 项目下拉数据 [{name,last_update}]

function loadGraph(){
  if(!gProjects){
    gProjects = [];
    api("/api/projects").then(ps=>{
      gProjects = (ps||[]).slice().sort((a,b)=>(b.last_update||0)-(a.last_update||0));
      if(!graphProj && gProjects.length) graphProj = gProjects[0].name;
      refreshGraph();
    }).catch(()=>{ gProjects = []; });
  }
  GRAPH = lazyPage(GRAPH, { proj:graphProj, data:null }, refreshGraph);
}
function refreshGraph(){
  if(!graphProj){ GRAPH = { proj:"", data:null }; menuRender("graph", true); return; }
  api("/api/graph?project="+encodeURIComponent(graphProj))
    .then(d=>{ GRAPH = { proj:graphProj, data:d }; graphReset(graphProj); })
    .catch(e=>{ GRAPH = { proj:graphProj, data:null, err:e.message }; })
    .then(()=>menuRender("graph", true));
}
function graphReset(proj){ gV = null; /* Task 3 填充：按 GRAPH.data 初始化视图状态 */ }
```

`renderBody` 分发（:4083 的 setup 分支前）加：

```js
  else if(state.menu==="graph"){
    loadGraph();
    renderGraph(main);
  }
```

侧栏缓存联动（:4051-4058 附近，照 manage 的写法）加：

```js
    if(m.key==="graph" && GRAPH) refreshGraph();
```

`renderGraph(main)` 骨架（Task 3 填引擎，本步先出工具栏 + 空态）：

```js
function renderGraph(main){
  const wrap = el("div","g-wrap");
  const bar = el("div","g-bar");
  const sel = el("select","g-proj");
  (gProjects||[]).forEach(p=>{
    const o = el("option"); o.value = p.name; o.textContent = p.name;
    if(p.name===graphProj) o.selected = true;
    sel.appendChild(o);
  });
  sel.onchange = ()=>{ graphProj = sel.value; GRAPH = null; gV = null; loadGraph(); render(); };
  bar.appendChild(sel);
  wrap.appendChild(bar);
  const stage = el("div","g-stage");
  if(GRAPH && GRAPH.err) stage.appendChild(el("div","g-empty", t("gEmpty")+" · "+GRAPH.err));
  else if(GRAPH && GRAPH.data && !GRAPH.data.nodes.length) stage.appendChild(el("div","g-empty", t("gEmpty")));
  wrap.appendChild(stage);
  main.appendChild(wrap);
}
```

- [ ] **Step 3: 样式（web/style.css 追加）**

```css
/* 图谱页 */
.g-wrap { position:relative; height:100%; display:flex; flex-direction:column; }
.g-bar { display:flex; gap:10px; align-items:center; padding:8px 14px;
  border-bottom:1px solid #e5e7eb; background:#fff; }
.g-proj { height:30px; padding:0 8px; border:1px solid #d1d5db; border-radius:6px; font-size:13px; }
.g-stage { position:relative; flex:1; overflow:hidden; background:#f3f4f6; }
.g-empty { padding:40px; text-align:center; color:#9ca3af; font-size:13px; }
.g-stage svg { width:100%; height:100%; display:block; cursor:grab;
  user-select:none; -webkit-user-select:none; }   /* 拖拽防文本选中蓝框，勿删 */
```

- [ ] **Step 4: 验证**

Run: `node --check web/app.js`（语法零错误）
手动：启动 okd，浏览器开 GUI → 左侧导航"管理"下方出现"图谱"图标项；点击进入，项目下拉默认选中最近更新项目，空数据时显示空态；hash `#graph` 刷新后停留在图谱页。

- [ ] **Step 5: Commit**

```bash
git add web/app.js web/style.css
git commit -m "feat(gui): 图谱页骨架——导航项/项目选择器/数据加载骨架"
```

---

### Task 3: 引擎移植——SVG 力导向渲染与基础交互

**Files:**
- Modify: `web/app.js`（图谱页区段，填充 gV/renderGraph 引擎部分）
- Modify: `web/style.css`（节点/边/标签/tooltip 样式）

**Interfaces:**
- Consumes: Task 2 的 `GRAPH.data`（nodes/edges/categories）、`graphReset`。
- Produces:
  - `gV = { nodes, edges, byId, adj, nodeEls, labelEls, edgeEls, view:{x,y,k}, alpha, dragNode, panning, panStart, moved, expanded:Set<string>|null, raf }`
  - `function gTick()` / `function gRender()` / `function gFrame()` —— 物理与绘制
  - `function gApplyView()` / `function gToWorld(px,py)`
  - `function hotNode(n)` —— 悬停高亮边 + 非邻居 `.fade`（原型 hotEdges 的增强版）
  - `PALETTE` / `catColor` / `gRadius(n)` —— 配色与半径档（deg≥6→13, ≥3→10, else 7；isDir 13）

- [ ] **Step 1: 引擎代码（按以下要点从原型移植，物理参数逐字不改）**

源码参照 `docs/prototypes/prototype-wiki-graph.html`，移植映射如下（每段改动点已列出，未列出的逐字照抄）：

1. 配色与邻接（原型 :82-94）：照抄 PALETTE/catColor/adj；`catColor` 的 categories 改取 `GRAPH.data.categories`。
2. SVG 分层（原型 :96-104）：gEdge/gNode/gText 三层照抄；svg 元素由 `renderGraph` 创建进 `.g-stage`（不用 getElementById）。
3. 节点/标签创建（原型 :106-140）：照抄 `gRadius`、菱形/圆形分支；`labelEls` 条件 `isDir || deg>=3` 不变；text 的 `class="lbl"`。
4. 力导向（原型 :142-235）：REP=9000 / REP_CUT=320 / LEN={struct:110,ref:170} / SPRING=.014 / GRAV=.004 / CLUSTER=.05 / 阻尼 .82 / 位移钳 ±12 / mulberry32(42) 全部逐字。三处改动：
   - `nodes/edges/byId` 改从 `gV` 取；
   - `focus` 按 `GRAPH.data.categories` 计算，`"目录"` 特判删掉（后端 category 无此值），焦点环直接对全部 categories；
   - 预跑 900 tick 与 fitView（原型 :256-268）保留，W/H 取 `.g-stage` 容器 `clientWidth/clientHeight`。
5. 渲染循环（原型 :243-269）：`render()`→`gRender()`，`frame`→`gFrame()`；rAF 循环加页面守卫：

```js
function gFrame(){
  if(state.menu!=="graph" || !gV){ if(gV) gV.raf = null; return; }   // 离开图谱页自动停帧
  gTick(); gRender();
  gV.raf = requestAnimationFrame(gFrame);
}
```

6. 平移/缩放/拖拽（原型 :273-326）：照抄，pointer 事件挂在 renderGraph 创建的 svg 上；`selectNode(n)` 调用改为 `gSelectNode(n)`（Task 4 实现，本步留空函数 `function gSelectNode(n){}` 并在注释标注 Task 4 填充——这是本计划唯一允许的占位，因为 Task 3 的评审者可独立验收拖拽/缩放/悬停）。
7. 悬停（原型 :291-309 + 已修 fade 版 hotEdges :340-360）：tooltip/hotNode 照抄 fade 增强版（邻居集合 nb + `.fade` toggle）；`showTip` 的 tooltip 容器改为 renderGraph 创建的 `.g-tip` 节点。
8. resize：GUI 内用 ResizeObserver 挂 `.g-stage` 代替 window resize（原型 :271 不照抄）。

`graphReset(proj)` 填充：

```js
function graphReset(proj){
  if(gV && gV.raf) cancelAnimationFrame(gV.raf);
  gV = null;
  const d = GRAPH && GRAPH.proj===proj ? GRAPH.data : null;
  if(!d || !d.nodes || !d.nodes.length) return;
  gV = {
    nodes: d.nodes.map(n=>({...n, x:0,y:0,vx:0,vy:0,pinned:false})),
    edges: d.edges, byId:{}, adj:{},
    nodeEls:{}, labelEls:{}, edgeEls:[],
    view:{x:0,y:0,k:1}, alpha:1,
    dragNode:null, panning:false, panStart:null, moved:false,
    expanded:null, raf:null,           // Task 5 用 expanded；本步恒 null = 全量模式
  };
  gV.nodes.forEach(n=>{ gV.byId[n.id]=n; gV.adj[n.id]=[]; });
  gV.edges.forEach(e=>{ (gV.adj[e.source]||[]).push(e); (gV.adj[e.target]||[]).push(e); });
}
```

> 注意：nodes 用 `{...n}` 拷贝，不污染 GRAPH.data 缓存（切项目重进时坐标应重置）。

`renderGraph` 在 Task 2 骨架基础上：`.g-stage` 内创建 svg + tooltip 节点，若 `gV` 非空调用引擎初始化（建元素、预跑、fitView、起 gFrame），若 svg 已存在且 gV 有坐标则按 gV 重建元素后仅 `gRender()` + `gApplyView()`（DOM 重建不丢布局——这是扛整页重渲的关键路径）。

- [ ] **Step 2: 样式追加（web/style.css）**

从原型 :55-63 移植（选择器加 `.g-stage` 前缀作用域）：

```css
.g-stage .edge { stroke:#cbd5e1; stroke-width:1.2; }
.g-stage .edge.ref { stroke:#93c5fd; stroke-dasharray:5 4; stroke-width:1; }
.g-stage .edge.hot { stroke:#f59e0b; stroke-width:2.6; stroke-dasharray:none; }
.g-stage .node { cursor:pointer; }
.g-stage .node circle, .g-stage .node rect { stroke:#ffffff; stroke-width:1.5; }
.g-stage .node.dir rect { stroke:#1f2937; stroke-width:2.2; }
.g-stage .lbl { font-size:11px; fill:#374151; text-anchor:middle;
  paint-order:stroke; stroke:#f3f4f6; stroke-width:3px; pointer-events:none; }
.g-stage .dim { opacity:.12; }
.g-stage .fade { opacity:.15; }
.g-stage .node, .g-stage .lbl { transition:opacity .15s ease; }
.g-tip { position:absolute; z-index:30; pointer-events:none; background:#111827;
  color:#f9fafb; font-size:12px; border-radius:6px; padding:6px 10px;
  max-width:340px; display:none; box-shadow:0 4px 12px rgba(0,0,0,.2); line-height:1.5; }
.g-tip .t { font-weight:600; }
.g-tip .s { color:#d1d5db; }
```

- [ ] **Step 3: 验证**

Run: `node --check web/app.js`
手动清单（对照需求 §3 验收 2/3/4）：
- 图谱页渲染出当前项目全图，布局与原型观感一致（簇聚拢、菱形目录居中）；
- 悬停节点：相连边橙色 + 非邻居淡化 .15；移出恢复；
- 拖拽节点/平移画布全程无文本选中蓝框；
- 滚轮缩放以鼠标为锚点；
- 切到别的页面再切回：图重建但布局坐标不丢（gV 保留），rAF 不叠加（devtools 无多个循环）。

- [ ] **Step 4: Commit**

```bash
git add web/app.js web/style.css
git commit -m "feat(gui): 图谱引擎移植——力导向/拖拽缩放/悬停邻居淡化"
```

---

### Task 4: 详情面板 + 图例过滤 + 搜索 + R4 双击跳管理页

**Files:**
- Modify: `web/app.js`（图谱页区段）
- Modify: `web/style.css`（面板/图例样式）

**Interfaces:**
- Consumes: Task 3 的 gV/hotNode/gSelectNode；管理页 `jumpToEntry(file, project)`（app.js:1952）、`dblClick(h, key)`（app.js:1002）、`loadManage()`；原型面板/图例/搜索段（:328-415）。
- Produces:
  - `function gSelectNode(n)` —— 填充右侧面板
  - `function gJumpToManage(file)` —— R4 接线
  - `const gRelClick = { last:null }` —— 关联条目双击检测 holder

- [ ] **Step 1: 面板 + 图例 + 搜索（移植原型 :328-415）**

- 面板（原型 :348-370）：`selectNode`→`gSelectNode`，esc 照抄；容器为 renderGraph 创建的 `.g-panel`（fixed 改 absolute，父容器 `.g-wrap`）。badge 颜色用 `catColor[n.category]`。
- 图例（原型 :372-391）：容器 `.g-legend`；catState 挂 `gV.catState`；`applyFilters`→`gApplyFilters`（dim 逻辑照抄原型 :399-415，读 gV）。
- 搜索（原型 :394-398）：输入框放 `.g-bar`，`el("input","g-search")`，placeholder `t("gSearch")`；query 挂 `gV.query`。

关联条目行（原型 :366-368）改为三行为：

```js
  rel.map(r=>`<div class="rel" data-id="${esc(r.n.id)}">${esc(r.n.title)}<span class="k">${r.k}</span></div>`).join("");
panelBody.querySelectorAll(".rel").forEach(el=>{
  el.onclick = ()=>{
    if(!dblClick(gRelClick, el.dataset.id)){
      // 单击：图内联动——选中对应节点并居中高亮（不跳页）
      const n = gV.byId[el.dataset.id];
      if(n){ hotNode(n); gSelectNode(n); }
      return;
    }
    gJumpToManage(el.dataset.id);   // 双击：跳管理页（R4）
  };
});
```

- [ ] **Step 2: R4 接线 gJumpToManage**

```js
/* R4：双击关联条目 → 管理页打开该条目并树内定位。
   顺序有讲究（jumpToEntry 的坑，api 探针已核）：
   1) 先切 menu 再 jump——jumpToEntry 不切页，render 错页则滚动静默无效；
   2) MGMT 未载先 loadManage()——MGMT 为 null 时 jumpToEntry 静默返回；
   3) 缓存缺条目时 jumpToEntry 自己挂 pendingJump 重试（app.js:1959），
      该重试只在 state.menu==="manage" 时不被守卫清掉（app.js:1065,1074-1077）。 */
function gJumpToManage(file){
  state.menu = "manage"; location.hash = "manage";
  loadManage();
  jumpToEntry(file, graphProj);
}
```

面板样式（style.css 追加，原型 :27-46 移植为 absolute 定位）：

```css
.g-panel { position:absolute; top:0; right:0; bottom:0; width:320px; z-index:15;
  background:#fff; border-left:1px solid #e5e7eb; padding:18px;
  overflow-y:auto; transform:translateX(100%); transition:transform .18s ease; }
.g-panel.open { transform:translateX(0); }
.g-panel * { word-break:break-all; }
.g-panel h2 { font-size:16px; margin-bottom:8px; line-height:1.4; }
.g-panel .row { font-size:12px; color:#6b7280; margin-bottom:6px; }
.g-panel .badge { display:inline-block; background:#eef2ff; color:#4338ca;
  border-radius:4px; padding:1px 7px; font-size:11px; margin-right:4px; }
.g-panel .tags span { display:inline-block; background:#f3f4f6; border:1px solid #e5e7eb;
  border-radius:10px; padding:1px 8px; font-size:11px; color:#4b5563; margin:2px 3px 2px 0; }
.g-panel .summary { font-size:13px; color:#374151; line-height:1.6;
  background:#f9fafb; border-radius:8px; padding:10px; margin:10px 0; }
.g-panel .rel { font-size:13px; padding:4px 6px; border-radius:5px; cursor:pointer; }
.g-panel .rel:hover { background:#eff6ff; color:#1d4ed8; }
.g-panel .rel .k { font-size:10px; color:#9ca3af; margin-left:6px; }
.g-legend { position:absolute; top:10px; left:10px; z-index:15; background:#fff;
  border:1px solid #e5e7eb; border-radius:10px; padding:10px 12px;
  box-shadow:0 2px 8px rgba(0,0,0,.06); font-size:12px; max-width:180px; }
.g-legend .item { display:flex; align-items:center; gap:8px; padding:3px 4px;
  border-radius:5px; cursor:pointer; user-select:none; }
.g-legend .item:hover { background:#f3f4f6; }
.g-legend .item.off { opacity:.35; }
.g-legend .dot { width:10px; height:10px; border-radius:50%; flex:none; }
.g-search { height:30px; padding:0 10px; border:1px solid #d1d5db; border-radius:6px;
  font-size:13px; outline:none; width:220px; }
.g-search:focus { border-color:#3b82f6; box-shadow:0 0 0 2px rgba(59,130,246,.15); }
```

- [ ] **Step 3: 验证**

Run: `node --check web/app.js`
手动清单（需求 §3 验收 5）：
- 单击节点 → 右侧滑出面板（标题/类型/tags/摘要/关联条目）；
- 单击面板关联条目 → 图内选中并高亮对应节点，不跳页；
- 双击关联条目 → 跳到管理页，该条目详情打开，左侧树展开到所在类目且该行滚动进视口；
- 管理页有未保存编辑草稿时双击 → 弹确认（exitEditGuarded 既有语义），取消则不跳；
- 搜索过滤、图例点选淡化与原型行为一致。

- [ ] **Step 4: Commit**

```bash
git add web/app.js web/style.css
git commit -m "feat(gui): 图谱详情面板/图例/搜索 + 关联条目双击跳管理页定位（R4）"
```

---

### Task 5: 分层模式（骨架常显 + 类目下钻 + DOM 懒挂载）

**Files:**
- Modify: `web/app.js`（图谱页区段）
- Modify: `web/style.css`（"返回总览"按钮样式）

**Interfaces:**
- Consumes: Task 3 的 gV（`expanded` 字段本步启用）；设计依据 `docs/2026-08-27-wiki-graph-layered-design.md`。
- Produces:
  - `const LAYER_THRESHOLD = 400` —— 分层触发阈值（调试后可调）
  - `gV.layered`（bool）/ `gV.expanded`（Set<category>）
  - `function gVisibleNodes()` —— 骨架 + 已展开类目成员
  - `function gExpandCat(cat)` / `function gCollapseCat(cat)` / `function gBackToOverview()`

- [ ] **Step 1: 分层判定与可见集**

`graphReset` 中加：

```js
  gV.layered = gV.nodes.length > LAYER_THRESHOLD;
  gV.expanded = new Set();
```

```js
// 骨架 = isDir 或 deg>=6（大球档，与 gRadius 对齐）；分层模式下叶子不进 DOM、不参与物理
function gIsSkeleton(n){ return n.isDir || n.deg >= 6; }
function gVisibleNodes(){
  if(!gV.layered) return gV.nodes;
  return gV.nodes.filter(n=> gIsSkeleton(n) || gV.expanded.has(n.category));
}
```

- [ ] **Step 2: 物理与渲染改走可见集（最小侵入）**

- `gTick()` 的斥力/碰撞双循环开头取 `const ns = gVisibleNodes();` 遍历 `ns` 代替 `gV.nodes`；弹簧仍遍历全部 edges 但端点不可见时跳过（`if(!vis[e.source]||!vis[e.target]) return;`，vis 为可见 id 集合，与 ns 同趟构建）。
- 元素创建改懒挂载：renderGraph/展开时为可见且无 `nodeEls[id]` 的节点建元素；收起时摘 DOM（`el.remove()` + 删 nodeEls/labelEls 键），**不清 `n.x/n.y`**（坐标保留，再展开不重排）。
- `gRender()` 只写可见集元素；边只画两端可见的。

- [ ] **Step 3: 下钻交互**

- 图例 item 点击语义按分层分流：

```js
item.onclick = ()=>{
  if(gV.layered){
    gV.expanded.has(c) ? gCollapseCat(c) : gExpandCat(c);
  } else {
    gV.catState[c] = !gV.catState[c];           // 全量模式保持 dim 语义
    gApplyFilters();
  }
};
```

```js
function gExpandCat(cat){
  gV.expanded.add(cat);
  gV.alpha = Math.max(gV.alpha, 0.35);   // 局部回热收敛（沿用拖拽回热机制）
  render();                              // 重建 DOM 挂载新成员
  if(!gV.raf) gV.raf = requestAnimationFrame(gFrame);
}
function gCollapseCat(cat){ gV.expanded.delete(cat); render(); }
function gBackToOverview(){ gV.expanded.clear(); render(); }
```

- 分层模式下 `.g-bar` 加"返回总览"按钮（`t("gBack")`，onclick=gBackToOverview，仅 `gV.expanded.size>0` 时显示）。
- 搜索命中未展开叶子时：`gExpandCat(n.category)` 后再走高亮（gApplyFilters 里对 layered 分支加该逻辑）。

- [ ] **Step 4: 验证**

Run: `node --check web/app.js`
手动清单（需求 §3 验收 6/7）：
- 当前项目（<400 条）行为与 Task 4 完全一致（零回归）；
- 临时构造 >400 条目的测试项目（或用脚本往测试项目 knowledge/ 批量生成 450 个 .md 条目）→ 首屏只有骨架节点；图例点类目展开/收起；展开时局部弹性收敛、其余不动；
- 搜索命中未展开叶子 → 自动展开其类目并高亮；
- 分层模式下拖拽、悬停淡化、双击跳转全部正常。

- [ ] **Step 5: Commit**

```bash
git add web/app.js web/style.css
git commit -m "feat(gui): 图谱分层模式——>400 条目自动骨架常显+类目下钻"
```

---

### Task 6: 收尾——构建同步、验收走查、文档

**Files:**
- Modify: `docs/2026-08-27-gui-graph-page-requirements.md`（状态改"已实现"，补验收结果）
- Modify: 知识库条目《GUI 图谱页》（ok add --force 更新为实现态，含关键路径）

- [ ] **Step 1: 全量测试与构建**

```bash
go test ./internal/gui/ -v        # 全绿
node --check web/app.js
python scripts/build.py           # 同步 dist/web 并出 exe（勿裸 go build——dist/web 不同步的坑）
```

- [ ] **Step 2: 验收走查**

用 build.py 产出的 dist 二进制启动，逐条核对需求 §3 验收 1-7，结果（含实测数据：当前项目节点数、分层触发验证方式）记录到需求文档尾部"验收记录"小节。

- [ ] **Step 3: 知识库更新**

```bash
"D:/software/OpenKnowledge/ok.exe" add --force --title "GUI 图谱页" --type reference --tags wiki,gui \
  --summary "管理下新增图谱栏目：v1 SVG 原型路线+分层下钻，关联条目双击复用 jumpToEntry 跳管理页定位" \
  --file <更新后的条目正文.md>
```

正文在需求态基础上补：实现落点（graph.go/apiGraph/图谱页区段行号）、验收结论。

- [ ] **Step 4: Commit**

```bash
git add docs/2026-08-27-gui-graph-page-requirements.md
git commit -m "docs: 图谱页验收记录"
```

---

## Self-Review 记录

- Spec 覆盖：R1→Task 2；R2→Task 2；R3→Task 1（数据契约/类目口径）+ Task 5（解构/分层）；R4→Task 4。验收 1-7 均有对应 Task 的手动/自动验证步骤。
- 已知有意取舍：前端无既有 JS 测试框架（web/ 无测试基建），前端 Task 用 `node --check` + 手动清单验收，未强行引入测试框架（YAGNI）；Task 3 Step 1 第 6 点的空 `gSelectNode` 是唯一允许占位（Task 4 填充，评审者可独立验收 Task 3）。
- 类型/命名一致性：GRAPH/graphProj/gV/loadGraph/refreshGraph/renderGraph/graphReset/gTick/gRender/gFrame/gApplyView/gToWorld/hotNode/gSelectNode/gJumpToManage/gVisibleNodes/gExpandCat/gCollapseCat/gBackToOverview/LAYER_THRESHOLD 在 Task 2-5 间已逐一对齐。
- 风险备查：① `newEnv/mkProject/do/writeKnowledge` 签名以 api_test.go 现状为准（Task 1 Step 1 注）；② GUI 整页重渲 vs rAF 模拟的冲突由 gV 模块级状态 + gFrame 页面守卫解决（Task 3）；③ 后端 `handler` 类型名以 api.go 现状为准。
