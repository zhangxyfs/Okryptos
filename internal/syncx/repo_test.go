package syncx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRepoLifecycle(t *testing.T) {
	dir := t.TempDir()
	r := Open(dir)
	if r.IsRepo() {
		t.Fatal("empty dir should not be a repo")
	}
	if err := r.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if !r.IsRepo() {
		t.Fatal("after Init should be a repo")
	}
	if got := r.CurrentBranch(); got != "main" {
		t.Fatalf("branch = %q, want main", got)
	}
	ign, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatalf(".gitignore missing: %v", err)
	}
	for _, want := range []string{"kb.db", "kb.db-*", "state/", "*.log"} {
		if !strings.Contains(string(ign), want) {
			t.Fatalf(".gitignore missing %q:\n%s", want, ign)
		}
	}
	if got := r.RemoteURL(); got != "" {
		t.Fatalf("remote = %q, want empty", got)
	}
	if err := r.SetRemote("https://example.com/a/b.git"); err != nil {
		t.Fatalf("SetRemote: %v", err)
	}
	if got := r.RemoteURL(); got != "https://example.com/a/b.git" {
		t.Fatalf("remote = %q", got)
	}
	// 重复 SetRemote 应覆盖而非报错
	if err := r.SetRemote("https://example.com/a/c.git"); err != nil {
		t.Fatalf("SetRemote update: %v", err)
	}
	if got := r.RemoteURL(); got != "https://example.com/a/c.git" {
		t.Fatalf("remote after update = %q", got)
	}
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCommitAllAndStatus(t *testing.T) {
	dir := t.TempDir()
	r := Open(dir)
	if err := r.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	// 无变更：committed=false
	committed, err := r.CommitAll("sync: test")
	if err != nil || committed {
		t.Fatalf("empty commit: committed=%v err=%v", committed, err)
	}
	writeFile(t, dir, "INDEX.md", "# index\n")
	dirty, _, _ := r.Status()
	if !dirty {
		t.Fatal("should be dirty after new file")
	}
	committed, err = r.CommitAll("sync: test")
	if err != nil || !committed {
		t.Fatalf("commit: committed=%v err=%v", committed, err)
	}
	dirty, ahead, behind := r.Status()
	if dirty {
		t.Fatal("should be clean after commit")
	}
	// 无 upstream：ahead/behind 为零值（fail-open）
	if ahead != 0 || behind != 0 {
		t.Fatalf("no upstream: ahead=%d behind=%d", ahead, behind)
	}
}

func TestPushPullRoundtrip(t *testing.T) {
	bare := t.TempDir()
	if _, err := execGit(bare, localTimeout, "init", "--bare", "-b", "main"); err != nil {
		t.Fatalf("bare init: %v", err)
	}
	// 设备 A：init + commit + push -u
	dirA := t.TempDir()
	ra := Open(dirA)
	if err := ra.Init(); err != nil {
		t.Fatalf("A Init: %v", err)
	}
	writeFile(t, dirA, "a.md", "v1\n")
	if _, err := ra.CommitAll("sync: a"); err != nil {
		t.Fatalf("A commit: %v", err)
	}
	if err := ra.SetRemote(bare); err != nil {
		t.Fatalf("A SetRemote: %v", err)
	}
	if err := ra.Push(); err != nil {
		t.Fatalf("A Push: %v", err)
	}
	// 设备 B：目录内有骨架文件（模拟 ok init 生成的 config.toml/state/），CloneToDir 应覆盖
	dirB := t.TempDir()
	writeFile(t, dirB, "config.toml", "# local skeleton\n")
	rb := Open(dirB)
	if err := rb.CloneToDir(bare); err != nil {
		t.Fatalf("B CloneToDir: %v", err)
	}
	if !rb.IsRepo() {
		t.Fatal("B should be a repo after CloneToDir")
	}
	data, err := os.ReadFile(filepath.Join(dirB, "a.md"))
	if err != nil || string(data) != "v1\n" {
		t.Fatalf("B a.md: %v %q", err, data)
	}
	// A 再推一个提交，B 能 pull 到（PullRebase 属 Task 4，这里仅验证 ahead/behind 计数）
	writeFile(t, dirA, "b.md", "v2\n")
	if _, err := ra.CommitAll("sync: a2"); err != nil {
		t.Fatalf("A commit2: %v", err)
	}
	if err := ra.Push(); err != nil {
		t.Fatalf("A Push2: %v", err)
	}
	if _, err := rbCloneFetch(rb); err != nil {
		t.Fatalf("B fetch: %v", err)
	}
	_, _, behind := rb.Status()
	if behind != 1 {
		t.Fatalf("B behind = %d, want 1", behind)
	}
}

// rbCloneFetch 是测试内 helper：fetch 让 behind 计数可见。
func rbCloneFetch(r *Repo) (string, error) {
	return execGit(r.Dir, networkTimeout, "fetch", "origin")
}
