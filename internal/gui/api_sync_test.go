package gui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
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
	mkConflictBodies(t, st, "v2a 远端修改\n", "v2b 本机修改\n")
}

// mkConflictBodies 同 mkConflictFor，两侧正文可定制
// （ai-merge 200 用例需要 local 版带 front matter 以验证 fm 取 local 前缀）。
func mkConflictBodies(t *testing.T, st *store.Store, remoteBody, localBody string) {
	t.Helper()
	r := syncx.Open(st.Root)
	// 第二台设备：clone bare 后改动推回
	dir2 := t.TempDir()
	if err := syncx.Open(dir2).CloneToDir(r.RemoteURL()); err != nil {
		t.Fatalf("clone: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir2, "knowledge", "k.md"), []byte(remoteBody), 0o644); err != nil {
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
	if err := os.WriteFile(filepath.Join(st.KnowledgeDir(), "k.md"), []byte(localBody), 0o644); err != nil {
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

// TestApiSyncConflictFullFlow 全链路：制造冲突 → POST sync 报 conflict →
// GET conflict-file 三版本且 local/remote 映射正确 → resolve merged → finish → 远端含合并内容。
func TestApiSyncConflictFullFlow(t *testing.T) {
	h, _, okHome := newEnv(t)
	name, bare := mkSyncProject(t, okHome)
	st := stFor(t, okHome, name)
	srv := httptest.NewServer(h)
	defer srv.Close()

	mkConflictFor(t, st, name) // 远端 v2a / 本机 v2b，rebase 半途

	// POST sync：守卫识别冲突 → status=conflict
	code, body := do(t, "POST", srv.URL+"/api/project/sync", testToken, map[string]any{"project": name})
	if code != 200 {
		t.Fatalf("sync: %d %s", code, body)
	}
	var so struct {
		Status    string   `json:"status"`
		Conflicts []string `json:"conflicts"`
	}
	if err := json.Unmarshal(body, &so); err != nil {
		t.Fatal(err)
	}
	if so.Status != "conflict" || len(so.Conflicts) != 1 {
		t.Fatalf("want conflict: %s", body)
	}

	// GET status：conflict=true 且 conflicts 含文件
	code, body = do(t, "GET", srv.URL+"/api/project/sync/status?project="+name, testToken, nil)
	var ss struct {
		Conflict  bool     `json:"conflict"`
		Conflicts []string `json:"conflicts"`
	}
	if err := json.Unmarshal(body, &ss); err != nil {
		t.Fatal(err)
	}
	if !ss.Conflict || len(ss.Conflicts) != 1 || !strings.Contains(ss.Conflicts[0], "k.md") {
		t.Fatalf("status: %s", body)
	}
	// conflicts 项是仓内相对路径（knowledge/k.md），后续端点与前端一样按此原样传参
	file := ss.Conflicts[0]

	// GET conflict-file：三版本齐全 + Me/Theirs 映射专项断言（反转纪律）
	code, body = do(t, "GET", srv.URL+"/api/project/sync/conflict-file?project="+name+"&file="+url.QueryEscape(file), testToken, nil)
	if code != 200 {
		t.Fatalf("conflict-file: %d %s", code, body)
	}
	var cf struct {
		Base, Local, Remote, Working string
	}
	if err := json.Unmarshal(body, &cf); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(cf.Local, "v2b 本机修改") {
		t.Fatalf("local 必须是本机版本: %q", cf.Local)
	}
	if !strings.Contains(cf.Remote, "v2a 远端修改") {
		t.Fatalf("remote 必须是远端版本: %q", cf.Remote)
	}
	if !strings.Contains(cf.Working, "<<<<<<<") {
		t.Fatalf("working 应含冲突标记: %q", cf.Working)
	}

	// resolve merged
	code, _ = do(t, "POST", srv.URL+"/api/project/sync/resolve", testToken, map[string]any{
		"project": name, "file": file, "action": "merged", "content": "v3 合并内容\n",
	})
	if code != 204 {
		t.Fatalf("resolve: %d", code)
	}

	// finish → 推送成功
	code, body = do(t, "POST", srv.URL+"/api/project/sync/finish", testToken, map[string]any{"project": name})
	if code != 200 {
		t.Fatalf("finish: %d %s", code, body)
	}
	// 远端含合并内容
	gitRun(t, bare, "show", "main:knowledge/k.md")
	out := gitOut(t, bare, "show", "main:knowledge/k.md")
	if !strings.Contains(out, "v3 合并内容") {
		t.Fatalf("remote content: %q", out)
	}
	// 状态恢复：conflict=false、冲突文件已清
	code, body = do(t, "GET", srv.URL+"/api/project/sync/status?project="+name, testToken, nil)
	if code != 200 {
		t.Fatalf("status after finish: %d %s", code, body)
	}
	if strings.Contains(string(body), `"conflict":true`) {
		t.Fatalf("should recover: %s", body)
	}
	if _, err := os.Stat(filepath.Join(st.StateDir(), "sync-conflict.json")); !os.IsNotExist(err) {
		t.Fatalf("conflict file should be cleared: %v", err)
	}
}

// TestApiSyncConflictAbortFlow 放弃路径：abort 后本地内容回滚到本机提交、状态清除。
func TestApiSyncConflictAbortFlow(t *testing.T) {
	h, _, okHome := newEnv(t)
	name, _ := mkSyncProject(t, okHome)
	st := stFor(t, okHome, name)
	srv := httptest.NewServer(h)
	defer srv.Close()

	mkConflictFor(t, st, name)
	code, _ := do(t, "POST", srv.URL+"/api/project/sync/abort", testToken, map[string]any{"project": name})
	if code != 204 {
		t.Fatalf("abort: %d", code)
	}
	if syncx.Open(st.Root).MergeInProgress() {
		t.Fatal("should not be in progress after abort")
	}
	data, _ := os.ReadFile(filepath.Join(st.KnowledgeDir(), "k.md"))
	if !strings.Contains(string(data), "v2b 本机修改") || strings.Contains(string(data), "<<<<<<<") {
		t.Fatalf("local content lost or markers left: %q", data)
	}
}

// TestApiSyncPartialResolveStatus C1 契约固化：部分解决后（resolve 一个文件、不 finish）
// GET status 的 conflicts 只含未决项（仓态动态列表——此处应为空数组，唯一冲突文件已解决），
// conflict 仍为 true（rebase 半途）；已解决文件再 GET conflict-file 必 409。
// 前端依据该契约：fresh 加载只显示未解决卡，不会拿到必 409 的已解决文件而白屏。
func TestApiSyncPartialResolveStatus(t *testing.T) {
	h, _, okHome := newEnv(t)
	name, _ := mkSyncProject(t, okHome)
	st := stFor(t, okHome, name)
	srv := httptest.NewServer(h)
	defer srv.Close()

	mkConflictFor(t, st, name) // rebase 半途，knowledge/k.md 冲突
	defer func() { _ = syncx.Open(st.Root).AbortRebase() }()

	// HTTP resolve（action=theirs）：文件落盘 + git add，stage 坍塌
	code, body := do(t, "POST", srv.URL+"/api/project/sync/resolve", testToken, map[string]any{
		"project": name, "file": "knowledge/k.md", "action": "theirs",
	})
	if code != 204 {
		t.Fatalf("resolve: %d %s", code, body)
	}

	// 不 finish，直接 GET status：conflicts 应为空数组（未决列表），conflict 仍为 true
	code, body = do(t, "GET", srv.URL+"/api/project/sync/status?project="+name, testToken, nil)
	if code != 200 {
		t.Fatalf("status: %d %s", code, body)
	}
	var ss struct {
		Conflict  bool     `json:"conflict"`
		Conflicts []string `json:"conflicts"`
	}
	if err := json.Unmarshal(body, &ss); err != nil {
		t.Fatal(err)
	}
	if !ss.Conflict {
		t.Fatalf("rebase 半途 conflict 应为 true: %s", body)
	}
	if len(ss.Conflicts) != 0 {
		t.Fatalf("conflicts 应只含未决项（此处已空）: %s", body)
	}
	if !strings.Contains(string(body), `"conflicts":[]`) {
		t.Fatalf("conflicts 应为 [] 而非 null/残留: %s", body)
	}

	// 已解决文件 conflict-file 必 409（stage 坍塌）
	code, _ = do(t, "GET", srv.URL+"/api/project/sync/conflict-file?project="+name+"&file="+url.QueryEscape("knowledge/k.md"), testToken, nil)
	if code != http.StatusConflict {
		t.Fatalf("conflict-file on resolved: want 409, got %d", code)
	}
}

// TestApiSyncGitPathGuard I1 契约：.git 路径纵深——conflict-file/resolve/ai-merge 三端点
// 一律拒绝 Clean 后首段为 .git 的参数（大小写不敏感，正反斜杠都算）。
func TestApiSyncGitPathGuard(t *testing.T) {
	h, _, okHome := newEnv(t)
	name, _ := mkSyncProject(t, okHome)
	st := stFor(t, okHome, name)
	srv := httptest.NewServer(h)
	defer srv.Close()

	mkConflictFor(t, st, name)
	defer func() { _ = syncx.Open(st.Root).AbortRebase() }()

	for _, file := range []string{".git/config", ".GIT/HEAD", ".git\\hooks\\x"} {
		code, _ := do(t, "GET", srv.URL+"/api/project/sync/conflict-file?project="+name+"&file="+url.QueryEscape(file), testToken, nil)
		if code != http.StatusBadRequest {
			t.Fatalf("conflict-file %q: want 400, got %d", file, code)
		}
		code, _ = do(t, "POST", srv.URL+"/api/project/sync/resolve", testToken, map[string]any{
			"project": name, "file": file, "action": "merged", "content": "x",
		})
		if code != http.StatusBadRequest {
			t.Fatalf("resolve %q: want 400, got %d", file, code)
		}
		code, _ = do(t, "POST", srv.URL+"/api/project/sync/ai-merge", testToken, map[string]any{
			"project": name, "file": file,
		})
		if code != http.StatusBadRequest {
			t.Fatalf("ai-merge %q: want 400, got %d", file, code)
		}
	}
}

// TestApiSyncAIMergeOK ai-merge 200 成功路径：httptest 假 OpenAI 兼容 LLM
// （/v1/chat/completions 回固定 merged 文本）+ 真 HTTP 配置并激活指向它的
// LLM profile（落盘隔离 OK_HOME/config.toml）。断言 {merged} 含假 server 文本、
// 带 local 版 front matter 前缀；且 200 分支同样不落盘（k.md 仍含冲突标记）。
func TestApiSyncAIMergeOK(t *testing.T) {
	h, _, okHome := newEnv(t)
	name, _ := mkSyncProject(t, okHome)
	st := stFor(t, okHome, name)

	fakeLLM := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]string{"content": "AI 合并后的正文"}}},
			"usage":   map[string]any{"prompt_tokens": 10, "completion_tokens": 5},
		})
	}))
	defer fakeLLM.Close()

	srv := httptest.NewServer(h)
	defer srv.Close()

	// 真 HTTP 配置并激活 LLM profile（与「模型配置」页同一路径）
	code, body := do(t, "POST", srv.URL+"/api/llm/profile", testToken, map[string]any{
		"name": "fake", "kind": "openai", "base_url": fakeLLM.URL + "/v1/",
		"model": "m1", "api_key": "sk-fake", "activate": true,
	})
	if code != 200 {
		t.Fatalf("save llm profile: %d %s", code, body)
	}

	// 制造冲突：local 版带 front matter（remote 版带不同 fm，验证不混入）
	mkConflictBodies(t, st,
		"---\ntitle: 远端标题\n---\n\nv2a 远端修改\n",
		"---\ntitle: 本机标题\n---\n\nv2b 本机修改\n")
	defer func() { _ = syncx.Open(st.Root).AbortRebase() }()

	code, body = do(t, "POST", srv.URL+"/api/project/sync/ai-merge", testToken, map[string]any{"project": name, "file": "knowledge/k.md"})
	if code != 200 {
		t.Fatalf("ai-merge: %d %s", code, body)
	}
	var out struct {
		Merged string `json:"merged"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.Merged, "AI 合并后的正文") {
		t.Fatalf("merged 应含假 LLM 文本: %q", out.Merged)
	}
	// fm 前缀取 local 版（front matter 不让 LLM 碰；remote 的 fm 不得混入）
	if !strings.HasPrefix(out.Merged, "---\ntitle: 本机标题\n---\n") {
		t.Fatalf("merged 应以 local front matter 开头: %q", out.Merged)
	}
	if strings.Contains(out.Merged, "远端标题") {
		t.Fatalf("remote front matter 不应混入: %q", out.Merged)
	}
	// 纪律断言：200 分支也未落盘——k.md 仍含冲突标记（落盘走 resolve）
	data, _ := os.ReadFile(filepath.Join(st.KnowledgeDir(), "k.md"))
	if !strings.Contains(string(data), "<<<<<<<") {
		t.Fatal("ai-merge 200 分支也必须不落盘")
	}
}

// gitOut 返回 git 输出（测试基建）。
func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := append([]string{"-C", dir}, args...)
	cmd := exec.Command("git", full...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "LC_ALL=C")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}
