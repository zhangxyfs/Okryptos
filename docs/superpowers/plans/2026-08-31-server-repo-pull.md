# 服务器仓拉取到本机 + 同步状态点实时化 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 新机器可一键把服务器已有个人仓拉取到本机（含登录自动提示、全部拉取），并让管理页同步状态点实时反映"有变更/同步中"。

**Architecture:** 服务端 okserver 仅新增一个自助重发 git token 端点；客户端 GUI 层抽取共用绑定管线（provision→凭据→init/clone→SetSync→首次同步），新增 `/api/server/pull` 注册空 Paths 壳项目后走 §14 clone 路径；syncing 用 `state/syncing` 标记文件、dirty 用 knowledge/ mtime 对比 LastSync，均为纯文件系统检查。

**Tech Stack:** Go 1.2x（net/http ServeMux 模式路由）、内部包 oksrv/serverx/syncx/gui/registry/store、原生 JS（web/app.js 无框架）。

**Spec:** `docs/superpowers/specs/2026-08-31-server-repo-pull-design.md`（已定稿，先读）

## Global Constraints

- 提交信息风格 `type(scope): 中文描述`（参照 `git log --oneline`）。
- oksrv 新增已认证端点必须套 `s.auth(s.gate(...))`（强制改密 gate 默认拦截是正确语义；**不得**加进白名单）。
- 测试隔离 git 全局/系统配置：`t.Setenv("GIT_CONFIG_GLOBAL"/"GIT_CONFIG_SYSTEM", ...)`（防 StoreCredential 污染真实凭据库）。
- 前端中英文字典成对加 key（zh 段在 web/app.js ~line 200 区、en 段 ~line 390 区）。
- 所有 fail-open 语义不破：列表/状态接口任何读失败给零值，绝不 500。
- 每 Task 结束跑对应包测试全绿后 commit；最后一关跑 `go test ./...` 全量。

---

### Task 1: oksrv 自助重发 git token 端点

**Files:**
- Modify: `internal/oksrv/gitbackend.go`（接口加 DeleteUserToken）
- Modify: `internal/oksrv/gitbackend_fake.go`（Fake 实现）
- Modify: `internal/oksrv/gitea.go`（Gitea 实现）
- Modify: `internal/oksrv/http.go`（路由 + handler）
- Test: `internal/oksrv/http_test.go`

**Interfaces:**
- Consumes: 现有 `GitBackend.CreateUserToken(ctx, username, tokenName)`、`Store.Audit(actor, action, target, detail)`、`backendErr(w, err)`。
- Produces: `GitBackend.DeleteUserToken(ctx, username, tokenName) error`；端点 `POST /api/v1/git-token` → 200 `{"git_token": "..."}`；Task 2 的 serverx.GitToken 依赖此契约。

- [ ] **Step 1: 写失败测试**（追加到 `internal/oksrv/http_test.go` 末尾）

```go
// TestApiGitToken 自助重发：未认证 401；认证后返回非空 token 且审计落库；
// 重复调用成功（删旧建新不撞名）；带强制改密标记的账号被 gate 拦 403。
func TestApiGitToken(t *testing.T) {
	srv, st, _ := newTestServer(t)
	// 未认证
	code, _ := call(t, srv, "POST", "/api/v1/git-token", "", nil)
	if code != 401 {
		t.Fatalf("unauth: %d", code)
	}
	// 认证（root 已在 newTestServer 里清掉强制改密标记；初始密码经包级 testEnv 取用）
	tok := login(t, srv, "root", getenv(t, "OK_TEST_ROOT_PW"))
	code, body := call(t, srv, "POST", "/api/v1/git-token", tok, nil)
	if code != 200 || body["git_token"] == "" {
		t.Fatalf("reissue: %d %v", code, body)
	}
	first := body["git_token"].(string)
	// 重复调用不撞名
	code, body = call(t, srv, "POST", "/api/v1/git-token", tok, nil)
	if code != 200 || body["git_token"] == "" || body["git_token"] == first {
		t.Fatalf("re-reissue: %d %v", code, body)
	}
	// 审计落库
	found := false
	for _, a := range st.ListAudit(10, 0) {
		if a.Action == "reissue-git-token" {
			found = true
		}
	}
	if !found {
		t.Fatal("audit missing reissue-git-token")
	}
	// gate：带标记账号 403 must_change_password
	if err := st.SetMustChangePassword("root", true); err != nil {
		t.Fatal(err)
	}
	code, body = call(t, srv, "POST", "/api/v1/git-token", tok, nil)
	if code != 403 || body["error"] != "must_change_password" {
		t.Fatalf("gate: %d %v", code, body)
	}
}
```

注：`login`/`call`/`newTestServer`/`getenv` 均为既有夹具（http_test.go；root 初始密码惯例是 `getenv(t, "OK_TEST_ROOT_PW")`）。`st.ListAudit(limit, offset)` 返回 `[]AuditEntry`（含 `Action` 字段，store.go:503/523）。

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/oksrv/ -run TestApiGitToken -v`
Expected: FAIL（404，端点不存在）

- [ ] **Step 3: GitBackend 接口加 DeleteUserToken**（`internal/oksrv/gitbackend.go`，加在 CreateUserToken 后）

```go
	// DeleteUserToken 删除同名 token（自助重发前的撞名清理；不存在时返回 nil 或
	// 可忽略错误——调用方尽力而为语义，成败以随后的 CreateUserToken 为准）。
	DeleteUserToken(ctx context.Context, username, tokenName string) error
```

- [ ] **Step 4: FakeBackend 实现**（`internal/oksrv/gitbackend_fake.go`，加在 CreateUserToken 后）

```go
// DeleteUserToken _fake 语义：记数即可（撞名场景由 tokens 计数自然区分）。
func (f *FakeBackend) DeleteUserToken(_ context.Context, _, _ string) error { return nil }
```

- [ ] **Step 5: GiteaBackend 实现**（`internal/oksrv/gitea.go`，加在 CreateUserToken 后）

```go
// DeleteUserToken 核实：DELETE /users/{username}/tokens/{tokenname}（与 CreateUserToken
// 同走 Basic 头——路由同样强制 reqBasicOrRevProxyAuth）；404（无此 token）视为成功。
func (g *GiteaBackend) DeleteUserToken(ctx context.Context, username, tokenName string) error {
	err := g.do(ctx, http.MethodDelete, "/users/"+url.PathEscape(username)+"/tokens/"+url.PathEscape(tokenName), nil, nil, true)
	if err != nil && strings.Contains(err.Error(), "404") {
		return nil
	}
	return err
}
```

实现时注意：`g.do` 最后一个 bool 参数是 basicAuth 标志（参照 CreateUserToken 调用处 `g.do(ctx, ..., true)`）；`strings` 如未导入则补上。若 `g.do` 的错误不携带状态码文本，改成在 `g.do` 内可判 404 的既有方式（看 gitea.go 里其他 404 处理先例，如 RepoExists），保持同风格。

- [ ] **Step 6: handler + 路由**（`internal/oksrv/http.go`）

路由注册（放在 `repos/personal` 行后）：

```go
	mux.HandleFunc("POST /api/v1/git-token", s.auth(s.gate(s.apiGitToken)))
```

handler（加在 apiPersonalRepo 之后）：

```go
// apiGitToken 自助重发 git token：删旧建新（规避同名撞名），明文返回一次。
// 解决新机器拉取已有仓时拿不到凭据的断链（token 原本只在建仓首发）。
func (s *server) apiGitToken(w http.ResponseWriter, r *http.Request, u *User) {
	const tokenName = "ok-sync-reissue"
	_ = s.backend.DeleteUserToken(r.Context(), u.Username, tokenName) // 尽力而为，以建为准
	token, err := s.backend.CreateUserToken(r.Context(), u.Username, tokenName)
	if err != nil {
		backendErr(w, err)
		return
	}
	s.st.Audit(u.Username, "reissue-git-token", u.Username, "")
	writeJSON(w, http.StatusOK, map[string]string{"git_token": token})
}
```

- [ ] **Step 7: 跑测试确认通过**

Run: `go test ./internal/oksrv/ -v`
Expected: 含 TestApiGitToken 全部 PASS

- [ ] **Step 8: Commit**

```bash
git add internal/oksrv/
git commit -m "feat(oksrv): POST /api/v1/git-token 自助重发 git token（删旧建新+审计，过强制改密 gate）"
```

---

### Task 2: serverx.GitToken + syncx.HasStoredCredential

**Files:**
- Modify: `internal/serverx/serverx.go`（加方法，放 ChangePassword 后）
- Modify: `internal/syncx/credential.go`（加 HasStoredCredential）
- Test: `internal/syncx/credential_test.go`

**Interfaces:**
- Consumes: Task 1 的 `POST /api/v1/git-token` 契约；`Client.call(ctx, method, path, body, out)`；`HasCredentialHelper(dir)`、`execGit` 超时 `localTimeout`、`procx.HideWindow`。
- Produces: `serverx.Client.GitToken(ctx context.Context) (string, error)`（旧服务端 404 时返回 `*serverx.Error{Code:404}`）；`syncx.HasStoredCredential(dir, remoteURL, username string) bool`。Task 3 两者都用。

- [ ] **Step 1: 写失败测试**（追加 `internal/syncx/credential_test.go`）

```go
// TestHasStoredCredential 无 helper → false；helper 指向测试脚本后可分辨有/无存凭据。
func TestHasStoredCredential(t *testing.T) {
	dir := t.TempDir()
	// 隔离配置：无 helper
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))
	t.Setenv("GIT_CONFIG_SYSTEM", filepath.Join(t.TempDir(), "gitconfig-sys"))
	if HasStoredCredential(dir, "http://h/u/r.git", "alice") {
		t.Fatal("no helper should be false")
	}
	// 配一个永远返回固定凭据的 helper（shell 脚本），验证 true 分支
	gitconfig := filepath.Join(t.TempDir(), "gitconfig2")
	helper := filepath.Join(t.TempDir(), "helper.sh")
	if err := os.WriteFile(helper, []byte("#!/bin/sh\necho username=alice\necho password=secret\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := "[credential]\n\thelper = !sh " + filepath.ToSlash(helper) + "\n"
	if err := os.WriteFile(gitconfig, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", gitconfig)
	if !HasStoredCredential(dir, "http://h/u/r.git", "alice") {
		t.Fatal("helper with stored cred should be true")
	}
}
```

注：本测试需要环境有 `git` 与 `sh`（项目既有 credential 测试同此前提，参照 credential_test.go 既有用法的跳过/隔离惯例；如既有测试有 `testing.Short` 跳过之类惯例则沿用）。

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/syncx/ -run TestHasStoredCredential -v`
Expected: FAIL（HasStoredCredential undefined）

- [ ] **Step 3: 实现 HasStoredCredential**（追加 `internal/syncx/credential.go`）

```go
// HasStoredCredential 查询本机 credential helper 是否已存指定 remote 的凭据
// （git credential fill 非交互探测；无 helper 或拿不到 password 均 false）。
func HasStoredCredential(dir, remoteURL, username string) bool {
	if !HasCredentialHelper(dir) {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), localTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "credential", "fill")
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "LC_ALL=C")
	procx.HideWindow(cmd)
	cmd.Stdin = strings.NewReader("url=" + remoteURL + "\nusername=" + username + "\n\n")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(out), "\n") {
		if v, ok := strings.CutPrefix(line, "password="); ok && v != "" {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: serverx.GitToken**（`internal/serverx/serverx.go`，放 ChangePassword 方法后）

```go
// GitToken 自助重发 git token（okserver v2.25 起；旧服务端返回 404 *Error，
// 调用方据此提示升级服务端或回退旧文案）。
func (c *Client) GitToken(ctx context.Context) (string, error) {
	var out struct {
		GitToken string `json:"git_token"`
	}
	if err := c.call(ctx, "POST", "/git-token", nil, &out); err != nil {
		return "", err
	}
	return out.GitToken, nil
}
```

- [ ] **Step 5: 跑测试确认通过**

Run: `go test ./internal/syncx/ ./internal/serverx/ -v`
Expected: 全部 PASS（serverx 编译通过即可，其端点测试由 Task 3 的 gui 全链路覆盖）

- [ ] **Step 6: Commit**

```bash
git add internal/syncx/credential.go internal/syncx/credential_test.go internal/serverx/serverx.go
git commit -m "feat(serverx,syncx): GitToken 自助重发客户端方法 + HasStoredCredential 凭据探测"
```

---

### Task 3: gui 抽取共用绑定管线 + 凭据自助重发分支

**Files:**
- Modify: `internal/gui/api_server.go:171-247`（apiServerRepos 重构为薄封装 + serveBindRepo）
- Test: `internal/gui/api_server_test.go`

**Interfaces:**
- Consumes: Task 2 的 `serverx.Client.GitToken`、`syncx.HasStoredCredential`；既有 `resolveProject`、`hasKnowledgeContent(st)`、`syncx.Open/StoreCredential/HasCredentialHelper/CredentialURLWithAuth/SyncOnce/RecordOutcome`、`config.SetSync/LoadMerged`、`describeOutcome`。
- Produces: `func (h *Handler) serveBindRepo(w http.ResponseWriter, r *http.Request, st *store.Store, project string)`——Task 4 的 apiServerPull 直接调用。对外 HTTP 契约（`/api/server/repos` 响应形状）不变。

- [ ] **Step 1: 调整既有测试预期**

`TestApiServerReposNoTokenNoHelper`（api_server_test.go ~line 211）语义变更：仓已存在+本机无凭据时，新行为是先尝试自助重发；该用例的 fakeOKServer 未注册 `/api/v1/git-token` 路由 → Go ServeMux 返回 404 → 走"服务端过旧"报错分支。断言保持 `Status == "error"`，消息断言改为新前缀（仍含"仓已存在但本机无 git 凭据"则无需改断言，只改实现里消息文本时保持此前缀）。先跑一遍确认该测试当前状态。

Run: `go test ./internal/gui/ -run TestApiServerRepos -v`
Expected: 现状 PASS（重构前基线）

- [ ] **Step 2: 重构 apiServerRepos 为薄封装 + serveBindRepo**

把 `api_server.go:171-247` 的 apiServerRepos 改为：

```go
// apiServerRepos 建仓一条龙：已注册项目直接进共用绑定管线。
func (h *Handler) apiServerRepos(w http.ResponseWriter, r *http.Request) {
	var req serverReposRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	st := resolveProject(w, req.Project)
	if st == nil {
		return
	}
	h.serveBindRepo(w, r, st, req.Project)
}

// serveBindRepo 绑定管线（apiServerRepos 与 apiServerPull 共用）：
// provision → 凭据（空 token 时查本机已存凭据，没有则自助重发）→ init/clone → SetSync → 首次同步。
func (h *Handler) serveBindRepo(w http.ResponseWriter, r *http.Request, st *store.Store, project string) {
	c, cfgServer, err := h.serverClient()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if c == nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "not_configured"})
		return
	}
	pr, err := c.ProvisionPersonalRepo(r.Context(), project)
	if err != nil {
		writeServerErr(w, err)
		return
	}
	// 凭据：优先系统 credential helper；仓已存在（token 仅建仓首发）且本机未存凭据时
	// 自助重发（v2.25 服务端起）；无 helper 回退 URL 内嵌（credNote 警告）。
	remote := pr.Repo.CloneURL
	credNote := ""
	gitToken := pr.GitToken
	if gitToken == "" && !syncx.HasStoredCredential(st.Root, pr.Repo.CloneURL, cfgServer.Username) {
		tok, terr := c.GitToken(r.Context())
		if terr != nil {
			var se *serverx.Error
			if errors.As(terr, &se) && se.Code == http.StatusNotFound {
				writeJSON(w, http.StatusOK, map[string]any{"status": "error", "clone_url": pr.Repo.CloneURL,
					"message": "仓已存在但本机无 git 凭据，且服务端版本过旧（不支持自助重发 token）：请升级 okserver，或联系管理员重置密码重发 token"})
				return
			}
			writeServerErr(w, terr)
			return
		}
		gitToken = tok
	}
	if gitToken != "" {
		if cerr := syncx.StoreCredential(st.Root, pr.Repo.CloneURL, cfgServer.Username, gitToken); cerr != nil {
			if errors.Is(cerr, syncx.ErrNoCredentialHelper) {
				remote = syncx.CredentialURLWithAuth(pr.Repo.CloneURL, cfgServer.Username, gitToken)
				credNote = "（凭据已内嵌 remote URL（本机无 git credential helper）——注意：项目 config.toml 会随仓同步，凭据将进入远端 git 历史。建议配置 credential helper 后重新绑定）"
			} else {
				writeErr(w, http.StatusInternalServerError, "写入 git 凭据失败："+cerr.Error())
				return
			}
		}
	} else if !syncx.HasCredentialHelper(st.Root) {
		writeJSON(w, http.StatusOK, map[string]any{"status": "error", "clone_url": pr.Repo.CloneURL,
			"message": "仓已存在但本机无 git 凭据（token 仅建仓首发、本机无 credential helper）：请在终端手工绑定或联系管理员重置密码重发 token"})
		return
	}
	repo := syncx.Open(st.Root)
	if !repo.IsRepo() {
		kind, ierr := repo.InitForSync(remote, hasKnowledgeContent(st), "sync: init "+syncCommitMsg())
		switch {
		case errors.Is(ierr, syncx.ErrRemoteNotEmpty):
			writeErr(w, http.StatusConflict, "远端仓已有内容，需手动合并一次（git pull --rebase origin main 或 merge --allow-unrelated-histories）后重试")
			return
		case ierr != nil:
			writeErr(w, http.StatusInternalServerError, "初始化失败："+ierr.Error())
			return
		}
		_ = kind
	} else if err := repo.SetRemote(remote); err != nil {
		writeErr(w, http.StatusInternalServerError, "绑定远端失败："+err.Error())
		return
	}
	cfg, _ := config.LoadMerged(st.ConfigPath(), "")
	if err := config.SetSync(st.ConfigPath(), config.Sync{Enabled: true, Remote: remote, AutoIntervalMin: cfg.Sync.AutoIntervalMin}); err != nil {
		writeErr(w, http.StatusInternalServerError, "同步配置写入失败："+err.Error())
		return
	}
	o := syncx.SyncOnce(st.Root, syncCommitMsg())
	syncx.RecordOutcome(st.Root, st.StateDir(), o)
	msg := describeOutcome(o) + credNote
	switch {
	case o.Err != nil:
		writeJSON(w, http.StatusOK, map[string]any{"status": "error", "clone_url": pr.Repo.CloneURL, "message": msg})
	case len(o.Conflicts) > 0:
		writeJSON(w, http.StatusOK, map[string]any{"status": "conflict", "clone_url": pr.Repo.CloneURL, "message": msg})
	default:
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "clone_url": pr.Repo.CloneURL, "message": msg})
	}
}
```

注意 `store` 包导入（`openknowledge/internal/store`）如未导入则补上；`serverx`、`errors`、`net/http` 已在用。

- [ ] **Step 3: 跑 gui 服务器页测试确认重构无回归**

Run: `go test ./internal/gui/ -run 'TestApiServer' -v`
Expected: 全部 PASS（含 TestApiServerReposNoTokenNoHelper——fake 无 git-token 路由 → 404 → 新报错消息含"仓已存在但本机无 git 凭据"前缀，原断言仍过）

- [ ] **Step 4: Commit**

```bash
git add internal/gui/api_server.go
git commit -m "refactor(gui): 抽取 serveBindRepo 共用绑定管线 + 空 token 时自助重发凭据"
```

---

### Task 4: gui `POST /api/server/pull`（注册空壳 + clone）

**Files:**
- Modify: `internal/gui/api_server.go`（路由 + handler）
- Test: `internal/gui/api_server_test.go`

**Interfaces:**
- Consumes: Task 3 的 `serveBindRepo`；`registry.Update(func(*registry.Registry) error)`（参照 internal/cli/cli.go 用法）；`registry.Project{Name, Paths}`、`validProjectName`（api.go）；`store.Store.EnsureDirs()`；`findProject(name)`。
- Produces: 端点 `POST /api/server/pull` `{project}` → 响应同 serveBindRepo 管线（200 status ok/error/conflict）；已注册 409、名字非法 400。Task 8 前端调用。

- [ ] **Step 1: 写失败测试**（追加 api_server_test.go）

```go
// TestApiServerPullNewMachine 新机器语义：项目本机未注册 → pull 注册空 Paths 壳 +
// 走 clone 路径（provision 返回 file:// 裸仓且空 git_token → 自助重发拿凭据）。
func TestApiServerPullNewMachine(t *testing.T) {
	h, _, okHome := newEnv(t)
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))
	t.Setenv("GIT_CONFIG_SYSTEM", filepath.Join(t.TempDir(), "gitconfig-sys"))
	// "远端"已有内容的裸仓（模拟另一台机器已推送）
	bare := t.TempDir()
	gitRun(t, bare, "init", "--bare", "-b", "main")
	bareURL := "file://" + filepath.ToSlash(bare)
	seed := t.TempDir()
	gitRun(t, seed, "init", "-b", "main")
	if err := os.MkdirAll(filepath.Join(seed, "knowledge"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(seed, "knowledge", "k.md"), []byte("远端知识\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, seed, "add", ".")
	gitRun(t, seed, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-m", "sync: seed")
	gitRun(t, seed, "remote", "add", "origin", bareURL)
	gitRun(t, seed, "push", "-u", "origin", "main")
	// fake：provision 返回该裸仓 + 空 token（仓已存在）；另注册 git-token 重发路由
	fake := fakeOKServer(t, bareURL, "", func(mux *http.ServeMux) {
		mux.HandleFunc("POST /api/v1/git-token", func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]string{"git_token": "git-tok-reissue"})
		})
	})
	defer fake.Close()
	srv := httptest.NewServer(h)
	defer srv.Close()
	res, body := do(t, "POST", srv.URL+"/api/server/login", testToken, map[string]any{"url": fake.URL, "username": "alice", "password": "pw"})
	if res != 200 {
		t.Fatalf("login: %d %s", res, body)
	}
	// 未注册直接 pull（本机全新）
	res, body = do(t, "POST", srv.URL+"/api/server/pull", testToken, map[string]any{"project": "freshproj"})
	if res != 200 {
		t.Fatalf("pull: %d %s", res, body)
	}
	var out struct {
		Status  string `json:"status"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatal(err)
	}
	if out.Status != "ok" {
		t.Fatalf("out: %s", body)
	}
	// 壳项目已注册且无关联路径
	reg, err := registry.Load(registry.DefaultPath())
	if err != nil {
		t.Fatal(err)
	}
	var found *registry.Project
	for i := range reg.Projects {
		if reg.Projects[i].Name == "freshproj" {
			found = &reg.Projects[i]
		}
	}
	if found == nil || len(found.Paths) != 0 {
		t.Fatalf("shell project: %+v", found)
	}
	// 内容已 clone 下来
	data, err := os.ReadFile(filepath.Join(okHome, "projects", "freshproj", "knowledge", "k.md"))
	if err != nil || !strings.Contains(string(data), "远端知识") {
		t.Fatalf("cloned content: %v %q", err, data)
	}
	// 再 pull 一次 → 409 已注册
	res, _ = do(t, "POST", srv.URL+"/api/server/pull", testToken, map[string]any{"project": "freshproj"})
	if res != 409 {
		t.Fatalf("re-pull: %d", res)
	}
}
```

同时给 `fakeOKServer` 加变参（签名改 `func fakeOKServer(t *testing.T, cloneURL, gitToken string, extra ...func(*http.ServeMux)) *httptest.Server`，函数体 `return httptest.NewServer(mux)` 前遍历 `for _, f := range extra { f(mux) }`）——既有调用方零改动。

测试前置知识：clone 路径克隆的是**仓根**结构——真实项目仓根即 st.Root（knowledge/ 是子目录），所以 seed 里按 `knowledge/k.md` 落文件，断言路径与之一致。`gitRun`/`newEnv`/`do`/`mkProjectAt`/`stFor` 均为既有夹具；`registry` 包如未导入测试文件则补导入。`CloneToDir`（syncx/repo.go:147）是克隆到临时目录再移 `.git` 进 st.Root + checkout，EnsureDirs 预建的空 knowledge/ 不冲突。

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/gui/ -run TestApiServerPullNewMachine -v`
Expected: FAIL（404，端点不存在；或 fakeOKServer 签名编译错——先加变参再确认 404 失败）

- [ ] **Step 3: 实现路由 + handler**（`internal/gui/api_server.go`）

路由（registerServerAPI 内，加在 repos 行后）：

```go
	api("POST /api/server/pull", h.apiServerPull)
```

handler（放 apiServerRepos 后）：

```go
// apiServerPull 新机器拉取：服务器有仓、本机未注册的项目——注册空 Paths 壳
// （备份恢复同款先例）后走共用绑定管线（本地无内容 → InitForSync 自动 clone）。
func (h *Handler) apiServerPull(w http.ResponseWriter, r *http.Request) {
	var req serverReposRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if !validProjectName(req.Project) {
		writeErr(w, http.StatusBadRequest, fmt.Sprintf("非法项目名: %q", req.Project))
		return
	}
	_, _, found, err := findProject(req.Project)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if found {
		writeErr(w, http.StatusConflict, "项目已注册："+req.Project)
		return
	}
	// 锁内读-改-写注册空壳（并发 ok init / GUI 删除互斥，与 cli.go init 同口径）；
	// 二次检查放锁内，防并发双注册。
	if err := registry.Update(func(reg *registry.Registry) error {
		for _, p := range reg.Projects {
			if p.Name == req.Project {
				return fmt.Errorf("项目 %q 已存在", req.Project)
			}
		}
		reg.Projects = append(reg.Projects, registry.Project{Name: req.Project})
		return nil
	}); err != nil {
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
	st := store.New(filepath.Join(registry.Home(), "projects", req.Project))
	if err := st.EnsureDirs(); err != nil {
		writeErr(w, http.StatusInternalServerError, "创建项目目录失败："+err.Error())
		return
	}
	h.serveBindRepo(w, r, st, req.Project)
}
```

导入补充：`openknowledge/internal/registry`、`openknowledge/internal/store`、`fmt`、`path/filepath`（按 api_server.go 现有导入查漏补缺）。

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/gui/ -v -run 'TestApiServer'`
Expected: 全部 PASS

- [ ] **Step 5: Commit**

```bash
git add internal/gui/api_server.go internal/gui/api_server_test.go
git commit -m "feat(gui): POST /api/server/pull 新机器注册空壳项目并 clone 服务器仓"
```

---

### Task 5: syncx syncing 标记文件

**Files:**
- Modify: `internal/syncx/status.go`（MarkSyncing/IsSyncing）
- Modify: `internal/syncx/sync.go`（SyncOnce 接入）
- Test: `internal/syncx/status_test.go`

**Interfaces:**
- Consumes: 既有 `StateDir` 约定（`<root>/state/`）、`fsx` 无需用（直接 os 写即可，与 WriteConflictFiles 风格一致用 fsx.WriteFile 更好——看 status.go 现有导入，沿用）。
- Produces: `syncx.MarkSyncing(stateDir string) func()`（返回清除函数）；`syncx.IsSyncing(stateDir string) bool`；`syncx.SyncingStale = 5 * time.Minute`。Task 6 的 gui 用 IsSyncing。

- [ ] **Step 1: 写失败测试**（追加 status_test.go）

```go
// TestSyncingMarker 标记写入→IsSyncing true→清除→false；stale（mtime 超阈）→ false。
func TestSyncingMarker(t *testing.T) {
	dir := t.TempDir()
	if IsSyncing(dir) {
		t.Fatal("no marker should be false")
	}
	unmark := MarkSyncing(dir)
	if !IsSyncing(dir) {
		t.Fatal("fresh marker should be true")
	}
	unmark()
	if IsSyncing(dir) {
		t.Fatal("removed marker should be false")
	}
	// stale：手工把标记 mtime 拨到 10 分钟前
	MarkSyncing(dir)
	stale := time.Now().Add(-10 * time.Minute)
	if err := os.Chtimes(filepath.Join(dir, "syncing"), stale, stale); err != nil {
		t.Fatal(err)
	}
	if IsSyncing(dir) {
		t.Fatal("stale marker should be false")
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/syncx/ -run TestSyncingMarker -v`
Expected: FAIL（undefined）

- [ ] **Step 3: 实现**（status.go 追加）

```go
// SyncingStale 是 syncing 标记的新鲜阈值：超过视为进程死亡残留，忽略。
const SyncingStale = 5 * time.Minute

func syncingPath(stateDir string) string { return filepath.Join(stateDir, "syncing") }

// MarkSyncing 写"同步进行中"标记（state/syncing），返回清除函数（defer 调用）。
// fail-open：写失败返回 no-op——标记缺失只影响状态点闪烁，不影响同步本体。
func MarkSyncing(stateDir string) func() {
	noop := func() {}
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return noop
	}
	if err := fsx.WriteFile(syncingPath(stateDir), []byte(time.Now().Format(time.RFC3339)), 0o644); err != nil {
		return noop
	}
	return func() { _ = os.Remove(syncingPath(stateDir)) }
}

// IsSyncing 读标记；缺失/超阈（stale，进程死亡残留）均视为未在同步。
func IsSyncing(stateDir string) bool {
	fi, err := os.Stat(syncingPath(stateDir))
	if err != nil {
		return false
	}
	return time.Since(fi.ModTime()) < SyncingStale
}
```

- [ ] **Step 4: SyncOnce 接入**（sync.go，执行者分支；等待者不打标）

```go
	defer flights.Delete(dir)
	// 同步进行中标记（GUI 状态点"黄闪"数据源）；非仓目录不打（Sync 会原样返回 NotRepo）。
	unmark := func() {}
	if Open(dir).IsRepo() {
		unmark = MarkSyncing(filepath.Join(dir, "state"))
	}
	defer unmark()
	f.o = Open(dir).Sync(msg)
```

sync.go 需补 `path/filepath` 导入。

- [ ] **Step 5: 跑测试确认通过**

Run: `go test ./internal/syncx/ -v`
Expected: 全部 PASS

- [ ] **Step 6: Commit**

```bash
git add internal/syncx/status.go internal/syncx/sync.go internal/syncx/status_test.go
git commit -m "feat(syncx): SyncOnce 同步进行中标记（state/syncing，stale 5min 守卫）"
```

---

### Task 6: gui projectSyncStatus 加 dirty/syncing 字段

**Files:**
- Modify: `internal/gui/api.go:341-376`（syncStatusJSON + projectSyncStatus）
- Modify: `internal/gui/api_sync.go:55-63`（hasKnowledgeContent 旁加 knowledgeDirty）
- Test: `internal/gui/api_test.go`（或 api_sync 既有测试文件，看既有 sync 状态测试落点，沿用）

**Interfaces:**
- Consumes: Task 5 的 `syncx.IsSyncing`；`st.KnowledgeDir()`、`syncx.LayerStatus.LastSync`。
- Produces: `syncStatusJSON` 新增 `Dirty bool \`json:"dirty"\``、`Syncing bool \`json:"syncing"\``；`knowledgeDirty(st *store.Store, lastSync time.Time) bool`。Task 7 前端依赖这两个字段名。

- [ ] **Step 1: 写失败测试**

参照 api_test.go 既有 projectSyncStatus 测试（找 `syncStatusJSON` 或 `/api/projects` sync 字段断言的用例）追加：

```go
// TestProjectSyncStatusDirtySyncing dirty：knowledge .md mtime 晚于 LastSync → true，
// 早于 → false；syncing 标记存在 → true。
func TestProjectSyncStatusDirtySyncing(t *testing.T) {
	h, _, okHome := newEnv(t)
	srv := httptest.NewServer(h)
	defer srv.Close()
	mkProjectAt(t, okHome, "dirtydemo", filepath.Join(t.TempDir(), "src"))
	st := stFor(t, okHome, "dirtydemo")
	if err := st.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	// 从未同步（LastSync 零值）+ 有内容 → dirty
	if err := os.WriteFile(filepath.Join(st.KnowledgeDir(), "a.md"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, body := do(t, "GET", srv.URL+"/api/projects", testToken, nil)
	if res != 200 {
		t.Fatalf("projects: %d", res)
	}
	var ps []struct {
		Name string `json:"name"`
		Sync struct {
			Dirty   bool `json:"dirty"`
			Syncing bool `json:"syncing"`
		} `json:"sync"`
	}
	if err := json.Unmarshal([]byte(body), &ps); err != nil {
		t.Fatal(err)
	}
	var got *struct {
		Name string `json:"name"`
		Sync struct {
			Dirty   bool `json:"dirty"`
			Syncing bool `json:"syncing"`
		} `json:"sync"`
	}
	for i := range ps {
		if ps[i].Name == "dirtydemo" {
			got = &ps[i]
		}
	}
	if got == nil || !got.Sync.Dirty {
		t.Fatalf("dirty expected: %s", body)
	}
	// LastSync 拨到现在 → 不 dirty
	sf := &syncx.StatusFile{Layers: map[string]*syncx.LayerStatus{}}
	sf.Layer("personal").LastSync = time.Now().Add(time.Minute)
	if err := sf.Save(st.StateDir()); err != nil {
		t.Fatal(err)
	}
	res, body = do(t, "GET", srv.URL+"/api/projects", testToken, nil)
	if res != 200 {
		t.Fatalf("projects 2: %d", res)
	}
	ps = nil
	if err := json.Unmarshal([]byte(body), &ps); err != nil {
		t.Fatal(err)
	}
	for _, p := range ps {
		if p.Name == "dirtydemo" && p.Sync.Dirty {
			t.Fatal("dirty should be false after LastSync now")
		}
	}
	// syncing 标记
	unmark := syncx.MarkSyncing(st.StateDir())
	defer unmark()
	res, body = do(t, "GET", srv.URL+"/api/projects", testToken, nil)
	if res != 200 {
		t.Fatalf("projects 3: %d", res)
	}
	if !strings.Contains(string(body), `"syncing":true`) {
		t.Fatalf("syncing expected: %s", body)
	}
}
```

注：`syncx`/`time`/`strings`/`encoding/json` 按测试文件现有导入补齐；匿名 struct 重复三段嫌丑的话，在测试文件顶部提一个 `type syncProbe struct{...}` 复用——与既有测试风格保持一致即可。

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/gui/ -run TestProjectSyncStatusDirtySyncing -v`
Expected: FAIL（JSON 无 dirty/syncing 字段 → 断言失败）

- [ ] **Step 3: 实现**

api.go 的 syncStatusJSON 加两字段：

```go
type syncStatusJSON struct {
	Enabled  bool `json:"enabled"`
	IsRepo   bool `json:"is_repo"`
	Ahead    int  `json:"ahead"`
	Behind   int  `json:"behind"`
	Conflict bool `json:"conflict"`
	Dirty    bool `json:"dirty"`
	Syncing  bool `json:"syncing"`
}
```

projectSyncStatus 返回处加两行：

```go
		Conflict: l.Conflict || repo.MergeInProgress(),
		Dirty:    knowledgeDirty(st, l.LastSync),
		Syncing:  syncx.IsSyncing(st.StateDir()),
```

api_sync.go 的 hasKnowledgeContent 旁加：

```go
// knowledgeDirty：knowledge/ 下最新 .md 的 mtime 晚于 lastSync = 有未同步变更。
// 纯文件系统检查（一次 ReadDir），不起 git 子进程；LastSync 零值（从未同步）且有内容即为 dirty。
func knowledgeDirty(st *store.Store, lastSync time.Time) bool {
	entries, err := os.ReadDir(st.KnowledgeDir())
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		if fi, err := e.Info(); err == nil && fi.ModTime().After(lastSync) {
			return true
		}
	}
	return false
}
```

`time` 导入补上。

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/gui/ -v`
Expected: 全部 PASS

- [ ] **Step 5: Commit**

```bash
git add internal/gui/api.go internal/gui/api_sync.go internal/gui/api_test.go
git commit -m "feat(gui): 项目同步状态加 dirty（mtime 对比）与 syncing（标记文件）字段"
```

---

### Task 7: 前端五态状态点

**Files:**
- Modify: `web/app.js`（syncDotClass/syncDotKey ~line 1859-1871、zh 字典 ~line 202、en 字典 ~line 399、pollManage ~line 1257）
- Modify: `web/style.css`（.pj-sync-dot 区 ~line 590）

**Interfaces:**
- Consumes: Task 6 的 `p.sync.dirty`、`p.sync.syncing` 字段。
- Produces: 状态点 class 五态：`off|conflict|syncing|ahead|synced`；字典 key `syncDotSyncing`。

- [ ] **Step 1: syncDotClass/syncDotKey 改写**（优先级：冲突 > 同步中 > 有变更 > 已同步 > 未启用）

```js
function syncDotClass(sy){
  if(!sy || !sy.enabled || !sy.is_repo) return "off";
  if(sy.conflict) return "conflict";
  if(sy.syncing) return "syncing";
  if(sy.dirty || sy.ahead > 0 || sy.behind > 0) return "ahead";
  return "synced";
}
function syncDotKey(sy){
  if(!sy || !sy.enabled || !sy.is_repo) return "syncDotOff";
  if(sy.conflict) return "syncDotConflict";
  if(sy.syncing) return "syncDotSyncing";
  if(sy.dirty || sy.ahead > 0 || sy.behind > 0) return "syncDotAhead";
  return "syncDotSynced";
}
```

- [ ] **Step 2: 字典加 key**

zh 段（syncDotSynced 那行）：`syncDotSyncing:"正在同步",`
en 段：`syncDotSyncing:"Syncing",`

- [ ] **Step 3: pollManage 变化检测补 sync 比对**（否则状态变化不重渲）

在 `const lu = {}; ps.forEach(...)` 后加：

```js
    const syMap = {}; ps.forEach(p=>{ syMap[p.name] = JSON.stringify(p.sync||null); });
    const syChanged = MGMT.list.some(p=>JSON.stringify(p.sync||null) !== syMap[p.name]);
```

把 `if(!luChanged && !newEntries) return;` 改为 `if(!luChanged && !newEntries && !syChanged) return;`，并在 `MGMT.list.forEach(p=>{ p.lastUpdate = ... })` 同一循环里加 `p.sync = ps.find(q=>q.name===p.name).sync;`（或单独 forEach 更新 sync 字段）。

- [ ] **Step 4: CSS**（style.css `.pj-sync-dot.conflict` 行后）

```css
.pj-sync-dot.syncing{ background:#d4a72c; animation:okblink 1s ease-in-out infinite; }
@keyframes okblink{ 0%,100%{ opacity:1; } 50%{ opacity:.25; } }
```

- [ ] **Step 5: 手工验证**（无前端测试框架）

起本地 daemon + GUI：管理页状态点 tooltip 四态文案仍正确；`syncDotSyncing` key 两种语言均有值（控制台 `t("syncDotSyncing")` 不返回 key 本体）。

- [ ] **Step 6: Commit**

```bash
git add web/app.js web/style.css
git commit -m "feat(web): 同步状态点五态——dirty 即黄、syncing 黄闪、轮询补 sync 比对"
```

---

### Task 8: 前端可拉取分组 + 全部拉取 + maybeAutoBind 扩展

**Files:**
- Modify: `web/app.js`（bindCard ~line 5946、maybeAutoBind ~line 5634、zh/en 字典）

**Interfaces:**
- Consumes: Task 4 的 `POST /api/server/pull`；既有 `SRV.user.repos`（RepoInfo：`{layer, owner, project, name, clone_url, ...}`）、`SRV.projects`（`{name, sync}`）、`api()`、`toast()`、`uiConfirm()`、`t()`、`esc()`、`loadServerRoleData()`、`refreshManage()`、`srvBind`。
- Produces: `srvPull(project)` 前端函数；字典 key（zh/en）：`srvPullable`、`srvPull`、`srvPullAll`、`srvPulling`、`srvPullDone`、`srvPullFail`、`srvAutoBindConfirm2`。

- [ ] **Step 1: 字典加 key**

zh 段：

```js
    srvPullable:"服务器仓（本机未拉取）", srvPull:"拉取", srvPullAll:"全部拉取", srvPulling:"拉取中…",
    srvPullDone:"已拉取 {n} 个项目到本机", srvPullFail:"，失败：",
    srvAutoBindConfirm2:"服务器上有 {n} 个本机尚未拉取的项目仓：{l}\n\n是否现在拉取到本机？（注册同名项目并克隆全部知识条目）",
```

en 段：

```js
    srvPullable:"Server repos (not on this machine)", srvPull:"Pull", srvPullAll:"Pull all", srvPulling:"Pulling…",
    srvPullDone:"Pulled {n} projects to this machine", srvPullFail:", failed: ",
    srvAutoBindConfirm2:"The server has {n} project repos not on this machine: {l}\n\nPull them now? (registers same-named projects and clones all entries)",
```

- [ ] **Step 2: srvPull 函数 + bindCard 分组**（srvBind 函数旁加 srvPull）

```js
/* 服务器有仓、本机未注册：注册壳 + clone（Task 4 端点） */
async function srvPull(project){
  SRV.bindBusy[project] = true; render();
  try{
    const r = await api("/api/server/pull", { method:"POST", body:{ project: project }, skip401Reload:true });
    toast(r.message || t("srvPullDone").replace("{n}", 1), r.status === "error");
    SRV.projects = null; loadServerRoleData(); refreshManage();
  }catch(err){ toast(err.message, true); }
  SRV.bindBusy[project] = false;
  if(state.menu === "server") render();
}

// 服务器仓中本机未注册的项目列表（personal 层）
function srvPullable(){
  const mine = (SRV.user && SRV.user.repos) || [];
  const local = {}; (SRV.projects || []).forEach(p=>{ local[p.name] = true; });
  return mine.filter(r=>r.layer === "personal" && !local[r.project]).map(r=>r.project);
}
```

bindCard 内 `(SRV.projects || []).forEach(...)` 循环结束后、`card.appendChild(tb)` 之前……实际结构：表格 append 后再加分组——在 `card.appendChild(tb);` 之后加：

```js
  // 服务器有仓、本机未拉取分组（一键拉取 / 全部拉取）
  const pullable = srvPullable();
  if(pullable.length){
    const gh = el("h3"); gh.style.marginTop = "18px"; gh.textContent = t("srvPullable"); card.appendChild(gh);
    const pt = el("table","list");
    pt.innerHTML = '<tr><th>'+t("srvProject")+'</th><th style="text-align:right">'+t("srvActions")+'</th></tr>';
    pullable.forEach(name=>{
      const tr = el("tr","");
      const td1 = el("td",""); td1.innerHTML = '<b>'+esc(name)+'</b>';
      const td2 = el("td",""); td2.style.textAlign = "right";
      const btn = el("button","btn btn-primary");
      btn.textContent = SRV.bindBusy[name] ? t("srvPulling") : t("srvPull");
      btn.disabled = !!SRV.bindBusy[name];
      btn.onclick = ()=>srvPull(name);
      td2.appendChild(btn);
      tr.appendChild(td1); tr.appendChild(td2); pt.appendChild(tr);
    });
    card.appendChild(pt);
    if(pullable.length > 1){
      const all = el("button","btn"); all.style.marginTop = "8px"; all.textContent = t("srvPullAll");
      all.onclick = ()=>srvPullAll(pullable);
      card.appendChild(all);
    }
  }
```

srvPullAll（srvPull 旁）：

```js
async function srvPullAll(names){
  let done = 0; const failed = [];
  for(const name of names){
    SRV.bindBusy[name] = true;
    if(state.menu === "server") render();
    try{
      const r = await api("/api/server/pull", { method:"POST", body:{ project: name }, skip401Reload:true });
      if(r.status === "error") failed.push(name); else done++;
    }catch(_){ failed.push(name); }
    SRV.bindBusy[name] = false;
  }
  toast(t("srvPullDone").replace("{n}", done) + (failed.length ? t("srvPullFail")+failed.join("、") : ""), failed.length > 0);
  SRV.projects = null; loadServerRoleData(); refreshManage();
  if(state.menu === "server") render();
}
```

- [ ] **Step 3: maybeAutoBind 扩展**（本机未注册的服务器仓也进候选）

`maybeAutoBind` 现状（web/app.js:5634-5660）只遍历本地项目。在 `if(!cands.length) return;` 之前插入第二候选集，并改确认与执行段：

```js
    // 第二候选集：服务器有仓、本机未注册（新机器场景）——注册壳 + clone
    const localNames = {}; (ps || []).forEach(p=>{ localNames[p.name] = true; });
    const remoteOnly = (me.repos || []).filter(r=>r.layer === "personal" && !localNames[r.project]).map(r=>r.project);
    if(!cands.length && !remoteOnly.length) return;
    let msg = "";
    if(cands.length) msg += t("srvAutoBindConfirm").replace("{n}", cands.length).replace("{l}", cands.join("、"));
    if(remoteOnly.length){
      if(msg) msg += "\n\n";
      msg += t("srvAutoBindConfirm2").replace("{n}", remoteOnly.length).replace("{l}", remoteOnly.join("、"));
    }
    if(!await uiConfirm(msg)) return;
    let done = 0; const failed = [];
    for(const name of cands){
      try{
        const r = await api("/api/server/repos", { method:"POST", body:{ project: name }, skip401Reload:true });
        if(r.status === "error") failed.push(name); else done++;
      }catch(_){ failed.push(name); }
    }
    for(const name of remoteOnly){
      try{
        const r = await api("/api/server/pull", { method:"POST", body:{ project: name }, skip401Reload:true });
        if(r.status === "error") failed.push(name); else done++;
      }catch(_){ failed.push(name); }
    }
    toast(t("srvAutoBindDone").replace("{n}", done) + (failed.length ? t("srvAutoBindFail")+failed.join("、") : ""), failed.length > 0);
    SRV.projects = null; SRV.repos = null; loadServerRoleData();
    refreshManage();
```

（即：原 `if(!cands.length) return;` 及确认/执行/toast 段整段替换为上面形态；函数头尾与 fail-open catch 不动。）

注意：`me` 的获取在现状代码里是 `SRV.user && SRV.user.repos ? SRV.user : await api("/api/server/me", ...)`，remoteOnly 依赖 `me.repos`——确认该对象含 `layer` 字段（serverx MeInfo.Repos 是 RepoInfo，含 Layer/Project；前端镜像同名字段小写，核对 `/api/server/me` 实际返回 JSON 的字段名 `layer`/`project`）。

- [ ] **Step 4: 手工验证**

起 daemon + GUI 连测试 okserver：服务器页成员视图出现「服务器仓（本机未拉取）」分组；单拉、全部拉取、登录自动弹窗三路径走通；失败项进 toast 不阻断。

- [ ] **Step 5: Commit**

```bash
git add web/app.js
git commit -m "feat(web): 服务器仓可拉取分组（单拉+全部拉取）+ 登录自动提示本机未注册仓"
```

---

### Task 9: 全量回归 + 终审 + 真机走查

- [ ] **Step 1: 全量测试**

Run: `go test ./...`
Expected: 全部 PASS

- [ ] **Step 2: 前端静态自检**

Run: `node --check web/app.js`（有 node 的话）或浏览器控制台无语法错误
Expected: 无语法错误

- [ ] **Step 3: 真机走查清单**（文档化在终审报告里逐项过）

1. 新机器语义：删测试项目注册 → 服务器页出现可拉取分组 → 全部拉取成功 → 管理页项目出现且条目齐。
2. 登录自动弹窗：清注册表 → 重新登录 → 弹窗列出未注册仓 → 确认后拉取成功。
3. 旧服务端语义：fake/旧版无 git-token → 报错文案提示升级 okserver。
4. 状态点：改一条目 → ≤4s 黄点 → 等 30s+ 防抖同步 → 黄闪 → 绿；冲突场景仍红点可点。
5. 强制改密账号登录新机器：先弹改密 → 改完才能拉取（git-token 过 gate）。

- [ ] **Step 4: 终审**（dispatch 评审 subagent，对照 spec 逐条核）

- [ ] **Step 5: Commit 收尾**（如有终审修复）

```bash
go test ./... && git add -A && git commit -m "fix: 终审修复"
```
