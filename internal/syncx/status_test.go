package syncx

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStatusRoundtrip(t *testing.T) {
	stateDir := t.TempDir()
	sf, err := LoadStatus(stateDir)
	if err != nil {
		t.Fatalf("LoadStatus empty: %v", err)
	}
	l := sf.Layer("personal")
	l.Ahead = 2
	l.LastError = ""
	if err := sf.Save(stateDir); err != nil {
		t.Fatalf("Save: %v", err)
	}
	sf2, err := LoadStatus(stateDir)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if sf2.Layer("personal").Ahead != 2 {
		t.Fatalf("ahead = %d", sf2.Layer("personal").Ahead)
	}
}

func TestConflictFilesLifecycle(t *testing.T) {
	stateDir := t.TempDir()
	if files, _ := ReadConflictFiles(stateDir); len(files) != 0 {
		t.Fatalf("initial: %v", files)
	}
	if err := WriteConflictFiles(stateDir, []string{"a.md", "b.md"}); err != nil {
		t.Fatalf("write: %v", err)
	}
	files, err := ReadConflictFiles(stateDir)
	if err != nil || len(files) != 2 || files[0] != "a.md" {
		t.Fatalf("read: %v %v", files, err)
	}
	if err := ClearConflictFiles(stateDir); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "sync-conflict.json")); !os.IsNotExist(err) {
		t.Fatalf("file should be gone: %v", err)
	}
}

func TestRecordOutcome(t *testing.T) {
	// 真仓（无远端）+ 状态目录
	dir := t.TempDir()
	r := Open(dir)
	if err := r.Init(); err != nil {
		t.Fatalf("init: %v", err)
	}
	writeFile(t, dir, "k.md", "v1\n")
	stateDir := t.TempDir()

	// 成功：last_sync 落时间、conflict 清空
	o := r.Sync("sync: test")
	RecordOutcome(dir, stateDir, o)
	sf, _ := LoadStatus(stateDir)
	l := sf.Layer("personal")
	if l.LastSync.IsZero() || l.Conflict || l.LastError != "" {
		t.Fatalf("after success: %+v", l)
	}

	// 冲突：conflict=true 且写冲突文件
	RecordOutcome(dir, stateDir, Outcome{Conflicts: []string{"k.md"}})
	sf, _ = LoadStatus(stateDir)
	if !sf.Layer("personal").Conflict {
		t.Fatal("conflict not recorded")
	}
	files, _ := ReadConflictFiles(stateDir)
	if len(files) != 1 || files[0] != "k.md" {
		t.Fatalf("conflict files: %v", files)
	}

	// 错误：last_error 记录，不清 conflict
	RecordOutcome(dir, stateDir, Outcome{Err: os.ErrNotExist})
	sf, _ = LoadStatus(stateDir)
	if sf.Layer("personal").LastError == "" {
		t.Fatal("last_error not recorded")
	}

	// 再次成功：conflict 清除、冲突文件删除
	RecordOutcome(dir, stateDir, Outcome{})
	sf, _ = LoadStatus(stateDir)
	if sf.Layer("personal").Conflict || sf.Layer("personal").LastError != "" {
		t.Fatalf("recovery: %+v", sf.Layer("personal"))
	}
	if files, _ := ReadConflictFiles(stateDir); len(files) != 0 {
		t.Fatalf("conflict files should be cleared: %v", files)
	}
}
