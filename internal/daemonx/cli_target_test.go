package daemonx

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func okName() string {
	if runtime.GOOS == "windows" {
		return "ok.exe"
	}
	return "ok"
}

func okdName() string {
	if runtime.GOOS == "windows" {
		return "okd.exe"
	}
	return "okd"
}

// daemon 进程（okd）内取同目录 ok：gui-split 后 okd 无子命令，注册/转发
// 必须落到 ok。
func TestCliTargetForOkdResolvesSiblingOk(t *testing.T) {
	dir := t.TempDir()
	okd := filepath.Join(dir, okdName())
	ok := filepath.Join(dir, okName())
	for _, p := range []string{okd, ok} {
		if err := os.WriteFile(p, []byte("x"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	got, err := CliTargetFor(okd)
	if err != nil {
		t.Fatal(err)
	}
	if got != ok {
		t.Fatalf("exe=%q, want sibling ok %q", got, ok)
	}
}

// okd 孤儿部署（无同目录 ok）必须报错：静默回落 okd 会注册出失效命令
// （gui-split 后 okd 对 hook 等子命令只会空转启动 daemon）。
func TestCliTargetForOrphanOkdErrors(t *testing.T) {
	dir := t.TempDir()
	okd := filepath.Join(dir, okdName())
	if err := os.WriteFile(okd, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := CliTargetFor(okd); err == nil {
		t.Fatal("orphan okd should error")
	}
}

// 非 okd 进程（ok 自身 / 测试二进制）返回自身。
func TestCliTargetForNonOkdReturnsSelf(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{okName(), "agent.test", "some-binary"} {
		self := filepath.Join(dir, name)
		got, err := CliTargetFor(self)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got != self {
			t.Fatalf("%s: exe=%q, want self", name, got)
		}
	}
}
