package e2e

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gitBare 建临时裸仓，返回路径。
func gitBare(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	cmd := exec.Command("git", "init", "--bare", "-b", "main", dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init --bare: %v\n%s", err, out)
	}
	return dir
}

// mkHome 建一台"设备"：OK_HOME + kimi 目录 + 项目工作目录 + ok init 注册。
// 返回 (home, proj)。项目名固定 demo（两个 home 各自独立互不干扰）。
func mkHome(t *testing.T) (string, string) {
	t.Helper()
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "kimi"), 0o755); err != nil {
		t.Fatal(err)
	}
	proj := filepath.Join(home, "demo")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	if stdout, _, code := runOK(t, home, proj, "", "init", "demo"); code != 0 {
		t.Fatalf("init: code=%d out=%q", code, stdout)
	}
	return home, proj
}

// addEntry 用 ok add 写一条知识条目。
func addEntry(t *testing.T, home, proj, title, body string) {
	t.Helper()
	bodyFile := filepath.Join(proj, "body.md")
	if err := os.WriteFile(bodyFile, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if stdout, _, code := runOK(t, home, proj, "", "add", "--title", title, "--type", "note", "--file", bodyFile); code != 0 {
		t.Fatalf("add %s: code=%d out=%q", title, code, stdout)
	}
}

// overwriteEntry 直接改条目文件正文（模拟另一台设备的手工编辑，走 mtime 重建）。
func overwriteEntry(t *testing.T, home, title, newBody string) {
	t.Helper()
	kdir := filepath.Join(home, "projects", "demo", "knowledge")
	entries, err := os.ReadDir(kdir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), title) {
			data, err := os.ReadFile(filepath.Join(kdir, e.Name()))
			if err != nil {
				t.Fatal(err)
			}
			// 保留 front matter（第一个 --- 到第二个 ---），替换正文
			s := string(data)
			idx := strings.Index(s[3:], "---")
			if idx < 0 {
				t.Fatalf("no front matter in %s", e.Name())
			}
			fm := s[:3+idx+3]
			if err := os.WriteFile(filepath.Join(kdir, e.Name()), []byte(fm+"\n"+newBody+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			return
		}
	}
	t.Fatalf("entry %s not found in %s", title, kdir)
}

func TestSyncTwoDevices(t *testing.T) {
	bare := gitBare(t)
	homeA, projA := mkHome(t)
	homeB, projB := mkHome(t)

	// 设备 A：写条目 → sync init → 首推
	addEntry(t, homeA, projA, "部署清单", "上线前先跑回归测试套件。")
	stdout, stderr, code := runOK(t, homeA, projA, "", "sync", "init", bare)
	if code != 0 {
		t.Fatalf("A sync init: code=%d out=%q err=%q", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "推送") {
		t.Fatalf("A sync init output: %q", stdout)
	}

	// 设备 B：sync init → clone → 能检索到 A 的条目
	stdout, stderr, code = runOK(t, homeB, projB, "", "sync", "init", bare)
	if code != 0 || !strings.Contains(stdout, "克隆") {
		t.Fatalf("B sync init: code=%d out=%q err=%q", code, stdout, stderr)
	}
	stdout, _, code = runOK(t, homeB, projB, "", "search", "部署清单")
	if code != 0 || !strings.Contains(stdout, "部署清单") {
		t.Fatalf("B search after clone: code=%d out=%q", code, stdout)
	}

	// 设备 A 再写一条 → sync；设备 B sync → 也能看到
	addEntry(t, homeA, projA, "发布清单", "发布前先跑 verify-deb。")
	stdout, _, code = runOK(t, homeA, projA, "", "sync")
	if code != 0 || !strings.Contains(stdout, "推送") {
		t.Fatalf("A sync: code=%d out=%q", code, stdout)
	}
	stdout, _, code = runOK(t, homeB, projB, "", "sync")
	if code != 0 || !strings.Contains(stdout, "拉取") {
		t.Fatalf("B sync: code=%d out=%q", code, stdout)
	}
	stdout, _, code = runOK(t, homeB, projB, "", "search", "发布清单")
	if code != 0 || !strings.Contains(stdout, "发布清单") {
		t.Fatalf("B search after sync: code=%d out=%q", code, stdout)
	}
	// 状态文件落盘且 conflict=false
	data, err := os.ReadFile(filepath.Join(homeB, "projects", "demo", "state", "sync-status.json"))
	if err != nil {
		t.Fatalf("B status file: %v", err)
	}
	if !strings.Contains(string(data), `"conflict": false`) {
		t.Fatalf("B status: %s", data)
	}
}

func TestSyncConflict(t *testing.T) {
	bare := gitBare(t)
	homeA, projA := mkHome(t)
	homeB, projB := mkHome(t)

	addEntry(t, homeA, projA, "冲突条目", "v1 原始内容")
	if _, _, code := runOK(t, homeA, projA, "", "sync", "init", bare); code != 0 {
		t.Fatalf("A sync init: code=%d", code)
	}
	if _, _, code := runOK(t, homeB, projB, "", "sync", "init", bare); code != 0 {
		t.Fatalf("B sync init: code=%d", code)
	}

	// A 改为 v2a 并同步推送
	overwriteEntry(t, homeA, "冲突条目", "v2a A 的修改")
	if stdout, _, code := runOK(t, homeA, projA, "", "sync"); code != 0 || !strings.Contains(stdout, "推送") {
		t.Fatalf("A sync: code=%d out=%q", code, stdout)
	}

	// B 改同一位置为 v2b → sync 必冲突
	overwriteEntry(t, homeB, "冲突条目", "v2b B 的修改")
	stdout, stderr, code := runOK(t, homeB, projB, "", "sync")
	if code == 0 {
		t.Fatalf("B sync should fail on conflict: out=%q", stdout)
	}
	if !strings.Contains(stderr, "冲突") {
		t.Fatalf("B sync stderr should mention conflict: %q", stderr)
	}
	// sync-conflict.json 落盘且含条目文件
	cfPath := filepath.Join(homeB, "projects", "demo", "state", "sync-conflict.json")
	data, err := os.ReadFile(cfPath)
	if err != nil {
		t.Fatalf("conflict file: %v", err)
	}
	var cf struct {
		Files []string `json:"files"`
	}
	if err := json.Unmarshal(data, &cf); err != nil || len(cf.Files) == 0 {
		t.Fatalf("conflict json: %v %s", err, data)
	}
	// 内容不丢：B 的改动仍在工作区（冲突标记内）
	kdir := filepath.Join(homeB, "projects", "demo", "knowledge")
	entries, _ := os.ReadDir(kdir)
	found := false
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "冲突条目") {
			body, _ := os.ReadFile(filepath.Join(kdir, e.Name()))
			if strings.Contains(string(body), "v2b B 的修改") {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("B 的本地修改丢失")
	}
	// 清理：中止 rebase（不影响后续测试，TempDir 会删，但 Windows 上 git 进程须已退出）
	cmd := exec.Command("git", "-C", filepath.Join(homeB, "projects", "demo"), "rebase", "--abort")
	_ = cmd.Run()
}
