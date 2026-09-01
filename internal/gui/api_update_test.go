package gui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// withGithubAPI 临时替换包级 githubAPI 并注册恢复。
func withGithubAPI(t *testing.T, url string) {
	t.Helper()
	old := githubAPI
	githubAPI = url
	t.Cleanup(func() { githubAPI = old })
}

func TestUpdateCheckNewVersion(t *testing.T) {
	h, _ := changelogEnv(t)
	withVersion(t, "1.0.0")
	var hits int32
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		if r.Header.Get("Accept") != "application/vnd.github+json" {
			t.Errorf("Accept header = %q", r.Header.Get("Accept"))
		}
		json.NewEncoder(w).Encode(map[string]any{
			"tag_name": "v99.0.0",
			"body":     "notes",
			"assets": []map[string]any{
				{"name": "OpenKnowledge-Setup-99.0.0.exe", "browser_download_url": "https://x/setup.exe"},
				{"name": "openknowledge_99.0.0_amd64.deb", "browser_download_url": "https://x/a.deb"},
				{"name": "openknowledge_99.0.0_linux_amd64.tar.gz", "browser_download_url": "https://x/a.tar.gz"},
			},
		})
	}))
	defer fake.Close()
	withGithubAPI(t, fake.URL)

	// 预置 skipped_version：响应须透传（Task 6 前端需要），写回缓存时不得丢
	if err := writeGuiState(guiState{SkippedVersion: "98.0.0"}); err != nil {
		t.Fatal(err)
	}

	body := doJSON(t, h, "GET", "/api/update/check")
	if body["update_available"] != true {
		t.Fatalf("update_available = %v, want true: %v", body["update_available"], body)
	}
	if body["latest"] != "99.0.0" || body["current"] != "1.0.0" {
		t.Fatalf("latest/current = %v/%v", body["latest"], body["current"])
	}
	if body["body"] != "notes" {
		t.Fatalf("body = %v", body["body"])
	}
	if body["installer_url"] != "https://x/setup.exe" ||
		body["deb_url"] != "https://x/a.deb" ||
		body["tar_url"] != "https://x/a.tar.gz" {
		t.Fatalf("asset urls = %v", body)
	}
	if body["skipped_version"] != "98.0.0" {
		t.Fatalf("skipped_version = %v, want 98.0.0", body["skipped_version"])
	}

	// 缓存写回 gui.json（skipped_version 保留）
	st, err := readGuiState()
	if err != nil {
		t.Fatal(err)
	}
	if st.UpdateCheck == nil || st.UpdateCheck.Latest != "99.0.0" || st.UpdateCheck.CheckedAt == 0 {
		t.Fatalf("cached UpdateCheck = %+v", st.UpdateCheck)
	}
	if st.SkippedVersion != "98.0.0" {
		t.Fatalf("SkippedVersion lost on write: %+v", st)
	}

	// 第二次命中 6h 缓存：不再请求 GitHub
	body = doJSON(t, h, "GET", "/api/update/check")
	if n := atomic.LoadInt32(&hits); n != 1 {
		t.Fatalf("github hits = %d, want 1 (cache miss)", n)
	}
	if body["update_available"] != true || body["latest"] != "99.0.0" {
		t.Fatalf("cached resp = %v", body)
	}
	if body["skipped_version"] != "98.0.0" {
		t.Fatalf("cached skipped_version = %v", body["skipped_version"])
	}
}

func TestUpdateCheckNotNewer(t *testing.T) {
	h, _ := changelogEnv(t)
	withVersion(t, "99.0.0")
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"tag_name": "v99.0.0", "assets": []map[string]any{}})
	}))
	defer fake.Close()
	withGithubAPI(t, fake.URL)

	body := doJSON(t, h, "GET", "/api/update/check")
	if body["update_available"] != false {
		t.Fatalf("same version should not update: %v", body)
	}
}

func TestUpdateCheckFailOpen(t *testing.T) {
	h, _ := changelogEnv(t)
	withVersion(t, "1.0.0")
	withGithubAPI(t, "http://127.0.0.1:1")

	body := doJSON(t, h, "GET", "/api/update/check")
	if body["update_available"] != false {
		t.Fatalf("fail-open: update_available = %v", body["update_available"])
	}
	if body["current"] != "1.0.0" {
		t.Fatalf("current = %v", body["current"])
	}

	// 失败也写缓存 CheckedAt（Latest 留空）防雪崩
	st, err := readGuiState()
	if err != nil {
		t.Fatal(err)
	}
	if st.UpdateCheck == nil || st.UpdateCheck.CheckedAt == 0 || st.UpdateCheck.Latest != "" {
		t.Fatalf("fail-open cache = %+v", st.UpdateCheck)
	}
}

func TestUpdateCheckBadTag(t *testing.T) {
	h, _ := changelogEnv(t)
	withVersion(t, "1.0.0")
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"tag_name": "not-a-version", "assets": []map[string]any{}})
	}))
	defer fake.Close()
	withGithubAPI(t, fake.URL)

	body := doJSON(t, h, "GET", "/api/update/check")
	if body["update_available"] != false {
		t.Fatalf("unparsable tag should not update: %v", body)
	}
}

// 无匹配资产时 URL 留空（宽松后缀匹配找不到不报错）。
func TestUpdateCheckNoMatchingAssets(t *testing.T) {
	h, _ := changelogEnv(t)
	withVersion(t, "1.0.0")
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"tag_name": "v99.0.0",
			"assets": []map[string]any{
				{"name": "checksums.txt", "browser_download_url": "https://x/sums.txt"},
			},
		})
	}))
	defer fake.Close()
	withGithubAPI(t, fake.URL)

	body := doJSON(t, h, "GET", "/api/update/check")
	if body["update_available"] != true {
		t.Fatalf("update_available = %v", body["update_available"])
	}
	if _, ok := body["installer_url"]; ok {
		t.Fatalf("installer_url should be omitted: %v", body)
	}
	// gui.json 不应留在 OK_HOME 之外的地方
	if _, err := os.Stat(filepath.Join(os.Getenv("OK_HOME"), "gui.json")); err != nil {
		t.Fatalf("gui.json not written: %v", err)
	}
}

// withUpdateURLPrefix 临时替换包级 updateURLPrefix 并注册恢复。
func withUpdateURLPrefix(t *testing.T, prefix string) {
	t.Helper()
	old := updateURLPrefix
	updateURLPrefix = prefix
	t.Cleanup(func() { updateURLPrefix = old })
}

// resetUpdateDownloadJob 清空包级下载任务，避免测试间串扰。
func resetUpdateDownloadJob(t *testing.T) {
	t.Helper()
	updMu.Lock()
	updCur = nil
	updMu.Unlock()
	t.Cleanup(func() {
		updMu.Lock()
		updCur = nil
		updMu.Unlock()
	})
}

// postUpdateDownload 发 POST /api/update/download 并断言 200。
func postUpdateDownload(t *testing.T, h *Handler, url, version string) map[string]any {
	t.Helper()
	req := httptest.NewRequest("POST", "/api/update/download",
		strings.NewReader(fmt.Sprintf(`{"url":%q,"version":%q}`, url, version)))
	req.Header.Set("X-Ok-Token", testToken)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/update/download -> %d: %s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// pollUpdateDownload 轮询 GET 快照直到 done/error 或超时。
func pollUpdateDownload(t *testing.T, h *Handler) map[string]any {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		body := doJSON(t, h, "GET", "/api/update/download")
		if body["state"] == "done" || body["state"] == "error" {
			return body
		}
		if time.Now().After(deadline) {
			t.Fatalf("download not finished in time: %v", body)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestUpdateDownloadJob(t *testing.T) {
	h, _ := changelogEnv(t)
	resetUpdateDownloadJob(t)
	content := []byte("fake installer payload content")
	release := make(chan struct{})
	var hits int32
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Header().Set("Content-Length", strconv.Itoa(len(content)))
		w.WriteHeader(http.StatusOK)
		<-release // 挂住响应体，保证重复 POST 到来时任务仍 running
		_, _ = w.Write(content)
	}))
	defer fake.Close()
	withUpdateURLPrefix(t, fake.URL+"/")

	// 初始快照 idle
	body := doJSON(t, h, "GET", "/api/update/download")
	if body["state"] != "idle" {
		t.Fatalf("initial state = %v, want idle", body["state"])
	}

	// POST 启动下载
	body = postUpdateDownload(t, h, fake.URL+"/setup.exe", "99.0.0")
	if body["state"] != "running" {
		t.Fatalf("post state = %v, want running: %v", body["state"], body)
	}

	// 等 goroutine 的请求到达假服务器（否则幂等校验可能抢跑）
	deadline := time.Now().Add(5 * time.Second)
	for atomic.LoadInt32(&hits) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("download goroutine never hit the fake server")
		}
		time.Sleep(5 * time.Millisecond)
	}

	// 幂等：重复 POST 返回当前快照，不起第二个 goroutine
	body = postUpdateDownload(t, h, fake.URL+"/setup.exe", "99.0.0")
	if body["state"] != "running" {
		t.Fatalf("idempotent post state = %v, want running", body["state"])
	}
	if n := atomic.LoadInt32(&hits); n != 1 {
		t.Fatalf("server hits = %d, want 1 (no second download)", n)
	}

	close(release)
	final := pollUpdateDownload(t, h)
	if final["state"] != "done" {
		t.Fatalf("final state = %v, want done (err=%v)", final["state"], final["err"])
	}
	path, _ := final["path"].(string)
	if path == "" || strings.HasSuffix(path, ".part") {
		t.Fatalf("path = %q, want final path without .part", path)
	}
	if filepath.Base(path) != "OpenKnowledge-Setup-99.0.0.exe" {
		t.Fatalf("path base = %q", filepath.Base(path))
	}
	if filepath.Dir(path) != filepath.Join(os.Getenv("OK_HOME"), "update") {
		t.Fatalf("path dir = %q, want under OK_HOME/update", filepath.Dir(path))
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("downloaded content mismatch: %q", got)
	}
	if _, err := os.Stat(path + ".part"); !os.IsNotExist(err) {
		t.Fatalf(".part should be renamed away, stat err = %v", err)
	}
	if final["done"] != float64(len(content)) || final["total"] != float64(len(content)) {
		t.Fatalf("done/total = %v/%v, want %d", final["done"], final["total"], len(content))
	}
}

// 断点续传：预置半截 .part，客户端须带 Range: bytes=N-；服务器 206 时追加续传，
// 不认 Range 回 200 时截断重写——两种路径最终文件内容都必须完整、.part 已改名消失。
func TestUpdateDownloadResume(t *testing.T) {
	for _, tc := range []struct {
		name       string
		honorRange bool // 服务器是否认 Range（206）还是忽略（200）
	}{
		{"206 续传", true},
		{"200 降级重写", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, _ := changelogEnv(t)
			resetUpdateDownloadJob(t)
			content := []byte(strings.Repeat("fake installer;", 128))
			const offset = 512
			// 预置半截 .part：完整内容的前 offset 字节
			dest := filepath.Join(os.Getenv("OK_HOME"), "update", "OpenKnowledge-Setup-99.0.0.exe")
			if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(dest+".part", content[:offset], 0o644); err != nil {
				t.Fatal(err)
			}
			fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if got, want := r.Header.Get("Range"), fmt.Sprintf("bytes=%d-", offset); got != want {
					t.Errorf("Range header = %q, want %q", got, want)
				}
				if tc.honorRange {
					w.Header().Set("Content-Length", strconv.Itoa(len(content)-offset))
					w.WriteHeader(http.StatusPartialContent)
					_, _ = w.Write(content[offset:])
					return
				}
				// 不认 Range：回 200 + 完整内容，客户端应截断重写
				w.Header().Set("Content-Length", strconv.Itoa(len(content)))
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(content)
			}))
			defer fake.Close()
			withUpdateURLPrefix(t, fake.URL+"/")

			body := postUpdateDownload(t, h, fake.URL+"/setup.exe", "99.0.0")
			if body["state"] != "running" {
				t.Fatalf("post state = %v, want running: %v", body["state"], body)
			}
			final := pollUpdateDownload(t, h)
			if final["state"] != "done" {
				t.Fatalf("final state = %v, want done (err=%v)", final["state"], final["err"])
			}
			got, err := os.ReadFile(dest)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, content) {
				t.Fatalf("final content mismatch: %d bytes, want %d bytes of full content", len(got), len(content))
			}
			if _, err := os.Stat(dest + ".part"); !os.IsNotExist(err) {
				t.Fatalf(".part should be renamed away, stat err = %v", err)
			}
			if final["done"] != float64(len(content)) || final["total"] != float64(len(content)) {
				t.Fatalf("done/total = %v/%v, want %d", final["done"], final["total"], len(content))
			}
		})
	}
}

// SSRF 纪律：URL 必须以 release 下载前缀开头，否则 400。
func TestUpdateDownloadBadURL(t *testing.T) {
	h, _ := changelogEnv(t)
	resetUpdateDownloadJob(t)
	for _, u := range []string{
		"https://evil.example.com/setup.exe",
		"http://github.com/zhangxyfs/OpenKnowledge/releases/download/v1/setup.exe",
		"https://github.com/zhangxyfs/OpenKnowledge/releases/download.evil/setup.exe",
	} {
		req := httptest.NewRequest("POST", "/api/update/download",
			strings.NewReader(fmt.Sprintf(`{"url":%q,"version":"1.0.0"}`, u)))
		req.Header.Set("X-Ok-Token", testToken)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("url %q -> %d, want 400: %s", u, rec.Code, rec.Body.String())
		}
	}
	// 不合法的 POST 不留下任务
	body := doJSON(t, h, "GET", "/api/update/download")
	if body["state"] != "idle" {
		t.Fatalf("state after bad posts = %v, want idle", body["state"])
	}
}

// 下载失败（HTTP 404）→ 快照 state=error，err 非空。
func TestUpdateDownloadError(t *testing.T) {
	h, _ := changelogEnv(t)
	resetUpdateDownloadJob(t)
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusNotFound)
	}))
	defer fake.Close()
	withUpdateURLPrefix(t, fake.URL+"/")

	body := postUpdateDownload(t, h, fake.URL+"/missing.exe", "1.0.0")
	if body["state"] != "running" {
		t.Fatalf("post state = %v, want running", body["state"])
	}
	final := pollUpdateDownload(t, h)
	if final["state"] != "error" {
		t.Fatalf("final state = %v, want error: %v", final["state"], final)
	}
	if final["err"] == "" || final["err"] == nil {
		t.Fatalf("err should be non-empty: %v", final)
	}
}

// ---------- POST /api/update/apply ----------

// withApplyStubs 把升级序列的副作用全部替换为记录调用的假实现（测试不真跑安装器、
// 不真退出进程）。stopDaemon/runInstaller 期间熔断标记必须存在，spawnOkd 时须已删除
// ——顺序错误直接在 goroutine 内报错（测试等待 done，报错发生在结束前，安全）。
// 返回事件记录（按发生顺序）与 exitProcess 被调用的信号 channel。
func withApplyStubs(t *testing.T) (*[]string, chan struct{}) {
	t.Helper()
	events := &[]string{}
	var mu sync.Mutex
	add := func(s string) { mu.Lock(); *events = append(*events, s); mu.Unlock() }
	mark := upgradeMarkPath()

	oldStop, oldRun, oldSpawn, oldExit := stopDaemon, runInstaller, spawnOkd, exitProcess
	stopDaemon = func() {
		if _, err := os.Stat(mark); err != nil {
			t.Errorf("stopDaemon 调用时熔断标记不存在（顺序错误）")
		}
		add("stopDaemon")
	}
	runInstaller = func(path string) error {
		if _, err := os.Stat(mark); err != nil {
			t.Errorf("runInstaller 调用时熔断标记不存在（顺序错误）")
		}
		add("runInstaller:" + filepath.Base(path))
		return nil
	}
	spawnOkd = func() error {
		if _, err := os.Stat(mark); !os.IsNotExist(err) {
			t.Errorf("spawnOkd 调用时熔断标记未删除（顺序错误）")
		}
		add("spawnOkd")
		return nil
	}
	done := make(chan struct{})
	exitProcess = func(code int) {
		add(fmt.Sprintf("exit:%d", code))
		close(done)
	}
	t.Cleanup(func() {
		stopDaemon, runInstaller, spawnOkd, exitProcess = oldStop, oldRun, oldSpawn, oldExit
	})
	return events, done
}

// setUpdateJobDone 预置一个 done 状态的下载任务，Path 指向 installer。
func setUpdateJobDone(t *testing.T, installer string) {
	t.Helper()
	updMu.Lock()
	updCur = &updJob{State: "done", Path: installer}
	updMu.Unlock()
}

// 升级序列：写熔断 → StopDaemon → 跑安装器 → 删熔断 → 拉起新 okd → 退出进程。
func TestUpdateApplySequence(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("apply 仅 Windows")
	}
	h, _ := changelogEnv(t)
	resetUpdateDownloadJob(t)
	events, done := withApplyStubs(t)

	installer := filepath.Join(os.Getenv("OK_HOME"), "update", "OpenKnowledge-Setup-99.0.0.exe")
	if err := os.MkdirAll(filepath.Dir(installer), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(installer, []byte("fake installer"), 0o644); err != nil {
		t.Fatal(err)
	}
	setUpdateJobDone(t, installer)

	body := doJSON(t, h, "POST", "/api/update/apply")
	if body["ok"] != true {
		t.Fatalf("resp = %v, want {ok:true}", body)
	}

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("apply goroutine 未走到 exitProcess")
	}
	want := []string{"stopDaemon", "runInstaller:OpenKnowledge-Setup-99.0.0.exe", "spawnOkd", "exit:0"}
	if fmt.Sprint(*events) != fmt.Sprint(want) {
		t.Fatalf("events = %v, want %v", *events, want)
	}
	if _, err := os.Stat(upgradeMarkPath()); !os.IsNotExist(err) {
		t.Fatalf("熔断标记应已删除, stat err = %v", err)
	}
}

// 前置校验：任务未 done → 409；done 但文件丢失 → 400。均不得进入升级序列。
func TestUpdateApplyNotReady(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("apply 仅 Windows")
	}
	h, _ := changelogEnv(t)
	resetUpdateDownloadJob(t)
	events, done := withApplyStubs(t)

	post := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/api/update/apply", nil)
		req.Header.Set("X-Ok-Token", testToken)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	// idle → 409
	if rec := post(); rec.Code != http.StatusConflict {
		t.Fatalf("idle -> %d, want 409: %s", rec.Code, rec.Body.String())
	}
	// running → 409
	updMu.Lock()
	updCur = &updJob{State: "running"}
	updMu.Unlock()
	if rec := post(); rec.Code != http.StatusConflict {
		t.Fatalf("running -> %d, want 409: %s", rec.Code, rec.Body.String())
	}
	// done 但安装器文件不存在 → 400
	setUpdateJobDone(t, filepath.Join(os.Getenv("OK_HOME"), "update", "missing.exe"))
	if rec := post(); rec.Code != http.StatusBadRequest {
		t.Fatalf("missing file -> %d, want 400: %s", rec.Code, rec.Body.String())
	}

	select {
	case <-done:
		t.Fatal("校验失败不应进入升级序列")
	default:
	}
	if len(*events) != 0 {
		t.Fatalf("events = %v, want 空（升级序列不应启动）", *events)
	}
}
