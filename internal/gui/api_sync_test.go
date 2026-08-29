package gui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
	// 无冲突时 conflicts 归一化为 [] 而非 null（前端 conflicts.map 依赖数组）
	if !strings.Contains(string(body), `"conflicts":[]`) {
		t.Fatalf("conflicts 应为 []: %s", body)
	}
	// 未注册项目 404
	code, _ = do(t, "GET", srv.URL+"/api/project/sync/status?project=nope", testToken, nil)
	if code != http.StatusNotFound {
		t.Fatalf("nope: %d", code)
	}
}

// TestApiSyncResolveTraversal：resolve 端点拒绝 ".." 路径穿越，仓外文件不得被创建。
func TestApiSyncResolveTraversal(t *testing.T) {
	h, _, okHome := newEnv(t)
	name, _ := mkSyncProject(t, okHome)
	srv := httptest.NewServer(h)
	defer srv.Close()

	code, body := do(t, "POST", srv.URL+"/api/project/sync/resolve", testToken, map[string]any{
		"project": name, "file": "../../evil.txt", "action": "merged", "content": "pwned",
	})
	if code != http.StatusBadRequest {
		t.Fatalf("traversal: want 400, got %d %s", code, body)
	}
	st := stFor(t, okHome, name)
	evil := filepath.Join(st.Root, "..", "..", "evil.txt")
	if _, err := os.Stat(evil); !os.IsNotExist(err) {
		t.Fatalf("仓外文件不应被创建: %s (err=%v)", evil, err)
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

func TestApiSyncAIMergeNoLLM(t *testing.T) {
	h, _, okHome := newEnv(t)
	name, _ := mkSyncProject(t, okHome)
	srv := httptest.NewServer(h)
	defer srv.Close()

	// 制造冲突（直接驱动 syncx，HTTP 全链路在 Task 8）
	st := stFor(t, okHome, name)
	mkConflictFor(t, st, name)

	res, body := do(t, "POST", srv.URL+"/api/project/sync/ai-merge", testToken, map[string]any{"project": name, "file": "k.md"})
	// 无 LLM 配置 → 409 no_llm
	if res != http.StatusConflict || !strings.Contains(string(body), "no_llm") {
		t.Fatalf("ai-merge no llm: %d %s", res, body)
	}
	// 纪律断言：没落盘——k.md 仍含冲突标记
	data, _ := os.ReadFile(filepath.Join(st.KnowledgeDir(), "k.md"))
	if !strings.Contains(string(data), "<<<<<<<") {
		t.Fatal("ai-merge must not write to disk")
	}
	// 清理
	_ = syncx.Open(st.Root).AbortRebase()
}

func TestApiSyncAIMergeOff(t *testing.T) {
	h, _, okHome := newEnv(t)
	name, _ := mkSyncProject(t, okHome)
	st := stFor(t, okHome, name)
	// llm_assist = off
	cfgPath := st.ConfigPath()
	data, _ := os.ReadFile(cfgPath)
	_ = os.WriteFile(cfgPath, append(data, []byte("llm_assist = \"off\"\n")...), 0o644)
	mkConflictFor(t, st, name)
	defer func() { _ = syncx.Open(st.Root).AbortRebase() }()

	srv := httptest.NewServer(h)
	defer srv.Close()
	res, body := do(t, "POST", srv.URL+"/api/project/sync/ai-merge", testToken, map[string]any{"project": name, "file": "k.md"})
	if res != http.StatusConflict || !strings.Contains(string(body), "no_llm") {
		t.Fatalf("ai-merge off: %d %s", res, body)
	}
}

func TestSplitFrontMatter(t *testing.T) {
	fm, body := splitFrontMatter("---\ntitle: 测试\n---\n\n正文内容\n")
	if !strings.Contains(fm, "title: 测试") || body != "正文内容\n" {
		t.Fatalf("fm=%q body=%q", fm, body)
	}
	fm, body = splitFrontMatter("无头部正文\n")
	if fm != "" || body != "无头部正文\n" {
		t.Fatalf("no fm: fm=%q body=%q", fm, body)
	}
}

// mkConflictFor 直接经 syncx 制造 k.md 冲突态（A 推 v2a、本地改 v2b）。
func mkConflictFor(t *testing.T, st *store.Store, name string) {
	t.Helper()
	r := syncx.Open(st.Root)
	// 第二台设备：clone bare 后改动推回
	dir2 := t.TempDir()
	if err := syncx.Open(dir2).CloneToDir(r.RemoteURL()); err != nil {
		t.Fatalf("clone: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir2, "knowledge", "k.md"), []byte("v2a 远端修改\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r2 := syncx.Open(dir2)
	if _, err := r2.CommitAll("sync: other"); err != nil {
		t.Fatalf("commit2: %v", err)
	}
	if err := r2.Push(); err != nil {
		t.Fatalf("push2: %v", err)
	}
	// 本机改同文件
	if err := os.WriteFile(filepath.Join(st.KnowledgeDir(), "k.md"), []byte("v2b 本机修改\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := r.CommitAll("sync: local"); err != nil {
		t.Fatalf("commit local: %v", err)
	}
	conflicts, err := r.PullRebase()
	if err != nil || len(conflicts) != 1 {
		t.Fatalf("should conflict: %v %v", conflicts, err)
	}
}
