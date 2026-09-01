# 统一 git 凭证为「每台机器一条」实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 废除建用户/建仓时的 git token 发放，统一为按机器分名（`ok-sync-r-<hostname>`）的自助重发通道，并提供本机自动迁移与凭证页一键清理。

**Architecture:** `apiGitToken` 成为唯一凭证发放口（服务端删两条发放路径）；新包 `internal/credmig` 承载机器级"确保新凭证+覆盖 remote+迁移标记"，daemon 同步周期与 GUI 清理按钮两个入口共用；客户端绑定管线砍掉 provision token 分支，file:// remote 跳过凭据环节。

**Tech Stack:** Go（oksrv / serverx / gui / daemon / credmig）、原生 JS（web/app.js）、Gitea token API。

## Global Constraints

- Spec：`docs/superpowers/specs/2026-09-01-unified-git-credential-design.md`（已定稿）。
- TDD：行为变化类测试先改断言、验证对旧代码**变红**，再写实现（项目已有约定）。
- 凭证命名规则不变：`ok-sync-r-<sanitize(hostname)>`，sanitize 只留 `[a-zA-Z0-9_-]`，服务端 `sanitizeTokenName` 已有，不改。
- 不新增服务端接口；清理能力复用 `GET/DELETE /api/v1/tokens`。
- GUI 交互两段式：清理按钮确认前零副作用。
- i18n：`web/app.js` 中英字典同步加/删 key（zh 约 238-243 行区，en 约 451-456 行区）。
- `internal/gui/api_server_test.go` 是混合 CRLF 文件，Edit 时 old_string 需带 `\r`。
- 版本 bump / changelog / 发布不在本计划范围（另行走 `scripts/sync-version.sh` 纪律）。

---

### Task 1: oksrv 建仓停发 token

**Files:**
- Modify: `internal/oksrv/http.go:231-272`（apiPersonalRepo）
- Test: `internal/oksrv/http_test.go`（TestFullManagementFlow，127-140 行区）

**Interfaces:**
- Consumes: 现有 `s.backend.CreatePersonalRepo`、`s.st.UpsertRepo`、`gitRepoJSON`。
- Produces: `POST /api/v1/repos/personal` 响应 `git_token` 恒为 `""`（字段保留，JSON 形状不变）。后续 Task 5 依赖这一语义。

- [ ] **Step 1: 改测试断言（红）**

`internal/oksrv/http_test.go` TestFullManagementFlow 中，把 `srv, st, _ := newTestServer(t)` 改为 `srv, st, backend := newTestServer(t)`，并将建仓段（原 127-140 行）改为：

```go
	// 成员建仓（幂等）；凭证统一（2026-09-01）：建仓不再下发 token，git_token 恒空
	code, out = call(t, srv, "POST", "/api/v1/repos/personal", aliceTok, map[string]string{"project": "demo"})
	if code != 200 {
		t.Fatalf("personal repo: %d %v", code, out)
	}
	repo := out["repo"].(map[string]any)
	if repo["owner"] != "alice" || repo["name"] != "ok-demo" || out["git_token"] != "" {
		t.Fatalf("repo: %v token=%v", repo, out["git_token"])
	}
	// 幂等：再来一次，git_token 仍为空串
	code, out = call(t, srv, "POST", "/api/v1/repos/personal", aliceTok, map[string]string{"project": "demo"})
	if code != 200 || out["git_token"] != "" {
		t.Fatalf("idempotent: %d %v", code, out)
	}
	// 两次建仓全程不得发放任何 token（唯一通道 = apiGitToken）
	if toks, err := backend.ListUserTokens(context.Background(), "alice"); err != nil || len(toks) != 0 {
		t.Fatalf("no token may be issued by provisioning: %v %v", toks, err)
	}
```

- [ ] **Step 2: 跑测试确认变红**

Run: `go test ./internal/oksrv/ -run TestFullManagementFlow -v`
Expected: FAIL（旧代码首次建仓 `git_token` 非空 / alice 持有 token）

- [ ] **Step 3: 改实现**

`internal/oksrv/http.go` apiPersonalRepo：函数注释（231-232 行）改为：

```go
// apiPersonalRepo 建个人仓 ok-<project>（幂等：已存在返回现有记录）。凭证统一
// （2026-09-01）：建仓不再下发 token，git_token 恒空串——客户端拿到空串走自助
// 重发（apiGitToken，按机器分名 ok-sync-r-<hostname>）。
```

函数尾部（原 263-271 行）改为：

```go
	// 审计随事实落库：仓已建成。
	s.st.Audit(u.Username, "create-personal-repo", u.Username+"/"+in.Project, repo.CloneURL)
	writeJSON(w, http.StatusOK, map[string]any{"repo": gitRepoJSON(repo), "git_token": ""})
```

（删除 `CreateUserToken("ok-sync-"+in.Project)` 调用及其错误分支。）

- [ ] **Step 4: 跑测试确认变绿**

Run: `go test ./internal/oksrv/ -run TestFullManagementFlow -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/oksrv/http.go internal/oksrv/http_test.go
git commit -m "oksrv: 建仓不再下发 git token（凭证统一，唯一通道=自助重发）"
```

---

### Task 2: oksrv 建用户停发 ok-sync token

**Files:**
- Modify: `internal/oksrv/http.go:423-453`（apiUserCreate）
- Test: `internal/oksrv/http_test.go`（TestFullManagementFlow 建用户段、TestCreateUserTokenFailureRollback、TestApiTokens）
- Test: `tests/e2e/okserver_test.go:111-114`

**Interfaces:**
- Consumes: 无新增。
- Produces: `POST /api/v1/users` 响应不再含 `git_token` 字段（只 `username`+`password`）。Task 6 的管理面展示依赖此契约。

- [ ] **Step 1: 改测试断言（红）——四处**

1. `TestFullManagementFlow` 建用户段（原 112-120 行）：

```go
	// 建用户（一次性返回明文密码；凭证统一后不再附带 git token）
	code, out := call(t, srv, "POST", "/api/v1/users", rootTok, map[string]string{"username": "alice"})
	if code != 200 {
		t.Fatalf("create user: %d %v", code, out)
	}
	pw := out["password"].(string)
	if pw == "" {
		t.Fatalf("create user resp: %v", out)
	}
	if _, has := out["git_token"]; has {
		t.Fatalf("create user must not issue git_token anymore: %v", out)
	}
```

2. `TestCreateUserTokenFailureRollback`（原 334-363 行）整体改写——原"token 失败回滚"场景随停发消失，改为钉住"建用户全程不触碰 token"：

```go
// TestCreateUserNoTokenIssued 钉住凭证统一（2026-09-01）：建用户全程不调
// CreateUserToken（failTokens 注入下也必须 200），响应不含 git_token。
// 原"token 失败回滚"场景随建用户/建仓停发 token 一并消失。
func TestCreateUserNoTokenIssued(t *testing.T) {
	srv, st, backend := newTestServer(t)
	defer srv.Close()
	rootTok := login(t, srv, "root", getenv(t, "OK_TEST_ROOT_PW"))

	backend.SetFailTokens(true) // 若建用户仍调 CreateUserToken 会 502——红即回归
	code, out := call(t, srv, "POST", "/api/v1/users", rootTok, map[string]string{"username": "carol"})
	if code != 200 {
		t.Fatalf("create user must not touch tokens: %d %v", code, out)
	}
	if _, has := out["git_token"]; has {
		t.Fatalf("resp must not contain git_token: %v", out)
	}
	if st.GetUser("carol") == nil {
		t.Fatal("local user must exist")
	}
}
```

3. `TestApiTokens`：581 行注释与 604-606 行断言改为（bob 建用户不再自带 ok-sync）：

```go
	// 建 bob（member；凭证统一后建用户不再发 token），清强制改密标记后登录
```

```go
	names := tokenNames(out)
	if !names["ok-sync-r-NB1"] || len(names) != 1 {
		t.Fatalf("tokens must contain only ok-sync-r-NB1: %v", names)
	}
```

598 行注释同步改为 `// 2. bob 列自己的 token：只有刚重发的 ok-sync-r-NB1`。

4. `tests/e2e/okserver_test.go:111-114`：

```go
	code, out = call("POST", "/api/v1/users", rootTok, map[string]string{"username": "alice"})
	if code != 200 || out["password"] == nil {
		t.Fatalf("create alice: %d %v", code, out)
	}
```

- [ ] **Step 2: 跑测试确认变红**

Run: `go test ./internal/oksrv/ -run 'TestFullManagementFlow|TestCreateUserNoTokenIssued|TestApiTokens' -v`
Expected: FAIL（TestCreateUserNoTokenIssued 在旧代码下 502；TestFullManagementFlow 旧代码含 git_token；TestApiTokens 旧代码下列出 2 条）

- [ ] **Step 3: 改实现**

`internal/oksrv/http.go` apiUserCreate：423-424 行顺序注释与 429-436 行 token 步骤替换为：

```go
	// 顺序：Gitea 用户 → 本地用户（最后）。凭证统一（2026-09-01）：不再随建用户发
	// git token——用户首次绑定项目时其机器走 apiGitToken 自助获取（按机器分名）。
	if err := s.backend.CreateUser(r.Context(), in.Username, pw); err != nil {
		backendErr(w, err)
		return
	}
```

450-452 行响应改为：

```go
	writeJSON(w, http.StatusOK, map[string]any{
		"username": in.Username, "password": pw,
	})
```

- [ ] **Step 4: 跑测试确认变绿（含 e2e 编译）**

Run: `go test ./internal/oksrv/ -v && go vet ./tests/e2e/`
Expected: PASS（`go vet` 确认 e2e 编译；e2e 实跑留到 Task 7）

- [ ] **Step 5: Commit**

```bash
git add internal/oksrv/http.go internal/oksrv/http_test.go tests/e2e/okserver_test.go
git commit -m "oksrv: 建用户不再下发 ok-sync token（凭证统一）"
```

---

### Task 3: serverx.GitToken 回显 token_name

**Files:**
- Modify: `internal/serverx/serverx.go:190-201`
- Modify: `internal/gui/api_server.go:275`（唯一调用点，一行适配保持编译绿；Task 5 再重构该段）
- Test: `internal/serverx/serverx_test.go`

**Interfaces:**
- Consumes: 无。
- Produces: **签名变更** `func (c *Client) GitToken(ctx context.Context, nameHint string) (token, tokenName string, err error)`。Task 4（credmig）与 Task 5（serveBindRepo）都按此签名调用。

- [ ] **Step 1: 写失败测试（红）**

`internal/serverx/serverx_test.go` 新增（脚手架镜像该文件现有 httptest 用例）：

```go
// TestGitTokenReturnsName 凭证统一：重发响应回显 token_name（清理按钮保本机用）。
func TestGitTokenReturnsName(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/git-token" || r.Method != "POST" {
			w.WriteHeader(404)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"git_token": "tok-x", "token_name": "ok-sync-r-H1"})
	}))
	defer srv.Close()
	c := New(srv.URL, "tok")
	tok, name, err := c.GitToken(context.Background(), "H1")
	if err != nil || tok != "tok-x" || name != "ok-sync-r-H1" {
		t.Fatalf("GitToken: %v %q %q", err, tok, name)
	}
}
```

- [ ] **Step 2: 跑测试确认变红（编译失败即红）**

Run: `go test ./internal/serverx/ -run TestGitTokenReturnsName -v`
Expected: FAIL（旧签名单返回值，编译错误 `assignment mismatch`）

- [ ] **Step 3: 改实现 + 调用点适配**

`internal/serverx/serverx.go` GitToken 改为：

```go
// GitToken 自助重发 git token（okserver v2.25 起；旧服务端返回 404 *Error，
// 调用方据此提示升级）。nameHint 一般是本机 hostname——服务端按 ok-sync-r-<hint>
// 分名，多机互不吊销。返回明文 token 与凭证名（token_name 回显）。
func (c *Client) GitToken(ctx context.Context, nameHint string) (token, tokenName string, err error) {
	var out struct {
		GitToken  string `json:"git_token"`
		TokenName string `json:"token_name"`
	}
	if err := c.call(ctx, "POST", "/git-token", map[string]string{"name_hint": nameHint}, &out); err != nil {
		return "", "", err
	}
	return out.GitToken, out.TokenName, nil
}
```

`internal/gui/api_server.go:275` 一行适配：

```go
		tok, _, terr := c.GitToken(r.Context(), host)
```

- [ ] **Step 4: 跑测试确认变绿**

Run: `go test ./internal/serverx/ ./internal/gui/ -v 2>&1 | tail -30`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/serverx/serverx.go internal/serverx/serverx_test.go internal/gui/api_server.go
git commit -m "serverx: GitToken 回显 token_name（凭证统一）"
```

---

### Task 4: credmig 包 + daemon 同步周期迁移钩子

**Files:**
- Create: `internal/credmig/credmig.go`
- Test: `internal/credmig/credmig_test.go`
- Modify: `internal/daemon/sync.go:47` 后插入迁移钩子
- Test: `internal/daemon/sync_test.go` 新增 TestRunSyncCycleMigratesCredential

**Interfaces:**
- Consumes: `serverx.Client.GitToken(ctx, nameHint) (token, tokenName string, err error)`（Task 3）；`syncx.Open/.IsRepo/.RemoteURL/.SetRemote/.HasCredentialHelper/.StoreCredential/.StripURLAuth/.CredentialURLWithAuth`；`registry.Home()/DefaultPath()/Load`；`config.Server{URL, Username, Token}`。
- Produces:
  - `func Ensure(cfg config.Server) (tokenName string, err error)` — 重发本机凭证、覆盖所有已绑定项目 remote、写迁移标记；幂等。
  - `func Done(home, username string) bool` — 标记存在且用户名一致。
  - 标记文件：`registry.Home()/cred-migrated.json`。
  - Task 5 的 GUI ensure 端点消费 `Ensure`。

- [ ] **Step 1: 写失败测试（红）**

`internal/credmig/credmig_test.go`：

```go
package credmig

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"openknowledge/internal/config"
	"openknowledge/internal/registry"
	"openknowledge/internal/store"
	"openknowledge/internal/syncx"
)

// TestEnsure file:// remote 项目跳过凭据覆盖；重发被调一次；标记落盘；重复调幂等。
func TestEnsure(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OK_HOME", home)
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))
	t.Setenv("GIT_CONFIG_SYSTEM", filepath.Join(t.TempDir(), "gitconfig-sys"))
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/git-token" {
			w.WriteHeader(404)
			return
		}
		calls++
		_ = json.NewEncoder(w).Encode(map[string]string{"git_token": "tok-1", "token_name": "ok-sync-r-H"})
	}))
	defer srv.Close()
	// 一个 file:// remote 的已绑定项目
	st := store.New(filepath.Join(home, "projects", "p1"))
	if err := os.MkdirAll(st.Root, 0o755); err != nil {
		t.Fatal(err)
	}
	repo := syncx.Open(st.Root)
	if err := repo.Init(); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetRemote("file://" + filepath.ToSlash(t.TempDir())); err != nil {
		t.Fatal(err)
	}
	reg := &registry.Registry{Projects: []registry.Project{{Name: "p1"}}}
	if err := reg.Save(registry.DefaultPath()); err != nil {
		t.Fatal(err)
	}

	cfg := config.Server{URL: srv.URL, Username: "alice", Token: "tok"}
	name, err := Ensure(cfg)
	if err != nil || name != "ok-sync-r-H" {
		t.Fatalf("Ensure: %v %q", err, name)
	}
	if calls != 1 {
		t.Fatalf("reissue calls = %d, want 1", calls)
	}
	if !Done(home, "alice") {
		t.Fatal("marker must be written")
	}
	if _, err := Ensure(cfg); err != nil { // 幂等
		t.Fatalf("re-Ensure: %v", err)
	}
}

// TestDone 无标记 false；用户名不匹配 false。
func TestDone(t *testing.T) {
	home := t.TempDir()
	if Done(home, "alice") {
		t.Fatal("no marker must be false")
	}
	if err := os.WriteFile(filepath.Join(home, markerFile),
		[]byte(`{"username":"bob","hostname":"h","token_name":"ok-sync-r-h"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if Done(home, "alice") || !Done(home, "bob") {
		t.Fatal("username mismatch must be false")
	}
}
```

- [ ] **Step 2: 跑测试确认变红**

Run: `go test ./internal/credmig/ -v`
Expected: FAIL（包不存在，编译错误）

- [ ] **Step 3: 实现 credmig**

`internal/credmig/credmig.go`：

```go
// Package credmig 凭证统一（2026-09-01 设计）：机器级 git 凭证确保/迁移。
// 唯一发放通道 = okserver apiGitToken（ok-sync-r-<hostname>）。本包负责
// "本机持有一份新凭证 + 覆盖到所有已绑定项目 remote + 迁移标记"，
// 供 daemon 同步周期与 GUI 清理按钮两个入口共用。
package credmig

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"openknowledge/internal/config"
	"openknowledge/internal/registry"
	"openknowledge/internal/serverx"
	"openknowledge/internal/store"
	"openknowledge/internal/syncx"
)

const markerFile = "cred-migrated.json"

type marker struct {
	Username  string    `json:"username"`
	Hostname  string    `json:"hostname"`
	TokenName string    `json:"token_name"`
	At        time.Time `json:"at"`
}

// Done 报告本机该账号是否已完成凭证迁移（标记存在且用户名一致）。
func Done(home, username string) bool {
	data, err := os.ReadFile(filepath.Join(home, markerFile))
	if err != nil {
		return false
	}
	var m marker
	return json.Unmarshal(data, &m) == nil && m.Username == username
}

// Ensure 为本机申请/刷新 ok-sync-r-<hostname> 凭证并覆盖到所有已绑定项目的
// remote 凭据，成功后写迁移标记。幂等（服务端重发 = 删同名再建，无副作用堆积）。
// file:// remote 无认证语义，跳过凭据覆盖；无 credential helper 时回退 URL 内嵌
// （与绑定路径同语义）。
func Ensure(cfg config.Server) (string, error) {
	if cfg.URL == "" {
		return "", errors.New("未配置服务器")
	}
	host, _ := os.Hostname()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c := serverx.New(cfg.URL, cfg.Token)
	tok, name, err := c.GitToken(ctx, host)
	if err != nil {
		return "", err
	}
	home := registry.Home()
	reg, err := registry.Load(registry.DefaultPath())
	if err != nil {
		return "", fmt.Errorf("加载注册表: %w", err)
	}
	for _, p := range reg.Projects {
		st := store.New(filepath.Join(home, "projects", p.Name))
		repo := syncx.Open(st.Root)
		if !repo.IsRepo() {
			continue
		}
		remote := repo.RemoteURL()
		if remote == "" || strings.HasPrefix(remote, "file://") {
			continue
		}
		if syncx.HasCredentialHelper(st.Root) {
			if err := syncx.StoreCredential(st.Root, remote, cfg.Username, tok); err != nil {
				return name, fmt.Errorf("%s 写入凭据: %w", p.Name, err)
			}
			continue
		}
		if err := repo.SetRemote(syncx.CredentialURLWithAuth(syncx.StripURLAuth(remote), cfg.Username, tok)); err != nil {
			return name, fmt.Errorf("%s 更新 remote: %w", p.Name, err)
		}
	}
	m := marker{Username: cfg.Username, Hostname: host, TokenName: name, At: time.Now()}
	data, _ := json.Marshal(m)
	// 标记写失败 fail-open：返回错误由调用方记日志，下轮重试（重发幂等）。
	if err := os.WriteFile(filepath.Join(home, markerFile), data, 0o600); err != nil {
		return name, fmt.Errorf("写迁移标记: %w", err)
	}
	return name, nil
}
```

- [ ] **Step 4: 跑 credmig 测试确认变绿**

Run: `go test ./internal/credmig/ -v`
Expected: PASS

- [ ] **Step 5: daemon 钩子测试（红）**

`internal/daemon/sync_test.go` 新增（imports 加 `encoding/json`、`net/http`、`net/http/httptest`、`openknowledge/internal/config`、`openknowledge/internal/credmig`）：

```go
// TestRunSyncCycleMigratesCredential 凭证统一（2026-09-01）：已配置服务器且本机无
// 迁移标记时，runSyncCycle 先做机器级凭证迁移（git-token 重发一次）并写标记；
// 第二轮标记已在，不再重发。
func TestRunSyncCycleMigratesCredential(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OK_HOME", home)
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))
	t.Setenv("GIT_CONFIG_SYSTEM", filepath.Join(t.TempDir(), "gitconfig-sys"))
	calls := 0
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/git-token" {
			w.WriteHeader(404)
			return
		}
		calls++
		_ = json.NewEncoder(w).Encode(map[string]string{"git_token": "tok-new", "token_name": "ok-sync-r-H"})
	}))
	defer fake.Close()
	if err := config.SetServer(filepath.Join(home, "config.toml"), config.Server{URL: fake.URL, Username: "alice", Token: "tok"}); err != nil {
		t.Fatal(err)
	}
	reg := &registry.Registry{}
	if err := reg.Save(registry.DefaultPath()); err != nil {
		t.Fatal(err)
	}

	runSyncCycle(io.Discard, true)
	if calls != 1 {
		t.Fatalf("reissue calls = %d, want 1", calls)
	}
	if !credmig.Done(home, "alice") {
		t.Fatal("marker must be written")
	}
	runSyncCycle(io.Discard, true)
	if calls != 1 {
		t.Fatalf("second cycle must not reissue, calls = %d", calls)
	}
}
```

Run: `go test ./internal/daemon/ -run TestRunSyncCycleMigratesCredential -v`
Expected: FAIL（`credmig` 未被调用，calls=0）

- [ ] **Step 6: 实现 daemon 钩子**

`internal/daemon/sync.go` 在 `globalCfg := filepath.Join(registry.Home(), "config.toml")`（47 行）之后插入：

```go
	// 凭证统一迁移（2026-09-01）：已配置服务器且本机未迁移时，先把本机凭证换成
	// ok-sync-r-<hostname> 再同步。失败仅记日志（旧凭证仍可用），下轮重试。
	if gcfg, gerr := config.Load(globalCfg); gerr == nil && gcfg.Server.URL != "" &&
		!credmig.Done(registry.Home(), gcfg.Server.Username) {
		if name, merr := credmig.Ensure(gcfg.Server); merr != nil {
			fmt.Fprintf(out, "sync: 凭证迁移失败（下轮重试）: %v\n", merr)
		} else {
			fmt.Fprintf(out, "sync: 凭证已迁移为 %s\n", name)
		}
	}
```

imports 加 `"openknowledge/internal/credmig"`。

- [ ] **Step 7: 跑测试确认变绿**

Run: `go test ./internal/credmig/ ./internal/daemon/ -v 2>&1 | tail -20`
Expected: PASS（含既有 daemon 用例不回归——它们未配置服务器，钩子跳过）

- [ ] **Step 8: Commit**

```bash
git add internal/credmig/ internal/daemon/sync.go internal/daemon/sync_test.go
git commit -m "credmig+daemon: 机器级凭证迁移（同步周期自动换新 ok-sync-r-<hostname>）"
```

---

### Task 5: gui 绑定管线重构 + 清理保本机端点

**Files:**
- Modify: `internal/gui/api_server.go`（serveBindRepo 268-302 行区；路由注册 46-47 行区；新增 apiServerCredEnsure）
- Test: `internal/gui/api_server_test.go`（混合 CRLF，Edit 注意 `\r`）

**Interfaces:**
- Consumes: `credmig.Ensure(cfg config.Server) (string, error)`（Task 4）；`GitToken` 新签名（Task 3）；Task 1 的"provision 恒空 token"语义。
- Produces: `POST /api/server/credential/ensure` → 200 `{"token_name": "ok-sync-r-<host>"}` / 409 `{"error":"not_configured"}`。Task 6 前端消费。

- [ ] **Step 1: 写 ensure 端点测试（红）**

`internal/gui/api_server_test.go` 新增：

```go
// TestApiServerCredEnsure 清理旧凭证前的保本机端点：调 git-token 重发并回显
// token_name；未配置服务器 409。
func TestApiServerCredEnsure(t *testing.T) {
	h, _, _ := newEnv(t)
	fake := fakeOKServer(t, "http://gitea/alice/ok-x.git", "", func(mux *http.ServeMux) {
		mux.HandleFunc("POST /api/v1/git-token", func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]string{"git_token": "tok-new", "token_name": "ok-sync-r-H"})
		})
	})
	defer fake.Close()
	srv := httptest.NewServer(h)
	defer srv.Close()
	res, body := do(t, "POST", srv.URL+"/api/server/login", testToken, map[string]any{"url": fake.URL, "username": "alice", "password": "pw"})
	if res != 200 {
		t.Fatalf("login: %d %s", res, body)
	}
	res, body = do(t, "POST", srv.URL+"/api/server/credential/ensure", testToken, nil)
	if res != 200 || !strings.Contains(string(body), "ok-sync-r-H") {
		t.Fatalf("ensure: %d %s", res, body)
	}
}
```

Run: `go test ./internal/gui/ -run TestApiServerCredEnsure -v`
Expected: FAIL（404，路由不存在）

- [ ] **Step 2: 实现 ensure 端点 + 路由**

路由注册（`api_server.go:46` `api("GET /api/server/tokens", ...)` 之后）：

```go
	api("POST /api/server/credential/ensure", h.apiServerCredEnsure)
```

handler（放在 fwdTokenDelete 之后）：

```go
// apiServerCredEnsure 清理旧凭证前的"保本机"步骤（凭证统一 2026-09-01）：
// 确保本机持有 ok-sync-r-<hostname> 新凭证（重发幂等），覆盖写入所有已绑定项目。
func (h *Handler) apiServerCredEnsure(w http.ResponseWriter, r *http.Request) {
	_, cfgServer, err := h.serverClient()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if cfgServer.URL == "" {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "not_configured"})
		return
	}
	name, err := credmig.Ensure(cfgServer)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"token_name": name})
}
```

imports 加 `"openknowledge/internal/credmig"`。

Run: `go test ./internal/gui/ -run TestApiServerCredEnsure -v`
Expected: PASS

- [ ] **Step 3: serveBindRepo 重构**

`api_server.go` serveBindRepo 中凭据段（原 268-302 行）整体替换为：

```go
	// 凭据：建仓不再下发 token（凭证统一 2026-09-01，provision 恒返空串）。
	// 本机已存凭据直接用；没有则按 hostname 自助重发（v2.25 服务端起）。
	// file:// remote 无认证语义，跳过凭据环节。无 helper 回退 URL 内嵌（credNote 警告）。
	remote := pr.Repo.CloneURL
	credNote := ""
	gitToken := ""
	if !strings.HasPrefix(remote, "file://") && !syncx.HasStoredCredential(st.Root, remote, cfgServer.Username) {
		host, _ := os.Hostname()
		tok, _, terr := c.GitToken(r.Context(), host)
		if terr != nil {
			var se *serverx.Error
			if errors.As(terr, &se) && se.Code == http.StatusNotFound {
				writeJSON(w, http.StatusOK, map[string]any{"status": "error", "clone_url": pr.Repo.CloneURL,
					"message": "仓已存在但本机无 git 凭据，且服务端版本过旧或客户端版本过旧（不支持自助重发 token）：请升级 okserver 与客户端后重试"})
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
	}
```

（删除原末尾 `else if !syncx.HasCredentialHelper(st.Root)` 死分支：新流程中 `gitToken==""` 必然意味着 file:// 或已存凭据，该分支不可达。）

注意：404 文案保留连续子串"服务端版本过旧"（TestApiServerReposOldServerNoGitToken 断言它），扩展为"服务端版本过旧或客户端版本过旧"。

- [ ] **Step 4: 跑 gui 全部测试确认变绿**

Run: `go test ./internal/gui/ -v 2>&1 | tail -30`
Expected: PASS——既有用例全部保持绿：
- TestApiServerReposFullFlow（file://，跳过凭据环节，断言不变）；
- TestApiServerPullNewMachine（file:// clone，fake 的 git-token 路由变为不再被调，断言不变）；
- TestApiServerReposNoTokenNoHelper（http remote + fake 无 git-token 路由 → 404 分支，文案仍含"仓已存在但本机无 git 凭据"）；
- TestApiServerReposOldServerNoGitToken（文案仍含"服务端版本过旧"）。

诚实说明：serveBindRepo 重构本身没有"对旧代码变红"的新测试——行为变化的源头在 Task 1（服务端停发），那里已走红-绿；本步是死代码清理 + 防御分支重排，由既有用例守住不回归。

- [ ] **Step 5: Commit**

```bash
git add internal/gui/api_server.go internal/gui/api_server_test.go
git commit -m "gui: 绑定管线砍掉 provision token 分支 + 凭证保本机 ensure 端点"
```

---

### Task 6: web/app.js 凭证卡清理按钮 + 管理面展示 + i18n

**Files:**
- Modify: `web/app.js`（tokensCard 6308-6328 行区；srvCreateUser 5784-5796 行区；zh 字典 238-243 行区；en 字典 451-456 行区）

**Interfaces:**
- Consumes: `POST /api/server/credential/ensure` → `{"token_name"}`（Task 5）；`GET/DELETE /api/server/tokens`（既有）；建用户响应无 `git_token`（Task 2）。
- Produces: 无（终端任务）。

- [ ] **Step 1: 管理面建用户展示去掉 git token**

srvCreateUser（5791-5793 行区）改为：

```js
    srvShowSecret(t("srvUserCreated")+" — "+name, t("srvCopyHint")+" "+t("srvUserCredNote"), [
      [t("srvPwd"), r.password],
    ]);
```

- [ ] **Step 2: tokensCard 加清理按钮**

tokensCard（6308-6328 行区）改为：

```js
/* 成员视图「我的凭证」：GET /api/server/tokens → 列/删本账号 git 凭证（tokenTable 与管理卡同形状）。
   凭证统一（2026-09-01）：旧命名（非 ok-sync-r-*）凭证可一键清理——先保本机再逐条删。 */
function tokensCard(){
  const card = el("div","pcard");
  const h = el("h3"); h.textContent = t("srvMyTokens"); card.appendChild(h);
  const desc = el("div","pdesc"); desc.textContent = t("srvMyTokensDesc"); card.appendChild(desc);
  const ts = SRV.tokens || [];
  const legacy = ts.filter(tk=>!/^ok-sync-r-/.test(tk.name));
  if(legacy.length){
    const bar = el("div",""); bar.style.margin = "6px 0";
    const clean = el("button","btn"); clean.textContent = t("srvCredClean")+" ("+legacy.length+")";
    clean.onclick = async ()=>{
      clean.disabled = true;
      try{
        // 保本机：确保本机新凭证存在（没有则服务端重发+本机落 helper），再清理
        await api("/api/server/credential/ensure", { method:"POST", skip401Reload:true });
        const r = await api("/api/server/tokens", { skip401Reload:true });
        const olds = (r.tokens||[]).filter(tk=>!/^ok-sync-r-/.test(tk.name));
        if(!olds.length){
          toast(t("srvCredCleanNone"));
          SRV.tokens = r.tokens||[];
          if(state.menu==="server") render();
          return;
        }
        const names = olds.map(tk=>tk.name).join("、");
        if(!await uiConfirm(t("srvCredCleanConfirm").replace("{n}", names), true)){ clean.disabled = false; return; }
        let ok = 0; const fail = [];
        for(const tk of olds){
          try{
            await api("/api/server/tokens/"+encodeURIComponent(tk.name), { method:"DELETE", skip401Reload:true });
            ok++;
          }catch(e){ fail.push(tk.name); }
        }
        toast(t("srvCredCleanDone").replace("{n}", ok)+(fail.length?"；"+t("srvCredCleanFail")+fail.join("、"):""), fail.length>0);
        SRV.tokens = null; loadServerRoleData();
      }catch(err){ toast(err.message, true); }
      clean.disabled = false;
      if(state.menu === "server") render();
    };
    bar.appendChild(clean); card.appendChild(bar);
  }
  if(!ts.length){
    const d = el("div","small muted"); d.textContent = t("srvNoToken"); card.appendChild(d);
    return card;
  }
  card.appendChild(tokenTable(ts, async tk=>{
    if(!await uiConfirm(t("srvTokenDelConfirm").replace("{n}", tk.name), true)) return;
    try{
      await api("/api/server/tokens/"+encodeURIComponent(tk.name), { method:"DELETE", skip401Reload:true });
      toast(t("srvTokenDeleted"));
      SRV.tokens = null; loadServerRoleData();
    }catch(err){ toast(err.message, true); }
    if(state.menu === "server") render();
  }));
  return card;
}
```

- [ ] **Step 3: i18n 字典**

zh（238-243 行区）：srvMyTokensDesc 改为：

```
    srvMyTokensDesc:"本账号在服务器上的 git 访问凭证：每台拉取过的机器一条（ok-sync-r-<主机名>）。不再使用的机器可删除其凭证，删除后该机器下次推送会失败，重新绑定即自愈。时间列为最近使用（未用过则显示创建时间）。",
```

241 行附近新增：

```
    srvCredClean:"清理旧版凭证", srvCredCleanConfirm:"将删除以下旧版凭证：{n}。仍使用这些凭证的其他机器下次推送会失败（升级新版客户端后重新绑定即自愈）。确定删除？",
    srvCredCleanDone:"已删除 {n} 条旧凭证", srvCredCleanFail:"失败：", srvCredCleanNone:"没有需要清理的旧凭证。",
    srvUserCredNote:"git 凭证不再随账号下发：用户首次绑定项目时，其机器会自动获取。",
```

243 行 srvGitTok 从 zh 字典删除（`srvPwd:"初始密码", srvNewPwd:"新密码", srvCopied:"我已复制", ...` 保持其余 key 不变）。

en（451-456 行区）对应改：

```
    srvMyTokensDesc:"Git access credentials of this account on the server: one per machine that has pulled (ok-sync-r-<hostname>). Delete credentials of machines no longer in use; affected machines fail their next push and self-heal by rebinding. The time column shows last use (creation time if never used).",
```

```
    srvCredClean:"Clean up legacy credentials", srvCredCleanConfirm:"These legacy credentials will be deleted: {n}. Other machines still using them will fail their next push (self-heal by rebinding after upgrading the client). Delete?",
    srvCredCleanDone:"Deleted {n} legacy credential(s)", srvCredCleanFail:"Failed: ", srvCredCleanNone:"No legacy credentials to clean.",
    srvUserCredNote:"Git credentials are no longer issued with the account: the user's machine obtains one automatically on first project binding.",
```

en 字典 srvGitTok 同步删除。

- [ ] **Step 4: 语法校验 + GUI 测试**

Run: `node --check web/app.js && go test ./internal/gui/ 2>&1 | tail -5`
Expected: 语法无输出；测试 PASS

- [ ] **Step 5: Commit**

```bash
git add web/app.js
git commit -m "web: 凭证卡一键清理旧版凭证 + 建用户展示去 git token（凭证统一）"
```

---

### Task 7: 全量验证

**Files:** 无（验证任务）。

- [ ] **Step 1: 全量构建与测试**

Run: `go build ./... && go test ./... 2>&1 | tail -40`
Expected: 全部 PASS（含 `tests/e2e`）

- [ ] **Step 2: 提醒（不做版本动作）**

- web/ 改动若要本地实机验证 GUI，必须经 `scripts/build.py` 同步 dist/web——裸 `go build` 二进制新页面旧（项目已知 pitfall）。
- 版本 bump / changelog / 发布另行走发布纪律（`scripts/sync-version.sh` 等），不在本计划。

- [ ] **Step 3: 收尾 commit（如有验证期修补）**

```bash
git add -A && git commit -m "test: 凭证统一全量验证修补"
```
