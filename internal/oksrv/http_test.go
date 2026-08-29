package oksrv

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newTestServer 起完整 mux + fake backend。
func newTestServer(t *testing.T) (*httptest.Server, *Store) {
	t.Helper()
	s, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	pw, created, err := s.EnsureRoot()
	if err != nil || !created {
		t.Fatalf("root: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	testEnv["OK_TEST_ROOT_PW"] = pw // 测试内取用（包级 map，非真 env）
	return httptest.NewServer(NewMux(s, NewFakeBackend(), "test-version")), s
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
	srv, _ := newTestServer(t)
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
	srv, _ := newTestServer(t)
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
	srv, st := newTestServer(t)
	defer srv.Close()
	rootTok := login(t, srv, "root", getenv(t, "OK_TEST_ROOT_PW"))

	// root 建 admin
	code, out := call(t, srv, "POST", "/api/v1/users", rootTok, map[string]string{"username": "op1", "role": "admin"})
	if code != 200 {
		t.Fatalf("create admin: %d %v", code, out)
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
