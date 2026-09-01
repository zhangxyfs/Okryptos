package gui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
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
