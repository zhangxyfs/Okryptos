# P1-C2 本地接入与 GUI 服务器页 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 打通本地 ↔ okserver：`internal/serverx` 客户端、okd 转发端点（连接/登录/me/建仓一条龙/管理类透传）、GUI 服务器页（stepper 三步向导 + 管理/成员双视图）、管理页 not_repo 接通服务器建仓。P1 至此整体闭环。

**Architecture:** serverx 是 Bearer token 的薄 HTTP 客户端（照 llmx 形态）；okd 端点收在 `internal/gui/api_server.go`（withAuth + 转发 serverx + 建仓一条龙里联动 syncx）；GUI 服务器页照定稿原型变体 B（stepper）移植到 web/app.js 的 DOM 构建风格。

**Tech Stack:** Go 1.25 标准库 + 既有 internal 包（无新第三方依赖）；前端 vanilla JS（web/app.js + web/style.css）。

## Global Constraints

- 规范源：`docs/superpowers/specs/2026-08-25-personal-sync-p1-design.md` §10（GUI 服务器页）与 §12（全局 [server] 配置）；交互形态以 `docs/prototypes/prototype-sync-p1-final.html` 的 server 变体 B 为准（stepper 三步骤、角色双视图、卡片清单）。
- **无新第三方依赖**（go.mod 不得新增 require）。
- **okserver API 契约已锁定**（C1 已入库，internal/oksrv/http.go）：serverx 按该契约消费，不得要求服务端改形状——发现契约不符先在报告提出，不擅自改 oksrv。
- **[server] 段只存全局 config.toml**（照 LLM 段先例）；token 文件权限 0600 语义由 fsx 既有写路径承担（0644 写入——配置目录在 OK_HOME 内，沿用现状口径）；token 不入日志、读取时 GUI 脱敏返回。
- **凭据分发**：建仓下发的 git token 写系统 git credential helper（`git credential approve`）；无 helper 时回退为 remote URL 内嵌凭据（v1 取舍，响应里注明）。
- **SSRF 裁决**：服务器地址来自用户自己的配置/输入，本地 GUI + X-Ok-Token 鉴权，目标是用户要连的 LAN 服务器——**不做回环限制**（与 ollama-models 端点的回环限制不同，那里是探测任意地址）。明文写进代码注释。
- GUI 按钮纪律：动作类即点即执行 + 可见反馈；配置类（连接信息）两段式（填写+保存/连接按钮）；杜绝假功能按钮（一次性显示的密码/token 弹窗必须带复制按钮与明确提示）。
- 前端零依赖零构建链；改 web/ 不重编 Go。
- 测试纪律：handler 测试走真 HTTP（httptest 假 okserver + 真 gui handler，照 TestApiSyncAIMergeOK 先例）；建仓一条龙测试用 file:// 裸仓当"Gitea 侧"（真 git 闭环）。
- commit 惯例 `feat(config): ...` / `feat(serverx): ...` / `feat(gui): ...` / `feat(web): ...`；最后任务统一补变更日志。
- 本计划不做：okserver 本体改动（C1 已交付）、Docker/CI、merge driver（P2）。

---

### Task 1: config [server] 全局段 + SetServer

**Files:**
- Modify: `internal/config/config.go`（Config 加字段 + Server 类型 + 文件尾追加 SetServer）
- Test: `internal/config/config_test.go`（追加）

**Interfaces:**
- Consumes: 既有 `Load/LoadMerged/Default`、`fsx.WithFileLock`、`SetSync` 行级小节先例（config.go:465）。
- Produces:
  - `type Server struct { URL string; Username string; Token string }`（toml 标签 url/username/token）
  - `Config.Server Server ` + "`toml:\"server\"`"
  - `func SetServer(path string, s Server) error` — 行级小节读-改-写；**token 为空串时保留旧 token 行**（脱敏回写防丢）；URL/Username 空串则整行省略

- [ ] **Step 1: Write the failing test**

```go
package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestServerSection(t *testing.T) {
	// 零值即未配置
	if Default().Server.URL != "" {
		t.Fatal("default server should be empty")
	}
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("# 注释\n[retrieve]\ntop_n = 5\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// 写入
	if err := SetServer(path, Server{URL: "http://nas:3100", Username: "alice", Token: "tok-1"}); err != nil {
		t.Fatalf("SetServer: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.URL != "http://nas:3100" || cfg.Server.Username != "alice" || cfg.Server.Token != "tok-1" {
		t.Fatalf("loaded: %+v", cfg.Server)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "# 注释") || !strings.Contains(string(data), "[retrieve]") {
		t.Fatalf("other content lost:\n%s", data)
	}
	if strings.Count(string(data), "[server]") != 1 {
		t.Fatalf("dup section:\n%s", data)
	}
	// 脱敏回写：token 空串 → 保留旧 token
	if err := SetServer(path, Server{URL: "http://nas2:3100", Username: "alice", Token: ""}); err != nil {
		t.Fatal(err)
	}
	cfg, _ = Load(path)
	if cfg.Server.Token != "tok-1" || cfg.Server.URL != "http://nas2:3100" {
		t.Fatalf("token should be preserved: %+v", cfg.Server)
	}
	// LoadMerged：全局段不被项目文件顶掉（项目文件无 [server] 段时继承全局）
	global := filepath.Join(t.TempDir(), "global.toml")
	project := filepath.Join(t.TempDir(), "project.toml")
	if err := SetServer(global, Server{URL: "http://nas:3100", Username: "alice", Token: "t"}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(project, []byte("[retrieve]\ntop_n = 9\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	merged, err := LoadMerged(project, global)
	if err != nil {
		t.Fatal(err)
	}
	if merged.Server.URL != "http://nas:3100" || merged.Retrieve.TopN != 9 {
		t.Fatalf("merged: %+v", merged.Server)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run TestServerSection -v`
Expected: 编译失败（`Server`/`SetServer` 未定义）。

- [ ] **Step 3: Write minimal implementation**

`internal/config/config.go`：

a) Config 结构体加字段（紧跟 `Sync` 字段后）：

```go
	// Server [server] 段，仅存全局 config.toml（okserver 管理面连接，设计文档 §12）。
	Server Server `toml:"server"`
```

b) 类型定义（放 Sync 类型旁）：

```go
// Server 是全局 [server] 段：okserver 管理面连接配置。
type Server struct {
	URL      string `toml:"url"`
	Username string `toml:"username"`
	Token    string `toml:"token"`
}
```

c) 文件尾追加 SetServer（照 SetSync 的行级小节形态；token 空串保留旧行——GUI 脱敏回写防丢）：

```go
// SetServer 行级更新 path 的 [server] 段（url/username/token）。
// token 传空串 = 不改动旧 token（GUI 脱敏回写语义）；url/username 空串则省略该行。
func SetServer(path string, s Server) error {
	return fsx.WithFileLock(path, func() error {
		data, _ := os.ReadFile(path)
		lines := strings.Split(string(data), "\n")
		// 先摘旧 token（token 空串时保留）
		oldToken := ""
		inOld := false
		for _, line := range lines {
			trim := strings.TrimSpace(line)
			if strings.HasPrefix(trim, "[") {
				inOld = trim == "[server]"
				continue
			}
			if inOld && strings.HasPrefix(trim, "token") {
				if _, v, ok := strings.Cut(trim, "="); ok {
					oldToken = strings.Trim(strings.TrimSpace(v), "\"")
				}
			}
		}
		if s.Token == "" {
			s.Token = oldToken
		}
		var section []string
		section = append(section, "[server]")
		if s.URL != "" {
			section = append(section, fmt.Sprintf("url = %q", s.URL))
		}
		if s.Username != "" {
			section = append(section, fmt.Sprintf("username = %q", s.Username))
		}
		if s.Token != "" {
			section = append(section, fmt.Sprintf("token = %q", s.Token))
		}
		// 行级替换（与 SetSync 同形态）
		var out []string
		inSync := false
		wrote := false
		for _, line := range lines {
			trim := strings.TrimSpace(line)
			if strings.HasPrefix(trim, "[") && strings.HasSuffix(trim, "]") {
				if inSync && !wrote {
					out = append(out, section...)
					wrote = true
				}
				inSync = trim == "[server]"
				if inSync {
					continue
				}
			} else if inSync {
				continue
			}
			out = append(out, line)
		}
		if !wrote {
			if inSync {
				out = append(out, section...)
			} else {
				for len(out) > 0 && strings.TrimSpace(out[len(out)-1]) == "" {
					out = out[:len(out)-1]
				}
				out = append(out, "", section...)
			}
		}
		return fsx.WriteFile(path, []byte(strings.Join(out, "\n")), 0o644)
	})
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/config/ -v -count=1`
Expected: 全部 PASS（含既有用例不回归）。

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): 全局 [server] 段与 SetServer（token 空串保留旧值）"
```

---

### Task 2: internal/serverx 客户端

**Files:**
- Create: `internal/serverx/serverx.go`
- Test: `internal/serverx/serverx_test.go`

**Interfaces:**
- Consumes: oksrv API 契约（`internal/oksrv/http.go` 已入库——**先读它对齐每个端点的响应形状**）；llmx 形态先例（`internal/llmx/llmx.go:20-38, 98-128`）。
- Produces（Task 4 依赖，不得改名）：

```go
type Client struct { /* base, token, hc */ }
func New(baseURL, token string) *Client

type Meta struct { Version string; Initialized bool; GitBackend struct{ Type string; Ok bool } } // json: version/initialized/git_backend{type,ok}
func (c *Client) Meta(ctx context.Context) (*Meta, error)

// Login 成功返回 token 与角色。
func (c *Client) Login(ctx context.Context, username, password string) (token string, role string, err error)

type MeInfo struct { Name string; Role string; Orgs []string; Repos []RepoInfo }
type RepoInfo struct { Layer, Owner, Project, CloneURL, CreatedBy, CreatedAt string }
func (c *Client) Me(ctx context.Context) (*MeInfo, error)

type ProvisionResult struct { Repo RepoInfo; GitToken string } // git_token 仅首建下发
func (c *Client) ProvisionPersonalRepo(ctx context.Context, project string) (*ProvisionResult, error)

// 管理类（root/admin）
type ServerUser struct { Name, Role string; Disabled bool; CreatedAt string }
type CreatedUser struct { Username, Password, GitToken string }
type ServerOrg struct { Name, Description string; Members []OrgMember }
type OrgMember struct { Username, Role string }
type ServerRepo struct { Layer, Owner, Project, CloneURL, CreatedBy, CreatedAt string }
type AuditEntry = oksrv.AuditEntry // 不依赖 oksrv——本地重定义同形状结构体
func (c *Client) ListUsers(ctx context.Context) ([]ServerUser, error)
func (c *Client) CreateUser(ctx context.Context, username, role string) (*CreatedUser, error)
func (c *Client) ResetPassword(ctx context.Context, username string) (string, error)
func (c *Client) SetUserDisabled(ctx context.Context, username string, disabled bool) error
func (c *Client) ListOrgs(ctx context.Context) ([]ServerOrg, error)
func (c *Client) CreateOrg(ctx context.Context, name, description string) error
func (c *Client) AddOrgMember(ctx context.Context, org, username, role string) error
func (c *Client) RemoveOrgMember(ctx context.Context, org, username string) error
func (c *Client) CreateTeamRepo(ctx context.Context, org, project string) (*RepoInfo, error)
func (c *Client) ListRepos(ctx context.Context) ([]ServerRepo, error)
func (c *Client) ListAudit(ctx context.Context, limit, offset int) ([]ServerAudit, error)

// Error 是 okserver 的错误响应（{"error": msg}）。
type Error struct { Code int; Msg string } // Error() string
```

（`ServerAudit` 本地定义 `{ID int64; Actor, Action, Target, Detail, CreatedAt string}`——serverx 不 import oksrv，两边结构同形状。上面"AuditEntry = oksrv.AuditEntry"的注释是反例，以本句为准。）

- [ ] **Step 1: Write the failing test（httptest 假 okserver）**

```go
package serverx

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// fakeOKServer 镜像 oksrv 契约（简版：login/me/repos/personal/users + 鉴权头校验）。
func fakeOKServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/meta", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"version": "v1", "initialized": true, "git_backend": map[string]any{"type": "fake", "ok": true}})
	})
	mux.HandleFunc("POST /api/v1/login", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Username, Password string }
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Username == "alice" && req.Password == "pw" {
			json.NewEncoder(w).Encode(map[string]any{"token": "tok-1", "user": map[string]string{"name": "alice", "role": "member"}})
			return
		}
		w.WriteHeader(401)
		json.NewEncoder(w).Encode(map[string]string{"error": "未认证"})
	})
	authed := func(w http.ResponseWriter, r *http.Request) bool {
		if r.Header.Get("Authorization") != "Bearer tok-1" {
			w.WriteHeader(401)
			json.NewEncoder(w).Encode(map[string]string{"error": "未认证"})
			return false
		}
		return true
	}
	mux.HandleFunc("GET /api/v1/me", func(w http.ResponseWriter, r *http.Request) {
		if !authed(w, r) { return }
		json.NewEncoder(w).Encode(map[string]any{"name": "alice", "role": "member", "orgs": []string{"acme"}, "repos": []any{}})
	})
	mux.HandleFunc("POST /api/v1/repos/personal", func(w http.ResponseWriter, r *http.Request) {
		if !authed(w, r) { return }
		json.NewEncoder(w).Encode(map[string]any{
			"repo":      map[string]string{"owner": "alice", "name": "ok-demo", "clone_url": "http://gitea/alice/ok-demo.git"},
			"git_token": "git-tok-1",
		})
	})
	mux.HandleFunc("GET /api/v1/users", func(w http.ResponseWriter, r *http.Request) {
		if !authed(w, r) { return }
		json.NewEncoder(w).Encode(map[string]any{"users": []map[string]any{{"name": "root", "role": "root", "disabled": false, "created_at": "2026-08-29T00:00:00Z"}}})
	})
	return httptest.NewServer(mux)
}

func TestClientFlow(t *testing.T) {
	srv := fakeOKServer(t)
	defer srv.Close()
	ctx := context.Background()
	c := New(srv.URL, "")

	meta, err := c.Meta(ctx)
	if err != nil || !meta.Initialized || !meta.GitBackend.Ok || meta.GitBackend.Type != "fake" {
		t.Fatalf("meta: %+v %v", meta, err)
	}
	// 登录失败 → *Error 401
	_, _, err = c.Login(ctx, "alice", "bad")
	var se *Error
	if !errors.As(err, &se) || se.Code != 401 {
		t.Fatalf("bad login: %v", err)
	}
	tok, role, err := c.Login(ctx, "alice", "pw")
	if err != nil || tok != "tok-1" || role != "member" {
		t.Fatalf("login: %v %q %q", err, tok, role)
	}
	c2 := New(srv.URL, tok)
	me, err := c2.Me(ctx)
	if err != nil || me.Name != "alice" || len(me.Orgs) != 1 || me.Orgs[0] != "acme" {
		t.Fatalf("me: %+v %v", me, err)
	}
	pr, err := c2.ProvisionPersonalRepo(ctx, "demo")
	if err != nil || pr.Repo.Name != "ok-demo" || pr.GitToken != "git-tok-1" {
		t.Fatalf("provision: %+v %v", pr, err)
	}
	users, err := c2.ListUsers(ctx)
	if err != nil || len(users) != 1 || users[0].Name != "root" {
		t.Fatalf("users: %+v %v", users, err)
	}
	// 无 token → 401
	if _, err := New(srv.URL, "").Me(ctx); err == nil {
		t.Fatal("no token must fail")
	}
	// 服务器不可达 → 错误
	if _, err := New("http://127.0.0.1:1", "").Meta(ctx); err == nil {
		t.Fatal("unreachable must fail")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/serverx/ -v`
Expected: 编译失败（包不存在）。

- [ ] **Step 3: Write minimal implementation**

照 llmx 形态（`llmx.go:20-38` 结构 + `:98-128` doJSON）：

```go
// Package serverx 是本地（ok/okd/GUI）与 okserver 管理 API 打交道的客户端。
// 薄 Bearer HTTP 客户端（照 llmx 形态）；不依赖其他 internal 包。
package serverx

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	base  string
	token string
	hc    *http.Client
}

// New 构造客户端；baseURL 去尾斜杠；默认 15s 超时。
func New(baseURL, token string) *Client {
	return &Client{base: strings.TrimRight(baseURL, "/"), token: token, hc: &http.Client{Timeout: 15 * time.Second}}
}

// Error 是 okserver 的错误响应。
type Error struct {
	Code int
	Msg  string
}

func (e *Error) Error() string { return fmt.Sprintf("okserver %d: %s", e.Code, e.Msg) }

// call 发请求：Bearer 头 + JSON 编解码 + 错误包装。
func (c *Client) call(ctx context.Context, method, path string, body any, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+"/api/v1"+path, rd)
	if err != nil {
		return err
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("连接服务器失败: %w", err)
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode >= 400 {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(data, &e)
		return &Error{Code: res.StatusCode, Msg: e.Error}
	}
	if out != nil {
		return json.Unmarshal(data, out)
	}
	return nil
}
```

各方法按 Produces 签名逐一实现（路径对照 oksrv 契约：meta/login/me/repos/personal/users/users/{name}/reset-password/users/{name}/disable|enable/orgs/orgs/{org}/members/repos/team/repos/audit?limit=&offset=；响应解包外层包装 users/orgs/repos/entries/repo/user/token 键）。

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/serverx/ -v -count=1`
Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add internal/serverx/serverx.go internal/serverx/serverx_test.go
git commit -m "feat(serverx): okserver 客户端（Bearer + 全端点）"
```

---

### Task 3: syncx git 凭据写入（credential helper）

**Files:**
- Modify: `internal/syncx/repo.go`（追加 StoreCredential；或新建 `internal/syncx/credential.go`——选后者，职责单一）
- Test: `internal/syncx/credential_test.go`

**Interfaces:**
- Consumes: `execGit`、`localTimeout`。
- Produces:
  - `func StoreCredential(remoteURL, username, password string) error` — `git credential approve`（stdin 协议）；无可用 helper 时返回 `ErrNoCredentialHelper`
  - `var ErrNoCredentialHelper = errors.New("系统未配置 git credential helper")`
  - `func CredentialURLWithAuth(remoteURL, username, token string) string` — 回退：URL 内嵌凭据（`http://user:token@host/path`；token 需 url.PathEscape）

- [ ] **Step 1: Write the failing test**

```go
package syncx

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStoreCredential(t *testing.T) {
	// 用 store helper + 临时文件验证 approve 真的落了凭据
	storeFile := filepath.Join(t.TempDir(), "creds")
	dir := t.TempDir()
	r := Open(dir)
	if err := r.Init(); err != nil {
		t.Fatalf("init: %v", err)
	}
	// 仓级配 store helper 指向临时文件
	if _, err := execGit(dir, localTimeout, "config", "credential.helper", "store --file "+filepath.ToSlash(storeFile)); err != nil {
		t.Fatalf("config helper: %v", err)
	}
	// StoreCredential 在 r.Dir 上下文跑 approve
	if err := storeCredentialIn(dir, "http://nas:3000/alice/ok-demo.git", "alice", "tok-1"); err != nil {
		t.Fatalf("store: %v", err)
	}
	data, err := os.ReadFile(storeFile)
	if err != nil {
		t.Fatalf("store file: %v", err)
	}
	if !strings.Contains(string(data), "alice:tok-1@nas") {
		t.Fatalf("cred content: %q", data)
	}
}

func TestStoreCredentialNoHelper(t *testing.T) {
	// 无 helper：-c credential.helper= 清空 → approve 静默成功但啥都没存（git 语义）
	// 我们的实现要先探测 helper 是否存在，不存在返回 ErrNoCredentialHelper
	dir := t.TempDir()
	r := Open(dir)
	if err := r.Init(); err != nil {
		t.Fatalf("init: %v", err)
	}
	err := storeCredentialIn(dir, "http://nas/x.git", "u", "p") // 该测试仓未配 helper；但全局可能配了 manager——
	// 用 GIT_CONFIG_GLOBAL 空文件隔离全局配置
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))
	err = storeCredentialIn(dir, "http://nas/x.git", "u", "p")
	if !errors.Is(err, ErrNoCredentialHelper) {
		t.Fatalf("want ErrNoCredentialHelper, got %v", err)
	}
}

func TestCredentialURLWithAuth(t *testing.T) {
	got := CredentialURLWithAuth("http://nas:3000/alice/ok-demo.git", "alice", "tok/with special")
	if !strings.Contains(got, "alice:tok") || !strings.Contains(got, "@nas:3000") {
		t.Fatalf("got %q", got)
	}
	if strings.Contains(got, " ") {
		t.Fatalf("space must be escaped: %q", got)
	}
}
```

（注意：`storeCredentialIn(dir, ...)` 是内部函数——包级 `StoreCredential` 也提供（在调用方目录跑）见实现；测试里 GIT_CONFIG_GLOBAL 隔离必须在 Init 之前生效，Init 挪到 Setenv 之后。）

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/syncx/ -run 'TestStoreCredential|TestCredentialURL' -v`
Expected: 编译失败。

- [ ] **Step 3: Write minimal implementation**

```go
// credential.go：git 凭据写入系统 credential helper（设计文档 §9.4）。
// 无 helper 时返回 ErrNoCredentialHelper，调用方回退 URL 内嵌（v1 取舍）。
package syncx

import (
	"context"
	"errors"
	"net/url"
	"os/exec"
	"strings"
	"time"

	"openknowledge/internal/procx"
)

var ErrNoCredentialHelper = errors.New("系统未配置 git credential helper")

// StoreCredential 把 remote URL 的凭据写入系统 credential helper。
// 在仓库目录 dir 上下文执行（仓级 helper 配置可见）。
func StoreCredential(dir, remoteURL, username, password string) error {
	return storeCredentialIn(dir, remoteURL, username, password)
}

func storeCredentialIn(dir, remoteURL, username, password string) error {
	// 先探测 helper（credential.helper 为空则 approve 静默丢弃——git 语义）
	out, err := execGit(dir, localTimeout, "config", "--get", "credential.helper")
	if err != nil || strings.TrimSpace(out) == "" {
		return ErrNoCredentialHelper
	}
	ctx, cancel := context.WithTimeout(context.Background(), localTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "credential", "approve")
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "LC_ALL=C")
	procx.HideWindow(cmd)
	cmd.Stdin = strings.NewReader("url=" + remoteURL + "\nusername=" + username + "\npassword=" + password + "\n\n")
	if out, err := cmd.CombinedOutput(); err != nil {
		return &ExitError{Code: exitCode(err), Output: string(out)}
	}
	return nil
}

func exitCode(err error) int {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return -1
}

// CredentialURLWithAuth 回退：把凭据嵌进 remote URL（无 helper 环境）。
func CredentialURLWithAuth(remoteURL, username, token string) string {
	u, err := url.Parse(remoteURL)
	if err != nil || u.Host == "" {
		return remoteURL
	}
	u.User = url.UserPassword(username, token)
	return u.String()
}
```

（import 需 `"os"`。）

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/syncx/ -v -count=1`
Expected: 全部 PASS（既有用例不回归）。

- [ ] **Step 5: Commit**

```bash
git add internal/syncx/credential.go internal/syncx/credential_test.go
git commit -m "feat(syncx): git 凭据写系统 credential helper（无 helper 回退 URL 内嵌）"
```

---

### Task 4: okd 转发端点（api_server.go）

**Files:**
- Create: `internal/gui/api_server.go`
- Modify: `internal/gui/api.go`（注册区加一行）
- Test: `internal/gui/api_server_test.go`（轻用例；全链路在 Task 7）

**Interfaces:**
- Consumes: Task 1 `config.SetServer/Server` + `globalConfigPath()`（api.go:235-238）；Task 2 serverx 全方法；Task 3 `syncx.StoreCredential/CredentialURLWithAuth`；既有 `syncx.InitForSync/SetSync/SyncOnce/RecordOutcome`（建仓一条龙）；gui 基建 `decodeJSON/writeJSON/writeErr`。
- Produces（前端 Task 5/6 依赖的契约，不得改形状）：

| 端点 | 请求 | 响应 |
|---|---|---|
| `GET /api/server/config` | — | `{url, username, has_token, logged_in}`（token 不回传） |
| `PUT /api/server/config` | `{url, username?, token?}` | 204（token 空=保留旧值） |
| `POST /api/server/test` | `{url}` | `{ok, version, git_backend_ok, error}` |
| `POST /api/server/login` | `{url, username, password}` | `{token_saved: true, user: {name, role}}`；401/502 透传 |
| `POST /api/server/logout` | — | 204（清 token 字段） |
| `GET /api/server/me` | — | okserver 的 me 原样；401 透传 |
| `POST /api/server/repos` | `{project}` | 一条龙：provision → 写凭据 → InitForSync + SetSync + 首次同步 → `{status, clone_url, message}`；`{status:"server_error"/"not_repo_error", message}` |
| 管理类透传（全部 root/admin 语义在服务端） | — | 原样转发状态码与 JSON：`GET /api/server/users`、`POST /api/server/users {username, role?}`、`POST /api/server/users/{name}/reset-password`、`POST /api/server/users/{name}/disable`、`/enable`、`GET /api/server/orgs`、`POST /api/server/orgs {name, description}`、`POST /api/server/orgs/{org}/members {username, role}`、`DELETE /api/server/orgs/{org}/members/{username}`、`POST /api/server/repos/team {org, project}`、`GET /api/server/repos-all`、`GET /api/server/audit?limit=&offset=` |

- [ ] **Step 1: Write the failing test**

```go
package gui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeOKServer 起假 okserver（login + me + provision，校验 Bearer）。
func fakeOKServer(t *testing.T, cloneURL string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/meta", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"version": "v1", "initialized": true, "git_backend": map[string]any{"type": "fake", "ok": true}})
	})
	mux.HandleFunc("POST /api/v1/login", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Username, Password string }
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Password != "pw" {
			w.WriteHeader(401)
			json.NewEncoder(w).Encode(map[string]string{"error": "未认证"})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"token": "tok-" + req.Username, "user": map[string]string{"name": req.Username, "role": "member"}})
	})
	mux.HandleFunc("GET /api/v1/me", func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer tok-") {
			w.WriteHeader(401)
			json.NewEncoder(w).Encode(map[string]string{"error": "未认证"})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"name": "alice", "role": "member", "orgs": []string{}, "repos": []any{}})
	})
	mux.HandleFunc("POST /api/v1/repos/personal", func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer tok-") {
			w.WriteHeader(401)
			return
		}
		var req struct{ Project string `json:"project"` }
		_ = json.NewDecoder(r.Body).Decode(&req)
		json.NewEncoder(w).Encode(map[string]any{
			"repo":      map[string]string{"layer": "personal", "owner": "alice", "project": req.Project, "name": "ok-" + req.Project, "clone_url": cloneURL},
			"git_token": "git-tok-1",
		})
	})
	return httptest.NewServer(mux)
}

func TestApiServerConfigAndLogin(t *testing.T) {
	h, _, okHome := newEnv(t)
	fake := fakeOKServer(t, "http://gitea/alice/ok-x.git")
	defer fake.Close()
	srv := httptest.NewServer(h)
	defer srv.Close()

	// 未配置：GET config 返回空
	res, body := do(t, "GET", srv.URL+"/api/server/config", testToken, nil)
	if res != 200 || strings.Contains(body, "tok") {
		t.Fatalf("initial config: %d %s", res, body)
	}
	// test 连接
	res, body = do(t, "POST", srv.URL+"/api/server/test", testToken, map[string]any{"url": fake.URL})
	if res != 200 || !strings.Contains(body, `"ok":true`) {
		t.Fatalf("test: %d %s", res, body)
	}
	// test 不可达地址
	res, body = do(t, "POST", srv.URL+"/api/server/test", testToken, map[string]any{"url": "http://127.0.0.1:1"})
	if res != 200 || !strings.Contains(body, `"ok":false`) {
		t.Fatalf("test down: %d %s", res, body)
	}
	// 登录（成功 → 配置落盘且 token 不回显）
	res, body = do(t, "POST", srv.URL+"/api/server/login", testToken, map[string]any{"url": fake.URL, "username": "alice", "password": "pw"})
	if res != 200 || !strings.Contains(body, "alice") {
		t.Fatalf("login: %d %s", res, body)
	}
	// 全局配置已落盘且含 token
	cfgData, err := os.ReadFile(filepath.Join(okHome, "config.toml"))
	if err != nil || !strings.Contains(string(cfgData), "[server]") || !strings.Contains(string(cfgData), "tok-alice") {
		t.Fatalf("global config: %v\n%s", err, cfgData)
	}
	// GET config 不回传 token 本体
	res, body = do(t, "GET", srv.URL+"/api/server/config", testToken, nil)
	if res != 200 || strings.Contains(body, "tok-alice") || !strings.Contains(body, `"has_token":true`) {
		t.Fatalf("config after login: %d %s", res, body)
	}
	// me 转发
	res, body = do(t, "GET", srv.URL+"/api/server/me", testToken, nil)
	if res != 200 || !strings.Contains(body, "alice") {
		t.Fatalf("me: %d %s", res, body)
	}
	// 登录失败 401 透传
	res, _ = do(t, "POST", srv.URL+"/api/server/login", testToken, map[string]any{"url": fake.URL, "username": "alice", "password": "bad"})
	if res != 401 {
		t.Fatalf("bad login: %d", res)
	}
	// logout
	res, _ = do(t, "POST", srv.URL+"/api/server/logout", testToken, nil)
	if res != 204 {
		t.Fatalf("logout: %d", res)
	}
	cfgData, _ = os.ReadFile(filepath.Join(okHome, "config.toml"))
	if strings.Contains(string(cfgData), "tok-alice") {
		t.Fatalf("logout should clear token:\n%s", cfgData)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/gui/ -run TestApiServer -v`
Expected: 编译失败 / 404。

- [ ] **Step 3: Write minimal implementation**

`internal/gui/api_server.go` 骨架（完整实现按契约表）：

```go
// api_server.go：GUI 服务器页的本地端点——配置读写 + 连接测试 + 登录 + 转发 okserver。
// SSRF 裁决（设计文档）：服务器地址来自用户自己的配置/输入，本地 GUI + X-Ok-Token
// 鉴权，目标是用户要连的 LAN 服务器——不做回环限制。
package gui

import (
	"errors"
	"net/http"
	"strings"

	"openknowledge/internal/config"
	"openknowledge/internal/serverx"
	"openknowledge/internal/syncx"
)

func (h *Handler) registerServerAPI(api func(string, http.HandlerFunc)) {
	api("GET /api/server/config", h.apiServerConfigGet)
	api("PUT /api/server/config", h.apiServerConfigPut)
	api("POST /api/server/test", h.apiServerTest)
	api("POST /api/server/login", h.apiServerLogin)
	api("POST /api/server/logout", h.apiServerLogout)
	api("GET /api/server/me", h.apiServerMe)
	api("POST /api/server/repos", h.apiServerRepos)
	// 管理类透传
	api("GET /api/server/users", h.fwdUsers)
	api("POST /api/server/users", h.fwdUserCreate)
	api("POST /api/server/users/{name}/reset-password", h.fwdUserResetPassword)
	api("POST /api/server/users/{name}/disable", h.fwdUserDisable(true))
	api("POST /api/server/users/{name}/enable", h.fwdUserDisable(false))
	api("GET /api/server/orgs", h.fwdOrgs)
	api("POST /api/server/orgs", h.fwdOrgCreate)
	api("POST /api/server/orgs/{org}/members", h.fwdOrgMemberAdd)
	api("DELETE /api/server/orgs/{org}/members/{username}", h.fwdOrgMemberRemove)
	api("POST /api/server/repos/team", h.fwdTeamRepo)
	api("GET /api/server/repos-all", h.fwdReposAll)
	api("GET /api/server/audit", h.fwdAudit)
}

// serverClient 从全局配置构造 serverx 客户端；未配置返回 nil。
func (h *Handler) serverClient() (*serverx.Client, config.Server, error) {
	cfg, err := config.Load(globalConfigPath())
	if err != nil {
		return nil, config.Server{}, err
	}
	s := cfg.Server
	if s.URL == "" {
		return nil, s, nil
	}
	return serverx.New(s.URL, s.Token), s, nil
}

func writeServerErr(w http.ResponseWriter, err error) {
	var se *serverx.Error
	if errors.As(err, &se) {
		writeJSON(w, se.Code, map[string]string{"error": se.Msg})
		return
	}
	writeErr(w, http.StatusBadGateway, err.Error())
}
```

关键 handler 语义：

- `apiServerConfigGet`：`{url, username, has_token: cfg.Server.Token != "", logged_in: has_token}`；
- `apiServerConfigPut`：`SetServer(globalConfigPath(), Server{...})`（token 空串保留旧值——Task 1 语义）→ 204；
- `apiServerTest`：`{url}` → `serverx.New(url, "").Meta(ctx)` → `{ok: err==nil, version, git_backend_ok: meta.GitBackend.Ok, error}`；
- `apiServerLogin`：`{url, username, password}` → `New(url,"").Login` → 成功 `SetServer(url, username, token)` → `{token_saved:true, user:{name, role}}`；失败 `writeServerErr`；
- `apiServerLogout`：`SetServer(globalConfigPath(), Server{URL: 旧url, Username: 旧username, Token: ""})`——**注意 SetServer 的"token 空串保留旧值"语义在这是反的**：logout 需要真正清空。处理：SetServer 加一个显示清空语义——改 Task 1 语义为"token 字段用指针"？不。简法：logout 走 `SetServer` 后追加一次行级删 token 行？更干净：SetServer 加形参 `clearToken bool`？改动 Task 1 已评审代码。**定案**：`SetServer` 签名不变；logout 用 `config.SetServer(path, Server{URL:"", Username:"", Token:""})` 整段重写为空段——并给 SetServer 加一条规则：URL 为空串时整段清空（三行全省略，`[server]` 段留空头或直接移除段）。实现时照此（空段残留无害，toml 解析为空值）。
- `apiServerMe`：serverClient → nil → 409 `{"error":"not_configured"}`；否则 `Me(ctx)` 原样 `writeJSON(200, me)`；*Error 透传；
- `apiServerRepos`（一条龙）：
  1. serverClient → 未配置 409 not_configured；
  2. `resolveProject(w, req.Project)` → st；
  3. `ProvisionPersonalRepo(ctx, req.Project)` → err 透传；
  4. `syncx.StoreCredential(st.Root, pr.Repo.CloneURL, me.Username?, pr.GitToken)`——username 从 cfg.Server.Username 取；`ErrNoCredentialHelper` 或 GitToken=="" → 回退 `CredentialURLWithAuth`（message 注明"凭据已内嵌 URL（无 credential helper）"）；
  5. `syncx.Open(st.Root)`；IsRepo false → `InitForSync(cloneURL, hasKnowledgeContent(st), msg)`（ErrRemoteNotEmpty → 409 指引）；已是仓 → `SetRemote(cloneURL)`；
  6. `config.SetSync(st.ConfigPath(), Sync{Enabled:true, Remote: cloneURL（内嵌版则内嵌版）, AutoIntervalMin: cfg.Sync.AutoIntervalMin})`；
  7. `SyncOnce` + `RecordOutcome` → `{status:"ok"/"conflict"/"error", clone_url, message: describeOutcome(o)}`（describeOutcome 在 api_sync.go 已有，直接复用——同包）。
- 透传 handler：每个 = serverClient → 对应 serverx 方法（PathValue 取 name/org/username）→ writeServerErr 或 writeJSON 200/204。`fwdReposAll` → ListRepos；`fwdAudit` → ListAudit（limit/offset 从 query 解析，缺省 50/0）。

`internal/gui/api.go` 注册区加一行：

```go
	h.registerServerAPI(api)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/gui/ -v -count=1`
Expected: 全部 PASS。

- [ ] **Step 5: Commit**

```bash
git add internal/gui/api_server.go internal/gui/api_server_test.go internal/gui/api.go
git commit -m "feat(gui): 服务器页本地端点（配置/测试/登录/me/建仓一条龙/管理类透传）"
```

---

### Task 5: GUI 服务器页（stepper 向导 + 双视图）

**Files:**
- Modify: `web/app.js`（MENUS/ICON/I18N/state/renderBody 分发 + 追加 renderServer 一整个页面段）
- Modify: `web/style.css`（移植原型的 stepper/statgrid 等类）
- Test: 无——`node --check` + 手动冒烟

**Interfaces:**
- Consumes: Task 4 全部端点契约；原型 `docs/prototypes/prototype-sync-p1-final.html` 的 server 页（**先读原型 :234-263 状态对象、:618-636 stepper 状态机、:415-433 步骤函数、管理/成员视图卡片清单与 CSS 类名 :123-145**——视觉类名照抄，DOM 构建风格（el()）不照原型的模板字符串）。
- Produces: `renderServer()` 返回 DOM 节点；`state.server = { step, user, ... }`。

**页面规格**（交互定稿 = 原型变体 B，逐条对齐）：

- MENUS 在 `prefs` 前插入 `{ key:"server", ico:ICON.server }`；ICON 加 `server`（服务器/云图标 svg）；I18N zh `server:"服务器"` / en `server:"Server"`；
- stepper 三步骤：`curStep()` = 未连接 1 / 未登录 2 / 已登录 3；已完成步骤可点击回看（回看时显示"返回当前步骤"按钮）；
- 步骤 1 连接卡：地址 + 端口输入（合成 url：`http://addr:port`）+ `测试连接`（转圈 → 成功显示"已连接：版本 + git 后端状态"，失败显示原因）+ 部署指引卡（details/summary 折叠，静态内容：compose 片段 + 五步要点，链接指向 `server/nas/README.md` 仓内文档——GUI 内嵌简版即可）；
- 步骤 2 登录卡：用户名 + 密码 + 登录按钮（转圈；401 提示"用户名或密码错误"；成功 toast + 进步骤 3）；
- 步骤 3 按角色：
  - root/admin 管理视图五卡：**状态卡**（版本/git 后端/用户数·组织数/仓库数——由 me + users + orgs + repos-all 聚合）；**用户卡**（新建用户：用户名+角色下拉（member/admin 仅 root 可见）→ 弹窗一次性显示初始密码 + git token（带复制按钮 + "只显示一次"提示）；列表 + 禁用/启用 + 重置密码（重置结果同样一次性弹窗））；**组织卡**（建组织 + 成员 chip 增减 + 团队仓创建）；**仓库总览卡**（repos-all 表）；**审计卡**（audit 表倒序）；
  - member 成员视图两卡：**我的项目绑定卡**（本地项目列表——复用 /api/projects，每行项目名 + 绑定状态（sync.is_repo）+ `一键建仓并绑定`按钮 → POST /api/server/repos {project} → busy 防连点 → toast 结果 + 刷新）；**我的组织卡**（me.orgs 只读列表）；
- 顶行右侧：`退出登录`（POST logout → 清态回步骤 2）与`断开连接`（清配置回步骤 1——PUT config 空 url）。
- 反馈纪律：全部动作转圈/disabled + toast；一次性凭据弹窗必须有复制按钮。

- [ ] **Step 1: 实现**

按上述规格在 `web/app.js` 追加页面段（放冲突页/合并编辑器段落之后），要点：

- 页面级状态：`const SRV = { url:"", user:null, users:null, orgs:null, repos:null, audit:null, bound:{}, wizardStep:null };`（数据进入步骤 3 时惰性拉取，缓存 + 操作后局部刷新）；
- `renderServer()` 骨架：

```js
function serverCurStep(){
  if(!SRV.url) return 1;
  if(!SRV.user) return 2;
  return 3;
}
function renderServer(){
  const cur = SRV.wizardStep || serverCurStep();
  const wrap = el("div","srvwrap");
  // stepper 横条：三个 stp 节点（done/cur/clickable）+ 连接线
  // bigtitle + 回看时的"返回当前步骤"按钮
  // body：cur===1 connectCard / cur===2 loginCard / cur===3 roleView
  return wrap;
}
```

- 连接卡 onConnect：`api("POST /api/server/test",{url})` → ok 则 `api("PUT /api/server/config",{url})` + SRV.url=url；
- 登录卡 onLogin：`api("POST /api/server/login",{url:SRV.url, username, password})` → SRV.user=r.user + toast；
- 角色视图的数据拉取：root/admin → 并行 `api users/orgs/repos-all/audit`；member → `api me` + `api /api/projects`；
- 用户卡建用户：`POST /api/server/users {username, role}` → 响应 `{password, git_token}` → 模态弹窗（复用现有 mask/modal 样式——读 app.js 现有弹窗先例，如详情编辑用的弹窗或原型 .mask/.modal）两行只读输入框 + 复制按钮 + "只显示这一次"警示；
- 成员绑定卡 onBind：`POST /api/server/repos {project}` → busy → toast message + 刷新该行状态；
- 全部 I18N 双语键（srv* 一族）；
- style.css 移植原型类：`.stepper/.stp/.stp-line/.bigtitle/.statgrid/.statcell/table.list/.chip/.mask/.modal`（读原型 :123-145 CSS 原文，变量名映射到 style.css 现有变量表）。

renderBody 分发链加 `else if(state.menu==="server"){ main.appendChild(renderServer()); }`（在 manage 分支之后）；菜单 onclick 的刷新钩子加 `if(m.key==="server"){ /* SRV 数据惰性，无需预拉 */ }`。

- [ ] **Step 2: 语法检查 + 冒烟**

`node --check web/app.js`；手动冒烟（无真服务器时用 `OKSERVER_...` 起一个本地 okserver fake 模式：构建 `go build -o /tmp/okserver ./cmd/okserver` + 起在 13100 端口）：三步走通（连接 fake → root 登录（读 INITIAL_ROOT_PASSWORD）→ 管理视图五卡 → 建用户弹窗 → 退出 → alice 登录 → 成员视图绑定卡）。

- [ ] **Step 3: Commit**

```bash
git add web/app.js web/style.css
git commit -m "feat(web): 服务器页（stepper 三步向导 + 管理/成员双视图）"
```

---

### Task 6: 管理页 not_repo 接通服务器建仓

**Files:**
- Modify: `web/app.js`（doProjectSync 的 not_repo 分支，:1819-1823 区）
- Test: 无——`node --check` + 手动冒烟

**Interfaces:**
- Consumes: Task 4 的 `GET /api/server/config`（logged_in）与 `POST /api/server/repos`；Task 5 的 SRV/toast。
- Produces: not_repo 分支新行为。

- [ ] **Step 1: 实现**

`doProjectSync` 的 not_repo 分支改为：

```js
if(r.status === "not_repo"){
  // 已登录服务器 → 提供"经服务器建仓并初始化"（真动作）；未登录 → 纯指引 toast
  try{
    const sc = await api("/api/server/config");
    if(sc.logged_in){
      if(confirm(t("syncServerBindConfirm"))){
        btn.classList.add("spinning");
        const r2 = await api("/api/server/repos", { method:"POST", body:{ project: project } });
        toast(r2.message || t("syncToastLatest"), r2.status === "error");
        refreshManage();
      }
      return;
    }
  }catch(_){ /* 未配置服务器，落纯指引 */ }
  toast(t("syncInitGuide"), true);
  return;
}
```

I18N 补键 `syncServerBindConfirm`（zh: "该项目尚未初始化同步。经服务器建仓并初始化（建仓 + 绑定 remote + 首次推送）？" / en: "..."）。

- [ ] **Step 2: 语法检查 + 冒烟**

`node --check web/app.js`；冒烟：fake okserver 登录态下，未初始化项目点同步 → confirm → 建仓绑定成功 toast + 状态点变绿。

- [ ] **Step 3: Commit**

```bash
git add web/app.js
git commit -m "feat(web): 管理页未初始化项目接通服务器一键建仓"
```

---

### Task 7: 全链路 handler 测试（假 okserver + 真 git bare）

**Files:**
- Modify: `internal/gui/api_server_test.go`（追加）

**Interfaces:**
- Consumes: Task 4 全部 + Task 3 syncx 凭据 + `mkSyncProject`/`stFor`（api_sync_test.go 已有）+ newEnv/do 基建。

- [ ] **Step 1: Write the test**

```go
// TestApiServerReposFullFlow 建仓一条龙全链路：假 okserver（provision 返回 file:// 裸仓地址）
// → POST /api/server/repos → 断言：项目仓已 init + 内容已推到 bare + 项目 [sync] 已落盘
// + credential 处理路径有提示（本机无 helper 时内嵌 URL）。
func TestApiServerReposFullFlow(t *testing.T) {
	h, _, okHome := newEnv(t)
	// "Gitea 侧"：file:// 裸仓
	bare := t.TempDir()
	gitRun(t, bare, "init", "--bare", "-b", "main")
	bareURL := "file://" + filepath.ToSlash(bare)
	fake := fakeOKServer(t, bareURL)
	defer fake.Close()
	srv := httptest.NewServer(h)
	defer srv.Close()

	// 登录（配 url + token 落盘）
	res, body := do(t, "POST", srv.URL+"/api/server/login", testToken, map[string]any{"url": fake.URL, "username": "alice", "password": "pw"})
	if res != 200 {
		t.Fatalf("login: %d %s", res, body)
	}
	// 注册一个带内容的未初始化项目
	name := "bindemo"
	projDir := filepath.Join(t.TempDir(), "work")
	mkProjectAt(t, okHome, name, projDir)
	st := stFor(t, okHome, name)
	if err := os.MkdirAll(st.KnowledgeDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(st.KnowledgeDir(), "k.md"), []byte("内容 v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// 一条龙
	res, body = do(t, "POST", srv.URL+"/api/server/repos", testToken, map[string]any{"project": name})
	if res != 200 {
		t.Fatalf("repos: %d %s", res, body)
	}
	var out struct {
		Status   string `json:"status"`
		CloneURL string `json:"clone_url"`
		Message  string `json:"message"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatal(err)
	}
	if out.Status != "ok" || out.CloneURL == "" {
		t.Fatalf("out: %s", body)
	}
	// 项目已是仓且首个提交推到 bare
	if !syncx.Open(st.Root).IsRepo() {
		t.Fatal("project should be repo")
	}
	log := gitOut(t, bare, "log", "--oneline", "main")
	if !strings.Contains(log, "sync:") {
		t.Fatalf("bare log: %q", log)
	}
	// [sync] 落盘
	cfgData, _ := os.ReadFile(st.ConfigPath())
	if !strings.Contains(string(cfgData), "[sync]") || !strings.Contains(string(cfgData), "enabled = true") {
		t.Fatalf("project config:\n%s", cfgData)
	}
}
```

（import 需 `"openknowledge/internal/syncx"`、`"strings"` 等。）

- [ ] **Step 2: Run + 全量回归**

Run: `go test ./internal/gui/ -run TestApiServerReposFullFlow -v -count=1` → PASS；`go test ./... -count=1` 全绿。

- [ ] **Step 3: Commit**

```bash
git add internal/gui/api_server_test.go
git commit -m "test(gui): 建仓一条龙全链路（假 okserver + 真 git bare）"
```

---

### Task 8: 变更日志与收尾

**Files:**
- Create: `docs/changelogs/2026-08-29-server-local-gui.md`
- Modify: `docs/superpowers/specs/2026-08-25-personal-sync-p1-design.md`（状态行）

- [ ] **Step 1: 变更日志**

```markdown
# 本地接入与 GUI 服务器页（P1-C2）

日期：2026-08-29

P1 多端同步整体闭环。internal/serverx 新增 okserver 客户端（Bearer 薄 HTTP 客户端照 llmx 形态，全端点：meta/login/me/repos/personal + 管理类 users/orgs/members/repos/team/repos/audit，错误包装 *serverx.Error 透传状态码）。全局 config.toml 新增 [server] 段（url/username/token，SetServer 行级写入，token 空串保留旧值）。okd 新增服务器页端点族（internal/gui/api_server.go）：GET/PUT /api/server/config（token 不回传）、POST test（连接测试）、POST login/logout（登录成功 url+username+token 落盘）、GET me、POST /api/server/repos 建仓一条龙（provision → 写系统 git credential helper（无 helper 回退 URL 内嵌凭据并注明）→ InitForSync/SetRemote → SetSync → 首次同步）、管理类透传一组（users/orgs/members/repos/team/repos-all/audit）。GUI 新增"服务器"页（左导航"设置"前）：stepper 三步向导（连接→登录→按角色落地，已完成步骤可回看），root/admin 管理视图五卡（状态/用户（建用户与重置密码一次性凭据弹窗带复制按钮）/组织（成员 chip + 团队仓）/仓库总览/审计），member 成员视图两卡（项目绑定（一键建仓并绑定）/我的组织），部署指引折叠卡。管理页未初始化项目的同步按钮接通服务器建仓（已登录时 confirm 后一条龙，未登录纯指引 toast）。SSRF 裁决：服务器地址来自用户配置/输入 + 本地 token 鉴权，不做回环限制（与 ollama 探测端点不同）。全链路 handler 测试：假 okserver + file:// 裸仓真 git 闭环。P1 三阶段（A 引擎/B GUI 同步面/C 服务端+本地接入）至此全部交付。
```

- [ ] **Step 2: 设计文档状态行**

`- 状态：P1 全部实施完成（2026-08-29，A 引擎 + B GUI 同步面 + C 服务端与本地接入）；P2 merge driver 起为后续阶段`

- [ ] **Step 3: Commit**

```bash
git add docs/changelogs/2026-08-29-server-local-gui.md docs/superpowers/specs/2026-08-25-personal-sync-p1-design.md
git commit -m "docs: P1-C2 变更日志与设计文档状态（P1 整体闭环）"
```

---

## 执行顺序与依赖

```
Task 1（config）→ Task 2（serverx）→ Task 3（syncx 凭据）→ Task 4（okd 端点）→ Task 5（GUI 服务器页）→ Task 6（管理页接通）→ Task 7（全链路测试）→ Task 8（收尾）
Task 2/3 可与 1 并行（接口先行）；按序执行。
```

## Self-Review 记录

- **Spec 覆盖**：§10.1 三步引导（T5）✓；§10.2 管理视图五卡（T5）✓；§10.3 成员视图两卡（T5）✓；§10.4 通用（退出登录/部署指引/本地 okd API 五个+管理类透传）（T4/T5）✓；§12 全局 [server]（T1）✓；§9.4 凭据分发本地侧（T3/T4）✓。管理页建仓入口（P1-B 移交）在 T6。
- **Placeholder 扫描**：T5 前端页面段以语义规格 + 原型引用给出（原型即像素级规格），函数骨架给了；T4 透传 handler 以统一模式描述（每个 5 行薄 handler，模式代码给了）。
- **类型一致性**：serverx 的 Meta/MeInfo/RepoInfo/ProvisionResult/ServerUser/ServerOrg/ServerRepo/ServerAudit/Error 跨 T2/T4/T5 核对一致；config.Server 字段名一致；logout 的 SetServer 空 URL 语义在 T4 定案写明。
- **已知取舍（评审可见）**：git token 无 helper 时内嵌 URL（v1）；部署指引卡 GUI 内嵌简版（完整版在 server/nas/README.md）；审计/仓库总览无分页 UI（API 支持，前端一次拉 50 条）。
