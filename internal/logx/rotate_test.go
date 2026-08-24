package logx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 未超限不轮替：文件原地不动，不产生 logs 目录。
func TestRotateIfOversizeSmallFileUntouched(t *testing.T) {
	home := t.TempDir()
	p := filepath.Join(home, "ok.log")
	if err := os.WriteFile(p, []byte("line1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	RotateIfOversize(p)
	data, err := os.ReadFile(p)
	if err != nil || string(data) != "line1\n" {
		t.Fatalf("小文件不应轮替: data=%q err=%v", data, err)
	}
	if _, err := os.Stat(filepath.Join(home, "logs")); !os.IsNotExist(err) {
		t.Fatalf("不应产生 logs 目录: %v", err)
	}
}

// 超限轮替：原文件改名为 logs/ok-<日期>_<时分秒>.log，原路径可被重开为新文件。
func TestRotateIfOversizeArchives(t *testing.T) {
	home := t.TempDir()
	p := filepath.Join(home, "ok.log")
	if err := os.WriteFile(p, []byte(strings.Repeat("x", 1024)), 0o644); err != nil {
		t.Fatal(err)
	}
	old := maxLogBytes
	maxLogBytes = 100
	t.Cleanup(func() { maxLogBytes = old })

	RotateIfOversize(p)
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatalf("轮替后原路径应已改名: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(home, "logs"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("logs 应恰有 1 个归档: %v %v", entries, err)
	}
	name := entries[0].Name()
	if !strings.HasPrefix(name, "ok-") || !strings.HasSuffix(name, ".log") {
		t.Fatalf("归档名应形如 ok-<日期>_<时分秒>.log: %s", name)
	}
	// 重开 append：新文件只含新内容
	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("new\n"); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	data, _ := os.ReadFile(p)
	if string(data) != "new\n" {
		t.Fatalf("轮替后应为全新文件: %q", data)
	}
}

// 同一秒两次轮替撞名：第二次追加序号，两份归档都保留。
func TestRotateIfOversizeNameCollision(t *testing.T) {
	home := t.TempDir()
	p := filepath.Join(home, "daemon.log")
	old := maxLogBytes
	maxLogBytes = 10
	t.Cleanup(func() { maxLogBytes = old })

	for _, content := range []string{"first-rotation-xx", "second-rotation-x"} {
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		RotateIfOversize(p)
	}
	entries, err := os.ReadDir(filepath.Join(home, "logs"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("同秒两次轮替应产生 2 个归档: %v", entries)
	}
}

// 过期归档清理：超保留期的删除，新归档与非 .log 文件保留。
func TestCleanArchives(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "logs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	oldLog := filepath.Join(dir, "ok-2026-01-01_000000.log")
	newLog := filepath.Join(dir, "ok-2099-01-01_000000.log")
	other := filepath.Join(dir, "note.txt")
	for _, p := range []string{oldLog, newLog, other} {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// oldLog 的 mtime 拨到保留期之前；newLog 的未来 mtime 必在保留期内
	oldTime := time.Now().Add(-time.Duration(archiveKeepDays+1) * 24 * time.Hour)
	if err := os.Chtimes(oldLog, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	CleanArchives(dir)
	if _, err := os.Stat(oldLog); !os.IsNotExist(err) {
		t.Fatalf("过期归档应删除: %v", err)
	}
	for _, p := range []string{newLog, other} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("%s 应保留: %v", p, err)
		}
	}
}
