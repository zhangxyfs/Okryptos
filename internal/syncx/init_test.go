package syncx

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitLocalOnly(t *testing.T) {
	r := Open(t.TempDir())
	kind, err := r.InitForSync("", true, "sync: init")
	if err != nil || kind != InitLocalOnly {
		t.Fatalf("%v %v", kind, err)
	}
	if !r.IsRepo() {
		t.Fatal("should be repo")
	}
}

func TestInitClone(t *testing.T) {
	bare, dirA, _ := mkPair(t) // bare 已有 A 的一个提交
	_ = dirA
	dir := t.TempDir()
	writeFile(t, dir, "config.toml", "# skeleton\n") // 骨架文件不算内容
	kind, err := Open(dir).InitForSync(bare, false, "sync: init")
	if err != nil || kind != InitCloned {
		t.Fatalf("%v %v", kind, err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "k.md"))
	if err != nil || !strings.Contains(string(data), "v1") {
		t.Fatalf("cloned content: %v %q", err, data)
	}
}

func TestInitPushFirstDevice(t *testing.T) {
	bare := t.TempDir()
	if _, err := execGit(bare, localTimeout, "init", "--bare", "-b", "main"); err != nil {
		t.Fatalf("bare: %v", err)
	}
	dir := t.TempDir()
	writeFile(t, dir, "k.md", "v1\n")
	kind, err := Open(dir).InitForSync(bare, true, "sync: init")
	if err != nil || kind != InitPushed {
		t.Fatalf("%v %v", kind, err)
	}
	out, _ := execGit(bare, localTimeout, "log", "--oneline", "main")
	if out == "" {
		t.Fatal("bare should have commits")
	}
}

func TestInitRemoteNotEmpty(t *testing.T) {
	bare, _, _ := mkPair(t) // 远端有内容
	dir := t.TempDir()
	writeFile(t, dir, "k.md", "本机内容\n") // 本地也有内容
	_, err := Open(dir).InitForSync(bare, true, "sync: init")
	if !errors.Is(err, ErrRemoteNotEmpty) {
		t.Fatalf("want ErrRemoteNotEmpty, got %v", err)
	}
}
