package syncx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// mkConflict 制造 rebase 冲突态，返回 (dirB, 冲突文件内容（含标记）)。
func mkConflict(t *testing.T) (string, string) {
	t.Helper()
	_, dirA, dirB := mkPair(t)
	writeFile(t, dirA, "k.md", "base 行\nv2a A 的修改\n")
	if o := Open(dirA).Sync("sync: a"); o.Err != nil {
		t.Fatalf("A sync: %v", o.Err)
	}
	writeFile(t, dirB, "k.md", "base 行\nv2b B 的修改\n")
	// B 手工 commit + pull --rebase 制造冲突
	if _, err := Open(dirB).CommitAll("sync: b"); err != nil {
		t.Fatalf("B commit: %v", err)
	}
	conflicts, err := Open(dirB).PullRebase()
	if err != nil || len(conflicts) != 1 {
		t.Fatalf("pull should conflict: %v %v", conflicts, err)
	}
	data, err := os.ReadFile(filepath.Join(dirB, "k.md"))
	if err != nil {
		t.Fatal(err)
	}
	return dirB, string(data)
}

func TestConflictFilesAndVersions(t *testing.T) {
	dirB, working := mkConflict(t)
	r := Open(dirB)

	files := r.ConflictFiles()
	if len(files) != 1 || files[0] != "k.md" {
		t.Fatalf("ConflictFiles: %v", files)
	}
	if !strings.Contains(working, "<<<<<<<") {
		t.Fatalf("working should contain markers: %q", working)
	}

	base, local, remote, err := r.ConflictVersions("k.md")
	if err != nil {
		t.Fatalf("ConflictVersions: %v", err)
	}
	if !strings.Contains(base, "v1") {
		t.Fatalf("base: %q", base)
	}
	// 反转纪律专项断言（知识库条目：rebase 期间 :2:=远端 :3:=本地）
	if !strings.Contains(local, "v2b B 的修改") {
		t.Fatalf("local 必须是本机（:3:）版本: %q", local)
	}
	if !strings.Contains(remote, "v2a A 的修改") {
		t.Fatalf("remote 必须是远端（:2:）版本: %q", remote)
	}
}

func TestResolveContinueFinish(t *testing.T) {
	dirB, _ := mkConflict(t)
	r := Open(dirB)

	if err := r.ResolveFile("k.md", "base 行\n合并后的内容\n"); err != nil {
		t.Fatalf("ResolveFile: %v", err)
	}
	if got := r.ConflictFiles(); len(got) != 0 {
		t.Fatalf("after resolve: %v", got)
	}
	// rebase 仍在进行（已暂存未 continue）
	if !r.MergeInProgress() {
		t.Fatal("rebase should still be in progress after resolve")
	}
	if err := r.ContinueRebase(); err != nil {
		t.Fatalf("ContinueRebase: %v", err)
	}
	if r.MergeInProgress() {
		t.Fatal("rebase should be done after continue")
	}
	data, _ := os.ReadFile(filepath.Join(dirB, "k.md"))
	if string(data) != "base 行\n合并后的内容\n" {
		t.Fatalf("content: %q", data)
	}
	if err := r.Push(); err != nil {
		t.Fatalf("Push: %v", err)
	}
}

func TestAbortRebase(t *testing.T) {
	dirB, _ := mkConflict(t)
	r := Open(dirB)
	if err := r.AbortRebase(); err != nil {
		t.Fatalf("AbortRebase: %v", err)
	}
	if r.MergeInProgress() {
		t.Fatal("should not be in progress after abort")
	}
	// 内容回滚到 B 的本地提交（v2b，无冲突标记）
	data, _ := os.ReadFile(filepath.Join(dirB, "k.md"))
	if !strings.Contains(string(data), "v2b B 的修改") || strings.Contains(string(data), "<<<<<<<") {
		t.Fatalf("after abort: %q", data)
	}
}

func TestConflictFilesIdle(t *testing.T) {
	_, dirA, _ := mkPair(t)
	if got := Open(dirA).ConflictFiles(); got != nil {
		t.Fatalf("idle should be nil: %v", got)
	}
	if Open(dirA).MergeInProgress() {
		t.Fatal("idle should not be in progress")
	}
}
