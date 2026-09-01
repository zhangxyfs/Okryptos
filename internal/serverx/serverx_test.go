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
		_ = json.NewEncoder(w).Encode(map[string]any{"version": "v1", "initialized": true, "git_backend": map[string]any{"type": "fake", "ok": true}})
	})
	mux.HandleFunc("POST /api/v1/login", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Username, Password string
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Username == "alice" && req.Password == "pw" {
			_ = json.NewEncoder(w).Encode(map[string]any{"token": "tok-1", "user": map[string]string{"name": "alice", "role": "member"}})
			return
		}
		w.WriteHeader(401)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "未认证"})
	})
	authed := func(w http.ResponseWriter, r *http.Request) bool {
		if r.Header.Get("Authorization") != "Bearer tok-1" {
			w.WriteHeader(401)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "未认证"})
			return false
		}
		return true
	}
	mux.HandleFunc("GET /api/v1/me", func(w http.ResponseWriter, r *http.Request) {
		if !authed(w, r) {
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"name": "alice", "role": "member", "must_change_password": true, "orgs": []string{"acme"}, "repos": []any{}})
	})
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
	mux.HandleFunc("POST /api/v1/repos/personal", func(w http.ResponseWriter, r *http.Request) {
		if !authed(w, r) {
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"repo":      map[string]string{"layer": "personal", "owner": "alice", "project": "demo", "name": "ok-demo", "clone_url": "http://gitea/alice/ok-demo.git"},
			"git_token": "git-tok-1",
		})
	})
	mux.HandleFunc("GET /api/v1/users", func(w http.ResponseWriter, r *http.Request) {
		if !authed(w, r) {
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"users": []map[string]any{{"name": "root", "role": "root", "disabled": false, "created_at": "2026-08-29T00:00:00Z"}}})
	})
	return httptest.NewServer(mux)
}

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
