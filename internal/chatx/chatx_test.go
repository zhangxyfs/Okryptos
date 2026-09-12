package chatx

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"
)

func TestManifestWellformed(t *testing.T) {
	if len(Models) != 3 {
		t.Fatalf("Models 应有 3 条，实际 %d", len(Models))
	}
	if Models[0].ID != "qwen3-1.7b-q8" {
		t.Fatalf("首条应为默认推荐 qwen3-1.7b-q8，实际 %s", Models[0].ID)
	}
	hexRe := regexp.MustCompile(`^[0-9a-f]{64}$`)
	seen := map[string]bool{}
	for _, m := range Models {
		if seen[m.ID] {
			t.Errorf("模型 ID 重复：%s", m.ID)
		}
		seen[m.ID] = true
		if m.Size <= 0 {
			t.Errorf("%s: Size 应 > 0，实际 %d", m.ID, m.Size)
		}
		if !hexRe.MatchString(m.SHA256) {
			t.Errorf("%s: SHA256 应为 64 位小写 hex，实际 %q", m.ID, m.SHA256)
		}
	}
	if _, ok := FindModel("qwen3-0.6b-q8"); !ok {
		t.Error("FindModel 应命中 qwen3-0.6b-q8")
	}
	if _, ok := FindModel("no-such-model"); ok {
		t.Error("FindModel 不应命中 no-such-model")
	}
}

func TestInstalledPathAndInstalled(t *testing.T) {
	dir := t.TempDir()
	m := Model{ID: "t-model", Size: 16}
	if got, want := m.InstalledPath(dir), filepath.Join(dir, "t-model.gguf"); got != want {
		t.Fatalf("InstalledPath = %s，期望 %s", got, want)
	}
	if m.Installed(dir) {
		t.Error("文件不存在时 Installed 应为 false")
	}
	if err := os.WriteFile(m.InstalledPath(dir), make([]byte, m.Size-1), 0o644); err != nil {
		t.Fatal(err)
	}
	if m.Installed(dir) {
		t.Error("尺寸少 1 字节时 Installed 应为 false")
	}
	if err := os.WriteFile(m.InstalledPath(dir), make([]byte, m.Size), 0o644); err != nil {
		t.Fatal(err)
	}
	if !m.Installed(dir) {
		t.Error("尺寸一致时 Installed 应为 true")
	}
}

func TestDownloadMapsToEmbed(t *testing.T) {
	content := []byte("chatx download thin-wrapper test payload, not a real GGUF.")
	sum := sha256.Sum256(content)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "model.gguf", time.Now(), bytes.NewReader(content))
	}))
	defer srv.Close()

	dir := t.TempDir()
	m := Model{ID: "t-chat", Size: int64(len(content)), SHA256: hex.EncodeToString(sum[:])}
	var progressed bool
	progress := func(done, total int64) {
		progressed = true
		if total != m.Size {
			t.Errorf("progress total = %d，期望 %d", total, m.Size)
		}
	}
	if err := Download(context.Background(), srv.Client(), m, srv.URL, dir, progress); err != nil {
		t.Fatalf("Download: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "t-chat.gguf"))
	if err != nil {
		t.Fatalf("落盘文件读取：%v", err)
	}
	if string(got) != string(content) {
		t.Errorf("落盘内容不一致")
	}
	if !progressed {
		t.Error("progress 回调应被调用")
	}
	if _, err := os.Stat(filepath.Join(dir, "t-chat.gguf.part")); !os.IsNotExist(err) {
		t.Error("下载完成后 .part 不应残留")
	}
}
