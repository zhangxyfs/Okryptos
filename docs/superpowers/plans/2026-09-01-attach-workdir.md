# 壳项目关联工作目录 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 拉取/恢复出的空壳项目（空 Paths）可在 GUI 服务器页关联工作目录（单个 + 一键批量），状态正名「未关联目录」，同时 `ok init` 同名幂等补挂，恢复 hooks 的 cwd 匹配。

**Architecture:** 注册表层新增 `AddPath`（锁内补挂、冲突口径同 `AddProject` 规范化相等）；GUI 新增 `POST /api/project/attach`；前端服务器页 bindCard 按 `paths` 是否为空区分「已绑定/未关联目录」并提供关联入口；CLI `ok init` 同名改走 AddPath。规格：`docs/superpowers/specs/2026-09-01-attach-workdir-design.md`。

**Tech Stack:** Go（internal/registry、internal/gui、internal/cli）、原生 JS（web/app.js，无前端测试框架）。

## Global Constraints

- 注册表一切写操作走 `registry.Update`（跨进程文件锁），禁止裸 Load→改→Save。
- 行为变化类新测试先验证对旧代码变红，再实现（既有约定）。
- hooks 每次现读注册表，关联后即时生效——无需也不得有"重启 daemon"提示。
- 改动 `web/**` 的提交必须附带 `docs/changelogs/*.md` 条目（changelog_required 强制规则，"前端也要写"）。
- i18n zh/en 双语同步补齐；按钮均有 busy 态与结果 toast（假功能按钮纪律）。
- 验证基线：`go build ./... && go vet ./... && go test ./...` 与 `node --check web/app.js`。
- 测试须 Windows/Linux 双平台可跑（路径大小写语义差异见 registry_test.go 现有先例）。

---

### Task 1: registry.AddPath

**Files:**
- Modify: `internal/registry/registry.go`（import 加 `errors`；AddProject 后新增）
- Test: `internal/registry/registry_test.go`

**Interfaces:**
- Produces: `func (r *Registry) AddPath(name, path string) error`；哨兵 `registry.ErrProjectNotFound`、`registry.ErrPathConflict`（`errors.Is` 分类用，Task 2/3 依赖）。

- [ ] **Step 1: 写失败测试**（追加到 registry_test.go）

```go
func TestAddPath(t *testing.T) {
	r := &Registry{Projects: []Project{
		{Name: "shell"}, // 拉取/恢复的空壳
		{Name: "other", Paths: []string{`D:\develop\other`}},
	}}
	// 补挂成功
	if err := r.AddPath("shell", `D:\develop\shell-src`); err != nil {
		t.Fatal(err)
	}
	if len(r.Projects[0].Paths) != 1 || r.Projects[0].Paths[0] != `D:\develop\shell-src` {
		t.Fatalf("unexpected paths %+v", r.Projects[0].Paths)
	}
	// 幂等：规范化后同路径（尾分隔符差异）不重复追加
	if err := r.AddPath("shell", `D:\develop\shell-src\`); err != nil {
		t.Fatal(err)
	}
	if len(r.Projects[0].Paths) != 1 {
		t.Fatalf("idempotent attach should not duplicate: %+v", r.Projects[0].Paths)
	}
	// 冲突：路径已挂别的项目
	if err := r.AddPath("shell", `D:\develop\other`); !errors.Is(err, ErrPathConflict) {
		t.Fatalf("expected ErrPathConflict, got %v", err)
	}
	// 未知项目
	if err := r.AddPath("nope", `D:\x`); !errors.Is(err, ErrProjectNotFound) {
		t.Fatalf("expected ErrProjectNotFound, got %v", err)
	}
}
```

测试文件 import 加 `errors`。

- [ ] **Step 2: 跑测试确认变红**

Run: `go test ./internal/registry/ -run TestAddPath -v`
Expected: FAIL（`undefined: ErrPathConflict` / `r.AddPath` 不存在，编译错误）

- [ ] **Step 3: 实现**

registry.go import 块加 `"errors"`，在 `AddProject` 函数（registry.go:121-142）之后插入：

```go
// ErrProjectNotFound / ErrPathConflict 是 AddPath 的分类哨兵（GUI 映射 404/409）。
var (
	ErrProjectNotFound = errors.New("项目未注册")
	ErrPathConflict    = errors.New("路径已注册给其他项目")
)

// AddPath 把 path 补挂到已存在项目的 Paths（服务器拉取/备份恢复的空壳项目
// 关联工作目录——hooks 的 FindByCwd 只按 Paths 前缀匹配，空壳永不命中）。
// 锁由调用方（Update）持有；规范化后同路径幂等，冲突口径与 AddProject 一致
// （相等判断）。持久化由 Update/Save 负责。
func (r *Registry) AddPath(name, path string) error {
	npath := NormalizePath(path)
	for i := range r.Projects {
		p := &r.Projects[i]
		if p.Name == name {
			for _, ep := range p.Paths {
				if NormalizePath(ep) == npath {
					return nil
				}
			}
			p.Paths = append(p.Paths, path)
			return nil
		}
		for _, ep := range p.Paths {
			if NormalizePath(ep) == npath {
				return fmt.Errorf("%w: %q 属于项目 %q", ErrPathConflict, path, p.Name)
			}
		}
	}
	return fmt.Errorf("%w: %q", ErrProjectNotFound, name)
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/registry/ -v`
Expected: PASS（含既有测试全绿）

- [ ] **Step 5: Commit**

```bash
git add internal/registry/registry.go internal/registry/registry_test.go
git commit -m "feat(registry): AddPath 空壳项目补挂工作目录（幂等/冲突哨兵）"
```

---

### Task 2: GUI `POST /api/project/attach`

**Files:**
- Modify: `internal/gui/api.go`（路由注册处 api.go:75 附近 + apiProjects 后新增 handler）
- Test: `internal/gui/api_test.go`

**Interfaces:**
- Consumes: Task 1 的 `registry.AddPath` / `ErrProjectNotFound` / `ErrPathConflict`；既有 `decodeJSON`（api.go:458）、`writeErr`/`writeJSON`（api.go:186-194）、`findProject`（api.go:201）、`registry.Update`。
- Produces: 端点 `POST /api/project/attach`，请求 `{project, path}`，200 返回 `{"name", "paths"}`；400 空路径/目录不存在；404 项目未注册；409 路径冲突；500 锁/IO。前端 Task 4/5 依赖该契约。

- [ ] **Step 1: 写失败测试**（追加到 api_test.go；import 需已有 `net/http/httptest`、`os`、`path/filepath`、`openknowledge/internal/registry`，缺则补）

```go
func TestApiProjectAttach(t *testing.T) {
	h, _, okHome := newEnv(t)
	srv := httptest.NewServer(h)
	defer srv.Close()
	// 空壳项目（模拟服务器拉取）：无 paths
	reg := &registry.Registry{Projects: []registry.Project{{Name: "shell"}}}
	if err := reg.Save(registry.DefaultPath()); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(okHome, "projects", "shell", "knowledge"), 0o755); err != nil {
		t.Fatal(err)
	}
	workdir := t.TempDir()

	res, body := do(t, "POST", srv.URL+"/api/project/attach", testToken, map[string]any{"project": "shell", "path": workdir})
	if res != 200 {
		t.Fatalf("attach: %d %s", res, body)
	}
	loaded, err := registry.Load(registry.DefaultPath())
	if err != nil {
		t.Fatal(err)
	}
	if p := loaded.FindByCwd(filepath.Join(workdir, "sub")); p == nil || p.Name != "shell" {
		t.Fatalf("FindByCwd should hit after attach: %+v", loaded.Projects)
	}

	// 幂等重入仍 200 且不重复
	res, body = do(t, "POST", srv.URL+"/api/project/attach", testToken, map[string]any{"project": "shell", "path": workdir})
	if res != 200 {
		t.Fatalf("reattach: %d %s", res, body)
	}
	loaded, _ = registry.Load(registry.DefaultPath())
	if len(loaded.Projects[0].Paths) != 1 {
		t.Fatalf("duplicate attach: %+v", loaded.Projects[0].Paths)
	}

	// 404 未知项目 / 400 空路径与不存在目录
	res, _ = do(t, "POST", srv.URL+"/api/project/attach", testToken, map[string]any{"project": "nope", "path": workdir})
	if res != 404 {
		t.Fatalf("unknown project: %d", res)
	}
	res, _ = do(t, "POST", srv.URL+"/api/project/attach", testToken, map[string]any{"project": "shell", "path": ""})
	if res != 400 {
		t.Fatalf("empty path: %d", res)
	}
	res, _ = do(t, "POST", srv.URL+"/api/project/attach", testToken, map[string]any{"project": "shell", "path": filepath.Join(workdir, "nonexistent")})
	if res != 400 {
		t.Fatalf("missing dir: %d", res)
	}

	// 409：路径已挂别的项目
	other := t.TempDir()
	reg2 := &registry.Registry{Projects: []registry.Project{
		{Name: "a", Paths: []string{workdir}},
		{Name: "b", Paths: []string{other}},
	}}
	if err := reg2.Save(registry.DefaultPath()); err != nil {
		t.Fatal(err)
	}
	res, _ = do(t, "POST", srv.URL+"/api/project/attach", testToken, map[string]any{"project": "b", "path": workdir})
	if res != 409 {
		t.Fatalf("conflict: %d", res)
	}
}
```

- [ ] **Step 2: 跑测试确认变红**

Run: `go test ./internal/gui/ -run TestApiProjectAttach -v`
Expected: FAIL（路由未注册 → 404，首个断言即挂）

- [ ] **Step 3: 实现**

api.go 路由注册处（api.go:75 `api("GET /api/projects", h.apiProjects)` 之后）加一行：

```go
	api("POST /api/project/attach", h.apiProjectAttach)
```

`apiProjects` 函数（api.go:673）之后新增：

```go
// apiProjectAttach 给已注册项目（服务器拉取/备份恢复的空壳）补挂工作目录：
// hooks 的 cwd 匹配即时恢复（注册表现读，无需重启）。
func (h *Handler) apiProjectAttach(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Project string `json:"project"`
		Path    string `json:"path"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Path == "" {
		writeErr(w, http.StatusBadRequest, "path 不能为空")
		return
	}
	if fi, err := os.Stat(req.Path); err != nil || !fi.IsDir() {
		writeErr(w, http.StatusBadRequest, "目录不存在或不是目录: "+req.Path)
		return
	}
	if err := registry.Update(func(reg *registry.Registry) error {
		return reg.AddPath(req.Project, req.Path)
	}); err != nil {
		switch {
		case errors.Is(err, registry.ErrProjectNotFound):
			writeErr(w, http.StatusNotFound, err.Error())
		case errors.Is(err, registry.ErrPathConflict):
			writeErr(w, http.StatusConflict, err.Error())
		default:
			writeErr(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	_, paths, _, err := findProject(req.Project)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"name": req.Project, "paths": paths})
}
```

api.go import 确认含 `errors`（已含则不动）。

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/gui/ -run TestApiProjectAttach -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/gui/api.go internal/gui/api_test.go
git commit -m "feat(gui): POST /api/project/attach 空壳项目补挂工作目录"
```

---

### Task 3: CLI `ok init` 同名补挂

**Files:**
- Modify: `internal/cli/cli.go`（Init，cli.go:85-90 的 registry.Update 闭包与 cli.go:102 的输出）
- Test: `internal/cli/cli_test.go`

**Interfaces:**
- Consumes: Task 1 的 `registry.AddPath`。
- Produces: 行为变化——`ok init` 遇到同名已注册项目时不再报错退出，而是补挂 cwd 并输出「已关联目录」。

- [ ] **Step 1: 写失败测试**（追加到 cli_test.go；环境搭建复用 TestInitAddSearchList 同款 Setenv 组）

```go
func TestInitAttachesWorkdirToShellProject(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OK_HOME", home)
	t.Setenv("KIMI_CODE_HOME", filepath.Join(home, "kimi"))
	t.Setenv("PI_CODING_AGENT_DIR", t.TempDir())
	t.Setenv("OK_ZCODE_HOME", filepath.Join(t.TempDir(), "nonexistent-zcode"))
	t.Setenv("OK_REASONIX_HOME", filepath.Join(t.TempDir(), "nonexistent-reasonix"))
	t.Setenv("OK_DSH_HOME", filepath.Join(t.TempDir(), "nonexistent-dsh"))
	t.Setenv("OK_OPENCODE_HOME", filepath.Join(t.TempDir(), "nonexistent-opencode"))
	t.Setenv("OK_CLAUDE_HOME", filepath.Join(t.TempDir(), "nonexistent-claude"))
	t.Setenv("OK_CODEPILOT_HOME", filepath.Join(t.TempDir(), "nonexistent-codepilot"))
	t.Setenv("OK_CODEX_HOME", filepath.Join(t.TempDir(), "nonexistent-codex"))
	t.Setenv("OK_QODER_HOME", filepath.Join(t.TempDir(), "nonexistent-qoder"))
	t.Setenv("OK_QODER_IDE_HOME", filepath.Join(t.TempDir(), "nonexistent-qoder-ide"))
	if err := os.MkdirAll(filepath.Join(home, "kimi"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OPENAI_API_KEY", "")
	// 预置空壳项目（模拟服务器拉取）：同名无 paths
	reg := &registry.Registry{Projects: []registry.Project{{Name: "demo"}}}
	if err := reg.Save(registry.DefaultPath()); err != nil {
		t.Fatal(err)
	}
	// 工作目录名与项目名不同（显式登记路径，不靠名字匹配）
	proj := filepath.Join(home, "demo-src")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	chdir(t, proj)
	var out, errBuf bytes.Buffer

	if code := Init([]string{"demo"}, &out, &errBuf); code != 0 {
		t.Fatalf("init attach code=%d err=%q", code, errBuf.String())
	}
	if !strings.Contains(out.String(), "已关联目录") {
		t.Fatalf("expected attach message, got %q", out.String())
	}
	loaded, err := registry.Load(registry.DefaultPath())
	if err != nil {
		t.Fatal(err)
	}
	if p := loaded.FindByCwd(proj); p == nil || p.Name != "demo" {
		t.Fatalf("FindByCwd should hit after init attach: %+v", loaded.Projects)
	}
}
```

cli_test.go import 确认含 `openknowledge/internal/registry`（缺则补）。

- [ ] **Step 2: 跑测试确认对旧代码变红**（行为变化类约定）

Run: `go test ./internal/cli/ -run TestInitAttachesWorkdirToShellProject -v`
Expected: FAIL（旧代码报「项目 "demo" 已存在」→ code=1）

- [ ] **Step 3: 实现**（cli.go:83-90 处替换闭包，cli.go:102 处输出分支）

```go
	// 锁内读-改-写：并发 ok init / GUI 删除 / 备份恢复各自 Load→Save 会互相
	// 覆盖，项目注册静默丢失（hooks 对该项目全部失效）。
	// 同名项目已存在（服务器拉取/备份恢复的空壳）→ 补挂当前目录而非报错。
	attached := false
	if err := registry.Update(func(reg *registry.Registry) error {
		for _, p := range reg.Projects {
			if p.Name == name {
				attached = true
				return reg.AddPath(name, cwd)
			}
		}
		return reg.AddProject(name, cwd)
	}); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
```

输出处（原 `fmt.Fprintf(stdout, "已注册项目 %q → %s\n知识库目录: %s\n", name, cwd, st.Root)`）改为：

```go
	if attached {
		fmt.Fprintf(stdout, "项目 %q 已注册，已关联目录 %s\n知识库目录: %s\n", name, cwd, st.Root)
	} else {
		fmt.Fprintf(stdout, "已注册项目 %q → %s\n知识库目录: %s\n", name, cwd, st.Root)
	}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/cli/ -v`
Expected: PASS（含既有 init 测试全绿）

- [ ] **Step 5: Commit**

```bash
git add internal/cli/cli.go internal/cli/cli_test.go
git commit -m "feat(cli): ok init 同名项目幂等补挂工作目录（救拉取/恢复空壳）"
```

---

### Task 4: 前端——状态正名 + 单个关联

**Files:**
- Modify: `web/app.js`（i18n zh ~L226 / en ~L431；bindCard L6043-6093；srvPull 后新增 srvAttachOne）

**Interfaces:**
- Consumes: Task 2 端点契约；既有 `uiPrompt`（app.js:1930）、`toast`、`t()`、`esc()`、`el()`、`SRV.bindBusy`、`loadServerRoleData()`、`refreshManage()`；`SRV.projects` 元素已含 `paths`（/api/projects 契约，api.go:355）。
- Produces: `srvAttachOne(project)`；i18n 键 `srvUnlinked`/`srvAttach`/`srvAttaching`/`srvAttachPrompt`/`srvAttachPh`/`srvAttachOneDone`（Task 5 复用 srvUnlinked 与 busy 语义）。

- [ ] **Step 1: i18n 词条**

zh 块（`srvPullDone:"已拉取 {n} 个项目到本机", srvPullFail:"，失败：",` 行后）加：

```js
    srvUnlinked:"未关联目录", srvAttach:"关联目录", srvAttaching:"关联中…",
    srvAttachPrompt:"为项目 {n} 登记工作目录的绝对路径（目录须已存在；目录名可与项目名不同）：",
    srvAttachPh:"如 /home/you/develop/foo 或 D:\\develop\\foo",
    srvAttachOneDone:"已关联目录", srvAttachDone:"已关联 {n} 个项目", srvAttachFail:"，失败：",
    srvAttachAll:"一键关联", srvAttachAllPrompt:"为每个项目填工作目录绝对路径（留空跳过）：",
```

en 块（`srvPullDone:"Pulled {n} projects to this machine", srvPullFail:", failed: ",` 行后）加：

```js
    srvUnlinked:"No workdir linked", srvAttach:"Link dir", srvAttaching:"Linking…",
    srvAttachPrompt:"Register the absolute working-directory path for project {n} (directory must exist; its name may differ from the project name):",
    srvAttachPh:"e.g. /home/you/develop/foo or D:\\develop\\foo",
    srvAttachOneDone:"Directory linked", srvAttachDone:"Linked {n} projects", srvAttachFail:", failed: ",
    srvAttachAll:"Link all", srvAttachAllPrompt:"Fill in the working-directory absolute path for each project (leave blank to skip):",
```

- [ ] **Step 2: bindCard 状态正名 + 行内按钮**

bindCard 行循环（app.js:6049-6066）整段替换为：

```js
  (SRV.projects || []).forEach(p=>{
    const bound = p.sync && p.sync.is_repo;
    const linked = (p.paths || []).length > 0;
    const tr = el("tr","");
    const td1 = el("td",""); td1.innerHTML = '<b>'+esc(p.name)+'</b>';
    const td2 = el("td","");
    // 壳项目（服务器拉取/备份恢复，paths 空）不算已绑定——hooks 按 paths 匹配 cwd，空壳 hooks 全失效
    td2.innerHTML = bound
      ? (linked ? '<span class="chip on">'+t("srvBound")+'</span>' : '<span class="chip off">'+t("srvUnlinked")+'</span>')
      : '<span class="chip off">'+t("srvNotCreated")+'</span>';
    const td3 = el("td",""); td3.style.textAlign = "right";
    // syncStatusJSON 无 remote 字段（恒 undefined）——已绑定态不再显示远端地址 span
    if(!bound){
      const btn = el("button","btn btn-primary");
      btn.textContent = SRV.bindBusy[p.name] ? t("srvBinding") : t("srvBind");
      btn.disabled = !!SRV.bindBusy[p.name];
      btn.onclick = ()=>srvBind(p.name);
      td3.appendChild(btn);
    } else if(!linked){
      const btn = el("button","btn btn-primary");
      btn.textContent = SRV.bindBusy[p.name] ? t("srvAttaching") : t("srvAttach");
      btn.disabled = !!SRV.bindBusy[p.name];
      btn.onclick = ()=>srvAttachOne(p.name);
      td3.appendChild(btn);
    }
    tr.appendChild(td1); tr.appendChild(td2); tr.appendChild(td3);
    tb.appendChild(tr);
  });
```

- [ ] **Step 3: srvAttachOne**（紧跟 srvPull 函数后插入）

```js
/* 壳项目关联工作目录：uiPrompt 收绝对路径 → attach 端点（hooks 即时恢复，无需重启） */
async function srvAttachOne(project){
  const dir = await uiPrompt(t("srvAttachPrompt").replace("{n}", project), t("srvAttachPh"));
  if(!dir) return;
  SRV.bindBusy[project] = true; render();
  try{
    await api("/api/project/attach", { method:"POST", body:{ project: project, path: dir }, skip401Reload:true });
    toast(t("srvAttachOneDone"));
    SRV.projects = null; loadServerRoleData(); refreshManage();
  }catch(err){ toast(err.message, true); }
  SRV.bindBusy[project] = false;
  if(state.menu === "server") render();
}
```

- [ ] **Step 4: 语法检查**

Run: `node --check web/app.js`
Expected: 无输出（退出码 0）

- [ ] **Step 5: Commit**

```bash
git add web/app.js
git commit -m "feat(web): 壳项目状态正名「未关联目录」+ 行内关联目录按钮"
```

---

### Task 5: 前端——一键关联批量入口

**Files:**
- Modify: `web/app.js`（bindCard `card.appendChild(tb);` 后加批量按钮；srvAttachOne 后加 srvAttachAllDlg/srvAttachAll）

**Interfaces:**
- Consumes: Task 4 的 i18n 键（srvAttachAll/srvAttachAllPrompt/srvAttachPh/srvAttachDone/srvAttachFail/srvAttaching）与 Task 2 端点。
- Produces: `srvAttachAll(names)`、`srvAttachAllDlg(projects)`（多输入 modal，仿 uiDlg 结构 app.js:1896-1928）。

- [ ] **Step 1: 批量按钮**（bindCard 中 `card.appendChild(tb);` 之后、`// 服务器有仓、本机未拉取分组` 注释之前插入）

```js
  // 已建库但未关联工作目录的壳项目 ≥2 个：一键批量关联
  const unlinked = (SRV.projects || []).filter(p=>p.sync && p.sync.is_repo && !(p.paths || []).length).map(p=>p.name);
  if(unlinked.length > 1){
    const all = el("button","btn"); all.style.marginTop = "8px"; all.textContent = t("srvAttachAll");
    all.onclick = ()=>srvAttachAll(unlinked);
    card.appendChild(all);
  }
```

- [ ] **Step 2: 多输入 modal + 批量执行**（srvAttachOne 后插入；注意不与局部变量重名——modal 收尾函数叫 `finish`，计数叫 `okCount`）

```js
/* 一键关联：一个 modal 列出全部壳项目，每行一个路径输入框，留空跳过 */
function srvAttachAllDlg(projects){
  return new Promise(resolve=>{
    const mask = el("div","mask");
    const m = el("div","modal"); m.style.width = "520px";
    const msg = el("div","pdesc"); msg.textContent = t("srvAttachAllPrompt"); m.appendChild(msg);
    const inputs = {};
    projects.forEach(name=>{
      const row = el("div","prow"); row.style.marginTop = "8px";
      const k = el("span","k"); k.textContent = name; row.appendChild(k);
      const inp = el("input","pinput"); inp.placeholder = t("srvAttachPh");
      inp.style.flex = "1"; inp.style.marginLeft = "10px";
      inputs[name] = inp; row.appendChild(inp);
      m.appendChild(row);
    });
    const foot = el("div","mfoot"); foot.style.marginTop = "14px";
    const finish = v=>{ document.removeEventListener("keydown", onKey, true); mask.remove(); resolve(v); };
    const ok = el("button","btn btn-primary"); ok.textContent = t("fOk");
    ok.onclick = ()=>{
      const out = {}; let any = false;
      projects.forEach(n=>{ const v = inputs[n].value.trim(); if(v){ out[n] = v; any = true; } });
      finish(any ? out : null);
    };
    const no = el("button","btn"); no.textContent = t("fCancel"); no.onclick = ()=>finish(null);
    foot.appendChild(ok); foot.appendChild(no); m.appendChild(foot);
    mask.appendChild(m);
    mask.onclick = ev=>{ if(ev.target===mask) finish(null); };
    const onKey = ev=>{ if(ev.key === "Escape"){ ev.stopPropagation(); finish(null); } };
    document.addEventListener("keydown", onKey, true);
    document.body.appendChild(mask);
  });
}

/* 批量关联：逐个串行调 attach，单个失败不阻断，结束汇总 toast（与「全部拉取」同款语义） */
async function srvAttachAll(names){
  const map = await srvAttachAllDlg(names);
  if(!map) return;
  let okCount = 0; const failed = [];
  for(const name of Object.keys(map)){
    SRV.bindBusy[name] = true;
    if(state.menu === "server") render();
    try{
      await api("/api/project/attach", { method:"POST", body:{ project: name, path: map[name] }, skip401Reload:true });
      okCount++;
    }catch(_){ failed.push(name); }
    SRV.bindBusy[name] = false;
  }
  toast(t("srvAttachDone").replace("{n}", okCount) + (failed.length ? t("srvAttachFail")+failed.join("、") : ""), failed.length > 0);
  SRV.projects = null; loadServerRoleData(); refreshManage();
  if(state.menu === "server") render();
}
```

- [ ] **Step 3: 语法检查**

Run: `node --check web/app.js`
Expected: 无输出（退出码 0）

- [ ] **Step 4: Commit**

```bash
git add web/app.js
git commit -m "feat(web): 壳项目一键关联批量入口（多输入 modal，单失败不阻断）"
```

---

### Task 6: changelog + 全量验证

**Files:**
- Create: `docs/changelogs/2026-09-01-web-attach-workdir.md`

**Interfaces:**
- Consumes: Task 1-5 全部产出（changelog_required 强制规则要求 web/** 改动带 changelog 条目，文风参照 docs/changelogs/2026-08-31-web-force-password-change-modal.md）。

- [ ] **Step 1: 写 changelog**

```markdown
# 壳项目关联工作目录：状态正名 + 单个/一键关联 + ok init 同名补挂

日期：2026-09-01

服务器拉取/备份恢复注册的空 Paths 壳项目，hooks 按 cwd 最长前缀匹配注册表 Paths 永不命中，hook 注入/沉淀静默失效（ok.log 每回合刷「目录未注册为知识库项目」），且 GUI 服务器页对其显示「已绑定」名不副实。本期闭环：注册表新增 AddPath（锁内补挂、规范化相等冲突口径同 AddProject、ErrProjectNotFound/ErrPathConflict 哨兵）；GUI 新增 POST /api/project/attach（400 空路径或目录不存在 / 404 未知项目 / 409 路径冲突，hooks 现读注册表即时生效无需重启）；服务器页「我的项目绑定」卡对壳项目显示「未关联目录」并给行内「关联目录」按钮（uiPrompt 收绝对路径，目录名可与项目名不同），≥2 个壳项目时出「一键关联」多输入 modal 批量串行执行、单个失败不阻断、结束汇总 toast；CLI 侧 ok init 遇同名已注册项目改为幂等补挂 cwd 并输出「已关联目录」，同一 AddPath 同时救拉取壳与备份恢复壳。
```

- [ ] **Step 2: 全量验证**

Run: `go build ./... && go vet ./... && go test ./... && node --check web/app.js`
Expected: 全绿

- [ ] **Step 3: 真机走查清单**（人工，前端无测试框架）

- 壳项目（如 Linux 机器上已拉取的 OpenKnowledge）在服务器页显示「未关联目录」而非「已绑定」
- 行内「关联目录」填入已 clone 的代码目录（目录名可与项目名不同）→ toast 成功 → 状态变「已绑定」
- 填不存在路径 → toast 400 文案；填已挂别的项目的路径 → toast 409 文案
- ≥2 个壳项目时「一键关联」出现；留空行跳过、单个失败不阻断、汇总 toast
- 关联后 ok.log 不再刷「目录未注册」，hook 注入恢复
- 同一目录 `ok init` 同名 → 输出「已关联目录」，重复执行幂等

- [ ] **Step 4: Commit**

```bash
git add docs/changelogs/2026-09-01-web-attach-workdir.md
git commit -m "docs(changelog): 壳项目关联工作目录条目"
```
