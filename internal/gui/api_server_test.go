package gui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"openknowledge/internal/config"
	"openknowledge/internal/syncx"
)

// fakeOKServer 起假 okserver（meta/login/me/repos/personal，校验 Bearer）。
// gitToken 传空串模拟"仓已存在"语义（token 仅建仓首发）。
func fakeOKServer(t *testing.T, cloneURL, gitToken string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/meta", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"version": "v1", "initialized": true, "git_backend": map[string]any{"type": "fake", "ok": true}})
	})
	mux.HandleFunc("POST /api/v1/login", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Username, Password string
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Password != "pw" {
			w.WriteHeader(401)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "未认证"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"token": "tok-" + req.Username, "user": map[string]string{"name": req.Username, "role": "member"}})
	})
	mux.HandleFunc("GET /api/v1/me", func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer tok-") {
			w.WriteHeader(401)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "未认证"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"name": "alice", "role": "member", "orgs": []string{}, "repos": []any{}})
	})
	mux.HandleFunc("POST /api/v1/repos/personal", func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer tok-") {
			w.WriteHeader(401)
			return
		}
		var req struct {
			Project string `json:"project"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"repo":      map[string]string{"layer": "personal", "owner": "alice", "project": req.Project, "name": "ok-" + req.Project, "clone_url": cloneURL},
			"git_token": gitToken,
		})
	})
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
	return httptest.NewServer(mux)
}

func TestApiServerConfigAndLogin(t *testing.T) {
	h, _, okHome := newEnv(t)
	fake := fakeOKServer(t, "http://gitea/alice/ok-x.git", "git-tok-1")
	defer fake.Close()
	srv := httptest.NewServer(h)
	defer srv.Close()

	// 未配置：GET config 返回空（响应不含 "token" 字段——只有 has_token 布尔）
	res, body := do(t, "GET", srv.URL+"/api/server/config", testToken, nil)
	if res != 200 || strings.Contains(string(body), `"token"`) {
		t.Fatalf("initial config: %d %s", res, body)
	}
	// test 连接
	res, body = do(t, "POST", srv.URL+"/api/server/test", testToken, map[string]any{"url": fake.URL})
	if res != 200 || !strings.Contains(string(body), `"ok":true`) {
		t.Fatalf("test: %d %s", res, body)
	}
	// test 不可达地址
	res, body = do(t, "POST", srv.URL+"/api/server/test", testToken, map[string]any{"url": "http://127.0.0.1:1"})
	if res != 200 || !strings.Contains(string(body), `"ok":false`) {
		t.Fatalf("test down: %d %s", res, body)
	}
	// 登录（成功 → 配置落盘且 token 不回显）
	res, body = do(t, "POST", srv.URL+"/api/server/login", testToken, map[string]any{"url": fake.URL, "username": "alice", "password": "pw"})
	if res != 200 || !strings.Contains(string(body), "alice") {
		t.Fatalf("login: %d %s", res, body)
	}
	// 全局配置已落盘且含 token
	cfgData, err := os.ReadFile(filepath.Join(okHome, "config.toml"))
	if err != nil || !strings.Contains(string(cfgData), "[server]") || !strings.Contains(string(cfgData), "tok-alice") {
		t.Fatalf("global config: %v\n%s", err, cfgData)
	}
	// GET config 不回传 token 本体
	res, body = do(t, "GET", srv.URL+"/api/server/config", testToken, nil)
	if res != 200 || strings.Contains(string(body), "tok-alice") || !strings.Contains(string(body), `"has_token":true`) {
		t.Fatalf("config after login: %d %s", res, body)
	}
	// me 转发
	res, body = do(t, "GET", srv.URL+"/api/server/me", testToken, nil)
	if res != 200 || !strings.Contains(string(body), "alice") {
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

// TestApiServerReposFullFlow 建仓一条龙全链路：假 okserver（provision 返回 file:// 裸仓地址）
// → POST /api/server/repos → 断言：项目仓已 init + 内容已推到 bare + 项目 [sync] 已落盘。
func TestApiServerReposFullFlow(t *testing.T) {
	h, _, okHome := newEnv(t)
	// 隔离全局/系统 git 配置：防 StoreCredential 往真实 credential store 写假凭据
	// （无 helper → 走 URL 内嵌回退，file:// 无 host 时 URL 原样）
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))
	t.Setenv("GIT_CONFIG_SYSTEM", filepath.Join(t.TempDir(), "gitconfig-sys"))
	// "Gitea 侧"：file:// 裸仓
	bare := t.TempDir()
	gitRun(t, bare, "init", "--bare", "-b", "main")
	bareURL := "file://" + filepath.ToSlash(bare)
	fake := fakeOKServer(t, bareURL, "git-tok-1")
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

// TestApiServerReposNoTokenNoHelper 仓已存在（provision 返回空 git_token）且本机无
// credential helper——不得闷头推进：返回 200 status=error + 出路提示，且项目仓未被 init。
func TestApiServerReposNoTokenNoHelper(t *testing.T) {
	h, _, okHome := newEnv(t)
	// 隔离全局/系统 git 配置 → 无 credential helper
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))
	t.Setenv("GIT_CONFIG_SYSTEM", filepath.Join(t.TempDir(), "gitconfig-sys"))
	fake := fakeOKServer(t, "http://gitea/alice/ok-x.git", "") // 空 git_token = 仓已存在语义
	defer fake.Close()
	srv := httptest.NewServer(h)
	defer srv.Close()

	res, body := do(t, "POST", srv.URL+"/api/server/login", testToken, map[string]any{"url": fake.URL, "username": "alice", "password": "pw"})
	if res != 200 {
		t.Fatalf("login: %d %s", res, body)
	}
	name := "bindemo2"
	projDir := filepath.Join(t.TempDir(), "work")
	mkProjectAt(t, okHome, name, projDir)
	st := stFor(t, okHome, name)

	res, body = do(t, "POST", srv.URL+"/api/server/repos", testToken, map[string]any{"project": name})
	if res != 200 {
		t.Fatalf("repos: %d %s", res, body)
	}
	var out struct {
		Status  string `json:"status"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatal(err)
	}
	if out.Status != "error" || !strings.Contains(out.Message, "仓已存在但本机无 git 凭据") {
		t.Fatalf("out: %s", body)
	}
	if syncx.Open(st.Root).IsRepo() {
		t.Fatal("project must NOT be init'ed when credential path is impossible")
	}
}

// TestFwdOrgMemberAdd 成员添加透传薄测试：POST /api/server/orgs/{org}/members
// （裸路径无尾段——前端曾拼尾斜杠 404）应到达 okserver 的 /orgs/{org}/members。
func TestFwdOrgMemberAdd(t *testing.T) {
	h, _, _ := newEnv(t)
	var gotOrg, gotUser, gotRole string
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/orgs/{org}/members", func(w http.ResponseWriter, r *http.Request) {
		gotOrg = r.PathValue("org")
		var req struct {
			Username string `json:"username"`
			Role     string `json:"role"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		gotUser, gotRole = req.Username, req.Role
		w.WriteHeader(http.StatusNoContent)
	})
	fake := httptest.NewServer(mux)
	defer fake.Close()
	if err := config.SetServer(globalConfigPath(), config.Server{URL: fake.URL, Username: "root", Token: "tok-root"}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h)
	defer srv.Close()

	res, body := do(t, "POST", srv.URL+"/api/server/orgs/core/members", testToken, map[string]any{"username": "alice", "role": "member"})
	if res != 204 {
		t.Fatalf("member add: %d %s", res, body)
	}
	if gotOrg != "core" || gotUser != "alice" || gotRole != "member" {
		t.Fatalf("forwarded: org=%q user=%q role=%q", gotOrg, gotUser, gotRole)
	}
}

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
