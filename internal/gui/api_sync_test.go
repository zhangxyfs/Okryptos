package gui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"openknowledge/internal/store"
	"openknowledge/internal/syncx"
)

// mkSyncProject 注册一个已初始化的同步项目（本地仓 + bare 远端 + 首个提交）。
func mkSyncProject(t *testing.T, okHome string) (name string, bare string) {
	t.Helper()
	name, dir := "syncdemo", filepath.Join(t.TempDir(), "work")
	mkProjectAt(t, okHome, name, dir)
	bare = t.TempDir()
	gitRun(t, bare, "init", "--bare", "-b", "main")
	st := stFor(t, okHome, name)
	r := syncx.Open(st.Root)
	if err := r.Init(); err != nil {
		t.Fatalf("init: %v", err)
	}
	if err := os.WriteFile(filepath.Join(st.KnowledgeDir(), "k.md"), []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := r.CommitAll("init"); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if err := r.SetRemote(bare); err != nil {
		t.Fatalf("remote: %v", err)
	}
	if err := r.Push(); err != nil {
		t.Fatalf("push: %v", err)
	}
	if err := os.WriteFile(st.ConfigPath(), []byte("[sync]\nenabled = true\nremote = \""+filepath.ToSlash(bare)+"\"\nauto_interval_min = 5\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return name, bare
}

func TestApiSyncStatus(t *testing.T) {
	h, _, okHome := newEnv(t)
	name, _ := mkSyncProject(t, okHome)
	srv := httptest.NewServer(h)
	defer srv.Close()

	code, body := do(t, "GET", srv.URL+"/api/project/sync/status?project="+name, testToken, nil)
	if code != 200 {
		t.Fatalf("status: %d %s", code, body)
	}
	var st struct {
		Enabled  bool   `json:"enabled"`
		IsRepo   bool   `json:"is_repo"`
		Ahead    int    `json:"ahead"`
		Conflict bool   `json:"conflict"`
		LastSync string `json:"last_sync"`
	}
	if err := json.Unmarshal(body, &st); err != nil {
		t.Fatal(err)
	}
	if !st.Enabled || !st.IsRepo || st.Conflict {
		t.Fatalf("unexpected: %+v", st)
	}
	// 未注册项目 404
	code, _ = do(t, "GET", srv.URL+"/api/project/sync/status?project=nope", testToken, nil)
	if code != http.StatusNotFound {
		t.Fatalf("nope: %d", code)
	}
}

func TestApiSyncManualTrigger(t *testing.T) {
	h, _, okHome := newEnv(t)
	name, _ := mkSyncProject(t, okHome)
	srv := httptest.NewServer(h)
	defer srv.Close()

	// 制造一个未提交变更再触发同步
	st := stFor(t, okHome, name)
	if err := os.WriteFile(filepath.Join(st.KnowledgeDir(), "k2.md"), []byte("v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, body := do(t, "POST", srv.URL+"/api/project/sync", testToken, map[string]any{"project": name})
	if code != 200 {
		t.Fatalf("sync: %d %s", code, body)
	}
	var out struct {
		Status  string `json:"status"`
		Pushed  int    `json:"pushed"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatal(err)
	}
	if out.Status != "ok" || out.Pushed != 1 || out.Message == "" {
		t.Fatalf("unexpected: %+v", out)
	}
}

func TestApiSyncNotRepo(t *testing.T) {
	h, _, okHome := newEnv(t)
	mkProjectAt(t, okHome, "plain", filepath.Join(t.TempDir(), "w"))
	srv := httptest.NewServer(h)
	defer srv.Close()

	code, body := do(t, "POST", srv.URL+"/api/project/sync", testToken, map[string]any{"project": "plain"})
	if code != 200 {
		t.Fatalf("sync: %d %s", code, body)
	}
	var out struct {
		Status string `json:"status"`
	}
	_ = json.Unmarshal(body, &out)
	if out.Status != "not_repo" {
		t.Fatalf("want not_repo: %s", body)
	}
}

func TestApiSyncAbortIdle(t *testing.T) {
	h, _, okHome := newEnv(t)
	name, _ := mkSyncProject(t, okHome)
	srv := httptest.NewServer(h)
	defer srv.Close()

	// 非冲突态 abort → 409
	code, _ := do(t, "POST", srv.URL+"/api/project/sync/abort", testToken, map[string]any{"project": name})
	if code != http.StatusConflict {
		t.Fatalf("abort idle: %d", code)
	}
}

// gitRun 直接 exec git（测试基建）。
func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	full := append([]string{"-C", dir}, args...)
	cmd := exec.Command("git", full...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "LC_ALL=C")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// stFor 取项目的 store（与 gui 内部同推导）。
func stFor(t *testing.T, okHome, name string) *store.Store {
	t.Helper()
	return store.New(filepath.Join(okHome, "projects", name))
}
