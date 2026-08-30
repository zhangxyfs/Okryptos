package gui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"openknowledge/internal/syncx"
)

// fakeOKServer 起假 okserver（meta/login/me/repos/personal，校验 Bearer）。
func fakeOKServer(t *testing.T, cloneURL string) *httptest.Server {
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
