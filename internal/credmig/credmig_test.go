package credmig

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"okryptos/internal/config"
	"okryptos/internal/registry"
	"okryptos/internal/store"
	"okryptos/internal/syncx"
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
