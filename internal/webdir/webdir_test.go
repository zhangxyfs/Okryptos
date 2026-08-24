package webdir

import (
	"os"
	"path/filepath"
	"testing"
)

// exe 目录无 web（go test 二进制在临时构建目录），两条用例均走 cwd 分支，
// Windows/Linux 行为一致。

func TestFindCwd(t *testing.T) {
	dir := t.TempDir()
	web := filepath.Join(dir, "web")
	if err := os.Mkdir(web, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)

	got, err := Find()
	if err != nil {
		t.Fatal(err)
	}
	// 返回值可能与 web 差一个符号链接解析（如 macOS /tmp），按 EvalSymlinks 对齐
	want, err := filepath.EvalSymlinks(web)
	if err != nil {
		t.Fatal(err)
	}
	if got != want && got != web {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestFindMissing(t *testing.T) {
	t.Chdir(t.TempDir()) // 空目录，无 web
	if _, err := Find(); err == nil {
		t.Fatal("期望找不到 web 目录时报错")
	}
}
