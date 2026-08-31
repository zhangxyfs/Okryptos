package oksrv

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// fakeGitea 镜像核实后的 Gitea API 形状（内存态）。
// 核实来源：docs.gitea.com swagger-22.json / swagger-latest.json + go-gitea/gitea 源码（v1.22.0、main）。
// 与 Task 4 简报表格的偏差（按真实形状调整）：
//   - 发 token：真实端点是 POST /users/{username}/tokens 且强制 Basic auth（reqBasicOrRevProxyAuth），
//     admin API token 直接调会 401——这里对非 Basic 请求返回 401 钉死该语义；
//   - 组织加成员：真实无 /orgs/{org}/membership/{username}，须 GET /orgs/{org}/teams 拿
//     建组织时自动创建的 Owners 队 id，再 PUT /teams/{id}/members/{username}；
//   - 组织减成员：真实路径是 DELETE /orgs/{org}/members/{username}；
//   - PATCH /admin/users/{username} 真实返回 200 + User（非 204）。
func fakeGitea(t *testing.T) *httptest.Server {
	t.Helper()
	type user struct{ active bool }
	users := map[string]*user{}
	userTokens := map[string]map[string]bool{} // username → token 名集合
	repos := map[string]bool{}
	orgs := map[string]map[string]bool{}
	orgTeam := map[string]int64{}  // org → Owners 队 id（真实 Gitea 建组织自动创建）
	teamOrg := map[int64]string{}  // team id → org
	var nextTeamID int64
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/version", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"version": "1.22.0"})
	})
	mux.HandleFunc("POST /api/v1/admin/users", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Username string `json:"username"`
			Email    string `json:"email"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Username == "" || req.Email == "" {
			w.WriteHeader(422) // 真实 Gitea：username/email 必填
			return
		}
		if users[req.Username] != nil {
			w.WriteHeader(422)
			return
		}
		users[req.Username] = &user{active: true}
		w.WriteHeader(201)
		json.NewEncoder(w).Encode(map[string]any{"username": req.Username})
	})
	mux.HandleFunc("POST /api/v1/users/{username}/tokens", func(w http.ResponseWriter, r *http.Request) {
		if _, _, ok := r.BasicAuth(); !ok {
			w.WriteHeader(401) // 真实 Gitea：reqBasicOrRevProxyAuth
			return
		}
		var req struct {
			Name   string   `json:"name"`
			Scopes []string `json:"scopes"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if len(req.Scopes) == 0 {
			// 真实 Gitea 1.22+：缺省空 scope 直接 400 "access token must have a scope"
			w.WriteHeader(400)
			json.NewEncoder(w).Encode(map[string]string{"message": "access token must have a scope"})
			return
		}
		u := r.PathValue("username")
		if users[u] == nil {
			w.WriteHeader(404)
			return
		}
		if userTokens[u] == nil {
			userTokens[u] = map[string]bool{}
		}
		userTokens[u][req.Name] = true
		w.WriteHeader(201)
		json.NewEncoder(w).Encode(map[string]string{"sha1": "tok-" + u})
	})
	mux.HandleFunc("DELETE /api/v1/users/{username}/tokens/{token}", func(w http.ResponseWriter, r *http.Request) {
		if _, _, ok := r.BasicAuth(); !ok {
			w.WriteHeader(401) // 同 POST：真实 Gitea 要求 Basic auth
			return
		}
		u, name := r.PathValue("username"), r.PathValue("token")
		if !userTokens[u][name] {
			w.WriteHeader(404) // 真实 Gitea：token 不存在 → 404
			return
		}
		delete(userTokens[u], name)
		w.WriteHeader(204)
	})
	mux.HandleFunc("GET /api/v1/users/{username}/tokens", func(w http.ResponseWriter, r *http.Request) {
		if _, _, ok := r.BasicAuth(); !ok {
			w.WriteHeader(401) // 同 POST：真实 Gitea 要求 Basic auth
			return
		}
		// 真实 Gitea 1.24+ 形状：created_at / last_used_at；
		// 名带 "legacy-" 前缀的模拟 ≤1.23（响应里完全无时间字段）。
		out := []map[string]any{}
		for name := range userTokens[r.PathValue("username")] {
			e := map[string]any{"name": name}
			if !strings.HasPrefix(name, "legacy-") {
				e["created_at"] = "2026-08-30T10:00:00Z"
				e["last_used_at"] = "2026-08-31T12:34:56Z"
			}
			out = append(out, e)
		}
		json.NewEncoder(w).Encode(out)
	})
	mux.HandleFunc("DELETE /api/v1/admin/users/{username}", func(w http.ResponseWriter, r *http.Request) {
		u := r.PathValue("username")
		if users[u] == nil {
			w.WriteHeader(404)
			return
		}
		delete(users, u)
		w.WriteHeader(204)
	})
	mux.HandleFunc("PATCH /api/v1/admin/users/{username}", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Active   bool   `json:"active"`
			SourceID int64  `json:"source_id"`
			Login    string `json:"login_name"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		u := r.PathValue("username")
		if users[u] == nil {
			w.WriteHeader(404)
			return
		}
		users[u].active = req.Active
		json.NewEncoder(w).Encode(map[string]any{"username": u, "active": req.Active})
	})
	mux.HandleFunc("POST /api/v1/admin/users/{username}/repos", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Name    string `json:"name"`
			Private bool   `json:"private"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		owner := r.PathValue("username")
		key := owner + "/" + req.Name
		if repos[key] {
			w.WriteHeader(409)
			return
		}
		repos[key] = true
		w.WriteHeader(201)
		json.NewEncoder(w).Encode(map[string]any{"clone_url": "http://gitea.test/" + key + ".git"})
	})
	mux.HandleFunc("POST /api/v1/orgs", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Username string `json:"username"` }
		_ = json.NewDecoder(r.Body).Decode(&req)
		if orgs[req.Username] != nil {
			w.WriteHeader(422)
			return
		}
		orgs[req.Username] = map[string]bool{}
		nextTeamID++
		orgTeam[req.Username] = nextTeamID
		teamOrg[nextTeamID] = req.Username
		w.WriteHeader(201)
	})
	mux.HandleFunc("GET /api/v1/orgs/{org}/teams", func(w http.ResponseWriter, r *http.Request) {
		org := r.PathValue("org")
		if orgs[org] == nil {
			w.WriteHeader(404)
			return
		}
		json.NewEncoder(w).Encode([]map[string]any{{"id": orgTeam[org], "name": "Owners"}})
	})
	mux.HandleFunc("PUT /api/v1/teams/{id}/members/{username}", func(w http.ResponseWriter, r *http.Request) {
		var id int64
		if _, err := fmt.Sscanf(r.PathValue("id"), "%d", &id); err != nil {
			w.WriteHeader(404)
			return
		}
		org, ok := teamOrg[id]
		u := r.PathValue("username")
		if !ok || users[u] == nil {
			w.WriteHeader(404)
			return
		}
		orgs[org][u] = true
		w.WriteHeader(204)
	})
	mux.HandleFunc("DELETE /api/v1/orgs/{org}/members/{username}", func(w http.ResponseWriter, r *http.Request) {
		org := r.PathValue("org")
		u := r.PathValue("username")
		if orgs[org] == nil || !orgs[org][u] {
			w.WriteHeader(404)
			return
		}
		delete(orgs[org], u)
		w.WriteHeader(204)
	})
	mux.HandleFunc("POST /api/v1/orgs/{org}/repos", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Name string `json:"name"` }
		_ = json.NewDecoder(r.Body).Decode(&req)
		key := r.PathValue("org") + "/" + req.Name
		repos[key] = true
		w.WriteHeader(201)
		json.NewEncoder(w).Encode(map[string]any{"clone_url": "http://gitea.test/" + key + ".git"})
	})
	mux.HandleFunc("GET /api/v1/repos/{owner}/{repo}", func(w http.ResponseWriter, r *http.Request) {
		if !repos[r.PathValue("owner")+"/"+r.PathValue("repo")] {
			w.WriteHeader(404)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"name": r.PathValue("repo")})
	})
	return httptest.NewServer(mux)
}

func TestGiteaBackend(t *testing.T) {
	srv := fakeGitea(t)
	defer srv.Close()
	b := NewGitea(srv.URL, "admin-token")
	ctx := context.Background()

	v, err := b.Ping(ctx)
	if err != nil || v != "1.22.0" {
		t.Fatalf("ping: %v %q", err, v)
	}
	if err := b.CreateUser(ctx, "alice", "pw"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := b.CreateUser(ctx, "alice", "pw"); err == nil {
		t.Fatal("dup must fail")
	}
	tok, err := b.CreateUserToken(ctx, "alice", "ok-sync")
	if err != nil || tok != "tok-alice" {
		t.Fatalf("token: %v %q", err, tok)
	}
	r, err := b.CreatePersonalRepo(ctx, "alice", "ok-demo")
	if err != nil || !strings.Contains(r.CloneURL, "alice/ok-demo.git") {
		t.Fatalf("repo: %+v %v", r, err)
	}
	ok, _ := b.RepoExists(ctx, "alice", "ok-demo")
	if !ok {
		t.Fatal("repo should exist")
	}
	if err := b.CreateOrg(ctx, "ok-acme", "Acme"); err != nil {
		t.Fatalf("org: %v", err)
	}
	if err := b.AddOrgMember(ctx, "ok-acme", "alice"); err != nil {
		t.Fatalf("member: %v", err)
	}
	if _, err := b.CreateOrgRepo(ctx, "ok-acme", "demo"); err != nil {
		t.Fatalf("org repo: %v", err)
	}
	if err := b.SetUserActive(ctx, "alice", false); err != nil {
		t.Fatalf("disable: %v", err)
	}
	// DeleteUser（建用户回滚用）：存在 → 204；不存在 → 404
	if err := b.DeleteUser(ctx, "alice"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := b.DeleteUser(ctx, "alice"); err == nil {
		t.Fatal("delete missing user must fail")
	}
}

// TestGiteaAuthHeader 假 server 断言每个请求带 admin token。
func TestGiteaAuthHeader(t *testing.T) {
	var sawAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAuth = r.Header.Get("Authorization")
		json.NewEncoder(w).Encode(map[string]string{"version": "x"})
	}))
	defer srv.Close()
	b := NewGitea(srv.URL, "admin-token")
	_, _ = b.Ping(context.Background())
	if sawAuth != "token admin-token" {
		t.Fatalf("auth header: %q", sawAuth)
	}
}

// TestGiteaDeleteUserToken404 删 token 的 404 容忍：旧 token 不存在（按机器分名后
// 本机首发即此情形）不视为失败——apiGitToken "删本机同名再建"的兜底分支回归。
func TestGiteaDeleteUserToken404(t *testing.T) {
	srv := fakeGitea(t)
	defer srv.Close()
	b := NewGitea(srv.URL, "admin-token")
	ctx := context.Background()
	if err := b.CreateUser(ctx, "alice", "pw"); err != nil {
		t.Fatalf("create: %v", err)
	}
	// 不存在 → Gitea 404 → DeleteUserToken 容忍返回 nil
	if err := b.DeleteUserToken(ctx, "alice", "ok-sync-r-NB1"); err != nil {
		t.Fatalf("missing token delete must be tolerated: %v", err)
	}
	// 存在 → 204 → nil；再删又回到 404 → 仍 nil
	if _, err := b.CreateUserToken(ctx, "alice", "ok-sync-r-NB1"); err != nil {
		t.Fatalf("create token: %v", err)
	}
	if err := b.DeleteUserToken(ctx, "alice", "ok-sync-r-NB1"); err != nil {
		t.Fatalf("delete existing: %v", err)
	}
	if err := b.DeleteUserToken(ctx, "alice", "ok-sync-r-NB1"); err != nil {
		t.Fatalf("re-delete must be tolerated: %v", err)
	}
}

// TestGiteaListUserTokens 钉死真实 Gitea 的 token 列表契约：1.24+ 的最近使用字段是
// last_used_at（不是 updated_at）；≤1.23 完全无时间字段（解析为零值，输出层转空串）。
// 字段名经 go-gitea/gitea v1.22.0 / v1.24.0 modules/structs/user_app.go 源码核实。
func TestGiteaListUserTokens(t *testing.T) {
	srv := fakeGitea(t)
	defer srv.Close()
	b := NewGitea(srv.URL, "admin-token")
	ctx := context.Background()
	if err := b.CreateUser(ctx, "alice", "pw"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := b.CreateUserToken(ctx, "alice", "ok-sync"); err != nil {
		t.Fatalf("create token: %v", err)
	}
	if _, err := b.CreateUserToken(ctx, "alice", "legacy-old"); err != nil {
		t.Fatalf("create legacy token: %v", err)
	}
	ts, err := b.ListUserTokens(ctx, "alice")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(ts) != 2 {
		t.Fatalf("want 2 tokens, got %d", len(ts))
	}
	byName := map[string]TokenInfo{}
	for _, tk := range ts {
		byName[tk.Name] = tk
	}
	modern, ok := byName["ok-sync"]
	if !ok {
		t.Fatalf("missing ok-sync in %v", ts)
	}
	if modern.CreatedAt.Format(time.RFC3339) != "2026-08-30T10:00:00Z" {
		t.Fatalf("created_at: %v", modern.CreatedAt)
	}
	if modern.UpdatedAt.Format(time.RFC3339) != "2026-08-31T12:34:56Z" {
		t.Fatalf("last_used_at → UpdatedAt: %v", modern.UpdatedAt)
	}
	legacy, ok := byName["legacy-old"]
	if !ok {
		t.Fatalf("missing legacy-old in %v", ts)
	}
	if !legacy.CreatedAt.IsZero() || !legacy.UpdatedAt.IsZero() {
		t.Fatalf("legacy (≤1.23 无时间字段) must parse zero, got %+v", legacy)
	}
}
