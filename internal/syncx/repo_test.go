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
