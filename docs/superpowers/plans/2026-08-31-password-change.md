# okserver 密码修改与强制改密 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** okserver 增加自助改密端点 + must_change_password 强制改密拦截（初始密码/管理员重置/root 运维重置后必须改密），客户端 GUI 提供强制改密弹窗与自助改密入口。

**Architecture:** 服务端 `internal/oksrv` 的 users 表加布尔标记列（PRAGMA 检测 + ALTER TABLE 幂等迁移），`auth` 之后插 `gate` 中间件按白名单拦截；客户端 `web/app.js` 经 `internal/serverx` + `internal/gui` 转发层调新端点，全局 403 钩子弹不可取消改密框。

**Tech Stack:** Go 1.22+（ServeMux 模式路由）、modernc.org/sqlite、x/crypto/bcrypt、原生 JS（web/app.js 无框架）。

**Spec:** `docs/superpowers/specs/2026-08-31-password-change-design.md`

## Global Constraints

- 仅标准库 + modernc.org/sqlite + golang.org/x/crypto——**不引新依赖**。
- 密码明文只存在于请求体内存，存储层只经手 bcrypt 哈希（DefaultCost）。
- 行为变化类新测试必须先跑一遍确认对旧代码变红（本项目既有教训：非判别性测试无价值）。
- `web/app.js` 的 i18n 键必须 zh/en 双语同步添加。
- 提交信息风格：`type(scope): 中文描述`（参照 `git log`，如 `feat(web): ...`）。
- 时间列一律 TEXT 存 UTC RFC3339；新代码注释风格与所在文件一致（中文注释）。
- GUI 手动验证用 `go run ./cmd/ok gui`（仓库根目录运行，`webdir.Find` 走 `<cwd>/web` 直接用源码页）；**不要**用 dist 下的旧二进制验证 GUI 改动（既有坑：裸 go build 不同步 dist/web）。

---

### Task 1: 存储层——must_change_password 列 + 会话删除方法

**Files:**
- Modify: `internal/oksrv/store.go`（schema、OpenStore、User、scanUser、三个 SELECT、两个新方法）
- Modify: `internal/oksrv/auth.go:96-97`（SessionUser 的 SELECT 列）
- Test: `internal/oksrv/store_test.go`

**Interfaces:**
- Produces:
  - `User.MustChangePassword bool`（`internal/oksrv/store.go` User 结构体新字段）
  - `func (s *Store) SetMustChangePassword(username string, v bool) error`
  - `func (s *Store) DeleteUserSessionsExcept(userID int64, keepTokenHash string) error`——`keepTokenHash` 传空串删全部
- Consumes: 无（本 Task 是地基）

- [ ] **Step 1: 写失败测试**

在 `internal/oksrv/store_test.go` 末尾追加（文件头 import 需加 `crypto/sha256`、`database/sql`、`encoding/hex`）：

```go
// 存量旧库（六列 users 表，无 must_change_password）OpenStore 应自动补列迁移，
// 老行默认 0；迁移幂等（重开不报错）；标记可读写。
func TestUserMustChangeMigration(t *testing.T) {
	dir := t.TempDir()
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(filepath.Join(dir, "okserver.db")))
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE users (
	  id INTEGER PRIMARY KEY AUTOINCREMENT,
	  username TEXT NOT NULL UNIQUE,
	  role TEXT NOT NULL,
	  password_hash TEXT NOT NULL,
	  disabled INTEGER NOT NULL DEFAULT 0,
	  created_at TEXT NOT NULL
	)`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO users(username,role,password_hash,created_at) VALUES('old','member','h','2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	s, err := OpenStore(dir)
	if err != nil {
		t.Fatalf("OpenStore migrate: %v", err)
	}
	u := s.GetUser("old")
	if u == nil || u.MustChangePassword {
		t.Fatalf("migrated user: %+v", u)
	}
	if err := s.SetMustChangePassword("old", true); err != nil {
		t.Fatal(err)
	}
	if !s.GetUser("old").MustChangePassword {
		t.Fatal("flag should be set")
	}
	if err := s.SetMustChangePassword("old", false); err != nil {
		t.Fatal(err)
	}
	if s.GetUser("old").MustChangePassword {
		t.Fatal("flag should be cleared")
	}
	s.Close()
	if s2, err := OpenStore(dir); err != nil { // 幂等：再开一次不报错
		t.Fatalf("reopen: %v", err)
	} else {
		s2.Close()
	}
}

// DeleteUserSessionsExcept：保留指定会话、删其余；空 keep 删全部。
func TestDeleteUserSessionsExcept(t *testing.T) {
	s, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	u, err := s.CreateUser("alice", "member", "h")
	if err != nil {
		t.Fatal(err)
	}
	tok1, err := s.CreateSession(u.ID)
	if err != nil {
		t.Fatal(err)
	}
	tok2, err := s.CreateSession(u.ID)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(tok1))
	if err := s.DeleteUserSessionsExcept(u.ID, hex.EncodeToString(sum[:])); err != nil {
		t.Fatal(err)
	}
	if s.SessionUser(tok1) == nil {
		t.Fatal("当前会话应保留")
	}
	if s.SessionUser(tok2) != nil {
		t.Fatal("其他会话应被删除")
	}
	if err := s.DeleteUserSessionsExcept(u.ID, ""); err != nil {
		t.Fatal(err)
	}
	if s.SessionUser(tok1) != nil {
		t.Fatal("空 keep 应删全部会话")
	}
}
```

- [ ] **Step 2: 跑测试确认变红**

Run: `go test ./internal/oksrv/ -run 'TestUserMustChangeMigration|TestDeleteUserSessionsExcept' -v`
Expected: 编译失败（`s.SetMustChangePassword undefined`、`u.MustChangePassword undefined` 等）——红。

- [ ] **Step 3: 实现**

`internal/oksrv/store.go`：

① schema 常量 users 表加列（`disabled` 行之后）：

```go
  disabled INTEGER NOT NULL DEFAULT 0,
  must_change_password INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL
```

② OpenStore 在 `db.Exec(schema)` 成功后加迁移调用：

```go
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("建 schema 失败: %w", err)
	}
	// 存量库补列：CREATE TABLE IF NOT EXISTS 不会改旧表，PRAGMA 检测 + ALTER，幂等
	if err := migrateUserColumns(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("迁移 users 表失败: %w", err)
	}
```

③ 迁移助手（放 OpenStore 之后）：

```go
// migrateUserColumns 给存量库补 users 表后加列。
func migrateUserColumns(db *sql.DB) error {
	has, err := hasColumn(db, "users", "must_change_password")
	if err != nil {
		return err
	}
	if !has {
		if _, err := db.Exec(`ALTER TABLE users ADD COLUMN must_change_password INTEGER NOT NULL DEFAULT 0`); err != nil {
			return err
		}
	}
	return nil
}

// hasColumn 用 PRAGMA table_info 检测列是否存在。
func hasColumn(db *sql.DB, table, col string) (bool, error) {
	rows, err := db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid, notnull, pk int
		var name, typ string
		var dflt any
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			return false, err
		}
		if name == col {
			return true, nil
		}
	}
	return false, rows.Err()
}
```

④ User 结构体加字段：

```go
// User 是一个管理面账号；Role ∈ root/admin/member，root 全库唯一。
// MustChangePassword=true 表示持初始/重置密码，HTTP 层拦截其改密外的一切请求。
type User struct {
	ID                 int64
	Username           string
	Role               string
	Disabled           bool
	MustChangePassword bool
	CreatedAt          time.Time
}
```

⑤ scanUser 改为扫六列：

```go
// scanUser 从一行 users 查询结果扫出 User；created_at TEXT 还原为 time.Time。
func scanUser(scan func(dest ...any) error) (*User, error) {
	var u User
	var disabled, mustChange int
	var createdAt string
	if err := scan(&u.ID, &u.Username, &u.Role, &disabled, &mustChange, &createdAt); err != nil {
		return nil, err
	}
	u.Disabled = disabled != 0
	u.MustChangePassword = mustChange != 0
	u.CreatedAt = parseTime(createdAt)
	return &u, nil
}
```

⑥ 三处 SELECT 列改为 `id,username,role,disabled,must_change_password,created_at`：`getUserByID`、`GetUser`、`ListUsers`。

⑦ 两个新方法（放 `SetUserPasswordHash` 之后）：

```go
// SetMustChangePassword 置/清"必须改密"标记（初始/重置密码置 1，自助改密成功清 0）。
func (s *Store) SetMustChangePassword(username string, v bool) error {
	n := 0
	if v {
		n = 1
	}
	_, err := s.db.Exec(`UPDATE users SET must_change_password=? WHERE username=?`, n, username)
	return err
}

// DeleteUserSessionsExcept 删除该用户除 keepTokenHash 外的全部会话；
// keepTokenHash 传空串即删全部（管理员重置/reset-root 场景）。
func (s *Store) DeleteUserSessionsExcept(userID int64, keepTokenHash string) error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE user_id=? AND token_hash != ?`, userID, keepTokenHash)
	return err
}
```

`internal/oksrv/auth.go`：SessionUser 的 SELECT 改为：

```go
	u, err := scanUser(s.db.QueryRow(
		"SELECT id, username, role, disabled, must_change_password, created_at FROM users WHERE id=?", userID).Scan)
```

- [ ] **Step 4: 跑测试确认变绿**

Run: `go test ./internal/oksrv/ -v`
Expected: 全部 PASS（含新两个测试与全部存量测试）。

- [ ] **Step 5: Commit**

```bash
git add internal/oksrv/store.go internal/oksrv/auth.go internal/oksrv/store_test.go
git commit -m "feat(oksrv): users 表加 must_change_password 列（幂等迁移）+ DeleteUserSessionsExcept"
```

---

### Task 2: 标记置位点——EnsureRoot / ResetRoot / 建用户 / 管理员重置

**Files:**
- Modify: `internal/oksrv/auth.go:36-49`（EnsureRoot）
- Modify: `internal/oksrv/store.go:239-256`（ResetRoot）
- Modify: `internal/oksrv/http.go:273` 附近（apiUserCreate）、`:305` 附近（apiUserResetPassword）
- Test: `internal/oksrv/auth_test.go`、`internal/oksrv/store_test.go`、`internal/oksrv/http_test.go`

**Interfaces:**
- Consumes: Task 1 的 `SetMustChangePassword`、`DeleteUserSessionsExcept`、`User.MustChangePassword`
- Produces: 无语义新接口；行为约定——初始/重置密码落库时标记为 1，重置时目标用户会话全清

- [ ] **Step 1: 写失败测试**

① `internal/oksrv/auth_test.go` TestPasswordAndRoot，在 `// root 可登录` 断言块之后追加：

```go
	// 初始 root 密码强制首登改密
	if !s.GetUser("root").MustChangePassword {
		t.Fatal("初始 root 密码必须置强制改密标记")
	}
```

② `internal/oksrv/store_test.go` TestResetRoot，在 `new1, err := st.ResetRoot()` 之前插入会话准备：

```go
	// 重置前置一个 root 会话
	root := st.GetUser("root")
	tok, err := st.CreateSession(root.ID)
	if err != nil {
		t.Fatal(err)
	}
```

在该函数末尾（INITIAL_ROOT_PASSWORD 断言之后）追加：

```go
	// 重置 = 强制改密 + 踢掉 root 全部旧会话
	if !st.GetUser("root").MustChangePassword {
		t.Fatal("ResetRoot 应置强制改密标记")
	}
	if st.SessionUser(tok) != nil {
		t.Fatal("ResetRoot 应踢掉 root 全部旧会话")
	}
```

③ `internal/oksrv/http_test.go` 末尾追加：

```go
// TestUserCreateAndResetSetFlag 钉住置位点：建用户（随机/自选密码）与管理员重置
// 都置强制改密标记；重置同时踢掉目标用户全部旧会话。
func TestUserCreateAndResetSetFlag(t *testing.T) {
	srv, st, _ := newTestServer(t)
	defer srv.Close()
	rootTok := login(t, srv, "root", getenv(t, "OK_TEST_ROOT_PW"))

	code, out := call(t, srv, "POST", "/api/v1/users", rootTok, map[string]string{"username": "alice"})
	if code != 200 {
		t.Fatalf("create alice: %d %v", code, out)
	}
	if !st.GetUser("alice").MustChangePassword {
		t.Fatal("建用户应置强制改密标记")
	}
	code, out = call(t, srv, "POST", "/api/v1/users", rootTok, map[string]string{"username": "dave", "password": "mypass123"})
	if code != 200 {
		t.Fatalf("create dave: %d %v", code, out)
	}
	if !st.GetUser("dave").MustChangePassword {
		t.Fatal("自选密码建用户同样置标记")
	}

	// dave 登录两次拿两个会话；清标记模拟已自助改过密
	daveTok1 := login(t, srv, "dave", "mypass123")
	daveTok2 := login(t, srv, "dave", "mypass123")
	if err := st.SetMustChangePassword("dave", false); err != nil {
		t.Fatal(err)
	}

	// root 重置 dave → 标记置位 + 全部旧会话失效 + 新密码可登录
	code, out = call(t, srv, "POST", "/api/v1/users/dave/reset-password", rootTok, nil)
	if code != 200 || out["password"].(string) == "" {
		t.Fatalf("reset dave: %d %v", code, out)
	}
	if !st.GetUser("dave").MustChangePassword {
		t.Fatal("重置应置强制改密标记")
	}
	if st.SessionUser(daveTok1) != nil || st.SessionUser(daveTok2) != nil {
		t.Fatal("重置应踢掉该用户全部旧会话")
	}
	login(t, srv, "dave", out["password"].(string))
}
```

- [ ] **Step 2: 跑测试确认变红**

Run: `go test ./internal/oksrv/ -run 'TestPasswordAndRoot|TestResetRoot|TestUserCreateAndResetSetFlag' -v`
Expected: FAIL——`初始 root 密码必须置强制改密标记` / `ResetRoot 应置强制改密标记` / `建用户应置强制改密标记` 等断言失败。

- [ ] **Step 3: 实现**

① `internal/oksrv/auth.go` EnsureRoot，CreateUser 成功后追加：

```go
	if _, err := s.CreateUser("root", "root", hash); err != nil {
		return "", false, err
	}
	// 初始 root 密码强制首登改密（自助改密成功后清除）
	if err := s.SetMustChangePassword("root", true); err != nil {
		return "", false, err
	}
```

② `internal/oksrv/store.go` ResetRoot，SetUserPasswordHash 成功后追加：

```go
	// 重置 = 强制改密 + 踢掉 root 全部旧会话
	if err := s.SetMustChangePassword("root", true); err != nil {
		return "", err
	}
	if root := s.GetUser("root"); root != nil {
		if err := s.DeleteUserSessionsExcept(root.ID, ""); err != nil {
			return "", err
		}
	}
```

③ `internal/oksrv/http.go` apiUserCreate，`s.st.CreateUser` 成功块之后追加：

```go
	// 他人代设的初始密码（自选或随机）一律强制首登改密
	if err := s.st.SetMustChangePassword(in.Username, true); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
```

④ `internal/oksrv/http.go` apiUserResetPassword，`SetUserPasswordHash` 成功后追加：

```go
	// 重置 = 强制下次登录改密 + 踢掉该用户全部旧会话
	if err := s.st.SetMustChangePassword(name, true); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.st.DeleteUserSessionsExcept(target.ID, ""); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
```

- [ ] **Step 4: 跑测试确认变绿**

Run: `go test ./internal/oksrv/ -v`
Expected: 全部 PASS（gate 尚未上线，存量管理流测试不受影响）。

- [ ] **Step 5: Commit**

```bash
git add internal/oksrv/auth.go internal/oksrv/store.go internal/oksrv/http.go internal/oksrv/auth_test.go internal/oksrv/store_test.go internal/oksrv/http_test.go
git commit -m "feat(oksrv): 初始/重置密码置强制改密标记，重置时踢掉目标用户全部会话"
```

---

### Task 3: HTTP 层——gate 中间件 + change-password 端点 + 标记下发

**Files:**
- Modify: `internal/oksrv/http.go`（mux 注册、gate、apiChangePassword、apiLogin/apiMe 响应）
- Modify: `internal/oksrv/auth.go`（bearerTokenHash，import 加 `net/http`）
- Test: `internal/oksrv/http_test.go`（newTestServer 调整 + 存量两处清标记 + 两个新测试）

**Interfaces:**
- Consumes: Task 1/2 全部产物
- Produces:
  - `POST /api/v1/change-password`——请求 `{old_password, new_password}`；200 `{ok:true}`；401 旧密码错误；400 规则不符；429 限流
  - 403 响应体 `{"error":"must_change_password"}`（带标记会话访问非白名单端点）
  - login/me 响应带 `must_change_password` 布尔字段
  - `func bearerTokenHash(r *http.Request) string`（auth.go）

- [ ] **Step 1: 写失败测试**

`internal/oksrv/http_test.go` 末尾追加两个测试：

```go
// TestForcePasswordChangeFlow 钉住强制改密全链路：建用户置标记 → gate 拦截 →
// 白名单放行 → 自助改密（错误分支 + 成功）→ 标记清除、功能恢复、旧密码失效。
func TestForcePasswordChangeFlow(t *testing.T) {
	srv, _, _ := newTestServer(t)
	defer srv.Close()
	rootTok := login(t, srv, "root", getenv(t, "OK_TEST_ROOT_PW"))

	code, out := call(t, srv, "POST", "/api/v1/users", rootTok, map[string]string{"username": "bob"})
	if code != 200 {
		t.Fatalf("create bob: %d %v", code, out)
	}
	bobPW := out["password"].(string)

	// 登录响应带标记
	code, out = call(t, srv, "POST", "/api/v1/login", "", map[string]string{"username": "bob", "password": bobPW})
	if code != 200 {
		t.Fatalf("bob login: %d %v", code, out)
	}
	bobTok := out["token"].(string)
	if u := out["user"].(map[string]any); u["must_change_password"] != true {
		t.Fatalf("login 响应应带 must_change_password=true: %v", u)
	}

	// gate：普通端点 403 must_change_password
	code, out = call(t, srv, "POST", "/api/v1/repos/personal", bobTok, map[string]string{"project": "demo"})
	if code != 403 || out["error"] != "must_change_password" {
		t.Fatalf("gate must 403: %d %v", code, out)
	}
	// 白名单：me 放行且带标记
	code, out = call(t, srv, "GET", "/api/v1/me", bobTok, nil)
	if code != 200 || out["must_change_password"] != true {
		t.Fatalf("me must pass gate: %d %v", code, out)
	}

	// 改密错误分支：旧密码错 401 / 太短 400 / 新旧相同 400
	code, _ = call(t, srv, "POST", "/api/v1/change-password", bobTok, map[string]string{"old_password": "bad", "new_password": "newpass123"})
	if code != 401 {
		t.Fatalf("wrong old must be 401: %d", code)
	}
	code, _ = call(t, srv, "POST", "/api/v1/change-password", bobTok, map[string]string{"old_password": bobPW, "new_password": "short"})
	if code != 400 {
		t.Fatalf("short must be 400: %d", code)
	}
	code, _ = call(t, srv, "POST", "/api/v1/change-password", bobTok, map[string]string{"old_password": bobPW, "new_password": bobPW})
	if code != 400 {
		t.Fatalf("same password must be 400: %d", code)
	}

	// 改密成功 → 200，标记清除，普通端点恢复
	code, _ = call(t, srv, "POST", "/api/v1/change-password", bobTok, map[string]string{"old_password": bobPW, "new_password": "newpass123"})
	if code != 200 {
		t.Fatalf("change-password: %d", code)
	}
	code, out = call(t, srv, "GET", "/api/v1/me", bobTok, nil)
	if code != 200 || out["must_change_password"] != false {
		t.Fatalf("flag must be cleared: %d %v", code, out)
	}
	code, _ = call(t, srv, "POST", "/api/v1/repos/personal", bobTok, map[string]string{"project": "demo"})
	if code != 200 {
		t.Fatalf("gated endpoint must recover: %d", code)
	}
	// 旧密码失效、新密码可登录
	if code, _ := call(t, srv, "POST", "/api/v1/login", "", map[string]string{"username": "bob", "password": bobPW}); code != 401 {
		t.Fatalf("old password must fail: %d", code)
	}
	login(t, srv, "bob", "newpass123")
}

// TestChangePasswordKeepsCurrentKicksOthers 改密后当前会话保留、其他会话失效。
func TestChangePasswordKeepsCurrentKicksOthers(t *testing.T) {
	srv, st, _ := newTestServer(t)
	defer srv.Close()
	rootTok := login(t, srv, "root", getenv(t, "OK_TEST_ROOT_PW"))

	code, _ := call(t, srv, "POST", "/api/v1/users", rootTok, map[string]string{"username": "carol", "password": "mypass123"})
	if code != 200 {
		t.Fatalf("create carol: %d", code)
	}
	tok1 := login(t, srv, "carol", "mypass123")
	tok2 := login(t, srv, "carol", "mypass123")
	if err := st.SetMustChangePassword("carol", false); err != nil {
		t.Fatal(err)
	}

	code, _ = call(t, srv, "POST", "/api/v1/change-password", tok1, map[string]string{"old_password": "mypass123", "new_password": "newpass456"})
	if code != 200 {
		t.Fatalf("change-password: %d", code)
	}
	if code, _ := call(t, srv, "GET", "/api/v1/me", tok2, nil); code != 401 {
		t.Fatalf("other session must be kicked: %d", code)
	}
	if code, _ := call(t, srv, "GET", "/api/v1/me", tok1, nil); code != 200 {
		t.Fatalf("current session must be kept: %d", code)
	}
}
```

- [ ] **Step 2: 跑测试确认变红**

Run: `go test ./internal/oksrv/ -run 'TestForcePasswordChangeFlow|TestChangePasswordKeepsCurrentKicksOthers' -v`
Expected: FAIL——gate 未实现时 bob 建仓返回 200 而非 403（`gate must 403` 断言失败）。

- [ ] **Step 3: 实现**

① `internal/oksrv/auth.go` 加 `net/http` import，末尾追加：

```go
// bearerTokenHash 返回请求携带的 Bearer token 的库存哈希（SHA-256 hex）；
// 无 Bearer 头返回空串。改密后"踢其他会话保当前"用。
func bearerTokenHash(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if len(h) <= 7 || h[:7] != "Bearer " {
		return ""
	}
	sum := sha256.Sum256([]byte(h[7:]))
	return hex.EncodeToString(sum[:])
}
```

② `internal/oksrv/http.go` NewMux 注册块整体改为：

```go
	mux.HandleFunc("GET /api/v1/meta", s.apiMeta)
	mux.HandleFunc("POST /api/v1/login", s.apiLogin)
	// 强制改密白名单：me 与 change-password 不套 gate，其余已认证端点全拦
	mux.HandleFunc("GET /api/v1/me", s.auth(s.apiMe))
	mux.HandleFunc("POST /api/v1/change-password", s.auth(s.apiChangePassword))
	mux.HandleFunc("POST /api/v1/repos/personal", s.auth(s.gate(s.apiPersonalRepo)))
	mux.HandleFunc("GET /api/v1/users", s.auth(s.gate(s.admin(s.apiUsers))))
	mux.HandleFunc("POST /api/v1/users", s.auth(s.gate(s.admin(s.apiUserCreate))))
	mux.HandleFunc("POST /api/v1/users/{name}/reset-password", s.auth(s.gate(s.admin(s.apiUserResetPassword))))
	mux.HandleFunc("DELETE /api/v1/users/{name}", s.auth(s.gate(s.admin(s.apiUserDelete))))
	mux.HandleFunc("POST /api/v1/users/{name}/disable", s.auth(s.gate(s.admin(s.apiUserDisable(true)))))
	mux.HandleFunc("POST /api/v1/users/{name}/enable", s.auth(s.gate(s.admin(s.apiUserDisable(false)))))
	mux.HandleFunc("GET /api/v1/orgs", s.auth(s.gate(s.admin(s.apiOrgs))))
	mux.HandleFunc("POST /api/v1/orgs", s.auth(s.gate(s.admin(s.apiOrgCreate))))
	mux.HandleFunc("POST /api/v1/orgs/{org}/members", s.auth(s.gate(s.admin(s.apiOrgMemberAdd))))
	mux.HandleFunc("DELETE /api/v1/orgs/{org}/members/{username}", s.auth(s.gate(s.admin(s.apiOrgMemberRemove))))
	mux.HandleFunc("POST /api/v1/repos/team", s.auth(s.gate(s.admin(s.apiTeamRepo))))
	mux.HandleFunc("GET /api/v1/repos", s.auth(s.gate(s.admin(s.apiRepos))))
	mux.HandleFunc("GET /api/v1/audit", s.auth(s.gate(s.admin(s.apiAudit))))
```

③ gate 中间件（放 `admin` 之后）：

```go
// gate：强制改密拦截——带 must_change_password 标记的会话只放行白名单端点
// （me/change-password 注册时不套 gate），其余一律 403，客户端据此弹强制改密框。
func (s *server) gate(fn func(http.ResponseWriter, *http.Request, *User)) func(http.ResponseWriter, *http.Request, *User) {
	return func(w http.ResponseWriter, r *http.Request, u *User) {
		if u.MustChangePassword {
			writeErr(w, http.StatusForbidden, "must_change_password")
			return
		}
		fn(w, r, u)
	}
}
```

④ apiChangePassword（放 apiMe 之后）：

```go
// apiChangePassword 自助改密：已认证用户凭旧密码换新密码。成功后清强制改密标记、
// 踢掉除当前会话外的全部会话（当前会话保留，客户端无需重登）。
// 旧密码失败计入登录限流（与 apiLogin 共用同一 LoginLimiter，防在线爆破）。
func (s *server) apiChangePassword(w http.ResponseWriter, r *http.Request, u *User) {
	var in struct {
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if !s.limiter.Allow(u.Username) {
		writeErr(w, http.StatusTooManyRequests, "尝试次数过多，请稍后再试")
		return
	}
	if s.st.VerifyLogin(u.Username, in.OldPassword) == nil {
		s.limiter.Fail(u.Username)
		writeErr(w, http.StatusUnauthorized, "旧密码错误")
		return
	}
	if len(in.NewPassword) < 8 {
		writeErr(w, http.StatusBadRequest, "密码至少 8 位")
		return
	}
	if in.NewPassword == in.OldPassword {
		writeErr(w, http.StatusBadRequest, "新密码不能与旧密码相同")
		return
	}
	hash, err := HashPassword(in.NewPassword)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.st.SetUserPasswordHash(u.Username, hash); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.st.SetMustChangePassword(u.Username, false); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.st.DeleteUserSessionsExcept(u.ID, bearerTokenHash(r)); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.limiter.Reset(u.Username)
	s.st.Audit(u.Username, "change-password", u.Username, "")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
```

⑤ apiLogin 响应 user 对象加字段：

```go
	writeJSON(w, http.StatusOK, map[string]any{
		"token": tok,
		"user":  map[string]any{"name": u.Username, "role": u.Role, "must_change_password": u.MustChangePassword},
	})
```

⑥ apiMe 响应加字段：

```go
	writeJSON(w, http.StatusOK, map[string]any{
		"name": u.Username, "role": u.Role, "orgs": orgs, "repos": repos,
		"must_change_password": u.MustChangePassword,
	})
```

⑦ `internal/oksrv/http_test.go` newTestServer 清 root 标记（gate 上线后存量管理流测试全靠它）：

```go
	pw, created, err := s.EnsureRoot()
	if err != nil || !created {
		t.Fatalf("root: %v", err)
	}
	// 存量管理流测试默认 root 已自助改密（否则 gate 拦截全部管理端点）；
	// 强制改密行为本身由 TestForcePasswordChange* / TestUserCreateAndResetSetFlag 覆盖。
	if err := s.SetMustChangePassword("root", false); err != nil {
		t.Fatalf("clear root flag: %v", err)
	}
```

⑧ 存量测试两处清标记：

- TestFullManagementFlow：`srv, _, _ := newTestServer(t)` 改 `srv, st, _ := newTestServer(t)`；`aliceTok := login(t, srv, "alice", pw)` 之前插入：

```go
	// alice 视为已自助改密（初始密码标记会让 gate 拦截后续建仓端点）
	if err := st.SetMustChangePassword("alice", false); err != nil {
		t.Fatal(err)
	}
```

- TestAdminWriteEndpoints：`adminTok := login(t, srv, "op1", out["password"].(string))` 之前插入：

```go
	// op1 视为已自助改密（同上，gate 会拦截带标记会话的管理端点）
	if err := st.SetMustChangePassword("op1", false); err != nil {
		t.Fatal(err)
	}
```

（TestFullManagementFlow 的 bob、TestAdminWriteEndpoints 的 m1/m2 只调 login 公开端点、token 未用于受 gate 保护的端点，无需清。）

- [ ] **Step 4: 跑测试确认变绿**

Run: `go test ./internal/oksrv/ -v`
Expected: 全部 PASS（含两个新测试与全部存量测试）。

- [ ] **Step 5: Commit**

```bash
git add internal/oksrv/http.go internal/oksrv/auth.go internal/oksrv/http_test.go
git commit -m "feat(oksrv): change-password 自助改密端点 + gate 中间件强制改密拦截"
```

---

### Task 4: serverx 客户端 + GUI 转发层

**Files:**
- Modify: `internal/serverx/serverx.go`（MeInfo 字段 + ChangePassword 方法）
- Modify: `internal/gui/api_server.go`（注册 + fwdChangePassword）
- Test: `internal/serverx/serverx_test.go`、`internal/gui/api_server_test.go`

**Interfaces:**
- Consumes: Task 3 的 `POST /api/v1/change-password` 契约与 me 响应字段
- Produces:
  - `serverx.MeInfo.MustChangePassword bool`（json `must_change_password`）
  - `func (c *serverx.Client) ChangePassword(ctx context.Context, oldPassword, newPassword string) error`
  - 本地 GUI 端点 `POST /api/server/change-password`——请求 `{old_password, new_password}`；成功 204；服务端错误码与 message 原样透传（401/403/400/429）

- [ ] **Step 1: 写失败测试**

① `internal/serverx/serverx_test.go`：fakeOKServer 的 mux 加 handler（`GET /api/v1/me` handler 的响应 map 里同时加 `"must_change_password": true`）：

```go
	mux.HandleFunc("POST /api/v1/change-password", func(w http.ResponseWriter, r *http.Request) {
		if !authed(w, r) {
			return
		}
		var req struct {
			OldPassword string `json:"old_password"`
			NewPassword string `json:"new_password"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.OldPassword != "pw" {
			w.WriteHeader(401)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "旧密码错误"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	})
```

文件末尾追加：

```go
func TestChangePassword(t *testing.T) {
	srv := fakeOKServer(t)
	defer srv.Close()
	ctx := context.Background()
	c := New(srv.URL, "tok-1")

	// 旧密码错误 → *Error 401 透传
	err := c.ChangePassword(ctx, "bad", "newpass123")
	var se *Error
	if !errors.As(err, &se) || se.Code != 401 {
		t.Fatalf("wrong old: %v", err)
	}
	// 正确改密 → nil
	if err := c.ChangePassword(ctx, "pw", "newpass123"); err != nil {
		t.Fatalf("change: %v", err)
	}
	// me 解析 must_change_password
	me, err := c.Me(ctx)
	if err != nil || !me.MustChangePassword {
		t.Fatalf("me flag: %+v %v", me, err)
	}
}
```

② `internal/gui/api_server_test.go`：fakeOKServer 的 mux 加 handler：

```go
	mux.HandleFunc("POST /api/v1/change-password", func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer tok-") {
			w.WriteHeader(401)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "未认证"})
			return
		}
		var req struct {
			OldPassword string `json:"old_password"`
			NewPassword string `json:"new_password"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.NewPassword == "force403xx" { // 模拟服务端 gate 命中
			w.WriteHeader(403)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "must_change_password"})
			return
		}
		if req.OldPassword != "pw" {
			w.WriteHeader(401)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "旧密码错误"})
			return
		}
		w.WriteHeader(204)
	})
```

文件末尾追加：

```go
func TestApiServerChangePassword(t *testing.T) {
	h, _, _ := newEnv(t)
	fake := fakeOKServer(t, "http://gitea/alice/ok-x.git", "git-tok-1")
	defer fake.Close()
	srv := httptest.NewServer(h)
	defer srv.Close()

	// 登录落配置（change-password 走已存 token）
	res, body := do(t, "POST", srv.URL+"/api/server/login", testToken, map[string]any{"url": fake.URL, "username": "alice", "password": "pw"})
	if res != 200 {
		t.Fatalf("login: %d %s", res, body)
	}
	// 旧密码错误 → 401 透传
	res, _ = do(t, "POST", srv.URL+"/api/server/change-password", testToken, map[string]any{"old_password": "bad", "new_password": "newpass123"})
	if res != 401 {
		t.Fatalf("wrong old: %d", res)
	}
	// 403 must_change_password 原样透传（前端据此弹强制改密框）
	res, body = do(t, "POST", srv.URL+"/api/server/change-password", testToken, map[string]any{"old_password": "pw", "new_password": "force403xx"})
	if res != 403 || !strings.Contains(string(body), "must_change_password") {
		t.Fatalf("403 passthrough: %d %s", res, body)
	}
	// 成功 → 204
	res, _ = do(t, "POST", srv.URL+"/api/server/change-password", testToken, map[string]any{"old_password": "pw", "new_password": "newpass123"})
	if res != 204 {
		t.Fatalf("change: %d", res)
	}
}
```

- [ ] **Step 2: 跑测试确认变红**

Run: `go test ./internal/serverx/ ./internal/gui/ -run 'TestChangePassword|TestApiServerChangePassword' -v`
Expected: 编译失败（`c.ChangePassword undefined`、`me.MustChangePassword undefined`）或 404（`/api/server/change-password` 未注册）——红。

- [ ] **Step 3: 实现**

① `internal/serverx/serverx.go` MeInfo 加字段：

```go
type MeInfo struct {
	Name               string     `json:"name"`
	Role               string     `json:"role"`
	MustChangePassword bool       `json:"must_change_password"`
	Orgs               []string   `json:"orgs"`
	Repos              []RepoInfo `json:"repos"`
}
```

端点方法区追加：

```go
// ChangePassword 自助改密（旧密码校验在服务端；401 旧密码错误、
// 403 must_change_password 等以 *Error 透传状态码）。
func (c *Client) ChangePassword(ctx context.Context, oldPassword, newPassword string) error {
	return c.call(ctx, "POST", "/change-password", map[string]string{"old_password": oldPassword, "new_password": newPassword}, nil)
}
```

② `internal/gui/api_server.go` registerServerAPI 登录/登出组后加注册：

```go
	api("POST /api/server/change-password", h.fwdChangePassword)
```

文件末尾（`fwdUserResetPassword` 之后）加 handler：

```go
// fwdChangePassword 透传自助改密；成功 204；401 旧密码错误 / 403 must_change_password
// 等经 writeServerErr 原样透传状态码与 message（前端据 403 弹强制改密框）。
func (h *Handler) fwdChangePassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	c, _, err := h.serverClient()
	if err != nil || c == nil {
		writeNotConfigured(w, err)
		return
	}
	if err := c.ChangePassword(r.Context(), req.OldPassword, req.NewPassword); err != nil {
		writeServerErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
```

- [ ] **Step 4: 跑测试确认变绿**

Run: `go test ./internal/serverx/ ./internal/gui/ -v`
Expected: 全部 PASS。

- [ ] **Step 5: Commit**

```bash
git add internal/serverx/serverx.go internal/serverx/serverx_test.go internal/gui/api_server.go internal/gui/api_server_test.go
git commit -m "feat(serverx,gui): ChangePassword 客户端方法与 /api/server/change-password 转发"
```

---

### Task 5: GUI 前端——强制改密弹窗 + 自助改密入口 + 全局 403 钩子

**Files:**
- Modify: `web/app.js`（i18n 双语、api() 钩子、srvChangePwdModal/srvForcePwdModal、SRV 状态、loadServer/srvLogin 触发点、renderServer 入口按钮）

**Interfaces:**
- Consumes: Task 4 的 `POST /api/server/change-password`；`/api/server/me` 响应的 `must_change_password` 字段；`api()` 的错误对象（`err.status`、`err.message`）
- Produces:
  - `srvChangePwdModal(force bool)`：改密弹窗；force=true 不可取消（无取消按钮、点遮罩不关）
  - `srvForcePwdModal()`：全局钩子入口（已登录才弹、防重入）
  - i18n 键：srvChangePwd / srvOldPwd / srvConfirmPwd / srvForcePwdTitle / srvForcePwdHint / srvPwdMismatch / srvPwdSame / srvPwdChanged / srvChangePwdFail（复用既有 srvNewPwd / srvPwdTooShort / fCancel）

- [ ] **Step 1: i18n 双语键**

zh 块（`srvPwdTooShort` 行之后，`web/app.js:220` 附近）追加：

```js
    srvChangePwd:"修改我的密码", srvOldPwd:"旧密码", srvConfirmPwd:"确认新密码",
    srvForcePwdTitle:"必须设置新密码", srvForcePwdHint:"你正在使用初始密码（或密码刚被管理员重置），设置新密码后才能继续使用。",
    srvPwdMismatch:"两次输入的新密码不一致", srvPwdSame:"新密码不能与旧密码相同",
    srvPwdChanged:"密码已修改", srvChangePwdFail:"修改失败：",
```

en 块（`srvPwdTooShort` 行之后，`web/app.js:413` 附近）追加：

```js
    srvChangePwd:"Change my password", srvOldPwd:"Current password", srvConfirmPwd:"Confirm new password",
    srvForcePwdTitle:"New password required", srvForcePwdHint:"You are signing in with an initial password (or an admin just reset it) — set a new password to continue.",
    srvPwdMismatch:"The two new passwords do not match", srvPwdSame:"New password must differ from the current one",
    srvPwdChanged:"Password changed", srvChangePwdFail:"Change failed: ",
```

- [ ] **Step 2: api() 全局 403 钩子**

`web/app.js:37-39` 的错误构造段改为：

```js
    let err;
    if(res.status === 403 && data.error === "must_change_password"){
      // 强制改密全局钩子：任何页面收到 okserver 403 都弹不可取消改密框；
      // 错误文案换成可读提示（调用方 catch 里 toast 不再露出 must_change_password 原文）
      err = new Error(t("srvForcePwdTitle"));
      err.status = res.status;
      if(typeof srvForcePwdModal === "function") srvForcePwdModal();
    } else {
      err = new Error(data.error || ("请求失败: " + res.status));
      err.status = res.status;
    }
    throw err;
```

- [ ] **Step 3: 弹窗函数与状态**

① SRV 初始化（`web/app.js:5388`）加 `pwdModalOpen:false`：

```js
const SRV = { loaded:false, url:"", username:"", loggedIn:false, user:null, users:null, orgs:null, repos:null, audit:null, projects:null, wizardStep:null, bindBusy:{}, pwdModalOpen:false };
```

② 在 `srvShowSecret`（`web/app.js:5748-5768`）之后追加：

```js
/* 改密弹窗：force=true 为强制改密（无取消按钮、点遮罩不关）；
   防重入：强制框已开着时不再叠（全局 403 钩子可能并发触发多次）。 */
function srvChangePwdModal(force){
  if(force && SRV.pwdModalOpen) return;
  if(force) SRV.pwdModalOpen = true;
  const mask = el("div","mask");
  const m = el("div","modal");
  const h = el("h3"); h.textContent = force ? t("srvForcePwdTitle") : t("srvChangePwd"); m.appendChild(h);
  if(force){
    const d = el("div","small muted"); d.style.marginBottom = "8px"; d.textContent = t("srvForcePwdHint"); m.appendChild(d);
  }
  const oldIn = el("input","pinput"); oldIn.type = "password"; oldIn.placeholder = t("srvOldPwd");
  const newIn = el("input","pinput"); newIn.type = "password"; newIn.placeholder = t("srvNewPwd");
  const cfIn = el("input","pinput"); cfIn.type = "password"; cfIn.placeholder = t("srvConfirmPwd");
  [oldIn, newIn, cfIn].forEach(i=>{ i.style.width = "100%"; i.style.boxSizing = "border-box"; i.style.marginBottom = "8px"; m.appendChild(i); });
  const close = ()=>{ SRV.pwdModalOpen = false; mask.remove(); };
  const foot = el("div","mfoot2");
  const ok = el("button","btn btn-primary"); ok.textContent = t("srvChangePwd");
  ok.onclick = async ()=>{
    if(newIn.value.length < 8){ toast(t("srvPwdTooShort"), true); return; }
    if(newIn.value !== cfIn.value){ toast(t("srvPwdMismatch"), true); return; }
    if(newIn.value === oldIn.value){ toast(t("srvPwdSame"), true); return; }
    ok.disabled = true;
    try{
      await api("/api/server/change-password", { method:"POST", body:{ old_password: oldIn.value, new_password: newIn.value }, skip401Reload:true });
      if(SRV.user) SRV.user.must_change_password = false;
      close();
      toast(t("srvPwdChanged"));
      if(state.menu === "server") render();
    }catch(err){
      toast(t("srvChangePwdFail")+(err.message||""), true);
    }
    ok.disabled = false;
  };
  foot.appendChild(ok);
  if(!force){
    const cancel = el("button","btn"); cancel.textContent = t("fCancel");
    cancel.onclick = close; foot.appendChild(cancel);
    mask.onclick = e=>{ if(e.target === mask) close(); };
  }
  m.appendChild(foot);
  mask.appendChild(m);
  document.body.appendChild(mask);
}

/* 强制改密入口（api() 全局钩子与各触发点共用）：已登录才弹 */
function srvForcePwdModal(){
  if(!SRV.user) return;
  srvChangePwdModal(true);
}
```

- [ ] **Step 4: 三个触发点 + 入口按钮**

① loadServer 的 me 拉取（`web/app.js:5404`）改为：

```js
      return api("/api/server/me", { skip401Reload:true }).then(me=>{
        SRV.user = me;
        if(me.must_change_password) srvForcePwdModal();
      }).catch(()=>{ SRV.loggedIn = false; SRV.user = null; });
```

② srvLogin 成功路径（`web/app.js:5591-5592`）在 `toast(t("srvLoginOk")...)` 之前插入：

```js
    if(SRV.user && SRV.user.must_change_password){ srvForcePwdModal(); }
```

③ renderServer 顶行（`web/app.js:5464-5466`）加"修改我的密码"按钮（管理员与成员视图共用此顶行）：

```js
  if(SRV.user){
    const cp = el("button","btn"); cp.textContent = t("srvChangePwd");
    cp.onclick = ()=>srvChangePwdModal(false); head.appendChild(cp);
    const lo = el("button","btn"); lo.textContent = t("srvLogout"); lo.style.marginLeft="auto";
    lo.onclick = srvLogout; head.appendChild(lo);
  }
```

- [ ] **Step 5: 编译检查 + 手动验证**

前端无测试框架，做编译检查 + 真机走查：

```bash
go build ./...
```

手动验证（仓库根目录跑 `go run`，webdir.Find 走 `<cwd>/web` 直接用源码页，绕开 dist/web 同步坑）：

```bash
# 终端 1：起临时 okserver（fake git 后端）；数据目录用 $HOME 下路径，
# Git Bash 与 Go 进程读同一 env 字符串，避免 /tmp 在两边解析不一致
OKSERVER_DATA_DIR="$HOME/oksrv-pwtest" go run ./cmd/okserver
# 取 root 初始密码
cat "$HOME/oksrv-pwtest/INITIAL_ROOT_PASSWORD"

# 终端 2：起 GUI
go run ./cmd/ok gui
```

走查清单：
1. GUI 服务器页用 root + 初始密码登录 → 应立即弹不可取消的"必须设置新密码"框（无取消按钮、点遮罩不关）。
2. 旧密码故意输错 → toast"修改失败：旧密码错误"；新密码 <8 位 / 两次不一致 / 新旧相同 → 对应本地校验 toast。
3. 正确改密 → toast"密码已修改"，框关闭，管理页正常加载（用户/组织/审计都能拉取）。
4. 顶行点"修改我的密码"→ 可取消的改密框；改一次成功后旧密码登录失败（可在登录页验证）。
5. 管理页建一个新用户 → 用其初始密码在另一个浏览器/无痕窗口登录 → 同样弹强制改密框。
6. root 重置该用户密码 → 该用户旧会话再操作任意功能 → 弹强制改密框（全局钩子），用新初始密码改密后恢复。

验证完清理临时数据目录：`rm -rf "$HOME/oksrv-pwtest"`。

- [ ] **Step 6: Commit**

```bash
git add web/app.js
git commit -m "feat(web): 服务器页强制改密弹窗 + 修改我的密码入口 + 全局 403 must_change_password 钩子"
```

---

## 收尾

- [ ] 全量回归：`go test ./... && go vet ./...`
- [ ] 更新规格文档状态行为"已实施"（`docs/superpowers/specs/2026-08-31-password-change-design.md` 首部）
