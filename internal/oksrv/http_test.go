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
