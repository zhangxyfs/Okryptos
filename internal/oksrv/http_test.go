package oksrv

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newTestServer 起完整 mux + fake backend。
func newTestServer(t *testing.T) (*httptest.Server, *Store, *FakeBackend) {
	t.Helper()
	s, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	pw, created, err := s.EnsureRoot()
	if err != nil || !created {
		t.Fatalf("root: %v", err)
	}
	// 存量管理流测试默认 root 已自助改密（否则 gate 拦截全部管理端点）；
	// 强制改密行为本身由 TestForcePasswordChange* / TestUserCreateAndResetSetFlag 覆盖。
	if err := s.SetMustChangePassword("root", false); err != nil {
		t.Fatalf("clear root flag: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	testEnv["OK_TEST_ROOT_PW"] = pw // 测试内取用（包级 map，非真 env）
	b := NewFakeBackend()
	// root 同步进 fake 后端（镜像"git 侧已有该用户"的前提；CreateUserToken 对未知用户拒绝）。
	if err := b.CreateUser(context.Background(), "root", pw); err != nil {
		t.Fatalf("fake root: %v", err)
	}
	return httptest.NewServer(NewMux(s, b, "test-version")), s, b
}

// call 带鉴权打请求。
func call(t *testing.T, srv *httptest.Server, method, path, token string, body any) (int, map[string]any) {
	t.Helper()
	var rd *strings.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = strings.NewReader(string(b))
	} else {
		rd = strings.NewReader("")
	}
	req, _ := http.NewRequest(method, srv.URL+path, rd)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(res.Body).Decode(&out)
	return res.StatusCode, out
}

func login(t *testing.T, srv *httptest.Server, username, pw string) string {
	t.Helper()
	code, out := call(t, srv, "POST", "/api/v1/login", "", map[string]string{"username": username, "password": pw})
	if code != 200 {
		t.Fatalf("login: %d %v", code, out)
	}
	return out["token"].(string)
}

func TestMetaAndLogin(t *testing.T) {
	srv, _, _ := newTestServer(t)
	defer srv.Close()

	code, out := call(t, srv, "GET", "/api/v1/meta", "", nil)
	if code != 200 || out["initialized"] != true {
		t.Fatalf("meta: %d %v", code, out)
	}
	gb := out["git_backend"].(map[string]any)
	if gb["type"] != "fake" || gb["ok"] != true {
		t.Fatalf("git_backend: %v", gb)
	}
	// 错误密码 401
	code, _ = call(t, srv, "POST", "/api/v1/login", "", map[string]string{"username": "root", "password": "bad"})
	if code != 401 {
		t.Fatalf("bad login: %d", code)
	}
	// 限流：5 次失败后 429
	for i := 0; i < 5; i++ {
		call(t, srv, "POST", "/api/v1/login", "", map[string]string{"username": "root", "password": "bad"})
	}
	code, _ = call(t, srv, "POST", "/api/v1/login", "", map[string]string{"username": "root", "password": "bad"})
	if code != 429 {
		t.Fatalf("rate limited: %d", code)
	}
	// 无 token 401
	code, _ = call(t, srv, "GET", "/api/v1/me", "", nil)
	if code != 401 {
		t.Fatalf("no token: %d", code)
	}
}

func TestFullManagementFlow(t *testing.T) {
	srv, st, _ := newTestServer(t)
	defer srv.Close()
	rootTok := login(t, srv, "root", getenv(t, "OK_TEST_ROOT_PW"))

	// 建用户（一次性返回明文密码 + git token）
	code, out := call(t, srv, "POST", "/api/v1/users", rootTok, map[string]string{"username": "alice"})
	if code != 200 {
		t.Fatalf("create user: %d %v", code, out)
	}
	pw := out["password"].(string)
	if pw == "" || out["git_token"].(string) == "" {
		t.Fatalf("create user resp: %v", out)
	}
	// alice 视为已自助改密（初始密码标记会让 gate 拦截后续建仓端点）
	if err := st.SetMustChangePassword("alice", false); err != nil {
		t.Fatal(err)
	}
	// 成员登录
	aliceTok := login(t, srv, "alice", pw)
	// 成员建仓（幂等）
	code, out = call(t, srv, "POST", "/api/v1/repos/personal", aliceTok, map[string]string{"project": "demo"})
	if code != 200 {
		t.Fatalf("personal repo: %d %v", code, out)
	}
	repo := out["repo"].(map[string]any)
	if repo["owner"] != "alice" || repo["name"] != "ok-demo" || out["git_token"] == "" {
		t.Fatalf("repo: %v token=%v", repo, out["git_token"])
	}
	// 幂等：再来一次，git_token 为空串（不重复下发）
	code, out = call(t, srv, "POST", "/api/v1/repos/personal", aliceTok, map[string]string{"project": "demo"})
	if code != 200 || out["git_token"] != "" {
		t.Fatalf("idempotent: %d %v", code, out)
	}
	// me
	code, out = call(t, srv, "GET", "/api/v1/me", aliceTok, nil)
	if code != 200 || len(out["repos"].([]any)) != 1 {
		t.Fatalf("me: %d %v", code, out)
	}
	// 成员访问管理端点 403
	code, _ = call(t, srv, "GET", "/api/v1/users", aliceTok, nil)
	if code != 403 {
		t.Fatalf("member forbidden: %d", code)
	}
	// 建组织 + 成员 + 团队仓
	code, _ = call(t, srv, "POST", "/api/v1/orgs", rootTok, map[string]string{"name": "acme", "description": "Acme"})
	if code != 201 {
		t.Fatalf("org: %d", code)
	}
	code, _ = call(t, srv, "POST", "/api/v1/orgs/acme/members", rootTok, map[string]string{"username": "alice", "role": "member"})
	if code != 204 {
		t.Fatalf("member: %d", code)
	}
	code, out = call(t, srv, "POST", "/api/v1/repos/team", rootTok, map[string]string{"org": "acme", "project": "demo"})
	if code != 200 || out["repo"].(map[string]any)["owner"] != "ok-acme" {
		t.Fatalf("team repo: %d %v", code, out)
	}
	// alice 的 me 现在有组织
	code, out = call(t, srv, "GET", "/api/v1/me", aliceTok, nil)
	if orgs := out["orgs"].([]any); len(orgs) != 1 {
		t.Fatalf("me orgs: %v", orgs)
	}
	// root 保护：admin 不能动 root
	code, out = call(t, srv, "POST", "/api/v1/users", rootTok, map[string]string{"username": "bob"})
	if code != 200 {
		t.Fatalf("create bob: %d", code)
	}
	bobTok := login(t, srv, "bob", out["password"].(string))
	// bob 是 member，升 admin 需要……v1 没有改角色端点（从简），跳过
	_ = bobTok
	code, _ = call(t, srv, "POST", "/api/v1/users/root/disable", rootTok, nil)
	if code != 403 && code != 400 {
		t.Fatalf("disable root must be refused: %d", code)
	}
	// 审计
	code, out = call(t, srv, "GET", "/api/v1/audit", rootTok, nil)
	if code != 200 {
		t.Fatalf("audit: %d", code)
	}
	entries := out["entries"].([]any)
	if len(entries) < 4 { // 建用户×2 + 建仓 + 建组织 + 加成员 + 团队仓
		t.Fatalf("audit entries: %d", len(entries))
	}
}

// TestAdminWriteEndpoints 覆盖管理写端点：admin 越权拒止、disable/enable 联动登录、
// 组织成员移除、审计动作落库（reset-password / disable-user / enable-user / remove-org-member）。
func TestAdminWriteEndpoints(t *testing.T) {
	srv, st, _ := newTestServer(t)
	defer srv.Close()
	rootTok := login(t, srv, "root", getenv(t, "OK_TEST_ROOT_PW"))

	// root 建 admin
	code, out := call(t, srv, "POST", "/api/v1/users", rootTok, map[string]string{"username": "op1", "role": "admin"})
	if code != 200 {
		t.Fatalf("create admin: %d %v", code, out)
	}
	// op1 视为已自助改密（同上，gate 会拦截带标记会话的管理端点）
	if err := st.SetMustChangePassword("op1", false); err != nil {
		t.Fatal(err)
	}
	adminTok := login(t, srv, "op1", out["password"].(string))

	// admin reset root 密码必须 403（root 保护）
	code, _ = call(t, srv, "POST", "/api/v1/users/root/reset-password", adminTok, nil)
	if code != 403 {
		t.Fatalf("admin reset root must be 403: %d", code)
	}

	// root 建 member
	code, out = call(t, srv, "POST", "/api/v1/users", rootTok, map[string]string{"username": "m1"})
	if code != 200 {
		t.Fatalf("create m1: %d %v", code, out)
	}
	m1pw := out["password"].(string)
	login(t, srv, "m1", m1pw)

	// admin disable m1 → 204；m1 登录 401
	code, _ = call(t, srv, "POST", "/api/v1/users/m1/disable", adminTok, nil)
	if code != 204 {
		t.Fatalf("disable m1: %d", code)
	}
	code, _ = call(t, srv, "POST", "/api/v1/login", "", map[string]string{"username": "m1", "password": m1pw})
	if code != 401 {
		t.Fatalf("disabled login must be 401: %d", code)
	}
	// admin enable m1 → 204；恢复可登录
	code, _ = call(t, srv, "POST", "/api/v1/users/m1/enable", adminTok, nil)
	if code != 204 {
		t.Fatalf("enable m1: %d", code)
	}
	login(t, srv, "m1", m1pw)

	// root reset m1 密码 → 200，新密码可登录（产出 reset-password 审计）
	code, out = call(t, srv, "POST", "/api/v1/users/m1/reset-password", rootTok, nil)
	if code != 200 || out["password"].(string) == "" {
		t.Fatalf("reset m1: %d %v", code, out)
	}
	login(t, srv, "m1", out["password"].(string))

	// root 移除组织成员 → 204，且 ListOrgMembers 不再含该成员
	code, _ = call(t, srv, "POST", "/api/v1/orgs", rootTok, map[string]string{"name": "acme2"})
	if code != 201 {
		t.Fatalf("create org: %d", code)
	}
	code, _ = call(t, srv, "POST", "/api/v1/orgs/acme2/members", rootTok, map[string]string{"username": "m1"})
	if code != 204 {
		t.Fatalf("add member: %d", code)
	}
	code, _ = call(t, srv, "DELETE", "/api/v1/orgs/acme2/members/m1", rootTok, nil)
	if code != 204 {
		t.Fatalf("remove member: %d", code)
	}
	for _, m := range st.ListOrgMembers("acme2") {
		if m.Username == "m1" {
			t.Fatalf("m1 must be removed from acme2: %+v", m)
		}
	}

	// 审计含 reset/disable/enable/remove 动作
	code, out = call(t, srv, "GET", "/api/v1/audit?limit=100", rootTok, nil)
	if code != 200 {
		t.Fatalf("audit: %d", code)
	}
	actions := map[string]bool{}
	for _, e := range out["entries"].([]any) {
		actions[e.(map[string]any)["action"].(string)] = true
	}
	for _, want := range []string{"reset-password", "disable-user", "enable-user", "remove-org-member"} {
		if !actions[want] {
			t.Fatalf("audit missing %s: %v", want, actions)
		}
	}

	// 自选密码建用户：短密码 400；合法密码可直接登录
	code, _ = call(t, srv, "POST", "/api/v1/users", rootTok, map[string]string{"username": "m2", "password": "short"})
	if code != 400 {
		t.Fatalf("short password must be 400: %d", code)
	}
	code, _ = call(t, srv, "POST", "/api/v1/users", rootTok, map[string]string{"username": "m2", "password": "mypass123"})
	if code != 200 {
		t.Fatalf("create m2 with custom password: %d", code)
	}
	login(t, srv, "m2", "mypass123")

	// 删除用户：admin 删 root → 403；admin 删 admin → 403；root 删 member → 204，
	// 删后登录 401、列表不再出现、审计落 delete-user
	code, _ = call(t, srv, "DELETE", "/api/v1/users/root", adminTok, nil)
	if code != 403 {
		t.Fatalf("admin delete root must be 403: %d", code)
	}
	code, _ = call(t, srv, "DELETE", "/api/v1/users/op1", adminTok, nil)
	if code != 403 {
		t.Fatalf("admin delete admin must be 403: %d", code)
	}
	code, _ = call(t, srv, "DELETE", "/api/v1/users/m1", rootTok, nil)
	if code != 204 {
		t.Fatalf("delete m1: %d", code)
	}
	code, _ = call(t, srv, "POST", "/api/v1/login", "", map[string]string{"username": "m1", "password": m1pw})
	if code != 401 {
		t.Fatalf("deleted login must be 401: %d", code)
	}
	code, out = call(t, srv, "GET", "/api/v1/users", rootTok, nil)
	if code != 200 {
		t.Fatalf("users: %d", code)
	}
	for _, uu := range out["users"].([]any) {
		if uu.(map[string]any)["name"] == "m1" {
			t.Fatal("m1 must be gone from user list")
		}
	}
	code, out = call(t, srv, "GET", "/api/v1/audit?limit=100", rootTok, nil)
	if code != 200 {
		t.Fatalf("audit: %d", code)
	}
	found := false
	for _, e := range out["entries"].([]any) {
		if e.(map[string]any)["action"] == "delete-user" {
			found = true
		}
	}
	if !found {
		t.Fatal("audit missing delete-user")
	}
}

// TestCreateUserTokenFailureRollback 钉住 2026-08-30 真机 bug：Gitea 发 token 失败
// （1.22 缺 scope 400）时建用户流程必须回滚 Gitea 侧用户、不留本地半截状态，重试可收敛。
func TestCreateUserTokenFailureRollback(t *testing.T) {
	srv, st, backend := newTestServer(t)
	defer srv.Close()
	rootTok := login(t, srv, "root", getenv(t, "OK_TEST_ROOT_PW"))

	backend.SetFailTokens(true)
	code, out := call(t, srv, "POST", "/api/v1/users", rootTok, map[string]string{"username": "carol"})
	if code != http.StatusBadGateway {
		t.Fatalf("token failure must be 502: %d %v", code, out)
	}
	// 本地不留半截（否则重试被"用户已存在"闸门挡住——真机第二次报错）
	if st.GetUser("carol") != nil {
		t.Fatal("local user must not exist after rollback")
	}
	// Gitea 侧用户已回滚删除：直接再调 CreateUser 应成功（没回滚会报已存在）
	if err := backend.CreateUser(context.Background(), "carol", "pw"); err != nil {
		t.Fatalf("backend user must be rolled back: %v", err)
	}
	if err := backend.DeleteUser(context.Background(), "carol"); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	// 修复后重试全链路成功
	backend.SetFailTokens(false)
	code, out = call(t, srv, "POST", "/api/v1/users", rootTok, map[string]string{"username": "carol"})
	if code != 200 || out["git_token"].(string) == "" {
		t.Fatalf("retry after fix must succeed: %d %v", code, out)
	}
}

func getenv(t *testing.T, key string) string {
	t.Helper()
	v := ""
	if env := strings.TrimSpace(getEnvForTest(key)); env != "" {
		v = env
	}
	return v
}

// getEnvForTest 隔离点（os.Getenv 包装，便于审计）。
func getEnvForTest(key string) string { return testEnv[key] }

var testEnv = map[string]string{}

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

// TestApiGitToken 自助重发：未认证 401；按机器分名（name_hint → ok-sync-r-<hint>，
// token_name 回显、审计 detail 含该名）；同名重复调仍 200（删本机同名再建）；
// 空 body 容忍落 unknown；带强制改密标记的账号被 gate 拦 403。
func TestApiGitToken(t *testing.T) {
	srv, st, _ := newTestServer(t)
	// 未认证
	code, _ := call(t, srv, "POST", "/api/v1/git-token", "", nil)
	if code != 401 {
		t.Fatalf("unauth: %d", code)
	}
	// 认证（root 已在 newTestServer 里清掉强制改密标记；初始密码经包级 testEnv 取用）
	tok := login(t, srv, "root", getenv(t, "OK_TEST_ROOT_PW"))
	// 带 hint：token 按机器分名并回显
	code, body := call(t, srv, "POST", "/api/v1/git-token", tok, map[string]string{"name_hint": "DESKTOP-1"})
	if code != 200 || body["git_token"] == "" || body["token_name"] != "ok-sync-r-DESKTOP-1" {
		t.Fatalf("reissue with hint: %d %v", code, body)
	}
	first := body["git_token"].(string)
	// 同名重复调（同 hint）：删本机同名再建，不撞名不失败
	code, body = call(t, srv, "POST", "/api/v1/git-token", tok, map[string]string{"name_hint": "DESKTOP-1"})
	if code != 200 || body["git_token"] == "" || body["git_token"] == first || body["token_name"] != "ok-sync-r-DESKTOP-1" {
		t.Fatalf("re-reissue: %d %v", code, body)
	}
	// 空 body 容忍：hint 缺省落 unknown
	code, body = call(t, srv, "POST", "/api/v1/git-token", tok, nil)
	if code != 200 || body["token_name"] != "ok-sync-r-unknown" {
		t.Fatalf("empty body: %d %v", code, body)
	}
	// 审计落库（detail 含 token 名）
	found := false
	for _, a := range st.ListAudit(10, 0) {
		if a.Action == "reissue-git-token" && strings.Contains(a.Detail, "ok-sync-r-DESKTOP-1") {
			found = true
		}
	}
	if !found {
		t.Fatal("audit missing reissue-git-token with token name")
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
